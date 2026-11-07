// SPDX-License-Identifier: GPL-3.0-or-later
// ZX81 crystal timing from rational enables in the shell clock domain. The 6.5 MHz
// oscillator drives the ULA and is divided by two for the Z80 edge connector.
// 6.5 / 52.224 = 1625 / 13056 exactly; events are eight or nine host cycles
// apart. DE25 uses 26/297 at 74.25 MHz. Keep clocks as enables rather than
// creating a fabric clock domain. Require 0 < NUMERATOR < DENOMINATOR.
module zx81_machine_clock #(
    parameter integer NUMERATOR = 1625,
    parameter integer DENOMINATOR = 13056
) (
    input  wire clk_sys,
    input  wire reset,
    output reg  ce_6m5 = 1'b0,
    output reg  ce_cpu_p = 1'b0,
    output reg  ce_cpu_n = 1'b0,
    output reg  cpu_clock = 1'b0
);
    localparam integer PHASE_WIDTH = $clog2(DENOMINATOR);
    localparam [PHASE_WIDTH:0] STEP = NUMERATOR[PHASE_WIDTH:0];
    localparam [PHASE_WIDTH:0] MODULUS = DENOMINATOR[PHASE_WIDTH:0];
    reg [PHASE_WIDTH-1:0] phase = 0;
    wire [PHASE_WIDTH:0] advanced = {1'b0, phase} + STEP;
    wire [PHASE_WIDTH:0] wrapped = advanced - MODULUS;

    // Prepare enables half a host cycle before the machine consumes them.
    always @(negedge clk_sys) begin
        ce_6m5 <= 1'b0;
        ce_cpu_p <= 1'b0;
        ce_cpu_n <= 1'b0;
        if (reset) begin
            phase <= 0;
            cpu_clock <= 1'b0;
        end else if (advanced >= MODULUS) begin
            phase <= wrapped[PHASE_WIDTH-1:0];
            ce_6m5 <= 1'b1;
            ce_cpu_p <= !cpu_clock;
            ce_cpu_n <= cpu_clock;
            cpu_clock <= !cpu_clock;
        end else begin
            phase <= advanced[PHASE_WIDTH-1:0];
        end
    end
endmodule
