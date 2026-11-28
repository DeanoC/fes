// SH7091 core with on-chip peripheral routing and an external memory port.
module fes_sh7091 #(
    parameter logic [31:0] RESET_PC = 32'ha000_0000,
    parameter logic [15:0] RESET_CCR = 16'b0
) (
    input  logic        clk,
    input  logic        reset,
    input  logic        bus_tick,
    input  logic        peripheral_tick, rtc_tick, tclk,
    input  logic        capture_ack,
    input  logic [1:0]  dreq_n,
    output logic [1:0]  dreq_ack,
    output logic        tclk_output_enable, capture_dma,
    input  logic [3:0]  irq_level,
    input  logic [11:0] irq_event,
    input  logic        nmi,
    input  logic        nmi_while_blocked,
    output logic        irq_ack,
    output logic        mem_valid,
    output logic        mem_lock,
    output logic        mem_sq,
    output logic [3:0]  mem_rmask,
    input  logic        mem_ready,
    output logic [31:0] mem_addr,
    output logic [3:0]  mem_wstrb,
    output logic [31:0] mem_wdata,
    input  logic        mem_rvalid,
    input  logic [31:0] mem_rdata,
    output logic [1:0]  refresh_irq,
    output logic [1:0]  sdram_command,
    output logic        sdram_mode_set,
    output logic [11:0] sdram_mode_address,
    output logic        retired,
    output logic [31:0] retired_pc,
    output logic [3:0]  fault,
    output logic [31:0] fault_pc
);
    logic cpu_valid, cpu_rvalid;
    logic [31:0] cpu_rdata, bsc_rdata, tmu_rdata, intc_rdata;
    logic [3:0] timer_irq, cpu_irq_level;
    logic [11:0] cpu_irq_event;
    logic cpu_irq_ack, tmu_select, tmu_rvalid, intc_select, intc_rvalid, local_select;
    logic bsc_select, bsc_rvalid, dmac_select, dmac_rvalid, cpu_ext_ready, cpu_ext_rvalid;
    logic cpu_lock, cpu_sq, dma_valid, dma_burst, dma_ready, dma_rvalid, capture_accept;
    logic [3:0] cpu_rmask, cpu_wstrb, dma_rmask, dma_wstrb;
    logic [4:0] dma_irq;
    logic [31:0] cpu_addr, cpu_wdata, dma_addr, dma_wdata, dmac_rdata, capture_value;

    fes_sh4 #(.RESET_PC(RESET_PC), .RESET_CCR(RESET_CCR)) cpu (
        .clk, .reset, .irq_level(cpu_irq_level), .irq_event(cpu_irq_event),
        .nmi, .nmi_while_blocked, .irq_ack(cpu_irq_ack),
        .mem_valid(cpu_valid), .mem_ready(local_select || cpu_ext_ready), .mem_lock(cpu_lock), .mem_sq(cpu_sq),
        .mem_rmask(cpu_rmask), .mem_addr(cpu_addr), .mem_wstrb(cpu_wstrb), .mem_wdata(cpu_wdata),
        .mem_rvalid(cpu_rvalid), .mem_rdata(cpu_rdata),
        .retired, .retired_pc, .fault, .fault_pc
    );

    fes_sh7091_bsc bsc (
        .clk, .reset, .bus_tick, .request(cpu_valid), .address(cpu_addr[31:2]),
        .rmask(cpu_rmask), .wstrb(cpu_wstrb), .wdata(cpu_wdata), .selected(bsc_select),
        .rvalid(bsc_rvalid), .rdata(bsc_rdata), .refresh_irq, .sdram_command,
        .sdram_mode_set, .sdram_mode_address
    );

    fes_sh7091_tmu tmu (
        .clk, .reset, .peripheral_tick, .rtc_tick, .tclk, .request(cpu_valid), .address(cpu_addr[31:2]),
        .rmask(cpu_rmask), .wstrb(cpu_wstrb), .wdata(cpu_wdata), .selected(tmu_select),
        .rvalid(tmu_rvalid), .rdata(tmu_rdata), .timer_irq, .tclk_output_enable, .capture_dma,
        .capture_ack(capture_ack || capture_accept), .capture_value
    );

    fes_sh7091_intc intc (
        .clk, .reset, .request(cpu_valid), .address(cpu_addr[31:2]),
        .rmask(cpu_rmask), .wstrb(cpu_wstrb), .wdata(cpu_wdata[15:0]), .selected(intc_select),
        .rvalid(intc_rvalid), .rdata(intc_rdata), .ext_level(irq_level), .ext_event(irq_event),
        .timer_irq, .refresh_irq, .dma_irq, .cpu_ack(cpu_irq_ack), .cpu_level(cpu_irq_level),
        .cpu_event(cpu_irq_event), .ext_ack(irq_ack)
    );

    fes_sh7091_dmac dmac (
        .clk, .reset, .nmi, .reg_request(cpu_valid), .reg_addr(cpu_addr[31:2]),
        .reg_rmask(cpu_rmask), .reg_wstrb(cpu_wstrb), .reg_wdata(cpu_wdata), .reg_selected(dmac_select),
        .reg_rvalid(dmac_rvalid), .reg_rdata(dmac_rdata), .dreq_n, .dreq_ack, .irq(dma_irq),
        .capture_pending(capture_dma), .capture_value, .capture_accept,
        .mem_valid(dma_valid), .mem_burst(dma_burst), .mem_ready(dma_ready),
        .mem_addr(dma_addr), .mem_wdata(dma_wdata), .mem_rmask(dma_rmask), .mem_wstrb(dma_wstrb),
        .mem_rvalid(dma_rvalid), .mem_rdata
    );

    fes_sh7091_bus bus_mux (
        .clk, .reset, .cpu_valid(cpu_valid && !local_select), .cpu_lock, .cpu_sq,
        .cpu_addr, .cpu_wdata, .cpu_rmask, .cpu_wstrb, .cpu_ready(cpu_ext_ready), .cpu_rvalid(cpu_ext_rvalid),
        .dma_valid, .dma_burst, .dma_addr, .dma_wdata, .dma_rmask, .dma_wstrb, .dma_ready, .dma_rvalid,
        .mem_valid, .mem_lock, .mem_sq, .mem_addr, .mem_wdata, .mem_rmask, .mem_wstrb, .mem_ready, .mem_rvalid
    );

    assign local_select = bsc_select || tmu_select || intc_select || dmac_select;
    assign cpu_rvalid = bsc_rvalid || tmu_rvalid || intc_rvalid || dmac_rvalid || cpu_ext_rvalid;
    assign cpu_rdata = ({32{bsc_rvalid}} & bsc_rdata) | ({32{tmu_rvalid}} & tmu_rdata) |
                       ({32{intc_rvalid}} & intc_rdata) | ({32{dmac_rvalid}} & dmac_rdata) |
                       ({32{cpu_ext_rvalid}} & mem_rdata);
endmodule
