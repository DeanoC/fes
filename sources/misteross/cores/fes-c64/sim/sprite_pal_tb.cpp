// SPDX-License-Identifier: MIT
// PAL-cycle replay with final registered DMA byte arriving between dots4/5.
#include <array>
#include <cstdint>
#include <cstdio>
#include "sprite_pal_events.h"
#ifdef GENERATE_SPRITE_PAL_VECTORS
extern "C" {void oracle_reset();void oracle_word(unsigned,unsigned,unsigned,unsigned,const unsigned*,const unsigned*);unsigned oracle_color(unsigned);}
#else
#include "Vc64_vic_sprite_pixels.h"
#include "verilated.h"
#include "sprite_pal_vectors.h"
#endif
int main(int argc,char**argv){
#ifdef GENERATE_SPRITE_PAL_VECTORS
 (void)argc;(void)argv;std::puts("static constexpr uint64_t sprite_pal_vectors[]={");
#else
 Verilated::commandArgs(argc,argv);
#endif
 unsigned count=0;
 for(unsigned scene=0;scene<4;++scene){
#ifdef GENERATE_SPRITE_PAL_VECTORS
  oracle_reset();
#else
  Vc64_vic_sprite_pixels t;t.background0=0;t.clk=0;t.reset=1;t.tick=0;t.dot=0;t.xpos=0;t.pending_update=0;t.display_bits=255;t.pointer_cycle=0;t.row_cycle=0;t.priority_bits=0;t.multicolor_bits=0;t.expand_bits=0;t.foreground=0;t.background_color=0;t.multi0=13;t.multi1=14;t.colors=0x87654321;
  for(unsigned i=0;i<3;++i)t.positions[i]=0;for(unsigned i=0;i<6;++i)t.data[i]=0;
  auto tick=[&](){t.clk=0;t.eval();t.clk=1;t.eval();};tick();t.reset=0;
#endif
  std::array<unsigned,8> fetched{},ideal{},positions{};
  for(unsigned i=0;i<8;++i)positions[i]=368+i*16;
  for(unsigned line=0;line<50;++line){
   uint64_t hash=UINT64_C(14695981039346656037);
   for(unsigned cycle=1;cycle<=63;++cycle){
    auto e=pal[cycle];
    for(unsigned i=0;i<8;++i)ideal[i]=((line*730201u+i*198733u)^0xaf5c39u)&0xffffff;
    unsigned mc=(scene&1)?0xaa:0,expand=(scene&2)?0xcc:0;
#ifdef GENERATE_SPRITE_PAL_VECTORS
    oracle_word(cycle,mc,expand,0x87654321,ideal.data(),positions.data());
#endif
    for(unsigned dot=0;dot<8;++dot){
#ifdef GENERATE_SPRITE_PAL_VECTORS
     unsigned color=oracle_color(dot);
#else
     for(unsigned i=0;i<8;++i){
      if((e.ptr&(1u<<i))&&dot==5)fetched[i]=(fetched[i]&0x00ffff)|(ideal[i]&0xff0000);
      if((e.row&(1u<<i))&&dot==1)fetched[i]=(fetched[i]&0xff00ff)|(ideal[i]&0x00ff00);
      if((e.row&(1u<<i))&&dot==5)fetched[i]=(fetched[i]&0xffff00)|(ideal[i]&0x0000ff);
     }
     for(unsigned i=0;i<3;++i)t.positions[i]=0;for(unsigned i=0;i<6;++i)t.data[i]=0;
     for(unsigned i=0;i<8;++i){for(unsigned b=0;b<9;++b)t.positions[(i*9+b)/32]|=((positions[i]>>b)&1)<<((i*9+b)%32);for(unsigned b=0;b<24;++b)t.data[(i*24+b)/32]|=((fetched[i]>>b)&1)<<((i*24+b)%32);}
     t.tick=1;t.dot=dot;t.xpos=e.x+dot;t.pending_update=e.pending;t.pointer_cycle=e.ptr;t.row_cycle=e.row;t.multicolor_bits=mc;t.expand_bits=expand;
     // Infer the winning source through background-port sensitivity, without
     // relying on a color-value comparison with D021 or internal winner state.
     t.clk=0;t.background0=1;t.eval();unsigned original=t.color;
     t.background_color=15;t.eval();bool inherited=t.color!=original;
     if(bool(t.color_background0)!=inherited){std::puts("sprite source tag mismatch");return 1;}
     // Equal background/sprite palette values still retain the winning source.
     t.background_color=original;t.eval();
     if(bool(t.color_background0)!=inherited){std::puts("equal-color sprite source tag mismatch");return 1;}
     t.background0=0;t.eval();
     if(t.color_background0){std::puts("sprite invented a background source");return 1;}
     t.background_color=0;tick();unsigned color=t.color_q;
#endif
     hash^=color;hash*=UINT64_C(1099511628211);++count;
    }
   }
#ifdef GENERATE_SPRITE_PAL_VECTORS
   std::printf(" UINT64_C(0x%016llx),\n",(unsigned long long)hash);
#else
   if(hash!=sprite_pal_vectors[scene*50+line]){std::printf("PAL sprite oracle mismatch scene%u line%u got%016llx expected%016llx\n",scene,line,(unsigned long long)hash,(unsigned long long)sprite_pal_vectors[scene*50+line]);return 1;}
#endif
  }
 }
#ifdef GENERATE_SPRITE_PAL_VECTORS
 std::puts("};");
#else
 std::printf("PAL sprite oracle PASS: %u dots, eight reload-edge positions, registered DMA bytes, mono/multicolor and X expansion\n",count);
#endif
}
