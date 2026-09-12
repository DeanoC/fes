// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vcoleco_vdp.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {

[[noreturn]] void fail(const char *message) {
    std::cerr << "FES Coleco VDP: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const char *message) {
    if (!condition) fail(message);
}

void tick(Vcoleco_vdp &dut) {
    dut.clk = 1;
    dut.eval();
    dut.clk = 0;
    dut.eval();
}

void io_write(Vcoleco_vdp &dut, uint8_t port, uint8_t value) {
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
}

uint8_t io_read(Vcoleco_vdp &dut, uint8_t port) {
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
    return value;
}

void write_register(Vcoleco_vdp &dut, uint8_t index, uint8_t value) {
    io_write(dut, 0xbf, value);
    io_write(dut, 0xbf, uint8_t(0x80 | (index & 0x0f)));
}

void write_vram(Vcoleco_vdp &dut, uint16_t address, uint8_t value) {
    io_write(dut, 0xbf, uint8_t(address));
    io_write(dut, 0xbf, uint8_t((address >> 8) & 0x3f));
    io_write(dut, 0xbe, value);
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vcoleco_vdp dut;
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
    for (unsigned i = 0; i != 8; ++i) tick(dut);
    dut.reset = 0;

    write_register(dut, 1, 0x20);
    write_register(dut, 2, 0x00);
    write_register(dut, 3, 0x80);
    write_register(dut, 4, 0x01);
    write_vram(dut, 0x0001, 0x01);
    write_vram(dut, 0x0808, 0x80);
    write_vram(dut, 0x2001, 0xf1);

    bool saw_foreground = false;
    for (unsigned i = 0; i != 70000; ++i) {
        dut.raster_ce = 1;
        dut.eval();
        if (dut.raster_x == 8 && dut.raster_y == 0 && dut.raster_pixel != 0)
            saw_foreground = true;
        tick(dut);
        dut.raster_ce = 0;
    }
    require(saw_foreground, "Graphics I tile did not produce a foreground pixel");
    const uint8_t status = io_read(dut, 0xbf);
    require((status & 0x80) != 0, "vertical blank status was not raised");
    require((io_read(dut, 0xbf) & 0x80) == 0, "status did not clear on read");
    std::cout << "FES Coleco VDP tile/status path passed\n";
    return EXIT_SUCCESS;
}
