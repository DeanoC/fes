// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_video_part.vh"

// Halve RGB on odd full-raster lines. Only CE-qualified SOF/EOL advance state;
// SOF makes its own pixel line zero, even after starting partway through a frame.
// Raster tracking continues during HOLD; picture mute never changes sync timing.
module fes_video_part_scanlines (
    input wire clock,
    input wire [`FES_VIDEO_PART_REQUEST_BITS-1:0] video_request,
    output wire [`FES_VIDEO_PART_RESPONSE_BITS-1:0] video_response
);
    wire ce = video_request[`FES_VIDEO_PART_REQUEST_CE_BIT] &&
              !video_request[`FES_VIDEO_PART_REQUEST_RESERVED_BIT];
    wire sof = video_request[`FES_VIDEO_PART_REQUEST_SOF_BIT];
    wire eol = video_request[`FES_VIDEO_PART_REQUEST_EOL_BIT];
    reg odd_line = 1'b0;
    always @(posedge clock) begin
        if (ce) begin
            if (sof) odd_line <= eol;
            else if (eol) odd_line <= !odd_line;
        end
    end

    wire [23:0] input_rgb = video_request[`FES_VIDEO_PART_REQUEST_RGB_LOW +: `FES_VIDEO_PART_RGB_BITS];
    wire shade = odd_line && !(ce && sof);
    wire [23:0] shaded_rgb = shade ?
        {1'b0, input_rgb[23:17], 1'b0, input_rgb[15:9], 1'b0, input_rgb[7:1]} : input_rgb;
    wire [23:0] rgb = !ce || video_request[`FES_VIDEO_PART_REQUEST_HOLD_BIT] ||
                      !video_request[`FES_VIDEO_PART_REQUEST_DE_BIT] ? 24'd0 : shaded_rgb;
    assign video_response = {
        ce,
        ce && video_request[`FES_VIDEO_PART_REQUEST_VS_BIT],
        ce && video_request[`FES_VIDEO_PART_REQUEST_HS_BIT],
        ce && video_request[`FES_VIDEO_PART_REQUEST_DE_BIT], rgb
    };
endmodule
