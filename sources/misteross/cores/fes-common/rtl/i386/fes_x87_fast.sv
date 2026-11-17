// SPDX-License-Identifier: MIT
// Seven registered stages, accepting one independent operation every clock.
// An input sampled on edge N appears at the output on edge N+6. Rounding is
// deliberately outside this pipe, shared with the iterative arithmetic lane.
module fes_x87_fast (
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

    // A PACKED array, not an unpacked one. The locked Yosys
    // (cores/fes-common/rtl/i386/toolchain.lock) reads and writes a
    // packed-struct member of an UNPACKED-array element as a fresh undriven
    // identifier -- `\meta[1].value' -- with a warning rather than an error,
    // so the netlist silently computes nonsense while the RTL simulation is
    // correct. The same code against a packed array is handled properly.
    // DeanoC/yosys#79 has the locked-binary repro; this is the workaround it
    // names, and unlike the whole-element-read attempt it was tried before
    // being believed.
    meta_t decoded;
    meta_t [6:1] meta;
    unpack_t ua, ub;
    logic b_sign, a_larger;
    logic [63:0] large_sig, small_sig;
    logic [17:0] distance;
    logic subtract;
    logic [6:0] valid;

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

    logic [63:0] s1_a, s1_b, s1_large, s1_small;
    logic [17:0] s1_distance;
    logic s1_subtract, s2_subtract;
    logic [66:0] s2_large, s2_small;
    logic [67:0] s3_sum;
    logic [66:0] s4_sum;
    logic [6:0] s4_left;
    logic s4_carry;
    logic [66:0] s5_add, s6_add;
    logic signed [17:0] s5_add_exponent, s6_add_exponent;

    // Nine unsigned tiles fit variable-precision DSP multipliers. The top
    // limb has only 20 bits. Registers sit at both multiplier boundaries;
    // no inferred 64x64 multiplier or combinational full-product tree exists.
    (* multstyle = "dsp" *) logic [43:0] p00, p01, p10, p11;
    (* multstyle = "dsp" *) logic [41:0] p02, p12, p20, p21;
    (* multstyle = "dsp" *) logic [39:0] p22;
    logic [43:0] s3_c0;
    logic [44:0] s3_c1, s3_c2_pair;
    logic [41:0] s3_c2_rest;
    logic [42:0] s3_c3;
    logic [39:0] s3_c4;
    logic [66:0] s4_low;
    logic [45:0] s4_mid;
    logic [61:0] s4_high;
    logic [66:0] s5_low;
    logic [83:0] s5_high;
    logic [127:0] s6_product;

    // Only validity is reset, allowing DSP internal registers to be packed.
    // Flushing drops every in-flight response, including a simultaneous input.
    always_ff @(posedge clk) begin
        if (reset || flush) valid <= '0;
        else valid <= {valid[5:0], in_valid};

        // 1: classify the shared normalized operands and order magnitudes.
        meta[1] <= decoded;
        s1_a <= ua.significand;
        s1_b <= ub.significand;
        s1_large <= large_sig;
        s1_small <= small_sig;
        s1_distance <= distance;
        s1_subtract <= subtract;
        for (int i = 2; i <= 6; i++) meta[i] <= meta[i-1];

        // 2: right alignment with jamming; nine registered DSP products.
        s2_large <= {s1_large, 3'b000};
        s2_small <= shr_sticky({s1_small, 3'b000}, s1_distance);
        s2_subtract <= s1_subtract;
        p00 <= s1_a[21:0] * s1_b[21:0];
        p01 <= s1_a[21:0] * s1_b[43:22];
        p02 <= s1_a[21:0] * s1_b[63:44];
        p10 <= s1_a[43:22] * s1_b[21:0];
        p11 <= s1_a[43:22] * s1_b[43:22];
        p12 <= s1_a[43:22] * s1_b[63:44];
        p20 <= s1_a[63:44] * s1_b[21:0];
        p21 <= s1_a[63:44] * s1_b[43:22];
        p22 <= s1_a[63:44] * s1_b[63:44];

        // 3: arithmetic add/subtract; first multiplier column reduction.
        s3_sum <= s2_subtract ? {1'b0,s2_large} - {1'b0,s2_small} :
                                  {1'b0,s2_large} + {1'b0,s2_small};
        s3_c0 <= p00;
        s3_c1 <= {1'b0,p01} + {1'b0,p10};
        s3_c2_pair <= {3'd0,p02} + {1'b0,p11};
        s3_c2_rest <= p20;
        s3_c3 <= {1'b0,p12} + {1'b0,p21};
        s3_c4 <= p22;

        // 4: carry handling and leading-zero count; second reduction.
        s4_sum <= s3_sum[67] ? {s3_sum[67:2], |s3_sum[1:0]} : s3_sum[66:0];
        s4_left <= s3_sum[67] ? 7'd0 : leading_zeroes(s3_sum[66:0]);
        s4_carry <= s3_sum[67];
        s4_low <= {23'd0,s3_c0} + {s3_c1,22'd0};
        s4_mid <= {1'b0,s3_c2_pair} + {4'd0,s3_c2_rest};
        s4_high <= {19'd0,s3_c3} + {s3_c4,22'd0};

        // 5: cancellation normalization; third multiplier reduction.
        s5_add <= s4_sum << s4_left;
        s5_add_exponent <= meta[4].exponent + $signed({17'd0,s4_carry}) -
                           $signed({11'd0,s4_left});
        s5_low <= s4_low;
        s5_high <= {38'd0,s4_mid} + {s4_high,22'd0};

        // 6: final full-product carry propagation, separate from normalization.
        s6_add <= s5_add;
        s6_add_exponent <= s5_add_exponent;
        s6_product <= {61'd0,s5_low} + {s5_high,44'd0};

        // 7: multiplier normalization, guard/round/sticky extraction and emit.
        res <= '0;
        res.slot <= meta[6].slot;
        res.pc <= meta[6].pc;
        res.rc <= meta[6].rc;
        res.sign <= meta[6].sign;
        res.special <= meta[6].special;
        res.value <= meta[6].value;
        res.flags <= meta[6].flags;
        res.cc <= meta[6].cc;
        if (meta[6].multiply) begin
            res.exponent <= meta[6].exponent + $signed({17'd0,s6_product[127]});
            res.significand <= s6_product[127] ?
                {s6_product[127:62], |s6_product[61:0]} :
                {s6_product[126:61], |s6_product[60:0]};
        end else begin
            res.exponent <= s6_add_exponent;
            res.significand <= s6_add;
            if (s6_add == 0 && !meta[6].special)
                res.sign <= meta[6].rc == 2'b01;
        end
    end
    assign out_valid = valid[6];
endmodule
