// SPDX-License-Identifier: GPL-2.0-or-later
module system_top #(parameter bit MDA = 0, SCANLINES = 0) (
    input wire clk, clk_pixel, cold_reset, reset_pixel, warm_reset, hold, load_enable,
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
    reg [15:0] dq_rise, dq_fall;
    always @(posedge clk) dq_rise <= dq_sample;
    always @(negedge clk) dq_fall <= dq_sample;
    wire firmware_valid, firmware_ready, firmware_done;
    wire [7:0] firmware_data;
    pcxt_boot boot (.clk(clk), .cold_reset(cold_reset), .enable(load_enable),
        .valid(firmware_valid), .ready(firmware_ready), .done(firmware_done),
        .address(firmware_address), .data(firmware_data));
    /* verilator lint_off PINCONNECTEMPTY */
    pcxt_system #(.MDA(MDA), .SCANLINES(SCANLINES)) system (
        .clk_rom(~clk), .clk_pin(clk), .audio_valid(), .audio_sample(), .*);
    /* verilator lint_on PINCONNECTEMPTY */
endmodule
