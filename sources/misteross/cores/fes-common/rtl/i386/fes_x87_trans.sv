// SPDX-License-Identifier: MIT
// The x87 transcendentals' engine, on the DE25 arithmetic backend (#1958).
//
// FSIN, FCOS, FSINCOS, FPTAN, F2XM1, FYL2X, FYL2XP1 and FPATAN are short
// sequences of the backend's own add, subtract, multiply and divide over
// constants in memory. scripts/x87_trans_ucode.py is where the sequences are
// written, where each microprogram word is defined, and where the exact
// model this engine must equal bit for bit lives; fes_x87_trans_rom and
// fes_x87_trans_pkg are generated from it. This file is the machine that
// runs them: sixteen 80-bit temporaries, an index J, an exponent E, a few
// flags and a one-level link.
//
// What the engine does not have is exception logic. `fes_i386_x87` enters it
// only for finite, non-zero operands it has already normalised, every
// intermediate operation is rounded to nearest at 64 bits and stays inside
// the normal range by construction, and the instruction's LAST operation is
// handed back unrounded, in the form the unit's A_RND takes, so that the
// final rounding under the control word, overflow, underflow and the inexact
// flag are the unit's, the same code the arithmetic uses.
//
// The backend is the unit's own `fes_x87_core`: the engine drives its
// request port while it runs, one operation at a time.
//
// FPREM, FPREM1, FRNDINT, FSCALE and FXTRACT run here too (#2027 slice 6).
// They are exact: the remainder is the trigonometric reduction's own
// bit-a-clock divider with the divisor y in place of pi/4, FRNDINT is one
// addition rounded under RC, and FSCALE and FXTRACT are exponent moves.
module fes_x87_trans (
    input  logic        clk,
    input  logic        reset,
    input  logic        cancel,
    // The instruction (fes_x87_trans_pkg TR_*), and its operands as the unit
    // normalises them: the sign, the unbiased exponent a denormal really
    // has, and the significand with bit 63 set. y is ST(1) of a two-operand
    // form.
    input  logic        start,
    input  logic [3:0]  instruction,
    input  logic [1:0]  rc,          // the control word's RC, for ADDRC
    input  logic        xs,
    input  logic signed [17:0] xe,
    input  logic [63:0] xm,
    input  logic        ys,
    input  logic signed [17:0] ye,
    input  logic [63:0] ym,
    // The backend's request and its result before rounding.
    output logic        req_valid,
    input  logic        req_ready,
    output logic [2:0]  req_op,
    output logic [79:0] req_a,
    output logic [79:0] req_b,
    input  logic        raw_valid,
    input  fes_x87_pkg::raw_t raw,
    // The last operation's result, unrounded: a zero, or a sign, an exponent
    // biased by 16383 and a 67-bit significand whose low three bits are the
    // guard, the round and the sticky. `second` is the other result of a
    // two-result form: 1 the value in `second`, 2 the x operand itself.
    output logic        busy,
    output logic        done,
    output logic        fin_zero,
    output logic        fin_sign,
    output logic signed [17:0] fin_exp,
    output logic [66:0] fin_sig,
    output logic [1:0]  second_mode,
    output logic [79:0] second,
    // FPREM's quotient's low three bits and whether the step was partial;
    // FRNDINT's rounding: inexact, and away from zero (C1).
    output logic [2:0]  prem_q,
    output logic        prem_part,
    output logic        rnd_pe,
    output logic        rnd_c1
);
    import fes_x87_trans_pkg::*;

    typedef enum logic [3:0] {
        S_IDLE, S_INIT, S_FETCH, S_DEC, S_RDB, S_EX, S_ISSUE, S_WAIT, S_NORM, S_TDIV, S_TFIX, S_TW2, S_SEC,
        S_DONE, S_PFIX
    } state_e;
    state_e state;

    // ---- The memories. ----
    logic [8:0]  pc, link;
    logic [39:0] uw;
    logic [9:0]  c_addr;
    logic [79:0] c_q;
    fes_x87_trans_rom rom (.clk, .pc, .word(uw), .address(c_addr), .constant(c_q));
    logic [79:0] temps [0:15];
    logic [3:0]  t_raddr, t_waddr;
    logic [79:0] t_q, t_wdata;
    logic        t_we;
    always_ff @(posedge clk) begin
        if (t_we) temps[t_waddr] <= t_wdata;
        t_q <= temps[t_raddr];
    end

    // ---- The word's fields. ----
    logic [4:0] u_op;
    logic [3:0] u_dst, u_a, u_negf, u_sect;
    logic [1:0] u_bmode, u_secm;
    logic [9:0] u_baddr;
    logic       u_use_e, u_neg;
    assign u_op = uw[4:0];
    assign u_dst = uw[8:5];
    assign u_a = uw[12:9];
    assign u_bmode = uw[14:13];
    assign u_baddr = uw[24:15];
    assign u_use_e = uw[25];
    assign u_neg = uw[26];
    assign u_negf = uw[30:27];
    assign u_secm = uw[32:31];
    assign u_sect = uw[36:33];

    // ---- The state the microprogram sees. ----
    logic [7:0]         j;
    logic signed [17:0] e;
    logic               f_swap, f_sneg, f_cneg, f_c, f_m1;
    logic [1:0]         init_n;
    logic [79:0]        a_val, b_val, b_now;
    logic [15:0]        flags;
    always_comb begin
        flags = '0;
        flags[FL_SWAP] = f_swap;
        flags[FL_SNEG] = f_sneg;
        flags[FL_CNEG] = f_cneg;
        flags[FL_TNEG] = f_sneg ^ f_cneg;
        flags[FL_XS] = xs;
        flags[FL_YS] = ys;
        flags[FL_T32] = xe < -18'sd32;
        flags[FL_T68] = xe < -18'sd68;
        flags[FL_C] = f_c;
        flags[FL_EZ] = e == 18'sd0;
        flags[FL_EBIG] = e > 18'sd70;
        flags[FL_ESMALL] = e < -18'sd70;
        flags[FL_YGTX] = ye > xe || (ye == xe && ym > xm);
        flags[FL_M1] = f_m1;
        flags[FL_J64] = j == 8'd64;
        flags[FL_X63] = xe >= 18'sd63;
    end

    // The constant's address follows the word, so the constant is out a
    // clock after the word is; a temporary is read in S_DEC (a) and S_RDB (b).
    assign c_addr = u_bmode == 2'd2 ? u_baddr + {2'b0, j} : u_baddr;
    assign b_now = u_bmode == 2'd0 ? t_q : c_q;
    always_comb begin
        t_raddr = u_a;
        if (state == S_RDB) t_raddr = u_baddr[3:0];
        if (state == S_ISSUE || state == S_WAIT || (state == S_EX && u_op == U_FIN)) t_raddr = u_sect;
    end

    // ---- Small combinational helpers. ----
    function automatic logic [14:0] biased(input logic signed [17:0] v);
        logic signed [17:0] b;
        b = v + 18'sd16383;
        biased = b[14:0];
    endfunction
    function automatic logic is_zero(input logic [79:0] v);
        is_zero = v[78:0] == 79'd0;
    endfunction

    // The backend's unrounded result, rounded to nearest even at 64 bits --
    // or, for ADDRC, under the control word's RC.
    // A zero comes back either as `special` with its value, when an operand
    // was a zero, or as a zero significand, when the operands cancelled.
    logic [79:0] rounded;
    logic        r_inexact, up;
    assign r_inexact = |raw.significand[2:0];
    always_comb begin
        logic [64:0] sum;
        logic signed [17:0] ex;
        if (u_op != U_ADDRC || rc == 2'd0)
            up = raw.significand[2] && (raw.significand[1] || raw.significand[0] || raw.significand[3]);
        else
            up = r_inexact && (rc == 2'd1 ? raw.sign : rc == 2'd2 ? !raw.sign : 1'b0);
        sum = {1'b0, raw.significand[66:3]} + 65'(up);
        ex = raw.exponent + 18'(sum[64]);
        if (raw.special) rounded = raw.value;
        else if (raw.significand == 67'd0) rounded = {raw.sign, 79'd0};
        else rounded = {raw.sign, ex[14:0], sum[64] ? 64'h8000_0000_0000_0000 : sum[63:0]};
    end

    // ---- The normaliser: shift left until bit 66 is set. ----
    // LO and ITOF use it for a value of their own, and the reduction for its
    // remainder, whose three bits below the 64-bit window are the rest.
    logic [66:0]        nr;
    logic signed [17:0] ne;      // the biased exponent of bit 66
    logic               nsign;
    logic               n_tred;
    logic               n_fin;       // the remainder: hand the normalised value back as FIN does

    // ---- The trigonometric reduction: |x| = q P + r, P = PM 2^-67. ----
    // FPREM and FPREM1 run the same divider with y's significand as the
    // divisor (`prem`), doubled where the exponent difference is -1, so that
    // after x's 64 bits and n more the remainder is x 2^n mod the divisor,
    // exactly, and the quotient's low three bits are what C0, C3 and C1 need.
    logic [66:0] tr_r;
    logic [63:0] tr_d;
    logic [7:0]  tr_n;
    logic [2:0]  tr_q;
    logic [67:0] tr_next;
    logic        tr_ge;
    logic        prem, p_one;
    logic [64:0] p_div;
    logic signed [17:0] p_e;         // the biased exponent of the remainder's bit 63
    logic [66:0] divisor;
    assign divisor = prem ? {2'b0, p_div} : PM;
    assign tr_next = {tr_r, tr_d[63]};
    assign tr_ge = tr_next >= {1'b0, divisor};
    logic [66:0] back;
    assign back = divisor - tr_r;

    // The rest of the reduced argument: the three bits below the window.
    function automatic logic [79:0] rest_of(input logic [2:0] low, input logic signed [17:0] top);
        logic signed [17:0] ex;
        if (low == 3'd0) rest_of = 80'd0;
        else if (low[2]) begin
            ex = top - 18'sd64;
            rest_of = {1'b0, ex[14:0], low, 61'd0};
        end else if (low[1]) begin
            ex = top - 18'sd65;
            rest_of = {1'b0, ex[14:0], low[1:0], 62'd0};
        end else begin
            ex = top - 18'sd66;
            rest_of = {1'b0, ex[14:0], 1'b1, 63'd0};
        end
    endfunction

    // y truncated toward zero, for FSCALE: below 2^16 its integer part is in
    // the significand's top sixteen bits; from 2^16 a scale past any exponent.
    logic signed [17:0] y_trunc;
    always_comb begin
        logic [15:0] whole;
        whole = ym[63:48] >> (4'd15 - ye[3:0]);
        if (ym == 64'd0 || ye < 18'sd0) y_trunc = 18'sd0;
        else if (ye >= 18'sd16) y_trunc = ys ? -18'sd36000 : 18'sd36000;
        else y_trunc = ys ? -$signed({2'b0, whole}) : $signed({2'b0, whole});
    end

    // ---- The single-clock operations. ----
    logic [79:0]        simple;
    logic signed [17:0] b_exp;       // b's unbiased exponent
    logic [8:0]         b_top;
    logic [3:0]         idx_shift;
    logic [8:0]         idx_whole;
    always_comb begin
        logic signed [17:0] ex;
        b_exp = $signed({3'b0, b_now[78:64]}) - 18'sd16383;
        b_top = b_now[63:55];
        // J = round(|b| 2^k): the significand's top nine bits, shifted right
        // by 8 - k - (b's exponent), plus the bit below.
        idx_shift = 4'(18'sd8 - 18'($signed({14'b0, u_dst})) - b_exp);
        idx_whole = (b_top >> idx_shift) + 9'((b_top >> (idx_shift - 4'd1)) & 9'd1);
        simple = b_now;
        ex = '0;
        case (u_op)
            U_NEG: simple = {!b_now[79], b_now[78:0]};
            U_ABS: simple = {1'b0, b_now[78:0]};
            U_HI: simple = {b_now[79:43], 43'd0};
            U_SCALE: begin
                ex = $signed({3'b0, b_now[78:64]}) + (u_a[0] ? -e : e);
                simple = is_zero(b_now) ? b_now : {b_now[79], ex[14:0], b_now[63:0]};
            end
            default: simple = b_now;
        endcase
    end

    // ---- The request. ----
    // RSUB is b - a: the operands change places.
    logic       fin_backend;
    logic [2:0] fin_kind;
    assign fin_kind = u_dst[2:0];
    assign fin_backend = u_op == U_FIN && fin_kind <= UF_DIV;
    assign req_valid = state == S_ISSUE;
    assign req_a = u_op == U_RSUB ? b_val : a_val;
    assign req_b = u_op == U_RSUB ? a_val : b_val;
    always_comb begin
        logic [4:0] what;
        what = u_op == U_FIN ? (fin_kind == UF_ADD ? U_ADD : fin_kind == UF_SUB ? U_SUB
                                : fin_kind == UF_MUL ? U_MUL : U_DIV) : u_op;
        req_op = what == U_MUL ? 3'(fes_x87_pkg::OP_MUL) : what == U_DIV ? 3'(fes_x87_pkg::OP_DIV)
               : (what == U_ADD || what == U_ADDRC) ? 3'(fes_x87_pkg::OP_ADD) : 3'(fes_x87_pkg::OP_SUB);
    end

    assign busy = state != S_IDLE;
    logic negate;
    assign negate = u_neg && flags[u_negf];

    always_ff @(posedge clk) begin
        t_we <= 1'b0;
        done <= 1'b0;
        if (reset || cancel) begin
            state <= S_IDLE;
        end else begin
            case (state)
                S_IDLE: if (start) begin
                    pc <= entry_of(instruction);
                    init_n <= 2'd0;
                    e <= 18'sd0;
                    j <= 8'd0;
                    f_swap <= 1'b0;
                    f_sneg <= 1'b0;
                    f_cneg <= 1'b0;
                    f_c <= 1'b0;
                    f_m1 <= 1'b0;
                    prem <= 1'b0;
                    prem_q <= 3'd0;
                    prem_part <= 1'b0;
                    rnd_pe <= 1'b0;
                    rnd_c1 <= 1'b0;
                    n_fin <= 1'b0;
                    second_mode <= 2'd0;
                    state <= S_INIT;
                end
                // x, y, and each one's significand at exponent 0, signed.
                S_INIT: begin
                    t_we <= 1'b1;
                    t_waddr <= {2'b0, init_n};
                    case (init_n)
                        2'd0: t_wdata <= {xs, biased(xe), xm};
                        2'd1: t_wdata <= {ys, biased(ye), ym};
                        2'd2: t_wdata <= {xs, 15'h3fff, xm};
                        default: t_wdata <= {ys, 15'h3fff, ym};
                    endcase
                    init_n <= init_n + 2'd1;
                    if (init_n == 2'd3) state <= S_FETCH;
                end
                // The word for `pc` is out of the ROM a clock after `pc`
                // settles, so a taken branch costs this one clock.
                S_FETCH: state <= S_DEC;
                S_DEC: state <= S_RDB;         // the word is valid; a is being read
                S_RDB: begin                   // a is out; b is being read
                    a_val <= t_q;
                    state <= S_EX;
                end
                S_EX: begin
                    b_val <= b_now;
                    pc <= pc + 9'd1;
                    state <= S_FETCH;
                    case (u_op)
                        U_ADD, U_SUB, U_RSUB, U_MUL, U_DIV, U_ADDRC: begin
                            pc <= pc;
                            state <= S_ISSUE;
                        end
                        U_MOV, U_NEG, U_ABS, U_HI, U_SCALE: begin
                            t_we <= 1'b1;
                            t_waddr <= u_dst;
                            t_wdata <= simple;
                        end
                        U_LO: begin
                            // What HI cleared, as a value: the low 43 bits,
                            // already 21 places up, then normalised.
                            if (b_now[42:0] == 43'd0) begin
                                t_we <= 1'b1;
                                t_waddr <= u_dst;
                                t_wdata <= {b_now[79], 79'd0};
                            end else begin
                                nr <= {b_now[42:0], 24'd0};
                                ne <= $signed({3'b0, b_now[78:64]}) - 18'sd21;
                                nsign <= b_now[79];
                                n_tred <= 1'b0;
                                pc <= pc;
                                state <= S_NORM;
                            end
                        end
                        U_ITOF: begin
                            if (e == 18'sd0) begin
                                t_we <= 1'b1;
                                t_waddr <= u_dst;
                                t_wdata <= 80'd0;
                            end else begin
                                nr <= {e[17] ? 17'(-e) : 17'(e), 50'd0};
                                ne <= 18'sd16383 + 18'sd16;
                                nsign <= e[17];
                                n_tred <= 1'b0;
                                pc <= pc;
                                state <= S_NORM;
                            end
                        end
                        U_TIDX: j <= is_zero(b_now) ? 8'd0 : idx_whole[7:0];
                        U_LSPL: begin
                            // The significand as a value in (sqrt 1/2, sqrt 2]
                            // and the exponent that leaves.
                            logic [63:0]        m;
                            logic signed [17:0] ex;
                            m = u_a[0] ? xm : b_now[63:0];
                            ex = u_a[0] ? xe : b_exp;
                            t_we <= 1'b1;
                            t_waddr <= u_dst;
                            t_wdata <= {1'b0, m > SQRT2 ? 15'h3ffe : 15'h3fff, m};
                            e <= m > SQRT2 ? ex + 18'sd1 : ex;
                            f_m1 <= m == 64'h8000_0000_0000_0000;
                        end
                        U_SETE: begin
                            case (u_a[2:0])
                                3'd0: e <= xe;
                                3'd1: e <= ye;
                                3'd2: e <= ye - xe;
                                3'd3: e <= xe - ye;
                                3'd4: e <= xe + ye;
                                3'd6: e <= y_trunc;
                                default: e <= 18'sd0;
                            endcase
                        end
                        U_CMP: f_c <= a_val[78:0] < b_now[78:0];
                        U_TRED: begin
                            pc <= pc;
                            if (xe >= -18'sd1) begin
                                tr_r <= 67'd0;
                                tr_d <= xm;
                                tr_q <= 3'd0;
                                tr_n <= 8'(xe + 18'sd68);
                                state <= S_TDIV;
                            end else begin
                                // Below pi/4 by its exponent alone: the
                                // argument is its own remainder.
                                f_swap <= 1'b0;
                                f_sneg <= xs;
                                f_cneg <= 1'b0;
                                t_we <= 1'b1;
                                t_waddr <= 4'd4;
                                t_wdata <= {1'b0, biased(xe), xm};
                                nr <= 67'd0;
                                state <= S_TW2;
                            end
                        end
                        U_PREM: begin
                            // The difference below zero gives x back, except
                            // FPREM1 at -1 with x's significand above y's;
                            // so does a divisor of infinity (exponent 16384).
                            logic signed [17:0] d;
                            logic [6:0]         n;
                            d = xe - ye;
                            n = d >= 18'sd64 ? (7'd32 | {2'b0, d[4:0]}) : d < 18'sd0 ? 7'd0 : 7'(d[6:0]);
                            pc <= pc;
                            if (ye == 18'sd16384 || d < -18'sd1 || (d == -18'sd1 && !(u_a[0] && xm > ym))) begin
                                fin_zero <= 1'b0;
                                fin_sign <= xs;
                                fin_exp <= xe + 18'sd16383;
                                fin_sig <= {xm, 3'b000};
                                state <= S_SEC;
                            end else begin
                                prem <= 1'b1;
                                p_one <= u_a[0] && d < 18'sd64;
                                prem_part <= d >= 18'sd64;
                                p_div <= d < 18'sd0 ? {ym, 1'b0} : {1'b0, ym};
                                p_e <= xe - 18'(n) + 18'sd16383;
                                tr_r <= 67'd0;
                                tr_d <= xm;
                                tr_q <= 3'd0;
                                tr_n <= 8'd64 + {1'b0, n};
                                state <= S_TDIV;
                            end
                        end
                        U_BR: if (flags[u_a] == u_dst[0]) pc <= u_baddr[8:0];
                        U_JMP: pc <= u_baddr[8:0];
                        U_CALL: begin
                            link <= pc + 9'd1;
                            pc <= u_baddr[8:0];
                        end
                        U_RET: pc <= link;
                        U_FIN: begin
                            pc <= pc;
                            second_mode <= u_secm;
                            if (fin_backend) state <= S_ISSUE;
                            else begin
                                // a itself, or an eighth of a unit short of
                                // or past it. A power of two's short side is
                                // in the binade below, where the unit halves.
                                logic signed [17:0] ex;
                                ex = $signed({3'b0, a_val[78:64]}) + (u_use_e ? e : 18'sd0);
                                fin_zero <= is_zero(a_val);
                                fin_sign <= a_val[79] ^ negate;
                                fin_exp <= ex;
                                fin_sig <= {a_val[63:0], 3'b000};
                                if (fin_kind == UF_X) begin
                                    // x itself, with the exponent it really has.
                                    fin_zero <= 1'b0;
                                    fin_sign <= xs ^ negate;
                                    fin_exp <= xe + 18'sd16383 + (u_use_e ? e : 18'sd0);
                                    fin_sig <= {xm, 3'b000};
                                end
                                if (fin_kind == UF_ABOVE) fin_sig <= {a_val[63:0], 3'b001};
                                if (fin_kind == UF_BELOW) begin
                                    if (a_val[62:0] == 63'd0) begin
                                        fin_exp <= ex - 18'sd1;
                                        fin_sig <= {64'hffff_ffff_ffff_ffff, 3'b110};
                                    end else
                                        fin_sig <= {a_val[63:0] - 64'd1, 3'b111};
                                end
                                state <= S_SEC;
                            end
                        end
                        default: ;
                    endcase
                end
                S_ISSUE: if (req_ready) state <= S_WAIT;
                // `raw` and `raw_valid` are the lanes' registered outputs, so
                // this reads a settled value.
                S_WAIT: if (raw_valid) begin
                    if (u_op == U_FIN) begin
                        fin_zero <= raw.special || raw.significand == 67'd0;
                        fin_sign <= (raw.special ? raw.value[79] : raw.sign) ^ negate;
                        fin_exp <= raw.exponent + (u_use_e ? e : 18'sd0);
                        fin_sig <= raw.significand;
                        state <= S_SEC;
                    end else begin
                        t_we <= 1'b1;
                        t_waddr <= u_dst;
                        t_wdata <= rounded;
                        if (u_op == U_ADDRC) begin
                            rnd_pe <= r_inexact;
                            rnd_c1 <= up;
                        end
                        pc <= pc + 9'd1;
                        state <= S_FETCH;
                    end
                end
                S_NORM: begin
                    if (n_fin && nr[66]) begin
                        fin_zero <= 1'b0;
                        fin_sign <= nsign;
                        fin_exp <= ne;
                        fin_sig <= nr;
                        state <= S_SEC;
                    end else if (nr[66] || nr == 67'd0) begin
                        t_we <= 1'b1;
                        t_waddr <= n_tred ? 4'd4 : u_dst;
                        t_wdata <= nr == 67'd0 ? 80'd0 : {nsign, ne[14:0], nr[66:3]};
                        if (n_tred) state <= S_TW2;
                        else begin
                            pc <= pc + 9'd1;
                            state <= S_FETCH;
                        end
                    end else begin
                        nr <= {nr[65:0], 1'b0};
                        ne <= ne - 18'sd1;
                    end
                end
                // One quotient bit a clock: the significand's 64 bits, then
                // the zeros below it down to the units of P.
                S_TDIV: begin
                    tr_r <= tr_ge ? 67'(tr_next - {1'b0, divisor}) : tr_next[66:0];
                    tr_q <= {tr_q[1:0], tr_ge};
                    tr_d <= {tr_d[62:0], 1'b0};
                    tr_n <= tr_n - 8'd1;
                    if (tr_n == 8'd1) state <= prem ? S_PFIX : S_TFIX;
                end
                // The remainder: FPREM1's completing step rounds the quotient
                // to nearest, which takes the divisor less the remainder and
                // the other sign (2r is compared with the divisor, not r with
                // half of it, which would drop an odd divisor's low bit).
                // `back` shares the reduction's P - r subtractor.
                S_PFIX: begin
                    logic       rup;
                    logic [63:0] rem;
                    rup = p_one && ({tr_r[64:0], 1'b0} > {1'b0, p_div}
                                    || ({tr_r[64:0], 1'b0} == {1'b0, p_div} && tr_q[0]));
                    rem = rup ? back[63:0] : tr_r[63:0];
                    prem_q <= tr_q + 3'(rup);
                    if (rem == 64'd0) begin
                        fin_zero <= 1'b1;
                        fin_sign <= xs ^ rup;
                        state <= S_SEC;
                    end else begin
                        nr <= {rem, 3'b000};
                        ne <= p_e;
                        nsign <= xs ^ rup;
                        n_tred <= 1'b0;
                        n_fin <= 1'b1;
                        state <= S_NORM;
                    end
                end
                // An odd octant takes P - r; then the octant's three flags,
                // and the remainder as a value: bit 66 is worth a half.
                S_TFIX: begin
                    nr <= tr_q[0] ? back : tr_r;
                    ne <= 18'sd16382;
                    nsign <= 1'b0;
                    n_tred <= 1'b1;
                    f_swap <= tr_q[1] ^ tr_q[0];
                    f_sneg <= tr_q[2] ^ xs;
                    f_cneg <= tr_q[2] ^ tr_q[1];
                    state <= S_NORM;
                end
                // The remainder's rest, into T5.
                S_TW2: begin
                    t_we <= 1'b1;
                    t_waddr <= 4'd5;
                    t_wdata <= rest_of(nr[2:0], ne);
                    pc <= pc + 9'd1;
                    state <= S_FETCH;
                end
                // The other result's temporary has been read since S_ISSUE.
                S_SEC: begin
                    second <= t_q;
                    done <= 1'b1;
                    state <= S_DONE;
                end
                S_DONE: state <= S_IDLE;
                default: state <= S_IDLE;
            endcase
        end
    end

    // synthesis translate_off
    // Every intermediate stays inside the normal range by construction; this
    // is what makes that a checked claim in every simulation.
    always_ff @(posedge clk)
        if (state == S_WAIT && raw_valid && u_op != U_FIN && !raw.special && raw.significand != 67'd0
            && (raw.exponent < 18'sd1 || raw.exponent > 18'sd32766))
            $fatal(1, "fes_x87_trans: an intermediate result left the normal range at word %0d", pc);
    // synthesis translate_on
endmodule
