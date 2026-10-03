// SPDX-License-Identifier: GPL-3.0-or-later
// Original MC68901 register/timer/interrupt model for the 520ST.
// Programming reference: Motorola MC68HC901 User's Manual, sections 3-7,
// https://www.nxp.com/docs/en/reference-manual/MC68901UM.pdf
// ST OS use: EmuTOS VERSION_1_4 bios/mfp.{c,h}: VR=$48, timer C /64, 192.
// https://github.com/emutos/emutos/blob/VERSION_1_4/bios/mfp.c
// All inputs are synchronous to clk. timer_ce represents the independent
// 2.4576 MHz XTAL clock; the motherboard generates it from its system clock.
// No serial connector is provided: RX stays idle and the TX timing model
// ends inside this module. Synchronous protocols, loopback, auto-turnaround,
// serial error detection and half-stop-bit timing are not implemented.
// Reset initializes undefined hardware data-register values to zero. Timer
// and GPIO edge sampling use clk; this is not cycle-exact MFP bus timing.
module st_mfp (
    input  wire clk,
    input  wire reset,
    input  wire timer_ce,
    input  wire req,
    input  wire [5:0] addr,
    input  wire write,
    input  wire [7:0] wdata,
    output reg  [7:0] rdata,
    output wire ack,
    input  wire [7:0] gpip,
    input  wire timer_a,
    input  wire timer_b,
    output wire irq,
    output wire [7:0] irq_vector,
    input  wire iack
);
    reg req_seen, iack_seen;
    reg [7:0] gpdr, aer, ddr, vr;
    reg [15:0] ier, ipr, isr, imr;
    reg [15:0] next_ier, next_ipr, next_isr, next_imr;
    reg [7:0] next_vr;
    reg [7:0] gpip_previous;
    reg [1:0] timer_previous;
    reg [3:0] control [0:3];
    reg [7:0] reload [0:3], count [0:3], prescale [0:3];
    reg [3:0] timer_output;
    reg [3:0] timer_count, timer_fire;
    reg [15:0] events;
    reg [7:0] read_value;
    reg selected;
    reg [3:0] channel;
    reg blocked;
    reg [7:0] acknowledged_vector;
    reg [7:0] scr, ucr;
    reg [1:0] rsr_control;
    reg [4:0] tsr_control;
    reg tx_initialized, tx_buffer_full, tx_busy, tx_underrun, tx_end;
    reg [3:0] tx_clock_count;
    reg [4:0] tx_bits;
    wire transfer = req && !req_seen;
    wire writing = transfer && write;
    wire [7:0] gpip_level = gpip ^ aer;
    wire [1:0] timer_level = {timer_b ^ aer[3], timer_a ^ aer[4]};
    wire [7:0] gpip_edge = gpip_previous & ~gpip_level & ~ddr;
    wire [1:0] timer_edge = timer_previous & ~timer_level;
    wire [1:0] timer_end_edge = ~timer_previous & timer_level;
    wire tx_clock = timer_fire[3] && !timer_output[3];
    wire [4:0] character_bits = 5'd8 - {3'd0, ucr[6:5]} +
        (ucr[4:3] == 0 ? 5'd0 : 5'd1) + (ucr[2] ? 5'd1 : 5'd0) +
        (ucr[4:3] == 0 ? 5'd0 : ucr[4] ? 5'd2 : 5'd1);

    assign ack = req && req_seen && !reset;
    assign irq = selected && !reset;
    // Preserve the acknowledged vector for the whole stretched IACK cycle.
    assign irq_vector = iack_seen ? acknowledged_vector :
                        selected ? {vr[7:4], channel} : 8'h18;

    function automatic [7:0] divisor(input [2:0] mode);
        case (mode)
            3'd1: divisor = 8'd4;
            3'd2: divisor = 8'd10;
            3'd3: divisor = 8'd16;
            3'd4: divisor = 8'd50;
            3'd5: divisor = 8'd64;
            3'd6: divisor = 8'd100;
            3'd7: divisor = 8'd200;
            default: divisor = 8'd1;
        endcase
    endfunction

    integer arbitration_index;
    always @* begin
        selected = 1'b0;
        channel = 4'd0;
        blocked = 1'b0;
        for (arbitration_index = 15; arbitration_index >= 0;
             arbitration_index = arbitration_index - 1) begin
            // An in-service channel also blocks another occurrence of itself.
            if (vr[3] && isr[arbitration_index]) blocked = 1'b1;
            if (!selected && !blocked && ipr[arbitration_index] && imr[arbitration_index]) begin
                selected = 1'b1;
                channel = 4'(arbitration_index);
            end
        end
    end

    integer timer_index;
    always @* begin
        timer_count = 4'd0;
        timer_fire = 4'd0;
        for (timer_index = 0; timer_index < 4; timer_index = timer_index + 1) begin
            if (control[timer_index] == 4'd8 && timer_index < 2)
                timer_count[timer_index] = timer_edge[timer_index];
            else if (control[timer_index][2:0] != 0 &&
                     (!control[timer_index][3] || !timer_level[timer_index % 2]))
                timer_count[timer_index] = timer_ce &&
                    prescale[timer_index] == divisor(control[timer_index][2:0]) - 8'd1;
            timer_fire[timer_index] = timer_count[timer_index] && count[timer_index] == 8'd1;
        end
    end

    always @* begin
        // Motorola's channel numbers are not GPIP bit numbers: 4/5 map to
        // 6/7 and 6/7 map to 14/15. In pulse-width mode TAI/TBI replace I4/I3.
        events = 16'd0;
        events[3:0] = gpip_edge[3:0];
        events[6] = gpip_edge[4];
        events[7] = gpip_edge[5];
        events[14] = gpip_edge[6];
        events[15] = gpip_edge[7];
        if (control[0] > 4'd8) events[6] = timer_end_edge[0];
        if (control[1] > 4'd8) events[3] = timer_end_edge[1];
        events[13] = timer_fire[0];
        events[8] = timer_fire[1];
        events[5] = timer_fire[2];
        events[4] = timer_fire[3];
        events[10] = tx_clock && tsr_control[0] && !tsr_control[3] &&
                     !tx_busy && tx_buffer_full;
        events[9] = (tx_clock && tx_busy &&
                     (!ucr[7] || tx_clock_count == 4'd15) && tx_bits == 5'd1 &&
                     (!tsr_control[0] || !tx_buffer_full)) ||
                    (writing && addr == 6'h2d && !wdata[0] &&
                     tsr_control[0] && !tx_busy);
    end

    always @* begin
        next_ier = ier;
        next_ipr = ipr;
        next_isr = isr;
        next_imr = imr;
        next_vr = vr;
        if (writing) case (addr)
            6'h07: begin next_ier[15:8] = wdata; next_ipr[15:8] = ipr[15:8] & wdata; end
            6'h09: begin next_ier[7:0] = wdata; next_ipr[7:0] = ipr[7:0] & wdata; end
            6'h0b: next_ipr[15:8] = ipr[15:8] & wdata;
            6'h0d: next_ipr[7:0] = ipr[7:0] & wdata;
            6'h0f: next_isr[15:8] = isr[15:8] & wdata;
            6'h11: next_isr[7:0] = isr[7:0] & wdata;
            6'h13: next_imr[15:8] = wdata;
            6'h15: next_imr[7:0] = wdata;
            6'h17: next_vr = wdata & 8'hf8;
            default: ;
        endcase
        if (iack && !iack_seen && selected) begin
            next_ipr[channel] = 1'b0;
            if (vr[3]) next_isr[channel] = 1'b1;
        end
        // New source events survive a simultaneous clear/acknowledge, but a
        // disabled source cannot retain a pending interrupt.
        next_ipr = (next_ipr | (events & ier)) & next_ier;
        if (!next_vr[3]) next_isr = 16'd0;
    end

    always @* begin
        read_value = 8'd0;
        case (addr)
            6'h01: read_value = (gpip & ~ddr) | (gpdr & ddr);
            6'h03: read_value = aer;
            6'h05: read_value = ddr;
            6'h07: read_value = ier[15:8];
            6'h09: read_value = ier[7:0];
            6'h0b: read_value = ipr[15:8];
            6'h0d: read_value = ipr[7:0];
            6'h0f: read_value = isr[15:8];
            6'h11: read_value = isr[7:0];
            6'h13: read_value = imr[15:8];
            6'h15: read_value = imr[7:0];
            6'h17: read_value = vr;
            6'h19: read_value = {4'd0, control[0]};
            6'h1b: read_value = {4'd0, control[1]};
            6'h1d: read_value = {1'b0, control[2][2:0], 1'b0, control[3][2:0]};
            6'h1f: read_value = count[0];
            6'h21: read_value = count[1];
            6'h23: read_value = count[2];
            6'h25: read_value = count[3];
            6'h27: read_value = scr;
            6'h29: read_value = ucr;
            6'h2b: read_value = {6'd0, rsr_control};
            6'h2d: read_value = {tx_initialized && !tx_buffer_full, tx_underrun,
                                   tsr_control[4], tx_end, tsr_control[3:0]};
            // The disconnected receiver never receives or echoes TX data.
            6'h2f: read_value = 8'd0;
            default: ;
        endcase
    end

    integer state_index;
    always @(posedge clk) begin
        if (reset) begin
            req_seen <= 1'b0;
            iack_seen <= 1'b0;
            rdata <= 8'd0;
            gpdr <= 8'd0;
            aer <= 8'd0;
            ddr <= 8'd0;
            vr <= 8'd0;
            ier <= 16'd0;
            ipr <= 16'd0;
            isr <= 16'd0;
            imr <= 16'd0;
            gpip_previous <= gpip;
            timer_previous <= {timer_b, timer_a};
            acknowledged_vector <= 8'h18;
            timer_output <= 4'd0;
            scr <= 8'd0;
            ucr <= 8'd0;
            rsr_control <= 2'd0;
            tsr_control <= 5'd0;
            tx_initialized <= 1'b0;
            tx_buffer_full <= 1'b0;
            tx_busy <= 1'b0;
            tx_underrun <= 1'b0;
            tx_end <= 1'b0;
            tx_clock_count <= 4'd0;
            tx_bits <= 5'd0;
            for (state_index = 0; state_index < 4; state_index = state_index + 1) begin
                control[state_index] <= 4'd0;
                reload[state_index] <= 8'd0;
                count[state_index] <= 8'd0;
                prescale[state_index] <= 8'd0;
            end
        end else begin
            ier <= next_ier;
            ipr <= next_ipr;
            isr <= next_isr;
            imr <= next_imr;
            vr <= next_vr;
            gpip_previous <= gpip_level;
            timer_previous <= timer_level;
            if (!req) req_seen <= 1'b0;
            if (transfer) begin
                req_seen <= 1'b1;
                rdata <= read_value;
            end
            if (!iack) iack_seen <= 1'b0;
            if (iack && !iack_seen) begin
                iack_seen <= 1'b1;
                acknowledged_vector <= selected ? {vr[7:4], channel} : 8'h18;
            end

            for (state_index = 0; state_index < 4; state_index = state_index + 1) begin
                if (control[state_index] == 0 || control[state_index] == 8 ||
                    (control[state_index][3] && timer_level[state_index % 2]))
                    prescale[state_index] <= 8'd0;
                else if (timer_ce)
                    prescale[state_index] <= timer_count[state_index] ? 8'd0 : prescale[state_index] + 8'd1;
                if (timer_count[state_index])
                    count[state_index] <= timer_fire[state_index] ? reload[state_index] : count[state_index] - 8'd1;
                if (timer_fire[state_index]) timer_output[state_index] <= ~timer_output[state_index];
                if (writing && addr == 6'h1f + 6'(state_index * 2)) begin
                    reload[state_index] <= wdata;
                    if (control[state_index] == 0 ||
                        (control[state_index] > 8 && timer_level[state_index % 2]))
                        count[state_index] <= wdata;
                end
            end
            if (writing) case (addr)
                6'h01: gpdr <= wdata;
                6'h03: aer <= wdata;
                6'h05: ddr <= wdata;
                6'h19: begin
                    control[0] <= wdata[3:0];
                    if (control[0] != wdata[3:0]) prescale[0] <= 8'd0;
                    if (wdata[4]) timer_output[0] <= 1'b0;
                end
                6'h1b: begin
                    control[1] <= wdata[3:0];
                    if (control[1] != wdata[3:0]) prescale[1] <= 8'd0;
                    if (wdata[4]) timer_output[1] <= 1'b0;
                end
                6'h1d: begin
                    control[2] <= {1'b0, wdata[6:4]};
                    control[3] <= {1'b0, wdata[2:0]};
                    if (control[2][2:0] != wdata[6:4]) prescale[2] <= 8'd0;
                    if (control[3][2:0] != wdata[2:0]) prescale[3] <= 8'd0;
                end
                6'h27: scr <= wdata;
                6'h29: ucr <= wdata;
                6'h2b: rsr_control <= wdata[1:0];
                6'h2d: begin
                    tsr_control <= {wdata[5], wdata[3:0]};
                    if (wdata[0]) begin
                        tx_initialized <= 1'b1;
                        tx_end <= 1'b0;
                    end else begin
                        tx_underrun <= 1'b0;
                        if (!tx_busy) tx_end <= 1'b1;
                    end
                end
                6'h2f: tx_buffer_full <= 1'b1;
                default: ;
            endcase
            if (transfer && !write && addr == 6'h2d) tx_underrun <= 1'b0;
            if (tx_clock) begin
                if (tx_busy) begin
                    if (!ucr[7] || tx_clock_count == 4'd15) begin
                        tx_clock_count <= 4'd0;
                        tx_bits <= tx_bits - 5'd1;
                        if (tx_bits == 5'd1) begin
                            tx_busy <= 1'b0;
                            tx_underrun <= tsr_control[0] && !tx_buffer_full;
                            tx_end <= !tsr_control[0];
                        end
                    end else tx_clock_count <= tx_clock_count + 4'd1;
                end else if (tsr_control[0] && !tsr_control[3] && tx_buffer_full) begin
                    tx_busy <= 1'b1;
                    tx_buffer_full <= writing && addr == 6'h2f;
                    tx_clock_count <= 4'd0;
                    tx_bits <= character_bits;
                end
            end
        end
    end
endmodule
