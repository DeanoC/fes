// SPDX-License-Identifier: MIT
// Actual motherboard with vacant cartridge sockets and live controller and
// keyboard inputs, for the CIA1 joystick port-isolation bench.
`include "c64_bus.vh"
module joyports_sim_top(input wire clk,reset,input wire [143:0] keyboard_rows,
 input wire [15:0] controller_buttons,output wire cpu_enable,output wire [15:0] debug_pc);
 c64_machine machine(.clk_sys(clk),.reset(reset),.keyboard_rows(keyboard_rows),
  .controller_buttons(controller_buttons),
  .media_write_addr(18'd0),.media_write_data(16'd0),.media_write_enable(2'd0),.disk_ready(1'b0),
  .slot1_request(),.slot2_request(),.slot1_response(28'd0),.slot2_response(28'd0),.video_clk(1'b0),
  .red(),.green(),.blue(),.de(),.hsync(),.vsync(),.audio_sample(),.debug_stage(),.debug_error(),.debug_iec(),.debug_pc(debug_pc));
 assign cpu_enable=machine.cpu_ce;
endmodule
