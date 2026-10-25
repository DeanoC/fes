// SPDX-License-Identifier: MIT
// The length classes of the x86 instruction whose opcode is o[0], with no
// prefix. o[0] may be the 0Fh escape. Only the opcode, ModRM and SIB bytes
// decide a length, so four bytes suffice. The length for an effective operand
// and address size is
//   head + modrm_bytes[as32] + (z ? (os32 ? 4 : 2) : 0) + (moffs ? (as32 ? 4 : 2) : 0)
// which L2 adds (fes_i386_lstage); this unit, in L1, only classifies.
// Lengths follow the i386: the ModRM of MOV to or from CR, DR and TR is always
// a register form, and F6h/F7h carry an immediate for /0 and /1. Encodings
// the i386 does not define get some length; D raises #UD for them.
module fes_i386_oplen (
    input  logic [3:0][7:0] o,
    output logic [2:0]      head,         // opcode bytes and fixed immediate bytes, 1-5
    output logic            z,            // an immediate of 2 or 4 bytes by operand size
    output logic            moffs,        // an offset of 2 or 4 bytes by address size
    output logic [1:0][2:0] modrm_bytes   // ModRM, SIB and displacement bytes by as32
);
    typedef enum logic [2:0] {
        IMM_NONE,   // no immediate
        IMM_B,      // 1 byte
        IMM_W,      // 2 bytes
        IMM_WB,     // ENTER: 3 bytes
        IMM_Z,      // 2 or 4 bytes by operand size
        IMM_AP,     // far pointer: 2 bytes and IMM_Z
        IMM_MOFFS   // 2 or 4 bytes by address size
    } imm_e;

    typedef struct packed {
        logic modrm;
        logic creg;   // ModRM present and always a register form
        imm_e imm;
    } form_t;

    function automatic form_t form(input logic modrm, input logic creg, input imm_e imm);
        form_t f;
        f.modrm = modrm;
        f.creg = creg;
        f.imm = imm;
        return f;
    endfunction

    function automatic form_t one_byte(input logic [7:0] op, input logic [2:0] reg_field);
        form_t f;
        f = form(1'b0, 1'b0, IMM_NONE);
        if (op[7:6] == 2'b00) begin
            // The ALU block: r/m forms, AL/eAX immediates, then one-byte
            // pushes, pops, prefixes and BCD adjusts.
            case (op[2:0])
                3'd0, 3'd1, 3'd2, 3'd3: f.modrm = 1'b1;
                3'd4: f.imm = IMM_B;
                3'd5: f.imm = IMM_Z;
                default: ;
            endcase
        end else begin
            case (op)
                8'h62, 8'h63: f.modrm = 1'b1;
                8'h68: f.imm = IMM_Z;
                8'h69: f = form(1'b1, 1'b0, IMM_Z);
                8'h6a: f.imm = IMM_B;
                8'h6b: f = form(1'b1, 1'b0, IMM_B);
                8'h70, 8'h71, 8'h72, 8'h73, 8'h74, 8'h75, 8'h76, 8'h77,
                8'h78, 8'h79, 8'h7a, 8'h7b, 8'h7c, 8'h7d, 8'h7e, 8'h7f: f.imm = IMM_B;
                8'h80, 8'h82, 8'h83: f = form(1'b1, 1'b0, IMM_B);
                8'h81: f = form(1'b1, 1'b0, IMM_Z);
                8'h84, 8'h85, 8'h86, 8'h87, 8'h88, 8'h89, 8'h8a, 8'h8b,
                8'h8c, 8'h8d, 8'h8e, 8'h8f: f.modrm = 1'b1;
                8'h9a, 8'hea: f.imm = IMM_AP;
                8'ha0, 8'ha1, 8'ha2, 8'ha3: f.imm = IMM_MOFFS;
                8'ha8: f.imm = IMM_B;
                8'ha9: f.imm = IMM_Z;
                8'hb0, 8'hb1, 8'hb2, 8'hb3, 8'hb4, 8'hb5, 8'hb6, 8'hb7: f.imm = IMM_B;
                8'hb8, 8'hb9, 8'hba, 8'hbb, 8'hbc, 8'hbd, 8'hbe, 8'hbf: f.imm = IMM_Z;
                8'hc0, 8'hc1, 8'hc6: f = form(1'b1, 1'b0, IMM_B);
                8'hc7: f = form(1'b1, 1'b0, IMM_Z);
                8'hc2, 8'hca: f.imm = IMM_W;
                8'hc4, 8'hc5: f.modrm = 1'b1;
                8'hc8: f.imm = IMM_WB;
                8'hcd, 8'hd4, 8'hd5: f.imm = IMM_B;
                8'hd0, 8'hd1, 8'hd2, 8'hd3,
                8'hd8, 8'hd9, 8'hda, 8'hdb, 8'hdc, 8'hdd, 8'hde, 8'hdf: f.modrm = 1'b1;
                8'he0, 8'he1, 8'he2, 8'he3, 8'he4, 8'he5, 8'he6, 8'he7, 8'heb: f.imm = IMM_B;
                8'he8, 8'he9: f.imm = IMM_Z;
                8'hf6: f = form(1'b1, 1'b0, reg_field[2:1] == 2'b00 ? IMM_B : IMM_NONE);
                8'hf7: f = form(1'b1, 1'b0, reg_field[2:1] == 2'b00 ? IMM_Z : IMM_NONE);
                8'hfe, 8'hff: f.modrm = 1'b1;
                default: ;
            endcase
        end
        return f;
    endfunction

    // The 0Fh map. The i486 forms (CMPXCHG, XADD, BSWAP) get their lengths
    // for the later i486-isa profile. 0F 0D (NOP Ev) and 0F B9 (UD1) raise
    // #UD on the i386; they keep the ModRM length the oracle decodes.
    function automatic form_t two_byte(input logic [7:0] op);
        form_t f;
        f = form(1'b0, 1'b0, IMM_NONE);
        case (op)
            8'h00, 8'h01, 8'h02, 8'h03, 8'h0d, 8'hb9: f.modrm = 1'b1;
            8'h20, 8'h21, 8'h22, 8'h23, 8'h24, 8'h26: f = form(1'b1, 1'b1, IMM_NONE);
            8'h80, 8'h81, 8'h82, 8'h83, 8'h84, 8'h85, 8'h86, 8'h87,
            8'h88, 8'h89, 8'h8a, 8'h8b, 8'h8c, 8'h8d, 8'h8e, 8'h8f: f.imm = IMM_Z;
            8'h90, 8'h91, 8'h92, 8'h93, 8'h94, 8'h95, 8'h96, 8'h97,
            8'h98, 8'h99, 8'h9a, 8'h9b, 8'h9c, 8'h9d, 8'h9e, 8'h9f: f.modrm = 1'b1;
            8'ha3, 8'ha5, 8'hab, 8'had, 8'haf: f.modrm = 1'b1;
            8'ha4, 8'hac, 8'hba: f = form(1'b1, 1'b0, IMM_B);
            8'hb0, 8'hb1, 8'hb2, 8'hb3, 8'hb4, 8'hb5, 8'hb6, 8'hb7,
            8'hbb, 8'hbc, 8'hbd, 8'hbe, 8'hbf, 8'hc0, 8'hc1: f.modrm = 1'b1;
            default: ;
        endcase
        return f;
    endfunction

    // ModRM, SIB and displacement bytes.
    function automatic logic [2:0] modrm16(input logic [7:0] m);
        case (m[7:6])
            2'b00: return m[2:0] == 3'd6 ? 3'd3 : 3'd1;
            2'b01: return 3'd2;
            2'b10: return 3'd3;
            default: return 3'd1;
        endcase
    endfunction

    function automatic logic [2:0] modrm32(input logic [7:0] m, input logic [7:0] sib);
        logic has_sib;
        has_sib = m[2:0] == 3'd4;
        case (m[7:6])
            2'b00: return has_sib ? (sib[2:0] == 3'd5 ? 3'd6 : 3'd2) : (m[2:0] == 3'd5 ? 3'd5 : 3'd1);
            2'b01: return has_sib ? 3'd3 : 3'd2;
            2'b10: return has_sib ? 3'd6 : 3'd5;
            default: return 3'd1;
        endcase
    endfunction

    logic       escape;
    form_t      f;
    logic [7:0] modrm, sib;

    assign escape = o[0] == 8'h0f;
    assign f = escape ? two_byte(o[1]) : one_byte(o[0], o[1][5:3]);
    assign modrm = escape ? o[2] : o[1];
    assign sib = escape ? o[3] : o[2];
    assign modrm_bytes[0] = !f.modrm ? 3'd0 : f.creg ? 3'd1 : modrm16(modrm);
    assign modrm_bytes[1] = !f.modrm ? 3'd0 : f.creg ? 3'd1 : modrm32(modrm, sib);
    logic [2:0] fixed;  // fixed immediate bytes
    always_comb begin
        case (f.imm)
            IMM_B: fixed = 3'd1;
            IMM_W, IMM_AP: fixed = 3'd2;
            IMM_WB: fixed = 3'd3;
            default: fixed = 3'd0;
        endcase
    end
    assign head = (escape ? 3'd2 : 3'd1) + (* no_carry *) fixed;
    assign z = f.imm == IMM_Z || f.imm == IMM_AP;
    assign moffs = f.imm == IMM_MOFFS;
endmodule
