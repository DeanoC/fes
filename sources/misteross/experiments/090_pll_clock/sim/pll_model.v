// Simulation only: the supported integer ratio, without analog lock behavior.
module altera_pll #(
    parameter reference_clock_frequency = "50.0 MHz",
    parameter number_of_clocks = 1,
    parameter output_clock_frequency0 = "25.0 MHz",
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
