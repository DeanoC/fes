// SPDX-License-Identifier: GPL-2.0-or-later
// Memory and capture clocks for the Quartus and OSS SDRAM diagnostics.
module ram_pll (
    input wire refclk,
    input wire rst,
    output wire outclk_0,
    output wire outclk_1,
    output wire outclk_2,
    output wire locked
);
`ifdef RAM_OSS_HIGH_SPEED
    wire [1:0] clocks;
    assign outclk_0 = clocks[0];
    assign outclk_1 = clocks[0];
    assign outclk_2 = clocks[1];
    altera_pll #(
        .reference_clock_frequency("50.0 MHz"),
        .number_of_clocks(2),
`ifdef RAM_100_ONLY
        .output_clock_frequency0("100.0 MHz"),
        .output_clock_frequency1("100.0 MHz"),
        .phase_shift1("5000 ps"),
`else
        .output_clock_frequency0("130.0 MHz"),
        .output_clock_frequency1("130.0 MHz"),
        .phase_shift1("6538 ps"),
`endif
        .phase_shift0("0 ps"),
        .duty_cycle0(50),
        .duty_cycle1(50),
        .operation_mode("direct"),
        .fractional_vco_multiplier("false")
    ) pll (
        .refclk(refclk), .rst(rst), .outclk(clocks), .locked(locked)
    );
`else
    wire [2:0] clocks;
    assign outclk_0 = clocks[0];
    assign outclk_1 = clocks[1];
    assign outclk_2 = clocks[2];

    altera_pll #(
        .reference_clock_frequency("50.0 MHz"),
        .number_of_clocks(3),
`ifdef RAM_100_ONLY
        .output_clock_frequency0("75.0 MHz"),
`else
        .output_clock_frequency0("130.0 MHz"),
`endif
        .output_clock_frequency1("100.0 MHz"),
`ifdef RAM_100_ONLY
        .output_clock_frequency2("100.0 MHz"),
`else
        .output_clock_frequency2("130.0 MHz"),
`endif
        // The capture clock is shifted from the phase-zero SDRAM pin clock.
        .phase_shift0("0 ps"),
        .phase_shift1("0 ps"),
`ifdef RAM_100_ONLY
        .phase_shift2("417 ps"),
`else
        .phase_shift2("2692 ps"),
`endif
        .duty_cycle0(50),
        .duty_cycle1(50),
        .duty_cycle2(50),
        .operation_mode("direct"),
        .fractional_vco_multiplier("false")
    ) pll (
        .refclk(refclk),
        .rst(rst),
        .outclk(clocks),
        .locked(locked)
    );
`endif
endmodule
