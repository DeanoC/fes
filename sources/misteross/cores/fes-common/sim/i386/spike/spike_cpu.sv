// SPDX-License-Identifier: MIT
// Timing spike (d): fes_i386_cpu as Phase 1 has built it, F1 to WB with its
// L1I, L1D and bus interface (fes_i386_l1i, fes_i386_l1d, fes_i386_biu). The
// envelope stands for the machine: HPS DDR port 0, the MMIO port, DMA, FLUSH,
// the I/O port and the interrupt lines answer with registered stimulus bits,
// and A20M is one too. The attribute map is pc386's, tied as its shell ties
// it: A0000h-BFFFFh MMIO, the two BIOS ranges read-only, 16 MiB of RAM.
//
// The reset state comes from the stimulus too, so CS's base and every
// register stay variables as later far transfers will make them. Every
// trace, port and architectural output feeds the signature.
module spike_cpu (
    input  wire  FPGA_CLK1_50,
    input  wire  KEY0_N,
    output logic LED0
);
    wire clk = FPGA_CLK1_50;
    logic         reset;
    logic [511:0] noise;
    spike_stimulus #(.WIDTH(512)) stimulus (.clk, .key0_n(KEY0_N), .reset, .noise);

    // pc386's attribute map: slots A0000h-BFFFFh MMIO (2), C0000h-C7FFFh
    // read-only (1), C8000h-EFFFFh RAM (0), F0000h-FFFFFh read-only (1).
    localparam logic [47:0] ATTR_UPPER = 48'h55_0000_05_aaaa;

    // The reset state, held while reset is.
    logic [7:0][31:0] init_gpr;
    logic [31:0]      init_eflags, init_eip, init_cr0;
    logic [5:0][15:0] init_sel, init_ar;
    logic [5:0][31:0] init_base, init_limit;
    logic             a20m;
    always_ff @(posedge clk) begin
        a20m <= &noise[7:6];
        if (reset) begin
            init_gpr <= noise[511:256];
            init_eflags <= noise[415:384];
            init_eip <= noise[447:416];
            init_cr0 <= noise[63:32];
            init_sel <= noise[479:384];
            init_base <= noise[447:256];
            init_limit <= noise[511:320];
            init_ar <= noise[383:288];
        end
    end

    // The machine's answers.
    logic             ddr_waitrequest, ddr_readdatavalid;
    logic [127:0]     ddr_readdata;
    logic             mmio_ready, mmio_rvalid;
    logic [31:0]      mmio_rdata;
    logic             dma_valid, dma_write, flush_request;
    logic [23:0]      dma_address;
    logic [7:0]       dma_wdata;
    logic             io_ack, intr, nmi, inta_ack;
    logic [31:0]      io_rdata;
    logic [7:0]       inta_vector;
    always_ff @(posedge clk) begin
        ddr_waitrequest <= &noise[369:368];
        ddr_readdatavalid <= |noise[372:370];
        ddr_readdata <= noise[255:128];
        mmio_ready <= |noise[375:373];
        mmio_rvalid <= &noise[388:387];
        mmio_rdata <= noise[95:64];
        dma_valid <= &noise[391:389];
        dma_write <= noise[392];
        dma_address <= noise[127:104];
        dma_wdata <= noise[103:96];
        flush_request <= &noise[395:393];
        io_ack <= |noise[378:376];
        io_rdata <= noise[127:96];
        intr <= &noise[381:379];
        nmi <= &noise[384:382];
        inta_ack <= |noise[387:385];
        inta_vector <= noise[63:56];
    end

    logic [27:0]      ddr_address;
    logic [7:0]       ddr_burstcount;
    logic             ddr_read, ddr_write;
    logic [127:0]     ddr_writedata;
    logic [15:0]      ddr_byteenable;
    logic             mmio_valid, mmio_write;
    logic [fes_i386_pkg::PA-3:0] mmio_address;
    logic [3:0]       mmio_byteenable;
    logic [31:0]      mmio_wdata;
    logic             dma_done, flush_done;
    logic [7:0]       dma_rdata;
    logic             io_req, io_write, inta_req;
    logic [15:0]      io_port;
    logic [1:0]       io_size;
    logic [31:0]      io_wdata;
    logic             trace_retire, trace_halt, trace_exception, trace_unimplemented, trace_issue, trace_md_wait;
    logic             trace_fetch, trace_dread, trace_mem_wait, trace_mem_idle, trace_replay, trace_store, trace_ex_store;
    logic [31:0]      trace_dread_addr;
    logic [27:0]      trace_fetch_block;
    logic [31:0]      trace_store_addr;
    logic [7:0][7:0]  trace_store_data;
    logic [7:0]       trace_store_mask;
    logic [7:0]       trace_vector;
    logic [7:0][31:0] arch_gpr;
    logic [31:0]      arch_eflags, arch_eip, arch_cr0;
    logic [5:0][15:0] arch_sel, arch_ar;
    logic [5:0][31:0] arch_base, arch_limit;

    fes_i386_cpu dut (
        .clk, .cold_reset(reset), .reset, .init_gpr, .init_eflags, .init_eip, .init_cr0, .init_sel, .init_base, .init_limit,
        .init_ar, .a20m, .attr_upper(ATTR_UPPER), .ram_top(7'd16),
        .ddr_address, .ddr_burstcount, .ddr_read, .ddr_write, .ddr_writedata, .ddr_byteenable, .ddr_waitrequest,
        .ddr_readdata, .ddr_readdatavalid,
        .mmio_valid, .mmio_write, .mmio_address, .mmio_byteenable, .mmio_wdata, .mmio_ready, .mmio_rvalid, .mmio_rdata,
        .dma_valid, .dma_write, .dma_address, .dma_wdata, .dma_done, .dma_rdata, .flush_request, .flush_done,
        .io_req, .io_write, .io_port, .io_size, .io_wdata, .io_ack, .io_rdata,
        .intr, .nmi, .inta_req, .inta_ack, .inta_vector, .trace_retire, .trace_exception, .trace_vector, .trace_halt,
        .trace_unimplemented, .trace_issue, .trace_md_wait, .trace_fetch, .trace_fetch_block, .trace_dread, .trace_dread_addr,
        .trace_mem_wait, .trace_mem_idle, .trace_replay, .trace_store, .trace_ex_store, .trace_store_addr, .trace_store_data,
        .trace_store_mask, .arch_gpr, .arch_eflags, .arch_eip, .arch_cr0, .arch_sel, .arch_base, .arch_limit, .arch_ar
    );

    logic [31:0] signature, observed;
    always_comb begin
        observed = arch_eflags ^ arch_eip ^ arch_cr0 ^ trace_dread_addr ^ {20'b0, trace_retire, trace_halt, trace_exception, trace_unimplemented, trace_vector}
                 ^ {trace_fetch_block, trace_fetch, trace_dread, trace_mem_wait, trace_replay}
                 ^ {31'd0, trace_ex_store}
                 ^ trace_store_addr ^ trace_store_data[3:0] ^ trace_store_data[7:4] ^ {23'b0, trace_store, trace_store_mask}
                 ^ {ddr_address, ddr_read, ddr_write, trace_mem_idle, trace_issue} ^ {ddr_burstcount, 8'd0, ddr_byteenable}
                 ^ ddr_writedata[31:0] ^ ddr_writedata[63:32] ^ ddr_writedata[95:64] ^ ddr_writedata[127:96]
                 ^ {mmio_address, mmio_valid, mmio_write} ^ {mmio_byteenable, 28'd0} ^ mmio_wdata
                 ^ {dma_rdata, dma_done, flush_done, 21'd0, trace_md_wait}
                 ^ io_wdata ^ {io_port, 11'b0, inta_req, io_req, io_write, io_size};
        for (int n = 0; n < 8; n++) observed ^= arch_gpr[n];
        for (int n = 0; n < 6; n++) observed ^= arch_base[n] ^ arch_limit[n] ^ {arch_sel[n], arch_ar[n]};
    end
    always_ff @(posedge clk) begin
        signature <= {signature[30:0], signature[31]} ^ observed;
        LED0 <= ^signature;
    end
endmodule
