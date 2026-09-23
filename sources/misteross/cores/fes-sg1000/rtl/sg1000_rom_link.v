// SPDX-License-Identifier: GPL-2.0-or-later
// Blank 16 KiB fixed cartridge; the sealed format-3 ROM map binds every lane.
module sg1000_rom_link (
    input wire [13:0] address,
    output wire [7:0] data,
    input wire [13:0] peek_address,
    output wire [7:0] peek_data
);
`ifdef VERILATOR
    reg [7:0] memory [0:16383];
    initial $readmemh("build/diagnostics/fes-sg1000/graphics-i-16k.hex", memory);
    assign data = memory[address];
    assign peek_data = memory[peek_address];
`else
    wire [9:0] lane_addr = address[9:0];
    wire [3:0] bank = address[13:10];
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
    reg [9:0] selected;
    always @* begin
        case (bank)
            4'd0: selected = q0;
            4'd1: selected = q1;
            4'd2: selected = q2;
            4'd3: selected = q3;
            4'd4: selected = q4;
            4'd5: selected = q5;
            4'd6: selected = q6;
            4'd7: selected = q7;
            4'd8: selected = q8;
            4'd9: selected = q9;
            4'd10: selected = q10;
            4'd11: selected = q11;
            4'd12: selected = q12;
            4'd13: selected = q13;
            4'd14: selected = q14;
            default: selected = q15;
        endcase
    end
    assign data = selected[7:0];
    // The production top does not expose a peek port.
    assign peek_data = 8'hff;
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
endmodule
