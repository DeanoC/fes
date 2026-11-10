// The opl2_pkg.sv:79 localparam with OPL2's real widths (DAC_OUTPUT_WIDTH 16, SAMPLE_WIDTH 16),
// used as dac_prep.sv:59 uses it: sample <<< DAC_LEFT_SHIFT. The right value is 0, so y = x.
// x is the constant 16'h1234 so y folds to a constant Quartus reports pin by pin.
module f01g_signed_pkg_shift (output logic signed [15:0] y, output logic ok);
    localparam DAC_OUTPUT_WIDTH = 16;
    localparam SAMPLE_WIDTH = 16;
    localparam DAC_LEFT_SHIFT = signed'(DAC_OUTPUT_WIDTH - SAMPLE_WIDTH - 2) < 0 ? 0 : DAC_OUTPUT_WIDTH - SAMPLE_WIDTH - 3;
    localparam logic signed [15:0] X = 16'sh1234;
    assign y = X <<< DAC_LEFT_SHIFT;
    assign ok = (y == X);
endmodule
