// SPDX-License-Identifier: GPL-2.0-or-later
// Blank 16 KiB Apple II firmware image, linked at download time.
//
// The linked source is the CPU's $C000-$FFFF window: offsets $1000-$3FFF are
// the motherboard ROM at $D000-$FFFF, and offsets $0n00-$0nFF hold the $Cn00
// firmware page of a built-in card in slot n (the Disk II P5 PROM at $0600).
// Other offsets below $1000 are reserved and ignored by this shell. Each
// 1 KiB lane is an individually placed, empty 1024x10 M10K that the ROM map
// authenticates against the routed BELs.
//
// Cyclone V M10K cannot do a true asynchronous read, so each lane registers
// its address on clk (CFG_ASYNC_READ=0, live CLK1, B1EN held high). The
// 3-bit sub-bank and the group bit are registered on that same edge. On the
// next edge the two 8:1 group registers capture the lane Q selected by the
// delayed sub-bank, and the group bit delayed one further cycle steers the
// combinational 2:1. CPU-facing latency stays two system clocks, which is
// the latency the old asynchronous lane plus the two output registers
// already had. apple2_machine captures the 6502 address on cpu_ce
// (cycle_clock == 0) and samples the read bus at cycle_clock == 16, about
// sixteen 52.224 MHz clocks later (~51 clocks per ~1.0205 MHz enable). Two
// clocks of ROM latency are still stable at that sample. The Verilator
// model uses the same two stages so simulation and the M10K agree.
module apple2_rom (
    input  wire        clk,
    input  wire [13:0] address,
    output wire [7:0]  data
);
`ifdef VERILATOR
    reg [7:0] memory [0:16383];
    initial $readmemh("build/diagnostics/fes-apple2/firmware.hex", memory);
    reg [7:0] sim_stage;
    reg [7:0] data_q;
    always @(posedge clk) begin
        sim_stage <= memory[address];
        data_q <= sim_stage;
    end
    assign data = data_q;
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
    // The M10K registers its read address on clk. Delay the sub-bank and
    // group selects alongside it, then let the group registers supply stage
    // two. `data` is the combinational group mux so the byte is not delayed
    // a third clock.
    reg [2:0] subbank_d;
    reg group_d;
    reg group_d2;
    reg [7:0] group0_q;
    reg [7:0] group1_q;
    always @(posedge clk) begin
        subbank_d <= address[12:10];
        group_d <= address[13];
        group_d2 <= group_d;
        case (subbank_d)
            3'd0: group0_q <= q0[7:0];
            3'd1: group0_q <= q1[7:0];
            3'd2: group0_q <= q2[7:0];
            3'd3: group0_q <= q3[7:0];
            3'd4: group0_q <= q4[7:0];
            3'd5: group0_q <= q5[7:0];
            3'd6: group0_q <= q6[7:0];
            3'd7: group0_q <= q7[7:0];
        endcase
        case (subbank_d)
            3'd0: group1_q <= q8[7:0];
            3'd1: group1_q <= q9[7:0];
            3'd2: group1_q <= q10[7:0];
            3'd3: group1_q <= q11[7:0];
            3'd4: group1_q <= q12[7:0];
            3'd5: group1_q <= q13[7:0];
            3'd6: group1_q <= q14[7:0];
            3'd7: group1_q <= q15[7:0];
        endcase
    end
    assign data = group_d2 ? group1_q : group0_q;
    // Legacy 10-bit M10K write enable is active low: A1EN=1 disables writes.
    (* keep, BEL = "MISTRAL_M10K.5.32.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane0 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q0), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.33.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane1 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q1), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.34.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane2 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q2), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.35.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane3 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q3), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.36.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane4 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q4), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.37.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane5 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q5), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.38.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane6 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q6), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.39.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane7 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q7), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.40.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane8 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q8), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.41.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane9 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q9), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.42.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane10 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q10), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.43.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane11 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q11), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.44.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane12 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q12), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.45.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane13 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q13), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.46.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane14 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q14), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.47.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane15 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q15), .ACLR0(1'b0), .ACLR1(1'b0)
    );
`endif
endmodule
