// SPDX-License-Identifier: GPL-2.0-or-later
// Controllable boundaries for simulating the Apple II top.v. Clock outputs
// and the HPS GPO word are driven by the C++ testbench; this is not a PLL,
// timing or hardware model.
/* verilator lint_off DECLFILENAME */
/* verilator lint_off UNUSEDSIGNAL */
module cyclonev_hps_interface_mpu_general_purpose (
    input wire [31:0] gp_in,
    output reg [31:0] gp_out
);
    wire [31:0] observed_gpi /* verilator public_flat_rd */ = gp_in;
    initial gp_out = 32'h00000000;
endmodule

module MISTRAL_IO (
    input wire I,
    input wire OE,
    output wire O,
    inout wire PAD
);
    assign PAD = OE ? I : 1'bz;
    assign O = OE ? I : 1'b1;
endmodule

module cyclonev_hps_interface_peripheral_i2c (
    input wire scl,
    input wire sda,
    output reg out_clk,
    output reg out_data
);
    initial begin
        out_clk = 1'b0;
        out_data = 1'b0;
    end
endmodule

module apple2_system_pll (
    input wire refclk,
    input wire rst,
    output reg outclk_0,
    output wire audio_clk, locked
);
    assign audio_clk = refclk;
    assign locked = 1'b1;
    initial outclk_0 = 1'b0;
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
