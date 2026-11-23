// SPDX-License-Identifier: GPL-2.0-or-later
// Shared register stimulus for the RTL replay and the VICE 3.10 oracle.
#include <algorithm>
#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#ifdef C64_SID_AUDIO_ORACLE
#include "sid.h"
#else
#include "Vc64_sid.h"
#endif

struct Pcm {
    int actual, reference;
};

int main(int argc, char **argv) {
#ifdef C64_SID_AUDIO_ORACLE
    if (argc != 1) return 2;
    reSID::SID sid;
    sid.set_chip_model(reSID::MOS8580);
    sid.set_voice_mask(7);
    sid.input(0);
    sid.enable_filter(true);
    sid.adjust_filter_bias(0.0);
    sid.enable_external_filter(true);
    sid.reset();
#else
    if (argc != 2) {
        std::fprintf(stderr, "usage: %s UNCOMPRESSED_REFERENCE\n", argv[0]);
        return 2;
    }
    auto *input = std::fopen(argv[1], "rb");
    if (!input) { std::perror(argv[1]); return 2; }
    Vc64_sid sid;
    auto edge = [&](bool phi, bool write, unsigned address, unsigned value) {
        sid.phi = phi;
        sid.cs = sid.we = write;
        sid.addr = address;
        sid.din = value;
        sid.clk = 0; sid.eval();
        sid.clk = 1; sid.eval();
    };
    sid.reset = 1;
    edge(false, false, 0, 0);
    sid.reset = 0;
#endif
    bool compare_osc3 = false;
    unsigned total = 0, count = 0;
    int peak = 0;
    double square_error = 0;
    auto tick = [&](int address = -1, unsigned value = 0) -> Pcm {
#ifdef C64_SID_AUDIO_ORACLE
        // SAMPLE_FAST is the pinned kernel's default. Read OSC3 *before* issuing
        // the next write: reSID also uses its bus latch for the delayed store.
        sid.clock();
        unsigned osc = compare_osc3 ? sid.read(0x1b) : 0;
        int pcm = std::clamp(sid.output(), -32768, 32767);
        if (address >= 0) sid.write(address, value);
        std::putchar(uint16_t(pcm) & 255);
        std::putchar(uint16_t(pcm) >> 8);
        std::putchar(osc);
        int expected = pcm;
#else
        // The motherboard presents stores on SYS edge 27 between Phi strobes.
        for (unsigned sys = 0; sys < 51; ++sys)
            edge(sys == 0, address >= 0 && sys == 27, address < 0 ? 0 : address, value);
        sid.addr = 0x1b; sid.eval();
        int pcm = int16_t(sid.sample);
        unsigned char b[3];
        if (std::fread(b, 1, sizeof b, input) != sizeof b) {
            std::fprintf(stderr, "truncated reference at Phi %u\n", total);
            std::exit(1);
        }
        int expected = int16_t(unsigned(b[0]) | (unsigned(b[1]) << 8));
        if (compare_osc3 && b[2] != sid.dout) {
            std::fprintf(stderr, "OSC3 at Phi %u: got %u, expected %u\n", total, sid.dout, b[2]);
            std::exit(1);
        }
#endif
        ++total; ++count;
        int error = std::abs(pcm - expected);
        peak = std::max(peak, error);
        square_error += double(error) * error;
        return {pcm, expected};
    };
    auto hold = [&](unsigned clocks) { while (clocks--) tick(); };
    // Per-case bounds in signed 16-bit PCM units: peak < 0.1% full scale,
    // RMS < 0.025%. Keep the quiet D418 waveform on its own tighter budget.
    auto finish = [&](const char *label, int max_peak = 32, int max_rms = 8) {
        double rms = std::sqrt(square_error / count);
        std::fprintf(stderr, "%s: %u samples, peak error %d, RMS %.2f\n", label, count, peak, rms);
        if (peak > max_peak || rms > max_rms) std::exit(1);
        peak = 0; square_error = 0; count = 0;
    };

    // Release all voices and allow the external DC-blocking network to settle
    // for >13 time constants before the quiet D418 playback measurement.
    for (unsigned voice = 0; voice < 3; ++voice) {
        tick(voice * 7 + 6, 0); // fastest release
        tick(voice * 7 + 4, 8); // TEST holds phase at zero
    }
    hold(131072);
    finish("power-up / release");
    constexpr unsigned nibbles[] = {0,1,2,4,8,12,15,15,9,6,3,0,5,10,14,1,7,13,11,0};
    auto playback = [&] {
        int lo = 32767, hi = -32768, ref_lo = 32767, ref_hi = -32768;
        for (unsigned volume : nibbles) {
            for (unsigned i = 0; i < 64; ++i) {
                auto v = tick(i == 0 ? 0x18 : -1, volume);
                lo = std::min(lo, v.actual); hi = std::max(hi, v.actual);
                ref_lo = std::min(ref_lo, v.reference); ref_hi = std::max(ref_hi, v.reference);
            }
        }
        std::fprintf(stderr, "D418 swing [%d,%d], VICE [%d,%d]\n", lo, hi, ref_lo, ref_hi);
    };
    playback(); finish("D418 released voices, no digi boost", 8, 3);

    tick(0x00, 0xb3); tick(0x01, 0x28);
    tick(0x05, 0); tick(0x06, 0xf0);
    tick(0x18, 15); tick(0x04, 0x21); // unfiltered saw, fastest attack to full sustain
    hold(4096); finish("direct saw");
    playback(); finish("D418 on a running voice");

    for (unsigned mode : {0x10, 0x20, 0x40, 0x70}) {
        tick(0x18, mode | 15);
        for (unsigned fc : {1, 0x106, 0x423, 0x7ff}) {
            tick(0x15, fc & 7); tick(0x16, fc >> 3);
            for (unsigned res : {0, 8, 15}) {
                tick(0x17, (res << 4) | 1); hold(192);
            }
        }
        finish(mode == 0x10 ? "LP sweep" : mode == 0x20 ? "BP sweep" :
               mode == 0x40 ? "HP sweep" : "LP+BP+HP sweep");
    }

    // Fundamental attenuation at ~0.49 and ~1.95 kHz (1 MHz Phi). Compare to
    // bypass at the same frequency. Four settling periods precede a full-period
    // DFT. The 0.25 dB limit corresponds to 2.9% amplitude, with 1 dB allowed
    // deep in the stopband where the signal approaches the PCM quantization floor.
    struct Amplitude { double actual, reference; };
    for (unsigned period : {2048u, 512u}) {
        unsigned freq = (1u << 24) / period;
        tick(0x00, freq & 255); tick(0x01, freq >> 8);
        auto amplitude = [&]() -> Amplitude {
            hold(4 * period);
            double re = 0, im = 0, ref_re = 0, ref_im = 0;
            for (unsigned i = 0; i < period; ++i) {
                double angle = 6.283185307179586 * i / period;
                double c = std::cos(angle), s = std::sin(angle);
                auto y = tick();
                re += y.actual * c; im += y.actual * s;
                ref_re += y.reference * c; ref_im += y.reference * s;
            }
            return {std::hypot(re, im), std::hypot(ref_re, ref_im)};
        };
        tick(0x17, 0); tick(0x18, 15);
        auto direct = amplitude();
        for (unsigned fc : {96, 768}) {
            tick(0x15, fc & 7); tick(0x16, fc >> 3);
            tick(0x17, 0x41); // route voice 1, resonance 4
            for (unsigned mode : {0x10, 0x20, 0x40}) {
                tick(0x18, mode | 15);
                auto a = amplitude();
                double db = 20 * std::log10(a.actual / direct.actual);
                double ref_db = 20 * std::log10(a.reference / direct.reference);
                std::fprintf(stderr, "period %u, FC %u, mode %02x: %.3f dB, VICE %.3f dB\n",
                             period, fc, mode, db, ref_db);
                if (!std::isfinite(db) || std::abs(db-ref_db) > (ref_db < -35 ? 1.0 : 0.25))
                    return 1;
            }
        }
        finish("filter fundamental response");
    }

    // Read OSC3 while voice 3 cycles through all four 8580 combined tables.
    tick(0x17, 0); tick(0x18, 15);
    tick(0x0e, 0x76); tick(0x0f, 0x59);
    tick(0x10, 0xab); tick(0x11, 6);
    tick(0x13, 0); tick(0x14, 0xf0);
    tick(0x12, 0x21); hold(32);
    compare_osc3 = true;
    for (unsigned control : {0x31, 0x51, 0x61, 0x71}) {
        tick(0x12, control); hold(1024);
    }
    finish("voice 3 combined waveforms and OSC3");
#ifndef C64_SID_AUDIO_ORACLE
    if (std::getc(input) != EOF || std::ferror(input)) return 1;
    std::fclose(input);
#endif
    std::fprintf(stderr, "8580 PCM and register replay: %u Phi samples\n", total);
}
