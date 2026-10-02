// SPDX-License-Identifier: GPL-2.0-or-later
// NTSC Z80 clock enables: alternating half cycles at exactly 3,579,545 Hz.
// Sega's official hardware manual and Coleco's technical manual specify this
// CPU clock. Keep phase running during machine reset, like the board oscillator.
module fes_z80_ce #(
    parameter integer SYSTEM_CLOCK_HZ = 52_224_000,
    parameter integer CPU_CLOCK_HZ = 3_579_545
) (
    input wire clk,
    output reg positive = 0,
    output reg negative = 0
);
    localparam integer PHASE_WIDTH = $clog2(SYSTEM_CLOCK_HZ);
    localparam [PHASE_WIDTH:0] MODULUS = (PHASE_WIDTH+1)'(SYSTEM_CLOCK_HZ);
    localparam [PHASE_WIDTH:0] INCREMENT = (PHASE_WIDTH+1)'(2 * CPU_CLOCK_HZ);
    reg [PHASE_WIDTH-1:0] phase = 0;
    reg polarity = 0;
    wire [PHASE_WIDTH:0] next_phase = {1'b0, phase} + INCREMENT;
    wire [PHASE_WIDTH-1:0] wrapped_phase = PHASE_WIDTH'(next_phase - MODULUS);
    // Publish before the CPU's rising system-clock edge, matching T80pa's
    // existing CEN_p/CEN_n interface without introducing another clock domain.
    always @(negedge clk) begin
        positive <= 0;
        negative <= 0;
        if (next_phase >= MODULUS) begin
            phase <= wrapped_phase;
            positive <= !polarity;
            negative <= polarity;
            polarity <= !polarity;
        end else phase <= next_phase[PHASE_WIDTH-1:0];
    end
endmodule
