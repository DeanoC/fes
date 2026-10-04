// SPDX-License-Identifier: GPL-2.0-or-later
// Backend packed-port adapter. The scaffold imports the 74.25 MHz pixel
// clock onto FPGA_CLK1_50; the native part owns no PLL or source clock.
/* verilator lint_off DECLFILENAME */
module cart (
    input wire FPGA_CLK1_50,
    input wire [31:0] plug_addr,
    output wire [27:0] plug_rdata
);
    fes_native_video #(
`ifdef FES_VIDEO_SCANLINES
        .SCANLINES(1)
`else
        .SCANLINES(0)
`endif
    ) part (
        .clock(FPGA_CLK1_50), .request(plug_addr), .response(plug_rdata)
    );
endmodule
