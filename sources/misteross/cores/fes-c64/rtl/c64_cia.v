// SPDX-License-Identifier: GPL-2.0-or-later
// Timer and interrupt transitions adapted for synthesizable RTL from VICE
// 3.10 core/ciatimer.c, core/ciatimer.h and core/ciacore.c, GPL-2.0-or-later.
// Original timer/CIA implementation by Andre Fachat; CIA improvements by
// Ettore Perazzoli, Andreas Boose, Alexander Bluhm and Olaf Seibert.
// Source archive: VICE-Team/svn-mirror release 3.10.0, vice-3.10.tar.gz.
// Source archive SHA256:
// 8e5bac18cbcb9f192380ad3ef881f8790f5b75c41d7b3da65d831985d864d6d1
//
// This program is free software; you can redistribute it and/or modify it
// under the terms of the GNU General Public License as published by the
// Free Software Foundation; either version 2 of the License, or (at your
// option) any later version. This program is distributed in the hope that
// it will be useful, but WITHOUT ANY WARRANTY; without even the implied
// warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
// See c64_cia.COPYING for the complete license text.
// Old MOS6526 timer and interrupt pipeline.
// Timer/IFR transitions follow VICE 3.10 core/ciatimer.{c,h} and ciacore.c.
// TOD, serial shifting and external CNT edges remain unsupported. CNT is high.
module c64_cia (
    input  wire       clk,
    input  wire       phi,
    input  wire       reset,
    input  wire       cs,
    input  wire       we,
    input  wire       reading,
    input  wire       read_sample,
    input  wire [3:0] rs,
    input  wire [7:0] din,
    output reg  [7:0] dout,
    input  wire [7:0] pa_pin,
    input  wire [7:0] pb_pin,
    output reg  [7:0] pa_reg,
    output reg  [7:0] pb_reg,
    output reg  [7:0] pa_ddr,
    output reg  [7:0] pb_ddr,
    output wire       irq
);
    reg [15:0] ta_latch, ta_count, tb_latch, tb_count;
    reg [7:0] cra, crb;
    reg [4:0] icr_mask, icr_flags;
    reg [12:0] ta_state, tb_state;
    reg [15:0] ifr_delay;
    reg irq_pin, icr_d7, tb_read_bug;

    // Control bits and delayed timer state correspond to the VICE timer
    // equations. LOAD and UNDERFLOW describe the current guest cycle.
    localparam [12:0]
        START        = 13'h001,
        COUNT2       = 13'h002,  // First count-enable stage.
        STEP         = 13'h004,  // Queued timer A underflow for timer B.
        ONESHOT_CTRL = 13'h008,
        FORCE        = 13'h010,  // Force-load strobe, not readable.
        PHI_INPUT    = 13'h020,
        COUNT3       = 13'h040,  // Second count-enable stage.
        LOAD1        = 13'h080,  // Delayed force-load request.
        ONESHOT0     = 13'h100,  // First one-shot stage.
        LOAD         = 13'h200,
        UNDERFLOW    = 13'h400,
        COUNT        = 13'h800,  // Reference bookkeeping; unused by actions.
        ONESHOT      = 13'h1000; // Second one-shot stage.
    localparam integer START_BIT = 0, UNDERFLOW_BIT = 10;

    // Each delay field shifts once per phi. A timer event is inserted before
    // that shift; a bus access is inserted afterwards. This preserves their
    // different delays. Bit7 and the pin can differ during acknowledgement.
    localparam [15:0]
        ACK1        = 16'h0001,
        ACK0        = 16'h0002,
        ACK_1       = 16'h0004,
        D7SET1      = 16'h0010,
        D7SET0      = 16'h0020,
        RAISE1      = 16'h0100,
        RAISE0      = 16'h0200,
        READ0       = 16'h1000,
        DELAY_CLEAR = 16'h4448;  // Expired ACK, D7, pin and read history.

    assign irq = irq_pin;
    wire write_reg = we && cs;
    wire ack_icr = reading && read_sample && cs && rs == 4'hd && !write_reg;
    wire [4:0] visible_flags = icr_flags &
        ((cs && reading && tb_read_bug) ? 5'b11101 : 5'b11111);

    // The old count-enable decrements; the newly clocked enable detects zero.
    // A load then inhibits that enable. STOP preserves pending pipeline work.
    function automatic [28:0] timer_step;
        input [12:0] old_state;
        input [15:0] old_count, latch;
        reg [12:0] next_state;
        reg [15:0] next_count;
        begin
            next_state = old_state & (START | ONESHOT_CTRL | PHI_INPUT);
            if ((|(old_state & START)) && (|(old_state & PHI_INPUT)))
                next_state = next_state | COUNT2;
            if ((|(old_state & COUNT2)) ||
                ((|(old_state & STEP)) && (|(old_state & START))))
                next_state = next_state | COUNT3;
            if (|(old_state & COUNT3))
                next_state = next_state | COUNT;
            if (|(old_state & FORCE))
                next_state = next_state | LOAD1;
            if (|(old_state & LOAD1))
                next_state = next_state | LOAD;
            if (|(old_state & ONESHOT_CTRL))
                next_state = next_state | ONESHOT0;
            if (|(old_state & ONESHOT0))
                next_state = next_state | ONESHOT;

            next_count = old_count;
            if (old_count != 0 && (|(old_state & COUNT3)))
                next_count = old_count - 16'd1;
            if (next_count == 0 && (|(next_state & COUNT3)))
                next_state = next_state | LOAD | UNDERFLOW;
            if (|(next_state & LOAD)) begin
                next_count = latch;
                next_state = next_state & ~COUNT3;
            end
            if ((|(next_state & UNDERFLOW)) &&
                (|(next_state & (ONESHOT | ONESHOT0))))
                next_state = next_state & ~(START | COUNT2);
            timer_step = {next_state, next_count};
        end
    endfunction

    always @* begin
        case (rs)
            4'h0: dout = pa_pin;
            4'h1: dout = pb_pin;
            4'h2: dout = pa_ddr;
            4'h3: dout = pb_ddr;
            4'h4: dout = ta_count[7:0];
            4'h5: dout = ta_count[15:8];
            4'h6: dout = tb_count[7:0];
            4'h7: dout = tb_count[15:8];
            4'hd: dout = {icr_d7, 2'b0, visible_flags};
            4'he: dout = {cra[7:1], ta_state[START_BIT]};
            4'hf: dout = {crb[7:1], tb_state[START_BIT]};
            default: dout = 8'h00;
        endcase
    end

    reg [15:0] ta_latch_d, ta_count_d, tb_latch_d, tb_count_d, delay_d;
    reg [12:0] ta_state_d, tb_state_d;
    reg [7:0] cra_d, crb_d;
    reg [4:0] mask_d, flags_d, new_flags;
    reg pin_d, d7_d, bug_d;

    always @* begin
        ta_latch_d = ta_latch;
        ta_count_d = ta_count;
        tb_latch_d = tb_latch;
        tb_count_d = tb_count;
        ta_state_d = ta_state;
        tb_state_d = tb_state;
        cra_d = cra;
        crb_d = crb;
        mask_d = icr_mask;
        flags_d = icr_flags;
        delay_d = ifr_delay;
        pin_d = irq_pin;
        d7_d = icr_d7;
        bug_d = tb_read_bug;
        new_flags = 5'd0;

        if (phi) begin
            {ta_state_d, ta_count_d} = timer_step(ta_state, ta_count, ta_latch);
            {tb_state_d, tb_count_d} = timer_step(tb_state, tb_count, tb_latch);
            new_flags = {3'b0, tb_state_d[UNDERFLOW_BIT], ta_state_d[UNDERFLOW_BIT]};

            // Both timers advance before A queues a step for B. CNT is high,
            // so both A-underflow input modes accept this event.
            if (ta_state_d[UNDERFLOW_BIT] && crb[6] && tb_state_d[START_BIT])
                tb_state_d = tb_state_d | STEP;
            if (tb_state_d[UNDERFLOW_BIT])
                bug_d = |(ifr_delay & READ0);
            flags_d = flags_d | new_flags;

            if (|(delay_d & ACK0))
                d7_d = 1'b0;
            if (|(new_flags & icr_mask))
                delay_d = delay_d | RAISE1 | D7SET1;
            if (|(delay_d & D7SET0))
                d7_d = 1'b1;
            if (|(delay_d & RAISE0))
                pin_d = 1'b1;
            delay_d = (delay_d << 1) & ~DELAY_CLEAR;
        end

        if (write_reg) begin
            case (rs)
                4'h4: begin
                    ta_latch_d[7:0] = din;
                    if (|(ta_state_d & LOAD))
                        ta_count_d[7:0] = din;
                end
                4'h5: begin
                    ta_latch_d[15:8] = din;
                    if ((|(ta_state_d & LOAD)) || !ta_state_d[START_BIT])
                        ta_count_d = ta_latch_d;
                end
                4'h6: begin
                    tb_latch_d[7:0] = din;
                    if (|(tb_state_d & LOAD))
                        tb_count_d[7:0] = din;
                end
                4'h7: begin
                    tb_latch_d[15:8] = din;
                    if ((|(tb_state_d & LOAD)) || !tb_state_d[START_BIT])
                        tb_count_d = tb_latch_d;
                end
                4'hd: begin
                    mask_d = din[7] ? (icr_mask | din[4:0]) :
                                      (icr_mask & ~din[4:0]);
                    if (|(flags_d & mask_d)) begin
                        // A pending next-cycle assertion is already active
                        // in VICE's scheduled-pin interface. Preserve it
                        // without scheduling another assertion after it.
                        if (!pin_d && !(|(delay_d & RAISE0)))
                            delay_d = delay_d | RAISE1 | D7SET1;
                    end else if (|(delay_d & ACK_1)) begin
                        // Old-6526 cancellation window after an ICR read.
                        delay_d = delay_d & ~(RAISE0 | D7SET0);
                    end
                end
                4'he: begin
                    cra_d = din & 8'hef;
                    ta_state_d = (ta_state_d & ~13'h039) |
                                 (({5'b0, din} & 13'h039) ^ PHI_INPUT);
                end
                4'hf: begin
                    crb_d = din & 8'hef;
                    tb_state_d = (tb_state_d & ~13'h039) |
                                 (({5'b0, din} & 13'h039) ^ PHI_INPUT);
                    if (din[6])
                        tb_state_d = tb_state_d & ~PHI_INPUT;
                end
                default: ;
            endcase
        end else if (ack_icr) begin
            // The CPU samples the pre-clear value. Old CIA bit7 clears later
            // and can remain set while the physical interrupt pin is low.
            flags_d = 5'd0;
            bug_d = 1'b0;
            pin_d = 1'b0;
            delay_d = (delay_d | ACK1 | READ0) & ~RAISE0;
        end
    end

    always @(posedge clk) begin
        if (reset) begin
            pa_reg <= 8'h00;
            pb_reg <= 8'h00;
            pa_ddr <= 8'h00;
            pb_ddr <= 8'h00;
            ta_latch <= 16'hffff;
            ta_count <= 16'hffff;
            tb_latch <= 16'hffff;
            tb_count <= 16'hffff;
            ta_state <= 13'd0;
            tb_state <= 13'd0;
            cra <= 8'h00;
            crb <= 8'h00;
            icr_mask <= 5'd0;
            icr_flags <= 5'd0;
            ifr_delay <= 16'd0;
            irq_pin <= 1'b0;
            icr_d7 <= 1'b0;
            tb_read_bug <= 1'b0;
        end else begin
            ta_latch <= ta_latch_d;
            ta_count <= ta_count_d;
            tb_latch <= tb_latch_d;
            tb_count <= tb_count_d;
            ta_state <= ta_state_d;
            tb_state <= tb_state_d;
            cra <= cra_d;
            crb <= crb_d;
            icr_mask <= mask_d;
            icr_flags <= flags_d;
            ifr_delay <= delay_d;
            irq_pin <= pin_d;
            icr_d7 <= d7_d;
            tb_read_bug <= bug_d;
            if (write_reg) begin
                case (rs)
                    4'h0: pa_reg <= din;
                    4'h1: pb_reg <= din;
                    4'h2: pa_ddr <= din;
                    4'h3: pb_ddr <= din;
                    default: ;
                endcase
            end
        end
    end
endmodule
