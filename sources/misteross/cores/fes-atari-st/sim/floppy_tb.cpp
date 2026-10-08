// SPDX-License-Identifier: GPL-3.0-or-later
// Exercises the register sequence used by EmuTOS 1.4 against external byte
// media and word RAM backends. No disk image or copyrighted ROM is required.
#include "Vst_floppy.h"
#include "verilated.h"

#include <algorithm>
#include <cstdint>
#include <cstdio>
#include <stdexcept>
#include <string>
#include <vector>

static void check(bool condition, const char *message)
{
    if (!condition) throw std::runtime_error(message);
}

class Rig {
public:
    Vst_floppy dut;
    std::vector<uint8_t> media = std::vector<uint8_t>(720 * 1024);
    std::vector<uint8_t> ram = std::vector<uint8_t>(512 * 1024, 0xa5);
    unsigned media_delay = 3, dma_delay = 7;
    bool stall_media = false, stall_dma = false, stall_disk_write = false;
    uint64_t cycles = 0, media_transfers = 0, dma_transfers = 0, disk_writes = 0, changes = 0;

    Rig()
    {
        // A deterministic original 720 KiB FAT12 fixture. The boot sector has
        // the same 512/9/2 geometry that EmuTOS detects from a PC-format BPB.
        for (size_t n = 0; n < media.size(); ++n)
            media[n] = uint8_t((n * 37 + (n >> 9) * 13 + 0x53) & 255);
        std::fill(media.begin(), media.begin() + 512, 0);
        media[0] = 0xeb; media[1] = 0x3c; media[2] = 0x90;
        const char oem[] = "FES ST  ";
        std::copy(oem, oem + 8, media.begin() + 3);
        put_le16(11, 512); media[13] = 2; put_le16(14, 1);
        media[16] = 2; put_le16(17, 112); put_le16(19, 1440);
        media[21] = 0xf9; put_le16(22, 5); put_le16(24, 9); put_le16(26, 2);
        for (unsigned fat : {1u, 6u}) {
            std::fill(media.begin() + fat * 512, media.begin() + (fat + 5) * 512, 0);
            media[fat * 512] = 0xf9; media[fat * 512 + 1] = 0xff;
            media[fat * 512 + 2] = 0xff;
        }
        std::fill(media.begin() + 11 * 512, media.begin() + 18 * 512, 0);
        reset();
    }

    void reset()
    {
        dut.clk = 0; dut.reset = 1; dut.cold_reset = 1; dut.media_frozen = 0; dut.mmio_req = 0;
        dut.mmio_addr = 0; dut.mmio_write = 0; dut.mmio_wdata = 0;
        dut.mmio_byte_enable = 3; dut.drive_select = 2; dut.side = 1;
        dut.media_size = 737280; dut.media_ready = 1; dut.media_valid = 0; dut.media_data = 0;
        dut.dma_ready = 0; dut.dma_rdata = 0; dut.media_write_ready = 0;
        stall_media = false; stall_dma = false; stall_disk_write = false;
        media_seen = dma_seen = disk_seen = false;
        for (int n = 0; n < 3; ++n) tick();
        dut.reset = 0; dut.cold_reset = 0; tick();
        check(!dut.irq && !dut.media_req && !dut.dma_req && !dut.mmio_ack,
              "reset leaves a pending request or interrupt");
    }

    void tick()
    {
        dut.clk = 0; dut.eval();
        const bool mr = dut.media_req;
        const bool dr = dut.dma_req;
        const uint32_t ma = dut.media_addr, da = dut.dma_addr;
        const uint16_t dw = dut.dma_wdata;
        const bool dma_write = dut.dma_write;
        if (!mr) media_seen = false;
        if (!dr) dma_seen = false;
        if (mr) {
            if (!media_seen) {
                media_seen = true; media_age = 0; saved_media = ma;
            } else {
                check(ma == saved_media, "media address changed while waiting");
                check(!media_done, "media request was accepted twice without an idle cycle");
            }
            check(ma < media.size(), "media request escaped 720 KiB image");
        } else media_done = false;
        if (dr) {
            if (!dma_seen) {
                dma_seen = true; dma_age = 0; saved_dma = da; saved_word = dw;
            } else {
                check(da == saved_dma && dw == saved_word,
                      "DMA payload changed while waiting");
                check(!dma_done, "DMA request was accepted twice without an idle cycle");
            }
            check(dut.dma_byte_enable == 3, "DMA is not a full word write");
            check(!(da & 1) && da >= 8 && da + 1 < ram.size(),
                  "DMA request escaped writable 520ST RAM");
        } else dma_done = false;
        dut.media_valid = mr && !stall_media && media_age++ >= media_delay;
        dut.media_data = mr ? media[ma] : 0;
        dut.dma_ready = dr && !stall_dma && dma_age++ >= dma_delay;
        dut.dma_rdata = dr ? uint16_t((unsigned(ram[da]) << 8) | ram[da + 1]) : 0;
        const bool wr = dut.media_write_req;
        const unsigned wa = unsigned(dut.media_write_addr) * 2;
        const uint16_t wd = dut.media_write_data;
        if (!wr) { disk_seen = false; disk_done = false; }
        if (wr) {
            check(wa + 1 < media.size(), "disk write escaped .st image");
            if (!disk_seen) { disk_seen = true; disk_age = 0; saved_disk = wa; saved_disk_word = wd; }
            else check(wa == saved_disk && wd == saved_disk_word && !disk_done,
                       "disk write changed/repeated while held");
        }
        dut.media_write_ready = wr && !stall_disk_write && disk_age++ >= media_delay;
        const bool disk_accepted = wr && dut.media_write_ready;
        const bool media_accepted = mr && dut.media_valid;
        const bool dma_accepted = dr && dut.dma_ready;
        dut.eval(); dut.clk = 1; dut.eval();
        if (media_accepted) { ++media_transfers; media_done = true; }
        if (dma_accepted) {
            if (dma_write) { ram[da] = uint8_t(dw >> 8); ram[da + 1] = uint8_t(dw); }
            ++dma_transfers; dma_done = true;
        }
        if (disk_accepted) {
            media[wa] = uint8_t(wd >> 8); media[wa + 1] = uint8_t(wd);
            ++disk_writes; disk_done = true;
        }
        if (dut.media_changed) ++changes;
        ++cycles;
    }

    void idle(unsigned clocks) { while (clocks--) tick(); }

    uint16_t mmio(unsigned addr, bool write, unsigned value = 0,
                  unsigned lanes = 3, unsigned hold = 3)
    {
        dut.mmio_addr = addr; dut.mmio_write = write;
        dut.mmio_wdata = value; dut.mmio_byte_enable = lanes; dut.mmio_req = 1;
        for (unsigned wait = 0; !dut.mmio_ack && wait < 8; ++wait) tick();
        check(dut.mmio_ack, "known MMIO register did not acknowledge");
        const uint16_t result = dut.mmio_rdata;
        while (hold--) {
            tick(); check(dut.mmio_ack && dut.mmio_rdata == result,
                          "MMIO response changed during held request");
        }
        dut.mmio_req = 0; tick(); check(!dut.mmio_ack, "MMIO ack did not release");
        return result;
    }

    void control(unsigned mode) { mmio(6, true, mode); }
    void fdc_write(unsigned reg, unsigned value, unsigned hold = 3)
    { control(0x80 + 2 * reg); mmio(4, true, value, 3, hold); }
    uint8_t fdc_read(unsigned reg)
    {
        control(0x80 + 2 * reg);
        uint16_t value = mmio(4, false);
        check((value & 0xff00) == 0xff00, "FDC read has the wrong upper byte");
        return uint8_t(value);
    }
    void wait_irq()
    {
        for (unsigned n = 0; !dut.irq && n < 50000; ++n) tick();
        check(dut.irq, "FDC command failed to complete");
    }
    void restore()
    { fdc_write(0, 0xd0); fdc_write(0, 0x08); wait_irq(); }
    void seek(unsigned track)
    { fdc_write(3, track); fdc_write(0, 0x14); wait_irq(); }
    void setup(unsigned address, unsigned count = 1)
    {
        mmio(0xd, true, address & 255, 1);
        mmio(0xb, true, (address >> 8) & 255, 1);
        mmio(9, true, (address >> 16) & 255, 1);
        // The exact FIFO reset and sector-count sequence in EmuTOS floppy.c.
        control(0x190); control(0x090); mmio(4, true, count);
    }
    void read_sector(unsigned sector, unsigned command = 0x80)
    { fdc_write(2, sector); fdc_write(0, command, 13); }
    uint32_t dma_address()
    { return ((mmio(9, false) & 255) << 16) | ((mmio(0xb, false) & 255) << 8) |
             (mmio(0xd, false) & 255); }
    void compare(unsigned address, unsigned offset, unsigned bytes)
    {
        check(std::equal(media.begin() + offset, media.begin() + offset + bytes,
                         ram.begin() + address), "DMA RAM differs from sector image");
    }

private:
    bool media_seen = false, dma_seen = false, media_done = false, dma_done = false;
    bool disk_seen = false, disk_done = false;
    unsigned disk_age = 0, saved_disk = 0; uint16_t saved_disk_word = 0;
    unsigned media_age = 0, dma_age = 0;
    uint32_t saved_media = 0, saved_dma = 0;
    uint16_t saved_word = 0;
    void put_le16(unsigned offset, unsigned value)
    { media[offset] = uint8_t(value); media[offset + 1] = uint8_t(value >> 8); }
};

static void test_registers_and_type_i(Rig &r)
{
    r.restore();
    check(r.dut.irq, "restore has no IRQ");
    r.control(0x90); check((r.mmio(6, false) & 1) != 0, "reset DMA is not OK");
    check(r.dut.irq, "DMA status unexpectedly cleared FDC IRQ");
    check((r.fdc_read(0) & 0x45) == 0x44, "restore lacks track-zero/write-protect status");
    check(!r.dut.irq, "FDC status failed to clear IRQ");
    r.seek(7); check(!(r.fdc_read(0) & 0x10) && r.fdc_read(1) == 7,
                     "seek failed to move the physical head and track register");
    r.fdc_write(0, 0x50); r.wait_irq(); check(r.fdc_read(1) == 8, "step-in update failed");
    r.fdc_write(0, 0x60); r.wait_irq(); check(r.fdc_read(1) == 8, "step-out changed track without U");
    r.fdc_write(0, 0x14); r.wait_irq();
    check(r.fdc_read(0) & 0x10, "verify did not detect physical/register track mismatch");
    r.restore(); r.fdc_read(0);
    r.mmio(9, true, 0x12, 2); check((r.mmio(9, false) & 255) == 0,
                                  "upper byte wrote an odd DMA register");
    r.control(0x82); r.mmio(4, true, 23, 1);
    check(r.fdc_read(1) == 0, "partial word wrote the FDC data port");
    r.control(0x82); r.mmio(6, true, 0x84, 1); r.mmio(4, true, 3);
    check(r.fdc_read(1) == 3, "partial word changed DMA mode selection");
    r.restore(); r.fdc_read(0);
}

static void test_sectors(Rig &r)
{
    auto m = r.media_transfers, d = r.dma_transfers;
    r.setup(0x1000); r.read_sector(1); r.wait_irq();
    check(r.media_transfers - m == 512 && r.dma_transfers - d == 256,
          "single sector transfer count differs from 512 bytes");
    r.compare(0x1000, 0, 512);
    check(r.dma_address() == 0x1200, "DMA cursor did not advance by one sector");
    r.control(0x90); check(r.mmio(6, false) == 1, "completed sector has wrong DMA status");
    check(!(r.fdc_read(0) & 0x1d), "single sector completed with FDC error or busy");
    r.seek(79); check(!(r.fdc_read(0) & 0x10), "last-track seek failed");
    r.dut.side = 0; r.setup(0x1800); r.read_sector(9); r.wait_irq();
    r.compare(0x1800, unsigned(r.media.size() - 512), 512);
    check(!(r.fdc_read(0) & 0x1d), "last-sector read failed");
    r.dut.side = 1; r.restore(); r.fdc_read(0);
}

static void test_multi_and_write_protect(Rig &r)
{
    auto d = r.dma_transfers;
    r.setup(0x2000, 2); r.read_sector(2, 0x90);
    for (unsigned n = 0; r.dma_transfers - d < 512 && n < 50000; ++n) r.tick();
    check(r.dma_transfers - d == 512, "multiple-sector read did not transfer its count");
    r.compare(0x2000, 512, 1024); r.idle(100);
    check(r.dma_transfers - d == 512 && !r.dut.irq,
          "multiple read exceeded sector count or fabricated command completion");
    check((r.fdc_read(0) & 3) == 3, "count exhaustion did not leave FDC busy with DRQ");
    r.fdc_write(0, 0xd0); check(!r.dut.irq && !(r.fdc_read(0) & 1),
                               "D0 failed to terminate without IRQ");
    r.fdc_write(0, 0xd8); check(r.dut.irq, "D8 immediate interrupt failed"); r.fdc_read(0);
    check(r.dut.irq, "status read incorrectly cleared latched D8 interrupt");
    r.fdc_write(0, 0xd0); check(!r.dut.irq, "D0 did not release latched D8 interrupt");
    auto m = r.media_transfers;
    r.setup(0x3000); r.fdc_write(0, 0xa0); r.wait_irq();
    check((r.fdc_read(0) & 0x41) == 0x40, "write command lacks write protection");
    r.fdc_write(0, 0xf0); r.wait_irq(); check(r.fdc_read(0) & 0x40, "format lacks write protection");
    check(r.media_transfers == m && r.dma_transfers - d == 512,
          "read-only command changed RAM or consumed media");
    r.setup(0x3000, 2); r.read_sector(9, 0x90); r.wait_irq();
    check(r.fdc_read(0) & 0x10, "multiple read beyond track lacks record-not-found");
    r.compare(0x3000, 8 * 512, 512);
}

static void test_backpressure_and_cancel(Rig &r)
{
    r.setup(0x4000); r.stall_dma = true; r.read_sector(1);
    for (unsigned n = 0; !r.dut.dma_req && n < 1000; ++n) r.tick();
    check(r.dut.dma_req, "read did not reach a stalled DMA write");
    const auto d = r.dma_transfers; const auto a = r.dut.dma_addr;
    const auto w = r.dut.dma_wdata;
    r.mmio(9, true, 0xfc, 1); r.fdc_write(1, 54); r.fdc_write(2, 9);
    r.control(0x90); r.mmio(4, true, 99); r.idle(100);
    check(r.dut.dma_req && r.dut.dma_addr == a && r.dut.dma_wdata == w &&
          r.dma_transfers == d, "CPU setup changed a pending DMA write");
    r.stall_dma = false; r.wait_irq(); r.compare(0x4000, 0, 512);
    check(r.dma_address() == 0x4200 && r.fdc_read(1) == 0,
          "busy setup writes were not ignored"); r.fdc_read(0);
    r.setup(0x5000); r.stall_dma = true; r.read_sector(1);
    for (unsigned n = 0; !r.dut.dma_req && n < 1000; ++n) r.tick();
    const auto before = r.dma_transfers;
    r.fdc_write(0, 0xd8); r.stall_dma = false; r.idle(20);
    check(r.dut.irq && !r.dut.dma_req && !r.dut.media_req &&
          r.dma_transfers == before, "force interrupt did not cancel stalled DMA");
    r.fdc_read(0); r.fdc_write(0, 0xd0);
    r.setup(0x5000); r.stall_media = true; r.read_sector(1); r.idle(20);
    check(r.dut.media_req, "read did not issue external media request");
    r.dut.media_ready = 0; r.tick(); r.stall_media = false; r.idle(10);
    check(r.dut.irq && !r.dut.media_req && r.dma_transfers == before,
          "media eject did not cancel pending stream request");
    check(r.fdc_read(0) & 0x10, "media eject lacks record-not-found");
    r.dut.media_ready = 1;
}

static void test_absence_and_dma_bounds(Rig &r)
{
    r.dut.drive_select = 1; r.restore();
    check((r.fdc_read(0) & 0x14) == 0x10, "absent drive B reports track zero");
    r.setup(0x6000); r.read_sector(1); r.wait_irq();
    check(r.fdc_read(0) & 0x10, "absent drive read lacks record-not-found");
    r.dut.drive_select = 2; r.restore(); r.fdc_read(0);
    r.dut.media_ready = 0; r.restore();
    check((r.fdc_read(0) & 0x44) == 0x44,
          "connected empty drive lacks restore track-zero/write-protect");
    r.setup(0x6000); r.read_sector(1); r.wait_irq();
    check(r.fdc_read(0) & 0x10, "empty media read lacks record-not-found");
    r.dut.media_ready = 1;
    for (unsigned address : {0u, 0xfc0000u, 0x080000u}) {
        auto d = r.dma_transfers;
        r.setup(address); r.read_sector(1); r.wait_irq();
        check(r.dma_transfers == d && (r.fdc_read(0) & 4),
              "unsafe DMA destination did not stop with lost data");
        r.control(0x90); check(!(r.mmio(6, false) & 1), "DMA error incorrectly reports OK");
    }
    auto d = r.dma_transfers;
    r.setup(0x07fffe); r.read_sector(1); r.wait_irq();
    check(r.dma_transfers == d + 1 && (r.fdc_read(0) & 4),
          "RAM boundary did not stop before out-of-range word");
    r.compare(0x07fffe, 0, 2);
    r.reset(); r.setup(0x6000); r.stall_media = true; r.read_sector(1);
    check(r.dut.media_req, "reset test has no pending request"); r.reset();
    r.idle(100); check(!r.dut.media_req && !r.dut.dma_req && !r.dut.irq,
                      "reset restarted an old command");
    r.setup(0x6000); r.stall_dma = true; r.read_sector(1);
    for (unsigned n = 0; !r.dut.dma_req && n < 1000; ++n) r.tick();
    check(r.dut.dma_req, "reset test has no pending RAM request");
    const auto before_reset = r.dma_transfers;
    r.reset(); r.idle(100);
    check(r.dma_transfers == before_reset && !r.dut.dma_req && !r.dut.irq,
          "reset committed or restarted an old RAM write");
    for (unsigned address : {0u, 2u, 0xeu, 0xfu}) {
        r.dut.mmio_addr = address; r.dut.mmio_req = 1; r.idle(10);
        check(!r.dut.mmio_ack, "controller acknowledged a non-ST register");
        r.dut.mmio_req = 0; r.tick();
    }
}

static void test_removal_during_stalled_dma(Rig &r)
{
    for (bool eject : {true, false}) {
        r.reset();
        const unsigned destination = eject ? 0x7000 : 0x7200;
        const uint8_t original_high = r.ram[destination];
        const uint8_t original_low = r.ram[destination + 1];
        r.setup(destination, 2); r.stall_dma = true; r.read_sector(1);
        for (unsigned n = 0; !r.dut.dma_req && n < 1000; ++n) r.tick();
        check(r.dut.dma_req, "removal test did not reach the first stalled RAM word");
        const auto words = r.dma_transfers, bytes = r.media_transfers;
        if (eject) r.dut.media_ready = 0;
        else r.dut.drive_select = 3;
        r.dut.eval();
        check(!r.dut.dma_req, "removed drive can still issue a new RAM command");
        r.tick();
        check(r.dut.irq && !r.dut.dma_req && !r.dut.media_req,
              "removal while DMA stalled did not immediately complete the command");
        // An old completion may arrive after withdrawal. It cannot advance
        // this command, and RAM becoming available cannot grant a new write.
        r.dut.dma_ready = 1; r.dut.clk = 0; r.dut.eval();
        r.dut.clk = 1; r.dut.eval(); ++r.cycles; r.dut.dma_ready = 0;
        r.stall_dma = false; r.idle(30);
        check(r.dma_transfers == words && r.media_transfers == bytes &&
              r.ram[destination] == original_high && r.ram[destination + 1] == original_low,
              "removal allowed an old disk word to reach RAM");
        check(r.dma_address() == destination, "canceled DMA advanced its address");
        r.control(0x90); check((r.mmio(4, false) & 255) == 2,
                              "canceled DMA consumed the sector count");
        const uint8_t status = r.fdc_read(0);
        check((status & 0x13) == 0x10,
              "removed DMA command lacks record-not-found or remains busy/DRQ");
    }
    r.reset();
}

static void test_force_index_and_motor(Rig &r)
{
    r.restore(); r.fdc_read(0); r.fdc_write(0, 0xd4);
    // This also runs with the physical default 300 RPM period. Unit recipes
    // may select INDEX_PERIOD_CYCLES=127 to exercise the same divider quickly.
    for (unsigned n = 0; !r.dut.irq && n < 20000000; ++n) r.tick();
    check(r.dut.irq, "D4 failed to interrupt at a virtual index pulse");
    r.fdc_read(0); check(!r.dut.irq, "ordinary index IRQ did not clear on status read");
    r.fdc_write(0, 0xd0);
    for (unsigned n = 0; n < 100000; ++n) {
        // Sampling status must not restart the spindle idle countdown. At the
        // physical period, amortize MMIO traffic between status checks.
        if (!(r.fdc_read(0) & 0x80)) return;
        r.idle(1024);
    }
    check(false, "idle motor did not stop after nine virtual revolutions");
}

static void test_supported_geometries(Rig &r)
{
    for (unsigned tracks : {80u, 81u, 82u}) for (unsigned heads : {1u, 2u})
    for (unsigned sectors : {9u, 10u}) {
        r.media.resize(tracks * heads * sectors * 512);
        for (unsigned n = 0; n < r.media.size(); ++n) r.media[n] = uint8_t(n * 17 + (n >> 9));
        r.dut.media_size = r.media.size();
        // Deliberately invalid BPB: guest data cannot redefine physical CHS.
        std::fill(r.media.begin(), r.media.begin() + 512, 0xff);
        r.dut.side = 1; r.restore(); r.fdc_read(0);
        for (unsigned track : {0u, tracks - 1}) for (unsigned head = 0; head < heads; ++head) {
            r.dut.side = head == 0; r.seek(track);
            check(!(r.fdc_read(0) & 0x10), "last supported track failed verify");
            for (unsigned sector : {1u, sectors}) {
                const unsigned offset = ((track * heads + head) * sectors + sector - 1) * 512;
                r.setup(0x1000); r.read_sector(sector); r.wait_irq();
                check(!(r.fdc_read(0) & 0x10), "valid geometry sector rejected");
                r.compare(0x1000, offset, 512);
            }
            r.setup(0x1000); r.read_sector(sectors + 1); r.wait_irq();
            check(r.fdc_read(0) & 0x10, "sector beyond geometry accepted");
            const auto bytes = r.media_transfers;
            r.setup(0x1000, 2); r.read_sector(sectors, 0x90); r.wait_irq();
            check((r.fdc_read(0) & 0x10) && r.media_transfers == bytes + 512,
                  "multi-sector command crossed track end");
        }
        r.dut.side = 1; r.seek(tracks);
        check(r.fdc_read(0) & 0x10, "track beyond geometry verified");
        r.restore(); r.fdc_read(0);
        if (heads == 1) {
            r.dut.side = 0; const auto bytes = r.media_transfers;
            r.setup(0x1000); r.read_sector(1); r.wait_irq();
            check((r.fdc_read(0) & 0x10) && r.media_transfers == bytes,
                  "absent second side issued disk reads");
        }
    }
    r.dut.side = 1; r.dut.media_size = 839679; r.restore(); r.fdc_read(0);
    const auto bytes = r.media_transfers;
    r.setup(0x1000); r.read_sector(1); r.wait_irq();
    check((r.fdc_read(0) & 0x10) && r.media_transfers == bytes, "unknown size produced geometry");
}

int main(int argc, char **argv)
{
    Verilated::commandArgs(argc, argv);
    try {
        Rig r;
        test_registers_and_type_i(r); test_sectors(r);
        test_multi_and_write_protect(r); test_backpressure_and_cancel(r);
        test_removal_during_stalled_dma(r);
        test_absence_and_dma_bounds(r);
        test_force_index_and_motor(r); test_supported_geometries(r);
        std::printf("PASS st_floppy: EmuTOS register setup, all twelve geometries, exact DMA, "
                    "waits/cancellation, multiple reads, read-only media, and RAM bounds "
                    "(%llu cycles, %llu media bytes, %llu RAM words)\n",
                    (unsigned long long)r.cycles, (unsigned long long)r.media_transfers,
                    (unsigned long long)r.dma_transfers);
        return 0;
    } catch (const std::exception &e) {
        std::fprintf(stderr, "FAIL st_floppy: %s\n", e.what());
        return 1;
    }
}
