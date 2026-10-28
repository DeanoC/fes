// SPDX-License-Identifier: GPL-2.0-or-later
// Beta 128 bench: drives spectrum_beta's ports as TR-DOS does and reads a raw
// track image (FogCast internal/spectrumdisks, from a synthetic .trd whose
// bytes encode their logical track, sector and offset) through a fetch port
// with a varying DDR delay. A T state is 15 system clocks.
//
// Checks Restore and Seek with verify, a verify that finds the wrong track,
// Read Sector on both sides with its 112 T byte rate, Read Address, Read
// Track from index to index, multi-sector reads that end at the track's last
// sector, Write Protect, a CRC error in a corrupted sector, Lost Data, a
// sector that is not there, an empty drive, Type II on an unloaded head,
// Force Interrupt, the head unloading 3 s after a command, and the boot
// pulse a launch image (header flags bit 1) raises once it is ready.
#include "Vspectrum_beta.h"
#include "verilated.h"

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <fstream>
#include <iterator>
#include <string>
#include <vector>

namespace {

[[noreturn]] void fail(const std::string &message) {
    std::fprintf(stderr, "beta: %s\n", message.c_str());
    std::exit(EXIT_FAILURE);
}

void expect(bool ok, const std::string &what) {
    if (!ok) fail(what);
}

constexpr unsigned CLOCKS_PER_T = 15;
constexpr uint64_t T_PER_MS = 3500;

// The .trd's byte at a logical track, sector (1-16) and offset.
uint8_t pattern(unsigned logical, unsigned sector, unsigned offset) {
    return uint8_t(logical * 7 + (sector - 1) * 3 + offset);
}

class Bench {
public:
    Vspectrum_beta dut;
    std::vector<uint8_t> disk;
    uint64_t clocks = 0;
    uint64_t t = 0;
    unsigned divider = 0;
    unsigned wait = 0;
    uint32_t seen = ~0u;

    explicit Bench(std::vector<uint8_t> image) : disk(std::move(image)) {
        dut.clk = 0;
        dut.power_reset = 1;
        dut.reset = 1;
        dut.disk_ready = 1;
        dut.disk_bytes = disk.size();
        dut.eval();
        for (int i = 0; i < 8; ++i) clock();
        dut.power_reset = 0;
        dut.reset = 0;
    }
    // Clocks of boot pulse within the next count clocks.
    unsigned boot_clocks(unsigned count) {
        unsigned high = 0;
        for (unsigned i = 0; i < count; ++i) {
            clock();
            high += dut.boot;
        }
        return high;
    }
    void clock() {
        if (!dut.fetch || dut.fetch_addr != seen) {
            seen = dut.fetch ? dut.fetch_addr : ~0u;
            wait = 6 + (clocks * 13) % 41;
        } else if (wait) --wait;
        dut.fetch_ready = dut.fetch && wait == 0;
        dut.fetch_data = dut.fetch_addr < disk.size() ? disk[dut.fetch_addr] : 0;
        dut.tick = divider == 0;
        dut.eval();
        dut.clk = 1; dut.eval();
        dut.clk = 0;
        dut.write_strobe = 0;
        dut.read_strobe = 0;
        dut.eval();
        if (divider == 0) ++t;
        divider = (divider + 1) % CLOCKS_PER_T;
        ++clocks;
    }
    void run_t(uint64_t count) {
        const uint64_t end = t + count;
        while (t < end) clock();
    }
    void out(uint8_t port, uint8_t value) {
        dut.port = port;
        dut.wdata = value;
        dut.write_strobe = 1;
        clock();
        run_t(4);
    }
    uint8_t in(uint8_t port) {
        dut.port = port;
        dut.eval();
        const uint8_t value = dut.rdata;
        dut.read_strobe = 1;
        clock();
        run_t(4);
        return value;
    }
    // Waits for INTRQ (port $FF bit 7), failing after a limit.
    uint64_t wait_intrq(uint64_t limit_ms, const std::string &what) {
        const uint64_t start = t;
        while (!(in(0xff) & 0x80))
            if (t - start > limit_ms * T_PER_MS) fail(what + ": no INTRQ");
        return t - start;
    }
    void wait_index(bool active, uint64_t limit_ms, const std::string &what) {
        const uint64_t start = t;
        while (((in(0x1f) & 0x02) != 0) != active)
            if (t - start > limit_ms * T_PER_MS) fail(what + ": index state did not change");
    }
    // A read loop as TR-DOS's: poll $FF, take the data on DRQ, end on INTRQ.
    std::vector<uint8_t> read_data(uint64_t limit_ms, const std::string &what, unsigned delay_t = 0) {
        std::vector<uint8_t> data;
        const uint64_t start = t;
        for (;;) {
            const uint8_t s = in(0xff);
            if (s & 0x80) break;
            if (s & 0x40) {
                if (delay_t) run_t(delay_t);
                data.push_back(in(0x7f));
            }
            if (t - start > limit_ms * T_PER_MS) fail(what + ": no INTRQ");
        }
        return data;
    }
    void seek(uint8_t track) {
        out(0x7f, track);
        out(0x1f, 0x1c);    // Seek, h, V, 6 ms
        wait_intrq(400, "seek");
        expect((in(0x1f) & 0x19) == 0x00, "seek ends without error");
        expect(in(0x3f) == track, "seek updates the track register");
    }
};

std::vector<uint8_t> read_file(const std::string &path) {
    std::ifstream in(path, std::ios::binary);
    if (!in) fail("cannot read " + path);
    return {std::istreambuf_iterator<char>(in), {}};
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    if (argc != 2) fail("usage: Vspectrum_beta IMAGE");
    std::vector<uint8_t> image = read_file(argv[1]);
    // Corrupt cylinder 3 side 0 sector 5's first data byte: find the data
    // mark after its ID in the slot's track bytes.
    {
        uint8_t *slot = &image[(1 + 2 * 3) * 8192];
        const unsigned length = slot[0] | slot[1] << 8;
        uint8_t *track = slot + 1024;
        bool done = false;
        for (unsigned i = 4; i + 40 < length && !done; ++i)
            if (track[i] == 0xfe && track[i - 1] == 0xa1 && track[i + 3] == 5)
                for (unsigned j = i + 7; j < i + 60; ++j)
                    if (track[j] == 0xfb && track[j - 1] == 0xa1) {
                        track[j + 1] ^= 0x55;
                        done = true;
                        break;
                    }
        expect(done, "found the sector to corrupt");
    }
    {
        Bench plain(image);
        expect(plain.boot_clocks(4000) == 0, "a disk changed while running does not boot");
        auto launch = image;
        launch[7] |= 2;
        Bench booted(launch);
        expect(booted.boot_clocks(4000) == 64, "a launch disk raises boot once for 64 clocks");
        booted.dut.disk_ready = 0;
        expect(booted.boot_clocks(100) == 0, "ejecting does not boot");
        booted.dut.disk_ready = 1;
        expect(booted.boot_clocks(4000) == 64, "reinserting a launch disk boots again");
    }
    Bench bench(image);

    // A Type II command on an unloaded head (Beta READY) ends at once.
    expect((bench.in(0x1f) & 0x80) != 0, "head unloaded after reset");
    bench.out(0xff, 0x3c);              // drive A, side 0, HLT, MFM
    bench.out(0x5f, 1);
    bench.out(0x1f, 0x80);
    expect(bench.wait_intrq(1, "unloaded read") < 50, "unloaded read ends at once");
    expect((bench.in(0x1f) & 0x01) == 0, "unloaded read is not busy");

    // Restore with head load and verify.
    bench.out(0x3f, 40);
    bench.out(0x1f, 0x0c);
    bench.wait_intrq(400, "restore");
    {
        const uint8_t s = bench.in(0x1f);
        expect((s & 0x9d) == 0x04, "restore: track 0, no error, head ready");
        expect((s & 0x20) != 0, "restore: head loaded bit");
        expect(bench.in(0x3f) == 0, "restore zeroes the track register");
    }

    // Seek 10 cylinders at 6 ms each, then the 15 ms settle and an ID.
    {
        const uint64_t before = bench.t;
        bench.seek(10);
        const uint64_t ms = (bench.t - before) / T_PER_MS;
        expect(ms >= 75 && ms < 300, "seek time " + std::to_string(ms) + " ms");
    }

    // Read Sector, both sides: 256 bytes at 112 T each.
    for (unsigned side = 0; side < 2; ++side) {
        bench.out(0xff, side ? 0x2c : 0x3c);
        for (unsigned sector : {1u, 9u, 16u}) {
            bench.out(0x5f, sector);
            bench.out(0x1f, 0x80);
            const uint64_t start = bench.t;
            const auto data = bench.read_data(400, "read sector");
            expect(data.size() == 256, "sector length " + std::to_string(data.size()));
            for (unsigned i = 0; i < 256; ++i)
                if (data[i] != pattern(10 * 2 + side, sector, i))
                    fail("cylinder 10 side " + std::to_string(side) + " sector " + std::to_string(sector) +
                         " byte " + std::to_string(i));
            expect((bench.in(0x1f) & 0x1d) == 0, "read sector status");
            expect(bench.t - start > 256 * 112, "bytes pass at the disk's rate");
        }
    }
    bench.out(0xff, 0x3c);

    // Read Address: the next ID under the head.
    bench.out(0x1f, 0xc0);
    {
        const auto id = bench.read_data(400, "read address");
        expect(id.size() == 6 && id[0] == 10 && id[1] == 0 && id[2] >= 1 && id[2] <= 16 && id[3] == 1,
               "read address ID");
        expect(bench.in(0x5f) == 10, "read address copies the track to the sector register");
        expect((bench.in(0x1f) & 0x1d) == 0, "read address status");
    }

    // Read Track: index to index, 6250 bytes, starting in the post-index gap.
    bench.out(0x1f, 0xe0);
    {
        const auto track = bench.read_data(800, "read track");
        expect(track.size() == 6250, "read track length " + std::to_string(track.size()));
        expect(track[0] == 0x4e && track[9] == 0x4e && track[10] == 0x00, "read track starts at the index");
    }

    // Multi-sector read from sector 15 ends with Record Not Found after 16.
    bench.out(0x5f, 15);
    bench.out(0x1f, 0x90);
    {
        const auto data = bench.read_data(1500, "multi-sector read");
        expect(data.size() == 512, "multi-sector read reads two sectors");
        expect(data[0] == pattern(20, 15, 0) && data[256] == pattern(20, 16, 0), "multi-sector data");
        expect((bench.in(0x1f) & 0x10) != 0, "multi-sector read ends not found");
        expect(bench.in(0x5f) == 17, "multi-sector read advances the sector register");
    }

    // A sector that is not on the track: Record Not Found after five turns.
    bench.out(0x5f, 17);
    bench.out(0x1f, 0x80);
    {
        const uint64_t ms = bench.wait_intrq(1500, "missing sector") / T_PER_MS;
        expect((bench.in(0x1f) & 0x10) != 0, "missing sector not found");
        expect(ms >= 800 && ms <= 1050, "not found after five revolutions: " + std::to_string(ms) + " ms");
    }

    // Write Sector and Write Track end with Write Protect.
    bench.out(0x5f, 1);
    bench.out(0x1f, 0xa0);
    bench.wait_intrq(100, "write sector");
    expect((bench.in(0x1f) & 0x40) != 0, "write sector is write protected");
    bench.out(0x1f, 0xf0);
    bench.wait_intrq(100, "write track");
    expect((bench.in(0x1f) & 0x40) != 0, "write track is write protected");

    // A sector read too slowly loses data.
    bench.out(0x5f, 2);
    bench.out(0x1f, 0x80);
    {
        const auto data = bench.read_data(400, "slow read", 200);
        expect(data.size() < 256 && (bench.in(0x1f) & 0x04) != 0, "a slow reader sees Lost Data");
    }

    // A CRC error in cylinder 3 sector 5.
    bench.seek(3);
    bench.out(0x5f, 5);
    bench.out(0x1f, 0x80);
    {
        const auto data = bench.read_data(400, "corrupt sector");
        expect(data.size() == 256 && data[0] == uint8_t(pattern(6, 5, 0) ^ 0x55), "corrupt sector data");
        expect((bench.in(0x1f) & 0x08) != 0, "corrupt sector sets CRC Error");
    }

    // Verify against the wrong track register: Seek Error.
    bench.out(0x3f, 7);
    bench.out(0x7f, 7);
    bench.out(0x1f, 0x1c);
    bench.wait_intrq(400, "wrong-track verify");
    expect((bench.in(0x1f) & 0x10) != 0, "verify on the wrong cylinder is a seek error");

    // Stepping out onto track 0 zeroes the track register, so a Restore
    // recovers the head's position, as TR-DOS does after a seek error.
    bench.out(0x1f, 0x0c);
    bench.wait_intrq(400, "restore after seek error");
    expect(bench.in(0x3f) == 0 && (bench.in(0x1f) & 0x14) == 0x04, "restore after seek error");

    // Force Interrupt stops a read in progress.
    bench.seek(3);
    bench.out(0x5f, 17);
    bench.out(0x1f, 0x80);
    bench.run_t(20 * T_PER_MS);
    expect((bench.in(0x1f) & 0x01) != 0, "read in progress");
    bench.out(0x1f, 0xd8);
    expect((bench.in(0xff) & 0x80) != 0, "force interrupt raises INTRQ");
    expect((bench.in(0x1f) & 0x01) == 0, "force interrupt ends the command");

    // I2 fires as the next index pulse ends, as Fuse 1.6.0's fdd_event calls
    // fdc_index only when the pulse falls: the index bit is clear by then.
    bench.wait_index(true, 210, "current index pulse starts");
    bench.out(0x1f, 0xd4);
    expect((bench.in(0xff) & 0x80) == 0, "I2 waits for the index pulse to end");
    bench.wait_intrq(30, "force interrupt I2");
    expect((bench.in(0x1f) & 0x02) == 0, "I2 raises INTRQ as the index pulse ends");

    // Drive B is empty: its reads are not found.
    bench.out(0xff, 0x3d);
    bench.out(0x1f, 0x0c);
    bench.wait_intrq(1500, "restore B");
    expect((bench.in(0x1f) & 0x10) != 0, "verify on an empty drive finds no ID");
    bench.out(0xff, 0x3c);

    // The head unloads 3 s after the last command ends.
    bench.out(0x1f, 0x08);
    bench.wait_intrq(400, "restore with head load");
    expect((bench.in(0x1f) & 0x80) == 0, "head loaded");
    bench.run_t(3100 * T_PER_MS);
    expect((bench.in(0x1f) & 0x80) != 0, "head unloaded after 3 s");

    std::printf("fes.spectrum Beta 128: FD1793 commands on a raw track image ok\n");
    return 0;
}
