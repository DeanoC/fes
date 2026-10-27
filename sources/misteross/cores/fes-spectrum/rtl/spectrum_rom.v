// SPDX-License-Identifier: GPL-2.0-or-later
// Blank linked ZX Spectrum ROM: 16, 32, 48 or 64 KiB, by build model.
//
// The linked firmware is the model's ROM pages in order: 48K BASIC; 128K
// editor then 48 BASIC; the four +2A/+3 ROMs; Pentagon 128, 48 and TR-DOS. Each
// 1 KiB lane is an individually placed, empty 1024x10 M10K that the ROM map
// authenticates against the routed BELs: lanes 0-15 column 5 rows 32-47,
// 16-23 rows 48-55, 24-31 rows 73-80, 32-63 column 14 rows 32-63. The model
// define selects lanes so routed cells keep flat machine.rom.laneN names.
// The M10K registers its read address; the lane selector is registered
// alongside it and the output register supplies stage two. No Sinclair or
// Amstrad ROM bytes are in this repository or the package.
`include "spectrum_model.vh"
module spectrum_rom #(
    parameter ROM_FILE = "build/diagnostics/fes-spectrum/firmware.hex"
) (
    input  wire        clk,
    input  wire [15:0] address,
    output reg  [7:0]  data
);
`ifdef SPECTRUM_ROM_64K
    localparam integer LANES = 64;
`elsif SPECTRUM_ROM_48K
    localparam integer LANES = 48;
`elsif SPECTRUM_ROM_32K
    localparam integer LANES = 32;
`else
    localparam integer LANES = 16;
`endif
    wire [7:0] source_data;
    // 48 lanes are three pages; the Pentagon never addresses a fourth.
    wire [15:0] used = LANES == 48 ? address : address & 16'(LANES * 1024 - 1);
`ifdef VERILATOR
    reg [7:0] memory [0:LANES*1024-1];
    initial $readmemh(ROM_FILE, memory);
    reg [7:0] sim_stage;
    always @(posedge clk) sim_stage <= memory[used];
    assign source_data = sim_stage;
`else
    wire [9:0] lane_addr = used[9:0];
    wire [9:0] q0, q1, q2, q3, q4, q5, q6, q7, q8, q9, q10, q11, q12, q13, q14, q15;
`ifdef SPECTRUM_ROM_32K
    wire [9:0] q16, q17, q18, q19, q20, q21, q22, q23, q24, q25, q26, q27, q28, q29, q30, q31;
`else
    wire [9:0] q16 = 10'd0;
    wire [9:0] q17 = 10'd0;
    wire [9:0] q18 = 10'd0;
    wire [9:0] q19 = 10'd0;
    wire [9:0] q20 = 10'd0;
    wire [9:0] q21 = 10'd0;
    wire [9:0] q22 = 10'd0;
    wire [9:0] q23 = 10'd0;
    wire [9:0] q24 = 10'd0;
    wire [9:0] q25 = 10'd0;
    wire [9:0] q26 = 10'd0;
    wire [9:0] q27 = 10'd0;
    wire [9:0] q28 = 10'd0;
    wire [9:0] q29 = 10'd0;
    wire [9:0] q30 = 10'd0;
    wire [9:0] q31 = 10'd0;
`endif
`ifdef SPECTRUM_ROM_48K
    wire [9:0] q32, q33, q34, q35, q36, q37, q38, q39, q40, q41, q42, q43, q44, q45, q46, q47;
`else
    wire [9:0] q32 = 10'd0;
    wire [9:0] q33 = 10'd0;
    wire [9:0] q34 = 10'd0;
    wire [9:0] q35 = 10'd0;
    wire [9:0] q36 = 10'd0;
    wire [9:0] q37 = 10'd0;
    wire [9:0] q38 = 10'd0;
    wire [9:0] q39 = 10'd0;
    wire [9:0] q40 = 10'd0;
    wire [9:0] q41 = 10'd0;
    wire [9:0] q42 = 10'd0;
    wire [9:0] q43 = 10'd0;
    wire [9:0] q44 = 10'd0;
    wire [9:0] q45 = 10'd0;
    wire [9:0] q46 = 10'd0;
    wire [9:0] q47 = 10'd0;
`endif
`ifdef SPECTRUM_ROM_64K
    wire [9:0] q48, q49, q50, q51, q52, q53, q54, q55, q56, q57, q58, q59, q60, q61, q62, q63;
`else
    wire [9:0] q48 = 10'd0;
    wire [9:0] q49 = 10'd0;
    wire [9:0] q50 = 10'd0;
    wire [9:0] q51 = 10'd0;
    wire [9:0] q52 = 10'd0;
    wire [9:0] q53 = 10'd0;
    wire [9:0] q54 = 10'd0;
    wire [9:0] q55 = 10'd0;
    wire [9:0] q56 = 10'd0;
    wire [9:0] q57 = 10'd0;
    wire [9:0] q58 = 10'd0;
    wire [9:0] q59 = 10'd0;
    wire [9:0] q60 = 10'd0;
    wire [9:0] q61 = 10'd0;
    wire [9:0] q62 = 10'd0;
    wire [9:0] q63 = 10'd0;
`endif
    wire [511:0] lane_data = {q63[7:0], q62[7:0], q61[7:0], q60[7:0], q59[7:0], q58[7:0], q57[7:0], q56[7:0], q55[7:0], q54[7:0], q53[7:0], q52[7:0], q51[7:0], q50[7:0], q49[7:0], q48[7:0], q47[7:0], q46[7:0], q45[7:0], q44[7:0], q43[7:0], q42[7:0], q41[7:0], q40[7:0], q39[7:0], q38[7:0], q37[7:0], q36[7:0], q35[7:0], q34[7:0], q33[7:0], q32[7:0], q31[7:0], q30[7:0], q29[7:0], q28[7:0], q27[7:0], q26[7:0], q25[7:0], q24[7:0], q23[7:0], q22[7:0], q21[7:0], q20[7:0], q19[7:0], q18[7:0], q17[7:0], q16[7:0], q15[7:0], q14[7:0], q13[7:0], q12[7:0], q11[7:0], q10[7:0], q9[7:0], q8[7:0], q7[7:0], q6[7:0], q5[7:0], q4[7:0], q3[7:0], q2[7:0], q1[7:0], q0[7:0]};
    reg [5:0] lane_d;
    always @(posedge clk) lane_d <= used[15:10];
    assign source_data = lane_data[lane_d * 8 +: 8];
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
`ifdef SPECTRUM_ROM_32K
    (* keep, BEL = "MISTRAL_M10K.5.48.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane16 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q16), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.49.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane17 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q17), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.50.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane18 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q18), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.51.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane19 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q19), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.52.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane20 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q20), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.53.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane21 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q21), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.54.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane22 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q22), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.55.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane23 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q23), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.73.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane24 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q24), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.74.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane25 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q25), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.75.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane26 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q26), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.76.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane27 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q27), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.77.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane28 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q28), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.78.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane29 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q29), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.79.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane30 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q30), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.80.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane31 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q31), .ACLR0(1'b0), .ACLR1(1'b0)
    );
`endif
`ifdef SPECTRUM_ROM_48K
    (* keep, BEL = "MISTRAL_M10K.14.32.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane32 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q32), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.33.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane33 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q33), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.34.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane34 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q34), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.35.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane35 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q35), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.36.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane36 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q36), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.37.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane37 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q37), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.38.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane38 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q38), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.39.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane39 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q39), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.40.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane40 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q40), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.41.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane41 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q41), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.42.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane42 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q42), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.43.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane43 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q43), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.44.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane44 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q44), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.45.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane45 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q45), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.46.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane46 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q46), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.47.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane47 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q47), .ACLR0(1'b0), .ACLR1(1'b0)
    );
`endif
`ifdef SPECTRUM_ROM_64K
    (* keep, BEL = "MISTRAL_M10K.14.48.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane48 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q48), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.49.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane49 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q49), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.50.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane50 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q50), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.51.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane51 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q51), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.52.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane52 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q52), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.53.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane53 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q53), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.54.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane54 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q54), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.55.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane55 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q55), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.56.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane56 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q56), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.57.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane57 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q57), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.58.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane58 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q58), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.59.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane59 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q59), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.60.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane60 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q60), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.61.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane61 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q61), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.62.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane62 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q62), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.14.63.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane63 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q63), .ACLR0(1'b0), .ACLR1(1'b0)
    );
`endif
`endif
    always @(posedge clk)
        data <= source_data;
endmodule
