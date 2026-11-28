// SH7091 interrupt control, pin modes and on-chip priority arbitration.
module fes_sh7091_intc (
    input  logic        clk, reset, bus_tick,
    input  logic        nmi, nmi_ack,
    output logic        nmi_request, nmi_block_override,
    input  logic        request,
    input  logic [31:2] address,
    input  logic [3:0]  rmask, wstrb,
    input  logic [15:0] wdata,
    output logic        selected, rvalid,
    output logic [31:0] rdata,
    input  logic [3:0]  ext_level,
    input  logic [11:0] ext_event,
    input  logic [3:0]  timer_irq,
    input  logic [1:0]  refresh_irq,
    input  logic [4:0]  dma_irq,
    input  logic [2:0]  rtc_irq,
    input  logic        watchdog_irq, cpu_ack,
    output logic [3:0]  cpu_level,
    output logic [11:0] cpu_event,
    output logic        ext_ack
);
    logic [15:0] ipra, iprb, iprc, selected_value;
    logic [14:0] icr;
    logic nmi_previous, write_icr, change_nmi_edge;
    logic [2:0] nmi_inhibit;
    typedef struct packed {
        logic [3:0] level;
        logic [11:0] event_code;
        logic external_pin;
    } interrupt_t;
    interrupt_t external_irq, dma, tmu0, tmu1, tmu2, rtc, watchdog, refresh, winner;
    interrupt_t pin_or_dma, tmu01, tmu2_or_rtc, watchdog_or_refresh;
    logic mask_interrupts, cpu_external, ack_external;

    function automatic interrupt_t higher_priority(input interrupt_t first, second);
        return first.level >= second.level ? first : second;
    endfunction

    assign selected = address[31:4] == 28'hffd0000;
    assign write_icr = request && selected && address[3:2] == 0 && wstrb == 4'h3;
    assign change_nmi_edge = write_icr && wdata[8] != icr[8];
    assign nmi_block_override = icr[9];
    assign mask_interrupts = icr[14] && nmi;
    assign ext_ack = cpu_ack && ack_external;

    assign tmu0 = '{timer_irq[0] ? ipra[15:12] : 4'b0, 12'h400, 1'b0};
    assign tmu1 = '{timer_irq[1] ? ipra[11:8] : 4'b0, 12'h420, 1'b0};
    assign tmu2 = '{(|timer_irq[3:2]) ? ipra[7:4] : 4'b0,
                    timer_irq[2] ? 12'h440 : 12'h460, 1'b0};
    assign watchdog = '{watchdog_irq ? iprb[15:12] : 4'b0, 12'h560, 1'b0};
    assign refresh = '{(|refresh_irq) ? iprb[11:8] : 4'b0,
                       refresh_irq[0] ? 12'h580 : 12'h5a0, 1'b0};

    // Compare sources in pairs. Each comparison gives ties to the left
    // source, preserving the order specified by the interrupt controller.
    assign pin_or_dma = higher_priority(external_irq, dma);
    assign tmu01 = higher_priority(tmu0, tmu1);
    assign tmu2_or_rtc = higher_priority(tmu2, rtc);
    assign watchdog_or_refresh = higher_priority(watchdog, refresh);
    assign winner = higher_priority(higher_priority(pin_or_dma, tmu01),
                                    higher_priority(tmu2_or_rtc, watchdog_or_refresh));

    always_comb begin
        case (address[3:2])
            1: selected_value = ipra;
            2: selected_value = iprb;
            3: selected_value = iprc;
            default: selected_value = {~nmi, icr[14:0]};
        endcase

        external_irq = '{ext_level, ext_event, ext_level != 0};
        // ext_level is the inversion of the four active-low IRL pins.
        // The board's custom event applies in encoded mode.
        if (icr[7]) begin
            casez (ext_level)
                4'b???1: external_irq = '{4'd13, 12'h240, 1'b1};
                4'b??10: external_irq = '{4'd10, 12'h2a0, 1'b1};
                4'b?100: external_irq = '{4'd7, 12'h300, 1'b1};
                4'b1000: external_irq = '{4'd4, 12'h360, 1'b1};
                default: external_irq = '0;
            endcase
        end

        dma = '{(|dma_irq) ? iprc[11:8] : 4'b0, 12'h6c0, 1'b0};
        casez (dma_irq)
            5'b????1: dma.event_code = 12'h640;
            5'b???10: dma.event_code = 12'h660;
            5'b??100: dma.event_code = 12'h680;
            5'b?1000: dma.event_code = 12'h6a0;
            default: ;
        endcase

        rtc = '{(|rtc_irq) ? ipra[3:0] : 4'b0, 12'h4c0, 1'b0};
        if (rtc_irq[0])
            rtc.event_code = 12'h480;
        else if (rtc_irq[1])
            rtc.event_code = 12'h4a0;
    end

    always_ff @(posedge clk) begin
        if (reset) begin
            icr <= 0;
            nmi_previous <= nmi;
            nmi_inhibit <= 0;
            nmi_request <= 0;
            ipra <= 0;
            iprb <= 0;
            iprc <= 0;
            cpu_level <= 0;
            cpu_event <= 0;
            cpu_external <= 0;
            ack_external <= 0;
            rvalid <= 0;
        end else begin
            nmi_previous <= nmi;
            nmi_request <= nmi_inhibit == 0 && nmi != nmi_previous && nmi != icr[8] &&
                           !change_nmi_edge;
            if (change_nmi_edge)
                nmi_inhibit <= 3'd6;
            else if (bus_tick && nmi_inhibit != 0)
                nmi_inhibit <= nmi_inhibit - 3'd1;

            if (write_icr)
                icr <= wdata[14:0] & 15'h4380;
            if (nmi_ack)
                icr[9] <= 0;

            cpu_level <= mask_interrupts ? 4'b0 : winner.level;
            cpu_event <= mask_interrupts ? 12'b0 : winner.event_code;
            cpu_external <= !mask_interrupts && winner.external_pin;
            ack_external <= cpu_external;
            rvalid <= request && selected && wstrb == 0;
            if (request && selected && wstrb == 0)
                rdata <= rmask == 4'h3 ? {16'b0, selected_value} : 32'b0;
            if (request && selected && wstrb == 4'h3) case (address[3:2])
                1: ipra <= wdata;
                2: iprb <= wdata & 16'hfff0;
                3: iprc <= wdata;
                default: ;
            endcase
        end
    end
endmodule
