// ksl_add_rom.sv:86 form: signed'() on an unsigned expression assigned in always_comb.
module f01d_signed_ksl (input logic [6:0] rom, input logic signed [4:0] t0, output logic signed [8:0] y, output logic ok);
    always_comb y = signed'(rom + (t0 << 3));
    assign ok = (y == $signed(rom + (t0 << 3)));
endmodule
