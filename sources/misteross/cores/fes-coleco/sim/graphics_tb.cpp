// SPDX-License-Identifier: GPL-2.0-or-later
// Explicit VRAM fixtures and literal pixel oracles, including real registered RAM.
#include "Vcoleco_vdp.h"
#include "verilated.h"
#include <cstdlib>
#include <iostream>
#include <map>
static void require(bool ok,const char*m){if(!ok){std::cerr<<m<<'\n';std::exit(1);}}
struct Bench {
 Vcoleco_vdp d;
 Bench(){d.clk=0;d.reset=1;d.cpu_ce=0;d.cpu_iorq_n=d.cpu_rd_n=d.cpu_wr_n=1;d.raster_ce=0;d.cpu_a=d.cpu_din=0;d.eval();tick();d.reset=0;}
 void tick(){d.clk=1;d.eval();d.clk=0;d.eval();}
 void out(unsigned port,unsigned v){d.cpu_a=port;d.cpu_din=v;d.cpu_ce=1;d.cpu_iorq_n=0;d.cpu_wr_n=0;tick();d.cpu_ce=0;d.cpu_iorq_n=d.cpu_wr_n=1;for(int i=0;i<4;++i)tick();}
 void reg(unsigned r,unsigned v){out(0xbf,v);out(0xbf,0x80|r);}
 void address(unsigned a){out(0xbf,a&255);out(0xbf,0x40|(a>>8));}
 void byte(unsigned a,unsigned v){address(a);out(0xbe,v);}
 void run(const std::map<unsigned,unsigned>&expected){
  std::map<unsigned,unsigned>seen;
  // Two complete frames settle registered sprite banks after setup.
  for(unsigned i=0;i<256*262*3;++i){d.raster_ce=1;tick();d.raster_ce=0;for(unsigned t=0;t<31;++t)tick();
   if(i>=256*262&&!d.raster_blank){unsigned xy=(unsigned(d.raster_y)<<8)|d.raster_x;auto e=expected.find(xy);if(e!=expected.end()){
    if(d.raster_pixel!=e->second){std::cerr<<"pixel "<<d.raster_x<<","<<d.raster_y<<" got "<<unsigned(d.raster_pixel)<<" expected "<<e->second<<'\n';std::exit(1);}seen[xy]++;}}
  }
  require(seen.size()==expected.size(),"not every explicit pixel was sampled");
 }
};
int main(int argc,char**argv){Verilated::commandArgs(argc,argv);Bench b;
 b.address(0);for(unsigned i=0;i<16384;++i)b.out(0xbe,0);
 b.reg(0,2);b.reg(1,0x40);b.reg(2,6);b.reg(3,0x7f);b.reg(4,7);b.reg(5,0x36);b.reg(6,7);b.reg(7,13);
 // Same tile names in all screen thirds; physically different row patterns
 // and color rows. These literal addresses are TI Graphics II table examples.
 for(unsigned y=0;y<24;++y)b.byte(0x1800+y*32+1,8);
 for(unsigned row=0;row<8;++row){
  b.byte(0x2000+row,0xaa);b.byte(0x2800+row,0xf0);b.byte(0x3000+row,0x0f);
  b.byte(0x0000+row,0x21);b.byte(0x0800+row,0x43);b.byte(0x1000+row,0x65);
  b.byte(0x2040+row,0xaa);b.byte(0x2840+row,0xaa);b.byte(0x3040+row,0xaa);
  b.byte(0x0040+row,0x08);b.byte(0x0840+row,0x08);b.byte(0x1040+row,0x08);
  b.byte(0x3800+row,0xff);
 }
 // Full-color sprite overlays background; color zero remains transparent.
 b.byte(0x1b00,255);b.byte(0x1b01,32);b.byte(0x1b02,0);b.byte(0x1b03,14);
 b.byte(0x1b04,255);b.byte(0x1b05,48);b.byte(0x1b06,0);b.byte(0x1b07,0);b.byte(0x1b08,208);
 b.run({{0,2},{1,1},{64*256,4},{64*256+4,3},{128*256,5},{128*256+4,6},
        {8,13},{9,8},{64*256+8,13},{128*256+9,8},{32,14},{48,2},{49,1}});
 // TMS99xx's R3 character mask affects patterns as well as colors. Tile 8
 // must alias tile 0 even though its own stored pattern is the opposite.
 b.byte(0x2040,0x55);b.reg(3,0);
 b.run({{8,2},{9,1},{64*256+8,2},{64*256+12,1},{128*256+8,1},{128*256+12,2}});
 // Masked page bits deliberately repeat the first physical tables.
 b.reg(4,4);b.reg(3,0x1f);
 b.run({{0,2},{64*256,2},{64*256+1,1},{128*256,2},{128*256+1,1}});
 // Disabled display emits backdrop, never an old framebuffer or sprite.
 b.reg(1,0);b.run({{0,13},{32,13},{64*256,13},{128*256+8,13}});
 // Graphics I: characters 0 and 7 share one color byte; character 8 uses
 // the next group. Use different fg/bg nibbles and an explicit backdrop.
 b.reg(0,0);b.reg(1,0x40);b.reg(3,0x80);b.reg(4,0);b.byte(0x1b00,208);
 b.byte(0x1800,0);b.byte(0x1801,7);b.byte(0x1802,8);
 for(unsigned row=0;row<8;++row){b.byte(row,0xaa);b.byte(56+row,0xaa);b.byte(64+row,0xaa);}
 b.byte(0x2000,0xa4);b.byte(0x2001,0x07);
 b.run({{0,10},{1,4},{8,10},{9,4},{16,13},{17,7}});
 // Text mode: six visible high glyph bits, 40 columns, side margins and no sprites.
 b.reg(0,0x00);b.reg(1,0x50);b.reg(2,6);b.reg(4,1);b.reg(7,0xa4);
 b.byte(0x1800,3);b.byte(0x1801,4);b.byte(0x1827,5);b.byte(0x1828,6);
 b.byte(0x0818,0xa7);b.byte(0x0820,0xc7);b.byte(0x0828,0x84);b.byte(0x0830,0xc0);b.byte(0x0819,0x00);
 b.byte(0x1b00,255);b.byte(0x1b01,32);b.byte(0x1b02,0);b.byte(0x1b03,14);b.byte(0x3800,0xff);
 b.run({{0,4},{7,4},{8,10},{9,4},{10,10},{11,4},{12,4},{13,10},
        {14,10},{15,10},{16,4},{18,4},{19,10},
        {32,4},{242,10},{247,10},{248,4},{255,4},{(1u<<8)|8u,4},
        {(8u<<8)|8u,10}});
 b.byte(0x0818,0x80);b.reg(7,0x04);b.run({{8,4}});b.reg(7,0xa4);
 // Invalid TMS mode selectors must render only the R7 backdrop, never fall through.
 b.reg(3,0x80);b.byte(0x1808,1);b.byte(0x0008,0xff);b.byte(0x0808,0xff);
 b.byte(0x2000,0xa2);b.byte(0x2008,0xa2);
 const unsigned invalid_modes[]={3,5,6,7};
 for(const unsigned selector:invalid_modes){
  const unsigned m1=(selector>>2)&1;const unsigned m2=(selector>>1)&1;const unsigned m3=selector&1;
  b.reg(0,m3<<1);b.reg(1,0x40|(m1<<4)|(m2<<3));b.run({{64,4}});
 }
 // Multicolor uses a byte pair per tile row, with each nibble filling 4x4 pixels.
 b.reg(0,0);b.reg(1,0x48);b.reg(2,6);b.reg(4,1);b.reg(7,0x0d);
 b.byte(0x1801,2);b.byte(0x1821,2);b.byte(0x1841,2);b.byte(0x1861,2);b.byte(0x1881,2);
 const unsigned multicolor_rows[]={0x2a,0x4c,0x6b,0x0d,0x31,0x52,0x73,0x84};
 for(unsigned row=0;row<8;++row)b.byte(0x0810+row,multicolor_rows[row]);
 b.run({{8,2},{12,10},{(4u<<8)|8u,4},{(4u<<8)|12u,12},
        {(8u<<8)|8u,6},{(8u<<8)|12u,11},{(12u<<8)|8u,13},{(12u<<8)|12u,13},
        {(16u<<8)|8u,3},{(16u<<8)|12u,1},{(20u<<8)|8u,5},{(20u<<8)|12u,2},
        {(24u<<8)|8u,7},{(24u<<8)|12u,3},{(28u<<8)|8u,8},{(28u<<8)|12u,4},
        {(32u<<8)|8u,2},{(32u<<8)|12u,10},{32,14}});
 std::cout<<"TMS Graphics I grouping, Graphics II thirds/masks, foreground/background, backdrop, display and full sprite color passed\n";
}
