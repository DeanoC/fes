#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Regenerate offline digital SID vectors from unmodified VICE 3.10 reSID."""
import argparse
import hashlib
from pathlib import Path
import shutil
import subprocess
import tempfile

SOURCE_HASHES = {'envelope.h': '52fdf21883cd05110e78a7fbb98f215cb950c2eb83574153e38bf0d9f1e7c0da',
 'envelope.cc': '35a0c17737ec37aeb40a929ddac28a4f20885e73e4478e8aacd15f50ba172dc9',
 'dac.h': '9fa86d5c2fd3e5644871de1cdfc0eb7296b7604f68845bd7861dc16ba8d21e6b',
 'dac.cc': '1b1133be332283dad798234fad913094e3ac222bb49f5be6a14d58e9842034fb',
 'resid-config.h': 'ccd0047dbaf8265e7a58b2fb4232dacfa441ffa7072f42eff80552f52feafe52',
 'siddefs.h.in': '944f934cb87020f70811e7cb71de7a58968c09c86750046968f4c7c98b318d77'}
CONFIG = {'RESID_INLINING': '1',
 'RESID_INLINE': 'inline',
 'RESID_BRANCH_HINTS': '0',
 'NEW_8580_FILTER': '0',
 'HAVE_BOOL': '1',
 'HAVE_BUILTIN_EXPECT': '1',
 'HAVE_LOG1P': '1'}

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--vice-source',type=Path,required=True)
    parser.add_argument('--output',type=Path)
    parser.add_argument('--component',choices=['all','sid','envelope'],default='all')
    args=parser.parse_args()
    if args.output and args.component=='all':parser.error('--output requires one --component')
    module=Path(__file__).resolve().parents[1]
    sim=module/'cores/fes-c64/sim'
    source=args.vice_source/'src/resid'
    for name,digest in SOURCE_HASHES.items():
        if hashlib.sha256((source/name).read_bytes()).hexdigest()!=digest:
            parser.error(f'{name}: expected unchanged VICE 3.10 source')
    with tempfile.TemporaryDirectory(prefix='c64-sid-reference-') as directory:
        temp=Path(directory)
        for name in SOURCE_HASHES:
            if name!='siddefs.h.in':shutil.copy2(source/name,temp/name)
        config=(source/'siddefs.h.in').read_text()
        for key,value in CONFIG.items():config=config.replace('@'+key+'@',value)
        (temp/'siddefs.h').write_text(config)
        selected=['sid','envelope'] if args.component=='all' else [args.component]
        for component in selected:
            stem='sid' if component=='sid' else 'sid_envelope'
            subprocess.run(['c++','-std=c++17','-O2','-DC64_SID_ORACLE','-I'+str(temp),
                            str(sim/(stem+'_tb.cpp')),str(temp/'envelope.cc'),str(temp/'dac.cc'),
                            '-o',str(temp/'oracle')],check=True)
            result=subprocess.run([str(temp/'oracle')],check=True,capture_output=True,text=True)
            output=args.output or sim/(stem+'_vectors.h')
            output.write_text(result.stdout)
            print(output)

if __name__=='__main__':main()
