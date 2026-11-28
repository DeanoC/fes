// Shared binary32/binary64 arithmetic datapath. Single significands use the high 24 bits
// of the double datapath. The low three bits carry guard, round, and sticky.
module fes_sh4_fp_arithmetic (
    input  logic        clk,
    input  logic        reset,
    input  logic        start,
    input  logic        wide,
    input  logic        subtract,
    input  logic        multiply,
    input  logic        toward_zero,
    input  logic        flush_denormals,
    input  logic [63:0] lhs, rhs,
    output logic        done,
    output logic [63:0] value,
    output logic [5:0]  causes, // E V Z O U I
    output logic        normal_operands
);
    typedef enum logic [4:0] {
        IDLE, CLASSIFY, ORDER, ALIGN_COARSE, ALIGN_FINE, SUM,
        SHIFT_CARRY, NORMALIZE, ROUND, PACK, DONE,
        PRODUCT, DIAGONALS, MIDDLE, COLUMN_1, COLUMN_2, COLUMN_3,
        COLUMN_4, PRODUCT_NORMALIZE
    } state_t;
    state_t state;

    localparam logic [3:0] ZERO = 0, NORMAL = 1, SUBNORMAL = 2,
                           INFINITY = 3, QUIET_NAN = 4, SIGNALING_NAN = 5;
    logic [63:0] a_bits, b_bits;
    logic [3:0] a_class, b_class;
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
    logic [12:0] denormal_shift;
    logic [55:0] product_significand;
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
        return wide ? {sign_bit, exponent, fraction} : {32'b0, sign_bit, exponent[7:0], fraction[51:29]};
    endfunction

    assign max_exponent = wide ? 11'd2047 : 11'd255;
    assign a_exponent = wide ? a_bits[62:52] : {3'b0, a_bits[30:23]};
    assign b_exponent = wide ? b_bits[62:52] : {3'b0, b_bits[30:23]};
    assign a_sig = {1'b1, wide ? a_bits[51:0] : {a_bits[22:0], 29'b0}};
    assign b_sig = {1'b1, wide ? b_bits[51:0] : {b_bits[22:0], 29'b0}};
    assign a_sign = wide ? a_bits[63] : a_bits[31];
    assign b_sign = (wide ? b_bits[63] : b_bits[31]) ^ subtract;
    assign a_class = classify(a_exponent, max_exponent, a_sig[51:0]);
    assign b_class = classify(b_exponent, max_exponent, b_sig[51:0]);

    assign b_larger = {b_exponent, b_sig} > {a_exponent, a_sig};
    assign exp_distance = b_larger ? b_exponent - a_exponent : a_exponent - b_exponent;
    assign fine_shifted = distance == 0 ? small_sig : (small_sig >> distance[2:0]) |
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
    assign denormal_shift = $unsigned(13'sd1 - normalized_product_exponent);
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

    assign guard_bit = wide ? norm_sig[2] : norm_sig[31];
    assign rest = wide ? |norm_sig[1:0] : |norm_sig[30:0];
    assign discarded = guard_bit || rest;
    assign round_up = !toward_zero && guard_bit && (rest || (wide ? norm_sig[3] : norm_sig[32]));
    assign carry = wide ? rounded[53] : rounded[24];
    assign tiny = exp_result == 1 && !(wide ? (carry || rounded[52]) : (carry || rounded[23]));
    assign overflow_result = exp_result >= max_exponent || packed_exponent >= max_exponent;
    assign packed_exponent = tiny ? 11'b0 : exp_result + {10'b0, carry};
    assign packed_fraction = wide ? (carry ? rounded[52:1] : rounded[51:0]) :
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
                state <= CLASSIFY;
            end
            CLASSIFY: begin
                normal_operands <= a_class == NORMAL && b_class == NORMAL;
                state <= DONE;
                if (a_class == SIGNALING_NAN || b_class == SIGNALING_NAN ||
                    (!multiply && a_class == INFINITY && b_class == INFINITY && a_sign != b_sign) ||
                    (multiply && ((a_class == INFINITY && b_class == ZERO) ||
                                  (b_class == INFINITY && a_class == ZERO)))) begin
                    value <= pack(1'b0, max_exponent, 52'h7_ffff_ffff_ffff);
                    causes[4] <= 1'b1;
                end else if (a_class == QUIET_NAN || b_class == QUIET_NAN)
                    value <= pack(1'b0, max_exponent, 52'h7_ffff_ffff_ffff);
                else if (a_class == SUBNORMAL || b_class == SUBNORMAL)
                    causes[5] <= 1'b1;
                else if (a_class == INFINITY || b_class == INFINITY)
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
                    small_sig <= {8'b0, small_sig[55:9], |small_sig[8:0]};
                    distance <= distance - 6'd8;
                end else
                    state <= ALIGN_FINE;
            end
            ALIGN_FINE: begin
                small_sig <= fine_shifted;
                if (multiply)
                    norm_sig <= fine_shifted;
                state <= multiply ? ROUND : SUM;
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
                state <= PRODUCT_NORMALIZE;
            end
            PRODUCT_NORMALIZE: begin
                if (normalized_product_exponent <= 0) begin
                    exp_result <= 11'd1;
                    distance <= denormal_shift >= 13'd56 ? 6'd56 : denormal_shift[5:0];
                    small_sig <= product_significand;
                    state <= ALIGN_COARSE;
                end else begin
                    exp_result <= normalized_product_exponent >= $signed({2'b0, max_exponent}) ?
                                  max_exponent : normalized_product_exponent[10:0];
                    norm_sig <= product_significand;
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
                rounded <= (wide ? {1'b0, norm_sig[55:3]} : {30'b0, norm_sig[55:32]}) +
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
                    causes[1:0] <= {tiny && discarded, discarded};
                end
                state <= DONE;
            end
            DONE: state <= IDLE;
            default: state <= IDLE;
        endcase
    end
endmodule
