// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_video_part.vh"

// Timed RGB888 passthrough. The shell registers the whole request and response;
// this part adds no latency or clock/pin ownership of its own.
module fes_video_part_direct (
    // Direct does not need the SOF/EOL tracking bits.
    /* verilator lint_off UNUSEDSIGNAL */
    input wire [`FES_VIDEO_PART_REQUEST_BITS-1:0] video_request,
    /* verilator lint_on UNUSEDSIGNAL */
    output wire [`FES_VIDEO_PART_RESPONSE_BITS-1:0] video_response
);
    // The required-zero reserved bit also keeps every output driven by logic
    // when the part is synthesized independently of its shell socket.
    wire valid = video_request[`FES_VIDEO_PART_REQUEST_CE_BIT] &&
                 !video_request[`FES_VIDEO_PART_REQUEST_RESERVED_BIT];
    wire [23:0] rgb = !valid || video_request[`FES_VIDEO_PART_REQUEST_HOLD_BIT] ||
                      !video_request[`FES_VIDEO_PART_REQUEST_DE_BIT] ? 24'd0 :
                      video_request[`FES_VIDEO_PART_REQUEST_RGB_LOW +: `FES_VIDEO_PART_RGB_BITS];
    assign video_response = {
        valid,
        valid && video_request[`FES_VIDEO_PART_REQUEST_VS_BIT],
        valid && video_request[`FES_VIDEO_PART_REQUEST_HS_BIT],
        valid && video_request[`FES_VIDEO_PART_REQUEST_DE_BIT], rgb
    };
endmodule
