// SPDX-License-Identifier: MIT
// Exercise the runtime mailbox and actual D64 store before the rotating medium.
module mailbox_medium_top (
 input wire clk,reset,ref_ce,
 input wire [31:0] gpo,
 output wire [31:0] gpi,
 input wire [7:0] drive_pins,drive_ddr,
 output wire exec_reset,flux,cache_ready,
 output wire [1:0] unit0_state,
 output wire [31:0] unit0_size,
 output wire [6:0] halftrack,
 output wire [21:0] rotation,
 output wire [12:0] debug_byte_addr,
 output wire [7:0] debug_byte
);
 wire [17:0] write_addr,disk_addr;
 wire [15:0] write_data;
 wire [1:0] write_enable;
 wire [7:0] disk_data;
 wire disk_ready=unit0_state==2'd3;
 fes_computer_mailbox #(.ENABLE_C64_DISK(1),.MEDIA_AW(18),
  .UNIT0_MIN(174848),.UNIT0_MAX(174848)) io(
  .clk(clk),.gpo(gpo),.build_id(128'd0),.gpi(gpi),.exec_reset(exec_reset),
  .keyboard_rows(),.controller_buttons(),.mouse_valid(),.mouse_dx(),
  .mouse_dy(),.mouse_buttons(),.mouse_ready(1'b0),
  .media_write_addr(write_addr),.media_write_data(write_data),
  .media_write_enable(write_enable),.media_write_ready(1'b1),
  .media_write_busy(1'b0),.media_changed(1'b0),.media_frozen(),
  .media_read_req(),.media_read_addr(),.media_read_ready(1'b0),
  .media_read_data(8'd0),.unit0_state(unit0_state),.unit0_size(unit0_size));
 c64_disk_store disk(.clk(clk),.write_addr(write_addr),.write_data(write_data),
  .write_enable(write_enable),.read_addr(disk_addr),.read_data(disk_data));
 c64_d64_medium mechanism(.*);
endmodule
