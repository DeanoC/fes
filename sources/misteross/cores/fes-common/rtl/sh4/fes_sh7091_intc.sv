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
    logic [3:0] levels [0:9];
    logic [11:0] events [0:9];
    logic [9:0] active;
    logic [3:0] selected_level;
    logic [11:0] selected_event;
    logic next_external, cpu_external, ack_external;

    assign selected = address[31:4] == 28'hffd0000;
    assign write_icr = request && selected && address[3:2] == 0 && wstrb == 4'h3;
    assign change_nmi_edge = write_icr && wdata[8] != icr[8];
    assign nmi_block_override = icr[9];
    assign active = {refresh_irq, watchdog_irq, rtc_irq, timer_irq};
    assign levels[0] = ipra[15:12];
    assign levels[1] = ipra[11:8];
    assign levels[2] = ipra[7:4];
    assign levels[3] = ipra[7:4];
    assign levels[4] = ipra[3:0];
    assign levels[5] = ipra[3:0];
    assign levels[6] = ipra[3:0];
    assign levels[7] = iprb[15:12];
    assign levels[8] = iprb[11:8];
    assign levels[9] = iprb[11:8];
    assign events[0] = 12'h400;
    assign events[1] = 12'h420;
    assign events[2] = 12'h440;
    assign events[3] = 12'h460;
    assign events[4] = 12'h480;
    assign events[5] = 12'h4a0;
    assign events[6] = 12'h4c0;
    assign events[7] = 12'h560;
    assign events[8] = 12'h580;
    assign events[9] = 12'h5a0;
    assign ext_ack = cpu_ack && ack_external;

    always_comb begin
        case (address[3:2])
            1: selected_value = ipra;
            2: selected_value = iprb;
            3: selected_value = iprc;
            default: selected_value = {~nmi, icr[14:0]};
        endcase

        selected_level = ext_level;
        selected_event = ext_event;
        // ext_level is the inversion of the four active-low IRL pins.
        // The board's custom event applies in encoded mode.
        if (icr[7]) begin
            casez (ext_level)
                4'b???1: begin selected_level = 13; selected_event = 12'h240; end
                4'b??10: begin selected_level = 10; selected_event = 12'h2a0; end
                4'b?100: begin selected_level = 7; selected_event = 12'h300; end
                4'b1000: begin selected_level = 4; selected_event = 12'h360; end
                default: begin selected_level = 0; selected_event = 0; end
            endcase
        end
        next_external = selected_level != 0;
        if ((|dma_irq) && iprc[11:8] > selected_level) begin
            selected_level = iprc[11:8];
            casez (dma_irq)
                5'b????1: selected_event = 12'h640;
                5'b???10: selected_event = 12'h660;
                5'b??100: selected_event = 12'h680;
                5'b?1000: selected_event = 12'h6a0;
                default: selected_event = 12'h6c0;
            endcase
            next_external = 0;
        end
        for (int i = 0; i < 10; i++) begin
            if (active[i] && levels[i] > selected_level) begin
                selected_level = levels[i];
                selected_event = events[i];
                next_external = 0;
            end
        end
        if (icr[14] && nmi) begin
            selected_level = 0;
            selected_event = 0;
            next_external = 0;
        end
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

            cpu_level <= selected_level;
            cpu_event <= selected_event;
            cpu_external <= next_external;
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
