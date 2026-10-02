// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_video_part.vh"

// Test the physical shell's two registered boundaries around each real part.
module video_part_top (
    input wire clock,
    input wire [`FES_VIDEO_PART_REQUEST_BITS-1:0] source_request,
    output wire [`FES_VIDEO_PART_RESPONSE_BITS-1:0] direct_response,
    output wire [`FES_VIDEO_PART_RESPONSE_BITS-1:0] scanlines_response
);
    wire [`FES_VIDEO_PART_REQUEST_BITS-1:0] direct_request, scanlines_request;
    wire [`FES_VIDEO_PART_RESPONSE_BITS-1:0] direct_result, scanlines_result;
    coleco_video_socket direct_socket (
        .clock(clock), .request(source_request), .response(direct_response),
        .plug_request(direct_request), .plug_response(direct_result)
    );
    coleco_video_socket scanlines_socket (
        .clock(clock), .request(source_request), .response(scanlines_response),
        .plug_request(scanlines_request), .plug_response(scanlines_result)
    );
    fes_video_part_direct direct (.video_request(direct_request), .video_response(direct_result));
    fes_video_part_scanlines scanlines (
        .clock(clock), .video_request(scanlines_request), .video_response(scanlines_result)
    );
endmodule
