// SPDX-License-Identifier: MIT
// Timing spike (a): fes_i386_frontend behind an L1I model. F1's block address
// loads the M10K address registers of a 16 KiB 4-way L1I: four data ways of
// one 16-byte block each (linear bits 11:4) and the tags of the 32-byte line
// (linear bits 11:5, four ways of valid and a 20-bit page). In F2 the four
// tags are compared with the ITLB page registered alongside, and the hitting
// way's block goes into the front end's F2 register: M2's shape. The hit
// signals are replicated, one set per two bytes of the block, so no hit
// drives more than 16 of the way select's bits. Yosys merges identical logic
// even on kept wires (DeanoC/yosys#51), so each set compares against its own
// copy of the page register. A miss passes zeros; there is no refill. The
// stimulus rewrites tags and data.
// Redirects, CS.D and D's ready come from the stimulus. Every field Q hands
// D feeds the signature.
module spike_frontend (
    input  wire  FPGA_CLK1_50,
    input  wire  KEY0_N,
    output logic LED0
);
    wire clk = FPGA_CLK1_50;
    logic         reset;
    logic [383:0] noise;
    spike_stimulus #(.WIDTH(384)) stimulus (.clk, .key0_n(KEY0_N), .reset, .noise);

    logic [27:0]            mem_block;
    logic                   mem_load;
    logic [15:0][7:0]       mem_data;
    logic                   redirect = 1'b1;
    logic [31:0]            redirect_linear;
    logic                   code32;
    logic                   d_ready;
    logic                   q_valid;
    fes_i386_pkg::fetched_t q;

    // L1I arrays, written by the stimulus, read with address stall.
    logic [127:0] l1i_data0 [0:255];
    logic [127:0] l1i_data1 [0:255];
    logic [127:0] l1i_data2 [0:255];
    logic [127:0] l1i_data3 [0:255];
    logic [83:0]  l1i_tags [0:127];
    logic [127:0] way0, way1, way2, way3;
    logic [83:0]  tags;
    // The ITLB's physical page for the block in F2, one copy per hit set.
    (* keep *) logic [7:0][19:0] itlb_page;

    always_ff @(posedge clk)
        if (noise[0] && noise[9]) begin
            case (noise[11:10])
                2'd0: l1i_data0[noise[19:12]] <= noise[255:128];
                2'd1: l1i_data1[noise[19:12]] <= noise[255:128];
                2'd2: l1i_data2[noise[19:12]] <= noise[255:128];
                default: l1i_data3[noise[19:12]] <= noise[255:128];
            endcase
        end
    always_ff @(posedge clk)
        if (noise[1] && noise[2]) l1i_tags[noise[26:20]] <= noise[343:260];
    always_ff @(posedge clk) if (mem_load) way0 <= l1i_data0[mem_block[7:0]];
    always_ff @(posedge clk) if (mem_load) way1 <= l1i_data1[mem_block[7:0]];
    always_ff @(posedge clk) if (mem_load) way2 <= l1i_data2[mem_block[7:0]];
    always_ff @(posedge clk) if (mem_load) way3 <= l1i_data3[mem_block[7:0]];
    always_ff @(posedge clk) if (mem_load) tags <= l1i_tags[mem_block[7:1]];
    always_ff @(posedge clk) if (mem_load) itlb_page <= {8{noise[363:356], mem_block[27:16]}};

    logic [7:0][3:0] hit;  // hit[s][w] selects way w for bytes 2s and 2s + 1
    for (genvar s = 0; s < 8; s++) begin : g_set
        for (genvar w = 0; w < 4; w++) begin : g_way
            assign hit[s][w] = tags[21 * w + 20] && tags[21 * w +: 20] == itlb_page[s];
        end
        for (genvar i = 16 * s; i < 16 * s + 16; i++) begin : g_bit
            assign mem_data[i / 8][i % 8] = (way0[i] & hit[s][0]) | (way1[i] & hit[s][1])
                                          | (way2[i] & hit[s][2]) | (way3[i] & hit[s][3]);
        end
    end

    always_ff @(posedge clk) begin
        redirect <= reset || noise[40:33] == 8'd0;
        if (reset || noise[40:33] == 8'd0) begin
            redirect_linear <= noise[72:41];
            code32 <= noise[73];
        end
        d_ready <= |noise[76:74];
    end

    // Every access hits; a miss has no refill here, so the data is always valid.
    fes_i386_frontend dut (
        .clk, .reset, .redirect, .redirect_linear, .code32, .mem_block, .mem_load, .fetch_next(),
        .mem_data, .mem_valid(1'b1), .q_valid, .q, .d_ready
    );

    logic [31:0]  signature;
    logic [159:0] observed;
    assign observed = {21'b0, q_valid, q};
    always_ff @(posedge clk) begin
        signature <= {signature[30:0], signature[31]}
                   ^ observed[31:0] ^ observed[63:32] ^ observed[95:64] ^ observed[127:96] ^ observed[159:128];
        LED0 <= ^signature;
    end
endmodule
