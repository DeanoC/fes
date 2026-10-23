// SPDX-License-Identifier: MIT
`include "c64_bus.vh"
module ba_timing_sim_top #(
    parameter integer SYS=52224000, CPU=985248
)(
    input wire clk, reset,
    output wire ce, run_ce, raw_ba, cpu_ba, active_reset, write_bus,
    output wire [5:0] phase,
    output wire [15:0] pc,
    output wire [8:0] raster,
    output wire [5:0] native_cycle
);
    c64_machine #(.SYSTEM_CLOCK_HZ(SYS),.CPU_CLOCK_HZ(CPU)) machine (
        .clk_sys(clk),.reset(reset),.keyboard_rows(144'd0),.controller_buttons(16'd0),
        .media_write_addr(18'd0),.media_write_data(16'd0),.media_write_enable(2'd0),
        .disk_ready(1'b0),.slot1_response(28'd0),.slot2_response(28'd0),
        .slot1_request(),.slot2_request(),.video_clk(1'b0),.red(),.green(),.blue(),
        .de(),.hsync(),.vsync(),.audio_sample(),.debug_stage(),.debug_error(),
        .debug_iec(),.debug_pc()
    );
    assign ce=machine.cpu_ce;
    assign run_ce=machine.cpu_run_ce;
    assign raw_ba=machine.vic_ba_n;
    assign cpu_ba=machine.cpu_ba_n;
    assign active_reset=machine.system_reset;
    assign write_bus=machine.bus_we;
    assign phase=machine.cycle_clock;
    assign pc=machine.debug_pc;
    assign raster=machine.vic.raster;
    assign native_cycle=machine.vic.native_cycle;
endmodule
