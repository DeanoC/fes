// Four-term SH-4 inner product. The same datapath evaluates each FTRV row.
module fes_sh4_fp_dot (
    input  logic        clk,
    input  logic        reset,
    input  logic        start,
    input  logic [1:0]  lane,
    input  logic        toward_zero,
    input  logic        flush_denormals,
    input  logic [31:0] lhs, rhs,
    output logic        done,
    output logic [31:0] value,
    output logic [5:0]  causes
);
    typedef enum logic [3:0] {
        IDLE, CLASSIFY, MULTIPLY, PRODUCT_NORMALIZE, STORE, CHECK,
        SELECT_TERM, ALIGN_COARSE, ALIGN_FINE, ACCUMULATE, ABSOLUTE,
        NORMALIZE, ROUND, PACK, DONE
    } state_t;
    state_t state;

    logic [31:0] a, b;
    logic a_zero, b_zero, a_inf, b_inf, invalid, quiet_nan;
    logic seen_invalid, seen_nan, seen_pos_inf, seen_neg_inf;
    logic [23:0] a_sig, b_sig;
    (* multstyle = "dsp" *) logic [47:0] product;
    logic signed [9:0] product_exp, max_exp, result_exp;
    logic [27:0] term_sig [0:3];
    logic signed [9:0] term_exp [0:3];
    logic [3:0] term_sign;
    logic [1:0] term;
    logic [31:0] aligned;
    logic [9:0] distance;
    logic align_sticky, selected_sign;
    logic [31:0] fine;
    logic [30:0] rounded_term;
    logic signed [32:0] accum;
    logic [31:0] magnitude;
    logic negative, carry, tiny;
    logic [24:0] rounded_value;
    logic [22:0] fraction;
    logic signed [9:0] packed_exp;

    assign a_zero = a[30:23] == 0 && (flush_denormals || a[22:0] == 0);
    assign b_zero = b[30:23] == 0 && (flush_denormals || b[22:0] == 0);
    assign a_inf = a[30:0] == 31'h7f80_0000;
    assign b_inf = b[30:0] == 31'h7f80_0000;
    assign invalid = (a[30:22] == 9'h1ff || b[30:22] == 9'h1ff) ||
                     (a_inf && b_zero) || (b_inf && a_zero);
    assign quiet_nan = (a[30:23] == 8'hff && !a_inf) || (b[30:23] == 8'hff && !b_inf);
    assign a_sig = {a[30:23] != 0, a[22:0]};
    assign b_sig = {b[30:23] != 0, b[22:0]};
    assign fine = aligned >> distance[2:0];
    assign rounded_term = {1'b0, fine[31:2]} +
                          {30'b0, !toward_zero && fine[1] &&
                          (fine[0] || align_sticky ||
                           (|(aligned[7:0] & (8'hff >> (4'd8 - {1'b0, distance[2:0]})))) ||
                           fine[2])};
    assign carry = rounded_value[24];
    assign tiny = result_exp == 1 && !(carry || rounded_value[23]);
    assign packed_exp = tiny ? 10'sd0 : result_exp + $signed({9'b0, carry});
    assign fraction = carry ? rounded_value[23:1] : rounded_value[22:0];
    assign done = state == DONE;

    always_ff @(posedge clk) begin
        if (reset)
            state <= IDLE;
        else case (state)
            IDLE: if (start) begin
                a <= lhs;
                b <= rhs;
                if (lane == 0) begin
                    seen_invalid <= 0;
                    seen_nan <= 0;
                    seen_pos_inf <= 0;
                    seen_neg_inf <= 0;
                    max_exp <= -10'sd512;
                    causes <= 6'b000001; // Approximate vector instructions report inexact.
                end
                state <= CLASSIFY;
            end
            CLASSIFY: begin
                if (invalid || quiet_nan || a_inf || b_inf || a_zero || b_zero) begin
                    seen_invalid <= seen_invalid || invalid;
                    seen_nan <= seen_nan || quiet_nan;
                    if ((a_inf || b_inf) && !quiet_nan && !invalid) begin
                        seen_pos_inf <= seen_pos_inf || !(a[31] ^ b[31]);
                        seen_neg_inf <= seen_neg_inf || (a[31] ^ b[31]);
                    end
                    product <= 0;
                    product_exp <= -10'sd512;
                    state <= STORE;
                end else
                    state <= MULTIPLY;
            end
            MULTIPLY: begin
                product <= a_sig * b_sig;
                product_exp <= $signed({2'b0, a[30:23]}) + $signed({2'b0, b[30:23]}) -
                               10'sd126 + $signed({9'b0, a[30:23] == 0}) +
                               $signed({9'b0, b[30:23] == 0});
                state <= PRODUCT_NORMALIZE;
            end
            PRODUCT_NORMALIZE: begin
                if (product[47])
                    state <= STORE;
                else if (product[46:44] != 0) begin
                    product <= product << 1;
                    product_exp <= product_exp - 10'sd1;
                end else begin
                    product <= product << 4;
                    product_exp <= product_exp - 10'sd4;
                end
            end
            STORE: begin
                term_sig[lane] <= product[47:20];
                term_exp[lane] <= product_exp;
                term_sign[lane] <= a[31] ^ b[31];
                if (product_exp > max_exp)
                    max_exp <= product_exp;
                state <= lane == 3 ? CHECK : DONE;
            end
            CHECK: begin
                state <= DONE;
                if (seen_invalid || (!seen_nan && seen_neg_inf && seen_pos_inf)) begin
                    value <= 32'h7fbf_ffff;
                    causes[4] <= 1;
                end else if (seen_nan)
                    value <= 32'h7fbf_ffff;
                else if (seen_pos_inf || seen_neg_inf)
                    value <= {seen_neg_inf, 31'h7f80_0000};
                else begin
                    term <= 0;
                    accum <= 0;
                    state <= SELECT_TERM;
                end
            end
            SELECT_TERM: begin
                aligned <= {term_sig[term], 4'b0};
                distance <= term_sig[term] == 0 ? 10'd0 : $unsigned(max_exp - term_exp[term]);
                selected_sign <= term_sign[term];
                align_sticky <= 0;
                state <= ALIGN_COARSE;
            end
            ALIGN_COARSE: begin
                if (distance >= 32) begin
                    aligned <= 0;
                    distance <= 0;
                end else if (distance >= 8) begin
                    aligned <= aligned >> 8;
                    distance <= distance - 10'd8;
                    align_sticky <= align_sticky || (|aligned[7:0]);
                end else
                    state <= ALIGN_FINE;
            end
            ALIGN_FINE: begin
                aligned <= {1'b0, rounded_term};
                state <= ACCUMULATE;
            end
            ACCUMULATE: begin
                accum <= selected_sign ? accum - $signed({1'b0, aligned}) : accum + $signed({1'b0, aligned});
                term <= term + 2'd1;
                state <= term == 3 ? ABSOLUTE : SELECT_TERM;
            end
            ABSOLUTE: begin
                result_exp <= max_exp;
                magnitude <= accum[32] ? -accum[31:0] : accum[31:0];
                negative <= accum[32];
                if (accum == 0) begin
                    value <= 0;
                    state <= DONE;
                end else
                    state <= NORMALIZE;
            end
            NORMALIZE: begin
                if (result_exp < -10'sd30) begin
                    result_exp <= 1;
                    magnitude <= 1;
                end else if (result_exp < -10'sd7) begin
                    result_exp <= result_exp + 10'sd8;
                    magnitude <= {8'b0, magnitude[31:9], |magnitude[8:0]};
                end else if (result_exp < 1 || magnitude[31:30] != 0) begin
                    result_exp <= result_exp + 10'sd1;
                    magnitude <= {1'b0, magnitude[31:2], |magnitude[1:0]};
                end else if (!magnitude[29] && result_exp > 1) begin
                    magnitude <= magnitude << 1;
                    result_exp <= result_exp - 10'sd1;
                end else
                    state <= ROUND;
            end
            ROUND: begin
                rounded_value <= {1'b0, magnitude[29:6]} +
                                 {24'b0, !toward_zero && magnitude[5] && ((|magnitude[4:0]) || magnitude[6])};
                state <= PACK;
            end
            PACK: begin
                if (packed_exp >= 255) begin
                    value <= {negative, toward_zero ? 31'h7f7f_ffff : 31'h7f80_0000};
                    causes[2] <= 1;
                end else begin
                    value <= {negative, packed_exp[7:0], tiny && flush_denormals ? 23'b0 : fraction};
                    causes[1] <= tiny && (flush_denormals || (|magnitude[5:0]));
                end
                state <= DONE;
            end
            DONE: state <= IDLE;
            default: state <= IDLE;
        endcase
    end
endmodule
