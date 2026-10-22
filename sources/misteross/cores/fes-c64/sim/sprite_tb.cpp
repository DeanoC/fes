// SPDX-License-Identifier: MIT
// Directed PAL bus checks use row counts and fixed crunch vectors, not a copy
// of the DMA state machine. The private VICE oracle covers additional timing.
#include "Vc64_vic_sprite_fetch.h"
#include "verilated.h"
#include <array>
#include <cstdio>
#include <cstdint>
static unsigned memory(unsigned address){
 unsigned a=address&0x3fff;
 if(a>=0x7f8&&a<=0x7ff)return 0x80+(a-0x7f8);
 return ((address^(address>>7)^(address>>12))*73+19)&255;
}
static unsigned data(const Vc64_vic_sprite_fetch &t,unsigned i){
 unsigned bit=i*24,off=bit%32,word=bit/32,value=t.sprite_data[word]>>off;
 if(off>8)value|=t.sprite_data[word+1]<<(32-off);return value&0xffffff;
}
static unsigned crunch_base(unsigned line){
 constexpr unsigned sequence[]={0,1,5,5,5};return sequence[line<=50?0:line<54?line-50:4];
}
int main(int argc,char **argv){
 Verilated::commandArgs(argc,argv);unsigned rows_checked=0;
 constexpr unsigned slots[]={58,60,62,1,3,5,7,9};
 for(unsigned bank=0;bank<4;++bank)for(unsigned mode=0;mode<4;++mode){
  Vc64_vic_sprite_fetch t;unsigned expanded=mode==1||mode==3?255:0;
  unsigned mask=mode==2?1:255;std::array<unsigned,8> rows{};
  t.clk=0;t.reset=1;t.phi1=0;t.phi2=0;t.sample=0;t.expansion_write=0;
  t.enabled=mode==2?0:mask;t.expanded=expanded;t.d018=0x10;t.bank=bank;
  t.other_ba_n=1;t.cpu_data=0xa5;t.ram_data=0;t.raster=0;t.cycle=1;t.ypos=0x3232323232323232ULL;
  auto tick=[&](){t.clk=0;t.eval();t.clk=1;t.eval();};tick();t.reset=0;
  for(unsigned line=0;line<130;++line)for(unsigned cycle=1;cycle<=63;++cycle){
   t.raster=line;t.cycle=cycle;t.expanded=expanded;t.expansion_write=0;
   if(mode==2&&line==50&&cycle==56)t.enabled=1;
   bool active=(line>50||(line==50&&cycle>=(mode==2?56:55)))&&
       (mode==3||line<50+(mode==1?42:21)||(line==50+(mode==1?42:21)&&cycle<16));
   unsigned sprite=8,lane=0;for(unsigned i=0;i<8;++i)if(cycle==slots[i]||cycle==slots[i]+1){sprite=i;lane=cycle==slots[i]?0:2;}
   for(unsigned phase=0;phase<2;++phase){
    t.phi1=phase==0;t.phi2=phase==1;t.sample=0;t.expansion_write=0;
    if(phase&&mode==3&&(cycle==15||cycle==17)){t.expansion_write=1;t.expansion_value=cycle==15?0:255;}
    tick();
    if(t.dma!=(active?mask:0)){std::printf("DMA fail mode%u line%u cycle%u: %02x\n",mode,line,cycle,t.dma);return 1;}
    bool pointer=phase==0&&lane==0;unsigned expected=0;bool request=false;
    if(sprite<8){
     bool enabled=active&&(mask&(1u<<sprite));
     bool unowned=mode==2&&sprite==0&&rows[0]==0&&phase==1&&lane==0;
     request=phase==0||(enabled&&!unowned);
     if(pointer)expected=(bank<<14)|0x7f8|sprite;
     else if(!enabled)expected=(bank<<14)|0x3fff;
     else if(mode!=3){unsigned row=rows[sprite]/(mode==1?2:1);expected=(bank<<14)|((0x80+sprite)<<6)|(row*3+(phase==0?1:lane));}
     else expected=(bank<<14)|((0x80+sprite)<<6)|(crunch_base(sprite<3?line:line-1)+(phase==0?1:lane));
    }
    if(bool(t.request)!=request||(request&&t.address!=expected)){std::printf("fetch fail bank%u mode%u line%u cycle%u phi%u request%u/%u address%04x/%04x\n",bank,mode,line,cycle,phase,t.request,request,t.address,expected);return 1;}
    t.phi1=0;t.phi2=0;t.expansion_write=0;
    if(t.request){t.ram_data=memory(t.address);t.sample=1;tick();t.sample=0;}
    if(phase&&mode==3&&(cycle==15||cycle==17)){expanded=cycle==15?0:255;t.expanded=expanded;}
    if(sprite<8&&phase==1&&lane==2&&active&&(mask&(1u<<sprite))){
     {unsigned row=rows[sprite]/(mode==1?2:1),offset=mode==3?crunch_base(sprite<3?line:line-1):row*3,a=(bank<<14)|((0x80+sprite)<<6)|offset;
      unsigned high=mode==2&&sprite==0&&!rows[0]?0xa5:memory(a);
      unsigned expected_data=(high<<16)|(memory(a+1)<<8)|memory(a+2);
      if(data(t,sprite)!=expected_data){std::printf("data fail sprite%u row%u %06x/%06x\n",sprite,row,data(t,sprite),expected_data);return 1;}
     }
     ++rows[sprite];++rows_checked;
    }
    if(mode==3&&line>=51&&cycle==16){
     constexpr unsigned bases[]={1,5,5,5};unsigned expected_base=bases[line<55?line-51:3];
     for(unsigned i=0;i<8;++i)if(((t.bases>>(i*6))&63)!=expected_base){std::printf("crunch base fail line%u sprite%u\n",line,i);return 1;}
    }
   }
  }
  if(mode!=3)for(unsigned i=0;i<8;++i)if(rows[i]!=((mask&(1u<<i))?(mode==1?42:21):0)){std::printf("row count fail mode%u sprite%u rows%u\n",mode,i,rows[i]);return 1;}
 }
 std::printf("C64 sprite PASS: %u complete DMA rows across four banks; 21/42-row timing, pointer/three-byte order, late byte ownership and sustained crunch\n",rows_checked);
}
