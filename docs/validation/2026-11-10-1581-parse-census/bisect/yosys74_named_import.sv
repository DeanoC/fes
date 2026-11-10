package p;
    typedef enum logic [1:0] {A, B, C} e_t;
endpackage

module top import p::e_t; (input logic [1:0] sel, output e_t y);
    always_comb begin
        for (int k = 0; k < 2; k++) ;
    end
    assign y = e_t'(sel);
endmodule
