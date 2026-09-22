// SPDX-License-Identifier: GPL-2.0-or-later
// Eight empty 1024x10 lanes for the ZX81 machine ROM.
// Address [12:10] selects the block and [9:0] selects the lane.
// The sealed OSS image leaves these lanes empty. Launch splices BASIC
// into the RAM muxes; this file does not read zx8x.hex.

module zx81_rom_link (
    input  wire [12:0] address,
    output wire [7:0]  data
);
    wire [9:0] lane_addr = address[9:0];
    wire [2:0] bank = address[12:10];
    wire [9:0] q0, q1, q2, q3, q4, q5, q6, q7;
    reg [9:0] q;

    always @* begin
        case (bank)
            3'd0: q = q0;
            3'd1: q = q1;
            3'd2: q = q2;
            3'd3: q = q3;
            3'd4: q = q4;
            3'd5: q = q5;
            3'd6: q = q6;
            default: q = q7;
        endcase
    end

    assign data = q[7:0];

    (* keep, BEL = "MISTRAL_M10K.5.73.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane0 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q0), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.74.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane1 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q1), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.75.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane2 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q2), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.76.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane3 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q3), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.77.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane4 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q4), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.78.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane5 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q5), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.79.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane6 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q6), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.80.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(10240'b0)) lane7 (
        .CLK1(1'b0), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR(lane_addr), .B1DATA(q7), .ACLR0(1'b0), .ACLR1(1'b0)
    );
endmodule
