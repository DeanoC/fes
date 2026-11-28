// SH7091 core with on-chip peripheral routing and an external memory port.
module fes_sh7091 #(
    parameter logic [31:0] RESET_PC = 32'ha000_0000,
    parameter logic [15:0] RESET_CCR = 16'b0
) (
    input  logic        clk,
    input  logic        reset,
    input  logic        bus_tick, watchdog_tick,
    output logic [1:0]  watchdog_reset,
    input  logic        peripheral_tick, rtc_tick, tclk,
    input  logic        sci_rxd, sci_sck, scif_rxd, scif_cts_n, scif_tick,
    output logic        sci_txd, sci_txd_enable, sci_sck_out, sci_sck_enable,
    output logic        scif_txd, scif_txd_enable, scif_rts_n,
    input  logic        capture_ack,
    input  logic [1:0]  dreq_n,
    output logic [1:0]  dreq_ack,
    output logic        tclk_output_enable, tclk_output, capture_dma,
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
    logic system_reset, cpu_valid, cpu_rvalid;
    logic nmi_request, nmi_ack, nmi_block_override, watchdog_irq, wdt_select, wdt_rvalid;
    logic [31:0] wdt_rdata, rtc_rdata;
    logic rtc_select, rtc_rvalid, rtc_tmu_tick;
    logic [2:0] rtc_irq;
    logic [31:0] cpu_rdata, bsc_rdata, tmu_rdata, intc_rdata;
    logic [3:0] timer_irq, cpu_irq_level;
    logic [11:0] cpu_irq_event;
    logic cpu_irq_ack, tmu_select, tmu_rvalid, intc_select, intc_rvalid, local_select;
    logic bsc_select, bsc_rvalid, dmac_select, dmac_rvalid, cpu_ext_ready, cpu_ext_rvalid;
    logic cpu_lock, cpu_sq, dma_valid, dma_burst, dma_ready, dma_rvalid, capture_accept;
    logic [3:0] cpu_rmask, cpu_wstrb, dma_rmask, dma_wstrb;
    logic [4:0] dma_irq;
    logic [3:0] sci_irq, scif_irq, serial_request;
    logic [1:0] sci_ready, scif_ready;
    logic [1:0] serial_select, serial_grant, serial_dma_owner, serial_rvalid, dma_serial;
    logic dma_ext_ready, dma_ext_rvalid;
    logic [31:0] sci_rdata, scif_rdata, dma_rdata;
    logic [31:0] cpu_addr, cpu_wdata, dma_addr, dma_wdata, dmac_rdata, capture_value;

    assign system_reset = reset || (|watchdog_reset);

    fes_sh4 #(.RESET_PC(RESET_PC), .RESET_CCR(RESET_CCR)) cpu (
        .clk, .reset(system_reset), .manual_reset(watchdog_reset[1] && !reset), .irq_level(cpu_irq_level), .irq_event(cpu_irq_event),
        .nmi(nmi_request), .nmi_while_blocked(nmi_while_blocked || nmi_block_override), .irq_ack(cpu_irq_ack), .nmi_ack,
        .mem_valid(cpu_valid), .mem_ready((local_select && !(|(serial_select & serial_grant))) || cpu_ext_ready), .mem_lock(cpu_lock), .mem_sq(cpu_sq),
        .mem_rmask(cpu_rmask), .mem_addr(cpu_addr), .mem_wstrb(cpu_wstrb), .mem_wdata(cpu_wdata),
        .mem_rvalid(cpu_rvalid), .mem_rdata(cpu_rdata),
        .retired, .retired_pc, .fault, .fault_pc
    );

    fes_sh7091_bsc bsc (
        .clk, .reset(system_reset), .bus_tick, .request(cpu_valid), .address(cpu_addr[31:2]),
        .rmask(cpu_rmask), .wstrb(cpu_wstrb), .wdata(cpu_wdata), .selected(bsc_select),
        .rvalid(bsc_rvalid), .rdata(bsc_rdata), .refresh_irq, .sdram_command,
        .sdram_mode_set, .sdram_mode_address
    );

    fes_sh7091_tmu tmu (
        .clk, .reset(system_reset), .peripheral_tick, .rtc_tick(rtc_tmu_tick), .tclk, .request(cpu_valid), .address(cpu_addr[31:2]),
        .rmask(cpu_rmask), .wstrb(cpu_wstrb), .wdata(cpu_wdata), .selected(tmu_select),
        .rvalid(tmu_rvalid), .rdata(tmu_rdata), .timer_irq, .tclk_output_enable, .capture_dma,
        .capture_ack(capture_ack || capture_accept), .capture_value
    );

    fes_sh7091_intc intc (
        .clk, .reset(system_reset), .bus_tick, .nmi, .nmi_ack, .nmi_request, .nmi_block_override,
        .request(cpu_valid), .address(cpu_addr[31:2]),
        .rmask(cpu_rmask), .wstrb(cpu_wstrb), .wdata(cpu_wdata[15:0]), .selected(intc_select),
        .rvalid(intc_rvalid), .rdata(intc_rdata), .ext_level(irq_level), .ext_event(irq_event),
        .timer_irq, .refresh_irq, .dma_irq, .rtc_irq, .sci_irq, .scif_irq, .watchdog_irq, .cpu_ack(cpu_irq_ack), .cpu_level(cpu_irq_level),
        .cpu_event(cpu_irq_event), .ext_ack(irq_ack)
    );

    fes_sh7091_dmac dmac (
        .clk, .reset(system_reset), .nmi(nmi_request), .reg_request(cpu_valid), .reg_addr(cpu_addr[31:2]),
        .reg_rmask(cpu_rmask), .reg_wstrb(cpu_wstrb), .reg_wdata(cpu_wdata), .reg_selected(dmac_select),
        .reg_rvalid(dmac_rvalid), .reg_rdata(dmac_rdata), .dreq_n, .dreq_ack, .irq(dma_irq),
        .capture_pending(capture_dma), .capture_value, .capture_accept,
        .serial_request, .mem_valid(dma_valid), .mem_burst(dma_burst), .mem_serial(dma_serial), .mem_ready(dma_ready),
        .mem_addr(dma_addr), .mem_wdata(dma_wdata), .mem_rmask(dma_rmask), .mem_wstrb(dma_wstrb),
        .mem_rvalid(dma_rvalid), .mem_rdata(dma_rdata)
    );

    fes_sh7091_wdt wdt (
        .clk, .reset, .tick(watchdog_tick), .request(cpu_valid),
        .address(cpu_addr[31:2]), .rmask(cpu_rmask), .wstrb(cpu_wstrb), .wdata(cpu_wdata[15:0]),
        .selected(wdt_select), .rvalid(wdt_rvalid), .rdata(wdt_rdata),
        .interval_irq(watchdog_irq), .reset_request(watchdog_reset)
    );

    fes_sh7091_rtc rtc (
        .clk, .reset(system_reset), .power_reset(reset || watchdog_reset[0]), .tick(rtc_tick),
        .request(cpu_valid), .address(cpu_addr[31:2]), .rmask(cpu_rmask), .wstrb(cpu_wstrb),
        .wdata(cpu_wdata[15:0]), .selected(rtc_select), .rvalid(rtc_rvalid), .rdata(rtc_rdata),
        .irq(rtc_irq), .tmu_tick(rtc_tmu_tick), .clock_out(tclk_output)
    );

    // A DMA beat targeting the serial block holds that local register port
    // for one cycle. A CPU request to the same block retries on the next cycle.
    assign serial_select[0] = cpu_addr[31:5] == 27'h7ff0000;
    assign serial_select[1] = cpu_addr[31:6] == 26'h3ffa000;
    assign serial_grant[0] = dma_valid && dma_serial[0] && sci_ready[dma_wstrb == 0];
    assign serial_grant[1] = dma_valid && dma_serial[1] && scif_ready[dma_wstrb == 0];
    assign dma_ready = (|dma_serial) ? (|serial_grant) : dma_ext_ready;
    assign dma_rvalid = dma_ext_rvalid || (|(serial_rvalid & serial_dma_owner));
    assign dma_rdata = ({32{dma_ext_rvalid}} & mem_rdata) |
                       ({32{serial_rvalid[0] && serial_dma_owner[0]}} & sci_rdata) |
                       ({32{serial_rvalid[1] && serial_dma_owner[1]}} & scif_rdata);

    always_ff @(posedge clk)
        serial_dma_owner <= system_reset ? 2'b0 : serial_grant;

    fes_sh7091_sci sci (
        .clk, .reset(system_reset), .peripheral_tick,
        .request(serial_grant[0] || (cpu_valid && serial_select[0])), .dma_access(serial_grant[0]),
        .index(serial_grant[0] ? dma_addr[5:2] : cpu_addr[5:2]),
        .rmask(serial_grant[0] ? dma_rmask : cpu_rmask), .wstrb(serial_grant[0] ? dma_wstrb : cpu_wstrb),
        .wdata(serial_grant[0] ? dma_wdata[7:0] : cpu_wdata[7:0]), .rvalid(serial_rvalid[0]), .rdata(sci_rdata),
        .rxd(sci_rxd), .sck_in(sci_sck), .txd(sci_txd), .txd_enable(sci_txd_enable),
        .sck_out(sci_sck_out), .sck_enable(sci_sck_enable),
        .irq(sci_irq), .dma_request(serial_request[1:0]), .dma_ready(sci_ready)
    );

    fes_sh7091_scif scif (
        .clk, .reset(system_reset), .peripheral_tick, .external_tick(scif_tick),
        .request(serial_grant[1] || (cpu_valid && serial_select[1])), .dma_access(serial_grant[1]),
        .index(serial_grant[1] ? dma_addr[5:2] : cpu_addr[5:2]),
        .rmask(serial_grant[1] ? dma_rmask : cpu_rmask), .wstrb(serial_grant[1] ? dma_wstrb : cpu_wstrb),
        .wdata(serial_grant[1] ? dma_wdata[7:0] : cpu_wdata[7:0]), .rvalid(serial_rvalid[1]), .rdata(scif_rdata),
        .rxd(scif_rxd), .cts_n(scif_cts_n), .txd(scif_txd), .txd_enable(scif_txd_enable), .rts_n(scif_rts_n),
        .irq(scif_irq), .dma_request(serial_request[3:2]), .dma_ready(scif_ready)
    );

    fes_sh7091_bus bus_mux (
        .clk, .reset(system_reset), .cpu_valid(cpu_valid && !local_select), .cpu_lock, .cpu_sq,
        .cpu_addr, .cpu_wdata, .cpu_rmask, .cpu_wstrb, .cpu_ready(cpu_ext_ready), .cpu_rvalid(cpu_ext_rvalid),
        .dma_valid(dma_valid && !(|dma_serial)), .dma_burst, .dma_addr, .dma_wdata, .dma_rmask, .dma_wstrb,
        .dma_ready(dma_ext_ready), .dma_rvalid(dma_ext_rvalid),
        .mem_valid, .mem_lock, .mem_sq, .mem_addr, .mem_wdata, .mem_rmask, .mem_wstrb, .mem_ready, .mem_rvalid
    );

    assign local_select = bsc_select || tmu_select || intc_select || dmac_select || wdt_select || rtc_select || (|serial_select);
    assign cpu_rvalid = bsc_rvalid || tmu_rvalid || intc_rvalid || dmac_rvalid || wdt_rvalid || rtc_rvalid || (|(serial_rvalid & ~serial_dma_owner)) || cpu_ext_rvalid;
    assign cpu_rdata = ({32{bsc_rvalid}} & bsc_rdata) | ({32{tmu_rvalid}} & tmu_rdata) |
                       ({32{intc_rvalid}} & intc_rdata) | ({32{dmac_rvalid}} & dmac_rdata) |
                       ({32{wdt_rvalid}} & wdt_rdata) | ({32{rtc_rvalid}} & rtc_rdata) |
                       ({32{serial_rvalid[0] && !serial_dma_owner[0]}} & sci_rdata) |
                       ({32{serial_rvalid[1] && !serial_dma_owner[1]}} & scif_rdata) |
                       ({32{cpu_ext_rvalid}} & mem_rdata);
endmodule
