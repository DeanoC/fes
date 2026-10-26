// SPDX-License-Identifier: MIT
// Rational clock enable (docs/pc386-machine-design.md, "Clocks and timing"):
// tick fires N times every D system clocks, at most once per clock. The
// producer derives N/D from the sealed clock, so guest rates never depend on it.
module pc386_rate #(
    parameter integer N = 1,
    parameter integer D = 2
) (
    input wire clk, reset,
    output reg tick
);
    localparam integer WIDTH = $clog2(D + N + 1);
    reg [WIDTH-1:0] accumulator;
    wire [WIDTH-1:0] sum = accumulator + WIDTH'(N);
    always @(posedge clk) begin
        if (reset) begin
            accumulator <= 0;
            tick <= 0;
        end else if (sum >= WIDTH'(D)) begin
            accumulator <= sum - WIDTH'(D);
            tick <= 1;
        end else begin
            accumulator <= sum;
            tick <= 0;
        end
    end
    initial if (N <= 0 || N >= D) $error("pc386_rate needs 0 < N < D");
endmodule
