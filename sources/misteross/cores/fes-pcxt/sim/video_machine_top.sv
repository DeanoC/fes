// SPDX-License-Identifier: GPL-2.0-or-later
// End-to-end 8088/ISA-card -> shared 720p Direct and Scanlines fixture.
module video_machine_top (
    input wire clk, reset, clk_pixel, reset_pixel, hold,
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
    output wire [7:0] rgbi,
    output wire [31:0] cga_video_request, mda_video_request,
    output wire [27:0] cga_video_response, mda_video_response
);
    display_machine_top machine (.*);
    pcxt_video_output #(.MDA(0), .SCANLINES(0)) cga_output (
        .clk_sys(clk), .clk_pixel(clk_pixel), .reset_sys(reset), .reset_pixel(reset_pixel),
        .hold(hold), .pixel_ce(pixel_ce[0]), .source_de(de[0]), .source_sof(sof[0]),
        .source_eol(eol[0]), .rgbi(rgbi[3:0]), .video_request(cga_video_request), .video_response(cga_video_response)
    );
    pcxt_video_output #(.MDA(1), .SCANLINES(1)) mda_output (
        .clk_sys(clk), .clk_pixel(clk_pixel), .reset_sys(reset), .reset_pixel(reset_pixel),
        .hold(hold), .pixel_ce(pixel_ce[1]), .source_de(de[1]), .source_sof(sof[1]),
        .source_eol(eol[1]), .rgbi(rgbi[7:4]), .video_request(mda_video_request), .video_response(mda_video_response)
    );
endmodule
