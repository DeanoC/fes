// SPDX-License-Identifier: GPL-2.0-or-later
// Adapter to the current compiler's packed freeze-scaffold ports. The fabric
// video contract is independent of these backend-specific port names.
module cart (
    input wire FPGA_CLK1_50,
    input wire [31:0] plug_addr,
    output wire [27:0] plug_rdata
);
`ifdef FES_VIDEO_SCANLINES
    fes_video_part_scanlines part (
        .clock(FPGA_CLK1_50), .video_request(plug_addr), .video_response(plug_rdata)
    );
`else
    fes_video_part_direct part (
        .video_request(plug_addr), .video_response(plug_rdata)
    );
`endif
endmodule
