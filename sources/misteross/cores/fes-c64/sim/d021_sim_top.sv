// SPDX-License-Identifier: MIT
`include "c64_bus.vh"
module d021_sim_top(
 input wire clk,reset,
 output wire active_reset,ce,run_ce,bus_we,show_io,strobe,foreground,border,
 output wire [5:0] phase,native_cycle,
 output wire [8:0] raster,x,
 output wire [15:0] address,pc,
 output wire [7:0] data,d020,d021,
 output wire [3:0] ink,
 output wire frame_valid,publish,
 output wire [16:0] frame_address,
 output wire [3:0] frame_ink
);
 c64_machine machine(
  .clk_sys(clk),.reset(reset),.keyboard_rows(144'd0),.controller_buttons(16'd0),
  .media_write_addr(18'd0),.media_write_data(16'd0),.media_write_enable(2'd0),
  .disk_ready(1'b0),.slot1_response(28'd0),.slot2_response(28'd0),
  .slot1_request(),.slot2_request(),.video_clk(1'b0),.red(),.green(),.blue(),
  .de(),.hsync(),.vsync(),.audio_sample(),.debug_stage(),.debug_error(),.debug_iec(),.debug_pc()
 );
 assign active_reset=machine.system_reset;
 assign ce=machine.cpu_ce;assign run_ce=machine.cpu_run_ce;
 assign phase=machine.cycle_clock;assign address=machine.bus_addr;
 assign data=machine.bus_wdata;assign bus_we=machine.bus_we;assign show_io=machine.show_io_c;
 assign pc=machine.cpu.PC;
 assign raster=machine.vic.raster;assign native_cycle=machine.vic.native_cycle;
 assign x=machine.vic.write_x;assign strobe=machine.vic.pixel_strobe;
 assign foreground=machine.vic.graphics_foreground;
 assign border=machine.vic.main_border||machine.vic.vertical_border;
 assign d020=machine.vic.regs[6'h20];
 assign ink=machine.vic.ink;assign d021=machine.vic.regs[6'h21];
 assign publish=machine.vic.native_frame_start&&machine.vic.frame_started;
 assign frame_valid=machine.vic.frame_write_valid&&!machine.system_reset;
 assign frame_address=machine.vic.frame_write_address;
 assign frame_ink=machine.vic.frame_write_ink;
endmodule
