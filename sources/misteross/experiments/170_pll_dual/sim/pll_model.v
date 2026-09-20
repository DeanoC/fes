// Simulation only: digital 25/40 MHz clocks and a lock delay, not analog PLL behavior.
module altera_pll #(
    parameter reference_clock_frequency = "50.0 MHz",
    parameter number_of_clocks = 2,
    parameter output_clock_frequency0 = "25.0 MHz",
    parameter output_clock_frequency1 = "40.0 MHz",
    parameter phase_shift0 = "0 ps",
    parameter phase_shift1 = "0 ps",
    parameter duty_cycle0 = 50,
    parameter duty_cycle1 = 50,
    parameter operation_mode = "direct",
    parameter fractional_vco_multiplier = "false"
) (
    input wire refclk,
    input wire rst,
    output wire [1:0] outclk,
    output reg locked = 0
);
    reg clk25 = 0;
    reg clk40 = 0;
    reg [3:0] acquire = 0;
    reg [3:0] acc40 = 0;
    assign outclk = {clk40, clk25};
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
    // 4 toggles per 5 half-cycles of 50 MHz is 40 MHz.
    always @(posedge refclk or negedge refclk or posedge rst)
        if (rst) begin
            clk40 <= 0;
            acc40 <= 0;
        end else if (acc40 + 4 >= 5) begin
            acc40 <= acc40 + 4 - 5;
            clk40 <= !clk40;
        end else
            acc40 <= acc40 + 4;
endmodule
