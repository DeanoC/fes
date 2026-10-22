#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Generate offline sprite colors using unmodified VICE3.10 draw_sprites8."""
import argparse
import hashlib
from pathlib import Path
import re
import subprocess
import tempfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--vice-source', type=Path, required=True)
parser.add_argument('--output-directory', type=Path, default=Path(__file__).resolve().parents[1]/'cores/fes-c64/sim')
args = parser.parse_args()
src = args.vice_source.resolve() / 'src/viciisc'
expected = {
 'vicii-draw-cycle.c': '78f70f99f77d26c507d7ebb2dba46ea5e8d2083f71c28d1c4c56420486dafaf6',
 'vicii-chip-model.c': '95ea3ef61e59a1c850ff516c1a827ae849521dce3199307586e061af97db639d',
}
for name, digest in expected.items():
 if hashlib.sha256((src/name).read_bytes()).hexdigest() != digest:
  raise SystemExit('Reference source mismatch: '+name)
s = (src/'vicii-draw-cycle.c').read_text()
def function(name):
 match=re.search(r'static DRAW_INLINE [^\n]+\b'+name+r'\(',s)
 assert match,name
 start=match.start()
 begin = s.index('{',start);depth=0
 for end in range(begin,len(s)):
  if s[end]=='{':depth+=1
  elif s[end]=='}':
   depth-=1
   if depth==0:return s[start:end+1]
 raise RuntimeError(name)
model=(src/'vicii-chip-model.c').read_text().split('static const struct ViciiCycle cycle_tab_pal[] = {',1)[1].split('};',1)[0]
rows=[]
for line in model.splitlines():
 m=re.search(r'Phi1\((\d+)\),\s*(0x[0-9a-f]+)',line)
 if not m:continue
 cycle,x=int(m[1]),int(m[2],16)&0x1f8
 ptr=re.search(r'SprPtr\((\d)\)',line);row=re.search(r'SprDma1\((\d)\)',line)
 rows.append((x,1<<int(ptr[1]) if ptr else 0,1<<int(row[1]) if row else 0,int('ChkSprDisp' in line)))
assert len(rows)==63 and sum(r[3] for r in rows)==1
out=args.output_directory.resolve()
header='// SPDX-License-Identifier: MIT\n// Generated from VICE3.10 PAL cycle table; SHA256 '+expected['vicii-chip-model.c']+'\n'
header+='struct PalEvent{unsigned x,ptr,row,pending;};\nstatic constexpr PalEvent pal[64]={{0,0,0,0},\n'
header+=''.join('{%d,%d,%d,%d},\n'%r for r in rows)+'};\n'
(out/'sprite_pal_events.h').write_text(header)
# The flags are a private encoding of the table fields. The draw routine and
# its helpers below are unmodified; these adapters replace VICE's flag getters.
setup='''
#include <cstdint>
#include <cstring>
#include "sprite_pal_events.h"
#define DRAW_INLINE inline
#define COL_D025 0x25
#define COL_D026 0x26
#define COL_D027 0x27
struct Sprite{unsigned data;int x;};
struct Vic{Sprite sprite[8];uint8_t regs[64],sprite_background_collisions,sprite_sprite_collisions,sprite_display_bits;int color_latency;}vicii;
static int sprite_x_pipe[8];
static uint8_t sprite_pri_bits,sprite_mc_bits,sprite_expx_bits,sprite_pending_bits,sprite_active_bits,sprite_halt_bits;
static uint32_t sbuf_reg[8];
static uint8_t sbuf_pixel_reg[8],sbuf_expx_flops,sbuf_mc_flops,render_buffer[8],pri_buffer[8];
static unsigned cycle_get_xpos(unsigned c){return pal[c].x;}
static unsigned cycle_is_check_spr_disp(unsigned c){return pal[c].pending;}
static unsigned cycle_is_sprite_ptr_dma0(unsigned c){return pal[c].ptr;}
static unsigned cycle_is_sprite_dma1_dma2(unsigned c){return pal[c].row;}
static unsigned cycle_get_sprite_num(unsigned c){unsigned m=pal[c].ptr|pal[c].row;for(unsigned i=0;i<8;++i)if(m&(1u<<i))return i;return 0;}
'''
wrapper='''
extern "C" void oracle_reset(){std::memset(&vicii,0,sizeof(vicii));vicii.color_latency=1;vicii.sprite_display_bits=255;std::memset(sprite_x_pipe,0,sizeof(sprite_x_pipe));std::memset(sbuf_reg,0,sizeof(sbuf_reg));std::memset(sbuf_pixel_reg,0,sizeof(sbuf_pixel_reg));sprite_pri_bits=sprite_mc_bits=sprite_expx_bits=sprite_pending_bits=sprite_active_bits=sprite_halt_bits=sbuf_expx_flops=sbuf_mc_flops=0;}
extern "C" void oracle_word(unsigned cycle,unsigned mc,unsigned expand,unsigned colors,const unsigned *data,const unsigned *positions){
 for(unsigned i=0;i<8;++i){vicii.sprite[i].data=data[i];vicii.sprite[i].x=positions[i];vicii.regs[0x27+i]=(colors>>(i*4))&15;render_buffer[i]=pri_buffer[i]=0;}
 vicii.regs[0x1c]=mc;vicii.regs[0x1d]=expand;vicii.regs[0x25]=13;vicii.regs[0x26]=14;draw_sprites8(cycle);
}
extern "C" unsigned oracle_color(unsigned dot){unsigned c=render_buffer[dot];return c>=COL_D025&&c<COL_D027+8?vicii.regs[c]:c;}
'''
names=['get_trigger_candidates','trigger_sprites','draw_sprites','update_sprite_mc_bits_6569','update_sprite_mc_bits_8565','update_sprite_data','update_sprite_xpos','draw_sprites8']
with tempfile.TemporaryDirectory(prefix='c64-sprite-pal-') as tmp:
 p=Path(tmp);(p/'oracle.cpp').write_text(s[:s.index('#include')]+setup+'\n'.join(function(n) for n in names)+wrapper)
 subprocess.run(['c++','-O2','-std=c++17','-DGENERATE_SPRITE_PAL_VECTORS','-I'+str(out),str(out/'sprite_pal_tb.cpp'),str(p/'oracle.cpp'),'-o',str(p/'oracle')],check=True)
 result=subprocess.check_output([str(p/'oracle')],text=True)
(out/'sprite_pal_vectors.h').write_text('// SPDX-License-Identifier: MIT\n// Unmodified VICE3.10 draw_sprites8 output, PAL6569R3.\n// vicii-draw-cycle.c SHA256 '+expected['vicii-draw-cycle.c']+'\n'+result)
