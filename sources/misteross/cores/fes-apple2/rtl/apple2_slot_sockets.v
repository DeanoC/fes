// SPDX-License-Identifier: GPL-2.0-or-later
// Registered boundaries of the four physical Apple II slot sockets
// (fes.apple2-bus.slots/1). Each socket pins its 32 request and 28 response
// flip-flops in the first three LABs of column 24 of its placement rectangle
// and a clock-coverage flip-flop in the fourth, so the frozen shell routes
// the system clock into every socket. A vacant socket's response registers
// clock in zero. Simulation uses plain registers with the same latency.
// Generated from the socket table in scripts/apple2_slots.py; do not edit.
`include "apple2_bus.vh"

module apple2_slot_socket2 (
    input  wire clock,
    input  wire [`A2_BUS_REQ-1:0] request,
    output wire [`A2_BUS_RSP-1:0] response,
    output wire [`A2_BUS_REQ-1:0] plug_request,
    input  wire [`A2_BUS_RSP-1:0] plug_response
);
`ifdef VERILATOR
    reg [`A2_BUS_REQ-1:0] request_q = 0;
    reg [`A2_BUS_RSP-1:0] response_q = 0;
    always @(posedge clock) begin
        request_q <= request;
        response_q <= plug_response;
    end
    assign plug_request = request_q;
    assign response = response_q;
`else
`define A2_SOCKET_FF(NAME, SITE, D, QOUT) \
    (* keep, BEL = SITE *) MISTRAL_FF NAME ( \
        .CLK(clock), .DATAIN(D), .Q(QOUT), .ACLR(1'b1), .ENA(1'b1), \
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep *) wire clock_coverage_unused;
    `A2_SOCKET_FF(clock_coverage_ff, "MISTRAL_FF.24.4.56", 1'b0, clock_coverage_unused)
    `A2_SOCKET_FF(plug_request_ff_0, "MISTRAL_FF.24.1.2", request[0], plug_request[0])
    `A2_SOCKET_FF(plug_request_ff_1, "MISTRAL_FF.24.1.4", request[1], plug_request[1])
    `A2_SOCKET_FF(plug_request_ff_2, "MISTRAL_FF.24.1.8", request[2], plug_request[2])
    `A2_SOCKET_FF(plug_request_ff_3, "MISTRAL_FF.24.1.10", request[3], plug_request[3])
    `A2_SOCKET_FF(plug_request_ff_4, "MISTRAL_FF.24.1.14", request[4], plug_request[4])
    `A2_SOCKET_FF(plug_request_ff_5, "MISTRAL_FF.24.1.16", request[5], plug_request[5])
    `A2_SOCKET_FF(plug_request_ff_6, "MISTRAL_FF.24.1.20", request[6], plug_request[6])
    `A2_SOCKET_FF(plug_request_ff_7, "MISTRAL_FF.24.1.22", request[7], plug_request[7])
    `A2_SOCKET_FF(plug_request_ff_8, "MISTRAL_FF.24.1.26", request[8], plug_request[8])
    `A2_SOCKET_FF(plug_request_ff_9, "MISTRAL_FF.24.1.28", request[9], plug_request[9])
    `A2_SOCKET_FF(plug_request_ff_10, "MISTRAL_FF.24.1.32", request[10], plug_request[10])
    `A2_SOCKET_FF(plug_request_ff_11, "MISTRAL_FF.24.1.34", request[11], plug_request[11])
    `A2_SOCKET_FF(plug_request_ff_12, "MISTRAL_FF.24.1.38", request[12], plug_request[12])
    `A2_SOCKET_FF(plug_request_ff_13, "MISTRAL_FF.24.1.40", request[13], plug_request[13])
    `A2_SOCKET_FF(plug_request_ff_14, "MISTRAL_FF.24.1.44", request[14], plug_request[14])
    `A2_SOCKET_FF(plug_request_ff_15, "MISTRAL_FF.24.1.46", request[15], plug_request[15])
    `A2_SOCKET_FF(plug_request_ff_16, "MISTRAL_FF.24.1.50", request[16], plug_request[16])
    `A2_SOCKET_FF(plug_request_ff_17, "MISTRAL_FF.24.1.52", request[17], plug_request[17])
    `A2_SOCKET_FF(plug_request_ff_18, "MISTRAL_FF.24.1.56", request[18], plug_request[18])
    `A2_SOCKET_FF(plug_request_ff_19, "MISTRAL_FF.24.1.58", request[19], plug_request[19])
    `A2_SOCKET_FF(plug_request_ff_20, "MISTRAL_FF.24.2.2", request[20], plug_request[20])
    `A2_SOCKET_FF(plug_request_ff_21, "MISTRAL_FF.24.2.4", request[21], plug_request[21])
    `A2_SOCKET_FF(plug_request_ff_22, "MISTRAL_FF.24.2.8", request[22], plug_request[22])
    `A2_SOCKET_FF(plug_request_ff_23, "MISTRAL_FF.24.2.10", request[23], plug_request[23])
    `A2_SOCKET_FF(plug_request_ff_24, "MISTRAL_FF.24.2.14", request[24], plug_request[24])
    `A2_SOCKET_FF(plug_request_ff_25, "MISTRAL_FF.24.2.16", request[25], plug_request[25])
    `A2_SOCKET_FF(plug_request_ff_26, "MISTRAL_FF.24.2.20", request[26], plug_request[26])
    `A2_SOCKET_FF(plug_request_ff_27, "MISTRAL_FF.24.2.22", request[27], plug_request[27])
    `A2_SOCKET_FF(plug_request_ff_28, "MISTRAL_FF.24.2.26", request[28], plug_request[28])
    `A2_SOCKET_FF(plug_request_ff_29, "MISTRAL_FF.24.2.28", request[29], plug_request[29])
    `A2_SOCKET_FF(plug_request_ff_30, "MISTRAL_FF.24.2.32", request[30], plug_request[30])
    `A2_SOCKET_FF(plug_request_ff_31, "MISTRAL_FF.24.2.34", request[31], plug_request[31])
    `A2_SOCKET_FF(plug_response_ff_0, "MISTRAL_FF.24.2.38", plug_response[0], response[0])
    `A2_SOCKET_FF(plug_response_ff_1, "MISTRAL_FF.24.2.40", plug_response[1], response[1])
    `A2_SOCKET_FF(plug_response_ff_2, "MISTRAL_FF.24.2.44", plug_response[2], response[2])
    `A2_SOCKET_FF(plug_response_ff_3, "MISTRAL_FF.24.2.46", plug_response[3], response[3])
    `A2_SOCKET_FF(plug_response_ff_4, "MISTRAL_FF.24.2.50", plug_response[4], response[4])
    `A2_SOCKET_FF(plug_response_ff_5, "MISTRAL_FF.24.2.52", plug_response[5], response[5])
    `A2_SOCKET_FF(plug_response_ff_6, "MISTRAL_FF.24.2.56", plug_response[6], response[6])
    `A2_SOCKET_FF(plug_response_ff_7, "MISTRAL_FF.24.2.58", plug_response[7], response[7])
    `A2_SOCKET_FF(plug_response_ff_8, "MISTRAL_FF.24.3.2", plug_response[8], response[8])
    `A2_SOCKET_FF(plug_response_ff_9, "MISTRAL_FF.24.3.4", plug_response[9], response[9])
    `A2_SOCKET_FF(plug_response_ff_10, "MISTRAL_FF.24.3.8", plug_response[10], response[10])
    `A2_SOCKET_FF(plug_response_ff_11, "MISTRAL_FF.24.3.10", plug_response[11], response[11])
    `A2_SOCKET_FF(plug_response_ff_12, "MISTRAL_FF.24.3.14", plug_response[12], response[12])
    `A2_SOCKET_FF(plug_response_ff_13, "MISTRAL_FF.24.3.16", plug_response[13], response[13])
    `A2_SOCKET_FF(plug_response_ff_14, "MISTRAL_FF.24.3.20", plug_response[14], response[14])
    `A2_SOCKET_FF(plug_response_ff_15, "MISTRAL_FF.24.3.22", plug_response[15], response[15])
    `A2_SOCKET_FF(plug_response_ff_16, "MISTRAL_FF.24.3.26", plug_response[16], response[16])
    `A2_SOCKET_FF(plug_response_ff_17, "MISTRAL_FF.24.3.28", plug_response[17], response[17])
    `A2_SOCKET_FF(plug_response_ff_18, "MISTRAL_FF.24.3.32", plug_response[18], response[18])
    `A2_SOCKET_FF(plug_response_ff_19, "MISTRAL_FF.24.3.34", plug_response[19], response[19])
    `A2_SOCKET_FF(plug_response_ff_20, "MISTRAL_FF.24.3.38", plug_response[20], response[20])
    `A2_SOCKET_FF(plug_response_ff_21, "MISTRAL_FF.24.3.40", plug_response[21], response[21])
    `A2_SOCKET_FF(plug_response_ff_22, "MISTRAL_FF.24.3.44", plug_response[22], response[22])
    `A2_SOCKET_FF(plug_response_ff_23, "MISTRAL_FF.24.3.46", plug_response[23], response[23])
    `A2_SOCKET_FF(plug_response_ff_24, "MISTRAL_FF.24.3.50", plug_response[24], response[24])
    `A2_SOCKET_FF(plug_response_ff_25, "MISTRAL_FF.24.3.52", plug_response[25], response[25])
    `A2_SOCKET_FF(plug_response_ff_26, "MISTRAL_FF.24.3.56", plug_response[26], response[26])
    `A2_SOCKET_FF(plug_response_ff_27, "MISTRAL_FF.24.3.58", plug_response[27], response[27])
`undef A2_SOCKET_FF
`endif
endmodule

module apple2_slot_socket4 (
    input  wire clock,
    input  wire [`A2_BUS_REQ-1:0] request,
    output wire [`A2_BUS_RSP-1:0] response,
    output wire [`A2_BUS_REQ-1:0] plug_request,
    input  wire [`A2_BUS_RSP-1:0] plug_response
);
`ifdef VERILATOR
    reg [`A2_BUS_REQ-1:0] request_q = 0;
    reg [`A2_BUS_RSP-1:0] response_q = 0;
    always @(posedge clock) begin
        request_q <= request;
        response_q <= plug_response;
    end
    assign plug_request = request_q;
    assign response = response_q;
`else
`define A2_SOCKET_FF(NAME, SITE, D, QOUT) \
    (* keep, BEL = SITE *) MISTRAL_FF NAME ( \
        .CLK(clock), .DATAIN(D), .Q(QOUT), .ACLR(1'b1), .ENA(1'b1), \
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep *) wire clock_coverage_unused;
    `A2_SOCKET_FF(clock_coverage_ff, "MISTRAL_FF.24.24.56", 1'b0, clock_coverage_unused)
    `A2_SOCKET_FF(plug_request_ff_0, "MISTRAL_FF.24.21.2", request[0], plug_request[0])
    `A2_SOCKET_FF(plug_request_ff_1, "MISTRAL_FF.24.21.4", request[1], plug_request[1])
    `A2_SOCKET_FF(plug_request_ff_2, "MISTRAL_FF.24.21.8", request[2], plug_request[2])
    `A2_SOCKET_FF(plug_request_ff_3, "MISTRAL_FF.24.21.10", request[3], plug_request[3])
    `A2_SOCKET_FF(plug_request_ff_4, "MISTRAL_FF.24.21.14", request[4], plug_request[4])
    `A2_SOCKET_FF(plug_request_ff_5, "MISTRAL_FF.24.21.16", request[5], plug_request[5])
    `A2_SOCKET_FF(plug_request_ff_6, "MISTRAL_FF.24.21.20", request[6], plug_request[6])
    `A2_SOCKET_FF(plug_request_ff_7, "MISTRAL_FF.24.21.22", request[7], plug_request[7])
    `A2_SOCKET_FF(plug_request_ff_8, "MISTRAL_FF.24.21.26", request[8], plug_request[8])
    `A2_SOCKET_FF(plug_request_ff_9, "MISTRAL_FF.24.21.28", request[9], plug_request[9])
    `A2_SOCKET_FF(plug_request_ff_10, "MISTRAL_FF.24.21.32", request[10], plug_request[10])
    `A2_SOCKET_FF(plug_request_ff_11, "MISTRAL_FF.24.21.34", request[11], plug_request[11])
    `A2_SOCKET_FF(plug_request_ff_12, "MISTRAL_FF.24.21.38", request[12], plug_request[12])
    `A2_SOCKET_FF(plug_request_ff_13, "MISTRAL_FF.24.21.40", request[13], plug_request[13])
    `A2_SOCKET_FF(plug_request_ff_14, "MISTRAL_FF.24.21.44", request[14], plug_request[14])
    `A2_SOCKET_FF(plug_request_ff_15, "MISTRAL_FF.24.21.46", request[15], plug_request[15])
    `A2_SOCKET_FF(plug_request_ff_16, "MISTRAL_FF.24.21.50", request[16], plug_request[16])
    `A2_SOCKET_FF(plug_request_ff_17, "MISTRAL_FF.24.21.52", request[17], plug_request[17])
    `A2_SOCKET_FF(plug_request_ff_18, "MISTRAL_FF.24.21.56", request[18], plug_request[18])
    `A2_SOCKET_FF(plug_request_ff_19, "MISTRAL_FF.24.21.58", request[19], plug_request[19])
    `A2_SOCKET_FF(plug_request_ff_20, "MISTRAL_FF.24.22.2", request[20], plug_request[20])
    `A2_SOCKET_FF(plug_request_ff_21, "MISTRAL_FF.24.22.4", request[21], plug_request[21])
    `A2_SOCKET_FF(plug_request_ff_22, "MISTRAL_FF.24.22.8", request[22], plug_request[22])
    `A2_SOCKET_FF(plug_request_ff_23, "MISTRAL_FF.24.22.10", request[23], plug_request[23])
    `A2_SOCKET_FF(plug_request_ff_24, "MISTRAL_FF.24.22.14", request[24], plug_request[24])
    `A2_SOCKET_FF(plug_request_ff_25, "MISTRAL_FF.24.22.16", request[25], plug_request[25])
    `A2_SOCKET_FF(plug_request_ff_26, "MISTRAL_FF.24.22.20", request[26], plug_request[26])
    `A2_SOCKET_FF(plug_request_ff_27, "MISTRAL_FF.24.22.22", request[27], plug_request[27])
    `A2_SOCKET_FF(plug_request_ff_28, "MISTRAL_FF.24.22.26", request[28], plug_request[28])
    `A2_SOCKET_FF(plug_request_ff_29, "MISTRAL_FF.24.22.28", request[29], plug_request[29])
    `A2_SOCKET_FF(plug_request_ff_30, "MISTRAL_FF.24.22.32", request[30], plug_request[30])
    `A2_SOCKET_FF(plug_request_ff_31, "MISTRAL_FF.24.22.34", request[31], plug_request[31])
    `A2_SOCKET_FF(plug_response_ff_0, "MISTRAL_FF.24.22.38", plug_response[0], response[0])
    `A2_SOCKET_FF(plug_response_ff_1, "MISTRAL_FF.24.22.40", plug_response[1], response[1])
    `A2_SOCKET_FF(plug_response_ff_2, "MISTRAL_FF.24.22.44", plug_response[2], response[2])
    `A2_SOCKET_FF(plug_response_ff_3, "MISTRAL_FF.24.22.46", plug_response[3], response[3])
    `A2_SOCKET_FF(plug_response_ff_4, "MISTRAL_FF.24.22.50", plug_response[4], response[4])
    `A2_SOCKET_FF(plug_response_ff_5, "MISTRAL_FF.24.22.52", plug_response[5], response[5])
    `A2_SOCKET_FF(plug_response_ff_6, "MISTRAL_FF.24.22.56", plug_response[6], response[6])
    `A2_SOCKET_FF(plug_response_ff_7, "MISTRAL_FF.24.22.58", plug_response[7], response[7])
    `A2_SOCKET_FF(plug_response_ff_8, "MISTRAL_FF.24.23.2", plug_response[8], response[8])
    `A2_SOCKET_FF(plug_response_ff_9, "MISTRAL_FF.24.23.4", plug_response[9], response[9])
    `A2_SOCKET_FF(plug_response_ff_10, "MISTRAL_FF.24.23.8", plug_response[10], response[10])
    `A2_SOCKET_FF(plug_response_ff_11, "MISTRAL_FF.24.23.10", plug_response[11], response[11])
    `A2_SOCKET_FF(plug_response_ff_12, "MISTRAL_FF.24.23.14", plug_response[12], response[12])
    `A2_SOCKET_FF(plug_response_ff_13, "MISTRAL_FF.24.23.16", plug_response[13], response[13])
    `A2_SOCKET_FF(plug_response_ff_14, "MISTRAL_FF.24.23.20", plug_response[14], response[14])
    `A2_SOCKET_FF(plug_response_ff_15, "MISTRAL_FF.24.23.22", plug_response[15], response[15])
    `A2_SOCKET_FF(plug_response_ff_16, "MISTRAL_FF.24.23.26", plug_response[16], response[16])
    `A2_SOCKET_FF(plug_response_ff_17, "MISTRAL_FF.24.23.28", plug_response[17], response[17])
    `A2_SOCKET_FF(plug_response_ff_18, "MISTRAL_FF.24.23.32", plug_response[18], response[18])
    `A2_SOCKET_FF(plug_response_ff_19, "MISTRAL_FF.24.23.34", plug_response[19], response[19])
    `A2_SOCKET_FF(plug_response_ff_20, "MISTRAL_FF.24.23.38", plug_response[20], response[20])
    `A2_SOCKET_FF(plug_response_ff_21, "MISTRAL_FF.24.23.40", plug_response[21], response[21])
    `A2_SOCKET_FF(plug_response_ff_22, "MISTRAL_FF.24.23.44", plug_response[22], response[22])
    `A2_SOCKET_FF(plug_response_ff_23, "MISTRAL_FF.24.23.46", plug_response[23], response[23])
    `A2_SOCKET_FF(plug_response_ff_24, "MISTRAL_FF.24.23.50", plug_response[24], response[24])
    `A2_SOCKET_FF(plug_response_ff_25, "MISTRAL_FF.24.23.52", plug_response[25], response[25])
    `A2_SOCKET_FF(plug_response_ff_26, "MISTRAL_FF.24.23.56", plug_response[26], response[26])
    `A2_SOCKET_FF(plug_response_ff_27, "MISTRAL_FF.24.23.58", plug_response[27], response[27])
`undef A2_SOCKET_FF
`endif
endmodule

module apple2_slot_socket5 (
    input  wire clock,
    input  wire [`A2_BUS_REQ-1:0] request,
    output wire [`A2_BUS_RSP-1:0] response,
    output wire [`A2_BUS_REQ-1:0] plug_request,
    input  wire [`A2_BUS_RSP-1:0] plug_response
);
`ifdef VERILATOR
    reg [`A2_BUS_REQ-1:0] request_q = 0;
    reg [`A2_BUS_RSP-1:0] response_q = 0;
    always @(posedge clock) begin
        request_q <= request;
        response_q <= plug_response;
    end
    assign plug_request = request_q;
    assign response = response_q;
`else
`define A2_SOCKET_FF(NAME, SITE, D, QOUT) \
    (* keep, BEL = SITE *) MISTRAL_FF NAME ( \
        .CLK(clock), .DATAIN(D), .Q(QOUT), .ACLR(1'b1), .ENA(1'b1), \
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep *) wire clock_coverage_unused;
    `A2_SOCKET_FF(clock_coverage_ff, "MISTRAL_FF.24.44.56", 1'b0, clock_coverage_unused)
    `A2_SOCKET_FF(plug_request_ff_0, "MISTRAL_FF.24.41.2", request[0], plug_request[0])
    `A2_SOCKET_FF(plug_request_ff_1, "MISTRAL_FF.24.41.4", request[1], plug_request[1])
    `A2_SOCKET_FF(plug_request_ff_2, "MISTRAL_FF.24.41.8", request[2], plug_request[2])
    `A2_SOCKET_FF(plug_request_ff_3, "MISTRAL_FF.24.41.10", request[3], plug_request[3])
    `A2_SOCKET_FF(plug_request_ff_4, "MISTRAL_FF.24.41.14", request[4], plug_request[4])
    `A2_SOCKET_FF(plug_request_ff_5, "MISTRAL_FF.24.41.16", request[5], plug_request[5])
    `A2_SOCKET_FF(plug_request_ff_6, "MISTRAL_FF.24.41.20", request[6], plug_request[6])
    `A2_SOCKET_FF(plug_request_ff_7, "MISTRAL_FF.24.41.22", request[7], plug_request[7])
    `A2_SOCKET_FF(plug_request_ff_8, "MISTRAL_FF.24.41.26", request[8], plug_request[8])
    `A2_SOCKET_FF(plug_request_ff_9, "MISTRAL_FF.24.41.28", request[9], plug_request[9])
    `A2_SOCKET_FF(plug_request_ff_10, "MISTRAL_FF.24.41.32", request[10], plug_request[10])
    `A2_SOCKET_FF(plug_request_ff_11, "MISTRAL_FF.24.41.34", request[11], plug_request[11])
    `A2_SOCKET_FF(plug_request_ff_12, "MISTRAL_FF.24.41.38", request[12], plug_request[12])
    `A2_SOCKET_FF(plug_request_ff_13, "MISTRAL_FF.24.41.40", request[13], plug_request[13])
    `A2_SOCKET_FF(plug_request_ff_14, "MISTRAL_FF.24.41.44", request[14], plug_request[14])
    `A2_SOCKET_FF(plug_request_ff_15, "MISTRAL_FF.24.41.46", request[15], plug_request[15])
    `A2_SOCKET_FF(plug_request_ff_16, "MISTRAL_FF.24.41.50", request[16], plug_request[16])
    `A2_SOCKET_FF(plug_request_ff_17, "MISTRAL_FF.24.41.52", request[17], plug_request[17])
    `A2_SOCKET_FF(plug_request_ff_18, "MISTRAL_FF.24.41.56", request[18], plug_request[18])
    `A2_SOCKET_FF(plug_request_ff_19, "MISTRAL_FF.24.41.58", request[19], plug_request[19])
    `A2_SOCKET_FF(plug_request_ff_20, "MISTRAL_FF.24.42.2", request[20], plug_request[20])
    `A2_SOCKET_FF(plug_request_ff_21, "MISTRAL_FF.24.42.4", request[21], plug_request[21])
    `A2_SOCKET_FF(plug_request_ff_22, "MISTRAL_FF.24.42.8", request[22], plug_request[22])
    `A2_SOCKET_FF(plug_request_ff_23, "MISTRAL_FF.24.42.10", request[23], plug_request[23])
    `A2_SOCKET_FF(plug_request_ff_24, "MISTRAL_FF.24.42.14", request[24], plug_request[24])
    `A2_SOCKET_FF(plug_request_ff_25, "MISTRAL_FF.24.42.16", request[25], plug_request[25])
    `A2_SOCKET_FF(plug_request_ff_26, "MISTRAL_FF.24.42.20", request[26], plug_request[26])
    `A2_SOCKET_FF(plug_request_ff_27, "MISTRAL_FF.24.42.22", request[27], plug_request[27])
    `A2_SOCKET_FF(plug_request_ff_28, "MISTRAL_FF.24.42.26", request[28], plug_request[28])
    `A2_SOCKET_FF(plug_request_ff_29, "MISTRAL_FF.24.42.28", request[29], plug_request[29])
    `A2_SOCKET_FF(plug_request_ff_30, "MISTRAL_FF.24.42.32", request[30], plug_request[30])
    `A2_SOCKET_FF(plug_request_ff_31, "MISTRAL_FF.24.42.34", request[31], plug_request[31])
    `A2_SOCKET_FF(plug_response_ff_0, "MISTRAL_FF.24.42.38", plug_response[0], response[0])
    `A2_SOCKET_FF(plug_response_ff_1, "MISTRAL_FF.24.42.40", plug_response[1], response[1])
    `A2_SOCKET_FF(plug_response_ff_2, "MISTRAL_FF.24.42.44", plug_response[2], response[2])
    `A2_SOCKET_FF(plug_response_ff_3, "MISTRAL_FF.24.42.46", plug_response[3], response[3])
    `A2_SOCKET_FF(plug_response_ff_4, "MISTRAL_FF.24.42.50", plug_response[4], response[4])
    `A2_SOCKET_FF(plug_response_ff_5, "MISTRAL_FF.24.42.52", plug_response[5], response[5])
    `A2_SOCKET_FF(plug_response_ff_6, "MISTRAL_FF.24.42.56", plug_response[6], response[6])
    `A2_SOCKET_FF(plug_response_ff_7, "MISTRAL_FF.24.42.58", plug_response[7], response[7])
    `A2_SOCKET_FF(plug_response_ff_8, "MISTRAL_FF.24.43.2", plug_response[8], response[8])
    `A2_SOCKET_FF(plug_response_ff_9, "MISTRAL_FF.24.43.4", plug_response[9], response[9])
    `A2_SOCKET_FF(plug_response_ff_10, "MISTRAL_FF.24.43.8", plug_response[10], response[10])
    `A2_SOCKET_FF(plug_response_ff_11, "MISTRAL_FF.24.43.10", plug_response[11], response[11])
    `A2_SOCKET_FF(plug_response_ff_12, "MISTRAL_FF.24.43.14", plug_response[12], response[12])
    `A2_SOCKET_FF(plug_response_ff_13, "MISTRAL_FF.24.43.16", plug_response[13], response[13])
    `A2_SOCKET_FF(plug_response_ff_14, "MISTRAL_FF.24.43.20", plug_response[14], response[14])
    `A2_SOCKET_FF(plug_response_ff_15, "MISTRAL_FF.24.43.22", plug_response[15], response[15])
    `A2_SOCKET_FF(plug_response_ff_16, "MISTRAL_FF.24.43.26", plug_response[16], response[16])
    `A2_SOCKET_FF(plug_response_ff_17, "MISTRAL_FF.24.43.28", plug_response[17], response[17])
    `A2_SOCKET_FF(plug_response_ff_18, "MISTRAL_FF.24.43.32", plug_response[18], response[18])
    `A2_SOCKET_FF(plug_response_ff_19, "MISTRAL_FF.24.43.34", plug_response[19], response[19])
    `A2_SOCKET_FF(plug_response_ff_20, "MISTRAL_FF.24.43.38", plug_response[20], response[20])
    `A2_SOCKET_FF(plug_response_ff_21, "MISTRAL_FF.24.43.40", plug_response[21], response[21])
    `A2_SOCKET_FF(plug_response_ff_22, "MISTRAL_FF.24.43.44", plug_response[22], response[22])
    `A2_SOCKET_FF(plug_response_ff_23, "MISTRAL_FF.24.43.46", plug_response[23], response[23])
    `A2_SOCKET_FF(plug_response_ff_24, "MISTRAL_FF.24.43.50", plug_response[24], response[24])
    `A2_SOCKET_FF(plug_response_ff_25, "MISTRAL_FF.24.43.52", plug_response[25], response[25])
    `A2_SOCKET_FF(plug_response_ff_26, "MISTRAL_FF.24.43.56", plug_response[26], response[26])
    `A2_SOCKET_FF(plug_response_ff_27, "MISTRAL_FF.24.43.58", plug_response[27], response[27])
`undef A2_SOCKET_FF
`endif
endmodule

module apple2_slot_socket7 (
    input  wire clock,
    input  wire [`A2_BUS_REQ-1:0] request,
    output wire [`A2_BUS_RSP-1:0] response,
    output wire [`A2_BUS_REQ-1:0] plug_request,
    input  wire [`A2_BUS_RSP-1:0] plug_response
);
`ifdef VERILATOR
    reg [`A2_BUS_REQ-1:0] request_q = 0;
    reg [`A2_BUS_RSP-1:0] response_q = 0;
    always @(posedge clock) begin
        request_q <= request;
        response_q <= plug_response;
    end
    assign plug_request = request_q;
    assign response = response_q;
`else
`define A2_SOCKET_FF(NAME, SITE, D, QOUT) \
    (* keep, BEL = SITE *) MISTRAL_FF NAME ( \
        .CLK(clock), .DATAIN(D), .Q(QOUT), .ACLR(1'b1), .ENA(1'b1), \
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep *) wire clock_coverage_unused;
    `A2_SOCKET_FF(clock_coverage_ff, "MISTRAL_FF.24.64.56", 1'b0, clock_coverage_unused)
    `A2_SOCKET_FF(plug_request_ff_0, "MISTRAL_FF.24.61.2", request[0], plug_request[0])
    `A2_SOCKET_FF(plug_request_ff_1, "MISTRAL_FF.24.61.4", request[1], plug_request[1])
    `A2_SOCKET_FF(plug_request_ff_2, "MISTRAL_FF.24.61.8", request[2], plug_request[2])
    `A2_SOCKET_FF(plug_request_ff_3, "MISTRAL_FF.24.61.10", request[3], plug_request[3])
    `A2_SOCKET_FF(plug_request_ff_4, "MISTRAL_FF.24.61.14", request[4], plug_request[4])
    `A2_SOCKET_FF(plug_request_ff_5, "MISTRAL_FF.24.61.16", request[5], plug_request[5])
    `A2_SOCKET_FF(plug_request_ff_6, "MISTRAL_FF.24.61.20", request[6], plug_request[6])
    `A2_SOCKET_FF(plug_request_ff_7, "MISTRAL_FF.24.61.22", request[7], plug_request[7])
    `A2_SOCKET_FF(plug_request_ff_8, "MISTRAL_FF.24.61.26", request[8], plug_request[8])
    `A2_SOCKET_FF(plug_request_ff_9, "MISTRAL_FF.24.61.28", request[9], plug_request[9])
    `A2_SOCKET_FF(plug_request_ff_10, "MISTRAL_FF.24.61.32", request[10], plug_request[10])
    `A2_SOCKET_FF(plug_request_ff_11, "MISTRAL_FF.24.61.34", request[11], plug_request[11])
    `A2_SOCKET_FF(plug_request_ff_12, "MISTRAL_FF.24.61.38", request[12], plug_request[12])
    `A2_SOCKET_FF(plug_request_ff_13, "MISTRAL_FF.24.61.40", request[13], plug_request[13])
    `A2_SOCKET_FF(plug_request_ff_14, "MISTRAL_FF.24.61.44", request[14], plug_request[14])
    `A2_SOCKET_FF(plug_request_ff_15, "MISTRAL_FF.24.61.46", request[15], plug_request[15])
    `A2_SOCKET_FF(plug_request_ff_16, "MISTRAL_FF.24.61.50", request[16], plug_request[16])
    `A2_SOCKET_FF(plug_request_ff_17, "MISTRAL_FF.24.61.52", request[17], plug_request[17])
    `A2_SOCKET_FF(plug_request_ff_18, "MISTRAL_FF.24.61.56", request[18], plug_request[18])
    `A2_SOCKET_FF(plug_request_ff_19, "MISTRAL_FF.24.61.58", request[19], plug_request[19])
    `A2_SOCKET_FF(plug_request_ff_20, "MISTRAL_FF.24.62.2", request[20], plug_request[20])
    `A2_SOCKET_FF(plug_request_ff_21, "MISTRAL_FF.24.62.4", request[21], plug_request[21])
    `A2_SOCKET_FF(plug_request_ff_22, "MISTRAL_FF.24.62.8", request[22], plug_request[22])
    `A2_SOCKET_FF(plug_request_ff_23, "MISTRAL_FF.24.62.10", request[23], plug_request[23])
    `A2_SOCKET_FF(plug_request_ff_24, "MISTRAL_FF.24.62.14", request[24], plug_request[24])
    `A2_SOCKET_FF(plug_request_ff_25, "MISTRAL_FF.24.62.16", request[25], plug_request[25])
    `A2_SOCKET_FF(plug_request_ff_26, "MISTRAL_FF.24.62.20", request[26], plug_request[26])
    `A2_SOCKET_FF(plug_request_ff_27, "MISTRAL_FF.24.62.22", request[27], plug_request[27])
    `A2_SOCKET_FF(plug_request_ff_28, "MISTRAL_FF.24.62.26", request[28], plug_request[28])
    `A2_SOCKET_FF(plug_request_ff_29, "MISTRAL_FF.24.62.28", request[29], plug_request[29])
    `A2_SOCKET_FF(plug_request_ff_30, "MISTRAL_FF.24.62.32", request[30], plug_request[30])
    `A2_SOCKET_FF(plug_request_ff_31, "MISTRAL_FF.24.62.34", request[31], plug_request[31])
    `A2_SOCKET_FF(plug_response_ff_0, "MISTRAL_FF.24.62.38", plug_response[0], response[0])
    `A2_SOCKET_FF(plug_response_ff_1, "MISTRAL_FF.24.62.40", plug_response[1], response[1])
    `A2_SOCKET_FF(plug_response_ff_2, "MISTRAL_FF.24.62.44", plug_response[2], response[2])
    `A2_SOCKET_FF(plug_response_ff_3, "MISTRAL_FF.24.62.46", plug_response[3], response[3])
    `A2_SOCKET_FF(plug_response_ff_4, "MISTRAL_FF.24.62.50", plug_response[4], response[4])
    `A2_SOCKET_FF(plug_response_ff_5, "MISTRAL_FF.24.62.52", plug_response[5], response[5])
    `A2_SOCKET_FF(plug_response_ff_6, "MISTRAL_FF.24.62.56", plug_response[6], response[6])
    `A2_SOCKET_FF(plug_response_ff_7, "MISTRAL_FF.24.62.58", plug_response[7], response[7])
    `A2_SOCKET_FF(plug_response_ff_8, "MISTRAL_FF.24.63.2", plug_response[8], response[8])
    `A2_SOCKET_FF(plug_response_ff_9, "MISTRAL_FF.24.63.4", plug_response[9], response[9])
    `A2_SOCKET_FF(plug_response_ff_10, "MISTRAL_FF.24.63.8", plug_response[10], response[10])
    `A2_SOCKET_FF(plug_response_ff_11, "MISTRAL_FF.24.63.10", plug_response[11], response[11])
    `A2_SOCKET_FF(plug_response_ff_12, "MISTRAL_FF.24.63.14", plug_response[12], response[12])
    `A2_SOCKET_FF(plug_response_ff_13, "MISTRAL_FF.24.63.16", plug_response[13], response[13])
    `A2_SOCKET_FF(plug_response_ff_14, "MISTRAL_FF.24.63.20", plug_response[14], response[14])
    `A2_SOCKET_FF(plug_response_ff_15, "MISTRAL_FF.24.63.22", plug_response[15], response[15])
    `A2_SOCKET_FF(plug_response_ff_16, "MISTRAL_FF.24.63.26", plug_response[16], response[16])
    `A2_SOCKET_FF(plug_response_ff_17, "MISTRAL_FF.24.63.28", plug_response[17], response[17])
    `A2_SOCKET_FF(plug_response_ff_18, "MISTRAL_FF.24.63.32", plug_response[18], response[18])
    `A2_SOCKET_FF(plug_response_ff_19, "MISTRAL_FF.24.63.34", plug_response[19], response[19])
    `A2_SOCKET_FF(plug_response_ff_20, "MISTRAL_FF.24.63.38", plug_response[20], response[20])
    `A2_SOCKET_FF(plug_response_ff_21, "MISTRAL_FF.24.63.40", plug_response[21], response[21])
    `A2_SOCKET_FF(plug_response_ff_22, "MISTRAL_FF.24.63.44", plug_response[22], response[22])
    `A2_SOCKET_FF(plug_response_ff_23, "MISTRAL_FF.24.63.46", plug_response[23], response[23])
    `A2_SOCKET_FF(plug_response_ff_24, "MISTRAL_FF.24.63.50", plug_response[24], response[24])
    `A2_SOCKET_FF(plug_response_ff_25, "MISTRAL_FF.24.63.52", plug_response[25], response[25])
    `A2_SOCKET_FF(plug_response_ff_26, "MISTRAL_FF.24.63.56", plug_response[26], response[26])
    `A2_SOCKET_FF(plug_response_ff_27, "MISTRAL_FF.24.63.58", plug_response[27], response[27])
`undef A2_SOCKET_FF
`endif
endmodule
