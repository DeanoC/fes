// SPDX-License-Identifier: GPL-3.0-or-later
// fes.atari-st-bus.socket/1. Pinned registered boundary, one request clock
// and one response clock. Vacant response is zero; the motherboard supplies
// acknowledged all-one cartridge reads when PRESENT is low.
`include "fes_atari_st_bus.vh"
module st_expansion_socket (
    input wire clock,
    input wire [`FES_ATARI_ST_BUS_REQUEST_BITS-1:0] request,
    output wire [`FES_ATARI_ST_BUS_RESPONSE_BITS-1:0] response,
    output wire [`FES_ATARI_ST_BUS_REQUEST_BITS-1:0] plug_request,
    input wire [`FES_ATARI_ST_BUS_RESPONSE_BITS-1:0] plug_response
);
`ifdef VERILATOR
    reg [55:0] request_q = 0;
    reg [31:0] response_q = 0;
    always @(posedge clock) begin
        request_q <= request;
        response_q <= plug_response;
    end
    assign plug_request = request_q;
    assign response = response_q;
`else
`define ST_SOCKET_FF(NAME, SITE, D, QOUT) \
    (* keep, BEL = SITE *) MISTRAL_FF NAME ( \
        .CLK(clock), .DATAIN(D), .Q(QOUT), .ACLR(1'b1), .ENA(1'b1), \
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep *) wire [30:0] clock_coverage_unused;
    `ST_SOCKET_FF(plug_request_ff_0, "MISTRAL_FF.24.1.2", request[0], plug_request[0])
    `ST_SOCKET_FF(plug_request_ff_1, "MISTRAL_FF.24.1.4", request[1], plug_request[1])
    `ST_SOCKET_FF(plug_request_ff_2, "MISTRAL_FF.24.1.8", request[2], plug_request[2])
    `ST_SOCKET_FF(plug_request_ff_3, "MISTRAL_FF.24.1.10", request[3], plug_request[3])
    `ST_SOCKET_FF(plug_request_ff_4, "MISTRAL_FF.24.1.14", request[4], plug_request[4])
    `ST_SOCKET_FF(plug_request_ff_5, "MISTRAL_FF.24.1.16", request[5], plug_request[5])
    `ST_SOCKET_FF(plug_request_ff_6, "MISTRAL_FF.24.1.20", request[6], plug_request[6])
    `ST_SOCKET_FF(plug_request_ff_7, "MISTRAL_FF.24.1.22", request[7], plug_request[7])
    `ST_SOCKET_FF(plug_request_ff_8, "MISTRAL_FF.24.1.26", request[8], plug_request[8])
    `ST_SOCKET_FF(plug_request_ff_9, "MISTRAL_FF.24.1.28", request[9], plug_request[9])
    `ST_SOCKET_FF(plug_request_ff_10, "MISTRAL_FF.24.1.32", request[10], plug_request[10])
    `ST_SOCKET_FF(plug_request_ff_11, "MISTRAL_FF.24.1.34", request[11], plug_request[11])
    `ST_SOCKET_FF(plug_request_ff_12, "MISTRAL_FF.24.1.38", request[12], plug_request[12])
    `ST_SOCKET_FF(plug_request_ff_13, "MISTRAL_FF.24.1.40", request[13], plug_request[13])
    `ST_SOCKET_FF(plug_request_ff_14, "MISTRAL_FF.24.1.44", request[14], plug_request[14])
    `ST_SOCKET_FF(plug_request_ff_15, "MISTRAL_FF.24.1.46", request[15], plug_request[15])
    `ST_SOCKET_FF(plug_request_ff_16, "MISTRAL_FF.24.1.50", request[16], plug_request[16])
    `ST_SOCKET_FF(plug_request_ff_17, "MISTRAL_FF.24.1.52", request[17], plug_request[17])
    `ST_SOCKET_FF(plug_request_ff_18, "MISTRAL_FF.24.1.56", request[18], plug_request[18])
    `ST_SOCKET_FF(plug_request_ff_19, "MISTRAL_FF.24.1.58", request[19], plug_request[19])
    `ST_SOCKET_FF(plug_request_ff_20, "MISTRAL_FF.24.2.2", request[20], plug_request[20])
    `ST_SOCKET_FF(plug_request_ff_21, "MISTRAL_FF.24.2.4", request[21], plug_request[21])
    `ST_SOCKET_FF(plug_request_ff_22, "MISTRAL_FF.24.2.8", request[22], plug_request[22])
    `ST_SOCKET_FF(plug_request_ff_23, "MISTRAL_FF.24.2.10", request[23], plug_request[23])
    `ST_SOCKET_FF(plug_request_ff_24, "MISTRAL_FF.24.2.14", request[24], plug_request[24])
    `ST_SOCKET_FF(plug_request_ff_25, "MISTRAL_FF.24.2.16", request[25], plug_request[25])
    `ST_SOCKET_FF(plug_request_ff_26, "MISTRAL_FF.24.2.20", request[26], plug_request[26])
    `ST_SOCKET_FF(plug_request_ff_27, "MISTRAL_FF.24.2.22", request[27], plug_request[27])
    `ST_SOCKET_FF(plug_request_ff_28, "MISTRAL_FF.24.2.26", request[28], plug_request[28])
    `ST_SOCKET_FF(plug_request_ff_29, "MISTRAL_FF.24.2.28", request[29], plug_request[29])
    `ST_SOCKET_FF(plug_request_ff_30, "MISTRAL_FF.24.2.32", request[30], plug_request[30])
    `ST_SOCKET_FF(plug_request_ff_31, "MISTRAL_FF.24.2.34", request[31], plug_request[31])
    `ST_SOCKET_FF(plug_request_ff_32, "MISTRAL_FF.24.2.38", request[32], plug_request[32])
    `ST_SOCKET_FF(plug_request_ff_33, "MISTRAL_FF.24.2.40", request[33], plug_request[33])
    `ST_SOCKET_FF(plug_request_ff_34, "MISTRAL_FF.24.2.44", request[34], plug_request[34])
    `ST_SOCKET_FF(plug_request_ff_35, "MISTRAL_FF.24.2.46", request[35], plug_request[35])
    `ST_SOCKET_FF(plug_request_ff_36, "MISTRAL_FF.24.2.50", request[36], plug_request[36])
    `ST_SOCKET_FF(plug_request_ff_37, "MISTRAL_FF.24.2.52", request[37], plug_request[37])
    `ST_SOCKET_FF(plug_request_ff_38, "MISTRAL_FF.24.2.56", request[38], plug_request[38])
    `ST_SOCKET_FF(plug_request_ff_39, "MISTRAL_FF.24.2.58", request[39], plug_request[39])
    `ST_SOCKET_FF(plug_request_ff_40, "MISTRAL_FF.24.3.2", request[40], plug_request[40])
    `ST_SOCKET_FF(plug_request_ff_41, "MISTRAL_FF.24.3.4", request[41], plug_request[41])
    `ST_SOCKET_FF(plug_request_ff_42, "MISTRAL_FF.24.3.8", request[42], plug_request[42])
    `ST_SOCKET_FF(plug_request_ff_43, "MISTRAL_FF.24.3.10", request[43], plug_request[43])
    `ST_SOCKET_FF(plug_request_ff_44, "MISTRAL_FF.24.3.14", request[44], plug_request[44])
    `ST_SOCKET_FF(plug_request_ff_45, "MISTRAL_FF.24.3.16", request[45], plug_request[45])
    `ST_SOCKET_FF(plug_request_ff_46, "MISTRAL_FF.24.3.20", request[46], plug_request[46])
    `ST_SOCKET_FF(plug_request_ff_47, "MISTRAL_FF.24.3.22", request[47], plug_request[47])
    `ST_SOCKET_FF(plug_request_ff_48, "MISTRAL_FF.24.3.26", request[48], plug_request[48])
    `ST_SOCKET_FF(plug_request_ff_49, "MISTRAL_FF.24.3.28", request[49], plug_request[49])
    `ST_SOCKET_FF(plug_request_ff_50, "MISTRAL_FF.24.3.32", request[50], plug_request[50])
    `ST_SOCKET_FF(plug_request_ff_51, "MISTRAL_FF.24.3.34", request[51], plug_request[51])
    `ST_SOCKET_FF(plug_request_ff_52, "MISTRAL_FF.24.3.38", request[52], plug_request[52])
    `ST_SOCKET_FF(plug_request_ff_53, "MISTRAL_FF.24.3.40", request[53], plug_request[53])
    `ST_SOCKET_FF(plug_request_ff_54, "MISTRAL_FF.24.3.44", request[54], plug_request[54])
    `ST_SOCKET_FF(plug_request_ff_55, "MISTRAL_FF.24.3.46", request[55], plug_request[55])
    `ST_SOCKET_FF(plug_response_ff_0, "MISTRAL_FF.24.3.50", plug_response[0], response[0])
    `ST_SOCKET_FF(plug_response_ff_1, "MISTRAL_FF.24.3.52", plug_response[1], response[1])
    `ST_SOCKET_FF(plug_response_ff_2, "MISTRAL_FF.24.3.56", plug_response[2], response[2])
    `ST_SOCKET_FF(plug_response_ff_3, "MISTRAL_FF.24.3.58", plug_response[3], response[3])
    `ST_SOCKET_FF(plug_response_ff_4, "MISTRAL_FF.24.4.2", plug_response[4], response[4])
    `ST_SOCKET_FF(plug_response_ff_5, "MISTRAL_FF.24.4.4", plug_response[5], response[5])
    `ST_SOCKET_FF(plug_response_ff_6, "MISTRAL_FF.24.4.8", plug_response[6], response[6])
    `ST_SOCKET_FF(plug_response_ff_7, "MISTRAL_FF.24.4.10", plug_response[7], response[7])
    `ST_SOCKET_FF(plug_response_ff_8, "MISTRAL_FF.24.4.14", plug_response[8], response[8])
    `ST_SOCKET_FF(plug_response_ff_9, "MISTRAL_FF.24.4.16", plug_response[9], response[9])
    `ST_SOCKET_FF(plug_response_ff_10, "MISTRAL_FF.24.4.20", plug_response[10], response[10])
    `ST_SOCKET_FF(plug_response_ff_11, "MISTRAL_FF.24.4.22", plug_response[11], response[11])
    `ST_SOCKET_FF(plug_response_ff_12, "MISTRAL_FF.24.4.26", plug_response[12], response[12])
    `ST_SOCKET_FF(plug_response_ff_13, "MISTRAL_FF.24.4.28", plug_response[13], response[13])
    `ST_SOCKET_FF(plug_response_ff_14, "MISTRAL_FF.24.4.32", plug_response[14], response[14])
    `ST_SOCKET_FF(plug_response_ff_15, "MISTRAL_FF.24.4.34", plug_response[15], response[15])
    `ST_SOCKET_FF(plug_response_ff_16, "MISTRAL_FF.24.4.38", plug_response[16], response[16])
    `ST_SOCKET_FF(plug_response_ff_17, "MISTRAL_FF.24.4.40", plug_response[17], response[17])
    `ST_SOCKET_FF(plug_response_ff_18, "MISTRAL_FF.24.4.44", plug_response[18], response[18])
    `ST_SOCKET_FF(plug_response_ff_19, "MISTRAL_FF.24.4.46", plug_response[19], response[19])
    `ST_SOCKET_FF(plug_response_ff_20, "MISTRAL_FF.24.4.50", plug_response[20], response[20])
    `ST_SOCKET_FF(plug_response_ff_21, "MISTRAL_FF.24.4.52", plug_response[21], response[21])
    `ST_SOCKET_FF(plug_response_ff_22, "MISTRAL_FF.24.4.56", plug_response[22], response[22])
    `ST_SOCKET_FF(plug_response_ff_23, "MISTRAL_FF.24.4.58", plug_response[23], response[23])
    `ST_SOCKET_FF(plug_response_ff_24, "MISTRAL_FF.24.5.2", plug_response[24], response[24])
    `ST_SOCKET_FF(plug_response_ff_25, "MISTRAL_FF.24.5.4", plug_response[25], response[25])
    `ST_SOCKET_FF(plug_response_ff_26, "MISTRAL_FF.24.5.8", plug_response[26], response[26])
    `ST_SOCKET_FF(plug_response_ff_27, "MISTRAL_FF.24.5.10", plug_response[27], response[27])
    `ST_SOCKET_FF(plug_response_ff_28, "MISTRAL_FF.24.5.14", plug_response[28], response[28])
    `ST_SOCKET_FF(plug_response_ff_29, "MISTRAL_FF.24.5.16", plug_response[29], response[29])
    `ST_SOCKET_FF(plug_response_ff_30, "MISTRAL_FF.24.5.20", plug_response[30], response[30])
    `ST_SOCKET_FF(plug_response_ff_31, "MISTRAL_FF.24.5.22", plug_response[31], response[31])
    `ST_SOCKET_FF(clock_coverage_ff_0, "MISTRAL_FF.24.6.56", 1'b0, clock_coverage_unused[0])
    `ST_SOCKET_FF(clock_coverage_ff_1, "MISTRAL_FF.24.7.56", 1'b0, clock_coverage_unused[1])
    `ST_SOCKET_FF(clock_coverage_ff_2, "MISTRAL_FF.24.8.56", 1'b0, clock_coverage_unused[2])
    `ST_SOCKET_FF(clock_coverage_ff_3, "MISTRAL_FF.24.9.56", 1'b0, clock_coverage_unused[3])
    `ST_SOCKET_FF(clock_coverage_ff_4, "MISTRAL_FF.24.10.56", 1'b0, clock_coverage_unused[4])
    `ST_SOCKET_FF(clock_coverage_ff_5, "MISTRAL_FF.24.11.56", 1'b0, clock_coverage_unused[5])
    `ST_SOCKET_FF(clock_coverage_ff_6, "MISTRAL_FF.24.12.56", 1'b0, clock_coverage_unused[6])
    `ST_SOCKET_FF(clock_coverage_ff_7, "MISTRAL_FF.24.13.56", 1'b0, clock_coverage_unused[7])
    `ST_SOCKET_FF(clock_coverage_ff_8, "MISTRAL_FF.24.14.56", 1'b0, clock_coverage_unused[8])
    `ST_SOCKET_FF(clock_coverage_ff_9, "MISTRAL_FF.24.15.56", 1'b0, clock_coverage_unused[9])
    `ST_SOCKET_FF(clock_coverage_ff_10, "MISTRAL_FF.24.16.56", 1'b0, clock_coverage_unused[10])
    `ST_SOCKET_FF(clock_coverage_ff_11, "MISTRAL_FF.24.17.56", 1'b0, clock_coverage_unused[11])
    `ST_SOCKET_FF(clock_coverage_ff_12, "MISTRAL_FF.24.18.56", 1'b0, clock_coverage_unused[12])
    `ST_SOCKET_FF(clock_coverage_ff_13, "MISTRAL_FF.28.1.56", 1'b0, clock_coverage_unused[13])
    `ST_SOCKET_FF(clock_coverage_ff_14, "MISTRAL_FF.28.2.56", 1'b0, clock_coverage_unused[14])
    `ST_SOCKET_FF(clock_coverage_ff_15, "MISTRAL_FF.28.3.56", 1'b0, clock_coverage_unused[15])
    `ST_SOCKET_FF(clock_coverage_ff_16, "MISTRAL_FF.28.4.56", 1'b0, clock_coverage_unused[16])
    `ST_SOCKET_FF(clock_coverage_ff_17, "MISTRAL_FF.28.5.56", 1'b0, clock_coverage_unused[17])
    `ST_SOCKET_FF(clock_coverage_ff_18, "MISTRAL_FF.28.6.56", 1'b0, clock_coverage_unused[18])
    `ST_SOCKET_FF(clock_coverage_ff_19, "MISTRAL_FF.28.7.56", 1'b0, clock_coverage_unused[19])
    `ST_SOCKET_FF(clock_coverage_ff_20, "MISTRAL_FF.28.8.56", 1'b0, clock_coverage_unused[20])
    `ST_SOCKET_FF(clock_coverage_ff_21, "MISTRAL_FF.28.9.56", 1'b0, clock_coverage_unused[21])
    `ST_SOCKET_FF(clock_coverage_ff_22, "MISTRAL_FF.28.10.56", 1'b0, clock_coverage_unused[22])
    `ST_SOCKET_FF(clock_coverage_ff_23, "MISTRAL_FF.28.11.56", 1'b0, clock_coverage_unused[23])
    `ST_SOCKET_FF(clock_coverage_ff_24, "MISTRAL_FF.28.12.56", 1'b0, clock_coverage_unused[24])
    `ST_SOCKET_FF(clock_coverage_ff_25, "MISTRAL_FF.28.13.56", 1'b0, clock_coverage_unused[25])
    `ST_SOCKET_FF(clock_coverage_ff_26, "MISTRAL_FF.28.14.56", 1'b0, clock_coverage_unused[26])
    `ST_SOCKET_FF(clock_coverage_ff_27, "MISTRAL_FF.28.15.56", 1'b0, clock_coverage_unused[27])
    `ST_SOCKET_FF(clock_coverage_ff_28, "MISTRAL_FF.28.16.56", 1'b0, clock_coverage_unused[28])
    `ST_SOCKET_FF(clock_coverage_ff_29, "MISTRAL_FF.28.17.56", 1'b0, clock_coverage_unused[29])
    `ST_SOCKET_FF(clock_coverage_ff_30, "MISTRAL_FF.28.18.56", 1'b0, clock_coverage_unused[30])
`undef ST_SOCKET_FF
`endif
endmodule
