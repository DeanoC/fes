// SPDX-License-Identifier: MIT
// One outstanding DIV or SQRT. Restoring digit recurrences share one 68-bit
// subtractor; no divider, multiplier, ROM or approximate reciprocal is used.
// PC selects 24/53/64 retained bits plus guard and round. The exact remainder
// supplies sticky, so the common rounder performs the only rounding operation.
module fes_x87_slow (
    input  logic clk,
    input  logic reset,
    input  logic flush,
    input  logic in_valid,
    output logic in_ready,
    input  fes_x87_pkg::request_t req,
    output logic out_valid,
    input  logic out_ready,
    output fes_x87_pkg::raw_t res
);
    import fes_x87_pkg::*;

    logic busy, starting, square_root;
    logic [6:0] remaining;
    logic [63:0] divisor;
    logic [65:0] digits, radicand;
    logic [66:0] remainder;
    logic [67:0] subtract_left, subtract_right, next_remainder;
    logic [68:0] difference;
    logic [65:0] next_digits;
    logic [65:0] aligned_digits;
    logic sticky;

    unpack_t a, b;
    raw_t setup_result;
    logic special_case;
    logic [6:0] digit_count;
    logic [64:0] initial_numerator, saved_numerator;
    logic exponent_odd;
    logic signed [17:0] unbiased;

    // Division keeps remainder < divisor. Square root keeps remainder <
    // 2*digits+1, so its pre-subtract remainder fits 68 bits through digit 66.
    always_comb begin
        if (starting) begin
            subtract_left = {3'd0, saved_numerator};
            subtract_right = {4'd0, divisor};
        end else if (square_root) begin
            subtract_left = {remainder[65:0], radicand[65:64]};
            subtract_right = {digits, 2'b01};
        end else begin
            subtract_left = {remainder[66:0], 1'b0};
            subtract_right = {4'd0, divisor};
        end
        difference = {1'b0, subtract_left} - {1'b0, subtract_right};
        next_remainder = difference[68] ? subtract_left : difference[67:0];
        next_digits = {digits[64:0], !difference[68]};
        // Fixed wiring for the three precisions avoids a general barrel shift.
        case (res.pc)
            2'b00: aligned_digits = {next_digits[25:0], 40'd0};
            2'b10: aligned_digits = {next_digits[54:0], 11'd0};
            default: aligned_digits = next_digits;
        endcase
        sticky = (next_remainder != 0) ||
                 (square_root && radicand[63:0] != 0);
    end

    always_comb begin
        // Common ingress has already registered normalization. Keeping its
        // leading-zero detector and barrel shifter outside this lane prevents
        // them from sharing a path with quotient ordering and subtraction.
        a = req.ua;
        b = req.ub;
        setup_result = '0;
        setup_result.slot = req.slot;
        setup_result.pc = req.pc;
        setup_result.rc = req.rc;
        setup_result.sign = req.a[79] ^ ((req.op == OP_SQRT) ? 1'b0 : req.b[79]);
        setup_result.flags[1] = is_denormal(req.a) ||
                           ((req.op != OP_SQRT) && is_denormal(req.b));
        setup_result.exponent = a.exponent - b.exponent + 18'sd16383;
        special_case = 1'b1;
        setup_result.special = 1'b1;

        if (is_unsupported(req.a) ||
            ((req.op != OP_SQRT) && is_unsupported(req.b))) begin
            setup_result.value = 80'hffffc000000000000000;
            setup_result.flags = 6'b000001;
        end else if (is_nan(req.a) ||
                     ((req.op != OP_SQRT) && is_nan(req.b))) begin
            setup_result.value = nan_value(req.a, (req.op == OP_SQRT) ? req.a : req.b);
            setup_result.flags[0] = is_snan(req.a) ||
                                ((req.op != OP_SQRT) && is_snan(req.b));
            setup_result.flags[1] = 1'b0;
        end else if (req.op == OP_SQRT) begin
            if (req.a[79] && !is_zero(req.a)) begin
                setup_result.value = 80'hffffc000000000000000;
                setup_result.flags = 6'b000001;
            end else if (is_inf(req.a) || is_zero(req.a)) begin
                setup_result.value = req.a;
            end else begin
                special_case = 1'b0;
                setup_result.special = 1'b0;
            end
        end else if ((is_zero(req.a) && is_zero(req.b)) ||
                     (is_inf(req.a) && is_inf(req.b))) begin
            setup_result.value = 80'hffffc000000000000000;
            setup_result.flags = 6'b000001;
        end else if (is_inf(req.a)) begin
            setup_result.value = {setup_result.sign, 15'h7fff, 64'h8000000000000000};
        end else if (is_zero(req.b)) begin
            setup_result.value = {setup_result.sign, 15'h7fff, 64'h8000000000000000};
            setup_result.flags = 6'b000100;
        end else if (is_zero(req.a) || is_inf(req.b)) begin
            setup_result.value = {setup_result.sign, 79'd0};
        end else begin
            special_case = 1'b0;
            setup_result.special = 1'b0;
        end

        // x87's reserved PC=01 is treated as extended precision.
        case (req.pc)
            2'b00: digit_count = 7'd26;
            2'b10: digit_count = 7'd55;
            default: digit_count = 7'd66;
        endcase
        initial_numerator = a.significand >= b.significand ?
                            {1'b0, a.significand} : {a.significand, 1'b0};
        unbiased = a.exponent - 18'sd16383;
        exponent_odd = unbiased[0];
        if (req.op == OP_SQRT)
            setup_result.exponent = (unbiased >>> 1) + 18'sd16383;
        else if (a.significand < b.significand)
            setup_result.exponent = setup_result.exponent - 18'sd1;
    end

    assign in_ready = !busy && !out_valid && !flush && !reset;

    always_ff @(posedge clk) begin
        if (reset || flush) begin
            busy <= 1'b0;
            starting <= 1'b0;
            out_valid <= 1'b0;
            // Operand and result bytes are overwritten before their validity
            // is asserted. Resetting only control avoids wide reset muxes.
        end else begin
            if (out_valid && out_ready)
                out_valid <= 1'b0;
            if (in_valid && in_ready) begin
                res <= setup_result;
                square_root <= req.op == OP_SQRT;
                if (special_case) begin
                    out_valid <= 1'b1;
                end else begin
                    busy <= 1'b1;
                    divisor <= b.significand;
                    starting <= req.op != OP_SQRT;
                    if (req.op == OP_SQRT) begin
                        // The omitted low 66 radicand bits are all zero.
                        // Odd unbiased exponent takes sqrt(2*significand).
                        radicand <= exponent_odd ? {a.significand, 2'b00} :
                                                  {1'b0, a.significand, 1'b0};
                        remainder <= '0;
                        digits <= '0;
                        remaining <= digit_count;
                    end else begin
                        // Register ordering/mux separately from subtraction.
                        // The next cycle reuses the recurrence subtractor to
                        // initialize remainder; the quotient starts with 1.
                        saved_numerator <= initial_numerator;
                        digits <= 66'd1;
                        radicand <= '0;
                        remaining <= digit_count - 7'd1;
                    end
                end
            end else if (busy) begin
                if (starting) begin
                    remainder <= difference[66:0];
                    starting <= 1'b0;
                end else begin
                    remainder <= next_remainder[66:0];
                    digits <= next_digits;
                    radicand <= {radicand[63:0], 2'b00};
                    remaining <= remaining - 7'd1;
                    if (remaining == 7'd1) begin
                        res.significand <= {aligned_digits, sticky};
                        out_valid <= 1'b1;
                        busy <= 1'b0;
                    end
                end
            end
        end
    end
endmodule
