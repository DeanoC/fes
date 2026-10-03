// SPDX-License-Identifier: GPL-2.0-or-later
// Documented CPU transactions to the frozen Spectrum registered socket bus.
// Five active clocks: request FF, card action, response FF, input capture,
// then CPU delivery. WAIT extends phase4; readiness/data are sampled together.
// Later WAIT cannot retract an accepted external transaction. Each request emits
// one STROBE, including writes stalled by WAIT; cards consume that launch
// event once. Internal RAM/ULA writes commit only at CPU acceptance.
// The inactive clock between transactions rearms RD/WR edge consumers.
module spectrum_fast_bus (
    input wire clk, reset, req, wait_n,
    input wire [2:0] kind,
    input wire [15:0] addr,
    input wire [7:0] wdata, rdata,
    output reg [7:0] captured_rdata,
    output wire ready, strobe,
    output wire m1_n, mreq_n, iorq_n, rd_n, wr_n,
    output reg [15:0] a,
    output reg [7:0] dout
);
    reg [2:0] phase = 3'd0;
    reg [2:0] held_kind = 3'd0;
    wire active = phase != 3'd0 && !reset;
    wire memory_read = held_kind == 3'd0 || held_kind == 3'd1 || held_kind == 3'd6;
    wire memory_write = held_kind == 3'd2;
    wire io_read = held_kind == 3'd3;
    wire io_write = held_kind == 3'd4;
    assign ready = active && phase == 3'd5;
    assign strobe = active && phase == 3'd1 && (memory_read || memory_write || io_read || io_write);
    assign m1_n = !(active && (held_kind == 3'd0 || held_kind == 3'd5 || held_kind == 3'd6));
    assign mreq_n = !(active && (memory_read || memory_write));
    assign iorq_n = !(active && (io_read || io_write || held_kind == 3'd5));
    assign rd_n = !(active && (memory_read || io_read));
    assign wr_n = !(active && (memory_write || io_write));
    always @(posedge clk) begin
        if (reset) begin
            phase <= 3'd0;
            held_kind <= 3'd0;
            a <= 16'd0;
            dout <= 8'd0;
            captured_rdata <= 8'd0;
        end else if (phase == 3'd0) begin
            if (req) begin
                phase <= 3'd1;
                held_kind <= kind;
                a <= addr;
                dout <= wdata;
            end
        end else if (ready) begin
            phase <= 3'd0;
        end else if (phase == 3'd4) begin
            if (wait_n) begin
                captured_rdata <= rdata;
                phase <= 3'd5;
            end
        end else begin
            phase <= phase + 3'd1;
        end
    end
endmodule
