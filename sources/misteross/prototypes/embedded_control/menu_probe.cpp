// SPDX-License-Identifier: GPL-3.0-or-later
#include "menu_bench.hpp"
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
