// A generate if at module level without generate/endgenerate, alone.
module f03b_generate_if_no_keywords #(parameter bit SEL = 1) (input logic [7:0] a, output logic [7:0] y, output logic ok);
    if (SEL) begin : g_on
        assign y = ~a;
    end else begin : g_off
        assign y = a;
    end
    assign ok = (y == ~a);
endmodule
