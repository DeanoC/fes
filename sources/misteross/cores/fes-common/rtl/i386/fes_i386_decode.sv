// SPDX-License-Identifier: MIT
// D: the x86 instruction Q aligned (fes_i386_pkg::fetched_t) to one uop
// (docs/i386-cpu-design.md, "Decode and microcode"). Phase 1's register-only
// slice decodes the one-uop register and immediate forms:
//   ALU group 1, INC, DEC, NEG, NOT, TEST, MOV (including to and from a
//   segment register in real mode), LEA, shifts and rotates, SHLD and SHRD,
//   SETcc, MOVZX and MOVSX, CBW/CWDE, CWD/CDQ, SAHF, LAHF, the flag set and
//   clear instructions, NOP, HLT, and the near JMP, Jcc, LOOP, LOOPE, LOOPNE
//   and JCXZ.
// Encodings the i386 rejects raise #UD (LOCK on any of these, MOV to CS or
// to segment register 6 or 7, LEA of a register, C6h/C7h with reg != 0);
// an instruction over 15 bytes raises #GP(0). Everything else, memory forms
// included, decodes as FAULT_UNIMPLEMENTED.
//
// In 16-bit code an instruction whose next EIP passes FFFFh is also
// FAULT_UNIMPLEMENTED: fetch would have to wrap to the segment's start, and
// the CS-limit check that goes with it is not built yet (wrap16).
//
// src1 is always the destination's register: a byte or word result merges
// into its old value. A LOOP or JCXZ counts in CX or ECX by address size, which
// is the uop's size; its target is truncated by operand size (opsize32).
module fes_i386_decode (
    input  fes_i386_pkg::fetched_t q,
    input  logic                   code32,      // CS.D
    input  logic                   wrap16,      // 16-bit code and the next EIP passes FFFFh
    output fes_i386_pkg::uop_t     uop,
    output logic                   opsize32,
    output logic                   lea,         // address operands go to AG
    output logic                   lea_early,   // ... and the offset is the whole result (32-bit LEA)
    output logic [31:0]            lea_disp,    // zero-extended from 16 bits when !addr32
    output logic                   branch,      // AG computes the target from EIP + length + imm
    output logic                   reads_seg,   // b is the selector of segment seg_index
    output logic [2:0]             seg_index,
    output logic                   stop         // nothing after this uop can retire: HLT or a fault
);
    typedef fes_i386_pkg::uop_t uop_t;

    logic [10:0][7:0] by;
    logic [7:0]       b0, op2, modrm;
    logic [1:0]       mode;
    logic [2:0]       reg_f, rm;
    logic             two, addr32, reg_form;
    logic [1:0]       osz;  // a size_e: SIZE_16 or SIZE_32

    assign by = q.bytes;
    assign b0 = by[0];
    assign two = b0 == 8'h0f;
    assign op2 = by[1];
    assign modrm = two ? by[2] : by[1];
    assign mode = modrm[7:6];
    assign reg_f = modrm[5:3];
    assign rm = modrm[2:0];
    assign reg_form = mode == 2'b11;
    assign opsize32 = code32 ^ q.prefixes.opsize;
    assign addr32 = code32 ^ q.prefixes.adsize;
    assign osz = opsize32 ? 2'd2 : 2'd1;

    // Immediates: after ModRM in a register form, or straight after the opcode.
    logic [31:0] imm8_m, imm8s_m, immz_m, imm8_o, rel8, relz1, relz2, imm8_m2;
    assign imm8_m = {24'b0, by[2]};
    assign imm8s_m = {{24{by[2][7]}}, by[2]};
    assign immz_m = opsize32 ? {by[5], by[4], by[3], by[2]} : {16'b0, by[3], by[2]};
    assign imm8_m2 = {24'b0, by[3]};
    assign imm8_o = {24'b0, by[1]};
    // Branch displacements, as AG's disp: 32 bits, or zero-extended from 16
    // when the operand size is 16 (the target wraps at 64 KiB).
    assign rel8 = opsize32 ? {{24{by[1][7]}}, by[1]} : {16'b0, {8{by[1][7]}}, by[1]};
    assign relz1 = opsize32 ? {by[4], by[3], by[2], by[1]} : {16'b0, by[2], by[1]};
    assign relz2 = opsize32 ? {by[5], by[4], by[3], by[2]} : {16'b0, by[3], by[2]};

    // ---- The opcode table -------------------------------------------------------
    logic [2:0]  cls;
    logic [4:0]  op;
    logic [1:0]  size, src_size;
    logic        count_cl;
    logic        s1_valid, s1_hi8, s2_valid, s2_hi8, d_valid, d_seg, d_hi8;
    logic [2:0]  s1_idx, s2_idx, d_idx;
    logic [31:0] imm;
    logic [3:0]  cond;
    logic        rd_cf, rd_oszap, wr_cf, wr_oszap;
    logic        known, undefined;

    localparam logic [2:0] C_ALU = 3'd0, C_SHIFT = 3'd1, C_BRANCH = 3'd4, C_SEGMENT = 3'd5, C_SPECIAL = 3'd7;

    always_comb begin
        cls = C_ALU;
        op = 5'd0;
        size = osz;
        src_size = osz;
        count_cl = 1'b0;
        s1_valid = 1'b0;
        s1_idx = 3'd0;
        s1_hi8 = 1'b0;
        s2_valid = 1'b0;
        s2_idx = 3'd0;
        s2_hi8 = 1'b0;
        d_valid = 1'b0;
        d_seg = 1'b0;
        d_idx = 3'd0;
        d_hi8 = 1'b0;
        imm = 32'd0;
        cond = 4'd0;
        rd_cf = 1'b0;
        rd_oszap = 1'b0;
        wr_cf = 1'b0;
        wr_oszap = 1'b0;
        lea = 1'b0;
        branch = 1'b0;
        reads_seg = 1'b0;
        seg_index = 3'd0;
        known = 1'b1;
        undefined = 1'b0;

        if (!two) begin
            casez (b0)
                // Group 1, r/m and accumulator forms: ADD OR ADC SBB AND SUB XOR CMP.
                // Columns 6 and 7 hold segment PUSH and POP and the BCD adjusts.
                8'b00??????: begin
                    known = b0[2:1] != 2'b11;
                    op = {2'b00, b0[5:3]};
                    size = b0[0] ? osz : 2'd0;
                    rd_cf = b0[5:3] == 3'd2 || b0[5:3] == 3'd3;
                    wr_cf = 1'b1;
                    wr_oszap = 1'b1;
                    if (b0[2:1] == 2'b10) begin
                        // AL/eAX, imm
                        s1_valid = 1'b1;
                        imm = b0[0] ? (opsize32 ? {by[4], by[3], by[2], by[1]} : {16'b0, by[2], by[1]}) : imm8_o;
                    end else begin
                        known = known && reg_form;
                        s1_valid = 1'b1;
                        s1_idx = b0[1] ? reg_f : rm;
                        s2_valid = 1'b1;
                        s2_idx = b0[1] ? rm : reg_f;
                    end
                    s1_hi8 = !b0[0] && s1_idx[2];
                    s2_hi8 = !b0[0] && s2_idx[2];
                    if (!b0[0]) begin
                        s1_idx = {1'b0, s1_idx[1:0]};
                        s2_idx = {1'b0, s2_idx[1:0]};
                    end
                    d_valid = b0[5:3] != 3'd7;
                    d_idx = s1_idx;
                    d_hi8 = s1_hi8;
                end
                8'b0100????: begin
                    // INC, DEC r16/32
                    op = b0[3] ? 5'd11 : 5'd10;  // ALU_DEC, ALU_INC
                    rd_cf = 1'b1;  // CF is kept
                    wr_oszap = 1'b1;
                    s1_valid = 1'b1;
                    s1_idx = b0[2:0];
                    d_valid = 1'b1;
                    d_idx = b0[2:0];
                end
                8'b0111????: begin
                    // Jcc rel8
                    cls = C_BRANCH;
                    op = 5'd0;  // BR_JCC
                    cond = b0[3:0];
                    rd_cf = 1'b1;
                    rd_oszap = 1'b1;
                    branch = 1'b1;
                    imm = rel8;
                end
                8'b100000??: begin
                    // Group 1, r/m, imm: 80h, 81h, 82h (80h's alias), 83h (sign-extended imm8)
                    known = reg_form;
                    op = {2'b00, reg_f};
                    size = b0[0] ? osz : 2'd0;
                    rd_cf = reg_f == 3'd2 || reg_f == 3'd3;
                    wr_cf = 1'b1;
                    wr_oszap = 1'b1;
                    s1_valid = 1'b1;
                    s1_idx = b0[0] ? rm : {1'b0, rm[1:0]};
                    s1_hi8 = !b0[0] && rm[2];
                    d_valid = reg_f != 3'd7;
                    d_idx = s1_idx;
                    d_hi8 = s1_hi8;
                    imm = b0[1:0] == 2'b01 ? immz_m : b0[1:0] == 2'b11 ? imm8s_m : imm8_m;
                end
                8'b1000010?: begin
                    // TEST r/m, r
                    known = reg_form;
                    op = 5'd8;  // ALU_TEST
                    size = b0[0] ? osz : 2'd0;
                    wr_cf = 1'b1;
                    wr_oszap = 1'b1;
                    s1_valid = 1'b1;
                    s1_idx = b0[0] ? rm : {1'b0, rm[1:0]};
                    s1_hi8 = !b0[0] && rm[2];
                    s2_valid = 1'b1;
                    s2_idx = b0[0] ? reg_f : {1'b0, reg_f[1:0]};
                    s2_hi8 = !b0[0] && reg_f[2];
                end
                8'b100010??: begin
                    // MOV r/m, r and MOV r, r/m
                    known = reg_form;
                    op = 5'd9;  // ALU_MOV
                    size = b0[0] ? osz : 2'd0;
                    s1_valid = 1'b1;
                    s1_idx = b0[1] ? reg_f : rm;
                    s2_valid = 1'b1;
                    s2_idx = b0[1] ? rm : reg_f;
                    s1_hi8 = !b0[0] && s1_idx[2];
                    s2_hi8 = !b0[0] && s2_idx[2];
                    if (!b0[0]) begin
                        s1_idx = {1'b0, s1_idx[1:0]};
                        s2_idx = {1'b0, s2_idx[1:0]};
                    end
                    d_valid = 1'b1;
                    d_idx = s1_idx;
                    d_hi8 = s1_hi8;
                end
                8'h8c: begin
                    // MOV r/m, Sreg: the selector, zero-extended to a 32-bit register
                    known = reg_form;
                    undefined = reg_f > 3'd5;
                    op = 5'd15;  // ALU_MOVZX
                    src_size = 2'd1;
                    s1_valid = 1'b1;
                    s1_idx = rm;
                    d_valid = 1'b1;
                    d_idx = rm;
                    reads_seg = 1'b1;
                    seg_index = reg_f;
                end
                8'h8d: begin
                    // LEA: the offset alone
                    undefined = reg_form;
                    op = 5'd14;  // ALU_LEA
                    lea = !reg_form;
                    s1_valid = 1'b1;
                    s1_idx = reg_f;
                    d_valid = 1'b1;
                    d_idx = reg_f;
                end
                8'h8e: begin
                    // MOV Sreg, r/m16 (real mode): not CS, not 6 or 7
                    known = reg_form;
                    undefined = reg_f == 3'd1 || reg_f > 3'd5;
                    cls = C_SEGMENT;
                    op = 5'd0;  // SEGOP_LOAD_REAL
                    size = 2'd1;
                    s2_valid = 1'b1;
                    s2_idx = rm;
                    d_valid = 1'b1;
                    d_seg = 1'b1;
                    d_idx = reg_f;
                end
                8'h90: begin
                    cls = C_SPECIAL;
                    op = 5'd0;  // SPECIAL_NOP
                end
                8'h98: begin
                    // CBW, CWDE
                    op = 5'd16;  // ALU_MOVSX
                    src_size = opsize32 ? 2'd1 : 2'd0;
                    s1_valid = 1'b1;
                    s2_valid = 1'b1;
                    d_valid = 1'b1;
                end
                8'h99: begin
                    // CWD, CDQ
                    op = 5'd31;  // ALU_SIGN
                    s1_valid = 1'b1;
                    s1_idx = 3'd2;
                    s2_valid = 1'b1;
                    d_valid = 1'b1;
                    d_idx = 3'd2;
                end
                8'h9e: begin
                    // SAHF
                    op = 5'd30;  // ALU_FLAGS
                    imm = 32'd7;  // FLAG_SAHF
                    size = 2'd0;
                    s2_valid = 1'b1;
                    s2_hi8 = 1'b1;
                    wr_cf = 1'b1;
                    wr_oszap = 1'b1;
                end
                8'h9f: begin
                    // LAHF
                    op = 5'd30;  // ALU_FLAGS
                    imm = 32'd8;  // FLAG_LAHF
                    size = 2'd0;
                    rd_cf = 1'b1;
                    rd_oszap = 1'b1;
                    s1_valid = 1'b1;
                    s1_hi8 = 1'b1;
                    d_valid = 1'b1;
                    d_hi8 = 1'b1;
                end
                8'b1010100?: begin
                    // TEST AL/eAX, imm
                    op = 5'd8;  // ALU_TEST
                    size = b0[0] ? osz : 2'd0;
                    wr_cf = 1'b1;
                    wr_oszap = 1'b1;
                    s1_valid = 1'b1;
                    imm = b0[0] ? (opsize32 ? {by[4], by[3], by[2], by[1]} : {16'b0, by[2], by[1]}) : imm8_o;
                end
                8'b1011????: begin
                    // MOV r, imm
                    op = 5'd9;  // ALU_MOV
                    size = b0[3] ? osz : 2'd0;
                    s1_valid = 1'b1;
                    s1_idx = b0[3] ? b0[2:0] : {1'b0, b0[1:0]};
                    s1_hi8 = !b0[3] && b0[2];
                    d_valid = 1'b1;
                    d_idx = s1_idx;
                    d_hi8 = s1_hi8;
                    imm = b0[3] ? (opsize32 ? {by[4], by[3], by[2], by[1]} : {16'b0, by[2], by[1]}) : imm8_o;
                end
                8'b1100000?, 8'b110100??: begin
                    // Group 2: C0h, C1h by imm8; D0h, D1h by 1; D2h, D3h by CL
                    known = reg_form;
                    cls = C_SHIFT;
                    op = {2'b00, reg_f};
                    size = b0[0] ? osz : 2'd0;
                    rd_cf = 1'b1;
                    wr_cf = 1'b1;
                    wr_oszap = 1'b1;
                    s1_valid = 1'b1;
                    s1_idx = b0[0] ? rm : {1'b0, rm[1:0]};
                    s1_hi8 = !b0[0] && rm[2];
                    d_valid = 1'b1;
                    d_idx = s1_idx;
                    d_hi8 = s1_hi8;
                    count_cl = b0[4] && b0[1];
                    imm = !b0[4] ? imm8_m : 32'd1;
                end
                8'b1100011?: begin
                    // MOV r/m, imm (reg 0 only)
                    known = reg_form;
                    undefined = reg_f != 3'd0;
                    op = 5'd9;  // ALU_MOV
                    size = b0[0] ? osz : 2'd0;
                    s1_valid = 1'b1;
                    s1_idx = b0[0] ? rm : {1'b0, rm[1:0]};
                    s1_hi8 = !b0[0] && rm[2];
                    d_valid = 1'b1;
                    d_idx = s1_idx;
                    d_hi8 = s1_hi8;
                    imm = b0[0] ? immz_m : imm8_m;
                end
                8'b111000??: begin
                    // LOOPNE, LOOPE, LOOP, JCXZ: CX or ECX by address size
                    cls = C_BRANCH;
                    op = b0[1:0] == 2'd0 ? 5'd8 : b0[1:0] == 2'd1 ? 5'd7 : b0[1:0] == 2'd2 ? 5'd6 : 5'd9;
                    size = addr32 ? 2'd2 : 2'd1;
                    rd_oszap = !b0[1];
                    branch = 1'b1;
                    s1_valid = 1'b1;
                    s1_idx = 3'd1;
                    d_valid = b0[1:0] != 2'd3;
                    d_idx = 3'd1;
                    imm = rel8;
                end
                8'he9, 8'heb: begin
                    // JMP rel16/32, rel8
                    cls = C_BRANCH;
                    op = 5'd1;  // BR_JMP
                    branch = 1'b1;
                    imm = b0[1] ? rel8 : relz1;
                end
                8'hf4: begin
                    cls = C_SPECIAL;
                    op = 5'd1;  // SPECIAL_HLT
                end
                8'hf5, 8'b1111100?, 8'b1111101?, 8'b1111110?: begin
                    // CMC, CLC, STC, CLI, STI, CLD, STD
                    op = 5'd30;  // ALU_FLAGS
                    case (b0[3:0])
                        4'h8: imm = 32'd0;  // FLAG_CLC
                        4'h9: imm = 32'd1;  // FLAG_STC
                        4'ha: imm = 32'd3;  // FLAG_CLI
                        4'hb: imm = 32'd4;  // FLAG_STI
                        4'hc: imm = 32'd5;  // FLAG_CLD
                        4'hd: imm = 32'd6;  // FLAG_STD
                        default: imm = 32'd2;  // FLAG_CMC
                    endcase
                    rd_cf = b0 == 8'hf5;
                    wr_cf = b0 == 8'hf5 || b0[3:1] == 3'b100;
                end
                8'b1111011?: begin
                    // Group 3: TEST r/m, imm (/0, /1); NOT; NEG. MUL, IMUL, DIV, IDIV later.
                    known = reg_form && !reg_f[2];
                    op = reg_f[1] ? (reg_f[0] ? 5'd12 : 5'd13) : 5'd8;  // NEG, NOT, TEST
                    size = b0[0] ? osz : 2'd0;
                    wr_cf = reg_f != 3'd2;
                    wr_oszap = reg_f != 3'd2;
                    s1_valid = 1'b1;
                    s1_idx = b0[0] ? rm : {1'b0, rm[1:0]};
                    s1_hi8 = !b0[0] && rm[2];
                    d_valid = reg_f[1];
                    d_idx = s1_idx;
                    d_hi8 = s1_hi8;
                    imm = b0[0] ? immz_m : imm8_m;
                end
                8'b1111111?: begin
                    // FEh /0 /1: INC, DEC r/m8; FFh /0 /1: INC, DEC r/m16/32
                    known = reg_form && reg_f[2:1] == 2'b00;
                    op = reg_f[0] ? 5'd11 : 5'd10;
                    size = b0[0] ? osz : 2'd0;
                    rd_cf = 1'b1;
                    wr_oszap = 1'b1;
                    s1_valid = 1'b1;
                    s1_idx = b0[0] ? rm : {1'b0, rm[1:0]};
                    s1_hi8 = !b0[0] && rm[2];
                    d_valid = 1'b1;
                    d_idx = s1_idx;
                    d_hi8 = s1_hi8;
                end
                default: known = 1'b0;
            endcase
        end else begin
            casez (op2)
                8'b1000????: begin
                    // Jcc rel16/32
                    cls = C_BRANCH;
                    op = 5'd0;  // BR_JCC
                    cond = op2[3:0];
                    rd_cf = 1'b1;
                    rd_oszap = 1'b1;
                    branch = 1'b1;
                    imm = relz2;
                end
                8'b1001????: begin
                    // SETcc r/m8
                    known = reg_form;
                    op = 5'd17;  // ALU_SETCC
                    cond = op2[3:0];
                    size = 2'd0;
                    rd_cf = 1'b1;
                    rd_oszap = 1'b1;
                    s1_valid = 1'b1;
                    s1_idx = {1'b0, rm[1:0]};
                    s1_hi8 = rm[2];
                    d_valid = 1'b1;
                    d_idx = s1_idx;
                    d_hi8 = s1_hi8;
                end
                8'b1010?10?: begin
                    // SHLD, SHRD r/m, r by imm8 (A4h, ACh) or by CL (A5h, ADh)
                    known = reg_form;
                    cls = C_SHIFT;
                    op = op2[3] ? 5'd9 : 5'd8;  // SHIFT_SHRD, SHIFT_SHLD
                    wr_cf = 1'b1;
                    wr_oszap = 1'b1;
                    s1_valid = 1'b1;
                    s1_idx = rm;
                    s2_valid = 1'b1;
                    s2_idx = reg_f;
                    d_valid = 1'b1;
                    d_idx = rm;
                    count_cl = op2[0];
                    imm = imm8_m2;
                end
                8'b1011?11?: begin
                    // MOVZX (B6h, B7h), MOVSX (BEh, BFh)
                    known = reg_form;
                    op = op2[3] ? 5'd16 : 5'd15;
                    src_size = op2[0] ? 2'd1 : 2'd0;
                    s1_valid = 1'b1;
                    s1_idx = reg_f;
                    s2_valid = 1'b1;
                    s2_idx = op2[0] ? rm : {1'b0, rm[1:0]};
                    s2_hi8 = !op2[0] && rm[2];
                    d_valid = 1'b1;
                    d_idx = reg_f;
                end
                default: known = 1'b0;
            endcase
        end
    end

    // ---- LEA's displacement -----------------------------------------------------
    logic        sib_form, disp32_no_base;
    logic [2:0]  dpos;   // the displacement's first byte
    logic [31:0] d8, d16, d32;
    assign sib_form = addr32 && rm == 3'd4;
    assign disp32_no_base = mode == 2'b00 && (sib_form ? by[2][2:0] == 3'd5 : rm == 3'd5);
    assign dpos = sib_form ? 3'd3 : 3'd2;
    assign d8 = {{24{by[dpos][7]}}, by[dpos]};
    assign d16 = {16'b0, by[3], by[2]};
    assign d32 = {by[dpos + 3'd3], by[dpos + 3'd2], by[dpos + 3'd1], by[dpos]};
    always_comb begin
        if (!addr32) begin
            lea_disp = mode == 2'b01 ? {16'b0, d8[15:0]} : mode == 2'b10 || (mode == 2'b00 && rm == 3'd6) ? d16 : 32'd0;
        end else begin
            lea_disp = mode == 2'b01 ? d8 : mode == 2'b10 || disp32_no_base ? d32 : 32'd0;
        end
    end
    assign lea_early = osz == 2'd2;

    // ---- Faults and the uop ---------------------------------------------------
    // LOCK is invalid on every instruction here; it is checked before the
    // 15-byte limit (Phase 0 divergence lock-ud-before-length-gp).
    logic [2:0] fault;
    assign fault = !known ? 3'd4                                      // FAULT_UNIMPLEMENTED
                 : undefined || q.prefixes.lock ? 3'd1                // FAULT_UD
                 : q.too_long ? 3'd2                                  // FAULT_GP0
                 : wrap16 ? 3'd4 : 3'd0;
    assign stop = fault != 3'd0 || (cls == C_SPECIAL && op == 5'd1);

    uop_t u;
    assign u.cls = fes_i386_pkg::uop_class_e'(cls);
    assign u.op = op;
    assign u.src1.valid = s1_valid;
    assign u.src1.space = fes_i386_pkg::RS_GPR;
    assign u.src1.index = {1'b0, s1_idx};
    assign u.src1.hi8 = s1_hi8;
    assign u.src2.valid = s2_valid;
    assign u.src2.space = fes_i386_pkg::RS_GPR;
    assign u.src2.index = {1'b0, s2_idx};
    assign u.src2.hi8 = s2_hi8;
    assign u.dst.valid = d_valid && fault == 3'd0;
    assign u.dst.space = d_seg ? fes_i386_pkg::RS_SEG : fes_i386_pkg::RS_GPR;
    assign u.dst.index = {1'b0, d_idx};
    assign u.dst.hi8 = d_hi8;
    assign u.size = fes_i386_pkg::size_e'(size);
    assign u.src_size = fes_i386_pkg::size_e'(src_size);
    assign u.count_cl = count_cl;
    assign u.imm = imm;
    assign u.mem.kind = fes_i386_pkg::MEM_NONE;
    assign u.mem.bytes = 4'd0;
    assign u.mem.seg = fes_i386_pkg::SEG_DS;
    assign u.mem.base = '0;
    assign u.mem.index = '0;
    assign u.mem.scale = 2'd0;
    assign u.mem.disp = lea_disp;
    assign u.mem.addr32 = addr32;
    assign u.mem.lock = q.prefixes.lock;
    assign u.mem.system = 1'b0;
    assign u.flags.read.cf = rd_cf;
    assign u.flags.read.oszap = rd_oszap;
    assign u.flags.write.cf = wr_cf;
    assign u.flags.write.oszap = wr_oszap;
    assign u.ctl.cond = cond;
    assign u.ctl.predict_taken = 1'b0;
    assign u.ctl.end_of_instruction = 1'b1;
    assign u.ctl.ilen = q.length;
    assign u.ctl.fault = fes_i386_pkg::fault_e'(fault);
    assign uop = u;
endmodule
