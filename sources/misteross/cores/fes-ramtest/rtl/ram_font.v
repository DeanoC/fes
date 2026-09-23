// SPDX-License-Identifier: GPL-2.0-or-later
// 8x8 glyphs for the RAM tester. Row 0 is the top. The high bit is the left pixel.
module ram_font (
    input wire [7:0] ch,
    input wire [2:0] row,
    output wire [7:0] pixels
);
    function [63:0] bitmap;
        input [7:0] code;
        begin
            case (code)
                "0": bitmap = 64'h3C666E7666663C00;
                "1": bitmap = 64'h1838181818187E00;
                "2": bitmap = 64'h3C66060C18307E00;
                "3": bitmap = 64'h3C66061C06663C00;
                "4": bitmap = 64'h0C1C3C6C7E0C0C00;
                "5": bitmap = 64'h7E607C0606663C00;
                "6": bitmap = 64'h1C30607C66663C00;
                "7": bitmap = 64'h7E060C1830303000;
                "8": bitmap = 64'h3C66663C66663C00;
                "9": bitmap = 64'h3C66663E060C3800;
                "A": bitmap = 64'h183C66667E666600;
                "B": bitmap = 64'h7C66667C66667C00;
                "D": bitmap = 64'h786C6666666C7800;
                "E": bitmap = 64'h7E60607C60607E00;
                "F": bitmap = 64'h7E60607C60606000;
                "G": bitmap = 64'h3C66606E66663C00;
                "H": bitmap = 64'h6666667E66666600;
                "I": bitmap = 64'h7E18181818187E00;
                "L": bitmap = 64'h6060606060607E00;
                "M": bitmap = 64'h63777F6B63636300;
                "N": bitmap = 64'h66767E7E6E666600;
                "O": bitmap = 64'h3C66666666663C00;
                "P": bitmap = 64'h7C66667C60606000;
                "R": bitmap = 64'h7C66667C6C666600;
                "S": bitmap = 64'h3C66603C06663C00;
                "T": bitmap = 64'h7E18181818181800;
                "U": bitmap = 64'h6666666666663C00;
                "V": bitmap = 64'h66666666663C1800;
                "W": bitmap = 64'h66666666666E3C00;
                "X": bitmap = 64'h66663C183C666600;
                "/": bitmap = 64'h0002040810204000;
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
