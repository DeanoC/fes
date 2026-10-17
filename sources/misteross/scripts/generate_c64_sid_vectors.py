#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Regenerate offline digital SID vectors from unmodified VICE 3.10 reSID."""
import argparse
import hashlib
import re
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
# VICE 3.10 reSID WaveformGenerator and every compiled dependency.
OSCILLATOR_SOURCE_HASHES = {'wave.cc': 'fbbc5c590ecf343ff70a54ce7e4861a0f674a05fe27561c497c61ad749f63673',
 'wave.h': '8b916531e274c97d5eb651ed546c02ec5f8ac7a6a024d6a4db97602dd147cf9f',
 'dac.cc': '1b1133be332283dad798234fad913094e3ac222bb49f5be6a14d58e9842034fb',
 'dac.h': '9fa86d5c2fd3e5644871de1cdfc0eb7296b7604f68845bd7861dc16ba8d21e6b',
 'resid-config.h': 'ccd0047dbaf8265e7a58b2fb4232dacfa441ffa7072f42eff80552f52feafe52',
 'siddefs.h.in': '944f934cb87020f70811e7cb71de7a58968c09c86750046968f4c7c98b318d77',
 'wave6581__ST.h': '44211b6a1b1d900ac67b2700407b1d3b6ca15abdbd1a4acba4e10d632f7a08f0',
 'wave6581_P_T.h': '5363c6bd6c2ecc5b2351b5552e51ee7a0c9d8a52d77b2d7971ee9f8560932500',
 'wave6581_PS_.h': '473e8728f6c4e2a753145d650796de3c88a7c2a834a37ea1eaab94b6bacc9290',
 'wave6581_PST.h': '5593ba67f9dd7d47c164cba85b431887934db802e3b51cf82305ff2631efe01a',
 'wave8580__ST.h': '5ea0a62754b5b3ef98d5870ef33de54edde64964d31ef9549bca0e3cb0b32169',
 'wave8580_P_T.h': '3a5d607aafc9646f6118de5a3d1b9a4bafccf700651da4acb1b1134832df883a',
 'wave8580_PS_.h': '676476fbfe85efe2c5e0d1ce8335d278cf39ada8df131a7198cb8499cbb8e51c',
 'wave8580_PST.h': '4c3e6b4edc3ba34ec972baab501f68b812509b24c56383be782b9f1bc33e7c52'}
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
    parser.add_argument('--check',action='store_true',help='compare generated fixtures with the selected output without modifying it')
    parser.add_argument('--component',choices=['all','sid','envelope','oscillator','oscillator-bus','coupling','oscillator-table'],default='all')
    args=parser.parse_args()
    if args.output and args.component=='all':parser.error('--output requires one --component')
    module=Path(__file__).resolve().parents[1]
    sim=module/'cores/fes-c64/sim'
    source=args.vice_source/'src/resid'
    selected=['sid','envelope','oscillator','oscillator-bus','coupling','oscillator-table'] if args.component=='all' else [args.component]
    hashes={}
    for component in selected:
        hashes.update(OSCILLATOR_SOURCE_HASHES if (component.startswith('oscillator') or component=='coupling') else SOURCE_HASHES)
    for name,digest in hashes.items():
        if hashlib.sha256((source/name).read_bytes()).hexdigest()!=digest:
            parser.error(f'{name}: expected unchanged VICE 3.10 source')
    with tempfile.TemporaryDirectory(prefix='c64-sid-reference-') as directory:
        temp=Path(directory)
        for name in hashes:
            if name!='siddefs.h.in':shutil.copy2(source/name,temp/name)
        config=(source/'siddefs.h.in').read_text()
        for key,value in CONFIG.items():config=config.replace('@'+key+'@',value)
        (temp/'siddefs.h').write_text(config)
        for component in selected:
            if component == 'oscillator-table':
                lines=['// SPDX-License-Identifier: GPL-2.0-or-later',
                       '// MOS6581 combined-wave samples from VICE 3.10 reSID.',
                       '// Copyright (C) 2010 Dag Lem <resid@nimrod.no>. See c64_sid.COPYING.']
                for name,function in [('wave6581__ST.h','combined_triangle_saw'),
                                      ('wave6581_P_T.h','combined_pulse_triangle'),
                                      ('wave6581_PS_.h','combined_pulse_saw'),
                                      ('wave6581_PST.h','combined_pulse_triangle_saw')]:
                    raw=(source/name).read_bytes()
                    text=re.sub(r'/\*.*?\*/', '', raw.decode(), flags=re.S)
                    values=[int(value,16) for value in re.findall(r'0x[0-9a-fA-F]+',text)]
                    if len(values)!=4096:parser.error(name+': requires 4096 entries')
                    lines += ['// Generated exactly from '+name+' SHA256 '+hashlib.sha256(raw).hexdigest()+'.',
                              'function automatic [11:0] '+function+'(input [11:0] index);', ' case(index)']
                    lines += [f"  12'h{index:03x}:{function}=12'h{value:03x};"
                              for index,value in enumerate(values) if value]
                    lines += ["  default:"+function+"=12'h000;", ' endcase', 'endfunction']
                result='\n'.join(lines)+'\n'
                output=args.output or module/'cores/fes-c64/rtl/c64_sid_combined_waves.vh'
                if args.check:
                    if output.read_text()!=result:parser.error(f'{output}: generated table differs')
                    print(f'{output}: unchanged')
                else:
                    output.write_text(result);print(output)
                continue
            oscillator=(component.startswith('oscillator') or component=='coupling')
            stem={'sid':'sid','envelope':'sid_envelope','oscillator':'sid_oscillator',
                  'oscillator-bus':'sid_oscillator_bus','coupling':'sid_coupling'}[component]
            define='C64_OSC_ORACLE' if oscillator else 'C64_SID_ORACLE'
            reference='wave.cc' if oscillator else 'envelope.cc'
            subprocess.run(['c++','-std=c++17','-O2','-D'+define,'-I'+str(temp),
                            str(sim/(stem+'_tb.cpp')),str(temp/reference),str(temp/'dac.cc'),
                            '-o',str(temp/'oracle')],check=True)
            result=subprocess.run([str(temp/'oracle')],check=True,capture_output=True,text=True)
            output=args.output or sim/(stem+'_vectors.h')
            if args.check:
                if output.read_text()!=result.stdout:parser.error(f'{output}: generated fixture differs')
                print(f'{output}: unchanged')
            else:
                output.write_text(result.stdout)
                print(output)

if __name__=='__main__':main()
