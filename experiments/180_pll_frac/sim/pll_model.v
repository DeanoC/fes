// Simulation only: a digital 12.288/50 ratio and lock delay, not analog PLL behavior.
module altera_pll #(
    parameter reference_clock_frequency = "50.0 MHz",
    parameter number_of_clocks = 1,
    parameter output_clock_frequency0 = "12.288 MHz",
    parameter phase_shift0 = "0 ps",
    parameter duty_cycle0 = 50,
    parameter operation_mode = "direct",
    parameter fractional_vco_multiplier = "true"
) (
    input wire refclk, rst,
    output reg outclk = 0,
    output reg locked = 0
);
    reg [3:0] acquire = 0;
    reg [15:0] acc = 0;
    always @(posedge refclk or posedge rst)
        if (rst) begin
            outclk <= 0;
            locked <= 0;
            acquire <= 0;
            acc <= 0;
        end else begin
            if (acc + 16'd12288 >= 16'd25000) begin
                acc <= acc + 16'd12288 - 16'd25000;
                outclk <= !outclk;
            end else
                acc <= acc + 16'd12288;
            if (acquire == 15)
                locked <= 1;
            else
                acquire <= acquire + 1'b1;
        end
endmodule
