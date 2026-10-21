// SPDX-License-Identifier: MIT
module medium_top (
 input wire clk, reset, ref_ce, disk_ready,
 input wire [7:0] drive_pins,drive_ddr,
 input wire [17:0] write_addr,
 input wire [15:0] write_data,
 input wire [1:0] write_enable,
 output wire flux,cache_ready,
 output wire [6:0] halftrack,
 output wire [21:0] rotation,
 output wire [12:0] debug_byte_addr,
 output wire [7:0] debug_byte
);
 wire [17:0] disk_addr;wire [7:0] disk_data;
 c64_disk_store disk(.clk(clk),.write_addr(write_addr),.write_data(write_data),
  .write_enable(write_enable),.read_addr(disk_addr),.read_data(disk_data));
 c64_d64_medium mechanism(.*);
endmodule
