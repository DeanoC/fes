// SPDX-License-Identifier: MIT
// D: the address registers a ModRM (and SIB) byte names, for a memory form
// (mod != 3). GPR numbers follow the x86 encoding: AX 0, CX 1, DX 2, BX 3,
// SP 4, BP 5, SI 6, DI 7.
//
// 16-bit forms add one or two of BX/BP (base) and SI/DI (index), unscaled.
// In 32-bit forms a SIB index of 100b means no index; with a nonzero scale
// the i386 then scales the base (Phase 0 divergence invalid-sib-scales-base),
// so the base moves to the scaled-index slot. stack is set when the base
// field names BP, EBP or ESP: the default segment is SS.
module fes_i386_amode (
    input  logic [7:0] modrm,
    input  logic [7:0] sib,
    input  logic       addr32,
    output logic       has_base,
    output logic [2:0] base,
    output logic       has_index,
    output logic [2:0] index,
    output logic [1:0] scale,
    output logic       stack
);
    logic [1:0] mode;
    logic [2:0] rm;
    logic       sib_form, no_base32, scaled_base;

    assign mode = modrm[7:6];
    assign rm = modrm[2:0];
    assign sib_form = rm == 3'd4;
    // mod 00 with r/m 101b (or SIB base 101b): a 32-bit displacement and no base.
    assign no_base32 = mode == 2'b00 && (sib_form ? sib[2:0] == 3'd5 : rm == 3'd5);
    assign scaled_base = sib_form && sib[5:3] == 3'd4 && sib[7:6] != 2'b00 && !no_base32;

    always_comb begin
        if (!addr32) begin
            // 16-bit: r/m 0-3 pair BX or BP with SI or DI; 4, 5 are SI, DI;
            // 6 is BP (a disp16 alone with mod 00); 7 is BX.
            has_base = rm[2:1] != 2'b10 && !(rm == 3'd6 && mode == 2'b00);
            base = (rm == 3'd2 || rm == 3'd3 || rm == 3'd6) ? 3'd5 : 3'd3;
            has_index = !rm[2] || rm[2:1] == 2'b10;
            index = rm[0] ? 3'd7 : 3'd6;
            scale = 2'd0;
            stack = has_base && base == 3'd5;
        end else if (sib_form) begin
            has_base = !no_base32 && !scaled_base;
            base = sib[2:0];
            has_index = sib[5:3] != 3'd4 || scaled_base;
            index = scaled_base ? sib[2:0] : sib[5:3];
            scale = sib[7:6];
            stack = !no_base32 && (sib[2:0] == 3'd4 || sib[2:0] == 3'd5);
        end else begin
            has_base = !no_base32;
            base = rm;
            has_index = 1'b0;
            index = 3'd0;
            scale = 2'd0;
            stack = !no_base32 && rm == 3'd5;
        end
    end
endmodule
