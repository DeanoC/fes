// SPDX-License-Identifier: GPL-2.0-or-later
module raster_ce_tb (
    input  wire clk,
    input  wire reset,
    output wire coleco_ce,
    output wire computer_ce
);
    tms9918_raster_ce #(
        .SYSTEM_CLOCK_HZ(52_224_000)
    ) coleco_timing (
        .clk(clk), .reset(reset), .raster_ce(coleco_ce)
    );

    tms9918_raster_ce #(
        .SYSTEM_CLOCK_HZ(52_000_000)
    ) computer_timing (
        .clk(clk), .reset(reset), .raster_ce(computer_ce)
    );
endmodule
