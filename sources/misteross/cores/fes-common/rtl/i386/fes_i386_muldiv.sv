// SPDX-License-Identifier: MIT
// EX's multiply and divide unit (docs/i386-cpu-design.md, "Execute"): MUL,
// IMUL in all three forms, DIV, IDIV, AAM and AAD. A multiply or divide uop
// works here over several cycles while D to WB freeze (go, which
// fes_i386_cpu registers as the uop enters EX). In its last cycle (last) the
// unit gives its result and whether it raises #DE, both from registers,
// which fes_i386_cpu writes into EX's register for the uop, and registers
// its flags; the uop leaves on the cycle after with them, so EX's result and
// fault logic gain nothing.
//
// The uop's operands are EX's, which stay put while frozen. In its first
// cycle (E0) the unit makes them itself from what EX makes them of, so
// nothing of EX's logic drives it, and registers them with the uop's
// decode: src1's whole value (EAX, the destination register, which the
// result merges into) from ar and the WB result (byp_a), and src2, or the
// loaded value, at the uop's size from yr, wb, ld and the bypass selects
// yw. It works from its own registers after that; the DSPs take theirs.
//
// A 16- or 32-bit MUL or DIV has two destinations. Its uop writes EAX and
// keeps the high half (EDX's product or remainder, hi), which a HI uop
// after it takes as its result as it leaves M2, merged into EDX by EX's
// lanes. A 16- or 32-bit DIV first takes
// EDX, its dividend's high half, from a LOAD uop (ld_en, while one is in EX).
//
// Multiply: four 18x18 signed DSP products of the 33-bit operands, signed
// or zero-extended (E1, from E0's registers), their sum P (E2), the result halves and CF and OF,
// which say the product does not fit the destination (E3), then the flags
// (E4). The undefined SF, ZF, AF and PF follow the 386EX, whose microcode
// multiplies a bit per cycle, right to left, and stops early once the
// multiplier's remaining bits are all zeros, or all ones for a negative IMUL
// multiplier: they are the flags of its last step, which come out of P
// shifted right by that step (W):
//   multiplier m >= 0, k = max(bits(m), 3) steps: ADD of the multiplicand a
//     to the partial product, whose sum is P >> (k - 1), or (P >> 2) + a
//     when m < 4;
//   m < 0 (IMUL), k = min(n, max(bits(~m), trailing zeros(m) + 4)): a SUB
//     of a, whose difference is P >> (k - 1) when m's bit k - 1 is clear,
//     else (P >> (k - 1)) - a; and -a for m = -2^(n-1).
// AAD multiplies AH by imm, then adds AL with the ADD's flags.
//
// Divide: restoring, a bit per cycle as the 386EX's microcode, whose flags
// it keeps: the last step's trial subtract of the divisor from the shifted
// partial remainder, at the operand size (its bit out at the top counts in
// the quotient bit for DIV). DIV first compares the dividend's high half
// with the divisor: if it is not below, the quotient overflows (#DE, de) and
// the 386EX keeps the subtract, then takes one step fewer. IDIV divides the
// dividend's one's complement when it is negative, by the divisor's
// magnitude, with no bit out at the top; the remainder is then one more,
// and a remainder equal to the divisor is zero with the quotient one more.
// It overflows when that quotient is too big for its sign. On an
// overflowing dividend the steps leave a garbage quotient, which now and
// then passes, as the 386EX's does (8-bit quotients of 80h); no corpus case
// says whether the 386EX checks before or after the one more, and after it
// agrees with the oracle. Its flags are those of the remainder's sign fix:
// x = the remainder, or its complement for a negative dividend, then SUB of
// the divisor from x when dividend and divisor have the same sign, else
// ADD. AAM divides AL by imm, its flags those of AL plus 0; AAM 0 raises #DE
// with the divide's.
module fes_i386_muldiv (
    input  logic                 clk,
    input  logic                 go,
    input  fes_i386_pkg::uop_t   u,
    input  logic [31:0]          ar,      // EX's src1 before its bypass (fes_i386_ex)
    input  logic                 byp_a,   // ... src1 is WB's result
    input  logic [31:0]          yr,      // EX's y before its bypass
    input  logic [31:0]          wb,
    input  logic [31:0]          ld,
    input  logic [4:0]           yw,      // yw32, yw16, yw_lo, yw_hi, yl
    input  logic                 ld_en,
    output logic                 last,
    output logic [31:0]          result,  // in the last cycle, from a register
    output logic                 de,      // ... #DE, likewise
    output logic [11:0]          flags,   // from the cycle after: CF, PF, AF, ZF, SF and OF in their EFLAGS places
    output logic [31:0]          hi       // the kept high half
);
    localparam int CF = 0, PF = 2, AF = 4, ZF = 6, SF = 7, OF = 11;

    // ---- The uop -------------------------------------------------------------
    // E0 decodes the uop (u_*), for the op's last cycle and the operands it
    // registers, and copies the decode; the rest works from the copies.
    logic [2:0]  cls;
    logic [4:0]  op;
    logic        u_mpath, u_imul3, u_aad, u_idiv, u_aam, u_n8, u_n16;
    logic [5:0]  u_n;
    logic [31:0] u_mask;
    assign cls = 3'(u.cls);
    assign op = u.op;
    assign u_mpath = cls == 3'd2;
    assign u_imul3 = u_mpath && op == 5'd6;
    assign u_aad = u_mpath && op == 5'd3;
    assign u_idiv = cls == 3'd3 && op == 5'd1;
    assign u_aam = cls == 3'd3 && op == 5'd2;
    assign u_n8 = u.size == fes_i386_pkg::SIZE_8;
    assign u_n16 = u.size == fes_i386_pkg::SIZE_16;
    assign u_n = u_n8 ? 6'd8 : u_n16 ? 6'd16 : 6'd32;
    assign u_mask = u_n8 ? 32'h0000_00ff : u_n16 ? 32'h0000_ffff : 32'hffff_ffff;
    logic        imul, imul3, aad, idiv, aam, mpath, n8, n16;

    logic [31:0] mask, top_bit;
    logic [5:0]  n;
    assign mask = n8 ? 32'h0000_00ff : n16 ? 32'h0000_ffff : 32'hffff_ffff;
    assign top_bit = n8 ? 32'h0000_0080 : n16 ? 32'h0000_8000 : 32'h8000_0000;
    assign n = n8 ? 6'd8 : n16 ? 6'd16 : 6'd32;

    // ADD (sub clear) or SUB of b from a at the operand size (mask, its top
    // bit t): the result, then the flags x86 gives it.
    function automatic logic [43:0] addsub(input logic [31:0] a, input logic [31:0] b, input logic sub,
                                           input logic [31:0] m, input logic [31:0] t);
        logic [32:0] s;
        logic [31:0] r, bb;
        logic [11:0] f;
        bb = (sub ? ~b : b) & m;
        s = {1'b0, a & m} + {1'b0, bb} + {32'd0, sub};
        r = s[31:0] & m;
        f = '0;
        f[CF] = |(s & {t, 1'b0}) ^ sub;  // the carry out of the size
        f[PF] = ~^r[7:0];
        f[AF] = s[4] ^ a[4] ^ bb[4] ^ sub;
        f[ZF] = r == 32'd0;
        f[SF] = |(r & t);
        f[OF] = |((~(a ^ bb) & (a ^ r)) & t);
        return {r, f};
    endfunction

    // ---- State ----------------------------------------------------------------
    // st counts the op's cycles from E0; stop is its last.
    logic [5:0]  st, stop;
    assign last = go && st != 6'd0 && st == stop;
    always_ff @(posedge clk) begin
        if (!go || last) st <= 6'd0;
        else st <= st + 6'd1;
    end

    logic [31:0] res_q, hi_q, ld_hi;
    logic [11:0] fl_q;
    always_ff @(posedge clk) if (ld_en) ld_hi <= byp_a ? wb : ar;

    // E0: src1 (af) and y (yb) bypassed as EX makes them; registered with
    // the decode, src1 (af_q, the merge's base), the multiply's multiplicand
    // and multiplier at the size (ma_q, mq) and the divisor (y_q).
    logic [31:0] af, yb, ys, af_q, ma_q, mq, y_q;
    logic        sgn;
    assign af = byp_a ? wb : ar;
    assign ys = yw[0] ? ld : wb;
    assign yb = yr ^ {{16{yw[4]}} & ys[31:16], {8{yw[3]}} & ys[15:8],
                      {8{yw[2]}} & ys[7:0] | {8{yw[1]}} & ys[15:8]};
    always_ff @(posedge clk) if (go && st == 6'd0) begin
        imul <= u_mpath && op == 5'd1;
        imul3 <= u_imul3;
        aad <= u_aad;
        idiv <= u_idiv;
        aam <= u_aam;
        mpath <= u_mpath;
        n8 <= u_n8;
        n16 <= u_n16;
        af_q <= af;
        ma_q <= (u_imul3 ? yb : u_aad ? {24'd0, af[15:8]} : af) & u_mask;
        mq <= (u_imul3 ? u.imm : u_aad ? {24'd0, u.imm[7:0]} : yb) & u_mask;
        y_q <= u_aam ? {24'd0, u.imm[7:0]} : yb & u_mask;
    end
    assign sgn = imul || imul3;

    // ---- Multiply ---------------------------------------------------------------
    logic [32:0] a33, b33;
    logic        mq_neg, ma_neg;
    assign mq_neg = sgn && |(mq & top_bit);
    assign ma_neg = sgn && |(ma_q & top_bit);
    assign a33 = {ma_neg, ma_q | (ma_neg ? ~mask : 32'd0)};
    assign b33 = {mq_neg, mq | (mq_neg ? ~mask : 32'd0)};
    logic signed [17:0] al, ah, bl, bh;
    assign al = {2'b00, a33[15:0]};
    assign ah = {a33[32], a33[32:16]};
    assign bl = {2'b00, b33[15:0]};
    assign bh = {b33[32], b33[32:16]};
    logic signed [35:0] p_ll, p_lh, p_hl, p_hh;
    always_ff @(posedge clk) if (go && st == 6'd1) begin
        p_ll <= al * bl;
        p_lh <= al * bh;
        p_hl <= ah * bl;
        p_hh <= ah * bh;
    end

    // The multiplier's steps (k), from mq while E2 sums: the shift
    // that gives W (k - 1) and the step's addend: none, +a or -a, and which
    // carry AF is (c4: of the low nibbles' sum; else W's below a's). For a
    // negative multiplier the step's bit is clear when k is bits(~m), else
    // set (k from the trailing zeros, or n).
    function automatic logic [5:0] bits(input logic [31:0] v);
        bits = 6'd0;
        for (int i = 0; i < 32; i++) if (v[i]) bits = 6'(i + 1);
    endfunction
    function automatic logic [5:0] zeros(input logic [31:0] v, input logic [5:0] w);
        zeros = w;
        for (int i = 31; i >= 0; i--) if (v[i]) zeros = 6'(i);
    endfunction
    logic [5:0] b_pos, b_inv, k_tz;
    logic       inv_wins;
    assign b_pos = bits(mq);
    assign b_inv = bits(~mq & mask);
    assign k_tz = zeros(mq, n) + 6'd4;
    assign inv_wins = b_inv >= k_tz;
    logic [4:0] sh;
    logic       w_zero, c4_sel;
    logic [1:0] add_sel;  // 0: W, 1: W + a, 2: W - a
    always_ff @(posedge clk) if (go && st == 6'd2) begin
        if (aad) begin
            sh <= 5'd0;
            w_zero <= 1'b0;
            add_sel <= 2'd1;
            c4_sel <= 1'b1;
        end else if (!mq_neg) begin
            sh <= b_pos < 6'd3 ? 5'd2 : 5'(b_pos - 6'd1);
            w_zero <= 1'b0;
            add_sel <= b_pos < 6'd3 ? 2'd1 : 2'd0;
            c4_sel <= b_pos < 6'd3;
        end else begin
            sh <= inv_wins ? 5'(b_inv - 6'd1) : k_tz > n ? 5'(n - 6'd1) : 5'(k_tz - 6'd1);
            w_zero <= mq == top_bit;
            add_sel <= mq == top_bit || !inv_wins ? 2'd2 : 2'd0;
            c4_sel <= mq != top_bit && inv_wins;
        end
    end
    // E2: the products' sum, as one 3:2 row then one carry chain: P is p_ll +
    // (p_lh + p_hl) << 16 + p_hh << 32, and p_ll fits its 32 bits.
    logic [63:0] p, p_x, p_y, p_z, p_cy;
    assign p_x = {p_hh[31:0], p_ll[31:0]};
    assign p_y = {{12{p_lh[35]}}, p_lh, 16'd0};
    assign p_z = {{12{p_hl[35]}}, p_hl, 16'd0};
    assign p_cy = (p_x & p_y | p_x & p_z | p_y & p_z) << 1;
    always_ff @(posedge clk) if (go && st == 6'd2) p <= (p_x ^ p_y ^ p_z) + p_cy;
    // E3: W and CF/OF (the result halves into res_q and hi_q, below); E4: the
    // step's flags.
    logic [31:0] w, wa;
    logic        m_over;
    logic [63:0] p_sh;
    assign p_sh = p >> sh;
    always_ff @(posedge clk) if (go && st == 6'd3) begin
        w <= w_zero ? 32'd0 : p_sh[31:0] & mask;
        wa <= aad ? {24'd0, af_q[7:0]} : ma_q;
        if (sgn) m_over <= n8 ? !(p[15:7] == 9'd0 || p[15:7] == '1) : n16 ? !(p[31:15] == 17'd0 || p[31:15] == '1)
                                                                       : !(p[63:31] == 33'd0 || p[63:31] == '1);
        else m_over <= n8 ? p[15:8] != 8'd0 : n16 ? p[31:16] != 16'd0 : p[63:32] != 32'd0;
    end
    logic [11:0] w_add;
    logic [31:0] w_unused;
    logic        w_c4, w_lt4;
    assign {w_unused, w_add} = addsub(w, add_sel == 2'd0 ? 32'd0 : wa, add_sel == 2'd2, mask, top_bit);
    assign w_c4 = 5'(w[3:0]) + 5'(wa[3:0]) > 5'd15;
    assign w_lt4 = w[3:0] < wa[3:0];

    // ---- Divide -----------------------------------------------------------------
    // R: the partial remainder at the size; l: the dividend's low half,
    // left-aligned, into which the quotient bits shift; dm: the divisor (its
    // magnitude for IDIV, from E2).
    logic [31:0] r, l, dm, dm1;
    logic        dn, mn, over;
    logic [31:0] d_hi, d_lo;
    assign d_hi = aam ? 32'd0 : n8 ? {24'd0, af_q[15:8]} : ld_hi & mask;
    assign d_lo = aam || n8 ? {24'd0, af_q[7:0]} : af_q & mask;
    logic        st_div;  // a divide step this cycle
    logic [31:0] rs, diff;
    logic [32:0] trial;
    logic        top, big;
    logic [11:0] fl_trial;
    assign top = |(r & top_bit);
    assign rs = (r << 1 | {31'd0, l[31]}) & mask;
    assign trial = {1'b0, rs} - {1'b0, dm};
    assign diff = trial[31:0] & mask;
    assign big = idiv ? !trial[32] : top || !trial[32];
    // IDIV's fixes after the steps (Q1, then Q2): the one more (inc), then
    // the signs, of the quotient (q2_q, negated when the signs differ) and
    // the remainder (q2_r, negated for a negative dividend, which is the
    // complement of the remainder before its one more).
    logic [31:0] q1_x, q, q2_q, q2_r;
    logic        q1_over, q1_eq, inc, neg;
    logic [32:0] q_fix;  // the quotient with the one more, unwrapped
    assign q = l & mask;
    assign q1_eq = r == dm1;
    assign inc = dn && q1_eq;
    assign neg = dn != mn;
    assign q_fix = {1'b0, q} + {32'd0, inc};
    assign q2_q = neg ? (~q + {31'd0, !inc}) & mask : q_fix[31:0];
    assign q2_r = dn ? (q1_eq ? 32'd0 : ~r & mask) : r;
    // A step's partial remainder and quotient after it.
    logic [31:0] r_nx, q_nx;
    assign r_nx = big ? diff : rs;
    assign q_nx = {l[30:0], big} & mask;
    always_ff @(posedge clk) begin
        if (go && st == 6'd1 && !mpath) begin
            dn <= idiv && |(d_hi & top_bit);
            if (idiv && |(d_hi & top_bit)) begin
                r <= ~d_hi & mask;
                l <= (~d_lo & mask) << (6'd32 - n);
            end else begin
                r <= d_hi;
                l <= d_lo << (6'd32 - n);
            end
            dm <= y_q;
            over <= 1'b0;
        end else if (go && st == 6'd2 && idiv) begin
            mn <= |(y_q & top_bit);
            dm <= |(y_q & top_bit) ? -y_q & mask : y_q;
            dm1 <= ((|(y_q & top_bit) ? -y_q : y_q) - 32'd1) & mask;
        end else if (go && st == 6'd2 && !mpath) begin
            // DIV's and AAM's first compare, kept when not below.
            mn <= 1'b0;
            if (r >= dm) begin
                over <= 1'b1;
                r <= (r - dm) & mask;
            end
        end else if (st_div) begin
            r <= r_nx;
            l <= {l[30:0], big};
            fl_trial[CF] <= trial[32];
            fl_trial[PF] <= ~^diff[7:0];
            fl_trial[AF] <= rs[3:0] < dm[3:0];
            fl_trial[ZF] <= diff == 32'd0;
            fl_trial[SF] <= |(diff & top_bit);
            fl_trial[OF] <= |((rs ^ dm) & (rs ^ diff) & top_bit);
        end
        if (go && st == 6'd3 + n && idiv) begin
            q1_x <= (dn ? ~r : r) & mask;
            q1_over <= dm == 32'd0 || (neg ? q_fix > {1'b0, top_bit} : q_fix >= {1'b0, top_bit});
        end
    end
    // stop: E4 for a multiply; after E2 and the steps, one cycle more for
    // DIV and AAM, two for IDIV.
    always_ff @(posedge clk) begin
        if (go && st == 6'd0) stop <= u_mpath ? 6'd4 : u_idiv ? 6'd4 + u_n : 6'd3 + u_n;
        else if (go && st == 6'd2 && !mpath && !idiv && r >= dm) stop <= 6'd2 + n;
    end
    assign st_div = go && !mpath && st >= 6'd3 && (idiv ? st < 6'd3 + n : st < stop);

    // ---- Outputs ------------------------------------------------------------------
    // The result, merged into the destination's old value, and the kept
    // half, registered by the cycle before the last: the multiply's at E3,
    // AAD's sum of AL and AH x imm with them; a divide's at each step, so
    // its last step's stays; IDIV's at Q1.
    always_ff @(posedge clk) begin
        if (go && st == 6'd3 && mpath) begin
            res_q <= aad ? {af_q[31:16], 8'd0, p[7:0] + af_q[7:0]} : n8 || n16 ? {af_q[31:16], p[15:0]} : p[31:0];
            hi_q <= n16 ? {16'd0, p[31:16]} : p[63:32];
        end else if (st_div) begin
            res_q <= aam ? {af_q[31:16], q_nx[7:0], r_nx[7:0]} : n8 ? {af_q[31:16], r_nx[7:0], q_nx[7:0]}
                   : n16 ? {af_q[31:16], q_nx[15:0]} : q_nx;
            hi_q <= r_nx;
        end else if (go && st == 6'd3 + n && idiv) begin
            res_q <= n8 ? {af_q[31:16], q2_r[7:0], q2_q[7:0]} : n16 ? {af_q[31:16], q2_q[15:0]} : q2_q;
            hi_q <= q2_r;
        end
    end
    assign result = res_q;
    assign de = idiv ? q1_over : over;
    logic [11:0] q2_fl;
    logic [31:0] q2_unused;
    assign {q2_unused, q2_fl} = addsub(q1_x, y_q, dn == mn, mask, top_bit);
    always_ff @(posedge clk) if (last) begin
        if (mpath) begin
            fl_q <= w_add;
            fl_q[AF] <= aad ? w_add[AF] : c4_sel ? w_c4 : w_lt4;
            if (!aad) begin
                fl_q[CF] <= m_over;
                fl_q[OF] <= m_over;
            end
        end else if (idiv) begin
            fl_q <= q2_fl;
        end else begin
            fl_q <= fl_trial;
            if (aam && !over) begin
                fl_q <= '0;
                fl_q[PF] <= ~^r[7:0];
                fl_q[ZF] <= r[7:0] == 8'd0;
                fl_q[SF] <= r[7];
            end
        end
    end
    assign flags = fl_q;
    assign hi = hi_q;
endmodule
