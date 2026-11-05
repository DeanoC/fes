// SPDX-License-Identifier: MIT
// pc386_ddr_model.h: a refused command must hold, and a held port is its
// guard's (#1372). Exit 0 when every case passes.
#include "pc386_ddr_model.h"

#include <cstdint>
#include <cstdio>
#include <stdexcept>
#include <string>

namespace {

struct Memory {
    uint8_t bytes[16]{};
    uint8_t *at(uint32_t, bool) { return bytes; }
};

struct Top {
    bool ddr_read = false, ddr_write = true;
    uint32_t ddr_address = (pc386::WINDOW_BASE + pc386::GUEST_OFFSET) / 16;
    uint8_t ddr_burstcount = 1, ddr_waitrequest = false, ddr_readdatavalid = false;
    uint16_t ddr_byteenable = 0x0100;
    uint32_t ddr_readdata[4]{}, ddr_writedata[4]{};
};

// A 64-bit port's master, driven by hand.
struct Master {
    uint32_t address = 0;
    uint8_t burstcount = 0, read = 0, write = 0;
    uint64_t writedata = 0;
    uint8_t byteenable = 0xff, waitrequest = 0;
    uint64_t readdata = 0;
    uint8_t readdatavalid = 0;
    pc386::PortSignals signals() {
        return {&address, &burstcount, &read, &write, &writedata, &byteenable, &waitrequest, &readdata,
                &readdatavalid};
    }
};

constexpr uint32_t CAPTURE_WORD = (pc386::WINDOW_BASE + pc386::CAPTURE_OFFSET) / 8;

bool fail(const std::string &what) {
    std::fprintf(stderr, "%s\n", what.c_str());
    return false;
}

// A stalled command must preserve port 0's upper byte-enable lanes too.
bool port0_refusal_holds_every_lane() {
    for (uint64_t seed = 1; seed <= 4096; ++seed) {
        Memory memory;
        pc386::Port0<Memory> port(memory, seed, "test port 0");
        Top top;
        port.drive(top, 0);
        if (!top.ddr_waitrequest) continue;
        top.ddr_byteenable = 0x0200;
        try {
            port.drive(top, 1);
        } catch (const std::runtime_error &error) {
            if (std::string(error.what()).find("changed or withdrawn under waitrequest") != std::string::npos)
                return true;
            return fail(std::string("unexpected model error: ") + error.what());
        }
        return fail("upper byte-enable change was not rejected");
    }
    return fail("no deterministic waitrequest seed found");
}

// Beats [from, from + count) of a burst at address, each beat's bytes its
// number plus one, through the port's refusals.
void write_beats(pc386::Port &port, Master &master, uint64_t &cycle, uint32_t address, unsigned from,
                 unsigned count) {
    master.write = 1;
    master.address = address;
    master.burstcount = 16;
    for (unsigned beat = from; beat < from + count;) {
        master.writedata = 0x0101010101010101ull * (beat + 1);
        port.drive(master.signals(), cycle++);
        if (!master.waitrequest) ++beat;
    }
    master.write = 0;
}

// A hold four beats into a 16-beat burst: the guard sends the other twelve
// with byte enables clear, so they write nothing and the next burst begins
// cleanly.
bool port_hold_finishes_a_write_burst() {
    pc386::Window window;
    pc386::Port port(window, 1, "test port 2");
    Master master;
    uint64_t cycle = 0;
    write_beats(port, master, cycle, CAPTURE_WORD, 0, 4);
    for (int n = 0; n < 4; ++n) {
        port.hold(master.signals());
        if (!master.waitrequest || master.readdatavalid) return fail("a held port answered its master");
    }
    if (port.beats_owed()) return fail("the hold left the burst open");
    try {
        write_beats(port, master, cycle, CAPTURE_WORD + 16, 0, 16);
    } catch (const std::runtime_error &error) {
        return fail(std::string("the burst after the hold: ") + error.what());
    }
    for (unsigned beat = 0; beat < 32; ++beat) {
        unsigned want = beat < 4 ? beat + 1 : beat < 16 ? 0 : beat - 15;
        if (window.byte(pc386::CAPTURE_OFFSET + 8 * beat) != want)
            return fail("beat " + std::to_string(beat) + " holds " +
                        std::to_string(window.byte(pc386::CAPTURE_OFFSET + 8 * beat)) + ", not " +
                        std::to_string(want));
    }
    return true;
}

// The data of a read issued before the hold never reaches the master.
bool port_hold_hides_read_data() {
    pc386::Window window;
    pc386::Port port(window, 1, "test port 2");
    Master master;
    master.read = 1;
    master.address = CAPTURE_WORD;
    master.burstcount = 4;
    uint64_t cycle = 0;
    do port.drive(master.signals(), cycle++);
    while (master.waitrequest);
    master.read = 0;
    port.hold(master.signals());
    for (int n = 0; n < 200; ++n) {
        port.drive(master.signals(), cycle++);
        if (master.readdatavalid) return fail("read data from before the hold reached the master");
    }
    return port.idle() || fail("the read before the hold is still pending");
}

// A command the port refused as the hold came never reached the guard: the
// master's reset withdraws it.
bool port_hold_withdraws_a_refused_command() {
    for (uint64_t seed = 1; seed <= 4096; ++seed) {
        pc386::Window window;
        pc386::Port port(window, seed, "test port 2");
        Master master;
        master.write = 1;
        master.address = CAPTURE_WORD;
        master.burstcount = 2;
        port.drive(master.signals(), 0);
        if (!master.waitrequest) continue;
        master.write = 0;
        port.hold(master.signals());
        try {
            port.drive(master.signals(), 2);
        } catch (const std::runtime_error &error) {
            return fail(std::string("after the hold: ") + error.what());
        }
        return port.idle() || fail("the refused command was taken");
    }
    return fail("no deterministic waitrequest seed found");
}

// Port 0's hold, one beat into a 4-beat burst.
bool port0_hold_finishes_a_write_burst() {
    Memory memory;
    pc386::Port0<Memory> port(memory, 1, "test port 0");
    Top top;
    top.ddr_burstcount = 4;
    uint64_t cycle = 0;
    do port.drive(top, cycle++);
    while (top.ddr_waitrequest);
    top.ddr_write = false;
    port.hold(top);
    if (!top.ddr_waitrequest || port.beats_owed()) return fail("port 0's hold left the burst open");
    top.ddr_write = true;
    top.ddr_address += 4;
    try {
        for (unsigned beat = 0; beat < 4;) {
            port.drive(top, cycle++);
            if (!top.ddr_waitrequest) ++beat;
        }
    } catch (const std::runtime_error &error) {
        return fail(std::string("port 0's burst after the hold: ") + error.what());
    }
    return port.idle() || fail("port 0's burst after the hold is still open");
}

} // namespace

int main() {
    bool ok = true;
    for (bool (*test)() : {port0_refusal_holds_every_lane, port_hold_finishes_a_write_burst, port_hold_hides_read_data,
                           port_hold_withdraws_a_refused_command, port0_hold_finishes_a_write_burst}) {
        try {
            ok = test() && ok;
        } catch (const std::runtime_error &error) {
            ok = fail(std::string("model error: ") + error.what());
        }
    }
    return ok ? 0 : 1;
}
