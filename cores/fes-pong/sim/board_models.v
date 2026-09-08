// SPDX-License-Identifier: GPL-2.0-or-later
// Controllable boundaries for simulating top.v. The test drives outclk_0
// independently; this is not a 74.25 MHz, PLL-lock, or hardware model.
/* verilator lint_off DECLFILENAME */
/* verilator lint_off UNUSEDSIGNAL */
module cyclonev_hps_interface_mpu_general_purpose (
    input wire [31:0] gp_in,
    output reg [31:0] gp_out
);
    wire [31:0] observed_gpi /* verilator public_flat_rd */ = gp_in;
    initial gp_out = 32'h00000000;
endmodule

module pixel_pll (
    input wire refclk,
    input wire rst,
    output reg outclk_0
);
    initial outclk_0 = 1'b0;
endmodule
/* verilator lint_on UNUSEDSIGNAL */
/* verilator lint_on DECLFILENAME */
