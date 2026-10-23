// SPDX-License-Identifier: GPL-2.0-or-later
// 64 KiB Commodore 64 main RAM. Port A is the 6510 in the system domain.
// Port B is the native VIC DMA reader in the system domain. Both reads are registered.
// Port A adds a second register after the block-select mux: the 64 KiB spans
// a whole M10K column, and its consumers sample at system edge 27 or later,
// so the extra edge is invisible at the bus level.
module c64_ram (
    input  wire        clk_a,
    input  wire [15:0] addr_a,
    input  wire [7:0]  wdata_a,
    input  wire        we_a,
    output reg  [7:0]  q_a,
    input  wire        clk_b,
    input  wire [15:0] addr_b,
    output reg  [7:0]  q_b
);
    (* ramstyle = "M10K" *) reg [7:0] ram [0:65535];

    reg [7:0] block_q;
    always @(posedge clk_a) begin
        if (we_a)
            ram[addr_a] <= wdata_a;
        block_q <= ram[addr_a];
        q_a <= block_q;
    end

    always @(posedge clk_b)
        q_b <= ram[addr_b];
endmodule
