// One external transaction at a time. A stalled request and its eventual
// read response retain their owner, including across a CPU TAS sequence.
module fes_sh7091_bus (
    input  logic        clk, reset,
    input  logic        cpu_valid, cpu_lock, cpu_sq,
    input  logic [31:0] cpu_addr, cpu_wdata,
    input  logic [3:0]  cpu_rmask, cpu_wstrb,
    output logic        cpu_ready, cpu_rvalid,
    input  logic        dma_valid, dma_burst,
    input  logic [31:0] dma_addr, dma_wdata,
    input  logic [3:0]  dma_rmask, dma_wstrb,
    output logic        dma_ready, dma_rvalid,
    output logic        mem_valid, mem_lock, mem_sq,
    output logic [31:0] mem_addr, mem_wdata,
    output logic [3:0]  mem_rmask, mem_wstrb,
    input  logic        mem_ready, mem_rvalid
);
    typedef enum logic [1:0] {IDLE, SEND, REPLY} state_t;
    state_t state;
    logic dma_owner, last_dma, select_dma, queue_active;

    assign select_dma = dma_valid && !cpu_lock && !queue_active && (!cpu_valid || dma_burst || !last_dma);
    assign mem_valid = state == SEND && !reset;
    assign mem_lock = cpu_lock && !dma_owner;
    assign mem_sq = cpu_sq && !dma_owner;
    assign mem_addr = dma_owner ? dma_addr : cpu_addr;
    assign mem_rmask = dma_owner ? dma_rmask : cpu_rmask;
    assign mem_wstrb = dma_owner ? dma_wstrb : cpu_wstrb;
    assign mem_wdata = dma_owner ? dma_wdata : cpu_wdata;
    assign cpu_ready = mem_valid && mem_ready && !dma_owner;
    assign dma_ready = mem_valid && mem_ready && dma_owner;
    assign cpu_rvalid = state == REPLY && mem_rvalid && !dma_owner;
    assign dma_rvalid = state == REPLY && mem_rvalid && dma_owner;

    always_ff @(posedge clk) begin
        if (reset) begin
            state <= IDLE;
            dma_owner <= 0;
            last_dma <= 1;
            queue_active <= 0;
        end else case (state)
            IDLE: if (cpu_valid || select_dma) begin
                dma_owner <= select_dma;
                state <= SEND;
            end
            SEND: if (mem_ready) begin
                last_dma <= dma_owner;
                if (mem_sq)
                    queue_active <= mem_addr[4:2] != 3'd7;
                state <= mem_wstrb == 0 ? REPLY : IDLE;
            end
            REPLY: if (mem_rvalid)
                state <= IDLE;
            default: state <= IDLE;
        endcase
    end
endmodule
