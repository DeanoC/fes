// SPDX-License-Identifier: GPL-3.0-or-later
// Isolated output oracle. Native pulses follow raw CPU phase 2 by one
// fabric edge. Registered palette commits exercise both the pulse itself and
// late phase-1 fabric edges; real CPU admission is independently diagnosed.
module st_shifter_output_sim_top (
    input wire clk_sys, clk_pixel, reset_sys, reset_pixel, hold,
    input wire native_vblank, native_display, native_pixel_ce, native_lookup_ce, pal,
    input wire [8:0] native_line, native_cycle,
    input wire native_short_line,
    input wire [8:0] native_word_loss,
    input wire commit,
    input wire [3:0] commit_index,
    input wire [8:0] commit_color,
    input wire memory_ready,
    input wire [15:0] memory_data,
    output wire memory_req,
    output wire [18:1] memory_addr,
    input wire output_sof,
    input wire [16:0] output_address,
    output wire [8:0] output_rgb,
    input wire [8:0] border_row, border_x,
    input wire border_start, border_ce,
    output wire [8:0] border_rgb,
    output wire pixel, tick, pending, frame_write,
    output wire [8:0] x, row, rgb, token_line, token_cycle,
    output wire [31:0] frames, sequence_number, underruns,
    output wire front_valid, front_pal,
    output wire [8:0] front_height
);
    wire [8:0] unused_border;
    wire unused_raster;
    wire [31:0] unused_skipped;
    reg [143:0] palette;
    always @(posedge clk_sys) begin
        if (reset_sys) begin
            palette <= 144'd0;
            palette[9 +: 9] <= 9'h040;
            palette[18 +: 9] <= 9'h1c7;
        end else if (commit) palette[commit_index * 9 +: 9] <= commit_color;
    end
    st_native_low_video capture (
        .native_short_line(native_short_line), .native_word_loss(native_word_loss),
        .clk_sys(clk_sys), .clk_pixel(clk_pixel), .reset_sys(reset_sys), .reset_pixel(reset_pixel), .hold(hold),
        .native_vblank(native_vblank), .native_display(native_display),
        .native_line(native_line), .native_cycle(native_cycle), .native_pixel_ce(native_pixel_ce), .native_lookup_ce(native_lookup_ce),
        .sync_mode(pal ? 8'd2 : 8'd0), .screen_base(24'h010000), .resolution(2'd0), .palette(palette),
        .memory_req(memory_req), .memory_addr(memory_addr), .memory_ready(memory_ready), .memory_data(memory_data),
        .output_sof(output_sof), .output_address(output_address), .output_rgb(output_rgb), .output_border(unused_border),
        .output_raster_rgb(border_rgb), .output_border_row(border_row), .output_border_x(border_x),
        .output_border_line_start(border_start), .output_border_ce(border_ce), .output_raster_border(unused_raster),
        .output_pal(front_pal), .output_valid(front_valid), .output_height(front_height), .debug_frames(frames), .debug_skipped(unused_skipped), .debug_underruns(underruns)
    );
    assign pixel = capture.write_pixel;
    assign x = capture.output_x;
    assign row = capture.output_row;
    assign rgb = capture.sample_rgb;
    assign tick = capture.output_tick;
    assign token_line = capture.output_token.line_number;
    assign token_cycle = capture.output_token.cycle_number;
    assign pending = capture.output_pending;
    assign frame_write = capture.frame_write;
    assign sequence_number = capture.front_sequence;
endmodule
