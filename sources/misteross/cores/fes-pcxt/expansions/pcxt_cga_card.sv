// SPDX-License-Identifier: GPL-2.0-or-later
module pcxt_cga_card #(parameter integer CLOCK_HZ = 100000000) (
    input wire clk, reset,
    input wire [47:0] request,
    output wire [31:0] response,
    output wire [11:0] font_address,
    input wire [7:0] font_data,
    input wire light_pen_trigger, light_pen_switch,
    output wire pixel_ce, de, hs, vs, sof, eol,
    output wire [3:0] rgbi,
    output wire unsupported_interlace
);
    pcxt_display_card #(.MDA(0), .CLOCK_HZ(CLOCK_HZ)) card (.*);
endmodule
