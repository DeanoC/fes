#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
from pathlib import Path
import re,hashlib,json
import argparse,tempfile,subprocess
parser=argparse.ArgumentParser(description='Regenerate collision vectors from a local VICE3.10 source tree')
parser.add_argument('--vice-source',type=Path,required=True,help='VICE source root containing src/viciisc')
args=parser.parse_args();src=args.vice_source.resolve()/'src/viciisc'
expected={'vicii-mem.c': 'f8e2bdd30fbe8abf60a7e77647723f4615834e5b414f2695698ed28b31857edf', 'vicii-cycle.c': '561876187fde8bfe8636e64593543aa77a5e0855ebbd9fe56d9da4fa6eb564ba'}
for name,digest in expected.items():
 if hashlib.sha256((src/name).read_bytes()).hexdigest()!=digest:raise SystemExit('Reference source mismatch: '+name)
temporary=tempfile.TemporaryDirectory(prefix='c64-collision-oracle-');p=Path(temporary.name)
m=(src/'vicii-mem.c').read_text();c=(src/'vicii-cycle.c').read_text()
def fn(text,name):
 match=re.search(r'(?:inline static|static inline) (?:void|uint8_t) '+name+r'\(',text);assert match,name
 masked=re.sub(r'/\*.*?\*/|//[^\n]*|"(?:\\.|[^"\\])*"',lambda m:' '*len(m.group()),text,flags=re.S);b=masked.index('{',match.end());depth=0
 for at in range(b,len(text)):
  if masked[at]=='{':depth+=1
  elif masked[at]=='}':
   depth-=1
   if depth==0:return text[match.start():at+1]
 raise RuntimeError(name)
a=c.index('    can_sprite_sprite =');b=c.index('    /*\n     *\n     * End of Phi1',a);fragment=c[a:b].replace('vicii_draw_cycle();','vicii.sprite_sprite_collisions |= ss;vicii.sprite_background_collisions |= bg;')
header='''#include <cstdint>
#include <cstring>
#define VICII_DEBUG_REGISTER(x)
struct Vic{uint8_t regs[64],irq_status,sprite_sprite_collisions,sprite_background_collisions,clear_collisions;}vicii;
struct Resources{int sprite_sprite_collisions_enabled=1,sprite_background_collisions_enabled=1;}vicii_resources;
static void vicii_irq_check_state(int){}
static void vicii_irq_set_line(){}
static void vicii_irq_sscoll_set(){vicii.irq_status|=4;}
static void vicii_irq_sbcoll_set(){vicii.irq_status|=2;}
'''
wrapper='''
extern "C" void oracle_reset(){std::memset(&vicii,0,sizeof(vicii));}
extern "C" unsigned oracle_read(bool bg){return bg?d01f_read():d01e_read();}
extern "C" void oracle_step(bool ack,unsigned mask,unsigned events,bool group,unsigned ss,unsigned bg){
 if(ack)d019_store(mask);vicii.irq_status|=events;if(group){int can_sprite_sprite,can_sprite_background;
'''+fragment+'''
}}
extern "C" unsigned oracle_state(unsigned i){return i==0?vicii.sprite_sprite_collisions:i==1?vicii.sprite_background_collisions:vicii.irq_status&15;}
'''
text=m[:m.index('#include')]+header+'\n'.join(fn(m,n) for n in ['d019_store','d01e_read','d01f_read'])+wrapper
p.joinpath('vice_collision_oracle.cpp').write_text(text);p.joinpath('oracle-source.json').write_text(json.dumps({n:hashlib.sha256((src/n).read_bytes()).hexdigest() for n in ['vicii-mem.c','vicii-cycle.c']},indent=2)+'\n')

caller=Path(__file__).resolve().parents[1]/'cores/fes-c64/sim/collision_tb.cpp'
subprocess.run(['c++','-std=c++17','-O2','-DGENERATE_COLLISION_VECTORS',str(caller),str(p/'vice_collision_oracle.cpp'),'-o',str(p/'oracle')],check=True)
result=subprocess.check_output([str(p/'oracle')],text=True)
header='// SPDX-License-Identifier: MIT\n// VICE3.10 source-oracle output; seed0x45ec0781,500000ticks,1000tick blocks.\n'
for name,digest in expected.items():header+='// '+name+' SHA256 '+digest+'\n'
caller.with_name('collision_vectors.h').write_text(header+result)
