// SPDX-License-Identifier: GPL-2.0-or-later
// Write a span, then read it back, for six patterns. A missing acknowledge
// stops the channel. A data mismatch is counted and the scan continues.
// stop freezes the counters where they are.
module mem_channel #(
    parameter integer ADDR_W = 32,
    parameter [31:0] WORDS = 32'd128,
    parameter [31:0] BASE = 32'd0
) (
    input wire clk,
    input wire reset,
    input wire stop,
    output reg start,
    output reg write,
    output reg [ADDR_W-1:0] addr,
    output reg [15:0] wdata,
    input wire done,
    input wire [15:0] rdata,
    output reg busy,
    output reg pass,
    output reg fail,
    output reg stopped,
    output reg [2:0] phase,
    output reg reading,
    output reg [31:0] shown_addr,
    output reg [15:0] errors,
    output reg [15:0] shown_expect,
    output reg [15:0] shown_got
);
    localparam [2:0] ST_RESET = 3'd0;
    localparam [2:0] ST_SETUP = 3'd1;
    localparam [2:0] ST_WAIT = 3'd2;
    localparam [2:0] ST_GAP = 3'd3;
    localparam [2:0] ST_DONE = 3'd4;
    localparam [2:0] ST_STOP = 3'd5;
    localparam [2:0] PHASES = 3'd6;
    localparam [15:0] TIMEOUT = 16'd8000;

    reg [2:0] state = ST_RESET;
    reg [31:0] index = 32'd0;
    reg [15:0] timer = 16'd0;
    reg [15:0] captured = 16'd0;
    reg faulted = 1'b0;

    function [15:0] expect_of;
        input [2:0] which;
        input [31:0] location;
        reg [15:0] mixed;
        begin
            mixed = location[15:0] ^ location[31:16];
            case (which)
                3'd0: expect_of = 16'h0000;
                3'd1: expect_of = 16'hFFFF;
                3'd2: expect_of = 16'h5555;
                3'd3: expect_of = 16'hAAAA;
                3'd4: expect_of = mixed;
                default: expect_of = ~mixed;
            endcase
        end
    endfunction

    wire [31:0] location = BASE + index;
    wire [15:0] expected = expect_of(phase, location);

    always @(posedge clk) begin
        if (reset) begin
            start <= 1'b0;
            write <= 1'b0;
            addr <= {ADDR_W{1'b0}};
            wdata <= 16'h0000;
            busy <= 1'b1;
            pass <= 1'b0;
            fail <= 1'b0;
            stopped <= 1'b0;
            phase <= 3'd0;
            reading <= 1'b0;
            shown_addr <= 32'd0;
            errors <= 16'd0;
            shown_expect <= 16'h0000;
            shown_got <= 16'h0000;
            state <= ST_RESET;
            index <= 32'd0;
            timer <= 16'd0;
            faulted <= 1'b0;
        end else begin
            case (state)
                ST_RESET: begin
                    start <= 1'b0;
                    busy <= 1'b1;
                    pass <= 1'b0;
                    fail <= 1'b0;
                    stopped <= 1'b0;
                    phase <= 3'd0;
                    reading <= 1'b0;
                    index <= 32'd0;
                    errors <= 16'd0;
                    faulted <= 1'b0;
                    state <= ST_SETUP;
                end
                ST_SETUP: begin
                    if (stop) begin
                        start <= 1'b0;
                        state <= ST_STOP;
                    end else begin
                        start <= 1'b1;
                        write <= ~reading;
                        addr <= location[ADDR_W-1:0];
                        wdata <= expected;
                        if (!faulted) begin
                            shown_addr <= location;
                            shown_expect <= expected;
                        end
                        timer <= 16'd0;
                        state <= ST_WAIT;
                    end
                end
                ST_WAIT: begin
                    if (done) begin
                        start <= 1'b0;
                        captured <= rdata;
                        if (reading && !faulted)
                            shown_got <= rdata;
                        state <= ST_GAP;
                    end else if (timer == TIMEOUT) begin
                        start <= 1'b0;
                        busy <= 1'b0;
                        fail <= 1'b1;
                        pass <= 1'b0;
                        if (errors != 16'hFFFF)
                            errors <= errors + 16'd1;
                        state <= ST_DONE;
                    end else begin
                        timer <= timer + 16'd1;
                    end
                end
                ST_GAP: begin
                    if (reading && captured != shown_expect) begin
                        faulted <= 1'b1;
                        if (errors != 16'hFFFF)
                            errors <= errors + 16'd1;
                        if (!faulted)
                            shown_got <= captured;
                    end
                    if (index + 32'd1 == WORDS) begin
                        index <= 32'd0;
                        if (reading) begin
                            reading <= 1'b0;
                            if (phase == PHASES - 3'd1) begin
                                busy <= 1'b0;
                                pass <= ~faulted && captured == shown_expect && errors == 16'd0;
                                fail <= faulted || captured != shown_expect || errors != 16'd0;
                                state <= ST_DONE;
                            end else begin
                                phase <= phase + 3'd1;
                                state <= ST_SETUP;
                            end
                        end else begin
                            reading <= 1'b1;
                            state <= ST_SETUP;
                        end
                    end else begin
                        index <= index + 32'd1;
                        state <= ST_SETUP;
                    end
                end
                ST_STOP: begin
                    start <= 1'b0;
                    busy <= 1'b0;
                    stopped <= 1'b1;
                end
                default: begin
                    start <= 1'b0;
                    if (stop)
                        stopped <= 1'b1;
                end
            endcase
        end
    end
endmodule
