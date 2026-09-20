// SPDX-License-Identifier: GPL-2.0-or-later
// 50 MHz DE10-Nano reference to the ColecoVision 52 MHz system clock.

module sys_pll (
    input  wire refclk,
    input  wire rst,
    output wire outclk_0
);
    wire locked;

    altera_pll #(
        .reference_clock_frequency("50.0 MHz"),
        .number_of_clocks(1),
        .output_clock_frequency0("52.0 MHz"),
        .phase_shift0("0 ps"),
        .duty_cycle0(50),
        .operation_mode("direct"),
        .fractional_vco_multiplier("false")
    ) pll (
        .refclk(refclk),
        .rst(rst),
        .outclk(outclk_0),
        .locked(locked)
    );
endmodule
