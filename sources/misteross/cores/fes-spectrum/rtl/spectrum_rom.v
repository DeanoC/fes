// SPDX-License-Identifier: GPL-2.0-or-later
// Blank 16 KiB ZX Spectrum ROM, linked at download time.
//
// The linked source is the CPU's $0000-$3FFF window. Each 1 KiB lane is an
// individually placed, empty 1024x10 M10K (column 5, rows 32-47) that the
// ROM map authenticates against the routed BELs. The data read is registered
// twice. No Sinclair ROM bytes are in this repository or the package.
module spectrum_rom #(
    parameter ROM_FILE = "build/diagnostics/fes-spectrum/firmware.hex"
) (
    input  wire        clk,
    input  wire [13:0] address,
    output reg  [7:0]  data
);
    wire [7:0] source_data;
`ifdef VERILATOR
    reg [7:0] memory [0:16383];
    initial $readmemh(ROM_FILE, memory);
    reg [7:0] sim_stage;
    always @(posedge clk) sim_stage <= memory[address];
    assign source_data = sim_stage;
`else
    wire [9:0] lane_addr = address[9:0];
    wire [9:0] q0;
    wire [9:0] q1;
    wire [9:0] q2;
    wire [9:0] q3;
    wire [9:0] q4;
    wire [9:0] q5;
    wire [9:0] q6;
    wire [9:0] q7;
    wire [9:0] q8;
    wire [9:0] q9;
    wire [9:0] q10;
    wire [9:0] q11;
    wire [9:0] q12;
    wire [9:0] q13;
    wire [9:0] q14;
    wire [9:0] q15;
    reg [7:0] group0_q;
    reg [7:0] group1_q;
    reg group_d;
    always @(posedge clk) begin
        case (address[12:10])
            3'd0: group0_q <= q0[7:0];
            3'd1: group0_q <= q1[7:0];
            3'd2: group0_q <= q2[7:0];
            3'd3: group0_q <= q3[7:0];
            3'd4: group0_q <= q4[7:0];
            3'd5: group0_q <= q5[7:0];
            3'd6: group0_q <= q6[7:0];
            3'd7: group0_q <= q7[7:0];
        endcase
        case (address[12:10])
            3'd0: group1_q <= q8[7:0];
            3'd1: group1_q <= q9[7:0];
            3'd2: group1_q <= q10[7:0];
            3'd3: group1_q <= q11[7:0];
            3'd4: group1_q <= q12[7:0];
            3'd5: group1_q <= q13[7:0];
            3'd6: group1_q <= q14[7:0];
            3'd7: group1_q <= q15[7:0];
        endcase
        group_d <= address[13];
    end
    assign source_data = group_d ? group1_q : group0_q;
    (* keep, BEL = "MISTRAL_M10K.5.32.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane0 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q0), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.33.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane1 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q1), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.34.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane2 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q2), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.35.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane3 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q3), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.36.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane4 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q4), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.37.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane5 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q5), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.38.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane6 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q6), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.39.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane7 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q7), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.40.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane8 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q8), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.41.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane9 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q9), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.42.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane10 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q10), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.43.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane11 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q11), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.44.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane12 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q12), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.45.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane13 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q13), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.46.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane14 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q14), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.47.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane15 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q15), .ACLR0(1'b0), .ACLR1(1'b0)
    );
`endif
    always @(posedge clk)
        data <= source_data;
endmodule
