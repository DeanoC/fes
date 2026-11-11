// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_menu_reader.h"
#include "verilated.h"
#include <cstdint>
#include <deque>
#include <iostream>
#include <stdexcept>

static void check(bool ok, const char* message) {
    if (!ok) throw std::runtime_error(message);
}

struct Beat { uint32_t index; unsigned slot; };
struct Bench {
    Vfes_menu_reader top;
    std::deque<Beat> responses;
    uint64_t cycle = 0;
    unsigned accepted = 0, pixels = 0;
    bool command_held = false;
    uint32_t held_address = 0;
    unsigned held_burst = 0;
    bool cancelling = false;
    bool varied = false, block_commands = false;
    static uint32_t pattern(unsigned slot, unsigned pixel) {
        return (slot ? 0x5a000000u : 0xa5000000u) | pixel;
    }
    void tick() {
        top.clk = 0;
        top.waitrequest = block_commands || (varied && cycle % 11 < 4);
        top.pixel_ready = !varied || cycle % 13 > 2;
        top.readdatavalid = !responses.empty() && (!varied || cycle % 17 > 4);
        if (top.readdatavalid) {
            auto b = responses.front();
            for (unsigned lane = 0; lane < 4; ++lane)
                top.readdata[lane] = pattern(b.slot, b.index * 4 + lane);
        }
        top.eval();
        if (command_held) {
            check(top.read && top.address == held_address && top.burstcount == held_burst,
                  "command changed under waitrequest");
        }
        command_held = top.read && top.waitrequest;
        held_address = top.address; held_burst = top.burstcount;
        if (top.pixel_valid && top.pixel_ready) {
            check(!cancelling, "cancelled frame emitted pixels");
            check(top.pixel_index == pixels, "pixel index is not ordered");
            check(top.pixel_bgrx == pattern(top.slot, pixels), "pixel data/order mismatch");
            ++pixels;
        }
        bool returning = top.readdatavalid;
        if (top.read && !top.waitrequest) {
            check(responses.empty() || returning && responses.size() == 1,
                  "more than one outstanding burst");
            uint32_t byte = top.address * 16u;
            uint32_t base = top.slot ? 0x30400000u : 0x30000000u;
            check(top.burstcount >= 1 && top.burstcount <= 128, "invalid burst count");
            check(byte >= base && uint64_t(byte) + top.burstcount * 16u <= uint64_t(base) + 3686400,
                  "burst crossed frame bounds");
            unsigned index = (byte - base) / 16;
            check(index == accepted, "DDR address skipped or repeated");
            for (unsigned i = 0; i < top.burstcount; ++i)
                responses.push_back({index + i, top.slot});
            accepted += top.burstcount;
        }
        if (returning) responses.pop_front();
        top.clk = 1; top.eval();
        ++cycle;
    }
    void start(unsigned slot) {
        top.slot = slot; top.enable = 1; top.start = 1;
        tick(); top.start = 0;
    }
    void frame(unsigned slot, bool stalls) {
        varied = stalls; accepted = pixels = 0; cancelling = false;
        start(slot);
        for (unsigned n = 0; n < 4000000 && !top.done; ++n) tick();
        check(top.done, "frame timed out");
        check(pixels == 921600 && accepted == 230400, "frame count mismatch");
        tick(); check(top.idle && responses.empty(), "frame did not become idle");
    }
    void held_cancel(bool reset) {
        accepted = pixels = 0; cancelling = false; block_commands = true;
        start(0);
        for(unsigned i=0;i<10 && !top.read;++i) tick();
        check(top.read,"no held command for cancellation test");
        cancelling=true;
        if(reset)top.rst=1;else top.enable=0;
        for(unsigned i=0;i<10;++i){tick();check(top.read && !top.idle,"held command withdrawn on cancel");}
        block_commands=false;
        for(unsigned i=0;i<1000&&!top.idle;++i)tick();
        check(top.idle&&responses.empty(),"held cancel did not drain");
        top.rst=0;top.enable=1;cancelling=false;tick();frame(1,true);
    }
    void cancel(bool reset) {
        varied = true; accepted = pixels = 0;
        start(0);
        while (responses.empty()) tick();
        cancelling = true;
        if (reset) top.rst = 1; else top.enable = 0;
        for (unsigned n = 0; n < 1000 && !top.idle; ++n) tick();
        check(top.idle && responses.empty(), "cancel did not drain outstanding reads");
        top.rst = 0; top.enable = 1; cancelling = false;
        tick(); frame(1, true);
    }
};
int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    try {
        Bench b;
        b.top.enable = 0; b.top.start = 0; b.top.rst = 1; b.tick();
        b.top.rst = 0; b.tick();
        b.frame(0, false); std::cout << "PASS exact_frame/last_burst\n";
        b.frame(1, true); std::cout << "PASS stalled_command/delayed_response\n";
        b.cancel(false); b.cancel(true); b.held_cancel(false); b.held_cancel(true); std::cout << "PASS disable/reset_drain\n";
    } catch (const std::exception& e) { std::cerr << e.what() << '\n'; return 1; }
}
