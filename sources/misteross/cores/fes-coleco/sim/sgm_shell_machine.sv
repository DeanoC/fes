// SPDX-License-Identifier: GPL-2.0-or-later
// CPU/socket harness: the C++ driver supplies only v2 RAM claims.
module sgm_shell_machine (
    input wire clk_sys,
    input wire reset,
    input wire media_ready,
    input wire [15:0] media_size,
    input wire [7:0] media_data,
    output wire [14:0] media_addr,
    input wire [15:0] peek_addr,
    output wire [7:0] peek_data,
    output wire cpu_halt_n,
    output wire [30:0] plug_request,
`ifndef SGM_INTEGRATED
    input wire [27:0] plug_response
`else
    output wire [27:0] plug_response
`endif
);
    wire [30:0] bus_request;
    wire [27:0] bus_response;
    coleco_expansion_socket_v2 socket (
        .clock(clk_sys), .request(bus_request), .response(bus_response),
        .plug_request(plug_request), .plug_response(plug_response)
    );
`ifdef SGM_INTEGRATED
    cart sgm (
        .FPGA_CLK1_50(clk_sys), .plug_addr(plug_request),
        .plug_rdata(plug_response)
    );
`endif
    coleco_machine machine (
        .clk_sys(clk_sys), .reset(reset),
        .controller_buttons(16'b0), .controller_keypad(24'b0),
        .media_ready(media_ready), .media_size(media_size),
        .media_data(media_data), .media_addr(media_addr),
        .peek_addr(peek_addr), .peek_data(peek_data),
        .cpu_halt_n(cpu_halt_n),
        .firmware_we_a(1'b0), .firmware_we_b(1'b0),
        .firmware_addr(13'b0), .firmware_data(16'b0),
        .bus_request(bus_request), .bus_response(bus_response),
        .controller1_value(), .controller2_value(),
        .logical_x(), .logical_y(), .logical_pixel(),
        .logical_blank(), .vdp_status(), .cpu_addr_debug(),
        .audio_sample()
    );
endmodule
