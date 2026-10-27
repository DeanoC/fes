// SPDX-License-Identifier: GPL-2.0-or-later
// +3 disk bench: drives spectrum_upd765's $2FFD/$3FFD as +3DOS does, against
// a raw track image (FogCast internal/spectrumdisks) of a synthetic two-sided
// extended DSK from scripts/sim_fes_spectrum.py: cylinders 0-2 have nine
// 512-byte sectors numbered 1-9 whose bytes encode cylinder, head, sector and
// offset; cylinder 3 side 0 holds sector 1 normal, 2 with a data CRC error,
// 3 with an ID CRC error, 4 deleted and 5 weak over bytes 100-299. The fetch
// port answers with a varying DDR delay; a T state is 15 system clocks.
//
// Checks the motor's 400 ms ready delay, Specify, Recalibrate and Seek with
// Sense Interrupt Status (and Sense Interrupt Status without an interrupt),
// Sense Drive Status, Read ID, Read Data of one sector and a run to EOT
// ending with End of Cylinder, multi-track head switching, SK at EOT, data
// bytes at 32 us, CRC and deleted-mark results, a weak sector reading
// differently, a missing sector, Overrun, Read Diagnostic, Write Data's Not
// Writable, Version, an invalid command, a read with the motor off, the empty
// drive B, and the launch image's boot pulse. Data bytes are sampled clocks
// after the read starts, as the CPU does.
#include "Vspectrum_upd765.h"
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
    std::fprintf(stderr, "upd765: %s\n", message.c_str());
    std::exit(EXIT_FAILURE);
}

void expect(bool ok, const std::string &what) {
    if (!ok) fail(what);
}

constexpr unsigned CLOCKS_PER_T = 15;
constexpr uint64_t T_PER_MS = 3547;

uint8_t pattern(unsigned cylinder, unsigned sector, unsigned offset, unsigned head = 0) {
    return uint8_t(cylinder * 9 * 7 + head * 97 + sector * 3 + offset * 13);
}

class Bench {
public:
    Vspectrum_upd765 dut;
    std::vector<uint8_t> disk;
    uint64_t clocks = 0, t = 0;
    unsigned divider = 0, wait = 0;
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
    uint8_t msr() {
        dut.data_port = 0;
        dut.eval();
        const uint8_t v = dut.rdata;
        dut.read_strobe = 1;
        clock();
        run_t(3);
        return v;
    }
    // The CPU samples the data register clocks after the read starts.
    uint8_t in_data() {
        dut.data_port = 1;
        dut.read_strobe = 1;
        clock();
        for (int i = 0; i < 20; ++i) clock();
        dut.eval();
        const uint8_t v = dut.rdata;
        run_t(3);
        return v;
    }
    void out_data(uint8_t v) {
        dut.data_port = 1;
        dut.wdata = v;
        dut.write_strobe = 1;
        clock();
        run_t(3);
    }
    // +3DOS's command loop: wait for RQM with DIO clear, write each byte.
    void command(std::initializer_list<uint8_t> bytes) {
        for (uint8_t b : bytes) {
            uint64_t start = t;
            while ((msr() & 0xc0) != 0x80)
                if (t - start > 100 * T_PER_MS) fail("command byte not requested");
            out_data(b);
        }
    }
    // The result phase: read while RQM and DIO are set and the FDC is busy.
    std::vector<uint8_t> results(uint64_t limit_ms = 2000) {
        std::vector<uint8_t> r;
        const uint64_t start = t;
        for (;;) {
            const uint8_t s = msr();
            if ((s & 0xd0) == 0xd0) r.push_back(in_data());
            else if ((s & 0xd0) == 0x80 && !r.empty()) return r;
            if (t - start > limit_ms * T_PER_MS) fail("no result phase");
        }
    }
    // Read Data / Read Diagnostic: execution bytes then the seven results.
    std::vector<uint8_t> transfer(std::vector<uint8_t> *res, uint64_t limit_ms = 2000, unsigned delay_t = 0) {
        std::vector<uint8_t> data;
        const uint64_t start = t;
        for (;;) {
            const uint8_t s = msr();
            if ((s & 0xf0) == 0xf0) {
                // A slow reader checks the status again, as +3DOS's loop does.
                if (delay_t) {
                    run_t(delay_t);
                    if ((msr() & 0xf0) != 0xf0) continue;
                }
                data.push_back(in_data());
            } else if ((s & 0xf0) == 0xd0) {
                *res = results(limit_ms);
                return data;
            }
            if (t - start > limit_ms * T_PER_MS) fail("no data or results");
        }
    }
    std::vector<uint8_t> read(uint8_t c, uint8_t r, uint8_t eot, std::vector<uint8_t> *res,
                              uint8_t op = 0x46, unsigned delay_t = 0, uint8_t unit_head = 0) {
        command({op, unit_head, c, 0x00, r, 0x02, eot, 0x2a, 0xff});
        return transfer(res, 2000, delay_t);
    }
    std::vector<uint8_t> sense_int() {
        command({0x08});
        return results();
    }
    void seek(uint8_t c) {
        command({0x0f, 0x00, c});
        const uint64_t start = t;
        while (msr() & 0x01)
            if (t - start > 2000 * T_PER_MS) fail("seek does not end");
        const auto r = sense_int();
        expect(r.size() == 2 && (r[0] & 0xf8) == 0x20 && r[1] == c, "seek sense interrupt");
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
    if (argc != 2) fail("usage: Vspectrum_upd765 IMAGE");
    const std::vector<uint8_t> image = read_file(argv[1]);
    {
        Bench plain(image);
        unsigned high = 0;
        for (int i = 0; i < 4000; ++i) { plain.clock(); high += plain.dut.boot; }
        expect(high == 0, "a disk changed while running does not boot");
        auto launch = image;
        launch[7] |= 2;
        Bench booted(launch);
        for (int i = 0; i < 4000; ++i) { booted.clock(); high += booted.dut.boot; }
        expect(high == 64, "a launch disk raises boot once for 64 clocks");
    }
    Bench b(image);

    expect(b.msr() == 0x80, "idle main status");
    b.command({0x10});
    expect(b.results() == std::vector<uint8_t>{0x80}, "version");
    b.command({0x55});
    expect(b.results() == std::vector<uint8_t>{0x80}, "invalid command");
    expect(b.sense_int() == std::vector<uint8_t>{0x80}, "sense interrupt without an interrupt is invalid");
    b.command({0x03, 0xaf, 0x03});   // Specify: 6 ms steps, 30 ms unload, 2 ms load, non-DMA

    // A read with the motor off finds nothing.
    {
        std::vector<uint8_t> r;
        const auto d = b.read(0, 1, 1, &r);
        expect(d.empty() && r.size() == 7 && (r[0] & 0xc0) == 0x40 && (r[1] & 0x05) == 0x05,
               "motor-off read: missing address mark");
    }
    b.command({0x04, 0x00});
    {
        const auto st3 = b.results();
        expect(st3.size() == 1 && (st3[0] & 0x20) == 0 && (st3[0] & 0x40), "not ready before the motor");
    }
    // Motor on: ready 400 ms later.
    b.dut.motor = 1;
    b.run_t(350 * T_PER_MS);
    b.command({0x04, 0x00});
    expect((b.results()[0] & 0x20) == 0, "not ready at 350 ms");
    b.run_t(60 * T_PER_MS);
    b.command({0x04, 0x00});
    expect((b.results()[0] & 0x70) == 0x70, "ready, write protected, track 0");
    b.command({0x04, 0x01});
    expect((b.results()[0] & 0x70) == 0x50, "drive B is empty: write protected at track 0");

    // A not-ready seek still raises a Sense Interrupt Status, and consuming
    // that status leaves no command pending.
    b.command({0x0f, 0x01, 0x01});
    while (b.msr() & 0x01) {}
    {
        const auto r = b.sense_int();
        expect(r.size() == 2 && (r[0] & 0xe8) == 0xe8 && r[1] == 0,
               "not-ready seek sense interrupt");
    }
    expect(b.sense_int() == std::vector<uint8_t>{0x80},
           "sense interrupt after consuming not-ready status is invalid");

    // Recalibrate, then seek.
    b.command({0x07, 0x00});
    while (b.msr() & 0x01) {}
    {
        const auto r = b.sense_int();
        expect(r.size() == 2 && (r[0] & 0xf0) == 0x20 && r[1] == 0, "recalibrate sense interrupt");
    }
    expect(b.sense_int() == std::vector<uint8_t>{0x80},
           "sense interrupt after consuming seek completion is invalid");
    {
        const uint64_t start = b.t;
        b.seek(2);
        const uint64_t ms = (b.t - start) / T_PER_MS;
        expect(ms >= 12 && ms < 40, "two 6 ms steps: " + std::to_string(ms) + " ms");
    }

    // Read ID: the next ID on cylinder 2.
    b.command({0x4a, 0x00});
    {
        const auto r = b.results();
        expect(r.size() == 7 && (r[0] & 0xc0) == 0 && r[3] == 2 && r[4] == 0 && r[5] >= 1 && r[5] <= 9 && r[6] == 2,
               "read ID");
    }

    // One sector: the +3's missing terminal count ends it with End of Cylinder.
    {
        std::vector<uint8_t> r;
        const uint64_t start = b.t;
        const auto d = b.read(2, 4, 4, &r);
        expect(d.size() == 512, "one sector: " + std::to_string(d.size()) + " bytes");
        for (unsigned i = 0; i < 512; ++i)
            if (d[i] != pattern(2, 4, i)) fail("cylinder 2 sector 4 byte " + std::to_string(i));
        expect(r[0] == 0x40 && r[1] == 0x80 && r[2] == 0 && r[3] == 2 && r[5] == 4,
               "one sector results: end of cylinder");
        expect(b.t - start > 512 * 112, "bytes pass at the disk's rate");
    }
    // A run of sectors 1 to 9.
    {
        std::vector<uint8_t> r;
        const auto d = b.read(2, 1, 9, &r);
        expect(d.size() == 9 * 512, "nine sectors: " + std::to_string(d.size()) + " bytes");
        for (unsigned s = 1; s <= 9; ++s)
            if (d[(s - 1) * 512 + 7] != pattern(2, s, 7)) fail("run sector " + std::to_string(s));
        expect(r[0] == 0x40 && r[1] == 0x80 && r[5] == 9, "run results");
    }
    // Multi-track reads the other side at the same C, then ends at its EOT.
    {
        std::vector<uint8_t> r;
        const auto d = b.read(2, 9, 9, &r, 0xc6);
        expect(d.size() == 10 * 512, "multi-track: " + std::to_string(d.size()) + " bytes");
        for (unsigned track_sector = 0; track_sector < 10; ++track_sector) {
            const unsigned head = track_sector == 0 ? 0 : 1;
            const unsigned sector = track_sector == 0 ? 9 : track_sector;
            for (unsigned offset = 0; offset < 512; ++offset)
                if (d[track_sector * 512 + offset] != pattern(2, sector, offset, head))
                    fail("multi-track head " + std::to_string(head) + " sector " + std::to_string(sector) +
                         " byte " + std::to_string(offset));
        }
        expect(r.size() == 7 && r[0] == 0x40 && r[1] == 0x80 && r[3] == 2 && r[4] == 1 && r[5] == 9,
               "multi-track results end at head 1 EOT");
    }
    // A sector that is not there: two index pulses, then Missing Address Mark.
    {
        std::vector<uint8_t> r;
        const uint64_t start = b.t;
        b.read(2, 12, 12, &r);
        const uint64_t ms = (b.t - start) / T_PER_MS;
        expect((r[0] & 0xc0) == 0x40 && (r[1] & 0x05) == 0x05, "missing sector results");
        expect(ms >= 200 && ms <= 450, "missing sector after two revolutions: " + std::to_string(ms) + " ms");
    }
    // Overrun: a reader too slow for 32 us bytes.
    {
        std::vector<uint8_t> r;
        const auto d = b.read(2, 5, 5, &r, 0x46, 200);
        expect(d.size() < 512 && (r[0] & 0xc0) == 0x40 && (r[1] & 0x10), "overrun");
    }

    // Cylinder 3: CRC error, bad ID, deleted and weak sectors.
    b.seek(3);
    {
        std::vector<uint8_t> r;
        const auto d = b.read(3, 2, 2, &r);
        expect(d.size() == 512 && (r[0] & 0xc0) == 0x40 && (r[1] & 0x20) && (r[2] & 0x20), "data CRC error");
        b.read(3, 3, 3, &r);
        // The search's last index clears CRC Error, as read_id does.
        expect((r[0] & 0xc0) == 0x40 && (r[1] & 0x05) == 0x05, "ID CRC error: never found");
        b.read(3, 4, 4, &r);
        expect((r[2] & 0x40) && (r[0] & 0xc0) == 0 && r[1] == 0, "deleted data mark: control mark");
        const auto skipped = b.read(3, 4, 4, &r, 0x46, 0, 0x20);
        expect(skipped.empty() && r.size() == 7 && r[0] == 0x40 && r[1] == 0x80 &&
               (r[2] & 0x40) && r[3] == 3 && r[4] == 0 && r[5] == 4,
               "skip deleted sector at EOT ends with End of Cylinder");
        const auto deleted = b.read(3, 4, 4, &r, 0x4c);   // Read Deleted Data
        expect(deleted.size() == 512 && (r[2] & 0x40) == 0, "read deleted data");
        const auto w1 = b.read(3, 5, 5, &r);
        const auto w2 = b.read(3, 5, 5, &r);
        expect(w1.size() == 512 && w2.size() == 512, "weak sector reads");
        bool outside = true, inside = false;
        for (unsigned i = 0; i < 512; ++i) {
            if ((i < 100 || i >= 300) && w1[i] != w2[i]) outside = false;
            if (i >= 100 && i < 300 && w1[i] != w2[i]) inside = true;
        }
        expect(outside && inside, "weak bytes differ only in the weak range");
    }

    // Read Diagnostic: the first sectors from the index, whatever their IDs.
    {
        std::vector<uint8_t> r;
        b.seek(1);
        b.command({0x42, 0x00, 0x01, 0x00, 0x01, 0x02, 0x02, 0x2a, 0xff});
        const auto d = b.transfer(&r);
        expect(d.size() == 2 * 512 && d[5] == pattern(1, 1, 5), "read diagnostic");
    }

    // When multiple seeks have completed, draining one Sense Interrupt must
    // leave the next unit's status pending for the following 0x08.
    b.command({0x0f, 0x00, 0x03});
    b.command({0x0f, 0x01, 0x01});
    {
        const uint64_t start = b.t;
        while (b.msr() & 0x03)
            if (b.t - start > 2000 * T_PER_MS) fail("queued seeks do not end");
    }
    {
        const auto r = b.sense_int();
        expect(r.size() == 2 && (r[0] & 0xf8) == 0x20 && r[1] == 3,
               "first of two pending seek interrupts");
    }
    {
        const auto r = b.sense_int();
        expect(r.size() == 2 && (r[0] & 0xe8) == 0xe8 && r[1] == 0,
               "second of two pending seek interrupts: ST0=" +
                   (r.empty() ? std::string("missing") : std::to_string(r[0])) +
                   ", PCN=" + (r.size() < 2 ? std::string("missing") : std::to_string(r[1])));
    }
    expect(b.sense_int() == std::vector<uint8_t>{0x80},
           "sense interrupt after consuming both pending statuses is invalid");

    // Write Data: the disk is write protected.
    b.command({0x45, 0x00, 0x01, 0x00, 0x01, 0x02, 0x01, 0x2a, 0xff});
    {
        const auto r = b.results();
        expect(r.size() == 7 && (r[0] & 0xc0) == 0x40 && (r[1] & 0x02), "write is not writable");
    }

    std::printf("fes.spectrum uPD765: +3 controller commands on a raw track image ok\n");
    return 0;
}
