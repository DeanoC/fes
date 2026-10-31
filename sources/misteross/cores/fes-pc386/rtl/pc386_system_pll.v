// SPDX-License-Identifier: MIT
// 50 MHz DE10-Nano reference to the pc386 system clock and the 12.288 MHz
// HDMI audio clock. The producer chooses SYSTEM_CLOCK; every guest time base
// is a rational enable of it.
module pc386_system_pll #(
    parameter SYSTEM_CLOCK = "61.44 MHz"
) (
    input wire refclk,
    output wire system_clk, audio_clk, locked
);
    wire [1:0] clocks;
    assign system_clk = clocks[0];
    assign audio_clk = clocks[1];
    altera_pll #(
        .reference_clock_frequency("50.0 MHz"), .number_of_clocks(2),
        .output_clock_frequency0(SYSTEM_CLOCK), .output_clock_frequency1("12.288 MHz"),
        .phase_shift0("0 ps"), .phase_shift1("0 ps"),
        .duty_cycle0(50), .duty_cycle1(50), .operation_mode("direct"),
        .fractional_vco_multiplier("true")
    ) pll (.refclk(refclk), .rst(1'b0), .outclk(clocks), .locked(locked));
endmodule
