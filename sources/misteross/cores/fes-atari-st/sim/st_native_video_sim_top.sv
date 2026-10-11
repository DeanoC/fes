// SPDX-License-Identifier: GPL-3.0-or-later
module st_native_video_sim_top (
    input wire clk_sys, clk_pixel, reset_sys, reset_pixel, hold,
    input wire native_vblank, native_display,
    input wire native_short_line, native_blank,
    input wire [8:0] native_word_loss,
    input wire [8:0] native_line, native_cycle,
    input wire native_pixel_ce, native_lookup_ce,
    input wire [7:0] sync_mode,
    input wire [23:0] screen_base,
    input wire [1:0] resolution,
    input wire [143:0] palette,
    output wire video_req,
    output wire [18:1] video_addr,
    input wire video_ready,
    input wire [15:0] video_rdata,
    output wire [31:0] video_request,
    output wire [31:0] captured_frames, skipped_frames, underruns, front_sequence,
    output wire front_valid, write_pixel, front_raster, front_pal, border_overflow,
    output wire [8:0] front_height,
    output wire [1:0] front_bank, write_bank, active_resolution
);
    wire [31:0] unused_debug_frame, unused_debug_underruns;
    // The authored source supplies the next raw phase2 state. Production IO
    // registers counters/DE and palette commits on that edge; capture sees
    // their new values with the registered native pulse on the following edge.
    reg display_q, vblank_q, short_q, blank_q;
    reg [8:0] loss_q;
    reg [8:0] line_q, cycle_q;
    reg [143:0] palette_q;
    always @(posedge clk_sys) begin
        display_q <= native_display;
        short_q <= native_short_line; blank_q <= native_blank; loss_q <= native_word_loss;
        vblank_q <= native_vblank;
        line_q <= native_line;
        cycle_q <= native_cycle;
        palette_q <= palette;
    end
    st_video_adapter adapter (
        .clk_sys(clk_sys), .clk_pixel(clk_pixel), .reset_sys(reset_sys), .reset_pixel(reset_pixel),
        .hold(hold), .native_vblank(vblank_q), .native_display(display_q), .native_short_line(short_q), .native_blank(blank_q), .native_word_loss(loss_q), .native_line(line_q), .native_cycle(cycle_q), .native_pixel_ce(native_pixel_ce), .native_lookup_ce(native_lookup_ce),
        .sync_mode(sync_mode), .screen_base(screen_base), .resolution(resolution), .palette(palette_q),
        .video_req(video_req), .video_addr(video_addr), .video_ready(video_ready), .video_rdata(video_rdata),
        .video_request(video_request), .debug_underruns(unused_debug_underruns), .debug_frame(unused_debug_frame)
    );
    assign captured_frames = adapter.native_capture.capture.debug_frames;
    assign skipped_frames = adapter.native_capture.capture.debug_skipped;
    assign underruns = adapter.native_capture.capture.debug_underruns;
    assign front_sequence = adapter.native_capture.capture.front_sequence;
    assign front_raster = adapter.native_capture.capture.output_raster_border;
    assign front_pal = adapter.native_capture.capture.output_pal;
    assign border_overflow = adapter.native_capture.capture.border_overflow;
    assign front_valid = adapter.native_capture.capture.output_valid;
    assign front_height = adapter.native_capture.capture.output_height;
    assign front_bank = adapter.native_capture.capture.front_bank;
    assign write_bank = adapter.native_capture.capture.write_bank;
    assign write_pixel = adapter.native_capture.capture.write_pixel;
    assign active_resolution = adapter.active_resolution;
endmodule
