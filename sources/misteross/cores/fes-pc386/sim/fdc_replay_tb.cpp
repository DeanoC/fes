// SPDX-License-Identifier: MIT
// Floppy controller replay (docs/design/pc386-removable-media.md, "Device
// references"): pc386_fdc against Bochs's iodev/floppy.cc (fdc_reference).
//
// The two models keep different time: Bochs completes a sector in one timer
// period and has no spin-up, settle or byte pacing, while pc386_fdc paces
// steps, spin-up and bytes as a drive does. So the replay is closed-loop, not
// a timed trace: each case is a script a guest would run (program, issue a
// command, wait for this model's own RQM, IRQ or DRQ, take the results), run
// once against each model. What the script reads is logged: port reads,
// result bytes, interrupts, the DMA byte streams with their CRCs, timeouts
// (a hang), and at the end the CRC of each disk image. The logs must match
// line for line, except in the cases fdc_divergences.toml lists, which must
// differ. A case also checks the RTL against what the datasheet and the
// design require (CHECK), so a listed divergence still has a defined RTL
// answer. RTL-only cases cover what Bochs does not model: non-DMA transfers
// and the timing.
//
// Usage: fdc_replay_tb [--case NAME] --divergent NAME,NAME,...
// Prints CASE name MATCH|DIFFER|RTL-ONLY, CHECK FAIL lines, DIFF lines and a
// SUMMARY line; exits 1 on an unlisted divergence, a listed case that
// matches, a Bochs panic in a compared case or a failed check.
#include "Vpc386_fdc.h"
#include "fdc_reference.h"
#include "verilated.h"

#include <algorithm>
#include <cstdarg>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <functional>
#include <memory>
#include <set>
#include <sstream>
#include <string>
#include <unistd.h>
#include <vector>

double sc_time_stamp() { return 0; }

namespace {

constexpr unsigned US_CLOCKS = 4;               // the bench's US_D: clocks per microsecond
constexpr uint32_t IMAGE0_WORD = (0x30000000u + 0x01800000u) >> 3;
constexpr uint32_t DRIVE_BYTES = 4u << 20;

std::string strf(const char *fmt, ...) {
    char buffer[1024];
    va_list ap; va_start(ap, fmt); std::vsnprintf(buffer, sizeof buffer, fmt, ap); va_end(ap);
    return buffer;
}

uint32_t crc32(const uint8_t *data, size_t length) {
    uint32_t crc = 0xffffffffu;
    for (size_t i = 0; i < length; ++i) {
        crc ^= data[i];
        for (int bit = 0; bit < 8; ++bit) crc = (crc >> 1) ^ (0xedb88320u & -(crc & 1));
    }
    return ~crc;
}

struct Geometry {
    unsigned cylinders, heads, spt;
    size_t bytes() const { return size_t(cylinders) * heads * spt * 512; }
};
const Geometry G360{40, 2, 9}, G720{80, 2, 9}, G12{80, 2, 15}, G144{80, 2, 18}, GDMF{80, 2, 21}, G288{80, 2, 36};

// A synthetic disk: every byte depends on its disk, sector and offset.
std::vector<uint8_t> make_image(const Geometry &g, unsigned seed) {
    std::vector<uint8_t> bytes(g.bytes());
    for (size_t i = 0; i < bytes.size(); ++i) {
        size_t lba = i / 512, offset = i % 512;
        bytes[i] = uint8_t(lba * 7 + offset * 13 + seed * 101 + (offset >> 8) * 29 + 0x5a);
    }
    return bytes;
}

// ---- pc386_fdc with its window and the bench's DMA ------------------------
class Rtl final : public pc386::FdcReference {
public:
    Rtl() : top(new Vpc386_fdc), memory(2 * DRIVE_BYTES, 0) {}
    void power_on(unsigned type_a, unsigned type_b) override {
        types[0] = type_a; types[1] = type_b;
        apply_drives();
        hardware_reset();
    }
    void hardware_reset() override {
        top->reset = 1;
        for (int i = 0; i < 8; ++i) cycle();
        top->reset = 0;
        cycle();
    }
    void out(uint16_t port, uint8_t value) override {
        if (!decoded(port)) return;
        top->access = 1; top->write = 1; top->address = port & 7; top->wdata = value;
        if (eject_with_write >= 0) {
            // The disk leaves in the same clock as this write.
            present[eject_with_write] = false;
            apply_drives();
            eject_with_write = -1;
        }
        cycle();
        top->access = 0; top->write = 0;
    }
    uint8_t in(uint16_t port) override {
        if (!decoded(port)) return 0xff;
        top->access = 1; top->write = 0; top->address = port & 7;
        uint8_t value = 0;
        cycle(&value);
        top->access = 0;
        return value;
    }
    bool irq() const override { return top->irq6; }
    bool drq() const override { return top->drq; }
    uint8_t dack_from(bool tc) override { return dack(0, tc); }
    void dack_to(uint8_t value, bool tc) override { dack(value, tc); }
    void advance_us(uint64_t us) override { for (uint64_t i = 0; i < us * US_CLOCKS; ++i) cycle(); }
    void insert(unsigned drive, const pc386::FloppyDisk &disk) override {
        std::ifstream file(disk.path, std::ios::binary);
        std::vector<uint8_t> bytes((std::istreambuf_iterator<char>(file)), std::istreambuf_iterator<char>());
        std::copy(bytes.begin(), bytes.end(), memory.begin() + drive * DRIVE_BYTES);
        disks[drive] = disk;
        present[drive] = true;
        apply_drives();
        cycle();
    }
    void eject(unsigned drive) override {
        present[drive] = false;
        apply_drives();
        cycle();
    }
    const std::string &panic() const override { return none; }

    std::vector<uint8_t> image(unsigned drive, size_t bytes) const {
        return std::vector<uint8_t>(memory.begin() + drive * DRIVE_BYTES, memory.begin() + drive * DRIVE_BYTES + bytes);
    }
    uint64_t clocks = 0;
    bool busy() const { return top->busy; }
    bool fenced() const { return top->fenced; }
    void set_frozen(bool frozen) { top->frozen = frozen; cycle(); }
    void step(uint64_t n) { for (uint64_t i = 0; i < n; ++i) cycle(); }
    // A write's byte by DACK, then `then` `after` clocks after the DACK
    // completes.
    void dack_then(uint8_t value, unsigned after, const std::function<void()> &then) {
        top->dack_valid = 1; top->dack_tc = 0; top->dack_wdata = value;
        for (int i = 0; i < 64; ++i) { cycle(); if (top->dack_done) break; }
        top->dack_valid = 0;
        for (unsigned i = 0; i < after; ++i) cycle();
        then();
    }
    int eject_with_write = -1;            // a drive whose disk leaves with the next port write

private:
    static bool decoded(uint16_t port) { return port == 0x3f2 || port == 0x3f4 || port == 0x3f5 || port == 0x3f7; }
    void apply_drives() {
        top->drive_type = (types[1] << 3) | types[0];
        top->present = (present[1] << 1) | present[0];
        top->write_protect = (disks[1].write_protected << 1) | disks[0].write_protected;
        top->two_sided = ((disks[1].heads == 2) << 1) | (disks[0].heads == 2);
        top->cylinders = ((disks[1].cylinders & 0x7f) << 7) | (disks[0].cylinders & 0x7f);
        top->spt = ((disks[1].spt & 0x3f) << 6) | (disks[0].spt & 0x3f);
    }
    uint8_t dack(uint8_t value, bool tc) {
        top->dack_valid = 1; top->dack_tc = tc; top->dack_wdata = value;
        uint8_t result = 0xff;
        for (int i = 0; i < 64; ++i) {
            cycle();
            if (top->dack_done) { result = top->dack_rdata; break; }
        }
        top->dack_valid = 0; top->dack_tc = 0;
        cycle();
        return result;
    }
    // One clock. The window answers a read burst after a latency, one beat a
    // clock, and stalls the port now and then.
    void cycle(uint8_t *read = nullptr) {
        random = random * 1103515245u + 12345u;
        top->aux_waitrequest = (random >> 16) % 4 == 0;
        top->aux_readdatavalid = 0;
        if (!beats.empty() && beats.front().first <= clocks) {
            uint32_t word = beats.front().second;
            uint64_t data = 0;
            if (word >= IMAGE0_WORD && word < IMAGE0_WORD + 2 * DRIVE_BYTES / 8)
                std::memcpy(&data, &memory[size_t(word - IMAGE0_WORD) * 8], 8);
            top->aux_readdatavalid = 1;
            top->aux_readdata = data;
            beats.erase(beats.begin());
        }
        top->clk = 0;
        top->eval();
        if (read) *read = top->rdata;
        bool read_taken = top->aux_read && !top->aux_waitrequest;
        bool write_taken = top->aux_write && !top->aux_waitrequest;
        if (read_taken && top->aux_burstcount != 64) fatal("FDC read burst of %u beats", top->aux_burstcount);
        if (write_taken) {
            if (top->aux_burstcount != 1 || top->aux_byteenable != 0xff) fatal("FDC write not one whole beat");
            uint32_t word = top->aux_address;
            if (word < IMAGE0_WORD || word >= IMAGE0_WORD + 2 * DRIVE_BYTES / 8) fatal("FDC write outside its images");
            uint64_t data = top->aux_writedata;
            std::memcpy(&memory[size_t(word - IMAGE0_WORD) * 8], &data, 8);
        }
        uint32_t address = top->aux_address;
        top->clk = 1;
        top->eval();
        ++clocks;
        if (read_taken)
            for (unsigned beat = 0; beat < 64; ++beat) beats.emplace_back(clocks + 6 + beat, address + beat);
    }
    [[noreturn]] void fatal(const char *fmt, unsigned value = 0) {
        std::fprintf(stderr, "FATAL ");
        std::fprintf(stderr, fmt, value);
        std::fprintf(stderr, "\n");
        std::exit(2);
    }
    std::unique_ptr<Vpc386_fdc> top;
    std::vector<uint8_t> memory;
    std::vector<std::pair<uint64_t, uint32_t>> beats;
    unsigned types[2] = {0, 0};
    bool present[2] = {false, false};
    pc386::FloppyDisk disks[2];
    uint32_t random = 1;
    std::string none;
};

// ---- The guest's script ------------------------------------------------------
struct Disk {
    Geometry geometry;
    unsigned seed;
    bool write_protected = false;
};

class Guest {
public:
    Guest(pc386::FdcReference &model, bool rtl, const std::string &directory)
        : f(model), rtl(rtl), directory(directory) {}
    pc386::FdcReference &f;
    bool rtl;
    std::vector<std::string> log, failures;
    uint64_t now = 0;                          // microseconds the script has waited
    std::function<uint64_t()> clock_us;        // the model's own time, for the RTL's timing checks
    std::string directory;
    std::vector<std::pair<std::string, Geometry>> files;   // per drive
    unsigned disks_made = 0;

    void note(const std::string &line) { log.push_back(line); }
    void check(bool ok, const std::string &what) { if (rtl && !ok) failures.push_back(what); }
    void tick(uint64_t us = 1) { f.advance_us(us); now += us; }
    void out(uint16_t port, uint8_t value) { f.out(port, value); tick(); }
    uint8_t in(uint16_t port) { uint8_t value = f.in(port); tick(); return value; }
    uint8_t show(uint16_t port) {
        uint8_t value = in(port);
        note(strf("IN %03x %02x", port, value));
        return value;
    }
    bool wait(const std::function<bool()> &done, uint64_t limit_us, const char *what) {
        for (uint64_t t = 0; t < limit_us; ++t) {
            if (done()) return true;
            tick();
        }
        note(std::string("TIMEOUT ") + what);
        return false;
    }
    bool rqm(uint8_t mask, uint8_t value, uint64_t limit, const char *what) {
        return wait([&] { return (f.in(0x3f4) & mask) == value; }, limit, what);
    }
    void command(std::initializer_list<uint8_t> bytes) { command(std::vector<uint8_t>(bytes)); }
    void command(const std::vector<uint8_t> &bytes) {
        for (uint8_t byte : bytes) {
            if (!rqm(0xc0, 0x80, 200000, "command byte")) return;
            out(0x3f5, byte);
        }
    }
    std::vector<uint8_t> results(uint64_t limit = 3000000) {
        std::vector<uint8_t> bytes;
        if (!rqm(0xc0, 0xc0, limit, "result")) return bytes;
        while (bytes.size() < 16) {
            bytes.push_back(in(0x3f5));
            if (!wait([&] { return (f.in(0x3f4) & 0x80) != 0; }, 1000, "result rqm")) break;
            if ((f.in(0x3f4) & 0xc0) != 0xc0) break;
        }
        std::string line = "RES";
        for (uint8_t byte : bytes) line += strf(" %02x", byte);
        note(line);
        return bytes;
    }
    bool irq(uint64_t limit = 3000000) {
        bool ok = wait([&] { return f.irq(); }, limit, "irq");
        if (ok) note("IRQ");
        return ok;
    }
    std::vector<uint8_t> sense() { command({0x08}); return results(); }
    // The BIOS's controller reset: DOR reset low, then high, IRQ, four senses.
    void controller_reset(uint8_t dor = 0x0c) {
        out(0x3f2, dor & ~0x04);
        out(0x3f2, dor);
        irq();
        for (int i = 0; i < 4; ++i) sense();
    }
    // DMA from the controller, TC on byte tc_at (or none).
    std::vector<uint8_t> dma_in(size_t count, long tc_at = -1, uint64_t limit = 3000000) {
        std::vector<uint8_t> bytes;
        for (size_t i = 0; i < count; ++i) {
            if (!wait([&] { return f.drq(); }, limit, "drq")) break;
            bytes.push_back(f.dack_from(long(i) == tc_at));
            tick();
        }
        note(strf("DMA-IN %zu %08x", bytes.size(), crc32(bytes.data(), bytes.size())));
        return bytes;
    }
    size_t dma_out(const std::vector<uint8_t> &bytes, long tc_at = -1, uint64_t limit = 3000000) {
        size_t sent = 0;
        for (size_t i = 0; i < bytes.size(); ++i) {
            if (!wait([&] { return f.drq(); }, limit, "drq")) break;
            f.dack_to(bytes[i], long(i) == tc_at);
            tick();
            ++sent;
        }
        note(strf("DMA-OUT %zu", sent));
        return sent;
    }
    // A disk file for both models (a fresh copy per model).
    pc386::FloppyDisk disk(unsigned drive, const Disk &d) {
        pc386::FloppyDisk out;
        out.path = strf("%s/%s-%u.img", directory.c_str(), rtl ? "rtl" : "bochs", disks_made++);
        std::vector<uint8_t> bytes = make_image(d.geometry, d.seed);
        std::ofstream(out.path, std::ios::binary).write(reinterpret_cast<const char *>(bytes.data()), bytes.size());
        out.cylinders = d.geometry.cylinders;
        out.heads = d.geometry.heads;
        out.spt = d.geometry.spt;
        out.write_protected = d.write_protected;
        if (files.size() <= drive) files.resize(drive + 1);
        files[drive] = {out.path, d.geometry};
        return out;
    }
    void insert(unsigned drive, const Disk &d) { f.insert(drive, disk(drive, d)); tick(); }
    // Power on with disks already in their drives.
    void boot(const Disk *a, const Disk *b = nullptr) {
        if (a) f.insert(0, disk(0, *a));
        if (b) f.insert(1, disk(1, *b));
        f.hardware_reset();
    }
    // The bytes a sector of a disk started with.
    static std::vector<uint8_t> original(const Disk &d, unsigned c, unsigned h, unsigned s, unsigned count = 1) {
        std::vector<uint8_t> all = make_image(d.geometry, d.seed);
        size_t lba = (size_t(c) * d.geometry.heads + h) * d.geometry.spt + s - 1;
        return std::vector<uint8_t>(all.begin() + lba * 512, all.begin() + (lba + count) * 512);
    }
};

// Read/write command bytes: op, drive/head, C, H, R, N=2, EOT, GPL, DTL.
std::vector<uint8_t> rw(uint8_t op, unsigned drive, unsigned c, unsigned h, unsigned r, unsigned eot,
                        uint8_t n = 2) {
    return {op, uint8_t(h << 2 | drive), uint8_t(c), uint8_t(h), uint8_t(r), n, uint8_t(eot), 0x1b, 0xff};
}

// ---- Cases -------------------------------------------------------------------
struct Case {
    const char *name;
    unsigned type_a, type_b;
    bool rtl_only;
    std::function<void(Guest &)> run;
};

// Power on, reset the controller as the BIOS does, motor on, Specify
// (SRT 0Dh, HUT 0Fh, HLT 1, DMA), recalibrate.
void start(Guest &g, unsigned drive = 0) {
    g.controller_reset();
    g.out(0x3f2, uint8_t(drive ? 0x2d : 0x1c));
    g.command({0x03, 0xdf, 0x02});
    g.command({0x07, uint8_t(drive)});
    g.irq();
    g.sense();
}

void seek(Guest &g, unsigned drive, unsigned head, unsigned cylinder) {
    g.command({0x0f, uint8_t(head << 2 | drive), uint8_t(cylinder)});
    g.irq();
    g.sense();
}

void read_back(Guest &g, unsigned drive, unsigned c, unsigned h, unsigned r, unsigned count, unsigned eot,
               const std::vector<uint8_t> &expect, const char *what) {
    seek(g, drive, h, c);
    g.command(rw(0xe6, drive, c, h, r, eot));
    auto bytes = g.dma_in(count * 512, long(count * 512 - 1));
    g.results();
    g.check(bytes == expect, std::string(what) + ": read-back data");
}

const Disk A144{G144, 1}, B12{G12, 2};

std::vector<Case> cases() {
    std::vector<Case> list;
    list.push_back({"registers", 4, 2, false, [](Guest &g) {
        g.show(0x3f2); g.show(0x3f4); g.show(0x3f7);
        g.out(0x3f2, 0x08);
        g.check(g.show(0x3f4) == 0x10, "MSR in reset");
        g.out(0x3f2, 0x0c);
        g.check(g.irq(), "reset interrupt");
        for (int i = 0; i < 4; ++i) {
            auto r = g.sense();
            g.check(r.size() == 2 && r[0] == 0xc0 + i, strf("sense %d after reset", i));
        }
        g.show(0x3f4);
        g.command({0x03, 0xdf, 0x02});
        g.show(0x3f4);
        g.command({0x10});
        g.check(g.results() == std::vector<uint8_t>{0x90}, "Version 90h");
        g.command({0x94}); g.results();
        g.command({0x14}); g.results();
        g.command({0x13, 0x00, 0x57, 0x00});
        g.command({0x12, 0x00});
        g.command({0x0e}); g.results();
        g.command({0x94}); g.results();
        g.out(0x3f2, 0x08); g.out(0x3f2, 0x0c);
        g.irq();
        for (int i = 0; i < 4; ++i) g.sense();
        g.command({0x0e}); g.results();      // Configure survives a reset when locked
        g.show(0x3f2);
    }});
    list.push_back({"soft_reset_lock", 4, 2, false, [](Guest &g) {
        g.controller_reset();
        g.command({0x0e});
        auto r = g.results();
        g.check(r.size() == 10 && (r[7] & 0x80) == 0, "a software reset leaves LOCK clear");
    }});
    list.push_back({"invalid", 4, 2, false, [](Guest &g) {
        g.controller_reset();
        for (uint8_t op : {0x00, 0x01, 0x1f, 0x83, 0x18, 0xa5}) {
            g.command({op});
            auto r = g.results();
            g.check(r == std::vector<uint8_t>{0x80}, strf("invalid %02x", op));
        }
        g.check(g.sense() == std::vector<uint8_t>{0x80}, "sense with nothing pending");
    }});
    list.push_back({"seek", 4, 2, false, [](Guest &g) {
        start(g);
        seek(g, 0, 0, 16);
        g.command({0xcf, 0x00, 5}); g.irq(); g.sense();
        g.command({0x8f, 0x04, 3}); g.irq(); g.sense();
        g.command({0x04, 0x00}); g.results();
        g.command({0x04, 0x04}); g.results();
        seek(g, 0, 1, 85);
        g.command({0x07, 0x00}); g.irq(); g.sense();
        g.command({0x04, 0x00}); g.results();
        g.out(0x3f2, 0x2d);
        g.command({0x07, 0x01}); g.irq(); g.sense();
        g.command({0x04, 0x01}); g.results();
        g.show(0x3f2);
    }});
    list.push_back({"seek_below_zero", 4, 2, false, [](Guest &g) {
        start(g);
        seek(g, 0, 0, 2);
        g.command({0x8f, 0x00, 5}); g.irq();
        auto r = g.sense();
        g.check(r.size() == 2 && r[0] == 0x70, "relative seek below 0: ST0 70h");
    }});
    list.push_back({"recalibrate_no_drive", 4, 0, false, [](Guest &g) {
        g.controller_reset();
        g.out(0x3f2, 0x2d);
        g.command({0x07, 0x01}); g.irq();
        auto r = g.sense();
        g.check(r.size() == 2 && r[0] == 0x71, "recalibrate with no drive: ST0 71h");
        g.out(0x3f2, 0x0c);
        g.command({0x07, 0x00}); g.irq();
        r = g.sense();
        g.check(r.size() == 2 && r[0] == 0x70, "recalibrate with the motor off: ST0 70h");
    }});
    list.push_back({"read_track", 4, 2, false, [](Guest &g) {
        { const Disk boot_disk = A144; g.boot(&boot_disk); }
        start(g);
        g.command(rw(0xe6, 0, 0, 0, 1, 18));
        auto bytes = g.dma_in(18 * 512, 18 * 512 - 1);
        auto r = g.results();
        g.check(bytes == Guest::original(A144, 0, 0, 1, 18), "track 0 data");
        g.check(r == std::vector<uint8_t>{0x04, 0, 0, 0, 1, 1, 2}, "track 0 result: next is head 1 sector 1");
        // Multi-track: 17 and 18 of head 0, then 1 and 2 of head 1.
        seek(g, 0, 0, 5);
        g.command(rw(0xe6, 0, 5, 0, 17, 18));
        bytes = g.dma_in(4 * 512, 4 * 512 - 1);
        r = g.results();
        auto expect = Guest::original(A144, 5, 0, 17, 2);
        auto second = Guest::original(A144, 5, 1, 1, 2);
        expect.insert(expect.end(), second.begin(), second.end());
        g.check(bytes == expect, "multi-track data");
        g.check(r == std::vector<uint8_t>{0x04, 0, 0, 5, 1, 3, 2}, "multi-track result");
    }});
    list.push_back({"read_eot_no_tc", 4, 2, false, [](Guest &g) {
        { const Disk boot_disk = A144; g.boot(&boot_disk); }
        start(g);
        seek(g, 0, 0, 1);
        g.command(rw(0x46, 0, 1, 0, 1, 3));
        g.dma_in(3 * 512);
        auto r = g.results();
        g.check(r.size() == 7 && r[0] == 0x40 && r[1] == 0x80, "past EOT without TC: ST1 EN");
    }});
    list.push_back({"read_tc_mid_sector", 4, 2, false, [](Guest &g) {
        { const Disk boot_disk = A144; g.boot(&boot_disk); }
        start(g);
        seek(g, 0, 1, 7);
        g.command(rw(0xe6, 0, 7, 1, 9, 18));
        auto bytes = g.dma_in(100, 99);
        auto r = g.results();
        auto expect = Guest::original(A144, 7, 1, 9);
        expect.resize(100);
        g.check(bytes == expect, "TC mid-sector data");
        g.check(r == std::vector<uint8_t>{0x04, 0, 0, 7, 1, 10, 2}, "TC mid-sector result");
    }});
    list.push_back({"write_read", 4, 2, false, [](Guest &g) {
        { const Disk boot_disk = A144; g.boot(&boot_disk); }
        start(g);
        seek(g, 0, 1, 5);
        std::vector<uint8_t> data(1024);
        for (size_t i = 0; i < data.size(); ++i) data[i] = uint8_t(i * 3 + 0x11);
        g.command(rw(0xc5, 0, 5, 1, 3, 18));
        g.dma_out(data, 1023);
        auto r = g.results();
        g.check(r == std::vector<uint8_t>{0x04, 0, 0, 5, 1, 5, 2}, "write result");
        read_back(g, 0, 5, 1, 3, 2, 18, data, "write");
    }});
    list.push_back({"read_id", 4, 2, false, [](Guest &g) {
        { const Disk boot_disk = A144; g.boot(&boot_disk); }
        start(g);
        seek(g, 0, 0, 3);
        for (int i = 0; i < 3; ++i) { g.command({0x4a, 0x00}); g.results(); }
        g.command({0x4a, 0x04}); g.results();
    }});
    list.push_back({"errors", 4, 2, false, [](Guest &g) {
        { const Disk boot_disk = A144; g.boot(&boot_disk); }
        start(g);
        // The head in the ID differs from the drive and head byte.
        g.command({0xe6, 0x00, 0x00, 0x01, 0x01, 0x02, 0x12, 0x1b, 0xff});
        auto r = g.results();
        g.check(r.size() == 7 && r[0] == 0x40 && r[1] == 0x04, "head mismatch: ST1 ND");
        g.command(rw(0xe6, 0, 0, 0, 19, 19));
        r = g.results();
        g.check(r.size() == 7 && r[0] == 0x40 && r[1] == 0x04, "sector past the track: ST1 ND");
        g.command(rw(0xe6, 0, 80, 0, 1, 18));
        r = g.results();
        g.check(r.size() == 7 && r[0] == 0x40 && r[1] == 0x04, "cylinder past the disk: ST1 ND");
    }});
    list.push_back({"format", 4, 2, false, [](Guest &g) {
        { const Disk boot_disk = B12; g.boot(nullptr, &boot_disk); }
        start(g, 1);
        seek(g, 1, 0, 2);
        std::vector<uint8_t> ids;
        for (unsigned r = 1; r <= 15; ++r) ids.insert(ids.end(), {2, 0, uint8_t(r), 2});
        g.command({0x4d, 0x01, 0x02, 0x0f, 0x54, 0xf6});
        g.dma_out(ids, long(ids.size() - 1));
        auto r = g.results();
        g.check(r == std::vector<uint8_t>{0x01, 0, 0, 0, 0, 0, 0}, "format result");
        read_back(g, 1, 2, 0, 1, 15, 15, std::vector<uint8_t>(15 * 512, 0xf6), "format");
        read_back(g, 1, 2, 1, 1, 1, 15, Guest::original(B12, 2, 1, 1), "format's other head");
    }});
    list.push_back({"disk_change", 4, 2, false, [](Guest &g) {
        { const Disk boot_disk = A144; g.boot(&boot_disk); }
        start(g);
        g.check((g.show(0x3f7) & 0x80) == 0, "no change after reset");
        g.f.eject(0); g.tick();
        g.check((g.show(0x3f7) & 0x80) != 0, "change after eject");
        g.insert(0, Disk{G720, 3});
        g.check((g.show(0x3f7) & 0x80) != 0, "change after insert");
        seek(g, 0, 0, 0);
        g.check((g.show(0x3f7) & 0x80) != 0, "a seek that does not step leaves the change");
        seek(g, 0, 0, 1);
        g.check((g.show(0x3f7) & 0x80) == 0, "a step clears the change");
        g.f.eject(0); g.tick();
        g.out(0x3f2, 0x0c);
        g.check(g.show(0x3f7) == 0x7f, "DIR with the motor off");
        g.out(0x3f2, 0x1c);
        g.show(0x3f7);
    }});
    list.push_back({"media_360_in_12", 4, 2, false, [](Guest &g) {
        const Disk d{G360, 4};
        { const Disk boot_disk = d; g.boot(nullptr, &boot_disk); }
        start(g, 1);
        seek(g, 1, 1, 39);
        g.command(rw(0xe6, 1, 39, 1, 1, 9));
        auto bytes = g.dma_in(9 * 512, 9 * 512 - 1);
        g.results();
        g.check(bytes == Guest::original(d, 39, 1, 1, 9), "360K cylinder 39 head 1");
    }});
    list.push_back({"media_720_in_144", 4, 2, false, [](Guest &g) {
        const Disk d{G720, 5};
        { const Disk boot_disk = d; g.boot(&boot_disk); }
        start(g);
        read_back(g, 0, 79, 1, 8, 2, 9, Guest::original(d, 79, 1, 8, 2), "720K");
    }});
    list.push_back({"media_dmf", 4, 2, false, [](Guest &g) {
        const Disk d{GDMF, 6};
        { const Disk boot_disk = d; g.boot(&boot_disk); }
        start(g);
        read_back(g, 0, 40, 0, 20, 2, 21, Guest::original(d, 40, 0, 20, 2), "DMF");
    }});
    list.push_back({"media_288", 5, 2, false, [](Guest &g) {
        const Disk d{G288, 7};
        { const Disk boot_disk = d; g.boot(&boot_disk); }
        start(g);
        read_back(g, 0, 79, 1, 35, 2, 36, Guest::original(d, 79, 1, 35, 2), "2.88M");
    }});
    list.push_back({"no_media", 4, 2, false, [](Guest &g) {
        start(g);
        g.command(rw(0xe6, 0, 0, 0, 1, 18));
        g.results(1500000);
        g.controller_reset(0x1c);
    }});
    list.push_back({"dsr_reset", 4, 2, false, [](Guest &g) {
        start(g);
        g.out(0x3f4, 0x80);
        g.irq();
        for (int i = 0; i < 4; ++i) g.sense();
        g.show(0x3f2);
        g.show(0x3f4);
    }});
    // ---- Cases fdc_divergences.toml lists
    list.push_back({"dor_select", 4, 2, false, [](Guest &g) {
        { const Disk a = A144, b = B12; g.boot(&a, &b); }
        start(g);
        g.out(0x3f2, 0x3c);                                  // both motors, drive 0 selected
        g.f.eject(0); g.tick();                              // drive 0's change is set
        g.command({0x04, 0x01}); g.results();                // Sense Drive Status names drive 1
        g.show(0x3f2);
        g.show(0x3f7);
        seek(g, 1, 0, 2);                                    // a step clears drive 1's change
        g.show(0x3f2);
        g.show(0x3f7);
        g.out(0x3f2, 0x3c);                                  // drive 0 again in Bochs
        read_back(g, 1, 2, 0, 1, 1, 15, Guest::original(B12, 2, 0, 1, 1), "drive 1's read under DOR's drive 0");
        g.show(0x3f2);
        g.show(0x3f7);
    }});
    list.push_back({"status_registers", 4, 2, false, [](Guest &g) {
        g.check(g.show(0x3f0) == 0xff, "3F0h reads FFh");
        g.check(g.show(0x3f1) == 0xff, "3F1h reads FFh");
        g.check(g.show(0x3f3) == 0xff, "3F3h reads FFh");
    }});
    list.push_back({"fm_read", 4, 2, false, [](Guest &g) {
        { const Disk boot_disk = A144; g.boot(&boot_disk); }
        start(g);
        g.command(rw(0x06, 0, 0, 0, 1, 18));
        auto r = g.results();
        g.check(r.size() == 7 && r[0] == 0x40 && r[1] == 0x01, "FM read: ST1 MA");
        g.command(rw(0x05, 0, 0, 0, 1, 18));
        r = g.results();
        g.check(r.size() == 7 && r[0] == 0x40 && r[1] == 0x01, "FM write: ST1 MA");
        g.command({0x0d, 0x00, 0x02, 0x12, 0x54, 0xf6});
        r = g.results();
        g.check(r.size() == 7 && r[0] == 0x40 && r[1] == 0x01, "FM format: ST1 MA");
    }});
    list.push_back({"read_deleted", 4, 2, false, [](Guest &g) {
        { const Disk boot_disk = A144; g.boot(&boot_disk); }
        start(g);
        g.command(rw(0x4c, 0, 0, 0, 2, 18));
        auto bytes = g.dma_in(512, 511);
        auto r = g.results();
        g.check(bytes == Guest::original(A144, 0, 0, 2), "Read Deleted data");
        g.check(r.size() == 7 && r[0] == 0x00 && r[2] == 0x40, "Read Deleted without SK: ST2 CM");
        g.command(rw(0x6c, 0, 0, 0, 3, 18));
        g.dma_in(512, 511);
        r = g.results();
        g.check(r.size() == 7 && r[0] == 0x00 && r[2] == 0x00, "Read Deleted with SK: no CM");
    }});
    list.push_back({"write_deleted", 4, 2, false, [](Guest &g) {
        { const Disk boot_disk = A144; g.boot(&boot_disk); }
        start(g);
        std::vector<uint8_t> data(512, 0xa5);
        g.command(rw(0x49, 0, 0, 0, 4, 18));
        g.dma_out(data, 511);
        auto r = g.results();
        g.check(r == std::vector<uint8_t>{0x00, 0, 0, 0, 0, 5, 2}, "Write Deleted result");
        read_back(g, 0, 0, 0, 4, 1, 18, data, "Write Deleted");
    }});
    list.push_back({"write_eot", 4, 2, false, [](Guest &g) {
        { const Disk boot_disk = A144; g.boot(&boot_disk); }
        start(g);
        seek(g, 0, 1, 9);
        std::vector<uint8_t> data(1024, 0x3c);
        g.command(rw(0xc5, 0, 9, 1, 17, 18));
        g.dma_out(data, 1023);
        auto r = g.results();
        g.check(r == std::vector<uint8_t>{0x00, 0, 0, 10, 0, 1, 2}, "write ending at EOT: next is C+1, H 0, R 1");
    }});
    list.push_back({"write_tc_mid_sector", 4, 2, false, [](Guest &g) {
        { const Disk boot_disk = A144; g.boot(&boot_disk); }
        start(g);
        // A read first, so that a controller that writes its whole buffer
        // writes that sector's tail.
        read_back(g, 0, 6, 0, 1, 1, 18, Guest::original(A144, 6, 0, 1), "the sector before");
        std::vector<uint8_t> data(203);
        for (size_t i = 0; i < data.size(); ++i) data[i] = uint8_t(i + 0x40);
        g.command(rw(0xc5, 0, 6, 0, 7, 18));
        g.dma_out(data, long(data.size() - 1));
        auto r = g.results();
        g.check(r == std::vector<uint8_t>{0x00, 0, 0, 6, 0, 8, 2}, "write with TC mid-sector: result");
        auto expect = data;
        expect.resize(512, 0x00);
        read_back(g, 0, 6, 0, 7, 1, 18, expect, "write with TC mid-sector: the rest of the sector is 00h");
    }});
    list.push_back({"write_protected", 4, 2, false, [](Guest &g) {
        { const Disk boot_disk = Disk{G144, 8, true}; g.boot(&boot_disk); }
        start(g);
        g.command(rw(0xc5, 0, 0, 0, 1, 18));
        auto r = g.results();
        g.check(r.size() == 7 && r[0] == 0x40 && r[1] == 0x02 && r[2] == 0x00, "write-protected: ST1 NW");
        g.command({0x04, 0x00});
        r = g.results();
        g.check(r.size() == 1 && (r[0] & 0x40), "ST3 WP");
    }});
    list.push_back({"unsupported", 4, 2, false, [](Guest &g) {
        { const Disk boot_disk = A144; g.boot(&boot_disk); }
        start(g);
        for (uint8_t op : {0x56, 0x42, 0x51, 0x0b, 0x36}) {   // Verify, Read Track, Scan Equal; Bochs's 0Bh, 36h
            g.command(rw(op, 0, 0, 0, 1, 18));
            auto r = g.results(200000);
            g.check(r == std::vector<uint8_t>{0x80}, strf("%02x invalid", op));
        }
    }});
    list.push_back({"format_bad_id", 4, 2, false, [](Guest &g) {
        { const Disk boot_disk = B12; g.boot(nullptr, &boot_disk); }
        start(g, 1);
        seek(g, 1, 0, 3);
        std::vector<uint8_t> ids;
        for (unsigned r = 1; r <= 15; ++r) ids.insert(ids.end(), {3, uint8_t(r == 4), uint8_t(r), 2});
        g.command({0x4d, 0x01, 0x02, 0x0f, 0x54, 0xe5});
        g.dma_out(ids, long(ids.size() - 1));
        auto r = g.results();
        g.check(r.size() == 7 && r[0] == 0x41 && r[1] == 0x04, "an ID with the wrong head ends the format");
    }});
    list.push_back({"insert_into_empty", 4, 2, false, [](Guest &g) {
        start(g);
        g.check((g.show(0x3f7) & 0x80) == 0, "empty drive after reset");
        g.insert(0, A144);
        g.check((g.show(0x3f7) & 0x80) != 0, "a disk arriving sets the change");
    }});
    list.push_back({"dma_gate", 4, 2, false, [](Guest &g) {
        { const Disk boot_disk = A144; g.boot(&boot_disk); }
        start(g);
        seek(g, 0, 0, 1);
        g.out(0x3f2, 0x14);                                // DMA and INT gate off
        g.command({0x0f, 0x00, 2});
        g.tick(200000);
        g.note(g.f.irq() ? "INT high" : "INT low");
        g.check(!g.f.irq(), "INT gated off");
        g.out(0x3f2, 0x1c);
        g.note(g.f.irq() ? "INT high" : "INT low");
        g.check(g.f.irq(), "INT gated back on");
        g.sense();
    }});
    // ---- RTL only: non-DMA transfers and time
    list.push_back({"pio_read_write", 4, 2, true, [](Guest &g) {
        { const Disk boot_disk = A144; g.boot(&boot_disk); }
        start(g);
        seek(g, 0, 0, 2);
        g.command({0x03, 0xdf, 0x03});                       // ND
        std::vector<uint8_t> data(512);
        for (size_t i = 0; i < data.size(); ++i) data[i] = uint8_t(255 - i);
        g.command(rw(0x45, 0, 2, 0, 18, 18));
        size_t sent = 0;
        for (; sent < data.size(); ++sent) {
            if (!g.rqm(0xe0, 0xa0, 1000000, "pio write byte")) break;
            g.check(g.f.irq(), "INT with each non-DMA byte");
            g.out(0x3f5, data[sent]);
        }
        auto r = g.results();
        g.check(sent == 512, "512 bytes written through 3F5h");
        g.check(r.size() == 7 && r[0] == 0x40 && r[1] == 0x80 && r[5] == 18,
                "non-DMA write ends at EOT with EN (no TC)");
        g.command(rw(0x46, 0, 2, 0, 18, 18));
        std::vector<uint8_t> back;
        while (back.size() < 512) {
            if (!g.rqm(0xe0, 0xe0, 1000000, "pio read byte")) break;
            back.push_back(g.in(0x3f5));
        }
        g.results();
        g.check(back == data, "non-DMA read-back");
        g.command({0x0e});
        auto dump = g.results();
        g.check(dump.size() == 10 && (dump[5] & 1), "Dumpreg shows ND");
    }});
    list.push_back({"fence_and_swap", 4, 2, true, [](Guest &g) {
        auto &rtl = dynamic_cast<Rtl &>(g.f);
        { const Disk boot_disk = A144; g.boot(&boot_disk); }
        start(g);
        seek(g, 0, 0, 4);
        std::vector<uint8_t> data(3 * 512);
        for (size_t i = 0; i < data.size(); ++i) data[i] = uint8_t(i * 7 + 3);
        g.command(rw(0xc5, 0, 4, 0, 1, 18));
        // Freeze with the first byte offered: the offer stands, so not fenced.
        g.wait([&] { return g.f.drq(); }, 2000000, "first drq");
        rtl.set_frozen(true);
        g.check(!rtl.fenced(), "not fenced while a write's first byte is offered");
        rtl.set_frozen(false);
        // Freeze inside the first sector: it runs to its end and is stored.
        g.dma_out(std::vector<uint8_t>(data.begin(), data.begin() + 100));
        rtl.set_frozen(true);
        g.check(!rtl.fenced(), "not fenced while a sector is being collected");
        g.dma_out(std::vector<uint8_t>(data.begin() + 100, data.begin() + 512));
        g.check(g.wait([&] { return rtl.fenced(); }, 10000, "fenced"), "fenced once the started sector is stored");
        g.tick(5000);
        g.check(!g.f.drq(), "the next sector waits at the fence");
        g.check(rtl.fenced(), "still fenced");
        rtl.set_frozen(false);
        g.dma_out(std::vector<uint8_t>(data.begin() + 512, data.end()), 1023);
        auto r = g.results();
        g.check(r == std::vector<uint8_t>{0x00, 0, 0, 4, 0, 4, 2}, "the write finishes after the fence");
        read_back(g, 0, 4, 0, 1, 3, 18, data, "fenced write");
        // A disk swapped in the middle of a write: the command ends, not ready,
        // and the new disk is untouched.
        seek(g, 0, 0, 8);
        g.command(rw(0xc5, 0, 8, 0, 1, 18));
        g.dma_out(std::vector<uint8_t>(300, 0xee));
        const Disk other{G144, 9};
        g.f.eject(0); g.tick();
        g.insert(0, other);
        g.dma_out(std::vector<uint8_t>(724, 0xee), 723, 200000);
        r = g.results();
        g.check(r.size() == 7 && r[0] == 0x48, "a disk that left ends the write: ST0 IC=01 NR");
        read_back(g, 0, 8, 0, 1, 2, 18, Guest::original(other, 8, 0, 1, 2), "the disk that replaced it");
        // A disk swapped while a write waits for spin-up: the write ends.
        g.out(0x3f2, 0x0c);
        g.out(0x3f2, 0x1c);
        g.command(rw(0xc5, 0, 8, 0, 1, 18));
        g.tick(1000);
        g.f.eject(0); g.tick();
        g.insert(0, Disk{G144, 10});
        r = g.results(1000000);
        g.check(r.size() == 7 && r[0] == 0x48, "a disk that left during spin-up ends the write: ST0 IC=01 NR");
        g.check(!g.f.drq(), "and it asks the new disk for nothing");
        // A disk swapped in the clock its read's last command byte arrives.
        g.controller_reset(0x1c);
        auto bytes = rw(0xe6, 0, 8, 0, 1, 18);
        g.command(std::vector<uint8_t>(bytes.begin(), bytes.end() - 1));
        g.rqm(0xc0, 0x80, 1000, "last command byte");
        rtl.eject_with_write = 0;
        g.out(0x3f5, bytes.back());
        g.insert(0, Disk{G144, 11});
        r = g.results(1000000);
        g.check(r.size() == 7 && r[0] == 0x48, "a disk that left with the last command byte ends the read: NR");
        // A disk swapped while Read ID waits out its sector time (the drive
        // has spun up, so Read ID is past its ready wait).
        g.tick(600000);
        g.command({0x4a, 0x00});
        g.tick(100);
        g.f.eject(0); g.tick();
        g.insert(0, Disk{G144, 12});
        r = g.results(1000000);
        g.check(r.size() == 7 && r[0] == 0x48, "a disk that left during Read ID ends it: NR");
        // ...and one leaving in each of the last clocks of that wait, the
        // clock it ends included: either the ID came first or the command
        // ends not ready. Each run starts on the same phase of the
        // microsecond strobe, so the wait ends on the same clock.
        rtl.step(US_CLOCKS - rtl.clocks % US_CLOCKS);
        g.command({0x4a, 0x00});
        uint64_t took = 0;
        while (!g.f.irq() && took < 100000) { rtl.step(1); ++took; }
        g.check(g.f.irq(), "Read ID finishes");
        g.results();
        for (uint64_t back = 1; back <= 4; ++back) {
            rtl.step(US_CLOCKS - rtl.clocks % US_CLOCKS);
            g.command({0x4a, 0x00});
            rtl.step(took - back);
            const bool done = g.f.irq();
            g.f.eject(0);
            g.insert(0, Disk{G144, unsigned(12 + back)});
            r = g.results(1000000);
            g.check(done || (r.size() == 7 && r[0] == 0x48),
                    strf("a disk that left %llu clocks before Read ID's IRQ ends it: NR", (unsigned long long)back - 1));
        }
        // A disk that leaves while a read's last byte waits for its DACK, and
        // one that leaves before a format's TC: both end not ready.
        g.command(rw(0x66, 0, 8, 0, 1, 1));                  // no MT: sector 1 is the last
        g.dma_in(511);
        g.wait([&] { return g.f.drq(); }, 100000, "last byte offered");
        g.f.eject(0); g.tick();
        g.insert(0, Disk{G144, 17});
        g.dma_in(1);
        r = g.results(1000000);
        g.check(r.size() == 7 && r[0] == 0x48, "a disk that left before the read's last DACK ends it: NR");
        g.command({0x4d, 0x00, 0x02, 18, 0x54, 0xf6});
        g.dma_out({8, 0});
        g.wait([&] { return g.f.drq(); }, 100000, "format's third ID byte offered");
        g.f.eject(0); g.tick();
        g.insert(0, Disk{G144, 18});
        g.dma_out({1}, 0);
        r = g.results(1000000);
        g.check(r.size() == 7 && r[0] == 0x48, "a disk that left before a format's TC ends it: NR");
        // A controller reset in each of the first clocks after a write's last
        // DACK: the sector the guest finished is stored all the same.
        for (unsigned after = 0; after < 4; ++after) {
            std::vector<uint8_t> sector(512);
            for (size_t i = 0; i < sector.size(); ++i) sector[i] = uint8_t(i * 5 + after * 17 + 1);
            g.command(rw(0x45, 0, 8, 0, 1, 1));
            g.dma_out(std::vector<uint8_t>(sector.begin(), sector.end() - 1));
            g.wait([&] { return g.f.drq(); }, 100000, "write's last byte offered");
            rtl.dack_then(sector.back(), after, [&] { rtl.out(0x3f2, 0x18); });
            g.out(0x3f2, 0x1c);
            g.irq();
            for (int i = 0; i < 4; ++i) g.sense();
            read_back(g, 0, 8, 0, 1, 1, 1, sector,
                      strf("a sector finished %u clocks before a DOR reset", after).c_str());
        }
        // The same for a DSR reset (which also turns the motor off), and for a
        // format's ID whose fourth byte comes just before a DOR reset.
        for (unsigned after = 0; after < 2; ++after) {
            std::vector<uint8_t> sector(512);
            for (size_t i = 0; i < sector.size(); ++i) sector[i] = uint8_t(i * 3 + after * 29 + 7);
            g.command(rw(0x45, 0, 8, 0, 2, 2));
            g.dma_out(std::vector<uint8_t>(sector.begin(), sector.end() - 1));
            g.wait([&] { return g.f.drq(); }, 100000, "write's last byte offered");
            rtl.dack_then(sector.back(), after, [&] { rtl.out(0x3f4, 0x80); });
            g.irq();
            for (int i = 0; i < 4; ++i) g.sense();
            g.out(0x3f2, 0x1c);
            read_back(g, 0, 8, 0, 2, 1, 2, sector,
                      strf("a sector finished %u clocks before a DSR reset", after).c_str());
            g.command({0x4d, 0x00, 0x02, 18, 0x54, uint8_t(0xa0 + after)});
            g.dma_out({8, 0, 1});
            g.wait([&] { return g.f.drq(); }, 100000, "format's fourth ID byte offered");
            rtl.dack_then(2, after, [&] { rtl.out(0x3f2, 0x18); });
            g.out(0x3f2, 0x1c);
            g.irq();
            for (int i = 0; i < 4; ++i) g.sense();
            read_back(g, 0, 8, 0, 1, 1, 1, std::vector<uint8_t>(512, uint8_t(0xa0 + after)),
                      strf("a format ID finished %u clocks before a DOR reset", after).c_str());
        }
        // A format sent at once after a reset that cut into another's store:
        // the store keeps its own fill byte.
        {
            g.command({0x4d, 0x00, 0x02, 18, 0x54, 0xc1});
            g.dma_out({8, 0, 1});
            g.wait([&] { return g.f.drq(); }, 100000, "format's fourth ID byte offered");
            bool overlapped = false;
            rtl.dack_then(2, 0, [&] {
                rtl.out(0x3f2, 0x18);
                rtl.out(0x3f2, 0x1c);
                rtl.step(48);                                // past the reset's 10 us
                for (uint8_t b : {0x4d, 0x00, 0x02, 18, 0x54, 0xc2}) rtl.out(0x3f5, b);
                rtl.step(4);
                overlapped = rtl.busy();
            });
            g.check(overlapped, "the second format reached its drive while the first's store ran");
            g.controller_reset(0x1c);
            read_back(g, 0, 8, 0, 1, 1, 1, std::vector<uint8_t>(512, 0xc1), "a store's fill byte across a new format");
        }
        // A TC on a write's fourth byte: the store's first word is the last
        // one written.
        {
            const std::vector<uint8_t> four{0x11, 0x22, 0x33, 0x44};
            g.command(rw(0x45, 0, 8, 0, 3, 3));
            g.dma_out(four, 3);
            g.results();
            auto expect = four;
            expect.resize(512, 0x00);
            read_back(g, 0, 8, 0, 3, 1, 3, expect, "a write with TC in its first word");
        }
        // A disk that leaves while a write's last sector is stored ends it
        // not ready; the store still lands on the image it was aimed at.
        {
            g.command(rw(0x45, 0, 8, 0, 4, 4));
            g.dma_out(std::vector<uint8_t>(511, 0x5a));
            g.wait([&] { return g.f.drq(); }, 100000, "write's last byte offered");
            rtl.dack_then(0x5a, 2, [&] { g.f.eject(0); });
            g.wait([&] { return !rtl.busy(); }, 100000, "store landed");
            g.insert(0, Disk{G144, 19});
            r = g.results(1000000);
            g.check(r.size() == 7 && r[0] == 0x48, "a disk that left during the last store ends the write: NR");
        }
    }});
    list.push_back({"timing", 4, 2, true, [](Guest &g) {
        g.boot(&A144);
        g.controller_reset();
        g.command({0x03, 0xdf, 0x02});                       // SRT 0Dh: 3 steps of 2 ms at 250 kbit/s
        g.out(0x3f2, 0x1c);                                  // motor on
        uint64_t motor = g.clock_us();
        g.command({0x0f, 0x00, 10});
        uint64_t started = g.clock_us();
        g.irq();
        uint64_t seek_us = g.clock_us() - started;
        g.check(seek_us >= 74990 && seek_us <= 75100, strf("seek of 10 at SRT 0Dh: %llu us, not 75 ms",
                                                           (unsigned long long)seek_us));
        g.sense();
        g.command(rw(0xe6, 0, 10, 0, 1, 18));
        g.wait([&] { return g.f.drq(); }, 2000000, "first byte");
        g.check(g.clock_us() - motor >= 500000, "the first byte waits for spin-up");
        uint64_t first = g.clock_us();
        g.dma_in(512, 511);
        uint64_t sector_us = g.clock_us() - first;
        g.check(sector_us >= 511 * 16 && sector_us <= 512 * 16 + 600,
                strf("a 1.44M sector at 16 us a byte: %llu us", (unsigned long long)sector_us));
        g.results();
    }});
    return list;
}

std::vector<std::string> run(const Case &c, bool rtl, const std::string &directory, std::vector<std::string> &fails,
                             std::string &panic) {
    std::unique_ptr<pc386::FdcReference> model;
    Rtl *hardware = nullptr;
    if (rtl) { hardware = new Rtl; model.reset(hardware); }
    else model = pc386::make_fdc_reference();
    Guest g(*model, rtl, directory);
    g.clock_us = [&] { return hardware ? hardware->clocks / US_CLOCKS : g.now; };
    model->power_on(c.type_a, c.type_b);
    c.run(g);
    // Every disk's final image.
    for (size_t drive = 0; drive < g.files.size(); ++drive) {
        if (g.files[drive].first.empty()) continue;
        std::vector<uint8_t> bytes;
        if (rtl) {
            g.tick(100);
            bytes = hardware->image(unsigned(drive), g.files[drive].second.bytes());
        } else {
            std::ifstream file(g.files[drive].first, std::ios::binary);
            bytes.assign(std::istreambuf_iterator<char>(file), std::istreambuf_iterator<char>());
        }
        g.note(strf("IMAGE %zu %08x", drive, crc32(bytes.data(), bytes.size())));
    }
    if (rtl) g.check(!hardware->busy(), "no port 2 traffic left in flight");
    panic = model->panic();
    if (!panic.empty()) g.note("PANIC " + panic);
    fails = g.failures;
    return g.log;
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    std::set<std::string> divergent;
    std::string only;
    std::string directory = "fdc-replay";
    for (int i = 1; i < argc; ++i) {
        std::string arg = argv[i];
        if (arg == "--divergent" && i + 1 < argc) {
            std::stringstream list(argv[++i]);
            std::string name;
            while (std::getline(list, name, ',')) if (!name.empty()) divergent.insert(name);
        } else if (arg == "--case" && i + 1 < argc) only = argv[++i];
        else if (arg == "--dir" && i + 1 < argc) directory = argv[++i];
    }
    std::string mkdir = "mkdir -p '" + directory + "'";
    if (std::system(mkdir.c_str())) return 2;
    unsigned matched = 0, differed = 0, rtl_only = 0, failed = 0;
    for (const Case &c : cases()) {
        if (!only.empty() && only != c.name) continue;
        std::vector<std::string> rtl_fails, bochs_fails;
        std::string rtl_panic, bochs_panic;
        auto rtl = run(c, true, directory, rtl_fails, rtl_panic);
        for (const auto &f : rtl_fails) { std::printf("CHECK FAIL %s: %s\n", c.name, f.c_str()); ++failed; }
        std::ofstream(directory + "/" + c.name + ".rtl.log") << [&] {
            std::string text; for (auto &l : rtl) text += l + "\n"; return text; }();
        if (c.rtl_only) {
            std::printf("CASE %s RTL-ONLY\n", c.name);
            ++rtl_only;
            continue;
        }
        auto bochs = run(c, false, directory, bochs_fails, bochs_panic);
        std::ofstream(directory + "/" + c.name + ".bochs.log") << [&] {
            std::string text; for (auto &l : bochs) text += l + "\n"; return text; }();
        bool same = rtl == bochs;
        bool listed = divergent.count(c.name) != 0;
        std::printf("CASE %s %s%s\n", c.name, same ? "MATCH" : "DIFFER", listed ? " LISTED" : "");
        if (!same) {
            ++differed;
            size_t n = std::max(rtl.size(), bochs.size());
            unsigned shown = 0;
            for (size_t i = 0; i < n && shown < 6; ++i) {
                std::string a = i < rtl.size() ? rtl[i] : "-", b = i < bochs.size() ? bochs[i] : "-";
                if (a != b) { std::printf("DIFF %s %zu rtl=[%s] bochs=[%s]\n", c.name, i, a.c_str(), b.c_str()); ++shown; }
            }
        } else ++matched;
        if (!bochs_panic.empty() && !listed) {
            std::printf("CHECK FAIL %s: Bochs panicked in a compared case: %s\n", c.name, bochs_panic.c_str());
            ++failed;
        }
        if (same == listed) {
            std::printf("CHECK FAIL %s: %s\n", c.name,
                        listed ? "listed in fdc_divergences.toml but matches Bochs" : "differs from Bochs and is not listed");
            ++failed;
        }
    }
    std::printf("SUMMARY matched=%u differed=%u rtl_only=%u failures=%u\n", matched, differed, rtl_only, failed);
    return failed ? 1 : 0;
}
