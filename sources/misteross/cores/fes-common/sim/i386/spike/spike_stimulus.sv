// SPDX-License-Identifier: MIT
// Shared envelope for the i386 timing spikes: reset from KEY0 and a register
// chain of pseudo-random bits, so no spike input is constant and nothing the
// spike measures can be optimised away. Each spike folds its results into a
// signature register that drives LED0.
module spike_stimulus #(
    parameter int WIDTH = 256
) (
    input  wire              clk,
    input  wire              key0_n,
    output logic             reset,
    output logic [WIDTH-1:0] noise
);
    logic        key_meta = 1'b1;
    logic [63:0] lfsr = 64'h9e3779b97f4a7c15;

    initial reset = 1'b1;
    always_ff @(posedge clk) begin
        key_meta <= !key0_n;
        reset <= key_meta;
        // x^64 + x^63 + x^61 + x^60 + 1
        lfsr <= {lfsr[62:0], lfsr[63] ^ lfsr[62] ^ lfsr[60] ^ lfsr[59]};
        noise <= {noise[WIDTH-2:0], lfsr[63]};
    end
endmodule
