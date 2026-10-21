#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Generate synthetic complete-SID checkpoints using pinned VICE 3.10 sources.

The upstream kernel is unmodified. A test-only derived Filter configures its
writable lookup data to the documented bounded policy; VCR/cutoff stay exact.
Normal simulation reads only the generated header and needs no VICE checkout.
"""
import argparse
import hashlib
import re
from pathlib import Path
import shutil
import subprocess
import tempfile

SOURCE_HASHES = {'COPYING': '3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986',
 'dac.cc': '1b1133be332283dad798234fad913094e3ac222bb49f5be6a14d58e9842034fb',
 'dac.h': '9fa86d5c2fd3e5644871de1cdfc0eb7296b7604f68845bd7861dc16ba8d21e6b',
 'envelope.cc': '35a0c17737ec37aeb40a929ddac28a4f20885e73e4478e8aacd15f50ba172dc9',
 'envelope.h': '52fdf21883cd05110e78a7fbb98f215cb950c2eb83574153e38bf0d9f1e7c0da',
 'extfilt.cc': 'ce4ea29de782fcd29ad6862c1b5b72f981f2f9a0afbc1f3c94133040298b896d',
 'extfilt.h': 'c4b6a8d8269c54ed3e8e487d28f80270ab5120b98e9346c3d0c4caeea28cd99b',
 'filter.cc': 'ab6d96909324ca93f9606d830d7d363750f0c3af31211c34d64d30b776277ffc',
 'filter.h': '61eb34bb7f104cc4a14e73eb940a0e5737b94e0c126075320702e426624b89ef',
 'filter8580new.cc': 'f37a03ea0c1884962c07d1561fd1351a127691f97855a6517c4690b174f2a2a4',
 'filter8580new.h': 'aeead0dd6701d54640053b3fd903f80b91e2783259798baa9b65f6284bc8ede0',
 'pot.cc': '09615ac768a4365eddebcf823dd15b41dce1e54054f752ead882077452d12b12',
 'pot.h': '09598ab1f1fac91adcd052173b749e27393c5d27d0df091d2869dc7856c9cf59',
 'resid-config.h': 'ccd0047dbaf8265e7a58b2fb4232dacfa441ffa7072f42eff80552f52feafe52',
 'sid.cc': 'a8b780d55c3a2ee8c34e1fd3c669ad25c35b585e3275043d0cd68efd57b8958c',
 'sid.h': '68ca9a9ff517115b0b25d79d3aea66e7fea993fa722cd1c68ca21e40bd4fdc62',
 'siddefs.h.in': '944f934cb87020f70811e7cb71de7a58968c09c86750046968f4c7c98b318d77',
 'spline.h': '60005cd321c0acf006548e682a4b0f4e3736070b1fac6a47c579fe1f6b8d1dea',
 'version.cc': '01813586e8d7fde27d27fb884deca97413814de9163f43948cd5e79d1229ef88',
 'voice.cc': '03eaccfd0c1c37572b76ae8b7889a05c3b862601e73bbb4373f586ebad18eef2',
 'voice.h': 'e40645d7de7ac6c86de891c4a0352608d8cd5d3047ac47e5ba119e866dfc475f',
 'wave.cc': 'fbbc5c590ecf343ff70a54ce7e4861a0f674a05fe27561c497c61ad749f63673',
 'wave.h': '8b916531e274c97d5eb651ed546c02ec5f8ac7a6a024d6a4db97602dd147cf9f',
 'wave6581_PST.h': '5593ba67f9dd7d47c164cba85b431887934db802e3b51cf82305ff2631efe01a',
 'wave6581_PS_.h': '473e8728f6c4e2a753145d650796de3c88a7c2a834a37ea1eaab94b6bacc9290',
 'wave6581_P_T.h': '5363c6bd6c2ecc5b2351b5552e51ee7a0c9d8a52d77b2d7971ee9f8560932500',
 'wave6581__ST.h': '44211b6a1b1d900ac67b2700407b1d3b6ca15abdbd1a4acba4e10d632f7a08f0',
 'wave8580_PST.h': '4c3e6b4edc3ba34ec972baab501f68b812509b24c56383be782b9f1bc33e7c52',
 'wave8580_PS_.h': '676476fbfe85efe2c5e0d1ce8335d278cf39ada8df131a7198cb8499cbb8e51c',
 'wave8580_P_T.h': '3a5d607aafc9646f6118de5a3d1b9a4bafccf700651da4acb1b1134832df883a',
 'wave8580__ST.h': '5ea0a62754b5b3ef98d5870ef33de54edde64964d31ef9549bca0e3cb0b32169'}
CONFIG = {'RESID_INLINING':'1', 'RESID_INLINE':'inline', 'RESID_BRANCH_HINTS':'0',
          'NEW_8580_FILTER':'0', 'HAVE_BOOL':'1', 'HAVE_BUILTIN_EXPECT':'1', 'HAVE_LOG1P':'1'}


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--vice-source', type=Path, required=True)
    parser.add_argument('--sim-dir', type=Path, default=Path(__file__).resolve().parents[1]/'cores/fes-c64/sim')
    parser.add_argument('--output', type=Path)
    parser.add_argument('--check', action='store_true')
    args=parser.parse_args();source=args.vice_source/'src/resid'
    for name,digest in SOURCE_HASHES.items():
        try:actual=hashlib.sha256((source/name).read_bytes()).hexdigest()
        except OSError as error:parser.error(str(error))
        if actual!=digest:parser.error(f'{name}: expected unchanged VICE 3.10 source')
    with tempfile.TemporaryDirectory(prefix='c64-sid-audio-oracle-') as directory:
        temp=Path(directory)
        for name in SOURCE_HASHES:
            if name!='siddefs.h.in':shutil.copy2(source/name,temp/name)
        config=(source/'siddefs.h.in').read_text()
        for key,value in CONFIG.items():config=config.replace('@'+key+'@',value)
        if re.search(r'@[A-Z_]+@',config):parser.error('unexpanded reSID configuration')
        (temp/'siddefs.h').write_text(config)
        command=['c++','-std=c++17','-O2','-DC64_SID_AUDIO_ORACLE','-I'+str(temp),
                 '-I'+str(args.sim_dir),str(args.sim_dir/'sid_audio_tb.cpp')]
        command += [str(temp/(name+'.cc')) for name in ('sid','voice','wave','envelope','filter','extfilt','pot','dac')]
        subprocess.run(command+['-o',str(temp/'oracle')],check=True)
        result=subprocess.run([str(temp/'oracle')],check=True,capture_output=True,text=True)
    output=args.output or args.sim_dir/'sid_audio_vectors.h'
    if args.check:
        if output.read_text()!=result.stdout:parser.error(f'{output}: generated fixture differs')
        print(f'{output}: unchanged')
    else:
        output.parent.mkdir(parents=True,exist_ok=True);output.write_text(result.stdout);print(output)


if __name__=='__main__':main()
