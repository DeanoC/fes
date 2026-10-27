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
    // The odd address follows the even one by a clock, so each byte spends a
    // clock in its M10K and a clock in the group registers before sampling.
    localparam [2:0] IDLE=0, EVEN_READ=1, ODD_READ=2, EVEN_GROUP=3, ODD_GROUP=4, COMPLETE=5;
    reg [2:0] state;
    reg [17:0] byte_address;
    reg [7:0] upper_byte;
    wire [7:0] byte_data;
`ifdef VERILATOR
    reg [7:0] memory [0:196607];
    initial $readmemh("st-firmware.hex",memory);
    reg [7:0] sampled_byte, grouped_byte;
    always @(posedge clk) begin
        sampled_byte <= byte_address < 196608 ? memory[byte_address] : 8'hff;
        grouped_byte <= sampled_byte;
    end
    assign byte_data=grouped_byte;
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
    // Mutually exclusive lane decodes feed a balanced byte reduction.
    // Undefined ROM banks retain the original FF read value.
    wire [7:0] masked0 = q0[7:0] & {8{byte_address[17:10] == 8'd0}};
    wire [7:0] masked1 = q1[7:0] & {8{byte_address[17:10] == 8'd1}};
    wire [7:0] masked2 = q2[7:0] & {8{byte_address[17:10] == 8'd2}};
    wire [7:0] masked3 = q3[7:0] & {8{byte_address[17:10] == 8'd3}};
    wire [7:0] masked4 = q4[7:0] & {8{byte_address[17:10] == 8'd4}};
    wire [7:0] masked5 = q5[7:0] & {8{byte_address[17:10] == 8'd5}};
    wire [7:0] masked6 = q6[7:0] & {8{byte_address[17:10] == 8'd6}};
    wire [7:0] masked7 = q7[7:0] & {8{byte_address[17:10] == 8'd7}};
    wire [7:0] masked8 = q8[7:0] & {8{byte_address[17:10] == 8'd8}};
    wire [7:0] masked9 = q9[7:0] & {8{byte_address[17:10] == 8'd9}};
    wire [7:0] masked10 = q10[7:0] & {8{byte_address[17:10] == 8'd10}};
    wire [7:0] masked11 = q11[7:0] & {8{byte_address[17:10] == 8'd11}};
    wire [7:0] masked12 = q12[7:0] & {8{byte_address[17:10] == 8'd12}};
    wire [7:0] masked13 = q13[7:0] & {8{byte_address[17:10] == 8'd13}};
    wire [7:0] masked14 = q14[7:0] & {8{byte_address[17:10] == 8'd14}};
    wire [7:0] masked15 = q15[7:0] & {8{byte_address[17:10] == 8'd15}};
    wire [7:0] masked16 = q16[7:0] & {8{byte_address[17:10] == 8'd16}};
    wire [7:0] masked17 = q17[7:0] & {8{byte_address[17:10] == 8'd17}};
    wire [7:0] masked18 = q18[7:0] & {8{byte_address[17:10] == 8'd18}};
    wire [7:0] masked19 = q19[7:0] & {8{byte_address[17:10] == 8'd19}};
    wire [7:0] masked20 = q20[7:0] & {8{byte_address[17:10] == 8'd20}};
    wire [7:0] masked21 = q21[7:0] & {8{byte_address[17:10] == 8'd21}};
    wire [7:0] masked22 = q22[7:0] & {8{byte_address[17:10] == 8'd22}};
    wire [7:0] masked23 = q23[7:0] & {8{byte_address[17:10] == 8'd23}};
    wire [7:0] masked24 = q24[7:0] & {8{byte_address[17:10] == 8'd24}};
    wire [7:0] masked25 = q25[7:0] & {8{byte_address[17:10] == 8'd25}};
    wire [7:0] masked26 = q26[7:0] & {8{byte_address[17:10] == 8'd26}};
    wire [7:0] masked27 = q27[7:0] & {8{byte_address[17:10] == 8'd27}};
    wire [7:0] masked28 = q28[7:0] & {8{byte_address[17:10] == 8'd28}};
    wire [7:0] masked29 = q29[7:0] & {8{byte_address[17:10] == 8'd29}};
    wire [7:0] masked30 = q30[7:0] & {8{byte_address[17:10] == 8'd30}};
    wire [7:0] masked31 = q31[7:0] & {8{byte_address[17:10] == 8'd31}};
    wire [7:0] masked32 = q32[7:0] & {8{byte_address[17:10] == 8'd32}};
    wire [7:0] masked33 = q33[7:0] & {8{byte_address[17:10] == 8'd33}};
    wire [7:0] masked34 = q34[7:0] & {8{byte_address[17:10] == 8'd34}};
    wire [7:0] masked35 = q35[7:0] & {8{byte_address[17:10] == 8'd35}};
    wire [7:0] masked36 = q36[7:0] & {8{byte_address[17:10] == 8'd36}};
    wire [7:0] masked37 = q37[7:0] & {8{byte_address[17:10] == 8'd37}};
    wire [7:0] masked38 = q38[7:0] & {8{byte_address[17:10] == 8'd38}};
    wire [7:0] masked39 = q39[7:0] & {8{byte_address[17:10] == 8'd39}};
    wire [7:0] masked40 = q40[7:0] & {8{byte_address[17:10] == 8'd40}};
    wire [7:0] masked41 = q41[7:0] & {8{byte_address[17:10] == 8'd41}};
    wire [7:0] masked42 = q42[7:0] & {8{byte_address[17:10] == 8'd42}};
    wire [7:0] masked43 = q43[7:0] & {8{byte_address[17:10] == 8'd43}};
    wire [7:0] masked44 = q44[7:0] & {8{byte_address[17:10] == 8'd44}};
    wire [7:0] masked45 = q45[7:0] & {8{byte_address[17:10] == 8'd45}};
    wire [7:0] masked46 = q46[7:0] & {8{byte_address[17:10] == 8'd46}};
    wire [7:0] masked47 = q47[7:0] & {8{byte_address[17:10] == 8'd47}};
    wire [7:0] masked48 = q48[7:0] & {8{byte_address[17:10] == 8'd48}};
    wire [7:0] masked49 = q49[7:0] & {8{byte_address[17:10] == 8'd49}};
    wire [7:0] masked50 = q50[7:0] & {8{byte_address[17:10] == 8'd50}};
    wire [7:0] masked51 = q51[7:0] & {8{byte_address[17:10] == 8'd51}};
    wire [7:0] masked52 = q52[7:0] & {8{byte_address[17:10] == 8'd52}};
    wire [7:0] masked53 = q53[7:0] & {8{byte_address[17:10] == 8'd53}};
    wire [7:0] masked54 = q54[7:0] & {8{byte_address[17:10] == 8'd54}};
    wire [7:0] masked55 = q55[7:0] & {8{byte_address[17:10] == 8'd55}};
    wire [7:0] masked56 = q56[7:0] & {8{byte_address[17:10] == 8'd56}};
    wire [7:0] masked57 = q57[7:0] & {8{byte_address[17:10] == 8'd57}};
    wire [7:0] masked58 = q58[7:0] & {8{byte_address[17:10] == 8'd58}};
    wire [7:0] masked59 = q59[7:0] & {8{byte_address[17:10] == 8'd59}};
    wire [7:0] masked60 = q60[7:0] & {8{byte_address[17:10] == 8'd60}};
    wire [7:0] masked61 = q61[7:0] & {8{byte_address[17:10] == 8'd61}};
    wire [7:0] masked62 = q62[7:0] & {8{byte_address[17:10] == 8'd62}};
    wire [7:0] masked63 = q63[7:0] & {8{byte_address[17:10] == 8'd63}};
    wire [7:0] masked64 = q64[7:0] & {8{byte_address[17:10] == 8'd64}};
    wire [7:0] masked65 = q65[7:0] & {8{byte_address[17:10] == 8'd65}};
    wire [7:0] masked66 = q66[7:0] & {8{byte_address[17:10] == 8'd66}};
    wire [7:0] masked67 = q67[7:0] & {8{byte_address[17:10] == 8'd67}};
    wire [7:0] masked68 = q68[7:0] & {8{byte_address[17:10] == 8'd68}};
    wire [7:0] masked69 = q69[7:0] & {8{byte_address[17:10] == 8'd69}};
    wire [7:0] masked70 = q70[7:0] & {8{byte_address[17:10] == 8'd70}};
    wire [7:0] masked71 = q71[7:0] & {8{byte_address[17:10] == 8'd71}};
    wire [7:0] masked72 = q72[7:0] & {8{byte_address[17:10] == 8'd72}};
    wire [7:0] masked73 = q73[7:0] & {8{byte_address[17:10] == 8'd73}};
    wire [7:0] masked74 = q74[7:0] & {8{byte_address[17:10] == 8'd74}};
    wire [7:0] masked75 = q75[7:0] & {8{byte_address[17:10] == 8'd75}};
    wire [7:0] masked76 = q76[7:0] & {8{byte_address[17:10] == 8'd76}};
    wire [7:0] masked77 = q77[7:0] & {8{byte_address[17:10] == 8'd77}};
    wire [7:0] masked78 = q78[7:0] & {8{byte_address[17:10] == 8'd78}};
    wire [7:0] masked79 = q79[7:0] & {8{byte_address[17:10] == 8'd79}};
    wire [7:0] masked80 = q80[7:0] & {8{byte_address[17:10] == 8'd80}};
    wire [7:0] masked81 = q81[7:0] & {8{byte_address[17:10] == 8'd81}};
    wire [7:0] masked82 = q82[7:0] & {8{byte_address[17:10] == 8'd82}};
    wire [7:0] masked83 = q83[7:0] & {8{byte_address[17:10] == 8'd83}};
    wire [7:0] masked84 = q84[7:0] & {8{byte_address[17:10] == 8'd84}};
    wire [7:0] masked85 = q85[7:0] & {8{byte_address[17:10] == 8'd85}};
    wire [7:0] masked86 = q86[7:0] & {8{byte_address[17:10] == 8'd86}};
    wire [7:0] masked87 = q87[7:0] & {8{byte_address[17:10] == 8'd87}};
    wire [7:0] masked88 = q88[7:0] & {8{byte_address[17:10] == 8'd88}};
    wire [7:0] masked89 = q89[7:0] & {8{byte_address[17:10] == 8'd89}};
    wire [7:0] masked90 = q90[7:0] & {8{byte_address[17:10] == 8'd90}};
    wire [7:0] masked91 = q91[7:0] & {8{byte_address[17:10] == 8'd91}};
    wire [7:0] masked92 = q92[7:0] & {8{byte_address[17:10] == 8'd92}};
    wire [7:0] masked93 = q93[7:0] & {8{byte_address[17:10] == 8'd93}};
    wire [7:0] masked94 = q94[7:0] & {8{byte_address[17:10] == 8'd94}};
    wire [7:0] masked95 = q95[7:0] & {8{byte_address[17:10] == 8'd95}};
    wire [7:0] masked96 = q96[7:0] & {8{byte_address[17:10] == 8'd96}};
    wire [7:0] masked97 = q97[7:0] & {8{byte_address[17:10] == 8'd97}};
    wire [7:0] masked98 = q98[7:0] & {8{byte_address[17:10] == 8'd98}};
    wire [7:0] masked99 = q99[7:0] & {8{byte_address[17:10] == 8'd99}};
    wire [7:0] masked100 = q100[7:0] & {8{byte_address[17:10] == 8'd100}};
    wire [7:0] masked101 = q101[7:0] & {8{byte_address[17:10] == 8'd101}};
    wire [7:0] masked102 = q102[7:0] & {8{byte_address[17:10] == 8'd102}};
    wire [7:0] masked103 = q103[7:0] & {8{byte_address[17:10] == 8'd103}};
    wire [7:0] masked104 = q104[7:0] & {8{byte_address[17:10] == 8'd104}};
    wire [7:0] masked105 = q105[7:0] & {8{byte_address[17:10] == 8'd105}};
    wire [7:0] masked106 = q106[7:0] & {8{byte_address[17:10] == 8'd106}};
    wire [7:0] masked107 = q107[7:0] & {8{byte_address[17:10] == 8'd107}};
    wire [7:0] masked108 = q108[7:0] & {8{byte_address[17:10] == 8'd108}};
    wire [7:0] masked109 = q109[7:0] & {8{byte_address[17:10] == 8'd109}};
    wire [7:0] masked110 = q110[7:0] & {8{byte_address[17:10] == 8'd110}};
    wire [7:0] masked111 = q111[7:0] & {8{byte_address[17:10] == 8'd111}};
    wire [7:0] masked112 = q112[7:0] & {8{byte_address[17:10] == 8'd112}};
    wire [7:0] masked113 = q113[7:0] & {8{byte_address[17:10] == 8'd113}};
    wire [7:0] masked114 = q114[7:0] & {8{byte_address[17:10] == 8'd114}};
    wire [7:0] masked115 = q115[7:0] & {8{byte_address[17:10] == 8'd115}};
    wire [7:0] masked116 = q116[7:0] & {8{byte_address[17:10] == 8'd116}};
    wire [7:0] masked117 = q117[7:0] & {8{byte_address[17:10] == 8'd117}};
    wire [7:0] masked118 = q118[7:0] & {8{byte_address[17:10] == 8'd118}};
    wire [7:0] masked119 = q119[7:0] & {8{byte_address[17:10] == 8'd119}};
    wire [7:0] masked120 = q120[7:0] & {8{byte_address[17:10] == 8'd120}};
    wire [7:0] masked121 = q121[7:0] & {8{byte_address[17:10] == 8'd121}};
    wire [7:0] masked122 = q122[7:0] & {8{byte_address[17:10] == 8'd122}};
    wire [7:0] masked123 = q123[7:0] & {8{byte_address[17:10] == 8'd123}};
    wire [7:0] masked124 = q124[7:0] & {8{byte_address[17:10] == 8'd124}};
    wire [7:0] masked125 = q125[7:0] & {8{byte_address[17:10] == 8'd125}};
    wire [7:0] masked126 = q126[7:0] & {8{byte_address[17:10] == 8'd126}};
    wire [7:0] masked127 = q127[7:0] & {8{byte_address[17:10] == 8'd127}};
    wire [7:0] masked128 = q128[7:0] & {8{byte_address[17:10] == 8'd128}};
    wire [7:0] masked129 = q129[7:0] & {8{byte_address[17:10] == 8'd129}};
    wire [7:0] masked130 = q130[7:0] & {8{byte_address[17:10] == 8'd130}};
    wire [7:0] masked131 = q131[7:0] & {8{byte_address[17:10] == 8'd131}};
    wire [7:0] masked132 = q132[7:0] & {8{byte_address[17:10] == 8'd132}};
    wire [7:0] masked133 = q133[7:0] & {8{byte_address[17:10] == 8'd133}};
    wire [7:0] masked134 = q134[7:0] & {8{byte_address[17:10] == 8'd134}};
    wire [7:0] masked135 = q135[7:0] & {8{byte_address[17:10] == 8'd135}};
    wire [7:0] masked136 = q136[7:0] & {8{byte_address[17:10] == 8'd136}};
    wire [7:0] masked137 = q137[7:0] & {8{byte_address[17:10] == 8'd137}};
    wire [7:0] masked138 = q138[7:0] & {8{byte_address[17:10] == 8'd138}};
    wire [7:0] masked139 = q139[7:0] & {8{byte_address[17:10] == 8'd139}};
    wire [7:0] masked140 = q140[7:0] & {8{byte_address[17:10] == 8'd140}};
    wire [7:0] masked141 = q141[7:0] & {8{byte_address[17:10] == 8'd141}};
    wire [7:0] masked142 = q142[7:0] & {8{byte_address[17:10] == 8'd142}};
    wire [7:0] masked143 = q143[7:0] & {8{byte_address[17:10] == 8'd143}};
    wire [7:0] masked144 = q144[7:0] & {8{byte_address[17:10] == 8'd144}};
    wire [7:0] masked145 = q145[7:0] & {8{byte_address[17:10] == 8'd145}};
    wire [7:0] masked146 = q146[7:0] & {8{byte_address[17:10] == 8'd146}};
    wire [7:0] masked147 = q147[7:0] & {8{byte_address[17:10] == 8'd147}};
    wire [7:0] masked148 = q148[7:0] & {8{byte_address[17:10] == 8'd148}};
    wire [7:0] masked149 = q149[7:0] & {8{byte_address[17:10] == 8'd149}};
    wire [7:0] masked150 = q150[7:0] & {8{byte_address[17:10] == 8'd150}};
    wire [7:0] masked151 = q151[7:0] & {8{byte_address[17:10] == 8'd151}};
    wire [7:0] masked152 = q152[7:0] & {8{byte_address[17:10] == 8'd152}};
    wire [7:0] masked153 = q153[7:0] & {8{byte_address[17:10] == 8'd153}};
    wire [7:0] masked154 = q154[7:0] & {8{byte_address[17:10] == 8'd154}};
    wire [7:0] masked155 = q155[7:0] & {8{byte_address[17:10] == 8'd155}};
    wire [7:0] masked156 = q156[7:0] & {8{byte_address[17:10] == 8'd156}};
    wire [7:0] masked157 = q157[7:0] & {8{byte_address[17:10] == 8'd157}};
    wire [7:0] masked158 = q158[7:0] & {8{byte_address[17:10] == 8'd158}};
    wire [7:0] masked159 = q159[7:0] & {8{byte_address[17:10] == 8'd159}};
    wire [7:0] masked160 = q160[7:0] & {8{byte_address[17:10] == 8'd160}};
    wire [7:0] masked161 = q161[7:0] & {8{byte_address[17:10] == 8'd161}};
    wire [7:0] masked162 = q162[7:0] & {8{byte_address[17:10] == 8'd162}};
    wire [7:0] masked163 = q163[7:0] & {8{byte_address[17:10] == 8'd163}};
    wire [7:0] masked164 = q164[7:0] & {8{byte_address[17:10] == 8'd164}};
    wire [7:0] masked165 = q165[7:0] & {8{byte_address[17:10] == 8'd165}};
    wire [7:0] masked166 = q166[7:0] & {8{byte_address[17:10] == 8'd166}};
    wire [7:0] masked167 = q167[7:0] & {8{byte_address[17:10] == 8'd167}};
    wire [7:0] masked168 = q168[7:0] & {8{byte_address[17:10] == 8'd168}};
    wire [7:0] masked169 = q169[7:0] & {8{byte_address[17:10] == 8'd169}};
    wire [7:0] masked170 = q170[7:0] & {8{byte_address[17:10] == 8'd170}};
    wire [7:0] masked171 = q171[7:0] & {8{byte_address[17:10] == 8'd171}};
    wire [7:0] masked172 = q172[7:0] & {8{byte_address[17:10] == 8'd172}};
    wire [7:0] masked173 = q173[7:0] & {8{byte_address[17:10] == 8'd173}};
    wire [7:0] masked174 = q174[7:0] & {8{byte_address[17:10] == 8'd174}};
    wire [7:0] masked175 = q175[7:0] & {8{byte_address[17:10] == 8'd175}};
    wire [7:0] masked176 = q176[7:0] & {8{byte_address[17:10] == 8'd176}};
    wire [7:0] masked177 = q177[7:0] & {8{byte_address[17:10] == 8'd177}};
    wire [7:0] masked178 = q178[7:0] & {8{byte_address[17:10] == 8'd178}};
    wire [7:0] masked179 = q179[7:0] & {8{byte_address[17:10] == 8'd179}};
    wire [7:0] masked180 = q180[7:0] & {8{byte_address[17:10] == 8'd180}};
    wire [7:0] masked181 = q181[7:0] & {8{byte_address[17:10] == 8'd181}};
    wire [7:0] masked182 = q182[7:0] & {8{byte_address[17:10] == 8'd182}};
    wire [7:0] masked183 = q183[7:0] & {8{byte_address[17:10] == 8'd183}};
    wire [7:0] masked184 = q184[7:0] & {8{byte_address[17:10] == 8'd184}};
    wire [7:0] masked185 = q185[7:0] & {8{byte_address[17:10] == 8'd185}};
    wire [7:0] masked186 = q186[7:0] & {8{byte_address[17:10] == 8'd186}};
    wire [7:0] masked187 = q187[7:0] & {8{byte_address[17:10] == 8'd187}};
    wire [7:0] masked188 = q188[7:0] & {8{byte_address[17:10] == 8'd188}};
    wire [7:0] masked189 = q189[7:0] & {8{byte_address[17:10] == 8'd189}};
    wire [7:0] masked190 = q190[7:0] & {8{byte_address[17:10] == 8'd190}};
    wire [7:0] masked191 = q191[7:0] & {8{byte_address[17:10] == 8'd191}};
    // Each region's masked lanes are reduced into a register beside its
    // pinned M10K column, so no clock spans the chip and a 192-input tree.
    reg [7:0] group0, group1, group2, group3, group4, group5, group6;
    always @(posedge clk) begin
        group0 <= masked0 | masked1 | masked2 | masked3 | masked4 | masked5 | masked6 | masked7 |
            masked8 | masked9 | masked10 | masked11 | masked12 | masked13;
        group1 <= masked14 | masked15 | masked16 | masked17 | masked18 | masked19 | masked20 | masked21 |
            masked22 | masked23 | masked24 | masked25 | masked26 | masked27 | masked28 | masked29 |
            masked30 | masked31 | masked32 | masked33 | masked34 | masked35 | masked36 | masked37;
        group2 <= masked38 | masked39 | masked40 | masked41 | masked42 | masked43 | masked44 | masked45;
        group3 <= masked46 | masked47 | masked48 | masked49 | masked50 | masked51 | masked52 | masked53 |
            masked54 | masked55 | masked56 | masked57 | masked58 | masked59 | masked60 | masked61 |
            masked62 | masked63 | masked64 | masked65 | masked66 | masked67 | masked68 | masked69 |
            masked70 | masked71 | masked72 | masked73 | masked74 | masked75 | masked76 | masked77 |
            masked78 | masked79 | masked80 | masked81 | masked82 | masked83 | masked84 | masked85;
        group4 <= masked86 | masked87 | masked88 | masked89 | masked90 | masked91 | masked92 | masked93 |
            masked94 | masked95 | masked96 | masked97 | masked98 | masked99 | masked100 | masked101 |
            masked102 | masked103 | masked104 | masked105 | masked106 | masked107 | masked108 | masked109 |
            masked110 | masked111 | masked112 | masked113 | masked114 | masked115 | masked116 | masked117 |
            masked118 | masked119 | masked120 | masked121 | masked122 | masked123 | masked124 | masked125;
        group5 <= masked126 | masked127 | masked128 | masked129 | masked130 | masked131 | masked132 | masked133 |
            masked134 | masked135 | masked136 | masked137 | masked138 | masked139 | masked140 | masked141 |
            masked142 | masked143 | masked144 | masked145 | masked146 | masked147 | masked148 | masked149 |
            masked150 | masked151 | masked152 | masked153 | masked154 | masked155 | masked156 | masked157 |
            masked158;
        group6 <= masked159 | masked160 | masked161 | masked162 | masked163 | masked164 | masked165 | masked166 |
            masked167 | masked168 | masked169 | masked170 | masked171 | masked172 | masked173 | masked174 |
            masked175 | masked176 | masked177 | masked178 | masked179 | masked180 | masked181 | masked182 |
            masked183 | masked184 | masked185 | masked186 | masked187 | masked188 | masked189 | masked190 |
            masked191;
    end
    assign byte_data = {8{byte_address[17:16] == 2'b11}} |
        group0 | group1 | group2 | group3 | group4 | group5 | group6;
`endif
    always @(posedge clk) begin
        if (reset) begin
            state<=IDLE; byte_address<=0; upper_byte<=0; ready<=0; rdata<=0;
        end else case (state)
            IDLE: if (req) begin byte_address<={address,1'b0}; state<=EVEN_READ; end
            EVEN_READ: begin byte_address[0]<=1; state<=ODD_READ; end
            ODD_READ: state<=EVEN_GROUP;
            EVEN_GROUP: begin upper_byte<=byte_data; state<=ODD_GROUP; end
            ODD_GROUP: begin rdata<={upper_byte,byte_data}; ready<=1; state<=COMPLETE; end
            COMPLETE: if (!req) begin ready<=0; state<=IDLE; end
            default: state<=IDLE;
        endcase
    end
endmodule
