// SPDX-License-Identifier: GPL-2.0-or-later
// Eight registered logical slots; CGA slot 2 and MDA slot 3, remaining vacant.
module display_top (
    input wire clk, reset, cycle_valid,
    output wire cycle_ready,
    input wire [19:0] cycle_address,
    input wire [7:0] cycle_wdata,
    input wire [1:0] cycle_kind,
    input wire cycle_write, cycle_aen, cycle_lock,
    input wire [1:0] cycle_dma_channel,
    input wire cycle_terminal_count, guest_ce,
    output wire cycle_done,
    output wire [7:0] cycle_rdata,
    output wire [1:0] cycle_fault,
    output wire [7:0] irq,
    output wire [2:0] drq,
    input wire light_pen_trigger, light_pen_switch,
    output wire [11:0] cga_font_address, mda_font_address,
    input wire [7:0] cga_font_data, mda_font_data,
    output wire [1:0] pixel_ce, de, hs, vs, sof, eol, unsupported_interlace,
    output wire [7:0] rgbi
);
    pcxt_display_bus bus (.*);
endmodule
