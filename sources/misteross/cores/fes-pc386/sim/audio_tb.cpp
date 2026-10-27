// SPDX-License-Identifier: MIT
// Directed checks of the pc386 audio through the AT core's I/O port
// (docs/pc386-machine-design.md, "Audio" and "Verification", items 1 and 4):
// the Sound Blaster 2.0 DSP's command set, its DMA and IRQ sequences through
// the AT core's 8237 and PICs at every base and IRQ choice, the OPL2's AdLib
// status and timers, and the mixer's levels. No CPU: each check writes and
// reads ports as a driver would. Models: Vpc386_at_core and Vpc386_audio,
// joined by sim/audio_link.h; 100 MHz.
//
// Prints PASS <name> or FAIL <name>: <detail> per check; exits 1 on a failure.
#include "audio_link.h"
#include "Vpc386_at_core.h"
#include "verilated.h"

#include <algorithm>
#include <cmath>
#include <cstdint>
#include <cstdio>
#include <functional>
#include <memory>
#include <string>
#include <vector>

namespace {

constexpr uint64_t US = 100, MS = 100000;    // clocks per microsecond, millisecond

int failures = 0;

void check(const std::string &name, bool ok, const std::string &detail = "") {
    if (ok) std::printf("PASS %s\n", name.c_str());
    else {
        std::printf("FAIL %s: %s\n", name.c_str(), detail.c_str());
        ++failures;
    }
}

std::string hex(uint64_t value) {
    char text[32];
    std::snprintf(text, sizeof text, "%llx", (unsigned long long)value);
    return text;
}

struct Bench {
    Vpc386_at_core top;
    AudioLink<Vpc386_at_core> link{top};
    uint64_t cycles = 0;
    std::vector<uint8_t> memory = std::vector<uint8_t>(16u << 20);
    bool dma_pending = false;
    uint16_t base;
    unsigned irq;
    std::vector<uint8_t> dac_changes;      // DAC bytes in order of change
    std::vector<uint64_t> dac_times;

    Bench(uint16_t base_port, unsigned irq_line) : base(base_port), irq(irq_line) {
        top.reset = 1;
        top.disk_present = 0;
        top.base_kib = 640;
        top.extended_kib = 15360;
        top.sb_base_high = uint8_t(base >> 4); top.sb_irq = uint8_t(irq); top.sb_dma = 1;
        for (int n = 0; n < 4; ++n) clock();
        top.reset = 0;
        while (top.cmos_busy) clock();
    }

    void clock() {
        top.dma_done = dma_pending;
        dma_pending = false;
        if (top.dma_valid && !top.dma_done) {
            dma_pending = true;
            if (top.dma_write) memory[top.dma_address] = top.dma_wdata;
            else top.dma_rdata = memory[top.dma_address];
        }
        top.vga_done = top.vga_valid;
        link.carry(cycles);
        link.edge();
        ++cycles;
        uint8_t dac = link.audio.dsp_dac;
        if (dac != last_dac) {
            dac_changes.push_back(dac);
            dac_times.push_back(cycles);
            last_dac = dac;
        }
    }
    void run(uint64_t n) { for (uint64_t i = 0; i < n; ++i) clock(); }
    bool wait(const std::function<bool()> &condition, uint64_t limit) {
        for (uint64_t i = 0; i < limit; ++i) {
            if (condition()) return true;
            clock();
        }
        return condition();
    }
    uint32_t io(bool write, uint16_t port, uint32_t value = 0) {
        while (top.io_busy) clock();
        top.io_valid = 1; top.io_write = write; top.io_port = port; top.io_size = 0; top.io_wdata = value;
        clock();
        top.io_valid = 0;
        while (!top.io_done) clock();
        return top.io_rdata & 0xff;
    }
    uint8_t in(uint16_t port) { return uint8_t(io(false, port)); }
    void out(uint16_t port, uint8_t value) { io(true, port, value); }
    uint8_t inta() {
        while (top.io_busy) clock();
        top.inta_valid = 1;
        clock();
        top.inta_valid = 0;
        while (!top.inta_done) clock();
        return top.inta_vector;
    }

    // ---- DSP, as a driver talks to it ------------------------------------------
    bool dsp_write(uint8_t value) {
        for (int n = 0; n < 1000; ++n)
            if (!(in(base + 0xc) & 0x80)) { out(base + 0xc, value); return true; }
        return false;
    }
    int dsp_read() {
        for (int n = 0; n < 1000; ++n)
            if (in(base + 0xe) & 0x80) return in(base + 0xa);
        return -1;
    }
    // Returns the clocks from releasing reset to AAh being readable, or 0.
    uint64_t dsp_reset() {
        out(base + 6, 1);
        run(3 * US);
        out(base + 6, 0);
        uint64_t released = cycles;
        for (int n = 0; n < 1000; ++n)
            if (in(base + 0xe) & 0x80) return in(base + 0xa) == 0xaa ? cycles - released : 0;
        return 0;
    }

    // ---- 8237 channel 1 and the PICs ----------------------------------------------
    void dma1(uint32_t address, uint16_t count_minus_one, uint8_t mode) {
        out(0x0a, 0x05);                         // mask channel 1
        out(0x0c, 0);
        out(0x02, uint8_t(address)); out(0x02, uint8_t(address >> 8));
        out(0x83, uint8_t(address >> 16));
        out(0x03, uint8_t(count_minus_one)); out(0x03, uint8_t(count_minus_one >> 8));
        out(0x0b, mode);
        out(0x0a, 0x01);                         // unmask
    }
    void pic_init() {
        out(0x20, 0x11); out(0x21, 0x08); out(0x21, 0x04); out(0x21, 0x01);
        out(0xa0, 0x11); out(0xa1, 0x70); out(0xa1, 0x02); out(0xa1, 0x01);
        unsigned line = irq == 2 ? 9 : irq;
        if (line >= 8) { out(0x21, 0xfb); out(0xa1, uint8_t(~(1u << (line - 8)))); }
        else { out(0x21, uint8_t(~(1u << line))); out(0xa1, 0xff); }
        run(20);
        if (top.intr) inta();
    }
    uint8_t expected_vector() const { return irq == 2 ? 0x71 : uint8_t(0x08 + irq); }
    // Takes the interrupt, acknowledges the DSP at base+E and sends EOI.
    bool take_irq(uint64_t limit, uint8_t *vector = nullptr) {
        if (!wait([&] { return bool(top.intr); }, limit)) return false;
        uint8_t v = inta();
        if (vector) *vector = v;
        in(base + 0xe);
        if (irq == 2) out(0xa0, 0x20);
        out(0x20, 0x20);
        return true;
    }
    void speaker_on() { dsp_write(0xd1); }

private:
    uint8_t last_dac = 0x80;
};

std::string base_name(const Bench &b) {
    return hex(b.base) + "h IRQ " + std::to_string(b.irq);
}

// Reset, identification and the forced IRQ at one base and IRQ setting.
void resources(uint16_t base, unsigned irq) {
    Bench b(base, irq);
    std::string where = base_name(b);
    uint64_t reset_clocks = b.dsp_reset();
    check("DSP reset at " + where + ": AAh about 20 us after release", reset_clocks >= 20 * US && reset_clocks < 30 * US,
          std::to_string(reset_clocks) + " clocks");
    b.dsp_write(0xe1);
    int major = b.dsp_read(), minor = b.dsp_read();
    check("DSP version at " + where + " is 2.01", major == 2 && minor == 1, hex(major) + " " + hex(minor));
    uint16_t other = base == 0x220 ? 0x240 : 0x220;
    check("the other base (" + hex(other) + "h) reads FFh at " + where,
          b.in(other + 0xe) == 0xff && b.in(other + 0xc) == 0xff && b.in(other + 0x8) == 0xff);
    b.pic_init();
    b.dsp_write(0xf2);
    uint64_t start = b.cycles;
    uint8_t vector = 0;
    bool raised = b.take_irq(100 * US, &vector);
    char want[8];
    std::snprintf(want, sizeof want, "%02Xh", b.expected_vector());
    check("F2h raises IRQ " + std::to_string(irq) + " as vector " + want + " about 10 us later at " + where,
          raised && vector == b.expected_vector() && b.cycles - start >= 10 * US, hex(vector));
    b.run(100);
    check("reading base+E acknowledges the IRQ at " + where, !b.top.intr && !(b.link.audio.irq));
    uint8_t status = b.in(base + 8);
    check("OPL2 status at base+8 at " + where + " reads the OPL2's 06h low bits", (status & 0x1f) == 0x06, hex(status));
}

void commands() {
    Bench b(0x220, 7);
    b.dsp_reset();
    b.out(0x22c, 0xd1);
    uint8_t busy = b.in(0x22c);
    b.run(3 * US);
    uint8_t ready = b.in(0x22c);
    check("write status reads AAh just after a command byte and 2Ah 3 us later", busy == 0xaa && ready == 0x2a,
          hex(busy) + " " + hex(ready));
    check("read status reads 2Ah with nothing to read", b.in(0x22e) == 0x2a);
    b.dsp_write(0xe0); b.dsp_write(0x5a);
    int ident = b.dsp_read();
    check("E0h returns the inverted byte", ident == 0xa5, hex(ident));
    b.dsp_write(0xd8);
    int on = b.dsp_read();
    b.dsp_write(0xd3); b.dsp_write(0xd8);
    int off = b.dsp_read();
    check("D8h reports the speaker on (FFh) and off (00h)", on == 0xff && off == 0x00, hex(on) + " " + hex(off));
    b.dsp_write(0xe4); b.dsp_write(0x3c); b.dsp_write(0xe8);
    int test = b.dsp_read();
    check("E4h/E8h test register", test == 0x3c, hex(test));
    b.dsp_write(0x20);
    int adc = b.dsp_read();
    check("20h direct ADC returns silence (80h)", adc == 0x80, hex(adc));
    int again = b.in(0x22a);
    check("an empty read buffer returns its last byte", again == 0x80, hex(again));
    b.dsp_write(0x10); b.dsp_write(0xf0);
    b.run(10);
    check("10h with the speaker off leaves the output at 80h", b.link.audio.dsp_dac == 0x80, hex(b.link.audio.dsp_dac));
    b.dsp_write(0xd1);
    b.run(10);
    check("the DAC holds the last direct byte; speaker on plays it", b.link.audio.dsp_dac == 0xf0, hex(b.link.audio.dsp_dac));
    // An ADPCM command with its two length bytes is consumed and ignored.
    b.dsp_write(0x74); b.dsp_write(0xd3); b.dsp_write(0xd3);
    b.dsp_write(0xe0); b.dsp_write(0x0f);
    int after = b.dsp_read();
    check("74h consumes its two argument bytes", after == 0xf0 && b.link.audio.dsp_dac == 0xf0, hex(after));

    b.dsp_write(0xd1);
    b.run(3 * US);
    b.out(0x22c, 0xd3); // Turn the speaker off.
    b.out(0x22c, 0xd1); // This unpolled OUT lands less than 1 us later, while busy.
    b.run(3 * US);
    b.dsp_write(0xd8);
    int speaker_after_busy_write = b.dsp_read();
    check("a command byte written during the 2 us busy window is dropped",
          speaker_after_busy_write == 0x00, hex(speaker_after_busy_write));
}

// Single-cycle DMA playback, 14h: the bytes, their spacing and the IRQ.
void single_cycle() {
    Bench b(0x220, 5);
    b.dsp_reset();
    b.pic_init();
    b.speaker_on();
    const int n = 32;
    for (int i = 0; i < n; ++i) b.memory[0x31000 + i] = uint8_t(0x90 + 3 * i);
    b.dma1(0x31000, n - 1, 0x49);                // single, read, channel 1
    b.dsp_write(0x40); b.dsp_write(256 - 100);   // 10 kHz
    b.dac_changes.clear(); b.dac_times.clear();
    uint64_t dacks = b.link.dacks;
    b.dsp_write(0x14); b.dsp_write(n - 1); b.dsp_write(0);
    uint64_t start = b.cycles;
    uint8_t vector = 0;
    bool raised = b.take_irq(uint64_t(n + 4) * 100 * US, &vector);
    uint64_t elapsed = b.cycles - start;
    bool bytes = b.dac_changes.size() == size_t(n);
    for (int i = 0; bytes && i < n; ++i) bytes = b.dac_changes[i] == uint8_t(0x90 + 3 * i);
    check("14h plays the 32 bytes in order through the DAC", bytes, std::to_string(b.dac_changes.size()) + " changes");
    uint64_t low = ~0ull, high = 0;
    for (size_t i = 1; i < b.dac_times.size(); ++i) {
        low = std::min(low, b.dac_times[i] - b.dac_times[i - 1]);
        high = std::max(high, b.dac_times[i] - b.dac_times[i - 1]);
    }
    check("time constant 9Ch spaces the samples 100 us apart (10 kHz)", low >= 100 * US - 30 && high <= 100 * US + 30,
          std::to_string(low) + ".." + std::to_string(high));
    check("IRQ 5 (vector 0Dh) at the block's end", raised && vector == 0x0d && elapsed >= uint64_t(n) * 100 * US - 200,
          hex(vector) + " after " + std::to_string(elapsed));
    b.run(500 * US);
    check("single-cycle stops after its block", b.link.dacks - dacks == uint64_t(n),
          std::to_string(b.link.dacks - dacks) + " acknowledges");
    uint8_t status = b.in(0x08);
    check("the 8237 reached terminal count on channel 1", status & 0x02, hex(status));
}

// Auto-init, 1Ch with 48h: an IRQ per block; D0h/D4h; DAh ends after the block.
void auto_init() {
    Bench b(0x240, 2);
    b.dsp_reset();
    b.pic_init();
    b.speaker_on();
    for (int i = 0; i < 64; ++i) b.memory[0x52000 + i] = uint8_t(i * 4);
    b.dma1(0x52000, 63, 0x59);                   // single, auto-init, read
    b.dsp_write(0x40); b.dsp_write(256 - 50);    // 20 kHz
    b.dsp_write(0x48); b.dsp_write(15); b.dsp_write(0);
    b.dsp_write(0x1c);
    std::vector<uint64_t> irqs;
    uint8_t vector = 0;
    for (int i = 0; i < 3; ++i) {
        if (!b.take_irq(40 * 50 * US, &vector)) break;
        irqs.push_back(b.cycles);
    }
    bool spacing = irqs.size() == 3;
    for (size_t i = 1; spacing && i < irqs.size(); ++i) {
        uint64_t gap = irqs[i] - irqs[i - 1];
        spacing = gap > 16 * 50 * US - 300 && gap < 16 * 50 * US + 300;
    }
    check("1Ch with 48h block 16: an IRQ every 16 samples at 20 kHz", spacing, std::to_string(irqs.size()) + " IRQs");
    check("IRQ 2 arrives as the AT's IRQ 9 (vector 71h) at 240h", vector == 0x71, hex(vector));
    uint64_t before = b.link.dacks;
    b.dsp_write(0xd0);
    b.run(10 * 50 * US);
    uint64_t halted = b.link.dacks - before;
    b.dsp_write(0xd4);
    b.run(4 * 50 * US);
    check("D0h halts DMA and D4h continues it", halted <= 1 && b.link.dacks - before >= 3,
          std::to_string(halted) + " then " + std::to_string(b.link.dacks - before));
    b.dsp_write(0xda);
    bool last = b.take_irq(40 * 50 * US);
    before = b.link.dacks;
    b.run(40 * 50 * US);
    check("DAh ends auto-init after the current block", last && b.link.dacks == before && !b.top.intr,
          std::to_string(b.link.dacks - before) + " more acknowledges");
}

// High speed, 91h: busy and deaf until the block ends.
void high_speed() {
    Bench b(0x220, 3);
    b.dsp_reset();
    b.pic_init();
    for (int i = 0; i < 64; ++i) b.memory[0x60000 + i] = uint8_t(255 - i);
    b.dma1(0x60000, 63, 0x49);
    b.dsp_write(0x40); b.dsp_write(233);         // 43.5 kHz
    b.dsp_write(0x48); b.dsp_write(63); b.dsp_write(0);
    b.dsp_write(0x91);
    b.run(200 * US);
    uint8_t status = b.in(0x22c);
    b.out(0x22c, 0xe1);                          // must be ignored
    uint8_t vector = 0;
    bool raised = b.take_irq(64 * 23 * US + 2000 * US, &vector);
    check("91h reads busy (AAh) during the block", status == 0xaa, hex(status));
    check("91h raises IRQ 3 (vector 0Bh) at the block's end", raised && vector == 0x0b, hex(vector));
    b.run(100 * US);
    check("a byte written during high speed is dropped", !(b.in(0x22e) & 0x80) && b.in(0x22c) == 0x2a);
    // 90h runs until reset.
    b.dma1(0x60000, 63, 0x59);
    b.dsp_write(0x90);
    b.run(3000 * US);
    uint64_t dacks = b.link.dacks;
    uint64_t reset_clocks = b.dsp_reset();
    b.run(1000 * US);
    check("reset stops high-speed auto-init", reset_clocks != 0 && b.link.dacks - dacks <= 1,
          std::to_string(b.link.dacks - dacks) + " acknowledges after reset");
}

// 80h silence, 24h recording and E2h DMA identification.
void other_transfers() {
    Bench b(0x220, 7);
    b.dsp_reset();
    b.pic_init();
    b.dsp_write(0x40); b.dsp_write(256 - 100);
    b.dsp_write(0x80); b.dsp_write(9); b.dsp_write(0);
    uint64_t start = b.cycles;
    uint8_t vector = 0;
    bool raised = b.take_irq(20 * 100 * US, &vector);
    uint64_t elapsed = b.cycles - start;
    check("80h: IRQ 7 (vector 0Fh) after 10 silent samples", raised && vector == 0x0f && elapsed > 9 * 100 * US &&
          elapsed < 11 * 100 * US + 1000, std::to_string(elapsed));

    std::fill(b.memory.begin() + 0x70000, b.memory.begin() + 0x70020, 0x11);
    b.dma1(0x70000, 15, 0x45);                   // single, write (device to memory), channel 1
    b.dsp_write(0x24); b.dsp_write(15); b.dsp_write(0);
    raised = b.take_irq(30 * 100 * US);
    bool silence = std::all_of(b.memory.begin() + 0x70000, b.memory.begin() + 0x70010, [](uint8_t v) { return v == 0x80; });
    check("24h records 16 bytes of silence (80h) by DMA and raises the IRQ", raised && silence && b.memory[0x70010] == 0x11);

    // DOSBox-X: valadd starts AAh, valxor 96h; each E2h adds (d ^ valxor) and
    // rotates valxor right by two.
    uint8_t add = 0xaa, xr = 0x96;
    for (uint8_t d : {uint8_t(0x5a), uint8_t(0xc3)}) {
        add = uint8_t(add + (d ^ xr));
        xr = uint8_t((xr >> 2) | (xr << 6));
        b.memory[0x71000] = 0;
        b.dma1(0x71000, 0, 0x45);
        b.dsp_write(0xe2); b.dsp_write(d);
        b.run(200 * US);
        check("E2h " + hex(d) + " writes " + hex(add) + " by DMA", b.memory[0x71000] == add, hex(b.memory[0x71000]));
    }
}

// AdLib detection and the OPL2 timers, at 388h.
void opl2_timers() {
    Bench b(0x220, 7);
    auto write = [&](uint8_t reg, uint8_t value) { b.out(0x388, reg); b.out(0x389, value); };
    write(0x04, 0x60); write(0x04, 0x80);
    uint8_t first = b.in(0x388);
    write(0x02, 0xff); write(0x04, 0x21);
    uint64_t start = b.cycles;
    bool flagged = b.wait([&] { return (b.in(0x388) & 0xe0) == 0xc0; }, 200 * US);
    uint64_t t1 = b.cycles - start;
    uint8_t second = b.in(0x388);
    write(0x04, 0x60); write(0x04, 0x80);
    check("AdLib detection: flags clear, then timer 1 at FFh sets IRQ and T1 (C0h) within 80.5 us",
          (first & 0xe0) == 0 && flagged && (second & 0xe0) == 0xc0 && t1 <= 81 * US,
          hex(first) + " " + hex(second) + " after " + std::to_string(t1));
    check("flags clear again after 04h = 80h", (b.in(0x388) & 0xe0) == 0);
    // Timer 1 from 00h overflows after 256 ticks of 80.46 us; timer 2 from F0h after 16 of 321.8 us.
    write(0x02, 0x00); write(0x03, 0xf0); write(0x04, 0x03);
    start = b.cycles;
    uint64_t t2_at = 0, t1_at = 0;
    while (b.cycles - start < 25 * MS && !(t1_at && t2_at)) {
        uint8_t status = b.in(0x388);
        if ((status & 0x20) && !t2_at) t2_at = b.cycles - start;
        if ((status & 0x40) && !t1_at) t1_at = b.cycles - start;
    }
    double t1_ms = t1_at / 1e5, t2_ms = t2_at / 1e5;
    check("timer 1 from 00h overflows in 20.60 ms (256 x 288 / 3.579545 MHz)", t1_ms > 20.5 && t1_ms < 20.7,
          std::to_string(t1_ms) + " ms");
    check("timer 2 from F0h overflows in 5.15 ms (16 x 1152 / 3.579545 MHz)", t2_ms > 5.0 && t2_ms < 5.2,
          std::to_string(t2_ms) + " ms");
    write(0x04, 0x60); write(0x04, 0x80);
    write(0x02, 0xff); write(0x04, 0x61);        // timer 1 masked
    b.run(200 * US);
    check("a masked timer sets no flag", (b.in(0x388) & 0xe0) == 0);
    check("the data port reads FFh", b.in(0x389) == 0xff);
}

// The tone's frequency from rising zero crossings, and its peak, over samples.
void tone(const std::vector<int16_t> &s, size_t from, double &hz, int &peak) {
    std::vector<size_t> rises;
    peak = 0;
    for (size_t i = from + 1; i < s.size(); ++i) {
        if (s[i - 1] < 0 && s[i] >= 0) rises.push_back(i);
        peak = std::max(peak, std::abs(int(s[i])));
    }
    hz = rises.size() >= 2 ? 48000.0 * double(rises.size() - 1) / double(rises.back() - rises.front()) : 0;
}

void mixer() {
    Bench b(0x220, 7);
    b.dsp_reset();
    // PC speaker: PIT channel 2, mode 3, 1193 -> 1000.2 Hz, gate and speaker on.
    b.out(0x43, 0xb6); b.out(0x42, uint8_t(1193)); b.out(0x42, uint8_t(1193 >> 8));
    b.out(0x61, 0x03);
    b.link.samples.clear();
    b.run(60 * MS);
    double hz; int peak;
    tone(b.link.samples, 960, hz, peak);         // after 20 ms of DC settling
    check("PC speaker square wave at 1000.2 Hz in the 48 kHz output", hz > 999 && hz < 1001.5, std::to_string(hz));
    check("PC speaker peak about -12 dBFS (8192)", peak > 7600 && peak < 9200, std::to_string(peak));
    b.out(0x61, 0x02);                           // gate off: output held high, speaker on
    b.link.samples.clear();
    b.run(150 * MS);
    int tail = std::abs(int(b.link.samples.back()));
    check("the DC blocker removes static cone drive", tail < 300, std::to_string(tail));
    b.out(0x61, 0x00);
    b.run(150 * MS);
    // DSP: direct DAC FFh with the speaker on steps the output by (FFh-80h)*256/2.
    b.speaker_on();
    b.link.samples.clear();
    b.dsp_write(0x10); b.dsp_write(0xff);
    b.run(2 * MS);
    int step = *std::max_element(b.link.samples.begin(), b.link.samples.end());
    check("DSP full scale reaches the output at -6 dBFS (16256)", step > 15800 && step < 16300, std::to_string(step));
    b.dsp_write(0x10); b.dsp_write(0x80);
    b.run(150 * MS);
    // OPL2: one channel, one operator at full level, through the mixer at half scale.
    auto write = [&](uint8_t reg, uint8_t value) { b.out(0x388, reg); b.out(0x389, value); };
    write(0x20, 0x01); write(0x23, 0x01); write(0x40, 0x3f); write(0x43, 0x00);
    write(0x60, 0xf0); write(0x63, 0xf0); write(0x80, 0x00); write(0x83, 0x00);
    write(0xc0, 0x01); write(0xa0, 0x44); write(0xb0, 0x32);     // f-num 244h, block 4
    b.link.samples.clear();
    b.run(60 * MS);
    tone(b.link.samples, 960, hz, peak);
    double expected = 0x244 * 49715.9 / double(1 << (20 - 4));
    check("OPL2 tone at its f-number frequency (" + std::to_string(expected) + " Hz)",
          std::fabs(hz - expected) < 1.0, std::to_string(hz));
    check("OPL2 single operator peak through the mixer about 4095/2", peak > 1900 && peak < 2150, std::to_string(peak));
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    std::setvbuf(stdout, nullptr, _IOLBF, 0);
    for (uint16_t base : {uint16_t(0x220), uint16_t(0x240)})
        for (unsigned irq : {2u, 3u, 5u, 7u}) resources(base, irq);
    commands();
    single_cycle();
    auto_init();
    high_speed();
    other_transfers();
    opl2_timers();
    mixer();
    std::printf("%s %d failure(s)\n", failures ? "FAIL" : "PASS", failures);
    return failures ? 1 : 0;
}
