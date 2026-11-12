// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_audio_output.h"
#include <array>
#include <cstdint>
#include <cstdlib>
#include <iostream>

static void check(bool condition, const char* message, unsigned scenario, uint64_t time) {
    if (!condition) {
        std::cerr << "scenario " << scenario << " at " << time << ": " << message << '\n';
        std::exit(1);
    }
}

// Observe only the I2S pins. A frame starts on the left-channel word-clock
// transition, one bit before its MSB; incomplete frames on lock loss are dropped.
struct Receiver {
    bool previous_bit_clock = false, previous_word_clock = false, framed = false;
    unsigned bit = 0;
    uint16_t left = 0, right = 0;
    std::array<unsigned, 3> active_frames{};
    std::array<unsigned, 3> changed_frames{};
    std::array<uint16_t, 3> last_left{};
    unsigned silent_frames = 0;

    void sample(const Vfes_audio_output& dut, uint64_t time, uint64_t frame_time,
                unsigned scenario) {
        if (!dut.locked) {
            check(!dut.sdata, "unlock must gate serial data with clocks stopped", scenario, time);
            framed = false;
            previous_word_clock = dut.lrclk;
        } else if (!previous_bit_clock && dut.sclk) {
            if (dut.lrclk != previous_word_clock) {
                if (!dut.lrclk) {
                    if (framed) {
                        check((left == 0 && right == 0) || uint16_t(left ^ right) == 0xffff,
                              "torn stereo sample after transfer or relock", scenario, time);
                        if (time > 14 * frame_time && time < 17 * frame_time) {
                            check(left == 0 && right == 0, "hold must serialize silence", scenario, time);
                            ++silent_frames;
                        }
                        if (time > 66 * frame_time) {
                            check(left == 0 && right == 0, "hold must survive loss and relock", scenario, time);
                            ++silent_frames;
                        }
                        if (left || right) {
                            int interval = -1;
                            if (time < 12 * frame_time) interval = 0;
                            if (time > 24 * frame_time && time < 34 * frame_time) interval = 1;
                            if (time > 49 * frame_time && time < 59 * frame_time) interval = 2;
                            if (interval >= 0) {
                                if (active_frames[interval] && left != last_left[interval])
                                    ++changed_frames[interval];
                                last_left[interval] = left;
                                ++active_frames[interval];
                            }
                        }
                    }
                    framed = true;
                    left = right = 0;
                }
                previous_word_clock = dut.lrclk;
                bit = 0;
            } else {
                ++bit;
            }
            if (framed) {
                if (bit >= 1 && bit <= 16) {
                    auto& value = dut.lrclk ? right : left;
                    value = uint16_t((value << 1) | dut.sdata);
                } else {
                    check(!dut.sdata, "I2S delay/padding must be zero", scenario, time);
                }
            }
        }
        previous_bit_clock = dut.sclk;
    }
};

static void run(unsigned source_half, unsigned audio_half, unsigned offset, unsigned scenario) {
    Vfes_audio_output dut;
    dut.source_clk = dut.audio_clk = dut.locked = dut.hold = 0;
    dut.left_sample = 0;
    dut.right_sample = 0xffff;
    dut.eval();
    Receiver receiver;
    const uint64_t frame_time = 512 * audio_half;
    unsigned lock_losses = 0;
    bool previous_locked = false;
    for (uint64_t time = 1; time < 78 * frame_time; ++time) {
        // Stop audio across the first relock, then stop the source across the
        // second. The third loss occurs while muted. None resets the CDC state.
        const bool audio_stopped = time >= 20 * frame_time && time < 22 * frame_time;
        const bool source_stopped = time >= 39 * frame_time && time < 46 * frame_time;
        const bool loss = (time >= 20 * frame_time + offset && time < 21 * frame_time + offset) ||
                          (time >= 40 * frame_time + offset && time < 41 * frame_time + offset) ||
                          (time >= 62 * frame_time + offset && time < 63 * frame_time + offset);
        dut.locked = time >= frame_time / 3 + offset && !loss;
        dut.hold = (time >= 12 * frame_time && time < 17 * frame_time) || time >= 60 * frame_time;
        if (previous_locked && !dut.locked) ++lock_losses;
        previous_locked = dut.locked;
        if (!source_stopped && time % source_half == 0) {
            dut.source_clk = !dut.source_clk;
            if (dut.source_clk) {
                dut.left_sample = uint16_t(dut.left_sample + 7919);
                dut.right_sample = uint16_t(~dut.left_sample);
            }
        }
        if (!audio_stopped && (time + offset) % audio_half == 0)
            dut.audio_clk = !dut.audio_clk;
        dut.eval();
        receiver.sample(dut, time, frame_time, scenario);
    }
    check(lock_losses == 3, "all three lock-loss cases must execute", scenario, 0);
    for (auto frames : receiver.active_frames)
        check(frames >= 6, "nonzero stereo must recover after each stopped-clock interval", scenario, 0);
    for (auto changes : receiver.changed_frames)
        check(changes >= 5, "sample transfers must resume, not repeat stale PCM", scenario, 0);
    check(receiver.silent_frames >= 10, "mute and muted relock must yield complete silent frames", scenario, 0);
    std::cout << "scenario " << scenario << ": source/audio half periods " << source_half << '/'
              << audio_half << ", offset " << offset << ", active frames "
              << receiver.active_frames[0] << '/' << receiver.active_frames[1] << '/'
              << receiver.active_frames[2] << ", silent frames " << receiver.silent_frames << '\n';
}

int main() {
    unsigned scenario = 0;
    // Fast/slow source clocks, coincident edges, and several lock-loss phases.
    for (const auto& periods : std::array<std::array<unsigned, 2>, 3>{{{3, 13}, {17, 5}, {1, 1}}})
        for (unsigned offset : {0u, 1u, 7u, 127u})
            run(periods[0], periods[1], offset, scenario++);
    std::cout << "PCM CDC/I2S: 12 clock/phase cases, 36 losses/relocks, atomic stereo and mute passed\n";
}
