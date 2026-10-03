// SPDX-License-Identifier: GPL-3.0-or-later
// Pluggable 192 KiB ST ROM: each blank 1024x10 M10K stores one contiguous
// 1024-byte source lane. The format-3 linker fills these lanes at launch.
// Word reads sample even then odd bytes, preserving the 68000 byte order.
// Placement matches FIRMWARE_LANE_ROWS in build_fes_atari_st_oss.py. Columns 24-28
// remain available to shared video and expansion socket reservations.
module st_rom (
    input wire clk, reset, req,
    input wire [17:1] address,
    output reg [15:0] rdata,
    output reg ready
);
    localparam [2:0] IDLE=0, EVEN_WAIT=1, EVEN=2, ODD_WAIT=3, ODD=4, COMPLETE=5;
    reg [2:0] state;
    reg [17:0] byte_address;
    reg [7:0] upper_byte;
    wire [7:0] byte_data;
`ifdef VERILATOR
    reg [7:0] memory [0:196607];
    initial $readmemh("st-firmware.hex",memory);
    reg [7:0] sampled_byte;
    always @(posedge clk) sampled_byte <= byte_address < 196608 ? memory[byte_address] : 8'hff;
    assign byte_data=sampled_byte;
`else
    wire [9:0] q0;
    (* keep, BEL = "MISTRAL_M10K.5.1.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane0 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q0), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q1;
    (* keep, BEL = "MISTRAL_M10K.5.2.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane1 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q1), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q2;
    (* keep, BEL = "MISTRAL_M10K.5.3.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane2 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q2), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q3;
    (* keep, BEL = "MISTRAL_M10K.5.4.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane3 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q3), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q4;
    (* keep, BEL = "MISTRAL_M10K.5.5.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane4 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q4), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q5;
    (* keep, BEL = "MISTRAL_M10K.5.6.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane5 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q5), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q6;
    (* keep, BEL = "MISTRAL_M10K.5.7.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane6 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q6), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q7;
    (* keep, BEL = "MISTRAL_M10K.5.8.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane7 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q7), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q8;
    (* keep, BEL = "MISTRAL_M10K.5.9.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane8 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q8), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q9;
    (* keep, BEL = "MISTRAL_M10K.5.10.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane9 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q9), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q10;
    (* keep, BEL = "MISTRAL_M10K.5.11.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane10 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q10), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q11;
    (* keep, BEL = "MISTRAL_M10K.5.12.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane11 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q11), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q12;
    (* keep, BEL = "MISTRAL_M10K.5.13.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane12 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q12), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q13;
    (* keep, BEL = "MISTRAL_M10K.5.14.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane13 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q13), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q14;
    (* keep, BEL = "MISTRAL_M10K.5.32.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane14 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q14), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q15;
    (* keep, BEL = "MISTRAL_M10K.5.33.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane15 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q15), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q16;
    (* keep, BEL = "MISTRAL_M10K.5.34.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane16 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q16), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q17;
    (* keep, BEL = "MISTRAL_M10K.5.35.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane17 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q17), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q18;
    (* keep, BEL = "MISTRAL_M10K.5.36.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane18 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q18), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q19;
    (* keep, BEL = "MISTRAL_M10K.5.37.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane19 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q19), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q20;
    (* keep, BEL = "MISTRAL_M10K.5.38.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane20 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q20), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q21;
    (* keep, BEL = "MISTRAL_M10K.5.39.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane21 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q21), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q22;
    (* keep, BEL = "MISTRAL_M10K.5.40.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane22 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q22), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q23;
    (* keep, BEL = "MISTRAL_M10K.5.41.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane23 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q23), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q24;
    (* keep, BEL = "MISTRAL_M10K.5.42.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane24 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q24), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q25;
    (* keep, BEL = "MISTRAL_M10K.5.43.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane25 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q25), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q26;
    (* keep, BEL = "MISTRAL_M10K.5.44.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane26 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q26), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q27;
    (* keep, BEL = "MISTRAL_M10K.5.45.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane27 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q27), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q28;
    (* keep, BEL = "MISTRAL_M10K.5.46.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane28 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q28), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q29;
    (* keep, BEL = "MISTRAL_M10K.5.47.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane29 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q29), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q30;
    (* keep, BEL = "MISTRAL_M10K.5.48.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane30 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q30), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q31;
    (* keep, BEL = "MISTRAL_M10K.5.49.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane31 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q31), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q32;
    (* keep, BEL = "MISTRAL_M10K.5.50.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane32 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q32), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q33;
    (* keep, BEL = "MISTRAL_M10K.5.51.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane33 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q33), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q34;
    (* keep, BEL = "MISTRAL_M10K.5.52.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane34 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q34), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q35;
    (* keep, BEL = "MISTRAL_M10K.5.53.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane35 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q35), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q36;
    (* keep, BEL = "MISTRAL_M10K.5.54.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane36 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q36), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q37;
    (* keep, BEL = "MISTRAL_M10K.5.55.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane37 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q37), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q38;
    (* keep, BEL = "MISTRAL_M10K.5.73.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane38 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q38), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q39;
    (* keep, BEL = "MISTRAL_M10K.5.74.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane39 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q39), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q40;
    (* keep, BEL = "MISTRAL_M10K.5.75.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane40 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q40), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q41;
    (* keep, BEL = "MISTRAL_M10K.5.76.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane41 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q41), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q42;
    (* keep, BEL = "MISTRAL_M10K.5.77.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane42 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q42), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q43;
    (* keep, BEL = "MISTRAL_M10K.5.78.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane43 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q43), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q44;
    (* keep, BEL = "MISTRAL_M10K.5.79.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane44 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q44), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q45;
    (* keep, BEL = "MISTRAL_M10K.5.80.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane45 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q45), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q46;
    (* keep, BEL = "MISTRAL_M10K.14.1.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane46 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q46), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q47;
    (* keep, BEL = "MISTRAL_M10K.14.2.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane47 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q47), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q48;
    (* keep, BEL = "MISTRAL_M10K.14.3.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane48 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q48), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q49;
    (* keep, BEL = "MISTRAL_M10K.14.4.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane49 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q49), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q50;
    (* keep, BEL = "MISTRAL_M10K.14.5.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane50 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q50), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q51;
    (* keep, BEL = "MISTRAL_M10K.14.6.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane51 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q51), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q52;
    (* keep, BEL = "MISTRAL_M10K.14.7.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane52 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q52), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q53;
    (* keep, BEL = "MISTRAL_M10K.14.8.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane53 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q53), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q54;
    (* keep, BEL = "MISTRAL_M10K.14.9.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane54 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q54), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q55;
    (* keep, BEL = "MISTRAL_M10K.14.10.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane55 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q55), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q56;
    (* keep, BEL = "MISTRAL_M10K.14.11.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane56 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q56), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q57;
    (* keep, BEL = "MISTRAL_M10K.14.12.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane57 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q57), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q58;
    (* keep, BEL = "MISTRAL_M10K.14.13.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane58 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q58), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q59;
    (* keep, BEL = "MISTRAL_M10K.14.14.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane59 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q59), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q60;
    (* keep, BEL = "MISTRAL_M10K.14.15.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane60 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q60), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q61;
    (* keep, BEL = "MISTRAL_M10K.14.16.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane61 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q61), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q62;
    (* keep, BEL = "MISTRAL_M10K.14.17.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane62 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q62), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q63;
    (* keep, BEL = "MISTRAL_M10K.14.18.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane63 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q63), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q64;
    (* keep, BEL = "MISTRAL_M10K.14.19.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane64 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q64), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q65;
    (* keep, BEL = "MISTRAL_M10K.14.20.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane65 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q65), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q66;
    (* keep, BEL = "MISTRAL_M10K.14.21.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane66 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q66), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q67;
    (* keep, BEL = "MISTRAL_M10K.14.22.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane67 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q67), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q68;
    (* keep, BEL = "MISTRAL_M10K.14.23.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane68 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q68), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q69;
    (* keep, BEL = "MISTRAL_M10K.14.24.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane69 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q69), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q70;
    (* keep, BEL = "MISTRAL_M10K.14.25.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane70 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q70), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q71;
    (* keep, BEL = "MISTRAL_M10K.14.26.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane71 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q71), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q72;
    (* keep, BEL = "MISTRAL_M10K.14.27.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane72 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q72), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q73;
    (* keep, BEL = "MISTRAL_M10K.14.28.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane73 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q73), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q74;
    (* keep, BEL = "MISTRAL_M10K.14.29.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane74 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q74), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q75;
    (* keep, BEL = "MISTRAL_M10K.14.30.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane75 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q75), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q76;
    (* keep, BEL = "MISTRAL_M10K.14.31.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane76 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q76), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q77;
    (* keep, BEL = "MISTRAL_M10K.14.32.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane77 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q77), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q78;
    (* keep, BEL = "MISTRAL_M10K.14.33.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane78 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q78), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q79;
    (* keep, BEL = "MISTRAL_M10K.14.34.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane79 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q79), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q80;
    (* keep, BEL = "MISTRAL_M10K.14.35.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane80 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q80), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q81;
    (* keep, BEL = "MISTRAL_M10K.14.36.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane81 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q81), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q82;
    (* keep, BEL = "MISTRAL_M10K.14.37.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane82 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q82), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q83;
    (* keep, BEL = "MISTRAL_M10K.14.38.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane83 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q83), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q84;
    (* keep, BEL = "MISTRAL_M10K.14.39.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane84 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q84), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q85;
    (* keep, BEL = "MISTRAL_M10K.14.40.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane85 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q85), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q86;
    (* keep, BEL = "MISTRAL_M10K.14.41.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane86 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q86), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q87;
    (* keep, BEL = "MISTRAL_M10K.14.42.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane87 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q87), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q88;
    (* keep, BEL = "MISTRAL_M10K.14.43.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane88 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q88), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q89;
    (* keep, BEL = "MISTRAL_M10K.14.44.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane89 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q89), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q90;
    (* keep, BEL = "MISTRAL_M10K.14.45.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane90 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q90), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q91;
    (* keep, BEL = "MISTRAL_M10K.14.46.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane91 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q91), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q92;
    (* keep, BEL = "MISTRAL_M10K.14.47.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane92 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q92), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q93;
    (* keep, BEL = "MISTRAL_M10K.14.48.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane93 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q93), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q94;
    (* keep, BEL = "MISTRAL_M10K.14.49.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane94 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q94), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q95;
    (* keep, BEL = "MISTRAL_M10K.14.50.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane95 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q95), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q96;
    (* keep, BEL = "MISTRAL_M10K.14.51.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane96 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q96), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q97;
    (* keep, BEL = "MISTRAL_M10K.14.52.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane97 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q97), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q98;
    (* keep, BEL = "MISTRAL_M10K.14.53.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane98 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q98), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q99;
    (* keep, BEL = "MISTRAL_M10K.14.54.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane99 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q99), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q100;
    (* keep, BEL = "MISTRAL_M10K.14.55.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane100 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q100), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q101;
    (* keep, BEL = "MISTRAL_M10K.14.56.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane101 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q101), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q102;
    (* keep, BEL = "MISTRAL_M10K.14.57.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane102 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q102), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q103;
    (* keep, BEL = "MISTRAL_M10K.14.58.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane103 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q103), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q104;
    (* keep, BEL = "MISTRAL_M10K.14.59.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane104 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q104), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q105;
    (* keep, BEL = "MISTRAL_M10K.14.60.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane105 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q105), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q106;
    (* keep, BEL = "MISTRAL_M10K.14.61.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane106 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q106), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q107;
    (* keep, BEL = "MISTRAL_M10K.14.62.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane107 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q107), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q108;
    (* keep, BEL = "MISTRAL_M10K.14.63.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane108 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q108), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q109;
    (* keep, BEL = "MISTRAL_M10K.14.64.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane109 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q109), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q110;
    (* keep, BEL = "MISTRAL_M10K.14.65.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane110 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q110), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q111;
    (* keep, BEL = "MISTRAL_M10K.14.66.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane111 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q111), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q112;
    (* keep, BEL = "MISTRAL_M10K.14.67.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane112 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q112), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q113;
    (* keep, BEL = "MISTRAL_M10K.14.68.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane113 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q113), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q114;
    (* keep, BEL = "MISTRAL_M10K.14.69.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane114 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q114), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q115;
    (* keep, BEL = "MISTRAL_M10K.14.70.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane115 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q115), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q116;
    (* keep, BEL = "MISTRAL_M10K.14.71.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane116 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q116), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q117;
    (* keep, BEL = "MISTRAL_M10K.14.72.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane117 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q117), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q118;
    (* keep, BEL = "MISTRAL_M10K.14.73.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane118 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q118), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q119;
    (* keep, BEL = "MISTRAL_M10K.14.74.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane119 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q119), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q120;
    (* keep, BEL = "MISTRAL_M10K.14.75.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane120 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q120), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q121;
    (* keep, BEL = "MISTRAL_M10K.14.76.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane121 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q121), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q122;
    (* keep, BEL = "MISTRAL_M10K.14.77.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane122 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q122), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q123;
    (* keep, BEL = "MISTRAL_M10K.14.78.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane123 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q123), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q124;
    (* keep, BEL = "MISTRAL_M10K.14.79.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane124 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q124), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q125;
    (* keep, BEL = "MISTRAL_M10K.14.80.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane125 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q125), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q126;
    (* keep, BEL = "MISTRAL_M10K.38.1.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane126 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q126), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q127;
    (* keep, BEL = "MISTRAL_M10K.38.2.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane127 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q127), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q128;
    (* keep, BEL = "MISTRAL_M10K.38.3.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane128 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q128), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q129;
    (* keep, BEL = "MISTRAL_M10K.38.4.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane129 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q129), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q130;
    (* keep, BEL = "MISTRAL_M10K.38.5.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane130 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q130), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q131;
    (* keep, BEL = "MISTRAL_M10K.38.6.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane131 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q131), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q132;
    (* keep, BEL = "MISTRAL_M10K.38.7.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane132 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q132), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q133;
    (* keep, BEL = "MISTRAL_M10K.38.8.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane133 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q133), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q134;
    (* keep, BEL = "MISTRAL_M10K.38.9.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane134 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q134), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q135;
    (* keep, BEL = "MISTRAL_M10K.38.10.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane135 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q135), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q136;
    (* keep, BEL = "MISTRAL_M10K.38.11.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane136 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q136), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q137;
    (* keep, BEL = "MISTRAL_M10K.38.12.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane137 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q137), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q138;
    (* keep, BEL = "MISTRAL_M10K.38.13.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane138 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q138), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q139;
    (* keep, BEL = "MISTRAL_M10K.38.14.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane139 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q139), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q140;
    (* keep, BEL = "MISTRAL_M10K.38.15.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane140 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q140), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q141;
    (* keep, BEL = "MISTRAL_M10K.38.16.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane141 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q141), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q142;
    (* keep, BEL = "MISTRAL_M10K.38.17.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane142 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q142), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q143;
    (* keep, BEL = "MISTRAL_M10K.38.18.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane143 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q143), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q144;
    (* keep, BEL = "MISTRAL_M10K.38.19.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane144 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q144), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q145;
    (* keep, BEL = "MISTRAL_M10K.38.20.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane145 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q145), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q146;
    (* keep, BEL = "MISTRAL_M10K.38.21.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane146 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q146), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q147;
    (* keep, BEL = "MISTRAL_M10K.38.22.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane147 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q147), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q148;
    (* keep, BEL = "MISTRAL_M10K.38.23.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane148 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q148), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q149;
    (* keep, BEL = "MISTRAL_M10K.38.24.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane149 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q149), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q150;
    (* keep, BEL = "MISTRAL_M10K.38.25.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane150 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q150), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q151;
    (* keep, BEL = "MISTRAL_M10K.38.26.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane151 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q151), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q152;
    (* keep, BEL = "MISTRAL_M10K.38.27.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane152 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q152), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q153;
    (* keep, BEL = "MISTRAL_M10K.38.28.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane153 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q153), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q154;
    (* keep, BEL = "MISTRAL_M10K.38.29.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane154 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q154), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q155;
    (* keep, BEL = "MISTRAL_M10K.38.30.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane155 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q155), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q156;
    (* keep, BEL = "MISTRAL_M10K.38.31.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane156 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q156), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q157;
    (* keep, BEL = "MISTRAL_M10K.38.32.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane157 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q157), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q158;
    (* keep, BEL = "MISTRAL_M10K.38.33.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane158 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q158), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q159;
    (* keep, BEL = "MISTRAL_M10K.38.34.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane159 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q159), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q160;
    (* keep, BEL = "MISTRAL_M10K.38.35.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane160 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q160), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q161;
    (* keep, BEL = "MISTRAL_M10K.38.36.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane161 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q161), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q162;
    (* keep, BEL = "MISTRAL_M10K.38.37.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane162 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q162), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q163;
    (* keep, BEL = "MISTRAL_M10K.38.38.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane163 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q163), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q164;
    (* keep, BEL = "MISTRAL_M10K.38.39.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane164 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q164), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q165;
    (* keep, BEL = "MISTRAL_M10K.38.40.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane165 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q165), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q166;
    (* keep, BEL = "MISTRAL_M10K.38.41.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane166 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q166), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q167;
    (* keep, BEL = "MISTRAL_M10K.38.42.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane167 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q167), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q168;
    (* keep, BEL = "MISTRAL_M10K.38.43.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane168 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q168), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q169;
    (* keep, BEL = "MISTRAL_M10K.38.44.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane169 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q169), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q170;
    (* keep, BEL = "MISTRAL_M10K.38.45.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane170 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q170), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q171;
    (* keep, BEL = "MISTRAL_M10K.38.46.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane171 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q171), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q172;
    (* keep, BEL = "MISTRAL_M10K.38.47.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane172 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q172), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q173;
    (* keep, BEL = "MISTRAL_M10K.38.48.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane173 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q173), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q174;
    (* keep, BEL = "MISTRAL_M10K.38.49.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane174 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q174), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q175;
    (* keep, BEL = "MISTRAL_M10K.38.50.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane175 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q175), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q176;
    (* keep, BEL = "MISTRAL_M10K.38.51.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane176 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q176), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q177;
    (* keep, BEL = "MISTRAL_M10K.38.52.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane177 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q177), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q178;
    (* keep, BEL = "MISTRAL_M10K.38.53.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane178 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q178), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q179;
    (* keep, BEL = "MISTRAL_M10K.38.54.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane179 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q179), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q180;
    (* keep, BEL = "MISTRAL_M10K.38.55.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane180 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q180), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q181;
    (* keep, BEL = "MISTRAL_M10K.38.56.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane181 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q181), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q182;
    (* keep, BEL = "MISTRAL_M10K.38.57.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane182 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q182), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q183;
    (* keep, BEL = "MISTRAL_M10K.38.58.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane183 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q183), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q184;
    (* keep, BEL = "MISTRAL_M10K.38.59.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane184 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q184), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q185;
    (* keep, BEL = "MISTRAL_M10K.38.60.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane185 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q185), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q186;
    (* keep, BEL = "MISTRAL_M10K.38.61.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane186 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q186), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q187;
    (* keep, BEL = "MISTRAL_M10K.38.62.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane187 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q187), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q188;
    (* keep, BEL = "MISTRAL_M10K.38.63.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane188 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q188), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q189;
    (* keep, BEL = "MISTRAL_M10K.38.64.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane189 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q189), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q190;
    (* keep, BEL = "MISTRAL_M10K.38.65.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane190 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q190), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    wire [9:0] q191;
    (* keep, BEL = "MISTRAL_M10K.38.66.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane191 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(byte_address[9:0]), .B1DATA(q191), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    reg [7:0] selected_byte;
    always @* begin
        case (byte_address[17:10])
            8'd0: selected_byte=q0[7:0];
            8'd1: selected_byte=q1[7:0];
            8'd2: selected_byte=q2[7:0];
            8'd3: selected_byte=q3[7:0];
            8'd4: selected_byte=q4[7:0];
            8'd5: selected_byte=q5[7:0];
            8'd6: selected_byte=q6[7:0];
            8'd7: selected_byte=q7[7:0];
            8'd8: selected_byte=q8[7:0];
            8'd9: selected_byte=q9[7:0];
            8'd10: selected_byte=q10[7:0];
            8'd11: selected_byte=q11[7:0];
            8'd12: selected_byte=q12[7:0];
            8'd13: selected_byte=q13[7:0];
            8'd14: selected_byte=q14[7:0];
            8'd15: selected_byte=q15[7:0];
            8'd16: selected_byte=q16[7:0];
            8'd17: selected_byte=q17[7:0];
            8'd18: selected_byte=q18[7:0];
            8'd19: selected_byte=q19[7:0];
            8'd20: selected_byte=q20[7:0];
            8'd21: selected_byte=q21[7:0];
            8'd22: selected_byte=q22[7:0];
            8'd23: selected_byte=q23[7:0];
            8'd24: selected_byte=q24[7:0];
            8'd25: selected_byte=q25[7:0];
            8'd26: selected_byte=q26[7:0];
            8'd27: selected_byte=q27[7:0];
            8'd28: selected_byte=q28[7:0];
            8'd29: selected_byte=q29[7:0];
            8'd30: selected_byte=q30[7:0];
            8'd31: selected_byte=q31[7:0];
            8'd32: selected_byte=q32[7:0];
            8'd33: selected_byte=q33[7:0];
            8'd34: selected_byte=q34[7:0];
            8'd35: selected_byte=q35[7:0];
            8'd36: selected_byte=q36[7:0];
            8'd37: selected_byte=q37[7:0];
            8'd38: selected_byte=q38[7:0];
            8'd39: selected_byte=q39[7:0];
            8'd40: selected_byte=q40[7:0];
            8'd41: selected_byte=q41[7:0];
            8'd42: selected_byte=q42[7:0];
            8'd43: selected_byte=q43[7:0];
            8'd44: selected_byte=q44[7:0];
            8'd45: selected_byte=q45[7:0];
            8'd46: selected_byte=q46[7:0];
            8'd47: selected_byte=q47[7:0];
            8'd48: selected_byte=q48[7:0];
            8'd49: selected_byte=q49[7:0];
            8'd50: selected_byte=q50[7:0];
            8'd51: selected_byte=q51[7:0];
            8'd52: selected_byte=q52[7:0];
            8'd53: selected_byte=q53[7:0];
            8'd54: selected_byte=q54[7:0];
            8'd55: selected_byte=q55[7:0];
            8'd56: selected_byte=q56[7:0];
            8'd57: selected_byte=q57[7:0];
            8'd58: selected_byte=q58[7:0];
            8'd59: selected_byte=q59[7:0];
            8'd60: selected_byte=q60[7:0];
            8'd61: selected_byte=q61[7:0];
            8'd62: selected_byte=q62[7:0];
            8'd63: selected_byte=q63[7:0];
            8'd64: selected_byte=q64[7:0];
            8'd65: selected_byte=q65[7:0];
            8'd66: selected_byte=q66[7:0];
            8'd67: selected_byte=q67[7:0];
            8'd68: selected_byte=q68[7:0];
            8'd69: selected_byte=q69[7:0];
            8'd70: selected_byte=q70[7:0];
            8'd71: selected_byte=q71[7:0];
            8'd72: selected_byte=q72[7:0];
            8'd73: selected_byte=q73[7:0];
            8'd74: selected_byte=q74[7:0];
            8'd75: selected_byte=q75[7:0];
            8'd76: selected_byte=q76[7:0];
            8'd77: selected_byte=q77[7:0];
            8'd78: selected_byte=q78[7:0];
            8'd79: selected_byte=q79[7:0];
            8'd80: selected_byte=q80[7:0];
            8'd81: selected_byte=q81[7:0];
            8'd82: selected_byte=q82[7:0];
            8'd83: selected_byte=q83[7:0];
            8'd84: selected_byte=q84[7:0];
            8'd85: selected_byte=q85[7:0];
            8'd86: selected_byte=q86[7:0];
            8'd87: selected_byte=q87[7:0];
            8'd88: selected_byte=q88[7:0];
            8'd89: selected_byte=q89[7:0];
            8'd90: selected_byte=q90[7:0];
            8'd91: selected_byte=q91[7:0];
            8'd92: selected_byte=q92[7:0];
            8'd93: selected_byte=q93[7:0];
            8'd94: selected_byte=q94[7:0];
            8'd95: selected_byte=q95[7:0];
            8'd96: selected_byte=q96[7:0];
            8'd97: selected_byte=q97[7:0];
            8'd98: selected_byte=q98[7:0];
            8'd99: selected_byte=q99[7:0];
            8'd100: selected_byte=q100[7:0];
            8'd101: selected_byte=q101[7:0];
            8'd102: selected_byte=q102[7:0];
            8'd103: selected_byte=q103[7:0];
            8'd104: selected_byte=q104[7:0];
            8'd105: selected_byte=q105[7:0];
            8'd106: selected_byte=q106[7:0];
            8'd107: selected_byte=q107[7:0];
            8'd108: selected_byte=q108[7:0];
            8'd109: selected_byte=q109[7:0];
            8'd110: selected_byte=q110[7:0];
            8'd111: selected_byte=q111[7:0];
            8'd112: selected_byte=q112[7:0];
            8'd113: selected_byte=q113[7:0];
            8'd114: selected_byte=q114[7:0];
            8'd115: selected_byte=q115[7:0];
            8'd116: selected_byte=q116[7:0];
            8'd117: selected_byte=q117[7:0];
            8'd118: selected_byte=q118[7:0];
            8'd119: selected_byte=q119[7:0];
            8'd120: selected_byte=q120[7:0];
            8'd121: selected_byte=q121[7:0];
            8'd122: selected_byte=q122[7:0];
            8'd123: selected_byte=q123[7:0];
            8'd124: selected_byte=q124[7:0];
            8'd125: selected_byte=q125[7:0];
            8'd126: selected_byte=q126[7:0];
            8'd127: selected_byte=q127[7:0];
            8'd128: selected_byte=q128[7:0];
            8'd129: selected_byte=q129[7:0];
            8'd130: selected_byte=q130[7:0];
            8'd131: selected_byte=q131[7:0];
            8'd132: selected_byte=q132[7:0];
            8'd133: selected_byte=q133[7:0];
            8'd134: selected_byte=q134[7:0];
            8'd135: selected_byte=q135[7:0];
            8'd136: selected_byte=q136[7:0];
            8'd137: selected_byte=q137[7:0];
            8'd138: selected_byte=q138[7:0];
            8'd139: selected_byte=q139[7:0];
            8'd140: selected_byte=q140[7:0];
            8'd141: selected_byte=q141[7:0];
            8'd142: selected_byte=q142[7:0];
            8'd143: selected_byte=q143[7:0];
            8'd144: selected_byte=q144[7:0];
            8'd145: selected_byte=q145[7:0];
            8'd146: selected_byte=q146[7:0];
            8'd147: selected_byte=q147[7:0];
            8'd148: selected_byte=q148[7:0];
            8'd149: selected_byte=q149[7:0];
            8'd150: selected_byte=q150[7:0];
            8'd151: selected_byte=q151[7:0];
            8'd152: selected_byte=q152[7:0];
            8'd153: selected_byte=q153[7:0];
            8'd154: selected_byte=q154[7:0];
            8'd155: selected_byte=q155[7:0];
            8'd156: selected_byte=q156[7:0];
            8'd157: selected_byte=q157[7:0];
            8'd158: selected_byte=q158[7:0];
            8'd159: selected_byte=q159[7:0];
            8'd160: selected_byte=q160[7:0];
            8'd161: selected_byte=q161[7:0];
            8'd162: selected_byte=q162[7:0];
            8'd163: selected_byte=q163[7:0];
            8'd164: selected_byte=q164[7:0];
            8'd165: selected_byte=q165[7:0];
            8'd166: selected_byte=q166[7:0];
            8'd167: selected_byte=q167[7:0];
            8'd168: selected_byte=q168[7:0];
            8'd169: selected_byte=q169[7:0];
            8'd170: selected_byte=q170[7:0];
            8'd171: selected_byte=q171[7:0];
            8'd172: selected_byte=q172[7:0];
            8'd173: selected_byte=q173[7:0];
            8'd174: selected_byte=q174[7:0];
            8'd175: selected_byte=q175[7:0];
            8'd176: selected_byte=q176[7:0];
            8'd177: selected_byte=q177[7:0];
            8'd178: selected_byte=q178[7:0];
            8'd179: selected_byte=q179[7:0];
            8'd180: selected_byte=q180[7:0];
            8'd181: selected_byte=q181[7:0];
            8'd182: selected_byte=q182[7:0];
            8'd183: selected_byte=q183[7:0];
            8'd184: selected_byte=q184[7:0];
            8'd185: selected_byte=q185[7:0];
            8'd186: selected_byte=q186[7:0];
            8'd187: selected_byte=q187[7:0];
            8'd188: selected_byte=q188[7:0];
            8'd189: selected_byte=q189[7:0];
            8'd190: selected_byte=q190[7:0];
            8'd191: selected_byte=q191[7:0];
            default: selected_byte=8'hff;
        endcase
    end
    assign byte_data=selected_byte;
`endif
    always @(posedge clk) begin
        if (reset) begin
            state<=IDLE; byte_address<=0; upper_byte<=0; ready<=0; rdata<=0;
        end else case (state)
            IDLE: if (req) begin byte_address<={address,1'b0}; state<=EVEN_WAIT; end
            EVEN_WAIT: state<=EVEN;
            EVEN: begin upper_byte<=byte_data; byte_address[0]<=1; state<=ODD_WAIT; end
            ODD_WAIT: state<=ODD;
            ODD: begin rdata<={upper_byte,byte_data}; ready<=1; state<=COMPLETE; end
            COMPLETE: if (!req) begin ready<=0; state<=IDLE; end
            default: state<=IDLE;
        endcase
    end
endmodule
