// SPDX-License-Identifier: GPL-2.0-or-later
// Original implementation from Zilog UM0080, timing figures 5--10.
// ce_p/ce_n are alternating rising/falling Z80 clock enables in the clk domain.
// A request becomes T1 immediately after the previous completion. Metadata is
// captured at the first enabled edge; the usual first edge is ce_n in T1.
// ready is valid BEFORE its completing ce_p edge, including back-to-back reqs.
// This fabric interface cannot tri-state: BUSAK requires the enclosing bus mux
// to disconnect a/dout and the inactive control outputs from a shared bus.
module fes_z80_bus #(
    parameter bit FAST = 1'b0
) (
    input  logic        clk, reset, ce_p, ce_n, wait_n, busrq_n,
    input  logic        req,
    input  logic [2:0]  kind,
    input  logic [15:0] addr,
    input  logic [7:0]  wdata,
    input  logic [15:0] refresh_addr,
    // Decode may supply this after a fetch byte is sampled. It must be stable
    // before the base cycle completes, and throughout any extension states.
    input  logic [3:0]  extra_t,
    input  logic [4:0]  internal_t,
    input  logic [7:0]  din,
    output logic        ready,
    output logic        interrupt_blocked, resume,
    output logic [7:0]  rdata,
    output logic        m1_n, mreq_n, iorq_n, rd_n, wr_n, rfsh_n, busak_n,
    output logic [15:0] a,
    output logic [7:0]  dout
);
    localparam logic [2:0] FETCH = 3'd0, MEM_RD = 3'd1, MEM_WR = 3'd2,
        IO_RD = 3'd3, IO_WR = 3'd4, IRQ_ACK = 3'd5, NMI_ACK = 3'd6,
        INTERNAL = 3'd7;

    logic active, granted, half, waiting;
    logic sampled_busrq, boundary_permit;
    logic [5:0] t_state, effective_length;
    logic [4:0] length;
    logic [2:0] held_kind;
    logic [15:0] held_addr, held_refresh;
    logic [7:0] held_wdata, sampled_data;
    logic [2:0] cycle_kind;
    logic [5:0] cycle_t, wait_t;
    logic cycle_half, presented;
    logic [15:0] cycle_addr, cycle_refresh;
    logic [7:0] cycle_wdata;

    function automatic logic [4:0] cycle_length(
        input logic [2:0] k, input logic [4:0] delay_t
    );
        case (k)
            FETCH, IO_RD, IO_WR: cycle_length = 5'd4;
            MEM_RD, MEM_WR: cycle_length = 5'd3;
            // IM0 replaces a 4T fetch with 6T. IM1/IM2 and IM0 RST supply
            // extra_t=1 for their seventh acknowledge state.
            IRQ_ACK: cycle_length = 5'd6;
            NMI_ACK: cycle_length = 5'd5;
            default: cycle_length = delay_t == 0 ? 5'd1 : delay_t;
        endcase
    endfunction

    always_comb begin
        cycle_kind = active ? held_kind : kind;
        cycle_addr = active ? held_addr : addr;
        cycle_refresh = active ? held_refresh : refresh_addr;
        cycle_wdata = active ? held_wdata : wdata;
        cycle_t = active ? t_state : 6'd1;
        effective_length = {1'b0, length} + {2'b0, extra_t};
        cycle_half = active && half;
        presented = !reset && !granted &&
            (active || (req && (busrq_n || (!FAST && boundary_permit))));
        case (cycle_kind)
            FETCH, MEM_RD, MEM_WR, NMI_ACK: wait_t = 6'd2;
            IO_RD, IO_WR: wait_t = 6'd3; // The first Tw is mandatory.
            IRQ_ACK: wait_t = 6'd4;      // Two automatic Tw states.
            default: wait_t = 6'd0;
        endcase

        ready = 1'b0;
        // The engine must defer boundary interrupt recognition while this
        // cycle has a sampled DMA request, including late BUSRQ release.
        interrupt_blocked = !reset && (granted || (active && sampled_busrq));
        resume = !reset && granted && ce_p && busrq_n;
        rdata = sampled_data;
        a = held_addr;
        dout = held_wdata;
        m1_n = 1'b1;
        mreq_n = 1'b1;
        iorq_n = 1'b1;
        rd_n = 1'b1;
        wr_n = 1'b1;
        rfsh_n = 1'b1;
        busak_n = reset || !granted;
        if (presented) begin
            a = cycle_addr;
            dout = cycle_wdata;
            if (FAST) begin
                // One ce_p transaction, with controls driven as soon as req
                // appears. A synchronous memory needs an intervening clk edge
                // before that ce_p; no Z80 half-cycle cadence is promised.
                // Refresh/internal fixed delays are intentionally compressed.
                ready = ce_p && wait_n;
                case (cycle_kind)
                    FETCH, NMI_ACK: begin
                        m1_n = 1'b0; mreq_n = 1'b0; rd_n = 1'b0;
                    end
                    MEM_RD: begin mreq_n = 1'b0; rd_n = 1'b0; end
                    MEM_WR: begin mreq_n = 1'b0; wr_n = 1'b0; end
                    IO_RD: begin iorq_n = 1'b0; rd_n = 1'b0; end
                    IO_WR: begin iorq_n = 1'b0; wr_n = 1'b0; end
                    IRQ_ACK: begin m1_n = 1'b0; iorq_n = 1'b0; end
                    default: begin end
                endcase
                if (cycle_kind == FETCH || cycle_kind == MEM_RD ||
                    cycle_kind == IO_RD || cycle_kind == IRQ_ACK ||
                    cycle_kind == NMI_ACK) rdata = din;
            end else begin
                ready = active && ce_p && half && t_state == effective_length;
                case (cycle_kind)
                    FETCH, NMI_ACK: begin
                        if (cycle_t < 3) begin
                            m1_n = 1'b0;
                            if (cycle_t == 2 || cycle_half) begin
                                mreq_n = 1'b0; rd_n = 1'b0;
                            end
                        end else begin
                            a = cycle_refresh;
                            if (cycle_t <= 4) begin
                                rfsh_n = 1'b0;
                                if ((cycle_t == 3 && cycle_half) ||
                                    (cycle_t == 4 && !cycle_half)) mreq_n = 1'b0;
                            end
                        end
                    end
                    MEM_RD, MEM_WR: begin
                        if ((cycle_t == 1 && cycle_half) || cycle_t == 2 ||
                            (cycle_t == 3 && !cycle_half)) mreq_n = 1'b0;
                        if (cycle_kind == MEM_RD) rd_n = mreq_n;
                        else if ((cycle_t == 2 && (cycle_half || waiting)) ||
                                 (cycle_t == 3 && !cycle_half)) wr_n = 1'b0;
                    end
                    IO_RD, IO_WR: begin
                        if (cycle_t == 2 || cycle_t == 3 ||
                            (cycle_t == 4 && !cycle_half)) begin
                            iorq_n = 1'b0;
                            if (cycle_kind == IO_RD) rd_n = 1'b0;
                            else wr_n = 1'b0;
                        end
                    end
                    IRQ_ACK: begin
                        // RD remains inactive during interrupt acknowledge.
                        if (cycle_t < 5) m1_n = 1'b0;
                        if ((cycle_t == 3 && cycle_half) || cycle_t == 4)
                            iorq_n = 1'b0;
                        if (cycle_t >= 5) a = cycle_refresh;
                        if (cycle_t == 5 || cycle_t == 6) begin
                            rfsh_n = 1'b0;
                            if ((cycle_t == 5 && cycle_half) ||
                                (cycle_t == 6 && !cycle_half)) mreq_n = 1'b0;
                        end
                    end
                    default: begin end
                endcase
            end
        end
    end

    always_ff @(posedge clk) begin
        if (reset) begin
            active <= 1'b0;
            granted <= 1'b0;
            half <= 1'b0;
            waiting <= 1'b0;
            sampled_busrq <= 1'b0;
            boundary_permit <= 1'b0;
            t_state <= 6'd1;
            length <= 5'd1;
            held_kind <= INTERNAL;
            held_addr <= 16'd0;
            held_refresh <= 16'd0;
            held_wdata <= 8'd0;
            sampled_data <= 8'd0;
        end else if (granted) begin
            if (ce_p && busrq_n) begin
                granted <= 1'b0;
                boundary_permit <= 1'b1;
            end
        end else if (FAST) begin
            if (ce_p) begin
                if (ready) begin
                    sampled_data <= rdata;
                    active <= 1'b0;
                    if (!busrq_n) granted <= 1'b1;
                end else if (!active) begin
                    if (!busrq_n) granted <= 1'b1;
                    else if (req) begin
                        active <= 1'b1;
                        held_kind <= kind;
                        held_addr <= addr;
                        held_refresh <= refresh_addr;
                        held_wdata <= wdata;
                    end
                end
            end
        end else if (!active) begin
            if (ce_p && !req) boundary_permit <= 1'b0;
            if (ce_p && !busrq_n && (!boundary_permit || !req)) begin
                granted <= 1'b1;
                boundary_permit <= 1'b0;
            end else if (req && (busrq_n || boundary_permit) && (ce_p || ce_n)) begin
                active <= 1'b1;
                boundary_permit <= 1'b0;
                held_kind <= kind;
                held_addr <= addr;
                held_refresh <= refresh_addr;
                held_wdata <= wdata;
                length <= cycle_length(kind, internal_t);
                t_state <= 6'd1;
                half <= ce_n;
                waiting <= 1'b0;
                sampled_busrq <= cycle_length(kind, internal_t) == 1 &&
                    extra_t == 0 && !busrq_n;
            end
        end else if (ready) begin
            active <= 1'b0;
            half <= 1'b0;
            waiting <= 1'b0;
            // BUSRQ was sampled at the positive start of the final T state.
            // A later request cannot revoke permission for the following T1.
            if (sampled_busrq) begin
                granted <= 1'b1;
                boundary_permit <= 1'b0;
            end else boundary_permit <= 1'b1;
        end else if (ce_n) begin
            half <= 1'b1;
            if (t_state == wait_t) waiting <= !wait_n;
            // Memory/I/O operands are sampled at falling T3. Fetch and IRQ
            // bytes are sampled earlier, before their refresh address appears.
            if ((held_kind == MEM_RD && t_state == 3) ||
                (held_kind == IO_RD && t_state == 4)) sampled_data <= din;
        end else if (ce_p && half) begin
            half <= 1'b0;
            if (!waiting || t_state != wait_t) begin
                t_state <= t_state + 6'd1;
                if (t_state + 6'd1 == effective_length) sampled_busrq <= !busrq_n;
                if (((held_kind == FETCH || held_kind == NMI_ACK) &&
                     t_state == 2) || (held_kind == IRQ_ACK && t_state == 4))
                    sampled_data <= din;
            end
        end
    end
endmodule
