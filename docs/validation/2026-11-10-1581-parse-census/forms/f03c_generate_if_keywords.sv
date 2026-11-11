// f03b's generate if, inside generate/endgenerate.
module f03c_generate_if_keywords #(parameter bit SEL = 1) (input logic [7:0] a, output logic [7:0] y, output logic ok);
    generate
        if (SEL) begin : g_on
            assign y = ~a;
        end else begin : g_off
            assign y = a;
        end
    endgenerate
    assign ok = (y == ~a);
endmodule
