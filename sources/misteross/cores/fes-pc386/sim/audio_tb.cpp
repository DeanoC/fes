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
    std::vector<uint64_t> dack_times;      // channel-1 acknowledges

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
        if (link.dacks != last_dacks) {
            dack_times.push_back(cycles);
            last_dacks = link.dacks;
        }
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

    // The DAC at a clock, from the changes recorded since dac_changes was cleared.
    uint8_t dac_at(uint64_t when, uint8_t before) const {
        size_t n = std::upper_bound(dac_times.begin(), dac_times.end(), when) - dac_times.begin();
        return n ? dac_changes[n - 1] : before;
    }

private:
    uint8_t last_dac = 0x80;
    uint64_t last_dacks = 0;
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
    // An unmodelled command with two argument bytes is consumed and ignored.
    b.dsp_write(0x41); b.dsp_write(0xd3); b.dsp_write(0xd3);
    b.dsp_write(0xe0); b.dsp_write(0x0f);
    int after = b.dsp_read();
    check("41h (SB16) consumes its two argument bytes", after == 0xf0 && b.link.audio.dsp_dac == 0xf0, hex(after));

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

// ---- ADPCM ----------------------------------------------------------------------
// A reference decoder from DOSBox-X's sblaster.cpp tables (decode_ADPCM_4,
// _3 and _2_sample): the step value and step-row move for each index
// code + step. Written out as tables, apart from the RTL's row formula.
struct AdpcmKind {
    const char *name;
    unsigned bits, codes, max_index;       // code width, codes per byte, last index
    const int *delta;
    const int *adjust;
};
const int delta4[64] = {
    0, 1, 2, 3, 4, 5, 6, 7, 0, -1, -2, -3, -4, -5, -6, -7,
    1, 3, 5, 7, 9, 11, 13, 15, -1, -3, -5, -7, -9, -11, -13, -15,
    2, 6, 10, 14, 18, 22, 26, 30, -2, -6, -10, -14, -18, -22, -26, -30,
    4, 12, 20, 28, 36, 44, 52, 60, -4, -12, -20, -28, -36, -44, -52, -60};
const int adjust4[64] = {
    0, 0, 0, 0, 0, 16, 16, 16, 0, 0, 0, 0, 0, 16, 16, 16,
    -16, 0, 0, 0, 0, 16, 16, 16, -16, 0, 0, 0, 0, 16, 16, 16,
    -16, 0, 0, 0, 0, 16, 16, 16, -16, 0, 0, 0, 0, 16, 16, 16,
    -16, 0, 0, 0, 0, 0, 0, 0, -16, 0, 0, 0, 0, 0, 0, 0};
const int delta3[40] = {
    0, 1, 2, 3, 0, -1, -2, -3, 1, 3, 5, 7, -1, -3, -5, -7,
    2, 6, 10, 14, -2, -6, -10, -14, 4, 12, 20, 28, -4, -12, -20, -28,
    5, 15, 25, 35, -5, -15, -25, -35};
const int adjust3[40] = {
    0, 0, 0, 8, 0, 0, 0, 8, -8, 0, 0, 8, -8, 0, 0, 8,
    -8, 0, 0, 8, -8, 0, 0, 8, -8, 0, 0, 8, -8, 0, 0, 8,
    -8, 0, 0, 0, -8, 0, 0, 0};
const int delta2[24] = {0, 1, 0, -1, 1, 3, -1, -3, 2, 6, -2, -6, 4, 12, -4, -12, 8, 24, -8, -24, 16, 48, -16, -48};
const int adjust2[24] = {0, 4, 0, 4, -4, 4, -4, 4, -4, 4, -4, 4, -4, 4, -4, 4, -4, 4, -4, 4, -4, 0, -4, 0};
const AdpcmKind adpcm4{"4-bit", 4, 2, 63, delta4, adjust4};
const AdpcmKind adpcm3{"2.6-bit", 3, 3, 39, delta3, adjust3};
const AdpcmKind adpcm2{"2-bit", 2, 4, 23, delta2, adjust2};

struct AdpcmReference {
    int reference = 0x80, step = 0;
    bool hit_low = false, hit_high = false;
    int top_index = 0;
    uint8_t decode(unsigned code, const AdpcmKind &k) {
        int index = std::min<int>(int(code) + step, int(k.max_index));
        top_index = std::max(top_index, index);
        int next = reference + k.delta[index];
        hit_low |= next < 0;
        hit_high |= next > 255;
        reference = std::clamp(next, 0, 255);
        step = (step + k.adjust[index]) & 0xff;
        return uint8_t(reference);
    }
    // The DAC over each sample period of a stream of bytes; a reference byte
    // holds for its byte's periods.
    void play(const std::vector<uint8_t> &bytes, bool with_ref, const AdpcmKind &k, std::vector<uint8_t> &out) {
        for (size_t i = 0; i < bytes.size(); ++i) {
            uint8_t b = bytes[i];
            if (i == 0 && with_ref) {
                reference = b;
                step = 0;
                out.insert(out.end(), k.codes, b);
                continue;
            }
            if (k.bits == 4) { out.push_back(decode(b >> 4, k)); out.push_back(decode(b & 15, k)); }
            else if (k.bits == 3) {
                out.push_back(decode(b >> 5, k)); out.push_back(decode((b >> 2) & 7, k));
                out.push_back(decode((b & 3) << 1, k));
            } else for (int shift = 6; shift >= 0; shift -= 2) out.push_back(decode((b >> shift) & 3, k));
        }
    }
};

// Bytes that climb to the top step row and both clamps, then pseudo-random.
std::vector<uint8_t> adpcm_stream(const AdpcmKind &k, size_t n, uint32_t seed) {
    uint8_t up = k.bits == 4 ? 0x77 : k.bits == 3 ? 0x6d : 0x55;     // the largest positive code
    uint8_t down = k.bits == 4 ? 0xff : k.bits == 3 ? 0xfe : 0xff;   // the largest negative code
    std::vector<uint8_t> bytes{0x80};
    for (int i = 0; i < 6; ++i) bytes.push_back(up);
    for (int i = 0; i < 10; ++i) bytes.push_back(down);
    for (int i = 0; i < 2; ++i) bytes.push_back(0x00);
    while (bytes.size() < n) {
        seed = seed * 1103515245u + 12345u;
        bytes.push_back(uint8_t(seed >> 16));
    }
    bytes.resize(n);
    return bytes;
}

// The DAC sampled mid-period for `periods` periods from the first
// acknowledge; the first code plays at each byte's acknowledge.
std::vector<uint8_t> dac_periods(const Bench &b, uint64_t first, uint64_t period, size_t periods, uint8_t before) {
    std::vector<uint8_t> out;
    for (size_t i = 0; i < periods; ++i) out.push_back(b.dac_at(first + i * period + period / 2, before));
    return out;
}

std::string first_difference(const std::vector<uint8_t> &got, const std::vector<uint8_t> &want) {
    for (size_t i = 0; i < std::min(got.size(), want.size()); ++i)
        if (got[i] != want[i]) return "period " + std::to_string(i) + ": " + hex(got[i]) + " not " + hex(want[i]);
    return std::to_string(got.size()) + " periods, expected " + std::to_string(want.size());
}

// Single-cycle ADPCM: the reference form, then the plain form continuing it.
void adpcm_single(const AdpcmKind &k, uint8_t with_ref, uint8_t plain) {
    Bench b(0x220, 7);
    b.dsp_reset();
    b.pic_init();
    b.speaker_on();
    const uint64_t period = 20 * US;
    b.dsp_write(0x40); b.dsp_write(256 - 20);    // 50 kHz
    AdpcmReference reference;
    uint8_t before = 0x80;
    for (int pass = 0; pass < 2; ++pass) {
        uint8_t command = pass ? plain : with_ref;
        std::string name = hex(command) + "h (" + k.name + (pass ? ", continuing)" : ", reference)");
        size_t n = pass ? 24 : 40;
        std::vector<uint8_t> bytes = adpcm_stream(k, n, 0x1455u + command);
        if (pass) bytes.erase(bytes.begin());    // no reference byte
        bytes.resize(n);
        uint32_t address = 0x40000 + 0x100 * pass;
        std::copy(bytes.begin(), bytes.end(), b.memory.begin() + address);
        b.dma1(address, uint16_t(n - 1), 0x49);
        b.dac_changes.clear(); b.dac_times.clear(); b.dack_times.clear();
        uint64_t dacks = b.link.dacks;
        b.dsp_write(command); b.dsp_write(uint8_t(n - 1)); b.dsp_write(0);
        uint8_t vector = 0;
        bool raised = b.take_irq(uint64_t(n + 2) * k.codes * period, &vector);
        size_t at_irq = b.dack_times.size();
        uint64_t irq_at = b.cycles;
        b.run((k.codes + 4) * period);
        check(name + ": IRQ 7 after length + 1 = " + std::to_string(n) + " bytes",
              raised && vector == 0x0f && at_irq == n && irq_at - b.dack_times.back() < 20 * US,
              std::to_string(at_irq) + " acknowledges at the IRQ");
        check(name + ": single-cycle stops after its block", b.link.dacks - dacks == n,
              std::to_string(b.link.dacks - dacks) + " acknowledges");
        uint64_t low = ~0ull, high = 0;
        for (size_t i = 1; i < b.dack_times.size(); ++i) {
            low = std::min(low, b.dack_times[i] - b.dack_times[i - 1]);
            high = std::max(high, b.dack_times[i] - b.dack_times[i - 1]);
        }
        check(name + ": one acknowledge per " + std::to_string(k.codes) + " sample periods",
              b.dack_times.size() == n && low + 30 >= k.codes * period && high <= k.codes * period + 30,
              std::to_string(low) + ".." + std::to_string(high) + " clocks");
        std::vector<uint8_t> want;
        reference.play(bytes, !pass, k, want);
        std::vector<uint8_t> got = b.dack_times.empty() ? std::vector<uint8_t>()
                                   : dac_periods(b, b.dack_times[0], period, want.size(), before);
        check(name + ": the DAC follows the reference decoder, last byte's codes included", got == want,
              first_difference(got, want));
        before = want.empty() ? before : want.back();
    }
    check(std::string(k.name) + " streams reach both clamps and the top step row",
          reference.hit_low && reference.hit_high && reference.top_index + int(1u << k.bits) > int(k.max_index),
          std::to_string(reference.top_index));
}

// Auto-init ADPCM: the reference byte only at the start, an IRQ per 48h block,
// and the 8237's wrap decoded as data.
void adpcm_auto(const AdpcmKind &k, uint8_t command) {
    Bench b(0x220, 7);
    b.dsp_reset();
    b.pic_init();
    b.speaker_on();
    const uint64_t period = 20 * US;
    const size_t block = 12, buffer = 20, blocks = 4;
    std::vector<uint8_t> bytes = adpcm_stream(k, buffer, 0x7d00u + command);
    std::copy(bytes.begin(), bytes.end(), b.memory.begin() + 0x50000);
    b.dma1(0x50000, uint16_t(buffer - 1), 0x59);     // single, auto-init, read
    b.dsp_write(0x40); b.dsp_write(256 - 20);
    b.dsp_write(0x48); b.dsp_write(uint8_t(block - 1)); b.dsp_write(0);
    b.dac_changes.clear(); b.dac_times.clear(); b.dack_times.clear();
    b.dsp_write(command);
    std::string name = hex(command) + "h (" + k.name + " auto-init)";
    std::vector<size_t> acks;
    for (size_t i = 0; i < blocks; ++i) {
        if (!b.take_irq((block + 2) * k.codes * period)) break;
        acks.push_back(b.dack_times.size());
    }
    bool every_block = acks.size() == blocks;
    for (size_t i = 0; every_block && i < blocks; ++i) every_block = acks[i] == (i + 1) * block;
    check(name + ": an IRQ every 12 bytes, 4 times", every_block, std::to_string(acks.size()) + " IRQs");
    std::vector<uint8_t> stream;
    for (size_t i = 0; i < blocks * block; ++i) stream.push_back(bytes[i % buffer]);
    AdpcmReference reference;
    std::vector<uint8_t> want;
    reference.play(stream, true, k, want);
    want.resize(want.size() - k.codes);          // the last byte's later codes play after the IRQ
    std::vector<uint8_t> got = b.dack_times.empty() ? std::vector<uint8_t>()
                               : dac_periods(b, b.dack_times[0], period, want.size(), 0x80);
    check(name + ": reference byte only at the start; the DAC follows the reference decoder", got == want,
          first_difference(got, want));
    b.dsp_write(0xda);
    bool last = b.take_irq((block + 2) * k.codes * period);
    uint64_t before = b.link.dacks;
    b.run((block + 2) * k.codes * period);
    check(name + ": DAh ends it after the block", last && b.link.dacks == before && !b.top.intr,
          std::to_string(b.link.dacks - before) + " more acknowledges");
}

void adpcm() {
    adpcm_single(adpcm4, 0x75, 0x74);
    adpcm_single(adpcm3, 0x77, 0x76);
    adpcm_single(adpcm2, 0x17, 0x16);
    adpcm_auto(adpcm4, 0x7d);
    adpcm_auto(adpcm3, 0x7f);
    adpcm_auto(adpcm2, 0x1f);
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
    adpcm();
    opl2_timers();
    mixer();
    std::printf("%s %d failure(s)\n", failures ? "FAIL" : "PASS", failures);
    return failures ? 1 : 0;
}
