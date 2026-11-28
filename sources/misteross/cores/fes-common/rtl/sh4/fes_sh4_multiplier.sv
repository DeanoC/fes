// Split into four DSP-sized products. Register the products and middle sum
// before producing the 64-bit result.
module fes_sh4_multiplier (
    input  logic        clk,
    input  logic        launch,
    input  logic        sum_products,
    input  logic        finish,
    input  logic        word_operands,
    input  logic        signed_operands,
    input  logic [31:0] lhs, rhs,
    output logic [63:0] product
);
    logic signed [16:0] lo_a, lo_b, hi_a, hi_b;
    (* multstyle = "dsp" *) logic signed [33:0] lh, hl;
    (* multstyle = "dsp" *) logic [31:0] ll, hh;
    logic signed [34:0] middle;
    logic [31:0] low_product, high_product;
    logic [16:0] low_mid_sum;

    assign lo_a = $signed({word_operands && signed_operands && lhs[15], lhs[15:0]});
    assign lo_b = $signed({word_operands && signed_operands && rhs[15], rhs[15:0]});
    assign hi_a = word_operands ? 17'sb0 : $signed({signed_operands && lhs[31], lhs[31:16]});
    assign hi_b = word_operands ? 17'sb0 : $signed({signed_operands && rhs[31], rhs[31:16]});
    assign low_mid_sum = {1'b0, low_product[31:16]} + {1'b0, middle[15:0]};

    always_ff @(posedge clk) begin
        if (launch) begin
            ll <= lo_a * lo_b;
            lh <= lo_a * hi_b;
            hl <= hi_a * lo_b;
            hh <= hi_a * hi_b;
        end
        if (sum_products) begin
            middle <= lh + hl;
            low_product <= ll[31:0];
            high_product <= hh[31:0];
        end
        if (finish) begin
            product[31:0] <= {low_mid_sum[15:0], low_product[15:0]};
            product[63:32] <= high_product + {{13{middle[34]}}, middle[34:16]} +
                              {31'b0, low_mid_sum[16]};
        end
    end
endmodule
