// SPDX-License-Identifier: MIT
// Two VGAs on the same inputs: pc386_vga_ref (the base commit's RTL, renamed
// by run.sh) and pc386_vga (this branch). diff names the first differing
// output group; the testbench fails on any difference in any clock.
module vga_diff_top #(
    parameter integer DOT0_N = 1, DOT0_D = 3, DOT1_N = 2, DOT1_D = 5
) (
    input wire clk, reset,
    input wire io_valid, io_write,
    input wire [15:0] io_port,
    input wire [7:0] io_wdata,
    input wire mem_valid, mem_write,
    input wire [16:2] mem_address,
    input wire [3:0] mem_byteenable,
    input wire [31:0] mem_wdata,
    input wire p1_waitrequest,
    input wire [63:0] p1_readdata,
    input wire p1_readdatavalid,
    input wire cap_waitrequest,
    input wire [1:0] present_buffer,
    // The reference's outputs, which the testbench answers
    output wire io_done, mem_ready, mem_rvalid,
    output wire [28:0] p1_address,
    output wire [7:0] p1_burstcount,
    output wire p1_read, p1_write,
    output wire [63:0] p1_writedata,
    output wire [7:0] p1_byteenable,
    output wire cap_write,
    output wire [28:0] cap_address,
    output wire [7:0] cap_burstcount,
    output wire [63:0] cap_writedata,
    output wire [15:0] frame_count,
    output wire [15:0] frame_sequence,
    output reg [4:0] diff,
    // The new side's capture port, for the failure report
    output wire [28:0] new_cap_address,
    output wire [7:0] new_cap_burstcount,
    output wire new_cap_write,
    output wire [63:0] new_cap_writedata
);
    wire [7:0] a_io_rdata, b_io_rdata;
    wire [31:0] a_mem_rdata, b_mem_rdata;
    // pc386_vga_ref drives the exported ports; pc386_vga drives b.
`define VGA_PORTS \
        .clk(clk), .reset(reset), .io_valid(io_valid), .io_write(io_write), .io_port(io_port), \
        .io_wdata(io_wdata), .mem_valid(mem_valid), .mem_write(mem_write), .mem_address(mem_address), \
        .mem_byteenable(mem_byteenable), .mem_wdata(mem_wdata), .p1_waitrequest(p1_waitrequest), \
        .p1_readdata(p1_readdata), .p1_readdatavalid(p1_readdatavalid), .cap_waitrequest(cap_waitrequest), \
        .present_buffer(present_buffer)
    wire a_io_done, a_mem_ready, a_mem_rvalid, a_p1_read, a_p1_write, a_cap_write;
    wire [28:0] a_p1_address, a_cap_address;
    wire [7:0] a_p1_burstcount, a_p1_byteenable, a_cap_burstcount;
    wire [63:0] a_p1_writedata, a_cap_writedata;
    wire [1:0] a_frame_buffer;
    wire [9:0] a_frame_width, a_frame_height;
    wire [15:0] a_frame_sequence, a_frame_count;
    wire a_frame_start, a_frame_done, a_start_latched;
    wire [31:0] a_underflows, a_drops;
    pc386_vga_ref #(.DOT0_N(DOT0_N), .DOT0_D(DOT0_D), .DOT1_N(DOT1_N), .DOT1_D(DOT1_D)) ref_vga (
        `VGA_PORTS,
        .io_done(a_io_done), .io_rdata(a_io_rdata), .mem_ready(a_mem_ready), .mem_rvalid(a_mem_rvalid),
        .mem_rdata(a_mem_rdata), .p1_address(a_p1_address), .p1_burstcount(a_p1_burstcount), .p1_read(a_p1_read),
        .p1_write(a_p1_write), .p1_writedata(a_p1_writedata), .p1_byteenable(a_p1_byteenable),
        .cap_address(a_cap_address), .cap_burstcount(a_cap_burstcount), .cap_write(a_cap_write),
        .cap_writedata(a_cap_writedata), .frame_buffer(a_frame_buffer), .frame_width(a_frame_width),
        .frame_height(a_frame_height), .frame_sequence(a_frame_sequence), .frame_start(a_frame_start),
        .frame_done(a_frame_done), .start_latched(a_start_latched), .frame_count(a_frame_count),
        .scanout_underflows(a_underflows), .capture_drops(a_drops));
    wire b_io_done, b_mem_ready, b_mem_rvalid, b_p1_read, b_p1_write, b_cap_write;
    wire [28:0] b_p1_address, b_cap_address;
    wire [7:0] b_p1_burstcount, b_p1_byteenable, b_cap_burstcount;
    wire [63:0] b_p1_writedata, b_cap_writedata;
    wire [1:0] b_frame_buffer;
    wire [9:0] b_frame_width, b_frame_height;
    wire [15:0] b_frame_sequence, b_frame_count;
    wire b_frame_start, b_frame_done, b_start_latched;
    wire [31:0] b_underflows, b_drops;
    // The netlist is elaborated at the RTL's default rates, which run.sh then
    // gives the top: the locked Yosys asserts on -chparam at this top.
`ifdef VGA_DIFF_NETLIST
    pc386_vga new_vga (
`else
    pc386_vga #(.DOT0_N(DOT0_N), .DOT0_D(DOT0_D), .DOT1_N(DOT1_N), .DOT1_D(DOT1_D)) new_vga (
`endif
        `VGA_PORTS,
        .io_done(b_io_done), .io_rdata(b_io_rdata), .mem_ready(b_mem_ready), .mem_rvalid(b_mem_rvalid),
        .mem_rdata(b_mem_rdata), .p1_address(b_p1_address), .p1_burstcount(b_p1_burstcount), .p1_read(b_p1_read),
        .p1_write(b_p1_write), .p1_writedata(b_p1_writedata), .p1_byteenable(b_p1_byteenable),
        .cap_address(b_cap_address), .cap_burstcount(b_cap_burstcount), .cap_write(b_cap_write),
        .cap_writedata(b_cap_writedata), .frame_buffer(b_frame_buffer), .frame_width(b_frame_width),
        .frame_height(b_frame_height), .frame_sequence(b_frame_sequence), .frame_start(b_frame_start),
        .frame_done(b_frame_done), .start_latched(b_start_latched), .frame_count(b_frame_count),
        .scanout_underflows(b_underflows), .capture_drops(b_drops));
    // Every output, every clock, including io_rdata and mem_rdata between
    // their strobes.
    always @* begin
        diff = 0;
        if (a_io_done != b_io_done || a_io_rdata != b_io_rdata) diff = 1;
        else if (a_mem_ready != b_mem_ready || a_mem_rvalid != b_mem_rvalid) diff = 2;
        else if (a_mem_rdata != b_mem_rdata) diff = 3;
        else if (a_p1_read != b_p1_read || a_p1_write != b_p1_write) diff = 4;
        else if (a_p1_address != b_p1_address || a_p1_burstcount != b_p1_burstcount) diff = 5;
        else if (a_p1_writedata != b_p1_writedata || a_p1_byteenable != b_p1_byteenable) diff = 6;
        else if (a_cap_write != b_cap_write) diff = 7;
        else if (a_cap_address != b_cap_address || a_cap_burstcount != b_cap_burstcount ||
                 a_cap_writedata != b_cap_writedata) diff = 8;
        else if (a_frame_buffer != b_frame_buffer || a_frame_width != b_frame_width ||
                 a_frame_height != b_frame_height || a_frame_sequence != b_frame_sequence) diff = 9;
        else if (a_frame_start != b_frame_start || a_frame_done != b_frame_done ||
                 a_start_latched != b_start_latched || a_frame_count != b_frame_count) diff = 10;
        else if (a_underflows != b_underflows || a_drops != b_drops) diff = 11;
    end
    assign new_cap_address = b_cap_address; assign new_cap_burstcount = b_cap_burstcount;
    assign new_cap_write = b_cap_write; assign new_cap_writedata = b_cap_writedata;
    assign io_done = a_io_done; assign mem_ready = a_mem_ready; assign mem_rvalid = a_mem_rvalid;
    assign p1_address = a_p1_address; assign p1_burstcount = a_p1_burstcount; assign p1_read = a_p1_read;
    assign p1_write = a_p1_write; assign p1_writedata = a_p1_writedata; assign p1_byteenable = a_p1_byteenable;
    assign cap_write = a_cap_write; assign cap_address = a_cap_address; assign cap_burstcount = a_cap_burstcount;
    assign cap_writedata = a_cap_writedata; assign frame_count = a_frame_count; assign frame_sequence = a_frame_sequence;
endmodule
