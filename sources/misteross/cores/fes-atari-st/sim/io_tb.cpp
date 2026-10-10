// SPDX-License-Identifier: GPL-3.0-or-later
// Register transactions exercise the actual motherboard and peripheral
// modules. Expected byte lanes, MFP vectors and Atari display-enable line
// counts are independent of the wrapper's implementation.
#include "Vst_io_sim_top.h"
#include "verilated.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <string>

static constexpr unsigned system_hz = 52'224'000;
static constexpr unsigned mfp_base = 0xfffa00;
struct Test {
    Vst_io_sim_top dut;
    uint64_t cycles=0, assertions=0, crystal_ticks=0;
    unsigned frames=0, lines=0, display_ends=0, cpu_phase=0;
    uint64_t cpu_ticks=0;
    unsigned first_display_line=UINT32_MAX, first_display_phase=0;
    bool previous_display=false;
    void require(bool good,const std::string &message) {
        ++assertions;
        if (!good) {
            std::cerr<<"ST I/O cycle "<<cycles<<": "<<message<<'\n';
            std::exit(1);
        }
    }
    void tick() {
        dut.cpu_cycle_ce=!dut.reset && cpu_phase+8'000'000>=system_hz;
        cpu_phase=dut.reset?0:(cpu_phase+8'000'000)%system_hz;
        cpu_ticks+=dut.cpu_cycle_ce;
        dut.clk=0; dut.eval();
        if (!dut.reset) {
            frames+=dut.vblank; lines+=dut.hblank;
            crystal_ticks+=dut.timer_ce_level;
            display_ends+=previous_display && !dut.timer_b_level;
            if(!previous_display&&dut.timer_b_level&&first_display_line==UINT32_MAX){
                first_display_line=dut.display_line;first_display_phase=dut.display_phase;
            }
        }
        previous_display=dut.timer_b_level;
        dut.clk=1; dut.eval(); ++cycles;
    }
    void run(unsigned clocks) { while (clocks--) tick(); }
    void reset(unsigned resolution=0,unsigned sync=2) {
        dut.req=0; dut.write=0; dut.addr=0; dut.wdata=0; dut.byte_enable=0;
        dut.irq_ack=0; dut.irq_level=0; dut.screen_base=0x10234;
        dut.resolution=resolution; dut.sync_mode=sync; dut.monochrome=resolution==2;
        for (unsigned k=0;k<5;++k) dut.keyboard[k]=0;
        dut.controller_buttons=0; dut.mouse_valid=0; dut.mouse_dx=0; dut.mouse_dy=0;
        dut.mouse_buttons=0; dut.media_ready=0; dut.media_data=0;
        dut.media_valid=0; dut.dma_ready=0;
        dut.reset=1; tick(); tick(); dut.reset=0; tick();
        frames=lines=display_ends=0; crystal_ticks=0;
        first_display_line=UINT32_MAX;first_display_phase=0;
    }
    uint16_t bus(bool write,unsigned address,unsigned data=0,unsigned lanes=3,unsigned hold=1) {
        dut.req=1; dut.write=write; dut.addr=address>>1;
        dut.wdata=data; dut.byte_enable=lanes;
        tick(); require(dut.selected,"peripheral address selection");
        for(unsigned waited=1;!dut.ack&&waited<100;++waited)tick();
        require(dut.ack,"peripheral acknowledgement");
        const uint16_t result=dut.rdata;
        for (unsigned k=1;k<hold;++k) {
            tick(); require(dut.ack && dut.rdata==result,"held bus retains one acknowledgement/read value");
        }
        dut.req=0; tick(); require(!dut.ack,"bus acknowledgement releases");
        return result;
    }
    uint8_t mread(unsigned offset) { return bus(false,mfp_base+offset,0,1)&255; }
    void mwrite(unsigned offset,unsigned data,unsigned hold=1) { bus(true,mfp_base+offset,data,1,hold); }
    uint8_t acread(unsigned address) { return bus(false,address,0,2)>>8; }
    void acwrite(unsigned address,unsigned data,unsigned hold=1) { bus(true,address,data<<8,2,hold); }
    void ymwrite(unsigned reg,unsigned value) { acwrite(0xff8800,reg); acwrite(0xff8802,value); }
    void iack(unsigned level,unsigned hold=1) {
        dut.irq_ack=1; dut.irq_level=level; run(hold);
        dut.irq_ack=0; tick();
    }
};

static void lanes_and_reset(Test &t) {
    t.reset();
    t.require(t.dut.irq==0 && !t.dut.irq_vectored,"reset has no pending interrupt");
    t.require(!t.dut.media_req && !t.dut.dma_req && t.dut.audio_pcm==0,"reset peripherals are quiet");
    t.require(t.dut.floppy_port_a==255,"reset YM floppy lines are pulled high");
    // The 68901 sits on D7..0 (odd bytes); ACIA/PSG sit on D15..8.
    t.bus(true,mfp_base+3,0xa500,2);
    t.require(t.mread(3)==0,"MFP ignores an upper-lane write");
    t.mwrite(3,0x08,15); t.require(t.mread(3)==0x08,"MFP low-lane write and held request");
    t.require((t.bus(false,mfp_base+3,0,2)&0xff00)==0xff00,"MFP upper lane is undriven");
    t.bus(true,0xfffc00,0x0016,1);
    t.require(t.acread(0xfffc00)==0,"keyboard ACIA ignores lower lane");
    t.acwrite(0xfffc00,0x16,15);
    t.require(t.acread(0xfffc00)==2,"keyboard ACIA upper-lane control");
    t.bus(true,0xfffc04,0x0015,1);
    t.require(t.acread(0xfffc04)==0,"MIDI ACIA ignores lower lane");
    t.acwrite(0xfffc04,0x15); t.require(t.acread(0xfffc04)==2,"MIDI ACIA has an independent register");
    t.ymwrite(7,0xff); t.ymwrite(14,0xf9);
    t.require(t.dut.floppy_port_a==0xf9,"YM upper lane drives side and active-low floppy selects");
    t.bus(true,0xff8802,0x0000,1);
    t.require(t.dut.floppy_port_a==0xf9,"YM ignores lower-lane data write");
    t.acwrite(0xff8800,14);
    t.require(t.bus(false,0xff8800,0,3)==0xf9ff,"PSG big-endian byte read");
    t.bus(true,0xff8608,0xa500,2);
    t.require(t.bus(false,0xff8608)==0xff00,"floppy DMA address ignores upper lane");
    t.bus(true,0xff8609,0x0012,1); t.require(t.bus(false,0xff8609,0,1)==0xff12,"floppy DMA odd-byte address write");
    t.bus(true,0xff860b,0x0034,1); t.bus(true,0xff860d,0x0057,1);
    t.require(t.bus(false,0xff860d,0,1)==0xff56,"floppy DMA low address remains word aligned");
    t.bus(true,0xff8606,0x0082); // FDC track register, word-only indirect access.
    t.bus(true,0xff8604,0x0037,1);
    t.require(t.bus(false,0xff8604)==0xff00,"FDC indirect register ignores byte-only write");
    t.bus(true,0xff8604,0x0037); t.require(t.bus(false,0xff8604)==0xff37,"FDC indirect word write");
    t.ymwrite(8,15); t.run(2000);
    t.require(t.dut.audio_pcm!=0,"PSG reaches the motherboard audio output");
    // A CPU execution hold resets peripherals independently of external media.
    t.dut.media_ready=1; t.dut.reset=1; t.tick(); t.tick();
    t.require(t.dut.irq==0 && !t.dut.media_req && !t.dut.dma_req && t.dut.audio_pcm==0,
        "execution reset immediately clears IRQ, DMA and audio outputs");
    t.require(t.dut.floppy_port_a==255,"execution reset releases floppy-select outputs");
    t.dut.reset=0; t.tick();
    t.require(t.mread(3)==0 && t.acread(0xfffc00)==0 && t.acread(0xfffc04)==0,
        "execution reset clears peripheral programming");
    t.require(t.bus(false,0xff8608)==0xff00,"execution reset clears floppy DMA address");
    t.dut.req=1; t.dut.addr=0xff9000>>1; t.dut.byte_enable=3; t.tick();
    t.require(!t.dut.selected && !t.dut.ack,"unclaimed MMIO belongs to the expansion socket");
    t.dut.req=0; t.tick();
}

static void interrupt_connection(Test &t) {
    t.reset();
    t.mwrite(0x17,0x48); // Software EOI, vector base $40, matching EmuTOS.
    t.mwrite(0x07,0x80); t.mwrite(0x13,0x80); // GPIP7: monitor input, channel 15.
    t.mwrite(0x09,0x20); t.mwrite(0x15,0x20); // Timer C: channel 5.
    t.mwrite(0x23,192); t.mwrite(0x1d,0x50); // 2.4576 MHz /64 /192 = 200 Hz.
    for (unsigned n=0;t.dut.irq!=6;++n) { t.require(n<270000,"Timer C interrupt timeout"); t.tick(); }
    t.require(t.dut.irq_vectored && t.dut.irq_vector==0x45,"Timer C vectored level-6 connection");
    t.dut.monochrome=1; t.run(3);
    t.require(t.dut.irq_vector==0x4f,"monitor GPIP7 takes priority above Timer C");
    t.dut.irq_ack=1; t.dut.irq_level=6; t.tick();
    t.require(t.dut.irq_vector==0x4f,"IACK latches the selected MFP vector");
    t.require((t.mread(0x0b)&0x80)==0 && (t.mread(0x0d)&0x20)!=0,"IACK consumes only the monitor source");
    t.require((t.mread(0x0f)&0x80)!=0 && t.dut.irq!=6,"software EOI blocks the lower pending source");
    t.mwrite(0x0f,0x7f); t.run(30);
    t.require(t.dut.irq==6 && t.dut.irq_vector==0x4f,"held IACK keeps its vector and does not consume Timer C");
    t.require((t.mread(0x0d)&0x20)!=0,"lower source survives a stretched IACK");
    t.dut.irq_ack=0; t.tick();
    t.require(t.dut.irq_vector==0x45,"ending IACK exposes the next pending vector");
    t.iack(6,20); t.require((t.mread(0x0d)&0x20)==0 && (t.mread(0x11)&0x20)!=0,"new IACK consumes Timer C once");
    t.mwrite(0x11,0xdf); t.mwrite(0x1d,0);
    while (t.frames==0) t.tick();
    const auto frame_boundary=t.cpu_ticks;
    while (t.cpu_ticks-frame_boundary<60) {
        t.require(t.dut.irq==2,"VBL does not assert before STF WS1 phase 60");
        t.tick();
    }
    t.require(t.cpu_ticks-frame_boundary==60 && t.dut.display_line==0 &&
              t.dut.display_phase==60,"IRQ4 uses native CPU cycles after frame boundary");
    t.require(t.dut.irq==4 && !t.dut.irq_vectored,"native VBL is autovectored level 4");
    t.iack(4); t.require(t.dut.irq==2,"VBL acknowledgement leaves native HBL pending");
    t.iack(2); t.require(t.dut.irq==0,"native HBL acknowledgement clears level 2");
}

static void vbl_interrupt_phase(Test &t,unsigned resolution,unsigned sync) {
    t.reset(resolution,sync);
    for (unsigned frame=1;frame<=2;++frame) {
        while(t.frames<frame) t.tick();
        const auto boundary=t.cpu_ticks;
        t.require(t.dut.irq==2,"frame boundary keeps HBL pending before IRQ4");
        while(t.cpu_ticks-boundary<60) {
            t.require(t.dut.irq==2,"IRQ4 waits for all sixty native cycles");
            t.tick();
        }
        t.require(t.dut.irq==4 && t.dut.display_phase==60,
                  "PAL/NTSC/mono IRQ4 phase is independent of frame period");
        t.run(37); t.require(t.dut.irq==4,"IRQ4 stays pending until its IACK");
        t.iack(2); t.require(t.dut.irq==4,"HBL IACK cannot clear delayed VBL");
        t.iack(4); t.require(t.dut.irq==0,"IRQ4 IACK clears one delayed event");
    }
    while(t.frames<3) t.tick();
    // Cancel a scheduled event through execution Hold/reset, then watch
    // beyond its old deadline without waiting for another native frame.
    t.reset(resolution,sync);
    const auto reset_tick=t.cpu_ticks;
    while(t.cpu_ticks-reset_tick<80) {
        t.require(t.dut.irq==0,"reset cancels the scheduled VBL event");
        t.tick();
    }
}

static void timer_c_rate(Test &t) {
    t.reset();
    const auto before=t.crystal_ticks;
    t.run(system_hz);
    t.require(t.crystal_ticks-before==2'457'600,"fractional MFP crystal rate is 2.4576 MHz");
    t.mwrite(0x17,0x40); t.mwrite(0x09,0x20); t.mwrite(0x15,0x20);
    t.mwrite(0x23,192); t.mwrite(0x1d,0x50);
    uint64_t previous=0;
    for (unsigned event=0;event<24;++event) {
        for (unsigned n=0;t.dut.irq!=6;++n) { t.require(n<270000,"periodic Timer C timeout"); t.tick(); }
        if (previous) t.require(t.cycles-previous==system_hz/200,"Timer C keeps the stock 200 Hz OS tick");
        previous=t.cycles; t.iack(6);
    }
}

static void display_enable(Test &t,unsigned resolution,unsigned sync,unsigned fps,unsigned active) {
    t.reset(resolution,sync);
    // Timer B counts native display-enable falling edges, once per active
    // scanline. Count 255 has no reload during a 200-line color frame.
    t.mwrite(0x21,255); t.mwrite(0x1b,8);
    while (t.frames<1) t.tick();
    uint64_t previous_frame=t.cpu_ticks;
    unsigned previous_lines=t.lines;
    t.require(t.display_ends==active,"one timer-B event per active native line");
    if(resolution!=2){
        // Ordinary ST timing positions from the primary video timing table.
        t.require(t.first_display_line==(fps==50?63u:34u),"Timer B begins after the native vertical porch");
        const unsigned phase=t.first_display_phase;
        t.require(phase>=(fps==50?80u:76u)&&phase<(fps==50?82u:78u),"Timer B follows the display porch by 24 CPU cycles");
    }
    if (active==200) t.require(t.mread(0x21)==55,"Timer B receives the 200 active color lines through its event pin");
    unsigned previous_ends=t.display_ends;
    for (unsigned frame=0;frame<3;++frame) {
        const unsigned target=t.frames+1;
        while (t.frames<target) t.tick();
        t.require(t.display_ends-previous_ends==active,"blank lines never create Timer B events");
        const uint64_t clocks=t.cpu_ticks-previous_frame;
        const unsigned frame_lines=resolution==2?501:fps==50?313:263;
        const unsigned line_cycles=resolution==2?224:fps==50?512:508;
        t.require(clocks==uint64_t(frame_lines)*line_cycles,"native frame has exact CPU cycle count");
        t.require(t.lines-previous_lines==frame_lines,"VBL occurs at the last complete native line");
        previous_lines=t.lines;
        t.require(t.dut.video_counter==0x10200,"VBL reloads the aligned shifter counter");
        previous_ends=t.display_ends; previous_frame=t.cpu_ticks;
    }
    t.mwrite(0x1b,0);
    const unsigned expected=255-(4*active)%255;
    t.require(t.mread(0x21)==expected,"Timer B counts all active display-enable edges, including reload");
    std::cout<<"ST I/O: nominal "<<fps<<" Hz, "<<active<<" Timer B events/frame\n";
}

// Hatari 2.5 Video_CalculateAddress: read cycle minus eight, then
// two bytes per four cycles, clamped to the ordinary DMA line window.
// Check every fabric edge, including the holds between CPU enables, and
// carry through both byte boundaries used by FF8205/07/09.
static void shifter_counter(Test &t,unsigned resolution,unsigned sync) {
    t.reset(resolution,sync);
    const bool mono=resolution==2, pal=(sync&2)!=0;
    const unsigned top=mono?34:pal?63:34;
    const unsigned start=mono?0:pal?56:52;
    const unsigned width=mono?160:320;
    const unsigned bytes=width/2;
    const unsigned base=0x12ff00;
    t.dut.screen_base=base|0xf3; // Low byte is ignored on an STF.
    while(t.dut.display_line<top-1) t.tick();
    while(t.dut.display_line<top) {
        t.require(t.dut.video_counter==0,"top blanking holds the initial counter");
        t.tick();
    }
    bool saw_negative=false;
    unsigned calibration_values=0;
    while(t.dut.display_line<top+4) {
        const unsigned row=t.dut.display_line-top;
        const int progress=int(t.dut.display_phase)-8-int(start);
        const unsigned elapsed=progress<0?0:unsigned(progress)>width?width:unsigned(progress);
        const unsigned expected=base+row*bytes+(elapsed/4)*2;
        t.require(t.dut.video_counter==expected,
                  "shifter counter exposes word progress at the documented read phase");
        const unsigned low=expected&255;
        if(low&128) saw_negative=true;
        else if(saw_negative && low<=8) calibration_values|=1u<<(low/2);
        t.tick();
    }
    t.require(calibration_values==31,"polling after a negative byte sees 0/2/4/6/8");
    t.require(t.dut.video_counter==base+4*bytes,"four rows carry into the high address byte");
    const unsigned stop=top+(mono?400:200);
    while(t.dut.display_line<stop) t.tick();
    const unsigned final=base+(mono?400:200)*bytes;
    while(t.frames==0) {
        t.require(t.dut.video_counter==final,"bottom blanking holds the final DMA address");
        t.tick();
    }
    t.require(t.dut.video_counter==base,"VBL reloads all counter bytes from aligned base");
}

static void brief_mode_writes(Test &t) {
    t.reset();
    const uint64_t start=t.cpu_ticks;
    t.mwrite(0x21,255); t.mwrite(0x1b,8);
    while(t.frames<1) {
        // Pulse both video settings inside every ordinary active line, away
        // from HBL/VBL. A register write must not generate a second DE edge.
        if(t.dut.timer_b_level && t.dut.display_phase>=170 && t.dut.display_phase<180) {
            t.dut.sync_mode=0; t.dut.resolution=2;
            t.run(3);
            t.dut.sync_mode=2; t.dut.resolution=0;
        }
        t.tick();
    }
    t.require(t.display_ends==200,"brief mid-line mode writes retain 200 display edges");
    t.require(t.mread(0x21)==55,"brief mode writes do not add Timer B events");
    t.require(t.cpu_ticks-start>=160256-2 && t.cpu_ticks-start<160256+20,
              "brief mode writes retain the ordinary PAL frame period");
}

static void timer_b_polarity(Test &t,bool rising) {
    t.reset();
    t.mwrite(3,rising?8:0); t.mwrite(0x21,255); t.mwrite(0x1b,8);
    // Primary ST timing: rising at 56+24, falling at 376+24.
    // Inspect the real MFP count on both sides, not just the wrapper pin.
    const unsigned edge=rising?80:400;
    // Leave room for the register transaction to finish before the edge.
    while(t.dut.display_line<63 || t.dut.display_phase<edge-8) t.tick();
    t.require(t.mread(0x21)==255,"Timer B does not count the undelayed video edge");
    while(t.dut.display_phase<edge+4) t.tick();
    t.require(t.mread(0x21)==254,"AER selects the delayed start or end of display");
}

static void bottom_border(Test &t,bool pal,bool cross_sample,bool next_line=false) {
    t.reset(0,pal?2:0);
    const uint64_t frame_start=t.cpu_ticks;
    t.mwrite(0x21,255); t.mwrite(0x1b,8);
    while(t.dut.display_line!=(pal?262u:233u) || t.dut.display_phase<490) t.tick();
    t.dut.sync_mode=pal?0:2;
    if(next_line) {
        // BIG restores on the following line at cycle 16/20, not before HBL.
        while(t.dut.display_line!=(pal?263u:234u) || t.dut.display_phase<20) t.tick();
    } else {
        while(t.dut.display_phase<(cross_sample?504u:500u)) t.tick();
    }
    t.dut.sync_mode=pal?2:0;
    // VIDEO_HEIGHT_BOTTOM_50HZ=47 and VIDEO_HEIGHT_BOTTOM_60HZ=26.
    const unsigned active=200+(cross_sample?(pal?47:26):0);
    const unsigned last_active=(pal?63:34)+active-1;
    while(t.dut.display_line<last_active || t.dut.display_phase<400) t.tick();
    t.require(t.dut.video_counter==0x10200+active*160,
              "counter includes every DMA word in opened bottom lines");
    while(t.frames<1) t.tick();
    const uint64_t next_frame=t.cpu_ticks;
    t.require(t.cpu_ticks-frame_start==(pal?160256u:133604u),
              "bottom sync pulse preserves the exact native frame length");
    t.require(t.lines==(pal?313u:263u),"bottom sync pulse retains every native HBL");
    t.require(t.display_ends==active,"bottom opening requires the opposite mode at the stop sample");
    t.require(t.mread(0x21)==255-active,"Timer B sees bottom-border DE lines");
    const unsigned prior=t.display_ends;
    while(t.frames<2) t.tick();
    t.require(t.display_ends-prior==200,"bottom opening clears at the next frame");
    t.require(t.cpu_ticks-next_frame==(pal?160256u:133604u),
              "frame after bottom opening retains its exact native period");
}

int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    Test test;
    lanes_and_reset(test); interrupt_connection(test);
    vbl_interrupt_phase(test,0,2); vbl_interrupt_phase(test,0,0);
    vbl_interrupt_phase(test,2,2); timer_c_rate(test);
    display_enable(test,0,2,50,200); display_enable(test,1,0,60,200);
    display_enable(test,2,2,71,400); brief_mode_writes(test);
    shifter_counter(test,0,2); shifter_counter(test,1,0); shifter_counter(test,2,2);
    timer_b_polarity(test,false); timer_b_polarity(test,true);
    bottom_border(test,true,false); bottom_border(test,true,true);
    bottom_border(test,false,false); bottom_border(test,false,true);
    bottom_border(test,true,true,true); bottom_border(test,false,true,true);
    std::cout<<"ST I/O: "<<test.assertions<<" assertions, "<<test.cycles
             <<" cycles; byte lanes, reset, interrupt wiring and timer rates passed\n";
}
