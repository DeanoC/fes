// SPDX-License-Identifier: GPL-3.0-or-later
// ZX81 crystal timing in the 52.224 MHz shell domain. The original 6.5 MHz
// oscillator drives the ULA and is divided by two for the Z80 edge connector.
// 6.5 / 52.224 = 1625 / 13056 exactly; events are eight or nine host cycles
// apart. Keep clocks as enables rather than creating a fabric clock domain.
module zx81_machine_clock (
    input  wire clk_sys,
    input  wire reset,
    output reg  ce_6m5 = 1'b0,
    output reg  ce_cpu_p = 1'b0,
    output reg  ce_cpu_n = 1'b0,
    output reg  cpu_clock = 1'b0
);
    reg [13:0] phase = 14'd0;
    wire [14:0] advanced = {1'b0, phase} + 15'd1625;
    wire [14:0] wrapped = advanced - 15'd13056;

    // Prepare enables half a host cycle before the machine consumes them.
    always @(negedge clk_sys) begin
        ce_6m5 <= 1'b0;
        ce_cpu_p <= 1'b0;
        ce_cpu_n <= 1'b0;
        if (reset) begin
            phase <= 14'd0;
            cpu_clock <= 1'b0;
        end else if (advanced >= 15'd13056) begin
            phase <= wrapped[13:0];
            ce_6m5 <= 1'b1;
            ce_cpu_p <= !cpu_clock;
            ce_cpu_n <= cpu_clock;
            cpu_clock <= !cpu_clock;
        end else begin
            phase <= advanced[13:0];
        end
    end
endmodule
