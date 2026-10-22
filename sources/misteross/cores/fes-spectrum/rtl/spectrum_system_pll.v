// SPDX-License-Identifier: GPL-2.0-or-later
// 50 MHz DE10-Nano reference to the paired 52.224 MHz system and 12.288 MHz
// HDMI audio clocks, and to the 74.25 MHz HDMI pixel clock. Every model
// derives its CPU T state from the system clock by rational enables.
module spectrum_system_pll (
    input  wire refclk,
    input  wire rst,
    output wire outclk_0,
    output wire audio_clk,
    output wire locked
);
    wire [1:0] clocks;
    assign outclk_0 = clocks[0];
    assign audio_clk = clocks[1];
    altera_pll #(
        .reference_clock_frequency("50.0 MHz"), .number_of_clocks(2),
        .output_clock_frequency0("52.224 MHz"), .output_clock_frequency1("12.288 MHz"),
        .phase_shift0("0 ps"), .phase_shift1("0 ps"),
        .duty_cycle0(50), .duty_cycle1(50), .operation_mode("direct"),
        .fractional_vco_multiplier("true")
    ) pll (.refclk(refclk), .rst(rst), .outclk(clocks), .locked(locked));
endmodule

module spectrum_pixel_pll (
    input  wire refclk,
    input  wire rst,
    output wire outclk_0,
    output wire locked
);
    altera_pll #(
        .reference_clock_frequency("50.0 MHz"), .number_of_clocks(1),
        .output_clock_frequency0("74.25 MHz"), .phase_shift0("0 ps"),
        .duty_cycle0(50), .operation_mode("direct"), .fractional_vco_multiplier("true")
    ) pll (.refclk(refclk), .rst(rst), .outclk(outclk_0), .locked(locked));
endmodule
