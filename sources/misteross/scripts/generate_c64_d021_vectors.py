#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Regenerate PAL6569R3 D020/D021 transitions from unchanged VICE 3.10 functions."""
from pathlib import Path
import argparse,hashlib,subprocess,tempfile
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--vice-source',type=Path,required=True)
parser.add_argument('--register',choices=('d020','d021'),default='d021')
parser.add_argument('--output',type=Path)
args=parser.parse_args();src=args.vice_source.resolve()/'src/viciisc'
register=int(args.register[1:],16)
if args.output is None:args.output=Path(__file__).resolve().parents[1]/f'cores/fes-c64/sim/{args.register}_vectors.h'
hashes={
 'vicii-draw-cycle.c':'78f70f99f77d26c507d7ebb2dba46ea5e8d2083f71c28d1c4c56420486dafaf6',
 'vicii-mem.c':'f8e2bdd30fbe8abf60a7e77647723f4615834e5b414f2695698ed28b31857edf',
 'vicii-draw.c':'f0157690392cc61ef29c91566bc3f498123e3c6891a0570c9ea2ae520d623f3f',
 'vicii-chip-model.c':'95ea3ef61e59a1c850ff516c1a827ae849521dce3199307586e061af97db639d'}
for name,digest in hashes.items():
 if hashlib.sha256((src/name).read_bytes()).hexdigest()!=digest:raise SystemExit('Reference source mismatch: '+str(src/name))
def function(file,name):
 s=file.read_text();start=s.index('static DRAW_INLINE void '+name) if 'draw' in file.name else s.index('inline static void '+name)
 brace=s.index('{',start);depth=1;i=brace+1
 while depth:
  if s[i]=='{':depth+=1
  if s[i]=='}':depth-=1
  i+=1
 return s[start:i]+'\n'
credit=(src/'vicii-draw-cycle.c').read_text().split('#include',1)[0]
code=credit+'''// Generated source oracle. Functions below are extracted byte-for-byte from
// pinned VICE sources; minimal data struct only supplies fields they reference.
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#define DRAW_INLINE inline
#define VICII_DRAW_BUFFER_SIZE (65*8)
static struct { uint8_t regs[64],last_color_reg,last_color_value; int color_latency,dbuf_offset; uint8_t dbuf[VICII_DRAW_BUFFER_SIZE]; } vicii;
static uint8_t pixel_buffer[8],render_buffer[8],cregs[0x2f],last_color_reg,last_color_value;
'''
functions=['update_cregs','draw_colors_6569','draw_colors_8565','draw_colors8']
for name in functions:code+=function(src/'vicii-draw-cycle.c',name)+'\n'
code+=function(src/'vicii-mem.c','color_reg_store')
code+='''
int main(int argc,char **argv) {
 if(argc!=2){return 2;}
 FILE *out=fopen(argv[1],"wb");
 if(!out){return 2;}
 unsigned cases=0;
 for(unsigned old=0;old<16;++old)for(unsigned next=0;next<16;++next)for(unsigned mask=0;mask<256;++mask){
  memset(&vicii,0,sizeof(vicii));memset(cregs,0,sizeof(cregs));
  for(unsigned i=0;i<16;++i)cregs[i]=i;
  cregs[0x21]=old;vicii.regs[0x21]=old;vicii.color_latency=1;
  last_color_reg=vicii.last_color_reg=0xff;
  for(unsigned i=0;i<8;++i){render_buffer[i]=(mask&(1u<<i))?3:0x21;pixel_buffer[i]=render_buffer[i];}
  pixel_buffer[0]=(mask&1)?3:old;
  // Store during zero-based raster cycle17, after its Phi1 draw. End-of-cycle
  // monitor reports cycle18 after CLK_INC, matching SET_ABS source ordering.
  vicii.dbuf_offset=17*8;
  color_reg_store(0x21,next);
  draw_colors8(); // cycle18: transfer last_color_reg, previous cregs still used
  draw_colors8(); // cycle19: lookup index i+1 resolves new color for pixels1..7
  for(unsigned i=0;i<8;++i){
   unsigned wanted=(mask&(1u<<i))?3:(i?next:old),got=vicii.dbuf[18*8+i];
   if(got!=wanted){printf("FAIL old%u new%u mask%u dot%u got%u expected%u\\n",old,next,mask,i,got,wanted);return 1;}
  }
  if(old==14&&next==0&&mask==0){printf("reference_raster246_x40_47");for(unsigned i=0;i<8;++i)printf(" %u",vicii.dbuf[18*8+i]);puts("");}
  if(mask==0)fwrite(&vicii.dbuf[18*8],1,8,out);
  ++cases;
 }
 fclose(out);
 printf("source oracle PASS %u old/new/foreground cases\\n",cases);
}
'''
if register==0x20:
 start=code.index('int main(')
 code=code[:start]+code[start:].replace('0x21','0x20')
with tempfile.TemporaryDirectory(prefix='c64-color-oracle-') as temporary:
 b=Path(temporary);(b/'oracle.c').write_text(code)
 subprocess.run(['cc','-std=c11','-O2','-Wall','-Wextra','-Werror',str(b/'oracle.c'),'-o',str(b/'oracle')],check=True)
 subprocess.run([str(b/'oracle'),str(b/'expected.bin')],check=True)
 data=(b/'expected.bin').read_bytes()
 if len(data)!=2048:raise SystemExit('Incomplete source output')
 header='// SPDX-License-Identifier: MIT\n// VICE 3.10 PAL6569R3 source-oracle output: [old*16+new][dot0..7].\n'
 header+='// '+ '; '.join(name+' SHA256 '+digest for name,digest in hashes.items())+'\n'
 header+='#include <cstdint>\nstatic constexpr uint8_t source_expected[]={\n'
 header+=''.join(' '+','.join(map(str,data[i:i+32]))+',\n' for i in range(0,len(data),32))+'};\n'
 if register==0x20:header=header.replace('source_expected','source_expected_d020')
 args.output.write_text(header)
