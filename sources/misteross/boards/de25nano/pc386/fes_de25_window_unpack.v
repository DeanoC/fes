// SPDX-License-Identifier: GPL-2.0-or-later
// One 64-bit port's response FIFO: 128-bit beats with the halves the burst
// covers, out one 64-bit beat a clock.
module fes_de25_window_unpack #(
    parameter integer BITS = 7
) (
    input  wire         clk, reset, push,
    input  wire [127:0] data,
    input  wire         lo_valid, hi_valid,
    output reg  [63:0]  readdata = 64'd0,
    output reg          readdatavalid = 1'b0,
    output wire         popped
);
    (* ramstyle = "M20K" *) reg [129:0] store [0:(1 << BITS)-1];
    reg [BITS-1:0] wr = 0, rd = 0;
    reg [BITS:0] count = 0;
    reg [129:0] q = 130'd0;
    reg q_valid = 1'b0, half = 1'b0;   // half: the low half of q has gone
    wire q_lo = q[128] && !half;
    wire q_hi = q[129];
    // Load the next entry when q is empty or its last half leaves this clock.
    wire leaving = q_valid && (q_lo ? !q_hi : 1'b1);
    wire load = count != 0 && (!q_valid || leaving);
    assign popped = load;
    always @(posedge clk) begin
        if (push) store[wr] <= {hi_valid, lo_valid, data};
        if (reset) begin
            wr <= 0; rd <= 0; count <= 0; q_valid <= 1'b0; half <= 1'b0; readdatavalid <= 1'b0;
        end else begin
            if (push) wr <= wr + 1'b1;
            count <= count + {{BITS{1'b0}}, push} - {{BITS{1'b0}}, load};
            readdatavalid <= q_valid && (q_lo || q_hi);
            readdata <= q_lo ? q[63:0] : q[127:64];
            if (q_valid && q_lo && q_hi) half <= 1'b1;
            if (load) begin q <= store[rd]; rd <= rd + 1'b1; q_valid <= 1'b1; half <= 1'b0; end
            else if (leaving) q_valid <= 1'b0;
        end
    end
endmodule
