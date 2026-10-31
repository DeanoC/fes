// SPDX-License-Identifier: MIT
// pc386 snapshot (docs/pc386-machine-design.md, phase M4): a value kept in
// one clock, read whole in another. The reader asks with a toggle; the source
// copies the value and toggles its answer, and the reader takes the copy once
// that answer has crossed, so the copy has been still for two of the reader's
// clocks. The reader asks again at once, so its snapshot trails the value by a
// few clocks of each. No reset is used; if the source clock stops, the
// snapshot keeps the last copy.
module pc386_snapshot #(
    parameter integer W = 32
) (
    input wire source_clk,
    input wire [W-1:0] value,
    input wire clk,
    output reg [W-1:0] snapshot
);
    reg ask = 1'b0, answer = 1'b0;
    (* async_reg = "true" *) reg [1:0] ask_sync = 2'b00;
    (* async_reg = "true" *) reg [1:0] answer_sync = 2'b00;
    reg [W-1:0] copy = {W{1'b0}};
    initial snapshot = {W{1'b0}};

    always @(posedge source_clk) begin
        ask_sync <= {ask_sync[0], ask};
        if (ask_sync[1] != answer) begin
            copy <= value;
            answer <= ask_sync[1];
        end
    end

    always @(posedge clk) begin
        answer_sync <= {answer_sync[0], answer};
        if (answer_sync[1] == ask) begin
            snapshot <= copy;
            ask <= !ask;
        end
    end
endmodule
