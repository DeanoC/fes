// SPDX-License-Identifier: GPL-2.0-or-later
// USB HID key state to the ZX Spectrum 40-key matrix.
//
// The matrix is active-low, eight rows of five keys, row-major:
//   CAPS Z X C V / A S D F G / Q W E R T / 1 2 3 4 5 /
//   0 9 8 7 6 / P O I U Y / ENTER L K J H / SPACE SYM M N B.
// Rows must be unchanged for about a millisecond before they are applied,
// so a key and its modifiers from one host update are seen together.
// Left/right Shift are CAPS SHIFT. Left/right Control are SYMBOL SHIFT.
// Backspace is CAPS+0, the arrows are CAPS+5/6/7/8, Escape is CAPS+SPACE.
module spectrum_keyboard #(
    parameter integer STABLE_CYCLES = 52224
) (
    input  wire        clk,
    input  wire        reset,
    input  wire [143:0] rows,
    output reg  [39:0] matrix
);
    reg [143:0] held = 144'd0;
    reg [16:0] quiet = 17'd0;
    reg primed = 1'b0;

    function [5:0] key_index;
        input [6:0] usage;
        begin
            case (usage)
                7'h04: key_index = 6'd5;   // A
                7'h05: key_index = 6'd39;  // B
                7'h06: key_index = 6'd3;   // C
                7'h07: key_index = 6'd7;   // D
                7'h08: key_index = 6'd12;  // E
                7'h09: key_index = 6'd8;   // F
                7'h0a: key_index = 6'd9;   // G
                7'h0b: key_index = 6'd34;  // H
                7'h0c: key_index = 6'd27;  // I
                7'h0d: key_index = 6'd33;  // J
                7'h0e: key_index = 6'd32;  // K
                7'h0f: key_index = 6'd31;  // L
                7'h10: key_index = 6'd37;  // M
                7'h11: key_index = 6'd38;  // N
                7'h12: key_index = 6'd26;  // O
                7'h13: key_index = 6'd25;  // P
                7'h14: key_index = 6'd10;  // Q
                7'h15: key_index = 6'd13;  // R
                7'h16: key_index = 6'd6;   // S
                7'h17: key_index = 6'd14;  // T
                7'h18: key_index = 6'd28;  // U
                7'h19: key_index = 6'd4;   // V
                7'h1a: key_index = 6'd11;  // W
                7'h1b: key_index = 6'd2;   // X
                7'h1c: key_index = 6'd29;  // Y
                7'h1d: key_index = 6'd1;   // Z
                7'h1e: key_index = 6'd15;  // 1
                7'h1f: key_index = 6'd16;  // 2
                7'h20: key_index = 6'd17;  // 3
                7'h21: key_index = 6'd18;  // 4
                7'h22: key_index = 6'd19;  // 5
                7'h23: key_index = 6'd24;  // 6
                7'h24: key_index = 6'd23;  // 7
                7'h25: key_index = 6'd22;  // 8
                7'h26: key_index = 6'd21;  // 9
                7'h27: key_index = 6'd20;  // 0
                7'h28: key_index = 6'd30;  // Enter
                7'h2c: key_index = 6'd35;  // Space
                default: key_index = 6'd63;
            endcase
        end
    endfunction

    reg [39:0] decoded;
    integer usage;
    reg [6:0] usage_code;

    always @* begin
        decoded = 40'hffffffffff;
        for (usage = 4; usage < 128; usage = usage + 1) begin
            usage_code = usage[6:0];
            if (rows[{usage_code[6:4], 4'b0000} + {7'b0, usage_code[3:0]}]) begin
                if (key_index(usage_code) < 6'd40)
                    decoded[key_index(usage_code)] = 1'b0;
                // CAPS chords: backspace, arrows and Escape.
                if (usage_code == 7'h2a || usage_code == 7'h29 ||
                    usage_code == 7'h4f || usage_code == 7'h50 ||
                    usage_code == 7'h51 || usage_code == 7'h52)
                    decoded[0] = 1'b0;
                if (usage_code == 7'h2a) decoded[20] = 1'b0; // CAPS+0
                if (usage_code == 7'h50) decoded[19] = 1'b0; // CAPS+5
                if (usage_code == 7'h51) decoded[24] = 1'b0; // CAPS+6
                if (usage_code == 7'h52) decoded[23] = 1'b0; // CAPS+7
                if (usage_code == 7'h4f) decoded[22] = 1'b0; // CAPS+8
                if (usage_code == 7'h29) decoded[35] = 1'b0; // CAPS+SPACE
            end
        end
        if (rows[8*16+1] || rows[8*16+5]) decoded[0] = 1'b0;
        if (rows[8*16+0] || rows[8*16+4]) decoded[36] = 1'b0;
    end

    always @(posedge clk) begin
        if (reset) begin
            held <= 144'd0;
            quiet <= 17'd0;
            primed <= 1'b0;
            matrix <= 40'hffffffffff;
        end else if (rows != held) begin
            held <= rows;
            quiet <= 17'd0;
            primed <= 1'b0;
        end else if (!primed) begin
            if (quiet == STABLE_CYCLES[16:0] - 17'd1) begin
                matrix <= decoded;
                quiet <= 17'd0;
                primed <= 1'b1;
            end else begin
                quiet <= quiet + 17'd1;
            end
        end
    end
endmodule
