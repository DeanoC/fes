// SPDX-License-Identifier: MIT
// Timing spike (c): the M2 stage of a 16 KiB, 4-way L1D with 32-byte lines.
// M1 registers the index into the M10K address registers: tags by linear
// bits 11:5 (four ways of valid and a 20-bit physical page), data by linear
// bits 11:3 (four ways of an even and an odd dword). It also registers the
// physical page the DTLB found. M2 compares the four tags with that page and
// selects the hitting way's data into the M2/EX register. The stimulus fills
// both arrays.
module spike_m2 (
    input  wire  FPGA_CLK1_50,
    input  wire  KEY0_N,
    output logic LED0
);
    wire clk = FPGA_CLK1_50;
    logic         reset;
    logic [383:0] noise;
    spike_stimulus #(.WIDTH(384)) stimulus (.clk, .key0_n(KEY0_N), .reset, .noise);

    logic [83:0]  tags [0:127];
    logic [255:0] data [0:511];
    logic [83:0]  tag_q;   // M10K outputs in M2
    logic [255:0] data_q;
    logic [31:0]  linear;  // AG/M1 register
    logic [19:0]  page_m1, page;  // DTLB result in M1, then the M1/M2 register

    always_ff @(posedge clk)
        if (noise[0] && noise[5]) tags[noise[12:6]] <= noise[96:13];
    always_ff @(posedge clk)
        if (noise[1] && noise[7]) data[noise[105:97]] <= noise[361:106];

    always_ff @(posedge clk) begin
        linear <= noise[383:352];
        page_m1 <= noise[371:352];
        page <= page_m1;
    end
    always_ff @(posedge clk) tag_q <= tags[linear[11:5]];
    always_ff @(posedge clk) data_q <= data[linear[11:3]];

    logic [3:0]  hit;
    logic [63:0] way_data;
    for (genvar w = 0; w < 4; w++) begin : g_way
        assign hit[w] = tag_q[21 * w + 20] && tag_q[21 * w +: 20] == page;
    end
    always_comb begin
        way_data = '0;
        for (int w = 0; w < 4; w++)
            way_data = way_data | (data_q[64 * w +: 64] & {64{hit[w]}});
    end

    logic [63:0] load_data;
    logic [3:0]  load_hit;
    logic        load_miss;
    always_ff @(posedge clk) begin
        load_data <= way_data;
        load_hit <= hit;
        load_miss <= hit == 4'd0;
    end

    logic [31:0] signature;
    always_ff @(posedge clk) begin
        signature <= {signature[30:0], signature[31]} ^ load_data[31:0] ^ load_data[63:32]
                   ^ {27'b0, load_miss, load_hit};
        LED0 <= ^signature;
    end
endmodule
