// SPDX-License-Identifier: MIT
// Shared types for fes_i386_cpu (docs/i386-cpu-design.md). The uop is the
// pipeline's only instruction format and follows the design's Decode table.
// The front-end records carry an x86 instruction from the fetch buffer
// through Q to D; pipe_t carries a uop from D to WB.
package fes_i386_pkg;

    // ---- Front end ----------------------------------------------------------

    typedef enum logic [2:0] {
        SEG_ES = 3'd0, SEG_CS = 3'd1, SEG_SS = 3'd2, SEG_DS = 3'd3, SEG_FS = 3'd4, SEG_GS = 3'd5
    } seg_e;

    typedef enum logic [1:0] { REP_NONE = 2'b00, REP_NE = 2'b10, REP_E = 2'b11 } rep_e;

    // Prefixes of one instruction. 66h and 67h are flags (two 66h equal one);
    // the last segment override and the last of F2h/F3h win. seg holds a
    // seg_e value and rep a rep_e value.
    typedef struct packed {
        logic       opsize;
        logic       adsize;
        logic       seg_valid;
        logic [2:0] seg;
        logic [1:0] rep;
        logic       lock;
    } prefixes_t;

    // L1's length classes for the instruction starting at one byte, its one
    // prefix included (fes_i386_lendec). L2 adds them up for each size.
    typedef struct packed {
        logic       prefix;   // the byte is a prefix
        logic       run;      // ... and so is the next byte
        logic       opsize;   // the byte is 66h
        logic       adsize;   // the byte is 67h
        logic [2:0] head;     // prefix, opcode and fixed-immediate bytes, 1-6
        logic [1:0] mb16;     // ModRM, SIB and displacement bytes, 16-bit addressing
        logic [2:0] mb32;     // ... 32-bit addressing
        logic       z;        // 2 or 4 more bytes by operand size
        logic       moffs;    // 2 or 4 more bytes by address size
    } lclass_t;

    // One buffer byte's decode from L2 (fes_i386_lstage), per prefix-
    // accumulator variant v = {66h seen, 67h seen}.
    typedef struct packed {
        logic            prefix;
        logic            run;
        logic [3:0][3:0] len;  // the instruction's length, its one prefix included
        logic [3:0][4:0] fin;  // where it ends, counted as L counts, modulo 32
        logic [3:0]      past_half;  // it ends past its own half ("far" is a C++ word)
    } lbyte_t;

    // One x86 instruction from Q to D. bytes[0] is the opcode (0Fh for a
    // two-byte opcode); every prefix has been stripped into prefixes.
    typedef struct packed {
        logic [31:0]      linear;        // linear address of the first byte
        logic [3:0]       length;        // every byte, prefixes included
        logic [3:0]       prefix_count;
        prefixes_t        prefixes;
        logic             too_long;      // over 15 bytes: #GP(0) at D
        logic [10:0][7:0] bytes;
    } fetched_t;

    function automatic logic is_prefix(input logic [7:0] b);
        case (b)
            8'h26, 8'h2e, 8'h36, 8'h3e, 8'h64, 8'h65, 8'h66, 8'h67, 8'hf0, 8'hf2, 8'hf3: is_prefix = 1'b1;
            default: is_prefix = 1'b0;
        endcase
    endfunction

    // Fold prefix byte b into p; b must satisfy is_prefix.
    function automatic prefixes_t add_prefix(input prefixes_t p, input logic [7:0] b);
        prefixes_t r;
        r = p;
        case (b)
            8'h26: begin r.seg_valid = 1'b1; r.seg = SEG_ES; end
            8'h2e: begin r.seg_valid = 1'b1; r.seg = SEG_CS; end
            8'h36: begin r.seg_valid = 1'b1; r.seg = SEG_SS; end
            8'h3e: begin r.seg_valid = 1'b1; r.seg = SEG_DS; end
            8'h64: begin r.seg_valid = 1'b1; r.seg = SEG_FS; end
            8'h65: begin r.seg_valid = 1'b1; r.seg = SEG_GS; end
            8'h66: r.opsize = 1'b1;
            8'h67: r.adsize = 1'b1;
            8'hf0: r.lock = 1'b1;
            8'hf2: r.rep = REP_NE;
            8'hf3: r.rep = REP_E;
            default: ;
        endcase
        return r;
    endfunction

    // ---- Uop ------------------------------------------------------------------

    // Operation class; op selects within it.
    typedef enum logic [2:0] {
        UOP_ALU, UOP_SHIFT, UOP_MUL, UOP_DIV, UOP_BRANCH, UOP_SEGMENT, UOP_SYSTEM, UOP_SPECIAL
    } uop_class_e;

    // UOP_ALU. The first eight follow the x86 group-1 order. MOVZX and MOVSX
    // extend from src_size; SIGN gives every bit the sign of the source at
    // src_size (CWD, CDQ); FLAGS is the flag instruction imm names (flag_op_e).
    typedef enum logic [4:0] {
        ALU_ADD, ALU_OR, ALU_ADC, ALU_SBB, ALU_AND, ALU_SUB, ALU_XOR, ALU_CMP,
        ALU_TEST, ALU_MOV, ALU_INC, ALU_DEC, ALU_NEG, ALU_NOT, ALU_LEA,
        ALU_MOVZX, ALU_MOVSX, ALU_SETCC, ALU_XCHG, ALU_BT, ALU_BTS, ALU_BTR, ALU_BTC,
        ALU_BSF, ALU_BSR, ALU_DAA, ALU_DAS, ALU_AAA, ALU_AAS, ALU_SALC, ALU_FLAGS, ALU_SIGN
    } alu_op_e;

    // ALU_FLAGS, in imm[3:0]. LOAD writes the flags from src2 (POPF, IRET);
    // READ gives FLAGS' image (PUSHF), READ_INT too, clearing IF and TF (INT).
    typedef enum logic [3:0] {
        FLAG_CLC, FLAG_STC, FLAG_CMC, FLAG_CLI, FLAG_STI, FLAG_CLD, FLAG_STD, FLAG_SAHF, FLAG_LAHF,
        FLAG_LOAD, FLAG_READ, FLAG_READ_INT
    } flag_op_e;

    // UOP_SHIFT. The first eight follow the x86 group-2 order.
    typedef enum logic [4:0] {
        SHIFT_ROL, SHIFT_ROR, SHIFT_RCL, SHIFT_RCR, SHIFT_SHL, SHIFT_SHR, SHIFT_SAL, SHIFT_SAR,
        SHIFT_SHLD, SHIFT_SHRD
    } shift_op_e;

    // UOP_SEGMENT: a real-mode or V86 load of the selector in src2, which
    // sets the base to selector * 16.
    typedef enum logic [4:0] { SEGOP_LOAD_REAL } segment_op_e;

    // UOP_SPECIAL.
    typedef enum logic [4:0] { SPECIAL_NOP, SPECIAL_HLT } special_op_e;

    // UOP_MUL and UOP_DIV.
    typedef enum logic [4:0] { MULDIV_UNSIGNED, MULDIV_SIGNED, MULDIV_AAM, MULDIV_AAD } muldiv_op_e;

    // UOP_BRANCH.
    typedef enum logic [4:0] {
        BR_JCC, BR_JMP, BR_CALL, BR_RET, BR_JMP_IND, BR_CALL_IND,
        BR_LOOP, BR_LOOPE, BR_LOOPNE, BR_JCXZ,
        BR_FAR_JMP, BR_FAR_CALL, BR_FAR_RET, BR_MICRO
    } branch_op_e;

    // Register spaces. GPR indexes follow the x86 encoding (EAX, ECX, EDX,
    // EBX, ESP, EBP, ESI, EDI); TEMP holds microcode temporaries T0-T7; SEG
    // uses seg_e; SYS uses sys_reg_e.
    typedef enum logic [1:0] { RS_GPR, RS_TEMP, RS_SEG, RS_SYS } reg_space_e;

    typedef enum logic [3:0] {
        SYS_CR0 = 4'd0, SYS_CR2 = 4'd2, SYS_CR3 = 4'd3,
        SYS_DR0 = 4'd4, SYS_DR1 = 4'd5, SYS_DR2 = 4'd6, SYS_DR3 = 4'd7,
        SYS_DR6 = 4'd8, SYS_DR7 = 4'd9, SYS_TR6 = 4'd10, SYS_TR7 = 4'd11,
        SYS_LDTR = 4'd12, SYS_TR = 4'd13, SYS_GDTR = 4'd14, SYS_IDTR = 4'd15
    } sys_reg_e;

    // A register operand. hi8 selects AH, CH, DH or BH for a byte-sized GPR.
    typedef struct packed {
        logic       valid;
        reg_space_e space;
        logic [3:0] index;
        logic       hi8;
    } reg_ref_t;

    typedef enum logic [1:0] { SIZE_8, SIZE_16, SIZE_32 } size_e;

    typedef enum logic [1:0] { MEM_NONE, MEM_LOAD, MEM_STORE, MEM_RMW } mem_kind_e;

    // The memory operand. The address is seg base + base + (index << scale)
    // + disp, wrapped to 16 bits before the segment base when addr32 is clear.
    // system marks descriptor, TSS and page-table accesses, which use
    // supervisor rights at any CPL.
    typedef struct packed {
        mem_kind_e   kind;
        logic [3:0]  bytes;  // 1, 2, 4, 6 or 8
        seg_e        seg;
        reg_ref_t    base;
        reg_ref_t    index;
        logic [1:0]  scale;
        logic [31:0] disp;
        logic        addr32;
        logic        lock;
        logic        system;
        logic        in_a;   // the operand is src1 (x), else src2 (y)
    } mem_t;

    // Flags are tracked as two groups: CF, and OF/SF/ZF/AF/PF together.
    typedef struct packed {
        logic oszap;
        logic cf;
    } flag_groups_t;

    typedef struct packed {
        flag_groups_t read;
        flag_groups_t write;
    } flags_t;

    // Faults decode finds; each travels with its uop and is taken at WB.
    // UNIMPLEMENTED is not architectural: an encoding this build does not
    // execute yet. WB stops on it, so a check never retires one silently.
    typedef enum logic [2:0] { FAULT_NONE, FAULT_UD, FAULT_GP0, FAULT_NM, FAULT_UNIMPLEMENTED, FAULT_SS0 } fault_e;

    typedef struct packed {
        logic [3:0] cond;             // x86 condition code (tttn) for Jcc, SETcc
        logic       predict_taken;
        logic       end_of_instruction;
        logic [3:0] ilen;             // x86 instruction length, on its last uop
        fault_e     fault;
    } control_t;

    // src1 is the destination's register whenever there is one: a byte or
    // word write merges into its old value, an implicit source. src2 is the
    // second register source, or imm when it is not valid; count_cl takes a
    // shift count from CL, a third source. A stack operation also writes ESP
    // (sp_write), from the value AG computes (pipe_t sp_new): a second
    // destination, which the main one overrides when it is ESP too (POP ESP).
    // A segment register can be a second destination too (seg2: LES and its
    // kin), written at WB with the selector in T0 (seg2_t) or in c. A
    // destination in RS_TEMP is T0 (16 bits), or T1 (32) with index bit 3,
    // which WB writes and no forwarding sees.
    // sp_add: WB adds sp_new to ESP (RETF, whose jump flushes every younger
    // uop, so nothing needs the new ESP sooner).
    // probe: the uop reads the lowest slot of a routine's pushes or pops
    // (PROBE_*), and M1 also faults when the slots cross the 64 KiB wrap
    // misaligned, so a routine writes nothing before it can fault.
    typedef struct packed {
        logic [1:0]  probe;
        logic        seg2;
        logic [2:0]  seg2_idx;
        logic        seg2_t;
        logic        sp_add;
        logic        sp_write;
        uop_class_e  cls;
        logic [4:0]  op;
        reg_ref_t    src1;
        reg_ref_t    src2;
        reg_ref_t    dst;
        size_e       size;
        size_e       src_size;  // src2's size where it differs (MOVZX, MOVSX, SIGN)
        logic        count_cl;
        logic [31:0] imm;
        mem_t        mem;
        flags_t      flags;
        control_t    ctl;
    } uop_t;

    // ---- Pipeline ---------------------------------------------------------------

    // An instruction decoded in DC, as D takes it (fes_i386_decode and
    // fes_i386_amode on Q's output register; fes_i386_cpu).
    typedef struct packed {
        uop_t        uop;
        logic [3:0]  length;
        logic        opsize32;
        logic        lea;
        logic        lea_early;
        logic [31:0] lea_disp;
        logic        consumes;
        logic        reads_seg;
        logic [2:0]  seg_index;
        logic        stop;
        logic        has_base;
        logic [2:0]  base;
        logic        has_index;
        logic [2:0]  index;
        logic [1:0]  scale;
        // The stack: what the instruction does to ESP (STK_*), and what D
        // puts in its uop: the next instruction's EIP as src2 (a CALL's push),
        // CS's base as src1 (RET and JMP r/m, whose EX adds the target).
        logic [2:0]  stk;
        logic        push_eip;
        logic        cs_base;
        // The uop reads a far pointer's selector, past its offset by the
        // operand size. Each part is checked against the limit on its own,
        // the selector's offset wrapped by address size, as the 386EX does.
        logic        sel_part;
        // A far transfer's selector and offset: far_sel and uop.imm, or T0
        // and T1 (far_t, once the uops loading them have retired). far_jmp: the
        // uop is the jump, whose target AG computes as the new CS's base
        // plus the offset; far_chk: the uop checks the offset against CS's
        // limit, before the transfer writes anything.
        logic        far_jmp;
        logic        far_chk;
        logic        far_t;
        logic [15:0] far_sel;
        // The segment registers the uop writes, one-hot: its destination's
        // or its second destination's (D's segment-busy update).
        logic [5:0]  wseg;
        // A stack read's slot past ESP, in operand-sized slots (STK_PEEK), and
        // a push of the value in T1 (b_t1, which waits as far_t does).
        logic [1:0]  slot;
        logic        b_t1;
        logic        a_t1;       // src1's value is T1's (POPAD's ESP), which waits as b_t1 does
        logic        popad_esp;  // the uop counts only for POPAD on a 16-bit stack
        // A microcode routine (SEQ_UCODE): the ROM address of its next uop.
        logic [7:0]  upc;
        // A fixed uop sequence (SEQ_*), the step dc holds, and whether
        // another uop follows (fes_i386_seq).
        logic [2:0]  seq;
        logic [2:0]  step;
        logic        more;
    } decoded_t;

    // STK_PEEK reads the stack without moving ESP (RETF's loads); STK_RETF
    // is RETF's step of ESP, at its jump; STK_PROBE reads the lower slot of
    // a far call's two pushes, only for its limit check.
    typedef enum logic [2:0] { STK_NONE, STK_PUSH, STK_POP, STK_RET_IMM, STK_LEAVE, STK_PEEK, STK_RETF, STK_PROBE } stk_e;
    typedef enum logic [2:0] { SEQ_NONE, SEQ_CALL, SEQ_LPTR, SEQ_CALLF, SEQ_RETF, SEQ_JMPFM, SEQ_CALLFM, SEQ_UCODE } seq_e;
    typedef enum logic [1:0] { PROBE_NONE, PROBE_INT, PROBE_PUSHA, PROBE_POPA } probe_e;

    // A microcode ROM word (fes_i386_ucode_rom, built by
    // scripts/fes_i386_ucode.py from ucode.toml): the fields of one uop that
    // a routine sets. fes_i386_seq expands it into dc, taking the rest from
    // the instruction there. The script packs the fields in this order.
    typedef struct packed {
        logic        last;       // the routine's last uop
        logic [2:0]  cls;
        logic [4:0]  op;
        logic [3:0]  imm;        // a small constant (a flag operation's)
        logic [1:0]  size;       // UC_SZ: operand size, 16, 32, 8
        logic [1:0]  src_size;
        logic        s1;
        logic [2:0]  s1_idx;
        logic        s2;
        logic [2:0]  s2_idx;
        logic        d;
        logic [1:0]  d_space;    // reg_space_e
        logic [3:0]  d_idx;      // T1 with bit 3, in RS_TEMP
        logic [1:0]  mkind;      // mem_kind_e
        logic [1:0]  mbytes;     // operand size, 2, 4, 1
        logic        in_a;
        logic [2:0]  stk;        // stk_e
        logic [1:0]  slot;
        logic        ivt;        // the interrupt vector's entry, at linear vector * 4
        logic        sel_part;   // ... its selector, at + 2
        logic [1:0]  probe;      // probe_e
        logic [1:0]  b;          // UC_B: src2, the next EIP, T1, segment seg's selector
        logic [2:0]  seg;
        logic        far_jmp;
        logic        far_chk;
        logic        far_t;
        logic        sp_add;
        logic        a_t1;       // src1's value is T1's
        logic        popad_esp;  // kept only for POPAD on a 16-bit stack
        logic        rd_cf;
        logic        rd_oszap;
        logic        wr_cf;
        logic        wr_oszap;
    } ucode_t;

    // The shifter's controls (fes_i386_shift), decoded in M2. sel gates
    // each candidate arrangement: sel[d * 15 + z * 5 + k] for direction d (0
    // right, 1 left), size z (8, 16, 32 bits) and fill k (the operand alone,
    // rotate, rotate through carry, sign, the SHLD/SHRD source).
    typedef struct packed {
        logic [29:0] sel;
        logic        left;
        logic        z8, z16, z32;
        logic [4:0]  s;                     // the shift amount: the count, or 31 - count left
        logic [4:0]  count;                 // the count, unless EX takes CL from WB (byp_c)
        logic        cf_rol, cf_ror, cf_shl, cf_shr;  // CF other than the last bit out
        logic        szap;                  // SF, ZF, AF and PF change (not a rotate)
    } shctl_t;

    // EX's controls, decoded from the uop in M2 (fes_i386_exctl) so that EX
    // sees registered selects: one-hot where EX picks among sources. M2 also
    // prepares EX's operands x and y (pipe_t); an operand still pending with
    // its producer one stage ahead comes from the WB result register in EX,
    // through the x and y bypass selects.
    typedef struct packed {
        logic       ok;                        // no fault: EX changes nothing otherwise
        logic       byp_a, byp_c;              // src1 (the merge's old value) and CL come from WB
        // x and y take WB's bits 7:0 (lo) or 15:8 (hi) into their low byte,
        // its bits 15:8 (w16) and 31:16 (w32) in place.
        logic       xw_lo, xw_hi, xw16, xw32;
        logic       yw_lo, yw_hi, yw16, yw32;
        logic       xl, yl;                    // ... the load data (pipe_t ld) instead of WB's
        logic       size8, size16;             // the operation's size (32 otherwise)
        logic       src8, src16;               // src2's size for MOVSX and SIGN
        logic       sub;                       // the adder subtracts
        logic       cin_cf, cin_ncf;           // ADC, SBB; else the carry-in is sub
        // The result by lane: bits 7:0 (lo), 15:8 (mid), the low byte's
        // result into 15:8 (h8, a high-byte destination) and 31:16 (hi); the
        // old value elsewhere (kp).
        logic       add_lo, add_mid, add_h8, add_hi;
        logic       sh_lo, sh_mid, sh_h8, sh_hi;
        logic       ot_lo, ot_mid, ot_h8, ot_hi;
        logic       kp_lo, kp_mid, kp_hi;
        logic       r_logic, r_lea, r_y, r_movsx, r_setcc, r_lahf, r_sign;  // the other results
        logic       r_flags;                   // FLAGS' image (PUSHF, INT's push)
        logic       l_or, l_xor;               // the logic unit; AND otherwise
        logic       f_add;                     // CF comes from the adder
        logic       f_arith;                   // OF, SF, ZF, AF and PF come from the adder
        logic       f_logic, f_shift;          // ... from the logic unit; from the shift, at WB
        logic       f_cf, f_if, f_df, f_szap;  // the flag instructions (fl_* the value)
        logic       f_load;                    // the writable flags from y (POPF, IRET)
        logic       f_clrit;                   // IF and TF cleared (INT)
        logic       fl_cf, fl_cmc, fl_set;
        logic       b_jcc, b_jmp, b_loop, b_loope, b_loopne, b_jcxz;
        logic       b_ind;                     // RET, JMP r/m: the target is x (CS's base) + y (the EIP)
        shctl_t     sh;
    } exctl_t;

    // A uop from D to WB. a, b and c are the values of src1, src2 (or imm)
    // and ECX for a CL count. An operand whose producer has not computed it
    // when D reads it is pending: it arrives from the WB result register when
    // that producer reaches WB, which is dist stages ahead (1-4; see
    // fes_i386_dops).
    typedef struct packed {
        logic        valid;
        uop_t        uop;
        logic [31:0] eip;         // the x86 instruction's EIP
        logic [31:0] eip_next;    // the next instruction's, sequentially
        logic [31:0] a;
        logic [31:0] b;
        logic [31:0] c;
        logic        pend_a;
        logic        pend_b;
        logic        pend_c;
        logic [2:0]  dist_a;
        logic [2:0]  dist_b;
        logic [2:0]  dist_c;
        logic        early;       // the result is ready at the end of AG (LEA)
        logic [31:0] sp_new;      // AG: ESP after a stack operation (uop sp_write)
        logic [31:0] maddr;       // AG: the memory operand's linear address
        logic [31:0] moff;        // ... its offset
        logic [31:0] mlimit;      // D: its segment's limit less the access's bytes but one
        logic        mtiny;       // D: ... which is below zero: every access faults
        logic        pbad;        // M1: a probe's slots straddle the 64 KiB wrap (#SS at M2)
        logic [31:0] ld;          // M2: the loaded value, zero-extended (fes_i386_exctl xl, yl)
        logic [31:0] result;      // AG: LEA's offset; EX: the destination's new value
        logic [31:0] target;      // AG: a branch's target linear address
        logic [31:0] target_eip;  // ... and EIP
        exctl_t      exc;         // M2: EX's controls
        logic [31:0] x;           // M2: EX's adder, logic and shifter operands (fes_i386_exctl)
        logic [31:0] y;
        logic [17:0] eflags;      // EX: EFLAGS after this uop
        logic        taken;       // EX: a branch that redirects fetch
        logic        sh_out0;     // EX: a shift's bit out at the right, the one out at the left by size,
        logic        sh_outn;     // and its wide-count CF (fes_i386_shflags, applied at WB: def_co)
        logic        sh_cfw;
        logic        sh_wide;
        logic        def_co;
        logic        def_szap;    // EX: a shift's SF, ZF and PF follow the result; AF is set
    } pipe_t;

endpackage
