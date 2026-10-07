// SPDX-License-Identifier: GPL-2.0-or-later
// Keep each rate's pattern counts on screen across the next re-init.
// mem_channel raises pass/fail and the total one cycle before the last
// pattern_errors increment, so this copy waits one extra clock. Copying on
// the pass/fail edge drops a mismatch on the final word and the total is
// then one higher than the pattern table.
module pattern_latch (
    input wire clk,
    input wire reset,
    input wire [1:0] rate,
    input wire pass,
    input wire fail,
    input wire [191:0] counts,
    output reg [191:0] pat50 = 192'd0,
    output reg [191:0] pat75 = 192'd0,
    output reg [191:0] pat100 = 192'd0,
    output reg [2:0] pat_ok = 3'd0
);
    reg seen = 1'b0;
    reg hold = 1'b0;

    always @(posedge clk) begin
        if (reset) begin
            seen <= 1'b0;
            hold <= 1'b0;
        end else if (hold) begin
            hold <= 1'b0;
            seen <= 1'b1;
            case (rate)
                2'd0: begin
                    pat50 <= counts;
                    pat_ok[0] <= 1'b1;
                end
                2'd1: begin
                    pat75 <= counts;
                    pat_ok[1] <= 1'b1;
                end
                default: begin
                    pat100 <= counts;
                    pat_ok[2] <= 1'b1;
                end
            endcase
        end else if ((pass || fail) && !seen)
            hold <= 1'b1;
    end
endmodule
