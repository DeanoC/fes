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
static constexpr unsigned cpu_hz = 8'021'247;
static constexpr unsigned mfp_base = 0xfffa00;
struct Test {
    Vst_io_sim_top dut;
    uint64_t cycles=0, assertions=0, crystal_ticks=0, psg_ticks=0, serial_ticks=0;
    unsigned psg_writes=0;
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
        dut.cpu_data_strobe=dut.req && (dut.byte_enable&1);
        dut.cpu_cycle_ce=!dut.reset && cpu_phase+cpu_hz>=system_hz;
        cpu_phase=dut.reset?0:(cpu_phase+cpu_hz)%system_hz;
        cpu_ticks+=dut.cpu_cycle_ce;
        dut.clk=0; dut.eval();
        if (!dut.reset) {
            psg_writes+=dut.psg_write_accept;
            frames+=dut.vblank; lines+=dut.hblank;
            crystal_ticks+=dut.timer_ce_level;
            psg_ticks+=dut.psg_ce_level; serial_ticks+=dut.serial_ce_level;
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
        // Early write ACK is combinational; keep the request through its
        // accepting edge, as the motherboard does before withdrawing it.
        if (write) {
            tick(); require(dut.ack,"write acknowledgement survives accepting edge");
        }
        const uint16_t result=dut.rdata;
        for (unsigned k=1;k<hold;++k) {
            tick(); require(dut.ack && (write || dut.rdata==result),"held bus retains one acknowledgement/read value");
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

int main(int argc, char **argv) {
    Verilated::commandArgs(argc,argv);
    Test t;
    // A burst contains four admitted accesses. The fourth must stay complete
    // until withdrawal, and the fifth starts a fresh four-clock wait.
    for (unsigned kind=0;kind<3;++kind) {
        t.reset();
        t.bus(true,0xff8800,7<<8,2);
        t.bus(true,0xff8802,255<<8,2);
        t.bus(true,0xff8800,8<<8,2);
        unsigned writes=t.psg_writes;
        t.bus(kind!=1,kind==1?0xff8800:0xff8802,10<<8,kind==2?1:2,100);
        t.require(t.psg_writes-writes==(kind==0?1:0),
                  "held fourth access has exactly one write, read/low lane have none");
        uint64_t start=t.cpu_ticks;
        t.bus(true,0xff8800,8<<8,2);
        t.require(t.cpu_ticks-start>=4,"fifth access starts fresh PSG wait");
        t.require((t.bus(false,0xff8800,0,2,100)>>8)==(kind==0?10:0),
                  "held read returns the selected volume without lane corruption");
    }
    std::cout<<"ST PSG burst waits: "<<t.assertions<<" assertions passed\n";
}
