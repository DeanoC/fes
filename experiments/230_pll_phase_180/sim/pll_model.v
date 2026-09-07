// Simulation only: digital 25 MHz clocks 20 ns apart, not analog phase behavior.
module altera_pll #(
    parameter reference_clock_frequency = "50.0 MHz",
    parameter number_of_clocks = 2,
    parameter output_clock_frequency0 = "25.0 MHz",
    parameter output_clock_frequency1 = "25.0 MHz",
    parameter phase_shift0 = "0 ps",
    parameter phase_shift1 = "20000 ps",
    parameter duty_cycle0 = 50,
    parameter duty_cycle1 = 50,
    parameter operation_mode = "direct",
    parameter fractional_vco_multiplier = "false"
) (
    input wire refclk, rst,
    output wire [1:0] outclk,
    output reg locked = 0
);
    reg phase0 = 0;
    reg phase180 = 1;
    reg [3:0] acquire = 0;
    assign outclk = {phase180, phase0};
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
endmodule
