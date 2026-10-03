// SPDX-License-Identifier: GPL-2.0-or-later
module native_socket_top (
    input wire clock,
    input wire [31:0] request,
    output wire [27:0] response
);
    wire [31:0] plug_request;
    wire [27:0] plug_response;
    coleco_native_video_socket socket (
        .clock(clock), .request(request), .response(response),
        .plug_request(plug_request), .plug_response(plug_response)
    );
    cart part (
        .FPGA_CLK1_50(clock), .plug_addr(plug_request), .plug_rdata(plug_response)
    );
endmodule
