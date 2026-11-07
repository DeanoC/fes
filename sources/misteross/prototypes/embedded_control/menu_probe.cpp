// SPDX-License-Identifier: GPL-3.0-or-later
// Actual menu/video/owned-endpoint RTL against independent memory and raster checks.
#include "Vembedded_menu_top.h"
#include "fes_embedded_control.h"
#include <array>
#include <cstdint>
#include <deque>
#include <fstream>
#include <iostream>
#include <iterator>
#include <stdexcept>
#include <vector>

static void require(bool condition, const char* why) {
    if (!condition) throw std::runtime_error(why);
}
static std::vector<uint8_t> load(const char* path) {
    std::ifstream file(path, std::ios::binary);
    require(bool(file), "frame missing");
    std::vector<uint8_t> bytes{std::istreambuf_iterator<char>(file), {}};
    require(bytes.size()==1280*720*4, "frame geometry differs");
    return bytes;
}
struct Burst { uint32_t address, beats, index; };
struct Bench {
    Vembedded_menu_top d;
    const std::array<std::vector<uint8_t>,2>& frames;
    std::deque<Burst> queue;
    static constexpr uint32_t base=0x95000000, slot_bytes=4<<20, frame_beats=115200;
    uint64_t cycles=0, addresses=0, beats=0, checked_pixels=0, acknowledgements=0;
    uint32_t h=0, v=720, read_index=0, pixel_count=0, last_sequence=0;
    unsigned fetch_slot=0;
    bool allow_ar=true, allow_r=true, strict=false, have_frame=false, ar_held=false;
    bool bad_response=false;
    uint32_t held_address=0, held_length=0;

    explicit Bench(const std::array<std::vector<uint8_t>,2>& images, bool invalid=false): frames(images) {
        d.clk=0; d.reset_n=0; d.video_reset_n=1; d.csr_valid=0;
        d.csr_write=0; d.csr_address=0; d.csr_wdata=0; d.csr_wstrb=15; d.csr_response_ready=1;
        d.start=0; d.initial_slot=0; d.initial_sequence=1;
        d.submit_valid=0; d.submit_slot=1; d.submit_sequence=2; d.quiesce=0;
        tick(); tick(); d.reset_n=1; tick();
        require(!d.start_ready && !d.running && bool(d.fault)==invalid, "disabled reader granted video");
        require(read(FES_EMBEDDED_REG_MAGIC)==FES_EMBEDDED_MAGIC, "endpoint identity differs");
    }
    uint32_t word(uint32_t address) const {
        require(address>=base, "read below arena");
        uint64_t offset=uint64_t(address)-base;
        unsigned slot=offset/slot_bytes;
        offset%=slot_bytes;
        require(slot<2 && offset+4<=frames[slot].size(), "read outside visible slot bytes");
        const auto& f=frames[slot];
        return uint32_t(f[offset])|uint32_t(f[offset+1])<<8|uint32_t(f[offset+2])<<16|uint32_t(f[offset+3])<<24;
    }
    void tick() {
        d.clk=0;
        d.m_arready=allow_ar && cycles%7!=2 && cycles%7!=3;
        d.m_rvalid=allow_r && !queue.empty(); d.m_rlast=0; d.m_rresp=0; d.m_rid=0;
        for (unsigned i=0;i<8;i++) d.m_rdata[i]=0;
        if (d.m_rvalid) {
            const auto& burst=queue.front(); d.m_rlast=burst.index+1==burst.beats;
            for(unsigned i=0;i<8;i++) d.m_rdata[i]=word(burst.address+burst.index*32+i*4);
            if (bad_response) d.m_rresp=2;
        }
        d.eval();
        if(d.reset_n && ar_held) require(d.m_arvalid && d.m_araddr==held_address && d.m_arlen==held_length,
            "stalled AXI address changed or vanished");
        bool ar=d.m_arvalid && d.m_arready, r=d.m_rvalid && d.m_rready;
        Burst offered{d.m_araddr,uint32_t(d.m_arlen)+1,0};
        if(ar) {
            require(d.m_arsize==5 && d.m_arburst==1 && d.m_arid==0, "AXI attributes differ");
            uint32_t offset=offered.address-base, slot=offset/slot_bytes;
            require(offered.address>=base && slot<2 && offered.address%32==0, "AXI outside arena");
            require(offered.beats==64 && (offered.address&4095)+offered.beats*32<=4096,
                "AXI burst crossed page or changed length");
            require(offset%slot_bytes==read_index*32, "scanout skipped/reordered memory");
            if(read_index==0) { fetch_slot=slot; have_frame=true; }
            else require(fetch_slot==slot, "slot changed within a read frame");
            read_index=(read_index+offered.beats)%frame_beats;
            addresses++;
        }
        if(d.reset_n && d.video_reset_n) {
            require(bool(d.de)==(h<1280 && v<720) && bool(d.hs)==(h>=1390 && h<1430) &&
                bool(d.vs)==(v>=725 && v<730), "720p raster/sync changed");
            if(h==0 && v==0) pixel_count=0;
            if(d.de && have_frame && d.running && !d.quiesce && !d.fault) {
                uint32_t expected=word(base+fetch_slot*slot_bytes+(v*1280+h)*4)&0xffffff;
                require(d.rgb==expected || (!strict && d.rgb==0), "wrong or shifted HDMI-facing pixel");
                pixel_count++; checked_pixels++;
            }
        }
        uint32_t sequence=d.displayed_sequence;
        ar_held=d.m_arvalid && !d.m_arready; held_address=d.m_araddr; held_length=d.m_arlen;
        d.clk=1; d.eval();
        if(d.reset_n && d.video_reset_n && d.displayed_sequence!=sequence) {
            require(h==1279 && v==719 && pixel_count==1280*720, "frame acknowledged before final pixel");
            require(d.displayed_sequence>last_sequence, "display sequence regressed");
            last_sequence=d.displayed_sequence; acknowledgements++;
        }
        if(r) { require(!queue.empty(), "unowned response"); if(++queue.front().index==queue.front().beats)queue.pop_front(); beats++; }
        if(ar)queue.push_back(offered);
        if(!d.reset_n || !d.video_reset_n) { h=0; v=720; }
        else { if(++h==1650) { h=0; if(++v==750)v=0; } }
        cycles++;
    }
    template<class F> void until(F predicate, uint64_t bound, const char* why) {
        for(uint64_t i=0;i<bound;i++) { if(predicate())return; tick(); }
        throw std::runtime_error(why);
    }
    void write(uint8_t address, uint32_t value) {
        d.csr_address=address; d.csr_wdata=value; d.csr_write=1; d.csr_valid=1;
        until([&]{return bool(d.csr_ready);},8,"CSR blocked"); tick(); d.csr_valid=0;
        require(d.csr_response_valid && !d.csr_error,"CSR write rejected"); tick();
    }
    uint32_t read(uint8_t address) {
        d.csr_address=address; d.csr_write=0; d.csr_valid=1;
        until([&]{return bool(d.csr_ready);},8,"CSR blocked");tick(); d.csr_valid=0;
        require(d.csr_response_valid && !d.csr_error,"CSR read rejected"); uint32_t result=d.csr_rdata;tick();return result;
    }
    void enable(uint64_t generation) {
        write(FES_EMBEDDED_REG_REQUEST_GENERATION_LO,generation);
        write(FES_EMBEDDED_REG_REQUEST_GENERATION_HI,generation>>32);
        write(FES_EMBEDDED_REG_COMMAND,FES_EMBEDDED_COMMAND_ENABLE);
        require(!d.reader_abort && d.reader_generation==generation,"endpoint enable differs");
    }
    void begin(uint32_t sequence=1, unsigned slot=0) {
        require(d.start_ready,"video did not observe retirement/new owner");
        d.initial_sequence=sequence; d.initial_slot=slot; d.start=1;tick();d.start=0;tick();
        require(d.running && !d.fault,"video start failed");
    }
    void submit(uint32_t sequence, unsigned slot) {
        until([&]{return bool(d.submit_ready);},1250000,"submission blocked");
        d.submit_sequence=sequence;d.submit_slot=slot;d.submit_valid=1;tick();d.submit_valid=0;tick();
    }
    void stop() {
        strict=false;d.quiesce=1;
        until([&]{return bool(d.stopped);},5000,"video stop failed");
        require(!d.running && !d.fault && queue.empty() && !d.m_arvalid,"local stop left reader traffic");
        d.quiesce=0; tick();
    }
    void disable(uint64_t generation, uint64_t cookie) {
        write(FES_EMBEDDED_REG_REQUEST_GENERATION_LO,generation);
        write(FES_EMBEDDED_REG_REQUEST_GENERATION_HI,generation>>32);
        write(FES_EMBEDDED_REG_REQUEST_COOKIE_LO,cookie);
        write(FES_EMBEDDED_REG_REQUEST_COOKIE_HI,cookie>>32);
        write(FES_EMBEDDED_REG_COMMAND,FES_EMBEDDED_COMMAND_DISABLE);
    }
};
int main(int argc, char** argv) {
    try {
        require(argc==3 || argc==4,"two actual Tenfoot frames required");
        std::array<std::vector<uint8_t>,2> frames{load(argv[1]),load(argv[2])};
        require(frames[0]!=frames[1],"frames must differ");
        if (argc==4) {
            require(std::string(argv[3])=="invalid", "unknown case");
            Bench b(frames, true); b.enable(1); b.d.start=1;
            for(int i=0;i<100;i++)b.tick();
            require(b.d.fault && !b.d.start_ready && !b.d.running && b.addresses==0,
                "invalid arena granted memory traffic");
            std::cout<<"PASS invalid single-slot arena rejected before DDR access\n";
            return 0;
        }
        uint64_t checked=0;
        {
            Bench b(frames);b.enable(1);b.strict=true;b.begin();
            b.until([&]{return b.d.displayed_sequence==1;},1250000,"first menu frame not acknowledged");
            require(b.checked_pixels==921600 && b.d.underflows==0,"first frame incomplete/underflowed");
            b.submit(2,1);
            b.until([&]{return b.d.displayed_sequence==2;},2500000,"second menu frame not acknowledged");
            require(b.d.underflows==0,"ordinary backpressure underflowed");
            checked=b.checked_pixels;b.stop();b.disable(1,1);
            b.until([&]{return b.read(FES_EMBEDDED_REG_STATUS)&FES_EMBEDDED_STATUS_DRAINED;},16,"endpoint did not drain");
            require(b.read(FES_EMBEDDED_REG_ACK_GENERATION_LO)==1 && b.read(FES_EMBEDDED_REG_ACK_COOKIE_LO)==1,
                "drain owner/cookie differs");
            require(!b.d.start_ready,"local stop authorized same-generation restart");
            b.enable(2); b.read_index=0;b.have_frame=false; b.begin(3,0);b.strict=true;
            b.until([&]{return b.d.displayed_sequence==3;},2500000,"new generation did not scan");
            checked=b.checked_pixels;b.stop();b.disable(2,2);
            require(!b.d.fault,"normal stop/disable faulted");
        }
        {
            Bench b(frames); b.enable(1); b.allow_ar=false; b.begin();
            b.until([&]{return bool(b.d.m_arvalid);},32,"no held address");
            b.d.quiesce=1; for(int i=0;i<64;i++)b.tick();
            require(!b.d.stopped && b.d.m_arvalid,"stop ignored held address");
            b.allow_ar=true;b.stop();b.disable(1,1);
        }
        {
            Bench b(frames);b.enable(1);b.begin();
            b.until([&]{return b.beats>20;},2000,"no initial DDR progress");
            b.allow_r=false;for(unsigned i=0;i<1250000;i++)b.tick();
            require(b.d.underflows>0 && b.d.displayed_sequence==0,"incomplete frame acknowledged");
            b.allow_r=true;b.stop();b.disable(1,1);
        }
        {
            Bench b(frames);b.enable(1);b.begin();
            b.until([&]{return b.beats>20;},1000,"no DDR progress before fault");
            b.bad_response=true;for(int i=0;i<32;i++)b.tick();
            require(b.d.fault && b.d.reader_abort && !b.d.stopped,"bus fault forged local stop");
            auto count=b.addresses;for(int i=0;i<32;i++)b.tick();
            require(b.addresses==count && b.d.rgb==0,"fault replayed read or leaked pixels");
        }
        {
            Bench b(frames);b.enable(1);b.begin();b.submit(1,1);
            require(b.d.fault,"stale sequence accepted");
        }
        {
            Bench b(frames);b.enable(1);b.begin();b.submit(2,0);
            require(b.d.fault,"currently submitted slot reused");
        }
        {
            Bench b(frames);b.enable(1);b.begin();b.disable(1,1);
            for(int i=0;i<8;i++)b.tick();
            require(b.d.fault && !b.d.stopped,"ownership loss forged local stop");
        }
        {
            Bench b(frames);b.enable(1);b.d.video_reset_n=0;b.tick();b.d.video_reset_n=1;b.tick();
            require(!b.d.start_ready,"video-only reset adopted active endpoint");
            b.d.start=1;b.tick();require(b.d.fault,"unowned start accepted after video reset");
        }
        {
            Bench b(frames); b.enable(1); b.begin(); b.stop(); b.disable(1,1);
            b.until([&]{return b.read(FES_EMBEDDED_REG_STATUS)&FES_EMBEDDED_STATUS_DRAINED;},16,"restart drain failed");
            b.enable(2); require(b.d.start_ready,"new endpoint did not permit start attempt");
            b.d.initial_sequence=1; b.d.start=1; b.tick();
            require(b.d.fault && !b.d.running,"initial sequence replay accepted");
        }
        std::cout<<"PASS owned menu scanout: "<<checked<<" exact Tenfoot pixels; double-buffer, full-frame acknowledgement, held-address stop, underflow, faults and ownership\n";
    } catch(const std::exception& e) { std::cerr<<"FAIL "<<e.what()<<"\n";return 1; }
}
