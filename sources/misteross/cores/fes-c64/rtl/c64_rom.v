// SPDX-License-Identifier: GPL-2.0-or-later
// Blank 16 KiB firmware window. Bytes 0..8191 are the BASIC window ($A000)
// and bytes 8192..16383 are the KERNAL window ($E000). Each 1 KiB lane is an
// empty 1024x10 M10K; the selected image is linked at download time.
module c64_rom (
    input  wire        clk,
    input  wire [13:0] address,
    output reg  [7:0]  data
);
`ifdef VERILATOR
    reg [7:0] memory [0:16383];
    initial $readmemh("build/diagnostics/fes-c64/firmware.hex", memory);
    always @(posedge clk) data <= memory[address];
`else
    wire [9:0] q0;
    (* keep, BEL = "MISTRAL_M10K.5.32.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane0 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(address[9:0]), .B1DATA(q0), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q1;
    (* keep, BEL = "MISTRAL_M10K.5.33.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane1 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(address[9:0]), .B1DATA(q1), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q2;
    (* keep, BEL = "MISTRAL_M10K.5.34.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane2 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(address[9:0]), .B1DATA(q2), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q3;
    (* keep, BEL = "MISTRAL_M10K.5.35.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane3 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(address[9:0]), .B1DATA(q3), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q4;
    (* keep, BEL = "MISTRAL_M10K.5.36.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane4 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(address[9:0]), .B1DATA(q4), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q5;
    (* keep, BEL = "MISTRAL_M10K.5.37.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane5 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(address[9:0]), .B1DATA(q5), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q6;
    (* keep, BEL = "MISTRAL_M10K.5.38.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane6 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(address[9:0]), .B1DATA(q6), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q7;
    (* keep, BEL = "MISTRAL_M10K.5.39.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane7 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(address[9:0]), .B1DATA(q7), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q8;
    (* keep, BEL = "MISTRAL_M10K.5.40.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane8 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(address[9:0]), .B1DATA(q8), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q9;
    (* keep, BEL = "MISTRAL_M10K.5.41.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane9 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(address[9:0]), .B1DATA(q9), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q10;
    (* keep, BEL = "MISTRAL_M10K.5.42.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane10 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(address[9:0]), .B1DATA(q10), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q11;
    (* keep, BEL = "MISTRAL_M10K.5.43.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane11 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(address[9:0]), .B1DATA(q11), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q12;
    (* keep, BEL = "MISTRAL_M10K.5.44.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane12 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(address[9:0]), .B1DATA(q12), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q13;
    (* keep, BEL = "MISTRAL_M10K.5.45.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane13 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(address[9:0]), .B1DATA(q13), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q14;
    (* keep, BEL = "MISTRAL_M10K.5.46.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane14 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(address[9:0]), .B1DATA(q14), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q15;
    (* keep, BEL = "MISTRAL_M10K.5.47.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane15 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(address[9:0]), .B1DATA(q15), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    reg [7:0] lane_q;
    always @(posedge clk) begin
        case (address[13:10])
            4'h0: lane_q <= q0[7:0];
            4'h1: lane_q <= q1[7:0];
            4'h2: lane_q <= q2[7:0];
            4'h3: lane_q <= q3[7:0];
            4'h4: lane_q <= q4[7:0];
            4'h5: lane_q <= q5[7:0];
            4'h6: lane_q <= q6[7:0];
            4'h7: lane_q <= q7[7:0];
            4'h8: lane_q <= q8[7:0];
            4'h9: lane_q <= q9[7:0];
            4'hA: lane_q <= q10[7:0];
            4'hB: lane_q <= q11[7:0];
            4'hC: lane_q <= q12[7:0];
            4'hD: lane_q <= q13[7:0];
            4'hE: lane_q <= q14[7:0];
            4'hF: lane_q <= q15[7:0];
            default: lane_q <= 8'hFF;
        endcase
        data <= lane_q;
    end
`endif
endmodule
