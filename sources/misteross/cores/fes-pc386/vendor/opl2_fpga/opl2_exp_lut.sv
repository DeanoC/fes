/*******************************************************************************
#   +html+<pre>
#
#   FILENAME: opl3_exp_lut.sv
#   AUTHOR: Greg Taylor     CREATION DATE: 26 Oct 2014
#
#   DESCRIPTION:
#   This is an auto-generated module using a C++ program.
#   It creates a synchronous ROM to store the LUT. It stores a
#	quarter period of a log sine wave according to the following formula from
#	"OPLx decapsulated" by Matthew Gambrell and Olli Niemitalo:
#	x <= 0..255, y <= round((power(2, x/256)-1)*1024)
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

module opl2_exp_lut (
    input wire clk,
    input wire [7:0] in,
    output logic [9:0] out = 0
);
    // An initialised array with a registered read, so that Quartus reads it
    // from a RAM block. As a case statement the pc386 seal's netlist
    // (proc -norom) built it in logic.
    logic [9:0] rom [0:255];
    initial begin
        rom[8'h0] = 0;
        rom[8'h1] = 3;
        rom[8'h2] = 6;
        rom[8'h3] = 8;
        rom[8'h4] = 11;
        rom[8'h5] = 14;
        rom[8'h6] = 17;
        rom[8'h7] = 20;
        rom[8'h8] = 22;
        rom[8'h9] = 25;
        rom[8'ha] = 28;
        rom[8'hb] = 31;
        rom[8'hc] = 34;
        rom[8'hd] = 37;
        rom[8'he] = 40;
        rom[8'hf] = 42;
        rom[8'h10] = 45;
        rom[8'h11] = 48;
        rom[8'h12] = 51;
        rom[8'h13] = 54;
        rom[8'h14] = 57;
        rom[8'h15] = 60;
        rom[8'h16] = 63;
        rom[8'h17] = 66;
        rom[8'h18] = 69;
        rom[8'h19] = 72;
        rom[8'h1a] = 75;
        rom[8'h1b] = 78;
        rom[8'h1c] = 81;
        rom[8'h1d] = 84;
        rom[8'h1e] = 87;
        rom[8'h1f] = 90;
        rom[8'h20] = 93;
        rom[8'h21] = 96;
        rom[8'h22] = 99;
        rom[8'h23] = 102;
        rom[8'h24] = 105;
        rom[8'h25] = 108;
        rom[8'h26] = 111;
        rom[8'h27] = 114;
        rom[8'h28] = 117;
        rom[8'h29] = 120;
        rom[8'h2a] = 123;
        rom[8'h2b] = 126;
        rom[8'h2c] = 130;
        rom[8'h2d] = 133;
        rom[8'h2e] = 136;
        rom[8'h2f] = 139;
        rom[8'h30] = 142;
        rom[8'h31] = 145;
        rom[8'h32] = 148;
        rom[8'h33] = 152;
        rom[8'h34] = 155;
        rom[8'h35] = 158;
        rom[8'h36] = 161;
        rom[8'h37] = 164;
        rom[8'h38] = 168;
        rom[8'h39] = 171;
        rom[8'h3a] = 174;
        rom[8'h3b] = 177;
        rom[8'h3c] = 181;
        rom[8'h3d] = 184;
        rom[8'h3e] = 187;
        rom[8'h3f] = 190;
        rom[8'h40] = 194;
        rom[8'h41] = 197;
        rom[8'h42] = 200;
        rom[8'h43] = 204;
        rom[8'h44] = 207;
        rom[8'h45] = 210;
        rom[8'h46] = 214;
        rom[8'h47] = 217;
        rom[8'h48] = 220;
        rom[8'h49] = 224;
        rom[8'h4a] = 227;
        rom[8'h4b] = 231;
        rom[8'h4c] = 234;
        rom[8'h4d] = 237;
        rom[8'h4e] = 241;
        rom[8'h4f] = 244;
        rom[8'h50] = 248;
        rom[8'h51] = 251;
        rom[8'h52] = 255;
        rom[8'h53] = 258;
        rom[8'h54] = 262;
        rom[8'h55] = 265;
        rom[8'h56] = 268;
        rom[8'h57] = 272;
        rom[8'h58] = 276;
        rom[8'h59] = 279;
        rom[8'h5a] = 283;
        rom[8'h5b] = 286;
        rom[8'h5c] = 290;
        rom[8'h5d] = 293;
        rom[8'h5e] = 297;
        rom[8'h5f] = 300;
        rom[8'h60] = 304;
        rom[8'h61] = 308;
        rom[8'h62] = 311;
        rom[8'h63] = 315;
        rom[8'h64] = 318;
        rom[8'h65] = 322;
        rom[8'h66] = 326;
        rom[8'h67] = 329;
        rom[8'h68] = 333;
        rom[8'h69] = 337;
        rom[8'h6a] = 340;
        rom[8'h6b] = 344;
        rom[8'h6c] = 348;
        rom[8'h6d] = 352;
        rom[8'h6e] = 355;
        rom[8'h6f] = 359;
        rom[8'h70] = 363;
        rom[8'h71] = 367;
        rom[8'h72] = 370;
        rom[8'h73] = 374;
        rom[8'h74] = 378;
        rom[8'h75] = 382;
        rom[8'h76] = 385;
        rom[8'h77] = 389;
        rom[8'h78] = 393;
        rom[8'h79] = 397;
        rom[8'h7a] = 401;
        rom[8'h7b] = 405;
        rom[8'h7c] = 409;
        rom[8'h7d] = 412;
        rom[8'h7e] = 416;
        rom[8'h7f] = 420;
        rom[8'h80] = 424;
        rom[8'h81] = 428;
        rom[8'h82] = 432;
        rom[8'h83] = 436;
        rom[8'h84] = 440;
        rom[8'h85] = 444;
        rom[8'h86] = 448;
        rom[8'h87] = 452;
        rom[8'h88] = 456;
        rom[8'h89] = 460;
        rom[8'h8a] = 464;
        rom[8'h8b] = 468;
        rom[8'h8c] = 472;
        rom[8'h8d] = 476;
        rom[8'h8e] = 480;
        rom[8'h8f] = 484;
        rom[8'h90] = 488;
        rom[8'h91] = 492;
        rom[8'h92] = 496;
        rom[8'h93] = 501;
        rom[8'h94] = 505;
        rom[8'h95] = 509;
        rom[8'h96] = 513;
        rom[8'h97] = 517;
        rom[8'h98] = 521;
        rom[8'h99] = 526;
        rom[8'h9a] = 530;
        rom[8'h9b] = 534;
        rom[8'h9c] = 538;
        rom[8'h9d] = 542;
        rom[8'h9e] = 547;
        rom[8'h9f] = 551;
        rom[8'ha0] = 555;
        rom[8'ha1] = 560;
        rom[8'ha2] = 564;
        rom[8'ha3] = 568;
        rom[8'ha4] = 572;
        rom[8'ha5] = 577;
        rom[8'ha6] = 581;
        rom[8'ha7] = 585;
        rom[8'ha8] = 590;
        rom[8'ha9] = 594;
        rom[8'haa] = 599;
        rom[8'hab] = 603;
        rom[8'hac] = 607;
        rom[8'had] = 612;
        rom[8'hae] = 616;
        rom[8'haf] = 621;
        rom[8'hb0] = 625;
        rom[8'hb1] = 630;
        rom[8'hb2] = 634;
        rom[8'hb3] = 639;
        rom[8'hb4] = 643;
        rom[8'hb5] = 648;
        rom[8'hb6] = 652;
        rom[8'hb7] = 657;
        rom[8'hb8] = 661;
        rom[8'hb9] = 666;
        rom[8'hba] = 670;
        rom[8'hbb] = 675;
        rom[8'hbc] = 680;
        rom[8'hbd] = 684;
        rom[8'hbe] = 689;
        rom[8'hbf] = 693;
        rom[8'hc0] = 698;
        rom[8'hc1] = 703;
        rom[8'hc2] = 708;
        rom[8'hc3] = 712;
        rom[8'hc4] = 717;
        rom[8'hc5] = 722;
        rom[8'hc6] = 726;
        rom[8'hc7] = 731;
        rom[8'hc8] = 736;
        rom[8'hc9] = 741;
        rom[8'hca] = 745;
        rom[8'hcb] = 750;
        rom[8'hcc] = 755;
        rom[8'hcd] = 760;
        rom[8'hce] = 765;
        rom[8'hcf] = 770;
        rom[8'hd0] = 774;
        rom[8'hd1] = 779;
        rom[8'hd2] = 784;
        rom[8'hd3] = 789;
        rom[8'hd4] = 794;
        rom[8'hd5] = 799;
        rom[8'hd6] = 804;
        rom[8'hd7] = 809;
        rom[8'hd8] = 814;
        rom[8'hd9] = 819;
        rom[8'hda] = 824;
        rom[8'hdb] = 829;
        rom[8'hdc] = 834;
        rom[8'hdd] = 839;
        rom[8'hde] = 844;
        rom[8'hdf] = 849;
        rom[8'he0] = 854;
        rom[8'he1] = 859;
        rom[8'he2] = 864;
        rom[8'he3] = 869;
        rom[8'he4] = 874;
        rom[8'he5] = 880;
        rom[8'he6] = 885;
        rom[8'he7] = 890;
        rom[8'he8] = 895;
        rom[8'he9] = 900;
        rom[8'hea] = 906;
        rom[8'heb] = 911;
        rom[8'hec] = 916;
        rom[8'hed] = 921;
        rom[8'hee] = 927;
        rom[8'hef] = 932;
        rom[8'hf0] = 937;
        rom[8'hf1] = 942;
        rom[8'hf2] = 948;
        rom[8'hf3] = 953;
        rom[8'hf4] = 959;
        rom[8'hf5] = 964;
        rom[8'hf6] = 969;
        rom[8'hf7] = 975;
        rom[8'hf8] = 980;
        rom[8'hf9] = 986;
        rom[8'hfa] = 991;
        rom[8'hfb] = 996;
        rom[8'hfc] = 1002;
        rom[8'hfd] = 1007;
        rom[8'hfe] = 1013;
        rom[8'hff] = 1018;
    end

    always_ff @(posedge clk)
        out <= rom[in];
endmodule
`default_nettype wire
