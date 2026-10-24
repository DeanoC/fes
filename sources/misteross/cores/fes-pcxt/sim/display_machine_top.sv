// SPDX-License-Identifier: GPL-2.0-or-later
// Real 8088 + motherboard + registered ISA display cards. Storage/font external.
module display_machine_top (
    input wire clk, reset,
    output wire memory_valid,
    input wire memory_ready, memory_done,
    input wire [7:0] memory_rdata,
    output wire [19:0] address,
    output wire [7:0] wdata,
    output wire write_cycle,
    output wire [1:0] fault,
    output wire guest_ce, halted,
    output wire accepted, completed,
    output wire [1:0] cycle_kind,
    output wire [7:0] cycle_rdata,
    output wire [11:0] cga_font_address, mda_font_address,
    input wire [7:0] cga_font_data, mda_font_data,
    output wire [1:0] pixel_ce, de, hs, vs, sof, eol,
    output wire [7:0] rgbi
);
    /* verilator lint_off PINCONNECTEMPTY */
    pcxt_machine machine (.clk_cpu(clk),.dma_owner(), .refresh_done(), .audio_valid(), .audio_sample(), .*);
    /* verilator lint_on PINCONNECTEMPTY */
endmodule
