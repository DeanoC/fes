// SPDX-License-Identifier: GPL-2.0-or-later
// Actual reader RTL: accepted plus held credit, full-command reservation,
// cancellation/drain, FIFO capacity and same-edge first responses.
#include "Vfes_menu_reader.h"
#include "verilated.h"
#include <cstdint>
#include <deque>
#include <iostream>
#include <stdexcept>

#ifndef READER_CREDIT_LIMIT
#define READER_CREDIT_LIMIT 256
#endif
static constexpr unsigned Limit = READER_CREDIT_LIMIT;
static constexpr unsigned Burst = 64;
static void check(bool ok, const char* why) {
    if (!ok) throw std::runtime_error(why);
}
struct Beat { unsigned index, slot; };
struct Bench {
    Vfes_menu_reader top;
    std::deque<Beat> queue;
    unsigned accepted = 0, pixels = 0, zero_latency_responses = 0;
    bool blocked = false, respond = false, consume = false, zero_latency = false;
    bool held = false;
    uint32_t held_address = 0;
    unsigned held_burst = 0;
    static uint32_t pattern(unsigned slot, unsigned pixel) {
        return (slot ? 0x5a000000u : 0xa5000000u) | pixel;
    }
    void tick() {
        top.clk = 0; top.waitrequest = blocked; top.pixel_ready = consume;
        top.readdatavalid = 0; top.response = 0; top.eval();
        const bool accepts = top.read && !top.waitrequest;
        const uint32_t byte = top.address * 16u;
        const uint32_t base = top.slot ? 0x30400000u : 0x30000000u;
        const unsigned count = top.burstcount;
        if (held)
            check(top.read && top.address == held_address && count == held_burst,
                  "held command changed on wait/cancellation");
        held = top.read && top.waitrequest;
        held_address = top.address; held_burst = count;
        const bool immediate = respond && queue.empty() && zero_latency && accepts;
        const bool returns = respond && (!queue.empty() || immediate);
        if (returns) {
            const Beat beat = immediate ? Beat{(byte-base)/16u, unsigned(top.slot)} : queue.front();
            for (unsigned lane=0; lane<4; ++lane)
                top.readdata[lane] = pattern(beat.slot, beat.index*4+lane);
            top.readdatavalid = 1;
            if (immediate) ++zero_latency_responses;
        }
        top.eval();
        if (top.pixel_valid && top.pixel_ready) {
            check(top.pixel_index == pixels, "pixel index skipped/repeated");
            check(top.pixel_bgrx == pattern(top.slot, pixels), "pixel contents/order changed");
            ++pixels;
        }
        if (accepts) {
            check(count == Burst, "unexpected command size");
            check((top.address & 255u)+count <= 256, "command crosses4KiB boundary");
            check(byte >= base && uint64_t(byte)+count*16u <= uint64_t(base)+3686400,
                  "command crosses frame");
            const unsigned index=(byte-base)/16u;
            check(index == accepted, "accepted addresses skipped/repeated");
            check(queue.size()+count <= Limit, "accepted command exceeds full credit reservation");
            for (unsigned i=0;i<count;++i) queue.push_back({index+i,unsigned(top.slot)});
            accepted += count;
        }
        if (returns) queue.pop_front();
        check(top.memory_errors == 0, "legitimate response faulted before acceptance edge");
        top.clk=1;top.eval();
        // Retire this edge's response before checking registered error state;
        // keeping valid high would describe a second response next cycle.
        top.readdatavalid=0;top.eval();
        check(top.memory_errors == 0, "legitimate response faulted");
        check(top.diagnostic_accepted_outstanding == queue.size(), "accepted-response accounting mismatch");
        check(top.outstanding_beats <= Limit, "accepted plus held credit wrapped/exceeded limit");
        check(unsigned(top.diagnostic_fifo_count)+unsigned(top.outstanding_beats) <= 2048,
              "FIFO plus accepted/held reservations exceed capacity");
    }
    Bench() { top.enable=0;top.start=0;top.rst=1;tick();top.rst=0;tick(); }
    void start(unsigned slot=0) {
        accepted=pixels=0;top.slot=slot;top.enable=1;top.start=1;tick();top.start=0;
    }
    void bounded_held_cancel(bool reset) {
        start();
        for(unsigned i=0;i<40;++i)tick();
        const unsigned initial=(Limit/Burst)*Burst;
        check(queue.size()==initial && !top.read, "did not stop at last legal whole command");
        for(unsigned i=0;i<8;++i)tick();
        check(queue.size()==initial && !top.read,"credit blocked command was issued");
        // At511, one return permits the next64-beat command exactly at511.
        // At256, a whole64 must return before another64 can be reserved.
        blocked=true;respond=true;
        const unsigned release=initial+Burst-Limit;
        for(unsigned i=0;i<release;++i)tick();
        respond=false;
        for(unsigned i=0;i<3 && !top.read;++i)tick();
        check(top.read && top.outstanding_beats==Limit,"did not reserve exact configured bound");
        check(top.diagnostic_accepted_outstanding==Limit-Burst,"held command counted as accepted");
        if(reset)top.rst=1;else top.enable=0;
        for(unsigned i=0;i<8;++i){tick();check(top.read && !top.idle,"cancel withdrew held traffic");}
        blocked=false;tick();
        check(queue.size()==Limit && top.diagnostic_accepted_outstanding==Limit &&
              top.outstanding_beats==Limit,"exact accepted credit bound wrapped/lost held command");
        respond=true;
        for(unsigned i=0;i<Limit+10 && !top.idle;++i)tick();
        check(top.idle && queue.empty() && !top.pixel_valid,"cancel failed to drain without pixels");
    }
    void fifo_reservation() {
        respond=true;zero_latency=true;start();
        for(unsigned i=0;i<2300;++i)tick();
        check(top.diagnostic_fifo_count==2048 && queue.empty() && !top.read,
              "full FIFO accepted another entire command");
        top.enable=0;tick();check(top.idle,"FIFO cancel failed");
    }
    void exact_zero_latency_frame() {
        respond=true;consume=true;zero_latency=true;start(1);
        for(unsigned i=0;i<1100000 && !top.done;++i)tick();
        check(top.done && pixels==921600 && accepted==230400,"zero-latency frame incomplete");
        tick();check(top.idle && queue.empty(),"completed frame did not retire");
        check(zero_latency_responses>0,"same-edge first response was not exercised");
    }
};
int main(int argc,char**argv) {
    Verilated::commandArgs(argc,argv);
    try {
        {Bench b;b.bounded_held_cancel(false);} std::cout<<"PASS credit"<<Limit<<"/held-disable-drain\n";
        {Bench b;b.bounded_held_cancel(true);} std::cout<<"PASS credit"<<Limit<<"/held-reset-drain\n";
        {Bench b;b.fifo_reservation();} std::cout<<"PASS credit"<<Limit<<"/FIFO-full-command-reservation\n";
        {Bench b;b.exact_zero_latency_frame();} std::cout<<"PASS credit"<<Limit<<"/zero-latency-exact-frame\n";
    }catch(const std::exception&e){std::cerr<<e.what()<<'\n';return 1;}
}
