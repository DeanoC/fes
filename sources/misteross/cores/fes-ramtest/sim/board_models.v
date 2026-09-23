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

// Fabric stand-in for the IO-cell DDR clock. High half is datain_h.
module altddio_out #(
    parameter width = 1,
    parameter intended_device_family = "Cyclone V",
    parameter power_up_high = "OFF",
    parameter oe_reg = "UNREGISTERED",
    parameter extend_oe_disable = "OFF",
    parameter invert_output = "OFF"
) (
    input wire datain_h,
    input wire datain_l,
    input wire outclock,
    input wire outclocken,
    input wire aset,
    input wire aclr,
    input wire sset,
    input wire sclr,
    input wire oe,
    output wire dataout,
    output wire oe_out
);
    reg q_h = 1'b0;
    reg q_l = 1'b0;
    always @(posedge outclock)
        if (outclocken)
            q_h <= datain_h;
    always @(negedge outclock)
        if (outclocken)
            q_l <= datain_l;
    assign dataout = outclock ? q_h : q_l;
    assign oe_out = 1'b0;
endmodule

// Fabric stand-in for the IO-cell DDR input. High half is the rising edge.
module altddio_in #(
    parameter width = 1,
    parameter intended_device_family = "Cyclone V",
    parameter power_up_high = "OFF",
    parameter invert_input_clocks = "OFF"
) (
    input wire datain,
    input wire inclock,
    input wire inclocken,
    input wire aset,
    input wire aclr,
    input wire sset,
    input wire sclr,
    output reg dataout_h,
    output reg dataout_l
);
    initial begin
        dataout_h = 1'b0;
        dataout_l = 1'b0;
    end
    always @(posedge inclock)
        if (inclocken)
            dataout_h <= datain;
    always @(negedge inclock)
        if (inclocken)
            dataout_l <= datain;
endmodule
/* verilator lint_on UNUSEDSIGNAL */
/* verilator lint_on DECLFILENAME */
