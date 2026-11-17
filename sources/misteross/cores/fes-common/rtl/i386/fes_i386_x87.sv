// SPDX-License-Identifier: MIT
// The 387-compatible unit inside fes_i386_cpu (docs/i386-cpu-design.md,
// "x87"; the plan in docs/validation/2026-11-12-i386-x87-plan). This is the
// x87 phase's slices 2 and 3.
//
// Slice 2 built the unit's state, its interface to the CPU, the AT board's
// error path, the loads and stores with their conversions, FXCH, the
// constants, FCHS, FABS, FTST, FXAM and the stack faults. Slice 3 adds the
// arithmetic: add, subtract and reverse subtract with their integer, ST(i)
// and popping forms, multiply, the ordered and unordered compares, FRNDINT,
// FSCALE and FXTRACT, under precision as well as rounding control, with
// every exception they raise. Divide, square root and the remainders are
// slice 4's, and the transcendentals and BCD slice 5's: a form a later slice
// builds raises FAULT_UNIMPLEMENTED (unimpl), which is how the build stops
// at an instruction it does not run.
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
//   the sticky bit. The one multiplier is the 9-DSP block below, and slice
//   3's multiply shares it: every step that needs it goes through S_SHIFT
//   and S_MUL and returns to the state `sh_ret` names, so an operation that
//   pre-normalises both operands, aligns them and renormalises the result
//   takes four passes of the same block and no second multiplier.
//
// - **Operand registers.** The multiplier and the adder take their operands
//   from registers of their own (`ua_mul`/`ub_mul` and `ua_add`/`ub_add`),
//   which is the fit spike's remedy for the one shared operand register its
//   standalone fits could not route, and Deano's D5 decision on #1768.
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
module fes_i386_x87 #(
    // Which arithmetic implementation the unit's A_RND state is fed from.
    // 0 is this file's own sequenced datapath (the A_* and D_* states), which
    // is the DE10's and the verified reference. 1 is the DE25's pipelined
    // `fes_x87_core`, whose pre-rounding `raw_t` lands in the same working
    // registers A_RND already reads. Only one is instantiated: at 1 the
    // sequencer's adder, its Goldschmidt divide and its reciprocal seed table
    // are unreachable and synthesis removes them. The rounding, the masked and
    // unmasked rules, the stack, the conversions and the AT error path are
    // the same code either way, which is the point of the split.
    parameter int X87_BACKEND = 0
) (
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
    // Six bits, not five: slice 4 took the sequencer past 31 states, and a
    // truncated trace reported Q_SEED as a low-numbered slice-2 state --
    // wrong precisely while diagnosing the new square-root path. Codex
    // found it on PR #1889.
    output logic [5:0]  trace_seq,
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

    // The seed is read synchronously from the divisor's significand, which is
    // stable from A_SETUP (or A_PNB for a denormal) onwards, so the entry is
    // valid by A_OP. A registered read is also what infers M10K rather than
    // logic.
    // ---- Slice 4: the reciprocal seed table, and the divide's registers. ----
    // 128 words of 10 bits in M10K, indexed by the divisor's bits 62:56 --
    // the shape the fit spike's standalone divider used. The entry
    // approximates 1/b for b in [0.5, 1) as v/512, so v is in [512, 1024]
    // and the seed always has bit 63 set once it is left-aligned.
    //
    // Goldschmidt with these 64-bit registers floors at about 2^-61 relative
    // error however long it runs, because every intermediate is the top 64
    // bits of the multiplier's product. That is short of the 67 bits the
    // extended format needs, and short in a way no iteration count fixes, so
    // the iterations are followed by an exact tail: a candidate guaranteed
    // below the true truncation and `k` restoring compare-subtract steps that
    // close the gap and leave the exact remainder. The sticky bit round_pc()
    // needs is that remainder being non-zero, which the tail produces for
    // free. docs/validation/2026-11-15-i386-x87-s4/design.md has the
    // measurements and model/fixed_constants.py is the check.
    (* ramstyle = "M10K" *) logic [9:0] rcp_tab [0:127];
    // Literals, and not an initial loop that computes them: the locked Yosys
    // rejects `real` in a module body ("syntax error, unexpected TOK_REAL"),
    // which the netlist bench found on the first run. The values are
    // 1/b for b = 0.5 + (i + 0.5)/256 as v/512, rounded to nearest;
    // model/seed_tables.py emits exactly this block and --check verifies the
    // RTL still holds it, so they are reproducible rather than magic.
    initial begin
        rcp_tab[0] = 10'd1020; rcp_tab[1] = 10'd1012; rcp_tab[2] = 10'd1004; rcp_tab[3] = 10'd997; rcp_tab[4] = 10'd989; rcp_tab[5] = 10'd982; rcp_tab[6] = 10'd975; rcp_tab[7] = 10'd967;
        rcp_tab[8] = 10'd960; rcp_tab[9] = 10'd953; rcp_tab[10] = 10'd946; rcp_tab[11] = 10'd940; rcp_tab[12] = 10'd933; rcp_tab[13] = 10'd926; rcp_tab[14] = 10'd920; rcp_tab[15] = 10'd913;
        rcp_tab[16] = 10'd907; rcp_tab[17] = 10'd901; rcp_tab[18] = 10'd895; rcp_tab[19] = 10'd889; rcp_tab[20] = 10'd883; rcp_tab[21] = 10'd877; rcp_tab[22] = 10'd871; rcp_tab[23] = 10'd865;
        rcp_tab[24] = 10'd859; rcp_tab[25] = 10'd854; rcp_tab[26] = 10'd848; rcp_tab[27] = 10'd843; rcp_tab[28] = 10'd838; rcp_tab[29] = 10'd832; rcp_tab[30] = 10'd827; rcp_tab[31] = 10'd822;
        rcp_tab[32] = 10'd817; rcp_tab[33] = 10'd812; rcp_tab[34] = 10'd807; rcp_tab[35] = 10'd802; rcp_tab[36] = 10'd797; rcp_tab[37] = 10'd792; rcp_tab[38] = 10'd787; rcp_tab[39] = 10'd783;
        rcp_tab[40] = 10'd778; rcp_tab[41] = 10'd773; rcp_tab[42] = 10'd769; rcp_tab[43] = 10'd764; rcp_tab[44] = 10'd760; rcp_tab[45] = 10'd755; rcp_tab[46] = 10'd751; rcp_tab[47] = 10'd747;
        rcp_tab[48] = 10'd743; rcp_tab[49] = 10'd738; rcp_tab[50] = 10'd734; rcp_tab[51] = 10'd730; rcp_tab[52] = 10'd726; rcp_tab[53] = 10'd722; rcp_tab[54] = 10'd718; rcp_tab[55] = 10'd714;
        rcp_tab[56] = 10'd710; rcp_tab[57] = 10'd707; rcp_tab[58] = 10'd703; rcp_tab[59] = 10'd699; rcp_tab[60] = 10'd695; rcp_tab[61] = 10'd692; rcp_tab[62] = 10'd688; rcp_tab[63] = 10'd684;
        rcp_tab[64] = 10'd681; rcp_tab[65] = 10'd677; rcp_tab[66] = 10'd674; rcp_tab[67] = 10'd670; rcp_tab[68] = 10'd667; rcp_tab[69] = 10'd664; rcp_tab[70] = 10'd660; rcp_tab[71] = 10'd657;
        rcp_tab[72] = 10'd654; rcp_tab[73] = 10'd650; rcp_tab[74] = 10'd647; rcp_tab[75] = 10'd644; rcp_tab[76] = 10'd641; rcp_tab[77] = 10'd638; rcp_tab[78] = 10'd635; rcp_tab[79] = 10'd632;
        rcp_tab[80] = 10'd629; rcp_tab[81] = 10'd626; rcp_tab[82] = 10'd623; rcp_tab[83] = 10'd620; rcp_tab[84] = 10'd617; rcp_tab[85] = 10'd614; rcp_tab[86] = 10'd611; rcp_tab[87] = 10'd608;
        rcp_tab[88] = 10'd605; rcp_tab[89] = 10'd603; rcp_tab[90] = 10'd600; rcp_tab[91] = 10'd597; rcp_tab[92] = 10'd594; rcp_tab[93] = 10'd592; rcp_tab[94] = 10'd589; rcp_tab[95] = 10'd586;
        rcp_tab[96] = 10'd584; rcp_tab[97] = 10'd581; rcp_tab[98] = 10'd579; rcp_tab[99] = 10'd576; rcp_tab[100] = 10'd574; rcp_tab[101] = 10'd571; rcp_tab[102] = 10'd569; rcp_tab[103] = 10'd566;
        rcp_tab[104] = 10'd564; rcp_tab[105] = 10'd561; rcp_tab[106] = 10'd559; rcp_tab[107] = 10'd557; rcp_tab[108] = 10'd554; rcp_tab[109] = 10'd552; rcp_tab[110] = 10'd550; rcp_tab[111] = 10'd547;
        rcp_tab[112] = 10'd545; rcp_tab[113] = 10'd543; rcp_tab[114] = 10'd541; rcp_tab[115] = 10'd538; rcp_tab[116] = 10'd536; rcp_tab[117] = 10'd534; rcp_tab[118] = 10'd532; rcp_tab[119] = 10'd530;
        rcp_tab[120] = 10'd527; rcp_tab[121] = 10'd525; rcp_tab[122] = 10'd523; rcp_tab[123] = 10'd521; rcp_tab[124] = 10'd519; rcp_tab[125] = 10'd517; rcp_tab[126] = 10'd515; rcp_tab[127] = 10'd513;
    end
    logic [9:0]  rcp_q;          // the seed, valid the cycle after ob_m settles
    // The only read of the table. With the DE25 core selected nothing reads
    // rcp_q either, so the table has no reader and is not built: that is the
    // marker to look for in the fit, because an M10K or M20K that survived
    // would mean the Goldschmidt divide came along uninvited.
    always_ff @(posedge clk) if (X87_BACKEND == 0) rcp_q <= rcp_tab[ob_m[62:56]];

    // The reciprocal-square-root seeds: 256 words of 11 bits, indexed by the
    // exponent's parity and the radicand's top 7 bits. The entry approximates
    // 1/sqrt(a) for a in [1, 4) as v/1024, so v is in [512, 1024].
    // The entries run from 513 to 1022 over the whole table -- a is never
    // exactly 1, because the index is an interval's midpoint -- so ten bits
    // hold them and the left-aligned seed always has bit 63 set.
    (* ramstyle = "M10K" *) logic [9:0] rsq_tab [0:255];
    // 1/sqrt(a) for a = (1 + (i + 0.5)/128) * 2^parity as v/1024, from the
    // same generator. Literals for the same reason as the reciprocal's.
    initial begin
        rsq_tab[0] = 10'd1022; rsq_tab[1] = 10'd1018; rsq_tab[2] = 10'd1014; rsq_tab[3] = 10'd1010; rsq_tab[4] = 10'd1006; rsq_tab[5] = 10'd1003; rsq_tab[6] = 10'd999; rsq_tab[7] = 10'd995;
        rsq_tab[8] = 10'd992; rsq_tab[9] = 10'd988; rsq_tab[10] = 10'd984; rsq_tab[11] = 10'd981; rsq_tab[12] = 10'd977; rsq_tab[13] = 10'd974; rsq_tab[14] = 10'd971; rsq_tab[15] = 10'd967;
        rsq_tab[16] = 10'd964; rsq_tab[17] = 10'd960; rsq_tab[18] = 10'd957; rsq_tab[19] = 10'd954; rsq_tab[20] = 10'd951; rsq_tab[21] = 10'd948; rsq_tab[22] = 10'd944; rsq_tab[23] = 10'd941;
        rsq_tab[24] = 10'd938; rsq_tab[25] = 10'd935; rsq_tab[26] = 10'd932; rsq_tab[27] = 10'd929; rsq_tab[28] = 10'd926; rsq_tab[29] = 10'd923; rsq_tab[30] = 10'd920; rsq_tab[31] = 10'd917;
        rsq_tab[32] = 10'd914; rsq_tab[33] = 10'd912; rsq_tab[34] = 10'd909; rsq_tab[35] = 10'd906; rsq_tab[36] = 10'd903; rsq_tab[37] = 10'd901; rsq_tab[38] = 10'd898; rsq_tab[39] = 10'd895;
        rsq_tab[40] = 10'd892; rsq_tab[41] = 10'd890; rsq_tab[42] = 10'd887; rsq_tab[43] = 10'd885; rsq_tab[44] = 10'd882; rsq_tab[45] = 10'd880; rsq_tab[46] = 10'd877; rsq_tab[47] = 10'd875;
        rsq_tab[48] = 10'd872; rsq_tab[49] = 10'd870; rsq_tab[50] = 10'd867; rsq_tab[51] = 10'd865; rsq_tab[52] = 10'd862; rsq_tab[53] = 10'd860; rsq_tab[54] = 10'd858; rsq_tab[55] = 10'd855;
        rsq_tab[56] = 10'd853; rsq_tab[57] = 10'd851; rsq_tab[58] = 10'd848; rsq_tab[59] = 10'd846; rsq_tab[60] = 10'd844; rsq_tab[61] = 10'd842; rsq_tab[62] = 10'd839; rsq_tab[63] = 10'd837;
        rsq_tab[64] = 10'd835; rsq_tab[65] = 10'd833; rsq_tab[66] = 10'd831; rsq_tab[67] = 10'd829; rsq_tab[68] = 10'd826; rsq_tab[69] = 10'd824; rsq_tab[70] = 10'd822; rsq_tab[71] = 10'd820;
        rsq_tab[72] = 10'd818; rsq_tab[73] = 10'd816; rsq_tab[74] = 10'd814; rsq_tab[75] = 10'd812; rsq_tab[76] = 10'd810; rsq_tab[77] = 10'd808; rsq_tab[78] = 10'd806; rsq_tab[79] = 10'd804;
        rsq_tab[80] = 10'd802; rsq_tab[81] = 10'd800; rsq_tab[82] = 10'd799; rsq_tab[83] = 10'd797; rsq_tab[84] = 10'd795; rsq_tab[85] = 10'd793; rsq_tab[86] = 10'd791; rsq_tab[87] = 10'd789;
        rsq_tab[88] = 10'd787; rsq_tab[89] = 10'd786; rsq_tab[90] = 10'd784; rsq_tab[91] = 10'd782; rsq_tab[92] = 10'd780; rsq_tab[93] = 10'd778; rsq_tab[94] = 10'd777; rsq_tab[95] = 10'd775;
        rsq_tab[96] = 10'd773; rsq_tab[97] = 10'd771; rsq_tab[98] = 10'd770; rsq_tab[99] = 10'd768; rsq_tab[100] = 10'd766; rsq_tab[101] = 10'd765; rsq_tab[102] = 10'd763; rsq_tab[103] = 10'd761;
        rsq_tab[104] = 10'd760; rsq_tab[105] = 10'd758; rsq_tab[106] = 10'd757; rsq_tab[107] = 10'd755; rsq_tab[108] = 10'd753; rsq_tab[109] = 10'd752; rsq_tab[110] = 10'd750; rsq_tab[111] = 10'd749;
        rsq_tab[112] = 10'd747; rsq_tab[113] = 10'd745; rsq_tab[114] = 10'd744; rsq_tab[115] = 10'd742; rsq_tab[116] = 10'd741; rsq_tab[117] = 10'd739; rsq_tab[118] = 10'd738; rsq_tab[119] = 10'd736;
        rsq_tab[120] = 10'd735; rsq_tab[121] = 10'd733; rsq_tab[122] = 10'd732; rsq_tab[123] = 10'd731; rsq_tab[124] = 10'd729; rsq_tab[125] = 10'd728; rsq_tab[126] = 10'd726; rsq_tab[127] = 10'd725;
        rsq_tab[128] = 10'd723; rsq_tab[129] = 10'd720; rsq_tab[130] = 10'd717; rsq_tab[131] = 10'd714; rsq_tab[132] = 10'd712; rsq_tab[133] = 10'd709; rsq_tab[134] = 10'd706; rsq_tab[135] = 10'd704;
        rsq_tab[136] = 10'd701; rsq_tab[137] = 10'd699; rsq_tab[138] = 10'd696; rsq_tab[139] = 10'd694; rsq_tab[140] = 10'd691; rsq_tab[141] = 10'd689; rsq_tab[142] = 10'd686; rsq_tab[143] = 10'd684;
        rsq_tab[144] = 10'd681; rsq_tab[145] = 10'd679; rsq_tab[146] = 10'd677; rsq_tab[147] = 10'd675; rsq_tab[148] = 10'd672; rsq_tab[149] = 10'd670; rsq_tab[150] = 10'd668; rsq_tab[151] = 10'd666;
        rsq_tab[152] = 10'd663; rsq_tab[153] = 10'd661; rsq_tab[154] = 10'd659; rsq_tab[155] = 10'd657; rsq_tab[156] = 10'd655; rsq_tab[157] = 10'd653; rsq_tab[158] = 10'd651; rsq_tab[159] = 10'd649;
        rsq_tab[160] = 10'd647; rsq_tab[161] = 10'd645; rsq_tab[162] = 10'd643; rsq_tab[163] = 10'd641; rsq_tab[164] = 10'd639; rsq_tab[165] = 10'd637; rsq_tab[166] = 10'd635; rsq_tab[167] = 10'd633;
        rsq_tab[168] = 10'd631; rsq_tab[169] = 10'd629; rsq_tab[170] = 10'd627; rsq_tab[171] = 10'd626; rsq_tab[172] = 10'd624; rsq_tab[173] = 10'd622; rsq_tab[174] = 10'd620; rsq_tab[175] = 10'd618;
        rsq_tab[176] = 10'd617; rsq_tab[177] = 10'd615; rsq_tab[178] = 10'd613; rsq_tab[179] = 10'd611; rsq_tab[180] = 10'd610; rsq_tab[181] = 10'd608; rsq_tab[182] = 10'd606; rsq_tab[183] = 10'd605;
        rsq_tab[184] = 10'd603; rsq_tab[185] = 10'd601; rsq_tab[186] = 10'd600; rsq_tab[187] = 10'd598; rsq_tab[188] = 10'd597; rsq_tab[189] = 10'd595; rsq_tab[190] = 10'd594; rsq_tab[191] = 10'd592;
        rsq_tab[192] = 10'd590; rsq_tab[193] = 10'd589; rsq_tab[194] = 10'd587; rsq_tab[195] = 10'd586; rsq_tab[196] = 10'd584; rsq_tab[197] = 10'd583; rsq_tab[198] = 10'd581; rsq_tab[199] = 10'd580;
        rsq_tab[200] = 10'd579; rsq_tab[201] = 10'd577; rsq_tab[202] = 10'd576; rsq_tab[203] = 10'd574; rsq_tab[204] = 10'd573; rsq_tab[205] = 10'd571; rsq_tab[206] = 10'd570; rsq_tab[207] = 10'd569;
        rsq_tab[208] = 10'd567; rsq_tab[209] = 10'd566; rsq_tab[210] = 10'd565; rsq_tab[211] = 10'd563; rsq_tab[212] = 10'd562; rsq_tab[213] = 10'd561; rsq_tab[214] = 10'd559; rsq_tab[215] = 10'd558;
        rsq_tab[216] = 10'd557; rsq_tab[217] = 10'd555; rsq_tab[218] = 10'd554; rsq_tab[219] = 10'd553; rsq_tab[220] = 10'd552; rsq_tab[221] = 10'd550; rsq_tab[222] = 10'd549; rsq_tab[223] = 10'd548;
        rsq_tab[224] = 10'd547; rsq_tab[225] = 10'd546; rsq_tab[226] = 10'd544; rsq_tab[227] = 10'd543; rsq_tab[228] = 10'd542; rsq_tab[229] = 10'd541; rsq_tab[230] = 10'd540; rsq_tab[231] = 10'd538;
        rsq_tab[232] = 10'd537; rsq_tab[233] = 10'd536; rsq_tab[234] = 10'd535; rsq_tab[235] = 10'd534; rsq_tab[236] = 10'd533; rsq_tab[237] = 10'd532; rsq_tab[238] = 10'd530; rsq_tab[239] = 10'd529;
        rsq_tab[240] = 10'd528; rsq_tab[241] = 10'd527; rsq_tab[242] = 10'd526; rsq_tab[243] = 10'd525; rsq_tab[244] = 10'd524; rsq_tab[245] = 10'd523; rsq_tab[246] = 10'd522; rsq_tab[247] = 10'd521;
        rsq_tab[248] = 10'd520; rsq_tab[249] = 10'd519; rsq_tab[250] = 10'd518; rsq_tab[251] = 10'd517; rsq_tab[252] = 10'd516; rsq_tab[253] = 10'd515; rsq_tab[254] = 10'd514; rsq_tab[255] = 10'd513;
    end
    logic [9:0]  rsq_q;
    logic        sq_p;           // the radicand's exponent parity
    // The only read of the square root's seed table, guarded for the same
    // reason as the reciprocal's: with the DE25 core selected nothing reads
    // rsq_q, so the table has no reader and is not built. Quartus Pro found
    // that this one had been missed -- it refused the Agilex 5 fit because
    // the surviving table still carried the DE10's `ramstyle = "M10K"`,
    // which Agilex 5 has no such block for. A fit that refuses is a better
    // exclusion check than a fit that quietly carries the table.
    always_ff @(posedge clk) if (X87_BACKEND == 0) rsq_q <= rsq_tab[{sq_p, oa_m[62:56]}];

    // The square root's working values, each a 64-bit significand with a
    // small signed exponent: value = sig * 2^(exp - 63). The multiplier's
    // operands both have bit 63 set, so a product needs at most one bit of
    // renormalisation, which is a mux and not a shift.
    logic [63:0]        qy_s;    // y, the reciprocal square root
    logic signed [5:0]  qy_e;
    logic [63:0]        qt_s;    // 3 - a*y^2
    logic signed [5:0]  qt_e;
    logic [63:0]        qz_s;    // a*y, then a*y^2
    logic signed [5:0]  qz_e;

    logic [63:0] gs_n, gs_d;     // Goldschmidt's numerator and denominator
    logic [1:0]  gs_it;          // iterations still to run
    logic [66:0] dv_c;           // the candidate quotient/root, 66 bits plus a carry
    logic [95:0] dv_rem;         // low word of the signed residual
    logic [37:0] dv_hi_rem;      // exact high word, or the fallback radicand
    logic [3:0]  dv_k;           // restoring steps still to run
    logic [6:0]  dv_exact_left;  // quotient/root digits still to generate
    logic        dv_s;           // the pre-scale: the quotient was below one
    logic        dv_lost;        // exact fallback bits below the selected precision
    logic        dv_exact_used;  // the certified digit-by-digit fallback supplied the result
    logic [63:0] pr_q;           // the remainder step's quotient
    logic [6:0]  pr_n;           // ... how many quotient bits it consumed
    logic        pr_part;        // ... and whether it is a partial step
    logic        pr_one;         // the form is FPREM1, so the quotient rounds
    logic [95:0] dv_sub;          // the tail's subtrahend, held and shifted one
                                  // place a step rather than formed by a
                                  // variable shift (see D_TAIL)
    logic [95:0] dv_pow;          // ... and 2^(2i), which the square root's
                                  // trial needs and which halves twice a step

    // E and k size the fast correction tail. Their empirical maxima are not
    // correctness assumptions: D_CHECK/Q_CHECK certify the full residual, and
    // an out-of-range candidate takes the exact digit-by-digit fallback.
    // PC 01 is reserved and reads as the extended format, exactly as
    // round_pc() and Bochs's softfloat read it, so it takes the extended
    // bound and step count and not the short ones. Getting that wrong left
    // the reserved encoding one ulp low with the precision exception set --
    // 150 of 484 cases in the bench's pc01 rows, and nothing else.
    function automatic logic [7:0] dv_e(input logic [1:0] prec);
        dv_e = (prec == 2'd0 || prec == 2'd2) ? 8'd4 : 8'd128;
    endfunction
    function automatic logic [3:0] dv_steps(input logic [1:0] prec);
        dv_steps = (prec == 2'd0 || prec == 2'd2) ? 4'd3 : 4'd8;
    endfunction
    function automatic logic [1:0] dv_iters(input logic [1:0] prec);
        dv_iters = prec == 2'd0 ? 2'd2 : 2'd3;   // PC 24 needs two, the rest three
    endfunction
    // How many low bits of the 66-bit target this precision does not need.
    // The 66-bit target's bits 65:2 are the significand, bit 1 the guard and
    // bit 0 the round. round_pc() at PC 24 reads m[63:40], the round bit
    // m[39] and a sticky over everything below, so the exact truncation it
    // needs stops at bit 39 -- which is the 66-bit target with its low 41
    // bits dropped. PC 53 stops at m[10], dropping 12. The empirical error
    // and step counts are tuned AT THIS POSITION for the fast path; the
    // full-width certificate and exact fallback make short results sound
    // without treating those measurements as an all-input bound.
    function automatic logic [5:0] dv_drop(input logic [1:0] prec);
        dv_drop = prec == 2'd0 ? 6'd41 : prec == 2'd2 ? 6'd12 : 6'd0;
    endfunction
    function automatic logic [6:0] dv_digits(input logic [1:0] prec);
        dv_digits = 7'd66 - 7'(dv_drop(prec));
    endfunction
    function automatic logic low_dropped_bits(input logic [66:0] v, input logic [1:0] prec);
        case (prec)
            2'd0:    low_dropped_bits = |v[40:0];
            2'd2:    low_dropped_bits = |v[11:0];
            default: low_dropped_bits = 1'b0;
        endcase
    endfunction
    // High word of A * 2^(65+s-drop), above bit 95. PC 24's dividend
    // fits below bit 96; the other positions are constant shifts.
    function automatic logic [35:0] dvdhi(input logic [63:0] m, input logic s,
                                          input logic [1:0] prec);
        case (prec)
            2'd0:    dvdhi = 36'd0;
            2'd2:    dvdhi = s ? 36'(m >> 42) : 36'(m >> 43);
            default: dvdhi = s ? 36'(m >> 30) : 36'(m >> 31);
        endcase
    endfunction
    // High word of the scaled square-root radicand used by the fast path.
    function automatic logic [35:0] sqradhi(input logic [63:0] m, input logic p,
                                            input logic [1:0] prec);
        case (prec)
            2'd0:    sqradhi = 36'd0;
            2'd2:    sqradhi = p ? 36'(m >> 52) : 36'(m >> 53);
            default: sqradhi = p ? 36'(m >> 28) : 36'(m >> 29);
        endcase
    endfunction
    // Multiply three high bits by a 64-bit operand using shifts and adds, so
    // the certificate does not infer another DSP multiplier.
    function automatic logic [66:0] mul3wide(input logic [2:0] a, input logic [63:0] b);
        mul3wide = (a[0] ? {3'd0, b} : 67'd0)
                 + (a[1] ? {2'd0, b, 1'b0} : 67'd0)
                 + (a[2] ? {1'd0, b, 2'b0} : 67'd0);
    endfunction
    // The drop is one of **three constants**, so each place it is applied is a
    // three-way mux of constant shifts and not a shifter. Writing
    // `v >> dv_drop(prec)` gives Quartus a variable amount and it builds the
    // general thing; these say what is meant. Option A has no barrel shifter.
    //
    // This was only possible once the remainders stopped sharing these paths:
    // FPREM needed a genuinely variable drop, and while it did, muxing the
    // divide's and the square root's constants bought nothing.
    function automatic logic [66:0] drop67(input logic [66:0] v, input logic [1:0] prec);
        case (prec)
            2'd0:    drop67 = v >> 41;
            2'd2:    drop67 = v >> 12;
            default: drop67 = v;
        endcase
    endfunction
    // The dividend at the remainder's scale: 65 + s - drop, so 24/25, 53/54
    // or 65/66 -- six constants, selected by the precision and the pre-scale.
    function automatic logic [95:0] dvd96(input logic [63:0] m, input logic s,
                                          input logic [1:0] prec);
        case (prec)
            2'd0:    dvd96 = s ? (96'(m) << 25) : (96'(m) << 24);
            2'd2:    dvd96 = s ? (96'(m) << 54) : (96'(m) << 53);
            default: dvd96 = s ? (96'(m) << 66) : (96'(m) << 65);
        endcase
    endfunction
    // The exact truncation put back where round_pc() reads it: the candidate's
    // bits land at m[63:39] at PC 24 and m[63:10] at PC 53, with the guard and
    // round bits clear because the sticky already covers what was not
    // computed.
    function automatic logic [63:0] place64(input logic [66:0] c, input logic [1:0] prec);
        case (prec)
            2'd0:    place64 = {c[24:0], 39'd0};
            2'd2:    place64 = {c[53:0], 10'd0};
            default: place64 = c[65:2];
        endcase
    endfunction
    // The square root's own bound and step count, measured the same way:
    // 64 and 7 at the extended format, 4 and 3 at PC 24 and PC 53.
    function automatic logic [7:0] sq_e(input logic [1:0] prec);
        sq_e = (prec == 2'd0 || prec == 2'd2) ? 8'd4 : 8'd64;
    endfunction
    function automatic logic [3:0] sq_steps(input logic [1:0] prec);
        sq_steps = (prec == 2'd0 || prec == 2'd2) ? 4'd3 : 4'd7;
    endfunction

    // One step of the square root's Newton iteration keeps every value
    // normalised with bit 63 set, so a product of two such values has bit 127
    // or bit 126 set and renormalising is a choice between two slices.
    function automatic logic [63:0] norm_hi(input logic [127:0] pr);
        norm_hi = pr[127] ? pr[127:64] : pr[126:63];
    endfunction
    function automatic logic signed [5:0] norm_e(input logic signed [5:0] e,
                                                input logic p127);
        norm_e = p127 ? e : e - 6'sd1;
    endfunction

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
    typedef enum logic [5:0] {
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
        A_XT,       // FXTRACT's exponent, once its magnitude has normalised
        // ---- Slice 4's divide. ----
        // Goldschmidt on the shared multiplier, a short correction tail, and
        // an exact residual check with a digit-by-digit fallback.
        D_SEED,     // the reciprocal seed is out of the M10K table: start N = A*R
        D_N0,       // take N = A*R, start D = B*R
        D_D0,       // take D = B*R; decide whether to iterate again
        D_ITN,      // an iteration's N*f: take it, start D*f
        D_ITD,      // ... take D*f, and loop or fall through
        D_CAND,     // form the candidate C0 = qhat - E, start C0[63:0]*B
        D_REM,      // take the product and form the full-width residual
        D_TAIL,     // the restoring compare-subtract steps, one a clock
        D_CHECK,    // certify the full-width residual or restart exactly
        D_EXACT,    // exact digit-by-digit divide or square root fallback
        D_FIN,      // the quotient, its guard, round and sticky, into A_RND
        // ---- Slice 4's square root. ----
        // Newton on the reciprocal square root, y <- y(3 - a*y^2)/2, on the
        // same multiplier, then the same exact tail with the square root's
        // own trial: (C + 2^i)^2 - C^2 is (C << (i+1)) + 2^(2i), two shifts
        // and an add, so the tail needs no multiply at all.
        Q_SEED,     // the reciprocal-square-root seed is out: start a*y
        Q_AY,       // take a*y, start (a*y)*y
        Q_AY2,      // take a*y^2 and form 3 - a*y^2, then start y*that
        Q_Y,        // take the new y, halve it, loop or fall through
        Q_RES,      // the result estimate q = a*y
        Q_CAND,     // the candidate, and start C0*C0
        Q_REM,      // the radicand's remainder
        Q_CHECK,    // certify the full-width residual or restart exactly
        Q_FIN,      // into A_RND
        // ---- Slice 4's remainders. ----
        // FPREM and FPREM1 run on the divide's primitive: the divide's tail
        // already produces the exact 66-bit truncation T of A*2^(65+s)/B, and
        // the quotient a partial or complete step needs is just T shifted
        // right, because floor(floor(x*2^k)/2^k) is floor(x). One more
        // multiply gives the exact remainder for that quotient.
        P_SH,       // the remainder's doubling loop: no Goldschmidt, no shifter
        P_FIN,      // FPREM1's round-to-nearest and the normalising shift
        P_ZERO,     // ... an exact zero remainder, which is packed not shifted
        P_PACK      // ... and the normalised magnitude, C2 and the quotient bits
        ,
        // ---- The DE25 backend's two states (X87_BACKEND = 1). ----
        // Appended, not inserted: the A_* and D_* states keep the
        // encodings they had, so `trace_seq` means the same thing in a
        // default build as it did before this change.
        B_ISSUE,    // offer the operation to fes_x87_core
        B_WAIT,     // ... and take its pre-rounding result into A_RND's registers
        // ---- Packed decimal (X87_BACKEND = 1, #1958). ----
        // Appended for the same reason. FBLD and FBSTP are built on the DE25
        // backend only, and every register these four arms write is written
        // nowhere else, so a DE10 build holds none of them: the start arms
        // exist so that not even the loop counters need a write outside a
        // guarded arm (x87_envelope_fit.py derives that list from this file).
        T_BLD_IN,   // FBLD: take the 18 digits
        T_BLD,      // ... acc = acc * 10 + digit, 18 times, then the normalising shift
        T_BST_IN,   // FBSTP: take the rounded integer
        T_BST,      // ... double-dabble it to 18 digits, a bit a clock, and pack
        // ---- The transcendentals (X87_BACKEND = 1, #1958). ----
        T_GO,       // start fes_x87_trans on the normalised operands
        T_RUN       // ... and take its last result, unrounded, into A_RND's registers
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
    assign trace_seq = 6'(seq);

    // ---- Packed decimal: FBLD and FBSTP (DF /4 and /6, #1958). ----
    // Built on the DE25 backend only. Their operand is ten bytes, moved in
    // the chunks FLD and FSTP m80 use, so it arrives as X87F_F80 (all eight
    // format values are taken); the opcode says which it is, since DF's only
    // other memory forms are integers. Both `cur_fmt` and `cur_op_raw` are
    // set when the instruction is taken, so this is settled from S_ARG on.
    // At X87_BACKEND = 0 it is constant zero and every use of it folds away.
    logic bcd;
    assign bcd = X87_BACKEND == 1 && cur_fmt == X87F_F80 && cur_op_raw[10:8] == 3'b111;
    // FBLD's working registers: the digits still to take, most significant
    // first, the binary integer so far and the count left. Eighteen digits
    // of at most 15 each stay below 2^61, so 64 bits hold any encoding.
    logic [63:0] bl_acc;
    logic [71:0] bl_dig;
    logic [4:0]  bl_n;
    // FBSTP's: the rounded integer still to shift out (at most 10^18 - 1,
    // which is below 2^60), the digits so far and the count left.
    logic [59:0] bs_bin;
    logic [71:0] bs_bcd;
    logic [5:0]  bs_n;
    // Double-dabble's adjustment: a digit of five or more gains three
    // before the shift, so the shift's doubling carries into the next digit
    // (5..9 become 8..12, and twice 5..9 is 10..18).
    // A function, as this file's other loops are: the locked Yosys loses the
    // package's type names after a `for (int ...)` loop in an always block
    // (the netlist bench found it on the first run of this code), and a
    // loop in a function does not have that effect.
    function automatic logic [71:0] bcd_adjust(input logic [71:0] d);
        for (int n = 0; n < 18; n++)
            bcd_adjust[4*n +: 4] = d[4*n +: 4] >= 4'd5 ? d[4*n +: 4] + 4'd3 : d[4*n +: 4];
    endfunction
    logic [71:0] bs_adj;
    assign bs_adj = bcd_adjust(bs_bcd);
    // The value a masked invalid FBSTP stores, "packed decimal indefinite".
    // The 387 PRM (Table 3-7) fixes only its top two bytes, FFh FFh, and
    // prints the low eight as undefined; C0h then zeros is what the host
    // x87 and the oracle both store, so bytes 0 to 7 are theirs, measured,
    // and not the manual's.
    localparam logic [79:0] BCD_INDEFINITE = 80'hffff_c000_0000_0000_0000;
    // The largest magnitude 18 digits hold, 10^18 - 1.
    localparam logic [64:0] BCD_MAX = 65'h0_0de0_b6b3_a763_ffff;

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
    assign iss_dec = x87_decode(iss_op, iss_fmt, X87_BACKEND == 1);
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
            X87F_F80:
                if (bcd) begin
                    // FBLD: the 18 digits as the binary integer T_BLD left
                    // in bl_acc, normalised as FILD's magnitude is. It is
                    // exact and raises nothing. The sign is bit 79 and bits
                    // 78 to 72 are ignored (387 PRM Figure 2-10, note 3),
                    // and unlike FILD a zero keeps its sign: "the sign of
                    // the source is preserved, including the case where the
                    // value is negative zero" (387 PRM section 4.3.8).
                    // **bl_acc is a register T_BLD writes**, so this is the
                    // loaded value only once T_BLD's count has run out:
                    // T_BLD's last step and S_APPLY read it, S_PRE must not.
                    ld_sign = mval[79];
                    if (bl_acc == 64'd0) ld_v = {mval[79], 79'd0};
                    else begin
                        ld_shift = 1'b1;
                        ld_mag = bl_acc;
                        ld_expb = 17'd16446;
                    end
                end else ld_v = mval;
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
                    // FLD quiets a signalling NaN and raises invalid. An
                    // arithmetic or compare form does neither here: the NaN
                    // reaches the operation still signalling, so the 387's
                    // NaN rule can tell it from a quiet one -- it delivers
                    // the quiet operand where the other signals -- and the
                    // operation raises the invalid itself.
                    ld_v = f32 == 23'd0 ? {mval[31], 15'h7fff, 1'b1, 63'd0}
                                        : {mval[31], 15'h7fff, 1'b1,
                                           f32[22] || a_mem ? f32 : {1'b1, f32[21:0]}, 40'd0};
                    if (f32 != 23'd0 && !f32[22] && !a_mem) ld_fl = 6'b000001;
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
                                        : {mval[63], 15'h7fff, 1'b1,
                                           f64[51] || a_mem ? f64 : {1'b1, f64[50:0]}, 11'd0};
                    if (f64 != 52'd0 && !f64[51] && !a_mem) ld_fl = 6'b000001;
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
    assign f_int = cur_fmt == X87F_I16 || cur_fmt == X87F_I32 || cur_fmt == X87F_I64 || bcd;
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
            X87F_F80: if (bcd) begin
                // FBSTP rounds ST(0) to an integer under RC exactly as the
                // 64-bit integer store below does (f_iw is 2 for this
                // format), and S_POST2 then range-checks the rounded
                // magnitude against 18 digits and T_BST converts it. What
                // differs here: a zero keeps its sign in the sign byte, and
                // every invalid source -- empty, a NaN, an infinity, an
                // unsupported format, or a magnitude of 2^64 or more, which
                // cannot be 18 digits whatever it rounds to -- stores the
                // packed decimal indefinite (387 PRM Table 3-10). A denormal
                // or pseudo-denormal source raises no denormal exception:
                // the PRM's Appendix B lists only IS, I and P for FBSTP, and
                // the host x87 and the oracle were both measured to set PE
                // alone.
                case (st_cls)
                    C_ZERO: st_v = {st_sign, 79'd0};
                    C_EMPTY, C_QNAN, C_SNAN, C_INF, C_UNSUP: begin
                        st_big = 1'b1;
                        st_fl = 6'b000001;
                        st_v = BCD_INDEFINITE;
                    end
                    default: begin
                        st_direct = 1'b0;
                        st_p = P_1;
                        if (st_e > 18'sd63) begin
                            st_big = 1'b1;
                            st_direct = 1'b1;
                            st_fl = 6'b000001;
                            st_v = BCD_INDEFINITE;
                        end else if (st_e == 18'sd63) begin
                            st_p = P_0;
                        end else begin
                            st_k = (18'sd62 - st_e) > 18'sd64 ? 7'd64 : 7'((18'sd62 - st_e));
                            st_shift = st_k != 7'd0;
                        end
                    end
                endcase
            end else begin
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
    logic       a_div;      // ... a divide (slice 4)
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
                 || cur == X87_SCALE || cur == X87_XTRACT || cur == X87_DIV
                 || cur == X87_SQRT || cur == X87_PREM || cur == X87_TRANS;
    assign a_mem = (cur == X87_ARITH || cur == X87_CMP || cur == X87_DIV)
                && cur_op_raw[7:6] != 2'b11;
    always_comb begin
        a_reg = cur_op_raw[7:6] == 2'b11;
        a_rf = cur_op_raw[5:3];
        a_mul = 1'b0;
        a_div = cur == X87_DIV;
        a_sub = 1'b0;
        a_rev = 1'b0;
        a_dsti = 1'b0;
        a_pop1 = 1'b0;
        a_pop2 = 1'b0;
        a_unord = 1'b0;
        if (cur == X87_ARITH || cur == X87_DIV) begin
            if (!a_reg) begin
                // D8, DC, DA and DE reg fields 0, 1, 4 and 5: add, multiply,
                // subtract and reverse subtract, into ST(0). Fields 6 and 7
                // are the two divides, and 7 is the reversed one just as 5 is.
                a_mul = cur == X87_ARITH && a_rf == 3'd1;
                a_sub = cur == X87_ARITH && (a_rf == 3'd4 || a_rf == 3'd5);
                a_rev = a_rf == 3'd5 || a_rf == 3'd7;
            end else begin
                // C0+i and C8+i are add and multiply, E0+i and E8+i the two
                // subtracts, F0+i and F8+i the two divides; DC and DE put the
                // result in ST(i), and DE pops. The F0/F8 sense is E0/E8's,
                // including the swap once the destination is ST(i): DC F0+i is
                // FDIVR ST(i),ST and DC F8+i is FDIV ST(i),ST (386 PRM).
                a_mul = cur == X87_ARITH && !cur_op_raw[5] && cur_op_raw[3];
                a_sub = cur == X87_ARITH && cur_op_raw[5];
                a_dsti = cur_op_raw[10:8] != 3'b000;
                a_rev = a_dsti ? !cur_op_raw[3] : cur_op_raw[3];
                a_pop1 = cur_op_raw[10:8] == 3'b110;
            end
        end else if (cur == X87_TRANS) begin
            // FYL2X, FYL2XP1 and FPATAN take ST(1) and ST(0), leave the
            // result in ST(1) and pop: the destination and the pop are
            // FADDP ST(1),ST's, so `pa` is ST(1) and `pb` is ST(0).
            a_dsti = t_two;
            a_pop1 = t_two;
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

    // ---- The transcendentals: which, and its shape. ----
    // The engine's instruction number (fes_x87_trans_pkg TR_*) off the
    // opcode's low byte. FSINCOS and FPTAN push; the three two-operand forms
    // pop; the four trigonometric forms have the 2^63 range and C2.
    logic [2:0] t_ins;
    logic       t_two, t_push, t_trig;
    always_comb begin
        case (cur_op_raw[7:0])
            8'hfe: t_ins = 3'd0;      // FSIN
            8'hff: t_ins = 3'd1;      // FCOS
            8'hfb: t_ins = 3'd2;      // FSINCOS
            8'hf2: t_ins = 3'd3;      // FPTAN
            8'hf0: t_ins = 3'd4;      // F2XM1
            8'hf1: t_ins = 3'd5;      // FYL2X
            8'hf9: t_ins = 3'd6;      // FYL2XP1
            default: t_ins = 3'd7;    // FPATAN
        endcase
    end
    assign t_two = t_ins >= 3'd5;
    assign t_push = t_ins == 3'd2 || t_ins == 3'd3;
    assign t_trig = t_ins <= 3'd3;
    logic       tr_c2;       // a trigonometric form sets C2
    logic       tr_nowr;     // ... and, out of range, writes and pushes nothing
    logic       tr_eng;      // the result is the engine's: inexact by definition
    logic       tr_under;    // a stack underflow, which a push form's overflow check must not override

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

    // The effective exponent's low bit, which is all the square root's parity
    // needs. A denormal's effective exponent is 1, so its parity bit is 1;
    // every other value's is the exponent field's own bit 0. Subtracting the
    // pre-normalising shift flips it by that shift's low bit, so the parity
    // after normalisation is one XOR away and needs no subtract.
    function automatic logic eff_e0(input logic [79:0] v);
        eff_e0 = v[78:64] == 15'd0 ? 1'b1 : v[64];
    endfunction

    // ---- The operand classes that give a result without the datapath. ----
    // An empty register, an unsupported format, a NaN, an infinity and the
    // zeros (387 PRM section 3.1.7 and Table 3.5). Everything else is a
    // finite non-zero pair, which the datapath computes.
    logic        sp_hit;
    logic [79:0] sp_v;
    logic [5:0]  sp_fl;
    logic        sp_sf;
    // An operand that is empty, an unsupported format or a NaN: the result
    // and the flags are that operand's alone, so a denormal the memory
    // conversion already reported is dropped along with it.
    logic        sp_bad;
    assign sp_bad = ca == C_EMPTY || cb == C_EMPTY || ca == C_UNSUP || cb == C_UNSUP
                 || ca == C_QNAN || cb == C_QNAN || ca == C_SNAN || cb == C_SNAN;
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
            if (nan_a && nan_b) begin
                // A quiet operand beats a signalling one, and otherwise the
                // larger significand wins (Intel's NaN rules; the host bench
                // settles it, where a quiet ST(0) beats a signalling memory
                // operand whose payload is larger).
                if (ca == C_SNAN && cb == C_QNAN) pick_a = 1'b0;
                else if (ca == C_QNAN && cb == C_SNAN) pick_a = 1'b1;
                else if (pa[62:0] != pb[62:0]) pick_a = pa[62:0] > pb[62:0];
                // Equal payloads: the positive one is delivered. The host
                // bench settles it both ways round -- a negative ST(0) with
                // a positive ST(1) of the same payload gives the positive
                // one, and so does the other way round -- so it is the sign
                // that decides and not which operand it is.
                else pick_a = !pa[79];
            end else
                pick_a = nan_a;
            sp_v = pick_a ? {pa[79:64], 2'b11, pa[61:0]} : {pb[79:64], 2'b11, pb[61:0]};
            if (ca == C_SNAN || cb == C_SNAN) sp_fl = 6'b000001;
        end else if (a_div) begin
            // The divide (387 PRM section 3.1.7 and Table 3.5). Zero over
            // zero and infinity over infinity are invalid; a finite non-zero
            // over zero is the DIVIDE-BY-ZERO exception, which is a
            // PRE-operation one, so unmasked it leaves the operands and TOP
            // alone and reports nothing else (slice 3's rule 1).
            if (ca == C_INF && cb == C_INF) sp_fl = 6'b000001;
            else if (ca == C_ZERO && cb == C_ZERO) sp_fl = 6'b000001;
            else if (ca == C_INF) sp_v = {sign_a ^ sign_b, 15'h7fff, 1'b1, 63'd0};
            else if (cb == C_INF) sp_v = {sign_a ^ sign_b, 79'd0};
            else if (cb == C_ZERO) begin
                sp_fl = 6'b000100;
                sp_v = {sign_a ^ sign_b, 15'h7fff, 1'b1, 63'd0};
            end else if (ca == C_ZERO) sp_v = {sign_a ^ sign_b, 79'd0};
            else sp_hit = 1'b0;
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
        //
        // Slice 4 adds the third half, also measured: **a denormal operand
        // does not raise DE beside the divide's own invalid or
        // divide-by-zero either.** A pseudo-denormal divided by zero reports
        // ZE alone on the host, not DE and ZE, which is the same shape as
        // the rule above and as slice 3's rule 5 -- the operation's own
        // pre-operation exception is the only thing reported. Four of 684
        // cases say so.
        if (!sp_bad && !sp_fl[0] && !sp_fl[2] && (ca == C_DEN || cb == C_DEN))
            sp_fl[1] = 1'b1;
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
    logic               wsat;          // FSCALE's scale saturated, so the true exponent is
                                       // out of the bias adjust's reach by construction
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
    // ---- The DE25 arithmetic backend (X87_BACKEND = 1). ----
    // `fes_x87_core` takes the operands as they stand -- it normalises
    // denormals itself, so the A_PNA/A_PNB pre-normalising shifts are not on
    // this path -- and returns the result BEFORE rounding. Its `raw_t` and
    // A_RND's working registers are the same representation: the exponent is
    // biased by 16383 and signed, the significand's bit 66 is the integer
    // bit, and bits 2, 1 and 0 are the guard, round and sticky. The bit
    // positions were checked against round_pc at all three precisions rather
    // than assumed; the record records that check.
    //
    // The lanes raise IE, DE and ZE only -- never PE, UE or OE, which are the
    // rounding's -- so OR-ing `bk_raw.flags` into `pfl` cannot double-count
    // what A_RND is about to set.
    logic                     bk_req_valid, bk_req_ready, bk_raw_valid;
    logic [2:0]               bk_op;
    logic                     bk_cancel;
    fes_x87_pkg::raw_t        bk_raw;
    // The CPU's `flush` must NOT reach the backend. Arithmetic is entered
    // after the instruction commits (S_IDLE on `wb && wb_last`), and a
    // retired instruction deliberately runs on through a later squash
    // (`flush && started && !committed` below is the only thing that stops
    // it), exactly as a 387 does with BUSY# still active. A straight-through
    // flush would cancel a committed operation and leave the unit waiting for
    // a result that was thrown away.
    assign bk_cancel = reset || f1 || (flush && started && !committed);
    // The transcendental engine drives the same backend while it runs, one
    // operation at a time, rounded to nearest at extended precision.
    logic                     tr_start, tr_busy, tr_done, tr_req_valid, tr_fin_zero, tr_fin_sign;
    logic [2:0]               tr_req_op;
    logic [79:0]              tr_req_a, tr_req_b, tr_second;
    logic signed [17:0]       tr_fin_exp;
    logic [66:0]              tr_fin_sig;
    logic [1:0]               tr_second_mode;
    assign tr_start = X87_BACKEND == 1 && seq == T_GO;
    generate
        if (X87_BACKEND == 1) begin : g_de25
            fes_x87_core core (
                .clk, .reset(reset || f1), .flush(bk_cancel),
                .req_valid(tr_busy ? tr_req_valid : bk_req_valid), .ext_ready(1'b1),
                .req_ready(bk_req_ready),
                .req_op(tr_busy ? tr_req_op : bk_op), .req_a(tr_busy ? tr_req_a : pa),
                .req_b(tr_busy ? tr_req_b : pb),
                .req_pc(tr_busy ? 2'd3 : wprec), .req_rc(tr_busy ? 2'd0 : rc), .req_slot(4'd0),
                .raw_valid(bk_raw_valid), .raw(bk_raw)
            );
            // x is ST(0) and y ST(1): a two-operand form has them in `pb`
            // and `pa`, a one-operand form has x in `pa`.
            fes_x87_trans trans (
                .clk, .reset(reset || f1), .cancel(bk_cancel),
                .start(tr_start), .instruction(t_ins),
                .xs(t_two ? pb[79] : pa[79]), .xe((t_two ? ob_e : oa_e) - 18'sd16383),
                .xm(t_two ? ob_m : oa_m),
                .ys(pa[79]), .ye(oa_e - 18'sd16383), .ym(oa_m),
                .req_valid(tr_req_valid), .req_ready(bk_req_ready), .req_op(tr_req_op),
                .req_a(tr_req_a), .req_b(tr_req_b), .raw_valid(bk_raw_valid), .raw(bk_raw),
                .busy(tr_busy), .done(tr_done), .fin_zero(tr_fin_zero), .fin_sign(tr_fin_sign),
                .fin_exp(tr_fin_exp), .fin_sig(tr_fin_sig), .second_mode(tr_second_mode),
                .second(tr_second)
            );
        end else begin : g_de10
            assign bk_req_ready = 1'b0;
            assign bk_raw_valid  = 1'b0;
            assign bk_raw        = '0;
            assign tr_busy = 1'b0;
            assign tr_done = 1'b0;
            assign tr_req_valid = 1'b0;
            assign tr_req_op = 3'd0;
            assign tr_req_a = 80'd0;
            assign tr_req_b = 80'd0;
            assign tr_fin_zero = 1'b0;
            assign tr_fin_sign = 1'b0;
            assign tr_fin_exp = 18'sd0;
            assign tr_fin_sig = 67'd0;
            assign tr_second_mode = 2'd0;
            assign tr_second = 80'd0;
        end
    endgenerate
    // The decoded operation, from the same form bits the sequencer's datapath
    // reads. `pb`'s sign already carries `a_sub` through `sign_b`, but the
    // backend takes the operands as stored, so a subtract is OP_SUB rather
    // than an add of a negated operand.
    always_comb begin
        if (cur == X87_SQRT)      bk_op = 3'(fes_x87_pkg::OP_SQRT);
        else if (cur == X87_DIV)  bk_op = 3'(fes_x87_pkg::OP_DIV);
        else if (a_mul)           bk_op = 3'(fes_x87_pkg::OP_MUL);
        else if (a_sub)           bk_op = 3'(fes_x87_pkg::OP_SUB);
        else                      bk_op = 3'(fes_x87_pkg::OP_ADD);
    end
    // The forms the backend answers. Everything else -- FRNDINT, FSCALE,
    // FXTRACT, FPREM and FPREM1 -- stays on the sequencer, because each needs
    // the shifter the conversions need anyway and none needs the adder or the
    // divider. The transcendentals are built on neither and stop in decode.
    assign bk_req_valid = seq == B_ISSUE;
    // How many operations of each kind the backend has been offered and has
    // taken. Nothing reads these in hardware, so synthesis removes them; the
    // `verilator public_flat_rd` attribute is what lets a bench read them, and
    // it is the whole mechanism -- no port is added, so no other instantiation
    // of fes_i386_cpu changes. The lockstep run reports them per operation,
    // because a green lockstep that issued no arithmetic proves nothing: on a
    // slice-2 base every arithmetic form decodes to X87_UNIMPL and the gate
    // would pass having reached the backend zero times.
    logic [31:0] bk_offered [0:7] /* verilator public_flat_rd */;
    logic [31:0] bk_taken [0:7] /* verilator public_flat_rd */;
    // No reset: a lockstep run resets the CPU between programs and the count
    // is wanted over the whole run, not the last program.
    always_ff @(posedge clk) begin
        if (bk_req_valid) bk_offered[bk_op] <= bk_offered[bk_op] + 32'd1;
        if (bk_req_valid && bk_req_ready) bk_taken[bk_op] <= bk_taken[bk_op] + 32'd1;
    end
    logic bk_form;
    assign bk_form = X87_BACKEND == 1
                  && (cur == X87_ARITH || cur == X87_DIV || cur == X87_SQRT);

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
    // Whether an unmasked PRE-operation exception fired: invalid, denormal or
    // divide-by-zero. Those are detected before the result exists, so an
    // unmasked one reports nothing else at all -- not the other flags, and
    // not the condition codes a remainder would otherwise write.
    function automatic logic pre_um(input logic [5:0] f, input logic [15:0] c);
        pre_um = |(f[2:0] & ~c[2:0]);
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
            dv_hi_rem <= 38'd0;
            dv_exact_left <= 7'd0;
            dv_exact_used <= 1'b0;
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
            dv_hi_rem <= 38'd0;
            dv_exact_left <= 7'd0;
            dv_exact_used <= 1'b0;
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
                    cur_i <= (iss_dec == X87_SCALE || iss_dec == X87_PREM || iss_dec == X87_TRANS) ? 3'd1
                           : iss_op[2:0];
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
                    // FBLD's digits become a binary integer first, and the
                    // conversion above is that integer's: nothing here may
                    // read ld_shift or ld_mag for it yet.
                    if (bcd) seq <= T_BLD_IN;
                    // A load's normalising left shift, a multiply by 2^lz.
                    else if (ld_shift) begin
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
                    wsat <= 1'b0;
                    // Precision control applies to add, subtract, multiply,
                    // divide and square root and to nothing else (387 PRM
                    // section 2.3.2).
                    wprec <= (cur == X87_ARITH || cur == X87_DIV || cur == X87_SQRT)
                           ? pc : PC_EXT;
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
                    // The square root's parity, set here because the seed is
                    // a registered read of it: the radicand's exponent has to
                    // be made even, so the parity is zero where oa_e is odd.
                    sq_p <= ~eff_e0(pa);
                    wsign <= 1'b0;
                    if (cur == X87_CMP) begin
                        // A compare writes the condition codes and nothing
                        // else, and needs no rounding.
                        pfl <= sp_bad ? cmp_fl : pfl | cmp_fl;
                        psf <= cmp_sf;
                        pc1 <= 1'b0;
                        pcc <= cmp_cc;
                        pcc_wr <= 1'b1;
                        seq <= S_APPLY;
                    end else if (X87_BACKEND == 1 && cur == X87_TRANS) begin
                        // ---- The transcendentals' operand classes. ----
                        // Everything the engine is not entered for: the stack
                        // faults, the unsupported formats and the NaNs, which
                        // are the arithmetic's rules, and each instruction's
                        // zeros, infinities and range, which are the 387's
                        // (docs/validation/2026-11-17-de25-x87-transcendentals
                        // rules.md, by rule number). A finite non-zero operand
                        // in range goes on to the engine through the same
                        // pre-normalising shifts the arithmetic uses.
                        logic        go, viapi;
                        logic [5:0]  tf;         // a two-operand answer's flags
                        logic [1:0]  pik;        // which multiple of pi/4: 0 pi/4, 1 pi/2, 2 3pi/4, 3 pi
                        logic        sx, sy, x_one, x_lt1, x_neg1, x_ltneg1;
                        go = 1'b0;
                        viapi = 1'b0;
                        tf = 6'd0;
                        pik = 2'd0;
                        sy = pa[79];
                        sx = pb[79];
                        x_one = pb == 80'h3fff_8000_0000_0000_0000;
                        x_lt1 = pb[78:64] < 15'h3fff;
                        x_neg1 = pb == 80'hbfff_8000_0000_0000_0000;
                        x_ltneg1 = pb[79] && pb[78:0] > 79'h3fff_8000_0000_0000_0000;
                        tr_c2 <= 1'b0;
                        tr_nowr <= 1'b0;
                        tr_eng <= 1'b0;
                        tr_under <= 1'b0;
                        pc1 <= 1'b0;
                        ares2 <= 80'h3fff_8000_0000_0000_0000;      // FPTAN's 1.0, and the cosine of zero
                        if (!t_two) begin
                            // One operand, x in `pa`.
                            if (ca == C_EMPTY) begin
                                // G13: a stack underflow.
                                pfl <= 6'b000001;
                                psf <= 1'b1;
                                tr_under <= 1'b1;
                                ares <= INDEFINITE;
                                ares2 <= INDEFINITE;
                            end else if (t_push && !empty[push_i]) begin
                                // G13, TAN2: a stack overflow, which the
                                // apply finds again and marks with C1.
                                pfl <= 6'b000001;
                                psf <= 1'b1;
                                ares <= INDEFINITE;
                                ares2 <= INDEFINITE;
                            end else if (ca == C_UNSUP) begin
                                pfl <= 6'b000001;               // G7
                                ares <= INDEFINITE;
                                ares2 <= INDEFINITE;
                            end else if (ca == C_QNAN || ca == C_SNAN) begin
                                pfl <= ca == C_SNAN ? 6'b000001 : 6'd0;     // G8
                                ares <= {pa[79:64], 2'b11, pa[61:0]};
                                ares2 <= {pa[79:64], 2'b11, pa[61:0]};
                            end else if (ca == C_INF) begin
                                if (t_trig) begin
                                    // TRIG4: invalid, the indefinite, and C2
                                    // set, which the manual states and the
                                    // modern references do not do.
                                    pfl <= 6'b000001;
                                    tr_c2 <= 1'b1;
                                    ares <= INDEFINITE;
                                    ares2 <= INDEFINITE;
                                end else
                                    // EXP2: 2^+inf - 1 is +inf, 2^-inf - 1 is -1.
                                    ares <= pa[79] ? 80'hbfff_8000_0000_0000_0000 : pa;
                            end else if (ca == C_ZERO) begin
                                // SIN1, COS1, SC1, TAN1, EXP2: exact, no flag.
                                ares <= t_ins == 3'd1 ? 80'h3fff_8000_0000_0000_0000 : pa;
                            end else if (t_trig && ca != C_DEN && pa[78:64] >= 15'd16383 + 15'd63) begin
                                // TRIG2: |x| >= 2^63. C2, and nothing else.
                                tr_c2 <= 1'b1;
                                tr_nowr <= 1'b1;
                            end else
                                go = 1'b1;
                        end else if (sp_bad) begin
                            // Two operands, y in `pa` and x in `pb`: an empty
                            // register, an unsupported format or a NaN is the
                            // arithmetic's rule (G7, G8, G13).
                            pfl <= sp_fl;
                            psf <= sp_sf;
                            ares <= sp_v;
                        end else if (t_ins == 3'd7) begin
                            // FPATAN: no operand pair is invalid (ATAN3, ATAN4).
                            if (ca == C_ZERO) begin
                                if (!sx) ares <= {sy, 79'd0};       // over a positive x: the zero
                                else begin viapi = 1'b1; pik = 2'd3; end
                            end else if (ca == C_INF) begin
                                viapi = 1'b1;
                                pik = cb == C_INF ? (sx ? 2'd2 : 2'd0) : 2'd1;
                            end else if (cb == C_ZERO) begin
                                viapi = 1'b1;
                                pik = 2'd1;
                            end else if (cb == C_INF) begin
                                if (!sx) ares <= {sy, 79'd0};
                                else begin viapi = 1'b1; pik = 2'd3; end
                            end else
                                go = 1'b1;
                        end else if (t_ins == 3'd5) begin
                            // FYL2X (LOG1 to LOG4).
                            if (cb == C_ZERO) begin
                                if (ca == C_ZERO) begin
                                    tf = 6'b000001;
                                    ares <= INDEFINITE;
                                end else begin
                                    // log2 0 is minus infinity: a zero divide
                                    // for a finite y.
                                    if (ca != C_INF) tf = 6'b000100;
                                    ares <= {!sy, 15'h7fff, 1'b1, 63'd0};
                                end
                            end else if (sx) begin
                                tf = 6'b000001;               // LOG2: a negative x
                                ares <= INDEFINITE;
                            end else if (cb == C_INF) begin
                                if (ca == C_ZERO) begin
                                    tf = 6'b000001;
                                    ares <= INDEFINITE;
                                end else
                                    ares <= {sy, 15'h7fff, 1'b1, 63'd0};
                            end else if (x_one) begin
                                if (ca == C_INF) begin
                                    tf = 6'b000001;
                                    ares <= INDEFINITE;
                                end else
                                    ares <= {sy, 79'd0};
                            end else if (ca == C_ZERO)
                                ares <= {sy ^ x_lt1, 79'd0};
                            else if (ca == C_INF)
                                ares <= {sy ^ x_lt1, 15'h7fff, 1'b1, 63'd0};
                            else
                                go = 1'b1;
                        end else begin
                            // FYL2XP1 (LOG5, LOG6).
                            if (cb == C_ZERO) begin
                                if (ca == C_INF) begin
                                    tf = 6'b000001;
                                    ares <= INDEFINITE;
                                end else
                                    ares <= {sy ^ sx, 79'd0};
                            end else if (cb == C_INF) begin
                                if (sx || ca == C_ZERO) begin
                                    tf = 6'b000001;
                                    ares <= INDEFINITE;
                                end else
                                    ares <= {sy, 15'h7fff, 1'b1, 63'd0};
                            end else if (x_ltneg1) begin
                                tf = 6'b000001;               // LOG5: more negative than -1
                                ares <= INDEFINITE;
                            end else if (x_neg1) begin
                                // log2 0 again: the manual leaves it undefined.
                                if (ca == C_ZERO) begin
                                    tf = 6'b000001;
                                    ares <= INDEFINITE;
                                end else begin
                                    if (ca != C_INF) tf = 6'b000100;
                                    ares <= {!sy, 15'h7fff, 1'b1, 63'd0};
                                end
                            end else if (ca == C_ZERO)
                                ares <= {sy ^ sx, 79'd0};
                            else if (ca == C_INF)
                                ares <= {sy ^ sx, 15'h7fff, 1'b1, 63'd0};
                            else
                                go = 1'b1;
                        end
                        // A denormal operand raises DE on these answers too, unless
                        // an invalid or a zero divide is the only thing reported.
                        if (t_two && !sp_bad && !go)
                            pfl <= tf | (!tf[0] && !tf[2] && (ca == C_DEN || cb == C_DEN) ? 6'b000010 : 6'd0);
                        if (viapi) begin
                            // A multiple of pi/4, to 67 bits and a sticky, so
                            // A_RND rounds it under the control word and the
                            // inexact flag and C1 are its own.
                            wsign <= sy;
                            we <= pik == 2'd0 ? 18'sd16382 : pik == 2'd1 ? 18'sd16383 : 18'sd16384;
                            wm <= pik == 2'd2 ? 64'h96cb_e3f9_990e_91a7 : 64'hc90f_daa2_2168_c234;
                            wg <= 1'b1;
                            wrd <= pik != 2'd2;
                            ws <= 1'b1;
                            seq <= A_RND;
                        end else if (go) begin
                            // G9: a denormal operand raises DE and is normalised.
                            pfl <= pfl | (ca == C_DEN || (t_two && cb == C_DEN) ? 6'b000010 : 6'd0);
                            if (ca == C_DEN) begin
                                oa_e <= eff_e(pa) - 18'(lza);
                                sh_src <= pa[63:0];
                                sh_pow <= 6'(lza);
                                sh_isprod <= 1'b0;
                                sh_ret <= A_PNA;
                                seq <= S_SHIFT;
                            end else if (t_two && cb == C_DEN) begin
                                ob_e <= eff_e(pb) - 18'(lzb);
                                sh_src <= pb[63:0];
                                sh_pow <= 6'(lzb);
                                sh_isprod <= 1'b0;
                                sh_ret <= A_PNB;
                                seq <= S_SHIFT;
                            end else
                                seq <= A_OP;
                        end else
                            seq <= S_APPLY;
                    end else if (cur == X87_RNDINT || cur == X87_XTRACT
                                 || cur == X87_SQRT) begin
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
                        end else if (cur == X87_SQRT && pa[79] && ca != C_ZERO) begin
                            // The square root of a negative is invalid, and
                            // that is a pre-operation exception, so it is the
                            // only thing reported -- a negative denormal gives
                            // IE and not IE with DE. Negative zero is not a
                            // negative here: it comes back as itself below.
                            pfl <= 6'b000001;
                            ares <= INDEFINITE;
                            seq <= S_APPLY;
                        end else if (cur == X87_SQRT && (ca == C_ZERO || ca == C_INF)) begin
                            // Both zeros and positive infinity are their own
                            // square roots, with no exception.
                            ares <= pa;
                            seq <= S_APPLY;
                        end else if (ca == C_DEN) begin
                            oa_e <= eff_e(pa) - 18'(lza);
                            sq_p <= ~(eff_e0(pa) ^ lza[0]);
                            sh_src <= pa[63:0];
                            sh_pow <= 6'(lza);
                            sh_isprod <= 1'b0;
                            sh_ret <= A_PNA;
                            seq <= S_SHIFT;
                        end else
                            seq <= A_OP;
                    end else if (cur == X87_PREM) begin
                        // FPREM's and FPREM1's own operand classes, read off
                        // the host rather than the manual (model/fprem_host.c
                        // has the status words): an infinite dividend and a
                        // zero divisor are invalid, a zero dividend gives
                        // itself with NO exception at all, and a divisor of
                        // infinity gives the dividend back unchanged with C2
                        // clear and no exception -- a divisor of infinity is
                        // not an invalid case and does not reduce, which is
                        // the one a PRM reading gets wrong.
                        wsign <= pa[79];
                        pcc <= 4'd0;
                        pcc_wr <= 1'b1;
                        if (ca == C_EMPTY || cb == C_EMPTY) begin
                            pfl <= pfl | 6'b000001;
                            psf <= 1'b1;
                            ares <= INDEFINITE;
                            seq <= S_APPLY;
                        end else if (ca == C_UNSUP || cb == C_UNSUP) begin
                            pfl <= pfl | 6'b000001;
                            ares <= INDEFINITE;
                            seq <= S_APPLY;
                        end else if (ca == C_QNAN || ca == C_SNAN
                                     || cb == C_QNAN || cb == C_SNAN) begin
                            pfl <= sp_fl;
                            ares <= sp_v;
                            seq <= S_APPLY;
                        end else if (ca == C_INF || cb == C_ZERO) begin
                            pfl <= 6'b000001;
                            ares <= INDEFINITE;
                            seq <= S_APPLY;
                        end else if (ca == C_ZERO) begin
                            // Zero over anything finite is itself, with no
                            // exception at all. A DIVISOR of infinity needs no
                            // case of its own: its exponent makes the
                            // difference very negative, which is the path that
                            // hands the dividend back.
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
                            pfl <= sp_fl;
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
                        //
                        // Where the operation's own PRE-operation exception
                        // fired -- invalid, or slice 4's divide-by-zero --
                        // it is the only thing reported, so a denormal the
                        // memory conversion already flagged goes with
                        // everything else. The host settles it on FDIVR with
                        // a denormal m32 over zero, which reports ZE alone
                        // and not ZE and DE: six to eight cases per memory
                        // row. This is slice 3's rule 5 holding for a masked
                        // exception as well as an unmasked one.
                        pfl <= (sp_bad || sp_fl[0] || sp_fl[2]) ? sp_fl : pfl | sp_fl;
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
                // The operation's first datapath step. With the DE25
                // backend selected the four forms it answers divert here,
                // which catches every path into A_OP -- the direct one and
                // the two that come back from a denormal's pre-normalising
                // shift. The shift is wasted work on this path, because the
                // backend normalises its own operands out of `pa` and `pb`,
                // which A_PNA and A_PNB do not touch; it costs cycles after
                // the instruction has already retired and nothing waits on
                // it. Correctness does not depend on skipping it, so it is
                // left where it is rather than threaded through three more
                // branches.
                A_OP: if (bk_form) begin
                    seq <= B_ISSUE;
                end else if (X87_BACKEND == 1 && cur == X87_TRANS) begin
                    seq <= T_GO;
                end else begin
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
                    end else if (cur == X87_PREM) begin
                        // The step reduces n = (expDiff & 31) | 32 quotient
                        // bits where the difference is 64 or more, and the
                        // whole difference where it is less -- measured on the
                        // host, and exactly what Bochs's cpu/fpu/fprem.cc
                        // computes at the pinned revision, so lockstep is
                        // exact on partial steps rather than divergent.
                        //
                        // A NEGATIVE difference never reaches the datapath:
                        // the quotient is zero and the remainder is the
                        // dividend, and only FPREM1 at a difference of -1 can
                        // round the quotient up to one.
                        logic signed [17:0] d;
                        logic               one, rnd_up;
                        logic [6:0]         n;
                        d = oa_e - ob_e;
                        one = cur_op_raw[3:0] == 4'h5;      // D9 F5 is FPREM1
                        // A divisor of infinity gives the dividend back and
                        // never rounds. It cannot be left to the difference
                        // being "very negative": an infinity's exponent field
                        // is 7FFFh, the LARGEST, so a dividend at 7FFEh is
                        // exactly one below it and took FPREM1's round-up path
                        // at a difference of -1. QEMU's grid found it, 4 cases
                        // of 23,328, and only a dividend within one binade of
                        // the top separates the two readings.
                        rnd_up = one && d == -18'sd1 && oa_m > ob_m
                              && cb != C_INF;
                        n = d >= 18'sd64 ? (7'd32 | {2'b0, d[4:0]}) : 7'(d[6:0]);
                        wsign <= pa[79];
                        pr_one <= one;
                        pcc_wr <= 1'b1;
                        if (d < 18'sd0 || cb == C_INF) begin
                            // FPREM1 at -1: a/b is in (0.25, 1), so compare A
                            // with B. Above gives a quotient of one, a
                            // magnitude of 2B - A and the other sign; equal
                            // rounds to even and stays zero.
                            pr_part <= 1'b0;
                            pr_q <= {63'd0, rnd_up};
                            pr_one <= 1'b0;   // the rounding is already applied
                            pcc <= 4'd0;
                            pc1 <= rnd_up;    // C1 is the quotient's bit 0
                            if (rnd_up) begin
                                pr_n <= 7'd0;
                                dv_rem <= {32'd0, 64'({ob_m, 1'b0} - {1'b0, oa_m})};
                                wsign <= !pa[79];
                                we <= oa_e;
                                seq <= P_FIN;
                            end else begin
                                // The dividend comes back **through A_RND**,
                                // not as it stands. It is exact, so nothing
                                // rounds, but the exponent still goes through
                                // the normalise, tiny and bias-adjust rules:
                                // the host returns a pseudo-denormal as
                                // 0001 8000... and a genuine denormal with an
                                // UNMASKED underflow and the exponent moved by
                                // 24576, which is slice 3's rule 2 reaching a
                                // form that never rounds. An earlier version
                                // special-cased the pseudo-denormal and got
                                // the genuine one wrong.
                                wm <= oa_m;
                                wg <= 1'b0;
                                wrd <= 1'b0;
                                ws <= 1'b0;
                                we <= oa_e;
                                seq <= A_RND;
                            end
                        end else begin
                            // **The remainder needs no Goldschmidt and no
                            // shifter.** Its quotient is never used beyond
                            // its low three bits, which are C0, C3 and C1, so
                            // there is nothing to estimate: the exact
                            // remainder of A * 2^n by B comes from n
                            // doublings with a conditional subtract, one
                            // clock each, and the quotient's bits fall out of
                            // the same steps.
                            //
                            // That is strictly less hardware than reusing the
                            // divide's tail at a variable scale, which is
                            // what this slice did first: it needed a general
                            // shift by 65 + s - n on a path option A says has
                            // no barrel shifter, and that shifter was shared,
                            // so the divide's and the square root's own
                            // constant drops could not be muxed while it
                            // existed.
                            pr_part <= d >= 18'sd64;
                            pr_n <= n;
                            pr_q <= {63'd0, oa_m >= ob_m};
                            dv_rem <= {32'd0, oa_m >= ob_m ? oa_m - ob_m : oa_m};
                            we <= oa_e - 18'({11'd0, n});
                            seq <= P_SH;
                        end
                    end else if (cur == X87_SQRT) begin
                        // The exponent halving. With the true exponent
                        // E = oa_e - 16446, the result is
                        //   we = 16446 + (oa_e - 16509 - p) / 2
                        // and the parity p is chosen to make that difference
                        // even: p is zero where oa_e is odd and one where it
                        // is even. sq_p is already set, because the seed is a
                        // registered read of it.
                        wsign <= 1'b0;
                        we <= 18'sd16446
                            + ((oa_e - 18'sd16509 - 18'($signed({1'b0, sq_p}))) >>> 1);
                        gs_it <= dv_iters(wprec);
                        dv_lost <= 1'b0;
                        seq <= Q_SEED;
                    end else if (cur == X87_DIV) begin
                        // The divide. Both significands are normalised, so
                        // the quotient is in (0.5, 2); where it is below one
                        // the result takes one more bit and the exponent one
                        // less, which `dv_s` carries. The 66-bit target is
                        // T = floor(A * 2^(65+s) / B), whose bits 65:2 are the
                        // significand, bit 1 the guard and bit 0 the round,
                        // with the sticky from the exact remainder.
                        wsign <= sign_a ^ sign_b;
                        dv_s <= oa_m < ob_m;
                        we <= oa_e - ob_e + 18'sd16383 - 18'(oa_m < ob_m);
                        gs_it <= dv_iters(wprec);
                        dv_lost <= 1'b0;
                        // D_SEED and not the multiply directly: `rcp_q` is a
                        // registered read of ob_m, so the entry for THIS
                        // divisor is not out of the table until the cycle
                        // after ob_m settled. A_SETUP assigns ob_m
                        // non-blocking, so during A_OP rcp_q still holds the
                        // previous operation's entry. This is the same
                        // one-cycle hazard that produced slice 2's BUSY#
                        // latch hole and slice 3's FLDCW bug, and it cost 585
                        // of 684 cases here before it was found.
                        seq <= D_SEED;
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
                            // The step stands in for the true scale, so the
                            // bias adjust an unmasked overflow or underflow
                            // applies cannot stand for it: `wsat` makes
                            // those deliver the masked result instead, which
                            // is what a real x87 does for a scale this
                            // large -- FSCALE of 1 by 2^64 with OM clear
                            // gives an infinity with OE, PE and C1, and not
                            // a wrapped exponent.
                            we <= pb[79] ? oa_e - 18'sd36000 : oa_e + 18'sd36000;
                            wsat <= 1'b1;
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
                A_LOAD: if (X87_BACKEND == 0) begin
                    // X87_BACKEND is a parameter, so this condition is settled at
                    // elaboration: with the DE25 core selected the state is
                    // unreachable and its datapath is not built at all. That is
                    // the point -- you pay for one arithmetic implementation,
                    // not both -- and the fit's entity list is the evidence.
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
                A_SUM: if (X87_BACKEND == 0) begin
                    // X87_BACKEND is a parameter, so this condition is settled at
                    // elaboration: with the DE25 core selected the state is
                    // unreachable and its datapath is not built at all. That is
                    // the point -- you pay for one arithmetic implementation,
                    // not both -- and the fit's entity list is the evidence.
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
                A_NORM: if (X87_BACKEND == 0) begin
                    // X87_BACKEND is a parameter, so this condition is settled at
                    // elaboration: with the DE25 core selected the state is
                    // unreachable and its datapath is not built at all. That is
                    // the point -- you pay for one arithmetic implementation,
                    // not both -- and the fit's entity list is the evidence.
                                        // The guard and sticky A_SUM worked out stay as they are:
                    // a shift of more than one place only happens where they
                    // are both clear.
                    wm <= mul_p[63:0];
                    seq <= A_RND;
                end
                // The product. Both operands were normalised, so it is in
                // [1, 4): bit 127 set means it needs no shift and bit 126
                // one place, which is a wire either way.
                A_PROD: if (X87_BACKEND == 0) begin
                    // X87_BACKEND is a parameter, so this condition is settled at
                    // elaboration: with the DE25 core selected the state is
                    // unreachable and its datapath is not built at all. That is
                    // the point -- you pay for one arithmetic implementation,
                    // not both -- and the fit's entity list is the evidence.
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
                    // A remainder's C1 is the quotient's bit 0, set in P_PACK
                    // and P_ZERO, not the rounding's direction -- and FPREM
                    // and FPREM1 never round, so there is nothing to record.
                    if (cur != X87_PREM) pc1 <= rp[64];
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
                    // The guard and round bits the arithmetic left are below
                    // bit 0 once the shift has moved the significand down,
                    // so they join the sticky rather than being dropped.
                    wm <= mul_p[127:64];
                    wg <= mul_p[63];
                    wrd <= mul_p[62];
                    ws <= ws || wg || wrd || |mul_p[61:0];
                    seq <= A_RND;
                end
                // Rounding at the destination's precision, and then the
                // exponent rules. A real x87 settles those, and the host
                // bench the record sets out is where they come from.
                //
                // The value is rounded once at PC as though the exponent
                // range were unbounded, and that rounded exponent decides
                // whether it is tiny: tininess after rounding. A product
                // whose exact value is below the smallest normal but whose
                // rounding carries it back up is not tiny and reports
                // precision alone; one whose rounding does not is tiny and
                // reports underflow as well. Precision control moves that
                // line, because it moves where the rounding carries -- one
                // pair of operands is tiny at PC 64 and not at PC 24, and
                // another is the other way round, which is how the bench
                // tells this rule from the alternatives.
                //
                // Then:
                //   - masked overflow stores an infinity, or the precision's
                //     largest finite value where the rounding is toward
                //     zero, with OE and PE, and C1 says which way it went;
                //   - unmasked overflow or underflow stores the result with
                //     its exponent moved by 24576, which is the 387's bias
                //     adjust, and reports OE or UE whether or not the result
                //     is inexact. Where the adjust cannot reach -- FSCALE's
                //     scale saturates, so its exponent is out of reach by
                //     construction -- the masked result is stored instead,
                //     and an infinity in every rounding mode;
                //   - masked underflow denormalises the value as it was and
                //     rounds it there, so the result goes through one
                //     rounding and not two, and reports UE where that
                //     rounding lost bits: a tiny exact result raises nothing;
                //   - the precision exception never holds a result back.
                // Offer the operation. `req_ready` can only be low here
                // for a divide or a square root whose lane has not drained,
                // which one ESC in flight makes unreachable; the handshake is
                // held anyway, because that is the backend's contract.
                B_ISSUE: if (bk_req_ready) seq <= B_WAIT;
                // Take the result before rounding. `bk_raw` and `bk_raw_valid`
                // are both lane outputs registered on the same edge, so this
                // reads a settled value and not one assigned in this state --
                // the hazard that has appeared in every slice of this unit.
                B_WAIT: if (bk_raw_valid) begin
                    // An exact cancellation. The core leaves the exponent it
                    // was working at beside a significand of zero, and it is
                    // `fes_x87_round` that zeroes the exponent field when the
                    // significand rounds to nothing -- so on this path, which
                    // does not use that rounder, the zero has to be packed
                    // here. A_RND would otherwise store {sign, e, 0}, which
                    // is a pseudo-denormal rather than a zero: the unit bench
                    // caught it 240 times, on every add and subtract case
                    // whose operands cancel, and on no multiply, divide or
                    // square root.
                    //
                    // Guard, round and sticky are in the same 67 bits, so a
                    // zero there is an exact zero and nothing rounds. The
                    // sign is the core's, which already carries x87's rule
                    // that a cancellation is +0 except when rounding down.
                    if (bk_raw.significand == 67'd0) begin
                        pfl  <= pfl | bk_raw.flags;
                        pc1  <= 1'b0;
                        ares <= {bk_raw.sign, 79'd0};
                        seq  <= S_APPLY;
                    end else begin
                    wsign <= bk_raw.sign;
                    we    <= bk_raw.exponent;
                    wm    <= bk_raw.significand[66:3];
                    wg    <= bk_raw.significand[2];
                    wrd   <= bk_raw.significand[1];
                    ws    <= bk_raw.significand[0];
                    // The lanes raise IE, DE and ZE; A_RND adds PE, UE and
                    // OE. `special` can only be set for an operand class
                    // A_SETUP has already answered and never reaches here,
                    // so the value it would carry is not read. The assertion
                    // below is what makes that a checked claim rather than a
                    // comment: every bench case that reaches this state runs
                    // it, and A_SETUP's dispatch is what has to keep it true.
                    // synthesis translate_off
                    if (bk_raw.special)
                        $fatal(1, "fes_i386_x87: the DE25 backend returned a special-cased result; A_SETUP should have answered this operand class without the datapath");
                    // synthesis translate_on
                    pfl   <= pfl | bk_raw.flags;
                    seq   <= A_RND;
                    end
                end
                A_RND: begin
                    logic [66:0]        rp;
                    logic [63:0]        mag;
                    logic signed [17:0] e2;
                    logic [6:0]         k;
                    rp = round_pc(wm, wg, wrd, ws, wsign, rc, wprec);
                    mag = rp[66] ? 64'h8000_0000_0000_0000 : rp[63:0];
                    e2 = we + 18'(rp[66]);
                    // A remainder's C1 is the quotient's bit 0, set in P_PACK
                    // and P_ZERO, not the rounding's direction -- and FPREM
                    // and FPREM1 never round, so there is nothing to record.
                    if (cur != X87_PREM) pc1 <= rp[64];
                    if (wtiny) begin
                        // The denormalised value, rounded here for the first
                        // and only time: the exponent field is 0, or 1 where
                        // the rounding took it up to the smallest normal.
                        if (rp[65]) begin
                            pfl[5] <= 1'b1;
                            pfl[4] <= 1'b1;
                        end
                        ares <= {wsign, mag[63] ? 15'd1 : 15'd0, mag};
                        seq <= S_APPLY;
                    end else if (e2 > 18'sd32766) begin
                        logic inf, fits;
                        inf = !cw[3] || rc == 2'd0 || (rc == 2'd1 && wsign) || (rc == 2'd2 && !wsign);
                        fits = e2 - 18'sd24576 <= 18'sd32766 && !wsat;
                        if (rp[65]) pfl[5] <= 1'b1;
                        pfl[3] <= 1'b1;
                        if (cw[3] || !fits) begin
                            pfl[5] <= 1'b1;
                            pc1 <= inf;
                            ares <= inf ? {wsign, 15'h7fff, 1'b1, 63'd0}
                                        : {wsign, 15'h7ffe, max_sig(wprec)};
                        end else
                            ares <= {wsign, 15'(e2 - 18'sd24576), mag};
                        seq <= S_APPLY;
                    end else if (e2 < 18'sd1) begin
                        if (cw[4] || wsat || we < -18'sd24575) begin
                            // Denormalise the value as it was, not as the
                            // rounding above left it, so the result goes
                            // through one rounding only. The precision
                            // exception is the denormalised rounding's too,
                            // so nothing is recorded here.
                            k = (18'sd1 - we) > 18'sd66 ? 7'd66 : 7'((18'sd1 - we));
                            wtiny <= 1'b1;
                            we <= 18'sd1;
                            if (k < 7'd65) begin
                                sh_src <= wm;
                                sh_pow <= 6'(7'd64 - k);
                                sh_isprod <= 1'b0;
                                sh_ret <= A_TINY;
                                seq <= S_SHIFT;
                            end else begin
                                // Below the round bit: nothing is left but
                                // the sticky, which rounds to zero or to the
                                // smallest denormal.
                                wm <= 64'd0;
                                wg <= 1'b0;
                                wrd <= 1'b0;
                                ws <= 1'b1;
                            end
                        end else begin
                            if (rp[65]) pfl[5] <= 1'b1;
                            pfl[4] <= 1'b1;
                            ares <= {wsign, 15'(e2 + 18'sd24576), mag};
                            seq <= S_APPLY;
                        end
                    end else begin
                        if (rp[65]) pfl[5] <= 1'b1;
                        ares <= {wsign, 15'(e2), mag};
                        seq <= S_APPLY;
                    end
                end
                // ---- Slice 4: the divide. ----
                // Goldschmidt's two seeded products, then its iterations on
                // the shared multiplier, then the exact tail. Every
                // intermediate is the product's top 64 bits, which is why the
                // iterations alone cannot reach the extended format's 67 bits
                // and the tail is not optional.
                D_SEED: if (X87_BACKEND == 0) begin
                    // X87_BACKEND is a parameter, so this condition is settled at
                    // elaboration: with the DE25 core selected the state is
                    // unreachable and its datapath is not built at all. That is
                    // the point -- you pay for one arithmetic implementation,
                    // not both -- and the fit's entity list is the evidence.
                                        ua_mul <= oa_m;
                    ub_mul <= {rcp_q, 54'd0};
                    mul_start <= 1'b1;
                    seq <= D_N0;
                end
                D_N0: if (X87_BACKEND == 0 && mul_done) begin
                    // X87_BACKEND is a parameter, so this condition is settled at
                    // elaboration: with the DE25 core selected the state is
                    // unreachable and its datapath is not built at all. That is
                    // the point -- you pay for one arithmetic implementation,
                    // not both -- and the fit's entity list is the evidence.
                                        gs_n <= mul_p[127:64];
                    ua_mul <= ob_m;
                    ub_mul <= {rcp_q, 54'd0};
                    mul_start <= 1'b1;
                    seq <= D_D0;
                end
                D_D0: if (X87_BACKEND == 0 && mul_done) begin
                    // X87_BACKEND is a parameter, so this condition is settled at
                    // elaboration: with the DE25 core selected the state is
                    // unreachable and its datapath is not built at all. That is
                    // the point -- you pay for one arithmetic implementation,
                    // not both -- and the fit's entity list is the evidence.
                                        // f = 2 - D, in the scaling where one is 2^63: the two's
                    // complement of D, and all ones where D is zero.
                    gs_d <= mul_p[127:64];
                    ua_mul <= gs_n;
                    ub_mul <= mul_p[127:64] == 64'd0 ? {64{1'b1}} : (~mul_p[127:64] + 64'd1);
                    mul_start <= 1'b1;
                    seq <= D_ITN;
                end
                D_ITN: if (X87_BACKEND == 0 && mul_done) begin
                    // X87_BACKEND is a parameter, so this condition is settled at
                    // elaboration: with the DE25 core selected the state is
                    // unreachable and its datapath is not built at all. That is
                    // the point -- you pay for one arithmetic implementation,
                    // not both -- and the fit's entity list is the evidence.
                                        gs_n <= {mul_p[126:64], 1'b0};
                    ua_mul <= gs_d;
                    mul_start <= 1'b1;
                    seq <= D_ITD;
                end
                D_ITD: if (X87_BACKEND == 0 && mul_done) begin
                    // X87_BACKEND is a parameter, so this condition is settled at
                    // elaboration: with the DE25 core selected the state is
                    // unreachable and its datapath is not built at all. That is
                    // the point -- you pay for one arithmetic implementation,
                    // not both -- and the fit's entity list is the evidence.
                                        logic [63:0] dn;
                    dn = {mul_p[126:64], 1'b0};
                    gs_d <= dn;
                    if (gs_it > 2'd1) begin
                        gs_it <= gs_it - 2'd1;
                        ua_mul <= gs_n;
                        ub_mul <= dn == 64'd0 ? {64{1'b1}} : (~dn + 64'd1);
                        mul_start <= 1'b1;
                        seq <= D_ITN;
                    end else
                        seq <= D_CAND;
                end
                // The fast candidate is qhat minus the precision's empirical
                // correction budget. The following full-width certificate
                // makes correctness independent of that budget.
                D_CAND: if (X87_BACKEND == 0) begin
                    // Guarded on the parameter, like every other arm of the sequenced
                    // divide and square root: with the DE25 core selected this
                    // state is unreachable and its datapath is not built.
                    logic [66:0] qh, c0;
                    qh = drop67({3'd0, gs_n} << (dv_s ? 3 : 2), wprec);
                    // Clamped, which the divide alone never needed: a
                    // remainder step's quotient can be smaller than the error
                    // bound, and an unclamped subtract would wrap it to a huge
                    // candidate that the tail cannot bring back.
                    c0 = qh > {59'd0, dv_e(wprec)} ? qh - {59'd0, dv_e(wprec)} : 67'd0;
                    dv_c <= c0;
                    dv_exact_used <= 1'b0;
                    dv_k <= dv_steps(wprec);
                    ua_mul <= c0[63:0];
                    ub_mul <= ob_m;
                    mul_start <= 1'b1;
                    seq <= D_REM;
                end
                // Form the residual as low 96 bits plus an exact high word.
                // The tail changes only the low word; D_CHECK accepts it only
                // when the full residual is a valid quotient remainder.
                D_REM: if (X87_BACKEND == 0 && mul_done) begin
                    // Guarded on the parameter, like every other arm of the sequenced
                    // divide and square root: with the DE25 core selected this
                    // state is unreachable and its datapath is not built.
                    logic [66:0] chi;
                    logic [96:0] plo;
                    logic [95:0] nlo;
                    logic [37:0] phi, nhi;
                    logic [3:0]  k;
                    chi = mul3wide(dv_c[66:64], ob_m);
                    plo = {1'b0, mul_p[95:0]} + {1'b0, chi[31:0], 64'd0};
                    nlo = dvd96(oa_m, dv_s, wprec);
                    phi = {6'd0, mul_p[127:96]} + {3'd0, chi[66:32]} + 38'(plo[96]);
                    nhi = {2'd0, dvdhi(oa_m, dv_s, wprec)};
                    k = cur == X87_PREM ? 4'd8 : dv_steps(wprec);
                    dv_rem <= nlo - plo[95:0];
                    dv_hi_rem <= dv_c[66] ? 38'd1
                                           : nhi - phi - 38'(nlo < plo[95:0]);
                    // The tail's first subtrahend: B << (k - 1), where k is
                    // one of three constants, so this is a mux and not a
                    // shifter.
                    dv_sub <= {32'd0, ob_m} << (k - 4'd1);
                    dv_pow <= 96'd0;
                    seq <= D_TAIL;
                end
                // The restoring compare-subtract steps, one a clock, a fixed
                // count per precision: this is not an early-exit search, so a
                // divide's latency does not depend on its operands.
                // The restoring steps, with the subtrahend **held and shifted
                // one place a step** rather than formed by a variable shift.
                // Option A has no barrel shifter, and forming
                // `B << i` or `(C << (i+1)) + 2^(2i)` from a counter put two
                // 96-bit variable shifters on this path. Both recurrences are
                // exact:
                //
                //   divide       S(i) = B * 2^i,  so S(i-1) = S(i) / 2.
                //   square root  S(i) = C(i) * 2^(i+1) + 2^(2i), and with
                //                P(i) = 2^(2i),
                //                  taken      S(i-1) = S/2 + P/2 + P/4
                //                  not taken  S(i-1) = S/2 - P/2 + P/4
                //                because taking the step moves C by 2^i.
                //
                // Every shift there is by one or two places, and the step
                // count is still fixed by precision control, so the latency
                // does not move.
                D_TAIL: if (X87_BACKEND == 0) begin
                    // X87_BACKEND is a parameter, so this condition is settled at
                    // elaboration: with the DE25 core selected the state is
                    // unreachable and its datapath is not built at all. That is
                    // the point -- you pay for one arithmetic implementation,
                    // not both -- and the fit's entity list is the evidence.
                                        logic [95:0] trial, nsub;
                    logic [3:0]  i;
                    i = dv_k - 4'd1;
                    trial = dv_rem - dv_sub;
                    nsub = (dv_sub >> 1) + (dv_pow >> 2);
                    if (!trial[95]) begin
                        dv_rem <= trial;
                        if (dv_rem < dv_sub) dv_hi_rem <= dv_hi_rem - 38'd1;
                        dv_c <= dv_c + (67'd1 << i);
                        if (cur == X87_SQRT) nsub = nsub + (dv_pow >> 1);
                    end else if (cur == X87_SQRT)
                        nsub = nsub - (dv_pow >> 1);
                    dv_sub <= cur == X87_SQRT ? nsub : (dv_sub >> 1);
                    dv_pow <= dv_pow >> 2;
`ifndef SYNTHESIS
                    // The recurrence against the closed form it replaces, so
                    // a drift is caught by a check rather than by a result.
                    if (cur == X87_SQRT) begin
                        if (dv_sub != ((96'(dv_c) << (i + 4'd1)) + (96'd1 << (i + i))))
                            $error("x87 sqrt tail subtrahend %h != closed form %h at i=%0d",
                                   dv_sub, (96'(dv_c) << (i + 4'd1)) + (96'd1 << (i + i)), i);
                    end else if (dv_sub != ({32'd0, ob_m} << i))
                        $error("x87 divide tail subtrahend %h != B<<%0d", dv_sub, i);
`endif
                    if (dv_k > 4'd1) dv_k <= dv_k - 4'd1;
                    else seq <= cur == X87_SQRT ? Q_CHECK
                              : cur == X87_PREM ? P_FIN : D_CHECK;
                end
                // The correction constants are an optimization only. Accept
                // the fast result only when its exact quotient residual is
                // non-negative and below B; otherwise recompute every digit.
                D_CHECK: if (X87_BACKEND == 0) begin
                    // Guarded on the parameter, like every other arm of the sequenced
                    // divide and square root: with the DE25 core selected this
                    // state is unreachable and its datapath is not built.
                    if (dv_c[66] || dv_hi_rem != 38'd0 || dv_rem[95]
                        || dv_rem >= {32'd0, ob_m}) begin
                        logic [64:0] r0;
                        r0 = dv_s ? ({oa_m, 1'b0} - {1'b0, ob_m})
                                  : ({1'b0, oa_m} - {1'b0, ob_m});
                        dv_c <= 67'd1;
                        dv_rem <= {31'd0, r0};
                        dv_hi_rem <= 38'd0;
                        dv_exact_left <= dv_digits(wprec) - 7'd1;
                        dv_exact_used <= 1'b1;
                        dv_lost <= 1'b0;
                        seq <= D_EXACT;
                    end else
                        seq <= D_FIN;
                end
                // The exact fallback emits the normalized quotient one bit
                // at a time. Its final remainder is the true sticky source.
                // Square root uses a radix-4 restoring recurrence below and
                // streams the 132-bit radicand through dv_hi_rem/dv_sub.
                D_EXACT: if (X87_BACKEND == 0) begin
                    // Guarded on the parameter, like every other arm of the sequenced
                    // divide and square root: with the DE25 core selected this
                    // state is unreachable and its datapath is not built.
                    if (cur == X87_SQRT) begin
                        logic [1:0]  pair;
                        logic [67:0] trial, subroot, rnext;
                        logic        exact_take;
                        logic [66:0] cnext;
                        pair = dv_hi_rem[35:34];
                        trial = {dv_rem[65:0], pair};
                        subroot = {dv_c[65:0], 2'b01};
                        exact_take = trial >= subroot;
                        rnext = exact_take ? trial - subroot : trial;
                        cnext = {dv_c[65:0], exact_take};
                        dv_rem <= {28'd0, rnext};
                        dv_hi_rem <= {2'd0, dv_hi_rem[33:0], dv_sub[95:94]};
                        dv_sub <= {dv_sub[93:0], 2'b00};
                        if (dv_exact_left > 7'd1) begin
                            dv_c <= cnext;
                            dv_exact_left <= dv_exact_left - 7'd1;
                        end else begin
                            dv_c <= drop67(cnext, wprec);
                            dv_lost <= low_dropped_bits(cnext, wprec);
                            seq <= Q_FIN;
                        end
                    end else begin
                        logic [65:0] trial, rnext;
                        logic        exact_take;
                        logic [66:0] cnext;
                        trial = {dv_rem[64:0], 1'b0};
                        exact_take = trial >= {2'b0, ob_m};
                        rnext = exact_take ? trial - {2'b0, ob_m} : trial;
                        cnext = {dv_c[65:0], exact_take};
                        dv_rem <= {30'd0, rnext};
                        if (dv_exact_left > 7'd1) begin
                            dv_c <= cnext;
                            dv_exact_left <= dv_exact_left - 7'd1;
                        end else begin
                            dv_c <= cnext;
                            dv_hi_rem <= 38'd0;
                            seq <= D_FIN;
                        end
                    end
                end
                // The exact truncation, its guard and round bits, and the
                // sticky the remainder gives for nothing. A_RND then applies
                // precision control, the overflow and underflow rules and the
                // bias adjust exactly as it does for slice 3's arithmetic.
                // The exact truncation at the precision's own position, put
                // back where round_pc() reads it: at PC 24 the candidate's
                // bits land at m[63:39] with zeros below and the guard and
                // round bits clear, because the sticky already covers
                // everything the shorter target did not compute.
                D_FIN: if (X87_BACKEND == 0) begin
                    // X87_BACKEND is a parameter, so this condition is settled at
                    // elaboration: with the DE25 core selected the state is
                    // unreachable and its datapath is not built at all. That is
                    // the point -- you pay for one arithmetic implementation,
                    // not both -- and the fit's entity list is the evidence.
                                        logic [5:0] dr;
                    dr = dv_drop(wprec);
`ifndef SYNTHESIS
                    if (dv_hi_rem != 38'd0 || dv_rem[95] || dv_rem >= {32'd0, ob_m})
                        $error("x87 divide result left residual=%h:%h outside [0, B=%h)",
                               dv_hi_rem, dv_rem, ob_m);
`endif
                    wm <= place64(dv_c, wprec);
                    wg <= dr == 6'd0 ? dv_c[1] : 1'b0;
                    wrd <= dr == 6'd0 ? dv_c[0] : 1'b0;
                    ws <= |dv_rem;
                    seq <= A_RND;
                end
                // ---- Slice 4: the square root. ----
                // Values here are (significand, exponent) pairs meaning
                // sig * 2^(exp - 63) with bit 63 of the significand set, which
                // is what lets one mux renormalise every product. The seed is
                // v/1024 with v in [513, 1022], so it is left-aligned to bit
                // 63 with an exponent of -1.
                Q_SEED: if (X87_BACKEND == 0) begin
                    // Guarded on the parameter for the reason A_SUM's arm gives: with
                    // the DE25 core selected this state is unreachable and its
                    // datapath is not built.
                                        qy_s <= {rsq_q, 54'd0};
                    qy_e <= -6'sd1;
                    ua_mul <= oa_m;
                    ub_mul <= {rsq_q, 54'd0};
                    mul_start <= 1'b1;
                    seq <= Q_AY;
                end
                Q_AY: if (X87_BACKEND == 0 && mul_done) begin
                    // Guarded on the parameter for the reason A_SUM's arm gives: with
                    // the DE25 core selected this state is unreachable and its
                    // datapath is not built.
                                        // z = a*y, then start z*y.
                    qz_s <= norm_hi(mul_p);
                    qz_e <= norm_e(6'sd1 + {5'd0, sq_p} + qy_e, mul_p[127]);
                    ua_mul <= norm_hi(mul_p);
                    ub_mul <= qy_s;
                    mul_start <= 1'b1;
                    seq <= Q_AY2;
                end
                Q_AY2: if (X87_BACKEND == 0 && mul_done) begin
                    // Guarded on the parameter for the reason A_SUM's arm gives: with
                    // the DE25 core selected this state is unreachable and its
                    // datapath is not built.
                                        // t = 3 - a*y^2, aligned by the exponent difference and
                    // renormalised. 3 is 0xC000...  with an exponent of 1, and
                    // a*y^2 is close to one, so the difference is close to two
                    // and needs at most a couple of bits of normalisation.
                    logic [63:0]       z2;
                    logic signed [5:0] z2e;
                    logic [63:0]       d;
                    logic              lz1;
                    z2 = norm_hi(mul_p);
                    z2e = norm_e(6'sd1 + qz_e + qy_e, mul_p[127]);
                    // **Two-way muxes, not shifters.** a*y^2 is within 2^-9 of
                    // one after the seed, so its exponent is 0 or -1 and the
                    // alignment against three is one place or two; the
                    // difference is then within 2^-9 of two, so it needs no
                    // normalisation or exactly one place. Writing these as
                    // `z2 >> sh` and `d << clz64(d)` built a 64-bit shifter
                    // and a leading-zero count for a one-bit decision, on a
                    // path option A says has no barrel shifter.
                    //
                    // The ranges are asserted rather than assumed, because
                    // they come from the seed's accuracy and a seed-table edit
                    // could break them silently.
                    d = 64'hc000_0000_0000_0000 - (z2e == 6'sd0 ? (z2 >> 1) : (z2 >> 2));
                    lz1 = !d[63];
                    qt_s <= lz1 ? {d[62:0], 1'b0} : d;
                    qt_e <= lz1 ? 6'sd0 : 6'sd1;
`ifndef SYNTHESIS
                    if (z2e != 6'sd0 && z2e != -6'sd1)
                        $error("x87 sqrt: a*y^2 exponent %0d is outside {0, -1}", z2e);
                    if (clz64(d) > 7'd1)
                        $error("x87 sqrt: 3 - a*y^2 needs %0d places of normalisation, not 0 or 1",
                               clz64(d));
`endif
                    ua_mul <= qy_s;
                    ub_mul <= lz1 ? {d[62:0], 1'b0} : d;
                    mul_start <= 1'b1;
                    seq <= Q_Y;
                end
                Q_Y: if (X87_BACKEND == 0 && mul_done) begin
                    // Guarded on the parameter for the reason A_SUM's arm gives: with
                    // the DE25 core selected this state is unreachable and its
                    // datapath is not built.
                                        // y <- y*t/2: the halving is one off the exponent.
                    qy_s <= norm_hi(mul_p);
                    qy_e <= norm_e(qy_e + qt_e, mul_p[127]);
                    if (gs_it > 2'd1) begin
                        gs_it <= gs_it - 2'd1;
                        ua_mul <= oa_m;
                        ub_mul <= norm_hi(mul_p);
                        mul_start <= 1'b1;
                        seq <= Q_AY;
                    end else begin
                        ua_mul <= oa_m;
                        ub_mul <= norm_hi(mul_p);
                        mul_start <= 1'b1;
                        seq <= Q_RES;
                    end
                end
                Q_RES: if (X87_BACKEND == 0 && mul_done) begin
                    // Guarded on the parameter for the reason A_SUM's arm gives: with
                    // the DE25 core selected this state is unreachable and its
                    // datapath is not built.
                                        // q = a*y, the square root's estimate. Its exponent is 0
                    // where q is in [1, 2) and **-1 where q is just below
                    // one**, which is what happens when the true root is an
                    // exact power of two: the iteration converges to it from
                    // below, so y comes out as 0xFFFF...FFFF with an exponent
                    // of -1 rather than 2^63 with zero. Hardcoding the shift
                    // at 2 on the assumption that q is always in [1, 2) put
                    // the candidate a whole binade high, and it failed on
                    // exactly the patterns whose root is exact -- 1.0 and the
                    // pseudo-denormal -- while every inexact root passed.
                    logic [66:0]       qh, c0;
                    logic signed [5:0] qe;
                    // The estimate is in (0.5, 2), so its exponent is 0 or
                    // -1 and the placement is two places or one -- a mux. The
                    // -1 case is the one that happens when the true root is an
                    // exact power of two, which the iteration approaches from
                    // below.
                    qe = norm_e(6'sd1 + {5'd0, sq_p} + qy_e, mul_p[127]);
                    qh = drop67(qe == 6'sd0 ? ({3'd0, norm_hi(mul_p)} << 2)
                                            : ({3'd0, norm_hi(mul_p)} << 1), wprec);
`ifndef SYNTHESIS
                    if (qe != 6'sd0 && qe != -6'sd1)
                        $error("x87 sqrt: the estimate's exponent %0d is outside {0, -1}", qe);
`endif
                    c0 = qh > {59'd0, sq_e(wprec)} ? qh - {59'd0, sq_e(wprec)} : 67'd0;
                    dv_c <= c0;
                    dv_exact_used <= 1'b0;
                    dv_k <= sq_steps(wprec);
                    ua_mul <= c0[63:0];
                    ub_mul <= c0[63:0];
                    mul_start <= 1'b1;
                    seq <= Q_REM;
                end
                // Form the fast path's residual in low and high words. The
                // high word makes the following check exact even when a
                // candidate is outside the correction tail's range.
                Q_REM: if (X87_BACKEND == 0 && mul_done) begin
                    // Guarded on the parameter, like every other arm of the sequenced
                    // divide and square root: with the DE25 core selected this
                    // state is unreachable and its datapath is not built.
                    logic signed [8:0] qsh;
                    logic [66:0]       cross_prod;
                    logic [96:0]       plo;
                    logic [37:0]       phi, nhi;
                    logic [5:0]        hsq;
                    logic [95:0]       rad;
                    qsh = 9'sd67 + 9'($signed({1'b0, sq_p}))
                        - 9'sd2 * 9'($signed({3'b0, dv_drop(wprec)}));
                    rad = qsh >= 9'sd0 ? (96'(oa_m) << qsh[7:0]) : (96'(oa_m) >> (-qsh));
                    cross_prod = mul3wide(dv_c[66:64], dv_c[63:0]);
                    plo = {1'b0, mul_p[95:0]} + {1'b0, cross_prod[30:0], 65'd0};
                    hsq = 6'(dv_c[66:64]) * 6'(dv_c[66:64]);
                    phi = {6'd0, mul_p[127:96]} + {2'd0, cross_prod[66:31]}
                        + {hsq, 32'd0} + 38'(plo[96]);
                    nhi = {2'd0, sqradhi(oa_m, sq_p, wprec)};
                    dv_rem <= rad - plo[95:0];
                    dv_hi_rem <= dv_c[66] ? 38'd1
                                           : nhi - phi - 38'(rad < plo[95:0]);
                    dv_lost <= qsh >= 9'sd0 ? 1'b0
                             : |(oa_m & ((64'd1 << (-qsh)) - 64'd1));
                    // S(k-1) = C0 * 2^k + 2^(2k-2) and P(k-1) = 2^(2k-2),
                    // both at one of three constant positions.
                    dv_sub <= (96'(dv_c) << sq_steps(wprec))
                            + (96'd1 << (2 * sq_steps(wprec) - 4'd2));
                    dv_pow <= 96'd1 << (2 * sq_steps(wprec) - 4'd2);
                    seq <= D_TAIL;
                end
                // The exact root certificate is 0 <= N-C^2 <= 2C. If the
                // short tail did not establish that range, restart with a
                // radix-4 restoring square root of A*2^(67+parity).
                Q_CHECK: if (X87_BACKEND == 0) begin
                    // Guarded on the parameter, like every other arm of the sequenced
                    // divide and square root: with the DE25 core selected this
                    // state is unreachable and its datapath is not built.
                    if (dv_c[66] || dv_hi_rem != 38'd0 || dv_rem[95]
                        || dv_rem > {28'd0, dv_c, 1'b0}) begin
                        dv_c <= 67'd0;
                        dv_rem <= 96'd0;
                        dv_hi_rem <= sq_p ? {2'd0, oa_m[63:28]} : {3'd0, oa_m[63:29]};
                        dv_sub <= sq_p ? {oa_m[27:0], 68'd0} : {oa_m[28:0], 67'd0};
                        dv_exact_left <= 7'd66;
                        dv_exact_used <= 1'b1;
                        dv_lost <= 1'b0;
                        seq <= D_EXACT;
                    end else
                        seq <= Q_FIN;
                end
                Q_FIN: if (X87_BACKEND == 0) begin
                    // Guarded on the parameter, like every other arm of the sequenced
                    // divide and square root: with the DE25 core selected this
                    // state is unreachable and its datapath is not built.
                    logic [5:0] dr;
                    dr = dv_drop(wprec);
`ifndef SYNTHESIS
                    // The fast path's certificate is a production check. The
                    // fallback carries the full-root remainder instead.
                    if (!dv_exact_used && (dv_hi_rem != 38'd0 || dv_rem[95]
                        || dv_rem > {28'd0, dv_c, 1'b0}))
                        $error("x87 sqrt result left residual=%h:%h outside [0, 2C+1] with C=%h",
                               dv_hi_rem, dv_rem, dv_c);
`endif
                    wm <= place64(dv_c, wprec);
                    wg <= dr == 6'd0 ? dv_c[1] : 1'b0;
                    wrd <= dr == 6'd0 ? dv_c[0] : 1'b0;
                    ws <= |dv_rem || dv_lost;
                    seq <= A_RND;
                end
                // The remainder's result: exact, so it does not go through
                // A_RND at all -- FPREM and FPREM1 ignore both rounding and
                // precision control. FPREM1's quotient rounds to nearest,
                // which can flip the sign and replace the magnitude.
                // One doubling and a conditional subtract a clock. Both
                // operands are normalised, so the dividend is below twice the
                // divisor and the initial reduction in A_OP is enough to put
                // the remainder below it; from there 2*rem is below twice the
                // divisor at every step, so one compare-subtract keeps it in
                // range. The quotient shifts in beside it.
                P_SH: begin
                    logic [95:0] r2, t;
                    r2 = {dv_rem[94:0], 1'b0};
                    t = r2 - {32'd0, ob_m};
                    if (pr_n == 7'd0) seq <= P_FIN;
                    else begin
                        dv_rem <= t[95] ? r2 : t;
                        pr_q <= {pr_q[62:0], ~t[95]};
                        pr_n <= pr_n - 7'd1;
                    end
`ifndef SYNTHESIS
                    if (dv_rem >= {32'd0, ob_m})
                        $error("x87 remainder loop: rem=%h not below B=%h", dv_rem, ob_m);
`endif
                end
                P_FIN: begin
                    logic [63:0] r0;
                    logic [64:0] twice, bb;
                    logic        up, flip;
`ifndef SYNTHESIS
                    if (dv_rem[95] || dv_rem >= {32'd0, ob_m})
                        $error("x87 remainder step left rem=%h outside [0, B=%h)", dv_rem, ob_m);
`endif
                    r0 = dv_rem[63:0];
                    // Compare 2*rem with B and not rem with B/2: `ob_m >> 1`
                    // drops an odd divisor's low bit, so a remainder strictly
                    // below half reads as a tie and FPREM1 rounds the
                    // quotient up where the host does not. Doubling the
                    // remainder instead is exact for every divisor.
                    //
                    // The counterexample is built rather than argued, and the
                    // bench carries it as `prem1-chain-odd-lo`: FPREM1 of
                    // 3FFF C000000000000001 by 3FFF 8000000000000001 gives
                    // BFFE 8000000000000002 with C3 set under `rem vs B >> 1`
                    // and 3FFE 8000000000000000 with C1 set -- the host's
                    // answer -- under this one. One case of 738, and only an
                    // odd divisor near the tie separates them.
                    twice = {r0, 1'b0};
                    bb = {1'b0, ob_m};
                    // Round the quotient to nearest only on a COMPLETING
                    // FPREM1 step: a partial step's remainder is the running
                    // dividend in [0, B), and only a completing FPREM1's is
                    // the IEEE [-B/2, B/2]. Applying the IEEE range everywhere
                    // is the plausible wrong fix -- an earlier version of this
                    // slice's own model did it and reported 7,860 violations
                    // against correct behaviour.
                    // Every completing FPREM1 step rounds, including one at an
                    // exponent difference of zero: gating this on a non-zero
                    // reduction excluded 3 mod 2, whose quotient rounds from
                    // one to two. The difference-below-zero path clears
                    // pr_one, which is how it says its rounding is done.
                    up = pr_one && !pr_part
                      && (twice > bb || (twice == bb && pr_q[0]));
                    flip = up;
                    if (up) pr_q <= pr_q + 64'd1;
                    if (up) begin
                        dv_rem <= {32'd0, ob_m - r0};
                        wsign <= wsign ^ 1'b1;
                    end
                    pcc <= pr_part ? 4'b0100               // C2 alone: loop again
                         : {1'b0, 1'b0, 1'b0, 1'b0};
                    // The magnitude normalises through the shifter, as every
                    // other value in this unit does: option A has no barrel
                    // shifter, so a left shift is a multiply by 2^lz.
                    if ((up ? ob_m - r0 : r0) == 64'd0) begin
                        // An exact zero keeps the dividend's sign and is
                        // packed as a true zero, not normalised.
                        ares <= {flip ? !pa[79] : pa[79], 79'd0};
                        seq <= P_ZERO;
                    end else begin
                        sh_src <= up ? ob_m - r0 : r0;
                        sh_pow <= 6'(clz64(up ? ob_m - r0 : r0));
                        sh_isprod <= 1'b0;
                        sh_ret <= P_PACK;
                        we <= we - 18'(clz64(up ? ob_m - r0 : r0));
                        seq <= S_SHIFT;
                    end
                end
                // The condition codes a completing step reports: C2 clear and
                // the quotient's low three bits as C0, C3 and C1 -- which is
                // q = C0*4 + C3*2 + C1, measured on the host over q = 0 to 9
                // for both instructions. The PRM's q2/q1/q0 map to C0/C3/C1 in
                // that order, which is easy to get backwards from the manual.
                P_ZERO: begin
                    // An unmasked PRE-operation exception reports nothing
                    // else -- slice 3's rule 5 -- and that includes C2 and
                    // the quotient bits. A denormal divisor with DM clear
                    // raises DE before the reduction runs, so the host leaves
                    // every condition code clear where the exponent
                    // difference alone would have set C2. The bench's mask
                    // matrix found it: six cases per unmasked-DE row.
                    pcc <= pre_um(pfl, cw) ? 4'd0
                         : pr_part ? 4'b0100
                         : {pr_q[1], 1'b0, pr_q[0], pr_q[2]};
                    // C1 comes from pc1 and not from this vector -- S_APPLY
                    // writes cc as {nc[3], nc[2], c1, nc[0]} -- so a
                    // remainder's C1, which is the quotient's bit 0 rather
                    // than a rounding direction, has to go through pc1.
                    pc1 <= !pre_um(pfl, cw) && !pr_part && pr_q[0];
                    pcc_wr <= 1'b1;
                    seq <= S_APPLY;
                end
                P_PACK: begin
                    wm <= mul_p[63:0];
                    wg <= 1'b0;
                    wrd <= 1'b0;
                    ws <= 1'b0;
                    pcc <= pre_um(pfl, cw) ? 4'd0
                         : pr_part ? 4'b0100
                         : {pr_q[1], 1'b0, pr_q[0], pr_q[2]};
                    pc1 <= !pre_um(pfl, cw) && !pr_part && pr_q[0];
                    pcc_wr <= 1'b1;
                    seq <= A_RND;
                end
                // ---- The transcendental engine. ----
                // `tr_start` is this state: the engine takes the operands the
                // pre-normalising shifts have settled in oa_m, oa_e, ob_m and
                // ob_e, which were assigned at least a clock ago.
                T_GO: if (X87_BACKEND == 1) seq <= T_RUN;
                // Its last operation's result, unrounded, into the registers
                // A_RND reads, so the rounding under the control word,
                // overflow, underflow and C1 are the arithmetic's. `tr_done`
                // and the fin_* values are the engine's registers, written
                // on the same edge. The instruction is inexact by definition
                // (rules.md G6), so PE is set whatever the last operation was.
                T_RUN: if (X87_BACKEND == 1 && tr_done) begin
                    tr_eng <= 1'b1;
                    pfl[5] <= 1'b1;
                    wtiny <= 1'b0;
                    wsat <= 1'b0;
                    // FPTAN pushes 1.0; FSINCOS pushes the engine's cosine.
                    if (t_ins == 3'd2) ares2 <= tr_second;
                    if (tr_fin_zero) begin
                        ares <= {tr_fin_sign, 79'd0};
                        seq <= S_APPLY;
                    end else begin
                        wsign <= tr_fin_sign;
                        we <= tr_fin_exp;
                        wm <= tr_fin_sig[66:3];
                        wg <= tr_fin_sig[2];
                        wrd <= tr_fin_sig[1];
                        ws <= tr_fin_sig[0];
                        seq <= A_RND;
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
                    if (bcd) begin
                        // FBSTP: the rounded magnitude has to fit 18 digits.
                        // One that does not is invalid and stores the packed
                        // decimal indefinite, with nothing rounded up; one
                        // that does goes to T_BST with its flags, C1 and
                        // the no-access decision already final, since the
                        // CPU takes the value the moment the sequencer
                        // reaches S_RDY.
                        if (rnd_q > BCD_MAX) begin
                            f[0] = 1'b1;
                            c1v = 1'b0;
                            sres <= BCD_INDEFINITE;
                        end else if (rnd_inex_q) f[5] = 1'b1;
                    end else if (c_int) begin
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
                    // FBSTP with the precision exception unmasked stores its
                    // digits and pops: precision is detected after the
                    // result exists, and the host x87 was measured to store,
                    // pop and raise ES. Only invalid holds the access back.
                    // (The integer and real stores keep the gate they were
                    // sealed with, which S_APPLY's comment names.)
                    pnx <= unmasked(bcd ? f & 6'b000111 : f, cw);
                    seq <= bcd && !f[0] ? T_BST_IN : S_RDY;
                end
                // ---- Packed decimal (#1958). ----
                // FBLD: 18 steps of acc * 10 + digit, the most significant
                // digit first, with the multiply as two shifts and an add.
                // A digit of A to F is not checked for: the 387 "does not
                // check for invalid digits" and calls the result undefined
                // (387 PRM section 4.3.8), and this arithmetic is what the
                // host x87 and the oracle were both measured to do with one.
                T_BLD_IN: if (X87_BACKEND == 1) begin
                    bl_acc <= 64'd0;
                    bl_dig <= mval[71:0];
                    bl_n <= 5'd18;
                    seq <= T_BLD;
                end
                // The step that starts the normalising shift reads bl_acc
                // through ld_shift and ld_mag, so it has to be a clock after
                // the last digit went in: the count runs out first and the
                // shift starts from the settled register, never in the
                // clock that assigns it. bl_acc then stays as it is until
                // S_APPLY, which reads the same conversion again.
                T_BLD: if (X87_BACKEND == 1) begin
                    if (bl_n != 5'd0) begin
                        bl_acc <= {bl_acc[60:0], 3'd0} + {bl_acc[62:0], 1'b0} + {60'd0, bl_dig[71:68]};
                        bl_dig <= {bl_dig[67:0], 4'd0};
                        bl_n <= bl_n - 5'd1;
                    end else if (ld_shift) begin
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
                end
                // FBSTP: the rounded integer to 18 digits by double-dabble,
                // one bit a clock from the top: each digit of five or more
                // gains three, then the digits shift left with the next bit
                // coming in at the bottom. Sixty bits hold 10^18 - 1.
                T_BST_IN: if (X87_BACKEND == 1) begin
                    bs_bin <= rnd_q[59:0];
                    bs_bcd <= 72'd0;
                    bs_n <= 6'd60;
                    seq <= T_BST;
                end
                // The packed value: the sign in bit 79, zeros in bits 78 to
                // 72 (387 PRM Figure 2-10, note 3: "zeros when storing") and
                // the digits. It is assigned from bs_bcd a clock after the
                // last bit went in, for the same reason as above.
                T_BST: if (X87_BACKEND == 1) begin
                    if (bs_n != 6'd0) begin
                        bs_bcd <= {bs_adj[70:0], bs_bin[59]};
                        bs_bin <= {bs_bin[58:0], 1'b0};
                        bs_n <= bs_n - 6'd1;
                    end else begin
                        sres <= {c_sign, 7'd0, bs_bcd};
                        seq <= S_RDY;
                    end
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
                        X87_ARITH, X87_RNDINT, X87_SCALE, X87_DIV, X87_SQRT,
                        X87_PREM: begin
                            // The result goes to ST(0), or to ST(i) for the
                            // DC and DE forms, and DE pops ST(0) after it.
                            // X87_DIV belongs here and not in a case of its
                            // own: a divide's destination, pop and write are
                            // the add's. Leaving it out of this list and the
                            // register write below left the flags landing and
                            // the result not written, which read as "the
                            // divide returned its dividend".
                            v = ares;
                            wr = 1'b1;
                            pop = a_pop1;
                        end
                        X87_TRANS: begin
                            // The trigonometric forms write C2: clear where
                            // the operand was reduced, set where it was out
                            // of range, which leaves the stack as it was.
                            if (t_trig) begin
                                ccw = 1'b1;
                                nc = {cc[3], tr_c2, 1'b0, cc[0]};
                            end
                            // A result the engine produced is inexact and, if
                            // it is tiny, an underflow (rules.md G6, G12).
                            if (tr_eng && ares[78:64] == 15'd0 && ares[63:0] != 64'd0) f[4] = 1'b1;
                            if (tr_nowr) c1 = 1'b0;
                            else if (t_push) begin
                                // FSINCOS and FPTAN: the result replaces ST(0)
                                // and the cosine or the 1.0 is pushed, as
                                // FXTRACT's two results are.
                                push = 1'b1;
                                v = ares2;
                                wr = 1'b1;
                                if (!empty[push_i] && !tr_under) begin
                                    f = 6'b000001;
                                    stk = 1'b1;
                                    c1 = 1'b1;
                                    ovf = 1'b1;
                                    v = INDEFINITE;
                                end
                            end else begin
                                v = ares;
                                wr = 1'b1;
                                pop = a_pop1;
                            end
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
                    // FBSTP follows the same measurement: with PE unmasked it
                    // has stored and it pops.
                    um = a_form || bcd ? unmasked(f & 6'b000111, cw) : unmasked(f, cw);
                    // ... and because such an exception means the operation
                    // never produced a result, the exceptions the result
                    // would have raised are not reported with it: a real x87
                    // adding the largest denormal to just over one with DM
                    // clear reports the denormal alone, and not the precision
                    // exception the sum would have raised.
                    if (a_form && um) begin
                        f[5:3] = 3'b000;
                        // ... and C1, which says which way the rounding
                        // went, has nothing to say about a result that was
                        // never produced. A stack fault keeps its C1,
                        // because there it says overflow or underflow and
                        // the handler reads it.
                        if (!stk) c1 = 1'b0;
                    end
                    // ERROR# rises with this apply, and the board latches
                    // BUSY# on that edge. Setting it here rather than from
                    // the edge detector below closes the cycle between this
                    // apply, which makes the unit idle, and the detector's
                    // own edge, where an ESC would see neither busy nor
                    // latched (387 PRM Appendix F.2, the AT's latch).
                    // Any unmasked exception raises ERROR# and latches
                    // BUSY#, including the ones that do not hold the result
                    // back: the latch is what the next ESC waits on, and the
                    // flags and the latch have to go up together. `um` above
                    // gates the write and this gates the latch, and they are
                    // not the same set -- an unmasked precision exception
                    // writes its result and still latches.
                    if (unmasked(f, cw)) latched <= 1'b1;
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
                        if (cur == X87_ARITH || cur == X87_RNDINT || cur == X87_SCALE
                            || cur == X87_DIV || cur == X87_SQRT
                            || cur == X87_PREM || (cur == X87_TRANS && !t_push && !tr_nowr)) begin
                            rw_idx <= a_dst;
                            rw_val <= v;
                            rw_en <= 1'b1;
                            // A popping form whose destination is ST(0) --
                            // FADDP ST(0),ST -- writes the register and pops
                            // it: the pop wins, so it reads empty.
                            if (!(pop && a_dst == top)) empty[a_dst] <= 1'b0;
                        end
                        if (cur == X87_XTRACT || (cur == X87_TRANS && t_push && !tr_nowr)) begin
                            // A push form's stack underflow leaves the indefinite
                            // in the register ST(0) was too, so it stops being empty.
                            if (cur == X87_TRANS) empty[top] <= 1'b0;
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
            //
            // A write to port F0h in this cycle wins. The apply that raises
            // an exception sets the latch itself, in the same cycle as the
            // flags, so this edge fires the cycle *after* it, when ES is up
            // and `es_q` has not caught up -- and an F0h write landing in
            // that one cycle would be undone by it, leaving the latch set
            // and every later ESC held for ever. Clearing the latch is what
            // the port is for, and a genuinely new exception sets it from
            // its own apply above and not from here.
            if (es && !es_q && !env_load && !f0) latched <= 1'b1;
        end
    end
endmodule
