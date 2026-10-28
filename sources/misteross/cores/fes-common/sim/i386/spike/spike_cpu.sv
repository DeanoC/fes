// SPDX-License-Identifier: MIT
// Timing spike (d): fes_i386_cpu as Phase 1 has built it, behind the same
// L1I model as spike_frontend: F1's block address loads the M10K address
// registers of a 16 KiB 4-way L1I, F2 compares the four tags with an ITLB
// page per two bytes and selects the hitting way. A miss passes zeros; there
// is no refill, and mem_valid is a registered stimulus bit, so fetch stalls
// without the L1I's own miss path (the L1I slice adds that). The stimulus
// rewrites tags and data, so the CPU executes whatever arrives.
//
// The data port reads an L1D model: two M10K banks of even and odd dwords
// (4 KiB), written by the stimulus and read with the address registered on
// the edge, so a load's 8 bytes arrive in M2 as the L1D's data will.
// dmem_valid is a registered stimulus bit, as mem_valid is, so loads wait
// and the CPU freezes without the L1D's own miss path. The CPU's stores feed
// the signature.
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

    // L1D model.
    logic [31:0]      dmem_addr, dmem_waddr;
    logic             dmem_read, dmem_write, dmem_valid;
    logic [7:0][7:0]  dmem_rdata, dmem_wdata;
    logic [7:0]       dmem_wmask;
    logic [31:0]      l1d_even [0:511];
    logic [31:0]      l1d_odd [0:511];
    logic [31:0]      even_q, odd_q;
    logic             odd_first;
    logic [9:0]       dword;
    logic [8:0]       even_index;
    assign dword = dmem_addr[11:2];
    assign even_index = dword[9:1] + 9'(dword[0]);
    always_ff @(posedge clk)
        if (noise[3] && noise[4]) begin
            if (noise[5]) l1d_even[noise[44:36]] <= noise[95:64];
            else l1d_odd[noise[44:36]] <= noise[95:64];
        end
    always_ff @(posedge clk) if (dmem_read) even_q <= l1d_even[even_index];
    always_ff @(posedge clk) if (dmem_read) odd_q <= l1d_odd[dword[9:1]];
    always_ff @(posedge clk) if (dmem_read) odd_first <= dword[0];
    assign dmem_rdata = odd_first ? {even_q, odd_q} : {odd_q, even_q};

    // The reset state, held while reset is.
    logic [7:0][31:0] init_gpr;
    logic [31:0]      init_eflags, init_eip;
    logic [5:0][15:0] init_sel, init_ar;
    logic [5:0][31:0] init_base, init_limit;
    always_ff @(posedge clk) begin
        mem_valid <= |noise[372:370];
        dmem_valid <= |noise[375:373];
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

    logic             trace_retire, trace_halt, trace_exception, trace_unimplemented, trace_issue, trace_md_wait;
    logic [7:0]       trace_vector;
    logic [7:0][31:0] arch_gpr;
    logic [31:0]      arch_eflags, arch_eip;
    logic [5:0][15:0] arch_sel, arch_ar;
    logic [5:0][31:0] arch_base, arch_limit;

    fes_i386_cpu dut (
        .clk, .reset, .init_gpr, .init_eflags, .init_eip, .init_sel, .init_base, .init_limit, .init_ar,
        .mem_block, .mem_load, .mem_data, .mem_valid, .dmem_addr, .dmem_read, .dmem_rdata, .dmem_valid, .dmem_write, .dmem_waddr,
        .dmem_wdata, .dmem_wmask, .trace_retire, .trace_halt, .trace_exception, .trace_vector,
        .trace_unimplemented, .trace_issue, .trace_md_wait, .arch_gpr, .arch_eflags, .arch_eip, .arch_sel, .arch_base, .arch_limit, .arch_ar
    );

    logic [31:0] signature, observed;
    always_comb begin
        observed = arch_eflags ^ arch_eip ^ {20'b0, trace_retire, trace_halt, trace_exception, trace_unimplemented, trace_vector}
                 ^ dmem_waddr ^ dmem_wdata[3:0] ^ dmem_wdata[7:4] ^ {23'b0, dmem_write, dmem_wmask};
        for (int n = 0; n < 8; n++) observed ^= arch_gpr[n];
        for (int n = 0; n < 6; n++) observed ^= arch_base[n] ^ arch_limit[n] ^ {arch_sel[n], arch_ar[n]};
    end
    always_ff @(posedge clk) begin
        signature <= {signature[30:0], signature[31]} ^ observed;
        LED0 <= ^signature;
    end
endmodule
