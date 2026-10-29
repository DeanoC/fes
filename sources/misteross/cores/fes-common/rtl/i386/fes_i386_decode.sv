// SPDX-License-Identifier: MIT
// DC: the x86 instruction Q aligned (fes_i386_pkg::fetched_t) to one uop
// (docs/i386-cpu-design.md, "Decode and microcode"). Phase 1 decodes the
// one-uop forms:
//   ALU group 1, INC, DEC, NEG, NOT, TEST, MOV (including to and from a
//   segment register in real mode), LEA, shifts and rotates, SHLD and SHRD,
//   SETcc, MOVZX and MOVSX, CBW/CWDE, CWD/CDQ, SAHF, LAHF, the flag set and
//   clear instructions, NOP, HLT, and the near JMP, Jcc, LOOP, LOOPE, LOOPNE
//   and JCXZ; PUSH and POP of a register, an immediate or a segment register,
//   RET and RET imm16, LEAVE, JMP r/m and CALL rel; XCHG, MOV to and from
//   a direct offset, XLAT and SALC; PUSH, POP and CALL of r/m; MUL, IMUL
//   (all three forms), DIV, IDIV, AAM and AAD; IN and OUT; BT, BTS, BTR and
//   BTC by an immediate, BSF and BSR; DAA, DAS, AAA and AAS; the first uops
//   of MOVS, CMPS, STOS, LODS, SCAS, INS, OUTS and ENTER, of BT, BTS, BTR and
//   BTC by a register with a memory operand (fes_i386_seq builds the rest),
//   and of the microcode routines (PUSHA, POPA, INT, INTO, IRET and BOUND);
//   CLTS and WAIT;
// with a ModRM memory operand where they take one. The memory operand is a
// load (src2, or src1 for CMP and TEST), a store, or both in one uop
// (read-modify-write), and replaces the register it stands for: no GPR is
// read or written for it. Its segment is the override, else SS for a BP-,
// EBP- or ESP-based address (stack, fes_i386_amode), else DS. A stack
// operation's memory operand is at SS:ESP (or SS:EBP for LEAVE), which D
// sets up; it also writes ESP (stk says how). CALL rel is two uops: the
// push of the next EIP, then JMP rel, which D builds from the push's
// otherwise unused imm (the displacement).
// WAIT raises #NM when CR0.MP and CR0.TS are both set (wait_nm), after
// every check above.
// Encodings the i386 rejects raise #UD (LOCK on anything but a read-modify-
// write of ADD, OR, ADC, SBB, AND, SUB, XOR, NOT, NEG, INC, DEC, BTS, BTR or
// BTC, MOV to CS or to segment register 6 or 7, LEA of a register, C6h/C7h
// with reg != 0);
// an instruction over 15 bytes raises #GP(0). Everything else decodes as
// FAULT_UNIMPLEMENTED. In 16-bit code an instruction
// whose next EIP passes FFFFh is FAULT_UNIMPLEMENTED too, but D applies
// that, since only D knows the instruction's EIP (fes_i386_cpu, wrap16).
//
// src1 is always the destination's register: a byte or word result merges
// into its old value. A LOOP or JCXZ counts in CX or ECX by address size, which
// is the uop's size; its target is truncated by operand size (opsize32).
module fes_i386_decode (
    input  fes_i386_pkg::fetched_t q,
    input  logic                   code32,      // CS.D
    input  logic                   stack,       // the address is BP-, EBP- or ESP-based (fes_i386_amode)
    input  logic                   wait_nm,     // CR0.MP and CR0.TS are both set: WAIT raises #NM
    output fes_i386_pkg::uop_t     uop,
    output logic                   opsize32,
    output logic                   lea,         // address operands go to AG
    output logic                   lea_early,   // ... and the offset is the whole result (32-bit LEA)
    output logic [31:0]            lea_disp,    // zero-extended from 16 bits when !addr32
    output logic                   branch,      // AG computes the target from EIP + length + imm
    output logic                   consumes,    // the result depends on the old flags
    output logic                   reads_seg,   // b is the selector of segment seg_index
    output logic [2:0]             seg_index,
    output logic                   stop,        // nothing after this uop can retire: HLT or a fault
    output logic [2:0]             stk,         // a stack operation (fes_i386_pkg STK_*)
    output logic                   push_eip,    // src2 is the next instruction's EIP
    output logic                   cs_base,     // src1 is CS's base (EX adds the target)
    output logic [4:0]             seq,         // a fixed uop sequence (fes_i386_pkg SEQ_*, fes_i386_seq)
    output logic                   sel_part,    // the uop reads a far pointer's selector
    output logic                   far_jmp,     // a far jump by far_sel:imm
    output logic                   far_chk,     // ... whose offset (imm) D checks against CS's limit
    output logic [15:0]            far_sel,
    output logic [5:0]             wseg,        // the segment registers the uop writes, one-hot
    output logic [7:0]             upc,         // SEQ_UCODE: the ROM address of the routine's second uop
    output logic                   direct,      // the memory operand has no base or index: a moffs, the vector's entry
    output logic                   xlat,        // ... is XLAT's [BX + AL]
    output logic [2:0]             m_seg,       // the ModRM operand's segment, apart from a stack uop's SS
    output logic [3:0]             slot,        // STK_PEEK: the stack slot, -8 to 7, from ESP
    output logic [2:0]             str_op,      // SEQ_STRING: the instruction (fes_i386_pkg str_e)
    output logic [1:0]             str_rep,     // ... its REP prefix (rep_e)
    output logic [4:0]             ecnt         // SEQ_ENTER: the nesting level, mod 32
);
    typedef fes_i386_pkg::uop_t uop_t;

    logic [10:0][7:0] by;
    logic [14:0][7:0] byx;  // by, then zeros, so an immediate's select stays in range
    logic [7:0]       b0, op2, modrm;
    logic [1:0]       mode;
    logic [2:0]       reg_f, rm;
    logic             two, addr32, reg_form;
    logic [1:0]       osz;  // a size_e: SIZE_16 or SIZE_32

    assign by = q.bytes;
    assign byx = {32'b0, by};
    assign b0 = by[0];
    assign two = b0 == 8'h0f;
    assign op2 = by[1];
    assign modrm = two ? by[2] : by[1];
    assign mode = modrm[7:6];
    assign reg_f = modrm[5:3];
    assign rm = modrm[2:0];
    assign reg_form = mode == 2'b11;
    logic osz32;  // the operand size as the prefixes set it
    assign osz32 = code32 ^ q.prefixes.opsize;
    // INT3, INT n and INTO push 16-bit slots in real mode, whatever the
    // operand size: D's stack steps follow opsize32. CLTS's jump to the
    // next instruction is not cut to 16 bits, so one that ends at FFFFh
    // leaves EIP 10000h as any other instruction does.
    assign opsize32 = osz32 && !(!two && b0 >= 8'hcc && b0 <= 8'hce) || two && op2 == 8'h06;
    assign addr32 = code32 ^ q.prefixes.adsize;
    assign osz = osz32 ? 2'd2 : 2'd1;

    // A flag consumer: ADC, SBB, Jcc, SETcc, RCL, RCR, LAHF, CMC, LOOPE,
    // LOOPNE, PUSHF, INTO, SALC, DAA, DAS, AAA, AAS. Straight from the opcode and ModRM bytes, not through the
    // decode below, because D's issue waits on it (fes_i386_cpu); a superset
    // here would hold instructions needlessly, which the lockstep's gap check
    // catches. by[1][5:4] == 01 is ModRM reg 2 or 3.
    assign consumes = (b0[7:6] == 2'b00 && b0[2:1] != 2'b11 && b0[5:4] == 2'b01)  // ADC, SBB r/m and acc
                   || (b0[7:2] == 6'b100000 && by[1][5:4] == 2'b01)               // ADC, SBB r/m, imm
                   || ((b0[7:1] == 7'b1100000 || b0[7:2] == 6'b110100) && by[1][5:4] == 2'b01)  // RCL, RCR
                   || b0[7:4] == 4'h7                                             // Jcc rel8
                   || (two && op2[7:5] == 3'b100)                                 // Jcc rel16/32, SETcc
                   || b0 == 8'h9f || b0 == 8'hf5 || b0[7:1] == 7'b1110000         // LAHF, CMC, LOOPNE, LOOPE
                   || b0 == 8'h9c || b0 == 8'hce || b0 == 8'hd6                   // PUSHF, INTO, SALC
                   || (b0[7:5] == 3'b001 && b0[2:0] == 3'b111);                   // DAA, DAS, AAA, AAS

    // Where the displacement is: after the ModRM and, in 32-bit addressing
    // with r/m 100b, a SIB byte.
    logic       sib_form;
    logic [2:0] mpos, dpos, dlen;  // ModRM and displacement positions, displacement bytes
    assign sib_form = addr32 && !reg_form && rm == 3'd4;
    assign mpos = two ? 3'd2 : 3'd1;
    assign dpos = mpos + 3'd1 + 3'(sib_form);
    always_comb begin
        if (reg_form) dlen = 3'd0;
        else if (mode == 2'b01) dlen = 3'd1;
        else if (!addr32) dlen = mode == 2'b10 || rm == 3'd6 ? 3'd2 : 3'd0;
        else dlen = mode == 2'b10 || (sib_form ? by[dpos - 3'd1][2:0] == 3'd5 : rm == 3'd5) ? 3'd4 : 3'd0;
    end

    // Immediates. One after a ModRM's operand bytes is the instruction's last
    // bytes, so its place comes from the length, beside the decode; one
    // straight after the opcode is at byte 1.
    logic [3:0]  blen;  // the instruction's bytes after its prefixes
    logic [31:0] imm8_m, imm8s_m, immz_m, imm8_o, rel8, relz1, relz2;
    assign blen = q.length - q.prefix_count;
    assign imm8_m = {24'b0, byx[blen - 4'd1]};
    assign imm8s_m = {{24{byx[blen - 4'd1][7]}}, byx[blen - 4'd1]};
    assign immz_m = osz32 ? {byx[blen - 4'd1], byx[blen - 4'd2], byx[blen - 4'd3], byx[blen - 4'd4]}
                             : {16'b0, byx[blen - 4'd1], byx[blen - 4'd2]};
    assign imm8_o = {24'b0, by[1]};
    // Branch displacements, as AG's disp: 32 bits, or zero-extended from 16
    // when the operand size is 16 (the target wraps at 64 KiB).
    assign rel8 = osz32 ? {{24{by[1][7]}}, by[1]} : {16'b0, {8{by[1][7]}}, by[1]};
    assign relz1 = osz32 ? {by[4], by[3], by[2], by[1]} : {16'b0, by[2], by[1]};
    assign relz2 = osz32 ? {by[5], by[4], by[3], by[2]} : {16'b0, by[3], by[2]};

    // ---- The opcode table -------------------------------------------------------
    logic [2:0]  cls;
    logic [4:0]  op;
    logic [1:0]  size, src_size;
    logic        count_cl;
    logic        s1_valid, s1_hi8, s2_valid, s2_hi8, d_valid, d_seg, d_temp, d_hi8;
    logic        seg2, seg2_t;
    logic [2:0]  seg2_idx;  // a far pointer's segment register, or CS
    logic [2:0]  s1_idx, s2_idx, d_idx;
    logic [31:0] imm;
    logic [3:0]  cond;
    logic        rd_cf, rd_oszap, wr_cf, wr_oszap;
    logic        known, undefined;
    logic        nm;        // WAIT's #NM
    logic        lptr;      // LES and its kin (SEQ_LPTR)
    logic        fptr;      // JMP and CALL m16:16/32 (SEQ_JMPFM, SEQ_CALLFM)
    logic        d_t1;      // the destination is T1, not T0
    logic        probe;     // probe_e: the uop probes INT's stack slots
    logic        ivt;       // the memory operand is the interrupt vector's entry
    logic        moffs;     // ... a direct offset, the bytes after the opcode (A0h-A3h)
    logic        lock_any;  // LOCK is valid whatever the first uop (XCHG with memory)
    logic        eoi_taken; // ends the instruction only when taken (INTO's JNO)
    logic [7:0]  vec;       // INT3, INT n, INTO: the vector
    logic        eoi;       // the uop ends the instruction
    logic [1:0]  mkind;     // a mem_kind_e
    logic        m_in_a;    // the memory operand stands for src1 (and the destination), else src2
    logic [2:0]  mbytes;    // the access's bytes, when not the operation's size
    logic        lockable;  // LOCK is valid on the memory form
    logic        strop;     // a string instruction: no ModRM, DS:ESI and ES:EDI (SEQ_STRING)
    logic        enter;     // ENTER: src1 is ESP, read for the string engine's copies
    logic [1:0]  io;        // an I/O access at WB (fes_i386_pkg io_e)

    localparam logic [2:0] C_ALU = 3'd0, C_SHIFT = 3'd1, C_MUL = 3'd2, C_DIV = 3'd3, C_BRANCH = 3'd4, C_SEGMENT = 3'd5,
                           C_SYSTEM = 3'd6, C_SPECIAL = 3'd7;

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
        d_temp = 1'b0;
        seg2 = 1'b0;
        seg2_t = 1'b1;
        seg2_idx = 3'd0;
        sel_part = 1'b0;
        far_jmp = 1'b0;
        far_chk = 1'b0;
        lptr = 1'b0;
        fptr = 1'b0;
        d_t1 = 1'b0;
        probe = fes_i386_pkg::PROBE_NONE;
        upc = 8'd0;
        ivt = 1'b0;
        moffs = 1'b0;
        xlat = 1'b0;
        lock_any = 1'b0;
        slot = 4'd0;
        eoi_taken = 1'b0;
        vec = 8'd0;
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
        nm = 1'b0;
        mkind = 2'd0;  // MEM_NONE
        m_in_a = 1'b0;
        mbytes = 3'd0;  // by size
        lockable = 1'b0;
        strop = 1'b0;
        enter = 1'b0;
        io = 2'd0;
        str_op = 3'd0;
        str_rep = 2'd0;
        ecnt = 5'd0;
        stk = fes_i386_pkg::STK_NONE;
        push_eip = 1'b0;
        cs_base = 1'b0;
        seq = fes_i386_pkg::SEQ_NONE;
        eoi = 1'b1;

        if (!two) begin
            casez (b0)
                8'b01010???: begin
                    // PUSH r16/32
                    op = 5'd9;  // ALU_MOV
                    s2_valid = 1'b1;
                    s2_idx = b0[2:0];
                    m_in_a = 1'b1;
                    mkind = 2'd2;
                    stk = fes_i386_pkg::STK_PUSH;
                end
                8'b01011???: begin
                    // POP r16/32
                    op = 5'd9;
                    s1_valid = 1'b1;
                    s1_idx = b0[2:0];
                    d_valid = 1'b1;
                    d_idx = b0[2:0];
                    mkind = 2'd1;
                    stk = fes_i386_pkg::STK_POP;
                end
                8'b011010?0: begin
                    // PUSH imm16/32 (68h), imm8 sign-extended (6Ah)
                    op = 5'd9;
                    imm = b0[1] ? {{24{by[1][7]}}, by[1]} : osz32 ? {by[4], by[3], by[2], by[1]} : {16'b0, by[2], by[1]};
                    m_in_a = 1'b1;
                    mkind = 2'd2;
                    stk = fes_i386_pkg::STK_PUSH;
                end
                8'b011010?1: begin
                    // IMUL r, r/m, imm16/32 (69h), imm8 sign-extended (6Bh): the
                    // register is src1 and the destination, r/m the multiplicand
                    // (src2) and imm the multiplier
                    cls = C_MUL;
                    op = 5'd6;  // MULDIV_IMUL3
                    mkind = reg_form ? 2'd0 : 2'd1;
                    wr_cf = 1'b1;
                    wr_oszap = 1'b1;
                    s1_valid = 1'b1;
                    s1_idx = reg_f;
                    s2_valid = 1'b1;
                    s2_idx = rm;
                    d_valid = 1'b1;
                    d_idx = reg_f;
                    imm = b0[1] ? imm8s_m : immz_m;
                end
                8'b1101010?: begin
                    // AAM (D4h): AH and AL from AL / imm8 and AL % imm8; AAD (D5h):
                    // AL from AL + AH * imm8, AH 0. EAX is src1 and the destination.
                    cls = b0[0] ? C_MUL : C_DIV;
                    op = b0[0] ? 5'd3 : 5'd2;  // MULDIV_AAD, MULDIV_AAM
                    size = 2'd0;
                    wr_cf = 1'b1;
                    wr_oszap = 1'b1;
                    s1_valid = 1'b1;
                    d_valid = 1'b1;
                    imm = imm8_o;
                end
                8'b1100001?: begin
                    // RET imm16 (C2h), RET (C3h): EX adds CS's base to the popped EIP.
                    cls = C_BRANCH;
                    op = 5'd10;  // BR_RET
                    imm = {16'b0, by[2], by[1]};
                    mkind = 2'd1;
                    cs_base = 1'b1;
                    stk = b0[0] ? fes_i386_pkg::STK_POP : fes_i386_pkg::STK_RET_IMM;
                end
                8'hc9: begin
                    // LEAVE: ESP from EBP, then POP EBP
                    op = 5'd9;
                    s1_valid = 1'b1;
                    s1_idx = 3'd5;
                    d_valid = 1'b1;
                    d_idx = 3'd5;
                    mkind = 2'd1;
                    stk = fes_i386_pkg::STK_LEAVE;
                end
                // Group 1, r/m and accumulator forms: ADD OR ADC SBB AND SUB XOR CMP.
                // Columns 6 and 7 hold segment PUSH and POP and the BCD adjusts.
                8'b00??????: if (b0[2:1] == 2'b11 && !b0[5]) begin
                    // Columns 6 and 7 of rows 0-1: PUSH and POP of ES, CS, SS
                    // and DS (POP CS, 0Fh, is the escape).
                    if (!b0[0]) begin
                        op = 5'd15;  // ALU_MOVZX of the selector
                        src_size = 2'd1;
                        reads_seg = 1'b1;
                        seg_index = {1'b0, b0[4:3]};
                        m_in_a = 1'b1;
                        mkind = 2'd2;
                        mbytes = 3'd2;   // a word, whatever the operand size
                        stk = fes_i386_pkg::STK_PUSH;
                    end else begin
                        cls = C_SEGMENT;
                        op = 5'd0;  // SEGOP_LOAD_REAL
                        size = 2'd1;
                        mkind = 2'd1;
                        mbytes = 3'd2;   // the selector's word; ESP still steps by 4
                        d_valid = 1'b1;
                        d_seg = 1'b1;
                        d_idx = {1'b0, b0[4:3]};
                        stk = fes_i386_pkg::STK_POP;
                    end
                end else if (b0[2:0] == 3'b111) begin
                    // Column 7 of rows 2-3: DAA (27h), DAS (2Fh), AAA (37h)
                    // and AAS (3Fh) adjust AL by AF and, for DAA and DAS, CF;
                    // AAA and AAS carry into AH. EAX is src1 and the
                    // destination.
                    case (b0[4:3])
                        2'd0: op = 5'd25;  // ALU_DAA
                        2'd1: op = 5'd26;  // ALU_DAS
                        2'd2: op = 5'd27;  // ALU_AAA
                        default: op = 5'd28;  // ALU_AAS
                    endcase
                    size = b0[4] ? 2'd1 : 2'd0;
                    rd_cf = !b0[4];
                    rd_oszap = 1'b1;
                    wr_cf = 1'b1;
                    wr_oszap = 1'b1;
                    s1_valid = 1'b1;
                    d_valid = 1'b1;
                end else begin
                    known = b0[2:1] != 2'b11;
                    op = {2'b00, b0[5:3]};
                    size = b0[0] ? osz : 2'd0;
                    rd_cf = b0[5:3] == 3'd2 || b0[5:3] == 3'd3;
                    wr_cf = 1'b1;
                    wr_oszap = 1'b1;
                    if (b0[2:1] == 2'b10) begin
                        // AL/eAX, imm
                        s1_valid = 1'b1;
                        imm = b0[0] ? (osz32 ? {by[4], by[3], by[2], by[1]} : {16'b0, by[2], by[1]}) : imm8_o;
                    end else begin
                        s1_valid = 1'b1;
                        s1_idx = b0[1] ? reg_f : rm;
                        s2_valid = 1'b1;
                        s2_idx = b0[1] ? rm : reg_f;
                        if (!reg_form) begin
                            // r/m, r: CMP loads, the rest read-modify-write; r, r/m loads.
                            m_in_a = !b0[1];
                            mkind = b0[1] || b0[5:3] == 3'd7 ? 2'd1 : 2'd3;
                            lockable = !b0[1] && b0[5:3] != 3'd7;
                        end
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
                    op = {2'b00, reg_f};
                    m_in_a = 1'b1;
                    mkind = reg_form ? 2'd0 : reg_f == 3'd7 ? 2'd1 : 2'd3;
                    lockable = reg_f != 3'd7;
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
                    op = 5'd8;  // ALU_TEST
                    m_in_a = 1'b1;
                    mkind = reg_form ? 2'd0 : 2'd1;
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
                    // MOV r/m, r (a store) and MOV r, r/m (a load)
                    op = 5'd9;  // ALU_MOV
                    m_in_a = !b0[1];
                    mkind = reg_form ? 2'd0 : b0[1] ? 2'd1 : 2'd2;
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
                    // MOV r/m, Sreg: the selector, zero-extended to a 32-bit
                    // register, or stored as a word
                    undefined = reg_f > 3'd5;
                    m_in_a = 1'b1;
                    mkind = reg_form ? 2'd0 : 2'd2;
                    mbytes = 3'd2;
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
                    mkind = reg_form ? 2'd0 : 2'd1;
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
                8'h98: begin
                    // CBW, CWDE
                    op = 5'd16;  // ALU_MOVSX
                    src_size = osz32 ? 2'd1 : 2'd0;
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
                8'h9c: begin
                    // PUSHF, PUSHFD: FLAGS' image, VM and RF clear
                    op = 5'd30;  // ALU_FLAGS
                    imm = 32'd10;  // FLAG_READ
                    rd_cf = 1'b1;
                    rd_oszap = 1'b1;
                    m_in_a = 1'b1;
                    mkind = 2'd2;
                    stk = fes_i386_pkg::STK_PUSH;
                end
                8'h9d: begin
                    // POPF, POPFD
                    op = 5'd30;  // ALU_FLAGS
                    imm = 32'd9;  // FLAG_LOAD
                    wr_cf = 1'b1;
                    wr_oszap = 1'b1;
                    mkind = 2'd1;
                    stk = fes_i386_pkg::STK_POP;
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
                    imm = b0[0] ? (osz32 ? {by[4], by[3], by[2], by[1]} : {16'b0, by[2], by[1]}) : imm8_o;
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
                    imm = b0[3] ? (osz32 ? {by[4], by[3], by[2], by[1]} : {16'b0, by[2], by[1]}) : imm8_o;
                end
                8'b1100000?, 8'b110100??: begin
                    // Group 2: C0h, C1h by imm8; D0h, D1h by 1; D2h, D3h by CL
                    cls = C_SHIFT;
                    m_in_a = 1'b1;
                    mkind = reg_form ? 2'd0 : 2'd3;
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
                    undefined = reg_f != 3'd0;
                    m_in_a = 1'b1;
                    mkind = reg_form ? 2'd0 : 2'd2;
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
                8'he8, 8'he9, 8'heb: begin
                    // JMP rel16/32, rel8; CALL rel16/32, which pushes the
                    // next EIP, then JMP rel by imm (SEQ_CALL)
                    imm = b0[1] ? rel8 : relz1;
                    if (b0[0]) begin
                        cls = C_BRANCH;
                        op = 5'd1;  // BR_JMP
                        branch = 1'b1;
                    end else begin
                        op = 5'd9;
                        m_in_a = 1'b1;
                        mkind = 2'd2;
                        push_eip = 1'b1;
                        stk = fes_i386_pkg::STK_PUSH;
                        seq = fes_i386_pkg::SEQ_CALL;
                        eoi = 1'b0;
                    end
                end
                8'hea: begin
                    // JMP ptr16:16 or ptr16:32: CS from c (far_sel)
                    cls = C_BRANCH;
                    op = 5'd1;  // BR_JMP
                    imm = relz1;
                    far_jmp = 1'b1;
                    far_chk = 1'b1;
                    seg2 = 1'b1;
                    seg2_idx = 3'd1;
                    seg2_t = 1'b0;
                end
                8'b1000011?: begin
                    // XCHG r/m, r: with registers, three XORs that write no
                    // flags (SEQ_XCHG), or a MOV of the register to itself
                    // when it is one; with memory, the moves through T1
                    // (SEQ_XCHGM), which LOCK may prefix.
                    size = b0[0] ? osz : 2'd0;
                    if (reg_form) begin
                        op = reg_f == rm ? 5'd9 : 5'd6;  // ALU_MOV, ALU_XOR
                        s1_valid = 1'b1;
                        s1_idx = b0[0] ? rm : {1'b0, rm[1:0]};
                        s1_hi8 = !b0[0] && rm[2];
                        s2_valid = 1'b1;
                        s2_idx = b0[0] ? reg_f : {1'b0, reg_f[1:0]};
                        s2_hi8 = !b0[0] && reg_f[2];
                        d_valid = 1'b1;
                        d_idx = s1_idx;
                        d_hi8 = s1_hi8;
                        if (reg_f != rm) begin
                            seq = fes_i386_pkg::SEQ_XCHG;
                            eoi = 1'b0;
                        end
                    end else begin
                        op = 5'd9;
                        size = 2'd2;  // T1's load, unmerged
                        mbytes = b0[0] ? (osz32 ? 3'd4 : 3'd2) : 3'd1;
                        mkind = 2'd1;
                        s1_idx = b0[0] ? reg_f : {1'b0, reg_f[1:0]};  // carried, unread
                        s1_hi8 = !b0[0] && reg_f[2];
                        d_valid = 1'b1;
                        d_temp = 1'b1;
                        d_t1 = 1'b1;
                        lock_any = 1'b1;
                        seq = fes_i386_pkg::SEQ_XCHGM;
                        eoi = 1'b0;
                    end
                end
                8'b10010???: if (b0[2:0] != 3'd0) begin
                    // XCHG eAX, r (SEQ_XCHG); 90h is NOP
                    op = 5'd6;  // ALU_XOR
                    s1_valid = 1'b1;
                    s1_idx = b0[2:0];
                    s2_valid = 1'b1;
                    d_valid = 1'b1;
                    d_idx = b0[2:0];
                    seq = fes_i386_pkg::SEQ_XCHG;
                    eoi = 1'b0;
                end else begin
                    cls = C_SPECIAL;
                    op = 5'd0;  // SPECIAL_NOP
                end
                8'b101000??: begin
                    // MOV AL/eAX, moffs (A0h, A1h) and MOV moffs, AL/eAX (A2h,
                    // A3h): the offset follows the opcode, by address size
                    op = 5'd9;  // ALU_MOV
                    size = b0[0] ? osz : 2'd0;
                    moffs = 1'b1;
                    m_in_a = b0[1];
                    mkind = b0[1] ? 2'd2 : 2'd1;
                    s1_valid = 1'b1;
                    s2_valid = 1'b1;
                    d_valid = 1'b1;
                end
                8'b1010010?, 8'b1010011?, 8'b1010101?, 8'b1010110?, 8'b1010111?, 8'b011011??: begin
                    // MOVS (A4h, A5h), CMPS (A6h, A7h), STOS (AAh, ABh),
                    // LODS (ACh, ADh), SCAS (AEh, AFh), INS (6Ch, 6Dh), OUTS
                    // (6Eh, 6Fh) (SEQ_STRING). This uop starts the
                    // instruction, a NOP that reads ESI and EDI (where the
                    // instruction uses them) and, under REP, ECX, for D's
                    // copies; fes_i386_seq builds the elements from it, at
                    // its size, address size and source segment. REP, F2h or
                    // F3h, repeats; for CMPS and SCAS F3h is REPE and F2h
                    // REPNE. INS stores at ES:EDI as STOS does, OUTS loads
                    // seg:ESI as LODS does, each from or to port DX.
                    cls = C_SPECIAL;
                    op = 5'd0;  // SPECIAL_NOP
                    size = b0[0] ? osz : 2'd0;
                    strop = 1'b1;
                    str_op = b0[7:2] == 6'b011011 ? (b0[1] ? 3'(fes_i386_pkg::STR_OUTS) : 3'(fes_i386_pkg::STR_INS))
                           : b0[3:1] == 3'b010 ? 3'(fes_i386_pkg::STR_MOVS)
                           : b0[3:1] == 3'b011 ? 3'(fes_i386_pkg::STR_CMPS)
                           : b0[3:1] == 3'b101 ? 3'(fes_i386_pkg::STR_STOS)
                           : b0[3:1] == 3'b110 ? 3'(fes_i386_pkg::STR_LODS) : 3'(fes_i386_pkg::STR_SCAS);
                    str_rep = q.prefixes.rep;
                    // ESI: MOVS, CMPS, LODS, OUTS; EDI: all but LODS and OUTS.
                    s1_valid = b0[7:2] == 6'b011011 ? b0[1] : b0[3:1] != 3'b101 && b0[3:1] != 3'b111;
                    s1_idx = 3'd6;
                    s2_valid = b0[7:2] == 6'b011011 ? !b0[1] : b0[3:1] != 3'b110;
                    s2_idx = 3'd7;
                    count_cl = q.prefixes.rep != 2'd0;
                    seq = fes_i386_pkg::SEQ_STRING;
                    eoi = 1'b0;
                end
                8'b1110?1??: begin
                    // IN AL/eAX, imm8 (E4h, E5h) or DX (ECh, EDh); OUT imm8 or
                    // DX, AL/eAX (E6h, E7h, EEh, EFh): an I/O access at WB.
                    // IN merges the port's data into eAX at WB; OUT sends
                    // eAX. The port is DX (src2) or the imm8.
                    op = 5'd9;  // ALU_MOV
                    size = b0[0] ? osz : 2'd0;
                    io = b0[1] ? 2'(fes_i386_pkg::IO_OUT) : 2'(fes_i386_pkg::IO_IN);
                    s1_valid = 1'b1;
                    s1_idx = 3'd0;
                    d_valid = !b0[1];
                    d_idx = 3'd0;
                    s2_valid = b0[3];
                    s2_idx = 3'd2;
                    imm = {24'b0, by[1]};
                end
                8'hc8: begin
                    // ENTER imm16, imm8 (SEQ_ENTER): the push of EBP, at
                    // SS:ESP less the size, ESP unmoved. It reads ESP as
                    // src1 for the string engine's copies; imm carries the
                    // final ESP's step past the engine's last push slot, the
                    // size less imm16.
                    op = 5'd9;  // ALU_MOV
                    enter = 1'b1;
                    s2_valid = 1'b1;
                    s2_idx = 3'd5;
                    m_in_a = 1'b1;
                    mkind = 2'd2;
                    stk = fes_i386_pkg::STK_PEEK;
                    slot = 4'hf;
                    imm = (osz32 ? 32'd4 : 32'd2) - {16'b0, by[2], by[1]};
                    ecnt = by[3][4:0];
                    seq = fes_i386_pkg::SEQ_ENTER;
                    eoi = 1'b0;
                end
                8'hd6: begin
                    // SALC: AL from CF, as SBB AL, AL writing no flags
                    op = 5'd3;  // ALU_SBB
                    size = 2'd0;
                    rd_cf = 1'b1;
                    s1_valid = 1'b1;
                    s2_valid = 1'b1;
                    d_valid = 1'b1;
                end
                8'hd7: begin
                    // XLAT: AL from [BX + AL], or [EBX + AL] by address size
                    op = 5'd9;
                    size = 2'd0;
                    xlat = 1'b1;
                    mkind = 2'd1;
                    s1_valid = 1'b1;
                    d_valid = 1'b1;
                end
                8'h8f: begin
                    // POP r/m (reg 0): a register as POP r; memory, the
                    // stack's slot into T1 and the rest from fes_i386_seq
                    // (SEQ_POPM). A 32-bit address that reads ESP reads it
                    // after the pop.
                    undefined = reg_f != 3'd0;
                    op = 5'd9;
                    mkind = 2'd1;
                    if (reg_form) begin
                        s1_valid = 1'b1;
                        s1_idx = rm;
                        d_valid = 1'b1;
                        d_idx = rm;
                        stk = fes_i386_pkg::STK_POP;
                    end else begin
                        size = 2'd2;
                        mbytes = osz32 ? 3'd4 : 3'd2;
                        stk = fes_i386_pkg::STK_PEEK;
                        d_valid = 1'b1;
                        d_temp = 1'b1;
                        d_t1 = 1'b1;
                        seq = fes_i386_pkg::SEQ_POPM;
                        eoi = 1'b0;
                    end
                end
                8'b0110000?: begin
                    // PUSHA, PUSHAD (SEQ_UCODE): EDI's store at the lowest of
                    // the eight slots below ESP; POPA, POPAD: DI's load from
                    // the lowest above it. Then the ROM's routine, ESP
                    // unmoved until its last uop.
                    op = 5'd9;  // ALU_MOV
                    stk = fes_i386_pkg::STK_PEEK;
                    if (b0[0]) begin
                        s1_valid = 1'b1;
                        s1_idx = 3'd7;
                        d_valid = 1'b1;
                        d_idx = 3'd7;
                        mkind = 2'd1;
                    end else begin
                        s2_valid = 1'b1;
                        s2_idx = 3'd7;
                        m_in_a = 1'b1;
                        mkind = 2'd2;
                        slot = 4'b1000;  // -8
                    end
                    seq = fes_i386_pkg::SEQ_UCODE;
                    upc = b0[0] ? fes_i386_ucode_pkg::UC_POPA : fes_i386_ucode_pkg::UC_PUSHA;
                    eoi = 1'b0;
                end
                8'h9a: begin
                    // CALL ptr16:16 or ptr16:32 (SEQ_CALLF): a read of the
                    // return address's lower slot, which checks the offset and
                    // that slot's limit before anything is written, then push
                    // CS (an operand-sized slot, zero-extended), push the next
                    // EIP, and JMP.
                    op = 5'd9;  // ALU_MOV
                    mkind = 2'd1;
                    stk = fes_i386_pkg::STK_PROBE;
                    imm = relz1;
                    far_chk = 1'b1;
                    seg2_idx = 3'd1;
                    seg2_t = 1'b0;
                    seq = fes_i386_pkg::SEQ_CALLF;
                    eoi = 1'b0;
                end
                8'b1100110?: begin
                    // INT3 (CCh), INT n (CDh) (SEQ_UCODE): the vector's offset
                    // into T1, then the ROM's routine
                    vec = b0[0] ? by[1] : 8'd3;
                    op = 5'd9;  // ALU_MOV
                    size = 2'd2;
                    mbytes = 3'd2;
                    mkind = 2'd1;
                    ivt = 1'b1;
                    d_valid = 1'b1;
                    d_temp = 1'b1;
                    d_t1 = 1'b1;
                    seq = fes_i386_pkg::SEQ_UCODE;
                    upc = fes_i386_ucode_pkg::UC_INT;
                    eoi = 1'b0;
                end
                8'h62: begin
                    // BOUND r, m (SEQ_UCODE): the upper bound, past the lower
                    // by the operand size, into T1, the register carried in
                    // src1 for the ROM's compares. Both bounds load, and so
                    // check their limits, before either compare can raise
                    // #BR, as the 386EX does. A register operand is #UD.
                    undefined = reg_form;
                    op = 5'd9;  // ALU_MOV
                    size = 2'd2;
                    mbytes = osz32 ? 3'd4 : 3'd2;
                    mkind = 2'd1;
                    sel_part = 1'b1;
                    s1_valid = 1'b1;
                    s1_idx = reg_f;
                    d_valid = 1'b1;
                    d_temp = 1'b1;
                    d_t1 = 1'b1;
                    seq = fes_i386_pkg::SEQ_UCODE;
                    upc = fes_i386_ucode_pkg::UC_BOUND;
                    eoi = 1'b0;
                end
                8'hce: begin
                    // INTO (SEQ_UCODE): JNO to the next instruction, ending it
                    // if taken; else the ROM's INT routine, vector 4
                    vec = 8'd4;
                    cls = C_BRANCH;
                    op = 5'd0;  // BR_JCC
                    cond = 4'b0001;  // NO
                    rd_oszap = 1'b1;
                    branch = 1'b1;
                    eoi_taken = 1'b1;
                    seq = fes_i386_pkg::SEQ_UCODE;
                    upc = fes_i386_ucode_pkg::UC_VECTOR;
                    eoi = 1'b0;
                end
                8'hcf: begin
                    // IRET, IRETD (SEQ_UCODE): the offset's slot into T1, then
                    // the ROM's routine
                    op = 5'd9;  // ALU_MOV
                    size = 2'd2;
                    mbytes = osz32 ? 3'd4 : 3'd2;
                    mkind = 2'd1;
                    stk = fes_i386_pkg::STK_PEEK;
                    d_valid = 1'b1;
                    d_temp = 1'b1;
                    d_t1 = 1'b1;
                    seq = fes_i386_pkg::SEQ_UCODE;
                    upc = fes_i386_ucode_pkg::UC_IRET;
                    eoi = 1'b0;
                end
                8'b1100101?: begin
                    // RETF imm16 (CAh), RETF (CBh) (SEQ_RETF): the offset into
                    // T1 and the selector into T0, ESP unmoved, then JMP. A
                    // temporary's load is 32-bit, so nothing merges into it.
                    op = 5'd9;  // ALU_MOV
                    size = 2'd2;
                    mbytes = osz32 ? 3'd4 : 3'd2;
                    mkind = 2'd1;
                    stk = fes_i386_pkg::STK_PEEK;
                    imm = b0[0] ? 32'd0 : {16'b0, by[2], by[1]};
                    d_valid = 1'b1;
                    d_temp = 1'b1;
                    d_t1 = 1'b1;
                    seg2_idx = 3'd1;
                    seg2_t = 1'b0;
                    seq = fes_i386_pkg::SEQ_RETF;
                    eoi = 1'b0;
                end
                8'b1100010?: begin
                    // LES (C4h), LDS (C5h): SEQ_LPTR
                    undefined = reg_form;
                    seg2_idx = b0[0] ? 3'd3 : 3'd0;
                    lptr = 1'b1;
                end
                8'hf4: begin
                    cls = C_SPECIAL;
                    op = 5'd1;  // SPECIAL_HLT
                end
                8'h9b: begin
                    // WAIT: with no FPU a NOP, or #NM when MP and TS are
                    // both set (docs/i386-cpu-design.md, FPU)
                    cls = C_SPECIAL;
                    op = 5'd0;  // SPECIAL_NOP
                    nm = wait_nm;
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
                8'b1111011?: if (reg_f[2]) begin
                    // Group 3: MUL, IMUL (/4, /5), DIV, IDIV (/6, /7) of AL or eAX
                    // by r/m: EAX is src1 and the destination, r/m src2. A 16- or
                    // 32-bit form also writes EDX: the high half MUL keeps
                    // (SEQ_MUL), or DIV's remainder, after a LOAD uop that hands
                    // the unit EDX and carries the divide's op in imm (SEQ_DIV).
                    cls = reg_f[1] ? C_DIV : C_MUL;
                    op = {4'd0, reg_f[0]};  // MULDIV_UNSIGNED, MULDIV_SIGNED
                    mkind = reg_form ? 2'd0 : 2'd1;
                    size = b0[0] ? osz : 2'd0;
                    wr_cf = 1'b1;
                    wr_oszap = 1'b1;
                    s1_valid = 1'b1;
                    s2_valid = 1'b1;
                    s2_idx = b0[0] ? rm : {1'b0, rm[1:0]};
                    s2_hi8 = !b0[0] && rm[2];
                    d_valid = 1'b1;
                    if (b0[0]) begin
                        if (reg_f[1]) begin
                            op = 5'd5;  // MULDIV_LOAD
                            imm = {31'd0, reg_f[0]};
                            s1_idx = 3'd2;
                            d_valid = 1'b0;
                            wr_cf = 1'b0;
                            wr_oszap = 1'b0;
                            seq = fes_i386_pkg::SEQ_DIV;
                        end else begin
                            seq = fes_i386_pkg::SEQ_MUL;
                        end
                        eoi = 1'b0;
                    end
                end else begin
                    // Group 3: TEST r/m, imm (/0, /1); NOT; NEG.
                    op = reg_f[1] ? (reg_f[0] ? 5'd12 : 5'd13) : 5'd8;  // NEG, NOT, TEST
                    m_in_a = 1'b1;
                    mkind = reg_form ? 2'd0 : reg_f[1] ? 2'd3 : 2'd1;
                    lockable = reg_f[1];
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
                    if (b0[0] && reg_f == 3'd4) begin
                        // FFh /4: JMP r/m16/32; EX adds CS's base to the target EIP.
                        cls = C_BRANCH;
                        op = 5'd11;  // BR_JMP_INDIRECT
                        s2_valid = 1'b1;
                        s2_idx = rm;
                        mkind = reg_form ? 2'd0 : 2'd1;
                        cs_base = 1'b1;
                    end else if (b0[0] && reg_f == 3'd2) begin
                        // FFh /2: CALL r/m near: the target, zero-extended
                        // by operand size, into T1 (SEQ_CALLI)
                        op = 5'd15;  // ALU_MOVZX
                        size = 2'd2;
                        src_size = osz;
                        s2_valid = 1'b1;
                        s2_idx = rm;
                        mkind = reg_form ? 2'd0 : 2'd1;
                        mbytes = osz32 ? 3'd4 : 3'd2;
                        d_valid = 1'b1;
                        d_temp = 1'b1;
                        d_t1 = 1'b1;
                        seq = fes_i386_pkg::SEQ_CALLI;
                        eoi = 1'b0;
                    end else if (b0[0] && reg_f == 3'd6) begin
                        // FFh /6: PUSH r/m: a register as PUSH r; memory into
                        // T1, then its push (SEQ_PUSHM)
                        op = 5'd9;
                        if (reg_form) begin
                            s2_valid = 1'b1;
                            s2_idx = rm;
                            m_in_a = 1'b1;
                            mkind = 2'd2;
                            stk = fes_i386_pkg::STK_PUSH;
                        end else begin
                            size = 2'd2;
                            mbytes = osz32 ? 3'd4 : 3'd2;
                            mkind = 2'd1;
                            d_valid = 1'b1;
                            d_temp = 1'b1;
                            d_t1 = 1'b1;
                            seq = fes_i386_pkg::SEQ_PUSHM;
                            eoi = 1'b0;
                        end
                    end else if (b0[0] && (reg_f == 3'd3 || reg_f == 3'd5)) begin
                        // FFh /3, /5: CALL and JMP m16:16/32 (SEQ_CALLFM, SEQ_JMPFM)
                        undefined = reg_form;
                        fptr = 1'b1;
                        size = 2'd2;  // the temporaries' loads, unmerged
                        seg2_idx = 3'd1;
                        seg2_t = 1'b0;
                        seq = reg_f[2] ? fes_i386_pkg::SEQ_JMPFM : fes_i386_pkg::SEQ_CALLFM;
                    end else begin
                        // FEh /0 /1: INC, DEC r/m8; FFh /0 /1: INC, DEC r/m16/32
                        known = reg_f[2:1] == 2'b00;
                        op = reg_f[0] ? 5'd11 : 5'd10;
                        m_in_a = 1'b1;
                        mkind = reg_form ? 2'd0 : 2'd3;
                        lockable = 1'b1;
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
                end
                default: known = 1'b0;
            endcase
        end else begin
            casez (op2)
                8'h06: begin
                    // CLTS: CR0.TS cleared at WB, by a jump to the next
                    // instruction (SYSOP_CLTS). Real mode is CPL 0.
                    cls = C_SYSTEM;
                    op = 5'd0;  // SYSOP_CLTS
                    branch = 1'b1;
                end
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
                    op = 5'd17;  // ALU_SETCC
                    m_in_a = 1'b1;
                    mkind = reg_form ? 2'd0 : 2'd2;
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
                8'b1010?00?: begin
                    // PUSH FS (A0h), POP FS (A1h), PUSH GS (A8h), POP GS (A9h)
                    if (!op2[0]) begin
                        op = 5'd15;
                        src_size = 2'd1;
                        reads_seg = 1'b1;
                        seg_index = op2[3] ? 3'd5 : 3'd4;
                        m_in_a = 1'b1;
                        mkind = 2'd2;
                        mbytes = 3'd2;
                        stk = fes_i386_pkg::STK_PUSH;
                    end else begin
                        cls = C_SEGMENT;
                        op = 5'd0;
                        size = 2'd1;
                        mkind = 2'd1;
                        mbytes = 3'd2;   // the selector's word; ESP still steps by 4
                        d_valid = 1'b1;
                        d_seg = 1'b1;
                        d_idx = op2[3] ? 3'd5 : 3'd4;
                        stk = fes_i386_pkg::STK_POP;
                    end
                end
                8'b1010?10?: begin
                    // SHLD, SHRD r/m, r by imm8 (A4h, ACh) or by CL (A5h, ADh)
                    cls = C_SHIFT;
                    m_in_a = 1'b1;
                    mkind = reg_form ? 2'd0 : 2'd3;
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
                    imm = imm8_m;
                end
                8'b10110010, 8'b1011010?: begin
                    // LSS (B2h), LFS (B4h), LGS (B5h): SEQ_LPTR
                    undefined = reg_form;
                    seg2_idx = op2[2] ? (op2[0] ? 3'd5 : 3'd4) : 3'd2;
                    lptr = 1'b1;
                end
                8'hba: begin
                    // BT, BTS, BTR, BTC r/m, imm8 (/4 to /7; /0 to /3 are not
                    // built). BT writes nothing but flags. LOCK is valid on
                    // a read-modify-write: the others' memory form, as on the
                    // 386EX.
                    known = reg_f[2];
                    case (reg_f[1:0])
                        2'd0: op = 5'd19;  // ALU_BT
                        2'd1: op = 5'd20;  // ALU_BTS
                        2'd2: op = 5'd21;  // ALU_BTR
                        default: op = 5'd22;  // ALU_BTC
                    endcase
                    m_in_a = 1'b1;
                    mkind = reg_form ? 2'd0 : reg_f[1:0] == 2'd0 ? 2'd1 : 2'd3;
                    lockable = 1'b1;
                    wr_cf = 1'b1;
                    wr_oszap = 1'b1;
                    s1_valid = 1'b1;
                    s1_idx = rm;
                    d_valid = reg_f[1:0] != 2'd0;
                    d_idx = rm;
                    imm = imm8_m;
                end
                8'b101??011: begin
                    // BT (A3h), BTS (ABh), BTR (B3h), BTC (BBh) r/m, r: the
                    // register numbers the bit. With a register operand,
                    // one uop. A memory operand's address moves by the
                    // register (SEQ_BTM): this uop puts that displacement in
                    // T1, and imm carries the bit test's op. LOCK is valid
                    // on BTS's, BTR's and BTC's memory form, as on the 386EX.
                    case (op2[4:3])
                        2'd0: op = 5'd19;  // ALU_BT
                        2'd1: op = 5'd20;  // ALU_BTS
                        2'd2: op = 5'd21;  // ALU_BTR
                        default: op = 5'd22;  // ALU_BTC
                    endcase
                    s2_valid = 1'b1;
                    s2_idx = reg_f;
                    if (reg_form) begin
                        wr_cf = 1'b1;
                        wr_oszap = 1'b1;
                        s1_valid = 1'b1;
                        s1_idx = rm;
                        d_valid = op2[4:3] != 2'd0;
                        d_idx = rm;
                    end else begin
                        op = 5'd18;  // ALU_BITDISP
                        size = 2'd2;
                        src_size = osz;
                        d_valid = 1'b1;
                        d_temp = 1'b1;
                        d_t1 = 1'b1;
                        lock_any = op2[4:3] != 2'd0;
                        imm = {30'd0, op2[4:3]};
                        seq = fes_i386_pkg::SEQ_BTM;
                        eoi = 1'b0;
                    end
                end
                8'b1011110?: begin
                    // BSF (BCh), BSR (BDh) r, r/m: src1 is the destination,
                    // whose value a source of 0 keeps
                    op = op2[0] ? 5'd24 : 5'd23;  // ALU_BSR, ALU_BSF
                    mkind = reg_form ? 2'd0 : 2'd1;
                    wr_cf = 1'b1;
                    wr_oszap = 1'b1;
                    s1_valid = 1'b1;
                    s1_idx = reg_f;
                    s2_valid = 1'b1;
                    s2_idx = rm;
                    d_valid = 1'b1;
                    d_idx = reg_f;
                end
                8'haf: begin
                    // IMUL r, r/m: the register is src1 and the destination
                    cls = C_MUL;
                    op = 5'd1;  // MULDIV_SIGNED
                    mkind = reg_form ? 2'd0 : 2'd1;
                    wr_cf = 1'b1;
                    wr_oszap = 1'b1;
                    s1_valid = 1'b1;
                    s1_idx = reg_f;
                    s2_valid = 1'b1;
                    s2_idx = rm;
                    d_valid = 1'b1;
                    d_idx = reg_f;
                end
                8'b1011?11?: begin
                    // MOVZX (B6h, B7h), MOVSX (BEh, BFh)
                    op = op2[3] ? 5'd16 : 5'd15;
                    mkind = reg_form ? 2'd0 : 2'd1;
                    mbytes = op2[0] ? 3'd2 : 3'd1;
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

        // A far pointer's first uop: the selector's word into T0. LES and
        // its kin keep the register as the destination for the second.
        if (lptr || fptr) begin
            op = 5'd9;  // ALU_MOV
            mkind = reg_form ? 2'd0 : 2'd1;
            mbytes = 3'd2;
            s1_valid = lptr;
            s1_idx = reg_f;
            d_valid = 1'b1;
            d_temp = 1'b1;
            d_idx = lptr ? reg_f : 3'd0;
            sel_part = 1'b1;
            if (lptr) seq = fes_i386_pkg::SEQ_LPTR;
            eoi = 1'b0;
        end

        // The memory operand replaces the register it stands for.
        if (mkind != 2'd0) begin
            if (m_in_a) begin
                s1_valid = 1'b0;
                s1_hi8 = 1'b0;
                d_valid = 1'b0;
                d_hi8 = 1'b0;
            end else begin
                s2_valid = 1'b0;
                s2_hi8 = 1'b0;
            end
        end
        if (enter) begin
            s1_valid = 1'b1;
            s1_idx = 3'd4;
        end
    end

    // ---- The address's displacement ---------------------------------------------
    logic [31:0] d8, d16, d32;
    assign d8 = {{24{byx[dpos][7]}}, byx[dpos]};
    assign d16 = {16'b0, byx[dpos + 3'd1], byx[dpos]};
    assign d32 = {byx[dpos + 3'd3], byx[dpos + 3'd2], byx[dpos + 3'd1], byx[dpos]};
    always_comb begin
        if (moffs) lea_disp = addr32 ? {by[4], by[3], by[2], by[1]} : {16'b0, by[2], by[1]};
        else if (xlat) lea_disp = 32'd0;
        else if (dlen == 3'd1) lea_disp = addr32 ? d8 : {16'b0, d8[15:0]};
        else if (dlen == 3'd2) lea_disp = d16;
        else if (dlen == 3'd4) lea_disp = d32;
        else lea_disp = 32'd0;
    end
    assign lea_early = osz == 2'd2;

    // ---- Faults and the uop ---------------------------------------------------
    // LOCK is checked before the 15-byte limit (Phase 0 divergence
    // lock-ud-before-length-gp).
    logic [2:0] fault;
    logic       lock_ok;
    assign lock_ok = lockable && mkind == 2'd3 || lock_any;
    assign fault = !known ? 3'd4                                      // FAULT_UNIMPLEMENTED
                 : undefined || (q.prefixes.lock && !lock_ok) ? 3'd1  // FAULT_UD
                 : q.too_long ? 3'd2                                  // FAULT_GP0
                 : nm ? 3'd3 : 3'd0;                                  // FAULT_NM
    assign stop = fault != 3'd0 || (cls == C_SPECIAL && op == 5'd1);

    uop_t u;
    assign u.seg2 = seg2 && fault == 3'd0;
    assign u.seg2_idx = seg2_idx;
    assign u.seg2_t = seg2_t;
    assign wseg = (u.dst.valid && d_seg ? 6'(1) << d_idx : 6'd0) | (u.seg2 ? 6'(1) << seg2_idx : 6'd0);
    // A far pointer's selector follows its offset.
    // INT3, INT n and INTO carry the vector's entry, vector * 4, here.
    assign far_sel = ivt || eoi_taken ? {6'd0, vec, 2'b00} : osz32 ? {by[6], by[5]} : {by[4], by[3]};
    assign u.probe = probe;
    assign u.eoi_taken = eoi_taken;
    // A string's elements and their steps come from fes_i386_seq.
    assign u.str_si = 1'b0;
    assign u.str_di = 1'b0;
    assign u.str_cx = 1'b0;
    assign u.str_down = 1'b0;
    assign u.st2 = 1'b0;
    assign u.rep_stop = 2'd0;
    assign u.io = io;
    assign u.iret = 1'b0;
    assign u.bound = 1'b0;
    assign u.sp_add = 1'b0;
    assign u.sp_write = stk != fes_i386_pkg::STK_NONE && stk != fes_i386_pkg::STK_PEEK && stk != fes_i386_pkg::STK_PROBE;
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
    assign u.dst.space = d_seg ? fes_i386_pkg::RS_SEG : d_temp ? fes_i386_pkg::RS_TEMP : fes_i386_pkg::RS_GPR;
    assign u.dst.index = {d_t1, d_idx};
    assign u.dst.hi8 = d_hi8;
    assign u.size = fes_i386_pkg::size_e'(size);
    assign u.src_size = fes_i386_pkg::size_e'(src_size);
    assign u.count_cl = count_cl;
    assign u.imm = imm;
    assign u.mem.kind = fes_i386_pkg::mem_kind_e'(mkind);
    assign u.mem.bytes = mbytes != 3'd0 ? 4'(mbytes) : size == 2'd0 ? 4'd1 : size == 2'd1 ? 4'd2 : 4'd4;
    // A direct offset, XLAT's [BX + AL] or a string's ESI and EDI has no BP
    // base, whatever byte 1 looks like as a ModRM.
    assign direct = ivt || moffs || strop;
    assign m_seg = q.prefixes.seg_valid ? q.prefixes.seg
                 : stack && !direct && !xlat ? 3'(fes_i386_pkg::SEG_SS) : 3'(fes_i386_pkg::SEG_DS);
    assign u.mem.seg = stk != fes_i386_pkg::STK_NONE ? fes_i386_pkg::SEG_SS : fes_i386_pkg::seg_e'(m_seg);
    assign u.mem.base = '0;
    assign u.mem.index = '0;
    assign u.mem.scale = 2'd0;
    assign u.mem.disp = lea_disp;
    assign u.mem.addr32 = addr32;
    assign u.mem.lock = q.prefixes.lock;
    assign u.mem.system = ivt;
    assign u.mem.in_a = m_in_a;
    assign u.flags.read.cf = rd_cf;
    assign u.flags.read.oszap = rd_oszap;
    assign u.flags.write.cf = wr_cf;
    assign u.flags.write.oszap = wr_oszap;
    assign u.ctl.cond = cond;
    assign u.ctl.predict_taken = 1'b0;
    assign u.ctl.end_of_instruction = eoi;
    assign u.ctl.ilen = q.length;
    assign u.ctl.fault = fes_i386_pkg::fault_e'(fault);
    assign uop = u;
endmodule
