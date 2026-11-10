// (* no_carry *) on a +, the fes_i386_qalign.sv:162 form.
module f08_no_carry (input logic [3:0] a, input logic [3:0] b, output logic [4:0] y, output logic ok);
    assign y = 5'(a) + (* no_carry *) 5'(b);
    assign ok = (y == 5'(a) + 5'(b));
endmodule
