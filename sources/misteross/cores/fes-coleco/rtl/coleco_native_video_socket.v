// SPDX-License-Identifier: GPL-2.0-or-later
// Closed Coleco native-pixel socket; scripts/native_video_parts.py checks the physical sites.
// Shell owns the source CDC; both boundary banks use the 74.25 MHz pixel clock.
module coleco_native_video_socket (
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
    (* keep *) wire [92:0] clock_coverage_unused;
    (* keep, BEL = "MISTRAL_FF.24.23.2" *) MISTRAL_FF plug_request_ff_0 (
        .CLK(clock), .DATAIN(request[0]), .Q(plug_request[0]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.23.4" *) MISTRAL_FF plug_request_ff_1 (
        .CLK(clock), .DATAIN(request[1]), .Q(plug_request[1]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.23.8" *) MISTRAL_FF plug_request_ff_2 (
        .CLK(clock), .DATAIN(request[2]), .Q(plug_request[2]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.23.10" *) MISTRAL_FF plug_request_ff_3 (
        .CLK(clock), .DATAIN(request[3]), .Q(plug_request[3]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.23.14" *) MISTRAL_FF plug_request_ff_4 (
        .CLK(clock), .DATAIN(request[4]), .Q(plug_request[4]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.23.16" *) MISTRAL_FF plug_request_ff_5 (
        .CLK(clock), .DATAIN(request[5]), .Q(plug_request[5]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.23.20" *) MISTRAL_FF plug_request_ff_6 (
        .CLK(clock), .DATAIN(request[6]), .Q(plug_request[6]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.23.22" *) MISTRAL_FF plug_request_ff_7 (
        .CLK(clock), .DATAIN(request[7]), .Q(plug_request[7]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.23.26" *) MISTRAL_FF plug_request_ff_8 (
        .CLK(clock), .DATAIN(request[8]), .Q(plug_request[8]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.23.28" *) MISTRAL_FF plug_request_ff_9 (
        .CLK(clock), .DATAIN(request[9]), .Q(plug_request[9]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.23.32" *) MISTRAL_FF plug_request_ff_10 (
        .CLK(clock), .DATAIN(request[10]), .Q(plug_request[10]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.23.34" *) MISTRAL_FF plug_request_ff_11 (
        .CLK(clock), .DATAIN(request[11]), .Q(plug_request[11]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.23.38" *) MISTRAL_FF plug_request_ff_12 (
        .CLK(clock), .DATAIN(request[12]), .Q(plug_request[12]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.23.40" *) MISTRAL_FF plug_request_ff_13 (
        .CLK(clock), .DATAIN(request[13]), .Q(plug_request[13]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.23.44" *) MISTRAL_FF plug_request_ff_14 (
        .CLK(clock), .DATAIN(request[14]), .Q(plug_request[14]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.23.46" *) MISTRAL_FF plug_request_ff_15 (
        .CLK(clock), .DATAIN(request[15]), .Q(plug_request[15]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.23.50" *) MISTRAL_FF plug_request_ff_16 (
        .CLK(clock), .DATAIN(request[16]), .Q(plug_request[16]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.23.52" *) MISTRAL_FF plug_request_ff_17 (
        .CLK(clock), .DATAIN(request[17]), .Q(plug_request[17]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.23.56" *) MISTRAL_FF plug_request_ff_18 (
        .CLK(clock), .DATAIN(request[18]), .Q(plug_request[18]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.23.58" *) MISTRAL_FF plug_request_ff_19 (
        .CLK(clock), .DATAIN(request[19]), .Q(plug_request[19]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.24.2" *) MISTRAL_FF plug_request_ff_20 (
        .CLK(clock), .DATAIN(request[20]), .Q(plug_request[20]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.24.4" *) MISTRAL_FF plug_request_ff_21 (
        .CLK(clock), .DATAIN(request[21]), .Q(plug_request[21]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.24.8" *) MISTRAL_FF plug_request_ff_22 (
        .CLK(clock), .DATAIN(request[22]), .Q(plug_request[22]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.24.10" *) MISTRAL_FF plug_request_ff_23 (
        .CLK(clock), .DATAIN(request[23]), .Q(plug_request[23]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.24.14" *) MISTRAL_FF plug_request_ff_24 (
        .CLK(clock), .DATAIN(request[24]), .Q(plug_request[24]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.24.16" *) MISTRAL_FF plug_request_ff_25 (
        .CLK(clock), .DATAIN(request[25]), .Q(plug_request[25]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.24.20" *) MISTRAL_FF plug_request_ff_26 (
        .CLK(clock), .DATAIN(request[26]), .Q(plug_request[26]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.24.22" *) MISTRAL_FF plug_request_ff_27 (
        .CLK(clock), .DATAIN(request[27]), .Q(plug_request[27]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.24.26" *) MISTRAL_FF plug_request_ff_28 (
        .CLK(clock), .DATAIN(request[28]), .Q(plug_request[28]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.24.28" *) MISTRAL_FF plug_request_ff_29 (
        .CLK(clock), .DATAIN(request[29]), .Q(plug_request[29]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.24.32" *) MISTRAL_FF plug_request_ff_30 (
        .CLK(clock), .DATAIN(request[30]), .Q(plug_request[30]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.24.34" *) MISTRAL_FF plug_request_ff_31 (
        .CLK(clock), .DATAIN(request[31]), .Q(plug_request[31]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.24.38" *) MISTRAL_FF plug_response_ff_0 (
        .CLK(clock), .DATAIN(plug_response[0]), .Q(response[0]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.24.40" *) MISTRAL_FF plug_response_ff_1 (
        .CLK(clock), .DATAIN(plug_response[1]), .Q(response[1]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.24.44" *) MISTRAL_FF plug_response_ff_2 (
        .CLK(clock), .DATAIN(plug_response[2]), .Q(response[2]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.24.46" *) MISTRAL_FF plug_response_ff_3 (
        .CLK(clock), .DATAIN(plug_response[3]), .Q(response[3]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.24.50" *) MISTRAL_FF plug_response_ff_4 (
        .CLK(clock), .DATAIN(plug_response[4]), .Q(response[4]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.24.52" *) MISTRAL_FF plug_response_ff_5 (
        .CLK(clock), .DATAIN(plug_response[5]), .Q(response[5]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.24.56" *) MISTRAL_FF plug_response_ff_6 (
        .CLK(clock), .DATAIN(plug_response[6]), .Q(response[6]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.24.58" *) MISTRAL_FF plug_response_ff_7 (
        .CLK(clock), .DATAIN(plug_response[7]), .Q(response[7]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.25.2" *) MISTRAL_FF plug_response_ff_8 (
        .CLK(clock), .DATAIN(plug_response[8]), .Q(response[8]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.25.4" *) MISTRAL_FF plug_response_ff_9 (
        .CLK(clock), .DATAIN(plug_response[9]), .Q(response[9]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.25.8" *) MISTRAL_FF plug_response_ff_10 (
        .CLK(clock), .DATAIN(plug_response[10]), .Q(response[10]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.25.10" *) MISTRAL_FF plug_response_ff_11 (
        .CLK(clock), .DATAIN(plug_response[11]), .Q(response[11]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.25.14" *) MISTRAL_FF plug_response_ff_12 (
        .CLK(clock), .DATAIN(plug_response[12]), .Q(response[12]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.25.16" *) MISTRAL_FF plug_response_ff_13 (
        .CLK(clock), .DATAIN(plug_response[13]), .Q(response[13]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.25.20" *) MISTRAL_FF plug_response_ff_14 (
        .CLK(clock), .DATAIN(plug_response[14]), .Q(response[14]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.25.22" *) MISTRAL_FF plug_response_ff_15 (
        .CLK(clock), .DATAIN(plug_response[15]), .Q(response[15]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.25.26" *) MISTRAL_FF plug_response_ff_16 (
        .CLK(clock), .DATAIN(plug_response[16]), .Q(response[16]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.25.28" *) MISTRAL_FF plug_response_ff_17 (
        .CLK(clock), .DATAIN(plug_response[17]), .Q(response[17]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.25.32" *) MISTRAL_FF plug_response_ff_18 (
        .CLK(clock), .DATAIN(plug_response[18]), .Q(response[18]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.25.34" *) MISTRAL_FF plug_response_ff_19 (
        .CLK(clock), .DATAIN(plug_response[19]), .Q(response[19]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.25.38" *) MISTRAL_FF plug_response_ff_20 (
        .CLK(clock), .DATAIN(plug_response[20]), .Q(response[20]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.25.40" *) MISTRAL_FF plug_response_ff_21 (
        .CLK(clock), .DATAIN(plug_response[21]), .Q(response[21]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.25.44" *) MISTRAL_FF plug_response_ff_22 (
        .CLK(clock), .DATAIN(plug_response[22]), .Q(response[22]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.25.46" *) MISTRAL_FF plug_response_ff_23 (
        .CLK(clock), .DATAIN(plug_response[23]), .Q(response[23]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.25.50" *) MISTRAL_FF plug_response_ff_24 (
        .CLK(clock), .DATAIN(plug_response[24]), .Q(response[24]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.25.52" *) MISTRAL_FF plug_response_ff_25 (
        .CLK(clock), .DATAIN(plug_response[25]), .Q(response[25]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.25.56" *) MISTRAL_FF plug_response_ff_26 (
        .CLK(clock), .DATAIN(plug_response[26]), .Q(response[26]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.25.58" *) MISTRAL_FF plug_response_ff_27 (
        .CLK(clock), .DATAIN(plug_response[27]), .Q(response[27]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.10.23.56" *) MISTRAL_FF clock_coverage_ff_0 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[0]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.10.24.56" *) MISTRAL_FF clock_coverage_ff_1 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[1]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.10.25.56" *) MISTRAL_FF clock_coverage_ff_2 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[2]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.10.26.56" *) MISTRAL_FF clock_coverage_ff_3 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[3]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.10.27.56" *) MISTRAL_FF clock_coverage_ff_4 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[4]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.10.28.56" *) MISTRAL_FF clock_coverage_ff_5 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[5]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.10.29.56" *) MISTRAL_FF clock_coverage_ff_6 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[6]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.10.30.56" *) MISTRAL_FF clock_coverage_ff_7 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[7]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.10.31.56" *) MISTRAL_FF clock_coverage_ff_8 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[8]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.10.32.56" *) MISTRAL_FF clock_coverage_ff_9 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[9]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.10.33.56" *) MISTRAL_FF clock_coverage_ff_10 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[10]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.10.34.56" *) MISTRAL_FF clock_coverage_ff_11 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[11]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.10.35.56" *) MISTRAL_FF clock_coverage_ff_12 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[12]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.10.36.56" *) MISTRAL_FF clock_coverage_ff_13 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[13]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.10.37.56" *) MISTRAL_FF clock_coverage_ff_14 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[14]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.10.38.56" *) MISTRAL_FF clock_coverage_ff_15 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[15]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.18.23.56" *) MISTRAL_FF clock_coverage_ff_16 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[16]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.18.24.56" *) MISTRAL_FF clock_coverage_ff_17 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[17]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.18.25.56" *) MISTRAL_FF clock_coverage_ff_18 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[18]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.18.26.56" *) MISTRAL_FF clock_coverage_ff_19 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[19]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.18.27.56" *) MISTRAL_FF clock_coverage_ff_20 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[20]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.18.28.56" *) MISTRAL_FF clock_coverage_ff_21 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[21]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.18.29.56" *) MISTRAL_FF clock_coverage_ff_22 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[22]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.18.30.56" *) MISTRAL_FF clock_coverage_ff_23 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[23]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.18.31.56" *) MISTRAL_FF clock_coverage_ff_24 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[24]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.18.32.56" *) MISTRAL_FF clock_coverage_ff_25 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[25]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.18.33.56" *) MISTRAL_FF clock_coverage_ff_26 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[26]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.18.34.56" *) MISTRAL_FF clock_coverage_ff_27 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[27]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.18.35.56" *) MISTRAL_FF clock_coverage_ff_28 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[28]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.18.36.56" *) MISTRAL_FF clock_coverage_ff_29 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[29]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.18.37.56" *) MISTRAL_FF clock_coverage_ff_30 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[30]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.18.38.56" *) MISTRAL_FF clock_coverage_ff_31 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[31]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.26.56" *) MISTRAL_FF clock_coverage_ff_32 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[32]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.27.56" *) MISTRAL_FF clock_coverage_ff_33 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[33]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.28.56" *) MISTRAL_FF clock_coverage_ff_34 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[34]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.29.56" *) MISTRAL_FF clock_coverage_ff_35 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[35]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.30.56" *) MISTRAL_FF clock_coverage_ff_36 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[36]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.31.56" *) MISTRAL_FF clock_coverage_ff_37 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[37]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.32.56" *) MISTRAL_FF clock_coverage_ff_38 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[38]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.33.56" *) MISTRAL_FF clock_coverage_ff_39 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[39]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.34.56" *) MISTRAL_FF clock_coverage_ff_40 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[40]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.35.56" *) MISTRAL_FF clock_coverage_ff_41 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[41]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.36.56" *) MISTRAL_FF clock_coverage_ff_42 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[42]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.37.56" *) MISTRAL_FF clock_coverage_ff_43 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[43]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.24.38.56" *) MISTRAL_FF clock_coverage_ff_44 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[44]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.23.56" *) MISTRAL_FF clock_coverage_ff_45 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[45]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.24.56" *) MISTRAL_FF clock_coverage_ff_46 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[46]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.25.56" *) MISTRAL_FF clock_coverage_ff_47 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[47]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.26.56" *) MISTRAL_FF clock_coverage_ff_48 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[48]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.27.56" *) MISTRAL_FF clock_coverage_ff_49 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[49]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.28.56" *) MISTRAL_FF clock_coverage_ff_50 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[50]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.29.56" *) MISTRAL_FF clock_coverage_ff_51 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[51]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.30.56" *) MISTRAL_FF clock_coverage_ff_52 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[52]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.31.56" *) MISTRAL_FF clock_coverage_ff_53 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[53]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.32.56" *) MISTRAL_FF clock_coverage_ff_54 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[54]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.33.56" *) MISTRAL_FF clock_coverage_ff_55 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[55]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.34.56" *) MISTRAL_FF clock_coverage_ff_56 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[56]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.35.56" *) MISTRAL_FF clock_coverage_ff_57 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[57]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.36.56" *) MISTRAL_FF clock_coverage_ff_58 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[58]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.37.56" *) MISTRAL_FF clock_coverage_ff_59 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[59]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.28.38.56" *) MISTRAL_FF clock_coverage_ff_60 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[60]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.35.23.56" *) MISTRAL_FF clock_coverage_ff_61 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[61]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.35.24.56" *) MISTRAL_FF clock_coverage_ff_62 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[62]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.35.25.56" *) MISTRAL_FF clock_coverage_ff_63 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[63]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.35.26.56" *) MISTRAL_FF clock_coverage_ff_64 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[64]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.35.27.56" *) MISTRAL_FF clock_coverage_ff_65 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[65]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.35.28.56" *) MISTRAL_FF clock_coverage_ff_66 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[66]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.35.29.56" *) MISTRAL_FF clock_coverage_ff_67 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[67]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.35.30.56" *) MISTRAL_FF clock_coverage_ff_68 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[68]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.35.31.56" *) MISTRAL_FF clock_coverage_ff_69 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[69]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.35.32.56" *) MISTRAL_FF clock_coverage_ff_70 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[70]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.35.33.56" *) MISTRAL_FF clock_coverage_ff_71 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[71]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.35.34.56" *) MISTRAL_FF clock_coverage_ff_72 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[72]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.35.35.56" *) MISTRAL_FF clock_coverage_ff_73 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[73]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.35.36.56" *) MISTRAL_FF clock_coverage_ff_74 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[74]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.35.37.56" *) MISTRAL_FF clock_coverage_ff_75 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[75]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.35.38.56" *) MISTRAL_FF clock_coverage_ff_76 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[76]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.37.23.56" *) MISTRAL_FF clock_coverage_ff_77 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[77]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.37.24.56" *) MISTRAL_FF clock_coverage_ff_78 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[78]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.37.25.56" *) MISTRAL_FF clock_coverage_ff_79 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[79]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.37.26.56" *) MISTRAL_FF clock_coverage_ff_80 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[80]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.37.27.56" *) MISTRAL_FF clock_coverage_ff_81 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[81]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.37.28.56" *) MISTRAL_FF clock_coverage_ff_82 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[82]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.37.29.56" *) MISTRAL_FF clock_coverage_ff_83 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[83]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.37.30.56" *) MISTRAL_FF clock_coverage_ff_84 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[84]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.37.31.56" *) MISTRAL_FF clock_coverage_ff_85 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[85]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.37.32.56" *) MISTRAL_FF clock_coverage_ff_86 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[86]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.37.33.56" *) MISTRAL_FF clock_coverage_ff_87 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[87]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.37.34.56" *) MISTRAL_FF clock_coverage_ff_88 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[88]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.37.35.56" *) MISTRAL_FF clock_coverage_ff_89 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[89]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.37.36.56" *) MISTRAL_FF clock_coverage_ff_90 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[90]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.37.37.56" *) MISTRAL_FF clock_coverage_ff_91 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[91]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep, BEL = "MISTRAL_FF.37.38.56" *) MISTRAL_FF clock_coverage_ff_92 (
        .CLK(clock), .DATAIN(1'b0), .Q(clock_coverage_unused[92]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
`endif
endmodule
