// SPDX-License-Identifier: MIT
// Timing spike (d): fes_i386_cpu as Phase 1 has built it, behind the same
// L1I model as spike_frontend: F1's block address loads the M10K address
// registers of a 16 KiB 4-way L1I, F2 compares the four tags with an ITLB
// page per two bytes and selects the hitting way. A miss passes zeros; there
// is no refill, and mem_valid is a registered stimulus bit, so fetch stalls
// without the L1I's own miss path (the L1I slice adds that). The stimulus
// rewrites tags and data, so the CPU executes whatever arrives.
//
// The reset state comes from the stimulus too, so CS's base and every
// register stay variables as later far transfers will make them. Every
// trace and architectural output feeds the signature.
module spike_cpu (
    input  wire  FPGA_CLK1_50,
    input  wire  KEY0_N,
    output logic LED0
);
    wire clk = FPGA_CLK1_50;
    logic         reset;
    logic [511:0] noise;
    spike_stimulus #(.WIDTH(512)) stimulus (.clk, .key0_n(KEY0_N), .reset, .noise);

    logic [27:0]      mem_block;
    logic             mem_load;
    logic [15:0][7:0] mem_data;
    logic             mem_valid;

    // L1I arrays, written by the stimulus, read with address stall.
    logic [127:0] l1i_data0 [0:255];
    logic [127:0] l1i_data1 [0:255];
    logic [127:0] l1i_data2 [0:255];
    logic [127:0] l1i_data3 [0:255];
    logic [83:0]  l1i_tags [0:127];
    logic [127:0] way0, way1, way2, way3;
    logic [83:0]  tags;
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

    // The reset state, held while reset is.
    logic [7:0][31:0] init_gpr;
    logic [31:0]      init_eflags, init_eip;
    logic [5:0][15:0] init_sel, init_ar;
    logic [5:0][31:0] init_base, init_limit;
    always_ff @(posedge clk) begin
        mem_valid <= |noise[372:370];
        if (reset) begin
            init_gpr <= noise[511:256];
            init_eflags <= noise[415:384];
            init_eip <= noise[447:416];
            init_sel <= noise[479:384];
            init_base <= noise[447:256];
            init_limit <= noise[511:320];
            init_ar <= noise[383:288];
        end
    end

    logic             trace_retire, trace_halt, trace_fault, trace_unimplemented;
    logic [7:0]       trace_vector;
    logic [7:0][31:0] arch_gpr;
    logic [31:0]      arch_eflags, arch_eip;
    logic [5:0][15:0] arch_sel, arch_ar;
    logic [5:0][31:0] arch_base, arch_limit;

    fes_i386_cpu dut (
        .clk, .reset, .init_gpr, .init_eflags, .init_eip, .init_sel, .init_base, .init_limit, .init_ar,
        .mem_block, .mem_load, .mem_data, .mem_valid, .trace_retire, .trace_halt, .trace_fault, .trace_vector,
        .trace_unimplemented, .arch_gpr, .arch_eflags, .arch_eip, .arch_sel, .arch_base, .arch_limit, .arch_ar
    );

    logic [31:0] signature, observed;
    always_comb begin
        observed = arch_eflags ^ arch_eip ^ {20'b0, trace_retire, trace_halt, trace_fault, trace_unimplemented, trace_vector};
        for (int n = 0; n < 8; n++) observed ^= arch_gpr[n];
        for (int n = 0; n < 6; n++) observed ^= arch_base[n] ^ arch_limit[n] ^ {arch_sel[n], arch_ar[n]};
    end
    always_ff @(posedge clk) begin
        signature <= {signature[30:0], signature[31]} ^ observed;
        LED0 <= ^signature;
    end
endmodule
