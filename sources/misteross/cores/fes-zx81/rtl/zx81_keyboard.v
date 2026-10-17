// SPDX-License-Identifier: GPL-2.0-or-later
// USB HID key state to the ZX81 8x5 matrix.
//
// The matrix is active-low. Bit 0 of each row is the left key:
//   SHIFT Z X C V / A S D F G / Q W E R T / 1 2 3 4 5 /
//   0 9 8 7 6 / P O I U Y / NEWLINE L K J H / SPACE . M N B.
// The packed output is {row7, row6, row5, row4, row3, row2, row1, row0}.
// Rows must be unchanged for about a millisecond before they are applied.
// Left and right Shift are SHIFT. Enter is NEWLINE. Backspace is SHIFT+0.
module zx81_keyboard #(
    parameter integer STABLE_CYCLES = 52224
) (
    input  wire         clk,
    input  wire         reset,
    input  wire [143:0] rows,
    output reg  [39:0]  matrix
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
                7'h28: key_index = 6'd30;  // Enter / NEWLINE
                7'h2c: key_index = 6'd35;  // Space
                7'h37: key_index = 6'd36;  // Period
                default: key_index = 6'd63;
            endcase
        end
    endfunction

    function [39:0] press;
        input [39:0] base;
        input [5:0] index;
        begin
            press = base;
            if (index < 6'd40)
                press[index] = 1'b0;
        end
    endfunction

    reg [39:0] next_matrix;
    integer bit_index;
    always @* begin
        next_matrix = 40'hff_ffff_ffff;
        for (bit_index = 0; bit_index < 128; bit_index = bit_index + 1) begin
            if (held[bit_index])
                next_matrix = press(next_matrix, key_index(bit_index[6:0]));
        end
        // Modifier row: 129/133 are Left/Right Shift. 128/132 are Control.
        if (held[129] || held[133])
            next_matrix[0] = 1'b0;
        if (held[42]) begin
            next_matrix[0] = 1'b0;
            next_matrix[20] = 1'b0;
        end
    end

    always @(posedge clk) begin
        if (reset) begin
            held <= 144'd0;
            quiet <= 17'd0;
            primed <= 1'b0;
            matrix <= 40'hff_ffff_ffff;
        end else if (rows != held) begin
            held <= rows;
            quiet <= 17'd0;
            primed <= 1'b0;
        end else if (!primed) begin
            if (quiet == STABLE_CYCLES[16:0] - 17'd1) begin
                matrix <= next_matrix;
                primed <= 1'b1;
            end else
                quiet <= quiet + 17'd1;
        end
    end
endmodule
