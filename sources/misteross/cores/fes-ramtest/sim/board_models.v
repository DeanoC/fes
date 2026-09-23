// SPDX-License-Identifier: GPL-2.0-or-later
// Simulation stand-ins. The PLL passes the board clock through; this is not a
// 74.25 MHz model. The mailbox register is writable so the test can issue
// fes.application identity requests.
/* verilator lint_off DECLFILENAME */
/* verilator lint_off UNUSEDSIGNAL */
module cyclonev_hps_interface_mpu_general_purpose (
    input wire [31:0] gp_in,
    output wire [31:0] gp_out
); /* verilator public_module */
    reg [31:0] gpo /* verilator public_flat_rw */;
    wire [31:0] observed /* verilator public_flat_rd */ = gp_in;
    initial gpo = 32'h00000000;
    assign gp_out = gpo;
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

module pixel_pll (
    input wire refclk,
    input wire rst,
    output wire outclk_0
);
    assign outclk_0 = refclk;
endmodule
/* verilator lint_on UNUSEDSIGNAL */
/* verilator lint_on DECLFILENAME */
