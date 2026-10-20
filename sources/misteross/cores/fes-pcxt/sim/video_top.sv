// SPDX-License-Identifier: GPL-2.0-or-later
module video_top #(parameter bit MDA = 0) (
    input wire clk_sys, clk_pixel, reset_sys, reset_pixel, hold,
    input wire pixel_ce, source_de, source_sof, source_eol,
    input wire [3:0] rgbi,
    output wire [31:0] video_request,
    output wire [27:0] direct_response, scanline_response
);
    wire [31:0] scanline_request;
    pcxt_video_output #(.MDA(MDA), .SCANLINES(0)) direct (
        .clk_sys(clk_sys), .clk_pixel(clk_pixel), .reset_sys(reset_sys), .reset_pixel(reset_pixel),
        .hold(hold), .pixel_ce(pixel_ce), .source_de(source_de), .source_sof(source_sof),
        .source_eol(source_eol), .rgbi(rgbi), .video_request(video_request), .video_response(direct_response)
    );
    pcxt_video_output #(.MDA(MDA), .SCANLINES(1)) scanlines (
        .clk_sys(clk_sys), .clk_pixel(clk_pixel), .reset_sys(reset_sys), .reset_pixel(reset_pixel),
        .hold(hold), .pixel_ce(pixel_ce), .source_de(source_de), .source_sof(source_sof),
        .source_eol(source_eol), .rgbi(rgbi), .video_request(scanline_request), .video_response(scanline_response)
    );
    wire unused_request = ^scanline_request;
endmodule
