// SPDX-License-Identifier: GPL-2.0-or-later
// The hardware DOS profile's ROM and disk upload path, with the real machine
// and SDRAM controller. The board PLLs, pads and HPS primitive are modeled by
// the testbench; the hold/enable wiring matches the DOS branch of the board.
module embedded_boot_top (
    input wire clk, clk_pixel, cold_reset, reset_pixel, execution_hold,
    /* verilator lint_off SYNCASYNCNET */
    output wire initialized, machine_reset, firmware_loaded, disk_loaded,
    /* verilator lint_on SYNCASYNCNET */
    output wire [15:0] firmware_address,
    output wire disk_valid, disk_ready, disk_done,
    output wire [18:0] disk_address,
    output wire [7:0] disk_data,
    output wire [1:0] fault,
    output wire sdram_clk, sdram_cke, sdram_ncs, sdram_nras, sdram_ncas, sdram_nwe,
    output wire [1:0] sdram_ba,
    output wire [12:0] sdram_a,
    output wire sdram_dqml, sdram_dqmh,
    output wire [15:0] dq_out,
    output wire dq_oe,
    input wire [15:0] dq_sample
);
    wire firmware_valid, firmware_ready, firmware_done;
    wire [7:0] firmware_data;
    pcxt_boot #(.ROM_PREFIX("pcxt_glabios")) boot (
        .clk(clk), .cold_reset(cold_reset), .enable(!execution_hold),
        .valid(firmware_valid), .ready(firmware_ready), .done(firmware_done),
        .address(firmware_address), .data(firmware_data));
    /* verilator lint_off PINCONNECTEMPTY */
    pcxt_disk_boot disk_boot (
        .clk(clk), .cold_reset(cold_reset), .enable(firmware_loaded),
        .valid(disk_valid), .ready(disk_ready), .done(disk_done),
        .address(disk_address), .data(disk_data), .finished());
    /* verilator lint_on PINCONNECTEMPTY */
    reg [15:0] dq_rise, dq_fall;
    always @(posedge clk) dq_rise <= dq_sample;
    always @(negedge clk) dq_fall <= dq_sample;
    /* verilator lint_off PINCONNECTEMPTY */
    pcxt_system #(.FLOPPY(1), .DIAGNOSTIC_PROBE(0)) system (
        .clk(clk), .clk_cpu(clk), .clk_pixel(clk_pixel), .clk_pin(clk),
        .cold_reset(cold_reset), .reset_pixel(reset_pixel),
        .warm_reset(execution_hold), .hold(!disk_loaded),
        .media_present(1'b1), .disk_restart(1'b0),
        .disk_load_valid(disk_valid), .disk_load_ready(disk_ready),
        .disk_load_done(disk_done), .disk_load_address(disk_address),
        .disk_load_data(disk_data), .disk_loaded(disk_loaded),
        .firmware_valid(firmware_valid), .firmware_ready(firmware_ready),
        .firmware_done(firmware_done), .firmware_address(firmware_address),
        .firmware_data(firmware_data), .firmware_loaded(firmware_loaded),
        .initialized(initialized), .machine_reset(machine_reset), .fault(fault),
        .keyboard_clock(1'b1), .keyboard_data(1'b0), .keyboard_clock_low(),
        .keyboard_data_low(), .keyboard_clear(), .cga_font_address(), .mda_font_address(),
        .cga_font_data(8'h81), .mda_font_data(8'h81), .halted(), .dma_owner(),
        .refresh_done(), .audio_valid(), .audio_sample(), .video_request(), .video_response(),
        .sdram_clk(sdram_clk), .sdram_cke(sdram_cke), .sdram_ncs(sdram_ncs),
        .sdram_nras(sdram_nras), .sdram_ncas(sdram_ncas), .sdram_nwe(sdram_nwe),
        .sdram_ba(sdram_ba), .sdram_a(sdram_a), .sdram_dqml(sdram_dqml),
        .sdram_dqmh(sdram_dqmh), .dq_out(dq_out), .dq_oe(dq_oe),
        .dq_rise(dq_rise), .dq_fall(dq_fall));
    /* verilator lint_on PINCONNECTEMPTY */
endmodule
