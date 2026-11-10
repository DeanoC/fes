// f01e's package localparam, made visible: v carries DAC_SHIFT's low 8 bits (0 expected).
// c is a control without the cast: the same comparison on int localparams (1 expected).
module f01f_signed_pkg_value import census_pkg::*; (output logic [7:0] v, output logic c, output logic ok);
    assign v = 8'(DAC_SHIFT);
    assign c = (OUT_W - SAMPLE_W - 2) < 0;
    assign ok = (DAC_SHIFT == 0);
endmodule
