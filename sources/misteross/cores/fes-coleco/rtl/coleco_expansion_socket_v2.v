// SPDX-License-Identifier: GPL-2.0-or-later
// Separate registered v2 boundary. The v1 socket and its sealed artifacts are unchanged.
`include "coleco_bus_v2_pack.vh"
module coleco_expansion_socket_v2 (
    input wire clock,
    input wire [`COLECO_V2_BUS_REQ-1:0] request,
    output wire [`COLECO_V2_BUS_RSP-1:0] response,
    output wire [`COLECO_V2_BUS_REQ-1:0] plug_request,
    input wire [`COLECO_V2_BUS_RSP-1:0] plug_response
);
`ifdef VERILATOR
    reg [`COLECO_V2_BUS_REQ-1:0] request_q = 0;
    reg [`COLECO_V2_BUS_RSP-1:0] response_q = 0;
    always @(posedge clock) begin
        request_q <= request;
        response_q <= plug_response;
    end
    assign plug_request = request_q;
    assign response = response_q;
`else
`define COLECO_V2_SOCKET_FF(NAME, SITE, D, QOUT) \
    (* keep, BEL = SITE *) MISTRAL_FF NAME ( \
        .CLK(clock), .DATAIN(D), .Q(QOUT), .ACLR(1'b1), .ENA(1'b1), \
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    `COLECO_V2_SOCKET_FF(plug_request_ff_0, "MISTRAL_FF.24.1.2", request[0], plug_request[0])
    `COLECO_V2_SOCKET_FF(plug_request_ff_1, "MISTRAL_FF.24.1.4", request[1], plug_request[1])
    `COLECO_V2_SOCKET_FF(plug_request_ff_2, "MISTRAL_FF.24.1.8", request[2], plug_request[2])
    `COLECO_V2_SOCKET_FF(plug_request_ff_3, "MISTRAL_FF.24.1.10", request[3], plug_request[3])
    `COLECO_V2_SOCKET_FF(plug_request_ff_4, "MISTRAL_FF.24.1.14", request[4], plug_request[4])
    `COLECO_V2_SOCKET_FF(plug_request_ff_5, "MISTRAL_FF.24.1.16", request[5], plug_request[5])
    `COLECO_V2_SOCKET_FF(plug_request_ff_6, "MISTRAL_FF.24.1.20", request[6], plug_request[6])
    `COLECO_V2_SOCKET_FF(plug_request_ff_7, "MISTRAL_FF.24.1.22", request[7], plug_request[7])
    `COLECO_V2_SOCKET_FF(plug_request_ff_8, "MISTRAL_FF.24.1.26", request[8], plug_request[8])
    `COLECO_V2_SOCKET_FF(plug_request_ff_9, "MISTRAL_FF.24.1.28", request[9], plug_request[9])
    `COLECO_V2_SOCKET_FF(plug_request_ff_10, "MISTRAL_FF.24.1.32", request[10], plug_request[10])
    `COLECO_V2_SOCKET_FF(plug_request_ff_11, "MISTRAL_FF.24.1.34", request[11], plug_request[11])
    `COLECO_V2_SOCKET_FF(plug_request_ff_12, "MISTRAL_FF.24.1.38", request[12], plug_request[12])
    `COLECO_V2_SOCKET_FF(plug_request_ff_13, "MISTRAL_FF.24.1.40", request[13], plug_request[13])
    `COLECO_V2_SOCKET_FF(plug_request_ff_14, "MISTRAL_FF.24.1.44", request[14], plug_request[14])
    `COLECO_V2_SOCKET_FF(plug_request_ff_15, "MISTRAL_FF.24.1.46", request[15], plug_request[15])
    `COLECO_V2_SOCKET_FF(plug_request_ff_16, "MISTRAL_FF.24.1.50", request[16], plug_request[16])
    `COLECO_V2_SOCKET_FF(plug_request_ff_17, "MISTRAL_FF.24.1.52", request[17], plug_request[17])
    `COLECO_V2_SOCKET_FF(plug_request_ff_18, "MISTRAL_FF.24.1.56", request[18], plug_request[18])
    `COLECO_V2_SOCKET_FF(plug_request_ff_19, "MISTRAL_FF.24.1.58", request[19], plug_request[19])
    `COLECO_V2_SOCKET_FF(plug_request_ff_20, "MISTRAL_FF.24.2.2", request[20], plug_request[20])
    `COLECO_V2_SOCKET_FF(plug_request_ff_21, "MISTRAL_FF.24.2.4", request[21], plug_request[21])
    `COLECO_V2_SOCKET_FF(plug_request_ff_22, "MISTRAL_FF.24.2.8", request[22], plug_request[22])
    `COLECO_V2_SOCKET_FF(plug_request_ff_23, "MISTRAL_FF.24.3.10", request[23], plug_request[23])
    `COLECO_V2_SOCKET_FF(plug_request_ff_24, "MISTRAL_FF.24.2.14", request[24], plug_request[24])
    `COLECO_V2_SOCKET_FF(plug_request_ff_25, "MISTRAL_FF.24.2.16", request[25], plug_request[25])
    `COLECO_V2_SOCKET_FF(plug_request_ff_26, "MISTRAL_FF.24.2.20", request[26], plug_request[26])
    `COLECO_V2_SOCKET_FF(plug_request_ff_27, "MISTRAL_FF.24.2.22", request[27], plug_request[27])
    `COLECO_V2_SOCKET_FF(plug_request_ff_28, "MISTRAL_FF.24.2.26", request[28], plug_request[28])
    `COLECO_V2_SOCKET_FF(plug_request_ff_29, "MISTRAL_FF.24.2.28", request[29], plug_request[29])
    `COLECO_V2_SOCKET_FF(plug_request_ff_30, "MISTRAL_FF.24.2.32", request[30], plug_request[30])
    `COLECO_V2_SOCKET_FF(plug_response_ff_0, "MISTRAL_FF.24.2.10", plug_response[0], response[0])
    `COLECO_V2_SOCKET_FF(plug_response_ff_1, "MISTRAL_FF.24.2.34", plug_response[1], response[1])
    `COLECO_V2_SOCKET_FF(plug_response_ff_2, "MISTRAL_FF.24.2.38", plug_response[2], response[2])
    `COLECO_V2_SOCKET_FF(plug_response_ff_3, "MISTRAL_FF.24.2.40", plug_response[3], response[3])
    `COLECO_V2_SOCKET_FF(plug_response_ff_4, "MISTRAL_FF.24.2.44", plug_response[4], response[4])
    `COLECO_V2_SOCKET_FF(plug_response_ff_5, "MISTRAL_FF.24.2.46", plug_response[5], response[5])
    `COLECO_V2_SOCKET_FF(plug_response_ff_6, "MISTRAL_FF.24.2.50", plug_response[6], response[6])
    `COLECO_V2_SOCKET_FF(plug_response_ff_7, "MISTRAL_FF.24.2.52", plug_response[7], response[7])
    `COLECO_V2_SOCKET_FF(plug_response_ff_8, "MISTRAL_FF.24.2.56", plug_response[8], response[8])
    `COLECO_V2_SOCKET_FF(plug_response_ff_9, "MISTRAL_FF.24.2.58", plug_response[9], response[9])
    `COLECO_V2_SOCKET_FF(plug_response_ff_10, "MISTRAL_FF.24.3.2", plug_response[10], response[10])
    `COLECO_V2_SOCKET_FF(plug_response_ff_11, "MISTRAL_FF.24.3.4", plug_response[11], response[11])
    `COLECO_V2_SOCKET_FF(plug_response_ff_12, "MISTRAL_FF.24.3.8", plug_response[12], response[12])
    `COLECO_V2_SOCKET_FF(plug_response_ff_13, "MISTRAL_FF.24.3.14", plug_response[13], response[13])
    `COLECO_V2_SOCKET_FF(plug_response_ff_14, "MISTRAL_FF.24.3.16", plug_response[14], response[14])
    `COLECO_V2_SOCKET_FF(plug_response_ff_15, "MISTRAL_FF.24.3.20", plug_response[15], response[15])
    `COLECO_V2_SOCKET_FF(plug_response_ff_16, "MISTRAL_FF.24.3.22", plug_response[16], response[16])
    `COLECO_V2_SOCKET_FF(plug_response_ff_17, "MISTRAL_FF.24.3.26", plug_response[17], response[17])
    `COLECO_V2_SOCKET_FF(plug_response_ff_18, "MISTRAL_FF.24.3.28", plug_response[18], response[18])
    `COLECO_V2_SOCKET_FF(plug_response_ff_19, "MISTRAL_FF.24.3.32", plug_response[19], response[19])
    `COLECO_V2_SOCKET_FF(plug_response_ff_20, "MISTRAL_FF.24.3.34", plug_response[20], response[20])
    `COLECO_V2_SOCKET_FF(plug_response_ff_21, "MISTRAL_FF.24.3.38", plug_response[21], response[21])
    `COLECO_V2_SOCKET_FF(plug_response_ff_22, "MISTRAL_FF.24.3.40", plug_response[22], response[22])
    `COLECO_V2_SOCKET_FF(plug_response_ff_23, "MISTRAL_FF.24.3.44", plug_response[23], response[23])
    `COLECO_V2_SOCKET_FF(plug_response_ff_24, "MISTRAL_FF.24.3.46", plug_response[24], response[24])
    `COLECO_V2_SOCKET_FF(plug_response_ff_25, "MISTRAL_FF.24.3.50", plug_response[25], response[25])
    `COLECO_V2_SOCKET_FF(plug_response_ff_26, "MISTRAL_FF.24.3.52", plug_response[26], response[26])
    `COLECO_V2_SOCKET_FF(plug_response_ff_27, "MISTRAL_FF.24.3.56", plug_response[27], response[27])
`undef COLECO_V2_SOCKET_FF
`endif
endmodule
