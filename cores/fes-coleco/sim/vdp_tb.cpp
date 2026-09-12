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
    for (unsigned i = 0; i < 4; ++i) tick(dut);
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
    for (unsigned i = 0; i < 4; ++i) tick(dut);
    return value;
}

void write_register(Vcoleco_vdp &dut, uint8_t index, uint8_t value) {
    io_write(dut, 0xbf, value);
    io_write(dut, 0xbf, uint8_t(0x80 | (index & 0x0f)));
}

void write_vram(Vcoleco_vdp &dut, uint16_t address, uint8_t value) {
    io_write(dut, 0xbf, uint8_t(address));
    io_write(dut, 0xbf, uint8_t(0x40 | ((address >> 8) & 0x3f)));
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

    // Read-address setup prefetches the first byte; subsequent reads return
    // consecutive buffered bytes, including the 16 KiB address wrap.
    write_vram(dut, 0x3ffe, 0x12);
    io_write(dut, 0xbe, 0x34);
    io_write(dut, 0xbe, 0x56);
    io_write(dut, 0xbf, 0xfe);
    io_write(dut, 0xbf, 0x3f);
    require(io_read(dut, 0xbe) == 0x12, "read setup did not prefetch first byte");
    require(io_read(dut, 0xbe) == 0x34, "sequential buffered read");
    require(io_read(dut, 0xbe) == 0x56, "buffered read did not wrap at 3fff");

    // A real Z80 holds RD across several enables. Return the same byte for
    // the entire transaction and consume it only once.
    io_write(dut, 0xbf, 0xfe);
    io_write(dut, 0xbf, 0x3f);
    dut.cpu_ce = 1; dut.cpu_iorq_n = 0; dut.cpu_rd_n = 0;
    dut.cpu_a = 0xbe; dut.eval();
    for (unsigned i = 0; i < 12; ++i) {
        require(dut.cpu_dout == 0x12, "held data read changed its return byte");
        tick(dut);
    }
    dut.cpu_iorq_n = 1; dut.cpu_rd_n = 1; tick(dut);
    require(io_read(dut, 0xbe) == 0x34, "held data read advanced more than once");

    // A write address must not prefetch. Data writes update the shared
    // read-ahead buffer; a subsequent read returns that byte first.
    write_vram(dut, 0x1234, 0xab);
    io_write(dut, 0xbf, 0xfe);
    io_write(dut, 0xbf, 0x7f);
    require(io_read(dut, 0xbe) == 0xab, "write setup incorrectly prefetched VRAM");
    require(io_read(dut, 0xbe) == 0x12, "read after write-mode setup skipped its address");

    // Reading status abandons a half-written control command.
    io_write(dut, 0xbf, 0x99);
    io_read(dut, 0xbf);
    io_write(dut, 0xbf, 0xfe);
    io_write(dut, 0xbf, 0x3f);
    require(io_read(dut, 0xbe) == 0x12, "status read did not clear the control latch");

    write_register(dut, 1, 0x00);
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
    require(dut.irq_n, "disabled VBlank interrupt asserted");
    write_register(dut, 1, 0x20);
    require(!dut.irq_n, "enabling pending VBlank did not assert interrupt");
    write_register(dut, 1, 0x00);
    require(dut.irq_n, "disabling interrupt did not release line");
    write_register(dut, 1, 0x20);
    require(!dut.irq_n, "disabling interrupt incorrectly cleared pending status");
    dut.cpu_ce = 1; dut.cpu_iorq_n = 0; dut.cpu_rd_n = 0;
    dut.cpu_a = 0xbf; dut.eval();
    for (unsigned i = 0; i < 12; ++i) {
        require((dut.cpu_dout & 0x80) != 0, "held status read lost VBlank before CPU sampled it");
        tick(dut);
        require(dut.irq_n, "status acknowledgement did not release interrupt");
        dut.cpu_ce = !dut.cpu_ce;
    }
    dut.cpu_iorq_n = 1; dut.cpu_rd_n = 1; tick(dut);
    require((io_read(dut, 0xbf) & 0x80) == 0, "status did not clear on read");
    for (unsigned i = 0; i < 70000; ++i) {
        dut.raster_ce = 1; tick(dut);
    }
    dut.raster_ce = 0;
    require(!dut.irq_n, "next frame did not reassert interrupt");
    dut.reset = 1; tick(dut); dut.reset = 0;
    require(dut.irq_n, "reset did not clear interrupt");
    require(io_read(dut, 0xbf) == 0, "reset did not clear status");
    write_register(dut, 1, 0x20);
    // Start from reset's known scan origin, without forcing raster state.
    // A new frame event on the acknowledgement edge must remain pending;
    // the held read still returns its snapshot from before that edge.
    dut.raster_ce = 1;
    for (unsigned i = 0; i < 256*192-1; ++i) tick(dut);
    dut.cpu_ce = 1; dut.cpu_iorq_n = 0; dut.cpu_rd_n = 0;
    dut.cpu_a = 0xbf; dut.eval();
    require(dut.cpu_dout == 0, "unexpected status before first frame event");
    tick(dut);
    dut.raster_ce = 0;
    for (unsigned i = 0; i < 12; ++i) {
        require(!dut.irq_n, "held status read erased a new frame event");
        require(dut.cpu_dout == 0, "new frame changed an already-held read snapshot");
        tick(dut);
    }
    dut.cpu_iorq_n = 1; dut.cpu_rd_n = 1; tick(dut);
    require(io_read(dut, 0xbf) == 0x80, "new frame was lost at status acknowledgement");
    require(dut.irq_n, "second acknowledgement did not clear the new event");
    std::cout << "FES Coleco VDP tile/status path passed\n";
    return EXIT_SUCCESS;
}
