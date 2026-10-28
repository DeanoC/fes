/*******************************************************************************
#   +html+<pre>
#
#   FILENAME: vibrato.sv
#   AUTHOR: Greg Taylor     CREATION DATE: 13 Oct 2014
#
#   DESCRIPTION:
#   Prepare the phase increment for the NCO (calc multiplier and vibrato)
#
#   CHANGE HISTORY:
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

module vibrato
    import opl2_pkg::*;
(
    input wire clk,
    input wire sample_clk_en,
    input wire [OP_NUM_WIDTH-1:0] op_num,
    input wire [REG_FNUM_WIDTH-1:0] fnum,
    input wire dvb,
    output logic signed [VIB_VAL_WIDTH:0] vib_val_p0
);
    /*
     * Low-Frequency Oscillator, as Nuked-OPL3: an eight-step position that
     * advances every 1024 samples, after the last operator of a sample. The
     * offset is added to the f-number before the block shift.
     */
    logic [9:0] timer = 0;
    logic [2:0] position = 0;
    logic [VIB_VAL_WIDTH-1:0] range;

    always_comb begin
        range = fnum[REG_FNUM_WIDTH-1 -: VIB_VAL_WIDTH];
        if (position[1:0] == 0)
            range = 0;
        else if (position[0])
            range = range >> 1;
        if (!dvb)
            range = range >> 1;
        vib_val_p0 = position[2] ? -signed'({1'b0, range}) : signed'({1'b0, range});
    end

    always_ff @(posedge clk)
        if (sample_clk_en && op_num == NUM_OPERATORS_PER_BANK - 1) begin
            timer <= timer + 1;
            if (timer == '1)
                position <= position + 1;
        end
endmodule
`default_nettype wire
