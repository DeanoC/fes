// calc_phase_inc.sv:70 form: a size cast of signed'() plus a signed operand.
module f01b_signed_phase_inc (input logic [9:0] fnum, input logic signed [6:0] vib, input logic en,
                              output logic [10:0] y, output logic ok);
    localparam W = 11;
    always_comb y = en ? W'(signed'({1'b0, fnum}) + vib) : {1'b0, fnum};
    assign ok = (y == (en ? 11'($signed({1'b0, fnum}) + vib) : {1'b0, fnum}));
endmodule
