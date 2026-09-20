// Simulation only: a digital toggling clock and lock=!rst.
// It does not model analog lock or the 50-to-52 MHz ratio.
module altera_pll #(
    parameter reference_clock_frequency = "50.0 MHz",
    parameter number_of_clocks = 1,
    parameter output_clock_frequency0 = "52.0 MHz",
    parameter phase_shift0 = "0 ps",
    parameter duty_cycle0 = 50,
    parameter operation_mode = "direct",
    parameter fractional_vco_multiplier = "false"
) (
    input wire refclk, rst,
    output reg outclk = 0,
    output wire locked
);
    assign locked = !rst;
    always @(posedge refclk)
        if (rst) outclk <= 0;
        else outclk <= !outclk;
endmodule
