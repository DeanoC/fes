// SPDX-License-Identifier: GPL-2.0-or-later
// Original NMOS-compatible pin adapter. All logic uses clk; ce_p and ce_n
// must alternate, must not be asserted together, and must be at least two
// clocks apart: the engine registers its ALU result and extra T states one
// clock after their inputs settle, and the pins and presented-cycle outputs
// follow the bus unit by one clock. No generated clocks.
module fes_z80_nmos (
    input logic clk, reset, ce_p, ce_n, wait_n, int_n, nmi_n, busrq_n,
    input logic [7:0] din,
    output logic m1_n, mreq_n, iorq_n, rd_n, wr_n, rfsh_n, halt_n, busak_n,
    output logic [15:0] a,
    output logic [7:0] dout,
    output logic illegal, retired,
    output logic [15:0] retire_pc,
    output logic [15:0] debug_pc, debug_sp, debug_af, debug_bc, debug_de,
                        debug_hl, debug_ix, debug_iy, debug_ir,
    output logic [2:0] debug_iff,
    // Presented bus cycle (kind 0 fetch .. 7 internal) and its T state, for
    // shells that stretch ce_p/ce_n by cycle type, such as ULA contention.
    output logic cycle_present,
    output logic [2:0] cycle_kind,
    output logic [5:0] cycle_t
);
    logic bus_req, bus_ready, halted, interrupt_blocked, bus_resume;
    logic [2:0] bus_kind;
    logic [3:0] bus_extra;
    logic [4:0] bus_delay;
    logic [15:0] bus_addr, refresh_addr;
    logic [7:0] bus_wdata, bus_rdata;
    // The bus unit's pins and presented cycle, registered below. A consumer
    // samples and stretches them only on enables, which are at least two
    // clocks apart, so the one-clock delay changes no T-state behaviour.
    logic pin_m1_n, pin_mreq_n, pin_iorq_n, pin_rd_n, pin_wr_n, pin_rfsh_n, pin_busak_n;
    logic [15:0] pin_a;
    logic [7:0] pin_dout;
    logic pin_cycle_present;
    logic [2:0] pin_cycle_kind;
    logic [5:0] pin_cycle_t;
    initial begin
        m1_n=1'b1; mreq_n=1'b1; iorq_n=1'b1; rd_n=1'b1; wr_n=1'b1; rfsh_n=1'b1;
        busak_n=1'b1; halt_n=1'b1; a=16'd0; dout=8'd0;
        cycle_present=1'b0; cycle_kind=3'd0; cycle_t=6'd0;
    end
    always_ff @(posedge clk) begin
        m1_n<=pin_m1_n; mreq_n<=pin_mreq_n; iorq_n<=pin_iorq_n; rd_n<=pin_rd_n;
        wr_n<=pin_wr_n; rfsh_n<=pin_rfsh_n; busak_n<=pin_busak_n; halt_n<=~halted;
        a<=pin_a; dout<=pin_dout;
        cycle_present<=pin_cycle_present; cycle_kind<=pin_cycle_kind; cycle_t<=pin_cycle_t;
    end
    fes_z80_engine #(.NMOS(1'b1)) cpu (
        .clk(clk), .reset(reset), .enable(ce_p), .int_n(int_n), .nmi_n(nmi_n),
        .interrupt_blocked(interrupt_blocked), .bus_resume(bus_resume),
        .bus_ready(bus_ready), .bus_rdata(bus_rdata), .bus_req(bus_req),
        .bus_kind(bus_kind), .bus_extra(bus_extra), .bus_delay(bus_delay), .bus_addr(bus_addr), .bus_wdata(bus_wdata),
        .refresh_addr(refresh_addr), .halted(halted), .illegal(illegal),
        .retired(retired), .retire_pc(retire_pc), .debug_pc(debug_pc),
        .debug_sp(debug_sp), .debug_af(debug_af), .debug_bc(debug_bc),
        .debug_de(debug_de), .debug_hl(debug_hl), .debug_ix(debug_ix),
        .debug_iy(debug_iy), .debug_ir(debug_ir), .debug_iff(debug_iff)
    );
    fes_z80_bus #(.FAST(1'b0)) bus (
        .clk(clk), .reset(reset), .ce_p(ce_p), .ce_n(ce_n), .wait_n(wait_n),
        .busrq_n(busrq_n), .req(bus_req), .kind(bus_kind), .addr(bus_addr),
        .extra_t(bus_extra), .internal_t(bus_delay), .wdata(bus_wdata), .refresh_addr(refresh_addr), .din(din),
        .interrupt_blocked(interrupt_blocked), .resume(bus_resume),
        .ready(bus_ready), .rdata(bus_rdata), .m1_n(pin_m1_n), .mreq_n(pin_mreq_n),
        .iorq_n(pin_iorq_n), .rd_n(pin_rd_n), .wr_n(pin_wr_n), .rfsh_n(pin_rfsh_n),
        .busak_n(pin_busak_n), .a(pin_a), .dout(pin_dout), .cycle_present(pin_cycle_present),
        .cycle_kind_out(pin_cycle_kind), .cycle_t_out(pin_cycle_t)
    );
`ifdef VERILATOR
    logic ce_prev = 1'b0;
    always_ff @(posedge clk) begin
        ce_prev <= ce_p || ce_n;
        if (!reset && ce_prev && (ce_p || ce_n))
            $fatal(1, "fes_z80_nmos: ce_p/ce_n on consecutive clocks");
    end
`endif
endmodule
