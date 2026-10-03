// SPDX-License-Identifier: GPL-2.0-or-later
// Original NMOS-compatible pin adapter. All logic uses clk; ce_p and ce_n
// must alternate, and must not be asserted together. No generated clocks.
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
    output logic [2:0] debug_iff
);
    logic bus_req, bus_ready, halted, interrupt_blocked, bus_resume;
    logic [2:0] bus_kind;
    logic [3:0] bus_extra;
    logic [4:0] bus_delay;
    logic [15:0] bus_addr, refresh_addr;
    logic [7:0] bus_wdata, bus_rdata;
    assign halt_n=~halted;
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
        .ready(bus_ready), .rdata(bus_rdata), .m1_n(m1_n), .mreq_n(mreq_n),
        .iorq_n(iorq_n), .rd_n(rd_n), .wr_n(wr_n), .rfsh_n(rfsh_n),
        .busak_n(busak_n), .a(a), .dout(dout)
    );
endmodule
