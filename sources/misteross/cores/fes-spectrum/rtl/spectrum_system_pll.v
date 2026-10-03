// SPDX-License-Identifier: GPL-2.0-or-later
// Normal: paired 52.224/12.288 MHz fractional PLL. Development fast mode:
// one 56 MHz PLL; fast audio is scheduled on this clock without a third PLL.
module spectrum_system_pll #(parameter bit FAST_CPU = 1'b0) (
    input wire refclk, rst,
    output wire outclk_0, audio_clk, locked
);
    generate if (FAST_CPU) begin : fast
        assign audio_clk = outclk_0;  // fast audio uses rational enables on clk_sys
        altera_pll #(
            .reference_clock_frequency("50.0 MHz"), .number_of_clocks(1),
            .output_clock_frequency0("56.0 MHz"), .phase_shift0("0 ps"),
            .duty_cycle0(50), .operation_mode("direct"), .fractional_vco_multiplier("true")
        ) system_pll (.refclk(refclk), .rst(rst), .outclk(outclk_0), .locked(locked));
    end else begin : faithful
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
    end endgenerate
endmodule
