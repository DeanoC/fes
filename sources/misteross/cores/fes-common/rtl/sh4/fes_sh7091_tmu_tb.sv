module fes_sh7091_tmu_tb(input logic clk, output logic finished);
    logic reset, request, peripheral_tick, rtc_tick, tclk, capture_ack;
    logic [31:2] address;
    logic [31:0] wdata, tmu_rdata, intc_rdata, capture_value;
    logic [3:0] rmask, wstrb, timer_irq, cpu_level, ext_level;
    logic [11:0] ext_event, cpu_event;
    logic [1:0] refresh_irq;
    logic tmu_selected, intc_selected, tmu_rvalid, intc_rvalid;
    logic tclk_output_enable, capture_dma, cpu_ack, ext_ack;

    fes_sh7091_tmu tmu (
        .clk, .reset, .peripheral_tick, .rtc_tick, .tclk, .request, .address,
        .rmask, .wstrb, .wdata, .selected(tmu_selected), .rvalid(tmu_rvalid), .rdata(tmu_rdata),
        .timer_irq, .tclk_output_enable, .capture_dma, .capture_ack, .capture_value
    );
    logic nmi = 0, nmi_ack = 0, nmi_request, nmi_override;
    logic watchdog_tick = 0, watchdog_irq, wdt_selected, wdt_rvalid;
    logic [31:0] wdt_rdata;
    logic [1:0] watchdog_reset;
    logic [4:0] dma_irq = 0;
    logic [3:0] sci_irq = 0, scif_irq = 0;
    fes_sh7091_intc intc (
        .clk, .reset, .bus_tick(1'b1), .nmi, .nmi_ack,
        .nmi_request, .nmi_block_override(nmi_override),
        .request, .address, .rmask, .wstrb, .wdata(wdata[15:0]),
        .selected(intc_selected), .rvalid(intc_rvalid), .rdata(intc_rdata),
        .ext_level, .ext_event, .timer_irq, .refresh_irq, .dma_irq, .rtc_irq(3'b0), .sci_irq, .scif_irq, .watchdog_irq, .cpu_ack,
        .cpu_level, .cpu_event, .ext_ack
    );

    fes_sh7091_wdt wdt (
        .clk, .reset, .tick(watchdog_tick), .request, .address, .rmask, .wstrb,
        .wdata(wdata[15:0]), .selected(wdt_selected), .rvalid(wdt_rvalid),
        .rdata(wdt_rdata), .interval_irq(watchdog_irq), .reset_request(watchdog_reset)
    );

    task automatic write_reg(input logic [31:0] byte_addr, value, input logic [3:0] mask);
        @(negedge clk);
        assert (byte_addr[1:0] == 0);
        address = byte_addr[31:2];
        wdata = value;
        wstrb = mask;
        request = 1;
        @(negedge clk);
        assert ($onehot({tmu_selected, intc_selected, wdt_selected}) && !(tmu_rvalid || intc_rvalid || wdt_rvalid))
            else $fatal(1, "Timer or interrupt write at %h was not decoded", byte_addr);
        request = 0;
    endtask

    task automatic read_reg(input logic [31:0] byte_addr, expected, input logic [3:0] mask);
        logic [31:0] value;
        @(negedge clk);
        assert (byte_addr[1:0] == 0);
        address = byte_addr[31:2];
        rmask = mask;
        wstrb = 0;
        request = 1;
        @(negedge clk);
        value = wdt_rvalid ? wdt_rdata : intc_rvalid ? intc_rdata : tmu_rdata;
        assert ($onehot({tmu_rvalid, intc_rvalid, wdt_rvalid}) && value == expected)
            else $fatal(1, "Timer/INTC/WDT read %h returned %h, expected %h", byte_addr, value, expected);
        request = 0;
    endtask

    initial begin
        reset = 1;
        request = 0;
        finished = 0;
        peripheral_tick = 0;
        rtc_tick = 0;
        tclk = 0;
        capture_ack = 0;
        cpu_ack = 0;
        ext_level = 0;
        ext_event = 0;
        refresh_irq = 0;
        @(negedge clk);
        reset = 0;
        read_reg('hffd8_000c, '1, 4'hf);
        write_reg('hffd0_0004, 'h5060, 4'h3);
        write_reg('hffd0_0008, 'h070f, 4'h3);
        read_reg('hffd0_0008, 'h0700, 4'h3);
        write_reg('hffd8_0008, 7, 4'hf);
        write_reg('hffd8_000c, 0, 4'hf);
        write_reg('hffd8_0010, 'h20, 4'h3);
        write_reg('hffd8_0004, 1, 4'h1);
        peripheral_tick = 1;
        repeat (3) @(negedge clk);
        assert (!timer_irq[0]) else $fatal(1, "TMU0 underflow was early");
        @(negedge clk);
        peripheral_tick = 0;
        @(negedge clk);
        assert (cpu_level == 5 && cpu_event == 'h400)
            else $fatal(1, "TMU0 underflow did not select the programmed priority");
        read_reg('hffd8_0010, 'h120, 4'h3);
        read_reg('hffd8_000c, 7, 4'hf);

        write_reg('hffd8_0024, 'h1234_5678, 4'hf);
        write_reg('hffd8_0028, 'hc7, 4'h3);
        write_reg('hffd8_0004, 5, 4'h1);
        tclk = 1;
        repeat (4) @(negedge clk);
        assert (capture_dma && capture_value == 'h1234_5678 && cpu_level == 6 && cpu_event == 'h460)
            else $fatal(1, "TMU2 capture did not request service");
        read_reg('hffd8_002c, 'h1234_5678, 4'hf);
        read_reg('hffd8_0024, 'h1234_5677, 4'hf);
        capture_ack = 1;
        @(negedge clk);
        capture_ack = 0;
        assert (!capture_dma) else $fatal(1, "TMU2 DMA acknowledge failed");

        refresh_irq = 2'b11;
        @(negedge clk);
        assert (cpu_level == 7 && cpu_event == 'h580)
            else $fatal(1, "Refresh compare priority was incorrect");
        cpu_ack = 1;
        assert (!ext_ack) else $fatal(1, "On-chip interrupt acknowledged external device");
        cpu_ack = 0;
        ext_level = 7;
        ext_event = 'h2a0;
        repeat (2) @(negedge clk);
        cpu_ack = 1;
        @(negedge clk);
        assert (cpu_level == 7 && cpu_event == 'h2a0 && ext_ack)
            else $fatal(1, "External interrupt tie priority or acknowledgement was incorrect");
        cpu_ack = 0;
        ext_level = 0;
        refresh_irq = 0;

        write_reg('hffd8_0028, 'hc7, 4'h3);
        write_reg('hffd8_0010, 'h20, 4'h3);
        write_reg('hffd8_0000, 1, 4'h1);
        assert (tclk_output_enable && cpu_level == 0)
            else $fatal(1, "Clearing TMU flags did not release interrupt");
        read_reg('hffd0_0000, 'h8000, 4'h3);
        write_reg('hffd0_0000, 'hc2ff, 4'h3);
        read_reg('hffd0_0000, 'hc280, 4'h3);
        ext_level = 4'b1100;
        @(negedge clk);
        assert (cpu_level == 7 && cpu_event == 'h300 && nmi_override)
            else $fatal(1, "ICR independent IRL and NMIB settings were ignored");
        nmi = 1;
        @(negedge clk);
        assert (nmi_request && cpu_level == 0) else $fatal(1, "Falling NMI or MAI failed");
        nmi_ack = 1;
        @(negedge clk);
        nmi_ack = 0;
        assert (!nmi_request && !nmi_override);
        read_reg('hffd0_0000, 'h4080, 4'h3);
        write_reg('hffd0_0000, 'h0100, 4'h3);
        nmi = 0;
        @(negedge clk);
        assert (!nmi_request) else $fatal(1, "NMI appeared during the polarity guard");
        nmi = 1;
        ext_level = 0;
        write_reg('hffd0_0000, 'h0100, 4'h3);
        repeat (3) @(negedge clk);
        assert (!nmi_request);
        nmi = 0;
        @(negedge clk);
        assert (nmi_request) else $fatal(1, "Rising NMI was lost after the polarity guard");

        write_reg('hffd0_0004, 'h5000, 4'h3);
        write_reg('hffd0_0008, 'h5500, 4'h3);
        write_reg('hffd0_000c, 'h0500, 4'h3);
        dma_irq = 5'b10001;
        refresh_irq = 2'b01;
        @(negedge clk);
        assert (cpu_level == 5 && cpu_event == 'h640);
        dma_irq = 0;
        write_reg('hffc0_0008, 'h5aff, 4'hf);
        write_reg('hffc0_0008, 'h00ff, 4'h3);
        read_reg('hffc0_0008, 0, 4'h1);
        write_reg('hffc0_0008, 'h5aff, 4'h3);
        write_reg('hffc0_000c, 'ha580, 4'h3);
        watchdog_tick = 1;
        repeat (31) @(negedge clk);
        assert (!watchdog_irq && watchdog_reset == 0);
        @(negedge clk);
        assert (watchdog_irq && watchdog_reset == 0);
        watchdog_tick = 0;
        @(negedge clk);
        assert (cpu_level == 5 && cpu_event == 'h560)
            else $fatal(1, "WDT interval IRQ did not beat the tied refresh IRQ");
        read_reg('hffc0_000c, 'h88, 4'h1);
        read_reg('hffc0_0008, 0, 4'h1);
        write_reg('hffc0_000c, 'ha518, 4'h3);
        assert (watchdog_irq);
        write_reg('hffc0_000c, 'ha500, 4'h3);
        assert (!watchdog_irq);
        write_reg('hffc0_0008, 'h5aff, 4'h3);
        write_reg('hffc0_000c, 'ha5e1, 4'h3);
        watchdog_tick = 1;
        repeat (63) @(negedge clk);
        assert (!watchdog_irq && watchdog_reset == 0);
        @(negedge clk);
        watchdog_tick = 0;
        assert (!watchdog_irq && watchdog_reset == 2'b10)
            else $fatal(1, "WDT manual reset pulse was incorrect");
        read_reg('hffc0_000c, 'hf1, 4'h1);
        assert (watchdog_reset == 0);
        write_reg('hffd0_0008, 'h5530, 4'h3);
        write_reg('hffd0_000c, 'h0040, 4'h3);
        sci_irq = 4'b0101;
        scif_irq = 4'b1100;
        refresh_irq = 0;
        @(negedge clk);
        assert (cpu_level == 4 && cpu_event == 'h740);
        write_reg('hffd0_0008, 'h5540, 4'h3);
        assert (cpu_level == 4 && cpu_event == 'h740);
        @(negedge clk);
        assert (cpu_level == 4 && cpu_event == 'h4e0);
        sci_irq = 4'b1000;
        @(negedge clk);
        assert (cpu_event == 'h540);
        sci_irq = 0;
        scif_irq = 4'b1101;
        @(negedge clk);
        assert (cpu_event == 'h700);
        finished = 1;
    end
endmodule
