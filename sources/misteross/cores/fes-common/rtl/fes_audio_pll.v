// SPDX-License-Identifier: GPL-2.0-or-later
module fes_audio_pll(input wire refclk, output wire clk, output wire locked);
    altera_pll #(
        .reference_clock_frequency("50.0 MHz"), .number_of_clocks(1),
        .output_clock_frequency0("12.288 MHz"), .phase_shift0("0 ps"),
        .duty_cycle0(50), .operation_mode("direct"),
        .fractional_vco_multiplier("true")
    ) pll (.refclk(refclk), .rst(1'b0), .outclk(clk), .locked(locked));
endmodule
