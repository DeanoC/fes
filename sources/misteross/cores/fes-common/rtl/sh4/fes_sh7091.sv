// SH7091 core with on-chip peripheral routing and an external memory port.
module fes_sh7091 #(
    parameter logic [31:0] RESET_PC = 32'ha000_0000,
    parameter logic [15:0] RESET_CCR = 16'b0
) (
    input  logic        clk,
    input  logic        reset,
    input  logic        bus_tick,
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
    logic [31:0] cpu_rdata, bsc_rdata;
    logic bsc_select, bsc_rvalid;

    fes_sh4 #(.RESET_PC(RESET_PC), .RESET_CCR(RESET_CCR)) cpu (
        .clk, .reset, .irq_level, .irq_event, .nmi, .nmi_while_blocked, .irq_ack,
        .mem_valid(cpu_valid), .mem_ready(bsc_select || mem_ready), .mem_lock, .mem_sq,
        .mem_rmask, .mem_addr, .mem_wstrb, .mem_wdata,
        .mem_rvalid(cpu_rvalid), .mem_rdata(cpu_rdata),
        .retired, .retired_pc, .fault, .fault_pc
    );

    fes_sh7091_bsc bsc (
        .clk, .reset, .bus_tick, .request(cpu_valid), .address(mem_addr[31:2]),
        .rmask(mem_rmask), .wstrb(mem_wstrb), .wdata(mem_wdata), .selected(bsc_select),
        .rvalid(bsc_rvalid), .rdata(bsc_rdata), .refresh_irq, .sdram_command,
        .sdram_mode_set, .sdram_mode_address
    );

    assign mem_valid = cpu_valid && !bsc_select;
    assign cpu_rvalid = bsc_rvalid || mem_rvalid;
    assign cpu_rdata = bsc_rvalid ? bsc_rdata : mem_rdata;
endmodule
