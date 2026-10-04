// SPDX-License-Identifier: GPL-2.0-or-later
// Bounded byte-write preservation test, independent of the full-span patterns.
// BE[0] writes DQ[7:0]; BE[1] writes DQ[15:8]. Read every write back.
module sdram_byte_lane (
    input wire clk, reset, stop,
    output reg start, write,
    output reg [25:0] addr,
    output reg [15:0] wdata,
    output reg [1:0] byte_enable,
    input wire done,
    input wire [15:0] rdata,
    output reg pass, fail, stopped, timed_out,
    output reg [7:0] completed,
    output reg [15:0] checks,
    output reg [25:0] fault_addr,
    output reg [2:0] fault_step,
    output reg [1:0] fault_be,
    output reg [15:0] fault_payload, fault_expect, fault_got
);
    localparam [1:0] SETUP = 2'd0, WAIT = 2'd1, GAP = 2'd2, FINISHED = 2'd3;
    reg [1:0] state = SETUP;
    reg [6:0] sample = 7'd0;
    reg [2:0] step = 3'd0;
    reg [15:0] timer = 16'd0;
    reg [15:0] expected = 16'hA55A;
    reg [15:0] payload = 16'hA55A;
    reg [1:0] requested_be = 2'b11;
    // Both orders, all banks, columns 0-7, and rows 0/1/0800/1000.
    // High row bits exercise the row/DQM shared pins; column bit 17 is above
    // the old simulation span. High rows use columns 4-7, low rows 0-3.
    wire [12:0] row = sample[6] ? (sample[5] ? 13'h1000 : 13'h0800) :
                                      (sample[5] ? 13'h0001 : 13'h0000);
    wire [25:0] location = {1'b0, 7'd0, sample[6], row, sample[2:1], sample[4:3]};
    // Long gaps cross refresh at every supported rate; zero is the minimal
    // request-low interval needed by the controller handshake.
    wire [15:0] gap = sample[6:5] == 2'd0 ? 16'd0 :
        sample[6:5] == 2'd1 ? 16'd32 : sample[6:5] == 2'd2 ? 16'd1024 : 16'd2048;

    always @(posedge clk) begin
        if (reset) begin
            state <= SETUP;
            sample <= 7'd0;
            step <= 3'd0;
            timer <= 16'd0;
            start <= 1'b0;
            write <= 1'b0;
            addr <= 26'd0;
            wdata <= 16'd0;
            byte_enable <= 2'b11;
            expected <= 16'hA55A;
            payload <= 16'hA55A;
            requested_be <= 2'b11;
            pass <= 1'b0;
            fail <= 1'b0;
            stopped <= 1'b0;
            timed_out <= 1'b0;
            completed <= 8'd0;
            checks <= 16'd0;
            fault_addr <= 26'd0;
            fault_step <= 3'd0;
            fault_be <= 2'b11;
            fault_payload <= 16'd0;
            fault_expect <= 16'd0;
            fault_got <= 16'd0;
        end else case (state)
            SETUP: begin
                if (stop) begin
                    stopped <= 1'b1;
                    state <= FINISHED;
                end else begin
                    start <= 1'b1;
                    write <= !step[0];
                    addr <= location;
                    timer <= 16'd0;
                    case (step)
                        3'd0: begin
                            wdata <= 16'hA55A;
                            byte_enable <= 2'b11;
                            payload <= 16'hA55A;
                            requested_be <= 2'b11;
                            expected <= 16'hA55A;
                        end
                        3'd2: begin
                            wdata <= sample[0] ? 16'hE169 : 16'h3CC7;
                            byte_enable <= sample[0] ? 2'b01 : 2'b10;
                            payload <= sample[0] ? 16'hE169 : 16'h3CC7;
                            requested_be <= sample[0] ? 2'b01 : 2'b10;
                            expected <= sample[0] ? 16'hA569 : 16'h3C5A;
                        end
                        3'd4: begin
                            wdata <= sample[0] ? 16'h3CC7 : 16'hE169;
                            byte_enable <= sample[0] ? 2'b10 : 2'b01;
                            payload <= sample[0] ? 16'h3CC7 : 16'hE169;
                            requested_be <= sample[0] ? 2'b10 : 2'b01;
                            expected <= 16'h3C69;
                        end
                        3'd6: begin
                            wdata <= 16'hF00F;
                            byte_enable <= 2'b00;
                            payload <= 16'hF00F;
                            requested_be <= 2'b00;
                            expected <= 16'h3C69;
                        end
                        default: byte_enable <= 2'b11;
                    endcase
                    state <= WAIT;
                end
            end
            WAIT: begin
                if (done || timer == 16'd20000) begin
                    start <= 1'b0;
                    timer <= 16'd0;
                    if (step[0] && done)
                        checks <= checks + 16'd1;
                    if (!done || (step[0] && rdata != expected)) begin
                        fail <= 1'b1;
                        timed_out <= !done;
                        fault_addr <= addr;
                        fault_step <= step;
                        fault_be <= requested_be;
                        fault_payload <= payload;
                        fault_expect <= expected;
                        fault_got <= rdata;
                        state <= FINISHED;
                    end else begin
                        state <= GAP;
                    end
                end else timer <= timer + 16'd1;
            end
            GAP: begin
                if (stop) begin
                    stopped <= 1'b1;
                    state <= FINISHED;
                end else if (timer == gap) begin
                    timer <= 16'd0;
                    step <= step + 3'd1;
                    state <= SETUP;
                    if (step == 3'd7) begin
                        completed <= completed + 8'd1;
                        sample <= sample + 7'd1;
                        if (sample == 7'd127) begin
                            pass <= 1'b1;
                            state <= FINISHED;
                        end
                    end
                end else timer <= timer + 16'd1;
            end
            FINISHED: begin
                start <= 1'b0;
                if (stop) stopped <= 1'b1;
            end
        endcase
    end
endmodule
