// SPDX-License-Identifier: GPL-2.0-or-later
// 1 KiB C64 color RAM ($D800-$DBFF, 1024x4). Port A is the 6510 in the
// system domain and stores every nybble, including the 24 locations past
// the 40x25 matrix that software uses as scratch. Port B is the VIC scanner
// in the HDMI pixel domain and returns zero outside that 1000-cell matrix.
// Both reads are registered so the memory maps to one synchronous M10K_TDP.
module c64_color_ram (
    input  wire        clk_a,
    input  wire [9:0]  addr_a,
    input  wire [3:0]  wdata_a,
    input  wire        we_a,
    output wire [3:0]  q_a,
    input  wire        clk_b,
    input  wire [9:0]  addr_b,
    output wire [3:0]  q_b
);
    (* ramstyle = "M10K" *) reg [3:0] ram [0:1023];
    reg [3:0] q_a_r;
    reg [3:0] q_b_r;
    reg valid_b;

    always @(posedge clk_a) begin
        if (we_a)
            ram[addr_a] <= wdata_a;
        q_a_r <= ram[addr_a];
    end

    always @(posedge clk_b) begin
        q_b_r <= ram[addr_b];
        valid_b <= addr_b < 10'd1000;
    end

    assign q_a = q_a_r;
    assign q_b = valid_b ? q_b_r : 4'h0;
endmodule
