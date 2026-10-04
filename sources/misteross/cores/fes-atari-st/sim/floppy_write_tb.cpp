// SPDX-License-Identifier: GPL-3.0-or-later
// Reuse the independent delayed RAM/.st backend, not any RTL internals.
#define main read_only_floppy_main
#include "floppy_tb.cpp"
#undef main

static void setup_write(Rig &r, unsigned address, unsigned count = 1)
{
    r.mmio(0xd, true, address & 255, 1);
    r.mmio(0xb, true, (address >> 8) & 255, 1);
    r.mmio(9, true, (address >> 16) & 255, 1);
    r.control(0x090); r.control(0x190); r.mmio(4, true, count);
}
static void write_sector(Rig &r, unsigned sector, unsigned command = 0xa0)
{
    r.control(0x184); r.mmio(4, true, sector); r.control(0x180); r.mmio(4, true, command, 3, 13);
}
static void wait_commit(Rig &r)
{
    for (unsigned n = 0; !r.dut.media_write_req && n < 50000; ++n) r.tick();
    check(r.dut.media_write_req, "write did not reach physical sector commit");
}
static void test_write_and_readback(Rig &r)
{
    r.restore(); r.fdc_read(0);
    check(!(r.fdc_read(0) & 0x40), "writable inserted disk reports write protection");
    for (unsigned side = 0; side < 2; ++side) {
        r.dut.side = side == 0;
        for (unsigned track : {0u, 79u}) {
            r.seek(track); r.fdc_read(0);
            for (unsigned sector : {1u, 9u}) {
                const unsigned offset = ((track * 2 + side) * 9 + sector - 1) * 512;
                for (unsigned n = 0; n < 512; ++n) r.ram[0x1000 + n] = uint8_t(n * 13 + offset / 512);
                const auto old = r.media; const auto reads = r.dma_transfers;
                const auto writes = r.disk_writes, changes = r.changes;
                setup_write(r, 0x1000); write_sector(r, sector); r.wait_irq();
                if (r.dma_transfers != reads + 256 || r.disk_writes != writes + 256 || r.changes != changes + 1)
                    std::fprintf(stderr, "counts DMA=%llu disk=%llu changed=%llu status=%02x\n", (unsigned long long)(r.dma_transfers-reads), (unsigned long long)(r.disk_writes-writes), (unsigned long long)(r.changes-changes), r.fdc_read(0));
                check(r.dma_transfers == reads + 256 && r.disk_writes == writes + 256 &&
                      r.changes == changes + 1, "sector did not read/commit/dirty exactly once");
                check(r.dma_address() == 0x1200 && !(r.fdc_read(0) & 0x5d), "successful write status/cursor incorrect");
                r.compare(0x1000, offset, 512);
                check(std::equal(old.begin(), old.begin() + offset, r.media.begin()) &&
                      std::equal(old.begin() + offset + 512, old.end(), r.media.begin() + offset + 512),
                      "write modified another sector");
                r.setup(0x2000); r.read_sector(sector); r.wait_irq(); r.compare(0x2000, offset, 512); r.fdc_read(0);
            }
        }
    }
    r.dut.side = 1; r.restore(); r.fdc_read(0);
}
static void test_cancel_and_freeze(Rig &r)
{
    // Interrupted collection never changes a sector, including a warm reset.
    for (unsigned abort : {0u, 1u, 127u, 255u}) {
        const auto old = r.media; const auto before = r.dma_transfers;
        setup_write(r, 0x3000); write_sector(r, 2);
        while (r.dma_transfers < before + abort) r.tick();
        r.dut.reset = 1; r.tick(); r.dut.reset = 0; r.idle(30);
        check(r.media == old && !r.dut.media_write_busy && !r.dut.media_write_req,
              "reset during collection changed disk or retained backend ownership");
        r.restore(); r.fdc_read(0);
    }
    // Freeze blocks new work without resetting the pending FDC command.
    r.dut.media_frozen = 1; const auto reads = r.dma_transfers, writes = r.disk_writes;
    setup_write(r, 0x3000); write_sector(r, 3); r.idle(100);
    check(r.dma_transfers == reads && r.disk_writes == writes && !r.dut.media_write_busy,
          "frozen image accepted a new writer");
    r.dut.media_frozen = 0; r.wait_irq(); r.fdc_read(0); r.compare(0x3000, 1024, 512);
    // The full staged sector drains after commit starts, even across a warm
    // reset/removal; no acknowledgment/address advance reaches the old FDC.
    for (unsigned event = 0; event < 3; ++event) {
        r.stall_disk_write = true; setup_write(r, 0x3000); write_sector(r, 4); wait_commit(r);
        const auto before = r.disk_writes, changed = r.changes;
        if (event == 0) r.dut.reset = 1;
        if (event == 1) r.dut.media_ready = 0;
        if (event == 2) r.fdc_write(0, 0xd8);
        r.idle(10); check(r.disk_writes == before, "stalled commit ignored backpressure");
        r.dut.media_frozen = 1; r.stall_disk_write = false;
        for (unsigned n = 0; r.dut.media_write_busy && n < 50000; ++n) r.tick();
        r.idle(3);
        check(!r.dut.media_write_busy && r.disk_writes == before + 256 && r.changes == changed + 1,
              "accepted sector did not drain through reset/eject/force");
        r.compare(0x3000, 1536, 512);
        r.dut.reset = 0; r.dut.media_ready = 1; r.dut.media_frozen = 0;
        r.fdc_write(0, 0xd0); r.restore(); r.fdc_read(0);
    }
}
static void test_errors_and_multiple(Rig &r)
{
    auto before = r.disk_writes;
    for (unsigned address : {0u, 6u, 0x7fe02u, 0x80000u}) {
        setup_write(r, address); write_sector(r, 1); r.wait_irq();
        check(r.fdc_read(0) & 4, "out-of-bounds write lacks lost-data error");
    }
    r.setup(0x3000); write_sector(r, 1); r.wait_irq();
    check(r.fdc_read(0) & 4, "wrong DMA direction accepted write");
    setup_write(r, 0x3000); write_sector(r, 1, 0xa1); r.wait_irq();
    check(r.fdc_read(0) & 0x10, "raw image falsely supports deleted-sector metadata");
    setup_write(r, 0x3000); write_sector(r, 1, 0xf0); r.wait_irq();
    check(r.fdc_read(0) & 0x10, "raw image falsely supports track format");
    check(r.disk_writes == before, "invalid write modified image");
    for (unsigned n = 0; n < 1024; ++n) r.ram[0x3000 + n] = uint8_t(n * 3);
    setup_write(r, 0x3000, 2); write_sector(r, 2, 0xb0);
    for (unsigned n = 0; r.disk_writes < before + 512 && n < 100000; ++n) r.tick();
    r.idle(50); check(r.disk_writes == before + 512 && !r.dut.irq,
                       "multi-sector write exceeded DMA count or completed early");
    check(r.dma_address() == 0x3400 && (r.fdc_read(0) & 3) == 3, "multi-sector cursor/exhaustion differs");
    r.compare(0x3000, 512, 1024); r.fdc_write(0, 0xd0);
}
int main(int argc, char **argv)
{
    Verilated::commandArgs(argc, argv);
    try {
        Rig r; test_write_and_readback(r); test_cancel_and_freeze(r); test_errors_and_multiple(r);
        std::printf("PASS st_floppy writable: geometry/readback, atomic sector commits, reset/eject/force drain, freeze, DMA bounds/direction and multiple writes\n");
        return 0;
    } catch (const std::exception &e) { std::fprintf(stderr, "FAIL writable floppy: %s\n", e.what()); return 1; }
}
