// SPDX-License-Identifier: MIT
// The 387-compatible unit inside fes_i386_cpu (docs/i386-cpu-design.md,
// "x87"; the plan in docs/validation/2026-11-12-i386-x87-plan). This is the
// x87 phase's slice 2: the unit's state, its interface to the CPU, the AT
// board's error path, the loads and stores with their conversions, FXCH, the
// constants, FCHS, FABS, FTST, FXAM, and the stack faults. Arithmetic (add,
// subtract, multiply, compare, divide, square root, remainder and the
// transcendentals) is slices 3 to 5: a form this slice does not build raises
// FAULT_UNIMPLEMENTED (unimpl), which is how the build stops at an
// instruction it does not run.
//
// Sources, which the plan record lists in full:
//   387 PRM  Intel 80387 Programmer's Reference Manual 231917-001 (1987):
//            section 2 (the registers, the tag, status and control words),
//            section 3.1.7 (the operand rules and the exceptions), section
//            4.8.1 and Appendix C.1 (the RESET state), Appendix F.1-F.2 (the
//            PC/AT interface: ERROR# into IRQ 13 through a latch that holds
//            BUSY#, port F0h clearing it).
//   HRM      Intel 80386 Hardware Reference Manual (1987) section 5.1: the
//            386 waits for BUSY# before every ESC instruction except FNINIT
//            and FNCLEX.
//   386 PRM  Intel 386 DX Programmer's Reference Manual 230985-003: the
//            instruction set's x87 entries and section 11.
//
// The design, from the plan and Deano's decisions on #1768:
//
// - **Integrated, not a coprocessor bus.** The unit sits in the CPU and
//   keeps every behaviour software can see of a 387 on an AT board. There is
//   no 800000F8h bus and no ERROR# pin into the CPU: an unmasked exception
//   sets the status word's ES, which is IRQ 13's level, and latches BUSY#.
//
// - **The identity.** With `present` clear the unit answers nothing and the
//   CPU takes exactly its empty-socket path (fes_i386_decode), so the
//   i386dx+387 identity is a machine setting and the no-387 behaviour stays
//   selectable per title.
//
// - **One ESC instruction in flight.** The CPU's issue interlock holds an
//   ESC uop in D while an older ESC has not retired (fes_i386_cpu, x87_busy),
//   so the register stack a store reads is always committed state and a
//   squashed instruction leaves nothing to undo. Every architectural change
//   this unit makes happens at the instruction's commit (`wb_last`), in
//   program order, as the x87 pointers already do.
//
// - **BUSY# is a hold in D, and there are two of them.** An ESC waits on
//   the board's BUSY# latch, and it waits on the unit's own work. They are
//   separate, and only the first is what the HRM calls the BUSY# wait.
//   While the latch is set, every ESC but FNINIT and FNCLEX, and WAIT,
//   holds before it runs (`hold`), after #NM and #UD. A write to port F0h
//   clears the latch, F1h resets the unit. The no-wait forms (FNSTSW,
//   FNSTCW, FNSTENV, FNSAVE) skip only the pending-error check, not the
//   wait for BUSY#, so they hold too: the 387 PRM's Appendix F.1 list that
//   leaves FSETPM out is the 286/287 AT's, and the HRM's rule is the one a
//   386 follows (slice 0's `fsetpm` case). **FNINIT and FNCLEX skip the
//   latch and nothing else: they still wait for the unit's work**, because
//   this unit cannot abort an operation in flight. See `hold` below for why
//   that is a decision and what it costs.
//
// - **The tag word is derived.** The unit keeps one empty bit a register and
//   reads the other three classes (valid, zero, special) out of the value,
//   which is what FNSTENV stores and what the oracle's accessor compares
//   ("empty stays empty, any other from the value", bochs_oracle.cpp). A
//   387 keeps the tag word itself, so an FLDENV whose tags disagree with its
//   registers reads back differently; divergences.toml records it.
//
// - **Shifts are multiplies.** Option A has no barrel shifter: a left shift
//   by n is a multiply by 2^n and the product's low half, a right shift by n
//   a multiply by 2^(64-n) with the product's high half and the low half as
//   the sticky bit. The one multiplier is the 9-DSP block below, which slice
//   3's multiply shares.
//
// - **Operand registers.** The multiplier and the adder take their operands
//   from registers of their own (`ua_mul`/`ub_mul` here, the adder's in slice
//   3), which is the fit spike's remedy for the one shared operand register
//   its standalone fits could not route.
//
// The 80-bit value this unit stores is {sign, exponent[14:0],
// significand[63:0]}, the memory format, and its value is
// significand * 2^(exponent - 16383 - 63) with the significand's bit 63 set
// for a normal number.

// The unit. Its interface to the CPU is per-uop, because an ESC instruction
// with a memory operand is several uops (fes_i386_decode and the microcode
// loops the no-387 forms already use):
//   - `iss` is an ESC uop at the head of D, with `iss_first` on the
//     instruction's first and `iss_last` on its last, and `iss_part` the part
//     or field it moves. `hold` keeps it in D: the board's latch is set, or
//     the unit is not ready to answer. A form a later slice builds never
//     reaches the unit, because fes_i386_decode stops the instruction as not
//     built through the same x87_decode.
//   - a store uop takes its value from `sval` in EX, which the unit holds
//     stable until the instruction commits; `nx` says an unmasked exception
//     means the access must not be made.
//   - `wb` is a uop's commit, with `wb_val` a load's value and `wb_part` its
//     part; `wb_last` commits the instruction, which is when every
//     architectural change this unit makes happens.
//   - `flush` drops an instruction the pipeline squashed.
// A load's operation finishes after the instruction retires, as a real 387's
// does with BUSY# still active: nothing the CPU does depends on it, and the
// next ESC holds in D until the unit is idle.
module fes_i386_x87 (
    input  logic        clk,
    input  logic        reset,
    input  logic        present,     // the i386dx+387 identity
    // The AT board's coprocessor controls (IBM PC AT Technical Reference,
    // "NMI and Coprocessor Controls"), which the machine decodes and the CPU
    // passes through: a write to F0h clears the latched BUSY#, one to F1h
    // resets the unit.
    input  logic        f0,
    input  logic        f1,
    output logic        error,       // ERROR#: the status word's ES, IRQ 13's level
    output logic        latched,     // the board's BUSY# latch
    // Issue, from D.
    input  logic        iss,
    input  logic        iss_wait,    // the uop is WAIT (9Bh), not an ESC
    input  logic [10:0] iss_op,      // {ESC's low three bits, the ModRM byte}
    input  logic [2:0]  iss_fmt,     // the memory operand's format (FMT_*)
    input  logic        iss_first,
    input  logic        iss_fire,    // ... and the uop issues this cycle (the CPU's issue)
    input  logic        iss_last,
    input  logic [5:0]  iss_part,
    output logic        hold,
    output logic [31:0] sval,
    output logic        nx,
    // Commit, at WB.
    input  logic        wb,
    input  logic        wb_last,
    input  logic [31:0] wb_val,
    input  logic [5:0]  wb_part,
    input  logic        flush,
    // FLDENV and FRSTOR load the last instruction's opcode out of the
    // environment, where the CPU finds it: the real-mode layouts carry it
    // beside the instruction pointer's high bits and the 32-bit protected
    // one beside the code selector, while the 16-bit protected layout has
    // no room for it and leaves it zero (387 PRM, the FLDENV layouts).
    input  logic        wb_opcode_set,
    input  logic [10:0] wb_opcode,
    // The state the CPU and the bench read.
    output logic [15:0] cw_o,
    output logic [15:0] sw_o,
    output logic [15:0] tw_o,
    output logic [10:0] opcode_o,
    output logic        busy_o,
    // The sequencer's state, for the unit's own bench and the CPU's trace;
    // nothing in the machine reads it, so the fit trims the wires.
    output logic [4:0]  trace_seq,
    // FINIT clears the x87 pointers as well as the words (387 PRM, FINIT),
    // and the CPU keeps them, so the unit says when: in the cycle FNINIT
    // commits, or FNSAVE, which leaves the unit as FINIT does. It is in
    // that cycle and not the one after, because the CPU keeps the pointers
    // in its commit block.
    output logic        clr_pointers,
    // The eight physical registers, which the bench reads through the CPU's
    // arch_* outputs to build the oracle's X87State; nothing in the machine
    // uses them, so the seal's fit trims the wires.
    output logic [639:0] regs_o
);
    import fes_i386_pkg::*;

    // ---- The value classes (387 PRM section 3.1.7). ----
    // A pseudo-denormal (exponent 0 with the significand's bit 63 set) is a
    // denormal whose exponent reads as 1; an unnormal (a non-zero exponent
    // with bit 63 clear), a pseudo-NaN and a pseudo-infinity are the
    // unsupported formats, which raise invalid when an operation uses them.
    typedef enum logic [2:0] { C_ZERO, C_DEN, C_NORM, C_INF, C_QNAN, C_SNAN, C_UNSUP, C_EMPTY } cls_e;
    function automatic cls_e class_of(input logic [79:0] v, input logic empty);
        logic [14:0] e;
        logic [63:0] s;
        e = v[78:64];
        s = v[63:0];
        if (empty) class_of = C_EMPTY;
        else if (e == 15'd0) class_of = s == 64'd0 ? C_ZERO : C_DEN;
        else if (e == 15'h7fff) begin
            if (!s[63]) class_of = C_UNSUP;
            else if (s[62:0] == 63'd0) class_of = C_INF;
            else class_of = s[62] ? C_QNAN : C_SNAN;
        end else class_of = s[63] ? C_NORM : C_UNSUP;
    endfunction
    // The two tag bits FNSTENV stores: 00 valid, 01 zero, 10 special, 11
    // empty, which the oracle's accessor computes the same way.
    function automatic logic [1:0] tag_of(input cls_e c);
        tag_of = c == C_EMPTY ? 2'b11 : c == C_ZERO ? 2'b01 : c == C_NORM ? 2'b00 : 2'b10;
    endfunction
    // The quiet NaN a masked invalid operation delivers, "real indefinite"
    // (387 PRM, Table 3.4): the sign set, every exponent bit set and the
    // significand 1100...0.
    localparam logic [79:0] INDEFINITE = {1'b1, 15'h7fff, 1'b1, 1'b1, 62'd0};

    // ---- The control, status and tag words. ----
    // The status word's bits: 15 B, 14 C3, 13-11 TOP, 10 C2, 9 C1, 8 C0,
    // 7 ES, 6 SF, 5-0 PE UE OE ZE DE IE (387 PRM section 2.4).
    logic [15:0] cw;
    logic [5:0]  fl;      // the six exception flags, PE UE OE ZE DE IE
    logic        sf;
    logic [3:0]  cc;      // C3 C2 C1 C0
    logic [2:0]  top;
    logic [10:0] opcode;
    logic [7:0]  empty;   // one bit a physical register
    logic [1:0]  rc, pc;  // the control word's rounding and precision fields
    assign rc = cw[11:10];
    assign pc = cw[9:8];
    // ES is the exception summary: any flag that is not masked. It is not a
    // bit of its own, so it follows every change to the flags and to the
    // control word -- an exception sets it, FNCLEX and FNINIT clear it with
    // the flags, FLDCW, FLDENV and FRSTOR recompute it, and FNSTENV clears
    // it by masking everything. That is how the oracle has it: Bochs
    // re-evaluates `status & ~control & exceptions` at each of those and
    // sets or clears ES and B together (`clear_unmasked_fpu_exception`,
    // called from its FLDENV, FNINIT, FNCLEX and FNSTENV). B mirrors ES on
    // a 387 (387 PRM Appendix C.1, where RESET leaves both set, with IM
    // clear and IE set).
    logic es;
    assign es = |(fl & ~cw[5:0]);
    assign sw_o = {es, cc[3], top, cc[2], cc[1], cc[0], es, sf, fl};
    assign cw_o = cw;
    assign opcode_o = opcode;
    // The tag word: the empty bit a register's own writes keep, and the two
    // class bits the write stored with it, so the word costs no read of the
    // stack (fes_i386_x87's header, "The tag word is derived").
    // Reset leaves them at 01, zero, which is the class of the zeroed
    // memory the stack powers up with, so an FLDENV whose tag word marks a
    // register that has never been written reads back a settled tag rather
    // than whatever the block held.
    logic [1:0] cls_bits [0:7];
    always_comb begin
        tw_o = 16'd0;
        for (int n = 0; n < 8; n++) tw_o[2*n +: 2] = empty[n] ? 2'b11 : cls_bits[n];
    end
    assign error = present && es;

    // ---- The register stack. ----
    // Eight 80-bit registers; ST(i) is the physical register
    // (TOP + i) & 7. Two read ports, so two copies, each written together.
    logic [79:0] rf_a [0:7];
    logic [79:0] rf_b [0:7];
    logic [79:0] rf_q [0:7];   // the values the tag word and the bench read
    logic [2:0]  ra_idx, rb_idx, rw_idx;
    logic [2:0]  ra_live;     // the index ra_val holds, a cycle behind ra_idx
    logic        rw_en;
    logic [79:0] rw_val;
    logic [79:0] ra_val, rb_val;
    always_ff @(posedge clk) begin
        ra_val <= rf_a[ra_idx];
        rb_val <= rf_b[rb_idx];
        ra_live <= ra_idx;
        if (reset)
            for (int n = 0; n < 8; n++) cls_bits[n] <= 2'b01;
        else if (rw_en) begin
            rf_a[rw_idx] <= rw_val;
            rf_b[rw_idx] <= rw_val;
            cls_bits[rw_idx] <= tag_of(class_of(rw_val, 1'b0));
        end
    end
    always_comb for (int n = 0; n < 8; n++) rf_q[n] = rf_a[n];
    always_comb for (int n = 0; n < 8; n++) regs_o[80*n +: 80] = rf_a[n];

    // ---- Shifts, on the multiplier. ----
    // A left shift by n is a multiply by 2^n and the product's low half; a
    // right shift by n a multiply by 2^(64-n), the product's high half and
    // its low half as the sticky bit. n is 1 to 63; 0 needs no multiply and
    // 64 or more leaves nothing but the sticky bit.
    logic         mul_start;
    logic [63:0]  ua_mul, ub_mul;   // the multiplier's own operand registers
    logic         mul_done;
    logic [127:0] mul_p;
    fes_i386_x87_mul mul (
        .clk, .reset, .start(mul_start), .a(ua_mul), .b(ub_mul), .done(mul_done), .p(mul_p)
    );

    // The leading-zero count of a 64-bit magnitude (64 when it is zero).
    function automatic logic [6:0] clz64(input logic [63:0] v);
        clz64 = 7'd64;
        for (int n = 0; n < 64; n++) if (v[n]) clz64 = 7'(63 - n);
    endfunction

    // Rounding under the control word's RC field (387 PRM section 2.3.2).
    // The position is one of four, so the shift is a mux: 0 and 1 for an
    // integer store, 11 for a 64-bit real and 40 for a 32-bit one. `sticky`
    // already holds the bits an earlier shift dropped. Round to nearest
    // breaks a tie to even; round down and round up go by the sign.
    localparam logic [1:0] P_0 = 2'd0, P_1 = 2'd1, P_11 = 2'd2, P_40 = 2'd3;
    function automatic logic [66:0] round_sel(input logic [64:0] m, input logic [1:0] sel,
                                              input logic sticky, input logic sign, input logic [1:0] mode);
        logic [64:0] q;
        logic        r, s, inc;
        case (sel)
            P_0: begin q = m; r = 1'b0; s = sticky; end
            P_1: begin q = {1'b0, m[64:1]}; r = m[0]; s = sticky; end
            P_11: begin q = {11'd0, m[64:11]}; r = m[10]; s = sticky || |m[9:0]; end
            default: begin q = {40'd0, m[64:40]}; r = m[39]; s = sticky || |m[38:0]; end
        endcase
        case (mode)
            2'd0: inc = r && (s || q[0]);
            2'd1: inc = sign && (r || s);
            2'd2: inc = !sign && (r || s);
            default: inc = 1'b0;
        endcase
        // The rounded magnitude, whether it is inexact, and whether the
        // rounding took it away from zero (C1).
        round_sel = {inc, r || s, inc ? q + 65'd1 : q};
    endfunction

    // ---- Rounding to the destination's precision (387 PRM section 2.3.2). ----
    // The arithmetic rounds its 64-bit significand at the bit the control
    // word's PC field names -- bit 0 for the extended format (PC 11), bit 11
    // for the 53-bit (PC 10) and bit 40 for the 24-bit (PC 00) -- and clears
    // the bits below it, which is what a destination's precision means. The
    // guard and round bits sit below bit 0 and the sticky holds what the
    // shifts dropped, so the three positions are one mux and not three
    // rounders. PC 01 is reserved and reads as the extended format, as
    // Bochs's softfloat does.
    //
    // Precision control applies to add, subtract, multiply, divide and
    // square root and to nothing else (387 PRM section 2.3.2), so FRNDINT,
    // FSCALE and FXTRACT pass PC_EXT whatever the control word says.
    localparam logic [1:0] PC_EXT = 2'd3;
    // {carry out of bit 63, inexact, the rounding went away from zero (C1),
    // the rounded magnitude}. On a carry the caller takes the significand as
    // 2^63 and steps the exponent, which is the only pattern a carry leaves.
    function automatic logic [66:0] round_pc(input logic [63:0] m, input logic g, input logic r,
                                            input logic s, input logic sign,
                                            input logic [1:0] mode, input logic [1:0] prec);
        logic [64:0] q;
        logic        rbit, sbit, lsb, inc;
        case (prec)
            2'd0:    begin rbit = m[39]; sbit = |m[38:0] || g || r || s; lsb = m[40]; end
            2'd2:    begin rbit = m[10]; sbit = |m[9:0]  || g || r || s; lsb = m[11]; end
            default: begin rbit = g;     sbit = r || s;                 lsb = m[0];  end
        endcase
        case (mode)
            2'd0: inc = rbit && (sbit || lsb);   // to nearest, ties to even
            2'd1: inc = sign && (rbit || sbit);  // down
            2'd2: inc = !sign && (rbit || sbit); // up
            default: inc = 1'b0;                 // toward zero
        endcase
        case (prec)
            2'd0:    q = {1'b0, m[63:40], 40'd0} + (inc ? 65'd1 << 40 : 65'd0);
            2'd2:    q = {1'b0, m[63:11], 11'd0} + (inc ? 65'd1 << 11 : 65'd0);
            default: q = {1'b0, m} + 65'(inc);
        endcase
        round_pc = {q[64], rbit || sbit, inc, q[63:0]};
    endfunction
    // The largest finite significand a precision can hold, which masked
    // overflow stores when the rounding is toward zero.
    function automatic logic [63:0] max_sig(input logic [1:0] prec);
        max_sig = prec == 2'd0 ? 64'hffff_ff00_0000_0000
                : prec == 2'd2 ? 64'hffff_ffff_ffff_f800 : 64'hffff_ffff_ffff_ffff;
    endfunction

    // ---- The instruction in flight. ----
    // One ESC instruction is in flight at a time (the CPU's interlock), so
    // these are the operation's own registers. `busy` is the 387's BUSY#:
    // it stays set until the operation finishes, which for a load is after
    // the instruction has retired, as a real 387's does.
    // The A_* states are slice 3's arithmetic. Every multiply -- a product
    // and every shift alike -- goes through S_SHIFT and S_MUL and comes back
    // to the state `sh_ret` names, so the one multiplier is shared without a
    // second sequencer.
    typedef enum logic [4:0] {
        S_IDLE, S_ARG, S_PRE, S_SHIFT, S_MUL, S_POST, S_POST2, S_RDY, S_SAVE_RD, S_APPLY, S_APPLY2,
        A_CVT,      // a memory operand's conversion has finished: take it
        A_CAP,      // take both operands, so the classes below can read them
        A_SETUP,    // the dispatch, and the pre-normalising shifts
        A_PNA,      // take pa's pre-normalising shift
        A_PNB,      // ... and pb's
        A_OP,       // the operation's first datapath step
        A_LOAD,     // load the adder's own operand registers
        A_SUM,      // the 67-bit sum or difference
        A_PROD,     // the 128-bit product
        A_NORM,     // take a cancellation's normalising shift
        A_TINY,     // take the denormalising shift of a tiny result
        A_RND,      // round at PC, then overflow, underflow and the flags
        A_SC,       // FSCALE's scale, once the truncating shift has run
        A_INT,      // FRNDINT's integer, once the fraction is below bit 0
        A_INT2,     // ... rounded, and its normalising shift started
        A_PACK,     // ... and packed
        A_XT        // FXTRACT's exponent, once its magnitude has normalised
    } seq_e;
    seq_e        seq;
    seq_e        sh_ret;      // the state S_MUL returns to when the multiply is done
    x87_op_e     cur;
    logic [2:0]  cur_fmt;
    logic        cur_pop;       // the form pops ST(0) after it stores
    logic [2:0]  cur_i;         // the form's ST(i) index
    logic [79:0] mval;          // the memory operand, or a register image's chunks
    logic [79:0] sres;          // a store's value in its operand's format
    logic [79:0] lres;          // a load's or a register form's result
    logic [5:0]  pfl;           // the operation's exception flags
    logic        psf, pc1;      // ... its stack flag and C1
    logic [3:0]  pcc;           // ... the condition codes it sets
    logic        pcc_wr;
    logic        pnx;           // an unmasked exception: make no access
    logic [2:0]  save_reg;      // the register FNSAVE or FRSTOR is moving
    logic        busy;
    logic        es_q;          // ES a cycle ago, for the board's latch
    logic        env_load;      // FLDENV or FRSTOR is loading, field by field
    logic        committed;     // the instruction in flight has retired
    logic [1:0]  nw;            // a no-wait form waiting to commit: 1 FNINIT, 2 FNCLEX
    logic        nw_waiting;    // ... and its own uop has not issued yet
    logic [10:0] cur_op_raw;    // its 387 opcode, which commit stores
    logic        cur_ctrl;      // ... and whether it is a control form
    assign busy_o = busy;
    assign trace_seq = 5'(seq);

    // ---- The board's BUSY# latch (387 PRM Appendix F.2). ----
    // ERROR# going active latches BUSY#, which holds the next waiting ESC or
    // WAIT until a write to port F0h clears it. A write to F1h resets the
    // unit with ERROR# active and leaves the latch as it is, as the oracle's
    // adapter does (bochs_oracle.cpp, the F1h case). The latch is set in the
    // same cycle as ES, below, so an ESC already waiting in D cannot slip
    // past it.

    // ---- Issue. ----
    // FNINIT and FNCLEX are the two forms that do not wait for BUSY# (HRM
    // section 5.1), so they pass the board's latch; every other ESC, and
    // WAIT, holds on it. They do not pass the unit's own work (`hold`).
    x87_op_e iss_dec;
    logic iss_nowait;
    assign iss_dec = x87_decode(iss_op, iss_fmt);
    assign iss_nowait = !iss_wait && (iss_dec == X87_INIT || iss_dec == X87_CLEX);
    // The forms whose value the CPU needs before the instruction retires:
    // a real or integer store, whose conversion the unit runs as the
    // instruction's first uop waits in D. FNSTCW, FNSTSW, FNSTENV and
    // FNSAVE answer out of state, FNSAVE after a register read apiece.
    logic started;             // the unit has taken the instruction at D's head
    logic fired;               // ... and that instruction's first uop has issued
    logic val_form, take, iss_save_rd;
    assign val_form = iss_dec == X87_ST_M || iss_dec == X87_STP_M;
    assign iss_save_rd = iss && !iss_wait && iss_dec == X87_SAVE && iss_part[5];
    // A form a later slice builds never reaches the unit: fes_i386_decode
    // stops the instruction as not built, through the same x87_decode.
    logic unbuilt;
    assign unbuilt = !iss_wait && iss_dec == X87_UNIMPL;
    // The hold (BUSY#): a new instruction's first uop waits for the board's
    // latch and for the unit, which owns the instruction before it until its
    // work is done -- `fired` says that instruction's first uop has gone, so
    // a first uop now is a new instruction's. It waits for a no-wait form
    // that has issued and not yet committed too (`nw`): FNINIT resets the
    // unit and FNCLEX clears its flags at their own commit, and a 387 is
    // busy while it does (33 clocks for FNINIT, datasheet pp. 37-39), so an
    // ESC after one must not be taken before it. An instruction the unit has
    // taken waits only for its own value.
    //
    // FNINIT and FNCLEX skip the board's latch and not the unit, which is a
    // decision and not an oversight. A real 387 taking FNINIT while busy
    // *aborts* the operation in flight; this unit has no abort, so its
    // operation runs to its apply and writes the stack, TOP and the flags
    // whenever that falls. Letting the no-wait forms overtake it without
    // that abort was measured on #1821 and takes the bench to 86 of 87:
    // `overlap.const-then-init` ends at TOP=7 with a register pushed, the
    // push having landed after FNINIT's reset, where the oracle and a 387
    // both end at TOP=0 with the stack empty. The faithful version is two
    // behaviours and not one deletion -- FNINIT cancelling the operation the
    // way `flush` does, and FNCLEX letting it run and set its flags
    // afterwards -- and the second carries an oracle question, because Bochs
    // is atomic: FLD of a denormal then FNCLEX leaves DE clear there and
    // here, and would leave it set under the overtaking version. That is a
    // divergences.toml entry and a design change. What is *not* at stake is
    // the exception itself: the operation's apply sets `latched` in the same
    // cycle it sets the flags, and FNCLEX clears only the flags and the
    // stack flag, never the board's latch, which on an AT only a write to
    // port F0h clears.
    //
    // A first uop waits for the unit only while the unit has not taken it
    // (`!started`): a store's own conversion makes the unit busy before its
    // first uop issues, so testing `busy` alone would hold it for ever. Once
    // its first uop has gone (`fired`), a first uop is a new instruction's
    // and waits again. A no-wait form's slot (`nw`) is its own until its uop
    // issues (`nw_waiting`), for the same reason: with the governor holding
    // D it waits a cycle or more between being taken and issuing, and it
    // must not wait on itself.
    assign hold = present && iss && !unbuilt
               && (iss_nowait ? fired || busy || nw != 2'd0 && !nw_waiting
                  : iss_wait ? latched || busy || nw != 2'd0
                             : iss_first && (fired || !started && (busy || latched || nw != 2'd0))
                              || val_form && seq != S_RDY
                              || iss_save_rd && iss_part[4:2] + top != ra_live);
    // FNINIT and FNCLEX are taken into their own slot (`nw`) rather than
    // the instruction's, so one of them can sit behind an ESC the unit has
    // taken; `take_nw` still waits for the unit to be idle, for the reason
    // set out above `hold`.
    logic take_nw;
    // A flush in the same cycle wins: D still holds the wrong-path
    // instruction this cycle, so taking it would leave `started` or the
    // no-wait slot set for an instruction that can never commit.
    assign take = present && iss && !flush && iss_first && !started && !busy && !latched
               && nw == 2'd0 && !iss_wait && !iss_nowait && iss_dec != X87_UNIMPL;
    assign take_nw = present && iss && !flush && iss_first && iss_nowait && nw == 2'd0 && !started && !busy;
    // The control forms, which leave the x87 pointers and the opcode as they
    // are: the same list fes_i386_decode gives X87P_NONE. FNOP (D9 D0) is
    // not one of them; FENI, FDISI and FSETPM (DB E0, E1, E4) are.
    logic ctrl_form;
    assign ctrl_form = iss_dec == X87_INIT || iss_dec == X87_CLEX || iss_dec == X87_STSW
                    || iss_dec == X87_STCW || iss_dec == X87_LDCW || iss_dec == X87_STENV
                    || iss_dec == X87_LDENV || iss_dec == X87_SAVE || iss_dec == X87_RSTOR
                    || iss_dec == X87_NOP && iss_op[10:8] == 3'b011;

    // ---- The load conversions (387 PRM section 2.2 and Table 3.4). ----
    // A load of a 32- or 64-bit real, or of a 16-, 32- or 64-bit integer, to
    // the extended format. Every one is exact; a denormal 32- or 64-bit
    // operand raises the denormal exception and is normalised, and a
    // signalling NaN raises invalid and is quieted. A 10-byte load is a bit
    // copy, which is why it raises neither: the unsupported formats it can
    // bring in raise invalid where an operation uses them.
    // The normalising left shift is a multiply by 2^lz, so the conversion
    // hands the sequencer a magnitude and the exponent it has before the
    // shift: the result is {sign, expb - lz, mag << lz}.
    logic [79:0] ld_v;       // the result when no shift is needed
    logic        ld_shift;
    logic [63:0] ld_mag;
    logic [16:0] ld_expb;
    logic        ld_sign;
    logic [5:0]  ld_fl;
    always_comb begin
        logic [63:0] iv;
        logic [22:0] f32;
        logic [51:0] f64;
        logic [7:0]  e32;
        logic [10:0] e64;
        ld_v = 80'd0;
        ld_shift = 1'b0;
        ld_mag = 64'd0;
        ld_expb = 17'd0;
        ld_sign = 1'b0;
        ld_fl = 6'd0;
        iv = 64'd0;
        f32 = mval[22:0];
        e32 = mval[30:23];
        f64 = mval[51:0];
        e64 = mval[62:52];
        case (cur_fmt)
            X87F_F80: ld_v = mval;
            X87F_F32: begin
                ld_sign = mval[31];
                if (e32 == 8'd0 && f32 == 23'd0) ld_v = {mval[31], 79'd0};
                else if (e32 == 8'd0) begin
                    // A denormal: normalise it and raise the denormal
                    // exception (387 PRM section 3.1.7).
                    ld_shift = 1'b1;
                    ld_mag = {41'd0, f32};
                    ld_expb = 17'd16297;
                    ld_fl = 6'b000010;
                end else if (e32 == 8'hff) begin
                    ld_v = f32 == 23'd0 ? {mval[31], 15'h7fff, 1'b1, 63'd0}
                                        : {mval[31], 15'h7fff, 1'b1, f32[22] ? f32 : {1'b1, f32[21:0]}, 40'd0};
                    if (f32 != 23'd0 && !f32[22]) ld_fl = 6'b000001;  // a signalling NaN: invalid, quieted
                end else
                    ld_v = {mval[31], 15'(17'(e32) + 17'd16256), 1'b1, f32, 40'd0};
            end
            X87F_F64: begin
                ld_sign = mval[63];
                if (e64 == 11'd0 && f64 == 52'd0) ld_v = {mval[63], 79'd0};
                else if (e64 == 11'd0) begin
                    ld_shift = 1'b1;
                    ld_mag = {12'd0, f64};
                    ld_expb = 17'd15372;
                    ld_fl = 6'b000010;
                end else if (e64 == 11'h7ff) begin
                    ld_v = f64 == 52'd0 ? {mval[63], 15'h7fff, 1'b1, 63'd0}
                                        : {mval[63], 15'h7fff, 1'b1, f64[51] ? f64 : {1'b1, f64[50:0]}, 11'd0};
                    if (f64 != 52'd0 && !f64[51]) ld_fl = 6'b000001;
                end else
                    ld_v = {mval[63], 15'(17'(e64) + 17'd15360), 1'b1, f64, 11'd0};
            end
            default: begin
                // FILD of a 16-, 32- or 64-bit integer, sign-extended, which
                // is exact: the magnitude normalised, or +0.0 for zero (a
                // zero integer loads as positive).
                iv = cur_fmt == X87F_I16 ? {{48{mval[15]}}, mval[15:0]}
                   : cur_fmt == X87F_I32 ? {{32{mval[31]}}, mval[31:0]} : mval[63:0];
                ld_sign = iv[63];
                if (iv == 64'd0) ld_v = 80'd0;
                else begin
                    ld_shift = 1'b1;
                    ld_mag = iv[63] ? (~iv + 64'd1) : iv;
                    ld_expb = 17'd16446;
                end
            end
        endcase
    end

    // ---- The store conversions. ----
    // ST(0) rounded to its operand's format under the control word's RC
    // field. Precision control does not apply: a store to a 32- or 64-bit
    // real rounds to that format, and an integer store to an integer
    // (387 PRM section 2.3.2; docs/i386-cpu-design.md, x87).
    // The rounding happens at a fixed bit of the significand, after a right
    // shift when the result is a denormal of the target format or an
    // integer: the shift is a multiply by 2^(64-k), whose high half is the
    // shifted magnitude and whose low half is the sticky bit.
    cls_e        st_cls;
    logic [14:0] st_exp;
    logic [63:0] st_sig;
    logic        st_sign;
    logic signed [17:0] st_e;     // the unbiased exponent, a pseudo-denormal's read as -16382
    logic [6:0]  st_k;            // the right shift
    logic [1:0]  st_p;            // the rounding position (P_*)
    logic        st_shift, st_direct, st_big;
    logic [79:0] st_v;            // the value a class that needs no rounding stores
    logic [5:0]  st_fl;
    assign st_cls = class_of(ra_val, empty[top]);
    assign st_sign = ra_val[79];
    assign st_exp = ra_val[78:64];
    assign st_sig = ra_val[63:0];
    assign st_e = st_exp == 15'd0 ? -18'sd16382 : $signed({3'b0, st_exp}) - 18'sd16383;
    // The target format, as selectors rather than widths, so every shift in
    // the rounding and packing below is a mux: f64 says which real format,
    // and iw which integer width (0 is 16 bits, 1 is 32, 2 is 64).
    logic       f_f64, f_int;
    logic [1:0] f_iw;
    logic [14:0] f_bias;
    assign f_f64 = cur_fmt == X87F_F64;
    assign f_int = cur_fmt == X87F_I16 || cur_fmt == X87F_I32 || cur_fmt == X87F_I64;
    assign f_iw = cur_fmt == X87F_I16 ? 2'd0 : cur_fmt == X87F_I32 ? 2'd1 : 2'd2;
    assign f_bias = f_f64 ? 15'd1023 : 15'd127;
    logic signed [17:0] st_te;
    assign st_te = st_e + $signed({3'b0, f_bias});
    // The value a masked invalid operation stores at an integer destination,
    // "integer indefinite" (387 PRM Table 3.4).
    function automatic logic [63:0] int_indefinite(input logic [1:0] iw);
        int_indefinite = iw == 2'd0 ? 64'h8000 : iw == 2'd1 ? 64'h8000_0000 : 64'h8000_0000_0000_0000;
    endfunction
    always_comb begin
        st_k = 7'd0;
        st_p = P_0;
        st_shift = 1'b0;
        st_direct = 1'b1;
        st_big = 1'b0;
        st_v = 80'd0;
        st_fl = 6'd0;
        case (cur_fmt)
            X87F_F80: begin
                // FSTP m80 is a bit copy, so no operand raises anything but
                // the stack fault of an empty ST(0).
                st_v = st_cls == C_EMPTY ? INDEFINITE : ra_val;
                st_fl = st_cls == C_EMPTY ? 6'b000001 : 6'd0;
            end
            X87F_F32, X87F_F64: begin
                st_p = f_f64 ? P_11 : P_40;
                case (st_cls)
                    C_EMPTY, C_UNSUP: begin
                        st_v = f_f64 ? 80'hfff8_0000_0000_0000 : 80'hffc0_0000;
                        st_fl = 6'b000001;
                    end
                    C_ZERO: st_v = f_f64 ? {16'd0, st_sign, 63'd0} : {48'd0, st_sign, 31'd0};
                    C_INF: st_v = f_f64 ? {16'd0, st_sign, 11'h7ff, 52'd0}
                                        : {48'd0, st_sign, 8'hff, 23'd0};
                    C_QNAN, C_SNAN: begin
                        // The significand's top bits, with a signalling NaN
                        // quieted and invalid raised.
                        logic [63:0] q;
                        q = st_cls == C_SNAN ? {2'b11, st_sig[61:0]} : st_sig;
                        st_v = f_f64 ? {16'd0, st_sign, 11'h7ff, q[62:11]}
                                     : {48'd0, st_sign, 8'hff, q[62:40]};
                        if (st_cls == C_SNAN) st_fl = 6'b000001;
                    end
                    default: begin
                        // A finite number: round it, after a right shift
                        // when the result is the target's denormal.
                        st_direct = 1'b0;
                        if (st_te <= 18'sd0) begin
                            st_k = (18'sd1 - st_te) > 18'sd64 ? 7'd64 : 7'((18'sd1 - st_te));
                            st_shift = st_k != 7'd0;
                        end
                    end
                endcase
            end
            default: begin
                case (st_cls)
                    C_ZERO: st_v = 80'd0;
                    C_EMPTY, C_QNAN, C_SNAN, C_INF, C_UNSUP: begin
                        st_big = 1'b1;
                        st_fl = 6'b000001;
                        st_v = {16'd0, int_indefinite(f_iw)};
                    end
                    default: begin
                        st_direct = 1'b0;
                        st_p = P_1;
                        if (st_e > 18'sd63) begin
                            st_big = 1'b1;
                            st_direct = 1'b1;
                            st_fl = 6'b000001;
                            st_v = {16'd0, int_indefinite(f_iw)};
                        end else if (st_e == 18'sd63) begin
                            st_p = P_0;
                        end else begin
                            st_k = (18'sd62 - st_e) > 18'sd64 ? 7'd64 : 7'((18'sd62 - st_e));
                            st_shift = st_k != 7'd0;
                        end
                    end
                endcase
            end
        endcase
    end

    // ---- The shifted magnitude, and the rounding. ----
    // `shm` is the significand after the shift the conversion asked for and
    // `shs` the bits it dropped; with no shift they are the significand
    // itself. A shift of 64 or more leaves nothing but the sticky bit.
    // The conversion's own state is registered as the sequencer leaves
    // S_PRE, so the rounding and the packing below start from registers:
    // the one combinational path from the register stack to the stored
    // value was 18 levels of logic and missed the clock by 7.5 ns.
    logic [63:0] shm;
    logic        shs;
    logic [6:0]  sh_k;        // the shift the sequencer started
    logic [63:0] sh_in;
    logic [14:0] sh_exp;      // a normalising load's exponent after the shift
    // The shift's operands, registered so the multiplier's one-hot
    // multiplicand is a decode of a register and not the end of the
    // exponent arithmetic: that chain, from the register stack through the
    // exponent subtract and the shift amount into the decoder, was the
    // critical path once the rounding was split.
    logic [63:0] sh_src;
    logic [5:0]  sh_pow;      // the shift is a multiply by 2^sh_pow
    // Slice 3 shares the one multiplier between the shifts and its own
    // products, so a step says where S_MUL goes when the multiply is done
    // and whether its second operand is a significand (`sh_b2`) rather than
    // a power of two.
    logic [63:0] sh_b2;
    logic        sh_isprod;
    assign shm = sh_k == 7'd0 ? sh_in : sh_k >= 7'd64 ? 64'd0 : mul_p[127:64];
    assign shs = sh_k == 7'd0 ? 1'b0 : sh_k >= 7'd64 ? sh_in != 64'd0 : mul_p[63:0] != 64'd0;
    logic               c_sign, c_f64, c_int;
    logic [1:0]         c_p, c_iw;
    logic signed [17:0] c_te;
    logic [66:0]        rnd_w;
    logic [64:0]        rnd_q;
    logic               rnd_inex_q, rnd_up_q;
    assign rnd_w = round_sel({1'b0, shm}, c_p, shs, c_sign, rc);

    // The real result, packed. A normal target's exponent field goes below
    // the rounded significand's leading one; the rounding's carry into that
    // one raises the exponent by itself. A denormal target's rounded value
    // is the pattern already, and a carry into its leading one makes it the
    // smallest normal, which is the next pattern up. Both shifts are fixed,
    // so they are wires.
    logic [63:0]        real_bits;
    logic signed [17:0] real_te;
    logic               real_ovf, real_unf, real_carry;
    always_comb begin
        real_carry = c_f64 ? rnd_q[53] : rnd_q[24];
        real_te = c_te + 18'(real_carry);
        real_ovf = c_te > 18'sd0 && real_te > (c_f64 ? 18'sd2046 : 18'sd254);
        real_unf = c_te <= 18'sd0 && rnd_inex_q;
        // The exponent field sits below the rounded significand's leading
        // one, so the rounding's carry into that one raises the exponent by
        // itself: the base takes the exponent before the carry and the sum
        // takes the whole rounded value.
        if (c_te > 18'sd0)
            real_bits = c_f64 ? {1'b0, 11'(c_te - 18'sd1), 52'd0} + {10'd0, rnd_q[53:0]}
                              : {33'd0, 8'(c_te - 18'sd1), 23'd0} + {39'd0, rnd_q[24:0]};
        else
            real_bits = rnd_q[63:0];
    end
    // The integer result and its range check: a signed value of 16, 32 or 64
    // bits takes -2^(n-1) to 2^(n-1)-1.
    logic        int_oor;
    logic [63:0] int_bits;
    always_comb begin
        logic [64:0] lim;
        lim = c_iw == 2'd0 ? 65'h8000 : c_iw == 2'd1 ? 65'h8000_0000 : 65'h8000_0000_0000_0000;
        if (!c_sign) lim = lim - 65'd1;
        int_oor = rnd_q > lim;
        int_bits = c_sign ? (~rnd_q[63:0] + 64'd1) : rnd_q[63:0];
    end

    // ---- Slice 3's arithmetic: the form. ----
    // Add, subtract, reverse subtract, multiply, the compares, FRNDINT,
    // FSCALE and FXTRACT. The operation's own opcode says which it is, which
    // way round its operands go, where its result lands and what it pops, so
    // x87_decode gives the unit one X87_ARITH and one X87_CMP and the rest is
    // read off `cur_op_raw` here.
    //
    // The operand order is the trap. D8 E0+i is FSUB ST,ST(i) and D8 E8+i is
    // FSUBR ST,ST(i), but DC E0+i is FSUBR ST(i),ST and DC E8+i is FSUB
    // ST(i),ST -- the E0/E8 sense is the other way round once the
    // destination is ST(i) (386 PRM, the FSUB and FSUBR entries). Written as
    // "the destination minus the other operand", D8 E8h and DC E0h are the
    // reversed ones, which is `a_rev` below.
    logic       a_reg;      // a register form, so the second operand is ST(i)
    logic [2:0] a_rf;       // a memory form's reg field
    logic       a_mul;      // the operation is a multiply, not an add
    logic       a_sub;      // ... a subtract, which is an add with b's sign flipped
    logic       a_rev;      // ... and the operands go the other way round
    logic       a_dsti;     // the destination is ST(i), not ST(0)
    logic       a_pop1;     // the form pops ST(0) when it is done
    logic       a_pop2;     // ... twice: FCOMPP and FUCOMPP
    logic       a_unord;    // the compare is the unordered flavour (FUCOM*)
    logic [2:0] a_dst;      // the destination's physical register
    logic       a_form;     // the operation in flight is one of slice 3's
    logic       a_mem;      // ... and its second operand comes from memory
    assign a_form = cur == X87_ARITH || cur == X87_CMP || cur == X87_RNDINT
                 || cur == X87_SCALE || cur == X87_XTRACT;
    assign a_mem = (cur == X87_ARITH || cur == X87_CMP) && cur_op_raw[7:6] != 2'b11;
    always_comb begin
        a_reg = cur_op_raw[7:6] == 2'b11;
        a_rf = cur_op_raw[5:3];
        a_mul = 1'b0;
        a_sub = 1'b0;
        a_rev = 1'b0;
        a_dsti = 1'b0;
        a_pop1 = 1'b0;
        a_pop2 = 1'b0;
        a_unord = 1'b0;
        if (cur == X87_ARITH) begin
            if (!a_reg) begin
                // D8, DC, DA and DE reg fields 0, 1, 4 and 5: add, multiply,
                // subtract and reverse subtract, into ST(0).
                a_mul = a_rf == 3'd1;
                a_sub = a_rf == 3'd4 || a_rf == 3'd5;
                a_rev = a_rf == 3'd5;
            end else begin
                // C0+i and C8+i are add and multiply, E0+i and E8+i the two
                // subtracts; DC and DE put the result in ST(i), and DE pops.
                a_mul = !cur_op_raw[5] && cur_op_raw[3];
                a_sub = cur_op_raw[5];
                a_dsti = cur_op_raw[10:8] != 3'b000;
                a_rev = a_dsti ? !cur_op_raw[3] : cur_op_raw[3];
                a_pop1 = cur_op_raw[10:8] == 3'b110;
            end
        end else if (cur == X87_CMP) begin
            if (!a_reg) a_pop1 = a_rf == 3'd3;
            else begin
                // DD E0+i and E8+i are FUCOM and FUCOMP, DA E9 is FUCOMPP;
                // D8 D0+i and D8+i are FCOM and FCOMP and DE D9 is FCOMPP.
                a_unord = cur_op_raw[10:8] == 3'b101 || cur_op_raw[10:8] == 3'b010;
                a_pop2 = cur_op_raw[10:8] == 3'b110 || cur_op_raw[10:8] == 3'b010;
                a_pop1 = !a_pop2 && cur_op_raw[3];
            end
        end
    end
    assign a_dst = a_dsti ? top + cur_i : top;

    // ---- The arithmetic's operands. ----
    // `pa` and `pb` are the two values in the operation's own order, each
    // with the empty bit it was read with, and `pb`'s sign is flipped for a
    // subtract only where the arithmetic uses it: a NaN operand is delivered
    // with its own sign, so the NaN rule below reads the values as they are.
    // The adder and the multiplier take their operands from registers of
    // their own, which is the fit spike's remedy and Deano's D5 decision on
    // #1768; `ua_mul`/`ub_mul` are the multiplier's and `ua_add`/`ub_add`
    // the adder's.
    logic [79:0] pa, pb;
    logic        pa_e0, pb_e0;      // ... and whether each was empty
    logic [79:0] m_val;             // a memory form's operand, converted
    logic [5:0]  m_fl;              // ... and the flags its conversion raised
    cls_e        ca, cb;
    logic        sign_a, sign_b;
    assign ca = class_of(pa, pa_e0);
    assign cb = class_of(pb, pb_e0);
    assign sign_a = pa[79];
    assign sign_b = pb[79] ^ a_sub;

    // The effective exponent: a denormal's exponent field reads 0 and its
    // value is significand * 2^(1 - 16383 - 63), so one is 1 here and the
    // significand keeps its leading zeros until the pre-normalising shift.
    // Reading it this way also puts a pseudo-denormal (field 0 with bit 63
    // set) at the smallest normal's exponent, which is its value.
    function automatic logic signed [17:0] eff_e(input logic [79:0] v);
        eff_e = v[78:64] == 15'd0 ? 18'sd1 : $signed({3'b0, v[78:64]});
    endfunction

    // ---- The operand classes that give a result without the datapath. ----
    // An empty register, an unsupported format, a NaN, an infinity and the
    // zeros (387 PRM section 3.1.7 and Table 3.5). Everything else is a
    // finite non-zero pair, which the datapath computes.
    logic        sp_hit;
    logic [79:0] sp_v;
    logic [5:0]  sp_fl;
    logic        sp_sf;
    always_comb begin
        logic nan_a, nan_b, pick_a;
        sp_hit = 1'b1;
        sp_v = INDEFINITE;
        sp_fl = 6'd0;
        sp_sf = 1'b0;
        pick_a = 1'b1;
        nan_a = ca == C_QNAN || ca == C_SNAN;
        nan_b = cb == C_QNAN || cb == C_SNAN;
        if (ca == C_EMPTY || cb == C_EMPTY) begin
            // A stack underflow: invalid, the stack flag, and the real
            // indefinite in the destination.
            sp_fl = 6'b000001;
            sp_sf = 1'b1;
        end else if (ca == C_UNSUP || cb == C_UNSUP) begin
            sp_fl = 6'b000001;
        end else if (nan_a || nan_b) begin
            // One NaN is delivered quieted; two give the one with the larger
            // significand, and a signalling operand raises invalid. The
            // delivered NaN keeps its own sign, so a subtract's flip does not
            // reach it.
            if (nan_a && nan_b)
                pick_a = pa[62:0] > pb[62:0] ? 1'b1 : pa[62:0] < pb[62:0] ? 1'b0 : 1'b1;
            else
                pick_a = nan_a;
            sp_v = pick_a ? {pa[79:64], 2'b11, pa[61:0]} : {pb[79:64], 2'b11, pb[61:0]};
            if (ca == C_SNAN || cb == C_SNAN) sp_fl = 6'b000001;
        end else if (a_mul) begin
            if (ca == C_INF || cb == C_INF) begin
                // Infinity times zero is invalid; otherwise an infinity with
                // the product's sign.
                if (ca == C_ZERO || cb == C_ZERO) sp_fl = 6'b000001;
                else sp_v = {sign_a ^ sign_b, 15'h7fff, 1'b1, 63'd0};
            end else if (ca == C_ZERO || cb == C_ZERO) begin
                sp_v = {sign_a ^ sign_b, 79'd0};
            end else
                sp_hit = 1'b0;
        end else begin
            if (ca == C_INF && cb == C_INF) begin
                // Infinity minus infinity is invalid; two of a sign give it.
                if (sign_a != sign_b) sp_fl = 6'b000001;
                else sp_v = {sign_a, 15'h7fff, 1'b1, 63'd0};
            end else if (ca == C_INF) sp_v = {sign_a, 15'h7fff, 1'b1, 63'd0};
            else if (cb == C_INF) sp_v = {sign_b, 15'h7fff, 1'b1, 63'd0};
            else if (ca == C_ZERO && cb == C_ZERO)
                // Two zeros of a sign give it; of opposite signs the sum is
                // positive, except under round down (387 PRM section 3.1.7).
                sp_v = {sign_a == sign_b ? sign_a : rc == 2'd1, 79'd0};
            else
                sp_hit = 1'b0;
        end
        // A denormal operand raises the denormal exception whatever the
        // operation does with it -- a real x87 gives the largest denormal
        // plus zero back with DE alone -- but not beside an operand that is
        // empty, unsupported or a NaN, where the 387 reports that operand's
        // own exception or none at all. The host bench found both halves.
        if (ca != C_EMPTY && cb != C_EMPTY && ca != C_UNSUP && cb != C_UNSUP
            && ca != C_QNAN && cb != C_QNAN && ca != C_SNAN && cb != C_SNAN
            && (ca == C_DEN || cb == C_DEN)) sp_fl[1] = 1'b1;
    end

    // ---- The compares (387 PRM, the FCOM and FUCOM entries). ----
    // C3 C2 C0 are 0 0 0 when ST(0) is above the operand, 0 0 1 below,
    // 1 0 0 equal, and 1 1 1 when the two are not comparable; C1 is cleared.
    // FCOM raises invalid for any NaN, FUCOM only for a signalling one -- a
    // quiet NaN answers "not comparable" without an exception, which is what
    // the unordered compares are for. An empty register is a stack
    // underflow as well as invalid.
    //
    // The order is a magnitude compare on the effective exponent and the
    // significand, which is the value's own order: a denormal's effective
    // exponent is the smallest normal's, so its smaller significand puts it
    // below, and a pseudo-denormal, whose value *is* the smallest normal's,
    // compares equal to it rather than below.
    logic [3:0] cmp_cc;
    logic [5:0] cmp_fl;
    logic       cmp_sf;
    always_comb begin
        logic        gt, lt, eq, uo, nan_q, nan_s;
        logic [78:0] mag_a, mag_b;
        nan_q = ca == C_QNAN || cb == C_QNAN;
        nan_s = ca == C_SNAN || cb == C_SNAN;
        uo = ca == C_EMPTY || cb == C_EMPTY || ca == C_UNSUP || cb == C_UNSUP || nan_q || nan_s;
        cmp_fl = 6'd0;
        cmp_sf = 1'b0;
        if (ca == C_EMPTY || cb == C_EMPTY) begin
            cmp_fl = 6'b000001;
            cmp_sf = 1'b1;
        end else if (ca == C_UNSUP || cb == C_UNSUP || nan_s) cmp_fl = 6'b000001;
        else if (nan_q) begin
            if (!a_unord) cmp_fl = 6'b000001;
        end else if (ca == C_DEN || cb == C_DEN) cmp_fl = 6'b000010;
        mag_a = {pa[78:64] == 15'd0 ? 15'd1 : pa[78:64], pa[63:0]};
        mag_b = {pb[78:64] == 15'd0 ? 15'd1 : pb[78:64], pb[63:0]};
        gt = 1'b0;
        lt = 1'b0;
        eq = 1'b0;
        if (ca == C_ZERO && cb == C_ZERO) eq = 1'b1;     // +0 and -0 compare equal
        else if (ca == C_ZERO) begin gt = pb[79]; lt = !pb[79]; end
        else if (cb == C_ZERO) begin gt = !pa[79]; lt = pa[79]; end
        else if (pa[79] != pb[79]) begin gt = !pa[79]; lt = pa[79]; end
        else begin
            eq = mag_a == mag_b;
            gt = pa[79] ? mag_a < mag_b : mag_a > mag_b;
            lt = pa[79] ? mag_a > mag_b : mag_a < mag_b;
        end
        cmp_cc = uo ? 4'b1101 : eq ? 4'b1000 : lt ? 4'b0001 : 4'b0000;
    end

    // ---- The arithmetic's datapath. ----
    // The working value is a 67-bit field: a 65-bit integer part whose bits
    // 63:0 are the significand, with a guard and a round bit below it, and a
    // sticky bit of its own for what a shift dropped. Both operands are
    // normalised before the datapath sees them, so an add's alignment is the
    // only shift that drops bits and a subtract's cancellation is the only
    // one that needs a leading-zero count.
    logic [63:0]        oa_m, ob_m;    // the operands' significands, normalised
    logic signed [17:0] oa_e, ob_e;    // ... and their effective exponents
    logic [63:0]        wm;            // the working significand
    logic               wg, wrd, ws;   // ... its guard, round and sticky bits
    logic signed [17:0] we;            // ... and its exponent, biased by 16383
    logic               wsign;
    logic [5:0]         wfl;           // the flags the operands and the steps raised
    logic               wsf;           // ... and the stack flag
    logic [1:0]         wprec;         // the precision this operation rounds to
    logic               wtiny;         // the denormalising shift has been taken
    logic [79:0]        ares, ares2;   // the result the apply writes, and FXTRACT's second
    logic [3:0]         acc;           // the condition codes it writes
    logic               acc_wr;
    logic               a_push;        // the form pushes (FXTRACT)
    logic signed [17:0] xt_e;          // FXTRACT's exponent, as an integer

    // The adder's own operand registers, which is Deano's D5 decision on
    // #1768: the multiplier has `ua_mul`/`ub_mul` and the adder these, so
    // neither waits on the other's operand path.
    logic [63:0] ua_add, ub_add;
    logic        ub_g, ub_r, ub_s;     // the aligned operand's bits below bit 0
    logic        add_neg;              // the step is a subtract
    // The alignment the adder's load takes: the two significands, the shift,
    // and whether the smaller operand is an exact zero, which contributes
    // nothing at all rather than a sticky bit.
    logic [63:0] al_big, al_small;
    logic [6:0]  al_k;
    logic        al_zero;
    logic [2:0]  a2_idx;               // the register the apply's second cycle writes

    // The 67-bit sum or difference. The sticky is not a bit of the field, so
    // a subtract takes one unit off the round position and the caller sets
    // the sticky instead: the true difference lies in the unit below the
    // computed one, which is exactly what a round bit and a sticky say.
    logic [66:0] sum_w;
    always_comb begin
        logic [66:0] x, y;
        x = {1'b0, ua_add, 2'b00};
        y = {1'b0, ub_add, ub_g, ub_r};
        sum_w = add_neg ? x - y - 67'(ub_s) : x + y;
    end

    // ---- The value the CPU stores. ----
    // A memory store's part of the converted value; FNSTCW's, FNSTSW's and
    // an environment's first three fields out of the words themselves (the
    // CPU builds fields 3 to 6, the x87 pointers, itself); and a save
    // image's chunk of the register the unit has read.
    always_comb begin
        if (iss_dec == X87_STCW) sval = {16'hffff, cw};
        else if (iss_dec == X87_STSW) sval = {16'hffff, sw_o};
        else if (iss_dec == X87_STENV || iss_dec == X87_SAVE) begin
            if (iss_part[5])
                sval = iss_part[1:0] == 2'd0 ? ra_val[31:0]
                     : iss_part[1:0] == 2'd1 ? ra_val[63:32] : {16'd0, ra_val[79:64]};
            else
                // A 32-bit layout stores these as dwords whose upper halves
                // are reserved and read all ones, as the empty socket's and
                // the oracle's do; a 16-bit layout stores the word alone.
                sval = iss_part[2:0] == 3'd0 ? {16'hffff, cw}
                     : iss_part[2:0] == 3'd1 ? {16'hffff, sw_o} : {16'hffff, tw_o};
        end else
            sval = iss_part[1:0] == 2'd0 ? sres[31:0]
                 : iss_part[1:0] == 2'd1 ? sres[63:32] : {16'd0, sres[79:64]};
    end
    assign nx = present && pnx;

    // ---- The sequencer. ----
    // A store's conversion runs while the instruction's first uop waits in
    // D; every other form's work starts at the instruction's commit, which
    // is where every architectural change happens. A load's work therefore
    // finishes after the instruction has retired, as a real 387's does with
    // BUSY# still active: the next ESC waits on the hold above.
    logic [2:0]  push_i, pop_i;
    logic [79:0] rimg;         // a save image's register, gathered by its chunks
    assign push_i = top - 3'd1;
    assign pop_i = top;
    assign busy = seq != S_IDLE || rw_en;
    assign env_load = started && (cur == X87_LDENV || cur == X87_RSTOR);
    // The cycle FNINIT or FNSAVE commits, which is when the CPU clears the
    // x87 pointers it keeps.
    assign clr_pointers = present && wb && wb_last
                       && (nw == 2'd1 || started && (cur == X87_INIT || cur == X87_SAVE));

    // The exceptions this operation raises that are not masked, which set ES
    // and the board's latch instead of a result.
    function automatic logic unmasked(input logic [5:0] f, input logic [15:0] c);
        unmasked = |(f & ~c[5:0]);
    endfunction

    always_ff @(posedge clk) begin
        mul_start <= 1'b0;
        if (reset) begin
            cw <= 16'h037f;
            fl <= 6'd0;
            sf <= 1'b0;
            es_q <= 1'b0;
            cc <= 4'd0;
            top <= 3'd0;
            opcode <= 11'd0;
            empty <= 8'hff;
            seq <= S_IDLE;
            started <= 1'b0;
            fired <= 1'b0;
            committed <= 1'b0;
            nw <= 2'd0;
            nw_waiting <= 1'b0;
            latched <= 1'b0;
            rw_en <= 1'b0;
            pnx <= 1'b0;
        end else if (f1) begin
            // The F1h reset: the 387's RESET state (387 PRM Appendix C.1 and
            // section 4.8.1), which is FNINIT's except that IM is clear and
            // IE and ES are set, so ERROR# is active: control 037Eh, status
            // 8081h with B mirroring ES, and every register empty.
            cw <= 16'h037e;
            fl <= 6'b000001;
            sf <= 1'b0;
            es_q <= 1'b1;   // ES is already active, so the latch sees no edge
            cc <= 4'd0;
            top <= 3'd0;
            opcode <= 11'd0;
            empty <= 8'hff;
            seq <= S_IDLE;
            started <= 1'b0;
            fired <= 1'b0;
            committed <= 1'b0;
            nw <= 2'd0;
            nw_waiting <= 1'b0;
            rw_en <= 1'b0;
            pnx <= 1'b0;
        end else begin
            rw_en <= 1'b0;
            es_q <= es;
            if (f0) latched <= 1'b0;
            // A squashed instruction leaves nothing behind, but one that has
            // retired runs on: its work finishes after it, as a 387's does
            // with BUSY# still active.
            // A wrong-path FNINIT or FNCLEX never commits, so its pending
            // apply goes with the flush whatever else is in flight.
            if (flush) begin
                nw <= 2'd0;
                nw_waiting <= 1'b0;
            end
            if (flush && started && !committed) begin
                started <= 1'b0;
                fired <= 1'b0;
                committed <= 1'b0;
                pnx <= 1'b0;
                seq <= S_IDLE;
            end else begin
            if (take_nw) begin
                nw <= iss_dec == X87_INIT ? 2'd1 : 2'd2;
                nw_waiting <= 1'b1;
            end
            if (iss && iss_fire && iss_nowait) nw_waiting <= 1'b0;
            if (wb && wb_last && started) committed <= 1'b1;
            // A load uop's value, and an environment's or a save image's
            // fields, as each commits.
            if (wb && started) begin
                if (cur == X87_LDENV || cur == X87_RSTOR) begin
                    if (!wb_part[5]) begin
                        case (wb_part[2:0])
                            3'd0: cw <= wb_val[15:0];
                            3'd1: begin
                                // The status word, TOP and the flags with it;
                                // a loaded ES raises ERROR# again.
                                fl <= wb_val[5:0];
                                sf <= wb_val[6];
                                // ES and B come from the flags and the
                                // control word, not from the image.
                                cc <= {wb_val[14], wb_val[10], wb_val[9], wb_val[8]};
                                top <= wb_val[13:11];
                            end
                            3'd2: for (int n = 0; n < 8; n++) empty[n] <= wb_val[2*n +: 2] == 2'b11;
                            default:
                                // The x87 pointers, which the CPU keeps; it
                                // hands back the opcode beside them.
                                if (wb_opcode_set) opcode <= wb_opcode;
                        endcase
                    end else begin
                        // FRSTOR's register image, a register's three chunks
                        // in order; the third writes the register.
                        case (wb_part[1:0])
                            2'd0: rimg[31:0] <= wb_val;
                            2'd1: rimg[63:32] <= wb_val;
                            default: begin
                                // The image holds ST(0) to ST(7), so the
                                // register is TOP plus the image's index
                                // (387 PRM, the FRSTOR image). FRSTOR has
                                // loaded the status word, and TOP with it,
                                // before the image's chunks arrive.
                                rw_idx <= top + wb_part[4:2];
                                rw_val <= {wb_val[15:0], rimg[63:0]};
                                rw_en <= 1'b1;
                            end
                        endcase
                    end
                end else if (cur == X87_LD_M || cur == X87_LDCW || a_mem) begin
                    case (wb_part[1:0])
                        2'd0: mval[31:0] <= wb_val;
                        2'd1: mval[63:32] <= wb_val;
                        default: mval[79:64] <= wb_val[15:0];
                    endcase
                end
            end
            case (seq)
                // The forms that are not stores start their work at the
                // instruction's commit: a load has its whole operand only
                // then, and a register form has nothing to wait for.
                S_IDLE: if (started && wb && wb_last && !flush) begin
                    // S_ARG gives the last operand part a cycle to land in
                    // mval and the register reads a cycle to arrive, so
                    // S_PRE sees the whole instruction.
                    ra_idx <= top;
                    rb_idx <= top + cur_i;
                    seq <= S_ARG;
                end else if (take) begin
                    cur <= iss_dec;
                    cur_fmt <= iss_fmt;
                    // FSCALE's second operand is ST(1); its ModRM byte (D9 FDh)
                    // names no register, so the index comes from the form.
                    cur_i <= iss_dec == X87_SCALE ? 3'd1 : iss_op[2:0];
                    cur_pop <= iss_dec == X87_STP_M || iss_dec == X87_STP_ST;
                    cur_op_raw <= iss_op;
                    cur_ctrl <= ctrl_form;
                    started <= 1'b1;
                    fired <= 1'b0;
                    committed <= 1'b0;
                    pnx <= 1'b0;
                    pfl <= 6'd0;
                    psf <= 1'b0;
                    pc1 <= 1'b0;
                    pcc_wr <= 1'b0;
                    if (val_form) begin
                        ra_idx <= top;
                        seq <= S_ARG;
                    end else if (iss_dec == X87_SAVE) begin
                        ra_idx <= top + iss_part[4:2];
                        seq <= S_SAVE_RD;
                    end
                end
                // A store's conversion: the register read lands, then the
                // shift (a multiply) when the result is a denormal of the
                // target or an integer, then the rounding.
                S_ARG: seq <= S_PRE;
                S_PRE: if (cur == X87_LD_M) begin
                    // A load's normalising left shift, a multiply by 2^lz.
                    if (ld_shift) begin
                        sh_in <= ld_mag;
                        sh_k <= 7'd0;
                        sh_src <= ld_mag;
                        sh_pow <= 6'(clz64(ld_mag));
                        sh_exp <= 15'(ld_expb - 17'(clz64(ld_mag)));
                        sh_isprod <= 1'b0;
                        sh_ret <= S_APPLY;
                        seq <= S_SHIFT;
                    end else
                        seq <= S_APPLY;
                end else if (a_form) begin
                    // Slice 3's forms. A memory operand goes through the
                    // load conversions first, which is a shift of its own
                    // for a denormal real and for every integer.
                    pfl <= 6'd0;
                    psf <= 1'b0;
                    pc1 <= 1'b0;
                    pcc_wr <= 1'b0;
                    wtiny <= 1'b0;
                    wprec <= cur == X87_ARITH ? pc : PC_EXT;
                    if (a_mem && ld_shift) begin
                        sh_src <= ld_mag;
                        sh_pow <= 6'(clz64(ld_mag));
                        sh_exp <= 15'(ld_expb - 17'(clz64(ld_mag)));
                        sh_isprod <= 1'b0;
                        sh_ret <= A_CVT;
                        seq <= S_SHIFT;
                    end else begin
                        m_val <= ld_v;
                        m_fl <= a_mem ? ld_fl : 6'd0;
                        seq <= A_CAP;
                    end
                end else if (cur != X87_ST_M && cur != X87_STP_M) seq <= S_APPLY;
                       else begin
                    sh_in <= st_sig;
                    sh_k <= st_shift ? st_k : 7'd0;
                    pfl <= st_fl;
                    psf <= st_cls == C_EMPTY;
                    c_sign <= st_sign;
                    c_p <= st_p;
                    c_te <= st_te;
                    c_f64 <= f_f64;
                    c_int <= f_int;
                    c_iw <= f_iw;
                    if (st_direct) begin
                        sres <= st_v;
                        pnx <= unmasked(st_fl, cw);
                        seq <= S_RDY;
                    end else if (st_shift && st_k < 7'd64) begin
                        sh_src <= st_sig;
                        sh_pow <= 6'(7'd64 - st_k);
                        sh_isprod <= 1'b0;
                        sh_ret <= S_POST;
                        seq <= S_SHIFT;
                    end else
                        seq <= S_POST;
                end
                S_SHIFT: begin
                    ua_mul <= sh_src;
                    ub_mul <= sh_isprod ? sh_b2 : 64'd1 << sh_pow;
                    mul_start <= 1'b1;
                    seq <= S_MUL;
                end
                S_MUL: if (mul_done) seq <= sh_ret;

                // ---- Slice 3's arithmetic. ----
                // A memory operand's conversion, once its normalising shift
                // has run: the magnitude is the product's low half and the
                // exponent the one the conversion worked out before it.
                A_CVT: begin
                    m_val <= {ld_sign, sh_exp, mul_p[63:0]};
                    m_fl <= ld_fl;
                    seq <= A_CAP;
                end
                // Both operands, in the operation's own order. `pa` and `pb`
                // are registers because the operand classes, the NaN rule
                // and the compare all read them, so the cycle after this one
                // is the first that can act on them.
                A_CAP: begin
                    logic [79:0] vd, vo;
                    logic        ed, eo;
                    // The destination's value and the other operand's. For a
                    // compare and the one-operand forms the destination is
                    // ST(0) and the "other" is the operand being compared or
                    // ignored; FSCALE's is ST(1), which cur_i is set to.
                    vd = a_dsti ? rb_val : ra_val;
                    ed = a_dsti ? empty[top + cur_i] : empty[top];
                    vo = a_dsti ? ra_val : a_mem ? m_val : rb_val;
                    eo = a_dsti ? empty[top] : a_mem ? 1'b0 : empty[top + cur_i];
                    // `a_rev` is the only thing that swaps them: the two
                    // subtract senses, written as the destination minus the
                    // other operand.
                    pa <= a_rev ? vo : vd;
                    pb <= a_rev ? vd : vo;
                    pa_e0 <= a_rev ? eo : ed;
                    pb_e0 <= a_rev ? ed : eo;
                    pfl <= m_fl;
                    seq <= A_SETUP;
                end
                // The dispatch: the operand classes that need no datapath,
                // the compares, and the pre-normalising shifts. Every
                // operand the datapath sees has its significand's bit 63
                // set, so a denormal is shifted left by its leading zeros
                // and its exponent follows; the exponent is signed and
                // 18 bits wide, so it may go below zero and come back.
                A_SETUP: begin
                    logic [6:0] lza, lzb;
                    lza = clz64(pa[63:0]);
                    lzb = clz64(pb[63:0]);
                    oa_m <= pa[63:0];
                    ob_m <= pb[63:0];
                    oa_e <= eff_e(pa);
                    ob_e <= eff_e(pb);
                    wsign <= 1'b0;
                    if (cur == X87_CMP) begin
                        // A compare writes the condition codes and nothing
                        // else, and needs no rounding.
                        pfl <= pfl | cmp_fl;
                        psf <= cmp_sf;
                        pc1 <= 1'b0;
                        pcc <= cmp_cc;
                        pcc_wr <= 1'b1;
                        seq <= S_APPLY;
                    end else if (cur == X87_RNDINT || cur == X87_XTRACT) begin
                        // The one-operand forms: only ST(0)'s class counts,
                        // so the two-operand special cases do not apply.
                        pfl <= pfl | (ca == C_EMPTY || ca == C_UNSUP || ca == C_SNAN ? 6'b000001
                                    : ca == C_DEN ? 6'b000010 : 6'd0);
                        psf <= ca == C_EMPTY;
                        wsign <= pa[79];
                        if (ca == C_EMPTY || ca == C_UNSUP) begin
                            ares <= INDEFINITE;
                            ares2 <= INDEFINITE;
                            seq <= S_APPLY;
                        end else if (ca == C_QNAN || ca == C_SNAN) begin
                            ares <= {pa[79:64], 2'b11, pa[61:0]};
                            ares2 <= {pa[79:64], 2'b11, pa[61:0]};
                            seq <= S_APPLY;
                        end else if (cur == X87_XTRACT && ca == C_ZERO) begin
                            // FXTRACT of zero: the exponent is minus
                            // infinity and divide-by-zero is raised, and the
                            // significand keeps the operand's sign
                            // (387 PRM, the FXTRACT entry).
                            pfl <= pfl | 6'b000100;
                            ares <= {1'b1, 15'h7fff, 1'b1, 63'd0};
                            ares2 <= pa;
                            seq <= S_APPLY;
                        end else if (cur == X87_XTRACT && ca == C_INF) begin
                            ares <= {1'b0, 15'h7fff, 1'b1, 63'd0};
                            ares2 <= pa;
                            seq <= S_APPLY;
                        end else if (cur == X87_RNDINT && (ca == C_ZERO || ca == C_INF)) begin
                            ares <= pa;
                            seq <= S_APPLY;
                        end else if (ca == C_DEN) begin
                            oa_e <= eff_e(pa) - 18'(lza);
                            sh_src <= pa[63:0];
                            sh_pow <= 6'(lza);
                            sh_isprod <= 1'b0;
                            sh_ret <= A_PNA;
                            seq <= S_SHIFT;
                        end else
                            seq <= A_OP;
                    end else if (cur == X87_SCALE) begin
                        // FSCALE's own operand classes: the add's and the
                        // multiply's rules do not apply, because the second
                        // operand is a scale and not a term. An infinite
                        // scale takes the result to an infinity or a zero,
                        // and the two combinations that would ask for both
                        // at once are invalid (387 PRM, the FSCALE entry).
                        logic bad_pair;
                        bad_pair = (ca == C_ZERO && cb == C_INF && !pb[79])
                                || (ca == C_INF && cb == C_INF && pb[79]);
                        wsign <= pa[79];
                        if (ca == C_EMPTY || cb == C_EMPTY) begin
                            pfl <= pfl | 6'b000001;
                            psf <= 1'b1;
                            ares <= INDEFINITE;
                            seq <= S_APPLY;
                        end else if (ca == C_UNSUP || cb == C_UNSUP || bad_pair) begin
                            pfl <= pfl | 6'b000001;
                            ares <= INDEFINITE;
                            seq <= S_APPLY;
                        end else if (ca == C_QNAN || ca == C_SNAN || cb == C_QNAN || cb == C_SNAN) begin
                            pfl <= pfl | sp_fl;
                            ares <= sp_v;
                            seq <= S_APPLY;
                        end else if (cb == C_INF) begin
                            // A finite non-zero ST(0) scaled by an infinity.
                            pfl <= pfl | (ca == C_DEN ? 6'b000010 : 6'd0);
                            ares <= pb[79] ? {pa[79], 79'd0} : {pa[79], 15'h7fff, 1'b1, 63'd0};
                            seq <= S_APPLY;
                        end else if (ca == C_ZERO || ca == C_INF) begin
                            // A zero or an infinity scales to itself, and a
                            // denormal scale still raises the denormal
                            // exception (the host bench).
                            pfl <= pfl | (cb == C_DEN ? 6'b000010 : 6'd0);
                            ares <= pa;
                            seq <= S_APPLY;
                        end else begin
                            pfl <= pfl | (ca == C_DEN || cb == C_DEN ? 6'b000010 : 6'd0);
                            if (ca == C_DEN) begin
                                oa_e <= eff_e(pa) - 18'(lza);
                                sh_src <= pa[63:0];
                                sh_pow <= 6'(lza);
                                sh_isprod <= 1'b0;
                                sh_ret <= A_PNA;
                                seq <= S_SHIFT;
                            end else if (cb == C_DEN) begin
                                ob_e <= eff_e(pb) - 18'(lzb);
                                sh_src <= pb[63:0];
                                sh_pow <= 6'(lzb);
                                sh_isprod <= 1'b0;
                                sh_ret <= A_PNB;
                                seq <= S_SHIFT;
                            end else
                                seq <= A_OP;
                        end
                    end else if (sp_hit) begin
                        // An empty register, an unsupported format, a NaN,
                        // an infinity or a pair of zeros: the result is the
                        // operand classes' and the datapath never runs.
                        pfl <= pfl | sp_fl;
                        psf <= sp_sf;
                        pc1 <= sp_sf && ca != C_EMPTY && cb != C_EMPTY;
                        ares <= sp_v;
                        seq <= S_APPLY;
                    end else begin
                        // A denormal operand raises the denormal exception
                        // even where the result is exact: a real x87 sets DE
                        // for a pseudo-denormal operand as well, which is
                        // what class_of calls a denormal.
                        wsign <= sign_a;
                        pfl <= pfl | (ca == C_DEN || cb == C_DEN ? 6'b000010 : 6'd0);
                        if (ca == C_DEN) begin
                            oa_e <= eff_e(pa) - 18'(lza);
                            sh_src <= pa[63:0];
                            sh_pow <= 6'(lza);
                            sh_isprod <= 1'b0;
                            sh_ret <= A_PNA;
                            seq <= S_SHIFT;
                        end else if (cb == C_DEN) begin
                            ob_e <= eff_e(pb) - 18'(lzb);
                            sh_src <= pb[63:0];
                            sh_pow <= 6'(lzb);
                            sh_isprod <= 1'b0;
                            sh_ret <= A_PNB;
                            seq <= S_SHIFT;
                        end else
                            seq <= A_OP;
                    end
                end
                // pa's pre-normalising shift has run; pb may need one too.
                A_PNA: begin
                    oa_m <= mul_p[63:0];
                    if (cb == C_DEN && cur != X87_RNDINT && cur != X87_XTRACT) begin
                        ob_e <= ob_e - 18'(clz64(ob_m));
                        sh_src <= ob_m;
                        sh_pow <= 6'(clz64(ob_m));
                        sh_isprod <= 1'b0;
                        sh_ret <= A_PNB;
                        seq <= S_SHIFT;
                    end else
                        seq <= A_OP;
                end
                A_PNB: begin
                    ob_m <= mul_p[63:0];
                    seq <= A_OP;
                end
                // The operation's first datapath step.
                A_OP: begin
                    if (cur == X87_ARITH && a_mul) begin
                        // The product, on the one multiplier. Both operands
                        // are normalised, so it lands in [1, 4) and needs a
                        // shift of one bit at most, which A_PROD does with a
                        // wire. Its sign is the two operands' together.
                        wsign <= sign_a ^ sign_b;
                        sh_src <= oa_m;
                        sh_b2 <= ob_m;
                        sh_isprod <= 1'b1;
                        sh_ret <= A_PROD;
                        seq <= S_SHIFT;
                    end else if (cur == X87_ARITH) begin
                        // The alignment: the operand with the smaller
                        // exponent shifts right by the difference, and the
                        // sum below adds or subtracts by the effective
                        // signs. A zero operand shifts nothing: it
                        // contributes no sticky bit either, so a denormal
                        // plus zero comes back exactly (the host probe).
                        logic signed [17:0] d;
                        logic [6:0]         k;
                        logic               a_big;
                        if (ca == C_ZERO || cb == C_ZERO) begin
                            // Exactly one operand is an exact zero, because
                            // two of them were a special case above. The
                            // result is the other operand -- but it still
                            // goes through the rounding, because precision
                            // control can shorten it, and a denormal comes
                            // back as the denormal it was.
                            al_big <= ca == C_ZERO ? ob_m : oa_m;
                            al_small <= 64'd0;
                            al_k <= 7'd0;
                            al_zero <= 1'b1;
                            add_neg <= 1'b0;
                            wsign <= ca == C_ZERO ? sign_b : sign_a;
                            we <= ca == C_ZERO ? ob_e : oa_e;
                            seq <= A_LOAD;
                        end else begin
                            // Which operand is the larger magnitude. Equal
                            // exponents leave it to the significands, and a
                            // subtract that takes the larger from the
                            // smaller wraps round instead of going negative:
                            // -2^63 plus (2^64 - 1) came out as -3*2^63.
                            d = oa_e - ob_e;
                            a_big = d > 18'sd0 || (d == 18'sd0 && oa_m >= ob_m);
                            k = (a_big ? d : -d) > 18'sd66 ? 7'd66 : 7'((a_big ? d : -d));
                            al_big <= a_big ? oa_m : ob_m;
                            al_small <= a_big ? ob_m : oa_m;
                            al_k <= k;
                            al_zero <= 1'b0;
                            add_neg <= sign_a != sign_b;
                            wsign <= a_big ? sign_a : sign_b;
                            we <= a_big ? oa_e : ob_e;
                            if (k != 7'd0 && k < 7'd65) begin
                                sh_src <= a_big ? ob_m : oa_m;
                                sh_pow <= 6'(7'd64 - k);
                                sh_isprod <= 1'b0;
                                sh_ret <= A_LOAD;
                                seq <= S_SHIFT;
                            end else
                                seq <= A_LOAD;
                        end
                    end else if (cur == X87_RNDINT) begin
                        // FRNDINT: the fraction is the low bits, so a shift
                        // right by 63 minus the unbiased exponent leaves the
                        // integer in the significand and the fraction in the
                        // guard, round and sticky bits. An operand whose
                        // exponent is 63 or more is an integer already.
                        logic signed [17:0] eu;
                        logic [6:0]         k;
                        eu = oa_e - 18'sd16383;
                        k = (18'sd63 - eu) > 18'sd66 ? 7'd66 : (18'sd63 - eu) < 18'sd0 ? 7'd0
                                                             : 7'((18'sd63 - eu));
                        al_k <= k;
                        al_big <= 64'd0;
                        al_small <= oa_m;
                        al_zero <= 1'b0;
                        we <= 18'sd16383 + 18'sd63;
                        if (eu >= 18'sd63) begin
                            ares <= pa;
                            seq <= S_APPLY;
                        end else if (k < 7'd65) begin
                            sh_src <= oa_m;
                            sh_pow <= 6'(7'd64 - k);
                            sh_isprod <= 1'b0;
                            sh_ret <= A_INT;
                            seq <= S_SHIFT;
                        end else begin
                            // Smaller than the round bit: nothing but a
                            // sticky bit, which rounds to zero or to one.
                            wm <= 64'd0;
                            wg <= 1'b0;
                            wrd <= 1'b0;
                            ws <= 1'b1;
                            seq <= A_INT2;
                        end
                    end else if (cur == X87_SCALE) begin
                        // FSCALE: ST(0) scaled by ST(1) truncated toward
                        // zero. The scale's magnitude only matters up to the
                        // exponent range, so an exponent of 16 or more
                        // saturates it and the result overflows or
                        // underflows whatever the rest of ST(1) holds.
                        logic signed [17:0] eu;
                        eu = ob_e - 18'sd16383;
                        wm <= oa_m;
                        wg <= 1'b0;
                        wrd <= 1'b0;
                        ws <= 1'b0;
                        wsign <= sign_a;
                        if (cb == C_ZERO || eu < 18'sd0) begin
                            we <= oa_e;
                            seq <= A_RND;
                        end else if (eu >= 18'sd16) begin
                            // A scale of 2^16 or more takes any operand out
                            // of range, so it saturates: 36000 is past the
                            // exponent field's own span either way, because
                            // the operand's exponent is between -62 (a
                            // pre-normalised denormal) and 32766. A smaller
                            // step left a denormal scaled by 2^63 finite.
                            we <= pb[79] ? oa_e - 18'sd36000 : oa_e + 18'sd36000;
                            seq <= A_RND;
                        end else begin
                            sh_src <= ob_m;
                            sh_pow <= 6'(18'sd64 - (18'sd63 - eu));
                            sh_isprod <= 1'b0;
                            sh_ret <= A_SC;
                            seq <= S_SHIFT;
                        end
                    end else begin
                        // FXTRACT: the significand with the exponent of one,
                        // and the unbiased exponent as an integer, which the
                        // conversion below normalises.
                        ares2 <= {pa[79], 15'd16383, oa_m};
                        xt_e <= oa_e - 18'sd16383;
                        if (oa_e == 18'sd16383) begin
                            ares <= 80'd0;
                            seq <= S_APPLY;
                        end else begin
                            logic [63:0] mag;
                            mag = oa_e >= 18'sd16383 ? 64'(oa_e - 18'sd16383) : 64'(18'sd16383 - oa_e);
                            sh_src <= mag;
                            sh_pow <= 6'(clz64(mag));
                            sh_isprod <= 1'b0;
                            sh_ret <= A_XT;
                            al_k <= clz64(mag);
                            seq <= S_SHIFT;
                        end
                    end
                end
                // FSCALE's scale, once the truncating shift has run.
                A_SC: begin
                    // The truncated scale is below 2^16, because an exponent
                    // of 16 or more saturated above, so its low word is it.
                    we <= pb[79] ? oa_e - $signed({2'b0, mul_p[79:64]})
                                 : oa_e + $signed({2'b0, mul_p[79:64]});
                    seq <= A_RND;
                end
                // FXTRACT's exponent, once its magnitude has normalised.
                A_XT: begin
                    ares <= {xt_e < 18'sd0, 15'(18'sd16383 + 18'sd63 - 18'(al_k)), mul_p[63:0]};
                    seq <= S_APPLY;
                end
                // The adder's operands, which are registers of their own
                // (Deano's D5 decision on #1768). The alignment's guard and
                // round bits are the top two it shifted out and the sticky
                // the rest; at a shift of 65 the round bit is the operand's
                // own top bit, and from 66 nothing is left but the sticky.
                A_LOAD: begin
                    ua_add <= al_big;
                    if (al_zero || al_k == 7'd0) begin
                        ub_add <= al_zero ? 64'd0 : al_small;
                        ub_g <= 1'b0;
                        ub_r <= 1'b0;
                        ub_s <= 1'b0;
                    end else if (al_k == 7'd65) begin
                        ub_add <= 64'd0;
                        ub_g <= 1'b0;
                        ub_r <= al_small[63];
                        ub_s <= |al_small[62:0];
                    end else if (al_k >= 7'd66) begin
                        ub_add <= 64'd0;
                        ub_g <= 1'b0;
                        ub_r <= 1'b0;
                        ub_s <= 1'b1;
                    end else begin
                        ub_add <= mul_p[127:64];
                        ub_g <= mul_p[63];
                        ub_r <= mul_p[62];
                        ub_s <= |mul_p[61:0];
                    end
                    seq <= A_SUM;
                end
                // The sum. The significand sits at bits 65:2 of the field,
                // so a carry out lands in bit 66 and a subtract that
                // cancelled leaves bit 65 clear.
                A_SUM: begin
                    logic [66:0] sw;
                    sw = sum_w;
                    if (sw[66]) begin
                        wm <= sw[66:3];
                        wg <= sw[2];
                        wrd <= sw[1];
                        ws <= ub_s || sw[0];
                        we <= we + 18'sd1;
                        seq <= A_RND;
                    end else if (sw[65]) begin
                        wm <= sw[65:2];
                        wg <= sw[1];
                        wrd <= sw[0];
                        ws <= ub_s;
                        seq <= A_RND;
                    end else if (sw[64:1] == 64'd0) begin
                        // Exact cancellation: a positive zero, except under
                        // round down (387 PRM section 3.1.7).
                        ares <= {rc == 2'd1, 79'd0};
                        pc1 <= 1'b0;
                        seq <= S_APPLY;
                    end else begin
                        // Bit 65 clear: the magnitude is the 64 bits at
                        // 64:1, one position below the significand's, which
                        // is the -1 here, and what is left below them is bit
                        // 0 and the sticky.
                        //
                        // A shift of more than one place needs the exponents
                        // to have been within one of each other, because
                        // from a difference of two the subtrahend is at most
                        // a quarter of the minuend and the difference stays
                        // above 2^62. So where the shift is more than one
                        // place the alignment dropped nothing -- bit 0 and
                        // the sticky are both clear -- and where it is one
                        // place or none they are the result's own guard and
                        // sticky, which a borrowed sticky makes non-zero.
                        // Dropping them was what turned -1 plus 2^-16382
                        // into -(1 - 2^-64).
                        sh_src <= sw[64:1];
                        sh_pow <= 6'(clz64(sw[64:1]));
                        sh_isprod <= 1'b0;
                        sh_ret <= A_NORM;
                        we <= we - 18'sd1 - 18'(clz64(sw[64:1]));
                        wg <= sw[0];
                        wrd <= 1'b0;
                        ws <= ub_s;
                        seq <= S_SHIFT;
                    end
                end
                A_NORM: begin
                    // The guard and sticky A_SUM worked out stay as they are:
                    // a shift of more than one place only happens where they
                    // are both clear.
                    wm <= mul_p[63:0];
                    seq <= A_RND;
                end
                // The product. Both operands were normalised, so it is in
                // [1, 4): bit 127 set means it needs no shift and bit 126
                // one place, which is a wire either way.
                A_PROD: begin
                    if (mul_p[127]) begin
                        wm <= mul_p[127:64];
                        wg <= mul_p[63];
                        wrd <= mul_p[62];
                        ws <= |mul_p[61:0];
                        we <= oa_e + ob_e - 18'sd16383 + 18'sd1;
                    end else begin
                        wm <= mul_p[126:63];
                        wg <= mul_p[62];
                        wrd <= mul_p[61];
                        ws <= |mul_p[60:0];
                        we <= oa_e + ob_e - 18'sd16383;
                    end
                    seq <= A_RND;
                end
                // FRNDINT's integer, once the shift has put the fraction
                // below bit 0.
                A_INT: begin
                    wm <= mul_p[127:64];
                    wg <= mul_p[63];
                    wrd <= mul_p[62];
                    ws <= |mul_p[61:0];
                    seq <= A_INT2;
                end
                // FRNDINT rounds at bit 0 whatever PC says, then normalises
                // the integer it rounded to.
                A_INT2: begin
                    logic [66:0] rp;
                    logic [63:0] mag;
                    rp = round_pc(wm, wg, wrd, ws, wsign, rc, PC_EXT);
                    pc1 <= rp[64];
                    if (rp[65]) pfl[5] <= 1'b1;
                    // A rounding that carried out of bit 63 is 2^64, which
                    // is the significand 2^63 one exponent up.
                    mag = rp[66] ? 64'h8000_0000_0000_0000 : rp[63:0];
                    if (!rp[66] && rp[63:0] == 64'd0) begin
                        // Zero keeps the operand's sign: round toward zero
                        // of -0.3 is -0.0 (387 PRM, the FRNDINT entry).
                        ares <= {wsign, 79'd0};
                        seq <= S_APPLY;
                    end else begin
                        sh_src <= mag;
                        sh_pow <= 6'(clz64(mag));
                        al_k <= clz64(mag);
                        we <= 18'sd16383 + 18'sd63 + 18'(rp[66]);
                        sh_isprod <= 1'b0;
                        sh_ret <= A_PACK;
                        seq <= S_SHIFT;
                    end
                end
                A_PACK: begin
                    ares <= {wsign, 15'(we - 18'(al_k)), mul_p[63:0]};
                    seq <= S_APPLY;
                end
                // The denormalising shift of a tiny result, which runs
                // before the rounding so the result is rounded once.
                A_TINY: begin
                    wm <= mul_p[127:64];
                    wg <= mul_p[63];
                    wrd <= mul_p[62];
                    ws <= ws || |mul_p[61:0];
                    seq <= A_RND;
                end
                // Rounding at the destination's precision, and then the
                // exponent rules. A real x87 settles those, and the host
                // probe the record sets out is where they come from:
                //
                //   - masked overflow stores an infinity, or the precision's
                //     largest finite value where the rounding is toward
                //     zero, with OE and PE, and C1 says which way it went;
                //   - unmasked overflow stores the result with its exponent
                //     lowered by 24576, and unmasked underflow with it
                //     raised by 24576, which is the 387's bias adjust;
                //   - masked underflow denormalises the result and reports
                //     UE only where the denormalising lost bits, so a tiny
                //     exact result raises nothing at all, while unmasked
                //     underflow reports UE on any tiny result, exact or not;
                //   - the precision exception never holds a result back.
                //
                // Tininess is decided before the rounding, because the
                // denormalising has to happen first, but reported after it:
                // a denormal the rounding takes up to the smallest normal is
                // not tiny any more and raises PE alone.
                A_RND: begin
                    logic [66:0]        rp;
                    logic [63:0]        mag;
                    logic signed [17:0] e2;
                    logic [6:0]         k;
                    if (!wtiny && we < 18'sd1 && cw[4]) begin
                        k = (18'sd1 - we) > 18'sd66 ? 7'd66 : 7'((18'sd1 - we));
                        wtiny <= 1'b1;
                        we <= 18'sd1;
                        al_k <= k;
                        if (k < 7'd65) begin
                            sh_src <= wm;
                            sh_pow <= 6'(7'd64 - k);
                            sh_isprod <= 1'b0;
                            sh_ret <= A_TINY;
                            seq <= S_SHIFT;
                        end else begin
                            // Below the round bit: nothing is left but the
                            // sticky, which rounds to zero or to the
                            // smallest denormal.
                            wm <= 64'd0;
                            wg <= 1'b0;
                            wrd <= 1'b0;
                            ws <= 1'b1;
                        end
                    end else begin
                        rp = round_pc(wm, wg, wrd, ws, wsign, rc, wprec);
                        mag = rp[66] ? 64'h8000_0000_0000_0000 : rp[63:0];
                        e2 = we + 18'(rp[66]);
                        pc1 <= rp[64];
                        if (rp[65]) pfl[5] <= 1'b1;
                        if (e2 > 18'sd32766) begin
                            logic inf;
                            inf = rc == 2'd0 || (rc == 2'd1 && wsign) || (rc == 2'd2 && !wsign);
                            pfl[3] <= 1'b1;
                            if (cw[3]) begin
                                pfl[5] <= 1'b1;
                                pc1 <= inf;
                                ares <= inf ? {wsign, 15'h7fff, 1'b1, 63'd0}
                                            : {wsign, 15'h7ffe, max_sig(wprec)};
                            end else
                                ares <= {wsign, 15'(e2 - 18'sd24576), mag};
                        end else if (wtiny) begin
                            ares <= {wsign, mag[63] ? 15'd1 : 15'd0, mag};
                            if (rp[65] && !mag[63]) pfl[4] <= 1'b1;
                        end else if (we < 18'sd1) begin
                            pfl[4] <= 1'b1;
                            ares <= {wsign, 15'(e2 + 18'sd24576), mag};
                        end else
                            ares <= {wsign, 15'(e2), mag};
                        seq <= S_APPLY;
                    end
                end
                S_POST: begin
                    rnd_q <= rnd_w[64:0];
                    rnd_inex_q <= rnd_w[65];
                    rnd_up_q <= rnd_w[66];
                    seq <= S_POST2;
                end
                S_POST2: begin
                    // The rounded result, its flags, and whether an unmasked
                    // exception means the CPU must make no access.
                    logic [5:0] f;
                    logic       c1v;
                    f = pfl;
                    c1v = rnd_up_q;
                    if (c_int) begin
                        if (int_oor) begin
                            f[0] = 1'b1;
                            c1v = 1'b0;
                            sres <= {16'd0, int_indefinite(c_iw)};
                        end else begin
                            if (rnd_inex_q) f[5] = 1'b1;
                            sres <= {16'd0, int_bits};
                        end
                    end else begin
                        if (real_ovf) begin
                            // Masked overflow stores infinity, or the
                            // format's largest finite value when the
                            // rounding is toward zero (387 PRM 3.1.7); C1
                            // says which way the rounding went.
                            logic inf;
                            inf = rc == 2'd0 || (rc == 2'd1 && c_sign) || (rc == 2'd2 && !c_sign);
                            f[3] = 1'b1;
                            f[5] = 1'b1;
                            c1v = inf;
                            sres <= c_f64
                                ? (inf ? {16'd0, c_sign, 11'h7ff, 52'd0}
                                       : {16'd0, c_sign, 11'h7fe, 52'hfffffffffffff})
                                : (inf ? {48'd0, c_sign, 8'hff, 23'd0}
                                       : {48'd0, c_sign, 8'hfe, 23'h7fffff});
                        end else begin
                            if (real_unf) f[4] = 1'b1;
                            if (rnd_inex_q) f[5] = 1'b1;
                            sres <= c_f64 ? {16'd0, c_sign, real_bits[62:0]}
                                          : {48'd0, c_sign, real_bits[30:0]};
                        end
                    end
                    pfl <= f;
                    pc1 <= c1v;
                    pnx <= unmasked(f, cw);
                    seq <= S_RDY;
                end
                S_RDY: if (flush) seq <= S_IDLE;
                       else if (wb && wb_last) seq <= S_APPLY;
                // FNSAVE's register read, one for each register's three
                // chunks: the first chunk waits for it.
                S_SAVE_RD: begin
                    if (flush) seq <= S_IDLE;
                    else if (wb && wb_last) seq <= S_APPLY;
                    else if (iss && iss_part[5]) ra_idx <= top + iss_part[4:2];
                end
                // Every architectural change the operation makes, at the
                // instruction's commit. An unmasked exception leaves the
                // stack and the destination as they were and sets ES, which
                // raises ERROR# and latches BUSY#, as a 387 on an AT does.
                S_APPLY: begin
                    logic [5:0]  f;
                    logic        stk, um, c1, ccw, wr, pop, push, c1_wr, ovf;
                    logic [79:0] v;
                    logic [3:0]  nc;
                    cls_e        c0cls, cicls;
                    f = pfl;
                    stk = psf;
                    c1 = pc1;
                    ccw = 1'b0;
                    ovf = 1'b0;
                    // The control forms, FNOP and FFREE leave the condition
                    // codes as they are; every other form writes C1.
                    c1_wr = !(cur == X87_NOP || cur == X87_STCW || cur == X87_STSW || cur == X87_STENV
                              || cur == X87_LDENV || cur == X87_SAVE || cur == X87_RSTOR
                              || cur == X87_FREE || cur == X87_LDCW);
                    nc = cc;
                    if (pcc_wr) begin
                        // A compare's codes, worked out when its operands
                        // were read.
                        ccw = 1'b1;
                        nc = pcc;
                    end
                    v = 80'd0;
                    wr = 1'b0;
                    pop = 1'b0;
                    push = 1'b0;
                    c0cls = class_of(ra_val, empty[top]);
                    cicls = class_of(rb_val, empty[top + cur_i]);
                    seq <= S_IDLE;
                    started <= 1'b0;
                    fired <= 1'b0;
                    committed <= 1'b0;
                    pnx <= 1'b0;
                    case (cur)
                        X87_LD_M, X87_LD_ST, X87_CONST: begin
                            // A push: an occupied destination is a stack
                            // overflow, whose masked result is the real
                            // indefinite (387 PRM section 3.1.7).
                            push = 1'b1;
                            v = cur == X87_CONST ? constant_of(cur_i, rc)
                              : cur == X87_LD_ST ? rb_val
                              : 80'd0;
                            if (cur == X87_LD_M) begin
                                f = ld_fl;
                                v = ld_shift ? {ld_sign, sh_exp, mul_p[63:0]} : ld_v;
                            end
                            if (cur == X87_LD_ST && cicls == C_EMPTY) begin
                                f[0] = 1'b1;
                                stk = 1'b1;
                                c1 = 1'b0;
                                v = INDEFINITE;
                            end
                            if (!empty[push_i]) begin
                                f[0] = 1'b1;
                                stk = 1'b1;
                                c1 = 1'b1;
                                v = INDEFINITE;
                            end
                            wr = 1'b1;
                        end
                        X87_ST_ST, X87_STP_ST: begin
                            // FST and FSTP of ST(i): an empty ST(0) is a
                            // stack underflow.
                            v = ra_val;
                            if (c0cls == C_EMPTY) begin
                                f[0] = 1'b1;
                                stk = 1'b1;
                                c1 = 1'b0;
                                v = INDEFINITE;
                            end
                            wr = 1'b1;
                            pop = cur == X87_STP_ST;
                        end
                        X87_XCH: begin
                            // FXCH: an empty register at either end is a
                            // stack underflow, and the masked result puts
                            // the real indefinite in both.
                            if (c0cls == C_EMPTY || cicls == C_EMPTY) begin
                                f[0] = 1'b1;
                                stk = 1'b1;
                                c1 = 1'b0;
                            end
                            wr = 1'b1;
                        end
                        X87_CHS, X87_ABS: begin
                            v = {cur == X87_CHS ? ~ra_val[79] : 1'b0, ra_val[78:0]};
                            if (c0cls == C_EMPTY) begin
                                f[0] = 1'b1;
                                stk = 1'b1;
                                v = INDEFINITE;
                            end
                            c1 = 1'b0;
                            wr = 1'b1;
                        end
                        X87_TST: begin
                            // FTST compares ST(0) with +0.0: C3 C2 C0 are
                            // 0 0 0 above, 0 0 1 below, 1 0 0 equal, and
                            // 1 1 1 when the two are not comparable, which
                            // raises invalid (387 PRM, FTST).
                            ccw = 1'b1;
                            c1 = 1'b0;
                            case (c0cls)
                                C_EMPTY: begin
                                    nc = {1'b1, 1'b1, 1'b0, 1'b1};
                                    f[0] = 1'b1;
                                    stk = 1'b1;
                                end
                                C_QNAN, C_SNAN, C_UNSUP: begin
                                    nc = {1'b1, 1'b1, 1'b0, 1'b1};
                                    f[0] = 1'b1;
                                end
                                C_ZERO: nc = {1'b1, 1'b0, 1'b0, 1'b0};
                                default: begin
                                    nc = {1'b0, 1'b0, 1'b0, ra_val[79]};
                                    if (c0cls == C_DEN) f[1] = 1'b1;
                                end
                            endcase
                        end
                        X87_XAM: begin
                            // FXAM's classes (387 PRM, Table 3.6): C1 is the
                            // sign, and C3 C2 C0 name the class, with the
                            // unsupported formats 0 0 0.
                            ccw = 1'b1;
                            c1 = ra_val[79] && c0cls != C_EMPTY;
                            case (c0cls)
                                C_UNSUP: nc = {1'b0, 1'b0, 1'b0, 1'b0};
                                C_QNAN, C_SNAN: nc = {1'b0, 1'b0, 1'b0, 1'b1};
                                C_NORM: nc = {1'b0, 1'b1, 1'b0, 1'b0};
                                C_INF: nc = {1'b0, 1'b1, 1'b0, 1'b1};
                                C_ZERO: nc = {1'b1, 1'b0, 1'b0, 1'b0};
                                C_EMPTY: nc = {1'b1, 1'b0, 1'b0, 1'b1};
                                default: nc = {1'b1, 1'b1, 1'b0, 1'b0};  // denormal
                            endcase
                        end
                        X87_ARITH, X87_RNDINT, X87_SCALE: begin
                            // The result goes to ST(0), or to ST(i) for the
                            // DC and DE forms, and DE pops ST(0) after it.
                            v = ares;
                            wr = 1'b1;
                            pop = a_pop1;
                        end
                        X87_CMP: pop = a_pop1 || a_pop2;
                        X87_XTRACT: begin
                            // FXTRACT replaces ST(0) with its exponent and
                            // pushes the significand, so the push carries the
                            // significand and the apply's second cycle writes
                            // the exponent to the register ST(0) was.
                            push = 1'b1;
                            v = ares2;
                            wr = 1'b1;
                            if (!empty[push_i]) begin
                                // A stack overflow, and only that, puts the
                                // real indefinite in both: a NaN operand
                                // gives both results the quieted NaN, so the
                                // second write below tests the overflow and
                                // not the invalid flag.
                                f[0] = 1'b1;
                                stk = 1'b1;
                                c1 = 1'b1;
                                ovf = 1'b1;
                                v = INDEFINITE;
                            end
                        end
                        X87_FREE: empty[top + cur_i] <= 1'b1;
                        X87_INCSTP: begin
                            top <= top + 3'd1;
                            c1 = 1'b0;
                        end
                        X87_DECSTP: begin
                            top <= top - 3'd1;
                            c1 = 1'b0;
                        end
                        X87_INIT, X87_SAVE: begin
                            // FNINIT, and FNSAVE once its image is stored,
                            // leave the unit as FINIT does (387 PRM section
                            // 4.8.1): control 037Fh, no flags, every tag
                            // empty, the pointers and the opcode cleared.
                            cw <= 16'h037f;
                            fl <= 6'd0;
                            sf <= 1'b0;
                            cc <= 4'd0;
                            top <= 3'd0;
                            empty <= 8'hff;
                            opcode <= 11'd0;
                        end
                        X87_CLEX: begin
                            // FNCLEX clears the exception flags, the stack
                            // flag and ES, and keeps the condition codes and
                            // TOP (387 PRM, FCLEX).
                            fl <= 6'd0;
                            sf <= 1'b0;
                        end
                        X87_LDCW: begin
                            cw <= mval[15:0];
                            // A new mask set can expose a flag the old one
                            // masked, and ES follows the registered control
                            // word, so the edge detector below would see it
                            // a cycle after this apply has made the unit
                            // idle. Latch BUSY# here, against the word being
                            // loaded (387 PRM section 2.3.1: FLDCW reports
                            // an exception it unmasks at the next ESC or
                            // WAIT).
                            if (unmasked(fl | f, mval[15:0])) latched <= 1'b1;
                        end
                        X87_STENV: cw[5:0] <= 6'h3f;  // FNSTENV masks every exception
                        X87_ST_M, X87_STP_M: pop = cur_pop;
                        X87_LDENV, X87_RSTOR:
                            // The whole environment is in: ERROR# now
                            // follows the flags and masks it loaded.
                            if (es) latched <= 1'b1;
                        default: ;  // FNOP, FNSTCW, FNSTSW
                    endcase
                    // Invalid, denormal and divide-by-zero are detected
                    // before the result exists, so an unmasked one leaves the
                    // operands and TOP alone. Overflow, underflow and
                    // precision are detected after it: unmasked, the result
                    // is still written, with the bias adjust A_RND applied,
                    // so they must not hold the write back. A real x87 is
                    // where both halves come from (the host probe: 0 times
                    // infinity with IM clear leaves both operands on the
                    // stack, while an inexact add with PM clear writes its
                    // result and pops). Slice 2's forms keep the gate they
                    // were sealed and lockstepped with; whether a store with
                    // PE unmasked should make its access is its own question
                    // and the record names it.
                    um = a_form ? unmasked(f & 6'b000111, cw) : unmasked(f, cw);
                    // ERROR# rises with this apply, and the board latches
                    // BUSY# on that edge. Setting it here rather than from
                    // the edge detector below closes the cycle between this
                    // apply, which makes the unit idle, and the detector's
                    // own edge, where an ESC would see neither busy nor
                    // latched (387 PRM Appendix F.2, the AT's latch).
                    if (um) latched <= 1'b1;
                    // The flags and the stack flag are sticky; ES stays set
                    // until FNCLEX, FNINIT or the F1h reset clears it.
                    if (cur != X87_INIT && cur != X87_SAVE && cur != X87_CLEX) begin
                        fl <= fl | f;
                        if (stk) sf <= 1'b1;

                        if (c1_wr) cc[1] <= c1;
                        if (ccw) cc <= {nc[3], nc[2], c1, nc[0]};
                    end
                    // An unmasked exception leaves the registers and TOP as
                    // they were; a masked one delivers its result.
                    if (!um) begin
                        if (a_pop2) begin
                            // FCOMPP and FUCOMPP drop both registers.
                            empty[top] <= 1'b1;
                            empty[top + 3'd1] <= 1'b1;
                            top <= top + 3'd2;
                        end else if (push) begin
                            top <= push_i;
                            empty[push_i] <= 1'b0;
                            rw_idx <= push_i;
                            rw_val <= v;
                            rw_en <= wr;
                        end else if (pop) begin
                            empty[top] <= 1'b1;
                            top <= top + 3'd1;
                        end
                        if (cur == X87_ST_ST || cur == X87_STP_ST) begin
                            rw_idx <= top + cur_i;
                            rw_val <= v;
                            rw_en <= 1'b1;
                            // FSTP ST(0) writes the destination and pops the
                            // same register: the pop wins, so it reads empty.
                            if (empty[top + cur_i] && !(pop && cur_i == 3'd0))
                                empty[top + cur_i] <= 1'b0;
                        end
                        if (cur == X87_CHS || cur == X87_ABS) begin
                            rw_idx <= top;
                            rw_val <= v;
                            rw_en <= 1'b1;
                            // A masked stack underflow leaves the real
                            // indefinite in the destination, so it is no
                            // longer empty and its tag reads special.
                            empty[top] <= 1'b0;
                        end
                        if (cur == X87_XCH) begin
                            // Two writes, so two cycles: ST(0) now and
                            // ST(i) next.
                            rw_idx <= top;
                            rw_val <= f[0] ? INDEFINITE : rb_val;
                            rw_en <= 1'b1;
                            empty[top] <= 1'b0;
                            empty[top + cur_i] <= 1'b0;
                            sres <= f[0] ? INDEFINITE : ra_val;
                            a2_idx <= top + cur_i;
                            seq <= S_APPLY2;
                        end
                        if (cur == X87_ARITH || cur == X87_RNDINT || cur == X87_SCALE) begin
                            rw_idx <= a_dst;
                            rw_val <= v;
                            rw_en <= 1'b1;
                            // A popping form whose destination is ST(0) --
                            // FADDP ST(0),ST -- writes the register and pops
                            // it: the pop wins, so it reads empty.
                            if (!(pop && a_dst == top)) empty[a_dst] <= 1'b0;
                        end
                        if (cur == X87_XTRACT) begin
                            // The push above wrote the significand; the
                            // exponent goes to the register ST(0) was.
                            sres <= ovf ? INDEFINITE : ares;
                            a2_idx <= top;
                            seq <= S_APPLY2;
                        end
                    end
                end
                S_APPLY2: begin
                    rw_idx <= a2_idx;
                    rw_val <= sres;
                    rw_en <= 1'b1;
                    seq <= S_IDLE;
                end
                default: ;
            endcase
            // The opcode a non-control ESC instruction leaves behind, which
            // FNSTENV and FNSAVE store: the forms fes_i386_decode gives
            // X87P_NONE leave it, as a 387 does. It is written at the
            // instruction's commit, with the x87 pointers, so one that
            // faults or is squashed leaves it alone.
            if (wb && wb_last && started && !cur_ctrl) opcode <= cur_op_raw;
            // The instruction the unit owns has issued its first uop, which
            // is set after the case so that a take and an issue in the same
            // cycle -- a load form, which does not hold -- leaves it set.
            if (iss && iss_fire && iss_first && !iss_wait && !iss_nowait) fired <= 1'b1;
            // FNINIT and FNCLEX, at their own commit, after the case above
            // so FNINIT's reset wins over an operation finishing with it.
            if (wb && wb_last && nw != 2'd0) begin
                nw <= 2'd0;
                nw_waiting <= 1'b0;
                if (nw == 2'd1) begin
                    cw <= 16'h037f;
                    fl <= 6'd0;
                    sf <= 1'b0;
                    cc <= 4'd0;
                    top <= 3'd0;
                    empty <= 8'hff;
                    opcode <= 11'd0;
                end else begin
                    fl <= 6'd0;
                    sf <= 1'b0;
                end
            end
            end
            // The environment arrives field by field, with the control
            // word before the status word, so the summary can rise on the
            // old flags part way through: the latch waits for the whole
            // instruction, and its own apply sets it if ES is still active.
            if (es && !es_q && !env_load) latched <= 1'b1;
        end
    end
endmodule
