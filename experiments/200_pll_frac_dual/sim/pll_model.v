// Simulation only: digital 12.288/24.576 MHz clocks and a lock delay, not analog PLL behavior.
module altera_pll #(
    parameter reference_clock_frequency = "50.0 MHz",
    parameter number_of_clocks = 2,
    parameter output_clock_frequency0 = "12.288 MHz",
    parameter output_clock_frequency1 = "24.576 MHz",
    parameter phase_shift0 = "0 ps",
    parameter phase_shift1 = "0 ps",
    parameter duty_cycle0 = 50,
    parameter duty_cycle1 = 50,
    parameter operation_mode = "direct",
    parameter fractional_vco_multiplier = "true"
) (
    input wire refclk,
    input wire rst,
    output wire [1:0] outclk,
    output reg locked = 0
);
    reg clk12288 = 0;
    reg clk24576 = 0;
    reg [3:0] acquire = 0;
    reg [15:0] acc12288 = 0;
    reg [15:0] acc24576 = 0;
    assign outclk = {clk24576, clk12288};
    always @(posedge refclk or posedge rst)
        if (rst) begin
            clk12288 <= 0;
            clk24576 <= 0;
            locked <= 0;
            acquire <= 0;
            acc12288 <= 0;
            acc24576 <= 0;
        end else begin
            if (acc12288 + 16'd12288 >= 16'd25000) begin
                acc12288 <= acc12288 + 16'd12288 - 16'd25000;
                clk12288 <= !clk12288;
            end else
                acc12288 <= acc12288 + 16'd12288;
            if (acc24576 + 16'd24576 >= 16'd25000) begin
                acc24576 <= acc24576 + 16'd24576 - 16'd25000;
                clk24576 <= !clk24576;
            end else
                acc24576 <= acc24576 + 16'd24576;
            if (acquire == 15)
                locked <= 1;
            else
                acquire <= acquire + 1'b1;
        end
endmodule
