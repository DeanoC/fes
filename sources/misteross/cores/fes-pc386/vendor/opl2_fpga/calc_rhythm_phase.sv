/*******************************************************************************
#   +html+<pre>
#
#   FILENAME: calc_rhythm_phase.sv
#   AUTHOR: Greg Taylor     CREATION DATE: 25 July 2015
#
#   DESCRIPTION:
#   Does additional transformations to the phase for certain rhythm instruments.
#   Simply passes through the unmodified phase otherwise.
#
#   CHANGE HISTORY:
#   3 June 2024     Greg Taylor
#       Refactored to match implementation in https://github.com/nukeykt/Nuked-OPL3
#
#   25 July 2015    Greg Taylor
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
/* altera message_off 10958 */

module calc_rhythm_phase
    import opl2_pkg::*;
(
    input wire clk,
    input wire sample_clk_en,
    input wire [OP_NUM_WIDTH-1:0] op_num,
    input wire [PHASE_FINAL_WIDTH-1:0] phase_p2,
    input var operator_t op_type_p0,
    output logic [PHASE_FINAL_WIDTH-1:0] rhythm_phase_p3 = 0
);
    localparam OPERATOR_T_BITS = 3;           // $bits(operator_t)
    localparam PIPELINE_DELAY = 3;
    localparam NOISE_WIDTH = 23;
    logic [PHASE_FINAL_WIDTH-1:0] hh_phase_friend = 0;
    logic [PHASE_FINAL_WIDTH-1:0] tc_phase_friend = 0;
    logic [PHASE_FINAL_WIDTH-1:0] hh_phase_p2;
    logic [PHASE_FINAL_WIDTH-1:0] tc_phase_p2;
    logic rm_xor_p2;
    logic [NOISE_WIDTH-1:0] noise = 1;
    logic [NOISE_WIDTH-1:0] noise_next;
    logic [2:1] noise_p;
    logic [PIPELINE_DELAY:1] sample_clk_en_p;
    logic [PIPELINE_DELAY:1] [OPERATOR_T_BITS-1:0] op_type_p;
    logic [PIPELINE_DELAY:1] [OP_NUM_WIDTH-1:0] op_num_p;

    pipeline_sr #(
        .ENDING_CYCLE(PIPELINE_DELAY)
    ) sample_clk_en_sr (
        .clk,
        .in(sample_clk_en),
        .out(sample_clk_en_p)
    );

    pipeline_sr #(
        .DATA_WIDTH(OPERATOR_T_BITS),
        .ENDING_CYCLE(PIPELINE_DELAY),
        .POR_VALUE(OP_NORMAL)
    ) op_type_sr (
        .clk,
        .in(op_type_p0),
        .out(op_type_p)
    );

    pipeline_sr #(
        .DATA_WIDTH(OP_NUM_WIDTH),
        .ENDING_CYCLE(PIPELINE_DELAY)
    ) op_num_sr (
        .clk,
        .in(op_num),
        .out(op_num_p)
    );

    always_ff @(posedge clk) begin
        // Store the hi hat phase when it comes by
        if (sample_clk_en_p[2] && op_num_p[2] == 13)
            hh_phase_friend <= phase_p2;

        // Store the top cymbal phase when it comes by
        if (op_type_p[2] == OP_TOP_CYMBAL)
            tc_phase_friend <= phase_p2;
    end

    always_comb begin
        hh_phase_p2 = op_type_p[2] == OP_HI_HAT ? phase_p2 : hh_phase_friend;
        tc_phase_p2 = op_type_p[2] == OP_TOP_CYMBAL ? phase_p2 : tc_phase_friend;
        rm_xor_p2 = (hh_phase_p2[2] ^ hh_phase_p2[7]) ||
                    (hh_phase_p2[3] ^ tc_phase_p2[5]) ||
                    (tc_phase_p2[3] ^ tc_phase_p2[5]);
    end

    always_ff @(posedge clk) begin
        unique case (op_type_p[2])
        OP_HI_HAT:     rhythm_phase_p3 <= (rm_xor_p2 << 9) | ((rm_xor_p2 ^ noise_p[2]) ? 'hd0 : 'h34);
        OP_SNARE_DRUM: rhythm_phase_p3 <= (hh_phase_p2[8] << 9) | ((hh_phase_p2[8] ^ noise_p[2]) << 8);
        OP_TOP_CYMBAL: rhythm_phase_p3 <= (rm_xor_p2 << 9) | 'h80;
        default:       rhythm_phase_p3 <= phase_p2; // all others pass through
        endcase
    end

    /*
     * Noise LFSR as Nuked-OPL3: it steps once per operator slot, 36 per sample
     * on an OPL3, so 18 more steps follow the last OPL2 operator. Each
     * operator sees the state before its own step.
     */
    function automatic logic [NOISE_WIDTH-1:0] noise_step(input logic [NOISE_WIDTH-1:0] n);
        return {n[14] ^ n[0], n[NOISE_WIDTH-1:1]};
    endfunction

    always_comb begin
        noise_next = noise_step(noise);
        if (op_num == NUM_OPERATORS_PER_BANK - 1)
            for (int i = 0; i < 18; i++)
                noise_next = noise_step(noise_next);
    end

    always_ff @(posedge clk) begin
        if (sample_clk_en)
            noise <= noise_next;
        noise_p[1] <= noise[0];
        noise_p[2] <= noise_p[1];
    end
endmodule
`default_nettype wire
