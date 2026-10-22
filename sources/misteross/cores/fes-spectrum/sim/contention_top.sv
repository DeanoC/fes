// SPDX-License-Identifier: GPL-2.0-or-later
// NMOS Z80 plus the Sinclair contention classifier, for the Fuse
// per-instruction contention-point regression.
module contention_top (
    input  wire        clk,
    input  wire        reset,
    input  wire        ce_p,
    input  wire        ce_n,
    input  wire [7:0]  din,
    output wire [15:0] a,
    output wire [7:0]  dout,
    output wire        mreq_n, iorq_n, rd_n, wr_n,
    output wire        retired,
    output wire        illegal,
    output wire [2:0]  cycle_kind,
    output wire        mem_point,
    output wire        ula_io_point
);
    wire cycle_present;
    wire [5:0] cycle_t;
    fes_z80_nmos cpu (
        .clk(clk), .reset(reset), .ce_p(ce_p), .ce_n(ce_n), .wait_n(1'b1),
        .int_n(1'b1), .nmi_n(1'b1), .busrq_n(1'b1), .din(din),
        .m1_n(), .mreq_n(mreq_n), .iorq_n(iorq_n), .rd_n(rd_n), .wr_n(wr_n),
        .rfsh_n(), .halt_n(), .busak_n(), .a(a), .dout(dout),
        .illegal(illegal), .retired(retired), .retire_pc(),
        .debug_pc(), .debug_sp(), .debug_af(), .debug_bc(), .debug_de(),
        .debug_hl(), .debug_ix(), .debug_iy(), .debug_ir(), .debug_iff(),
        .cycle_present(cycle_present), .cycle_kind(cycle_kind), .cycle_t(cycle_t)
    );
    spectrum_contention #(.STYLE(2'd1)) points (
        .cycle_present(cycle_present), .cycle_kind(cycle_kind), .cycle_t(cycle_t),
        .a(a), .a_contended(1'b0), .mem_point(mem_point),
        .ula_io_point(ula_io_point), .request()
    );
endmodule
