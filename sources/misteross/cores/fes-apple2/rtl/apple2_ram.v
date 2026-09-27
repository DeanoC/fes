// SPDX-License-Identifier: GPL-2.0-or-later
// 64 KiB Apple II RAM: 48 KiB main memory plus the built-in 16 KiB language
// card. Port A belongs to the CPU in the system domain; port B is a read-only
// video scanner port in the independent HDMI pixel domain. Both reads are
// registered, the same independent-clock M10K shape as coleco_video_dpram.
module apple2_ram (
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

    always @(posedge clk_a) begin
        if (we_a)
            ram[addr_a] <= wdata_a;
        q_a <= ram[addr_a];
    end

    always @(posedge clk_b)
        q_b <= ram[addr_b];
endmodule
