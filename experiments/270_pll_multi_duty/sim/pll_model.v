// Simulation only: digital 25/50 MHz clocks and a lock delay. The 100 MHz
// output toggles on both reference edges (50 MHz); it does not model the
// analog 100 MHz ratio or 25% pulse width.
module altera_pll #(
    parameter reference_clock_frequency = "50.0 MHz",
    parameter number_of_clocks = 3,
    parameter output_clock_frequency0 = "25.0 MHz",
    parameter output_clock_frequency1 = "50.0 MHz",
    parameter output_clock_frequency2 = "100.0 MHz",
    parameter phase_shift0 = "0 ps",
    parameter phase_shift1 = "0 ps",
    parameter phase_shift2 = "0 ps",
    parameter duty_cycle0 = 25,
    parameter duty_cycle1 = 50,
    parameter duty_cycle2 = 25,
    parameter operation_mode = "direct",
    parameter fractional_vco_multiplier = "false"
) (
    input wire refclk,
    input wire rst,
    output wire [2:0] outclk,
    output reg locked = 0
);
    reg clk25 = 0;
    reg clk100 = 0;
    reg [3:0] acquire = 0;
    assign outclk = {clk100, rst ? 1'b0 : refclk, clk25};
    always @(posedge refclk or posedge rst)
        if (rst) begin
            clk25 <= 0;
            locked <= 0;
            acquire <= 0;
        end else begin
            clk25 <= !clk25;
            if (acquire == 15)
                locked <= 1;
            else
                acquire <= acquire + 1'b1;
        end
    always @(posedge refclk or negedge refclk or posedge rst)
        if (rst)
            clk100 <= 0;
        else
            clk100 <= !clk100;
endmodule
