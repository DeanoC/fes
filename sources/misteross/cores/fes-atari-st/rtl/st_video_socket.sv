// SPDX-License-Identifier: GPL-2.0-or-later
// Closed ST raster video socket; checked by scripts/atari_st_video_parts.py.
module st_video_socket (
    input wire clock, input wire [31:0] request, output wire [27:0] response,
    output wire [31:0] plug_request, input wire [27:0] plug_response
);
`ifdef VERILATOR
    reg [31:0] request_q = 0;
    reg [27:0] response_q = 0;
    always @(posedge clock) begin
        request_q <= request; response_q <= plug_response;
    end
    assign plug_request = request_q;
    assign response = response_q;
`else
    // Permanent in-slot loads give every request FF a routed egress before
    // freezing the shell. Cart routes can extend the retained input branches.
    (* keep *) wire [31:0] request_egress_unused;
    (* keep, BEL = "MISTRAL_MCOMB.25.44.0" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_0 (
        .A(plug_request[0]), .B(1'b0), .Q(request_egress_unused[0]));
    (* keep, BEL = "MISTRAL_MCOMB.25.44.1" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_1 (
        .A(plug_request[1]), .B(1'b0), .Q(request_egress_unused[1]));
    (* keep, BEL = "MISTRAL_MCOMB.25.44.6" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_2 (
        .A(plug_request[2]), .B(1'b0), .Q(request_egress_unused[2]));
    (* keep, BEL = "MISTRAL_MCOMB.25.44.7" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_3 (
        .A(plug_request[3]), .B(1'b0), .Q(request_egress_unused[3]));
    (* keep, BEL = "MISTRAL_MCOMB.25.44.12" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_4 (
        .A(plug_request[4]), .B(1'b0), .Q(request_egress_unused[4]));
    (* keep, BEL = "MISTRAL_MCOMB.25.44.13" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_5 (
        .A(plug_request[5]), .B(1'b0), .Q(request_egress_unused[5]));
    (* keep, BEL = "MISTRAL_MCOMB.25.44.18" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_6 (
        .A(plug_request[6]), .B(1'b0), .Q(request_egress_unused[6]));
    (* keep, BEL = "MISTRAL_MCOMB.25.44.19" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_7 (
        .A(plug_request[7]), .B(1'b0), .Q(request_egress_unused[7]));
    (* keep, BEL = "MISTRAL_MCOMB.25.44.24" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_8 (
        .A(plug_request[8]), .B(1'b0), .Q(request_egress_unused[8]));
    (* keep, BEL = "MISTRAL_MCOMB.25.44.25" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_9 (
        .A(plug_request[9]), .B(1'b0), .Q(request_egress_unused[9]));
    (* keep, BEL = "MISTRAL_MCOMB.25.44.30" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_10 (
        .A(plug_request[10]), .B(1'b0), .Q(request_egress_unused[10]));
    (* keep, BEL = "MISTRAL_MCOMB.25.44.31" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_11 (
        .A(plug_request[11]), .B(1'b0), .Q(request_egress_unused[11]));
    (* keep, BEL = "MISTRAL_MCOMB.25.44.36" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_12 (
        .A(plug_request[12]), .B(1'b0), .Q(request_egress_unused[12]));
    (* keep, BEL = "MISTRAL_MCOMB.25.44.37" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_13 (
        .A(plug_request[13]), .B(1'b0), .Q(request_egress_unused[13]));
    (* keep, BEL = "MISTRAL_MCOMB.25.44.42" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_14 (
        .A(plug_request[14]), .B(1'b0), .Q(request_egress_unused[14]));
    (* keep, BEL = "MISTRAL_MCOMB.25.44.43" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_15 (
        .A(plug_request[15]), .B(1'b0), .Q(request_egress_unused[15]));
    (* keep, BEL = "MISTRAL_MCOMB.25.44.48" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_16 (
        .A(plug_request[16]), .B(1'b0), .Q(request_egress_unused[16]));
    (* keep, BEL = "MISTRAL_MCOMB.25.44.49" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_17 (
        .A(plug_request[17]), .B(1'b0), .Q(request_egress_unused[17]));
    (* keep, BEL = "MISTRAL_MCOMB.25.44.54" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_18 (
        .A(plug_request[18]), .B(1'b0), .Q(request_egress_unused[18]));
    (* keep, BEL = "MISTRAL_MCOMB.25.44.55" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_19 (
        .A(plug_request[19]), .B(1'b0), .Q(request_egress_unused[19]));
    (* keep, BEL = "MISTRAL_MCOMB.25.45.0" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_20 (
        .A(plug_request[20]), .B(1'b0), .Q(request_egress_unused[20]));
    (* keep, BEL = "MISTRAL_MCOMB.25.45.1" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_21 (
        .A(plug_request[21]), .B(1'b0), .Q(request_egress_unused[21]));
    (* keep, BEL = "MISTRAL_MCOMB.25.45.6" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_22 (
        .A(plug_request[22]), .B(1'b0), .Q(request_egress_unused[22]));
    (* keep, BEL = "MISTRAL_MCOMB.25.45.7" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_23 (
        .A(plug_request[23]), .B(1'b0), .Q(request_egress_unused[23]));
    (* keep, BEL = "MISTRAL_MCOMB.25.45.12" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_24 (
        .A(plug_request[24]), .B(1'b0), .Q(request_egress_unused[24]));
    (* keep, BEL = "MISTRAL_MCOMB.25.45.13" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_25 (
        .A(plug_request[25]), .B(1'b0), .Q(request_egress_unused[25]));
    (* keep, BEL = "MISTRAL_MCOMB.25.45.18" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_26 (
        .A(plug_request[26]), .B(1'b0), .Q(request_egress_unused[26]));
    (* keep, BEL = "MISTRAL_MCOMB.25.45.19" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_27 (
        .A(plug_request[27]), .B(1'b0), .Q(request_egress_unused[27]));
    (* keep, BEL = "MISTRAL_MCOMB.25.45.24" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_28 (
        .A(plug_request[28]), .B(1'b0), .Q(request_egress_unused[28]));
    (* keep, BEL = "MISTRAL_MCOMB.25.45.25" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_29 (
        .A(plug_request[29]), .B(1'b0), .Q(request_egress_unused[29]));
    (* keep, BEL = "MISTRAL_MCOMB.25.45.30" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_30 (
        .A(plug_request[30]), .B(1'b0), .Q(request_egress_unused[30]));
    (* keep, BEL = "MISTRAL_MCOMB.25.45.31" *) MISTRAL_ALUT2 #(.LUT(4'hA)) request_egress_anchor_31 (
        .A(plug_request[31]), .B(1'b0), .Q(request_egress_unused[31]));
    (* keep *) wire [32:0] clock_coverage_unused;
    (* keep, BEL = "MISTRAL_FF.24.41.2" *) MISTRAL_FF plug_request_ff_0 (
        .CLK(clock), .DATAIN(request[0]), .Q(plug_request[0]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.41.4" *) MISTRAL_FF plug_request_ff_1 (
        .CLK(clock), .DATAIN(request[1]), .Q(plug_request[1]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.41.8" *) MISTRAL_FF plug_request_ff_2 (
        .CLK(clock), .DATAIN(request[2]), .Q(plug_request[2]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.41.10" *) MISTRAL_FF plug_request_ff_3 (
        .CLK(clock), .DATAIN(request[3]), .Q(plug_request[3]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.41.14" *) MISTRAL_FF plug_request_ff_4 (
        .CLK(clock), .DATAIN(request[4]), .Q(plug_request[4]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.41.16" *) MISTRAL_FF plug_request_ff_5 (
        .CLK(clock), .DATAIN(request[5]), .Q(plug_request[5]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.41.20" *) MISTRAL_FF plug_request_ff_6 (
        .CLK(clock), .DATAIN(request[6]), .Q(plug_request[6]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.41.22" *) MISTRAL_FF plug_request_ff_7 (
        .CLK(clock), .DATAIN(request[7]), .Q(plug_request[7]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.41.26" *) MISTRAL_FF plug_request_ff_8 (
        .CLK(clock), .DATAIN(request[8]), .Q(plug_request[8]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.41.28" *) MISTRAL_FF plug_request_ff_9 (
        .CLK(clock), .DATAIN(request[9]), .Q(plug_request[9]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.41.32" *) MISTRAL_FF plug_request_ff_10 (
        .CLK(clock), .DATAIN(request[10]), .Q(plug_request[10]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.41.34" *) MISTRAL_FF plug_request_ff_11 (
        .CLK(clock), .DATAIN(request[11]), .Q(plug_request[11]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.41.38" *) MISTRAL_FF plug_request_ff_12 (
        .CLK(clock), .DATAIN(request[12]), .Q(plug_request[12]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.41.40" *) MISTRAL_FF plug_request_ff_13 (
        .CLK(clock), .DATAIN(request[13]), .Q(plug_request[13]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.41.44" *) MISTRAL_FF plug_request_ff_14 (
        .CLK(clock), .DATAIN(request[14]), .Q(plug_request[14]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.41.46" *) MISTRAL_FF plug_request_ff_15 (
        .CLK(clock), .DATAIN(request[15]), .Q(plug_request[15]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.41.50" *) MISTRAL_FF plug_request_ff_16 (
        .CLK(clock), .DATAIN(request[16]), .Q(plug_request[16]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.41.52" *) MISTRAL_FF plug_request_ff_17 (
        .CLK(clock), .DATAIN(request[17]), .Q(plug_request[17]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.41.56" *) MISTRAL_FF plug_request_ff_18 (
        .CLK(clock), .DATAIN(request[18]), .Q(plug_request[18]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.41.58" *) MISTRAL_FF plug_request_ff_19 (
        .CLK(clock), .DATAIN(request[19]), .Q(plug_request[19]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.42.2" *) MISTRAL_FF plug_request_ff_20 (
        .CLK(clock), .DATAIN(request[20]), .Q(plug_request[20]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.42.4" *) MISTRAL_FF plug_request_ff_21 (
        .CLK(clock), .DATAIN(request[21]), .Q(plug_request[21]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.42.8" *) MISTRAL_FF plug_request_ff_22 (
        .CLK(clock), .DATAIN(request[22]), .Q(plug_request[22]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.42.10" *) MISTRAL_FF plug_request_ff_23 (
        .CLK(clock), .DATAIN(request[23]), .Q(plug_request[23]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.42.14" *) MISTRAL_FF plug_request_ff_24 (
        .CLK(clock), .DATAIN(request[24]), .Q(plug_request[24]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.42.16" *) MISTRAL_FF plug_request_ff_25 (
        .CLK(clock), .DATAIN(request[25]), .Q(plug_request[25]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.42.20" *) MISTRAL_FF plug_request_ff_26 (
        .CLK(clock), .DATAIN(request[26]), .Q(plug_request[26]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.42.22" *) MISTRAL_FF plug_request_ff_27 (
        .CLK(clock), .DATAIN(request[27]), .Q(plug_request[27]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.42.26" *) MISTRAL_FF plug_request_ff_28 (
        .CLK(clock), .DATAIN(request[28]), .Q(plug_request[28]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.42.28" *) MISTRAL_FF plug_request_ff_29 (
        .CLK(clock), .DATAIN(request[29]), .Q(plug_request[29]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.42.32" *) MISTRAL_FF plug_request_ff_30 (
        .CLK(clock), .DATAIN(request[30]), .Q(plug_request[30]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.42.34" *) MISTRAL_FF plug_request_ff_31 (
        .CLK(clock), .DATAIN(request[31]), .Q(plug_request[31]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.42.38" *) MISTRAL_FF plug_response_ff_0 (
        .CLK(clock), .DATAIN(plug_response[0]), .Q(response[0]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.42.40" *) MISTRAL_FF plug_response_ff_1 (
        .CLK(clock), .DATAIN(plug_response[1]), .Q(response[1]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.42.44" *) MISTRAL_FF plug_response_ff_2 (
        .CLK(clock), .DATAIN(plug_response[2]), .Q(response[2]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.42.46" *) MISTRAL_FF plug_response_ff_3 (
        .CLK(clock), .DATAIN(plug_response[3]), .Q(response[3]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.42.50" *) MISTRAL_FF plug_response_ff_4 (
        .CLK(clock), .DATAIN(plug_response[4]), .Q(response[4]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.42.52" *) MISTRAL_FF plug_response_ff_5 (
        .CLK(clock), .DATAIN(plug_response[5]), .Q(response[5]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.42.56" *) MISTRAL_FF plug_response_ff_6 (
        .CLK(clock), .DATAIN(plug_response[6]), .Q(response[6]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.42.58" *) MISTRAL_FF plug_response_ff_7 (
        .CLK(clock), .DATAIN(plug_response[7]), .Q(response[7]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.43.2" *) MISTRAL_FF plug_response_ff_8 (
        .CLK(clock), .DATAIN(plug_response[8]), .Q(response[8]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.43.4" *) MISTRAL_FF plug_response_ff_9 (
        .CLK(clock), .DATAIN(plug_response[9]), .Q(response[9]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.43.8" *) MISTRAL_FF plug_response_ff_10 (
        .CLK(clock), .DATAIN(plug_response[10]), .Q(response[10]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.43.10" *) MISTRAL_FF plug_response_ff_11 (
        .CLK(clock), .DATAIN(plug_response[11]), .Q(response[11]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.43.14" *) MISTRAL_FF plug_response_ff_12 (
        .CLK(clock), .DATAIN(plug_response[12]), .Q(response[12]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.43.16" *) MISTRAL_FF plug_response_ff_13 (
        .CLK(clock), .DATAIN(plug_response[13]), .Q(response[13]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.43.20" *) MISTRAL_FF plug_response_ff_14 (
        .CLK(clock), .DATAIN(plug_response[14]), .Q(response[14]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.43.22" *) MISTRAL_FF plug_response_ff_15 (
        .CLK(clock), .DATAIN(plug_response[15]), .Q(response[15]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.43.26" *) MISTRAL_FF plug_response_ff_16 (
        .CLK(clock), .DATAIN(plug_response[16]), .Q(response[16]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.43.28" *) MISTRAL_FF plug_response_ff_17 (
        .CLK(clock), .DATAIN(plug_response[17]), .Q(response[17]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.43.32" *) MISTRAL_FF plug_response_ff_18 (
        .CLK(clock), .DATAIN(plug_response[18]), .Q(response[18]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.43.34" *) MISTRAL_FF plug_response_ff_19 (
        .CLK(clock), .DATAIN(plug_response[19]), .Q(response[19]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.43.38" *) MISTRAL_FF plug_response_ff_20 (
        .CLK(clock), .DATAIN(plug_response[20]), .Q(response[20]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.43.40" *) MISTRAL_FF plug_response_ff_21 (
        .CLK(clock), .DATAIN(plug_response[21]), .Q(response[21]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.43.44" *) MISTRAL_FF plug_response_ff_22 (
        .CLK(clock), .DATAIN(plug_response[22]), .Q(response[22]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.43.46" *) MISTRAL_FF plug_response_ff_23 (
        .CLK(clock), .DATAIN(plug_response[23]), .Q(response[23]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.43.50" *) MISTRAL_FF plug_response_ff_24 (
        .CLK(clock), .DATAIN(plug_response[24]), .Q(response[24]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.43.52" *) MISTRAL_FF plug_response_ff_25 (
        .CLK(clock), .DATAIN(plug_response[25]), .Q(response[25]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.43.56" *) MISTRAL_FF plug_response_ff_26 (
        .CLK(clock), .DATAIN(plug_response[26]), .Q(response[26]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.43.58" *) MISTRAL_FF plug_response_ff_27 (
        .CLK(clock), .DATAIN(plug_response[27]), .Q(response[27]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.44.56" *) MISTRAL_FF clock_coverage_ff_0 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[0]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.45.56" *) MISTRAL_FF clock_coverage_ff_1 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[1]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.46.56" *) MISTRAL_FF clock_coverage_ff_2 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[2]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.47.56" *) MISTRAL_FF clock_coverage_ff_3 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[3]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.48.56" *) MISTRAL_FF clock_coverage_ff_4 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[4]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.49.56" *) MISTRAL_FF clock_coverage_ff_5 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[5]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.50.56" *) MISTRAL_FF clock_coverage_ff_6 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[6]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.51.56" *) MISTRAL_FF clock_coverage_ff_7 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[7]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.52.56" *) MISTRAL_FF clock_coverage_ff_8 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[8]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.53.56" *) MISTRAL_FF clock_coverage_ff_9 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[9]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.54.56" *) MISTRAL_FF clock_coverage_ff_10 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[10]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.55.56" *) MISTRAL_FF clock_coverage_ff_11 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[11]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.56.56" *) MISTRAL_FF clock_coverage_ff_12 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[12]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.57.56" *) MISTRAL_FF clock_coverage_ff_13 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[13]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.58.56" *) MISTRAL_FF clock_coverage_ff_14 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[14]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.41.56" *) MISTRAL_FF clock_coverage_ff_15 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[15]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.42.56" *) MISTRAL_FF clock_coverage_ff_16 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[16]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.43.56" *) MISTRAL_FF clock_coverage_ff_17 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[17]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.44.56" *) MISTRAL_FF clock_coverage_ff_18 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[18]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.45.56" *) MISTRAL_FF clock_coverage_ff_19 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[19]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.46.56" *) MISTRAL_FF clock_coverage_ff_20 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[20]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.47.56" *) MISTRAL_FF clock_coverage_ff_21 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[21]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.48.56" *) MISTRAL_FF clock_coverage_ff_22 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[22]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.49.56" *) MISTRAL_FF clock_coverage_ff_23 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[23]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.50.56" *) MISTRAL_FF clock_coverage_ff_24 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[24]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.51.56" *) MISTRAL_FF clock_coverage_ff_25 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[25]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.52.56" *) MISTRAL_FF clock_coverage_ff_26 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[26]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.53.56" *) MISTRAL_FF clock_coverage_ff_27 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[27]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.54.56" *) MISTRAL_FF clock_coverage_ff_28 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[28]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.55.56" *) MISTRAL_FF clock_coverage_ff_29 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[29]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.56.56" *) MISTRAL_FF clock_coverage_ff_30 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[30]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.57.56" *) MISTRAL_FF clock_coverage_ff_31 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[31]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.58.56" *) MISTRAL_FF clock_coverage_ff_32 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[32]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
`endif
endmodule
