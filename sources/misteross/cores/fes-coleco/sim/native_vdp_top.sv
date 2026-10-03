// SPDX-License-Identifier: GPL-2.0-or-later
// Actual OSS registered VDP, production fractional raster enable and native
// source/CDC/consumers. CPU port writes initialize all test VRAM; no RAM peeks.
module native_vdp_top (
    input wire clk_sys, pixel_clk, reset, hold, run_raster,
    input wire cpu_ce, cpu_iorq_n, cpu_rd_n, cpu_wr_n,
    input wire [7:0] cpu_a, cpu_din,
    output wire [7:0] cpu_dout,
    output wire raster_ce,
    output wire [7:0] logical_x,
    output wire [8:0] logical_y,
    output wire [3:0] logical_pixel,
    output wire logical_blank,
    output wire [31:0] source_request, crossed_request,
    output wire [27:0] direct_response, scanlines_response
);
    tms9918_raster_ce #(.SYSTEM_CLOCK_HZ(52_224_000)) timing (
        .clk(clk_sys), .reset(reset || !run_raster), .raster_ce(raster_ce)
    );
    /* verilator lint_off PINCONNECTEMPTY */
    coleco_vdp vdp (
        .clk(clk_sys), .reset(reset), .cpu_ce(cpu_ce),
        .cpu_iorq_n(cpu_iorq_n), .cpu_rd_n(cpu_rd_n), .cpu_wr_n(cpu_wr_n),
        .cpu_a(cpu_a), .cpu_din(cpu_din), .cpu_dout(cpu_dout),
        .raster_ce(raster_ce), .raster_x(logical_x), .raster_y(logical_y),
        .raster_pixel(logical_pixel), .raster_blank(logical_blank),
        .status_collision(), .status_overflow(), .status_fifth_index(), .irq_n()
    );
    /* verilator lint_on PINCONNECTEMPTY */
    coleco_native_video adapter (
        .clk_sys(clk_sys), .hold(hold || reset), .logical_x(logical_x),
        .logical_y(logical_y[7:0]), .logical_pixel(logical_pixel),
        .logical_blank(logical_blank), .request(source_request)
    );
    fes_native_cdc crossing (
        .source_clk(clk_sys), .pixel_clk(pixel_clk),
        .source_request(source_request), .request(crossed_request)
    );
    fes_native_video #(.SCANLINES(0)) direct (
        .clock(pixel_clk), .request(crossed_request), .response(direct_response)
    );
    fes_native_video #(.SCANLINES(1)) scanlines (
        .clock(pixel_clk), .request(crossed_request), .response(scanlines_response)
    );
endmodule
