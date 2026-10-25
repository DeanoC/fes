// SPDX-License-Identifier: GPL-2.0-or-later
// Latency statistics of one sample stream over a window: exact minimum and
// maximum, and the median and 99th percentile from a histogram.
//
// The bins count cycles above a base, which is the previous window's
// minimum for the same target: one bin per cycle up to base+31, then eight
// bins per doubling up to base+1023, and one bin above that. A percentile
// reports the top of its bin, capped at the maximum, so it is exact within
// 31 cycles of the base and at most one bin width high beyond that.
//
// sample takes value. A sample is read, incremented and written back over
// five cycles, so the caller waits for idle before the next one. finish
// scans the histogram, raises done for one cycle with the window's four
// values, clears the histogram and keeps the minimum as target's new base.
module lat_stats #(
    // Samples in a full window; it sets the median and 99th percentile ranks.
    parameter [15:0] SAMPLES = 16'd32768
) (
    input  wire        clk,
    input  wire        reset,
    input  wire [1:0]  target,
    input  wire        sample,
    input  wire [13:0] value,
    input  wire        finish,
    output wire        idle,
    output reg         done,
    output reg  [13:0] min,
    output reg  [13:0] med,
    output reg  [13:0] p99,
    output reg  [13:0] max
);
    localparam integer BINS = 73;
    localparam [6:0] TOP_BIN = 7'd72;
    localparam [16:0] RANK_MED = {1'b0, SAMPLES} >> 1;
    localparam [16:0] RANK_P99 = {1'b0, SAMPLES} - {1'b0, SAMPLES} / 17'd100;

    function [6:0] bin_of;
        input [13:0] offset;
        begin
            if (offset[13:10] != 4'd0) bin_of = TOP_BIN;
            else if (offset[9]) bin_of = {4'd8, offset[8:6]};
            else if (offset[8]) bin_of = {4'd7, offset[7:5]};
            else if (offset[7]) bin_of = {4'd6, offset[6:4]};
            else if (offset[6]) bin_of = {4'd5, offset[5:3]};
            else if (offset[5]) bin_of = {4'd4, offset[4:2]};
            else bin_of = {2'd0, offset[4:0]};
        end
    endfunction

    // Largest offset that falls in bin b, for b below the top bin.
    function [10:0] bin_top;
        input [6:0] b;
        begin
            if (b < 7'd32)
                bin_top = {6'd0, b[4:0]};
            else
                bin_top = ({7'd0, 1'b1, b[2:0]} + 11'd1 << (b[6:3] - 4'd2)) - 11'd1;
        end
    endfunction

    function [15:0] pick8;
        input [127:0] slice;
        input [2:0] which;
        begin
            case (which)
                3'd0: pick8 = slice[15:0];
                3'd1: pick8 = slice[31:16];
                3'd2: pick8 = slice[47:32];
                3'd3: pick8 = slice[63:48];
                3'd4: pick8 = slice[79:64];
                3'd5: pick8 = slice[95:80];
                3'd6: pick8 = slice[111:96];
                default: pick8 = slice[127:112];
            endcase
        end
    endfunction

    function [15:0] pick10;
        input [159:0] groups;
        input [3:0] which;
        begin
            case (which)
                4'd0: pick10 = groups[15:0];
                4'd1: pick10 = groups[31:16];
                4'd2: pick10 = groups[47:32];
                4'd3: pick10 = groups[63:48];
                4'd4: pick10 = groups[79:64];
                4'd5: pick10 = groups[95:80];
                4'd6: pick10 = groups[111:96];
                4'd7: pick10 = groups[127:112];
                4'd8: pick10 = groups[143:128];
                default: pick10 = groups[159:144];
            endcase
        end
    endfunction

    reg [13:0] base0 = 14'd0, base1 = 14'd0, base2 = 14'd0, base3 = 14'd0;
    wire [13:0] base = target == 2'd0 ? base0 : target == 2'd1 ? base1 :
        target == 2'd2 ? base2 : base3;
    reg [13:0] lo = 14'h3FFF;
    reg [13:0] hi = 14'd0;

    // Insert: offset, then bin, then two mux stages, then the write.
    reg        ins0 = 1'b0, ins1 = 1'b0, ins_a = 1'b0, ins_b = 1'b0;
    reg [13:0] offset = 14'd0;
    // Scan: bins 0 to 72 in turn through the same mux stages.
    reg        scanning = 1'b0;
    reg [6:0]  scan_bin = 7'd0;
    reg        scan1 = 1'b0, scan_a = 1'b0, scan_b = 1'b0;
    reg [16:0] below = 17'd0;
    reg        med_found = 1'b0, p99_found = 1'b0;
    reg [6:0]  med_bin = 7'd0, p99_bin = 7'd0;
    // Results: bin tops, then base plus top, then the cap.
    reg [2:0]  compute = 3'd0;
    reg [10:0] med_top = 11'd0, p99_top = 11'd0;
    reg [14:0] med_sum = 15'd0, p99_sum = 15'd0;
    reg        clear = 1'b0;

    reg [6:0]   bin_q = 7'd0, bin_a = 7'd0, bin_b = 7'd0;
    reg [159:0] group = 160'd0;
    reg [15:0]  count_b = 16'd0;
    wire [16*80-1:0] counts;
    assign counts[16*80-1:16*BINS] = {16*(80-BINS){1'b0}};

    genvar b;
    generate
        for (b = 0; b < BINS; b = b + 1) begin : bin
            reg [15:0] count = 16'd0;
            always @(posedge clk)
                if (clear || reset)
                    count <= 16'd0;
                else if (ins_b && bin_b == b)
                    count <= count_b + 16'd1;
            assign counts[b*16 +: 16] = count;
        end
        // First mux stage: the bin within each group of eight.
        for (b = 0; b < 10; b = b + 1) begin : group_pick
            wire [127:0] slice = counts[b*128 +: 128];
            always @(posedge clk)
                group[b*16 +: 16] <= pick8(slice, bin_q[2:0]);
        end
    endgenerate

    assign idle = !(sample | ins0 | ins1 | ins_a | ins_b | scanning | scan1 | scan_a | scan_b |
                    compute != 3'd0 | clear);

    wire [16:0] below_next = below + {1'b0, count_b};

    always @(posedge clk) begin
        done <= 1'b0;
        clear <= 1'b0;

        ins0 <= sample;
        if (sample) begin
            offset <= value > base ? value - base : 14'd0;
            if (value < lo) lo <= value;
            if (value > hi) hi <= value;
        end
        ins1 <= ins0;
        scan1 <= scanning;
        bin_q <= scanning ? scan_bin : bin_of(offset);
        if (finish && !scanning) begin
            scanning <= 1'b1;
            scan_bin <= 7'd0;
        end else if (scanning) begin
            scan_bin <= scan_bin + 7'd1;
            if (scan_bin == TOP_BIN)
                scanning <= 1'b0;
        end

        // The group stage above picks within each group; this picks the group.
        ins_a <= ins1;
        scan_a <= scan1;
        bin_a <= bin_q;
        ins_b <= ins_a;
        scan_b <= scan_a;
        bin_b <= bin_a;
        count_b <= pick10(group, bin_a[6:3]);

        if (scan_b) begin
            below <= below_next;
            if (!med_found && below_next >= RANK_MED) begin
                med_found <= 1'b1;
                med_bin <= bin_b;
            end
            if (!p99_found && below_next >= RANK_P99) begin
                p99_found <= 1'b1;
                p99_bin <= bin_b;
            end
            if (bin_b == TOP_BIN)
                compute <= 3'd1;
        end

        case (compute)
            3'd1: begin
                med_top <= bin_top(med_bin);
                p99_top <= bin_top(p99_bin);
                compute <= 3'd2;
            end
            3'd2: begin
                med_sum <= {1'b0, base} + {4'd0, med_top};
                p99_sum <= {1'b0, base} + {4'd0, p99_top};
                compute <= 3'd3;
            end
            3'd3: begin
                min <= lo;
                max <= hi;
                med <= med_bin == TOP_BIN || med_sum > {1'b0, hi} ? hi : med_sum[13:0];
                p99 <= p99_bin == TOP_BIN || p99_sum > {1'b0, hi} ? hi : p99_sum[13:0];
                done <= 1'b1;
                clear <= 1'b1;
                // A window without samples keeps the old base.
                if (lo <= hi) begin
                    case (target)
                        2'd0: base0 <= lo;
                        2'd1: base1 <= lo;
                        2'd2: base2 <= lo;
                        default: base3 <= lo;
                    endcase
                end
                lo <= 14'h3FFF;
                hi <= 14'd0;
                below <= 17'd0;
                med_found <= 1'b0;
                p99_found <= 1'b0;
                compute <= 3'd0;
            end
            default: ;
        endcase

        if (reset) begin
            ins0 <= 1'b0;
            ins1 <= 1'b0;
            ins_a <= 1'b0;
            ins_b <= 1'b0;
            scanning <= 1'b0;
            scan1 <= 1'b0;
            scan_a <= 1'b0;
            scan_b <= 1'b0;
            compute <= 3'd0;
            done <= 1'b0;
            base0 <= 14'd0;
            base1 <= 14'd0;
            base2 <= 14'd0;
            base3 <= 14'd0;
            lo <= 14'h3FFF;
            hi <= 14'd0;
            below <= 17'd0;
            med_found <= 1'b0;
            p99_found <= 1'b0;
        end
    end
endmodule
