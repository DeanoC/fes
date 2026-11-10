package p;
    typedef enum logic [1:0] {A, B, C} e_t;
endpackage

module top import p::*; (input logic [1:0] sel, output e_t y);
    assign y = e_t'(sel);
endmodule
