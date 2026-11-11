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
        // The branch target buffer (fes_i386_btb; fes_i386_qalign): bm, the
        // instruction ends where the BTB entry of the block holding its last
        // byte says a branch ends, that entry's way and counter; tk, the
        // entry predicted it taken and Q went on at its target. xbad: the
        // instruction runs into a buffer half that holds no sequential
        // continuation (a predicted target), so its bytes past that point
        // are not its own.
        logic             bm;
        logic             bway;
        logic [1:0]       bctr;
        logic             tk;
        logic             xbad;
        // ptgt: where Q went on after a predicted taken branch (tk), the
        // target's linear address. D gives a RET predicted by the return
        // stack its EIP from it.
        logic [31:0]      ptgt;
        // Paging (fes_i386_l1i's ITLB, carried with each block): pp0, the
        // physical page bits 19:12 of the instruction's first byte, pp1 of the
        // block it runs into (its own when it does not), for the self-modifying
        // code check; tm0 and tm1, the ITLB had no entry for that block's
        // page; ts0 and ts1, the entry is a supervisor page. D turns an
        // instruction with a mark, or a supervisor page at CPL 3, into a walk
        // (pipe_t ifw).
        logic [7:0]       pp0;
        logic [7:0]       pp1;
        logic             tm0;
        logic             tm1;
        logic             ts0;
        logic             ts1;
        logic [10:0][7:0] bytes;
    } fetched_t;

    // A fetch block's paging tag from the L1I (fes_i386_l1i): no ITLB entry
    // for its page (tm), a supervisor page (ts), and its physical page bits
    // 19:12 (pp). With paging off, tm and ts are clear and pp is the
    // linear address's bits 19:12.
    typedef struct packed {
        logic       tm;
        logic       ts;
        logic [7:0] pp;
    } ftag_t;

    // One BTB lookup, for the block F2 holds (fes_i386_btb), and one write,
    // from WB. An entry describes the first predicted branch of the 16-byte
    // block that holds the branch's last byte: last is that byte's offset,
    // target its target's linear address, ctr a 2-bit counter (taken at 2
    // or more), uncond a JMP or CALL rel or r/m or a RET, always taken. A
    // JMP or CALL r/m's target is the one it last went to. A CALL's entry
    // (call) pushes the return stack (fes_i386_ras) as F2 predicts it; a
    // RET's (ret) pops it and goes to its top, not to target.
    typedef struct packed {
        logic        valid;
        logic        uncond;
        logic        call;
        logic        ret;
        logic [1:0]  ctr;
        logic [3:0]  last;
        logic [19:0] tag;     // linear address bits 31:12
        logic [31:0] target;
    } btb_entry_t;

    typedef struct packed {
        logic        hit;     // an entry of the block's set holds the block
        logic        way;
        logic [1:0]  ctr;
        logic [3:0]  last;
        logic        taken;   // F2 predicted it taken and fetched its target next
        logic [27:0] tblk;    // ... this block
        logic [3:0]  toff;    // ... at this offset in it
    } btb_info_t;

    typedef struct packed {
        logic       valid;
        logic [1:0] ways;     // the ways it writes
        logic [7:0] index;    // linear address bits 11:4
        btb_entry_t entry;
    } btb_write_t;

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
    // BITDISP is a bit test's address displacement from its register bit
    // number, src2 at src_size: (n >> 4) * 2 or (n >> 5) * 4, signed
    // (SEQ_BTM).
    typedef enum logic [4:0] {
        ALU_ADD, ALU_OR, ALU_ADC, ALU_SBB, ALU_AND, ALU_SUB, ALU_XOR, ALU_CMP,
        ALU_TEST, ALU_MOV, ALU_INC, ALU_DEC, ALU_NEG, ALU_NOT, ALU_LEA,
        ALU_MOVZX, ALU_MOVSX, ALU_SETCC, ALU_BITDISP, ALU_BT, ALU_BTS, ALU_BTR, ALU_BTC,
        ALU_BSF, ALU_BSR, ALU_DAA, ALU_DAS, ALU_AAA, ALU_AAS, ALU_SALC, ALU_FLAGS, ALU_SIGN
    } alu_op_e;

    // ALU_FLAGS, in imm[3:0]. LOAD writes the flags from src2 (POPF, IRET),
    // LOAD_RF RF too (IRETD); READ gives FLAGS' image (PUSHF), READ_INT too,
    // clearing IF and TF (a real-mode INT). READ_PM is a protected-mode
    // delivery's EFLAGS image, RF (bit 16) from imm[4], and clears TF and NT,
    // and IF unless imm[5] says the gate is a trap gate. READ_TSS is a task
    // switch's image of EFLAGS for the old TSS: all of it, VM too, RF from
    // imm[4] and NT cleared by imm[6] (IRET's). LOAD_TSS writes every flag
    // from src2, IOPL, IF, NT, RF and VM included, NT set too by imm[6] (a
    // nesting switch: CALL, an interrupt or an exception).
    typedef enum logic [3:0] {
        FLAG_CLC, FLAG_STC, FLAG_CMC, FLAG_CLI, FLAG_STI, FLAG_CLD, FLAG_STD, FLAG_SAHF, FLAG_LAHF,
        FLAG_LOAD, FLAG_READ, FLAG_READ_INT, FLAG_LOAD_RF, FLAG_READ_PM, FLAG_READ_TSS, FLAG_LOAD_TSS
    } flag_op_e;

    // UOP_SHIFT. The first eight follow the x86 group-2 order.
    typedef enum logic [4:0] {
        SHIFT_ROL, SHIFT_ROR, SHIFT_RCL, SHIFT_RCR, SHIFT_SHL, SHIFT_SHR, SHIFT_SAL, SHIFT_SAR,
        SHIFT_SHLD, SHIFT_SHRD
    } shift_op_e;

    // UOP_SEGMENT: a real-mode or V86 load of the selector in src2, which
    // sets the base to selector * 16; or ZF, the result of LAR, LSL, VERR,
    // VERW or ARPL: ZF from imm[0], and the destination takes src2 when it
    // is set, else keeps src1 (fes_i386_cpu, "System segments"); or TSEL, a
    // task switch's selector alone, from src2 or its load, the segment's
    // cache kept and every access refused until the new task's descriptor
    // loads it (fes_i386_cpu, "Task switches").
    typedef enum logic [4:0] { SEGOP_LOAD_REAL, SEGOP_ZF, SEGOP_TSEL } segment_op_e;

    // UOP_SPECIAL.
    typedef enum logic [4:0] { SPECIAL_NOP, SPECIAL_HLT } special_op_e;

    // UOP_SYSTEM: a write at WB of a register the decode or the address
    // path reads. Each but GATE is a jump to the next instruction
    // (fes_i386_exctl b_jmp), so every instruction after it decodes and
    // reads it anew. CLTS clears CR0.TS; CR0, CR2 and CR3 take T1 (MOV CRn,
    // r32); LMSW takes T1's PE, MP, EM and TS, and cannot clear PE; GDTR and
    // IDTR take T0 as the limit and T1 as the base, its top byte cleared at
    // a 16-bit size (LGDT, LIDT); LDTR and TR take T0's descriptor (LLDT,
    // LTR). GATE splits a protected-mode delivery's gate, T1 and T3: T0 the
    // gate's selector, T1 its offset, and the gate's size and kind for the
    // pushes (fes_i386_cpu, "Protected-mode delivery"); with imm[0] a far
    // CALL's call gate, T2 and T3, and its parameter count. DR takes T1
    // into the debug register imm[2:0] names (MOV DRn, r32; DR4 is DR6,
    // DR5 DR7). A task switch's (fes_i386_cpu, "Task switches") do not jump
    // (sys_nojmp, as GATE): TSTR, TR from T0's descriptor, busy; TSLDT,
    // LDTR's selector from T5, its cache not valid; TSLDTD, LDTR from T0's
    // descriptor (the new task's LDT, after its checks).
    typedef enum logic [4:0] {
        SYSOP_CLTS = 5'd0, SYSOP_CR0 = 5'd1, SYSOP_LMSW = 5'd2, SYSOP_CR2 = 5'd3, SYSOP_CR3 = 5'd4,
        SYSOP_GDTR = 5'd5, SYSOP_IDTR = 5'd6, SYSOP_LDTR = 5'd7, SYSOP_TR = 5'd8, SYSOP_GATE = 5'd9,
        SYSOP_DR = 5'd10, SYSOP_TSTR = 5'd12, SYSOP_TSLDT = 5'd13, SYSOP_TSLDTD = 5'd14
    } system_op_e;
    // The system uops that do not jump to the next instruction.
    function automatic logic sys_nojmp(input logic [4:0] op);
        sys_nojmp = op == 5'(SYSOP_GATE) || op[4:2] == 3'b011;
    endfunction

    // UOP_MUL and UOP_DIV, in EX's multiply and divide unit (fes_i386_muldiv).
    // HI writes the high half a two-destination MUL or DIV kept (EDX, DX);
    // LOAD gives a 16- or 32-bit DIV its dividend's high half (EDX), and
    // carries the divide's signedness in imm[0]; IMUL3 is IMUL r, r/m, imm,
    // whose multiplier is imm and multiplicand src2.
    typedef enum logic [4:0] {
        MULDIV_UNSIGNED, MULDIV_SIGNED, MULDIV_AAM, MULDIV_AAD, MULDIV_HI, MULDIV_LOAD, MULDIV_IMUL3
    } muldiv_op_e;

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

    // A system register D reads as src2's value (decoded_t b_sys): MOV r32,
    // CRn, SMSW, SGDT, SIDT, SLDT and STR; a delivery's error code (ERR);
    // all ones, for the fields an absent 387 supplies (ONES); the x87
    // instruction and data pointers' words of FNSTENV's and FNSAVE's
    // layouts (FIP, FCS, FDP, FDS; fes_i386_cpu, "x87 pointers"); ARPL's
    // adjusted selector (from T1 and T2). LAR, LSL, VERR and VERW name
    // their result here; M1 puts it in from the staged descriptor. Those
    // from FIP on are read in D behind the uops that write them (t_rd).
    // MOV r32, DRn reads DR0-DR3, DR6 (DR4 too) and DR7 (DR5 too).
    typedef enum logic [4:0] {
        SYS_RD_CR0, SYS_RD_CR2, SYS_RD_CR3, SYS_RD_GDTL, SYS_RD_GDTB, SYS_RD_IDTL, SYS_RD_IDTB, SYS_RD_LDTR,
        SYS_RD_TR, SYS_RD_ERR, SYS_RD_ONES, SYS_RD_FIP, SYS_RD_FCS, SYS_RD_FDP, SYS_RD_FDS, SYS_RD_ARPL,
        SYS_RD_LAR, SYS_RD_LSL, SYS_RD_VERR, SYS_RD_VERW, SYS_RD_DR0, SYS_RD_DR1, SYS_RD_DR2, SYS_RD_DR3,
        SYS_RD_DR6, SYS_RD_DR7
    } sys_rd_e;

    // The x87 pointers an ESC instruction's last uop writes as it retires
    // (uop_t x87p): none (the control forms), the instruction pointer (CS
    // and EIP), or both it and the data pointer (the memory operand's
    // segment selector and offset).
    typedef enum logic [1:0] { X87P_NONE, X87P_IP, X87P_IPDP } x87p_e;

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
    // DE is the divide error EX finds (vector 0), BR BOUND's range check
    // (vector 5), which EX finds too. REPLAY is not architectural either:
    // an uncached load that was not the oldest uop when it reached M2
    // (fes_i386_l1d), which WB restarts from its instruction instead of
    // delivering anything. GP_SEL, NP_SEL and SS_SEL are #GP, #NP and #SS
    // with the selector in T0 as their error code (a protected-mode segment
    // load's checks, M1). GP_IDT and NP_IDT are #GP and #NP with the
    // vector's IDT entry as their error code, vector * 8 + 2 (a delivery's
    // gate checks). TS_SEL and TS_TR are #TS with T0's selector (a delivery's
    // new SS) or TR's (its stack's slots past the TSS's limit).
    // PF is a page fault (vector 14) with the error code and CR2's linear
    // address the uop carries (pipe_t pf_err, pf_lin), from the walker
    // (fes_i386_l1d). DB is a debug exception (vector 1), a fault: a MOV
    // to or from DRn while DR7.GD is set, which sets DR6.BD and clears GD as
    // it is taken.
    typedef enum logic [4:0] {
        FAULT_NONE          = 5'd0,
        FAULT_UD            = 5'd1,
        FAULT_GP0           = 5'd2,
        FAULT_NM            = 5'd3,
        FAULT_UNIMPLEMENTED = 5'd4,
        FAULT_SS0           = 5'd5,
        FAULT_DE            = 5'd6,
        FAULT_BR            = 5'd7,
        FAULT_REPLAY        = 5'd8,
        FAULT_GP_SEL        = 5'd9,
        FAULT_NP_SEL        = 5'd10,
        FAULT_SS_SEL        = 5'd11,
        FAULT_GP_IDT        = 5'd12,
        FAULT_NP_IDT        = 5'd13,
        FAULT_TS_SEL        = 5'd14,
        FAULT_TS_TR         = 5'd15,
        FAULT_PF            = 5'd16,
        FAULT_DB            = 5'd17
    } fault_e;

    // Why an instruction stops as not built (FAULT_UNIMPLEMENTED), kept
    // with its uop (uop_t why) for trace_unbuilt: an encoding or form this
    // build does not decode; a far JMP through a call gate; a protected-mode
    // 16-bit stack access past 64 KiB under a limit that reaches there.
    typedef enum logic [3:0] {
        WHY_ENCODING, WHY_GATE, WHY_STACK64K
    } why_e;

    typedef struct packed {
        logic [3:0] cond;             // x86 condition code (tttn) for Jcc, SETcc
        logic       predict_taken;
        logic       end_of_instruction;
        logic [3:0] ilen;             // x86 instruction length, on its last uop
        fault_e     fault;
        why_e       why;              // FAULT_UNIMPLEMENTED's reason
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
    // probe: the uop reads the lowest slot of INT's three pushes, and M1
    // also faults when they cross the 64 KiB wrap misaligned, so INT writes
    // nothing before it can fault. EX then checks the vector's offset, in
    // T1, against CS's limit, as Bochs does after the pushes.
    // eoi_taken: the uop ends the instruction when it is a taken branch
    // (INTO's JNO, past which the rest of the routine is the interrupt).
    // A string element's last uop steps ESI, EDI and ECX at WB (str_si,
    // str_di, str_cx), by its access's bytes down or up (str_down), wrapped
    // by address size; st2: it also stores its result at pipe_t target (a
    // MOVS element, ENTER's copy of a frame pointer); rep_stop: REPE (bit
    // 0) or REPNE (bit 1) ends the instruction here, a taken branch to the
    // next, when ZF is clear or set.
    // io: the uop is an I/O access at WB (io_e): IN merges the port's data
    // into its destination (EAX) or stores it (INS); OUT sends a (OUT) or its
    // result, the loaded value (OUTS). The port is src2's low half (DX), else
    // imm's. iret: IRET's last uop, which unblocks NMI as it retires.
    // bound: BOUND's compare, which raises #BR at EX when x is below y,
    // signed, and writes no flags.
    // A protected-mode segment load (microcode, fes_i386_cpu "Protected
    // mode"): desc, the memory operand is the descriptor T0's selector
    // names (desc_e), or a delivery's IDT gate; segchk, M1 raises the staged
    // descriptor's checks (segchk_e); seg2_pm, the second destination takes
    // T0's descriptor rather than the real-mode rule; shadow, the uop ends a
    // MOV SS or POP SS, after which no interrupt is taken. nwrap: a
    // protected-mode far CALL's, RETF's or IRET's stack slot on a 16-bit
    // stack, at SP plus or less sdisp without the 64 KiB wrap, as Bochs
    // reads and writes it (M1). ntchk: a protected-mode IRET's first uop,
    // which waits in D for NT to be committed and replays when it is not
    // what DC decoded by (fes_i386_cpu, nt_busy). v86 (fes_i386_cpu, "V86 mode"): a real-mode segment load gives
    // the segment V86's cache (limit FFFFh, access rights F3h) whatever the
    // mode; the far jump into V86 mode (no xfer) sets VM and CPL 3 and
    // loads ESP from T5; the far jump out of it (xfer) clears VM and nulls
    // ES, DS, FS and GS. x87p: the uop is an ESC instruction's last, which
    // writes the x87 pointers as it retires (x87p_e).
    // A privilege transition (fes_i386_cpu, "Privilege transitions"): hold,
    // WB keeps the staged CS descriptor and whether the transfer changes
    // privilege (xp); xfer, the far jump writes CS from that hold, and with
    // xp SS from the staged descriptor and ESP from T5; nstk, the access is
    // to the new stack (the staged SS's base and checks, at T5 plus its
    // slot), whose fault is #SS(selector); sflt, the fault a system read's
    // check raises (sflt_e).
    // A task switch (fes_i386_cpu, "Task switches"): ntset, IRET's first
    // uop for a return from a nested task (NT as DC saw it, which D checks
    // with ntchk); tcom, the switch's commit,
    // a far jump to the new task's EIP after which its descriptors load;
    // tscpl, the uop that loads the new task's SS, after which the CPL is
    // CS's RPL.
    typedef struct packed {
        logic        ntset;
        logic        tcom;
        logic        tscpl;
        logic        hold;
        logic        xfer;
        logic        nstk;
        logic [1:0]  sflt;
        logic [1:0]  x87p;
        logic [3:0]  desc;
        logic [4:0]  segchk;
        logic        nwrap;
        logic        v86;
        logic        ntchk;
        logic        seg2_pm;
        logic        shadow;
        logic        bound;
        logic        bound_pair;  // first BOUND memory access: validate both slots before reading
        logic        iret;
        logic [1:0]  io;
        logic        str_si;
        logic        str_di;
        logic        str_cx;
        logic        str_down;
        logic        st2;
        logic [1:0]  rep_stop;
        logic        eoi_taken;
        logic        probe;
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

    // ---- Memory -------------------------------------------------------------------
    // Guest physical addresses are PA bits wide (64 MiB of RAM at most); a
    // line is 32 bytes. The attribute map (docs/i386-cpu-design.md, "Memory
    // types"): RAM, read-only RAM (cacheable, writes dropped), uncached MMIO,
    // or a hole (reads FFh, writes dropped).
    localparam int PA = 26;
    typedef enum logic [1:0] { ATTR_RAM, ATTR_ROM, ATTR_MMIO, ATTR_HOLE } attr_e;

    // A TLB entry (fes_i386_tlb): a linear page, its physical page as phys
    // gives it (above bit and PA bits 25:12, A20M applied), that page's
    // attribute, and the rights the PDE and PTE give together (U/S, R/W)
    // with the PTE's D bit. The walker fills it (fes_i386_l1d); A is set in
    // both entries of every page an entry holds.
    typedef struct packed {
        logic [19:0]    lp;
        logic [PA-12:0] pp;
        attr_e          at;
        logic           u;
        logic           w;
        logic           d;
    } tlb_entry_t;

    // The physical address of a linear one with paging off: A20M masks bit 20,
    // and FFFF0000h-FFFFFFFFh reads the F0000h shadow (the reset fetch).
    // above: the address is past PA bits, a hole unless it is that alias.
    function automatic logic [PA:0] phys(input logic [31:0] linear, input logic a20m);
        if (linear[31:16] == 16'hffff) phys = {1'b0, 10'h00f, linear[15:0]};
        else phys = {|linear[31:PA], linear[PA-1:21], linear[20] && !a20m, linear[19:0]};
    endfunction

    // The attribute of a physical address (with phys's above bit): 640 KiB to
    // 1 MiB by the machine's map, 16 KiB a slot from A0000h; RAM elsewhere
    // below ram_top MiB.
    function automatic attr_e attr(input logic [PA:0] p, input logic [23:0][1:0] upper, input logic [6:0] ram_top);
        if (p[PA] || {1'b0, p[PA-1:20]} >= ram_top) attr = ATTR_HOLE;
        else if (p[PA-1:20] != '0 || p[19:14] < 6'd40) attr = ATTR_RAM;
        else attr = attr_e'(upper[5'(p[19:14] - 6'd40)]);
    endfunction

    // ---- Segments ---------------------------------------------------------------

    // A segment's access check, worked out when it is loaded so M1's check
    // is one compare, or two for an expand-down segment: from its limit and
    // its access rights (descriptor bits 8-23: type, S, DPL, P, AVL, D/B, G),
    // as Bochs's read_virtual_checks and write_virtual_checks make it.
    //   lim[k], tiny[k]: for an access of 1, 2 or 4 bytes (k 0, 1, 2), the
    //     last offset it may start at, and whether there is none: the limit
    //     less 0, 1 or 3, or for an expand-down segment its upper bound
    //     (FFFFh, or FFFFFFFFh with D/B) so less;
    //   low, expd: an expand-down segment faults at offsets up to its limit;
    //   rbad, wbad: the segment refuses reads (not present, a system
    //     segment, execute-only code) or writes (not present, a system
    //     segment, code, read-only data).
    typedef struct packed {
        logic [2:0][31:0] lim;
        logic [2:0]       tiny;
        logic [31:0]      low;
        logic             expd;
        logic             rbad;
        logic             wbad;
    } seg_chk_t;
    function automatic seg_chk_t seg_chk(input logic [31:0] limit, input logic [15:0] ar);
        seg_chk_t c;
        logic [31:0] upper;
        c.expd = ar[4] && !ar[3] && ar[2];
        upper = ar[14] ? 32'hffff_ffff : 32'h0000_ffff;
        c.lim[0] = c.expd ? upper : limit;
        c.lim[1] = c.expd ? upper - 32'd1 : limit - 32'd1;
        c.lim[2] = c.expd ? upper - 32'd3 : limit - 32'd3;
        c.tiny = c.expd ? 3'b000 : {limit < 32'd3, limit < 32'd1, 1'b0};
        c.low = limit;
        c.rbad = !ar[7] || !ar[4] || ar[3] && !ar[1];
        c.wbad = !ar[7] || !ar[4] || ar[3] || !ar[1];
        return c;
    endfunction

    // ---- Pipeline ---------------------------------------------------------------

    // What the speed governor charges an instruction by (fes_i386_governor),
    // taken from Q's bytes in DC and carried by each of its uops to WB: the
    // opcode (two: after 0Fh), the ModRM byte's reg field (mreg) and whether its
    // operand is a register (mod 3), the operand size, a REP prefix, and an
    // address of base, index and displacement (ea3). The ModRM fields mean
    // nothing for an opcode without one; the governor's classes ignore them.
    typedef struct packed {
        logic       two;
        logic [7:0] op;
        logic [2:0] mreg;
        logic       rm_reg;
        logic       o32;
        logic       rep;
        logic       ea3;
    } gov_t;

    // An instruction decoded in DC, as D takes it (fes_i386_decode and
    // fes_i386_amode on Q's output register; fes_i386_cpu).
    typedef struct packed {
        gov_t        gov;
        uop_t        uop;
        logic [3:0]  length;
        logic        opsize32;
        logic        lea;
        logic        lea_early;
        // The early ALU (fes_i386_cpu, "The early ALU"): an ADD, INC, DEC or
        // MOV of registers and an immediate, whose result AG's adder gives
        // from has_base, base, has_index, index and lea_disp.
        logic        ealu;
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
        // A stack slot at ESP plus slot operand-sized slots, ESP unmoved
        // (STK_PEEK): -16 to 15 from the ROM, -8 to 7 from the decode,
        // further for a call gate's parameters (fes_i386_seq).
        logic [6:0]  slot;
        logic        a_t1;       // src1's value is T1's (POPAD's ESP), which waits as far_t does
        logic        b_t1;       // src2's value is T1's, which waits the same way
        logic        disp_t1;    // the address's displacement is T1, far_off's with far_t (SEQ_BTM)
        logic        t_rd;       // the uop reads T0 or T1 (far_t, a_t1, b_t1)
        logic        popad_esp;  // the uop counts only for POPAD on a 16-bit stack
        // The ModRM memory operand's address registers and segment, kept
        // while a stack uop holds the address fields (POP r/m's store).
        logic        m_has_base;
        logic [2:0]  m_base;
        logic        m_has_index;
        logic [1:0]  m_scale;
        logic [2:0]  m_seg;
        // POP m's store when its address reads ESP, which the 386 reads
        // after the pop (SEQ_POPM): esp_adj, the operand size scaled as ESP
        // is (by an invalid SIB's scale, else 1), which D adds to the
        // displacement; esp_wrap, that scale, of 10000h, which D takes off
        // when a 16-bit stack's SP wraps (fes_i386_cpu).
        logic [5:0]  esp_adj;
        logic [3:0]  esp_wrap;
        logic        index8;     // the index is its register's low byte, zero-extended (XLAT's AL)
        // The string engine (SEQ_STRING, SEQ_ENTER): D's copies of the next
        // source and destination offsets and of the count (fes_i386_cpu).
        // str_load: the step loads them; str_a: the address is the source
        // copy, which steps; str_b: ... the destination copy, which steps;
        // str_two: the uop also stores at the destination copy (st2), which
        // steps; str_fin: ENTER's last, at the destination copy plus imm;
        // str_cnt: the count steps down. b_fp: src2's value is ENTER's frame
        // pointer copy. x_prev: src1's value is the result of the uop just
        // before, one stage ahead (a CMPS element's first load).
        logic [2:0]  str_op;     // str_e
        logic [1:0]  str_rep;    // rep_e: the instruction's REP prefix
        logic [4:0]  ecnt;       // ENTER's nesting level, mod 32
        logic        str_load;
        logic        str_a;
        logic        str_b;
        logic        str_two;
        logic        str_fin;
        logic        str_cnt;
        logic        b_fp;
        logic        x_prev;
            // src2's value is the far pointer's selector (b_far: a protected-mode
        // far jump's T0), the system register sys_idx names (b_sys,
        // sys_rd_e), or T4 (b_t4, IRET's EFLAGS image). moff: the memory
        // operand is moff bytes past the ModRM operand's, wrapped by address
        // size (LGDT's and SGDT's base, 2; a part of an x87 operand, or a
        // field of an x87 environment or save image). nochk: the uop steps ESP after a
        // protected-mode POP Sreg, with no access and no limit check, as
        // Bochs pops after the load.
        // A protected-mode delivery's uops (fes_i386_cpu, "Protected-mode
        // delivery"): gsz, the operand size is the gate's (16 or 32 bits),
        // not the instruction's; errp, the error code's slot, whose probe
        // and store a delivery without one drops, and whose jump steps ESP
        // one slot less then. dmode: how a descriptor read treats its
        // selector (dmode_e).
        logic        b_far;
        logic        b_sys;
        logic        b_t4;
        logic [4:0]  sys_idx;
        logic [6:0]  moff;
        logic        nochk;
        logic        gsz;
        logic        errp;
        logic [1:0]  dmode;
        // A privilege transition's uops: xc, when it issues (xc_e); xs, a
        // delivery's push or probe, which with xp moves two slots down onto
        // the new stack (nstk); tss, a read of the new stack from the TSS.
        // A call gate's parameter copy (fes_i386_seq): pm, the word's part
        // (pm_e); pk, the parameter the copy is at; pdrop, the copy is past
        // the gate's count, a NOP. zx: an old SS's ESP or the return EIP,
        // stored as SP or IP from a 16-bit stack or code segment. pimm: the
        // stack slot is past the instruction's immediate too (RETF imm16's
        // SS and ESP slots).
        logic [3:0]  xc;
        logic        xs;
        logic [2:0]  tss;
        logic [1:0]  pm;
        logic [4:0]  pk;
        logic        pdrop;
        logic        zx;
        logic        pimm;
        // V86 mode (fes_i386_cpu, "V86 mode"): vgo, a protected-mode IRET's
        // word that, for an IRETD at CPL 0 whose EFLAGS image (T4) has VM
        // set, is a NOP, as is the word after it, after which the ROM goes
        // on at the return to V86 mode (UC_V86RET, fes_i386_seq).
        logic        vgo;
        // A task switch (fes_i386_cpu, "Task switches"): tgo and tgk, the
        // ROM word's (ucode_t); tsk, the switch's kind (tsk_e), set as the
        // ROM goes on at it, which its xc reads; tsf, a TSS access's field.
        logic        tgo;
        logic [1:0]  tgk;
        logic [1:0]  tsk;
        logic [4:0]  tsf;
        // tsw: the uop is a task switch's (from its tgo word's successor,
        // IRET's UC_TSSAVE, or the commit's tsload), which issues serially
        // under paging, so none replays (fes_i386_cpu, "Task switches").
        logic        tsw;
        // Which of ESI, EDI and ECX (bits 0-2) the uop reads, as a source,
        // a CL count or an address register (str_reads): D holds it while
        // a string element that steps one is in flight.
        logic [2:0]  rd_str;
        // A microcode routine (SEQ_UCODE): the ROM address of its next uop.
        logic [8:0]  upc;
        // A fixed uop sequence (SEQ_*), the step dc holds, and whether
        // another uop follows (fes_i386_seq).
        logic [4:0]  seq;
        logic [2:0]  step;
        logic        more;
        // The fetch (fetched_t): the instruction's linear address, and its
        // BTB fields. fetched: it came from Q, not an exception's entry.
        logic        fetched;
        logic [31:0] lin;
        logic        bm;
        logic        bway;
        logic [1:0]  bctr;
        logic        tk;
        logic        xbad;
        logic [31:0] ptgt;
        logic [7:0]  pp0;
        logic [7:0]  pp1;
        logic        tm0;
        logic        tm1;
        logic        ts0;
        logic        ts1;
    } decoded_t;

    // Which of ESI, EDI and ECX (bits 0-2) a uop reads: src1, src2, a CL
    // count, or its address registers when it has an address (lea, or a
    // memory operand).
    function automatic logic [2:0] str_reads(input uop_t u, input logic lea, input logic has_base,
                                             input logic [2:0] base, input logic has_index, input logic [2:0] index);
        logic a;
        logic [2:0] r;
        a = lea || u.mem.kind != MEM_NONE;
        for (int k = 0; k < 3; k++) begin
            r[k] = u.src1.valid && u.src1.index[2:0] == 3'(k == 2 ? 1 : 6 + k)
                || u.src2.valid && u.src2.index[2:0] == 3'(k == 2 ? 1 : 6 + k)
                || a && (has_base && base == 3'(k == 2 ? 1 : 6 + k) || has_index && index == 3'(k == 2 ? 1 : 6 + k));
        end
        r[2] = r[2] || u.count_cl;
        return r;
    endfunction

    // STK_PEEK reads or writes a stack slot without moving ESP (RETF's
    // loads, PUSHA's and POPA's slots); one that writes ESP (sp_write) steps
    // it past eight slots, down from a slot below ESP, up from one above.
    // STK_RETF steps ESP at a far transfer's jump by its slot of
    // operand-sized slots (RETF's 2, IRET's 3, a protected-mode CALL's -2,
    // a delivery's -3 or -4) plus imm; STK_PROBE reads the lower slot of a
    // far call's two pushes, or INT's three, only for its limit check.
    typedef enum logic [2:0] { STK_NONE, STK_PUSH, STK_POP, STK_RET_IMM, STK_LEAVE, STK_PEEK, STK_RETF, STK_PROBE } stk_e;
    typedef enum logic [4:0] {
        SEQ_NONE, SEQ_CALL, SEQ_LPTR, SEQ_CALLF, SEQ_RETF, SEQ_JMPFM, SEQ_CALLFM, SEQ_UCODE,
        SEQ_XCHG, SEQ_XCHGM, SEQ_CALLI, SEQ_PUSHM, SEQ_POPM, SEQ_MUL, SEQ_DIV, SEQ_STRING, SEQ_ENTER,
        SEQ_BTM
    } seq_e;
    // The string instructions (SEQ_STRING's str_op).
    typedef enum logic [2:0] { STR_MOVS, STR_CMPS, STR_STOS, STR_LODS, STR_SCAS, STR_INS, STR_OUTS } str_e;
    // A uop's I/O access (uop_t io).
    typedef enum logic [1:0] { IO_NONE, IO_IN, IO_OUT } io_e;
    typedef enum logic { PROBE_NONE, PROBE_INT } probe_e;
    // A protected-mode memory operand's part of a descriptor (uop_t desc):
    // T0's selector's low dword, high dword, access byte (the accessed bit's
    // store) or high dword again as a store (LTR's busy bit); a delivery's
    // IDT gate, its low and high dword, at IDTR's base plus the vector times
    // 8.
    // DESC_HACC is the held CS descriptor's access byte (a privilege
    // transition's accessed bit, after its pushes). DESC_TRHI is TR's
    // descriptor's high dword, read, and DESC_UNBUSY the same stored from T4
    // with the busy bit clear (a task switch's old TSS).
    typedef enum logic [3:0] {
        DESC_NONE, DESC_LO, DESC_HI, DESC_ACC, DESC_IDT_LO, DESC_IDT_HI, DESC_BUSY, DESC_HACC, DESC_TRHI, DESC_UNBUSY
    } desc_e;
    // The fault a system read past its limit raises (uop_t sflt): #GP with
    // the selector (a descriptor's), #TS with it (a delivery's new SS), #TS
    // with TR's (the TSS's stack slots).
    typedef enum logic [1:0] { SFLT_GP, SFLT_TS, SFLT_TR, SFLT_GP0 } sflt_e;
    // A privilege transition's conditional uops (decoded_t xc): always;
    // only when the transfer changes privilege (xp); only when it does not;
    // and for a protected-mode far CALL, only to a code segment (direct),
    // only through a call gate (gate), only through one to the same
    // privilege (gsame). A task switch's
    // (fes_i386_cpu, "Task switches"), by its kind (decoded_t tsk): nest,
    // CALL, an interrupt or an exception; unnest, JMP or IRET; iret; by
    // TR's TSS (the old one until TSTR, the new after): t32, a 386 TSS; t16,
    // a 286 one; t32pg, a 386 one with paging on; err, a delivery with an
    // error code; nnull, T0's selector is not null.
    typedef enum logic [3:0] {
        XC_ALL, XC_XP, XC_SAME, XC_DIRECT, XC_GATE, XC_GSAME,
        XC_NEST, XC_UNNEST, XC_IRET, XC_T32, XC_T16, XC_T32PG, XC_ERR, XC_NNULL
    } xc_e;
    // A task switch's kind (decoded_t tsk).
    typedef enum logic [1:0] { TSK_JMP, TSK_CALL, TSK_INT, TSK_IRET } tsk_e;
    // A call gate's parameter copy (decoded_t pm): a parameter's load from
    // the old stack, at slot count - 1 - pk; its store to the new stack, at
    // slot -3 - pk, after which the copy goes back to its load until it has
    // copied count parameters; a slot below them, the word's slot less the
    // count (with xp; also the far jump's step of ESP).
    typedef enum logic [1:0] { PM_NONE, PM_LOAD, PM_STORE, PM_BELOW } pm_e;
    // A read from the TSS (decoded_t tss): a delivery's new stack, ESP
    // (checked against TR's limit) and SS; an I/O access's permission
    // bitmap base, at 66h, and its bitmap's word for the port (T5) at that
    // base (T4) plus the port's byte, checked against TR's limit. A task
    // switch's field (decoded_t tsf) of the TSS TR holds (FLD), or of the
    // new TSS, the staged descriptor's (NFLD), in that TSS's layout
    // (fes_i386_cpu, "Task switches").
    typedef enum logic [2:0] { TSS_NONE, TSS_ESP, TSS_SS, TSS_IOMAP, TSS_BITMAP, TSS_FLD, TSS_NFLD } tss_e;
    // The staged descriptor's checks M1 raises (uop_t segchk;
    // fes_i386_cpu "Protected mode: a segment load's descriptor"): a data
    // segment's load, SS's, a far JMP's CS, a delivery's gate, a delivery's
    // or call gate's CS, a far CALL's CS or call gate, IRET's or RETF's CS,
    // LLDT's descriptor, LTR's; an outer return's new SS, an inner
    // transfer's new SS, the offset against the held CS's limit; an I/O
    // access's bits in the permission bitmap's word (T2) for the port (T5);
    // a delivery from V86 mode's CS as a delivery's, which must also be
    // nonconforming code below CPL 3, and that CS's DPL, which must be 0
    // (fes_i386_cpu, "V86 mode").
    // A task switch's (fes_i386_cpu, "Task switches"): the new TSS's limit
    // and the old one's (TSW); a task gate's TSS (TSGATE); IRET's back
    // link's (TSRET); the new task's LDT, SS, CS and data segments; the old
    // TSS's limit alone, ahead of IRET's save (TSOLD).
    typedef enum logic [4:0] {
        CHK_NONE, CHK_DATA, CHK_SS, CHK_JMP, CHK_GATE, CHK_INTCS, CHK_CALL, CHK_RET, CHK_LDT, CHK_TSS,
        CHK_SSRET, CHK_SSINT, CHK_HOFF, CHK_IOBM, CHK_V86CS, CHK_V86DPL,
        CHK_TSW, CHK_TSGATE, CHK_TSRET, CHK_TSLDT, CHK_TSSS, CHK_TSCS, CHK_TSDATA, CHK_TSOLD
    } segchk_e;
    // How a descriptor read treats its selector (decoded_t dmode): load and
    // check it; LAR, LSL, VERR and VERW, which read nothing for a selector
    // past its table and raise nothing; LLDT and LTR, which read nothing for
    // TI=1 (their check raises #GP).
    // DM_TS: a delivery's new SS, whose read past its table raises #TS.
    typedef enum logic [1:0] { DM_LOAD, DM_PROBE, DM_GDT, DM_TS } dmode_e;

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
        logic [4:0]  slot;       // -16 to 15
        logic        ivt;        // the interrupt vector's entry, at linear vector * 4
        logic        sel_part;   // ... its selector, at + 2
        logic        probe;      // probe_e
        logic [2:0]  b;          // UC_B: src2, the next EIP, segment seg's selector, the far selector, T1, a system register
        logic [2:0]  seg;
        logic        far_jmp;
        logic        far_chk;
        logic        far_t;
        logic        sp_add;
        logic        sp_step;    // a STK_PEEK uop writes ESP, past its eight slots
        logic        a_t1;       // src1's value is T1's
        logic        popad_esp;  // kept only for POPAD on a 16-bit stack
        logic        rd_cf;
        logic        rd_oszap;
        logic        wr_cf;
        logic        wr_oszap;
        logic        iret;       // IRET's last uop (uop_t iret)
        logic        rg;         // s1_idx and s2_idx are the instruction's register, which its first uop has in src1
        logic        rm;         // the memory operand is the instruction's ModRM operand, not a stack slot
        logic        bound;      // BOUND's compare (uop_t bound)
        logic [3:0]  desc;       // uop_t desc (desc_e)
        logic [4:0]  segchk;     // uop_t segchk (segchk_e)
        logic        segw;       // the uop writes segment register seg2 from T0's descriptor (uop_t seg2_pm)
        logic        shadow;     // uop_t shadow, when the segment is SS
        logic        nochk;      // ESP's step after a POP Sreg: no access, no limit check
        logic        plus2;      // the address is the ModRM operand's plus 2 (LGDT's and SGDT's base)
        logic [2:0]  fpn;        // ... plus fpn operand-sized fields (an x87 environment's)
        logic [4:0]  fpj;        // ... plus fpj dwords (an x87 save image's registers, an x87 operand's part)
        logic [1:0]  x87p;       // uop_t x87p
        logic [4:0]  sys;        // b = "sys": the system register sys_rd_e names
        logic [1:0]  dmode;      // decoded_t dmode
        logic        gsz;        // decoded_t gsz: the gate's operand size
        logic        errp;       // decoded_t errp: the error code's slot
        logic        nwrap;      // uop_t nwrap
        logic        vgo;        // decoded_t vgo
        logic        iimm;       // imm is the instruction's (RETF imm16's), not the word's
        logic        hold;       // uop_t hold
        logic        xfer;       // uop_t xfer
        logic        nstk;       // uop_t nstk
        logic [3:0]  xc;         // decoded_t xc
        logic        xs;         // decoded_t xs
        logic [2:0]  tss;        // decoded_t tss
        logic [1:0]  pm;         // decoded_t pm
        logic        zx;         // decoded_t zx
        logic        pimm;       // decoded_t pimm
        logic        xend;       // the uop ends the routine when its xc keeps it (fes_i386_seq)
        logic        iof;        // the instruction's I/O access, which its first uop keeps (fes_i386_seq)
        logic        v86;        // uop_t v86
        // A task switch (fes_i386_cpu, "Task switches"): tgo, a far JMP's
        // or CALL's descriptor check, or a delivery's gate, after which the
        // ROM goes on at the switch for a TSS or a task gate, of kind tgk
        // (decoded_t tsk); tsf, the TSS field the word's tss access names;
        // tcom, uop_t tcom; wsg, segw writes the word's seg, not the
        // instruction's; tscpl, uop_t tscpl.
        logic        tgo;
        logic [1:0]  tgk;
        logic [4:0]  tsf;
        logic        tcom;
        logic        wsg;
        logic        tscpl;
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
        logic       r_flags;                   // FLAGS' image (PUSHF, INT's push), with RF from imm[4] (f_clrpm)
        logic       r_zf;                      // SEGOP_ZF: src2 when imm[0] is set, else src1, and ZF from imm[0]
        logic       l_or, l_xor;               // the logic unit; AND otherwise
        logic       f_add;                     // CF comes from the adder
        logic       f_arith;                   // OF, SF, ZF, AF and PF come from the adder
        logic       f_logic, f_shift;          // ... from the logic unit; from the shift, at WB
        logic       f_cf, f_if, f_df, f_szap;  // the flag instructions (fl_* the value)
        logic       f_load;                    // the writable flags from y (POPF, IRET)
        logic       f_rf;                      // ... and RF (IRETD); every other uop clears RF, but IRET's last
        logic       f_clrit;                   // IF and TF cleared (INT)
        logic       f_clrpm;                   // TF and NT cleared, and IF unless imm[5] (a protected-mode delivery)
        logic       f_rdtss;                   // READ_TSS's image: VM, RF from imm[4], NT cleared by imm[6]
        logic       f_ldtss;                   // every flag from y, NT set by imm[6] (a task switch's new EFLAGS)
        logic       fl_cf, fl_cmc, fl_set;
        // BT, BTS, BTR and BTC: the flags (f_bt) and result (r_bt) of the bit
        // the shifter's count numbers, which BTS ORs in (bt_or), BTR clears
        // (bt_andn) and BTC flips (fes_i386_ex). r_btd: BITDISP's result.
        logic       f_bt, r_bt, bt_or, bt_andn, r_btd;
        logic       r_scan, bsr;               // BSF, BSR: the result and flags of the scan of y
        // DAA, DAS, AAA and AAS: the result and flags of AL's adjust
        // (fes_i386_ex); bcd_sub: DAS, AAS; bcd_ax: AAA, AAS.
        logic       r_bcd, bcd_sub, bcd_ax;
        logic       b_jcc, b_jmp, b_loop, b_loope, b_loopne, b_jcxz;
        logic       b_rpe, b_rpne;             // a string element ends REPE, REPNE (uop rep_stop)
        logic       b_ind;                     // RET, JMP r/m: the target is x (CS's base) + y (the EIP)
        logic       bound;                     // BOUND's compare: #BR when x is below y, signed (uop bound)
        // The result (md) and the flags (md_fl) come from the multiply and
        // divide unit (fes_i386_muldiv); md_wait: it works over several
        // cycles, while D to WB freeze; md_de: the uop is a divide, which may
        // raise #DE.
        logic       md, md_fl, md_wait, md_de;
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
        gov_t        gov;         // D: the instruction's governor fields (decoded_t gov)
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
        logic        ldfwd;       // the result is the load data, whole (pipe_t ld): a 32-bit MOV, MOVZX or POP load
        logic [31:0] sp_new;      // AG: ESP after a stack operation (uop sp_write)
        logic [31:0] maddr;       // AG: the memory operand's linear address
        logic [31:0] moff;        // ... its offset
        logic [31:0] mlimit;      // D: its segment's limit less the access's bytes but one (an expand-down
                                  // segment's upper bound so less)
        logic        mtiny;       // D: ... which is below zero, or the segment refuses the access: every access faults
        logic [31:0] mlow;        // D: an expand-down segment's limit, at or below which every offset faults
        logic        mexpd;       // D: ... the segment is expand-down
        logic        pbad;        // M1: a probe's slots straddle the 64 KiB wrap (#SS at M2)
        logic [17:0] sdisp;       // D: a stack slot's displacement from ESP, signed (uop nwrap)
        logic        nodata;      // D: a load whose data nothing uses, made for its limit check (fes_i386_cpu):
                                  // the L1D reads nothing for it
        fault_e      dfault;      // D: the fault of a second address's limit check (uop st2), which M1
                                  // takes when the first passes
        logic        cs_late;     // D: a far transfer's offset past CS's limit (far_chk), #GP(0) at M1
                                  // when the uop's own checks pass
        logic        tchk;        // D: a near branch, or CALL rel's push, whose target_eip M1 checks
                                  // against CS's limit; M1: a branch's target is past it (#GP(0) at
                                  // EX when taken)
        logic        cpast;       // D (CALL r/m) or M1 (CALL rel): a near CALL's push whose target is
                                  // past CS's limit, which stores and leaves ESP (WB) for its jump's
                                  // #GP(0)
        logic [31:0] ld;          // M2: the loaded value, zero-extended (fes_i386_exctl xl, yl)
        logic [31:0] result;      // AG: LEA's offset; EX: the destination's new value
        logic [31:0] target;      // AG: a branch's target linear address; D: a st2 uop's store address,
                                  // a REPE or REPNE element's next instruction
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
        // The fetch check and the BTB (fes_i386_cpu, "Branch prediction").
        logic [31:0] lin;         // D: the instruction's fetched linear address
        logic [3:0]  ilen;        // ... and length
        logic        fchk;        // D: its first uop, whose lin AG checks against CS.base + EIP
        logic        fbad;        // D, AG: the instruction's bytes are not those at its EIP: replay it
        logic        dbr;         // D: a direct branch: Jcc, LOOP and its kin, JCXZ, JMP rel, CALL rel's jump
        logic        pt;          // D: ... which the front end predicted taken
        logic        bm;          // D: the instruction matched a BTB entry (fetched_t bm)
        logic        bway;
        logic [1:0]  bctr;
        // The return stack (fes_i386_cpu, "Branch prediction").
        logic        ncall;       // D: a near CALL's jump (CALL rel, CALL r/m)
        logic        nret;        // D: a near RET (C3h, C2h)
        logic        nind;        // D: an indirect near JMP's or CALL's jump (JMP r/m, CALL r/m)
        logic [31:0] peip;        // D: ... or nind's, the EIP the return stack or BTB predicted, when pt
        // Paging (fes_i386_cpu, "Paging"). ipp0, ipp1: D, the physical page
        // bits 19:12 of the instruction's first line and of the block it runs
        // into (fetched_t pp0, pp1), for the self-modifying-code check. ifw:
        // D, the uop is the walk of an instruction's fetch (fetched_t tm0 to
        // ts1), of the page at pf_lin, which ends in FAULT_REPLAY or #PF.
        // pg_a0, pg_a1, pg_t0, pg_t1: M2, the physical pages (PA bits 25:12
        // with phys's above bit) of the memory operand's page (maddr) and the
        // next, and of a st2 store's (target) and the next. pf_lin, pf_err:
        // a #PF's linear address (CR2) and error code (P, W/R, U/S).
        logic [7:0]     ipp0;
        logic [7:0]     ipp1;
        logic           ifw;
        // nx: D, a load whose data nothing uses that does not translate (a
        // probe, POP m's step, MUL and DIV's LOAD and HI); wx: ENTER's last
        // check, which translates as a write.
        logic           nx;
        logic           wx;
        logic [PA-12:0] pg_a0;
        logic [PA-12:0] pg_a1;
        logic [PA-12:0] pg_t0;
        logic [PA-12:0] pg_t1;
        logic [31:0]    pf_lin;
        logic [2:0]     pf_err;
        // txr: M2, the uop replays because the walker would have walked for
        // it as not the oldest, so the serial issue after it ends with its
        // REP element, not its instruction.
        logic           txr;
        // dfl: M1, the uop's fault is its st2 store's segment check
        // (dfault), which a #PF on its own pages comes before.
        logic           dfl;
        // ones: D, a load of a 286 TSS's word into a GPR, whose high half
        // reads as ones (fes_i386_cpu, "Task switches").
        logic           ones;
        // iret0: D, the uop is an IRET's first (a real-mode, V86 or
        // protected-mode IRET's, or a return from a nested task's), which
        // clears the NMI block as it reaches WB (fes_i386_cpu, "Interrupts").
        logic           iret0;
    } pipe_t;

endpackage
