// SPDX-License-Identifier: GPL-2.0-or-later
// Fixed 720p playfield text for the SDRAM scan and the three HPS DDR ports.
// Pass is green, fail and NACK are red, a scan still running is amber, and a
// button stop is white.
//
// Each DDR status bus is, from the top bit down: phase[2:0], reading,
// address[31:0], errors[31:0], first fault[31:0], last fault[31:0],
// fault phase[2:0], failing bits[127:0], done, nack, stopped,
// write MB/s digits[15:0], read MB/s digits[15:0].
module ram_display (
    input wire [106:0] byte_status,
    input wire pixel_clk,
    input wire [9:0] x,
    input wire [9:0] y,
    input wire active,
    input wire [2:0] sdram_phase,
    input wire sdram_reading,
    input wire [31:0] sdram_addr,
    input wire [31:0] sdram_fault,
    input wire [31:0] sdram_last,
    input wire [15:0] sdram_was,
    input wire [31:0] sdram_errors,
    input wire [15:0] sdram_expect,
    input wire [15:0] sdram_got,
    input wire sdram_pass,
    input wire sdram_fail,
    input wire [7:0] sdram_mhz,
    input wire sdram_stopped,
    // Six pattern counts for each finished rate, in pattern order.
    input wire [191:0] pat50,
    input wire [191:0] pat75,
    input wire [191:0] pat100,
    input wire [2:0] pat_ok,
    input wire [297:0] ddr0,
    input wire [297:0] ddr1,
    input wire [297:0] ddr2,
    output reg [7:0] red,
    output reg [7:0] green,
    output reg [7:0] blue
);
    reg [106:0] byte_sync0 = 107'd0, byte_sync1 = 107'd0;
    reg [769:0] sync0 = 770'd0;
    reg [769:0] sync1 = 770'd0;
    reg [297:0] ddr0_sync0 = 298'd0, ddr0_sync1 = 298'd0;
    reg [297:0] ddr1_sync0 = 298'd0, ddr1_sync1 = 298'd0;
    reg [297:0] ddr2_sync0 = 298'd0, ddr2_sync1 = 298'd0;
    wire [769:0] snap = {
        pat_ok, pat100, pat75, pat50,
        sdram_mhz,
        sdram_phase, sdram_reading, sdram_addr, sdram_fault, sdram_last, sdram_errors, sdram_was,
        sdram_expect, sdram_got, sdram_pass, sdram_fail, sdram_stopped
    };
    wire [2:0] s_pat_ok = sync1[769:767];
    wire [191:0] s_pat100 = sync1[766:575];
    wire [191:0] s_pat75 = sync1[574:383];
    wire [191:0] s_pat50 = sync1[382:191];
    wire [7:0] s_mhz = sync1[190:183];
    wire [2:0] s_phase = sync1[182:180];
    wire s_reading = sync1[179];
    wire [31:0] s_addr = sync1[178:147];
    wire [31:0] s_fault = sync1[146:115];
    wire [31:0] s_last = sync1[114:83];
    wire [31:0] s_errors = sync1[82:51];
    wire [15:0] s_was = sync1[50:35];
    wire [15:0] s_expect = sync1[34:19];
    wire [15:0] s_got = sync1[18:3];
    wire s_pass = sync1[2];
    wire s_fail = sync1[1];
    wire s_stopped = sync1[0];

    wire [7:0] glyph_pixels;

    function [7:0] hex_digit;
        input [3:0] nibble;
        begin
            hex_digit = (nibble < 4'd10) ? (8'h30 + {4'd0, nibble}) : (8'h37 + {4'd0, nibble});
        end
    endfunction

    function [31:0] phase_name;
        input [2:0] which;
        begin
            case (which)
                3'd0: phase_name = "0000";
                3'd1: phase_name = "FFFF";
                3'd2: phase_name = "5555";
                3'd3: phase_name = "AAAA";
                3'd4: phase_name = "ADDR";
                default: phase_name = "INVR";
            endcase
        end
    endfunction

    function [31:0] ddr_phase_name;
        input [2:0] which;
        begin
            case (which)
                3'd0: ddr_phase_name = "ZERO";
                3'd1: ddr_phase_name = "ONES";
                3'd2: ddr_phase_name = "CHCK";
                3'd3: ddr_phase_name = "WALK";
                3'd4: ddr_phase_name = "INVR";
                3'd5: ddr_phase_name = "BYTE";
                default: ddr_phase_name = "ADDR";
            endcase
        end
    endfunction

    function [31:0] status_name;
        input stopped;
        input passed;
        input failed;
        begin
            if (stopped)
                status_name = "STOP";
            else if (failed)
                status_name = "FAIL";
            else if (passed)
                status_name = "PASS";
            else
                status_name = "RUN ";
        end
    endfunction

    function [7:0] byte4;
        input [31:0] word;
        input [5:0] column;
        begin
            case (column)
                6'd0: byte4 = word[31:24];
                6'd1: byte4 = word[23:16];
                6'd2: byte4 = word[15:8];
                6'd3: byte4 = word[7:0];
                default: byte4 = 8'h00;
            endcase
        end
    endfunction

    function [7:0] byte5;
        input [39:0] word;
        input [5:0] column;
        begin
            case (column)
                6'd0: byte5 = word[39:32];
                6'd1: byte5 = word[31:24];
                6'd2: byte5 = word[23:16];
                6'd3: byte5 = word[15:8];
                6'd4: byte5 = word[7:0];
                default: byte5 = 8'h00;
            endcase
        end
    endfunction

    function [7:0] byte6;
        input [47:0] word;
        input [5:0] column;
        begin
            case (column)
                6'd0: byte6 = word[47:40];
                6'd1: byte6 = word[39:32];
                6'd2: byte6 = word[31:24];
                6'd3: byte6 = word[23:16];
                6'd4: byte6 = word[15:8];
                6'd5: byte6 = word[7:0];
                default: byte6 = 8'h00;
            endcase
        end
    endfunction

    function [3:0] hex_nibble;
        input [31:0] value;
        input [5:0] column;
        input [5:0] first;
        begin
            case (column)
                first + 6'd0: hex_nibble = value[31:28];
                first + 6'd1: hex_nibble = value[27:24];
                first + 6'd2: hex_nibble = value[23:20];
                first + 6'd3: hex_nibble = value[19:16];
                first + 6'd4: hex_nibble = value[15:12];
                first + 6'd5: hex_nibble = value[11:8];
                first + 6'd6: hex_nibble = value[7:4];
                first + 6'd7: hex_nibble = value[3:0];
                default: hex_nibble = 4'h0;
            endcase
        end
    endfunction

    function [3:0] hex16;
        input [15:0] value;
        input [5:0] column;
        input [5:0] first;
        begin
            case (column)
                first + 6'd0: hex16 = value[15:12];
                first + 6'd1: hex16 = value[11:8];
                first + 6'd2: hex16 = value[7:4];
                first + 6'd3: hex16 = value[3:0];
                default: hex16 = 4'h0;
            endcase
        end
    endfunction

    // Failing-bit nibble shown at a column from 7, most significant first.
    function [3:0] bad_nibble;
        input [127:0] bits;
        input [5:0] column;
        input wide;
        integer i;
        begin
            bad_nibble = 4'h0;
            for (i = 0; i < 32; i = i + 1)
                if (column == 6'd7 + i[5:0])
                    bad_nibble = wide ? bits[127 - 4 * i -: 4] :
                        (i < 16 ? bits[63 - 4 * i -: 4] : 4'h0);
        end
    endfunction

    // Character first + i of a four-character word, by constant compares.
    function [7:0] byte4_at;
        input [31:0] word;
        input [5:0] column;
        input [5:0] first;
        begin
            case (column)
                first + 6'd0: byte4_at = word[31:24];
                first + 6'd1: byte4_at = word[23:16];
                first + 6'd2: byte4_at = word[15:8];
                first + 6'd3: byte4_at = word[7:0];
                default: byte4_at = 8'h00;
            endcase
        end
    endfunction

    function [7:0] mhz_at;
        input [7:0] value;
        input [5:0] column;
        input [5:0] first;
        reg [23:0] text;
        begin
            case (value)
                8'd50: text = " 50";
                8'd130: text = "130";
                8'd100: text = "100";
                default: text = "   ";
            endcase
            case (column)
                first + 6'd0: mhz_at = text[23:16];
                first + 6'd1: mhz_at = text[15:8];
                default: mhz_at = text[7:0];
            endcase
        end
    endfunction

    function [7:0] line_char;
        input [4:0] line;
        input [5:0] column;
        input [39:0] label;
        input [2:0] which;
        input reading;
        input [31:0] cursor;
        input [31:0] err_count;
        input [31:0] fault_at;
        input [31:0] last_at;
        input [15:0] was;
        input [15:0] expected_word;
        input [15:0] got;
        input [7:0] mhz;
        input stopped;
        input passed;
        input failed;
        begin
            case (line)
                5'd0: line_char = byte5(label, column);
                5'd1: begin
                    if (column < 6'd4)
                        line_char = byte4(phase_name(which), column);
                    else if (column == 6'd5)
                        line_char = reading ? "R" : "W";
                    else if (column == 6'd7)
                        line_char = hex_digit({1'b0, which} + 4'd1);
                    else if (column == 6'd8)
                        line_char = "/";
                    else if (column == 6'd9)
                        line_char = "6";
                    else if (mhz != 8'd0 && column >= 6'd12 && column < 6'd15)
                        line_char = mhz_at(mhz, column, 6'd12);
                    else if (mhz != 8'd0 && column == 6'd16)
                        line_char = "M";
                    else if (mhz != 8'd0 && column == 6'd17)
                        line_char = "H";
                    else if (mhz != 8'd0 && column == 6'd18)
                        line_char = "Z";
                    else
                        line_char = 8'h00;
                end
                5'd2: begin
                    if (column < 6'd4)
                        line_char = byte4("ADDR", column);
                    else if (column >= 6'd5 && column < 6'd13)
                        line_char = hex_digit(hex_nibble(cursor, column, 6'd5));
                    else if (column >= 6'd14 && column < 6'd16)
                        line_char = byte4_at("AT  ", column, 6'd14);
                    else if (column >= 6'd17 && column < 6'd25)
                        line_char = hex_digit(hex_nibble(fault_at, column, 6'd17));
                    else if (column >= 6'd26 && column < 6'd34)
                        line_char = hex_digit(hex_nibble(last_at, column, 6'd26));
                    else
                        line_char = 8'h00;
                end
                5'd3: begin
                    if (column < 6'd4)
                        line_char = byte4("ERR ", column);
                    else if (column >= 6'd4 && column < 6'd12)
                        line_char = hex_digit(hex_nibble(err_count, column, 6'd4));
                    else if (column >= 6'd13 && column < 6'd16)
                        line_char = byte4_at("WAS ", column, 6'd13);
                    else if (column >= 6'd17 && column < 6'd21)
                        line_char = hex_digit(hex16(was, column, 6'd17));
                    else
                        line_char = 8'h00;
                end
                5'd4: begin
                    if (column < 6'd3)
                        line_char = byte4("EXP ", column);
                    else if (column >= 6'd4 && column < 6'd8)
                        line_char = hex_digit(hex16(expected_word, column, 6'd4));
                    else if (column >= 6'd9 && column < 6'd12)
                        line_char = byte4_at("GOT ", column, 6'd9);
                    else if (column >= 6'd13 && column < 6'd17)
                        line_char = hex_digit(hex16(got, column, 6'd13));
                    else
                        line_char = 8'h00;
                end
                5'd5: line_char = byte4(status_name(stopped, passed, failed), column);
                default: line_char = 8'h00;
            endcase
        end
    endfunction

    function [31:0] pattern_name;
        input [2:0] which;
        begin
            case (which)
                3'd0: pattern_name = "0000";
                3'd1: pattern_name = "FFFF";
                3'd2: pattern_name = "5555";
                3'd3: pattern_name = "AAAA";
                3'd4: pattern_name = "ADDR";
                default: pattern_name = "INVR";
            endcase
        end
    endfunction

    function [31:0] pattern_count;
        input [191:0] counts;
        input [2:0] which;
        begin
            case (which)
                3'd0: pattern_count = counts[31:0];
                3'd1: pattern_count = counts[63:32];
                3'd2: pattern_count = counts[95:64];
                3'd3: pattern_count = counts[127:96];
                3'd4: pattern_count = counts[159:128];
                default: pattern_count = counts[191:160];
            endcase
        end
    endfunction

    function [7:0] pattern_digits;
        input [31:0] count;
        input have;
        input [5:0] column;
        input [5:0] first;
        begin
            if (have && column >= first && column < first + 6'd8)
                pattern_digits = hex_digit(hex_nibble(count, column, first));
            else
                pattern_digits = 8'h00;
        end
    endfunction

    // One row per pattern. Columns are 50 MHz, then 130, then 100. The row's
    // three counts are selected a stage earlier into table_counts_q.
    reg [95:0] table_counts_q = 96'd0;
    function [7:0] pattern_row;
        input [2:0] which;
        input [5:0] column;
        begin
            if (column < 6'd4)
                pattern_row = byte4(pattern_name(which), column);
            else if (column < 6'd14)
                pattern_row = pattern_digits(table_counts_q[31:0], s_pat_ok[0], column, 6'd5);
            else if (column < 6'd23)
                pattern_row = pattern_digits(table_counts_q[63:32], s_pat_ok[1], column, 6'd14);
            else
                pattern_row = pattern_digits(table_counts_q[95:64], s_pat_ok[2], column, 6'd23);
        end
    endfunction

    function [7:0] rate_head;
        input [5:0] column;
        begin
            if (column == 6'd5)
                rate_head = "5";
            else if (column == 6'd6)
                rate_head = "0";
            else if (column == 6'd14)
                rate_head = "1";
            else if (column == 6'd15)
                rate_head = "3";
            else if (column == 6'd16)
                rate_head = "0";
            else if (column == 6'd23)
                rate_head = "1";
            else if (column == 6'd24)
                rate_head = "0";
            else if (column == 6'd25)
                rate_head = "0";
            else
                rate_head = 8'h00;
        end
    endfunction

    // A 40-column template with spaces where fields go.
    function [7:0] text_at;
        input [319:0] text;
        input [5:0] column;
        begin
            text_at = text[(6'd39 - column) * 8 +: 8];
        end
    endfunction

    function [7:0] decimal4;
        input [15:0] digits;
        input [5:0] column;
        input [5:0] first;
        begin
            decimal4 = 8'h30 + {4'd0, hex16(digits, column, first)};
        end
    endfunction

    // DDR block rows: summary per port, first and last fault per port,
    // failing bits per port, then the MB/s of each port's last passes.
    localparam [4:0] DDR_HEAD = 5'd7;
    localparam [4:0] DDR_SUMMARY = 5'd8;
    localparam [4:0] DDR_FAULTS = 5'd11;
    localparam [4:0] DDR_BITS = 5'd14;
    localparam [4:0] DDR_RATES = 5'd17;
    localparam [4:0] BUTTON_ROW = 5'd27;
    localparam [4:0] TABLE_HEAD = 5'd20;
    localparam [4:0] TABLE_FIRST = 5'd21;

    function [1:0] ddr_port_of;
        input [4:0] line;
        begin
            if (line == DDR_SUMMARY || line == DDR_FAULTS || line == DDR_BITS)
                ddr_port_of = 2'd0;
            else if (line == DDR_SUMMARY + 5'd1 || line == DDR_FAULTS + 5'd1 || line == DDR_BITS + 5'd1)
                ddr_port_of = 2'd1;
            else
                ddr_port_of = 2'd2;
        end
    endfunction

    // Stage 1 of the character pipeline selects the DDR port of this row.
    reg [297:0] port_q = 298'd0;
    reg [1:0] port_index_q = 2'd0;
    wire [2:0] q_phase = port_q[297:295];
    wire q_reading = port_q[294];
    wire [31:0] q_addr = port_q[293:262];
    wire [31:0] q_errors = port_q[261:230];
    wire [31:0] q_fault = port_q[229:198];
    wire [31:0] q_last = port_q[197:166];
    wire [2:0] q_fault_phase = port_q[165:163];
    wire [127:0] q_bad = port_q[162:35];
    wire q_done = port_q[34];
    wire q_nack = port_q[33];
    wire q_stopped = port_q[32];
    wire [31:0] q_word = q_nack ? "NACK" : status_name(q_stopped, q_done && q_errors == 32'd0,
        q_done && q_errors != 32'd0);

    function [7:0] ddr_rates;
        input [5:0] column;
        begin
            if (column >= 6'd8 && column < 6'd12)
                ddr_rates = decimal4(ddr0_sync1[31:16], column, 6'd8);
            else if (column >= 6'd13 && column < 6'd17)
                ddr_rates = decimal4(ddr1_sync1[31:16], column, 6'd13);
            else if (column >= 6'd18 && column < 6'd22)
                ddr_rates = decimal4(ddr2_sync1[31:16], column, 6'd18);
            else if (column >= 6'd26 && column < 6'd30)
                ddr_rates = decimal4(ddr0_sync1[15:0], column, 6'd26);
            else if (column >= 6'd31 && column < 6'd35)
                ddr_rates = decimal4(ddr1_sync1[15:0], column, 6'd31);
            else if (column >= 6'd36)
                ddr_rates = decimal4(ddr2_sync1[15:0], column, 6'd36);
            else
                ddr_rates = text_at("MB/S WR                RD               ", column);
        end
    endfunction

    function [7:0] ddr_char;
        input [4:0] line;
        input [5:0] column;
        reg [7:0] port_digit;
        begin
            port_digit = 8'h30 + {6'd0, port_index_q};
            if (line == DDR_HEAD) begin
                if (column >= 6'd8 && column < 6'd11)
                    ddr_char = mhz_at(s_mhz, column, 6'd8);
                else
                    ddr_char = text_at("HPS DDR     MHZ 30000000-3FFFFFFF       ", column);
            end else if (line == DDR_RATES) begin
                ddr_char = ddr_rates(column);
            end else if (column == 6'd0) begin
                ddr_char = "P";
            end else if (column == 6'd1) begin
                ddr_char = port_digit;
            end else if (line < DDR_FAULTS) begin
                if (column >= 6'd3 && column < 6'd7)
                    ddr_char = byte4_at(ddr_phase_name(q_phase), column, 6'd3);
                else if (column == 6'd8)
                    ddr_char = q_reading ? "R" : "W";
                else if (column == 6'd10)
                    ddr_char = hex_digit({1'b0, q_phase} + 4'd1);
                else if (column == 6'd11)
                    ddr_char = "/";
                else if (column == 6'd12)
                    ddr_char = "7";
                else if (column >= 6'd14 && column < 6'd22)
                    ddr_char = hex_digit(hex_nibble(q_addr, column, 6'd14));
                else if (column == 6'd23)
                    ddr_char = "E";
                else if (column >= 6'd25 && column < 6'd33)
                    ddr_char = hex_digit(hex_nibble(q_errors, column, 6'd25));
                else if (column >= 6'd34 && column < 6'd38)
                    ddr_char = byte4_at(q_word, column, 6'd34);
                else
                    ddr_char = 8'h00;
            end else if (line < DDR_BITS) begin
                if (column >= 6'd6 && column < 6'd14)
                    ddr_char = hex_digit(hex_nibble(q_fault, column, 6'd6));
                else if (column >= 6'd17 && column < 6'd25)
                    ddr_char = hex_digit(hex_nibble(q_last, column, 6'd17));
                else if (q_errors != 32'd0 && column >= 6'd29 && column < 6'd33)
                    ddr_char = byte4_at(ddr_phase_name(q_fault_phase), column, 6'd29);
                else if (q_errors != 32'd0 && (column == 6'd26 || column == 6'd27))
                    ddr_char = text_at("                          IN            ", column);
                else
                    ddr_char = text_at("   AT          L                        ", column);
            end else begin
                // Port 0 shows 128 bits; ports 1 and 2 show their 64.
                if (column >= 6'd7 && column < (port_index_q == 2'd0 ? 6'd39 : 6'd23))
                    ddr_char = hex_digit(bad_nibble(q_bad, column, port_index_q == 2'd0));
                else
                    ddr_char = text_at("   BAD                                  ", column);
            end
        end
    endfunction

    function [7:0] byte_char;
        input [4:0] line;
        input [5:0] column;
        begin
            if (line == 5'd6) begin
                if (column >= 6'd5 && column < 6'd9)
                    byte_char = byte4_at(byte_sync1[103] ? "NACK" :
                        status_name(byte_sync1[104], byte_sync1[106], byte_sync1[105]), column, 6'd5);
                else if (column >= 6'd15 && column < 6'd19)
                    byte_char = hex_digit(hex16({8'd0, byte_sync1[102:95]}, column, 6'd15));
                else if (column >= 6'd28 && column < 6'd32)
                    byte_char = hex_digit(hex16(byte_sync1[94:79], column, 6'd28));
                else byte_char = text_at("BYTE      CASE     /0080 RD     /0200   ", column);
            end else if (line == 5'd18) begin
                if (column >= 6'd8 && column < 6'd16)
                    byte_char = hex_digit(hex_nibble({6'd0, byte_sync1[78:53]}, column, 6'd8));
                else if (column == 6'd22)
                    byte_char = hex_digit({1'b0, byte_sync1[52:50]});
                else if (column == 6'd27 || column == 6'd28)
                    byte_char = (column == 6'd27 ? byte_sync1[49] : byte_sync1[48]) ? "1" : "0";
                else byte_char = text_at("BYTE AT          STEP   BE              ", column);
            end else begin
                if (column >= 6'd5 && column < 6'd9)
                    byte_char = hex_digit(hex16(byte_sync1[47:32], column, 6'd5));
                else if (column >= 6'd14 && column < 6'd18)
                    byte_char = hex_digit(hex16(byte_sync1[31:16], column, 6'd14));
                else if (column >= 6'd23 && column < 6'd27)
                    byte_char = hex_digit(hex16(byte_sync1[15:0], column, 6'd23));
                else byte_char = text_at("DATA      EXP      GOT                  ", column);
            end
        end
    endfunction

    function [7:0] table_char;
        input [4:0] line;
        input [5:0] column;
        reg [4:0] table_line;
        begin
            table_line = line - TABLE_FIRST;
            if (line == BUTTON_ROW)
                table_char = text_at("BUTTON STOPS                            ", column);
            else if (line == TABLE_HEAD)
                table_char = rate_head(column);
            else if (line >= TABLE_FIRST && line < TABLE_FIRST + 5'd6)
                table_char = pattern_row(table_line[2:0], column);
            else
                table_char = 8'h00;
        end
    endfunction

    // The row selects a DDR port bundle. Each screen section then decodes
    // its character in parallel into its own register, one register picks
    // the section, and the glyph lookup follows. The pixel clock cannot
    // carry a whole-screen decode in one 74.25 MHz cycle. The 3x scale is
    // counted here so the raster does not divide the HDMI counters on this
    // clock.
    reg [9:0] field_x = 10'd0;
    reg [9:0] field_y = 10'd0;
    reg [1:0] x_phase = 2'd0;
    reg [1:0] y_phase = 2'd0;
    reg active_d = 1'b0;
    reg [11:0] inactive = 12'd0;
    reg [9:0] x_q = 10'd0;
    reg [9:0] y_q = 10'd0;
    reg active_q = 1'b0;
    reg [4:0] row_s = 5'd0;
    reg [5:0] col_s = 6'd0;
    reg [2:0] glyph_row_s = 3'd0;
    reg [2:0] glyph_col_s = 3'd0;
    reg active_s = 1'b0;
    reg [7:0] sdram_ch_t = 8'h00;
    reg [7:0] ddr_ch_t = 8'h00;
    reg [7:0] byte_ch_t = 8'h00;
    reg [7:0] table_ch_t = 8'h00;
    reg [2:0] glyph_row_t = 3'd0;
    reg [2:0] glyph_col_t = 3'd0;
    reg active_t = 1'b0;
    reg status_t = 1'b0;
    reg failed_t = 1'b0;
    reg passed_t = 1'b0;
    reg halted_t = 1'b0;
    reg [7:0] ch_q = 8'h00;
    reg [2:0] glyph_row_q = 3'd0;
    reg [2:0] glyph_col_q = 3'd0;
    reg active_c = 1'b0;
    reg status_c = 1'b0;
    reg failed_c = 1'b0;
    reg passed_c = 1'b0;
    reg halted_c = 1'b0;
    reg [7:0] pixels_q = 8'h00;
    reg [2:0] glyph_col_p = 3'd0;
    reg active_p = 1'b0;
    reg status_p = 1'b0;
    reg failed_p = 1'b0;
    reg passed_p = 1'b0;
    reg halted_p = 1'b0;

    wire [4:0] row_q = y_q[7:3];
    wire [1:0] port_now = ddr_port_of(row_q);
    wire [4:0] table_now = row_q - TABLE_FIRST;
    wire ddr_status_row = row_s >= DDR_SUMMARY && row_s < DDR_FAULTS;
    wire status_now = row_s == 5'd5 || row_s == 5'd6 || ddr_status_row;
    wire failed_now = ddr_status_row ? (q_nack || q_errors != 32'd0) : (row_s == 5'd6 ? byte_sync1[105] : s_fail);
    wire passed_now = ddr_status_row ? q_done : (row_s == 5'd6 ? byte_sync1[106] : s_pass);
    wire halted_now = ddr_status_row ? q_stopped : (row_s == 5'd6 ? byte_sync1[104] : s_stopped);

    ram_font font (
        .ch(ch_q),
        .row(glyph_row_q),
        .pixels(glyph_pixels)
    );
    wire ink = active_p && pixels_q[3'd7 - glyph_col_p];
    wire [7:0] ink_red = halted_p ? 8'hE8 : (failed_p ? 8'hE0 : (passed_p ? 8'h20 : 8'hE0));
    wire [7:0] ink_green = halted_p ? 8'hE8 : (failed_p ? 8'h30 : (passed_p ? 8'hC0 : 8'hA0));
    wire [7:0] ink_blue = halted_p ? 8'hE8 : (failed_p ? 8'h28 : (passed_p ? 8'h40 : 8'h20));

    always @(posedge pixel_clk) begin
        byte_sync0 <= byte_status;
        byte_sync1 <= byte_sync0;
        sync0 <= snap;
        sync1 <= sync0;
        ddr0_sync0 <= ddr0;
        ddr0_sync1 <= ddr0_sync0;
        ddr1_sync0 <= ddr1;
        ddr1_sync1 <= ddr1_sync0;
        ddr2_sync0 <= ddr2;
        ddr2_sync1 <= ddr2_sync0;
        active_d <= active;
        x_q <= field_x;
        y_q <= field_y;
        active_q <= active;
        if (!active) begin
            field_x <= 10'd0;
            x_phase <= 2'd0;
            if (inactive != 12'hFFF)
                inactive <= inactive + 12'd1;
            // One falling edge per line. A gap longer than a blanking interval
            // is vertical blank, which starts the next frame at line 0.
            if (inactive == 12'd2000) begin
                field_y <= 10'd0;
                y_phase <= 2'd0;
            end else if (active_d) begin
                if (y_phase == 2'd2) begin
                    y_phase <= 2'd0;
                    field_y <= field_y + 10'd1;
                end else
                    y_phase <= y_phase + 2'd1;
            end
        end else begin
            inactive <= 12'd0;
            if (x_phase == 2'd2) begin
                x_phase <= 2'd0;
                field_x <= field_x + 10'd1;
            end else
                x_phase <= x_phase + 2'd1;
        end
        row_s <= row_q;
        col_s <= x_q[8:3];
        glyph_row_s <= y_q[2:0];
        glyph_col_s <= x_q[2:0];
        active_s <= active_q;
        port_index_q <= port_now;
        table_counts_q <= {pattern_count(s_pat100, table_now[2:0]),
            pattern_count(s_pat75, table_now[2:0]), pattern_count(s_pat50, table_now[2:0])};
        port_q <= port_now == 2'd0 ? ddr0_sync1 : (port_now == 2'd1 ? ddr1_sync1 : ddr2_sync1);
        sdram_ch_t <= row_s < 5'd6 ? line_char(row_s, col_s, "SDRAM", s_phase, s_reading, s_addr,
            s_errors, s_fault, s_last, s_was, s_expect, s_got, s_mhz, s_stopped, s_pass, s_fail) : 8'h00;
        ddr_ch_t <= row_s >= DDR_HEAD && row_s <= DDR_RATES ? ddr_char(row_s, col_s) : 8'h00;
        byte_ch_t <= row_s == 5'd6 || row_s == 5'd18 || row_s == 5'd19 ? byte_char(row_s, col_s) : 8'h00;
        table_ch_t <= table_char(row_s, col_s);
        glyph_row_t <= glyph_row_s;
        glyph_col_t <= glyph_col_s;
        active_t <= active_s;
        status_t <= status_now;
        failed_t <= failed_now;
        passed_t <= passed_now;
        halted_t <= halted_now;
        // At most one section decodes a character on any row.
        ch_q <= sdram_ch_t | ddr_ch_t | table_ch_t | byte_ch_t;
        glyph_row_q <= glyph_row_t;
        glyph_col_q <= glyph_col_t;
        active_c <= active_t;
        status_c <= status_t;
        failed_c <= failed_t;
        passed_c <= passed_t;
        halted_c <= halted_t;
        pixels_q <= glyph_pixels;
        glyph_col_p <= glyph_col_q;
        active_p <= active_c;
        status_p <= status_c;
        failed_p <= failed_c;
        passed_p <= passed_c;
        halted_p <= halted_c;
        if (ink && status_p) begin
            red <= ink_red;
            green <= ink_green;
            blue <= ink_blue;
        end else if (ink) begin
            red <= 8'hE8;
            green <= 8'hEC;
            blue <= 8'hF0;
        end else begin
            red <= 8'h10;
            green <= 8'h14;
            blue <= 8'h18;
        end
    end
endmodule
