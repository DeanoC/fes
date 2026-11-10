package p;
    typedef enum logic [1:0] {A, B, C} e_t;
endpackage

module top (input logic [1:0] sel, output p::e_t y);
    always_comb begin
        for (int k = 0; k < 2; k++) ;
    end
    assign y = p::e_t'(sel);
endmodule
