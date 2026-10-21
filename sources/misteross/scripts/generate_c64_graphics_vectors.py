#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
from pathlib import Path
import re,subprocess,json,hashlib
import argparse,tempfile
parser=argparse.ArgumentParser(description='Regenerate graphics vectors from local VICE3.10 source')
parser.add_argument('--vice-source',type=Path,required=True)
args=parser.parse_args();src=args.vice_source.resolve()/'src/viciisc/vicii-draw-cycle.c'
expected='78f70f99f77d26c507d7ebb2dba46ea5e8d2083f71c28d1c4c56420486dafaf6'
if hashlib.sha256(src.read_bytes()).hexdigest()!=expected:raise SystemExit('Reference source mismatch: '+str(src))
temporary=tempfile.TemporaryDirectory(prefix='c64-graphics-oracle-');p=Path(temporary.name);s=src.read_text()
a=s.index('/* colors */');b=s.index('/* foreground/background graphics */');macros=s[a:b]
a=s.index('static uint8_t gbuf_pipe0_reg');b=s.index('/* sprites */',a);variables=s[a:b]
a=s.index('static const uint8_t colors[]');b=s.index('static DRAW_INLINE uint8_t get_trigger_candidates',a);functions=s[a:b]
header=s[:s.index('#include')]+'''#include <cstdint>
#include <cstring>
using std::memset;
#define DRAW_INLINE inline
struct Vic{uint8_t regs[64],vbuf[40],cbuf[40],gbuf;int color_latency,vborder,idle_state;}vicii;
static uint8_t render_buffer[8],pri_buffer[8];
static int cycle_is_visible(unsigned flags){return flags!=0;}
'''
wrapper='''
extern "C" void oracle_reset(){std::memset(&vicii,0,sizeof(vicii));vicii.color_latency=1;gbuf_pipe0_reg=cbuf_pipe0_reg=vbuf_pipe0_reg=gbuf_pipe1_reg=cbuf_pipe1_reg=vbuf_pipe1_reg=xscroll_pipe=vmode11_pipe=vmode16_pipe=vmode16_pipe2=gbuf_reg=gbuf_mc_flop=gbuf_pixel_reg=cbuf_reg=vbuf_reg=dmli=0;}
extern "C" void oracle_word(unsigned glyph,unsigned matrix,unsigned color,unsigned d011,unsigned d016,unsigned background,unsigned visible){
 gbuf_pipe1_reg=glyph;vbuf_pipe1_reg=matrix;cbuf_pipe1_reg=color;dmli=0;
 vicii.regs[0x11]=d011;vicii.regs[0x16]=d016;for(unsigned i=0;i<4;++i)vicii.regs[0x21+i]=(background>>(i*4))&15;
 draw_graphics8(visible?1:0);
}
extern "C" unsigned oracle_color(unsigned dot){unsigned color=render_buffer[dot];return color>=0x21&&color<=0x24?vicii.regs[color]:color;}
extern "C" unsigned oracle_foreground(unsigned dot){return pri_buffer[dot]!=0;}
'''
(p/'vice_graphics_oracle.cpp').write_text(header+macros+variables+functions+wrapper)
(p/'oracle-source.json').write_text(json.dumps({'source':str(src),'sha256':hashlib.sha256(src.read_bytes()).hexdigest(),'extracted':'unaltered draw_graphics/draw_graphics8 andcolor table; callerprefetchboundary directlysupplies pipe1 metadata; PALcolor_latency1'},indent=2)+'\n')

caller=Path(__file__).resolve().parents[1]/'cores/fes-c64/sim/graphics_tb.cpp'
subprocess.run(['c++','-O2','-std=c++17','-DGENERATE_GRAPHICS_VECTORS',str(caller),str(p/'vice_graphics_oracle.cpp'),'-o',str(p/'oracle')],check=True)
result=subprocess.check_output([str(p/'oracle')],text=True)
header='// SPDX-License-Identifier: MIT\n// VICE3.10 PAL6569R3 source-oracle output,seed0xe46112a9,1000-word blocks.\n// vicii-draw-cycle.c SHA256 '+expected+'\n'
caller.with_name('graphics_vectors.h').write_text(header+result)
