// Simulation only: the pin is treated as 25 MHz. 50/100 MHz outputs use a
// both-edge 25 MHz stand-in; they do not model analog ratios or pulse width.
module altera_pll #(
    parameter reference_clock_frequency = "25.0 MHz",
    parameter number_of_clocks = 3,
    parameter output_clock_frequency0 = "25.0 MHz",
    parameter output_clock_frequency1 = "50.0 MHz",
    parameter output_clock_frequency2 = "100.0 MHz",
    parameter phase_shift0 = "0 ps",
    parameter phase_shift1 = "0 ps",
    parameter phase_shift2 = "0 ps",
    parameter duty_cycle0 = 50,
    parameter duty_cycle1 = 50,
    parameter duty_cycle2 = 50,
    parameter operation_mode = "direct",
    parameter fractional_vco_multiplier = "false"
) (
    input wire refclk,
    input wire rst,
    output wire [2:0] outclk,
    output reg locked = 0
);
    reg clk50 = 0;
    reg [3:0] acquire = 0;
    assign outclk = {clk50, clk50, rst ? 1'b0 : refclk};
    always @(posedge refclk or posedge rst)
        if (rst) begin
            locked <= 0;
            acquire <= 0;
        end else if (acquire == 15)
            locked <= 1;
        else
            acquire <= acquire + 1'b1;
    always @(posedge refclk or negedge refclk or posedge rst)
        if (rst)
            clk50 <= 0;
        else
            clk50 <= !clk50;
endmodule
