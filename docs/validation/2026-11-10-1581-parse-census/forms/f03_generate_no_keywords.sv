// genvar declared outside, loop at module level without generate/endgenerate.
module f03_generate_no_keywords (input logic [7:0] a, output logic [7:0] y, output logic ok);
    genvar i;
    for (i = 0; i < 8; i = i + 1) begin : g
        assign y[i] = ~a[i];
    end
    if (1) begin : g_if
        assign ok = (y == ~a);
    end
endmodule
