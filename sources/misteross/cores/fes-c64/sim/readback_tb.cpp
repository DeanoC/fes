// SPDX-License-Identifier: MIT
#include "Vc64_video_sim_top.h"
#include "verilated.h"
#include <cstdio>
int main(int argc,char**argv){Verilated::commandArgs(argc,argv);Vc64_video_sim_top t;t.clk_sys=0;t.pixel_clk=0;t.reset=1;t.phi=0;t.cycle_clock=12;t.we=0;t.reading=0;t.read_sample=0;t.color_we=0;t.addr=0;t.din=0;t.color_addr=0;t.color_din=0;t.bank=0;t.eval();auto tick=[&](){t.clk_sys=0;t.eval();t.clk_sys=1;t.eval();};tick();t.reset=0;tick();unsigned checks=0,errors=0;
for(unsigned addr=0;addr<64;++addr){if(addr==0x19||addr==0x1e||addr==0x1f)continue;
 for(unsigned value=0;value<256;++value){t.addr=addr;t.din=value;t.we=1;tick();t.we=0;t.reading=1;t.eval();unsigned expected=value;
  if(addr==0x11)expected=value&0x7f;else if(addr==0x12||addr==0x13||addr==0x14)expected=0;else if(addr==0x16)expected=value|0xc0;else if(addr==0x18)expected=value|1;else if(addr==0x1a||(addr>=0x20&&addr<=0x2e))expected=value|0xf0;else if(addr>=0x2f)expected=255;
  if(t.vic_dout!=expected){if(errors<8)std::printf("readback %02x write%02x got%02x expected%02x\n",addr,value,t.vic_dout,expected);++errors;}++checks;t.reading=0;
 }
}
std::printf("VIC readback: %u checks, %u errors\n",checks,errors);return errors?1:0;}
