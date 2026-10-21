// SPDX-License-Identifier: GPL-2.0-or-later
// BASIC 8 KiB, KERNAL 8 KiB, character ROM 4 KiB, then 1541 ROM 16 KiB.
// CPU read latency is
// two system clocks; VIC character reads use one native system clock. Character
// lanes share a single INIT through true-dual-port CPU/VIC reads. The drive
// has an independent one-system-clock port into the final sixteen lanes.
module c64_rom (
 input wire clk,input wire [14:0] address,output reg [7:0] data,
 input wire video_clk,input wire [11:0] video_address,output wire [7:0] video_data,
 input wire [13:0] drive_address,output wire [7:0] drive_data
);
 wire [7:0] source_data;
`ifdef VERILATOR
 reg [7:0] memory [0:36863];
 string firmware_hex;
 initial begin
  if(!$value$plusargs("C64_FIRMWARE_HEX=%s",firmware_hex))
   firmware_hex="build/diagnostics/fes-c64/firmware.hex";
  $readmemh(firmware_hex,memory);
 end
 reg [7:0] cpu_stage,video_stage,drive_stage;
 always @(posedge clk)cpu_stage<=address<20480?memory[address]:8'hff;
 always @(posedge video_clk)video_stage<=memory[16384+video_address];
 always @(posedge clk)drive_stage<=memory[20480+drive_address];
 assign source_data=cpu_stage;assign video_data=video_stage;assign drive_data=drive_stage;
`else
 reg [4:0] bank_d;reg [1:0] video_bank_d;reg [3:0] drive_bank_d;
 always @(posedge clk)begin
  bank_d<=address[14:10];drive_bank_d<=drive_address[13:10];
 end
 always @(posedge video_clk)video_bank_d<=video_address[11:10];
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
 wire [9:0] q16;
 wire [9:0] q17;
 wire [9:0] q18;
 wire [9:0] q19;
 wire [9:0] q20;
 wire [9:0] q21;
 wire [9:0] q22;
 wire [9:0] q23;
 wire [9:0] q24;
 wire [9:0] q25;
 wire [9:0] q26;
 wire [9:0] q27;
 wire [9:0] q28;
 wire [9:0] q29;
 wire [9:0] q30;
 wire [9:0] q31;
 wire [9:0] q32;
 wire [9:0] q33;
 wire [9:0] q34;
 wire [9:0] q35;
 wire [9:0] v16;
 wire [9:0] v17;
 wire [9:0] v18;
 wire [9:0] v19;
 reg [7:0] selected;
 always @* begin
  case(bank_d)
   5'd0:selected=q0[7:0];
   5'd1:selected=q1[7:0];
   5'd2:selected=q2[7:0];
   5'd3:selected=q3[7:0];
   5'd4:selected=q4[7:0];
   5'd5:selected=q5[7:0];
   5'd6:selected=q6[7:0];
   5'd7:selected=q7[7:0];
   5'd8:selected=q8[7:0];
   5'd9:selected=q9[7:0];
   5'd10:selected=q10[7:0];
   5'd11:selected=q11[7:0];
   5'd12:selected=q12[7:0];
   5'd13:selected=q13[7:0];
   5'd14:selected=q14[7:0];
   5'd15:selected=q15[7:0];
   5'd16:selected=q16[7:0];
   5'd17:selected=q17[7:0];
   5'd18:selected=q18[7:0];
   5'd19:selected=q19[7:0];
   default:selected=8'hff;
  endcase
 end
 assign source_data=selected;
 reg [7:0] vselected;
 always @* begin
  case(video_bank_d)
   2'd0:vselected=v16[7:0];
   2'd1:vselected=v17[7:0];
   2'd2:vselected=v18[7:0];
   2'd3:vselected=v19[7:0];
  endcase
 end
 assign video_data=vselected;
 reg [7:0] drive_selected;
 always @* begin
  case(drive_bank_d)
   4'd0:drive_selected=q20[7:0];
   4'd1:drive_selected=q21[7:0];
   4'd2:drive_selected=q22[7:0];
   4'd3:drive_selected=q23[7:0];
   4'd4:drive_selected=q24[7:0];
   4'd5:drive_selected=q25[7:0];
   4'd6:drive_selected=q26[7:0];
   4'd7:drive_selected=q27[7:0];
   4'd8:drive_selected=q28[7:0];
   4'd9:drive_selected=q29[7:0];
   4'd10:drive_selected=q30[7:0];
   4'd11:drive_selected=q31[7:0];
   4'd12:drive_selected=q32[7:0];
   4'd13:drive_selected=q33[7:0];
   4'd14:drive_selected=q34[7:0];
   4'd15:drive_selected=q35[7:0];
  endcase
 end
 assign drive_data=drive_selected;
 (* keep,BEL="MISTRAL_M10K.5.32.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane0 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(address[9:0]),.B1DATA(q0),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.5.33.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane1 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(address[9:0]),.B1DATA(q1),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.5.34.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane2 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(address[9:0]),.B1DATA(q2),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.5.35.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane3 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(address[9:0]),.B1DATA(q3),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.5.36.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane4 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(address[9:0]),.B1DATA(q4),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.5.37.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane5 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(address[9:0]),.B1DATA(q5),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.5.38.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane6 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(address[9:0]),.B1DATA(q6),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.5.39.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane7 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(address[9:0]),.B1DATA(q7),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.5.40.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane8 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(address[9:0]),.B1DATA(q8),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.5.41.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane9 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(address[9:0]),.B1DATA(q9),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.5.42.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane10 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(address[9:0]),.B1DATA(q10),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.5.43.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane11 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(address[9:0]),.B1DATA(q11),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.5.44.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane12 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(address[9:0]),.B1DATA(q12),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.5.45.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane13 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(address[9:0]),.B1DATA(q13),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.5.46.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane14 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(address[9:0]),.B1DATA(q14),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.5.47.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane15 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(address[9:0]),.B1DATA(q15),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.5.48.0" *)
 MISTRAL_M10K_TDP #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane16 (
 .CLK1(clk),.CLK2(video_clk),.A1ADDR(address[9:0]),.B1ADDR(video_address[9:0]),
 .A1DATA(10'd0),.B1DATA(10'd0),.A1Q(q16),.B1Q(v16),
 .A1EN(1'b1),.B1EN(1'b1),.A1WE(1'b0),.B1WE(1'b0),
 .ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.5.49.0" *)
 MISTRAL_M10K_TDP #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane17 (
 .CLK1(clk),.CLK2(video_clk),.A1ADDR(address[9:0]),.B1ADDR(video_address[9:0]),
 .A1DATA(10'd0),.B1DATA(10'd0),.A1Q(q17),.B1Q(v17),
 .A1EN(1'b1),.B1EN(1'b1),.A1WE(1'b0),.B1WE(1'b0),
 .ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.5.50.0" *)
 MISTRAL_M10K_TDP #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane18 (
 .CLK1(clk),.CLK2(video_clk),.A1ADDR(address[9:0]),.B1ADDR(video_address[9:0]),
 .A1DATA(10'd0),.B1DATA(10'd0),.A1Q(q18),.B1Q(v18),
 .A1EN(1'b1),.B1EN(1'b1),.A1WE(1'b0),.B1WE(1'b0),
 .ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.5.51.0" *)
 MISTRAL_M10K_TDP #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane19 (
 .CLK1(clk),.CLK2(video_clk),.A1ADDR(address[9:0]),.B1ADDR(video_address[9:0]),
 .A1DATA(10'd0),.B1DATA(10'd0),.A1Q(q19),.B1Q(v19),
 .A1EN(1'b1),.B1EN(1'b1),.A1WE(1'b0),.B1WE(1'b0),
 .ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.14.32.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane20 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(drive_address[9:0]),.B1DATA(q20),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.14.33.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane21 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(drive_address[9:0]),.B1DATA(q21),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.14.34.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane22 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(drive_address[9:0]),.B1DATA(q22),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.14.35.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane23 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(drive_address[9:0]),.B1DATA(q23),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.14.36.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane24 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(drive_address[9:0]),.B1DATA(q24),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.14.37.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane25 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(drive_address[9:0]),.B1DATA(q25),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.14.38.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane26 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(drive_address[9:0]),.B1DATA(q26),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.14.39.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane27 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(drive_address[9:0]),.B1DATA(q27),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.14.40.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane28 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(drive_address[9:0]),.B1DATA(q28),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.14.41.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane29 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(drive_address[9:0]),.B1DATA(q29),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.14.42.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane30 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(drive_address[9:0]),.B1DATA(q30),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.14.43.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane31 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(drive_address[9:0]),.B1DATA(q31),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.14.44.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane32 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(drive_address[9:0]),.B1DATA(q32),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.14.45.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane33 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(drive_address[9:0]),.B1DATA(q33),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.14.46.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane34 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(drive_address[9:0]),.B1DATA(q34),.ACLR0(1'b0),.ACLR1(1'b0));
 (* keep,BEL="MISTRAL_M10K.14.47.0" *)
 MISTRAL_M10K #(.CFG_ABITS(10),.CFG_DBITS(10),.CFG_ASYNC_READ(0),.INIT(10240'b0)) lane35 (
 .CLK1(clk),.A1ADDR(10'd0),.A1DATA(10'd0),.A1EN(1'b1),
 .B1EN(1'b1),.B1ADDR(drive_address[9:0]),.B1DATA(q35),.ACLR0(1'b0),.ACLR1(1'b0));
`endif
 always @(posedge clk)data<=source_data;
endmodule
