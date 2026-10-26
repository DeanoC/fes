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
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <functional>
#include <string>
#include <vector>

namespace {

constexpr double CLOCK_HZ = 100e6;
constexpr uint64_t RTC_SECOND = 1u << 19;
constexpr uint16_t CYLINDERS = 20, HEADS = 4, SPT = 17;
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
        top.clk = 1; top.eval();
        top.clk = 0; top.eval();
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
};

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
    b.wait([&] { return (b.in(0x64) & 1) != 0; }, 2000000);
    uint8_t bat = b.in(0x60);
    check("keyboard FF: FA no sooner than one PS/2 byte time (1 ms), then AA", ack == 0xfa && bat == 0xaa &&
          ack_time >= 100000, hex(ack) + " " + hex(bat) + " " + std::to_string(ack_time));

    // Translation and IRQ1.
    pic_init(b);
    b.out(0x21, 0xfd);
    auto key = [&](uint8_t code) {
        b.wait([&] { return bool(b.top.key_ready); }, 1000);
        b.top.key_valid = 1; b.top.key_code = code; b.clock(); b.top.key_valid = 0;
    };
    key(0x1c);
    bool raised = b.wait([&] { return b.top.intr; }, 300000);
    uint8_t vector = raised ? b.inta() : 0;
    uint8_t make = b.in(0x60);
    b.out(0x20, 0x20);
    key(0xf0); key(0x1c);
    b.wait([&] { return (b.in(0x64) & 1) != 0; }, 300000);
    uint8_t brk = b.in(0x60);
    check("set 2 1Ch (A) arrives as set 1 1Eh on IRQ1, its break as 9Eh", raised && vector == 0x09 && make == 0x1e &&
          brk == 0x9e, hex(vector) + " " + hex(make) + " " + hex(brk));
    b.out(0x60, 0xf2);
    uint8_t id[3] = {};
    for (auto &byte : id) {
        b.wait([&] { return (b.in(0x64) & 1) != 0; }, 300000);
        byte = b.in(0x60);
    }
    check("keyboard F2 identifies as AB 83, translated to AB 41", id[0] == 0xfa && id[1] == 0xab && id[2] == 0x41,
          hex(id[0]) + " " + hex(id[1]) + " " + hex(id[2]));

    // Keep the scan-code FIFO near full, then require the three-byte F2 reply
    // to arrive intact after the queued make codes.
    int queued_keys = 0;
    while (queued_keys < 32 && b.top.key_ready) {
        b.top.key_valid = 1; b.top.key_code = 0x1c; b.clock(); b.top.key_valid = 0;
        ++queued_keys;
    }
    bool fifo_near_full = !b.top.key_ready;
    bool reply_seen = false;
    std::vector<uint8_t> tail;
    b.out(0x60, 0xf2);
    for (int n = 0; n < 24 && !reply_seen; ++n) {
        if (!b.wait([&] { return (b.in(0x64) & 1) != 0; }, 300000)) break;
        tail.push_back(b.in(0x60));
        if (tail.size() > 3) tail.erase(tail.begin());
        reply_seen = tail.size() == 3 && tail[0] == 0xfa && tail[1] == 0xab && tail[2] == 0x41;
    }
    check("keyboard keeps F2 reply intact with scan-code FIFO nearly full", queued_keys > 0 && fifo_near_full && reply_seen,
          std::to_string(queued_keys) + " " + std::to_string(fifo_near_full) + " " + std::to_string(reply_seen));

    queued_keys = 0;
    while (queued_keys < 32 && b.top.key_ready) {
        b.top.key_valid = 1; b.top.key_code = 0x1c; b.clock(); b.top.key_valid = 0;
        ++queued_keys;
    }
    b.out(0x60, 0xf2);
    b.run(8);                   // fill the last three FIFO slots with the reply
    b.out(0x60, 0xee);          // its response waits while the FIFO is full
    std::vector<uint8_t> full_reply;
    for (int n = 0; n < queued_keys + 4; ++n) {
        if (!b.wait([&] { return (b.in(0x64) & 1) != 0; }, 300000)) break;
        full_reply.push_back(b.in(0x60));
    }
    bool full_reply_intact = full_reply.size() == size_t(queued_keys + 4) &&
                             std::all_of(full_reply.begin(), full_reply.begin() + queued_keys,
                                         [](uint8_t byte) { return byte == 0x1e; }) &&
                             full_reply[queued_keys] == 0xfa && full_reply[queued_keys + 1] == 0xab &&
                             full_reply[queued_keys + 2] == 0x41 && full_reply.back() == 0xee;
    std::string full_reply_detail = std::to_string(queued_keys) + " " + std::to_string(full_reply.size());
    if (!full_reply.empty()) full_reply_detail += " " + hex(full_reply.back());
    check("keyboard reply waits for FIFO space instead of dropping bytes", full_reply_intact,
          full_reply_detail);
    b.out(0x21, 0xff);

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

    mouse_command(0xf6);
    b.run(11000000);  // let any preceding sample interval expire
    mouse_command(0xf3); mouse_command(10); mouse_command(0xf4);
    while (b.in(0x64) & 1) b.in(0x60);  // discard any earlier device reply before timing packets
    b.top.mouse_valid = 1; b.top.mouse_dx = 1; b.top.mouse_dy = 0; b.top.mouse_buttons = 1;
    b.clock(); b.top.mouse_valid = 0;
    auto first_rate_packet = mouse_packet();
    uint64_t first_packet_read = b.cycles;
    b.top.mouse_valid = 1; b.top.mouse_dx = 1; b.top.mouse_dy = 0; b.top.mouse_buttons = 1;
    b.clock(); b.top.mouse_valid = 0;
    bool early_rate_packet = b.wait([&] { return (b.in(0x64) & 1) != 0; }, 90000);
    uint64_t early_elapsed = b.cycles - first_packet_read;
    bool low_rate_packet = early_rate_packet || b.wait([&] { return (b.in(0x64) & 1) != 0; }, 50000);
    uint64_t second_packet_arrival = b.cycles;
    auto second_rate_packet = low_rate_packet ? mouse_packet() : std::array<uint8_t, 3>{0, 0, 0};
    check("10 Hz mouse sampling waits at least 100 ms", first_rate_packet[1] == 1 && !early_rate_packet &&
          low_rate_packet && second_rate_packet[1] == 1 && second_packet_arrival - first_packet_read >= 9500000,
          std::to_string(early_rate_packet) + " " + std::to_string(early_elapsed) + " " +
          std::to_string(low_rate_packet) + " " + std::to_string(second_packet_arrival - first_packet_read) + " " +
          hex(second_rate_packet[1]));

    b.out(0x64, 0xa7);
    b.run(200000);
    while (b.in(0x64) & 1) b.in(0x60);
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
    }
    std::printf("%s %d failure(s)\n", failures ? "FAIL" : "PASS", failures);
    return failures ? 1 : 0;
}
