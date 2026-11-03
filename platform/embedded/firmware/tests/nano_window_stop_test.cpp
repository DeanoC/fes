/* SPDX-License-Identifier: BSD-3-Clause */
/* Share the real AXI/Avalon and inherited probe models, not a stop emulator. */
#define FES_WINDOW_STOP_TEST 1
#include "nano_fabric_stop_test.cpp"

int main() {
    try {
        unsigned cases=0,points=0;
        for(bool active:{false,true}) {
            Fixture f("",active);
            require(!fes_nano_fabric_stop(&f.stop),"window stop failed");
            require(f.stop.done && f.writes==(active?3U:0U) && f.cancels==(active?1U:0U),"window stop command count differs");
            require(f.stop.last.words[21]==1,"window maintenance hold was not read at c8");
            points=std::max(points,f.reads);
            auto writes=f.writes,cancels=f.cancels;
            require(!fes_nano_fabric_stopped(&f.stop) && !fes_nano_fabric_stop(&f.stop) &&
                f.writes==writes && f.cancels==cancels,"completed window stop replayed commands");
            f.fault="maintenance";require(fes_nano_fabric_stopped(&f.stop)<0,"late window maintenance loss accepted");f.retired();cases++;
        }
        {
            Fixture f;f.stall=false;
            for(unsigned i=0;i<32;i++)f.tick();
            require(f.pending.size()==4,"window test did not create four external reads");
            require(!fes_nano_fabric_stop(&f.stop) && f.pending.empty() && f.stop.done,
                "four outstanding window bursts did not drain");cases++;
        }
        {
            Fixture f;f.stall=false;f.responses=true;
            for(unsigned i=0;i<400000 && !(f.access(false,0x90)&4);i++)f.tick();
            require((f.access(false,0x90)&4) && f.access(false,0xa4)==3686400,
                "window source did not complete its full frame");
            require(f.access(false,56)==1 && !f.access(false,72),"frame completion invented an endpoint drain");
            require(!fes_nano_fabric_stop(&f.stop) && f.writes==3 && f.stop.disable_issued,
                "frame completion bypassed DISABLE");cases++;
        }
        for(const std::string fault:{"scope","scope-two","identity","version","abi","build","base","span","maintenance",
            "reserved","pending","queue","unstable","draining","stale-ack","request","late-generation","wrong-cookie",
            "premature","owner-after-read","owner-after-write","read-positive","write-before","write-after","write-positive",
            "late-restart","held","no-response","response","probe-identity","probe-owner","probe-read","probe-write",
            "probe-held","probe-no-response"}) {
            Fixture f(fault);require(fes_nano_fabric_stop(&f.stop)<0,"invalid window stop accepted: "+fault);
            f.retired();cases++;
        }
        for(const std::string fault:{"held","no-response"}) {
            Fixture f(fault);
            require(fes_nano_fabric_stop(&f.stop)==-110,"stalled reader lost timeout");
            require(f.stop.observations && f.stop.observed_poll==128 &&
                f.stop.first_observed.status==FES_EMBEDDED_STATUS_ENABLED &&
                f.stop.last_observed.status==FES_EMBEDDED_STATUS_DRAINING &&
                f.stop.last_observed.generation==1 && f.stop.last_observed.request_generation==1 &&
                f.stop.last_observed.cookie==7 && !f.stop.last_observed.ack_generation &&
                !f.stop.last_observed.ack_cookie,"missing cached stable failure samples");
            require(f.stop.last_observed.pending==(fault=="held"?0x10000U:1U),
                "cached sample did not distinguish held address and missing response");
            for(auto word:f.stop.last.words)require(!word,"diagnostic sample invented a drain receipt");
            auto samples=f.stop.observations;auto pending=f.stop.last_observed.pending;
            f.retired();
            require(f.stop.observations==samples && f.stop.last_observed.pending==pending,
                "inspecting retired stop changed its failure samples");cases++;
        }
        for(const std::string fault:{"identity","unstable","owner-after-read"}) {
            Fixture f(fault);
            require(fes_nano_fabric_stop(&f.stop)<0 && !f.stop.observations,
                "partial, unstable or unowned reads fabricated a diagnostic sample");
            f.retired();cases++;
        }
        for(unsigned point=1;point<=88;point++) {
            Fixture f;f.fail_read=point;
            require(fes_nano_fabric_stop(&f.stop)==-7 && f.reads==point,"window lost a failed read");f.retired();cases++;
        }
        for(unsigned wrong=0;wrong<4;wrong++) {
            Fixture f;
            if(wrong==0)f.stop.bytes=8192;
            if(wrong==1)f.stop.abi=0x10203041;
            if(wrong==2)f.stop.bus_base+=4096;
            if(wrong==3)f.stop.qualified=0;
            require(fes_nano_fabric_stop(&f.stop)==-22 && !f.reads && !f.probe_reads && !f.writes,
                "foreign window tuple performed I/O");f.retired();cases++;
        }
        std::cout<<"{\"status\":\"passed\",\"cases\":"<<cases<<",\"read_failure_points\":88,\"initial_stop_reads\":"<<points
            <<",\"frame_bytes\":3686400,\"window_bytes\":4194304,\"hardware_access\":false,\"memory_released\":false}\n";
        return 0;
    } catch(const std::exception& e) {std::cerr<<"window stop: "<<e.what()<<"\n";return 1;}
}
