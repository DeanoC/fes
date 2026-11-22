// SPDX-License-Identifier: MIT
// The fast lane: add, subtract, multiply and compare. Rounding is
// deliberately outside it, shared with the iterative arithmetic lane.
//
// PIPELINED picks how the one datapath below is cut.
//   - 1: seven registered stages, accepting one independent operation every
//     clock. An input sampled on edge N appears at the output on edge N+6.
//     `fes_x87_pipe`, the standalone backend #1892 fitted at 200 MHz, needs
//     this throughput.
//   - 0: the six stage boundaries are wires and only the result is
//     registered, so an input sampled on edge N is out on edge N+1. This is
//     for `fes_i386_x87`, which keeps one ESC instruction in flight and so
//     never uses the throughput (#2027 slice 6). It spends the clock
//     headroom: it closes at the DE25 machine's 61.44 MHz, and a later
//     clock raise needs the pipelining back.
module fes_x87_fast #(
    parameter bit PIPELINED = 1'b1
) (
    input  logic clk, reset, flush, in_valid,
    input  fes_x87_pkg::request_t req,
    output logic out_valid,
    output fes_x87_pkg::raw_t res
);
    import fes_x87_pkg::*;

    typedef struct packed {
        logic [3:0] slot;
        logic [1:0] pc, rc;
        logic sign;
        logic signed [17:0] exponent;
        logic multiply, special;
        logic [79:0] value;
        logic [5:0] flags;
        logic [2:0] cc;
    } meta_t;

    // No unpacked arrays: the locked Yosys
    // (cores/fes-common/rtl/i386/toolchain.lock) reads and writes a
    // packed-struct member of an UNPACKED-array element as a fresh undriven
    // identifier, with a warning rather than an error, so the netlist
    // silently computes nonsense while the RTL simulation is correct
    // (DeanoC/yosys#79). Each stage's metadata rides in that stage's packed
    // struct below.
    meta_t decoded;
    unpack_t ua, ub;
    logic b_sign, a_larger;
    logic [63:0] large_sig, small_sig;
    logic [17:0] distance;
    logic subtract;

    function automatic logic [6:0] leading_zeroes(input logic [66:0] x);
        logic found;
        logic [6:0] n;
        found = 0;
        n = 7'd67;
        for (int i = 66; i >= 0; i--)
            if (!found && x[i]) begin
                found = 1;
                n = 7'(66-i);
            end
        return n;
    endfunction

    // Comparisons use all 64 operand bits, independently of precision control.
    // Zero needs explicit handling: its unpacked exponent is immaterial.
    function automatic logic [2:0] compare_finite(
        input logic [79:0] a, b,
        input unpack_t a_unpacked, b_unpacked
    );
        logic magnitude_less, equal_value;
        equal_value = (is_zero(a) && is_zero(b)) ||
            (a_unpacked.sign == b_unpacked.sign &&
             a_unpacked.exponent == b_unpacked.exponent &&
             a_unpacked.significand == b_unpacked.significand);
        magnitude_less = is_zero(a) ? !is_zero(b) :
            (!is_zero(b) &&
             (a_unpacked.exponent < b_unpacked.exponent ||
              (a_unpacked.exponent == b_unpacked.exponent &&
               a_unpacked.significand < b_unpacked.significand)));
        if (equal_value) return 3'b100;
        if (a_unpacked.sign != b_unpacked.sign)
            return a_unpacked.sign ? 3'b001 : 3'b000;
        return (magnitude_less ^ a_unpacked.sign) ? 3'b001 : 3'b000;
    endfunction

    always_comb begin
        ua = req.ua;
        ub = req.ub;
        b_sign = ub.sign ^ (req.op == OP_SUB);
        a_larger = is_zero(req.b) ||
            (!is_zero(req.a) &&
             (ua.exponent > ub.exponent ||
              (ua.exponent == ub.exponent && ua.significand >= ub.significand)));
        large_sig = a_larger ? ua.significand : ub.significand;
        small_sig = a_larger ? ub.significand : ua.significand;
        // A zero operand may have an arbitrary unpacked exponent. It must
        // never cause a backwards shift of the nonzero operand.
        distance = (is_zero(req.a) || is_zero(req.b)) ? 18'd0 :
            18'(a_larger ? ua.exponent - ub.exponent : ub.exponent - ua.exponent);
        subtract = ua.sign != b_sign;

        decoded = '0;
        decoded.slot = req.slot;
        decoded.pc = req.pc;
        decoded.rc = req.rc;
        decoded.multiply = req.op == OP_MUL;
        decoded.sign = a_larger ? ua.sign : b_sign;
        decoded.exponent = a_larger ? ua.exponent : ub.exponent;
        if (req.op == OP_MUL) begin
            decoded.sign = ua.sign ^ ub.sign;
            decoded.exponent = ua.exponent + ub.exponent - 18'sd16383;
        end

        if (is_unsupported(req.a) || is_unsupported(req.b) ||
            is_nan(req.a) || is_nan(req.b)) begin
            decoded.special = 1;
            decoded.value = nan_value(req.a, req.b);
            decoded.flags[0] = is_unsupported(req.a) || is_unsupported(req.b) ||
                is_snan(req.a) || is_snan(req.b) || req.op == OP_CMP;
            if (req.op == OP_CMP || req.op == OP_UCMP) begin
                decoded.value = 0;
                decoded.cc = 3'b111;
            end
        end else begin
            decoded.flags[1] = is_denormal(req.a) || is_denormal(req.b);
            if (req.op == OP_CMP || req.op == OP_UCMP) begin
                decoded.special = 1;
                decoded.cc = compare_finite(req.a, req.b, ua, ub);
            end else if (req.op == OP_MUL) begin
                if ((is_inf(req.a) && is_zero(req.b)) ||
                    (is_inf(req.b) && is_zero(req.a))) begin
                    decoded.special = 1;
                    decoded.value = 80'hffffc000000000000000;
                    decoded.flags = 6'b000001;
                end else if (is_inf(req.a) || is_inf(req.b)) begin
                    decoded.special = 1;
                    decoded.value = {decoded.sign, 15'h7fff, 64'h8000000000000000};
                end else if (is_zero(req.a) || is_zero(req.b)) begin
                    decoded.special = 1;
                    decoded.value = {decoded.sign, 79'd0};
                end
            end else if (is_inf(req.a) || is_inf(req.b)) begin
                decoded.special = 1;
                if (is_inf(req.a) && is_inf(req.b) && subtract) begin
                    decoded.value = 80'hffffc000000000000000;
                    decoded.flags = 6'b000001;
                end else begin
                    decoded.value = {is_inf(req.a) ? ua.sign : b_sign,
                                     15'h7fff, 64'h8000000000000000};
                end
            end else if (is_zero(req.a) && is_zero(req.b)) begin
                decoded.special = 1;
                decoded.value = {subtract ? req.rc == 2'b01 : ua.sign, 79'd0};
            end
        end
    end

    // Each stage's values as the stage before computes them (`*_d`), and as
    // the next stage reads them: a register under PIPELINED, the same wire
    // otherwise.
    typedef struct packed {
        meta_t meta;
        logic [63:0] a, b, larger, smaller;
        logic [17:0] distance;
        logic subtract;
    } st1_t;
    typedef struct packed {
        meta_t meta;
        logic [66:0] larger, smaller;
        logic subtract;
    } st2_t;
    typedef struct packed {
        meta_t meta;
        logic [67:0] sum;
        logic [43:0] c0;
        logic [44:0] c1, c2_pair;
        logic [41:0] c2_rest;
        logic [42:0] c3;
        logic [39:0] c4;
    } st3_t;
    typedef struct packed {
        meta_t meta;
        logic [66:0] sum;
        logic [6:0] left;
        logic carry;
        logic [66:0] low;
        logic [45:0] mid;
        logic [61:0] high;
    } st4_t;
    typedef struct packed {
        meta_t meta;
        logic [66:0] add;
        logic signed [17:0] add_exponent;
        logic [66:0] low;
        logic [83:0] high;
    } st5_t;
    typedef struct packed {
        meta_t meta;
        logic [66:0] add;
        logic signed [17:0] add_exponent;
        logic [127:0] product;
    } st6_t;
    st1_t s1_d, s1;
    st2_t s2_d, s2;
    st3_t s3_d, s3;
    st4_t s4_d, s4;
    st5_t s5_d, s5;
    st6_t s6_d, s6;
    raw_t res_d;

    // Nine unsigned tiles fit variable-precision DSP multipliers. The top
    // limb has only 20 bits. Under PIPELINED registers sit at both
    // multiplier boundaries; no inferred 64x64 multiplier or full-product
    // tree exists either way.
    (* multstyle = "dsp" *) logic [43:0] p00_d, p01_d, p10_d, p11_d, p00, p01, p10, p11;
    (* multstyle = "dsp" *) logic [41:0] p02_d, p12_d, p20_d, p21_d, p02, p12, p20, p21;
    (* multstyle = "dsp" *) logic [39:0] p22_d, p22;

    // One block a stage, so no block reads a value it writes: without
    // PIPELINED each stage's input is the wire of the block before.
    always_comb begin
        // 1: classify the shared normalized operands and order magnitudes.
        s1_d.meta = decoded;
        s1_d.a = ua.significand;
        s1_d.b = ub.significand;
        s1_d.larger = large_sig;
        s1_d.smaller = small_sig;
        s1_d.distance = distance;
        s1_d.subtract = subtract;
    end
    always_comb begin
        // 2: right alignment with jamming; nine DSP products.
        s2_d.meta = s1.meta;
        s2_d.larger = {s1.larger, 3'b000};
        s2_d.smaller = shr_sticky({s1.smaller, 3'b000}, s1.distance);
        s2_d.subtract = s1.subtract;
        p00_d = s1.a[21:0] * s1.b[21:0];
        p01_d = s1.a[21:0] * s1.b[43:22];
        p02_d = s1.a[21:0] * s1.b[63:44];
        p10_d = s1.a[43:22] * s1.b[21:0];
        p11_d = s1.a[43:22] * s1.b[43:22];
        p12_d = s1.a[43:22] * s1.b[63:44];
        p20_d = s1.a[63:44] * s1.b[21:0];
        p21_d = s1.a[63:44] * s1.b[43:22];
        p22_d = s1.a[63:44] * s1.b[63:44];
    end
    always_comb begin
        // 3: arithmetic add/subtract; first multiplier column reduction.
        s3_d.meta = s2.meta;
        s3_d.sum = s2.subtract ? {1'b0,s2.larger} - {1'b0,s2.smaller} :
                                 {1'b0,s2.larger} + {1'b0,s2.smaller};
        s3_d.c0 = p00;
        s3_d.c1 = {1'b0,p01} + {1'b0,p10};
        s3_d.c2_pair = {3'd0,p02} + {1'b0,p11};
        s3_d.c2_rest = p20;
        s3_d.c3 = {1'b0,p12} + {1'b0,p21};
        s3_d.c4 = p22;
    end
    always_comb begin
        // 4: carry handling and leading-zero count; second reduction.
        s4_d.meta = s3.meta;
        s4_d.sum = s3.sum[67] ? {s3.sum[67:2], |s3.sum[1:0]} : s3.sum[66:0];
        s4_d.left = s3.sum[67] ? 7'd0 : leading_zeroes(s3.sum[66:0]);
        s4_d.carry = s3.sum[67];
        s4_d.low = {23'd0,s3.c0} + {s3.c1,22'd0};
        s4_d.mid = {1'b0,s3.c2_pair} + {4'd0,s3.c2_rest};
        s4_d.high = {19'd0,s3.c3} + {s3.c4,22'd0};
    end
    always_comb begin
        // 5: cancellation normalization; third multiplier reduction.
        s5_d.meta = s4.meta;
        s5_d.add = s4.sum << s4.left;
        s5_d.add_exponent = s4.meta.exponent + $signed({17'd0,s4.carry}) -
                            $signed({11'd0,s4.left});
        s5_d.low = s4.low;
        s5_d.high = {38'd0,s4.mid} + {s4.high,22'd0};
    end
    always_comb begin
        // 6: final full-product carry propagation, separate from normalization.
        s6_d.meta = s5.meta;
        s6_d.add = s5.add;
        s6_d.add_exponent = s5.add_exponent;
        s6_d.product = {61'd0,s5.low} + {s5.high,44'd0};
    end
    always_comb begin
        // 7: multiplier normalization, guard/round/sticky extraction and emit.
        res_d = '0;
        res_d.slot = s6.meta.slot;
        res_d.pc = s6.meta.pc;
        res_d.rc = s6.meta.rc;
        res_d.sign = s6.meta.sign;
        res_d.special = s6.meta.special;
        res_d.value = s6.meta.value;
        res_d.flags = s6.meta.flags;
        res_d.cc = s6.meta.cc;
        if (s6.meta.multiply) begin
            res_d.exponent = s6.meta.exponent + $signed({17'd0,s6.product[127]});
            res_d.significand = s6.product[127] ?
                {s6.product[127:62], |s6.product[61:0]} :
                {s6.product[126:61], |s6.product[60:0]};
        end else begin
            res_d.exponent = s6.add_exponent;
            res_d.significand = s6.add;
            if (s6.add == 0 && !s6.meta.special)
                res_d.sign = s6.meta.rc == 2'b01;
        end
    end

    // Only validity is reset, allowing DSP internal registers to be packed.
    // Flushing drops every in-flight response, including a simultaneous input.
    generate
        if (PIPELINED) begin : g_pipelined
            logic [6:0] valid;
            always_ff @(posedge clk) begin
                if (reset || flush) valid <= '0;
                else valid <= {valid[5:0], in_valid};
                s1 <= s1_d;
                s2 <= s2_d;
                s3 <= s3_d;
                s4 <= s4_d;
                s5 <= s5_d;
                s6 <= s6_d;
                {p00, p01, p02, p10, p11, p12, p20, p21, p22}
                    <= {p00_d, p01_d, p02_d, p10_d, p11_d, p12_d, p20_d, p21_d, p22_d};
                res <= res_d;
            end
            assign out_valid = valid[6];
        end else begin : g_single
            logic valid;
            assign s1 = s1_d;
            assign s2 = s2_d;
            assign s3 = s3_d;
            assign s4 = s4_d;
            assign s5 = s5_d;
            assign s6 = s6_d;
            assign {p00, p01, p02, p10, p11, p12, p20, p21, p22}
                = {p00_d, p01_d, p02_d, p10_d, p11_d, p12_d, p20_d, p21_d, p22_d};
            always_ff @(posedge clk) begin
                if (reset || flush) valid <= 1'b0;
                else valid <= in_valid;
                if (in_valid) res <= res_d;
            end
            assign out_valid = valid;
        end
    endgenerate
endmodule
