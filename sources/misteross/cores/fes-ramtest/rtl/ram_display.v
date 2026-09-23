// SPDX-License-Identifier: GPL-2.0-or-later
// Fixed 720p playfield text for the two memory scans. Pass is green, fail is
// red, a scan still running is amber, and a button stop is white.
module ram_display (
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
    input wire sdram_stopped,
    input wire [2:0] hps_phase,
    input wire hps_reading,
    input wire [31:0] hps_addr,
    input wire [31:0] hps_fault,
    input wire [31:0] hps_last,
    input wire [15:0] hps_was,
    input wire [31:0] hps_errors,
    input wire [15:0] hps_expect,
    input wire [15:0] hps_got,
    input wire hps_pass,
    input wire hps_fail,
    input wire hps_stopped,
    output reg [7:0] red,
    output reg [7:0] green,
    output reg [7:0] blue
);
    reg [365:0] sync0 = 366'd0;
    reg [365:0] sync1 = 366'd0;
    wire [365:0] snap = {
        sdram_phase, sdram_reading, sdram_addr, sdram_fault, sdram_last, sdram_errors, sdram_was,
        sdram_expect, sdram_got, sdram_pass, sdram_fail, sdram_stopped,
        hps_phase, hps_reading, hps_addr, hps_fault, hps_last, hps_errors, hps_was,
        hps_expect, hps_got, hps_pass, hps_fail, hps_stopped
    };
    wire [2:0] s_phase = sync1[365:363];
    wire s_reading = sync1[362];
    wire [31:0] s_addr = sync1[361:330];
    wire [31:0] s_fault = sync1[329:298];
    wire [31:0] s_last = sync1[297:266];
    wire [31:0] s_errors = sync1[265:234];
    wire [15:0] s_was = sync1[233:218];
    wire [15:0] s_expect = sync1[217:202];
    wire [15:0] s_got = sync1[201:186];
    wire s_pass = sync1[185];
    wire s_fail = sync1[184];
    wire s_stopped = sync1[183];
    wire [2:0] h_phase = sync1[182:180];
    wire h_reading = sync1[179];
    wire [31:0] h_addr = sync1[178:147];
    wire [31:0] h_fault = sync1[146:115];
    wire [31:0] h_last = sync1[114:83];
    wire [31:0] h_errors = sync1[82:51];
    wire [15:0] h_was = sync1[50:35];
    wire [15:0] h_expect = sync1[34:19];
    wire [15:0] h_got = sync1[18:3];
    wire h_pass = sync1[2];
    wire h_fail = sync1[1];
    wire h_stopped = sync1[0];

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
            case (column - first)
                6'd0: hex_nibble = value[31:28];
                6'd1: hex_nibble = value[27:24];
                6'd2: hex_nibble = value[23:20];
                6'd3: hex_nibble = value[19:16];
                6'd4: hex_nibble = value[15:12];
                6'd5: hex_nibble = value[11:8];
                6'd6: hex_nibble = value[7:4];
                6'd7: hex_nibble = value[3:0];
                default: hex_nibble = 4'h0;
            endcase
        end
    endfunction

    function [3:0] hex16;
        input [15:0] value;
        input [5:0] column;
        input [5:0] first;
        begin
            case (column - first)
                6'd0: hex16 = value[15:12];
                6'd1: hex16 = value[11:8];
                6'd2: hex16 = value[7:4];
                6'd3: hex16 = value[3:0];
                default: hex16 = 4'h0;
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
                    else
                        line_char = 8'h00;
                end
                5'd2: begin
                    if (column < 6'd4)
                        line_char = byte4("ADDR", column);
                    else if (column >= 6'd5 && column < 6'd13)
                        line_char = hex_digit(hex_nibble(cursor, column, 6'd5));
                    else if (column >= 6'd14 && column < 6'd16)
                        line_char = byte4("AT  ", column - 6'd14);
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
                        line_char = byte4("WAS ", column - 6'd13);
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
                        line_char = byte4("GOT ", column - 6'd9);
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

    function [7:0] screen_char;
        input [4:0] line;
        input [5:0] column;
        begin
            if (line < 5'd6)
                screen_char = line_char(line, column, "SDRAM", s_phase, s_reading, s_addr,
                    s_errors, s_fault, s_last, s_was, s_expect, s_got, s_stopped, s_pass, s_fail);
            else if (line >= 5'd7 && line < 5'd13)
                screen_char = line_char(line - 5'd7, column, "HPS  ", h_phase, h_reading, h_addr,
                    h_errors, h_fault, h_last, h_was, h_expect, h_got, h_stopped, h_pass, h_fail);
            else if (line == 5'd14) begin
                if (column < 6'd6)
                    screen_char = byte6("BUTTON", column);
                else if (column >= 6'd7 && column < 6'd12)
                    screen_char = byte5("STOPS", column - 6'd7);
                else
                    screen_char = 8'h00;
            end else
                screen_char = 8'h00;
        end
    endfunction

    // The character decode is registered away from the video counters, then
    // the glyph lookup is registered away from that decode. The pixel clock
    // cannot carry both in one 74.25 MHz cycle.
    reg [9:0] x_q = 10'd0;
    reg [9:0] y_q = 10'd0;
    reg active_q = 1'b0;
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
    wire [5:0] col_q = x_q[8:3];
    wire [7:0] ch_now = screen_char(row_q, col_q);
    wire status_now = row_q == 5'd5 || row_q == 5'd12;
    wire failed_now = (row_q == 5'd5) ? s_fail : h_fail;
    wire passed_now = (row_q == 5'd5) ? s_pass : h_pass;
    wire halted_now = (row_q == 5'd5) ? s_stopped : h_stopped;

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
        sync0 <= snap;
        sync1 <= sync0;
        x_q <= x;
        y_q <= y;
        active_q <= active;
        ch_q <= ch_now;
        glyph_row_q <= y_q[2:0];
        glyph_col_q <= x_q[2:0];
        active_c <= active_q;
        status_c <= status_now;
        failed_c <= failed_now;
        passed_c <= passed_now;
        halted_c <= halted_now;
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
