// SPDX-License-Identifier: MIT
// Timing spike (a): fes_i386_frontend. Fetch reads a 4 KiB M10K memory
// model whose address register holds while F2 waits, as an L1I data array
// would; the stimulus rewrites its rows. Redirects, CS.D and D's ready come
// from the stimulus. Every field Q hands D feeds the signature.
module spike_frontend (
    input  wire  FPGA_CLK1_50,
    input  wire  KEY0_N,
    output logic LED0
);
    wire clk = FPGA_CLK1_50;
    logic         reset;
    logic [255:0] noise;
    spike_stimulus #(.WIDTH(256)) stimulus (.clk, .key0_n(KEY0_N), .reset, .noise);

    logic [27:0]            mem_block;
    logic                   mem_load;
    logic [127:0]           mem [0:255];
    logic [127:0]           mem_data;
    logic                   redirect = 1'b1;
    logic [31:0]            redirect_linear;
    logic                   code32;
    logic                   d_ready;
    logic                   q_valid;
    fes_i386_pkg::fetched_t q;

    always_ff @(posedge clk)
        if (noise[0] && noise[9]) mem[noise[20:13]] <= noise[255:128];
    always_ff @(posedge clk)
        if (mem_load) mem_data <= mem[mem_block[7:0]];

    always_ff @(posedge clk) begin
        redirect <= reset || noise[40:33] == 8'd0;
        if (reset || noise[40:33] == 8'd0) begin
            redirect_linear <= noise[72:41];
            code32 <= noise[73];
        end
        d_ready <= |noise[76:74];
    end

    fes_i386_frontend dut (
        .clk, .reset, .redirect, .redirect_linear, .code32, .mem_block, .mem_load,
        .mem_data(mem_data), .q_valid, .q, .d_ready
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
