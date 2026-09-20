// Simulation only: a digital 25 MHz clock and lock delay, not analog duty-cycle behavior.
module altera_pll #(
    parameter reference_clock_frequency = "50.0 MHz",
    parameter number_of_clocks = 1,
    parameter output_clock_frequency0 = "25.0 MHz",
    parameter phase_shift0 = "0 ps",
    parameter duty_cycle0 = 25,
    parameter operation_mode = "direct",
    parameter fractional_vco_multiplier = "false"
) (
    input wire refclk, rst,
    output reg outclk = 0,
    output reg locked = 0
);
    reg [3:0] acquire = 0;
    always @(posedge refclk or posedge rst)
        if (rst) begin
            outclk <= 0;
            locked <= 0;
            acquire <= 0;
        end else begin
            outclk <= !outclk;
            if (acquire == 15)
                locked <= 1;
            else
                acquire <= acquire + 1'b1;
        end
endmodule
