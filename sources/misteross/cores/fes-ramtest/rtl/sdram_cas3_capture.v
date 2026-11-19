// SPDX-License-Identifier: GPL-2.0-or-later
// Shared 100 MHz CAS-3 retention: capture 0, forwarding +1.250 ns,
// handoff +9.375 ns. All clock crossings retain normal setup/hold checks.
module sdram_cas3_capture (
    input wire clk, clk_handoff, reset, read_strobe,
    input wire [15:0] captured,
    output wire [15:0] rdata
);
    reg [3:0] read_window = 0;
    reg [15:0] read_word, return_word;
    always @(posedge clk)
        if (reset) read_window <= 0;
        else read_window <= {read_window[2:0], read_strobe};
    always @(posedge clk_handoff)
        if (read_window[3]) read_word <= captured;
    always @(negedge clk) return_word <= read_word;
    assign rdata = return_word;
endmodule
