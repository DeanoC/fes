// SPDX-License-Identifier: MIT
// Source-oracle output at the prefetched-byte boundary; no fetch timing claim.
#include <cstdint>
#include <cstdio>
#ifdef GENERATE_GRAPHICS_VECTORS
extern "C" {void oracle_reset();void oracle_word(unsigned,unsigned,unsigned,unsigned,unsigned,unsigned,unsigned);unsigned oracle_color(unsigned);unsigned oracle_foreground(unsigned);}
#else
#include "Vc64_vic_graphics_pixels.h"
#include "verilated.h"
#include "graphics_vectors.h"
#endif
static uint32_t random_state=0xe46112a9;
static uint32_t random_word(){random_state^=random_state<<13;random_state^=random_state>>17;random_state^=random_state<<5;return random_state;}
int main(int argc,char**argv){
#ifdef GENERATE_GRAPHICS_VECTORS
 (void)argc;(void)argv;oracle_reset();std::puts("static constexpr uint64_t graphics_vectors[]={");
#else
 Verilated::commandArgs(argc,argv);Vc64_vic_graphics_pixels t;t.clk=0;t.reset=1;t.tick=0;t.dot=0;t.glyph=0;t.matrix=0;t.cell_color=0;t.d011=0;t.d016=0;t.background=0;t.latch_scroll=1;
 auto clock=[&](){t.clk=0;t.eval();t.clk=1;t.eval();};clock();t.reset=0;
 static_assert(sizeof(graphics_vectors)/sizeof(*graphics_vectors)==100);
#endif
 uint64_t hash=UINT64_C(14695981039346656037);unsigned modes=0,scrolls=0;
 for(unsigned word=0;word<100000;++word){unsigned a=random_word(),b=random_word();unsigned glyph=a&255,matrix=(a>>8)&255,color=(a>>16)&15,d011=(a>>20)&255,d016=b&255,background=b>>8;
  modes|=1u<<((((d011>>5)&3)<<1)|((d016>>4)&1));scrolls|=1u<<(d016&7);
#ifdef GENERATE_GRAPHICS_VECTORS
  oracle_word(glyph,matrix,color,d011,d016,background,(b>>7)&1);
#endif
  for(unsigned dot=0;dot<8;++dot){
#ifdef GENERATE_GRAPHICS_VECTORS
   unsigned result=oracle_color(dot)|(oracle_foreground(dot)<<4);
#else
   t.latch_scroll=(b>>7)&1;t.dot=dot;t.glyph=glyph;t.matrix=matrix;t.cell_color=color;t.d011=d011;t.d016=d016;t.background=background;t.tick=1;
   // A source tag is exactly sensitivity to D021, including equal-valued
   // direct colors. Probe without an edge, then restore the oracle inputs.
   t.clk=0;t.eval();unsigned original=t.color,fg=t.foreground,tag=t.background0;
   t.background=background^15;t.eval();
   if(tag!=unsigned(t.color!=original)||t.foreground!=fg){std::puts("D021 graphics source tag mismatch");return 1;}
   t.background=background;clock();unsigned result=t.color_q|(t.foreground_q<<4);
#endif
   hash^=result;hash*=UINT64_C(1099511628211);
  }
  if(word%1000==999){
#ifdef GENERATE_GRAPHICS_VECTORS
   std::printf(" UINT64_C(0x%016llx),\n",(unsigned long long)hash);
#else
   if(hash!=graphics_vectors[word/1000]){std::printf("graphics oracle mismatch words%u..%u got%016llx expected%016llx\n",word-999,word,(unsigned long long)hash,(unsigned long long)graphics_vectors[word/1000]);return 1;}
#endif
   hash=UINT64_C(14695981039346656037);
  }
 }
 if(modes!=255||scrolls!=255)return 1;
#ifdef GENERATE_GRAPHICS_VECTORS
 std::puts("};");
#else
 std::puts("C64 graphics source oracle PASS:800000 dots,100000words,all8modes,scroll0..7,carry,mode/metadata/palette transitions andforeground");
#endif
}
