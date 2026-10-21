// SPDX-License-Identifier: MIT
// Actual motherboard with vacant cartridge sockets; the fixture aligns VIC phase.
`include "c64_bus.vh"
module ready_sim_top(input wire clk,reset,output wire cpu_enable,output wire [15:0] debug_pc);
 c64_machine machine(.clk_sys(clk),.reset(reset),.keyboard_rows(144'd0),.controller_buttons(16'd0),
  .media_write_addr(18'd0),.media_write_data(16'd0),.media_write_enable(2'd0),.disk_ready(1'b0),
  .slot1_request(),.slot2_request(),.slot1_response(28'd0),.slot2_response(28'd0),.video_clk(1'b0),
  .red(),.green(),.blue(),.de(),.hsync(),.vsync(),.audio_sample(),.debug_stage(),.debug_error(),.debug_iec(),.debug_pc(debug_pc));
 assign cpu_enable=machine.cpu_ce;
endmodule
