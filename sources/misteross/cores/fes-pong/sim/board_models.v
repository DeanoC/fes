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

// Digital wired-AND boundary: external_low models another device pulling the
// pad low; released lines have a pull-up. Drive intent is exposed separately
// so the test rejects active-high output as well as incorrect pad feedback.
module MISTRAL_IO (
    input wire I,
    input wire OE,
    output wire O,
    inout wire PAD
);
    reg external_low /* verilator public_flat_rw */ = 1'b0;
    wire drive_low /* verilator public_flat_rd */ = OE && !I;
    wire drive_high /* verilator public_flat_rd */ = OE && I;
    assign PAD = OE ? I : 1'bz;
    assign O = (OE ? I : 1'b1) && !external_low;
endmodule

module cyclonev_hps_interface_peripheral_i2c (
    input wire scl,
    input wire sda,
    output reg out_clk,
    output reg out_data
);
    wire observed_scl /* verilator public_flat_rd */ = scl;
    wire observed_sda /* verilator public_flat_rd */ = sda;
    initial begin
        out_clk = 1'b0;
        out_data = 1'b0;
    end
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
