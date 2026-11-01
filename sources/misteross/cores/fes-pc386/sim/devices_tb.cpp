// SPDX-License-Identifier: MIT
// Directed checks of the pc386 AT core through its CPU I/O port
// (docs/pc386-machine-design.md, "Verification", item 1). No CPU: each check
// writes and reads ports as a driver would and compares with the data sheet.
// Built by scripts/sim_fes_pc386.py with RTC_N/RTC_D = 1/16, so one RTC second
// is 2^19 clocks; every other rate is the 100 MHz default.
//
// Prints PASS <name> or FAIL <name>: <detail> per check; exits 1 on a failure.
#include "Vpc386_at_core.h"
#include "verilated.h"

#include <algorithm>
#include <array>
#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <functional>
#include <string>
#include <utility>
#include <vector>

namespace {

constexpr double CLOCK_HZ = 100e6;
constexpr uint64_t RTC_SECOND = 1u << 19;
constexpr uint16_t CYLINDERS = 40, HEADS = 4, SPT = 17;  // 2,720 sectors: dirty map words 0 and 1
constexpr uint32_t SECTORS = uint32_t(CYLINDERS) * HEADS * SPT;

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
    uint64_t cycles = 0;
    std::vector<uint8_t> image = std::vector<uint8_t>(size_t(SECTORS) * 512);
    std::vector<uint8_t> memory = std::vector<uint8_t>(16u << 20);
    std::vector<uint32_t> dma_reads;               // CPU-port addresses read by DMA
    std::vector<uint8_t> dma_device_bytes;         // bytes DACKed to the device
    bool pending_read = false, dma_pending = false, ack_pending = false, vga_answered = false;
    uint64_t pending_address = 0;
    int64_t held_read_lba = -1;
    std::function<void()> each_clock;
    // The fes.pc386.machine record's CMOS words, as fes_computer_storage keeps
    // them: the RTC's byte writes, and its load a clock after each read.
    uint16_t record[64] = {};
    unsigned written = 0;  // disk_written pulses

    Bench() {
        top.reset = 1;
        top.disk_present = 1;
        top.disk_sectors = SECTORS;
        top.disk_cylinders = CYLINDERS;
        top.disk_heads = HEADS;
        top.disk_spt = SPT;
        top.base_kib = 640;
        top.extended_kib = 15360;
        top.sb_base_high = 0x22; top.sb_irq = 7; top.sb_dma = 1;
        for (int n = 0; n < 4; ++n) clock();
        top.reset = 0;
        while (top.cmos_busy) clock();
    }

    void clock() {
        top.disk_rvalid = pending_read &&
                          (held_read_lba < 0 || pending_address / 64 != uint64_t(held_read_lba));
        if (top.disk_rvalid) {
            std::memcpy(&top.disk_rdata, &image[pending_address * 8], 8);
            pending_read = false;
        }
        top.disk_ready = top.disk_valid;
        if (top.disk_valid && top.disk_ready) {
            if (top.disk_write) {
                uint64_t value = top.disk_wdata;
                std::memcpy(&image[size_t(top.disk_address) * 8], &value, 8);
            } else { pending_read = true; pending_address = top.disk_address; }
        }
        top.dma_done = dma_pending;
        dma_pending = false;
        if (top.dma_valid && !top.dma_done) {
            dma_pending = true;
            if (top.dma_write) memory[top.dma_address] = top.dma_wdata;
            else { top.dma_rdata = memory[top.dma_address]; dma_reads.push_back(top.dma_address); }
        }
        top.dma_ack_done = ack_pending;
        ack_pending = false;
        if (top.dma_ack_valid && !top.dma_ack_done) {
            ack_pending = true;
            if (top.dma_ack_write) dma_device_bytes.push_back(top.dma_ack_wdata);
            top.dma_ack_rdata = 0x5a;
        }
        top.vga_done = top.vga_valid && !vga_answered;
        top.vga_rdata = 0x42;
        vga_answered = top.vga_valid && top.vga_done;
        if (each_clock) each_clock();
        uint16_t loaded = record[top.record_pair & 63];
        if (top.record_write) {
            unsigned shift = 8 * (top.record_index & 1);
            record[top.record_index >> 1] = uint16_t(record[top.record_index >> 1] & ~(0xff << shift) |
                                                     top.record_byte << shift);
        }
        written += top.disk_written;
        top.clk = 1; top.eval();
        top.clk = 0; top.eval();
        top.record_word = loaded;
        ++cycles;
    }
    void run(uint64_t n) { for (uint64_t i = 0; i < n; ++i) clock(); }
    bool wait(const std::function<bool()> &condition, uint64_t limit) {
        for (uint64_t i = 0; i < limit; ++i) {
            if (condition()) return true;
            clock();
        }
        return condition();
    }

    uint64_t last_io = 0;
    uint32_t io(bool write, uint16_t port, unsigned size, uint32_t value = 0) {
        while (top.io_busy) clock();
        top.io_valid = 1; top.io_write = write; top.io_port = port;
        top.io_size = size == 1 ? 0 : size == 2 ? 1 : 2; top.io_wdata = value;
        uint64_t start = cycles;
        clock();
        top.io_valid = 0;
        while (!top.io_done) clock();
        last_io = cycles - start;
        return size == 4 ? top.io_rdata : top.io_rdata & ((1u << (8 * size)) - 1);
    }
    uint8_t in(uint16_t port) { return uint8_t(io(false, port, 1)); }
    // Polls the 8042 status for a full output buffer for up to `clocks`.
    bool output_within(uint64_t clocks) {
        for (uint64_t end = cycles + clocks; cycles < end;)
            if (in(0x64) & 1) return true;
        return false;
    }
    uint16_t inw(uint16_t port) { return uint16_t(io(false, port, 2)); }
    void out(uint16_t port, uint8_t value) { io(true, port, 1, value); }
    void outw(uint16_t port, uint16_t value) { io(true, port, 2, value); }
    uint8_t inta() {
        while (top.io_busy) clock();
        top.inta_valid = 1;
        clock();
        top.inta_valid = 0;
        while (!top.inta_done) clock();
        return top.inta_vector;
    }
    uint8_t cmos(uint8_t index) { out(0x70, index); return in(0x71); }
    void cmos_write(uint8_t index, uint8_t value) { out(0x70, index); out(0x71, value); }
    // fes.keyboard.hid: usages 00h-7Fh are bits 0-127, the modifiers E0h-E7h
    // bits 128-135.
    void key(uint8_t usage, bool down) {
        unsigned bit = usage >= 0xe0 ? 128u + (usage - 0xe0) : usage;
        if (down) top.keyboard_rows[bit / 32] |= 1u << (bit % 32);
        else top.keyboard_rows[bit / 32] &= ~(1u << (bit % 32));
    }
    // Every byte the 8042 delivers until none has come for `quiet` clocks.
    std::vector<uint8_t> drain(uint64_t quiet = 300000) {
        std::vector<uint8_t> bytes;
        while (output_within(quiet)) bytes.push_back(in(0x60));
        return bytes;
    }
};

std::string hex_bytes(const std::vector<uint8_t> &bytes) {
    std::string text;
    for (uint8_t byte : bytes) text += (text.empty() ? "" : " ") + hex(byte);
    return text.empty() ? "-" : text;
}

void timing(Bench &b) {
    b.out(0x80, 0x12);
    uint64_t byte = b.last_io;
    b.io(false, 0x61, 2);
    uint64_t word = b.last_io;
    b.io(false, 0x61, 4);
    uint64_t dword = b.last_io;
    b.io(false, 0x1f0, 4);
    uint64_t sized = b.last_io;
    b.io(false, 0x3c4, 2);
    uint64_t vga = b.last_io;
    check("timing isa8 byte, word, dword = 72, 144, 288 clocks + 2", byte == 74 && word == 146 && dword == 290,
          std::to_string(byte) + " " + std::to_string(word) + " " + std::to_string(dword));
    check("timing dword at 1F0h: a 16-bit cycle, then bytes at 1F2h and 1F3h (36 + 72 + 72 + 2)", sized == 182,
          std::to_string(sized));
    check("timing vga word: two posted bytes, no ISA wait", vga == 5, std::to_string(vga));
    check("port 80h reads back the POST code", b.in(0x80) == 0x12);
    check("unclaimed port reads FFh at isa8", b.in(0x2f8) == 0xff && b.last_io == 74);
}

void pic_init(Bench &b) {
    b.out(0x20, 0x11); b.out(0x21, 0x08); b.out(0x21, 0x04); b.out(0x21, 0x01);
    b.out(0xa0, 0x11); b.out(0xa1, 0x70); b.out(0xa1, 0x02); b.out(0xa1, 0x01);
}

// KF8259 holds INT once raised until it is acknowledged, so a request seen
// during initialisation and then masked leaves a spurious IR7 to take.
void clear_stale_interrupt(Bench &b) {
    b.run(20);
    if (b.top.intr) b.inta();
}

void pic_and_rtc_interrupts(Bench &b) {
    pic_init(b);
    b.out(0x21, 0xfb);  // master: only IR2, the cascade
    b.out(0xa1, 0xfe);  // slave: only IRQ8
    check("PIC masks read back", b.in(0x21) == 0xfb && b.in(0xa1) == 0xfe);
    b.cmos_write(0x0b, 0x42);  // PIE, 24-hour, BCD
    bool raised = b.wait([&] { return b.top.intr; }, 8 * 2048);
    uint8_t vector = raised ? b.inta() : 0;
    check("IRQ8 (RTC periodic) arrives through master IR2 as vector 70h", raised && vector == 0x70, hex(vector));
    uint8_t flags = b.cmos(0x0c);
    check("RTC C reports IRQF and PF, and a read clears them", (flags & 0xc0) == 0xc0 && (b.cmos(0x0c) & 0xc0) == 0, hex(flags));
    b.out(0xa0, 0x20); b.out(0x20, 0x20);
    b.out(0xa0, 0x0b);
    check("EOI clears the slave's in-service bit", b.in(0xa0) == 0);
    b.cmos_write(0x0b, 0x02);
    b.out(0xa1, 0xbf);  // only IRQ14
    b.out(0x3f6, 0x00);  // nIEN clear
    b.out(0x1f6, 0xa0);
    b.out(0x1f7, 0x90);  // EXECUTE DEVICE DIAGNOSTIC
    raised = b.wait([&] { return b.top.intr; }, 1000);
    vector = raised ? b.inta() : 0;
    check("IRQ14 (ATA) arrives as vector 76h", raised && vector == 0x76, hex(vector));
    b.in(0x1f7);
    b.out(0xa0, 0x20); b.out(0x20, 0x20);
    b.out(0xa1, 0xff);
    b.out(0x21, 0xff);
}

void pit(Bench &b) {
    pic_init(b);
    b.out(0x21, 0xfe);
    b.out(0xa1, 0xff);
    // Channel 0, mode 2, divisor 0 (65536): 54.9254 ms.
    b.out(0x43, 0x34); b.out(0x40, 0x00); b.out(0x40, 0x00);
    std::vector<uint64_t> edges;
    for (int n = 0; n < 3; ++n) {
        if (!b.wait([&] { return b.top.intr; }, 7000000)) break;
        edges.push_back(b.cycles);
        b.inta();
        b.out(0x20, 0x20);
    }
    double expected = 65536.0 / 1193181.818 * CLOCK_HZ;
    bool ok = edges.size() == 3;
    std::string detail;
    for (size_t n = 1; n < edges.size(); ++n) {
        double period = double(edges[n] - edges[n - 1]);
        detail += std::to_string(edges[n] - edges[n - 1]) + " ";
        // One PIT clock is 83.8 system clocks.
        ok &= period > expected - 90 && period < expected + 90;
    }
    check("PIT channel 0 divisor 65536 gives IRQ0 every 54.925 ms (5,492,541 clocks)", ok, detail);
    b.out(0x21, 0xff);

    // 8254 read-back: status then count of channel 0.
    b.out(0x43, 0xc2);
    uint8_t status = b.in(0x40);
    uint8_t low = b.in(0x40), high = b.in(0x40);
    check("8254 read-back status: mode 2, LSB/MSB, binary, count loaded", (status & 0x7f) == 0x34, hex(status));
    check("8254 read-back count follows the status", uint16_t(low | high << 8) != 0, hex(low | high << 8));
    b.out(0x43, 0xb0);  // channel 2, mode 0, LSB/MSB
    b.out(0x43, 0xe8);  // read-back status of channel 2
    status = b.in(0x42);
    check("8254 null count is set until a count is loaded", (status & 0x7f) == 0x70, hex(status));
    b.out(0x42, 0x10); b.out(0x42, 0x00);
    b.out(0x61, 0x01);  // gate channel 2
    b.run(400);
    b.out(0x43, 0xe8);
    status = b.in(0x42);
    check("8254 null count clears once the counter has the count", (status & 0x40) == 0, hex(status));
    b.out(0x43, 0xd8);  // read-back count only, channel 2
    low = b.in(0x42); high = b.in(0x42);
    check("8254 read-back count latch alone", uint16_t(low | high << 8) < 0x10, hex(low | high << 8));
    // Port 61h bit 4 toggles with channel 1 (BIOS programs 15 us).
    b.out(0x43, 0x54); b.out(0x41, 18);
    int toggles = 0;
    uint8_t last = b.in(0x61) & 0x10;
    uint64_t start = b.cycles;
    while (b.cycles - start < 150000) {
        uint8_t now = b.in(0x61) & 0x10;
        if (now != last) ++toggles;
        last = now;
    }
    // 1.5 ms at a 15.09 us rising-edge rate: 99 toggles.
    check("port 61h bit 4 toggles every 15.09 us with channel 1 at 18", toggles >= 96 && toggles <= 102,
          std::to_string(toggles));
    b.out(0x61, 0x00);
}

void dma(Bench &b) {
    for (int port = 0x81; port <= 0x8f; ++port) b.out(uint16_t(port), uint8_t(port ^ 0x5a));
    bool pages = true;
    for (int port = 0x81; port <= 0x8f; ++port) pages &= b.in(uint16_t(port)) == uint8_t(port ^ 0x5a);
    check("DMA page registers 81h-8Fh read back", pages);
    b.out(0x0d, 0);          // master clear
    b.out(0x0c, 0);          // clear the byte pointer
    b.out(0x02, 0x00); b.out(0x02, 0x34);  // channel 1 address 3400h
    b.out(0x03, 0x03); b.out(0x03, 0x00);  // four bytes
    b.out(0x83, 0x12);       // channel 1 page: 123400h
    b.out(0x0b, 0x59);       // single, increment, auto-init, read (memory to device), channel 1
    for (int n = 0; n < 4; ++n) b.memory[0x123400 + n] = uint8_t(0xa0 + n);
    b.out(0x0a, 0x01);       // unmask channel 1
    b.dma_reads.clear(); b.dma_device_bytes.clear();
    b.top.dma_drq = 0x1;     // DREQ1 (bit 0 is channel 1)
    b.wait([&] { return b.dma_device_bytes.size() >= 5; }, 20000);
    b.top.dma_drq = 0;
    bool addresses = b.dma_reads.size() >= 5 && b.dma_reads[0] == 0x123400 && b.dma_reads[3] == 0x123403 &&
                     b.dma_reads[4] == 0x123400;
    check("DMA channel 1 reads 123400h-123403h through the CPU port, then auto-init restarts it", addresses,
          b.dma_reads.empty() ? "none" : hex(b.dma_reads[0]) + ".." + hex(b.dma_reads.back()));
    check("DMA bytes reach the device's DACK in order",
          b.dma_device_bytes.size() >= 4 && b.dma_device_bytes[0] == 0xa0 && b.dma_device_bytes[3] == 0xa3);
    uint8_t status = b.in(0x08);
    check("DMA status shows channel 1 terminal count", status & 0x02, hex(status));
    b.out(0x0c, 0);
    b.out(0xc4, 0x78); b.out(0xc4, 0x56);  // controller 2 channel 5 address
    b.out(0xd8, 0);
    check("DMA controller 2 registers read back at C0h + 2n", b.in(0xc4) == 0x78 && b.in(0xc4) == 0x56);
}

void kbc(Bench &b) {
    b.out(0x64, 0xaa);
    b.wait([&] { return (b.in(0x64) & 1) != 0; }, 100000);
    check("8042 self-test returns 55h", b.in(0x60) == 0x55);
    b.out(0x64, 0x20);
    uint8_t command = b.in(0x60);
    check("8042 command byte after self-test: XLAT, aux disabled, SYS, both IRQs", command == 0x67, hex(command));

    check("A20 is on at power-on", !b.top.a20m);
    b.out(0x64, 0xd1);
    b.out(0x60, 0xdd);
    // IBF clears only once A20 has changed.
    bool ordered = true;
    for (int n = 0; n < 4; ++n) {
        uint8_t status = b.in(0x64);
        if (!(status & 0x02) && !b.top.a20m) ordered = false;
    }
    check("D1 with bit 1 clear turns A20 off before IBF clears", b.top.a20m && ordered);
    b.out(0x92, 0x02);
    check("port 92h bit 1 turns A20 on with the 8042 bit clear", !b.top.a20m);
    b.out(0x92, 0x00);
    b.out(0x64, 0xdf);
    check("8042 DF turns A20 on", !b.top.a20m);
    b.out(0x64, 0xdd);
    check("8042 DD turns A20 off", b.top.a20m);
    b.out(0x64, 0xdf);

    // CPU reset waits for the CPU's FLUSH.
    bool reset_early = false, reset_seen = false;
    b.top.flush_done = 0;
    b.each_clock = [&] { if (b.top.cpu_reset) reset_seen = true; };
    b.out(0x64, 0xfe);
    b.run(200);
    reset_early = reset_seen;
    bool flushing = b.top.flush_request;
    b.top.flush_done = 1;
    b.run(4);
    b.top.flush_done = 0;
    check("8042 FE asks for FLUSH and resets the CPU only after it", flushing && !reset_early && reset_seen);
    reset_seen = false;
    b.out(0x92, 0x01);
    bool fast = b.top.flush_request;
    b.top.flush_done = 1; b.run(4); b.top.flush_done = 0;
    check("port 92h bit 0 rising edge resets the CPU through FLUSH", fast && reset_seen);
    b.out(0x92, 0x00);
    b.each_clock = nullptr;

    // Keyboard reset: FA after a PS/2 byte time, then AA after BAT.
    b.out(0x60, 0xff);
    uint64_t sent = b.cycles;
    b.wait([&] { return (b.in(0x64) & 1) != 0; }, 2000000);
    uint64_t ack_time = b.cycles - sent;
    uint8_t ack = b.in(0x60);
    b.key(0x04, true);
    b.run(300000);  // a key changes during the 5 ms BAT window
    bool bat_ready = b.wait([&] { return (b.in(0x64) & 1) != 0; }, 2000000);
    uint8_t bat = b.in(0x60);
    bool key_ready = b.wait([&] { return (b.in(0x64) & 1) != 0; }, 300000);
    uint8_t after_bat = b.in(0x60);
    // The byte time is BYTE_US ticks of the 1 us enable, so it is 1 ms less
    // up to one tick (100 clocks), as the enable's phase falls.
    check("keyboard FF: FA, BAT AA before a key changed during self-test", ack == 0xfa && bat_ready && bat == 0xaa &&
          key_ready && after_bat == 0x1e && ack_time > 100000 - 100,
          hex(ack) + " " + hex(bat) + " " + hex(after_bat) + " " + std::to_string(ack_time));
    b.key(0x04, false);
    b.drain();

    // Translation and IRQ1, from the host keyboard model's rows: the change
    // is taken 1 ms after the rows last changed.
    pic_init(b);
    b.out(0x21, 0xfd);
    uint64_t pressed_at = b.cycles;
    b.key(0x04, true);
    bool early = b.wait([&] { return b.top.intr; }, 99000);
    bool raised = b.wait([&] { return b.top.intr; }, 300000);
    uint64_t make_at = b.cycles - pressed_at;
    uint8_t vector = raised ? b.inta() : 0;
    uint8_t make = b.in(0x60);
    b.out(0x20, 0x20);
    b.key(0x04, false);
    b.wait([&] { return (b.in(0x64) & 1) != 0; }, 300000);
    uint8_t brk = b.in(0x60);
    check("HID A (04h) arrives as set 1 1Eh on IRQ1 once the rows have settled for 1 ms, its break as 9Eh",
          !early && raised && make_at < 110000 && vector == 0x09 && make == 0x1e && brk == 0x9e,
          hex(vector) + " " + hex(make) + " " + hex(brk) + " after " + std::to_string(make_at));
    b.out(0x21, 0xff);

    // E0 keys, Print Screen and Pause through XLAT.
    auto press = [&](uint8_t usage, bool down) { b.key(usage, down); return b.drain(); };
    std::vector<uint8_t> up_make = press(0x52, true), up_break = press(0x52, false);
    check("Up arrives as E0 48, its break as E0 C8",
          up_make == std::vector<uint8_t>{0xe0, 0x48} && up_break == std::vector<uint8_t>{0xe0, 0xc8},
          hex_bytes(up_make) + " / " + hex_bytes(up_break));
    std::vector<uint8_t> print_make = press(0x46, true), print_break = press(0x46, false);
    check("Print Screen arrives as E0 2A E0 37, its break as E0 B7 E0 AA",
          print_make == std::vector<uint8_t>{0xe0, 0x2a, 0xe0, 0x37} &&
              print_break == std::vector<uint8_t>{0xe0, 0xb7, 0xe0, 0xaa},
          hex_bytes(print_make) + " / " + hex_bytes(print_break));
    std::vector<uint8_t> pause_make = press(0x48, true), pause_break = press(0x48, false);
    check("Pause arrives as E1 1D 45 E1 9D C5, with nothing at its release",
          pause_make == std::vector<uint8_t>{0xe1, 0x1d, 0x45, 0xe1, 0x9d, 0xc5} && pause_break.empty(),
          hex_bytes(pause_make) + " / " + hex_bytes(pause_break));
    b.key(0xe1, true);
    b.key(0x1e, true);
    std::vector<uint8_t> shifted = b.drain();
    b.key(0xe1, false);
    b.key(0x1e, false);
    std::vector<uint8_t> unshifted = b.drain();
    check("Shift and 1 written together arrive Shift first and leave Shift last",
          shifted == std::vector<uint8_t>{0x2a, 0x02} && unshifted == std::vector<uint8_t>{0x82, 0xaa},
          hex_bytes(shifted) + " / " + hex_bytes(unshifted));

    // Typematic through the 8042: F3 00h is 250 ms then 30 per second; F6
    // restores 500 ms and 10.9 per second. The guest polls 60h, so each
    // byte is seen within the 8042's pacing and a poll (1.1 ms).
    auto repeat_times = [&](unsigned count) {
        std::vector<uint64_t> at;
        b.key(0x04, true);
        while (at.size() < count && b.output_within(110000000)) {
            at.push_back(b.cycles);
            b.in(0x60);
        }
        b.key(0x04, false);
        b.drain();
        return at;
    };
    auto typematic_ok = [](const std::vector<uint64_t> &at, double delay_ms, double period_ms, std::string &detail) {
        if (at.size() < 4) { detail = std::to_string(at.size()) + " bytes"; return false; }
        double delay = (at[1] - at[0]) / 1e5;
        bool ok = std::abs(delay - delay_ms) < 1.1;
        detail = "delay " + std::to_string(delay) + " ms, periods";
        for (size_t n = 2; n < at.size(); ++n) {
            double period = (at[n] - at[n - 1]) / 1e5;
            ok &= std::abs(period - period_ms) < 1.1;
            detail += " " + std::to_string(period);
        }
        return ok;
    };
    b.out(0x60, 0xf3);
    uint8_t f3_ack = b.drain()[0];
    b.out(0x60, 0x00);
    uint8_t rate_ack = b.drain()[0];
    std::string fast_detail, slow_detail;
    bool quick = typematic_ok(repeat_times(5), 250, 1000.0 / 30, fast_detail);
    check("keyboard F3 00h: A repeats after 250 ms at 30 per second", f3_ack == 0xfa && rate_ack == 0xfa && quick,
          fast_detail);
    b.out(0x60, 0xf6);
    uint8_t f6_ack = b.drain()[0];
    bool slow = typematic_ok(repeat_times(4), 500, 1000.0 / 240 * 22, slow_detail);
    check("keyboard F6 restores the typematic default, 500 ms and 10.9 per second", f6_ack == 0xfa && slow,
          slow_detail);
    b.out(0x60, 0xed);
    b.drain();
    b.out(0x60, 0x07);
    b.drain();
    std::string led_detail;
    bool leds = typematic_ok(repeat_times(4), 500, 1000.0 / 240 * 22, led_detail);
    check("an ED LED byte does not change the typematic setting", leds, led_detail);

    // F5 stops scanning and F6 starts it again, as Bochs's keyboard has them.
    auto first = [](const std::vector<uint8_t> &bytes) { return bytes.empty() ? 0 : int(bytes[0]); };
    b.key(0x04, true);
    std::vector<uint8_t> prior_make = b.drain();
    b.out(0x60, 0xf5);
    int f5_reply = first(b.drain());
    b.key(0x04, false);
    std::vector<uint8_t> straddling_break = b.drain();
    b.key(0x05, true);
    std::vector<uint8_t> unscanned_make = b.drain();
    b.key(0x05, false);
    std::vector<uint8_t> unscanned_break = b.drain();
    b.out(0x60, 0xf6);
    std::vector<uint8_t> f6_response = b.drain();
    int f6_reply = first(f6_response);
    std::vector<uint8_t> resumed_break;
    if (!f6_response.empty()) resumed_break.insert(resumed_break.end(), f6_response.begin() + 1, f6_response.end());
    std::vector<uint8_t> after_f6 = b.drain();
    resumed_break.insert(resumed_break.end(), after_f6.begin(), after_f6.end());
    b.key(0x05, true);
    std::vector<uint8_t> rescanned = b.drain();
    b.key(0x05, false);
    std::vector<uint8_t> rereleased = b.drain();
    check("F5 hides an in-window tap but F6 sends a straddling break",
          prior_make == std::vector<uint8_t>{0x1e} && f5_reply == 0xfa && straddling_break.empty() &&
              unscanned_make.empty() && unscanned_break.empty() && f6_reply == 0xfa &&
              resumed_break == std::vector<uint8_t>{0x9e} && rescanned == std::vector<uint8_t>{0x30} &&
              rereleased == std::vector<uint8_t>{0xb0},
          hex_bytes(prior_make) + " / " + hex(f5_reply) + " " + hex_bytes(straddling_break) + " " +
              hex_bytes(unscanned_make) + " / " + hex_bytes(unscanned_break) + " / " + hex(f6_reply) + " " +
              hex_bytes(resumed_break) + " " + hex_bytes(rescanned) + " / " + hex_bytes(rereleased));
    b.out(0x60, 0xf2);
    uint8_t id[3] = {};
    for (auto &byte : id) {
        b.wait([&] { return (b.in(0x64) & 1) != 0; }, 300000);
        byte = b.in(0x60);
    }
    check("keyboard F2 identifies as AB 83, translated to AB 41", id[0] == 0xfa && id[1] == 0xab && id[2] == 0x41,
          hex(id[0]) + " " + hex(id[1]) + " " + hex(id[2]));

    // Sixteen keys in one change: the 8042 holds the first make, the
    // keyboard's FIFO the next 13, and the model waits with two. F2's
    // three-byte reply fills the FIFO, EE's echo waits for a free slot, and
    // the last two makes follow it; nothing is dropped. Set 1 makes of
    // usages 04h-13h (A to P).
    static const uint8_t SET1[16] = {0x1e, 0x30, 0x2e, 0x20, 0x12, 0x21, 0x22, 0x23,
                                     0x17, 0x24, 0x25, 0x26, 0x32, 0x31, 0x18, 0x19};
    for (uint8_t usage = 0x04; usage < 0x14; ++usage) b.key(usage, true);
    b.run(300000);              // settled, scanned, and the FIFO full
    b.out(0x60, 0xf2);
    b.run(8);                   // the reply takes the last three slots
    b.out(0x60, 0xee);          // its echo waits while the FIFO is full
    std::vector<uint8_t> queued = b.drain();
    std::vector<uint8_t> expected(SET1, SET1 + 14);
    for (uint8_t byte : {uint8_t(0xfa), uint8_t(0xab), uint8_t(0x41), uint8_t(0xee), SET1[14], SET1[15]})
        expected.push_back(byte);
    check("keyboard keeps F2's reply intact behind a nearly full FIFO, and EE's echo waits for a slot",
          queued == expected, hex_bytes(queued));
    for (uint8_t usage = 0x04; usage < 0x14; ++usage) b.key(usage, false);
    std::vector<uint8_t> breaks = b.drain();
    bool all_broken = breaks.size() == 16;
    for (size_t n = 0; n < breaks.size() && all_broken; ++n) all_broken = breaks[n] == (SET1[n] | 0x80);
    check("the sixteen breaks follow in usage order", all_broken, hex_bytes(breaks));

    // Leave a controller byte in OBF so the host key make stays in the
    // keyboard FIFO. F5 clears it; the held key must be reconciled after F4.
    b.out(0x64, 0x20);
    b.run(100);
    b.key(0x04, true);
    b.run(200000);
    b.out(0x60, 0xf5);
    b.out(0x60, 0xf4);
    uint8_t old_obf = b.in(0x60);
    std::vector<uint8_t> replayed_make = b.drain();
    b.key(0x04, false);
    std::vector<uint8_t> replayed_break = b.drain();
    check("F5 FIFO clear retries a held make before its later break",
          old_obf != 0 && replayed_make == std::vector<uint8_t>{0xfa, 0xfa, 0x1e} &&
              replayed_break == std::vector<uint8_t>{0x9e},
          hex(old_obf) + " / " + hex_bytes(replayed_make) + " / " + hex_bytes(replayed_break));

    // Aux mouse: enable, then one movement packet with dy negated.
    b.out(0x64, 0xa8);
    b.out(0x64, 0xd4); b.out(0x60, 0xf4);
    b.wait([&] { return (b.in(0x64) & 1) != 0; }, 300000);
    uint8_t status = b.in(0x64);
    uint8_t mack = b.in(0x60);
    check("aux F4 is acknowledged through the aux output buffer", mack == 0xfa && (status & 0x20), hex(status));
    b.top.mouse_valid = 1; b.top.mouse_dx = 5; b.top.mouse_dy = 3; b.top.mouse_buttons = 1;
    b.clock();
    b.top.mouse_valid = 0;
    uint8_t packet[3] = {};
    for (auto &byte : packet) {
        b.wait([&] { return (b.in(0x64) & 1) != 0; }, 3000000);
        byte = b.in(0x60);
    }
    check("mouse packet: left button, dx +5, dy -3 (PS/2 Y is up)", packet[0] == 0x29 && packet[1] == 5 &&
          packet[2] == 0xfd, hex(packet[0]) + " " + hex(packet[1]) + " " + hex(packet[2]));
    for (int n = 0; n < 3; ++n) {
        b.top.mouse_valid = 1; b.top.mouse_dx = 127; b.top.mouse_dy = 0; b.top.mouse_buttons = 1;
        b.clock();
    }
    b.top.mouse_valid = 0;
    int total = 0;
    for (int n = 0; n < 2; ++n) {
        uint8_t p[3];
        for (auto &byte : p) {
            b.wait([&] { return (b.in(0x64) & 1) != 0; }, 3000000);
            byte = b.in(0x60);
        }
        total += (p[0] & 0x10) ? int(p[1]) - 256 : int(p[1]);
    }
    check("movement beyond 255 is carried into the next packet", total == 381, std::to_string(total));
    // The runtime's packets reach -128: host dy -128 (up) is PS/2 +128.
    bool ready = b.top.mouse_ready;
    b.top.mouse_valid = 1; b.top.mouse_dx = -128; b.top.mouse_dy = -128; b.top.mouse_buttons = 0;
    b.clock();
    b.top.mouse_valid = 0;
    uint8_t extreme[3] = {};
    for (auto &byte : extreme) {
        b.wait([&] { return (b.in(0x64) & 1) != 0; }, 3000000);
        byte = b.in(0x60);
    }
    check("the aux device takes packets out of reset; dx -128 and dy -128 arrive as -128 and +128",
          ready && extreme[0] == 0x18 && extreme[1] == 0x80 && extreme[2] == 0x80,
          hex(extreme[0]) + " " + hex(extreme[1]) + " " + hex(extreme[2]));

    auto mouse_command = [&](uint8_t command) {
        b.out(0x64, 0xd4); b.out(0x60, command);
        if (!b.wait([&] { return (b.in(0x64) & 1) != 0; }, 300000)) return uint8_t(0);
        return b.in(0x60);
    };
    auto mouse_packet = [&] {
        uint8_t result[3] = {};
        for (auto &byte : result) {
            b.wait([&] { return (b.in(0x64) & 1) != 0; }, 3000000);
            byte = b.in(0x60);
        }
        return std::array<uint8_t, 3>{result[0], result[1], result[2]};
    };
    b.out(0x64, 0xa8);
    uint8_t mouse_reset = mouse_command(0xff);
    uint8_t reset_id = 0, reset_type = 0;
    b.wait([&] { return (b.in(0x64) & 1) != 0; }, 300000);
    reset_id = b.in(0x60);
    b.wait([&] { return (b.in(0x64) & 1) != 0; }, 300000);
    reset_type = b.in(0x60);
    mouse_command(0xe8); mouse_command(0x00);  // resolution 0: one quarter count per host count
    mouse_command(0xf0);                      // remote mode; EB asks for each packet
    b.top.mouse_valid = 1; b.top.mouse_dx = -1; b.top.mouse_dy = 1; b.top.mouse_buttons = 1;
    b.clock(); b.top.mouse_valid = 0;
    uint8_t eb_ack = mouse_command(0xeb);
    auto negative = mouse_packet();
    b.top.mouse_valid = 1; b.top.mouse_dx = 1; b.top.mouse_dy = -1; b.top.mouse_buttons = 1;
    b.clock(); b.top.mouse_valid = 0;
    mouse_command(0xeb);
    auto cancelled = mouse_packet();
    check("resolution 0 carries negative dx/dy fractions without early packets",
          mouse_reset == 0xfa && reset_id == 0xaa && reset_type == 0x00 && eb_ack == 0xfa &&
          (negative[0] & 0x30) == 0 && negative[1] == 0 && negative[2] == 0 &&
          (cancelled[0] & 0x30) == 0 && cancelled[1] == 0 && cancelled[2] == 0,
          hex(negative[0]) + " " + hex(negative[1]) + " " + hex(negative[2]) + "; " +
          hex(cancelled[0]) + " " + hex(cancelled[1]) + " " + hex(cancelled[2]));

    b.top.mouse_valid = 1; b.top.mouse_dx = -4; b.top.mouse_dy = 0; b.top.mouse_buttons = 1;
    b.clock(); b.top.mouse_valid = 0;
    mouse_command(0xeb);
    auto whole_negative = mouse_packet();
    check("resolution 0 reports four negative quarter-counts as -1",
          (whole_negative[0] & 0x10) != 0 && whole_negative[1] == 0xff,
          hex(whole_negative[0]) + " " + hex(whole_negative[1]));

    mouse_command(0xf6);
    b.run(11000000);  // let any preceding sample interval expire
    mouse_command(0xf3); mouse_command(10); mouse_command(0xf4);
    while (b.in(0x64) & 1) b.in(0x60);  // discard any earlier device reply before timing packets
    auto move = [&](int dx) {
        b.top.mouse_valid = 1; b.top.mouse_dx = int8_t(dx); b.top.mouse_dy = 0; b.top.mouse_buttons = 1;
        b.clock();
        b.top.mouse_valid = 0;
    };
    mouse_command(0xe8); mouse_command(0x00);  // resolution 0: a count is four host counts
    move(-1);
    bool quiet = !b.output_within(3000000);  // longer than a 10 ms sample period
    move(1);
    quiet &= !b.output_within(3000000);
    move(-4);
    uint8_t q[3] = {};
    for (auto &byte : q) {
        b.output_within(3000000);
        byte = b.in(0x60);
    }
    check("at resolution 0 a quarter-count move and its reverse send nothing; four send -1",
          quiet && q[0] == 0x19 && q[1] == 0xff, hex(q[0]) + " " + hex(q[1]));
    mouse_command(0xe8); mouse_command(0x02);
    mouse_command(0xf3); mouse_command(10);
    std::vector<uint64_t> packets;
    const uint64_t sampling_from = b.cycles;
    while (packets.size() < 2) {
        move(4);
        uint64_t polled = b.cycles;
        if (b.output_within(20000)) {
            packets.push_back(polled);
            for (int n = 0; n < 3; ++n) {
                b.output_within(300000);
                b.in(0x60);
            }
        }
        if (packets.empty() && b.cycles - sampling_from > 40000000) break;
    }
    uint64_t gap = packets.size() == 2 ? packets[1] - packets[0] : 0;
    // Detected within one 20,000-clock poll window of the packet.
    check("sample rate 10: stream packets 100 ms apart", gap >= 9980000 && gap < 10040000, std::to_string(gap));
    mouse_command(0xf3); mouse_command(100);

    // A click while the guest is not reading. With the 8042's aux interface
    // off, four moves fill the device's FIFO (12 bytes, leaving the four a
    // command reply needs) and a fifth waits in the sums. A button-down is
    // taken; the button-up after it waits (mouse_ready low) until the
    // pending down can be queued, and the guest sees both edges and every
    // count. The mouse is
    // reset first: the sample-rate check leaves movement to report.
    mouse_command(0xff);
    while (b.output_within(3000000)) b.in(0x60);  // AA 00
    mouse_command(0xf4);
    while (b.output_within(3000000)) b.in(0x60);
    b.run(11000000);  // the 10 per second check's sample interval runs out
    struct Offer { bool pending = false; int dx = 0, buttons = 0; } offer;
    b.each_clock = [&] {
        b.top.mouse_valid = offer.pending;
        if (!offer.pending) return;
        b.top.mouse_dx = int8_t(offer.dx); b.top.mouse_dy = 0; b.top.mouse_buttons = uint8_t(offer.buttons);
        b.top.eval();
        if (b.top.mouse_ready) offer.pending = false;  // taken on this clock's edge
    };
    auto send = [&](int dx, int buttons) { offer = {true, dx, buttons}; };
    b.out(0x64, 0xa7);
    for (int n = 0; n < 5; ++n) { send(1, 0); b.run(1200000); }  // a packet per 10 ms sample
    send(0, 1);
    b.run(100000);
    bool down_taken = !offer.pending;
    send(0, 0);
    b.run(300000);
    bool up_held = offer.pending;
    b.out(0x64, 0xa8);
    std::vector<uint8_t> clicked;
    while (b.output_within(3000000)) clicked.push_back(b.in(0x60));
    bool up_taken = !offer.pending;
    b.each_clock = nullptr;
    b.top.mouse_valid = 0;
    std::vector<uint8_t> want;
    for (int n = 0; n < 4; ++n) for (uint8_t byte : {0x08, 0x01, 0x00}) want.push_back(byte);
    for (uint8_t byte : {0x09, 0x01, 0x00, 0x08, 0x00, 0x00}) want.push_back(byte);
    check("a click while the aux FIFO is full: the up waits for room, and the guest sees both edges",
          down_taken && up_held && up_taken && clicked == want,
          std::to_string(down_taken) + std::to_string(up_held) + std::to_string(up_taken) + " " + hex_bytes(clicked));

    b.out(0x64, 0xa7);
    b.run(200000);
    while (b.in(0x64) & 1) b.in(0x60);

    // A guest that never reads the aux FIFO must not keep the mailbox's
    // button-up acknowledgement pending beyond the runtime's 100 ms limit.
    for (int n = 0; n < 4; ++n) {
        b.top.mouse_valid = 1; b.top.mouse_dx = 1; b.top.mouse_dy = 0; b.top.mouse_buttons = 0;
        b.top.eval();
        if (!b.top.mouse_ready) b.run(5000000);
        b.clock();
        b.top.mouse_valid = 0;
        b.run(1100000);  // sample one three-byte packet into the undrained FIFO
    }
    b.top.mouse_valid = 1; b.top.mouse_dx = 0; b.top.mouse_dy = 0; b.top.mouse_buttons = 1;
    b.top.eval();
    bool down_ready = b.top.mouse_ready;
    b.clock();
    b.top.mouse_valid = 0;
    b.run(1100000);  // leave the down pending because only four FIFO slots remain
    b.top.mouse_valid = 1; b.top.mouse_dx = 0; b.top.mouse_dy = 0; b.top.mouse_buttons = 0;
    b.top.eval();
    bool up_blocked = !b.top.mouse_ready;
    uint64_t wait_from = b.cycles;
    while (!b.top.mouse_ready && b.cycles - wait_from < 5200000) b.clock();
    uint64_t wait_clocks = b.cycles - wait_from;
    bool up_ready = b.top.mouse_ready;
    if (up_ready) b.clock();
    b.top.mouse_valid = 0;
    check("a full aux FIFO bounds a click wait to 50 ms", down_ready && up_blocked && up_ready &&
          wait_clocks >= 4900000 && wait_clocks <= 5100000, std::to_string(wait_clocks));

    // A full scan-code queue still leaves room for a command's reply: with
    // the keyboard interface disabled, 13 of 20 makes are queued when F2
    // (which enables it again) is sent; the other 7 follow its reply.
    b.out(0x64, 0xad);
    for (uint8_t usage = 0x04; usage < 0x18; ++usage) b.key(usage, true);
    b.run(300000);
    b.out(0x60, 0xf2);
    std::vector<uint8_t> drained = b.drain(3000000);
    size_t n = drained.size();
    check("an F2 reply fits behind a full scan-code queue", n == 23 && drained[13] == 0xfa &&
          drained[14] == 0xab && drained[15] == 0x41, hex_bytes(drained));
    for (uint8_t usage = 0x04; usage < 0x18; ++usage) b.key(usage, false);
    b.drain(3000000);
}

void rtc(Bench &b) {
    bool config = b.cmos(0x10) == 0x00 && b.cmos(0x12) == 0xf0 && b.cmos(0x14) == 0x04 && b.cmos(0x15) == 0x80 &&
                  b.cmos(0x16) == 0x02 && b.cmos(0x17) == 0x00 && b.cmos(0x18) == 0x3c && b.cmos(0x19) == 47 &&
                  b.cmos(0x1b) == CYLINDERS && b.cmos(0x1d) == HEADS && b.cmos(0x23) == SPT &&
                  b.cmos(0x30) == 0x00 && b.cmos(0x31) == 0x3c && b.cmos(0x3d) == 0x02 && b.cmos(0x3f) == 0x01 &&
                  b.cmos(0x60) == 0x22 && b.cmos(0x61) == 0x07 && b.cmos(0x62) == 0x01;
    check("CMOS configuration bytes", config);
    unsigned sum = 0;
    for (int n = 0x10; n <= 0x2d; ++n) sum += b.cmos(uint8_t(n));
    check("CMOS checksum 2Eh-2Fh covers 10h-2Dh", b.cmos(0x2e) == (sum >> 8) && b.cmos(0x2f) == (sum & 0xff));

    b.top.wall_year = 2099; b.top.wall_month = 12; b.top.wall_day = 31; b.top.wall_hour = 23;
    b.top.wall_minute = 59; b.top.wall_second = 59; b.top.wall_weekday = 7;
    b.top.wall_valid = 1; b.clock(); b.top.wall_valid = 0;
    b.run(100);
    check("wall clock seed: century 20h in 32h, year 99h, in BCD", b.cmos(0x32) == 0x20 && b.cmos(0x09) == 0x99 &&
          b.cmos(0x00) == 0x59 && b.cmos(0x04) == 0x23);
    b.cmos_write(0x0b, 0x12);  // UIE, 24-hour, BCD
    // Poll register A alone (one isa8 read, 74 clocks): UIP must be seen
    // high with the old time, for the eight ticks (128 clocks here) before
    // the update, within one poll.
    b.out(0x70, 0x0a);
    bool uip = b.wait([&] { return (b.in(0x71) & 0x80) != 0; }, 2 * RTC_SECOND);
    uint64_t uip_at = b.cycles;
    bool fell = b.wait([&] { return (b.in(0x71) & 0x80) == 0; }, RTC_SECOND);
    uint64_t window = b.cycles - uip_at;
    check("UIP rises 8 RTC ticks (244 us at 32.768 kHz) before the update", uip && fell &&
          window + 74 >= 8 * 16 && window <= 8 * 16 + 2 * 74, std::to_string(window));
    bool updated = b.cmos(0x00) == 0x00;
    check("the update rolls 23:59:59 31-12-99 into 00:00:00 01-01-00, Sunday to Monday",
          b.cmos(0x02) == 0 && b.cmos(0x04) == 0 && b.cmos(0x07) == 0x01 && b.cmos(0x08) == 0x01 &&
          b.cmos(0x09) == 0x00 && b.cmos(0x06) == 0x01);
    uint8_t flags = b.cmos(0x0c);
    check("the update sets UF and IRQF", (flags & 0x90) == 0x90, hex(flags));
    b.cmos_write(0x0b, 0x06);  // binary, 24-hour
    check("binary mode reads the same time in binary", b.cmos(0x07) == 1 && b.cmos(0x09) == 0);
    b.cmos_write(0x0b, 0x00);  // BCD, 12-hour
    check("12-hour mode reads midnight as 12 AM", b.cmos(0x04) == 0x12);
    b.cmos_write(0x0b, 0x06);  // binary, 24-hour for the divider-restart check

    b.cmos(0x0c);  // clear any prior update flags
    b.cmos_write(0x0a, 0x70);  // hold divider chain in reset
    b.run(1000);
    b.cmos_write(0x0a, 0x26);  // release DV=010 at periodic rate 6
    uint64_t restart = b.cycles;
    uint8_t before = b.cmos(0x00);
    b.run(RTC_SECOND / 2 - 2048);
    uint8_t early_flags = b.cmos(0x0c);
    uint8_t early_second = b.cmos(0x00);
    bool first_update = b.wait([&] { return (b.cmos(0x0c) & 0x10) != 0; }, 4096);
    uint64_t elapsed = b.cycles - restart;
    uint8_t after = b.cmos(0x00);
    check("RTC divider restart makes its first update after 500 ms",
          (early_flags & 0x10) == 0 && early_second == before && first_update &&
          after == uint8_t((before + 1) % 60) && elapsed >= RTC_SECOND / 2 && elapsed < RTC_SECOND / 2 + 4096,
          std::to_string(elapsed) + " " + hex(before) + " " + hex(early_second) + " " + hex(after));
}

void ata(Bench &b) {
    b.out(0x3f6, 0x02);  // nIEN
    b.out(0x1f6, 0xa0);
    b.out(0x1f7, 0xec);
    b.wait([&] { return (b.in(0x1f7) & 0x88) == 0x08; }, 1000);
    std::vector<uint16_t> identify(256);
    for (auto &word : identify) word = b.inw(0x1f0);
    std::string model;
    for (int w = 27; w <= 46; ++w) { model.push_back(char(identify[w] >> 8)); model.push_back(char(identify[w])); }
    check("IDENTIFY: geometry, LBA capacity, multiple 16, LBA, no DMA, no 32-bit PIO",
          identify[0] == 0x0040 && identify[1] == CYLINDERS && identify[3] == HEADS && identify[6] == SPT &&
          identify[5] == 512 && identify[47] == 0x8010 && identify[48] == 0 && identify[49] == 0x0200 &&
          (identify[60] | identify[61] << 16) == SECTORS, hex(identify[60]));
    check("IDENTIFY model string", model.rfind("FES pc386 ATA disk", 0) == 0, model);
    check("isa16: a data-port word is one 360 ns cycle", b.last_io == 38, std::to_string(b.last_io));

    // WRITE SECTORS at LBA 130 (dirty chunk 1), two sectors, then read them back.
    b.out(0x1f2, 2); b.out(0x1f3, 130); b.out(0x1f4, 0); b.out(0x1f5, 0); b.out(0x1f6, 0xe0);
    b.out(0x1f7, 0x30);
    for (int sector = 0; sector < 2; ++sector) {
        b.wait([&] { return (b.in(0x1f7) & 0x88) == 0x08; }, 10000);
        for (int n = 0; n < 256; ++n) b.outw(0x1f0, uint16_t(sector << 12 | n));
    }
    uint8_t after_write = b.in(0x1f7);
    check("WRITE SECTORS completes with DRDY at once (the sector is buffered)", (after_write & 0xc9) == 0x40,
          hex(after_write));
    b.run(1000);
    check("written sectors reach the image whole", b.image[130 * 512 + 2] == 1 && b.image[131 * 512 + 1] == 0x10 &&
          b.image[131 * 512 + 510] == 0xff);
    b.top.dirty_index = 0;
    b.run(2);
    check("the sector store sets dirty chunk 1 only", b.top.dirty_word == 0x0002, hex(b.top.dirty_word));

    // The dirty map: chunk LBA / 128 is bit chunk % 16 of word chunk / 16.
    // Writes one sector at lba with dirty_index at index, watching dirty_word
    // every clock: it reads old_word, then new_word from some clock after the
    // store goes idle. Returns whether it read nothing else, and that clock.
    auto store_marks = [&](uint32_t lba, uint8_t index, uint16_t old_word, uint16_t new_word) {
        b.top.dirty_index = index;
        b.run(2);
        bool clean = b.top.dirty_word == old_word;
        uint64_t idle = 0, seen = 0;
        b.each_clock = [&] {
            uint16_t word = b.top.dirty_word;
            if (!seen && idle && word == new_word) seen = b.cycles;
            clean &= word == (seen ? new_word : old_word);
            if (b.top.disk_valid && b.top.disk_ready && b.top.disk_write && (b.top.disk_address & 63) == 63)
                idle = b.cycles + 1;  // the last beat is accepted at this clock's edge
        };
        b.out(0x1f2, 1); b.out(0x1f3, uint8_t(lba)); b.out(0x1f4, uint8_t(lba >> 8)); b.out(0x1f5, 0);
        b.out(0x1f6, 0xe0); b.out(0x1f7, 0x30);
        b.wait([&] { return (b.in(0x1f7) & 0x88) == 0x08; }, 10000);
        for (int n = 0; n < 256; ++n) b.outw(0x1f0, uint16_t(lba + n));
        b.run(1000);
        b.each_clock = nullptr;
        b.out(0x1f4, 0);  // the later commands set only the low LBA byte
        return std::make_pair(clean && seen != 0, seen - idle);
    };
    auto [kept, latency] = store_marks(5 * 128, 0, 0x0002, 0x0022);
    check("a mark keeps its word's earlier bits, and dirty_word holds over the mark's read-modify-write",
          kept, std::to_string(latency));
    check("a mark reaches dirty_word 4 clocks after the store goes idle", latency == 4, std::to_string(latency));
    bool steady = store_marks(19 * 128, 0, 0x0022, 0x0022).first;
    check("a mark in another word leaves dirty_word steady", steady && b.image[19 * 128 * 512 + 2] == 0x81);
    b.top.dirty_index = 1;
    b.run(1);
    uint16_t one_clock = b.top.dirty_word;
    b.run(1);
    uint16_t word1 = b.top.dirty_word;
    b.top.dirty_index = 0;
    b.run(2);
    check("dirty_word reads word 1 (chunk 19), then word 0 (chunks 1, 5), two clocks after dirty_index",
          one_clock == 0x0022 && word1 == 0x0008 && b.top.dirty_word == 0x0022,
          hex(one_clock) + " " + hex(word1) + " " + hex(b.top.dirty_word));
    b.out(0x1f2, 2); b.out(0x1f3, 130); b.out(0x1f6, 0xe0);
    b.out(0x1f7, 0x20);
    bool same = true;
    for (int sector = 0; sector < 2; ++sector) {
        b.wait([&] { return (b.in(0x1f7) & 0x88) == 0x08; }, 10000);
        for (int n = 0; n < 256; ++n) same &= b.inw(0x1f0) == uint16_t(sector << 12 | n);
    }
    check("READ SECTORS returns what was written", same && (b.in(0x1f7) & 0x89) == 0);

    // READ MULTIPLE, four sectors per block, with interrupts per block.
    for (size_t n = 0; n < 8 * 512; ++n) b.image[512 * 200 + n] = uint8_t(n * 7);
    b.out(0x1f2, 4); b.out(0x1f7, 0xc6);
    b.in(0x1f7);
    pic_init(b);
    b.out(0x21, 0xfb);
    b.out(0xa1, 0xbf);
    clear_stale_interrupt(b);
    b.out(0x3f6, 0x00);
    b.out(0x1f2, 8); b.out(0x1f3, 200); b.out(0x1f6, 0xe0);
    b.out(0x1f7, 0xc4);
    int interrupts = 0;
    same = true;
    for (int block = 0; block < 2; ++block) {
        bool raised = b.wait([&] { return bool(b.top.intr); }, 20000);
        if (raised && b.inta() == 0x76) ++interrupts;
        uint8_t status = b.in(0x1f7);  // acknowledges INTRQ
        same &= (status & 0x88) == 0x08;
        b.out(0xa0, 0x20); b.out(0x20, 0x20);
        for (int n = 0; n < 4 * 256; ++n) {
            uint16_t word = b.inw(0x1f0);
            size_t at = size_t(block) * 2048 + size_t(n) * 2;
            same &= word == uint16_t(b.image[512 * 200 + at] | b.image[512 * 200 + at + 1] << 8);
        }
    }
    check("READ MULTIPLE streams two four-sector blocks, one IRQ14 each", same && interrupts == 2,
          std::to_string(interrupts));
    b.out(0x3f6, 0x02);
    b.out(0xa1, 0xff); b.out(0x21, 0xff);

    auto fill_sector = [&](uint32_t lba) {
        for (size_t n = 0; n < 512; ++n) b.image[size_t(lba) * 512 + n] = uint8_t(lba + n);
    };
    auto issue_lba = [&](uint8_t lba, uint8_t count, uint8_t opcode) {
        b.out(0x1f2, count); b.out(0x1f3, lba); b.out(0x1f4, 0); b.out(0x1f5, 0); b.out(0x1f6, 0xe0);
        b.out(0x1f7, opcode);
    };
    auto read_matches = [&](uint32_t lba) {
        bool matches = true;
        for (int n = 0; n < 256; ++n) {
            uint16_t expected = uint16_t(b.image[size_t(lba) * 512 + size_t(n) * 2]) |
                                uint16_t(b.image[size_t(lba) * 512 + size_t(n) * 2 + 1]) << 8;
            matches &= b.inw(0x1f0) == expected;
        }
        return matches;
    };
    b.out(0x1f2, 2); b.out(0x1f7, 0xc6); b.in(0x1f7);  // READ MULTIPLE block size 2
    for (uint32_t lba = 240; lba <= 242; ++lba) fill_sector(lba);
    b.held_read_lba = 241;
    issue_lba(240, 2, 0xc4);
    bool old_block_ready = b.wait([&] { return (b.in(0x1f7) & 0x88) == 0x08; }, 20000);
    bool old_prefetch_held = b.wait([&] { return b.pending_read && b.pending_address / 64 == 241; }, 20000);
    issue_lba(242, 1, 0x20);  // replace the command while the second sector is in flight
    b.held_read_lba = -1;
    bool new_read_ready = b.wait([&] { return (b.in(0x1f7) & 0x88) == 0x08; }, 20000);
    bool new_read_matches = read_matches(242);
    check("new ATA command drains an old prefetch before filling its buffer",
          old_block_ready && old_prefetch_held && new_read_ready && new_read_matches,
          std::to_string(old_block_ready) + " " + std::to_string(old_prefetch_held) + " " +
          std::to_string(new_read_ready) + " " + std::to_string(new_read_matches));

    for (uint32_t lba = 250; lba <= 252; ++lba) fill_sector(lba);
    b.held_read_lba = 251;
    issue_lba(250, 2, 0xc4);
    old_block_ready = b.wait([&] { return (b.in(0x1f7) & 0x88) == 0x08; }, 20000);
    old_prefetch_held = b.wait([&] { return b.pending_read && b.pending_address / 64 == 251; }, 20000);
    b.out(0x3f6, 0x06);  // assert SRST with a prefetched beat outstanding
    b.out(0x3f6, 0x02);
    bool reset_finished = b.wait([&] { return (b.in(0x1f7) & 0x80) == 0; }, 10000);
    b.held_read_lba = -1;
    b.run(512);          // let the stale response drain
    issue_lba(252, 1, 0x20);
    new_read_ready = b.wait([&] { return (b.in(0x1f7) & 0x88) == 0x08; }, 20000);
    new_read_matches = read_matches(252);
    check("ATA SRST drains an old prefetch without wrapping or mixing the next read",
          old_block_ready && old_prefetch_held && reset_finished && new_read_ready && new_read_matches,
          std::to_string(old_block_ready) + " " + std::to_string(old_prefetch_held) + " " +
          std::to_string(reset_finished) + " " + std::to_string(new_read_ready) + " " +
          std::to_string(new_read_matches));

    // CHS through the default translation: cylinder 1, head 2, sector 3.
    uint32_t lba = (1 * HEADS + 2) * SPT + 2;
    b.image[lba * 512] = 0x77;
    b.out(0x1f2, 1); b.out(0x1f3, 3); b.out(0x1f4, 1); b.out(0x1f5, 0); b.out(0x1f6, 0xa2);
    b.out(0x1f7, 0x20);
    b.wait([&] { return (b.in(0x1f7) & 0x88) == 0x08; }, 10000);
    uint16_t first = b.inw(0x1f0);
    for (int n = 1; n < 256; ++n) b.inw(0x1f0);
    check("CHS addressing through the IDENTIFY geometry", (first & 0xff) == 0x77, hex(first));

    b.out(0x1f2, 2); b.out(0x1f3, SPT); b.out(0x1f4, 1); b.out(0x1f5, 0); b.out(0x1f6, 0xa2);
    b.out(0x1f7, 0x40);  // READ VERIFY from cylinder 1, head 2, sector 17
    bool verify_done = b.wait([&] { return (b.in(0x1f7) & 0x80) == 0; }, 1000);
    check("CHS READ VERIFY leaves the task file at the last verified sector",
          verify_done && b.in(0x1f2) == 0 && b.in(0x1f3) == 1 && b.in(0x1f4) == 1 &&
          b.in(0x1f5) == 0 && b.in(0x1f6) == 0xa3 && (b.in(0x1f7) & 0x41) == 0x40,
          hex(b.in(0x1f6)) + " " + hex(b.in(0x1f4)) + " " + hex(b.in(0x1f3)));

    issue_lba(80, 2, 0x40);
    verify_done = b.wait([&] { return (b.in(0x1f7) & 0x80) == 0; }, 1000);
    check("LBA READ VERIFY leaves the task file at the last verified sector",
          verify_done && b.in(0x1f2) == 0 && b.in(0x1f3) == 81 && b.in(0x1f4) == 0 &&
          b.in(0x1f5) == 0 && b.in(0x1f6) == 0xe0 && (b.in(0x1f7) & 0x41) == 0x40,
          hex(b.in(0x1f6)) + " " + hex(b.in(0x1f5)) + " " + hex(b.in(0x1f4)) + " " + hex(b.in(0x1f3)));

    b.out(0x1f2, 1); b.out(0x1f3, 0xff); b.out(0x1f4, 0xff); b.out(0x1f5, 0); b.out(0x1f6, 0xe0);
    b.out(0x1f7, 0x20);
    b.run(100);
    uint8_t status = b.in(0x1f7), error = b.in(0x1f1);
    check("a sector beyond the image fails with IDNF", (status & 0x01) && error == 0x10, hex(status) + " " + hex(error));
    b.out(0x1f7, 0x55);
    status = b.in(0x1f7); error = b.in(0x1f1);
    check("an unknown command aborts", (status & 0x01) && error == 0x04, hex(status) + " " + hex(error));
    b.out(0x1f6, 0xb0);
    check("device 1 is absent: its status reads 00h", b.in(0x1f7) == 0x00);
    b.out(0x1f6, 0xa0);
    b.out(0x3f6, 0x06);
    check("SRST sets BSY", (b.in(0x3f6) & 0x80) != 0);
    b.out(0x3f6, 0x02);
    b.wait([&] { return (b.in(0x1f7) & 0x80) == 0; }, 10000);
    check("after reset: signature 01 01 00 00, DRDY",
          b.in(0x1f2) == 1 && b.in(0x1f3) == 1 && b.in(0x1f4) == 0 && b.in(0x1f5) == 0 && (b.in(0x1f7) & 0xc1) == 0x40);
}

// After the other checks: what a machine reset keeps and what the
// fes.computer 1.1 shell clears.
void disk_and_record(Bench &b) {
    // The RTC writes every CMOS RAM byte to the record and loads it back
    // after a reset; the clock's registers carry over, as an MC146818's do,
    // but for PIE, AIE, UIE and SQWE.
    b.cmos_write(0x50, 0x5a);
    check("a guest CMOS write reaches the record", (b.record[0x28] & 0xff) == 0x5a, hex(b.record[0x28]));
    check("the configuration and checksum bytes reach the record",
          (b.record[0x09] & 0xff) == 0xf0 && b.record[0x17] == uint16_t(b.cmos(0x2e) | b.cmos(0x2f) << 8),
          hex(b.record[0x09]) + " " + hex(b.record[0x17]));
    b.record[0x30] = 0x3c3c;  // 60h-61h as a restore would leave them: rewritten from the straps
    b.cmos_write(0x0b, 0x7a);  // PIE, AIE, UIE, SQWE, binary, 24-hour
    uint8_t year = b.cmos(0x09);
    b.top.dirty_index = 0;
    b.run(2);
    uint16_t map = b.top.dirty_word;
    b.top.reset = 1;
    b.run(4);
    b.top.reset = 0;
    while (b.top.cmos_busy) b.clock();
    b.run(4);
    check("CMOS RAM comes back from the record after a reset, and the setup rewrites the straps",
          b.cmos(0x50) == 0x5a && b.cmos(0x60) == 0x22 && b.cmos(0x61) == 0x07, hex(b.cmos(0x50)));
    check("a reset clears B's interrupt enables and SQWE and keeps the clock", b.cmos(0x0b) == 0x02 &&
          b.cmos(0x09) == year, hex(b.cmos(0x0b)) + " " + hex(b.cmos(0x09)));
    check("a reset leaves the dirty map", b.top.dirty_word == map && map != 0, hex(map));
    b.top.disk_dirty_clear = 1;
    b.clock();
    b.top.disk_dirty_clear = 0;
    uint64_t asked = b.cycles;
    b.wait([&] { return !b.top.disk_dirty_clearing; }, 1000);
    b.run(4);
    check("dirty_clear clears the map over 128 clocks", b.top.dirty_word == 0 && b.cycles - asked >= 128,
          std::to_string(b.cycles - asked) + " " + hex(b.top.dirty_word));

    // The record reads 00h-0Dh as the guest does, two at a time, and reading
    // C that way leaves its flags for the guest.
    b.cmos_write(0x05, 0x17);
    b.top.record_clock_index = 2;  // 04h, 05h
    b.clock();
    uint16_t hours = b.top.record_clock_pair;
    bool same = hours == uint16_t(b.cmos(0x04) | b.cmos(0x05) << 8);
    b.cmos(0x0c);
    b.top.record_clock_index = 6;  // 0Ch, 0Dh
    bool flagged = b.wait([&] { return (b.top.record_clock_pair & 0xff) != 0; }, 100000);
    uint16_t flags = b.top.record_clock_pair;
    b.run(4);
    bool kept = b.top.record_clock_pair == flags;
    uint8_t read = b.cmos(0x0c);
    b.clock();
    check("the record reads the clock's registers as the guest does, without clearing C",
          same && (hours >> 8) == 0x17 && flagged && (flags >> 8) == 0x80 && kept && read == (flags & 0xff) &&
              (b.top.record_clock_pair & 0xff) == 0,
          hex(hours) + " " + hex(flags) + " " + hex(read) + " " + hex(b.top.record_clock_pair));

    // The fence holds a write command back under BSY; the drive is fenced at
    // once while idle.
    b.out(0x3f6, 0x02);  // nIEN
    b.top.disk_frozen = 1;
    b.run(2);
    bool fenced_idle = b.top.disk_fenced;
    unsigned before = b.written;
    b.out(0x1f2, 1); b.out(0x1f3, 44); b.out(0x1f4, 1); b.out(0x1f5, 0); b.out(0x1f6, 0xe0);  // LBA 300
    b.out(0x1f7, 0x30);
    bool held = !b.wait([&] { return (b.in(0x1f7) & 0x88) != 0x80; }, 5000);
    check("a write command waits under BSY while frozen, and the drive stays fenced", fenced_idle && held &&
          b.top.disk_fenced, hex(b.in(0x1f7)));
    b.top.disk_frozen = 0;
    bool opened = b.wait([&] { return (b.in(0x1f7) & 0x88) == 0x08; }, 5000);
    for (int n = 0; n < 256; ++n) b.outw(0x1f0, uint16_t(0x3000 + n));
    bool busy = b.top.disk_writer_busy;
    b.wait([&] { return !b.top.disk_writer_busy; }, 5000);
    b.top.dirty_index = 0;
    b.run(4);
    check("Resume opens the block; its store reports busy, then lands once with its mark",
          opened && busy && b.written == before + 1 && b.top.dirty_word == 0x0004 &&
          b.image[300 * 512] == 0x00 && b.image[300 * 512 + 1] == 0x30,
          std::to_string(b.written - before) + " " + hex(b.top.dirty_word));

    // A block the guest has begun runs to its end; the next waits for Resume.
    b.out(0x1f2, 4); b.out(0x1f7, 0xc6);  // SET MULTIPLE 4
    b.wait([&] { return (b.in(0x1f7) & 0x80) == 0; }, 1000);
    b.out(0x1f2, 8); b.out(0x1f3, 0x90); b.out(0x1f4, 1); b.out(0x1f6, 0xe0);  // LBA 400
    b.out(0x1f7, 0xc5);
    b.wait([&] { return (b.in(0x1f7) & 0x88) == 0x08; }, 5000);
    for (int n = 0; n < 256; ++n) b.outw(0x1f0, uint16_t(0x4000 + n));
    b.top.disk_frozen = 1;
    b.run(2);
    bool open_block = !b.top.disk_fenced;
    for (int sector = 1; sector < 4; ++sector)
        for (int n = 0; n < 256; ++n) b.outw(0x1f0, uint16_t(0x4000 + sector * 0x100 + n));
    bool block_held = !b.wait([&] { return (b.in(0x1f7) & 0x88) != 0x80; }, 5000);
    b.wait([&] { return b.top.disk_fenced; }, 5000);
    bool whole = b.image[403 * 512 + 1] == 0x43 && b.image[404 * 512 + 1] == 0x00;
    check("Freeze mid-block: the block runs to its end, then the next waits under BSY and the drive is fenced",
          open_block && block_held && b.top.disk_fenced && whole, hex(b.in(0x1f7)));
    b.top.disk_frozen = 0;
    b.wait([&] { return (b.in(0x1f7) & 0x88) == 0x08; }, 5000);
    for (int sector = 4; sector < 8; ++sector)
        for (int n = 0; n < 256; ++n) b.outw(0x1f0, uint16_t(0x4000 + sector * 0x100 + n));
    b.wait([&] { return !b.top.disk_writer_busy; }, 5000);
    check("Resume opens the next block", b.image[407 * 512 + 1] == 0x47 && (b.in(0x1f7) & 0xc9) == 0x40);
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    std::setvbuf(stdout, nullptr, _IOLBF, 0);
    {
        Bench b;
        timing(b);
        pic_and_rtc_interrupts(b);
        dma(b);
        kbc(b);
        rtc(b);
        ata(b);
        pit(b);
        disk_and_record(b);
    }
    std::printf("%s %d failure(s)\n", failures ? "FAIL" : "PASS", failures);
    return failures ? 1 : 0;
}
