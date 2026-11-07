// Actual captured vendor I2C bus, with an open-drain slave and level IRQ.
#include "Vhdmi_control_model.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <iostream>
#include <stdexcept>
#include <vector>

static void check(bool value, const char *message) {
    if (!value) throw std::runtime_error(message);
}
static const std::array<unsigned,33> table = {
    0x9803,0x0100,0x0218,0x0300,0x0b2e,0x0cbc,0x1472,0x1520,
    0x1630,0x1846,0x4080,0x4110,0x49a8,0x5510,0x5608,0x96f6,
    0x7307,0x761f,0x9803,0x9902,0x9ae0,0x9c30,0x9d61,0xa2a4,
    0xa3a4,0xa504,0xab40,0xaf16,0xba60,0xd1ff,0xde10,0xe460,0xfa7d
};

struct Bus {
    Vhdmi_control_model d;
    uint64_t ticks=0;
    unsigned starts=0, stops=0, accepted=0, rejected=0, index=0;
    unsigned bit=0, byte=0;
    bool active=false, bad_ack=false, injected=false;
    std::vector<unsigned> bytes;
    Bus() { d.clk=0; d.reset=1; d.raw_irq=1; d.ack_low=0; d.eval(); }
    void half() {
        bool old_scl=d.scl, old_sda=d.sda;
        d.clk=!d.clk; d.eval(); ++ticks;
        if (d.reset) return;
        bool scl=d.scl, sda=d.sda;
        if (scl && old_scl && old_sda && !sda) {
            if (active) throw std::runtime_error("unexpected repeated START tick="+std::to_string(ticks)+" phase="+std::to_string(d.phase)+" bit="+std::to_string(bit)+" bytes="+std::to_string(bytes.size())+" accepted="+std::to_string(accepted));
            active=true; bit=0; byte=0; bad_ack=false; bytes.clear(); ++starts;
        }
        if (scl && old_scl && !old_sda && sda && active) {
            if (bytes.size()!=3 || bit!=0) throw std::runtime_error("STOP before three complete bytes/ACKs tick="+std::to_string(ticks)+" phase="+std::to_string(d.phase)+" bit="+std::to_string(bit)+" bytes="+std::to_string(bytes.size())+" accepted="+std::to_string(accepted)+" starts="+std::to_string(starts));
            check(bytes[0]==0x72,"wrong ADV7513 slave address");
            if (bad_ack) { ++rejected; }
            else {
                check(((bytes[1]<<8)|bytes[2])==table[index],"configuration write order or data differs");
                index=(index+1)%table.size(); ++accepted;
            }
            ++stops; active=false;
        }
        if (active && !old_scl && scl && (bytes.size()<3 || bit!=0)) {
            if (bit<8) {
                byte=(byte<<1)|unsigned(sda); ++bit;
                if (bit==8) bytes.push_back(byte);
            } else {
                bad_ack|=sda; bit=0; byte=0;
            }
        }
        if (active && old_scl && !scl) {
            if (bit==8) {
                // NACK exactly the first address. The actual vendor must retry
                // the same LUT entry, with no skipped accepted write.
                bool nack=!injected && bytes.size()==1;
                if (nack) injected=true;
                d.ack_low=!nack;
            } else if (bit==0) d.ack_low=0;
            d.eval();
        }
    }
    template<class Predicate> void until(Predicate done) {
        uint64_t deadline=ticks+30000000;
        while (!done()) { check(ticks<deadline,"bounded HDMI bus deadline"); half(); }
    }
    void clocks(unsigned count) { for (unsigned i=0;i<2*count;++i) half(); }
    void rising() { if (d.clk) half(); half(); }
    void qualify_irq() {
        if (d.clk) half();
        check(d.irq_seen,"idle interrupt not high");
        d.raw_irq=0; d.eval();
        check(d.irq_seen,"raw IRQ reached the consumer without a clock");
        rising(); check(d.irq_seen,"IRQ bypassed its second stage");
        rising(); check(!d.irq_seen,"IRQ did not reach consumer after two edges");
        d.raw_irq=1; d.eval(); check(!d.irq_seen,"raw IRQ release reached consumer asynchronously");
        rising(); check(!d.irq_seen,"IRQ release bypassed second stage");
        rising(); check(d.irq_seen,"IRQ release did not qualify");
    }
    void assert_reset() {
        d.reset=1; d.ack_low=0; d.eval();
        check(d.scl && d.sda && d.irq_seen,"stopped-clock reset did not release I2C and idle IRQ");
        active=false; bit=0; byte=0; bytes.clear(); index=0;
    }
};

int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    try {
        Bus b;
        b.clocks(4); b.d.reset=0; b.clocks(4);
        b.qualify_irq();
        b.until([&]{return b.accepted==33 && b.d.ready;});
        check(b.rejected==1 && b.stops==34,"NACK was not retried exactly once");
        b.clocks(10004);
        unsigned starts=b.starts;
        b.d.raw_irq=0; b.d.eval();
        check(b.d.irq_seen,"live raw IRQ reached consumer asynchronously");
        b.until([&]{return b.starts>starts;});
        b.d.raw_irq=1;
        b.until([&]{return b.accepted==66 && b.d.ready;});
        // Force reset during another active transaction with the reference
        // clock parked. No completed write or ACK is invented by reset.
        starts=b.starts; b.d.raw_irq=0;
        b.until([&]{return b.starts>starts && !b.d.scl && b.bit>0;});
        unsigned accepted=b.accepted;
        b.assert_reset(); check(b.accepted==accepted,"reset invented a completed write");
        b.clocks(5); b.d.raw_irq=1; b.d.reset=0;
        b.until([&]{return b.accepted==99 && b.d.ready;});
        check(b.rejected==1 && b.index==0,"reset/reinitialization changed accepted table");
        std::cout << "PASS HDMI control accepted="<<b.accepted<<" nacks="<<b.rejected
                  <<" starts="<<b.starts<<" stops="<<b.stops<<" half_ticks="<<b.ticks
                  <<" startup retry IRQ_reinit stopped_clock_reset\n";
        return 0;
    } catch (const std::exception &e) {
        std::cerr<<"HDMI control failure: "<<e.what()<<"\n";
        return 1;
    }
}
