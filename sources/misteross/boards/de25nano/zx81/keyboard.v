// SPDX-License-Identifier: GPL-2.0-or-later
// Two physical active-low buttons: KEY0 reset and KEY1 ZX81 NEWLINE.
module de25_zx81_keys #(
    parameter integer STABLE_CYCLES = 522240
) (
    input wire clk,
    input wire reset_n,
    input wire [1:0] key_n,
    output reg [1:0] stable_n
);
    // Only key_meta receives the asynchronous pad values. The debounce logic
    // sees the second stage; stage-to-stage setup/hold remains timed.
    (* preserve, dont_merge *) reg [1:0] key_meta;
    (* preserve, dont_merge *) reg [1:0] key_sync;
    localparam integer COUNT_WIDTH = $clog2(STABLE_CYCLES + 1);
    localparam [COUNT_WIDTH-1:0] LAST_COUNT = STABLE_CYCLES[COUNT_WIDTH-1:0] - 1'b1;
    reg [COUNT_WIDTH-1:0] quiet0;
    reg [COUNT_WIDTH-1:0] quiet1;
    always @(posedge clk or negedge reset_n) begin
        if (!reset_n) begin
            key_meta <= 2'b11;
            key_sync <= 2'b11;
        end else begin
            key_meta <= key_n;
            key_sync <= key_meta;
        end
    end
    always @(posedge clk) begin
        if (!reset_n) begin
            // Hold the machine at startup until KEY0 has been released for a
            // full debounce interval. KEY1 starts released to avoid phantom keys.
            stable_n <= 2'b10;
            quiet0 <= 0;
            quiet1 <= 0;
        end else begin
            if (key_sync[0] == stable_n[0]) quiet0 <= 0;
            else if (quiet0 == LAST_COUNT) begin
                stable_n[0] <= key_sync[0];
                quiet0 <= 0;
            end else quiet0 <= quiet0 + 1'b1;
            if (key_sync[1] == stable_n[1]) quiet1 <= 0;
            else if (quiet1 == LAST_COUNT) begin
                stable_n[1] <= key_sync[1];
                quiet1 <= 0;
            end else quiet1 <= quiet1 + 1'b1;
        end
    end
endmodule
