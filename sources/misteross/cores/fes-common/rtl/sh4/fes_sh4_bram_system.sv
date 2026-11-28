// Standalone diagnostic with a 2 KiB unified memory. While reset is asserted,
// the loader can write any word. The CPU owns the port after reset.
module fes_sh4_bram_system (
    input  logic        clk,
    input  logic        reset,
    input  logic [3:0]  irq_level,
    input  logic [11:0] irq_event,
    input  logic        nmi,
    input  logic        nmi_while_blocked,
    output logic        irq_ack,
    input  logic        load_we,
    input  logic [8:0]  load_word_addr,
    input  logic [31:0] load_data,
    output logic        retired,
    output logic [31:0] retired_pc,
    output logic [3:0]  fault,
    output logic [31:0] fault_pc
);
    // The private RAM has no competing master or read side effects.
    logic valid, rvalid, unused_mem_lock, unused_mem_sq;
    logic [3:0] unused_mem_rmask;
    logic [31:0] addr, wdata, rdata, ram_wdata, ram_rdata;
    logic [3:0] wstrb, ram_wstrb;
    logic [8:0] ram_addr;
    logic ram_read, in_range, read_in_range;

    fes_sh4 #(.RESET_PC(32'b0), .RESET_CCR(9'h103)) cpu (
        .clk, .reset, .irq_level, .irq_event, .nmi, .nmi_while_blocked, .irq_ack,
        .mem_valid(valid), .mem_lock(unused_mem_lock), .mem_sq(unused_mem_sq), .mem_rmask(unused_mem_rmask), .mem_ready(1'b1),
        .mem_addr(addr), .mem_wstrb(wstrb), .mem_wdata(wdata),
        .mem_rvalid(rvalid), .mem_rdata(rdata),
        .retired, .retired_pc, .fault, .fault_pc
    );

    assign in_range = addr[31:11] == 0 && addr[1:0] == 0;
    assign ram_addr = reset ? load_word_addr : addr[10:2];
    assign ram_wdata = reset ? load_data : wdata;
    assign ram_wstrb = reset ? {4{load_we}} : valid && in_range ? wstrb : 4'b0;
    assign ram_read = valid && wstrb == 0;
    assign rdata = read_in_range ? ram_rdata : 32'b0;

    always_ff @(posedge clk) begin
        rvalid <= ram_read;
        if (ram_read)
            read_in_range <= in_range;
    end

    for (genvar lane = 0; lane < 4; lane++) begin : bytes
        (* ramstyle = "M20K" *) logic [7:0] bank [0:511];

        always_ff @(posedge clk) begin
            if (ram_wstrb[lane])
                bank[ram_addr] <= ram_wdata[8*lane +: 8];
            if (ram_read)
                ram_rdata[8*lane +: 8] <= bank[ram_addr];
        end
    end
endmodule
