// SPDX-License-Identifier: GPL-2.0-or-later
// Latency windows over the memories once every scan has ended. Targets take
// turns: 0 is DDR port 0, 1 and 2 are DDR ports 1 and 2, 3 is the SDRAM
// addon. A window is SAMPLES single-line reads on one target, one at a time
// and none on the others, so the DDR ports never contend with each other.
//
// Each read gives two counts, to its first and to its last data. Each count
// has its own lat_stats. At the end of a window the eight values become
// decimal digits in results, 128 bits per target in target order:
// first-data min, median, p99 and max, then the same for the last data.
// The minimum, median and p99 saturate at 999 and the maximum at 9999. A
// target whose channel cannot measure is skipped and keeps its old digits;
// one that stops answering mid-window publishes nothing for that window.
module lat_monitor #(
    parameter [15:0] SAMPLES = 16'd32768
) (
    input  wire         clk,
    input  wire         reset,
    // Every scan has ended; stop halts the windows and keeps the results.
    input  wire         enable,
    input  wire         stop,
    // Per target: can take a start, has stopped answering, a finished read.
    input  wire [3:0]   ready,
    input  wire [3:0]   dead,
    output reg  [3:0]   start,
    input  wire [3:0]   sample,
    input  wire [55:0]  first,
    input  wire [55:0]  last,
    output reg  [511:0] results
);
    localparam [2:0] S_PICK = 3'd0;
    localparam [2:0] S_START = 3'd1;
    localparam [2:0] S_MEASURE = 3'd2;
    localparam [2:0] S_FINISH = 3'd3;
    localparam [2:0] S_RESULTS = 3'd4;
    localparam [2:0] S_CONVERT = 3'd5;
    localparam [2:0] S_NEXT = 3'd6;

    reg [2:0]  state = S_PICK;
    reg [1:0]  target = 2'd0;
    reg [15:0] taken = 16'd0;
    reg        abandoned = 1'b0;
    reg        finish = 1'b0;

    wire take = state == S_MEASURE && sample[target];
    wire first_idle, last_idle, first_done;
    wire [13:0] first_min, first_med, first_p99, first_max;
    wire [13:0] last_min, last_med, last_p99, last_max;
    lat_stats #(.SAMPLES(SAMPLES)) first_stats (
        .clk(clk), .reset(reset), .target(target),
        .sample(take), .value(first[target*14 +: 14]), .finish(finish),
        .idle(first_idle), .done(first_done),
        .min(first_min), .med(first_med), .p99(first_p99), .max(first_max)
    );
    lat_stats #(.SAMPLES(SAMPLES)) last_stats (
        .clk(clk), .reset(reset), .target(target),
        .sample(take), .value(last[target*14 +: 14]), .finish(finish),
        .idle(last_idle), .done(),
        .min(last_min), .med(last_med), .p99(last_p99), .max(last_max)
    );
    wire stats_idle = first_idle && last_idle;

    // Decimal conversion, one value at a time: double dabble on 14 bits.
    reg [2:0]  slot = 3'd0;
    reg [3:0]  step = 4'd0;
    reg [13:0] binary = 14'd0;
    reg [15:0] bcd = 16'd0;
    wire [15:0] adjusted = {
        bcd[15:12] >= 4'd5 ? bcd[15:12] + 4'd3 : bcd[15:12],
        bcd[11:8] >= 4'd5 ? bcd[11:8] + 4'd3 : bcd[11:8],
        bcd[7:4] >= 4'd5 ? bcd[7:4] + 4'd3 : bcd[7:4],
        bcd[3:0] >= 4'd5 ? bcd[3:0] + 4'd3 : bcd[3:0]
    };
    reg [13:0] value_of_slot;
    always @(*) begin
        case (slot)
            3'd0: value_of_slot = first_min;
            3'd1: value_of_slot = first_med;
            3'd2: value_of_slot = first_p99;
            3'd3: value_of_slot = first_max;
            3'd4: value_of_slot = last_min;
            3'd5: value_of_slot = last_med;
            3'd6: value_of_slot = last_p99;
            default: value_of_slot = last_max;
        endcase
    end
    wire [13:0] cap = slot[1:0] == 2'd3 ? 14'd9999 : 14'd999;

    always @(posedge clk) begin
        start <= 4'd0;
        finish <= 1'b0;
        if (reset) begin
            state <= S_PICK;
            target <= 2'd0;
            taken <= 16'd0;
            abandoned <= 1'b0;
            results <= 512'd0;
        end else case (state)
            S_PICK: begin
                taken <= 16'd0;
                abandoned <= 1'b0;
                if (enable && !stop) begin
                    if (ready[target])
                        state <= S_START;
                    else
                        target <= target + 2'd1;
                end
            end
            S_START: begin
                if (stop || !ready[target]) begin
                    abandoned <= 1'b1;
                    state <= S_FINISH;
                end else if (stats_idle) begin
                    start[target] <= 1'b1;
                    state <= S_MEASURE;
                end
            end
            S_MEASURE: begin
                if (sample[target]) begin
                    taken <= taken + 16'd1;
                    state <= taken == SAMPLES - 16'd1 ? S_FINISH : S_START;
                end else if (dead[target]) begin
                    abandoned <= 1'b1;
                    state <= S_FINISH;
                end
            end
            S_FINISH: begin
                // Clears the histograms of an abandoned window too.
                if (stats_idle) begin
                    finish <= 1'b1;
                    state <= S_RESULTS;
                end
            end
            S_RESULTS: begin
                if (first_done) begin
                    slot <= 3'd0;
                    step <= 4'd0;
                    state <= abandoned ? S_NEXT : S_CONVERT;
                end
            end
            S_CONVERT: begin
                if (step == 4'd0) begin
                    binary <= value_of_slot > cap ? cap : value_of_slot;
                    bcd <= 16'd0;
                    step <= 4'd14;
                end else begin
                    {bcd, binary} <= {adjusted, binary} << 1;
                    step <= step - 4'd1;
                    if (step == 4'd1) begin
                        results[{target, slot, 4'd0} +: 16] <= {adjusted[14:0], binary[13]};
                        slot <= slot + 3'd1;
                        if (slot == 3'd7)
                            state <= S_NEXT;
                    end
                end
            end
            default: begin
                target <= target + 2'd1;
                state <= S_PICK;
            end
        endcase
    end
endmodule
