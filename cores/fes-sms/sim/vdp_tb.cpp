// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vsms_vdp.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {

[[noreturn]] void fail(const char *message) {
    std::cerr << "FES SMS Mode 4 VDP: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const char *message) {
    if (!condition) fail(message);
}

void tick(Vsms_vdp &dut) {
    dut.clk = 1;
    dut.eval();
    dut.clk = 0;
    dut.eval();
}

void io_write(Vsms_vdp &dut, uint8_t port, uint8_t value) {
    dut.cpu_ce = 1;
    dut.cpu_iorq_n = 0;
    dut.cpu_rd_n = 1;
    dut.cpu_wr_n = 0;
    dut.cpu_a = port;
    dut.cpu_din = value;
    tick(dut);
    dut.cpu_ce = 0;
    dut.cpu_iorq_n = 1;
    dut.cpu_wr_n = 1;
    for (unsigned cycle = 0; cycle != 4; ++cycle) tick(dut);
}

uint8_t io_read(Vsms_vdp &dut, uint8_t port) {
    dut.cpu_ce = 1;
    dut.cpu_iorq_n = 0;
    dut.cpu_rd_n = 0;
    dut.cpu_wr_n = 1;
    dut.cpu_a = port;
    dut.eval();
    const uint8_t value = dut.cpu_dout;
    tick(dut);
    dut.cpu_ce = 0;
    dut.cpu_iorq_n = 1;
    dut.cpu_rd_n = 1;
    for (unsigned cycle = 0; cycle != 4; ++cycle) tick(dut);
    return value;
}

void write_register(Vsms_vdp &dut, uint8_t index, uint8_t value) {
    io_write(dut, 0xbf, value);
    io_write(dut, 0xbf, uint8_t(0x80 | index));
}

void set_address(Vsms_vdp &dut, uint16_t address, uint8_t command) {
    io_write(dut, 0xbf, uint8_t(address));
    io_write(dut, 0xbf, uint8_t((command << 6) | ((address >> 8) & 0x3f)));
}

void write_vram(Vsms_vdp &dut, uint16_t address, uint8_t value) {
    set_address(dut, address, 1);
    io_write(dut, 0xbe, value);
}

void write_cram(Vsms_vdp &dut, uint8_t address, uint8_t value) {
    set_address(dut, address, 3);
    io_write(dut, 0xbe, value);
}

void raster_step(Vsms_vdp &dut) {
    dut.raster_ce = 1;
    tick(dut);
    dut.raster_ce = 0;
    for (unsigned cycle = 0; cycle != 15; ++cycle) tick(dut);
}

uint8_t sample_pixel(Vsms_vdp &dut, uint8_t x, uint8_t y, unsigned occurrence) {
    unsigned seen = 0;
    for (unsigned pixel = 0; pixel != 256 * 262 * 4; ++pixel) {
        dut.raster_ce = 1;
        dut.eval();
        if (dut.raster_x == x && dut.raster_y == y) {
            ++seen;
            if (seen == occurrence) {
                const uint8_t color = dut.raster_color;
                raster_step(dut);
                return color;
            }
        }
        raster_step(dut);
    }
    fail("pixel sample did not occur");
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vsms_vdp dut;
    dut.clk = 0;
    dut.reset = 1;
    dut.cpu_ce = 0;
    dut.cpu_iorq_n = 1;
    dut.cpu_rd_n = 1;
    dut.cpu_wr_n = 1;
    dut.cpu_a = 0;
    dut.cpu_din = 0;
    dut.raster_ce = 0;
    dut.eval();
    for (unsigned cycle = 0; cycle != 8; ++cycle) tick(dut);
    dut.reset = 0;

    write_register(dut, 0, 0x04);
    write_register(dut, 1, 0x40);
    write_register(dut, 2, 0x0e);
    write_register(dut, 5, 0x7e);
    write_register(dut, 6, 0x00);
    write_register(dut, 7, 0x00);
    write_register(dut, 8, 0x00);
    write_register(dut, 9, 0x00);
    write_register(dut, 10, 0xff);

    write_vram(dut, 0x1234, 0xa5);
    set_address(dut, 0x1234, 0);
    require(io_read(dut, 0xbe) == 0xa5, "VRAM read-ahead mismatch");

    write_cram(dut, 1, 0x07);
    write_cram(dut, 17, 0x34);
    write_cram(dut, 18, 0x3f);
    write_vram(dut, 0x0020, 0x80);
    write_vram(dut, 0x0041, 0x80);
    write_vram(dut, 0x3800, 0x01);
    write_vram(dut, 0x3801, 0x18);

    write_vram(dut, 0x3f00, 0xff);
    write_vram(dut, 0x3f01, 0xff);
    write_vram(dut, 0x3f02, 0xd0);
    write_vram(dut, 0x3f80, 0x00);
    write_vram(dut, 0x3f81, 0x02);
    write_vram(dut, 0x3f82, 0x00);
    write_vram(dut, 0x3f83, 0x02);

    const uint8_t priority_pixel = sample_pixel(dut, 0, 0, 3);
    if (priority_pixel != 0x34) {
        std::cerr << "priority pixel was 0x" << std::hex << unsigned(priority_pixel)
                  << std::dec << '\n';
        fail("priority/palette background did not cover the sprite");
    }
    const uint8_t collision = io_read(dut, 0xbf);
    require((collision & 0x20) != 0, "overlapping sprites did not latch collision");
    require((io_read(dut, 0xbf) & 0x20) == 0, "status read did not clear collision");

    write_register(dut, 0, 0x14);
    write_register(dut, 10, 0x00);
    while (!(dut.raster_x == 255 && dut.raster_y == 261)) raster_step(dut);
    raster_step(dut);
    while (!(dut.raster_x == 255 && dut.raster_y == 0)) raster_step(dut);
    raster_step(dut);
    require(dut.irq_n == 0, "line interrupt did not assert");
    io_read(dut, 0xbf);
    require(dut.irq_n == 1, "status read did not clear line interrupt");

    write_register(dut, 0, 0x04);
    write_register(dut, 1, 0x60);
    while (!(dut.raster_x == 255 && dut.raster_y == 191)) raster_step(dut);
    raster_step(dut);
    require(dut.irq_n == 0, "VBlank interrupt did not assert");
    require((io_read(dut, 0xbf) & 0x80) != 0, "VBlank status was not reported");
    require(dut.irq_n == 1, "status read did not clear VBlank interrupt");

    std::cout << "FES SMS Mode 4 VDP checks passed\n";
    return EXIT_SUCCESS;
}
