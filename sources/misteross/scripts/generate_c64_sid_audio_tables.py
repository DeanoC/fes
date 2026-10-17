#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Regenerate MOS6581 filter constants from pinned, unmodified VICE 3.10 reSID."""
import argparse
from array import array
import hashlib
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

HASHES = {
 'filter.h':'61eb34bb7f104cc4a14e73eb940a0e5737b94e0c126075320702e426624b89ef',
 'filter.cc':'ab6d96909324ca93f9606d830d7d363750f0c3af31211c34d64d30b776277ffc',
 'dac.h':'9fa86d5c2fd3e5644871de1cdfc0eb7296b7604f68845bd7861dc16ba8d21e6b',
 'dac.cc':'1b1133be332283dad798234fad913094e3ac222bb49f5be6a14d58e9842034fb',
 'spline.h':'60005cd321c0acf006548e682a4b0f4e3736070b1fac6a47c579fe1f6b8d1dea',
 'resid-config.h':'ccd0047dbaf8265e7a58b2fb4232dacfa441ffa7072f42eff80552f52feafe52',
 'siddefs.h.in':'944f934cb87020f70811e7cb71de7a58968c09c86750046968f4c7c98b318d77'}
CONFIG={'RESID_INLINING':'1','RESID_INLINE':'inline','RESID_BRANCH_HINTS':'0',
        'NEW_8580_FILTER':'0','HAVE_BOOL':'1','HAVE_BUILTIN_EXPECT':'1','HAVE_LOG1P':'1'}
NAMES=('gain','summer','mixer','opamp_rev','vcr_kVg','vcr_n_Ids_term')
BOUNDS=(4,4,16,1,1,1) # Last family retained for diagnostics; the filter uses exact VCR below.
NOTICE='''// SPDX-License-Identifier: GPL-2.0-or-later
// Generated MOS6581 constants from pinned VICE 3.10 reSID, by Dag Lem.
// Copyright (C) 2010 Dag Lem <resid@nimrod.no>. See c64_sid.COPYING.
// Regenerate with scripts/generate_c64_sid_audio_tables.py; never edit data.
'''
PROBE=r'''#include "filter.h"
#include <cstdio>
#include <cstdlib>
#include <string>
class Probe:public reSID::Filter {
public:void dump(const char*dir){auto&f=model_filter[0];
 auto save=[&](const char*name,const unsigned short*p,size_t count){
 std::string path=std::string(dir)+"/"+name+".bin";FILE*out=std::fopen(path.c_str(),"wb");if(!out)std::abort();
 for(size_t i=0;i<count;++i){if(std::fputc(p[i]&255,out)==EOF||std::fputc(p[i]>>8,out)==EOF)std::abort();}
 if(std::fclose(out))std::abort();};
 save("gain",&f.gain[0][0],sizeof(f.gain)/sizeof(unsigned short));
 save("summer",f.summer,sizeof(f.summer)/sizeof(unsigned short));
 save("mixer",f.mixer,sizeof(f.mixer)/sizeof(unsigned short));
 save("opamp_rev",f.opamp_rev,65536);save("vcr_kVg",vcr_kVg,65536);
 save("vcr_n_Ids_term",vcr_n_Ids_term,65536);save("f0_dac",f.f0_dac,2048);
}};
int main(int argc,char**argv){if(argc!=2)return 2;Probe p;p.set_chip_model(reSID::MOS6581);p.dump(argv[1]);}
'''

def read_words(path):
    words=array('H');words.frombytes(path.read_bytes())
    if sys.byteorder!='little':words.byteswap()
    return words


def compress(values, family, bound):
    # Each independent source function has its own endpoint. Do not interpolate
    # across gain/resonance/summer/mixer function boundaries.
    lengths=([65536]*16 if family==0 else [k*65536 for k in range(2,7)] if family==1
             else [1]+[k*65536 for k in range(1,8)] if family==2 else [65536])
    coefficients=[];pages=[];sequences={};start=0;max_error=0
    for length in lengths:
        if length==1:start+=1;continue
        for relative in range(0,length,256):
            at=start+relative;page=list(values[at:at+256])
            page.append(values[at+256] if relative+256<length else min(65535,max(0,2*page[-1]-page[-2])))
            for shift in range(8,-1,-1):
                step=1<<shift;knots=tuple(page[::step])
                error=max(abs(((knots[i>>shift]*(step-(i&(step-1)))+
                               knots[(i>>shift)+1]*(i&(step-1))+step//2)>>shift)-page[i]) for i in range(256))
                if error>bound:continue
                base=sequences.get(knots)
                if base is None:
                    shared=bool(coefficients) and coefficients[-1]==knots[0]
                    base=len(coefficients)-int(shared);coefficients.extend(knots[int(shared):]);sequences[knots]=base
                pages.append((base<<4)|shift);max_error=max(max_error,error);break
            else:raise ValueError('unrepresentable page')
        start+=length
    if start!=len(values):raise ValueError('wrong source function length')
    print(f'{NAMES[family]}: {len(pages)} pages, {len(coefficients)} coefficients, maximum error {max_error}',flush=True)
    return pages,coefficients


def bank_function(banks):
    result=' function automatic [10239:0] init_bank(input integer n);begin case(n)\n'
    for number,words in sorted(banks.items()):
        packed=sum(int(word)<<(20*i) for i,word in enumerate(words))
        result+=f"{number}:init_bank=10240'h{packed:02560x};\n"
    return result+'default:init_bank=0;endcase end endfunction\n'


def generate(temp):
    tables=[read_words(temp/(name+'.bin')) for name in NAMES]
    compressed=[compress(values,i,BOUNDS[i]) for i,values in enumerate(tables)]
    counts=[len(c) for _,c in compressed];width=max(counts)*16
    header=NOTICE+'function automatic integer coefficient_count(input integer family_id); begin case(family_id)\n'
    header+=''.join(f'{i}:coefficient_count={n};\n' for i,n in enumerate(counts))+'default:coefficient_count=1;endcase end endfunction\n'
    header+='function automatic integer page_count(input integer family_id); begin case(family_id)\n'
    header+=''.join(f'{i}:page_count={len(p)};\n' for i,(p,_) in enumerate(compressed))+'6:page_count=2048;default:page_count=1;endcase end endfunction\n'
    header+=f'function automatic [{width-1}:0] coefficient_data(input integer family_id); begin case(family_id)\n'
    for i,(_,values) in enumerate(compressed):
        chunks=[f"{len(values[j:j+256])*16}'h"+''.join(f'{x:04x}' for x in reversed(values[j:j+256])) for j in reversed(range(0,len(values),256))]
        header+=f"{i}:coefficient_data={width}'("+'{'+','.join(chunks)+'});\n'
    header+='default:coefficient_data=0;endcase end endfunction\n'
    banks={}
    for family,(pages,_) in enumerate(compressed):
        for offset in range(0,len(pages),512):banks[family*32+offset//512]=pages[offset:offset+512]
    cutoff=read_words(temp/'f0_dac.bin')
    if len(cutoff)!=2048:raise ValueError('wrong cutoff table length')
    for offset in range(0,2048,512):banks[192+offset//512]=cutoff[offset:offset+512]
    # Exact VCR: 16-bit base + 127 two-bit monotone deltas per 128-code record.
    vcr=tables[5];records=[]
    for start in range(0,65536,128):
        record=int(vcr[start])
        for i in range(127):
            delta=int(vcr[start+i+1])-int(vcr[start+i])
            if not 0<=delta<=2:raise ValueError('VCR delta exceeds two bits')
            record|=delta<<(16+2*i)
        records.append(record)
    for index,value in enumerate(vcr):
        record=records[index>>7]
        if (record&65535)+sum((record>>(16+2*i))&3 for i in range(index&127))!=value:
            raise ValueError('VCR packing differs')
    vcr_banks={lane:[(record>>(lane*20))&0xfffff for record in records] for lane in range(14)}
    return {'c64_sid_filter_coefficients.vh':header,
            'c64_sid_filter_pages.vh':NOTICE+bank_function(banks),
            'c64_sid_vcr_data.vh':NOTICE+bank_function(vcr_banks)}


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--vice-source',type=Path,required=True)
    parser.add_argument('--output-dir',type=Path,default=Path(__file__).resolve().parents[1]/'cores/fes-c64/rtl')
    parser.add_argument('--check',action='store_true')
    args=parser.parse_args();source=args.vice_source/'src/resid'
    for name,digest in HASHES.items():
        try:actual=hashlib.sha256((source/name).read_bytes()).hexdigest()
        except OSError as error:parser.error(str(error))
        if actual!=digest:parser.error(f'{name}: expected unchanged VICE 3.10 source')
    with tempfile.TemporaryDirectory(prefix='c64-sid-audio-tables-') as directory:
        temp=Path(directory)
        for name in HASHES:
            if name!='siddefs.h.in':shutil.copy2(source/name,temp/name)
        config=(source/'siddefs.h.in').read_text()
        for key,value in CONFIG.items():config=config.replace('@'+key+'@',value)
        (temp/'siddefs.h').write_text(config);(temp/'probe.cpp').write_text(PROBE)
        subprocess.run(['c++','-std=c++17','-O2','-I'+str(temp),str(temp/'probe.cpp'),str(temp/'filter.cc'),str(temp/'dac.cc'),'-o',str(temp/'probe')],check=True)
        subprocess.run([str(temp/'probe'),str(temp)],check=True)
        result=generate(temp)
    for name,text in result.items():
        output=args.output_dir/name
        if args.check:
            if output.read_text()!=text:parser.error(f'{output}: generated constants differ')
            print(f'{output}: unchanged')
        else:
            output.parent.mkdir(parents=True,exist_ok=True);output.write_text(text);print(output)

if __name__=='__main__':main()
