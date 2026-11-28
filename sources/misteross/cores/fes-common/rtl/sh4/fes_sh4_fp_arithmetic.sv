// Shared binary32/binary64 arithmetic datapath. Single significands use the high 24 bits
// of the double datapath. The low three bits carry guard, round, and sticky.
module fes_sh4_fp_arithmetic (
    input  logic        clk,
    input  logic        reset,
    input  logic        start,
    input  logic        wide,
    input  logic        subtract,
    input  logic        multiply,
    input  logic        divide,
    input  logic        square_root,
    input  logic        reciprocal_root,
    input  logic [2:0]  transfer,
    input  logic        toward_zero,
    input  logic        flush_denormals,
    input  logic [63:0] lhs, rhs,
    input  logic [31:0] addend,
    output logic        done,
    output logic [63:0] value,
    output logic [5:0]  causes, // E V Z O U I
    output logic        normal_operands
);
    typedef enum logic [4:0] {
        IDLE, CLASSIFY, ORDER, ALIGN_COARSE, ALIGN_FINE, SUM,
        SHIFT_CARRY, NORMALIZE, ROUND, PACK, DONE,
        PRODUCT, DIAGONALS, MIDDLE, COLUMN_1, COLUMN_2, COLUMN_3,
        COLUMN_4, PRODUCT_NORMALIZE, DIVIDE_PREP, ROOT_PREP,
        DIGIT_TEST, DIGIT_NEXT, DIGIT_RESULT,
        CONVERT, COMPARE, INTEGER_RESULT,
        MAC_CLASSIFY, MAC_PRODUCT, MAC_ORDER, MAC_ALIGN, RECIPROCAL_PREP
    } state_t;
    state_t state;
    localparam logic [2:0] ARITHMETIC = 0, CMP_EQ = 1, CMP_GT = 2, FROM_INT = 3,
                           TO_INT = 4, TO_DOUBLE = 5, TO_SINGLE = 6, MAC = 7;

    localparam logic [3:0] ZERO = 0, NORMAL = 1, SUBNORMAL = 2,
                           INFINITY = 3, QUIET_NAN = 4, SIGNALING_NAN = 5;
    logic [63:0] a_bits, b_bits;
    logic input_wide, result_wide;
    logic [10:0] input_max_exponent;
    logic [31:0] integer_magnitude;
    logic signed [12:0] single_exponent;
    logic [5:0] integer_shift;
    logic [62:0] compare_a, compare_b;
    logic compare_a_sign, compare_b_sign, compare_equal, compare_greater;
    logic [3:0] a_class, b_class, addend_class;
    logic mac_product_sign, mac_product_first;
    logic signed [12:0] mac_exponent, mac_delta;
    logic [55:0] mac_significand, addend_significand;
    logic [12:0] mac_distance;
    logic [10:0] a_exponent, b_exponent, max_exponent;
    logic [52:0] a_sig, b_sig;
    logic a_sign, b_sign;
    logic b_larger, negative, subtract_magnitude;
    logic [5:0] distance;
    logic [10:0] exp_result;
    logic [10:0] exp_distance;
    logic [55:0] large_sig, small_sig, norm_sig, fine_shifted;
    logic [53:0] product_a, product_b;
    (* multstyle = "dsp" *) logic [35:0] partial [0:2][0:2];
    logic [36:0] diagonal_1, diagonal_2_first, diagonal_3;
    logic [37:0] diagonal_2;
    logic [19:0] column_carry;
    logic [37:0] column_1, column_2, column_3;
    logic [33:0] column_4;
    logic [105:0] product_result;
    logic signed [12:0] product_exponent, normalized_product_exponent;

    logic [55:0] product_significand;
    logic signed [12:0] quotient_exponent, unrounded_exponent;
    logic [12:0] tiny_shift;
    logic [10:0] root_biased_exponent;
    logic [10:0] bias;
    logic [109:0] radicand;
    logic [54:0] digit_value;
    logic [52:0] divisor;
    logic [56:0] remainder, trial_dividend, trial_subtrahend, trial_original, digit_remainder;
    logic [57:0] trial_difference;
    logic [5:0] digits_left;
    logic digit, digit_sticky, digit_root, reciprocal_scaled;
    logic [55:0] digit_significand;
    logic [56:0] sum;
    logic [3:0] leading, left_count;
    logic guard_bit, rest, discarded, round_up;
    logic [53:0] rounded;
    logic carry, tiny, overflow_result;
    logic [10:0] packed_exponent;
    logic [51:0] packed_fraction;

    function automatic logic [3:0] classify(
        input logic [10:0] exponent, max_exp,
        input logic [51:0] fraction
    );
        if (exponent == 0)
            return fraction == 0 || flush_denormals ? ZERO : SUBNORMAL;
        if (exponent == max_exp) begin
            if (fraction == 0)
                return INFINITY;
            return fraction[51] ? SIGNALING_NAN : QUIET_NAN;
        end
        return NORMAL;
    endfunction

    function automatic logic [63:0] pack(
        input logic sign_bit, input logic [10:0] exponent, input logic [51:0] fraction
    );
        return result_wide ? {sign_bit, exponent, fraction} : {32'b0, sign_bit, exponent[7:0], fraction[51:29]};
    endfunction

    assign input_wide = wide && transfer != TO_DOUBLE;
    assign result_wide = wide && transfer != TO_SINGLE;
    assign input_max_exponent = input_wide ? 11'd2047 : 11'd255;
    assign max_exponent = result_wide ? 11'd2047 : 11'd255;
    assign a_exponent = input_wide ? a_bits[62:52] : {3'b0, a_bits[30:23]};
    assign b_exponent = input_wide ? b_bits[62:52] : {3'b0, b_bits[30:23]};
    assign a_sig = {1'b1, input_wide ? a_bits[51:0] : {a_bits[22:0], 29'b0}};
    assign b_sig = {1'b1, input_wide ? b_bits[51:0] : {b_bits[22:0], 29'b0}};
    assign a_sign = input_wide ? a_bits[63] : a_bits[31];
    assign b_sign = (input_wide ? b_bits[63] : b_bits[31]) ^ subtract;
    assign a_class = classify(a_exponent, input_max_exponent, a_sig[51:0]);
    assign b_class = classify(b_exponent, input_max_exponent, b_sig[51:0]);
    assign addend_class = classify({3'b0, addend[30:23]}, 11'd255, {addend[22:0], 29'b0});
    assign addend_significand = {1'b1, addend[22:0], 32'b0};
    assign mac_product_sign = a_sign ^ b_sign;
    assign mac_product_first = mac_delta > 0 || (mac_delta == 0 && mac_significand >= addend_significand);
    assign integer_magnitude = a_bits[31] ? -a_bits[31:0] : a_bits[31:0];
    assign single_exponent = $signed({2'b0, a_exponent}) - 13'sd896;
    assign integer_shift = bias[5:0] + 6'd52 - a_exponent[5:0];
    assign compare_equal = compare_a_sign == compare_b_sign && compare_a == compare_b;
    assign compare_greater = compare_a_sign != compare_b_sign ? !compare_a_sign :
                             compare_a_sign ? compare_a < compare_b : compare_a > compare_b;

    assign b_larger = {b_exponent, b_sig} > {a_exponent, a_sig};
    assign exp_distance = b_larger ? b_exponent - a_exponent : a_exponent - b_exponent;
    assign fine_shifted = distance == 0 || transfer == TO_INT ? small_sig >> distance[2:0] : (small_sig >> distance[2:0]) |
                          {55'b0, |(small_sig[7:0] & (8'hff >> (4'd8 - {1'b0, distance[2:0]})))};

    // Nine unsigned 18-bit products fit the FPGA's DSP lanes. Diagonal
    // sums and the base-2^18 carry chain each have registered boundaries.
    assign product_a = {1'b0, a_sig};
    assign product_b = {1'b0, b_sig};
    for (genvar i = 0; i < 3; i++) begin : product_rows
        for (genvar j = 0; j < 3; j++) begin : product_columns
            always_ff @(posedge clk)
                if (state == PRODUCT)
                    partial[i][j] <= product_a[18*i +: 18] * product_b[18*j +: 18];
        end
    end
    assign column_1 = {1'b0, diagonal_1} + {20'b0, partial[0][0][35:18]};
    assign column_2 = diagonal_2 + {18'b0, column_carry};
    assign column_3 = {1'b0, diagonal_3} + {18'b0, column_carry};
    assign column_4 = partial[2][2][33:0] + {14'b0, column_carry};
    assign normalized_product_exponent = product_exponent + $signed({12'b0, product_result[105]});
    assign bias = wide ? 11'd1023 : 11'd127;
    assign root_biased_exponent = {1'b0, a_exponent[10:1]} + {1'b0, bias[10:1]} + {10'b0, a_exponent[0]};
    assign quotient_exponent = $signed({2'b0, a_exponent}) - $signed({2'b0, b_exponent}) +
                               $signed({2'b0, bias}) - $signed({12'b0, a_sig < b_sig});

    // A restoring quotient or square-root digit uses a registered
    // trial subtraction. The following cycle accepts or restores it.
    assign trial_dividend = digit_root ? {remainder[54:0], radicand[109:108]} : remainder;
    assign trial_subtrahend = digit_root ? {digit_value, 2'b01} : {4'b0, divisor};
    assign digit = !trial_difference[57];
    assign digit_remainder = digit ? trial_difference[56:0] : trial_original;
    assign digit_sticky = |remainder || (digit_root && (|radicand));
    assign reciprocal_scaled = digit_value[28:0] != 0;
    assign digit_significand = wide ? {digit_value, digit_sticky} :
                                     {digit_value[25:0], 29'b0, digit_sticky};
    assign unrounded_exponent = transfer == TO_SINGLE ? single_exponent : multiply ? normalized_product_exponent : product_exponent;
    assign tiny_shift = $unsigned(13'sd1 - unrounded_exponent);
    assign product_significand = product_result[105] ?
                                 {product_result[105:51], |product_result[50:0]} :
                                 {product_result[104:50], |product_result[49:0]};

    // Shift by up to eight bits per normalize cycle. Close cancellation
    // may take several cycles, keeping the leading-zero path short.
    always_comb begin
        casez (norm_sig[55:48])
            8'b1???????: leading = 0;
            8'b01??????: leading = 1;
            8'b001?????: leading = 2;
            8'b0001????: leading = 3;
            8'b00001???: leading = 4;
            8'b000001??: leading = 5;
            8'b0000001?: leading = 6;
            8'b00000001: leading = 7;
            default: leading = 8;
        endcase
    end
    assign left_count = exp_result > {7'b0, leading} ? leading : exp_result[3:0] - 4'd1;

    assign guard_bit = result_wide ? norm_sig[2] : norm_sig[31];
    assign rest = result_wide ? |norm_sig[1:0] : |norm_sig[30:0];
    assign discarded = guard_bit || rest;
    assign round_up = !toward_zero && guard_bit && (rest || (result_wide ? norm_sig[3] : norm_sig[32]));
    assign carry = result_wide ? rounded[53] : rounded[24];
    assign tiny = exp_result == 1 && !(result_wide ? (carry || rounded[52]) : (carry || rounded[23]));
    assign overflow_result = exp_result >= max_exponent || packed_exponent >= max_exponent;
    assign packed_exponent = tiny ? 11'b0 : exp_result + {10'b0, carry};
    assign packed_fraction = result_wide ? (carry ? rounded[52:1] : rounded[51:0]) :
                                    {(carry ? rounded[23:1] : rounded[22:0]), 29'b0};
    assign done = state == DONE;

    always_ff @(posedge clk) begin
        if (reset)
            state <= IDLE;
        else case (state)
            IDLE: if (start) begin
                a_bits <= lhs;
                b_bits <= rhs;
                causes <= 0;
                digit_root <= square_root || reciprocal_root;
                state <= CLASSIFY;
            end
            CLASSIFY: begin
                normal_operands <= digit_root ? a_class == NORMAL && !a_sign :
                                   transfer == FROM_INT || (transfer == TO_SINGLE && a_class == NORMAL) ||
                                   (transfer == ARITHMETIC && a_class == NORMAL && b_class == NORMAL);
                state <= DONE;
                if (transfer == MAC)
                    state <= MAC_CLASSIFY;
                else if (transfer != ARITHMETIC)
                    state <= CONVERT;
                else if (a_class == SIGNALING_NAN || (!digit_root && b_class == SIGNALING_NAN)) begin
                    value <= pack(1'b0, max_exponent, 52'h7_ffff_ffff_ffff);
                    causes[4] <= 1'b1;
                end else if (a_class == QUIET_NAN || (!digit_root && b_class == QUIET_NAN))
                    value <= pack(1'b0, max_exponent, 52'h7_ffff_ffff_ffff);
                else if (digit_root) begin
                    if (a_sign && a_class != ZERO) begin
                        value <= pack(1'b0, max_exponent, 52'h7_ffff_ffff_ffff);
                        causes[4] <= 1'b1;
                    end else if (a_class == SUBNORMAL)
                        causes[5] <= 1'b1;
                    else if (a_class == NORMAL)
                        state <= ROOT_PREP;
                    else if (reciprocal_root && a_class == ZERO) begin
                        value <= pack(a_sign, max_exponent, '0);
                        causes[3] <= 1'b1;
                    end else
                        value <= pack(a_sign, reciprocal_root ? 11'b0 : a_exponent, '0);
                end else if ((!(multiply || divide) && a_class == INFINITY &&
                              b_class == INFINITY && a_sign != b_sign) ||
                             (multiply && ((a_class == INFINITY && b_class == ZERO) ||
                                           (b_class == INFINITY && a_class == ZERO))) ||
                             (divide && a_class == b_class && (a_class inside {ZERO, INFINITY}))) begin
                    value <= pack(1'b0, max_exponent, 52'h7_ffff_ffff_ffff);
                    causes[4] <= 1'b1;
                end else if (a_class == SUBNORMAL || b_class == SUBNORMAL)
                    causes[5] <= 1'b1;
                else if (divide) begin
                    if (a_class == INFINITY)
                        value <= pack(a_sign ^ b_sign, max_exponent, 52'b0);
                    else if (b_class == ZERO) begin
                        value <= pack(a_sign ^ b_sign, max_exponent, 52'b0);
                        causes[3] <= 1'b1;
                    end else if (a_class == ZERO || b_class == INFINITY)
                        value <= pack(a_sign ^ b_sign, 11'b0, 52'b0);
                    else
                        state <= DIVIDE_PREP;
                end else if (a_class == INFINITY || b_class == INFINITY)
                    value <= pack(multiply ? a_sign ^ b_sign : a_class == INFINITY ? a_sign : b_sign,
                                  max_exponent, 52'b0);
                else if (multiply && (a_class == ZERO || b_class == ZERO))
                    value <= pack(a_sign ^ b_sign, 11'b0, 52'b0);
                else if (a_class == ZERO && b_class == ZERO)
                    value <= pack(a_sign && b_sign, 11'b0, 52'b0);
                else if (a_class == ZERO)
                    value <= pack(b_sign, b_exponent, b_sig[51:0]);
                else if (b_class == ZERO)
                    value <= pack(a_sign, a_exponent, a_sig[51:0]);
                else
                    state <= multiply ? PRODUCT : ORDER;
            end
            MAC_CLASSIFY: begin
                state <= DONE;
                if (a_class == SIGNALING_NAN || b_class == SIGNALING_NAN || addend_class == SIGNALING_NAN) begin
                    value <= 64'h7fbf_ffff;
                    causes[4] <= 1'b1;
                end else if (a_class == QUIET_NAN || b_class == QUIET_NAN)
                    value <= 64'h7fbf_ffff;
                else if (a_class == SUBNORMAL || b_class == SUBNORMAL)
                    causes[5] <= 1'b1;
                else if ((a_class == INFINITY && b_class == ZERO) ||
                         (b_class == INFINITY && a_class == ZERO)) begin
                    value <= 64'h7fbf_ffff;
                    causes[4] <= 1'b1;
                end else if (addend_class == QUIET_NAN)
                    value <= 64'h7fbf_ffff;
                else if (addend_class == SUBNORMAL)
                    causes[5] <= 1'b1;
                else if (a_class == INFINITY || b_class == INFINITY) begin
                    if (addend_class == INFINITY && mac_product_sign != addend[31]) begin
                        value <= 64'h7fbf_ffff;
                        causes[4] <= 1'b1;
                    end else
                        value <= pack(mac_product_sign, 11'd255, '0);
                end else if (addend_class == INFINITY)
                    value <= {32'b0, addend};
                else if (a_class == ZERO || b_class == ZERO)
                    value <= addend_class == ZERO ?
                             pack(mac_product_sign ^ addend[31], '0, '0) : {32'b0, addend};
                else
                    state <= PRODUCT;
            end
            MAC_PRODUCT: begin
                // Preserve the full 48-bit product before aligning the addend.
                mac_exponent <= normalized_product_exponent;
                mac_significand <= product_significand;
                mac_delta <= normalized_product_exponent - $signed({5'b0, addend[30:23]});
                state <= MAC_ORDER;
            end
            MAC_ORDER: begin
                if (addend_class == ZERO) begin
                    subtract_magnitude <= 0;
                    if (mac_exponent <= 0) begin
                        exp_result <= 11'd1;
                        large_sig <= 0;
                        small_sig <= mac_significand;
                        mac_distance <= $unsigned(13'sd1 - mac_exponent);
                    end else begin
                        exp_result <= mac_exponent[10:0];
                        large_sig <= mac_significand;
                        small_sig <= 0;
                        mac_distance <= 0;
                    end
                end else begin
                    subtract_magnitude <= mac_product_sign != addend[31];
                    negative <= mac_product_first ? mac_product_sign : addend[31];
                    exp_result <= mac_product_first ? mac_exponent[10:0] : {3'b0, addend[30:23]};
                    large_sig <= mac_product_first ? mac_significand : addend_significand;
                    small_sig <= mac_product_first ? addend_significand : mac_significand;
                    mac_distance <= $unsigned(mac_product_first ? mac_delta : -mac_delta);
                end
                state <= MAC_ALIGN;
            end
            MAC_ALIGN: begin
                distance <= mac_distance >= 13'd56 ? 6'd56 : mac_distance[5:0];
                state <= ALIGN_COARSE;
            end
            CONVERT: begin
                state <= DONE;
                case (transfer)
                    CMP_EQ, CMP_GT: begin
                        if (a_class == SIGNALING_NAN || b_class == SIGNALING_NAN ||
                            a_class == QUIET_NAN || b_class == QUIET_NAN) begin
                            value <= 0;
                            causes[4] <= transfer == CMP_GT || a_class == SIGNALING_NAN ||
                                         b_class == SIGNALING_NAN;
                        end else begin
                            compare_a <= a_class == ZERO ? 63'b0 : {a_exponent, a_sig[51:0]};
                            compare_b <= b_class == ZERO ? 63'b0 : {b_exponent, b_sig[51:0]};
                            compare_a_sign <= a_class != ZERO && a_sign;
                            compare_b_sign <= b_class != ZERO && b_sign;
                            state <= COMPARE;
                        end
                    end
                    FROM_INT: begin
                        if (integer_magnitude == 0)
                            value <= 0;
                        else begin
                            negative <= a_bits[31];
                            norm_sig <= {integer_magnitude, 24'b0};
                            exp_result <= bias + 11'd31;
                            state <= NORMALIZE;
                        end
                    end
                    TO_INT: begin
                        if (a_class == QUIET_NAN || a_class == SIGNALING_NAN ||
                            a_class == INFINITY || a_exponent >= bias + 11'd32) begin
                            causes[4] <= 1'b1;
                            value <= a_sign || a_class inside {QUIET_NAN, SIGNALING_NAN} ?
                                     64'h8000_0000 : 64'h7fff_ffff;
                        end else if (a_class == ZERO || a_class == SUBNORMAL || a_exponent < bias)
                            value <= 0;
                        else begin
                            negative <= a_sign;
                            small_sig <= {3'b0, a_sig};
                            distance <= integer_shift;
                            state <= ALIGN_COARSE;
                        end
                    end
                    TO_DOUBLE, TO_SINGLE: begin
                        case (a_class)
                            ZERO: value <= pack(a_sign, '0, '0);
                            INFINITY: value <= pack(a_sign, max_exponent, '0);
                            QUIET_NAN, SIGNALING_NAN: begin
                                value <= pack(1'b0, max_exponent, 52'h7_ffff_ffff_ffff);
                                causes[4] <= a_class == SIGNALING_NAN;
                            end
                            SUBNORMAL: causes[5] <= 1'b1;
                            NORMAL: begin
                                if (transfer == TO_DOUBLE)
                                    value <= pack(a_sign, a_exponent + 11'd896, a_sig[51:0]);
                                else begin
                                    negative <= a_sign;
                                    state <= PRODUCT_NORMALIZE;
                                end
                            end
                            default: ;
                        endcase
                    end
                    default: ;
                endcase
            end
            COMPARE: begin
                value <= {63'b0, transfer == CMP_EQ ? compare_equal : compare_greater};
                state <= DONE;
            end
            INTEGER_RESULT: begin
                if (norm_sig[31] && (!negative || norm_sig[30:0] != 0)) begin
                    value <= negative ? 64'h8000_0000 : 64'h7fff_ffff;
                    causes[4] <= 1'b1;
                end else
                    value <= {32'b0, negative ? -norm_sig[31:0] : norm_sig[31:0]};
                state <= DONE;
            end
            ORDER: begin
                negative <= b_larger ? b_sign : a_sign;
                subtract_magnitude <= a_sign != b_sign;
                exp_result <= b_larger ? b_exponent : a_exponent;
                large_sig <= {b_larger ? b_sig : a_sig, 3'b0};
                small_sig <= {b_larger ? a_sig : b_sig, 3'b0};
                distance <= exp_distance >= 11'd56 ? 6'd56 : exp_distance[5:0];
                state <= ALIGN_COARSE;
            end
            ALIGN_COARSE: begin
                // Iterative eight-bit shifts fold each group into sticky.
                if (distance >= 8) begin
                    small_sig <= transfer == TO_INT ? small_sig >> 8 :
                                 {8'b0, small_sig[55:9], |small_sig[8:0]};
                    distance <= distance - 6'd8;
                end else
                    state <= ALIGN_FINE;
            end
            ALIGN_FINE: begin
                small_sig <= fine_shifted;
                norm_sig <= fine_shifted;
                if (transfer == TO_INT)
                    state <= INTEGER_RESULT;
                else if (multiply || divide || transfer == TO_SINGLE)
                    state <= ROUND;
                else
                    state <= SUM;
            end
            SUM: begin
                sum <= subtract_magnitude ? {1'b0, large_sig} - {1'b0, small_sig} :
                                            {1'b0, large_sig} + {1'b0, small_sig};
                state <= SHIFT_CARRY;
            end
            PRODUCT: begin
                negative <= a_sign ^ b_sign;
                product_exponent <= $signed({2'b0, a_exponent}) + $signed({2'b0, b_exponent}) -
                                    (wide ? 13'sd1023 : 13'sd127);
                state <= DIAGONALS;
            end
            DIAGONALS: begin
                diagonal_1 <= {1'b0, partial[0][1]} + {1'b0, partial[1][0]};
                diagonal_2_first <= {1'b0, partial[0][2]} + {1'b0, partial[1][1]};
                diagonal_3 <= {1'b0, partial[1][2]} + {1'b0, partial[2][1]};
                state <= MIDDLE;
            end
            MIDDLE: begin
                diagonal_2 <= {1'b0, diagonal_2_first} + {2'b0, partial[2][0]};
                state <= COLUMN_1;
            end
            COLUMN_1: begin
                product_result[35:0] <= {column_1[17:0], partial[0][0][17:0]};
                column_carry <= column_1[37:18];
                state <= COLUMN_2;
            end
            COLUMN_2: begin
                product_result[53:36] <= column_2[17:0];
                column_carry <= column_2[37:18];
                state <= COLUMN_3;
            end
            COLUMN_3: begin
                product_result[71:54] <= column_3[17:0];
                column_carry <= column_3[37:18];
                state <= COLUMN_4;
            end
            COLUMN_4: begin
                product_result[105:72] <= column_4;
                state <= transfer == MAC ? MAC_PRODUCT : PRODUCT_NORMALIZE;
            end
            DIVIDE_PREP: begin
                negative <= a_sign ^ b_sign;
                product_exponent <= quotient_exponent;
                divisor <= b_sig;
                remainder <= a_sig < b_sig ? {3'b0, a_sig, 1'b0} : {4'b0, a_sig};
                digit_value <= 0;
                digits_left <= wide ? 6'd55 : 6'd26;
                state <= DIGIT_TEST;
            end
            ROOT_PREP: begin
                negative <= 0;
                product_exponent <= $signed({2'b0, root_biased_exponent});
                radicand <= a_exponent[0] ? {1'b0, a_sig, 56'b0} : {a_sig, 57'b0};
                remainder <= 0;
                digit_value <= 0;
                digits_left <= wide ? 6'd55 : reciprocal_root ? 6'd30 : 6'd26;
                state <= DIGIT_TEST;
            end
            DIGIT_TEST: begin
                trial_difference <= {1'b0, trial_dividend} - {1'b0, trial_subtrahend};
                trial_original <= trial_dividend;
                state <= DIGIT_NEXT;
            end
            DIGIT_NEXT: begin
                digit_value <= {digit_value[53:0], digit};
                if (digit_root) begin
                    remainder <= digit_remainder;
                    radicand <= {radicand[107:0], 2'b0};
                end else
                    remainder <= {digit_remainder[55:0], 1'b0};
                digits_left <= digits_left - 6'd1;
                if (digits_left == 6'd1)
                    state <= reciprocal_root && digit_root ? RECIPROCAL_PREP : DIGIT_RESULT;
                else
                    state <= DIGIT_TEST;
            end
            RECIPROCAL_PREP: begin
                // Retain thirty root digits and divide before binary32 rounding.
                divisor <= {digit_value[29:0], 23'b0};
                remainder <= reciprocal_scaled ? 57'h20_0000_0000_0000 : 57'h10_0000_0000_0000;
                product_exponent <= 13'sd254 - product_exponent - $signed({12'b0, reciprocal_scaled});
                digit_root <= 0;
                digit_value <= 0;
                digits_left <= 6'd26;
                state <= DIGIT_TEST;
            end
            DIGIT_RESULT, PRODUCT_NORMALIZE: begin
                if (unrounded_exponent <= 0) begin
                    exp_result <= 11'd1;
                    distance <= tiny_shift >= 13'd56 ? 6'd56 : tiny_shift[5:0];
                    small_sig <= transfer == TO_SINGLE ? {a_sig, 3'b0} : multiply ? product_significand : digit_significand;
                    state <= ALIGN_COARSE;
                end else begin
                    exp_result <= unrounded_exponent >= $signed({2'b0, max_exponent}) ?
                                  max_exponent : unrounded_exponent[10:0];
                    norm_sig <= transfer == TO_SINGLE ? {a_sig, 3'b0} : multiply ? product_significand : digit_significand;
                    state <= ROUND;
                end
            end
            SHIFT_CARRY: begin
                if (sum == 0) begin
                    value <= 0;
                    state <= DONE;
                end else if (sum[56]) begin
                    norm_sig <= {sum[56:2], sum[1] || sum[0]};
                    exp_result <= exp_result + 11'd1;
                    state <= ROUND;
                end else begin
                    norm_sig <= sum[55:0];
                    state <= NORMALIZE;
                end
            end
            NORMALIZE: begin
                norm_sig <= norm_sig << left_count;
                exp_result <= exp_result - {7'b0, left_count};
                if (leading != 8 || exp_result <= 8)
                    state <= ROUND;
            end
            ROUND: begin
                rounded <= (result_wide ? {1'b0, norm_sig[55:3]} : {30'b0, norm_sig[55:32]}) +
                           {53'b0, round_up};
                state <= PACK;
            end
            PACK: begin
                if (overflow_result) begin
                    value <= toward_zero ? pack(negative, max_exponent - 11'd1, '1) :
                                          pack(negative, max_exponent, '0);
                    causes[2:0] <= 3'b101;
                end else if (tiny && flush_denormals) begin
                    value <= pack(negative, 11'b0, 52'b0);
                    causes[1:0] <= 2'b11;
                end else begin
                    value <= pack(negative, packed_exponent, packed_fraction);
                    causes[1:0] <= {tiny && discarded, discarded || reciprocal_root};
                end
                state <= DONE;
            end
            DONE: state <= IDLE;
            default: state <= IDLE;
        endcase
    end
endmodule
