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
// - **BUSY# is a hold in D.** While the board's latch is set, every ESC but
//   FNINIT and FNCLEX, and WAIT, holds before it runs (`hold`), after #NM
//   and #UD. A write to port F0h clears the latch, F1h resets the unit.
//   The no-wait forms (FNSTSW, FNSTCW, FNSTENV, FNSAVE) skip only the
//   pending-error check, not the wait for BUSY#, so they hold too: the 387
//   PRM's Appendix F.1 list that leaves FSETPM out is the 286/287 AT's, and
//   the HRM's rule is the one a 386 follows (slice 0's `fsetpm` case).
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
//     the unit is not ready to answer. `unimpl` says the form is a later
//     slice's.
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
    input  logic        iss_last,
    input  logic [5:0]  iss_part,
    output logic        hold,
    output logic        unimpl,
    output logic [31:0] sval,
    output logic        nx,
    // Commit, at WB.
    input  logic        wb,
    input  logic        wb_last,
    input  logic [31:0] wb_val,
    input  logic [5:0]  wb_part,
    input  logic        flush,
    // The state the CPU and the bench read.
    output logic [15:0] cw_o,
    output logic [15:0] sw_o,
    output logic [15:0] tw_o,
    output logic [10:0] opcode_o,
    output logic        busy_o,
    // The eight physical registers, which the bench reads through the CPU's
    // arch_* outputs to build the oracle's X87State; nothing in the machine
    // uses them, so the seal's fit trims the wires.
    output logic [639:0] regs_o
);
    import fes_i386_pkg::*;

    // ---- The operand formats, as fes_i386_pkg names them. ----
    localparam logic [2:0] FMT_NONE = 3'd0;  // a register form: no operand
    localparam logic [2:0] FMT_F32  = 3'd1;
    localparam logic [2:0] FMT_F64  = 3'd2;
    localparam logic [2:0] FMT_F80  = 3'd3;
    localparam logic [2:0] FMT_I16  = 3'd4;
    localparam logic [2:0] FMT_I32  = 3'd5;
    localparam logic [2:0] FMT_I64  = 3'd6;
    localparam logic [2:0] FMT_ENV  = 3'd7;  // an environment or save image's field

    // ---- The operations this slice builds. ----
    typedef enum logic [4:0] {
        OP_UNIMPL,   // a later slice's form
        OP_NOP,      // FNOP, FENI, FDISI, FSETPM
        OP_LD_M,     // FLD or FILD of a memory operand: push
        OP_ST_M,     // FST, FIST: store ST(0)
        OP_STP_M,    // FSTP, FISTP: store ST(0) and pop
        OP_LDCW,
        OP_STCW,
        OP_STSW,
        OP_STENV,
        OP_LDENV,
        OP_SAVE,
        OP_RSTOR,
        OP_LD_ST,    // FLD ST(i)
        OP_ST_ST,    // FST ST(i)
        OP_STP_ST,   // FSTP ST(i)
        OP_XCH,
        OP_FREE,
        OP_INCSTP,
        OP_DECSTP,
        OP_INIT,
        OP_CLEX,
        OP_CONST,
        OP_CHS,
        OP_ABS,
        OP_TST,
        OP_XAM
    } op_e;

    // The eight constants FLD1 to FLDZ push, in the 387's order (D9 E8-EE),
    // and the significand each rounding mode gives for the ones that are not
    // exact in 64 bits (387 PRM, "FLDPI and the other constants"; the 387
    // rounds a constant under RC, so round down and round to zero give the
    // significand one less).
    function automatic logic [79:0] constant_of(input logic [2:0] i, input logic [1:0] rc);
        logic [63:0] sig;
        logic [14:0] exp;
        logic        near_up;   // rounding to nearest takes the significand above
        logic        inexact;
        case (i)
            3'd0: begin sig = 64'h8000000000000000; exp = 15'h3fff; near_up = 1'b0; inexact = 1'b0; end  // 1.0
            3'd1: begin sig = 64'hd49a784bcd1b8afe; exp = 15'h4000; near_up = 1'b0; inexact = 1'b1; end  // log2(10)
            3'd2: begin sig = 64'hb8aa3b295c17f0bb; exp = 15'h3fff; near_up = 1'b1; inexact = 1'b1; end  // log2(e)
            3'd3: begin sig = 64'hc90fdaa22168c234; exp = 15'h4000; near_up = 1'b1; inexact = 1'b1; end  // pi
            3'd4: begin sig = 64'h9a209a84fbcff798; exp = 15'h3ffd; near_up = 1'b1; inexact = 1'b1; end  // log10(2)
            3'd5: begin sig = 64'hb17217f7d1cf79ab; exp = 15'h3ffe; near_up = 1'b1; inexact = 1'b1; end  // ln(2)
            default: begin sig = 64'd0; exp = 15'd0; near_up = 1'b0; inexact = 1'b0; end                 // +0.0
        endcase
        // Every constant is positive, so round down and round to zero keep
        // the truncated significand and round up takes the next one.
        if (inexact && (rc == 2'd0 && near_up || rc == 2'd2)) sig = sig + 64'd1;
        constant_of = {1'b0, exp, sig};
    endfunction

    // ---- Decode: the 387 opcode to an operation. ----
    // op[10:8] is ESC's low three bits (D8h-DFh), op[7:0] the ModRM byte.
    function automatic op_e decode_op(input logic [10:0] o, input logic [2:0] fmt);
        logic [2:0] esc, rf;
        logic       regform;
        esc = o[10:8];
        rf = o[5:3];
        regform = o[7:6] == 2'b11;
        decode_op = OP_UNIMPL;
        if (!regform) begin
            // The memory forms. Every arithmetic form (D8, DA, DC, DE, and
            // D9/D8 with a memory operand) is a later slice's.
            case ({esc, rf})
                {3'b001, 3'd0}, {3'b101, 3'd0}, {3'b011, 3'd5},   // FLD m32, m64, m80
                {3'b011, 3'd0}, {3'b111, 3'd0}, {3'b111, 3'd5}:   // FILD m32, m16, m64
                    decode_op = OP_LD_M;
                {3'b001, 3'd2}, {3'b101, 3'd2},                   // FST m32, m64
                {3'b011, 3'd2}, {3'b111, 3'd2}:                   // FIST m32, m16
                    decode_op = OP_ST_M;
                {3'b001, 3'd3}, {3'b101, 3'd3}, {3'b011, 3'd7},   // FSTP m32, m64, m80
                {3'b011, 3'd3}, {3'b111, 3'd3}, {3'b111, 3'd7}:   // FISTP m32, m16, m64
                    decode_op = OP_STP_M;
                {3'b001, 3'd5}: decode_op = OP_LDCW;  // FLDCW, a word like any other
                {3'b001, 3'd7}: decode_op = OP_STCW;
                {3'b101, 3'd7}: decode_op = OP_STSW;
                {3'b001, 3'd6}: decode_op = OP_STENV;
                {3'b001, 3'd4}: decode_op = OP_LDENV;
                {3'b101, 3'd6}: decode_op = OP_SAVE;
                {3'b101, 3'd4}: decode_op = OP_RSTOR;
                default: decode_op = OP_UNIMPL;  // FBLD, FBSTP and the arithmetic forms
            endcase
            // A memory form needs its operand's format, and the four
            // environment and save forms the environment's.
            if (fmt == FMT_NONE) decode_op = OP_UNIMPL;
            if ((decode_op == OP_STENV || decode_op == OP_LDENV || decode_op == OP_SAVE
                 || decode_op == OP_RSTOR) != (fmt == FMT_ENV)) decode_op = OP_UNIMPL;
        end else begin
            case (esc)
                3'b001: begin  // D9
                    case (o[7:0])
                        8'hd0: decode_op = OP_NOP;   // FNOP
                        8'he0: decode_op = OP_CHS;
                        8'he1: decode_op = OP_ABS;
                        8'he4: decode_op = OP_TST;
                        8'he5: decode_op = OP_XAM;
                        8'hf6: decode_op = OP_DECSTP;
                        8'hf7: decode_op = OP_INCSTP;
                        default: begin
                            if (o[7:3] == 5'b11000) decode_op = OP_LD_ST;        // D9 C0+i
                            else if (o[7:3] == 5'b11001) decode_op = OP_XCH;     // D9 C8+i
                            else if (o[7:0] >= 8'he8 && o[7:0] <= 8'hee) decode_op = OP_CONST;
                            else decode_op = OP_UNIMPL;
                        end
                    endcase
                end
                3'b011: begin  // DB
                    // FENI, FDISI and FSETPM are a 387's FNOPs; FNCLEX and
                    // FNINIT are the two forms that do not wait for BUSY#.
                    case (o[7:0])
                        8'he0, 8'he1, 8'he4: decode_op = OP_NOP;
                        8'he2: decode_op = OP_CLEX;
                        8'he3: decode_op = OP_INIT;
                        default: decode_op = OP_UNIMPL;
                    endcase
                end
                3'b101: begin  // DD
                    if (o[7:3] == 5'b11000) decode_op = OP_FREE;         // DD C0+i
                    else if (o[7:3] == 5'b11010) decode_op = OP_ST_ST;   // DD D0+i
                    else if (o[7:3] == 5'b11011) decode_op = OP_STP_ST;  // DD D8+i
                    else decode_op = OP_UNIMPL;
                end
                3'b111: decode_op = o[7:0] == 8'he0 ? OP_STSW : OP_UNIMPL;  // DF E0
                default: decode_op = OP_UNIMPL;
            endcase
        end
    endfunction

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
    logic        sf, es;
    logic [3:0]  cc;      // C3 C2 C1 C0
    logic [2:0]  top;
    logic [10:0] opcode;
    logic [7:0]  empty;   // one bit a physical register
    logic [1:0]  rc, pc;  // the control word's rounding and precision fields
    assign rc = cw[11:10];
    assign pc = cw[9:8];
    // ES is set when an unmasked exception happens and stays set until
    // FNCLEX, FNINIT or the F1h reset clears it; B mirrors it on a 387
    // (Appendix C.1, where RESET leaves both set).
    assign sw_o = {es, cc[3], top, cc[2], cc[1], cc[0], es, sf, fl};
    assign cw_o = cw;
    assign opcode_o = opcode;
    // The tag word: the empty bit a register's own writes keep, and the two
    // class bits the write stored with it, so the word costs no read of the
    // stack (fes_i386_x87's header, "The tag word is derived").
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
    logic        rw_en;
    logic [79:0] rw_val;
    logic [79:0] ra_val, rb_val;
    always_ff @(posedge clk) begin
        ra_val <= rf_a[ra_idx];
        rb_val <= rf_b[rb_idx];
        if (rw_en) begin
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

    // ---- The instruction in flight. ----
    // One ESC instruction is in flight at a time (the CPU's interlock), so
    // these are the operation's own registers. `busy` is the 387's BUSY#:
    // it stays set until the operation finishes, which for a load is after
    // the instruction has retired, as a real 387's does.
    typedef enum logic [3:0] {
        S_IDLE, S_ARG, S_PRE, S_MUL, S_POST, S_POST2, S_RDY, S_SAVE_RD, S_APPLY, S_APPLY2
    } seq_e;
    seq_e        seq;
    op_e         cur;
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
    logic        committed;     // the instruction in flight has retired
    logic [1:0]  nw;            // a no-wait form waiting to commit: 1 FNINIT, 2 FNCLEX
    logic [10:0] cur_op_raw;    // its 387 opcode, which commit stores
    logic        cur_ctrl;      // ... and whether it is a control form
    assign busy_o = busy;

    // ---- The board's BUSY# latch (387 PRM Appendix F.2). ----
    // ERROR# going active latches BUSY#, which holds the next waiting ESC or
    // WAIT until a write to port F0h clears it. A write to F1h resets the
    // unit with ERROR# active and leaves the latch as it is, as the oracle's
    // adapter does (bochs_oracle.cpp, the F1h case). The latch is set in the
    // same cycle as ES, below, so an ESC already waiting in D cannot slip
    // past it.

    // ---- Issue. ----
    // FNINIT and FNCLEX are the two forms that do not wait for BUSY# (HRM
    // section 5.1), so they pass the latch and the unit's own work; every
    // other ESC, and WAIT, holds.
    op_e  iss_dec;
    logic iss_nowait;
    assign iss_dec = decode_op(iss_op, iss_fmt);
    assign iss_nowait = !iss_wait && (iss_dec == OP_INIT || iss_dec == OP_CLEX);
    // The forms whose value the CPU needs before the instruction retires:
    // a real or integer store, whose conversion the unit runs as the
    // instruction's first uop waits in D. FNSTCW, FNSTSW, FNSTENV and
    // FNSAVE answer out of state, FNSAVE after a register read apiece.
    logic started;             // the unit has taken the instruction at D's head
    logic val_form, take, iss_save_rd;
    assign val_form = iss_dec == OP_ST_M || iss_dec == OP_STP_M;
    assign iss_save_rd = iss && !iss_wait && iss_dec == OP_SAVE && iss_part[5] && iss_part[1:0] == 2'd0;
    assign unimpl = present && iss && !iss_wait && iss_first && iss_dec == OP_UNIMPL;
    // The hold (BUSY#): a new instruction waits for the board's latch and
    // for the unit, and one the unit has taken waits for its own value.
    // FNINIT and FNCLEX wait for neither (HRM section 5.1).
    assign hold = present && iss && !iss_nowait && !unimpl
               && (iss_first && (!started || committed) && (latched || busy)
                   || val_form && seq != S_RDY
                   || iss_save_rd && seq != S_SAVE_RD);
    // FNINIT and FNCLEX wait for neither the latch nor the unit, so they are
    // taken on their own, beside whatever the unit is working on: FNINIT
    // resets it, which is why it needs no wait, and FNCLEX only clears the
    // flags, the hazard Intel's note about FWAIT before FNCLEX describes.
    logic take_nw;
    assign take = present && iss && iss_first && !started && !busy && !latched
               && !iss_wait && !iss_nowait && iss_dec != OP_UNIMPL;
    assign take_nw = present && iss && iss_first && iss_nowait && nw == 2'd0;
    // The control forms, which leave the x87 pointers and the opcode as they
    // are: the same list fes_i386_decode gives X87P_NONE. FNOP (D9 D0) is
    // not one of them; FENI, FDISI and FSETPM (DB E0, E1, E4) are.
    logic ctrl_form;
    assign ctrl_form = iss_dec == OP_INIT || iss_dec == OP_CLEX || iss_dec == OP_STSW
                    || iss_dec == OP_STCW || iss_dec == OP_LDCW || iss_dec == OP_STENV
                    || iss_dec == OP_LDENV || iss_dec == OP_SAVE || iss_dec == OP_RSTOR
                    || iss_dec == OP_NOP && iss_op[10:8] == 3'b011;

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
            FMT_F80: ld_v = mval;
            FMT_F32: begin
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
            FMT_F64: begin
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
                iv = cur_fmt == FMT_I16 ? {{48{mval[15]}}, mval[15:0]}
                   : cur_fmt == FMT_I32 ? {{32{mval[31]}}, mval[31:0]} : mval[63:0];
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
    assign f_f64 = cur_fmt == FMT_F64;
    assign f_int = cur_fmt == FMT_I16 || cur_fmt == FMT_I32 || cur_fmt == FMT_I64;
    assign f_iw = cur_fmt == FMT_I16 ? 2'd0 : cur_fmt == FMT_I32 ? 2'd1 : 2'd2;
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
            FMT_F80: begin
                // FSTP m80 is a bit copy, so no operand raises anything but
                // the stack fault of an empty ST(0).
                st_v = st_cls == C_EMPTY ? INDEFINITE : ra_val;
                st_fl = st_cls == C_EMPTY ? 6'b000001 : 6'd0;
            end
            FMT_F32, FMT_F64: begin
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

    // ---- The value the CPU stores. ----
    // A memory store's part of the converted value; FNSTCW's, FNSTSW's and
    // an environment's first three fields out of the words themselves (the
    // CPU builds fields 3 to 6, the x87 pointers, itself); and a save
    // image's chunk of the register the unit has read.
    always_comb begin
        if (iss_dec == OP_STCW) sval = {16'd0, cw};
        else if (iss_dec == OP_STSW) sval = {16'd0, sw_o};
        else if (iss_dec == OP_STENV || iss_dec == OP_SAVE) begin
            if (iss_part[5])
                sval = iss_part[1:0] == 2'd0 ? ra_val[31:0]
                     : iss_part[1:0] == 2'd1 ? ra_val[63:32] : {16'd0, ra_val[79:64]};
            else
                sval = iss_part[2:0] == 3'd0 ? {16'd0, cw}
                     : iss_part[2:0] == 3'd1 ? {16'd0, sw_o} : {16'd0, tw_o};
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
    logic        mul_store;    // the multiply in flight is a store's shift
    logic [79:0] rimg;         // a save image's register, gathered by its chunks
    assign push_i = top - 3'd1;
    assign pop_i = top;
    assign busy = seq != S_IDLE || rw_en;

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
            es <= 1'b0;
            cc <= 4'd0;
            top <= 3'd0;
            opcode <= 11'd0;
            empty <= 8'hff;
            seq <= S_IDLE;
            started <= 1'b0;
            committed <= 1'b0;
            nw <= 2'd0;
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
            es <= 1'b1;
            cc <= 4'd0;
            top <= 3'd0;
            opcode <= 11'd0;
            empty <= 8'hff;
            seq <= S_IDLE;
            started <= 1'b0;
            committed <= 1'b0;
            nw <= 2'd0;
            rw_en <= 1'b0;
            pnx <= 1'b0;
        end else begin
            logic es_now;   // ES goes active this cycle, which latches BUSY#
            es_now = 1'b0;
            rw_en <= 1'b0;
            if (f0) latched <= 1'b0;
            // A squashed instruction leaves nothing behind, but one that has
            // retired runs on: its work finishes after it, as a 387's does
            // with BUSY# still active.
            if (flush && started && !committed) begin
                started <= 1'b0;
                committed <= 1'b0;
                nw <= 2'd0;
                pnx <= 1'b0;
                seq <= S_IDLE;
            end else begin
            if (take_nw) nw <= iss_dec == OP_INIT ? 2'd1 : 2'd2;
            if (wb && wb_last && started) committed <= 1'b1;
            // A load uop's value, and an environment's or a save image's
            // fields, as each commits.
            if (wb && started) begin
                if (cur == OP_LDENV || cur == OP_RSTOR) begin
                    if (!wb_part[5]) begin
                        case (wb_part[2:0])
                            3'd0: cw <= wb_val[15:0];
                            3'd1: begin
                                // The status word, TOP and the flags with it;
                                // a loaded ES raises ERROR# again.
                                fl <= wb_val[5:0];
                                sf <= wb_val[6];
                                es <= wb_val[7];
                                if (wb_val[7]) es_now = 1'b1;
                                cc <= {wb_val[14], wb_val[10], wb_val[9], wb_val[8]};
                                top <= wb_val[13:11];
                            end
                            3'd2: for (int n = 0; n < 8; n++) empty[n] <= wb_val[2*n +: 2] == 2'b11;
                            default: ;  // the x87 pointers, which the CPU keeps
                        endcase
                    end else begin
                        // FRSTOR's register image, a register's three chunks
                        // in order; the third writes the register.
                        case (wb_part[1:0])
                            2'd0: rimg[31:0] <= wb_val;
                            2'd1: rimg[63:32] <= wb_val;
                            default: begin
                                rw_idx <= wb_part[4:2];
                                rw_val <= {wb_val[15:0], rimg[63:0]};
                                rw_en <= 1'b1;
                            end
                        endcase
                    end
                end else if (cur == OP_LD_M || cur == OP_LDCW) begin
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
                    mul_store <= 1'b0;
                    seq <= S_ARG;
                end else if (take) begin
                    cur <= iss_dec;
                    cur_fmt <= iss_fmt;
                    cur_i <= iss_op[2:0];
                    cur_pop <= iss_dec == OP_STP_M || iss_dec == OP_STP_ST;
                    cur_op_raw <= iss_op;
                    cur_ctrl <= ctrl_form;
                    started <= 1'b1;
                    committed <= 1'b0;
                    pnx <= 1'b0;
                    pfl <= 6'd0;
                    psf <= 1'b0;
                    pc1 <= 1'b0;
                    pcc_wr <= 1'b0;
                    if (val_form) begin
                        ra_idx <= top;
                        mul_store <= 1'b1;
                        seq <= S_ARG;
                    end else if (iss_dec == OP_SAVE) begin
                        ra_idx <= iss_part[4:2];
                        seq <= S_SAVE_RD;
                    end
                end
                // A store's conversion: the register read lands, then the
                // shift (a multiply) when the result is a denormal of the
                // target or an integer, then the rounding.
                S_ARG: seq <= S_PRE;
                S_PRE: if (cur == OP_LD_M) begin
                    // A load's normalising left shift, a multiply by 2^lz.
                    if (ld_shift) begin
                        sh_in <= ld_mag;
                        sh_k <= 7'd0;
                        ua_mul <= ld_mag;
                        ub_mul <= 64'd1 << clz64(ld_mag);
                        sh_exp <= 15'(ld_expb - 17'(clz64(ld_mag)));
                        mul_start <= 1'b1;
                        seq <= S_MUL;
                    end else
                        seq <= S_APPLY;
                end else if (cur != OP_ST_M && cur != OP_STP_M) seq <= S_APPLY;
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
                        ua_mul <= st_sig;
                        ub_mul <= 64'd1 << (7'd64 - st_k);
                        mul_start <= 1'b1;
                        seq <= S_MUL;
                    end else
                        seq <= S_POST;
                end
                S_MUL: if (mul_done) seq <= mul_store ? S_POST : S_APPLY;
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
                    else if (iss && iss_part[5] && iss_part[4:2] != ra_idx) ra_idx <= iss_part[4:2];
                end
                // Every architectural change the operation makes, at the
                // instruction's commit. An unmasked exception leaves the
                // stack and the destination as they were and sets ES, which
                // raises ERROR# and latches BUSY#, as a 387 on an AT does.
                S_APPLY: begin
                    logic [5:0]  f;
                    logic        stk, um, c1, ccw, wr, pop, push, c1_wr;
                    logic [79:0] v;
                    logic [3:0]  nc;
                    cls_e        c0cls, cicls;
                    f = pfl;
                    stk = psf;
                    c1 = pc1;
                    ccw = 1'b0;
                    // The control forms, FNOP and FFREE leave the condition
                    // codes as they are; every other form writes C1.
                    c1_wr = !(cur == OP_NOP || cur == OP_STCW || cur == OP_STSW || cur == OP_STENV
                              || cur == OP_LDENV || cur == OP_SAVE || cur == OP_RSTOR
                              || cur == OP_FREE || cur == OP_LDCW);
                    nc = cc;
                    v = 80'd0;
                    wr = 1'b0;
                    pop = 1'b0;
                    push = 1'b0;
                    c0cls = class_of(ra_val, empty[top]);
                    cicls = class_of(rb_val, empty[top + cur_i]);
                    seq <= S_IDLE;
                    started <= 1'b0;
                    case (cur)
                        OP_LD_M, OP_LD_ST, OP_CONST: begin
                            // A push: an occupied destination is a stack
                            // overflow, whose masked result is the real
                            // indefinite (387 PRM section 3.1.7).
                            push = 1'b1;
                            v = cur == OP_CONST ? constant_of(cur_i, rc)
                              : cur == OP_LD_ST ? rb_val
                              : (sh_k != 7'd0 || mul_store ? 80'd0 : 80'd0);
                            if (cur == OP_LD_M) begin
                                f = ld_fl;
                                v = ld_shift ? {ld_sign, sh_exp, mul_p[63:0]} : ld_v;
                            end
                            if (cur == OP_LD_ST && cicls == C_EMPTY) begin
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
                        OP_ST_ST, OP_STP_ST: begin
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
                            pop = cur == OP_STP_ST;
                        end
                        OP_XCH: begin
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
                        OP_CHS, OP_ABS: begin
                            v = {cur == OP_CHS ? ~ra_val[79] : 1'b0, ra_val[78:0]};
                            if (c0cls == C_EMPTY) begin
                                f[0] = 1'b1;
                                stk = 1'b1;
                                v = INDEFINITE;
                            end
                            c1 = 1'b0;
                            wr = 1'b1;
                        end
                        OP_TST: begin
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
                        OP_XAM: begin
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
                        OP_FREE: empty[top + cur_i] <= 1'b1;
                        OP_INCSTP: begin
                            top <= top + 3'd1;
                            c1 = 1'b0;
                        end
                        OP_DECSTP: begin
                            top <= top - 3'd1;
                            c1 = 1'b0;
                        end
                        OP_INIT, OP_SAVE: begin
                            // FNINIT, and FNSAVE once its image is stored,
                            // leave the unit as FINIT does (387 PRM section
                            // 4.8.1): control 037Fh, no flags, every tag
                            // empty, the pointers and the opcode cleared.
                            cw <= 16'h037f;
                            fl <= 6'd0;
                            sf <= 1'b0;
                            es <= 1'b0;
                            cc <= 4'd0;
                            top <= 3'd0;
                            empty <= 8'hff;
                            opcode <= 11'd0;
                        end
                        OP_CLEX: begin
                            // FNCLEX clears the exception flags, the stack
                            // flag and ES, and keeps the condition codes and
                            // TOP (387 PRM, FCLEX).
                            fl <= 6'd0;
                            sf <= 1'b0;
                            es <= 1'b0;
                        end
                        OP_LDCW: begin
                            cw <= mval[15:0];
                            // Unmasking an exception that is already pending
                            // makes it due at the next waiting ESC, so ES
                            // goes active now.
                            if (unmasked(fl, mval[15:0])) es <= 1'b1;
                        end
                        OP_STENV: cw[5:0] <= 6'h3f;  // FNSTENV masks every exception
                        OP_ST_M, OP_STP_M: pop = cur_pop;
                        default: ;  // FNOP, FNSTCW, FNSTSW, FLDENV, FRSTOR
                    endcase
                    um = unmasked(f, cw);
                    // The flags and the stack flag are sticky; ES stays set
                    // until FNCLEX, FNINIT or the F1h reset clears it.
                    if (cur != OP_INIT && cur != OP_SAVE && cur != OP_CLEX) begin
                        fl <= fl | f;
                        if (stk) sf <= 1'b1;
                        if (um) begin
                            es <= 1'b1;
                            es_now = 1'b1;
                        end
                        if (c1_wr) cc[1] <= c1;
                        if (ccw) cc <= {nc[3], nc[2], c1, nc[0]};
                    end
                    // An unmasked exception leaves the registers and TOP as
                    // they were; a masked one delivers its result.
                    if (!um) begin
                        if (push) begin
                            top <= push_i;
                            empty[push_i] <= 1'b0;
                            rw_idx <= push_i;
                            rw_val <= v;
                            rw_en <= wr;
                        end else if (pop) begin
                            empty[top] <= 1'b1;
                            top <= top + 3'd1;
                        end
                        if (cur == OP_ST_ST || cur == OP_STP_ST) begin
                            rw_idx <= top + cur_i;
                            rw_val <= v;
                            rw_en <= 1'b1;
                            if (empty[top + cur_i]) empty[top + cur_i] <= 1'b0;
                        end
                        if (cur == OP_CHS || cur == OP_ABS) begin
                            rw_idx <= top;
                            rw_val <= v;
                            rw_en <= 1'b1;
                        end
                        if (cur == OP_XCH) begin
                            // Two writes, so two cycles: ST(0) now and
                            // ST(i) next.
                            rw_idx <= top;
                            rw_val <= f[0] ? INDEFINITE : rb_val;
                            rw_en <= 1'b1;
                            empty[top] <= 1'b0;
                            empty[top + cur_i] <= 1'b0;
                            sres <= f[0] ? INDEFINITE : ra_val;
                            seq <= S_APPLY2;
                        end
                    end
                end
                S_APPLY2: begin
                    rw_idx <= top + cur_i;
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
            // FNINIT and FNCLEX, at their own commit, after the case above
            // so FNINIT's reset wins over an operation finishing with it.
            if (wb && wb_last && nw != 2'd0) begin
                nw <= 2'd0;
                if (nw == 2'd1) begin
                    cw <= 16'h037f;
                    fl <= 6'd0;
                    sf <= 1'b0;
                    es <= 1'b0;
                    cc <= 4'd0;
                    top <= 3'd0;
                    empty <= 8'hff;
                    opcode <= 11'd0;
                    seq <= S_IDLE;
                    started <= 1'b0;
                    committed <= 1'b0;
                end else begin
                    fl <= 6'd0;
                    sf <= 1'b0;
                    es <= 1'b0;
                end
            end
            end
            if (es_now && !es) latched <= 1'b1;
        end
    end
endmodule
