// SPDX-License-Identifier: GPL-2.0-or-later
// Inline development output using the production shared Direct/Scanlines RTL.
// The two boundary registers match the existing raster fabric's latency.
module pcxt_video_output #(parameter bit MDA = 0, SCANLINES = 0) (
    input wire clk_sys, clk_pixel, reset_sys, reset_pixel, hold,
    input wire pixel_ce, source_de, source_sof, source_eol,
    input wire [3:0] rgbi,
    output wire [31:0] video_request,
    output reg [27:0] video_response
);
    pcxt_video #(.MDA(MDA)) adapter (.*);
    reg [31:0] request_q;
    wire [27:0] response;
    generate if (SCANLINES) begin: scanlines
        fes_video_part_scanlines part (.clock(clk_pixel), .video_request(request_q), .video_response(response));
    end else begin: direct
        fes_video_part_direct part (.video_request(request_q), .video_response(response));
    end endgenerate
    always @(posedge clk_pixel) begin
        if (reset_pixel) begin request_q <= 0; video_response <= 0; end
        else begin request_q <= video_request; video_response <= response; end
    end
endmodule
