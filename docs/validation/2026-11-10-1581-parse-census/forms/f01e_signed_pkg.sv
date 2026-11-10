// opl2_pkg.sv:79 form: signed'() inside a package localparam. OUT_W 12, SAMPLE_W 16, so DAC_SHIFT is 0.
module f01e_signed_pkg import census_pkg::*; (output logic ok);
    assign ok = (DAC_SHIFT == 0);
endmodule
