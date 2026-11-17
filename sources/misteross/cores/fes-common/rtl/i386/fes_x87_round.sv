// SPDX-License-Identifier: MIT
// Shared three-stage rounding pipeline. PC changes the significand precision,
// never the exponent range. Outputs are the masked x87 arithmetic results;
// flags describe this operation only (no architectural sticky status here).
module fes_x87_round (
    input logic clk, reset, flush, in_valid,
    input fes_x87_pkg::raw_t raw,
    output logic out_valid,
    output fes_x87_pkg::result_t res
);
    import fes_x87_pkg::*;
    raw_t denorm, meta;
    logic [2:0] valid;
    logic [63:0] truncated, ulp;
    logic guard_bit, tail, inexact, increment;
    logic [64:0] rounded;
    logic rounded_inexact, rounded_increment;
    logic tiny_s1, tiny_s2, precision_carry, discarded_raw;
    logic [64:0] rounded_next;
    logic [63:0] mantissa;
    logic signed [17:0] exponent;
    logic [5:0] flags;
    logic [79:0] packed_value;
    logic to_infinity;

    // x87 tests tininess after rounding to PC with an unbounded exponent.
    // Denormal packing can round a tiny value up to the minimum normal later;
    // that must not erase UE. Detect the only normalizing carry at exp=0.
    always_comb begin
        case (raw.pc)
            2'b00: begin
                discarded_raw = |raw.significand[42:0];
                precision_carry = &raw.significand[66:43];
                if (raw.rc == 0) precision_carry = precision_carry && raw.significand[42];
            end
            2'b10: begin
                discarded_raw = |raw.significand[13:0];
                precision_carry = &raw.significand[66:14];
                if (raw.rc == 0) precision_carry = precision_carry && raw.significand[13];
            end
            default: begin
                discarded_raw = |raw.significand[2:0];
                precision_carry = &raw.significand[66:3];
                if (raw.rc == 0) precision_carry = precision_carry && raw.significand[2];
            end
        endcase
        if (raw.rc != 0)
            precision_carry = precision_carry && discarded_raw &&
                ((raw.rc == 1 && raw.sign) || (raw.rc == 2 && !raw.sign));
    end

    always_comb begin
        case (denorm.pc)
            2'b00: begin
                truncated = denorm.significand[66:3] & 64'hffffff0000000000;
                ulp = 64'h0000010000000000;
                guard_bit = denorm.significand[42];
                tail = |denorm.significand[41:0];
            end
            2'b10: begin
                truncated = denorm.significand[66:3] & 64'hfffffffffffff800;
                ulp = 64'h0000000000000800;
                guard_bit = denorm.significand[13];
                tail = |denorm.significand[12:0];
            end
            default: begin // PC=01 is reserved; deterministically use PC64.
                truncated = denorm.significand[66:3];
                ulp = 64'd1;
                guard_bit = denorm.significand[2];
                tail = |denorm.significand[1:0];
            end
        endcase
        inexact = guard_bit | tail;
        case (denorm.rc)
            2'b00: increment = guard_bit && (tail || |(truncated & ulp));
            2'b01: increment = denorm.sign && inexact;
            2'b10: increment = !denorm.sign && inexact;
            default: increment = 0;
        endcase
        rounded_next = {1'b0,truncated} + (increment ? {1'b0,ulp} : 65'd0);
    end

    always_comb begin
        mantissa = rounded[64] ? rounded[64:1] : rounded[63:0];
        exponent = meta.exponent + (rounded[64] ? 18'sd1 : 18'sd0);
        flags = meta.flags;
        flags[5] = flags[5] | rounded_inexact;
        // Masked underflow: tiny after PC rounding AND lost precision.
        if (tiny_s2 && rounded_inexact) flags[4] = 1;
        packed_value = {meta.sign,exponent[14:0],mantissa};
        if (mantissa == 0 || (exponent == 1 && !mantissa[63])) packed_value[78:64] = 0;
        to_infinity = meta.rc == 0 || (meta.rc == 1 && meta.sign) || (meta.rc == 2 && !meta.sign);
        if (exponent >= 18'sd32767) begin
            flags[3] = 1;
            flags[5] = 1;
            if (to_infinity) packed_value = {meta.sign,15'h7fff,64'h8000000000000000};
            else begin
                case (meta.pc)
                    2'b00: mantissa = 64'hffffff0000000000;
                    2'b10: mantissa = 64'hfffffffffffff800;
                    default: mantissa = 64'hffffffffffffffff;
                endcase
                packed_value = {meta.sign,15'h7ffe,mantissa};
            end
        end
    end

    always_ff @(posedge clk) begin
        if (reset || flush) valid <= 0;
        else valid <= {valid[1:0],in_valid};
        denorm <= raw;
        tiny_s1 <= raw.exponent < 0 || (raw.exponent == 0 && !precision_carry);
        tiny_s2 <= tiny_s1;
        if (raw.exponent < 1) begin
            denorm.significand <= shr_sticky(raw.significand,18'(18'sd1-raw.exponent));
            denorm.exponent <= 18'sd1;
        end
        meta <= denorm;
        rounded <= rounded_next;
        rounded_inexact <= inexact;
        rounded_increment <= increment;
        res.slot <= meta.slot;
        res.cc <= meta.cc;
        res.value <= meta.special ? meta.value : packed_value;
        res.flags <= meta.special ? meta.flags : flags;
        res.incremented <= !meta.special && (exponent >= 18'sd32767 ? to_infinity : rounded_increment);
    end
    assign out_valid = valid[2];
endmodule
