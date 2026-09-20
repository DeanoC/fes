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

void raster_gap(Vcoleco_vdp &dut) {
    dut.raster_ce = 0;
    // The machine's VDP enable is one pulse every sixteen negedges (roughly
    // thirty-two full system clocks). Keep the direct unit on that cadence so
    // the registered sprite line walker has time to prepare the next line.
    for (unsigned i = 0; i != 32; ++i) tick(dut);
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

    write_register(dut, 1, 0x40);
    write_register(dut, 2, 0x00);
    write_register(dut, 3, 0x80);
    write_register(dut, 4, 0x01);
    write_vram(dut, 0x0001, 0x01);
    write_vram(dut, 0x0808, 0x80);
    write_vram(dut, 0x2000, 0xf0);

    bool saw_foreground = false;
    for (unsigned i = 0; i != 70000; ++i) {
        dut.raster_ce = 1;
        dut.eval();
        if (dut.raster_x == 8 && dut.raster_y == 0 && dut.raster_pixel != 0)
            saw_foreground = true;
        tick(dut);
        raster_gap(dut);
    }
    require(saw_foreground, "Graphics I tile did not produce a foreground pixel");
    require(dut.irq_n, "disabled VBlank interrupt asserted");
    write_register(dut, 1, 0x60);
    require(!dut.irq_n, "enabling pending VBlank did not assert interrupt");
    write_register(dut, 1, 0x40);
    require(dut.irq_n, "disabling interrupt did not release line");
    write_register(dut, 1, 0x60);
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
        raster_gap(dut);
    }
    raster_gap(dut);
    require(!dut.irq_n, "next frame did not reassert interrupt");
    dut.reset = 1; tick(dut); dut.reset = 0;
    require(dut.irq_n, "reset did not clear interrupt");
    require(io_read(dut, 0xbf) == 0, "reset did not clear status");
    write_register(dut, 5, 0x36);
    write_vram(dut, 0x1b00, 0xd0);  // no sprites during the VBlank race test
    write_register(dut, 1, 0x60);
    // Start from reset's known scan origin, without forcing raster state.
    // A new frame event on the acknowledgement edge must remain pending;
    // the held read still returns its snapshot from before that edge.
    for (unsigned i = 0; i < 256*192-1; ++i) {
        dut.raster_ce = 1;
        tick(dut);
        raster_gap(dut);
    }
    dut.raster_ce = 1;
    dut.cpu_ce = 1; dut.cpu_iorq_n = 0; dut.cpu_rd_n = 0;
    dut.cpu_a = 0xbf; dut.eval();
    if (dut.cpu_dout != 0) {
        std::cerr << "FES Coleco VDP: status before frame event = 0x"
                  << std::hex << unsigned(dut.cpu_dout) << std::dec << '\n';
        fail("unexpected status before first frame event");
    }
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

    // Graphics II sprite path: the lowest-numbered non-transparent sprite
    // wins a pixel, overlapping sprites latch collision, and a fifth sprite
    // on one line is reported and suppressed. Y is the TMS9918 SAT value,
    // so the visible top line is Y+1.
    dut.reset = 1;
    tick(dut);
    dut.reset = 0;
    write_register(dut, 1, 0x40);  // normal 8x8 sprites, no magnification
    write_register(dut, 5, 0x36);  // SAT at 1b00
    write_register(dut, 6, 0x01);  // sprite patterns at 0800
    write_vram(dut, 0x1b00, 15);   // sprite 0: y=16, x=20, pattern 0
    write_vram(dut, 0x1b01, 20);
    write_vram(dut, 0x1b02, 0);
    write_vram(dut, 0x1b03, 0x01);
    write_vram(dut, 0x1b04, 15);   // sprite 1 is a separately visible color
    write_vram(dut, 0x1b05, 28);
    write_vram(dut, 0x1b06, 0);
    write_vram(dut, 0x1b07, 0x02);
    write_vram(dut, 0x1b08, 15);   // sprite 2 overlaps sprite 0
    write_vram(dut, 0x1b09, 20);
    write_vram(dut, 0x1b0a, 0);
    write_vram(dut, 0x1b0b, 0x00);  // transparent pattern still collides
    write_vram(dut, 0x1b0c, 0xd0);  // terminate the SAT after three sprites
    write_vram(dut, 0x0800, 0x80);
    write_register(dut, 2, 0x0f);     // isolate a blank background table
    write_register(dut, 4, 0x02);     // keep background patterns separate from sprites
    write_register(dut, 3, 0x80);
    write_vram(dut, 0x2000, 0x00); // explicitly transparent background color group
    write_vram(dut, 0x1000, 0x00);
    write_vram(dut, 0x3c42, 0x00);
    write_vram(dut, 0x3c43, 0x00);
    io_write(dut, 0xbf, 0x00);
    io_write(dut, 0xbf, 0x1b);
    for (unsigned sprite = 0; sprite != 3; ++sprite) {
        require(io_read(dut, 0xbe) == 15, "sprite SAT y readback mismatch");
        require(io_read(dut, 0xbe) == (sprite == 1 ? 28 : 20), "sprite SAT x readback mismatch");
        require(io_read(dut, 0xbe) == 0, "sprite SAT pattern readback mismatch");
        require(io_read(dut, 0xbe) == (sprite == 0 ? 1 : sprite == 2 ? 0 : 2),
                "sprite SAT color readback mismatch");
    }

    bool saw_sprite = false;
    bool saw_second_sprite = false;
    for (unsigned i = 0; i != 256 * 18; ++i) {
        dut.raster_ce = 1;
        dut.eval();
        if (dut.raster_x == 20 && dut.raster_y == 16) {
            require(dut.raster_pixel == 1,
                    "sprite priority did not keep sprite zero");
            saw_sprite = true;
        }
        if (dut.raster_x == 28 && dut.raster_y == 16) {
            require(dut.raster_pixel == 2,
                    "registered sprite walker lost a later sprite");
            saw_second_sprite = true;
        }
        tick(dut);
        raster_gap(dut);
    }
    require(saw_sprite, "sprite test did not reach its visible pixel");
    require(saw_second_sprite, "sprite test did not reach its second visible pixel");
    const uint8_t collision_status = io_read(dut, 0xbf);
    require((collision_status & 0x20) != 0,
            "overlapping sprites did not latch collision");

    // In 16x16 mode both low pattern-name bits are ignored. Magnification
    // repeats the top source row vertically and horizontally, while the
    // right edge clips instead of wrapping into x=0.
    dut.reset = 1;
    tick(dut);
    dut.reset = 0;
    write_register(dut, 1, 0x43);  // 16x16 sprites, magnified
    write_register(dut, 2, 0x0f);
    write_register(dut, 4, 0x02);
    write_register(dut, 3, 0x80);
    write_vram(dut, 0x2000, 0x00); // explicitly transparent background color group
    write_register(dut, 5, 0x36);
    write_register(dut, 6, 0x01);
    write_vram(dut, 0x1b00, 40);   // visible top line is y=41
    write_vram(dut, 0x1b01, 255);  // only x=255 remains on screen
    write_vram(dut, 0x1b02, 3);    // low two pattern bits are ignored
    write_vram(dut, 0x1b03, 0x01);
    write_vram(dut, 0x1b04, 40);   // second sprite exposes the right half/row 1
    write_vram(dut, 0x1b05, 100);
    write_vram(dut, 0x1b06, 3);
    write_vram(dut, 0x1b07, 0x02);
    write_vram(dut, 0x1b08, 0xd0);
    write_vram(dut, 0x0800, 0x80); // row 0, left half
    write_vram(dut, 0x0801, 0x80); // row 1, left half
    write_vram(dut, 0x0802, 0x00); // reject the old interleaved row address
    write_vram(dut, 0x0803, 0x00);
    write_vram(dut, 0x0810, 0x01); // row 0, right half at its least-significant bit
    write_vram(dut, 0x0811, 0x00); // row 1, right half
    io_write(dut, 0xbf, 0x10);
    io_write(dut, 0xbf, 0x08);
    require(io_read(dut, 0xbe) == 0x01, "16x16 right-half pattern write did not persist");
    write_vram(dut, 0x3ca0, 0x00); // blank background at x=0, y=41/42
    write_vram(dut, 0x3cbf, 0x00); // blank background at x=255, y=41/42

    bool saw_magnified_sprite = false;
    bool saw_vertical_repeat = false;
    bool saw_right_half = false;
    bool saw_second_source_row = false;
    for (unsigned i = 0; i != 256 * 44; ++i) {
        dut.raster_ce = 1;
        dut.eval();
        if (dut.raster_x == 255 && dut.raster_y == 41) {
            require(dut.raster_pixel == 1,
                    "16x16 magnified sprite did not clip at the right edge");
            saw_magnified_sprite = true;
        }
        if (dut.raster_x == 255 && dut.raster_y == 42) {
            require(dut.raster_pixel == 1,
                    "magnified sprite did not repeat its source row vertically");
            saw_vertical_repeat = true;
        }
        if (dut.raster_x == 131 && dut.raster_y == 41) {
            require(dut.raster_pixel == 2,
                    "16x16 sprite right half used the wrong pattern address");
            saw_right_half = true;
        }
        if (dut.raster_x == 100 && dut.raster_y == 43) {
            require(dut.raster_pixel == 2,
                    "16x16 sprite row 1 used the wrong pattern address");
            saw_second_source_row = true;
        }
        if ((dut.raster_x == 0 && dut.raster_y == 41) ||
            (dut.raster_x == 0 && dut.raster_y == 42))
            require(dut.raster_pixel == 0,
                    "clipped magnified sprite wrapped around the raster");
        tick(dut);
        raster_gap(dut);
    }
    require(saw_magnified_sprite, "16x16 sprite test did not reach its right edge");
    require(saw_vertical_repeat, "16x16 sprite test did not reach its second line");
    require(saw_right_half, "16x16 sprite test did not reach its right half");
    require(saw_second_source_row, "16x16 sprite test did not reach source row 1");

    // Early-clock sprites begin 32 pixels before their SAT x coordinate.
    dut.reset = 1;
    tick(dut);
    dut.reset = 0;
    write_register(dut, 1, 0x40);
    write_register(dut, 2, 0x0f);
    write_register(dut, 4, 0x02);
    write_register(dut, 3, 0x80);
    write_vram(dut, 0x2000, 0x00); // explicitly transparent background color group
    write_register(dut, 5, 0x36);
    write_register(dut, 6, 0x01);
    write_vram(dut, 0x1b00, 60);   // visible top line is y=61
    write_vram(dut, 0x1b01, 40);
    write_vram(dut, 0x1b02, 0);
    write_vram(dut, 0x1b03, 0x81); // color 1 plus early-clock bit
    write_vram(dut, 0x1b04, 0xd0);
    write_vram(dut, 0x0800, 0x80);
    write_vram(dut, 0x3ce1, 0x00); // blank background at x=8, y=61
    write_vram(dut, 0x3ce5, 0x00); // blank background at x=40, y=61

    bool saw_early_clock = false;
    for (unsigned i = 0; i != 256 * 64; ++i) {
        dut.raster_ce = 1;
        dut.eval();
        if (dut.raster_x == 8 && dut.raster_y == 61) {
            require(dut.raster_pixel == 1,
                    "early-clock sprite did not shift left by 32 pixels");
            saw_early_clock = true;
        }
        if (dut.raster_x == 40 && dut.raster_y == 61)
            require(dut.raster_pixel == 0,
                    "early-clock sprite remained at its unshifted x coordinate");
        tick(dut);
        raster_gap(dut);
    }
    require(saw_early_clock, "early-clock sprite test did not reach its visible pixel");

    // E1..FF are signed negative Y positions (except D0, the terminator).
    // F9 therefore starts six lines above the display and reaches source row 6
    // on logical line zero instead of wrapping to the bottom of the screen.
    dut.reset = 1;
    tick(dut);
    dut.reset = 0;
    write_register(dut, 1, 0x40);
    write_register(dut, 2, 0x0f);
    write_register(dut, 4, 0x02);
    write_register(dut, 3, 0x80);
    write_vram(dut, 0x2000, 0x00); // explicitly transparent background color group
    write_register(dut, 5, 0x36);
    write_register(dut, 6, 0x01);
    write_vram(dut, 0x1b00, 0xf9);
    write_vram(dut, 0x1b01, 20);
    write_vram(dut, 0x1b02, 0);
    write_vram(dut, 0x1b03, 0x01);
    write_vram(dut, 0x1b04, 0xd0);
    write_vram(dut, 0x0800 + 6, 0x80);
    write_vram(dut, 0x0800 + 7, 0x80);
    bool saw_negative_y = false;
    unsigned negative_y_line_zero_samples = 0;
    // The registered evaluator can already have sampled the reset-time empty
    // SAT before the CPU finishes its setup writes. Let one frame drain before
    // asserting the first configured line; this models the normal BIOS warm-up
    // and keeps the test independent of setup timing.
    for (unsigned i = 0; i != 256 * 264; ++i) {
        dut.raster_ce = 1;
        dut.eval();
        if (dut.raster_x == 20 && dut.raster_y == 0) {
            ++negative_y_line_zero_samples;
            if (negative_y_line_zero_samples == 2) {
                require(dut.raster_pixel == 1,
                        "negative Y sprite did not enter at the top of the raster");
                saw_negative_y = true;
            }
        }
        tick(dut);
        raster_gap(dut);
    }
    require(saw_negative_y, "negative Y sprite test did not reach line zero");

    dut.reset = 1;
    tick(dut);
    dut.reset = 0;
    write_register(dut, 5, 0x36);
    for (unsigned sprite = 0; sprite != 5; ++sprite) {
        const uint16_t base = uint16_t(0x1b00 + 4 * sprite);
        write_vram(dut, base + 0, 32);
        write_vram(dut, base + 1, uint8_t(8 + 16 * sprite));
        write_vram(dut, base + 2, uint8_t(sprite == 4 ? 1 : 0));
        write_vram(dut, base + 3, 0x01);
    }
    write_vram(dut, 0x1b00 + 20, 0xd0);  // SAT terminator after five entries
    write_vram(dut, 0x0800, 0x00);
    write_vram(dut, 0x0808, 0x80);
    write_vram(dut, 0x0801, 0x00);        // keep the fifth-sprite test background blank
    write_vram(dut, 0x0009, 0x00);
    write_register(dut, 2, 0x0f);          // isolate the background table used below
    write_register(dut, 4, 0x01);
    write_vram(dut, 0x3c89, 0x00);
    io_write(dut, 0xbf, 0x00);
    io_write(dut, 0xbf, 0x1b);
    require(io_read(dut, 0xbe) == 32, "sprite SAT y write did not persist");
    require(io_read(dut, 0xbe) == 8, "sprite SAT x write did not persist");
    require(io_read(dut, 0xbe) == 0, "sprite SAT pattern write did not persist");
    require(io_read(dut, 0xbe) == 1, "sprite SAT color write did not persist");
    require(io_read(dut, 0xbe) == 32, "second sprite SAT y write did not persist");
    require(io_read(dut, 0xbe) == 24, "second sprite SAT x write did not persist");
    require(io_read(dut, 0xbe) == 0, "second sprite SAT pattern write did not persist");
    require(io_read(dut, 0xbe) == 1, "second sprite SAT color write did not persist");
    for (unsigned i = 0; i != 256 * 35; ++i) {
        dut.raster_ce = 1;
        dut.eval();
        if (dut.raster_x == 72 && dut.raster_y == 33)
            require(dut.raster_pixel == 0, "fifth sprite was rendered instead of suppressed");
        tick(dut);
        raster_gap(dut);
    }
    const uint8_t fifth_status = io_read(dut, 0xbf);
    require((fifth_status & 0x40) != 0, "fifth sprite did not set overflow status");
    require((fifth_status & 0x1f) == 4, "fifth sprite index was not reported");

    // The TMS9918 only sets 5S while the frame flag is clear. Four sprites
    // pass through the first frame; adding a fifth after VBlank is pending
    // must not create a new overflow event before the next status read.
    dut.reset = 1;
    tick(dut);
    dut.reset = 0;
    write_register(dut, 1, 0x40);
    write_register(dut, 2, 0x0f);
    write_register(dut, 4, 0x02);
    write_register(dut, 3, 0x80);
    write_vram(dut, 0x2000, 0x00); // explicitly transparent background color group
    write_register(dut, 5, 0x36);
    write_register(dut, 6, 0x01);
    for (unsigned sprite = 0; sprite != 4; ++sprite) {
        const uint16_t base = uint16_t(0x1b00 + 4 * sprite);
        write_vram(dut, base + 0, 32);
        write_vram(dut, base + 1, uint8_t(8 + 16 * sprite));
        write_vram(dut, base + 2, 0);
        write_vram(dut, base + 3, 0x01);
    }
    write_vram(dut, 0x1b10, 0xd0);
    write_vram(dut, 0x0800, 0x80);
    for (unsigned i = 0; i != 256 * 192; ++i) {
        dut.raster_ce = 1;
        dut.eval();
        tick(dut);
        raster_gap(dut);
    }
    // VBlank is now pending. Add the fifth sprite without acknowledging it.
    write_vram(dut, 0x1b10, 32);
    write_vram(dut, 0x1b11, 80);
    write_vram(dut, 0x1b12, 0);
    write_vram(dut, 0x1b13, 0x01);
    write_vram(dut, 0x1b14, 0xd0);
    for (unsigned i = 0; i != 256 * 110; ++i) {
        dut.raster_ce = 1;
        dut.eval();
        tick(dut);
        raster_gap(dut);
    }
    const uint8_t vblank_pending_status = io_read(dut, 0xbf);
    require((vblank_pending_status & 0x60) == 0x00,
            "fifth-sprite status changed while VBlank was pending");
    std::cout << "FES Coleco VDP tile/status path passed\n";
    return EXIT_SUCCESS;
}
