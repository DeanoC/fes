// SPDX-License-Identifier: GPL-2.0-or-later
// Simulation-only shell: two registered boundaries surround the real parts.
module st_video_sim_top (
    input wire clk, input wire reset, input wire hold,
    input wire [23:0] screen_base, input wire [1:0] resolution,
    input wire [143:0] palette, input wire [15:0] mem_data,
    output wire [18:1] mem_addr, output wire [31:0] source_request,
    output wire fetch_valid, output wire [8:0] fetch_row,
    output wire [6:0] fetch_column,
    output reg [27:0] direct_response = 28'd0,
    output reg [27:0] scanlines_response = 28'd0
);
    reg [31:0] request_q = 32'd0;
    wire [27:0] direct_result, scanlines_result;
    st_video video (
        .clk(clk), .reset(reset), .hold(hold), .screen_base(screen_base),
        .resolution(resolution), .palette(palette), .mem_data(mem_data),
        .mem_addr(mem_addr), .fetch_valid(fetch_valid), .fetch_row(fetch_row),
        .fetch_column(fetch_column), .video_request(source_request)
    );
    fes_video_part_direct direct (
        .video_request(request_q), .video_response(direct_result)
    );
    fes_video_part_scanlines scanlines (
        .clock(clk), .video_request(request_q), .video_response(scanlines_result)
    );
    always @(posedge clk) begin
        request_q <= source_request;
        direct_response <= direct_result;
        scanlines_response <= scanlines_result;
    end
endmodule
