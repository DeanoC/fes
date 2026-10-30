// SPDX-License-Identifier: GPL-2.0-or-later
module system_top #(parameter bit MDA = 0, SCANLINES = 0, HALF_RATE_BOARD = 0) (
    input wire clk, clk_board, clk_cs, clk_pixel, reset_pixel, warm_reset, hold, load_enable,
    // Cold reset asserts the memory reset stretcher asynchronously and is
    // also sampled by firmware/control; the board50 fixtures exercise both phases.
    /* verilator lint_off SYNCASYNCNET */
    input wire cold_reset,
    /* verilator lint_on SYNCASYNCNET */
    output wire initialized, machine_reset, firmware_loaded,
    output wire [15:0] firmware_address,
    output wire [1:0] fault,
    output wire halted, dma_owner, refresh_done,
    output wire [31:0] video_request,
    output wire [27:0] video_response,
    output wire sdram_clk, sdram_cke, sdram_ncs, sdram_nras, sdram_ncas, sdram_nwe,
    output wire [1:0] sdram_ba,
    output wire [12:0] sdram_a,
    output wire sdram_dqml, sdram_dqmh,
    output wire [15:0] dq_out,
    output wire dq_oe,
    input wire [15:0] dq_sample
);
    wire board_clock = HALF_RATE_BOARD ? clk_board : clk;
    wire unused_optional_board_clock = HALF_RATE_BOARD ? 1'b0 : clk_board;
    wire unused_optional_cs_clock = HALF_RATE_BOARD ? 1'b0 : clk_cs;
    reg [15:0] dq_rise, dq_fall;
    always @(posedge board_clock) dq_rise <= dq_sample;
    always @(negedge board_clock) dq_fall <= dq_sample;
    wire firmware_valid, firmware_ready, firmware_done;
    wire [7:0] firmware_data;
    wire controller_ncs;
    generate if (HALF_RATE_BOARD) begin: early_chip_select
        // Match fes_pcxt_top: sample raw controller CS after its board edge,
        // on the separately stepped phase-shifted output clock.
        reg cs;
        always @(posedge clk_cs) cs <= controller_ncs;
        assign sdram_ncs = cs;
    end else begin: common_chip_select
        assign sdram_ncs = controller_ncs;
    end endgenerate
    pcxt_boot boot (.clk(board_clock), .cold_reset(cold_reset), .enable(load_enable),
        .valid(firmware_valid), .ready(firmware_ready), .done(firmware_done),
        .address(firmware_address), .data(firmware_data));
    /* verilator lint_off PINCONNECTEMPTY */
    pcxt_system #(.MDA(MDA), .SCANLINES(SCANLINES), .HALF_RATE_BOARD(HALF_RATE_BOARD),
                  .EXTERNAL_CS_REGISTER(HALF_RATE_BOARD)) system (
        .keyboard_clock(1'b1), .keyboard_data(1'b0),
        .keyboard_clock_low(), .keyboard_data_low(), .keyboard_clear(),
        .cga_font_address(), .mda_font_address(),
        .cga_font_data(8'h81), .mda_font_data(8'h81),
        .clk(board_clock), .clk_cpu(clk), .clk_pin(board_clock), .sdram_ncs(controller_ncs),
        .audio_valid(), .audio_sample(), .*);
    /* verilator lint_on PINCONNECTEMPTY */
endmodule
