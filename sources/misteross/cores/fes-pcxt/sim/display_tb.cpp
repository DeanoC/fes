// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vdisplay_top.h"
#include "verilated.h"
#include <cstdio>
#include <array>
#include <random>
#include <string>
#include <stdexcept>
static Vdisplay_top top;
static uint64_t ticks;
static std::array<uint8_t,16384> cga{};
static std::array<uint8_t,4096> mda{};
static uint8_t font(unsigned a) {
    return (a&15)==13 ? 0xff : (a>>4)==0xc4 ? 0x81 : 0x80;
}
static void tick() {
    top.clk=0; top.eval();
    // Original diagnostic glyph: left and right dots, solid final scan line.

    top.cga_font_data=font(top.cga_font_address);
    top.mda_font_data=font(top.mda_font_address);
    top.eval(); top.clk=1; top.eval(); ++ticks;
}
static void check(bool v,const char* why) {if(!v) throw std::runtime_error(why);}
static unsigned bus(unsigned kind,unsigned addr,bool write=false,unsigned data=0,bool aen=false) {
    check(top.cycle_ready,"bus busy");
    top.cycle_kind=kind;top.cycle_address=addr;top.cycle_write=write;top.cycle_wdata=data;
    top.cycle_aen=aen;top.cycle_valid=1;tick();top.cycle_valid=0;
    unsigned n=0; while(!top.cycle_done && n++<200)tick();
    check(top.cycle_done && !top.cycle_fault,"ISA completion fault");return top.cycle_rdata;
}
static void out(unsigned a,unsigned d) {bus(1,a,true,d);}
static unsigned in(unsigned a) {return bus(1,a);}
static void reg(unsigned base,unsigned index,unsigned data) {out(base+4,index);out(base+5,data);}
static void mem(unsigned card,unsigned off,unsigned d) {
    bus(0,(card ? 0xb0000 : 0xb8000)+off,true,d);
    if(card)mda[off&4095]=d;else cga[off&16383]=d;
}
static void setup(unsigned card,unsigned mode,unsigned start=0) {
    const unsigned base=card ? 0x3b0 : 0x3d0;
    out(base+8,card ? 1 : 0);
    const std::array<unsigned,16> values = card ?
        std::array<unsigned,16>{97,80,82,15,25,6,25,25,2,13,0x20,12,start>>8,start&255,0,0} :
        std::array<unsigned,16>{(mode&1)?113u:56u,(mode&1)?80u:40u,(mode&1)?90u:45u,10,
                               (mode&2)?127u:31u,6,(mode&2)?100u:25u,(mode&2)?112u:28u,
                               2,(mode&2)?1u:7u,0x20,7,start>>8,start&255,0,0};
    for(unsigned i=0;i<16;i++)reg(base,i,values[i]);
    out(base+8,mode);
}
static unsigned expected(unsigned card,unsigned mode,unsigned palette,unsigned start,unsigned x,unsigned y) {
    const unsigned charwidth=card ? 9 : (mode&1) ? 8 : 16;
    const unsigned columns=card || (mode&1) ? 80 : 40;
    const unsigned rowsize=card ? 14 : (mode&2) ? 2 : 8;
    unsigned border=(!card && !(mode&1) && !(mode&16)) ? palette&15 : 0;
    if(!(mode&8))return 0;
    if(x>=columns*charwidth || y>=(card ? 350:200))return border;
    unsigned ma=(start+(y/rowsize)*columns+x/charwidth)&0x3fff;
    if(!card && (mode&2)) {
        unsigned addr=(((y&1)*8192)+(ma&4095)*2+(x%16)/8)&16383;
        unsigned b=cga[addr];
        if(mode&16)return (b & (0x80 >> (x%8))) ? palette&15 : 0;
        unsigned v=(b>>(6-2*((x%8)/2)))&3;
        if(!v)return palette&15;
        static const unsigned p0[]={0,2,4,6},p1[]={0,3,5,7},pbw[]={0,3,4,7};
        return ((mode&4) ? pbw[v] : (palette&32) ? p1[v] : p0[v]) | ((palette&16)?8:0);
    }
    unsigned a=ma*2;
    const unsigned ch=card ? mda[a&4095] : cga[a&16383];
    const unsigned attr=card ? mda[(a+1)&4095] : cga[(a+1)&16383];
    unsigned dot=card || (mode&1) ? x%charwidth : (x%charwidth)/2;
    unsigned glyph=font(ch*16+(y%rowsize));
    bool ink=dot<8 ? (glyph&(0x80>>dot)) : ch>=0xc0 && ch<=0xdf && (glyph&1);
    if(!card)return ink ? attr&15 : ((attr>>4)&((mode&32)?7:15));
    if((attr&0x77)==1 && y%14==13)ink=true;
    if((attr&0x77)==0x70)return ink ? 0 : 7;
    if((attr&0x77)==0)return 0;
    return ink ? ((attr&8)?15:7) : 0;
}
static void frame(unsigned card,unsigned mode,unsigned palette,unsigned start,bool enabled=true) {
    const unsigned mask=1u<<card,width=card ? 882 : 912,height=card ? 370:262;
    bool began=false;
    unsigned x=0,y=0,active=0,hs=0,vs=0;
    uint64_t first=0;
    for(unsigned n=0;n<4500000;n++) {
        tick();if(!(top.pixel_ce&mask))continue;
        if(top.sof&mask) {
            if(began) {
                check(x==0 && y==height,"frame geometry");
                const uint64_t dots=uint64_t(width)*height;
                const uint64_t hz=card ? 16257000:14318180;
                check((ticks-first)*hz>=dots*100000000-hz &&
                      (ticks-first)*hz<=dots*100000000+hz,"fractional raster cadence");
                check(active==(card ? 720u*350:640u*200),"active geometry");
                check(hs==(card ? 15u*9 : (mode&1)?10u*8:10u*16)*height,"HS pulse width");
                check(vs==16*width,"MC6845 fixed VS width");
                std::printf("%s mode=%02x palette=%02x start=%04x: %ux%u frame, all pixels checked\n",
                            card?"MDA":"CGA",mode,palette,start,width,height);
                return;
            }
            began=true;first=ticks;
        }
        if(!began)continue;
        const unsigned got=(top.rgbi>>(card*4))&15;
        const unsigned want=enabled ? expected(card,mode,palette,start,x,y) : 0;
        if(got!=want)throw std::runtime_error("pixel mismatch card="+std::to_string(card)+
            " x="+std::to_string(x)+" y="+std::to_string(y)+" got="+std::to_string(got)+" want="+std::to_string(want));
        check(bool(top.eol&mask)==(x==width-1),"EOL alignment");
        bool visible=x<(card ? 720u:640u) && y<(card ? 350u:200u);
        check(bool(top.de&mask)==visible,"DE alignment");
        const unsigned hstart=card ? 82u*9 : 720u;
        const unsigned hwidth=card ? 15u*9 : (mode&1)?10u*8:10u*16;
        check(bool(top.hs&mask)==(x>=hstart && x<hstart+hwidth),"HS position");
        check(bool(top.vs&mask)==(y>=(card ? 350u:224u) && y<(card ? 366u:240u)),"VS position");
        active+=bool(top.de&mask);hs+=bool(top.hs&mask);vs+=bool(top.vs&mask);
        if(++x==width){x=0;++y;}
    }
    throw std::runtime_error("frame timeout");
}
static void blink_cursor() {
    // Small nonstandard totals exercise CRTC programming and blink without
    // replacing guest timing with a test clock.
    setup(0,0x29);
    for(unsigned i=0;i<4;i++)mem(0,i,(i&1)?0x87:0x41);
    for(auto p:std::array<std::array<unsigned,2>,10>{{{0,3},{1,2},{2,2},{3,1},{4,1},{5,0},{6,1},{7,1},{9,1},{10,0x20}}})reg(0x3d0,p[0],p[1]);
    unsigned frames=0,changes=0,lastchange=0;bool previous=false;
    for(unsigned n=0;n<200000;n++) {
        tick();if(!(top.pixel_ce&1) || !(top.sof&1))continue;
        ++frames;bool on=(top.rgbi&15)==7;
        if(frames>2 && on!=previous){
            if(changes)check(frames-lastchange==16,"text blink period");
            ++changes;lastchange=frames;
        }
        previous=on;if(frames>=66)break;
    }
    check(frames>=66 && changes>=3,"blink did not toggle");
    // Steady cursor overrides a hidden blinking glyph and covers both scans.
    reg(0x3d0,10,0);reg(0x3d0,11,1);reg(0x3d0,14,0);reg(0x3d0,15,0);
    unsigned x=0,y=0,seen=0;bool start=false;
    for(unsigned n=0;n<10000;n++){
        tick();if(!(top.pixel_ce&1))continue;
        if(top.sof&1){if(start && ++seen==2)break;start=true;x=y=0;}
        if(!start)continue;
        if(x<8 && y<2)check((top.rgbi&15)==7,"steady cursor shape");
        if(top.eol&1){x=0;++y;}else ++x;
    }
    check(seen==2,"cursor frame timeout");
    // Clear HS width suppresses the pulse, as on MC6845 (not HD6845 width 16).
    reg(0x3d0,3,0);for(unsigned n=0;n<1000;n++)tick();
    for(unsigned n=0;n<3000;n++){tick();check(!(top.hs&1),"zero HS width");}
}
int main(int argc,char**argv) {
    Verilated::commandArgs(argc,argv);
    try {
        top.reset=1;for(unsigned n=0;n<10;n++)tick();top.reset=0;for(unsigned n=0;n<10;n++)tick();
        bus(0,0xb8000,true,0x5a);check(bus(0,0xbc000)==0x5a,"CGA VRAM mirror");
        bus(0,0xb0000,true,0xa5);check(bus(0,0xb7000)==0xa5,"MDA VRAM mirror");
        check(bus(0,0xb8000)==0x5a,"display decode collision");
        check(in(0x3d8)==0xff,"mode is write only");
        reg(0x3d0,14,0x3f);reg(0x3d0,15,0x45);out(0x7d0,14);
        check(in(0x7d1)==0x3f,"CGA CRTC alias");
        bus(1,0x3d1,true,0,false); // current R14
        check(in(0x3d1)==0,"cursor readback");
        out(0x3d4,15);bus(1,0x3d5,true,0x89,true);check(in(0x3d5)==0x45,"AEN IO suppression");
        // Independent byte scoreboards through the registered bus, including DMA
        // memory cycles and both aperture mirrors. Uninitialized VRAM is not assumed.
        std::mt19937 rng(0xc6a);
        for(unsigned i=0;i<cga.size();i++)mem(0,i,rng());
        for(unsigned i=0;i<mda.size();i++)mem(1,i,rng());
        for(unsigned i=0;i<1024;i++) {
            unsigned a=rng()%cga.size();check(bus(0,0xbc000+a)==cga[a],"random CGA read");
            a=rng()%mda.size();check(bus(0,0xb6000+a)==mda[a],"random MDA read");
        }
        // Text attributes without blink initially; glyph C4 exercises ninth column.
        for(unsigned i=0;i<cga.size();i+=2){mem(0,i,(i&2)?0xc4:0x41);mem(0,i+1,(i/2)%128);}
        for(unsigned i=0;i<mda.size();i+=2){mem(1,i,(i&2)?0xc4:0x41);mem(1,i+1, std::array<unsigned,8>{0,1,7,8,9,15,0x70,0x78}[(i/2)%8]);}
        setup(0,9,0x1ff0);out(0x3d9,0x15);setup(1,9,0x7f0);
        frame(0,9,0x15,0x1ff0);frame(1,9,0,0x7f0);
        setup(0,8,0);frame(0,8,0x15,0);
        for(unsigned i=0;i<cga.size();i++)mem(0,i,(i*37)^(i>>5));
        for(unsigned mode: {10u,14u,26u}) {
            setup(0,mode,0x1ff0);
            for(unsigned palette: {0u,0x12u,0x25u,0x3fu}) {
                out(0x3d9,palette);frame(0,mode,palette,0x1ff0);
            }
        }
        out(0x3d8,2);frame(0,10,0x3f,0x1ff0,false);
        check(!top.unsupported_interlace,"standard R8=02 must be supported");
        reg(0x3d0,8,1);check(top.unsupported_interlace==1,"interlace diagnostic");
        reg(0x3d0,8,2);
        // Light pen capture is edge-latched, read-only, and cleared on committed OUT.
        out(0x3db,0);check(!(in(0x3da)&2),"pen clear");
        top.light_pen_trigger=1;tick();tick();check(in(0x3da)&2,"pen latch");
        out(0x3db,0);check(!(in(0x3da)&2),"held pen must not retrigger");
        top.light_pen_trigger=0;tick();out(0x3dc,0);check(in(0x3da)&2,"software pen latch");
        // Live palette writes affect the running raster without a frame rebuild.
        for(unsigned i=0;i<cga.size();i++)mem(0,i,0xff);
        setup(0,26);out(0x3d9,5);
        for(unsigned n=0;n<3000000;n++){tick();if((top.sof&1)&&(top.pixel_ce&1))break;}
        for(unsigned n=0;n<100;n++)tick();
        check((top.de&1) && (top.rgbi&15)==5,"initial live palette");
        out(0x3d9,10);for(unsigned n=0;n<20;n++)tick();
        check((top.de&1) && (top.rgbi&15)==10,"mid-line palette write");
        // AEN permits DMA's memory half while suppressing I/O decode.
        bus(0,0xb8000,true,0x00,true);cga[0]=0;
        check(bus(0,0xbc000)==0,"DMA VRAM write");
        frame(0,26,10,0);
        blink_cursor();
        for(unsigned card=0;card<2;card++)for(unsigned phase=0;phase<30;phase++){
            const unsigned address=card?0xb0000:0xb8000;
            top.cycle_kind=0;top.cycle_address=address;top.cycle_wdata=0xcc;
            top.cycle_write=1;top.cycle_aen=0;top.cycle_valid=1;tick();top.cycle_valid=0;
            for(unsigned n=0;n<phase;n++)tick();
            top.reset=1;for(unsigned n=0;n<5;n++)tick();top.reset=0;
            for(unsigned n=0;n<10;n++)tick();
            check(top.cycle_ready && !top.cycle_done,"reset did not drain ISA");
            bus(0,address,true,0x3d);check(bus(0,address)==0x3d,"stale reset completion");
            check(!top.rgbi,"reset must blank both cards");
        }
        check(!top.irq && !top.drq,"display cards asserted IRQ/DRQ");
        std::puts("PCXT CGA/MDA ISA and complete-frame checks passed");
    }catch(const std::exception&e){std::fprintf(stderr,"display: %s\n",e.what());return 1;}
}
