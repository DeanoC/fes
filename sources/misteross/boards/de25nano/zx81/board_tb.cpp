// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vde25nano_zx81.h"
#include "Vde25nano_zx81___024root.h"
#include "verilated.h"
#include <cstdint>
#include <cstdio>
#include <cstdlib>

static void check(bool ok, const char* message) {
    if (!ok) { std::fprintf(stderr, "FAIL DE25 ZX81: %s\n", message); std::exit(1); }
}

#define SIGNAL(name) t.rootp->de25nano_zx81__DOT__##name

// Integer femtosecond scheduling with remainder carry preserves the requested
// average frequencies without making the unrelated clocks simultaneous.
struct Clock {
    uint64_t frequency, next = 0, remainder = 0;
    bool level = false, running = true;
    void advance() {
        constexpr uint64_t half_second_fs = 500000000000000ull;
        next += half_second_fs / frequency;
        remainder += half_second_fs % frequency;
        if (remainder >= frequency) { next++; remainder -= frequency; }
    }
};

struct Board {
    Vde25nano_zx81 t;
    Clock reference{50000000}, pixel{74250000};
    uint64_t reference_edges = 0, system_edges = 0, pixel_edges = 0, source_ink = 0;
    uint64_t ula_enables = 0, cpu_positive = 0, cpu_negative = 0;
    uint64_t frame_pixels = 0, complete_frames = 0, picture_frames = 0;
    unsigned active = 0, hs = 0, vs = 0, ink = 0, paper = 0, max_ink = 0, max_paper = 0;
    bool previous_vs = false, frame_started = false, inspect_picture = false;

    Board() {
        t.CLOCK0_50 = 0;
        t.KEY = 3;
        t.HDMI_TX_INT = 1;
        t.eval();
    }

    void blank() {
        check(!t.HDMI_TX_DE && !t.HDMI_TX_HS && !t.HDMI_TX_VS && !t.HDMI_TX_D,
              "configuration or PLL loss did not immediately blank HDMI");
    }

    void pixel_sample() {
        pixel_edges++;
        check(!(t.HDMI_MCLK || t.HDMI_SCLK || t.HDMI_LRCLK || t.HDMI_I2S),
              "audio pins must stay silent");
        if (!SIGNAL(pixel_reset_n)) {
            blank();
            // A released clock may resume in the middle of VS. Wait for its
            // next complete low-to-high transition before measuring a frame.
            previous_vs = true;
            frame_started = false;
            return;
        }
        if (t.HDMI_TX_VS && !previous_vs) {
            if (frame_started) {
                check(frame_pixels == 1650u * 750u, "720p frame period is not 1650x750");
                check(active == 1280u * 720u && hs == 40u * 750u && vs == 5u * 1650u,
                      "720p active area or sync widths changed");
                complete_frames++;
                if (ink > max_ink) max_ink = ink;
                if (paper > max_paper) max_paper = paper;
                if (inspect_picture && ink > 0 && paper > 1000) picture_frames++;
            }
            frame_started = true;
            frame_pixels = 0;
            active = hs = vs = ink = paper = 0;
        }
        previous_vs = t.HDMI_TX_VS;
        if (!frame_started) return;
        // Positive VS begins at (0,725). This phase reference checks every
        // external DE/HS/VS sample without relying on internal raster signals.
        const unsigned h = frame_pixels % 1650;
        const unsigned v = (725 + frame_pixels / 1650) % 750;
        check(bool(t.HDMI_TX_DE) == (h < 1280 && v < 720), "DE raster position changed");
        check(bool(t.HDMI_TX_HS) == (h >= 1390 && h < 1430), "HS raster position changed");
        check(bool(t.HDMI_TX_VS) == (v >= 725 && v < 730), "VS raster position changed");
        active += t.HDMI_TX_DE;
        hs += t.HDMI_TX_HS;
        vs += t.HDMI_TX_VS;
        check(t.HDMI_TX_D == 0 || t.HDMI_TX_D == 0xffffff, "ZX81 output is not monochrome");
        if (!t.HDMI_TX_DE) check(!t.HDMI_TX_D, "RGB porch is not black");
        if (h >= 312 && h < 968 && v >= 90 && v < 630) {
            ink += t.HDMI_TX_D == 0;
            paper += t.HDMI_TX_D == 0xffffff;
        }
        frame_pixels++;
    }

    void step() {
        uint64_t next = reference.next < pixel.next ? reference.next : pixel.next;
        bool pixel_rise = false;
        if (reference.next == next) {
            if (reference.running) {
                reference.level = !reference.level;
                if (reference.level) reference_edges++;
            }
            t.CLOCK0_50 = reference.level;
            reference.advance();
        }
        if (pixel.next == next) {
            if (pixel.running) { pixel.level = !pixel.level; pixel_rise = pixel.level; }
            SIGNAL(video_clock__DOT__outclk_0) = pixel.level;
            pixel.advance();
        }
        t.eval();
        if (pixel_rise) {
            system_edges++;
            ula_enables += SIGNAL(machine__DOT__ce_6m5) != 0;
            cpu_positive += SIGNAL(machine__DOT__ce_cpu_p) != 0;
            cpu_negative += SIGNAL(machine__DOT__ce_cpu_n) != 0;
            check(!(SIGNAL(machine__DOT__ce_cpu_p) && SIGNAL(machine__DOT__ce_cpu_n)),
                  "positive and negative CPU enables coincide");
            if (!SIGNAL(machine_reset) && SIGNAL(machine__DOT__ce_6m5) &&
                !SIGNAL(machine__DOT__hblank) && !SIGNAL(machine__DOT__vblank) &&
                SIGNAL(machine__DOT__video_pixel)) source_ink++;
        }
        if (pixel_rise) pixel_sample();
    }

    void cycles(uint64_t count) {
        const uint64_t until = system_edges + count;
        while (system_edges != until) step();
    }

    void reference_cycles(uint64_t count) {
        const uint64_t until = reference_edges + count;
        while (reference_edges != until) step();
    }

    uint8_t peek(uint16_t address) const {
        // Observe actual machine RAM; no test firmware or forced memory writes.
        return SIGNAL(machine__DOT__builtin_ram__DOT__ram_block__DOT__ram)[address & 0x3fff];
    }
    uint16_t word(uint16_t address) const {
        return uint16_t(peek(address)) | uint16_t(peek(address + 1)) << 8;
    }
    bool basic_ready() const {
        const uint16_t dfile = word(0x400c);
        return peek(0x4000) == 0 && dfile >= 0x4070 && dfile < 0x8000 && peek(dfile) == 0x76;
    }
    void boot(const char* description) {
        const uint64_t start = system_edges, old_ink = source_ink;
        bool halted = false;
        while (system_edges - start < 60000000ull) {
            cycles(4096);
            halted |= !SIGNAL(halt_n);
            if (!SIGNAL(machine_reset) && basic_ready() && halted && source_ink > old_ink) {
                std::printf("PASS DE25 ZX81 %s: BASIC D_FILE=%04x, HALT and ULA ink after %llu system clocks\n",
                            description, word(0x400c), (unsigned long long)(system_edges - start));
                return;
            }
        }
        check(false, "real ROM did not reach BASIC display/HALT/ink");
    }
};

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    auto* firmware = std::fopen("zx8x.hex", "rb");
    check(firmware != nullptr, "selected repository ROM is missing from the simulation directory");
    std::fclose(firmware);
    std::setvbuf(stdout, nullptr, _IOLBF, 0);
    Board b;
    auto& t = b.t;
    b.cycles(16); b.blank();
    SIGNAL(configuration_release__DOT__ninit_done) = 0;
    t.eval(); b.cycles(16); b.blank();
    const uint64_t ula = b.ula_enables, positive = b.cpu_positive, negative = b.cpu_negative;
    b.cycles(297);
    check(b.ula_enables - ula == 26 && b.cpu_positive - positive == 13 && b.cpu_negative - negative == 13,
          "74.25MHz scheduler does not preserve exact 6.5MHz ULA and 3.25MHz CPU enables");
    SIGNAL(video_clock__DOT__locked) = 1;
    t.eval(); b.blank();
    b.boot("cold boot");
    check(SIGNAL(keyboard) == 0xffffffffffull, "startup generated a phantom key");
    b.inspect_picture = true;
    const uint64_t picture_target = b.picture_frames + 2;
    const uint64_t picture_deadline = b.system_edges + 15000000;
    while (b.picture_frames < picture_target && b.system_edges < picture_deadline) b.step();
    if (b.picture_frames < picture_target) {
        unsigned stored_ink = 0;
        for (unsigned n = 0; n < 270u * 512u; ++n) stored_ink += SIGNAL(video__DOT__fb)[n] != 0;
        std::printf("picture diagnostic: frames=%llu max_ink=%u max_paper=%u last_ink=%u last_paper=%u "
                    "stored_ink=%u source_ink=%llu capture=(%u,%u)\n",
                    (unsigned long long)b.complete_frames, b.max_ink, b.max_paper, b.ink, b.paper,
                    stored_ink, (unsigned long long)b.source_ink,
                    SIGNAL(video__DOT__cap_x), SIGNAL(video__DOT__cap_y));
    }
    check(b.picture_frames >= picture_target, "complete HDMI frames did not contain both ink and paper");

    const uint64_t idle_deadline = b.system_edges + 22000000;
    while (b.word(0x4025) != 0xffff && b.system_edges < idle_deadline) b.cycles(4096);
    check(b.word(0x4025) == 0xffff, "ROM keyboard scanner did not become idle before KEY1");

    // Exercise the physical key path, including its real 10ms stable interval.
    const uint64_t before_key = SIGNAL(keyboard);
    t.KEY = 1; // KEY1 low is NEWLINE; KEY0 stays released.
    t.eval(); b.cycles(1000);
    check(SIGNAL(keyboard) == before_key, "KEY1 bypassed the debounce interval");
    b.cycles(744000);
    check(SIGNAL(keyboard) == (0xffffffffffull & ~(1ull << 30)),
          "KEY1 did not select only the active-low NEWLINE matrix bit");
    bool saw_rom_key = false;
    for (unsigned n = 0; n < 3000 && !saw_rom_key; ++n) {
        b.cycles(4096);
        saw_rom_key = b.word(0x4025) != 0xffff;
    }
    check(saw_rom_key, "ROM keyboard scanner did not observe external KEY1");
    t.KEY = 3; t.eval(); b.cycles(744000);
    check(SIGNAL(keyboard) == 0xffffffffffull, "KEY1 release did not clear NEWLINE");
    std::puts("PASS DE25 ZX81 KEY1: debounced NEWLINE reaches real ROM keyboard scanner");

    t.KEY = 2; t.eval(); b.cycles(744000);
    check(SIGNAL(machine_reset), "KEY0 did not hold machine reset");
    b.cycles(2048);
    t.KEY = 3; t.eval(); b.cycles(744000);
    check(!SIGNAL(machine_reset), "KEY0 release did not restart the machine");
    b.boot("KEY0 restart");

    // No new pixel edge is permitted before either asynchronous blank check.
    b.pixel.running = false;
    SIGNAL(video_clock__DOT__locked) = 0;
    t.eval(); b.blank();
    check(SIGNAL(machine_reset), "PLL loss did not hold execution reset");
    SIGNAL(video_clock__DOT__locked) = 1;
    t.eval(); b.blank();
    b.reference_cycles(16); b.blank();
    b.pixel.running = true;
    b.boot("PLL recovery");

    b.pixel.running = false;
    SIGNAL(configuration_release__DOT__ninit_done) = 1;
    t.eval(); b.blank();
    check(!SIGNAL(config_reset_n) && SIGNAL(machine_reset),
          "configuration assertion did not reset configuration and machine");
    SIGNAL(configuration_release__DOT__ninit_done) = 0;
    t.eval(); b.blank();
    b.reference_cycles(16); b.blank();
    b.pixel.running = true;
    b.boot("configuration recovery");
    const uint64_t final_frames = b.complete_frames + 2;
    const uint64_t final_deadline = b.system_edges + 4000000;
    while (b.complete_frames < final_frames && b.system_edges < final_deadline) b.step();
    check(b.complete_frames >= final_frames, "HDMI frames did not recover after configuration reset");
    std::printf("PASS DE25 ZX81: %llu complete 1650x750 frames, %llu frames with ink/paper; "
                "50MHz reference/74.25MHz machine-pixel clock, KEY0/KEY1, PLL/configuration blanking/recovery; "
                "digital board models only\n",
                (unsigned long long)b.complete_frames, (unsigned long long)b.picture_frames);
}
