// SPDX-License-Identifier: MIT
// Shared types for fes_i386_cpu (docs/i386-cpu-design.md). The uop is the
// pipeline's only instruction format and follows the design's Decode table.
// The front-end records carry an x86 instruction from the fetch buffer
// through Q to D.
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

    // A sum of a few bits built from gates. Yosys maps every + to an ALM
    // carry chain, and entering one costs about a nanosecond; for lengths and
    // positions a LUT is faster.
    function automatic logic [4:0] small_add(input logic [4:0] a, input logic [4:0] b);
        logic carry;
        carry = 1'b0;
        for (int i = 0; i < 5; i++) begin
            small_add[i] = a[i] ^ b[i] ^ carry;
            carry = (a[i] & b[i]) | (a[i] & carry) | (b[i] & carry);
        end
    endfunction

    // The same for six bits.
    function automatic logic [5:0] small_add6(input logic [5:0] a, input logic [5:0] b);
        logic carry;
        carry = 1'b0;
        for (int i = 0; i < 6; i++) begin
            small_add6[i] = a[i] ^ b[i] ^ carry;
            carry = (a[i] & b[i]) | (a[i] & carry) | (b[i] & carry);
        end
    endfunction

    // Fold prefix byte b into p; b must satisfy is_prefix. The seg and rep
    // values are computed from the byte, since Yosys cannot resolve package
    // constants inside a package function called from a module.
    function automatic prefixes_t add_prefix(input prefixes_t p, input logic [7:0] b);
        prefixes_t r;
        r = p;
        case (b)
            8'h26, 8'h2e, 8'h36, 8'h3e: begin r.seg_valid = 1'b1; r.seg = {1'b0, b[4:3]}; end  // ES CS SS DS
            8'h64, 8'h65: begin r.seg_valid = 1'b1; r.seg = {2'b10, b[0]}; end                // FS GS
            8'h66: r.opsize = 1'b1;
            8'h67: r.adsize = 1'b1;
            8'hf0: r.lock = 1'b1;
            8'hf2, 8'hf3: r.rep = {1'b1, b[0]};                                                 // REP_NE REP_E
            default: ;
        endcase
        add_prefix = r;
    endfunction

    // ---- Uop ------------------------------------------------------------------

    // Operation class; op selects within it.
    typedef enum logic [2:0] {
        UOP_ALU, UOP_SHIFT, UOP_MUL, UOP_DIV, UOP_BRANCH, UOP_SEGMENT, UOP_SYSTEM, UOP_SPECIAL
    } uop_class_e;

    // UOP_ALU. The first eight follow the x86 group-1 order.
    typedef enum logic [4:0] {
        ALU_ADD, ALU_OR, ALU_ADC, ALU_SBB, ALU_AND, ALU_SUB, ALU_XOR, ALU_CMP,
        ALU_TEST, ALU_MOV, ALU_INC, ALU_DEC, ALU_NEG, ALU_NOT, ALU_LEA,
        ALU_MOVZX, ALU_MOVSX, ALU_SETCC, ALU_XCHG, ALU_BT, ALU_BTS, ALU_BTR, ALU_BTC,
        ALU_BSF, ALU_BSR, ALU_DAA, ALU_DAS, ALU_AAA, ALU_AAS, ALU_SALC, ALU_FLAGS
    } alu_op_e;

    // UOP_SHIFT. The first eight follow the x86 group-2 order.
    typedef enum logic [4:0] {
        SHIFT_ROL, SHIFT_ROR, SHIFT_RCL, SHIFT_RCR, SHIFT_SHL, SHIFT_SHR, SHIFT_SAL, SHIFT_SAR,
        SHIFT_SHLD, SHIFT_SHRD
    } shift_op_e;

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
    typedef enum logic [2:0] { FAULT_NONE, FAULT_UD, FAULT_GP0, FAULT_NM } fault_e;

    typedef struct packed {
        logic [3:0] cond;             // x86 condition code (tttn) for Jcc, SETcc
        logic       predict_taken;
        logic       end_of_instruction;
        logic [3:0] ilen;             // x86 instruction length, on its last uop
        fault_e     fault;
    } control_t;

    typedef struct packed {
        uop_class_e  cls;
        logic [4:0]  op;
        reg_ref_t    src1;
        reg_ref_t    src2;
        reg_ref_t    dst;
        size_e       size;
        logic [31:0] imm;
        mem_t        mem;
        flags_t      flags;
        control_t    ctl;
    } uop_t;

endpackage
