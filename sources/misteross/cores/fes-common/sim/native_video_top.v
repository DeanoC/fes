// SPDX-License-Identifier: GPL-2.0-or-later
// Exercise the public request boundary, then the real source adapter and CDC.
module native_video_top (
    input wire pixel_clock,
    input wire source_clock,
    input wire [1:0] source_mode,
    input wire [31:0] native_request,
    input wire [31:0] source_request,
    input wire source_hold,
    input wire [7:0] logical_x,
    input wire [7:0] logical_y,
    input wire [3:0] logical_pixel,
    input wire logical_blank,
    output wire [31:0] adapter_request,
    output wire [31:0] crossed_request,
    output wire [27:0] direct_response,
    output wire [27:0] scanlines_response
);
    coleco_native_video adapter (
        .clk_sys(source_clock), .hold(source_hold), .logical_x(logical_x),
        .logical_y(logical_y), .logical_pixel(logical_pixel), .logical_blank(logical_blank),
        .request(adapter_request)
    );
    fes_native_cdc crossing (
        .source_clk(source_clock), .pixel_clk(pixel_clock),
        .source_request(source_mode == 2 ? adapter_request : source_request),
        .request(crossed_request)
    );
    wire [31:0] request = source_mode == 0 ? native_request : crossed_request;
    fes_native_video #(.SCANLINES(0)) direct (
        .clock(pixel_clock), .request(request), .response(direct_response)
    );
    fes_native_video #(.SCANLINES(1)) scanlines (
        .clock(pixel_clock), .request(request), .response(scanlines_response)
    );
endmodule
