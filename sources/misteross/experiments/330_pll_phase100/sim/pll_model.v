// Simulation only: 100 MHz outputs are 50 MHz stand-ins. Analog 2.5 ns
// 90/270 phase is not modeled.
module altera_pll #(
    parameter reference_clock_frequency = "50.0 MHz",
    parameter number_of_clocks = 4,
    parameter output_clock_frequency0 = "100.0 MHz",
    parameter output_clock_frequency1 = "100.0 MHz",
    parameter output_clock_frequency2 = "100.0 MHz",
    parameter output_clock_frequency3 = "100.0 MHz",
    parameter phase_shift0 = "0 ps",
    parameter phase_shift1 = "2500 ps",
    parameter phase_shift2 = "5000 ps",
    parameter phase_shift3 = "7500 ps",
    parameter duty_cycle0 = 50,
    parameter duty_cycle1 = 50,
    parameter duty_cycle2 = 50,
    parameter duty_cycle3 = 50,
    parameter operation_mode = "direct",
    parameter fractional_vco_multiplier = "false"
) (
    input wire refclk,
    input wire rst,
    output wire [3:0] outclk,
    output reg locked = 0
);
    reg [3:0] acquire = 0;
    assign outclk = rst ? 4'b0 : {~refclk, ~refclk, refclk, refclk};
    always @(posedge refclk or posedge rst)
        if (rst) begin
            locked <= 0;
            acquire <= 0;
        end else if (acquire == 15)
            locked <= 1;
        else
            acquire <= acquire + 1'b1;
endmodule
