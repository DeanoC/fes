// Simulation only: digital 25 MHz quadrature from 50 MHz edges, not analog phase.
module altera_pll #(
    parameter reference_clock_frequency = "50.0 MHz",
    parameter number_of_clocks = 4,
    parameter output_clock_frequency0 = "25.0 MHz",
    parameter output_clock_frequency1 = "25.0 MHz",
    parameter output_clock_frequency2 = "25.0 MHz",
    parameter output_clock_frequency3 = "25.0 MHz",
    parameter phase_shift0 = "0 ps",
    parameter phase_shift1 = "10000 ps",
    parameter phase_shift2 = "20000 ps",
    parameter phase_shift3 = "30000 ps",
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
    reg phase0 = 0;
    reg phase90 = 0;
    reg phase180 = 1;
    reg phase270 = 1;
    reg [3:0] acquire = 0;
    assign outclk = {phase270, phase180, phase90, phase0};
    always @(posedge refclk or posedge rst)
        if (rst) begin
            phase0 <= 0;
            phase180 <= 1;
            locked <= 0;
            acquire <= 0;
        end else begin
            phase0 <= !phase0;
            phase180 <= !phase180;
            if (acquire == 15)
                locked <= 1;
            else
                acquire <= acquire + 1'b1;
        end
    always @(negedge refclk or posedge rst)
        if (rst) begin
            phase90 <= 0;
            phase270 <= 1;
        end else begin
            phase90 <= !phase90;
            phase270 <= !phase270;
        end
endmodule
