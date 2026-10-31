// SPDX-License-Identifier: GPL-2.0-or-later
module memory_top (
    input wire clk, cold_reset, reset,
    output wire initialized,
    input wire memory_valid,
    output wire memory_ready,
    input wire [19:0] address,
    input wire [7:0] wdata,
    input wire write_cycle,
    output wire memory_done,
    output wire [7:0] memory_rdata,
    input wire firmware_valid,
    output wire firmware_ready,
    input wire [15:0] firmware_address,
    input wire [7:0] firmware_data,
    output wire firmware_done,
    input wire disk_load_valid,
    output wire disk_load_ready,
    input wire [18:0] disk_load_address,
    input wire [7:0] disk_load_data,
    output wire disk_load_done,
    input wire media_read_valid,
    input wire [18:0] media_read_addr,
    output wire media_read_ready,
    output wire [7:0] media_read_data,
    output wire sdram_clk, sdram_cke, sdram_ncs, sdram_nras, sdram_ncas, sdram_nwe,
    output wire [1:0] sdram_ba,
    output wire [12:0] sdram_a,
    output wire sdram_dqml, sdram_dqmh,
    output wire [15:0] dq_out,
    output wire dq_oe,
    input wire [15:0] dq_sample
);
    reg [15:0] dq_rise, dq_fall;
    always @(posedge clk) dq_rise <= dq_sample;
    always @(negedge clk) dq_fall <= dq_sample;
    pcxt_memory memory (.clk_pin(clk), .*);
endmodule

// Only the clock pin is needed; the stand-in retains high/low DDR values.
/* verilator lint_off DECLFILENAME */
/* verilator lint_off UNUSEDSIGNAL */
/* verilator lint_off UNUSEDPARAM */
module altddio_out #(
    parameter width = 1,
    parameter intended_device_family = "Cyclone V",
    parameter power_up_high = "OFF",
    parameter oe_reg = "UNREGISTERED",
    parameter extend_oe_disable = "OFF",
    parameter invert_output = "OFF"
) (
    input wire [width-1:0] datain_h, datain_l,
    input wire outclock, outclocken, aset, aclr, sset, sclr, oe,
    output wire [width-1:0] dataout,
    output wire oe_out
);
    reg [width-1:0] high_data = 0, low_data = 0;
    always @(posedge outclock) if (outclocken) high_data <= datain_h;
    always @(negedge outclock) if (outclocken) low_data <= datain_l;
    assign dataout = outclock ? high_data : low_data;
    assign oe_out = 1'b0;
endmodule
/* verilator lint_on UNUSEDPARAM */
/* verilator lint_on UNUSEDSIGNAL */
/* verilator lint_on DECLFILENAME */
