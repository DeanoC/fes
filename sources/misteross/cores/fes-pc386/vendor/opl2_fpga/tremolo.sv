/*******************************************************************************
#   +html+<pre>
#
#   FILENAME: tremolo.sv
#   AUTHOR: Greg Taylor     CREATION DATE: 2 Nov 2014
#
#   DESCRIPTION:
#
#   CHANGE HISTORY:
#   2 Nov 2014    Greg Taylor
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
#   Copyright (C) 2012  Steffen Ohrendorf <steffen.ohrendorf@gmx.de>
#
#   Some code based on forum posts in:
#   http://forums.submarine.org.uk/phpBB/viewforum.php?f=9,
#   Copyright (C) 2010-2013 by carbon14 and opl3
#
#******************************************************************************/
`timescale 1ns / 1ps
`default_nettype none
/* altera message_off 10230 */

module tremolo
    import opl2_pkg::*;
(
    input wire clk,
    input wire sample_clk_en,
    input wire [OP_NUM_WIDTH-1:0] op_num,
    input wire dam, // depth of tremolo
    output logic [AM_VAL_WIDTH-1:0] am_val_p2 = 0
);
    /*
     * Low-Frequency Oscillator, as Nuked-OPL3: a triangle position 0-209 that
     * steps every 64 samples. The value for the next sample is computed once
     * the last operator of this one has taken its own, with this sample's depth.
     */
    logic [5:0] timer = 0;
    logic [7:0] position = 0;
    logic [7:0] next_position;
    logic [7:0] level;
    logic [AM_VAL_WIDTH-1:0] value = 0;
    logic last_op_p1 = 0;

    always_comb begin
        next_position = position;
        if (timer == '1)
            next_position = position == 209 ? 0 : position + 1;
        level = next_position < 105 ? next_position : 8'd210 - next_position;
    end

    always_ff @(posedge clk) begin
        last_op_p1 <= sample_clk_en && op_num == NUM_OPERATORS_PER_BANK - 1;
        am_val_p2 <= value;
        if (last_op_p1) begin
            timer <= timer + 1;
            position <= next_position;
            value <= AM_VAL_WIDTH'(dam ? level >> 2 : level >> 4);
        end
    end
endmodule
`default_nettype wire
