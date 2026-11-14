// SPDX-License-Identifier: GPL-2.0-or-later
// 8x8 glyphs for the RAM tester. Row 0 is the top. The high bit is the left pixel.
// The glyphs are the square-cornered FES block face drawn for FES (the C64
// diagnostic font uses the same face); they are not copied from any ROM font.
module ram_font (
    input wire [7:0] ch,
    input wire [2:0] row,
    output wire [7:0] pixels
);
    function [63:0] bitmap;
        input [7:0] code;
        begin
            case (code)
                "0": bitmap = 64'h7E666E7E76667E00;
                "1": bitmap = 64'h1838781818187E00;
                "2": bitmap = 64'h7E06067E60607E00;
                "3": bitmap = 64'h7E06063E06067E00;
                "4": bitmap = 64'h6666667E06060600;
                "5": bitmap = 64'h7E60607E06067E00;
                "6": bitmap = 64'h7E60607E66667E00;
                "7": bitmap = 64'h7E06060C18181800;
                "8": bitmap = 64'h7E66663C66667E00;
                "9": bitmap = 64'h7E66667E06067E00;
                "A": bitmap = 64'h7E66667E66666600;
                "B": bitmap = 64'h7C66667E66667C00;
                "C": bitmap = 64'h7E66606060667E00;
                "D": bitmap = 64'h7C66666666667C00;
                "E": bitmap = 64'h7E60607C60607E00;
                "F": bitmap = 64'h7E60607C60606000;
                "G": bitmap = 64'h7E66606E66667E00;
                "H": bitmap = 64'h6666667E66666600;
                "I": bitmap = 64'h7E18181818187E00;
                "K": bitmap = 64'h66666C786C666600;
                "L": bitmap = 64'h6060606060607E00;
                "M": bitmap = 64'h7F6B6B6B63636300;
                "N": bitmap = 64'h6676767E6E6E6600;
                "O": bitmap = 64'h7E66666666667E00;
                "P": bitmap = 64'h7E66667E60606000;
                "R": bitmap = 64'h7E66667E6C666600;
                "S": bitmap = 64'h7E66607E06667E00;
                "T": bitmap = 64'h7E18181818181800;
                "U": bitmap = 64'h6666666666667E00;
                "V": bitmap = 64'h666666663C3C1800;
                "W": bitmap = 64'h6363636B6B6B7F00;
                "X": bitmap = 64'h6666663C66666600;
                "Y": bitmap = 64'h6666667E18181800;
                "Z": bitmap = 64'h7E060E1C38707E00;
                "/": bitmap = 64'h02060C1830604000;
                "-": bitmap = 64'h0000003C00000000;
                default: bitmap = 64'h0000000000000000;
            endcase
        end
    endfunction

    function [7:0] row_bits;
        input [63:0] bits;
        input [2:0] which;
        begin
            case (which)
                3'd0: row_bits = bits[63:56];
                3'd1: row_bits = bits[55:48];
                3'd2: row_bits = bits[47:40];
                3'd3: row_bits = bits[39:32];
                3'd4: row_bits = bits[31:24];
                3'd5: row_bits = bits[23:16];
                3'd6: row_bits = bits[15:8];
                default: row_bits = bits[7:0];
            endcase
        end
    endfunction

    assign pixels = row_bits(bitmap(ch), row);
endmodule
