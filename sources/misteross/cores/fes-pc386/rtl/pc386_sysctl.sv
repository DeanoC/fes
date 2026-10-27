// SPDX-License-Identifier: MIT
// System control ports (docs/pc386-machine-design.md, "I/O ports").
//
// 61h (port B): bit 0 gates PIT channel 2, bit 1 enables the speaker, bits 2-3
// store the parity and I/O check enables. Reads return those bits, bit 4
// toggling on each rising edge of PIT channel 1, bit 5 PIT channel 2's output
// and bits 6-7 zero (no NMI sources).
// 92h (port A): bit 0 rising edge requests a CPU reset, bit 1 is A20.
// 80h is the POST port and the AT's spare page register: reads return the
// last write. Every write is reported on post_valid for the trace.
module pc386_sysctl (
    input wire clk, reset,
    input wire access, write,
    input wire select_b, select_a, select_post,
    input wire [7:0] wdata,
    output reg [7:0] rdata,
    input wire pit_out1, pit_out2,
    output wire pit_gate2, speaker_enable,
    output wire port_a_a20,
    output reg fast_reset,       // one clock
    output reg post_valid,
    output reg [7:0] post_code
);
    reg [3:0] port_b;
    reg [1:0] port_a;
    reg refresh_toggle, out1_previous;
    assign pit_gate2 = port_b[0];
    assign speaker_enable = port_b[1];
    assign port_a_a20 = port_a[1];

    always @* begin
        rdata = 8'hff;
        if (select_b) rdata = {2'b00, pit_out2, refresh_toggle, port_b};
        else if (select_a) rdata = {6'b0, port_a};
        else if (select_post) rdata = post_code;
    end

    always @(posedge clk) begin
        fast_reset <= 0;
        post_valid <= 0;
        if (reset) begin
            port_b <= 0;
            port_a <= 0;
            refresh_toggle <= 0;
            out1_previous <= 0;
            post_code <= 0;
        end else begin
            out1_previous <= pit_out1;
            if (pit_out1 && !out1_previous) refresh_toggle <= !refresh_toggle;
            if (access && write) begin
                if (select_b) port_b <= wdata[3:0];
                if (select_a) begin
                    port_a <= wdata[1:0];
                    if (wdata[0] && !port_a[0]) fast_reset <= 1;
                end
                if (select_post) begin
                    post_code <= wdata;
                    post_valid <= 1;
                end
            end
        end
    end
endmodule
