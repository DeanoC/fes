// SPDX-License-Identifier: GPL-2.0-or-later
// Write then read four halfwords. A missing acknowledge becomes a failure
// so a contained HPS bridge does not leave the display busy forever.
module mem_channel (
    input wire clk,
    input wire reset,
    output reg start,
    output reg write,
    output reg [15:0] addr,
    output reg [15:0] wdata,
    input wire done,
    input wire [15:0] rdata,
    output reg busy,
    output reg pass,
    output reg fail,
    output reg [15:0] result_addr
);
    localparam [2:0] ST_RESET = 3'd0;
    localparam [2:0] ST_SETUP = 3'd1;
    localparam [2:0] ST_WAIT = 3'd2;
    localparam [2:0] ST_GAP = 3'd3;
    localparam [2:0] ST_DONE = 3'd4;
    localparam [15:0] PATTERN = 16'hA65A;
    localparam [15:0] TIMEOUT = 16'd8000;

    reg [2:0] state = ST_RESET;
    reg [1:0] index = 2'd0;
    reg phase = 1'b0;
    reg [15:0] timer = 16'd0;
    reg [15:0] captured = 16'd0;

    function [15:0] location;
        input [1:0] which;
        begin
            case (which)
                2'd0: location = 16'h0010;
                2'd1: location = 16'h0011;
                2'd2: location = 16'h00FF;
                default: location = 16'h0100;
            endcase
        end
    endfunction

    always @(posedge clk) begin
        if (reset) begin
            start <= 1'b0;
            write <= 1'b0;
            addr <= 16'h0000;
            wdata <= 16'h0000;
            busy <= 1'b1;
            pass <= 1'b0;
            fail <= 1'b0;
            result_addr <= 16'h0000;
            state <= ST_RESET;
            index <= 2'd0;
            phase <= 1'b0;
            timer <= 16'd0;
        end else begin
            case (state)
                ST_RESET: begin
                    start <= 1'b0;
                    busy <= 1'b1;
                    pass <= 1'b0;
                    fail <= 1'b0;
                    index <= 2'd0;
                    phase <= 1'b0;
                    state <= ST_SETUP;
                end
                ST_SETUP: begin
                    start <= 1'b1;
                    write <= phase == 1'b0;
                    addr <= location(index);
                    wdata <= PATTERN;
                    timer <= 16'd0;
                    state <= ST_WAIT;
                end
                ST_WAIT: begin
                    if (done) begin
                        start <= 1'b0;
                        captured <= rdata;
                        state <= ST_GAP;
                    end else if (timer == TIMEOUT) begin
                        start <= 1'b0;
                        busy <= 1'b0;
                        fail <= 1'b1;
                        result_addr <= addr;
                        state <= ST_DONE;
                    end else begin
                        timer <= timer + 16'd1;
                    end
                end
                ST_GAP: begin
                    if (phase == 1'b0) begin
                        phase <= 1'b1;
                        state <= ST_SETUP;
                    end else if (captured != PATTERN) begin
                        busy <= 1'b0;
                        fail <= 1'b1;
                        result_addr <= addr;
                        state <= ST_DONE;
                    end else if (index == 2'd3) begin
                        busy <= 1'b0;
                        pass <= 1'b1;
                        result_addr <= location(2'd3);
                        state <= ST_DONE;
                    end else begin
                        index <= index + 2'd1;
                        phase <= 1'b0;
                        state <= ST_SETUP;
                    end
                end
                default: begin
                    start <= 1'b0;
                end
            endcase
        end
    end
endmodule
