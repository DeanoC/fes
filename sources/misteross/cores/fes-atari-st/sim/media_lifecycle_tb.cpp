// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vst_media_lifecycle_sim_top.h"
#include "verilated.h"
#include <algorithm>
#include <cstdint>
#include <iostream>
#include <stdexcept>
#include <vector>

static void check(bool ok, const char* message)
{
    if (!ok) throw std::runtime_error(message);
}
static uint32_t crc32(const std::vector<uint8_t>& bytes)
{
    uint32_t crc = ~uint32_t(0);
    for (uint8_t byte : bytes) {
        crc ^= byte;
        for (unsigned bit = 0; bit < 8; ++bit)
            crc = (crc >> 1) ^ ((crc & 1) ? 0xedb88320u : 0);
    }
    return ~crc;
}
struct Bench {
    Vst_media_lifecycle_sim_top d;
    std::vector<uint8_t> disk = std::vector<uint8_t>(1024);
    std::vector<uint8_t> ram = std::vector<uint8_t>(0x80000);
    bool toggle = false, dma_seen = false, memory_seen = false;
    bool stall_dma = false, stall_disk = false;
    unsigned dma_delay = 0, memory_delay = 0;
    unsigned held_dma = 0, held_addr = 0, held_data = 0, held_enable = 0;
    unsigned dma_words = 0, host_words = 0, disk_words = 0, changes = 0, host_requests = 0;
    Bench()
    {
        d.clk = 0; d.cold_reset = 1; d.gpo = 0; d.job_req = 0;
        d.job_ram_addr = 0x1000; d.job_media_addr = 256;
        d.dma_ready = 0; d.dma_data = 0; d.memory_ready = 0;
        d.eval(); tick(); tick(); d.cold_reset = 0; idle(4);
        for (unsigned n = 0; n < 512; ++n) ram[0x1000 + n] = uint8_t(n * 29 + 17);
    }
    void tick()
    {
        d.clk = 0; d.eval();
        if (!d.dma_req) { dma_seen = false; dma_delay = 0; d.dma_ready = 0; }
        else if (!dma_seen) {
            if (!dma_delay) held_dma = d.dma_addr;
            check(d.dma_addr == held_dma, "DMA address changed before completion");
            ++dma_delay;
            if (!stall_dma && dma_delay >= 3 + dma_words % 7) {
                check(held_dma + 1 < ram.size(), "DMA outside ST RAM");
                d.dma_data = unsigned(ram[held_dma]) << 8 | ram[held_dma + 1];
                d.dma_ready = 1; dma_seen = true; ++dma_words;
            }
        }
        if (!d.memory_req) { memory_seen = false; memory_delay = 0; d.memory_ready = 0; }
        else if (!memory_seen) {
            if (!memory_delay) {
                held_addr = unsigned(d.memory_addr) << 1; held_data = d.memory_data; held_enable = d.memory_enable;
            }
            check((unsigned(d.memory_addr) << 1) == held_addr && d.memory_data == held_data && d.memory_enable == held_enable,
                  "arbiter changed a physical write before completion");
            ++memory_delay;
            if (!stall_disk && memory_delay >= 5 + (host_words + disk_words) % 11) {
                check(held_addr + 1 < disk.size(), "physical write outside inserted image");
                // The real host adapter and ST DMA share this big-endian
                // physical halfword port, including independent byte lanes.
                if (held_enable & 2) disk[held_addr] = uint8_t(held_data >> 8);
                if (held_enable & 1) disk[held_addr + 1] = uint8_t(held_data);
                d.memory_ready = 1; memory_seen = true;
            }
        }
        d.eval();
        if (d.client_ready & 1) ++host_words;
        if (d.client_ready & 2) ++disk_words;
        if (d.host_req) ++host_requests;
        d.clk = 1; d.eval();
        if (d.changed) ++changes;
        d.clk = 0; d.eval();
    }
    void idle(unsigned cycles) { for (unsigned n = 0; n < cycles; ++n) tick(); }
    unsigned gp(unsigned opcode, unsigned index = 0, unsigned argument = 0, bool error = false)
    {
        toggle = !toggle;
        d.gpo = (toggle ? 0x80000000u : 0) | opcode << 24 | index << 16 | argument;
        for (unsigned n = 0; bool(d.gpi & 0x00800000u) != toggle; ++n) {
            check(n < 10000, "mailbox ACK timeout"); tick();
        }
        if (bool(d.gpi & 0x00400000u) != error) {
            std::cerr << "opcode=" << opcode << " index=" << index
                      << " expected_error=" << error << " state=" << unsigned(d.unit0_state)
                      << " writer_busy=" << unsigned(d.writer_busy) << '\n';
            throw std::runtime_error("unexpected mailbox acceptance/rejection");
        }
        return d.gpi & 65535;
    }
    void upload(const std::vector<uint8_t>& image)
    {
        const uint32_t crc = crc32(image);
        gp(6, 0, image.size()); gp(6, 1, 0); gp(6, 2, crc & 65535); gp(6, 3, crc >> 16);
        for (unsigned offset = 0; offset < image.size(); offset += 512) {
            gp(7, 0, offset); gp(7, 1, 0); gp(7, 2, 512);
            for (unsigned word = 0; word < 256; ++word)
                gp(8, word, unsigned(image[offset + word * 2]) | unsigned(image[offset + word * 2 + 1]) << 8);
        }
        gp(9);
        check(d.unit0_state == 3 && d.unit0_size == image.size() && disk == image,
              "host replacement did not commit the exact new image");
    }
    void reject_destructive(const std::vector<uint8_t>& expected)
    {
        check(d.writer_busy && !d.frozen, "test must cover a volatile busy image");
        const unsigned before = host_requests;
        check(gp(6, 0, 1024, true) == 4, "busy Begin did not return invalid-state");
        check(gp(10, 0, 0, true) == 4, "busy Eject did not return invalid-state");
        idle(8);
        check(d.writer_busy && d.unit0_state == 3 && d.unit0_size == 1024 && !d.frozen,
              "rejected mutation changed old image ownership");
        check(disk == expected && host_requests == before,
              "rejected mutation changed storage or issued host writes");
    }
};

int main(int argc, char** argv)
{
    Verilated::commandArgs(argc, argv);
    try {
        Bench b;
        std::vector<uint8_t> old(1024), replacement(1024);
        for (unsigned n = 0; n < old.size(); ++n) {
            old[n] = uint8_t(n * 3 + 41); replacement[n] = uint8_t(n * 7 + 93);
        }
        b.upload(old);
        // Both an untouched collection and the final not-yet-collected word
        // retain readiness and all bytes when a volatile mutation is rejected.
        b.stall_dma = true; b.d.job_req = 1; b.idle(4);
        b.reject_destructive(old);
        b.stall_dma = false;
        for (unsigned n = 0; b.dma_words < 255; ++n) {
            check(n < 10000, "writer collection timeout"); b.tick();
        }
        b.stall_dma = true; b.idle(3); b.reject_destructive(old);
        // Hold the first physical write, then partway through the same accepted
        // commit. Rejection must not withdraw its owner or overlap an upload.
        b.stall_disk = true; b.stall_dma = false;
        for (unsigned n = 0; !b.d.writer_req; ++n) { check(n < 100, "writer did not commit"); b.tick(); }
        b.idle(4); b.reject_destructive(old);
        b.d.job_req = 0; b.idle(3); // Force/reset withdrawal still drains a staged sector.
        b.stall_disk = false;
        for (unsigned n = 0; b.disk_words < 127; ++n) {
            check(n < 10000, "partial sector commit timeout"); b.tick();
        }
        b.stall_disk = true;
        std::vector<uint8_t> partial = old;
        for (unsigned n = 0; n < 127 * 2; n += 2) {
            partial[512 + n] = b.ram[0x1000 + n];
            partial[512 + n + 1] = b.ram[0x1000 + n + 1];
        }
        b.reject_destructive(partial);
        b.stall_disk = false;
        for (unsigned n = 0; b.d.writer_busy; ++n) { check(n < 10000, "sector did not drain"); b.tick(); }
        b.d.job_req = 0; b.idle(4);
        std::vector<uint8_t> committed = old;
        for (unsigned n = 0; n < 512; n += 2) {
            committed[512 + n] = b.ram[0x1000 + n];
            committed[512 + n + 1] = b.ram[0x1000 + n + 1];
        }
        check(b.disk == committed && b.disk_words == 256 && b.dma_words == 256 && b.changes == 1 &&
              !b.d.job_error && !b.d.job_ready,
              "old accepted sector did not finish exactly once");
        check(b.gp(12, 7) == 1, "rejected commands lost the old dirty epoch");
        // The same host may explicitly retry after busy clears. No Freeze is
        // needed for volatile semantics, and no old write reaches this image.
        b.upload(replacement); b.idle(100);
        check(b.disk == replacement && b.disk_words == 256 && b.host_words == 1024,
              "old commit leaked into the replacement image");
        b.gp(10);
        check(b.d.unit0_state == 1 && b.d.unit0_size == 0 && b.disk == replacement,
              "idle volatile Eject was rejected or changed disk bytes");
        std::cout << "PASS ST media lifecycle: actual writer/mailbox/arbiter busy Begin/Eject rejection, old-sector drain, explicit replacement and eject\n";
        return 0;
    } catch (const std::exception& e) { std::cerr << "FAIL ST media lifecycle: " << e.what() << '\n'; return 1; }
}
