/*******************************************************************************
#   +html+<pre>
#
#   FILENAME: opl3_log_out_lut.sv
#   AUTHOR: Greg Taylor     CREATION DATE: 26 Oct 2014
#
#   DESCRIPTION:
#   This is an auto-generated module using a C++ program.
#   It creates a synchronous ROM to store the LUT. It stores a
#	quarter period of a log out wave according to the following formula from
#	"OPLx decapsulated" by Matthew Gambrell and Olli Niemitalo:
#	x <= 0..255, y <= round(-log(sin((x+0.5)*pi/256/2))/log(2)*256)
#
#   CHANGE HISTORY:
#   26 Oct 2014        Greg Taylor
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

module opl2_log_sine_lut (
    input wire clk,
    input wire [7:0] theta,
    output logic [11:0] out = 0
);
    // An initialised array with a registered read, so that Quartus reads it
    // from a RAM block. As a case statement the pc386 seal's netlist
    // (proc -norom) built it in logic.
    logic [11:0] rom [0:255];
    initial begin
        rom[8'h0] = 2137;
        rom[8'h1] = 1731;
        rom[8'h2] = 1543;
        rom[8'h3] = 1419;
        rom[8'h4] = 1326;
        rom[8'h5] = 1252;
        rom[8'h6] = 1190;
        rom[8'h7] = 1137;
        rom[8'h8] = 1091;
        rom[8'h9] = 1050;
        rom[8'ha] = 1013;
        rom[8'hb] = 979;
        rom[8'hc] = 949;
        rom[8'hd] = 920;
        rom[8'he] = 894;
        rom[8'hf] = 869;
        rom[8'h10] = 846;
        rom[8'h11] = 825;
        rom[8'h12] = 804;
        rom[8'h13] = 785;
        rom[8'h14] = 767;
        rom[8'h15] = 749;
        rom[8'h16] = 732;
        rom[8'h17] = 717;
        rom[8'h18] = 701;
        rom[8'h19] = 687;
        rom[8'h1a] = 672;
        rom[8'h1b] = 659;
        rom[8'h1c] = 646;
        rom[8'h1d] = 633;
        rom[8'h1e] = 621;
        rom[8'h1f] = 609;
        rom[8'h20] = 598;
        rom[8'h21] = 587;
        rom[8'h22] = 576;
        rom[8'h23] = 566;
        rom[8'h24] = 556;
        rom[8'h25] = 546;
        rom[8'h26] = 536;
        rom[8'h27] = 527;
        rom[8'h28] = 518;
        rom[8'h29] = 509;
        rom[8'h2a] = 501;
        rom[8'h2b] = 492;
        rom[8'h2c] = 484;
        rom[8'h2d] = 476;
        rom[8'h2e] = 468;
        rom[8'h2f] = 461;
        rom[8'h30] = 453;
        rom[8'h31] = 446;
        rom[8'h32] = 439;
        rom[8'h33] = 432;
        rom[8'h34] = 425;
        rom[8'h35] = 418;
        rom[8'h36] = 411;
        rom[8'h37] = 405;
        rom[8'h38] = 399;
        rom[8'h39] = 392;
        rom[8'h3a] = 386;
        rom[8'h3b] = 380;
        rom[8'h3c] = 375;
        rom[8'h3d] = 369;
        rom[8'h3e] = 363;
        rom[8'h3f] = 358;
        rom[8'h40] = 352;
        rom[8'h41] = 347;
        rom[8'h42] = 341;
        rom[8'h43] = 336;
        rom[8'h44] = 331;
        rom[8'h45] = 326;
        rom[8'h46] = 321;
        rom[8'h47] = 316;
        rom[8'h48] = 311;
        rom[8'h49] = 307;
        rom[8'h4a] = 302;
        rom[8'h4b] = 297;
        rom[8'h4c] = 293;
        rom[8'h4d] = 289;
        rom[8'h4e] = 284;
        rom[8'h4f] = 280;
        rom[8'h50] = 276;
        rom[8'h51] = 271;
        rom[8'h52] = 267;
        rom[8'h53] = 263;
        rom[8'h54] = 259;
        rom[8'h55] = 255;
        rom[8'h56] = 251;
        rom[8'h57] = 248;
        rom[8'h58] = 244;
        rom[8'h59] = 240;
        rom[8'h5a] = 236;
        rom[8'h5b] = 233;
        rom[8'h5c] = 229;
        rom[8'h5d] = 226;
        rom[8'h5e] = 222;
        rom[8'h5f] = 219;
        rom[8'h60] = 215;
        rom[8'h61] = 212;
        rom[8'h62] = 209;
        rom[8'h63] = 205;
        rom[8'h64] = 202;
        rom[8'h65] = 199;
        rom[8'h66] = 196;
        rom[8'h67] = 193;
        rom[8'h68] = 190;
        rom[8'h69] = 187;
        rom[8'h6a] = 184;
        rom[8'h6b] = 181;
        rom[8'h6c] = 178;
        rom[8'h6d] = 175;
        rom[8'h6e] = 172;
        rom[8'h6f] = 169;
        rom[8'h70] = 167;
        rom[8'h71] = 164;
        rom[8'h72] = 161;
        rom[8'h73] = 159;
        rom[8'h74] = 156;
        rom[8'h75] = 153;
        rom[8'h76] = 151;
        rom[8'h77] = 148;
        rom[8'h78] = 146;
        rom[8'h79] = 143;
        rom[8'h7a] = 141;
        rom[8'h7b] = 138;
        rom[8'h7c] = 136;
        rom[8'h7d] = 134;
        rom[8'h7e] = 131;
        rom[8'h7f] = 129;
        rom[8'h80] = 127;
        rom[8'h81] = 125;
        rom[8'h82] = 122;
        rom[8'h83] = 120;
        rom[8'h84] = 118;
        rom[8'h85] = 116;
        rom[8'h86] = 114;
        rom[8'h87] = 112;
        rom[8'h88] = 110;
        rom[8'h89] = 108;
        rom[8'h8a] = 106;
        rom[8'h8b] = 104;
        rom[8'h8c] = 102;
        rom[8'h8d] = 100;
        rom[8'h8e] = 98;
        rom[8'h8f] = 96;
        rom[8'h90] = 94;
        rom[8'h91] = 92;
        rom[8'h92] = 91;
        rom[8'h93] = 89;
        rom[8'h94] = 87;
        rom[8'h95] = 85;
        rom[8'h96] = 83;
        rom[8'h97] = 82;
        rom[8'h98] = 80;
        rom[8'h99] = 78;
        rom[8'h9a] = 77;
        rom[8'h9b] = 75;
        rom[8'h9c] = 74;
        rom[8'h9d] = 72;
        rom[8'h9e] = 70;
        rom[8'h9f] = 69;
        rom[8'ha0] = 67;
        rom[8'ha1] = 66;
        rom[8'ha2] = 64;
        rom[8'ha3] = 63;
        rom[8'ha4] = 62;
        rom[8'ha5] = 60;
        rom[8'ha6] = 59;
        rom[8'ha7] = 57;
        rom[8'ha8] = 56;
        rom[8'ha9] = 55;
        rom[8'haa] = 53;
        rom[8'hab] = 52;
        rom[8'hac] = 51;
        rom[8'had] = 49;
        rom[8'hae] = 48;
        rom[8'haf] = 47;
        rom[8'hb0] = 46;
        rom[8'hb1] = 45;
        rom[8'hb2] = 43;
        rom[8'hb3] = 42;
        rom[8'hb4] = 41;
        rom[8'hb5] = 40;
        rom[8'hb6] = 39;
        rom[8'hb7] = 38;
        rom[8'hb8] = 37;
        rom[8'hb9] = 36;
        rom[8'hba] = 35;
        rom[8'hbb] = 34;
        rom[8'hbc] = 33;
        rom[8'hbd] = 32;
        rom[8'hbe] = 31;
        rom[8'hbf] = 30;
        rom[8'hc0] = 29;
        rom[8'hc1] = 28;
        rom[8'hc2] = 27;
        rom[8'hc3] = 26;
        rom[8'hc4] = 25;
        rom[8'hc5] = 24;
        rom[8'hc6] = 23;
        rom[8'hc7] = 23;
        rom[8'hc8] = 22;
        rom[8'hc9] = 21;
        rom[8'hca] = 20;
        rom[8'hcb] = 20;
        rom[8'hcc] = 19;
        rom[8'hcd] = 18;
        rom[8'hce] = 17;
        rom[8'hcf] = 17;
        rom[8'hd0] = 16;
        rom[8'hd1] = 15;
        rom[8'hd2] = 15;
        rom[8'hd3] = 14;
        rom[8'hd4] = 13;
        rom[8'hd5] = 13;
        rom[8'hd6] = 12;
        rom[8'hd7] = 12;
        rom[8'hd8] = 11;
        rom[8'hd9] = 10;
        rom[8'hda] = 10;
        rom[8'hdb] = 9;
        rom[8'hdc] = 9;
        rom[8'hdd] = 8;
        rom[8'hde] = 8;
        rom[8'hdf] = 7;
        rom[8'he0] = 7;
        rom[8'he1] = 7;
        rom[8'he2] = 6;
        rom[8'he3] = 6;
        rom[8'he4] = 5;
        rom[8'he5] = 5;
        rom[8'he6] = 5;
        rom[8'he7] = 4;
        rom[8'he8] = 4;
        rom[8'he9] = 4;
        rom[8'hea] = 3;
        rom[8'heb] = 3;
        rom[8'hec] = 3;
        rom[8'hed] = 2;
        rom[8'hee] = 2;
        rom[8'hef] = 2;
        rom[8'hf0] = 2;
        rom[8'hf1] = 1;
        rom[8'hf2] = 1;
        rom[8'hf3] = 1;
        rom[8'hf4] = 1;
        rom[8'hf5] = 1;
        rom[8'hf6] = 1;
        rom[8'hf7] = 1;
        rom[8'hf8] = 0;
        rom[8'hf9] = 0;
        rom[8'hfa] = 0;
        rom[8'hfb] = 0;
        rom[8'hfc] = 0;
        rom[8'hfd] = 0;
        rom[8'hfe] = 0;
        rom[8'hff] = 0;
    end

    always_ff @(posedge clk)
        out <= rom[theta];
endmodule
`default_nettype wire
