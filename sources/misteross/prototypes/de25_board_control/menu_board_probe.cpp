// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vmenu_board_model.h"
#include "fes_embedded_control.h"
#include <array>
#include <cstdint>
#include <deque>
#include <fstream>
#include <iostream>
#include <iterator>
#include <stdexcept>
#include <vector>
static void check(bool value,const char* why) { if(!value)throw std::runtime_error(why); }
static std::vector<uint8_t> load(const char* path) {
    std::ifstream file(path,std::ios::binary);check(bool(file),"frame missing");
    std::vector<uint8_t> b{std::istreambuf_iterator<char>(file),{}};
    check(b.size()==1280*720*4,"frame size");return b;
}
struct Burst { uint32_t address,beats,index; };
struct Bench {
    Vmenu_board_model d;
    const std::array<std::vector<uint8_t>,2>& frames;
    std::deque<Burst> queue;
    uint64_t time=0,cpu_next=297,pixel_next=417,cpu_edges=0,replies=0,pixels=0,acks=0,offers=0;
    uint32_t h=0,v=720,last_sequence=0,last_data=0,read_index=0,held_address=0,held_length=0;
    bool pixel_run=true,allow_ar=true,allow_r=true,strict=true,have_frame=false,held=false,error=false;
    unsigned fetch_slot=0;
    explicit Bench(const std::array<std::vector<uint8_t>,2>& images):frames(images) {
        d.clk=0;d.pixel_clk=0;d.reset_n=0;d.pixel_reset_n=0;
        d.address=0;d.read=0;d.write=0;d.byteenable=15;d.writedata=0;
        d.ddr_arready=0;d.ddr_rvalid=0;d.ddr_rlast=0;d.ddr_rresp=0;d.ddr_rid=0;
        d.ddr_awready=0;d.ddr_wready=0;d.ddr_bvalid=0;d.ddr_bresp=0;d.ddr_bid=0;
        for(int i=0;i<8;i++)d.ddr_rdata[i]=0;
        advance(32);d.reset_n=1;d.pixel_reset_n=1;advance(64);
    }
    uint32_t word(uint32_t address) {
        check(address>=0x95000000,"DDR below arena");uint32_t off=address-0x95000000;
        unsigned slot=off/(4<<20);off%=4<<20;
        check(slot<2 && off+4<=frames[slot].size(),"DDR outside complete frame");
        const auto& b=frames[slot];return uint32_t(b[off])|uint32_t(b[off+1])<<8|uint32_t(b[off+2])<<16|uint32_t(b[off+3])<<24;
    }
    bool step() {
        if(pixel_run && pixel_next<=time)pixel_next=time+400;
        uint64_t next=cpu_next;if(pixel_run && pixel_next<next)next=pixel_next;
        bool ct=next==cpu_next,pt=pixel_run && next==pixel_next;
        bool cr=ct&&!d.clk,pr=pt&&!d.pixel_clk;
        d.ddr_arready=allow_ar && offers%7!=3;
        // One stalled-address cycle is followed by readiness, independently
        // of whether the producer offers another command.
        if(pr && d.ddr_arvalid && !d.ddr_arready)offers++;
        d.ddr_rvalid=allow_r&&!queue.empty();d.ddr_rlast=0;d.ddr_rresp=0;
        if(d.ddr_rvalid) {
            const auto& b=queue.front();d.ddr_rlast=b.index+1==b.beats;
            for(unsigned i=0;i<8;i++)d.ddr_rdata[i]=word(b.address+b.index*32+i*4);
        }
        d.eval();
        bool accepted=cr&&(d.read||d.write)&&!d.waitrequest;
        bool ar=pr&&d.ddr_arvalid&&d.ddr_arready,r=pr&&d.ddr_rvalid&&d.ddr_rready;
        bool active=d.qualified_reset_n;
        uint32_t sequence=d.displayed_sequence;
        Burst offer{d.ddr_araddr,uint32_t(d.ddr_arlen)+1,0};
        if(pr) {
            check(!d.ddr_awvalid && !d.ddr_wvalid && !d.ddr_wstrb,"menu issued a DDR write");
            if(held)check(d.ddr_arvalid && d.ddr_araddr==held_address && d.ddr_arlen==held_length,"held AXI address changed");
            if(ar) {
                check(d.ddr_arsize==5 && d.ddr_arburst==1 && !d.ddr_arid && !d.ddr_arcache && d.ddr_arprot==2,"DDR attributes");
                check(offer.address>=0x95000000 && offer.beats==64 && (offer.address&31)==0 &&
                      (offer.address&4095)+offer.beats*32<=4096,"DDR burst bounds");
                uint32_t off=offer.address-0x95000000,slot=off/(4<<20);check(slot<2,"DDR slot");
                check(off%(4<<20)==read_index*32,"DDR order");
                if(!read_index){fetch_slot=slot;have_frame=true;}else check(fetch_slot==slot,"slot changed in flight");
                read_index=(read_index+offer.beats)%115200;offers++;
            }
            if(active) {
                check(bool(d.video_de)==(h<1280&&v<720) && bool(d.video_hs)==(h>=1390&&h<1430) &&
                      bool(d.video_vs)==(v>=725&&v<730),"independent 720p raster differs");
                if(d.video_de && d.running && have_frame && !d.fault && strict) {
                    check(d.video_rgb==(word(0x95000000+fetch_slot*(4<<20)+(v*1280+h)*4)&0xffffff),"wrong HDMI-facing Tenfoot pixel");pixels++;
                }
            }
            held=d.ddr_arvalid&&!d.ddr_arready;held_address=d.ddr_araddr;held_length=d.ddr_arlen;
        }
        if(ct){d.clk=!d.clk;cpu_next+=297;}
        if(pt){d.pixel_clk=!d.pixel_clk;pixel_next+=400;}
        d.eval();
        if(cr) {
            cpu_edges++;
            if(d.readdatavalid||d.writeresponsevalid){replies++;error=d.response!=0;last_data=d.readdata;}
        }
        if(pr) {
            if(d.displayed_sequence!=sequence){check(active&&h==1279&&v==719 && d.displayed_sequence>last_sequence,"early frame acknowledgement");last_sequence=d.displayed_sequence;acks++;}
            if(r){check(!queue.empty(),"unowned DDR reply");if(++queue.front().index==queue.front().beats)queue.pop_front();}
            if(ar)queue.push_back(offer);
            if(!active){h=0;v=720;}else if(++h==1650){h=0;if(++v==750)v=0;}
        }
        time=next;check(time<12000000000ULL,"board model time cap");return accepted;
    }
    void advance(unsigned n){while(n--)step();}
    uint32_t access(uint8_t address,bool write,uint32_t value=0,bool success=true) {
        uint64_t before=replies,start=cpu_edges;d.address=address;d.write=write;d.read=!write;d.writedata=value;
        bool accepted=false;for(unsigned i=0;i<100&&!accepted;i++)accepted=step();
        check(accepted,"Avalon request blocked");d.read=0;d.write=0;
        for(unsigned i=0;i<3000&&replies==before;i++)step();
        check(replies==before+1 && cpu_edges-start<=260,"bounded Avalon response missing");
        if(error==success)throw std::runtime_error("unexpected Avalon response at register "+std::to_string(address));
        if(!success)check(last_data==0xffffffffU,"error vanished from CPU register data");
        for(unsigned i=0;i<100&&d.waitrequest;i++)step();check(!d.waitrequest,"return handshake blocked");return last_data;
    }
    void write(uint8_t a,uint32_t v){access(a,true,v);}
    uint32_t read(uint8_t a){return access(a,false);}
    void frame(uint32_t seq,unsigned slot) {
        // Separate submit readiness from complete display, as the native driver does.
        bool ready=false;for(unsigned n=0;n<100;n++){if(read(0x94)&(seq==1?1U:4U)){ready=true;break;}advance(70000);}
        check(ready,"menu submission readiness");write(0x84,1);write(0x88,0);write(0x8c,seq);write(0x90,slot);write(0x80,seq==1?1:2);
        for(unsigned n=0;n<300 && d.displayed_sequence!=seq;n++)advance(70000);
        check(d.displayed_sequence==seq && !d.fault && !d.underflows,"frame incomplete across clock boundary");
    }
};
int main(int argc,char** argv) {
    try {
        check(argc==4,"two actual Tenfoot frames and compiled build ID required");std::array<std::vector<uint8_t>,2> frames{load(argv[1]),load(argv[2])};
        std::string build=argv[3];check(build.size()==32,"build ID size");
        Bench b(frames);
        check(b.read(FES_EMBEDDED_REG_MAGIC)==FES_EMBEDDED_MAGIC && b.read(FES_EMBEDDED_REG_ABI)==0x10203043,"source ABI");
        for(unsigned i=0;i<4;i++)check(b.read(FES_EMBEDDED_REG_BUILD0+i*4)==std::stoul(build.substr(24-i*8,8),nullptr,16),"sealed build word");
        check(b.read(0xb4)==8U<<20 && b.read(0xcc)==1,"compiled geometry/maintenance identity");
        b.write(FES_EMBEDDED_REG_REQUEST_GENERATION_LO,1);b.write(FES_EMBEDDED_REG_REQUEST_GENERATION_HI,0);b.write(FES_EMBEDDED_REG_COMMAND,FES_EMBEDDED_COMMAND_ENABLE);
        b.frame(1,0);b.frame(2,1);b.frame(3,0);check(b.acks==3 && b.pixels>=2764800,"missing exact displayed pixels");
        b.strict=false;b.write(0x80,3);for(unsigned n=0;n<30000&&!b.d.stopped;n++)b.step();
        check(b.d.stopped && b.queue.empty() && !b.d.ddr_arvalid,"local stop left DDR traffic");
        b.write(FES_EMBEDDED_REG_REQUEST_COOKIE_LO,9);b.write(FES_EMBEDDED_REG_REQUEST_COOKIE_HI,0);b.write(FES_EMBEDDED_REG_COMMAND,FES_EMBEDDED_COMMAND_DISABLE);
        check(b.read(FES_EMBEDDED_REG_STATUS)==FES_EMBEDDED_STATUS_DRAINED && b.read(FES_EMBEDDED_REG_ACK_COOKIE_LO)==9,"owned drain did not cross back to CPU");
        b.pixel_run=false;b.access(FES_EMBEDDED_REG_MAGIC,false,0,false);b.pixel_run=true;b.advance(1000);
        b.access(FES_EMBEDDED_REG_MAGIC,false,0,false);
        std::cout<<"PASS board menu: 100/74.25 MHz, actual Tenfoot pixels, three frames, owned drain and bounded stopped-clock retirement\n";return 0;
    }catch(const std::exception& e){std::cerr<<"board menu: "<<e.what()<<"\n";return 1;}
}
