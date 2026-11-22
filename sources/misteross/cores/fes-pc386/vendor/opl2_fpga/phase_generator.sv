/*******************************************************************************
#   +html+<pre>
#
#   FILENAME: phase_generator.sv
#   AUTHOR: Greg Taylor     CREATION DATE: 13 Oct 2014
#
#   DESCRIPTION:
#
#   CHANGE HISTORY:
#   3 June 2024     Greg Taylor
#       Refactored to match implementation in https://github.com/nukeykt/Nuked-OPL3
#
#   13 Oct 2014    Greg Taylor
#       Initial version
#
#   Copyright (C) 2014 Greg Taylor <gtaylor@sonic.net>
#
#   This file is part of OPL3 FPGA.
#
#   OPL3 FPGA is free software: you can redistribute it and/or modify
#   it under the terms of the GNU Lesser General Public License as published by
#   the Free Software Foundation, either version 3 of the License, or
#   (at your option) any later version.
#
#   OPL3 FPGA is distributed in the hope that it will be useful,
#   but WITHOUT ANY WARRANTY; without even the implied warranty of
#   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
#   GNU Lesser General Public License for more details.
#
#   You should have received a copy of the GNU Lesser General Public License
#   along with OPL3 FPGA.  If not, see <http://www.gnu.org/licenses/>.
#
#   Original Java Code:
#   Copyright (C) 2008 Robson Cozendey <robson@cozendey.com>
#
#   Original C++ Code:
#   Copyright (C) 2013-2020 Nuke.YKT
#
#   Some code based on forum posts in:
#   http://forums.submarine.org.uk/phpBB/viewforum.php?f=9,
#   Copyright (C) 2010-2013 by carbon14 and opl3
#
#******************************************************************************/
`timescale 1ns / 1ps
`default_nettype none
/* altera message_off 10230 */

module phase_generator
    import opl2_pkg::*;
(
    input wire clk,
    input wire sample_clk_en,
    input wire [OP_NUM_WIDTH-1:0] op_num,
    input wire [PHASE_ACC_WIDTH-1:0] phase_inc_p2,
    input wire [REG_WS_WIDTH-1:0] ws,
    input wire [FINAL_ENV_WIDTH-1:0] env_p3,
    input wire pg_reset_p2,
    input wire [OP_OUT_WIDTH-1:0] modulation_p1,
    input var operator_t op_type_p0,
    output logic signed [OP_OUT_WIDTH-1:0] out_p6 = 0
);
    localparam LOG_SIN_OUT_WIDTH = 12;
    localparam EXP_OUT_WIDTH = 10;
    localparam OPERATOR_T_BITS = 3;           // $bits(operator_t)
    localparam PIPELINE_DELAY = 6;

    logic [PIPELINE_DELAY:1] sample_clk_en_p;
    logic [PHASE_ACC_WIDTH-1:0] phase_acc_p2;
    logic [PHASE_FINAL_WIDTH-1:0] phase_acc_shifted_p2;
    logic [PHASE_ACC_WIDTH-1:0] phase_acc_p3 = 0;
    logic [PHASE_FINAL_WIDTH-1:0] final_phase_p3;
    logic [7:0] theta_p3;
    logic [PHASE_FINAL_WIDTH-1:0] rhythm_phase_p3;
    logic [LOG_SIN_OUT_WIDTH-1:0] log_sin_out_p4;
    logic [OP_OUT_WIDTH-1:0] pre_gain_p4;
    logic [FINAL_ENV_WIDTH+4-1:0] post_gain_p4; // need extra bits to detect overflow
    logic [PIPELINE_DELAY:1] [REG_WS_WIDTH-1:0] ws_p;
    logic [PIPELINE_DELAY:1] [OP_NUM_WIDTH-1:0] op_num_p;
    logic [PIPELINE_DELAY:1] [OPERATOR_T_BITS-1:0] op_type_p;
    logic [PIPELINE_DELAY:2] [OP_OUT_WIDTH-1:0] modulation_p;
    logic [PIPELINE_DELAY:4] [PHASE_FINAL_WIDTH-1:0] final_phase_p;
    logic [FINAL_ENV_WIDTH+3-1:0] env_shifted_p4 = 0;
    logic [OP_OUT_WIDTH-1:0] level_p4;
    logic [OP_OUT_WIDTH-1:0] level_p5 = 0;
    logic [EXP_OUT_WIDTH-1:0] exp_out_p5;
    logic [OP_OUT_WIDTH-1:0] exp_out_post_level_p5;
    logic [OP_OUT_WIDTH-1:0] neg_p5 = 0;

    pipeline_sr #(
        .ENDING_CYCLE(PIPELINE_DELAY)
    ) sample_clk_en_sr (
        .clk,
        .in(sample_clk_en),
        .out(sample_clk_en_p)
    );

    pipeline_sr #(
        .DATA_WIDTH(OP_NUM_WIDTH),
        .ENDING_CYCLE(PIPELINE_DELAY)
    ) op_num_sr (
        .clk,
        .in(op_num),
        .out(op_num_p)
    );

    pipeline_sr #(
        .DATA_WIDTH(OPERATOR_T_BITS),
        .ENDING_CYCLE(PIPELINE_DELAY)
    ) op_type_sr (
        .clk,
        .in(op_type_p0),
        .out(op_type_p)
    );

    pipeline_sr #(
        .DATA_WIDTH(OP_OUT_WIDTH),
        .STARTING_CYCLE(2),
        .ENDING_CYCLE(PIPELINE_DELAY)
    ) modulation_sr (
        .clk,
        .in(modulation_p1),
        .out(modulation_p)
    );

    pipeline_sr #(
        .DATA_WIDTH(PHASE_FINAL_WIDTH),
        .STARTING_CYCLE(4),
        .ENDING_CYCLE(PIPELINE_DELAY)
    ) final_phase_sr (
        .clk,
        .in(final_phase_p3),
        .out(final_phase_p)
    );

    pipeline_sr #(
        .DATA_WIDTH(REG_WS_WIDTH),
        .ENDING_CYCLE(PIPELINE_DELAY)
    ) ws_post_opl_sr (
        .clk,
        .in(ws),
        .out(ws_p)
    );

    mem_single_bank #(
        .DATA_WIDTH(PHASE_ACC_WIDTH),
        .DEPTH(NUM_OPERATORS_PER_BANK),
        .OUTPUT_DELAY(2),
        .DEFAULT_VALUE(0)
    ) phase_acc_mem (
        .clk,
        .wea(sample_clk_en_p[3]),
        .reb(sample_clk_en),
        .addra(op_num_p[3]),
        .addrb(op_num),
        .dia(phase_acc_p3),
        .dob(phase_acc_p2)
    );

    /*
     * Phase Accumulator. Modulation and rhythm get added to the final phase but not
     * back into the accumulator.
     */
    always_ff @(posedge clk)
        if (pg_reset_p2)
            phase_acc_p3 <= phase_inc_p2;
        else
            phase_acc_p3 <= phase_acc_p2 + phase_inc_p2;

    always_comb phase_acc_shifted_p2 = phase_acc_p2 >> 9;

    /*
     * Some rhythm instruments modify the phase, otherwise pass-through normally.
     * Bottom bits of phase accumulator are fractional and get dropped off.
     */
     calc_rhythm_phase calc_rhythm_phase (
        .clk,
        .sample_clk_en,
        .op_num,
        .phase_p2(phase_acc_shifted_p2),
        .op_type_p0,
        .rhythm_phase_p3
    );

    always_comb begin
        final_phase_p3 = rhythm_phase_p3 + modulation_p[3];

        unique case (ws_p[3])
        0, 1, 2: theta_p3 = final_phase_p3[8] ? final_phase_p3[7:0] ^ 'hff : final_phase_p3[7:0];
        3:       theta_p3 = final_phase_p3[7:0];
        endcase
    end


    always_ff @(posedge clk)
        env_shifted_p4 <= env_p3 << 3;

    always_comb begin
        unique case (ws_p[4])
        0, 2:       pre_gain_p4 = log_sin_out_p4;
        1:          pre_gain_p4 = final_phase_p[4][9] ? 'h1000 : log_sin_out_p4; // setting msb effectively mutes
        3:          pre_gain_p4 = final_phase_p[4][8] ? 'h1000 : log_sin_out_p4;
        endcase

        post_gain_p4 = pre_gain_p4 + env_shifted_p4;
        level_p4 = post_gain_p4 > 'h1fff ? 'h1fff : post_gain_p4; // clamp level
    end

    opl2_phase_rom phase_rom (
        .clk,
        .theta(theta_p3),
        .exp_in(~post_gain_p4[7:0]),
        .log_sin(log_sin_out_p4),
        .exp_out(exp_out_p5)
    );

    always_ff @(posedge clk) begin
        level_p5 <= level_p4;

        neg_p5 <= 0;
        if (ws_p[4] == 0)
            neg_p5 <= final_phase_p[4][9] ? '1 : 0;
    end

    always_comb
        exp_out_post_level_p5 = ((exp_out_p5 | 'h400) << 1) >> (level_p5 >> 8);

    always_ff @(posedge clk)
        out_p6 <= exp_out_post_level_p5 ^ neg_p5;

endmodule

/*
 * The tables of upstream's opl2_log_sine_lut and opl2_exp_lut in one
 * initialised array read through two registered ports, so that Quartus builds
 * both from a single RAM block. Upstream's case statements became logic in the
 * pc386 seal's netlist (proc -norom). Rows 000h-0FFh: the log-sine of a quarter
 * wave, round(-log2(sin((x + 0.5) * pi / 512)) * 256). Rows 100h-1FFh:
 * round((2 ** (x / 256) - 1) * 1024). Values, latency and power-on outputs are
 * upstream's.
 */
module opl2_phase_rom (
    input wire clk,
    input wire [7:0] theta,
    input wire [7:0] exp_in,
    output logic [11:0] log_sin = 0,
    output logic [9:0] exp_out = 0
);
    logic [11:0] rom [0:511];
    initial begin
        rom[9'h000] = 2137;
        rom[9'h001] = 1731;
        rom[9'h002] = 1543;
        rom[9'h003] = 1419;
        rom[9'h004] = 1326;
        rom[9'h005] = 1252;
        rom[9'h006] = 1190;
        rom[9'h007] = 1137;
        rom[9'h008] = 1091;
        rom[9'h009] = 1050;
        rom[9'h00a] = 1013;
        rom[9'h00b] = 979;
        rom[9'h00c] = 949;
        rom[9'h00d] = 920;
        rom[9'h00e] = 894;
        rom[9'h00f] = 869;
        rom[9'h010] = 846;
        rom[9'h011] = 825;
        rom[9'h012] = 804;
        rom[9'h013] = 785;
        rom[9'h014] = 767;
        rom[9'h015] = 749;
        rom[9'h016] = 732;
        rom[9'h017] = 717;
        rom[9'h018] = 701;
        rom[9'h019] = 687;
        rom[9'h01a] = 672;
        rom[9'h01b] = 659;
        rom[9'h01c] = 646;
        rom[9'h01d] = 633;
        rom[9'h01e] = 621;
        rom[9'h01f] = 609;
        rom[9'h020] = 598;
        rom[9'h021] = 587;
        rom[9'h022] = 576;
        rom[9'h023] = 566;
        rom[9'h024] = 556;
        rom[9'h025] = 546;
        rom[9'h026] = 536;
        rom[9'h027] = 527;
        rom[9'h028] = 518;
        rom[9'h029] = 509;
        rom[9'h02a] = 501;
        rom[9'h02b] = 492;
        rom[9'h02c] = 484;
        rom[9'h02d] = 476;
        rom[9'h02e] = 468;
        rom[9'h02f] = 461;
        rom[9'h030] = 453;
        rom[9'h031] = 446;
        rom[9'h032] = 439;
        rom[9'h033] = 432;
        rom[9'h034] = 425;
        rom[9'h035] = 418;
        rom[9'h036] = 411;
        rom[9'h037] = 405;
        rom[9'h038] = 399;
        rom[9'h039] = 392;
        rom[9'h03a] = 386;
        rom[9'h03b] = 380;
        rom[9'h03c] = 375;
        rom[9'h03d] = 369;
        rom[9'h03e] = 363;
        rom[9'h03f] = 358;
        rom[9'h040] = 352;
        rom[9'h041] = 347;
        rom[9'h042] = 341;
        rom[9'h043] = 336;
        rom[9'h044] = 331;
        rom[9'h045] = 326;
        rom[9'h046] = 321;
        rom[9'h047] = 316;
        rom[9'h048] = 311;
        rom[9'h049] = 307;
        rom[9'h04a] = 302;
        rom[9'h04b] = 297;
        rom[9'h04c] = 293;
        rom[9'h04d] = 289;
        rom[9'h04e] = 284;
        rom[9'h04f] = 280;
        rom[9'h050] = 276;
        rom[9'h051] = 271;
        rom[9'h052] = 267;
        rom[9'h053] = 263;
        rom[9'h054] = 259;
        rom[9'h055] = 255;
        rom[9'h056] = 251;
        rom[9'h057] = 248;
        rom[9'h058] = 244;
        rom[9'h059] = 240;
        rom[9'h05a] = 236;
        rom[9'h05b] = 233;
        rom[9'h05c] = 229;
        rom[9'h05d] = 226;
        rom[9'h05e] = 222;
        rom[9'h05f] = 219;
        rom[9'h060] = 215;
        rom[9'h061] = 212;
        rom[9'h062] = 209;
        rom[9'h063] = 205;
        rom[9'h064] = 202;
        rom[9'h065] = 199;
        rom[9'h066] = 196;
        rom[9'h067] = 193;
        rom[9'h068] = 190;
        rom[9'h069] = 187;
        rom[9'h06a] = 184;
        rom[9'h06b] = 181;
        rom[9'h06c] = 178;
        rom[9'h06d] = 175;
        rom[9'h06e] = 172;
        rom[9'h06f] = 169;
        rom[9'h070] = 167;
        rom[9'h071] = 164;
        rom[9'h072] = 161;
        rom[9'h073] = 159;
        rom[9'h074] = 156;
        rom[9'h075] = 153;
        rom[9'h076] = 151;
        rom[9'h077] = 148;
        rom[9'h078] = 146;
        rom[9'h079] = 143;
        rom[9'h07a] = 141;
        rom[9'h07b] = 138;
        rom[9'h07c] = 136;
        rom[9'h07d] = 134;
        rom[9'h07e] = 131;
        rom[9'h07f] = 129;
        rom[9'h080] = 127;
        rom[9'h081] = 125;
        rom[9'h082] = 122;
        rom[9'h083] = 120;
        rom[9'h084] = 118;
        rom[9'h085] = 116;
        rom[9'h086] = 114;
        rom[9'h087] = 112;
        rom[9'h088] = 110;
        rom[9'h089] = 108;
        rom[9'h08a] = 106;
        rom[9'h08b] = 104;
        rom[9'h08c] = 102;
        rom[9'h08d] = 100;
        rom[9'h08e] = 98;
        rom[9'h08f] = 96;
        rom[9'h090] = 94;
        rom[9'h091] = 92;
        rom[9'h092] = 91;
        rom[9'h093] = 89;
        rom[9'h094] = 87;
        rom[9'h095] = 85;
        rom[9'h096] = 83;
        rom[9'h097] = 82;
        rom[9'h098] = 80;
        rom[9'h099] = 78;
        rom[9'h09a] = 77;
        rom[9'h09b] = 75;
        rom[9'h09c] = 74;
        rom[9'h09d] = 72;
        rom[9'h09e] = 70;
        rom[9'h09f] = 69;
        rom[9'h0a0] = 67;
        rom[9'h0a1] = 66;
        rom[9'h0a2] = 64;
        rom[9'h0a3] = 63;
        rom[9'h0a4] = 62;
        rom[9'h0a5] = 60;
        rom[9'h0a6] = 59;
        rom[9'h0a7] = 57;
        rom[9'h0a8] = 56;
        rom[9'h0a9] = 55;
        rom[9'h0aa] = 53;
        rom[9'h0ab] = 52;
        rom[9'h0ac] = 51;
        rom[9'h0ad] = 49;
        rom[9'h0ae] = 48;
        rom[9'h0af] = 47;
        rom[9'h0b0] = 46;
        rom[9'h0b1] = 45;
        rom[9'h0b2] = 43;
        rom[9'h0b3] = 42;
        rom[9'h0b4] = 41;
        rom[9'h0b5] = 40;
        rom[9'h0b6] = 39;
        rom[9'h0b7] = 38;
        rom[9'h0b8] = 37;
        rom[9'h0b9] = 36;
        rom[9'h0ba] = 35;
        rom[9'h0bb] = 34;
        rom[9'h0bc] = 33;
        rom[9'h0bd] = 32;
        rom[9'h0be] = 31;
        rom[9'h0bf] = 30;
        rom[9'h0c0] = 29;
        rom[9'h0c1] = 28;
        rom[9'h0c2] = 27;
        rom[9'h0c3] = 26;
        rom[9'h0c4] = 25;
        rom[9'h0c5] = 24;
        rom[9'h0c6] = 23;
        rom[9'h0c7] = 23;
        rom[9'h0c8] = 22;
        rom[9'h0c9] = 21;
        rom[9'h0ca] = 20;
        rom[9'h0cb] = 20;
        rom[9'h0cc] = 19;
        rom[9'h0cd] = 18;
        rom[9'h0ce] = 17;
        rom[9'h0cf] = 17;
        rom[9'h0d0] = 16;
        rom[9'h0d1] = 15;
        rom[9'h0d2] = 15;
        rom[9'h0d3] = 14;
        rom[9'h0d4] = 13;
        rom[9'h0d5] = 13;
        rom[9'h0d6] = 12;
        rom[9'h0d7] = 12;
        rom[9'h0d8] = 11;
        rom[9'h0d9] = 10;
        rom[9'h0da] = 10;
        rom[9'h0db] = 9;
        rom[9'h0dc] = 9;
        rom[9'h0dd] = 8;
        rom[9'h0de] = 8;
        rom[9'h0df] = 7;
        rom[9'h0e0] = 7;
        rom[9'h0e1] = 7;
        rom[9'h0e2] = 6;
        rom[9'h0e3] = 6;
        rom[9'h0e4] = 5;
        rom[9'h0e5] = 5;
        rom[9'h0e6] = 5;
        rom[9'h0e7] = 4;
        rom[9'h0e8] = 4;
        rom[9'h0e9] = 4;
        rom[9'h0ea] = 3;
        rom[9'h0eb] = 3;
        rom[9'h0ec] = 3;
        rom[9'h0ed] = 2;
        rom[9'h0ee] = 2;
        rom[9'h0ef] = 2;
        rom[9'h0f0] = 2;
        rom[9'h0f1] = 1;
        rom[9'h0f2] = 1;
        rom[9'h0f3] = 1;
        rom[9'h0f4] = 1;
        rom[9'h0f5] = 1;
        rom[9'h0f6] = 1;
        rom[9'h0f7] = 1;
        rom[9'h0f8] = 0;
        rom[9'h0f9] = 0;
        rom[9'h0fa] = 0;
        rom[9'h0fb] = 0;
        rom[9'h0fc] = 0;
        rom[9'h0fd] = 0;
        rom[9'h0fe] = 0;
        rom[9'h0ff] = 0;
        rom[9'h100] = 0;
        rom[9'h101] = 3;
        rom[9'h102] = 6;
        rom[9'h103] = 8;
        rom[9'h104] = 11;
        rom[9'h105] = 14;
        rom[9'h106] = 17;
        rom[9'h107] = 20;
        rom[9'h108] = 22;
        rom[9'h109] = 25;
        rom[9'h10a] = 28;
        rom[9'h10b] = 31;
        rom[9'h10c] = 34;
        rom[9'h10d] = 37;
        rom[9'h10e] = 40;
        rom[9'h10f] = 42;
        rom[9'h110] = 45;
        rom[9'h111] = 48;
        rom[9'h112] = 51;
        rom[9'h113] = 54;
        rom[9'h114] = 57;
        rom[9'h115] = 60;
        rom[9'h116] = 63;
        rom[9'h117] = 66;
        rom[9'h118] = 69;
        rom[9'h119] = 72;
        rom[9'h11a] = 75;
        rom[9'h11b] = 78;
        rom[9'h11c] = 81;
        rom[9'h11d] = 84;
        rom[9'h11e] = 87;
        rom[9'h11f] = 90;
        rom[9'h120] = 93;
        rom[9'h121] = 96;
        rom[9'h122] = 99;
        rom[9'h123] = 102;
        rom[9'h124] = 105;
        rom[9'h125] = 108;
        rom[9'h126] = 111;
        rom[9'h127] = 114;
        rom[9'h128] = 117;
        rom[9'h129] = 120;
        rom[9'h12a] = 123;
        rom[9'h12b] = 126;
        rom[9'h12c] = 130;
        rom[9'h12d] = 133;
        rom[9'h12e] = 136;
        rom[9'h12f] = 139;
        rom[9'h130] = 142;
        rom[9'h131] = 145;
        rom[9'h132] = 148;
        rom[9'h133] = 152;
        rom[9'h134] = 155;
        rom[9'h135] = 158;
        rom[9'h136] = 161;
        rom[9'h137] = 164;
        rom[9'h138] = 168;
        rom[9'h139] = 171;
        rom[9'h13a] = 174;
        rom[9'h13b] = 177;
        rom[9'h13c] = 181;
        rom[9'h13d] = 184;
        rom[9'h13e] = 187;
        rom[9'h13f] = 190;
        rom[9'h140] = 194;
        rom[9'h141] = 197;
        rom[9'h142] = 200;
        rom[9'h143] = 204;
        rom[9'h144] = 207;
        rom[9'h145] = 210;
        rom[9'h146] = 214;
        rom[9'h147] = 217;
        rom[9'h148] = 220;
        rom[9'h149] = 224;
        rom[9'h14a] = 227;
        rom[9'h14b] = 231;
        rom[9'h14c] = 234;
        rom[9'h14d] = 237;
        rom[9'h14e] = 241;
        rom[9'h14f] = 244;
        rom[9'h150] = 248;
        rom[9'h151] = 251;
        rom[9'h152] = 255;
        rom[9'h153] = 258;
        rom[9'h154] = 262;
        rom[9'h155] = 265;
        rom[9'h156] = 268;
        rom[9'h157] = 272;
        rom[9'h158] = 276;
        rom[9'h159] = 279;
        rom[9'h15a] = 283;
        rom[9'h15b] = 286;
        rom[9'h15c] = 290;
        rom[9'h15d] = 293;
        rom[9'h15e] = 297;
        rom[9'h15f] = 300;
        rom[9'h160] = 304;
        rom[9'h161] = 308;
        rom[9'h162] = 311;
        rom[9'h163] = 315;
        rom[9'h164] = 318;
        rom[9'h165] = 322;
        rom[9'h166] = 326;
        rom[9'h167] = 329;
        rom[9'h168] = 333;
        rom[9'h169] = 337;
        rom[9'h16a] = 340;
        rom[9'h16b] = 344;
        rom[9'h16c] = 348;
        rom[9'h16d] = 352;
        rom[9'h16e] = 355;
        rom[9'h16f] = 359;
        rom[9'h170] = 363;
        rom[9'h171] = 367;
        rom[9'h172] = 370;
        rom[9'h173] = 374;
        rom[9'h174] = 378;
        rom[9'h175] = 382;
        rom[9'h176] = 385;
        rom[9'h177] = 389;
        rom[9'h178] = 393;
        rom[9'h179] = 397;
        rom[9'h17a] = 401;
        rom[9'h17b] = 405;
        rom[9'h17c] = 409;
        rom[9'h17d] = 412;
        rom[9'h17e] = 416;
        rom[9'h17f] = 420;
        rom[9'h180] = 424;
        rom[9'h181] = 428;
        rom[9'h182] = 432;
        rom[9'h183] = 436;
        rom[9'h184] = 440;
        rom[9'h185] = 444;
        rom[9'h186] = 448;
        rom[9'h187] = 452;
        rom[9'h188] = 456;
        rom[9'h189] = 460;
        rom[9'h18a] = 464;
        rom[9'h18b] = 468;
        rom[9'h18c] = 472;
        rom[9'h18d] = 476;
        rom[9'h18e] = 480;
        rom[9'h18f] = 484;
        rom[9'h190] = 488;
        rom[9'h191] = 492;
        rom[9'h192] = 496;
        rom[9'h193] = 501;
        rom[9'h194] = 505;
        rom[9'h195] = 509;
        rom[9'h196] = 513;
        rom[9'h197] = 517;
        rom[9'h198] = 521;
        rom[9'h199] = 526;
        rom[9'h19a] = 530;
        rom[9'h19b] = 534;
        rom[9'h19c] = 538;
        rom[9'h19d] = 542;
        rom[9'h19e] = 547;
        rom[9'h19f] = 551;
        rom[9'h1a0] = 555;
        rom[9'h1a1] = 560;
        rom[9'h1a2] = 564;
        rom[9'h1a3] = 568;
        rom[9'h1a4] = 572;
        rom[9'h1a5] = 577;
        rom[9'h1a6] = 581;
        rom[9'h1a7] = 585;
        rom[9'h1a8] = 590;
        rom[9'h1a9] = 594;
        rom[9'h1aa] = 599;
        rom[9'h1ab] = 603;
        rom[9'h1ac] = 607;
        rom[9'h1ad] = 612;
        rom[9'h1ae] = 616;
        rom[9'h1af] = 621;
        rom[9'h1b0] = 625;
        rom[9'h1b1] = 630;
        rom[9'h1b2] = 634;
        rom[9'h1b3] = 639;
        rom[9'h1b4] = 643;
        rom[9'h1b5] = 648;
        rom[9'h1b6] = 652;
        rom[9'h1b7] = 657;
        rom[9'h1b8] = 661;
        rom[9'h1b9] = 666;
        rom[9'h1ba] = 670;
        rom[9'h1bb] = 675;
        rom[9'h1bc] = 680;
        rom[9'h1bd] = 684;
        rom[9'h1be] = 689;
        rom[9'h1bf] = 693;
        rom[9'h1c0] = 698;
        rom[9'h1c1] = 703;
        rom[9'h1c2] = 708;
        rom[9'h1c3] = 712;
        rom[9'h1c4] = 717;
        rom[9'h1c5] = 722;
        rom[9'h1c6] = 726;
        rom[9'h1c7] = 731;
        rom[9'h1c8] = 736;
        rom[9'h1c9] = 741;
        rom[9'h1ca] = 745;
        rom[9'h1cb] = 750;
        rom[9'h1cc] = 755;
        rom[9'h1cd] = 760;
        rom[9'h1ce] = 765;
        rom[9'h1cf] = 770;
        rom[9'h1d0] = 774;
        rom[9'h1d1] = 779;
        rom[9'h1d2] = 784;
        rom[9'h1d3] = 789;
        rom[9'h1d4] = 794;
        rom[9'h1d5] = 799;
        rom[9'h1d6] = 804;
        rom[9'h1d7] = 809;
        rom[9'h1d8] = 814;
        rom[9'h1d9] = 819;
        rom[9'h1da] = 824;
        rom[9'h1db] = 829;
        rom[9'h1dc] = 834;
        rom[9'h1dd] = 839;
        rom[9'h1de] = 844;
        rom[9'h1df] = 849;
        rom[9'h1e0] = 854;
        rom[9'h1e1] = 859;
        rom[9'h1e2] = 864;
        rom[9'h1e3] = 869;
        rom[9'h1e4] = 874;
        rom[9'h1e5] = 880;
        rom[9'h1e6] = 885;
        rom[9'h1e7] = 890;
        rom[9'h1e8] = 895;
        rom[9'h1e9] = 900;
        rom[9'h1ea] = 906;
        rom[9'h1eb] = 911;
        rom[9'h1ec] = 916;
        rom[9'h1ed] = 921;
        rom[9'h1ee] = 927;
        rom[9'h1ef] = 932;
        rom[9'h1f0] = 937;
        rom[9'h1f1] = 942;
        rom[9'h1f2] = 948;
        rom[9'h1f3] = 953;
        rom[9'h1f4] = 959;
        rom[9'h1f5] = 964;
        rom[9'h1f6] = 969;
        rom[9'h1f7] = 975;
        rom[9'h1f8] = 980;
        rom[9'h1f9] = 986;
        rom[9'h1fa] = 991;
        rom[9'h1fb] = 996;
        rom[9'h1fc] = 1002;
        rom[9'h1fd] = 1007;
        rom[9'h1fe] = 1013;
        rom[9'h1ff] = 1018;
    end

    always_ff @(posedge clk) begin
        log_sin <= rom[{1'b0, theta}];
        exp_out <= rom[{1'b1, exp_in}][9:0];
    end
endmodule
`default_nettype wire

