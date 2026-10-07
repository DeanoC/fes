// SPDX-License-Identifier: GPL-2.0-or-later
// Four-word mem_channel with a one-cycle memory response. corrupt_phases
// flips a bit on selected read patterns; corrupt_last_only limits that to
// the final word. withhold_done never acknowledges, so the channel times out.
module pattern_count_bench (
    input wire clk,
    input wire reset,
    input wire [1:0] rate,
    input wire [5:0] corrupt_phases,
    input wire corrupt_last_only,
    input wire withhold_done,
    output wire pass,
    output wire fail,
    output wire [31:0] errors,
    output wire [191:0] live,
    output wire [191:0] pat50,
    output wire [191:0] pat75,
    output wire [191:0] pat100,
    output wire [2:0] pat_ok
);
    localparam [31:0] WORDS = 32'd4;
    localparam integer ADDR_W = 8;

    wire start, write, done, busy, stopped, reading;
    wire [ADDR_W-1:0] addr;
    wire [15:0] wdata, rdata;
    wire [2:0] phase;
    wire [31:0] shown_addr, fault_addr, last_addr;
    wire [15:0] fault_got, shown_expect, shown_got;

    mem_channel #(.ADDR_W(ADDR_W), .WORDS(WORDS), .BASE(32'd0)) channel (
        .clk(clk), .reset(reset), .stop(1'b0),
        .start(start), .write(write), .addr(addr), .wdata(wdata),
        .done(done), .rdata(rdata),
        .busy(busy), .pass(pass), .fail(fail), .stopped(stopped),
        .phase(phase), .reading(reading), .shown_addr(shown_addr),
        .fault_addr(fault_addr), .last_addr(last_addr), .fault_got(fault_got),
        .errors(errors), .pattern_errors(live),
        .shown_expect(shown_expect), .shown_got(shown_got)
    );
    pattern_latch snap (
        .clk(clk), .reset(reset), .rate(rate),
        .pass(pass), .fail(fail), .counts(live),
        .pat50(pat50), .pat75(pat75), .pat100(pat100), .pat_ok(pat_ok)
    );

    localparam [ADDR_W-1:0] LAST = WORDS[ADDR_W-1:0] - 1'b1;
    wire last_word = addr == LAST;
    wire corrupt = reading && phase <= 3'd5 && corrupt_phases[phase]
        && (!corrupt_last_only || last_word);

    reg [15:0] mem [0:3];
    reg done_q = 1'b0;
    reg [15:0] rdata_q = 16'h0000;
    assign done = done_q;
    assign rdata = rdata_q;

    always @(posedge clk) begin
        if (reset) begin
            done_q <= 1'b0;
            rdata_q <= 16'h0000;
            mem[0] <= 16'h0000;
            mem[1] <= 16'h0000;
            mem[2] <= 16'h0000;
            mem[3] <= 16'h0000;
        end else begin
            done_q <= start && !withhold_done;
            if (start && !withhold_done && write)
                mem[addr[1:0]] <= wdata;
            else if (start && !withhold_done && !write)
                rdata_q <= mem[addr[1:0]] ^ (corrupt ? 16'h0001 : 16'h0000);
        end
    end
endmodule
