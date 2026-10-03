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

static constexpr unsigned system_hz = 5'222'400;
static constexpr unsigned mfp_base = 0xfffa00;
struct Test {
    Vst_io_sim_top dut;
    uint64_t cycles=0, assertions=0, crystal_ticks=0;
    unsigned frames=0, lines=0, display_ends=0;
    bool previous_display=false;
    void require(bool good,const std::string &message) {
        ++assertions;
        if (!good) {
            std::cerr<<"ST I/O cycle "<<cycles<<": "<<message<<'\n';
            std::exit(1);
        }
    }
    void tick() {
        dut.clk=0; dut.eval();
        if (!dut.reset) {
            frames+=dut.vblank; lines+=dut.hblank;
            crystal_ticks+=dut.timer_ce_level;
            display_ends+=previous_display && !dut.timer_b_level;
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
    }
    uint16_t bus(bool write,unsigned address,unsigned data=0,unsigned lanes=3,unsigned hold=1) {
        dut.req=1; dut.write=write; dut.addr=address>>1;
        dut.wdata=data; dut.byte_enable=lanes;
        tick(); require(dut.selected,"peripheral address selection");
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
    t.ymwrite(8,15); t.run(200);
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
    for (unsigned n=0;t.dut.irq!=6;++n) { t.require(n<27000,"Timer C interrupt timeout"); t.tick(); }
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
    t.require(t.dut.irq==4 && !t.dut.irq_vectored,"native VBL is autovectored level 4");
    t.iack(4); t.require(t.dut.irq==2,"VBL acknowledgement leaves native HBL pending");
    t.iack(2); t.require(t.dut.irq==0,"native HBL acknowledgement clears level 2");
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
        for (unsigned n=0;t.dut.irq!=6;++n) { t.require(n<27000,"periodic Timer C timeout"); t.tick(); }
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
    uint64_t previous_frame=t.cycles;
    t.require(t.display_ends==active,"one timer-B event per active native line");
    if (active==200) t.require(t.mread(0x21)==55,"Timer B receives the 200 active color lines through its event pin");
    unsigned previous_ends=t.display_ends;
    for (unsigned frame=0;frame<3;++frame) {
        const unsigned target=t.frames+1;
        while (t.frames<target) t.tick();
        t.require(t.display_ends-previous_ends==active,"blank lines never create Timer B events");
        const uint64_t clocks=t.cycles-previous_frame;
        t.require(clocks==system_hz/fps || clocks==(system_hz+fps-1)/fps,"native VBL frequency follows mode/sync");
        t.require(t.dut.video_counter==0x10200,"VBL reloads the aligned shifter counter");
        previous_ends=t.display_ends; previous_frame=t.cycles;
    }
    t.mwrite(0x1b,0);
    const unsigned expected=255-(4*active)%255;
    t.require(t.mread(0x21)==expected,"Timer B counts all active display-enable edges, including reload");
    std::cout<<"ST I/O: "<<fps<<" Hz, "<<active<<" Timer B events/frame\n";
}

int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    Test test;
    lanes_and_reset(test); interrupt_connection(test); timer_c_rate(test);
    display_enable(test,0,2,50,200); display_enable(test,1,0,60,200);
    display_enable(test,2,2,71,400);
    std::cout<<"ST I/O: "<<test.assertions<<" assertions, "<<test.cycles
             <<" cycles; byte lanes, reset, interrupt wiring and timer rates passed\n";
}
