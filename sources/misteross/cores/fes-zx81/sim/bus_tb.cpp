// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vexpansion_bus_harness.h"
#include "verilated.h"
#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <iostream>
#include <string>

static void tick(Vexpansion_bus_harness &dut) {
    dut.clk = 1;
    dut.eval();
    dut.clk = 0;
    dut.eval();
}

static void idle(Vexpansion_bus_harness &dut) {
    dut.cpu_mreq_n = 1;
    dut.cpu_iorq_n = 1;
    dut.cpu_rd_n = 1;
    dut.cpu_wr_n = 1;
    dut.cpu_m1_n = 1;
    dut.cpu_rfsh_n = 1;
}

static void settle(Vexpansion_bus_harness &dut, unsigned n = 4) {
    for (unsigned i = 0; i < n; ++i) tick(dut);
}

static void mem_write(Vexpansion_bus_harness &dut, uint16_t addr, uint8_t data) {
    dut.cpu_addr = addr;
    dut.cpu_wdata = data;
    dut.cpu_mreq_n = 0;
    dut.cpu_iorq_n = 1;
    dut.cpu_rd_n = 1;
    dut.cpu_wr_n = 0;
    dut.cpu_m1_n = 1;
    dut.cpu_rfsh_n = 1;
    settle(dut);
    idle(dut);
    settle(dut, 2);
}

static uint8_t mem_read(Vexpansion_bus_harness &dut, uint16_t addr, bool release = true) {
    dut.cpu_addr = addr;
    dut.cpu_mreq_n = 0;
    dut.cpu_iorq_n = 1;
    dut.cpu_rd_n = 0;
    dut.cpu_wr_n = 1;
    dut.cpu_m1_n = 1;
    dut.cpu_rfsh_n = 1;
    settle(dut);
    const uint8_t value = dut.bus_rdata;
    if (release) {
        idle(dut);
        settle(dut, 2);
    }
    return value;
}

static void io_write(Vexpansion_bus_harness &dut, uint16_t port, uint8_t data) {
    dut.cpu_addr = port;
    dut.cpu_wdata = data;
    dut.cpu_mreq_n = 1;
    dut.cpu_iorq_n = 0;
    dut.cpu_rd_n = 1;
    dut.cpu_wr_n = 0;
    dut.cpu_m1_n = 1;
    dut.cpu_rfsh_n = 1;
    settle(dut);
    idle(dut);
    settle(dut, 2);
}

static uint8_t io_read(Vexpansion_bus_harness &dut, uint16_t port, bool release = true) {
    dut.cpu_addr = port;
    dut.cpu_mreq_n = 1;
    dut.cpu_iorq_n = 0;
    dut.cpu_rd_n = 0;
    dut.cpu_wr_n = 1;
    dut.cpu_m1_n = 1;
    dut.cpu_rfsh_n = 1;
    settle(dut);
    const uint8_t value = dut.bus_rdata;
    if (release) {
        idle(dut);
        settle(dut, 2);
    }
    return value;
}

static uint8_t peek(Vexpansion_bus_harness &dut, uint16_t address) {
    dut.peek_address = address & 0x3fff;
    settle(dut, 3);
    return dut.bus_peek_data;
}

static int fail(const std::string &message) {
    std::cerr << "ZX81 expansion bus: " << message << '\n';
    return EXIT_FAILURE;
}

static int test_ram16k(Vexpansion_bus_harness &dut) {
    settle(dut, 8);
    if (!dut.bus_ram_present) return fail("16K pack did not assert RAM_PRESENT");
    mem_write(dut, 0x4000, 0xa5);
    mem_write(dut, 0x7fff, 0x5a);
    mem_write(dut, 0x1234, 0x11);
    if (peek(dut, 0x0000) != 0xa5) return fail("4000 write missed the pack");
    if (peek(dut, 0x3fff) != 0x5a) return fail("7FFF write missed the pack");
    if (peek(dut, 0x1234) == 0x11) return fail("below-4000 write leaked into the pack");
    if (mem_read(dut, 0x4000) != 0xa5) return fail("4000 CPU read missed the pack");
    if (dut.bus_dsel || dut.bus_romcs || dut.bus_wait)
        return fail("16K pack drove ROMCS/WAIT/DSEL");
    std::cout << "ZX81 16K pack decode passed\n";
    return EXIT_SUCCESS;
}

static int test_zonx(Vexpansion_bus_harness &dut) {
    settle(dut, 8);
    if (dut.bus_ram_present) return fail("Zon X asserted RAM_PRESENT");
    for (unsigned n = 0; n < 16; ++n) {
        const uint8_t value = uint8_t(n * 17);
        io_write(dut, 0x00df, uint8_t(n));
        io_write(dut, 0x000f, value);
        const uint8_t got = io_read(dut, 0x000f, false);
        if (!dut.bus_dsel) return fail("Zon X data read did not select the bus");
        idle(dut);
        settle(dut, 2);
        if (got != value) {
            std::cerr << "Zon X register " << n << " got " << unsigned(got)
                      << " expected " << unsigned(value) << '\n';
            return EXIT_FAILURE;
        }
    }
    io_write(dut, 0x00df, 3);
    io_write(dut, 0x00cf, 3);
    io_write(dut, 0x000f, 0x42);
    io_write(dut, 0x00df, 3);
    if (io_read(dut, 0x000f) != 0x42) return fail("Zon X xxCF select alias failed");
    io_write(dut, 0x00df, 0);
    io_write(dut, 0x000f, 8);
    io_write(dut, 0x00df, 1);
    io_write(dut, 0x000f, 0);
    io_write(dut, 0x00df, 7);
    io_write(dut, 0x000f, 0x3e);
    io_write(dut, 0x00df, 8);
    io_write(dut, 0x000f, 0x0f);
    idle(dut);
    bool saw_high = false;
    bool saw_low = false;
    for (int i = 0; i < 4096; ++i) {
        tick(dut);
        const uint8_t pcm = dut.bus_peek_data;
        if (pcm == 0xf0) saw_high = true;
        if (pcm == 0x00) saw_low = true;
    }
    if (!saw_high || !saw_low)
        return fail("Zon X channel A square missed peek_d");
    std::cout << "ZX81 Zon X two-cycle registers passed\n";
    return EXIT_SUCCESS;
}

static int test_qs(Vexpansion_bus_harness &dut) {
    settle(dut, 8);
    if (dut.bus_ram_present) return fail("QS asserted RAM_PRESENT");
    // Sinclair '0' is code 28; ROM 1E00 row 1 is 0x3c.
    if (mem_read(dut, 0x8400 + (28 * 8) + 1) != 0x3c)
        return fail("QS charset preload missed Sinclair glyph 0");
    mem_write(dut, 0x8400, 0xa5);
    mem_write(dut, 0x87ff, 0x5a);
    mem_write(dut, 0x4000, 0x11);
    const uint8_t low = mem_read(dut, 0x8400, false);
    if (!dut.bus_dsel || !dut.bus_romcs) return fail("QS window did not assert DSEL/ROMCS");
    idle(dut);
    settle(dut, 2);
    if (low != 0xa5) return fail("8400 write missed QS memory");
    if (mem_read(dut, 0x87ff) != 0x5a) return fail("87FF write missed QS memory");
    idle(dut);
    dut.cpu_addr = 0x4000;
    dut.cpu_mreq_n = 0;
    dut.cpu_rd_n = 0;
    settle(dut);
    if (dut.bus_romcs || dut.bus_dsel) return fail("QS decoded outside 8400-87FF");
    idle(dut);
    dut.cpu_addr = 0x8400;
    dut.cpu_mreq_n = 0;
    dut.cpu_iorq_n = 1;
    dut.cpu_rd_n = 1;
    dut.cpu_wr_n = 1;
    dut.cpu_rfsh_n = 0;
    settle(dut);
    if (!dut.bus_romcs) return fail("QS /RFSH fetch did not assert ROMCS");
    if (dut.bus_rdata != 0xa5) return fail("QS /RFSH fetch missed character memory");
    idle(dut);
    std::cout << "ZX81 QS character window passed\n";
    return EXIT_SUCCESS;
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    if (argc != 2) return EXIT_FAILURE;
    Vexpansion_bus_harness dut;
    dut.cpu_addr = 0;
    dut.cpu_wdata = 0;
    dut.peek_address = 0;
    idle(dut);
    dut.clk = 0;
    dut.eval();
    const std::string which = argv[1];
    if (which == "ram16k") return test_ram16k(dut);
    if (which == "zonx") return test_zonx(dut);
    if (which == "qs") return test_qs(dut);
    return fail("unknown cart " + which);
}
