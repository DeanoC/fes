// SPDX-License-Identifier: MIT
// HPS DDR window and 64-bit Avalon-MM port model for the pc386 benches.
//
// Read latency follows the Kit B measurement with Linux idle
// (https://github.com/DeanoC/fes/pull/1076, port 1 at 100 MHz): first data
// 17-18 clocks after the command for most reads, 32-51 for about 1.5% and
// 54-79 for 0.1%. Beats of a burst then come one a clock with an occasional
// gap. The port refuses a command at random about one clock in sixteen and
// while 14 reads are pending, as fes-menu's controller model does. Every draw
// comes from a seeded generator, consumed once per clock, so two benches that
// drive a port identically see identical timing.
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
        if (refuse) return;
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
};

// VRAM as the RTL keeps it: word w (planes 0-3) at window offset 0x800000 + 4w.
inline uint8_t vram_byte(Window &window, uint32_t offset, unsigned plane) {
    return window.byte(VRAM_OFFSET + 4 * offset + plane);
}

}  // namespace pc386
