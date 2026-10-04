// SPDX-License-Identifier: GPL-3.0-or-later
// Independent-clock adapter plus the real shared video part boundaries.
module st_video_adapter_sim_top (
    input wire clk_sys, clk_pixel, reset_sys, reset_pixel, hold,
    input wire [23:0] screen_base,
    input wire [1:0] resolution,
    input wire [143:0] palette,
    output wire video_req,
    output wire [18:1] video_addr,
    input wire video_ready,
    input wire [15:0] video_rdata,
    output wire [31:0] video_request,
    output wire [31:0] debug_underruns, debug_frame,
    output wire [23:0] active_base,
    output wire [1:0] active_resolution,
    output wire [143:0] active_palette,
    output wire fetch_valid, lookup_valid, lookup_bank,
    output wire [8:0] fetch_row, raster_row, raster_next_row, bank0_row, bank1_row,
    output wire [6:0] fetch_column, lookup_column,
    output wire [15:0] renderer_data,
    output wire [1:0] cache_valid, cache_busy,
    output wire [31:0] bank0_frame, bank1_frame,
    output reg [27:0] direct_response = 28'd0,
    output reg [27:0] scanlines_response = 28'd0
);
    st_video_adapter adapter (
        .clk_sys(clk_sys), .clk_pixel(clk_pixel), .reset_sys(reset_sys), .reset_pixel(reset_pixel),
        .hold(hold), .screen_base(screen_base), .resolution(resolution), .palette(palette),
        .video_req(video_req), .video_addr(video_addr), .video_ready(video_ready),
        .video_rdata(video_rdata), .video_request(video_request),
        .debug_underruns(debug_underruns), .debug_frame(debug_frame)
    );
    // Inspection signals are simulation-only. The real public configuration
    // remains the held-bundle adapter inputs, never these hierarchical views.
    assign active_base = adapter.active_base;
    assign active_resolution = adapter.active_resolution;
    assign active_palette = adapter.active_palette;
    assign fetch_valid = adapter.renderer_fetch_valid;
    assign fetch_row = adapter.renderer_row;
    assign fetch_column = adapter.renderer_column;
    assign raster_row = adapter.current_row;
    assign raster_next_row = adapter.next_row;
    assign lookup_valid = adapter.lookup_valid;
    assign lookup_bank = adapter.lookup_bank;
    assign lookup_column = adapter.lookup_column;
    assign renderer_data = adapter.renderer_data;
    assign cache_valid = adapter.cache_valid;
    assign cache_busy = adapter.cache_busy;
    assign bank0_row = adapter.job_row[0];
    assign bank1_row = adapter.job_row[1];
    assign bank0_frame = adapter.job_frame[0];
    assign bank1_frame = adapter.job_frame[1];
    reg [31:0] request_q = 32'd0;
    wire [27:0] direct_result, scanlines_result;
    fes_video_part_direct direct (.video_request(request_q), .video_response(direct_result));
    fes_video_part_scanlines scanlines (
        .clock(clk_pixel), .video_request(request_q), .video_response(scanlines_result)
    );
    always @(posedge clk_pixel) begin
        if (reset_pixel) begin
            request_q <= 32'd0;
            direct_response <= 28'd0;
            scanlines_response <= 28'd0;
        end else begin
            request_q <= video_request;
            direct_response <= direct_result;
            scanlines_response <= scanlines_result;
        end
    end
endmodule
