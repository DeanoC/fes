// SPDX-License-Identifier: GPL-2.0-or-later
// Two V11-reachable PLLs are available: video uses one; this shared 417.792
// MHz fractional VCO supplies system C8 and exact 48 kHz audio MCLK C34.
module coleco_system_pll (
    input wire refclk, rst,
    output wire outclk_0, audio_clk, locked
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
