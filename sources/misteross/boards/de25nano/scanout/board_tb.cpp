// Digital board protocol. Only the explicit PLL/I2C model outputs are driven;
// functional DUT registers are never changed by this bench. No timing/physical proof.
#include "Vfes_de25_scanout_board.h"
#include "Vfes_de25_scanout_board___024root.h"
#include "verilated.h"
#include <algorithm>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
static void check(bool ok,const char *message) {if(!ok){std::fprintf(stderr,"FAIL: %s\n",message);std::exit(1);}}
struct Clock {
    uint64_t hz,next,rem=0; bool high=false,running=true;
    void advance(){const uint64_t half=500000000000000ull;next+=half/hz;rem+=half%hz;if(rem>=hz){++next;rem-=hz;}}
};
struct Rig {
    Vfes_de25_scanout_board t;
    Clock ref{50000000,10000000},sys{100000000,5000000},pix;
    uint64_t system_edges=0,pixel_edges=0,loss_system=0,loss_pixel=0;
    bool previous_hs=false,previous_boundary=false,lost=false,released=false,fault_seen=false;
    Rig(uint64_t phase, bool early_init=false, bool wait_hdmi=false):pix{74250000,phase+1}{
        t.CLOCK0_50=t.system_clk=0;t.ninit_done=1;t.top_reset_n=0;t.HDMI_TX_INT=1;
        t.video_rgb=0x123456;t.video_de=1;t.video_hs=t.video_vs=0;t.eval();
        set_lock(false);
        t.rootp->fes_de25_scanout_board__DOT__hdmi_config__DOT__configured=!wait_hdmi;
        t.eval();
        if (early_init) ref.next=1000000000ull; else events(100);
        t.ninit_done=0;t.top_reset_n=1;t.eval();set_lock(true);
        if (early_init) {
            events(100);
            check(!t.scanout_reset_n&&!t.clock_fault&&!t.display_ready,
                  "system released before the first reference-clock qualification");
            black();
        }
        if (wait_hdmi) {
            events(2000);
            check(t.scanout_reset_n&&!t.clock_fault&&!t.display_ready,
                  "unconfigured HDMI did not preserve a disabled live control path");
            black();
            t.rootp->fes_de25_scanout_board__DOT__hdmi_config__DOT__configured=1;t.eval();
        }
        train();
    }
    void black(){check(!t.HDMI_TX_D&&!t.HDMI_TX_DE,"clock-loss epoch exposed pixel data");}
    void set_lock(bool value){
        bool hs=t.HDMI_TX_HS,vs=t.HDMI_TX_VS;
        t.rootp->fes_de25_scanout_board__DOT__video_clock__DOT__locked=value;t.eval();
        if(!value){black();check(bool(t.HDMI_TX_HS)==hs&&bool(t.HDMI_TX_VS)==vs,"asynchronous loss clipped sync");
            if(released&&t.top_reset_n&&!lost){lost=true;loss_system=system_edges;loss_pixel=pixel_edges;}}
    }
    void step(){
        uint64_t now=std::min(ref.next,std::min(sys.next,pix.next));
        bool data=t.HDMI_TX_DE,hs=t.HDMI_TX_HS,vs=t.HDMI_TX_VS;
        bool pixel_rise=false;
        if(ref.next==now){if(ref.running){ref.high=!ref.high;t.CLOCK0_50=ref.high;}ref.advance();}
        if(sys.next==now){if(sys.running){sys.high=!sys.high;t.system_clk=sys.high;if(sys.high)++system_edges;}sys.advance();}
        if(pix.next==now){if(pix.running){pix.high=!pix.high;t.rootp->fes_de25_scanout_board__DOT__video_clock__DOT__outclk_0=pix.high;pixel_rise=pix.high;
            if(pixel_rise)++pixel_edges;else{unsigned n=pixel_edges%16;t.video_hs=n>=4&&n<8;t.video_vs=(pixel_edges/16)%16>=12;}}
            pix.advance();}
        bool boundary=pixel_rise&&previous_hs&&!t.video_hs&&!t.video_vs;
        t.eval();
        if(!data&&t.HDMI_TX_DE)check(pixel_rise&&previous_boundary,"picture re-enabled outside the registered complete HS trailing edge");
        if(hs&&!t.HDMI_TX_HS)check(pixel_rise&&!t.video_hs,"HS pulse shortened or changed off-clock");
        if(!hs&&t.HDMI_TX_HS)check(pixel_rise&&t.video_hs,"HS started mid-pulse or changed off-clock");
        if(vs&&!t.HDMI_TX_VS)check(pixel_rise&&!t.video_vs,"VS pulse shortened or changed off-clock");
        if(!vs&&t.HDMI_TX_VS)check(pixel_rise&&t.video_vs,"VS started mid-pulse or changed off-clock");
        if(pixel_rise){previous_hs=t.video_hs;previous_boundary=boundary;}
        check(!t.display_ready||t.HDMI_TX_DE,"display readiness preceded registered pins");
        if(t.top_reset_n){
            if(released)check(t.scanout_reset_n,"clock loss reset live bridge/FIFO epoch");
            if(t.scanout_reset_n)released=true;
            if(fault_seen)check(t.clock_fault,"clock fault was not sticky");
            if(t.clock_fault)fault_seen=true;
            if(lost){black();
                if(system_edges>=loss_system+3)check(t.clock_fault,"fault not latched within two system stages plus register");
                if(pixel_edges>=loss_pixel+3)check(!t.display_ready,"display readiness remained high after three pixel edges with peer clocks stopped");
            }
        }
    }
    void events(unsigned n){while(n--)step();}
    void pixels(unsigned n){uint64_t end=pixel_edges+n;for(unsigned i=0;pixel_edges<end;++i){check(i<50000,"pixel progress bound");step();}}
    void train(){ref.running=sys.running=pix.running=true;for(unsigned i=0;!t.HDMI_TX_DE||!t.display_ready||!t.scanout_reset_n;++i){check(i<20000,"startup/recovery bound");step();}check(!t.clock_fault,"cold startup falsely latched fault");}
    void high_sync(){for(unsigned i=0;!t.HDMI_TX_HS||!t.HDMI_TX_VS;++i){check(i<20000,"sync setup bound");step();}}
    void configuration_case(){
        const auto before=pixel_edges;
        t.rootp->fes_de25_scanout_board__DOT__hdmi_config__DOT__configured=0;t.eval();
        for(unsigned n=0;t.display_ready;++n){check(n<2000,"HDMI reconfiguration readiness close bound");step();}
        check(pixel_edges>before&&!t.clock_fault&&t.scanout_reset_n&&t.stream_ready,
              "configuration interval changed clock/bridge epoch");
        black();events(300);
        check(!t.display_ready&&!t.clock_fault&&t.scanout_reset_n&&t.stream_ready,
              "configuration interval did not preserve active clocks and epoch");
        t.rootp->fes_de25_scanout_board__DOT__hdmi_config__DOT__configured=1;t.eval();train();
        check(!t.clock_fault&&t.scanout_reset_n,"configuration recovery reset clock epoch");
        std::puts("PASS HDMI configuration-only interval closes readiness with clocks/raster live");
    }
    void loss_case(unsigned which){
        high_sync();
        ref.running=which!=0&&which!=2;sys.running=which!=0&&which!=1;pix.running=which!=3;
        set_lock(false);set_lock(true);
        if(pix.running)pixels(8);else events(200);
        black();ref.running=sys.running=pix.running=true;events(300);
        check(t.clock_fault&&t.scanout_reset_n&&!t.display_ready,"clock loss did not preserve epoch and close readiness");
        for(unsigned i=0;i<8;++i){set_lock(false);events(8);set_lock(true);events(16);}
        events(500);black();
        t.top_reset_n=0;t.eval();released=fault_seen=lost=false;
        check(!t.scanout_reset_n&&!t.clock_fault,"global reset did not clear epoch monitor");
        events(100);t.top_reset_n=1;t.eval();train();
        std::printf("PASS three-clock case=%u\n",which);
    }
};
int main(int argc, char **argv) {
    Verilated::commandArgs(argc,argv);
    Rig before_reference(0, true);
    Rig configuration_wait(77, false, true);
    configuration_wait.configuration_case();
    for (uint64_t phase : {0ull,77ull,999999ull}) {
        Rig r(phase);
        for (unsigned c=0;c<4;++c) r.loss_case(c);
    }
    std::puts("PASS digital 50/100/74.25MHz board:12 stopped-clock cases, early startup, HDMI wait, whole sync, latched loss, retained bridge epoch and cold recovery");
}
