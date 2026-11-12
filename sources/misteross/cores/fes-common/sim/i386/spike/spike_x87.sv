// SPDX-License-Identifier: MIT
// x87 fit spike (#1768 slice 1): the lean 387 datapath the x87 plan
// (docs/validation/2026-11-12-i386-x87-plan) describes, built to be fitted,
// not run. Nothing here ships in a core. scripts/quartus_x87_spike.py fits
// it alone (spike_x87, inside the spike envelope) and inside pc386_system
// (x87s_port, on the CPU's I/O bus at ports F8h-FFh); the record is
// docs/validation/2026-11-12-i386-x87-fit-spike.
//
// The structure is built in full so the area is honest: the eight-register
// stack in M10K with its tags and FXCH remap table, the control, status and
// tag words, every precision and rounding mode with guard and sticky, the
// denormalising shift, the exception flags and their masks, the load and
// store conversions, compare, the multiplier, and the slow path. The
// microcode, dispatch and constant ROMs hold placeholder words (a hash of
// the address) of the real widths and depths, so their M10K use and the
// muxes their fields steer are real, but the unit computes nothing useful.
//
// Parameters, which the variants in the record set:
//   MUL      0: 64x64 product from nine 22x22 DSP multiplies, four clocks;
//            1: three DSP multiplies, three passes; 2: one, nine passes.
//   SHIFT    0: alignment and normalising shifts as multiplies by 2^n on
//            the multiplier; 1: LUT barrel shifters.
//   DIVSQRT  0: divide and square root on the HPS (the mailbox);
//            1: Goldschmidt/Newton in microcode on the multiplier, with
//            reciprocal and reciprocal-square-root seeds in M10K;
//            2: a radix-2 digit-recurrence unit, one bit a clock.
//   TRANS    0: transcendentals, FPREM, FBLD and FBSTP on the HPS, a
//            256-word microcode ROM and 8 constants;
//            1: in microcode, a 1,024-word ROM and 256 constants.
// The mailbox to the HPS exists when DIVSQRT or TRANS is 0.

// The 64x64 mantissa multiply. done pulses with p.
module x87s_mul #(
    parameter int MODE = 0
) (
    input  logic         clk,
    input  logic         reset,
    input  logic         start,
    input  logic [63:0]  a,
    input  logic [63:0]  b,
    output logic         done,
    output logic [127:0] p
);
    // Operand chunks: 22, 22 and 20 bits, each within a 27x27 DSP block.
    function automatic logic [21:0] chunk(input logic [63:0] v, input logic [1:0] i);
        chunk = i == 2'd0 ? v[21:0] : i == 2'd1 ? v[43:22] : {2'b0, v[63:44]};
    endfunction

    if (MODE == 0) begin : g_full
        logic [63:0] ar, br;
        logic [43:0] p00, p01, p02, p10, p11, p12, p20, p21, p22;
        logic [87:0] r0, r1, r2;
        logic [3:0]  v;
        always_ff @(posedge clk) begin
            v <= reset ? 4'd0 : {v[2:0], start};
            ar <= a;
            br <= b;
            p00 <= ar[21:0] * br[21:0];
            p01 <= ar[21:0] * br[43:22];
            p02 <= ar[21:0] * br[63:44];
            p10 <= ar[43:22] * br[21:0];
            p11 <= ar[43:22] * br[43:22];
            p12 <= ar[43:22] * br[63:44];
            p20 <= ar[63:44] * br[21:0];
            p21 <= ar[63:44] * br[43:22];
            p22 <= ar[63:44] * br[63:44];
            r0 <= {44'd0, p00} + {22'd0, p01, 22'd0} + {p02, 44'd0};
            r1 <= {44'd0, p10} + {22'd0, p11, 22'd0} + {p12, 44'd0};
            r2 <= {44'd0, p20} + {22'd0, p21, 22'd0} + {p22, 44'd0};
            p <= {40'd0, r0} + {18'd0, r1, 22'd0} + {r2[83:0], 44'd0};
        end
        assign done = v[3];
    end else if (MODE == 1) begin : g_rows
        // a times one chunk of b a pass, on three DSP multiplies.
        logic [63:0]  ar, br;
        logic [1:0]   j, jq;
        logic         busy, vq, lastq;
        logic [43:0]  q0, q1, q2;
        logic [127:0] acc;
        logic [87:0]  row;
        assign row = {44'd0, q0} + {22'd0, q1, 22'd0} + {q2, 44'd0};
        always_ff @(posedge clk) begin
            if (reset) begin
                busy <= 1'b0;
                vq <= 1'b0;
                done <= 1'b0;
            end else begin
                if (start) begin
                    ar <= a;
                    br <= b;
                    j <= 2'd0;
                    busy <= 1'b1;
                end else if (busy) begin
                    j <= j + 2'd1;
                    busy <= j != 2'd2;
                end
                q0 <= ar[21:0] * chunk(br, j);
                q1 <= ar[43:22] * chunk(br, j);
                q2 <= {2'b0, ar[63:44]} * chunk(br, j);
                vq <= busy;
                jq <= j;
                lastq <= busy && j == 2'd2;
                if (start)
                    acc <= 128'd0;
                else if (vq)
                    acc <= acc + (jq == 2'd0 ? {40'd0, row} : jq == 2'd1 ? {18'd0, row, 22'd0} : {row[83:0], 44'd0});
                done <= vq && lastq;
            end
        end
        assign p = acc;
    end else begin : g_one
        // One chunk by one chunk a pass, on one DSP multiply.
        logic [63:0]  ar, br;
        logic [1:0]   i, j;
        logic [2:0]   kq;
        logic         busy, vq, lastq;
        logic [43:0]  q;
        logic [127:0] acc, shifted;
        always_comb
            case (kq)
                3'd0: shifted = {84'd0, q};
                3'd1: shifted = {62'd0, q, 22'd0};
                3'd2: shifted = {40'd0, q, 44'd0};
                3'd3: shifted = {18'd0, q, 66'd0};
                default: shifted = {q[39:0], 88'd0};
            endcase
        always_ff @(posedge clk) begin
            if (reset) begin
                busy <= 1'b0;
                vq <= 1'b0;
                done <= 1'b0;
            end else begin
                if (start) begin
                    ar <= a;
                    br <= b;
                    i <= 2'd0;
                    j <= 2'd0;
                    busy <= 1'b1;
                end else if (busy) begin
                    if (j == 2'd2) begin
                        j <= 2'd0;
                        i <= i + 2'd1;
                    end else begin
                        j <= j + 2'd1;
                    end
                    busy <= !(i == 2'd2 && j == 2'd2);
                end
                q <= chunk(ar, i) * chunk(br, j);
                kq <= {1'b0, i} + {1'b0, j};
                vq <= busy;
                lastq <= busy && i == 2'd2 && j == 2'd2;
                if (start)
                    acc <= 128'd0;
                else if (vq)
                    acc <= acc + shifted;
                done <= vq && lastq;
            end
        end
        assign p = acc;
    end
endmodule

// Round an unpacked value (sign, 17-bit biased exponent, 68-bit significand
// with the integer bit at 67) to 24, 53 or 64 bits under a rounding control.
module x87s_round (
    input  logic        s,
    input  logic [16:0] e,
    input  logic [67:0] m,
    input  logic [1:0]  prec,   // the control word's PC: 0 24 bits, 2 53, 3 64
    input  logic [1:0]  rc,     // 0 nearest, 1 down, 2 up, 3 toward zero
    output logic [16:0] e_out,
    output logic [67:0] m_out,
    output logic        inexact,
    output logic        rounded_up,
    output logic        overflow,
    output logic        tiny
);
    logic [67:0] keep, half, below, unit;
    logic        g, st, lsb, inc;
    logic [68:0] sum;
    always_comb begin
        case (prec)
            2'd0: begin unit = 68'd1 << 44; end
            2'd2: begin unit = 68'd1 << 15; end
            default: begin unit = 68'd1 << 4; end
        endcase
        half = unit >> 1;
        below = unit - 68'd1;
        keep = m & ~below;
        lsb = |(m & unit);
        g = |(m & half);
        st = |(m & (below >> 1));
        case (rc)
            2'd0: inc = g && (st || lsb);
            2'd1: inc = s && (g || st);
            2'd2: inc = !s && (g || st);
            default: inc = 1'b0;
        endcase
        sum = {1'b0, keep} + (inc ? {1'b0, unit} : 69'd0);
        m_out = sum[68] ? {1'b1, sum[67:1]} & ~below : sum[67:0];
        e_out = e + {16'd0, sum[68]};
        inexact = g || st;
        rounded_up = inc;
        overflow = e_out >= 17'h07fff && !e_out[16];
        tiny = e[16] || e == 17'd0;
    end
endmodule

// A radix-2 divide and square root, one quotient bit a clock, stopping at
// the precision's width plus guard and sticky.
module x87s_divsqrt (
    input  logic        clk,
    input  logic        reset,
    input  logic        start,
    input  logic        sqrt,
    input  logic [1:0]  prec,
    input  logic [67:0] a,     // dividend, or the radicand (pre-shifted for an odd exponent)
    input  logic [67:0] b,     // divisor
    output logic        done,
    output logic [67:0] q,
    output logic        sticky
);
    logic [69:0] r, trial, diff;
    logic [67:0] d;
    logic [6:0]  n;
    logic        busy;
    always_comb begin
        trial = sqrt ? {q, 2'b01} : {2'b0, d};
        diff = {r[68:0], 1'b0} - trial;
    end
    always_ff @(posedge clk) begin
        if (reset) begin
            busy <= 1'b0;
            done <= 1'b0;
        end else begin
            done <= 1'b0;
            if (start) begin
                r <= {2'b0, a};
                d <= b;
                q <= 68'd0;
                n <= prec == 2'd0 ? 7'd27 : prec == 2'd2 ? 7'd56 : 7'd67;
                busy <= 1'b1;
            end else if (busy) begin
                q <= {q[66:0], !diff[69]};
                r <= diff[69] ? {r[68:0], 1'b0} : diff;
                n <= n - 7'd1;
                if (n == 7'd0) begin
                    busy <= 1'b0;
                    done <= 1'b1;
                end
            end
        end
    end
    assign sticky = |r;
endmodule

module x87s_unit #(
    parameter int MUL = 0,
    parameter int SHIFT = 0,
    parameter int DIVSQRT = 1,
    parameter int TRANS = 1
) (
    input  logic        clk,
    input  logic        reset,
    // From the CPU: an ESC instruction's 387 opcode (ESC's low three bits
    // and the ModRM byte), its memory operand's format and value.
    input  logic        cmd_valid,
    input  logic [10:0] cmd_op,
    input  logic [2:0]  cmd_fmt,   // 0 f32, 1 f64, 2 f80, 3 i16, 4 i32, 5 i64, 6 BCD
    input  logic [79:0] cmd_data,
    output logic        busy,
    output logic        st_valid,
    output logic [79:0] st_data,
    output logic [15:0] sw,
    output logic [15:0] cw,
    output logic [15:0] tw,
    output logic        ferr,
    // The mailbox to the HPS for the operations it takes.
    output logic        mb_req,
    output logic [7:0]  mb_op,
    output logic [79:0] mb_a,
    output logic [79:0] mb_b,
    input  logic        mb_ack,
    input  logic [79:0] mb_result,
    input  logic [7:0]  mb_flags
);
    localparam bit MAILBOX = DIVSQRT == 0 || TRANS == 0;
    localparam int UC_BITS = TRANS != 0 ? 10 : 8;
    localparam int K_BITS = TRANS != 0 ? 8 : 3;
    localparam logic [16:0] BIAS = 17'd16383;

    // ---- Placeholder ROM words: a hash of the address. ----
    function automatic logic [63:0] hash(input int i, input logic [63:0] salt);
        logic [63:0] x;
        x = (64'(i) + 64'd1) * 64'h9e3779b97f4a7c15 ^ salt;
        x = x ^ (x >> 29);
        x = x * 64'hbf58476d1ce4e5b9;
        hash = x ^ (x >> 32);
    endfunction

    // ---- Unpacked values: sign, 17-bit exponent, 68-bit significand. ----
    function automatic logic [85:0] unpack(input logic [79:0] v);
        unpack = {v[79], 2'b0, v[78:64] == 15'd0 ? 15'd1 : v[78:64], v[63:0], 4'b0};
    endfunction
    function automatic logic [79:0] pack(input logic [85:0] u);
        pack = {u[85], u[82:68], u[67:4]};
    endfunction
    // {unsupported, signalling NaN, NaN, infinity, denormal, zero}
    function automatic logic [5:0] classify(input logic [79:0] v);
        logic max, zexp, j, frac;
        max = &v[78:64];
        zexp = v[78:64] == 15'd0;
        j = v[63];
        frac = |v[62:0];
        classify = {!zexp && !j, max && j && frac && !v[62], max && j && frac, max && j && !frac,
                    zexp && (j || frac), zexp && !j && !frac};
    endfunction
    function automatic logic [6:0] lzc(input logic [67:0] m);
        lzc = 7'd68;
        for (int i = 0; i < 68; i++)
            if (m[i]) lzc = 7'(67 - i);
    endfunction

    // ---- Control, status and tag state. ----
    logic [2:0]  top;
    logic [7:0][2:0] phys;   // FXCH renames: ST(i)'s register is phys[(top + i) & 7]
    logic [7:0][1:0] tag;    // 0 valid, 1 zero, 2 special, 3 empty
    logic [5:0]  flags;      // PE UE OE ZE DE IE
    logic        sf, c0, c1, c2, c3;
    logic [10:0] fop;
    assign sw = {busy, c3, top, c2, c1, c0, |(flags & ~cw[5:0]), sf, flags[5:0]};
    assign tw = tag;
    assign ferr = |(flags & ~cw[5:0]);

    // ---- The register stack: eight 80-bit registers, two read ports. ----
    (* ramstyle = "M10K" *) logic [79:0] regs [0:7];
    logic [2:0]  ra_idx, rb_idx, wr_idx;
    logic [79:0] ra, rb, wdata;
    logic        we;
    always_ff @(posedge clk) begin
        if (we) regs[phys[top + wr_idx]] <= wdata;
        ra <= regs[phys[top + ra_idx]];
        rb <= regs[phys[top + rb_idx]];
    end

    // ---- Microcode: dispatch, sequencer and the constant ROM. ----
    // A word: [9:0] target, [13:10] condition, [14] branch, [15] invert,
    // [16] end, [19:17] source A, [22:20] source B, [26:23] operation,
    // [28:27] destination, [29] write ST, [30] push, [31] pop, [32] pop
    // twice, [34:33] precision, [36:35] rounding, [37] merge flags, [38]
    // store, [40:39] condition codes, [48:41] constant or immediate, [50:49]
    // sign, [52:51] shift amount, [53] load counter, [54] port A is ST(1),
    // [55] port B is ST(i), [58:56] write index, [63:59] flag set.
    (* ramstyle = "M10K" *) logic [UC_BITS-1:0] dispatch [0:2047];
    (* ramstyle = "M10K" *) logic [63:0] ucode [0:(1 << UC_BITS) - 1];
    (* ramstyle = "M10K" *) logic [79:0] konst [0:(1 << K_BITS) - 1];
    initial for (int i = 0; i < 2048; i++) dispatch[i] = UC_BITS'(hash(i, 64'h5a17));
    initial for (int i = 0; i < (1 << UC_BITS); i++) ucode[i] = hash(i, 64'hc0de);
    initial for (int i = 0; i < (1 << K_BITS); i++) konst[i] = {16'(hash(i, 64'h3141)), hash(i, 64'h2718)};

    logic [UC_BITS-1:0] upc, next_upc, entry;
    logic [63:0] w;
    logic [79:0] kq;
    logic        running, taken, cond;
    logic [5:0]  counter;
    always_ff @(posedge clk) begin
        entry <= dispatch[cmd_op];
        w <= ucode[next_upc];
        kq <= konst[w[41 +: K_BITS]];
    end

    // ---- Operands. ----
    logic [85:0] x, y, z, ua, ub, ld, ld_c, ext;
    logic [5:0]  ca, cb;
    logic [2:0]  fmt_q;
    logic [79:0] data_q;
    always_comb begin
        // The load conversions from the memory formats.
        logic [7:0]  e8;
        logic [10:0] e11;
        logic [63:0] iv;
        e8 = data_q[30:23];
        e11 = data_q[62:52];
        iv = fmt_q == 3'd3 ? {{48{data_q[15]}}, data_q[15:0]}
           : fmt_q == 3'd4 ? {{32{data_q[31]}}, data_q[31:0]} : data_q[63:0];
        case (fmt_q)
            3'd0: ld_c = {data_q[31], &e8 ? 17'h07fff : e8 == 8'd0 ? 17'd16257 : {9'd0, e8} + 17'd16256,
                        e8 != 8'd0, data_q[22:0], 44'd0};
            3'd1: ld_c = {data_q[63], &e11 ? 17'h07fff : e11 == 11'd0 ? 17'd15361 : {6'd0, e11} + 17'd15360,
                        e11 != 11'd0, data_q[51:0], 15'd0};
            3'd3, 3'd4, 3'd5: ld_c = {iv[63], BIAS + 17'd63, iv[63] ? -iv : iv, 4'd0};
            default: ld_c = unpack(data_q);
        endcase
    end
    function automatic logic [85:0] source(input logic [2:0] sel);
        case (sel)
            3'd0: source = unpack(ra);
            3'd1: source = unpack(rb);
            3'd2: source = x;
            3'd3: source = y;
            3'd4: source = z;
            3'd5: source = unpack(kq);
            3'd6: source = ld;
            default: source = ext;
        endcase
    endfunction
    // The operands and the word that uses them, a clock after the word
    // selects them: the datapath runs a stage behind the sequencer.
    logic [63:0] e;
    logic        e_v;
    always_ff @(posedge clk) begin
        ua <= source(w[19:17]);
        ub <= source(w[22:20]);
        e <= w;
        e_v <= running && !reset;
        ld <= ld_c;
    end
    assign ca = classify(pack(ua));
    assign cb = classify(pack(ub));

    // ---- The shifts: the exponent difference, aligning and normalising. ----
    logic [6:0]  k, lz;
    logic [16:0] kdiff;
    logic        swap, kbig;
    always_comb begin
        swap = ua[84:68] < ub[84:68];
        case (e[52:51])
            2'd0: kdiff = ua[84:68] - ub[84:68];          // align B to A
            2'd1: kdiff = BIAS + 17'd63 - ub[84:68];      // B to an integer
            2'd2: kdiff = 17'd1 - ub[84:68];              // denormalise B
            default: kdiff = {9'd0, e[48:41]};            // a fixed shift
        endcase
        kbig = kdiff[16] ? 1'b0 : kdiff > 17'd67;
        k = kdiff[16] ? 7'd0 : kbig ? 7'd68 : kdiff[6:0];
        lz = lzc(ua[67:0]);
    end

    // ---- The multiplier, shared by multiply, divide, square root and,
    // with SHIFT 0, the shifts. ----
    logic         mul_start, mul_done;
    logic [63:0]  mul_a, mul_b;
    logic [127:0] mul_p;
    logic [1:0]   mul_mode, mul_mode_q;    // 0 product, 1 shift right, 2 shift left
    logic [1:0]   mul_dst;
    logic [85:0]  mul_u;                   // the operand being shifted, or the product's sign and exponent
    logic [6:0]   mul_k;
    x87s_mul #(.MODE(MUL)) mul (.clk, .reset, .start(mul_start), .a(mul_a), .b(mul_b), .done(mul_done),
                                .p(mul_p));

    // ---- The operation. ----
    logic [85:0] res;
    logic [5:0]  rflags;
    logic        rc1, cmp_lt, cmp_eq, cmp_un;
    logic [1:0]  prec, rc;
    logic [16:0] re;
    logic [67:0] rm;
    logic        r_inexact, r_up, r_over, r_tiny;
    assign prec = e[34:33] == 2'd0 ? cw[9:8] : e[34:33] == 2'd1 ? 2'd0 : e[34:33] == 2'd2 ? 2'd2 : 2'd3;
    assign rc = e[36:35] == 2'd0 ? cw[11:10] : e[36:35] == 2'd1 ? 2'd0 : e[36:35] == 2'd2 ? 2'd3 : 2'd1;
    x87s_round rounder (.s(ua[85]), .e(ua[84:68]), .m(ua[67:0]), .prec, .rc, .e_out(re), .m_out(rm),
                        .inexact(r_inexact), .rounded_up(r_up), .overflow(r_over), .tiny(r_tiny));
    always_comb begin
        logic [68:0] sum;
        logic [67:0] mag_hi, mag_lo;
        logic        sub;
        logic [135:0] wide;
        res = ua;
        rflags = 6'd0;
        rc1 = c1;
        sub = ua[85] ^ ub[85] ^ (e[26:23] == 4'd3);
        mag_hi = ua[67:0] >= ub[67:0] ? ua[67:0] : ub[67:0];
        mag_lo = ua[67:0] >= ub[67:0] ? ub[67:0] : ua[67:0];
        sum = sub ? {1'b0, mag_hi} - {1'b0, mag_lo} : {1'b0, ua[67:0]} + {1'b0, ub[67:0]};
        wide = {ub[67:0], 68'd0} >> k;
        cmp_un = ca[3] || cb[3];
        cmp_eq = ca[0] && cb[0] || ua == ub;
        cmp_lt = ua[85] != ub[85] ? ua[85] && !(ca[0] && cb[0])
               : ua[85] ^ ({ua[84:68], ua[67:0]} < {ub[84:68], ub[67:0]});
        case (e[26:23])
            4'd2, 4'd3: begin  // add, subtract (B aligned to A by an earlier step)
                res[85] = sub && ua[67:0] < ub[67:0] ? ub[85] ^ (e[26:23] == 4'd3) : ua[85];
                res[84:68] = ua[84:68] + {16'd0, sum[68]};
                res[67:0] = sum[68] ? {sum[68:2], sum[1] | sum[0]} : sum[67:0];
            end
            4'd5: if (SHIFT != 0) begin  // align B by k, with sticky
                res = {ub[85], ua[84:68], wide[135:69], wide[68] | (|wide[67:0])};
            end
            4'd6: if (SHIFT != 0) begin  // normalise A
                res = {ua[85], ua[84:68] - {10'd0, lz}, ua[67:0] << lz};
            end
            4'd7: begin  // round
                res = {ua[85], re, rm};
                rflags = {r_inexact, r_tiny && r_inexact, r_over, 3'd0};
                rc1 = r_up;
            end
            4'd8: begin  // compare: invalid on a signalling NaN or an unsupported format
                rflags = {5'd0, ca[4] || cb[4] || ca[5] || cb[5]};
            end
            4'd9: res = {ub[85], 17'd0, lz, 61'd0};           // leading zeros, for FPREM's quotient bits
            4'd10: res = {ua[85] ^ ub[85], ua[84:68] - ub[84:68] + BIAS, ua[67:0]};  // the quotient's sign and exponent
            4'd11: res = {ua[85], ua[84:68] + {{1{ub[67]}}, ub[67:52]}, ua[67:0]};    // FSCALE
            4'd12: res = {ua[85], ua[84:68], ~ua[67:0] + 68'd1};                     // two's complement
            default: ;
        endcase
        case (e[50:49])
            2'd1: res[85] = !res[85];
            2'd2: res[85] = 1'b0;
            2'd3: res[85] = ua[85] ^ ub[85];
            default: ;
        endcase
    end

    // ---- Divide and square root, and the mailbox. ----
    logic        slow_start, slow_done;
    logic [85:0] slow_q;
    logic [79:0] seed_q;
    if (DIVSQRT == 1) begin : g_seed
        // Reciprocal and reciprocal-square-root seeds, 256 words each, for
        // Goldschmidt's first step; the iterations run in microcode.
        (* ramstyle = "M10K" *) logic [9:0] recip [0:255];
        (* ramstyle = "M10K" *) logic [9:0] rsqrt [0:255];
        initial for (int i = 0; i < 256; i++) recip[i] = 10'(hash(i, 64'h1d1d));
        initial for (int i = 0; i < 256; i++) rsqrt[i] = 10'(hash(i, 64'h5a5a));
        logic [9:0] sr, sq;
        always_ff @(posedge clk) begin
            sr <= recip[ub[66:59]];
            sq <= rsqrt[{ub[68], ub[66:60]}];
        end
        assign seed_q = {1'b0, 15'(BIAS - 17'd1), 1'b1, e[24] ? sq : sr, 53'd0};
    end else begin : g_noseed
        assign seed_q = 80'd0;
    end
    if (DIVSQRT == 2) begin : g_r2
        logic        dq_done, dq_st;
        logic [67:0] dq;
        logic [85:0] dq_hold;
        x87s_divsqrt divsqrt (.clk, .reset, .start(slow_start && e[48:41] != 8'd0), .sqrt(e[41]), .prec,
                              .a(ua[67:0]), .b(ub[67:0]), .done(dq_done), .q(dq), .sticky(dq_st));
        always_ff @(posedge clk) if (slow_start) dq_hold <= res;
        assign slow_done = dq_done || mb_ack;
        assign slow_q = dq_done ? {dq_hold[85:68], dq[67:1], dq[0] | dq_st} : unpack(mb_result);
    end else begin : g_nor2
        assign slow_done = mb_ack;
        assign slow_q = unpack(mb_result);
    end
    if (MAILBOX) begin : g_mb
        always_ff @(posedge clk) begin
            if (reset) begin
                mb_req <= 1'b0;
            end else if (slow_start && !(DIVSQRT == 2 && e[48:41] != 8'd0)) begin
                mb_req <= 1'b1;
                mb_op <= e[48:41];
                mb_a <= pack(ua);
                mb_b <= pack(ub);
            end else if (mb_ack) begin
                mb_req <= 1'b0;
            end
        end
    end else begin : g_nomb
        assign mb_req = 1'b0;
        assign mb_op = 8'd0;
        assign mb_a = 80'd0;
        assign mb_b = 80'd0;
    end

    // ---- Conditions, and the next microcode address. ----
    logic slow_seen;
    always_comb begin
        case (w[13:10])
            4'd0: cond = 1'b1;
            4'd1: cond = !mul_start && mul_mode_q == 2'd3;
            4'd2: cond = ca[0];
            4'd3: cond = ca[2];
            4'd4: cond = ca[3];
            4'd5: cond = ca[1];
            4'd6: cond = cb[0];
            4'd7: cond = cb[2];
            4'd8: cond = cb[3];
            4'd9: cond = cb[5] || cb[4];
            4'd10: cond = swap;
            4'd11: cond = r_tiny;
            4'd12: cond = r_over;
            4'd13: cond = |(flags & ~cw[5:0]);
            4'd14: cond = slow_seen;
            default: cond = counter == 6'd0;
        endcase
        taken = w[14] && (cond ^ w[15]);
        next_upc = !running ? entry : taken ? w[UC_BITS-1:0] : upc + UC_BITS'(1);
    end

    // ---- The issue port and the fast operations. ----
    logic fast, go;
    always_comb begin
        // FLDCW, FNSTCW, FNSTSW (both forms), FNCLEX, FNINIT, FXCH, FFREE,
        // FINCSTP and FDECSTP do not enter the microcode.
        fast = cmd_op[10:8] == 3'd1 && cmd_op[7:6] != 2'd3 && (cmd_op[5:3] == 3'd5 || cmd_op[5:3] == 3'd7)
            || cmd_op[10:8] == 3'd5 && cmd_op[7:6] != 2'd3 && cmd_op[5:3] == 3'd7
            || cmd_op == 11'h7e0 || cmd_op == 11'h3e2 || cmd_op == 11'h3e3
            || cmd_op[10:3] == 8'h39 || cmd_op[10:3] == 8'h5c || cmd_op == 11'h1f6 || cmd_op == 11'h1f7;
        go = cmd_valid && !busy && !fast;
    end

    always_ff @(posedge clk) begin
        st_valid <= 1'b0;
        we <= 1'b0;
        mul_start <= 1'b0;
        slow_start <= 1'b0;
        if (reset) begin
            running <= 1'b0;
            busy <= 1'b0;
            top <= 3'd0;
            cw <= 16'h037f;
            flags <= 6'd0;
            {sf, c0, c1, c2, c3} <= 5'd0;
            tag <= '1;
            phys <= {3'd7, 3'd6, 3'd5, 3'd4, 3'd3, 3'd2, 3'd1, 3'd0};
            mul_mode_q <= 2'd0;
            slow_seen <= 1'b0;
            counter <= 6'd0;
        end else begin
            if (cmd_valid && !busy) begin
                fmt_q <= cmd_fmt;
                data_q <= cmd_data;
                fop <= cmd_op;
            end
            if (cmd_valid && !busy && fast) begin
                if (cmd_op[10:8] == 3'd1 && cmd_op[5:3] == 3'd5) cw <= cmd_data[15:0];
                if (cmd_op[10:8] == 3'd1 && cmd_op[5:3] == 3'd7) begin
                    st_valid <= 1'b1;
                    st_data <= {64'd0, cw};
                end
                if (cmd_op[10:8] == 3'd5 || cmd_op == 11'h7e0) begin
                    st_valid <= 1'b1;
                    st_data <= {64'd0, sw};
                end
                if (cmd_op == 11'h3e2) begin
                    flags <= 6'd0;
                    sf <= 1'b0;
                end
                if (cmd_op == 11'h3e3) begin
                    cw <= 16'h037f;
                    flags <= 6'd0;
                    {sf, c0, c1, c2, c3} <= 5'd0;
                    top <= 3'd0;
                    tag <= '1;
                end
                if (cmd_op[10:3] == 8'h39) begin  // FXCH: swap the names, not the data
                    phys[top] <= phys[top + cmd_op[2:0]];
                    phys[top + cmd_op[2:0]] <= phys[top];
                    if (tag[phys[top]] == 2'd3 || tag[phys[top + cmd_op[2:0]]] == 2'd3) begin
                        flags[0] <= 1'b1;
                        sf <= 1'b1;
                    end
                end
                if (cmd_op[10:3] == 8'h5c) tag[phys[top + cmd_op[2:0]]] <= 2'd3;
                if (cmd_op == 11'h1f7) top <= top + 3'd1;
                if (cmd_op == 11'h1f6) top <= top - 3'd1;
            end
            if (go) begin
                running <= 1'b1;
                busy <= 1'b1;
            end
            if (running) begin
                upc <= next_upc;
                // The register file's read and write addresses for the next word.
                ra_idx <= w[54] ? 3'd1 : 3'd0;
                rb_idx <= w[55] ? fop[2:0] : 3'd0;
                if (w[53]) counter <= w[46:41];
                else if (w[13:10] == 4'd15 && counter != 6'd0) counter <= counter - 6'd1;
                if (w[16]) begin
                    running <= 1'b0;
                    busy <= 1'b0;
                end
            end
            if (e_v) begin
                // The operation's result, or a multiply, shift or slow operation started.
                if (e[26:23] == 4'd4 || e[26:23] == 4'd5 && SHIFT == 0 || e[26:23] == 4'd6 && SHIFT == 0) begin
                    mul_start <= 1'b1;
                    mul_dst <= e[28:27];
                    mul_mode_q <= e[26:23] == 4'd4 ? 2'd0 : e[26:23] == 4'd5 ? 2'd1 : 2'd2;
                    mul_u <= e[26:23] == 4'd4 ? {ua[85] ^ ub[85], ua[84:68] + ub[84:68] - BIAS + 17'd1, 68'd0}
                           : e[26:23] == 4'd5 ? ub : ua;
                    mul_k <= e[26:23] == 4'd5 ? k : lz;
                    mul_a <= e[26:23] == 4'd4 ? ua[67:4] : e[26:23] == 4'd5 ? ub[67:4] : ua[67:4];
                    mul_b <= e[26:23] == 4'd4 ? ub[67:4] : e[26:23] == 4'd5 ? 64'd1 << (7'd63 - (k > 7'd63 ? 7'd63 : k))
                           : 64'd1 << (lz > 7'd63 ? 7'd63 : lz);
                end else if (e[26:23] == 4'd13 && (DIVSQRT != 1 || MAILBOX)) begin
                    slow_start <= 1'b1;
                    slow_seen <= 1'b0;
                end else if (e[26:23] == 4'd14 && DIVSQRT == 1) begin
                    case (e[28:27])
                        2'd0: x <= unpack(seed_q);
                        2'd1: y <= unpack(seed_q);
                        2'd2: z <= unpack(seed_q);
                        default: ;
                    endcase
                end else begin
                    case (e[28:27])
                        2'd0: x <= res;
                        2'd1: y <= res;
                        2'd2: z <= res;
                        default: ;
                    endcase
                end
                if (slow_done) begin
                    slow_seen <= 1'b1;
                    ext <= slow_q;
                    if (MAILBOX) flags <= flags | mb_flags[5:0];
                end
                // The register stack.
                if (e[29]) begin
                    we <= 1'b1;
                    wr_idx <= e[58:56];
                    wdata <= pack(x);
                    tag[phys[top + e[58:56]]] <= x[67:0] == 68'd0 ? 2'd1 : &x[82:68] || !x[67] ? 2'd2 : 2'd0;
                end
                if (e[30]) begin
                    top <= top - 3'd1;
                    if (tag[phys[top - 3'd1]] != 2'd3) begin
                        flags[0] <= 1'b1;
                        sf <= 1'b1;
                        c1 <= 1'b1;
                    end
                end else if (e[31] || e[32]) begin
                    tag[phys[top]] <= 2'd3;
                    if (e[32]) tag[phys[top + 3'd1]] <= 2'd3;
                    top <= top + (e[32] ? 3'd2 : 3'd1);
                end
                if (e[37]) flags <= flags | rflags | {1'b0, e[63:59]};
                case (e[40:39])
                    2'd1: {c3, c2, c0} <= cmp_un ? 3'b111 : {cmp_eq, 1'b0, cmp_lt};
                    2'd2: {c3, c2, c1, c0} <= {ca[0] || ca[1], !ca[0] && !ca[3], ua[85], ca[3] || ca[2]};
                    2'd3: {c0, c3, c1} <= {x[6], x[5], x[4]};
                    default: if (e[26:23] == 4'd7) c1 <= rc1;
                endcase
                // A store, converted to its memory format.
                if (e[38]) begin
                    st_valid <= 1'b1;
                    case (fmt_q)
                        3'd0: st_data <= {48'd0, x[85], x[75:68] + 8'd129, x[66:44]};
                        3'd1: st_data <= {16'd0, x[85], x[78:68] + 11'd1025, x[66:15]};
                        3'd3: st_data <= {64'd0, x[85] ? -x[19:4] : x[19:4]};
                        3'd4: st_data <= {48'd0, x[85] ? -x[35:4] : x[35:4]};
                        3'd5: st_data <= {16'd0, x[85] ? -x[67:4] : x[67:4]};
                        default: st_data <= pack(x);
                    endcase
                end
            end
            // A multiply's product, or a shift's result, lands in its destination.
            if (mul_done) begin
                logic [85:0] mr;
                case (mul_mode_q)
                    2'd0: mr = {mul_u[85:68], mul_p[127:61], |mul_p[60:0]};
                    2'd1: mr = {mul_u[85], mul_u[84:68] + {10'd0, mul_k}, mul_p[126:60], |mul_p[59:0]};
                    default: mr = {mul_u[85], mul_u[84:68] - {10'd0, mul_k}, mul_p[63:0] | {60'd0, mul_u[3:0]}, 4'd0};
                endcase
                case (mul_dst)
                    2'd0: x <= mr;
                    2'd1: y <= mr;
                    2'd2: z <= mr;
                    default: ;
                endcase
                mul_mode_q <= 2'd3;
            end
        end
    end
endmodule

// The unit on pc386's CPU I/O bus, for the in-machine fit: an operand
// written in 32-bit parts to F8h-FAh, a command to FCh, results read back
// from F8h-FCh. Both directions are registered. This stands in for the
// CPU's ESC path, which has no x87 port yet; the CPU-side issue, scoreboard
// and IRQ 13 latch the plan estimates at about 400 ALMs are not built. The
// mailbox loops back through a register in place of the HPS, which keeps
// its registers and the result path live; the HPS bridge is not built.
module x87s_port #(
    parameter int MUL = 0,
    parameter int SHIFT = 0,
    parameter int DIVSQRT = 1,
    parameter int TRANS = 1
) (
    input  logic        clk,
    input  logic        reset,
    input  logic        io_valid,
    input  logic        io_write,
    input  logic [15:0] io_port,
    input  logic [31:0] io_wdata,
    output logic        hit,       // the port is the unit's
    output logic [31:0] rdata
);
    logic        mb_req, mb_ack;
    logic [7:0]  mb_op, mb_flags;
    logic [79:0] mb_a, mb_b, mb_result;
    always_ff @(posedge clk) begin
        mb_ack <= mb_req && !mb_ack;
        mb_result <= mb_a ^ {mb_b[78:0], mb_b[79]};
        mb_flags <= mb_op;
    end
    logic        v_q, w_q;
    logic [2:0]  p_q;
    logic [31:0] d_q;
    logic [95:0] operand;
    logic        cmd_valid, busy, st_valid, ferr;
    logic [10:0] cmd_op;
    logic [2:0]  cmd_fmt;
    logic [79:0] st_data, result;
    logic [15:0] sw, cw, tw;
    assign hit = io_port[15:3] == 13'h001f;
    always_ff @(posedge clk) begin
        v_q <= io_valid && hit;
        w_q <= io_write;
        p_q <= io_port[2:0];
        d_q <= io_wdata;
        cmd_valid <= 1'b0;
        if (v_q && w_q) begin
            case (p_q)
                3'd0: operand[31:0] <= d_q;
                3'd1: operand[63:32] <= d_q;
                3'd2: operand[95:64] <= d_q;
                3'd4: begin
                    cmd_valid <= 1'b1;
                    cmd_op <= d_q[10:0];
                    cmd_fmt <= d_q[14:12];
                end
                default: ;
            endcase
        end
        if (st_valid) result <= st_data;
        case (p_q)
            3'd0: rdata <= result[31:0];
            3'd1: rdata <= result[63:32];
            3'd2: rdata <= {16'd0, result[79:64]};
            3'd3: rdata <= {sw, cw};
            3'd4: rdata <= {tw, 13'd0, ferr, busy, st_valid};
            default: rdata <= 32'hffffffff;
        endcase
    end
    x87s_unit #(.MUL(MUL), .SHIFT(SHIFT), .DIVSQRT(DIVSQRT), .TRANS(TRANS)) unit (
        .clk, .reset, .cmd_valid, .cmd_op, .cmd_fmt, .cmd_data(operand[79:0]), .busy, .st_valid, .st_data,
        .sw, .cw, .tw, .ferr, .mb_req, .mb_op, .mb_a, .mb_b, .mb_ack, .mb_result, .mb_flags);
endmodule

// The standalone spike: the unit in the shared envelope, every input from
// the stimulus and every output folded into the signature.
module spike_x87 #(
    parameter int MUL = 0,
    parameter int SHIFT = 0,
    parameter int DIVSQRT = 1,
    parameter int TRANS = 1
) (
    input  wire  FPGA_CLK1_50,
    input  wire  KEY0_N,
    output logic LED0
);
    wire clk = FPGA_CLK1_50;
    logic         reset;
    logic [191:0] noise;
    logic         busy, st_valid, ferr, mb_req;
    logic [79:0]  st_data, mb_a, mb_b;
    logic [15:0]  sw, cw, tw;
    logic [7:0]   mb_op;
    logic [63:0]  signature;
    spike_stimulus #(.WIDTH(192)) stimulus (.clk, .key0_n(KEY0_N), .reset, .noise);
    x87s_unit #(.MUL(MUL), .SHIFT(SHIFT), .DIVSQRT(DIVSQRT), .TRANS(TRANS)) dut (
        .clk, .reset, .cmd_valid(noise[0] && noise[1]), .cmd_op(noise[12:2]), .cmd_fmt(noise[15:13]),
        .cmd_data(noise[95:16]), .busy, .st_valid, .st_data, .sw, .cw, .tw, .ferr, .mb_req, .mb_op, .mb_a, .mb_b,
        .mb_ack(noise[96] && mb_req), .mb_result(noise[176:97]), .mb_flags(noise[184:177]));
    always_ff @(posedge clk) begin
        signature <= {signature[62:0], signature[63]} ^ {st_data[63:0]}
                   ^ {st_data[79:64], sw, cw, tw} ^ mb_a[63:0] ^ {mb_a[79:64], mb_b[79:64], mb_op, 21'd0, busy, st_valid, ferr}
                   ^ mb_b[63:0];
        LED0 <= ^signature;
    end
endmodule
