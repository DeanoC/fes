// SPDX-License-Identifier: MIT
// HPS DDR window and Avalon-MM port models for the pc386 and i386 benches:
// Port, a 64-bit port (1 or 2), and Port0, the 128-bit port 0.
//
// Port's read latency follows the Kit B measurement with Linux idle
// (https://github.com/DeanoC/fes/pull/1076, port 1 at 100 MHz): first data
// 17-18 clocks after the command for most reads, 32-51 for about 1.5% and
// 54-79 for 0.1%. Port0's follows the Kit A measurement of port 0 with Linux
// idle (same PR): 18-19 clocks for most reads, 47-51 for the slowest 1%, and
// a tail to the worst seen, 135. Beats of a burst then come one a clock with
// an occasional gap. A port refuses a command at random about one clock in
// sixteen and while 14 reads are pending, as fes-menu's controller model
// does. Every draw comes from a seeded generator, consumed once per clock, so
// two benches that drive a port identically see identical timing. The clock
// counts are 100 MHz ones: at a slower port clock they overstate latency.
//
// Port holds its masters to Avalon-MM, as the HPS controller needs: a
// command it refused comes again unchanged on the next clock, no read starts
// inside a write burst, and every beat of a write burst carries its first
// beat's address and length, so a burst cut short and followed by another
// is caught at the new burst's first beat.
#pragma once

#include <array>
#include <cstdint>
#include <cstring>
#include <deque>
#include <memory>
#include <stdexcept>
#include <string>
#include <vector>

namespace pc386 {

constexpr uint32_t WINDOW_BASE = 0x30000000u;
constexpr uint32_t WINDOW_BYTES = 0x10000000u;
constexpr uint32_t VRAM_OFFSET = 0x00800000u;
constexpr uint32_t CAPTURE_OFFSET = 0x00a00000u;
constexpr uint32_t CAPTURE_BYTES = 0x00200000u;
constexpr uint32_t CAPTURE_STRIDE = 3200u;
constexpr uint32_t GUEST_OFFSET = 0x04000000u;  // guest RAM: physical 0 at this window offset

// The 256 MiB window, allocated a MiB at a time as it is touched.
class Window {
public:
    uint8_t *at(uint32_t offset) {
        if (offset >= WINDOW_BYTES) throw std::runtime_error("DDR access outside the window");
        auto &chunk = chunks[offset >> 20];
        if (!chunk) {
            chunk = std::make_unique<std::array<uint8_t, 1u << 20>>();
            chunk->fill(0);
        }
        return chunk->data() + (offset & 0xfffff);
    }
    uint8_t byte(uint32_t offset) { return *at(offset); }

private:
    std::array<std::unique_ptr<std::array<uint8_t, 1u << 20>>, 256> chunks;
};

// xorshift64*: small, fast and the same everywhere.
class Random {
public:
    explicit Random(uint64_t seed) : state(seed ? seed : 1) {}
    uint32_t next() {
        state ^= state >> 12;
        state ^= state << 25;
        state ^= state >> 27;
        return uint32_t((state * 0x2545f4914f6cdd1dull) >> 32);
    }

private:
    uint64_t state;
};

struct PortSignals {
    uint32_t *address;
    uint8_t *burstcount, *read, *write;
    uint64_t *writedata;
    uint8_t *byteenable, *waitrequest;
    uint64_t *readdata;
    uint8_t *readdatavalid;
};

class Port {
public:
    Port(Window &window, uint64_t seed, std::string name) : window(window), random(seed), name(std::move(name)) {}

    // Before each rising edge: drive this clock's responses and take the
    // command the master presents, if the port accepts it.
    void drive(const PortSignals &port, uint64_t cycle) {
        uint32_t draw_wait = random.next(), draw_gap = random.next(), draw_latency = random.next();
        if (refused && !(presented(port) == held))
            throw std::runtime_error(name + ": a command changed or withdrawn under waitrequest at clock " +
                                     std::to_string(cycle));
        refused = false;
        *port.readdatavalid = 0;
        if (!reads.empty() && reads.front().ready <= cycle && (draw_gap & 15) != 0) {
            Burst &burst = reads.front();
            uint64_t value;
            std::memcpy(&value, window.at(offset(burst.address + burst.done)), 8);
            *port.readdata = value;
            *port.readdatavalid = 1;
            ++beats_read;
            if (++burst.done == burst.beats) reads.pop_front();
        }
        bool refuse = (draw_wait & 15) == 0 || reads.size() >= 14;
        *port.waitrequest = refuse;
        if (refuse) {
            if (*port.read || *port.write) {
                held = presented(port);
                refused = true;
            }
            return;
        }
        if (*port.read) {
            if (write_left) throw std::runtime_error(name + ": read during a write burst");
            unsigned beats = *port.burstcount;
            if (beats == 0 || beats > 128) throw std::runtime_error(name + ": bad read burst length");
            offset(*port.address + beats - 1);
            unsigned roll = draw_latency % 1000;
            unsigned latency = roll < 1 ? 54 + (draw_latency >> 10) % 26 :
                               roll < 15 ? 32 + (draw_latency >> 10) % 20 : 17 + (draw_latency >> 10) % 2;
            reads.push_back({cycle + latency, *port.address, beats, 0});
            ++commands;
        } else if (*port.write) {
            if (!write_left) {
                write_address = *port.address;
                write_left = *port.burstcount;
                write_beats = write_left;
                if (write_left == 0 || write_left > 128) throw std::runtime_error(name + ": bad write burst length");
                ++commands;
            } else if (*port.address != write_address || *port.burstcount != write_beats) {
                throw std::runtime_error(name + ": a write burst cut short, then another begun at clock " +
                                         std::to_string(cycle));
            }
            uint8_t *target = window.at(offset(write_address + (write_beats - write_left)));
            uint64_t data = *port.writedata;
            for (int lane = 0; lane < 8; ++lane)
                if (*port.byteenable & (1u << lane)) target[lane] = uint8_t(data >> (8 * lane));
            ++beats_written;
            --write_left;
        }
    }

    bool idle() const { return reads.empty() && write_left == 0; }
    uint64_t commands = 0, beats_read = 0, beats_written = 0;

private:
    struct Burst {
        uint64_t ready;
        uint32_t address;
        unsigned beats, done;
    };
    // The fields a refused command must hold; write data only for a write.
    struct Command {
        bool read, write;
        uint32_t address;
        uint8_t burstcount, byteenable;
        uint64_t writedata;
        bool operator==(const Command &other) const {
            return read == other.read && write == other.write && address == other.address &&
                   burstcount == other.burstcount &&
                   (!write || (byteenable == other.byteenable && writedata == other.writedata));
        }
    };
    static Command presented(const PortSignals &port) {
        return {*port.read != 0, *port.write != 0, *port.address, *port.burstcount, *port.byteenable,
                *port.writedata};
    }
    uint32_t offset(uint32_t word) {
        uint64_t byte = uint64_t(word) * 8;
        if (byte < WINDOW_BASE || byte + 8 > uint64_t(WINDOW_BASE) + WINDOW_BYTES)
            throw std::runtime_error(name + ": command outside the DDR window");
        return uint32_t(byte - WINDOW_BASE);
    }
    Window &window;
    Random random;
    std::string name;
    std::deque<Burst> reads;
    unsigned write_left = 0, write_beats = 0;
    uint32_t write_address = 0;
    bool refused = false;
    Command held{};
};

// Port 0: 128-bit beats on word addresses (byte address / 16), for any
// Verilated top with fes_i386_cpu's ddr_* port names. Memory is anything
// with uint8_t *at(uint32_t window_offset). fixed gives every read the
// median latency and never refuses or gaps, for runs whose timing must not
// vary.
template <typename Memory> class Port0 {
public:
    Port0(Memory &memory, uint64_t seed, std::string name) : memory(memory), random(seed), name(std::move(name)) {}
    bool fixed = false;

    // Before each rising edge: drive this clock's responses and take the
    // command the master presents, if the port accepts it.
    template <typename Top> void drive(Top &top, uint64_t cycle) {
        uint32_t draw_wait = random.next(), draw_gap = random.next(), draw_latency = random.next();
        top.ddr_readdatavalid = 0;
        if (!reads.empty() && reads.front().ready <= cycle && (fixed || (draw_gap & 15) != 0)) {
            Burst &burst = reads.front();
            const uint8_t *beat = at(burst.address + burst.done);
            for (int word = 0; word < 4; ++word) {
                uint32_t value;
                std::memcpy(&value, beat + 4 * word, 4);
                top.ddr_readdata[word] = value;
            }
            top.ddr_readdatavalid = 1;
            ++beats_read;
            if (++burst.done == burst.beats) reads.pop_front();
        }
        bool refuse = (!fixed && (draw_wait & 15) == 0) || reads.size() >= 14;
        top.ddr_waitrequest = refuse;
        if (refuse) return;
        if (top.ddr_read) {
            if (write_left) throw std::runtime_error(name + ": read during a write burst");
            unsigned beats = top.ddr_burstcount;
            if (beats == 0 || beats > 128) throw std::runtime_error(name + ": bad read burst length");
            at(top.ddr_address + beats - 1);
            unsigned roll = draw_latency % 1000;
            unsigned latency = fixed ? 18 : roll < 1 ? 52 + (draw_latency >> 10) % 84 :
                               roll < 10 ? 47 + (draw_latency >> 10) % 5 : 18 + (draw_latency >> 10) % 2;
            reads.push_back({cycle + latency, uint32_t(top.ddr_address), beats, 0});
            ++commands;
        } else if (top.ddr_write) {
            if (!write_left) {
                write_address = top.ddr_address;
                write_left = top.ddr_burstcount;
                write_beats = write_left;
                if (write_left == 0 || write_left > 128) throw std::runtime_error(name + ": bad write burst length");
                ++commands;
            }
            uint8_t *target = at(write_address + (write_beats - write_left));
            for (int lane = 0; lane < 16; ++lane)
                if (top.ddr_byteenable & (1u << lane)) target[lane] = uint8_t(top.ddr_writedata[lane / 4] >> (8 * (lane % 4)));
            ++beats_written;
            --write_left;
        }
    }

    bool idle() const { return reads.empty() && write_left == 0; }
    uint64_t commands = 0, beats_read = 0, beats_written = 0;

private:
    struct Burst {
        uint64_t ready;
        uint32_t address;
        unsigned beats, done;
    };
    uint8_t *at(uint32_t word) {
        uint64_t byte = uint64_t(word) * 16;
        if (byte < WINDOW_BASE || byte + 16 > uint64_t(WINDOW_BASE) + WINDOW_BYTES)
            throw std::runtime_error(name + ": command outside the DDR window");
        return memory.at(uint32_t(byte - WINDOW_BASE));
    }
    Memory &memory;
    Random random;
    std::string name;
    std::deque<Burst> reads;
    unsigned write_left = 0, write_beats = 0;
    uint32_t write_address = 0;
};

// VRAM as the RTL keeps it: word w (planes 0-3) at window offset 0x800000 + 4w.
inline uint8_t vram_byte(Window &window, uint32_t offset, unsigned plane) {
    return window.byte(VRAM_OFFSET + 4 * offset + plane);
}

}  // namespace pc386
