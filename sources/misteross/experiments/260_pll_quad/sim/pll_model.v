// Simulation only: digital 25/50 MHz clocks and a lock delay. The 100 MHz
// output toggles on both reference edges (50 MHz). The 75 MHz accumulator is
// a 37.5 MHz stand-in. Neither models the analog ratio.
module altera_pll #(
    parameter reference_clock_frequency = "50.0 MHz",
    parameter number_of_clocks = 4,
    parameter output_clock_frequency0 = "25.0 MHz",
    parameter output_clock_frequency1 = "50.0 MHz",
    parameter output_clock_frequency2 = "100.0 MHz",
    parameter output_clock_frequency3 = "75.0 MHz",
    parameter phase_shift0 = "0 ps",
    parameter phase_shift1 = "0 ps",
    parameter phase_shift2 = "0 ps",
    parameter phase_shift3 = "0 ps",
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
    reg clk25 = 0;
    reg clk100 = 0;
    reg clk75 = 0;
    reg [3:0] acquire = 0;
    reg [2:0] acc75 = 0;
    assign outclk = {clk75, clk100, rst ? 1'b0 : refclk, clk25};
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
        if (rst) begin
            clk100 <= 0;
            clk75 <= 0;
            acc75 <= 0;
        end else begin
            clk100 <= !clk100;
            if (acc75 + 3 >= 4) begin
                acc75 <= acc75 + 3 - 4;
                clk75 <= !clk75;
            end else
                acc75 <= acc75 + 3;
        end
endmodule
