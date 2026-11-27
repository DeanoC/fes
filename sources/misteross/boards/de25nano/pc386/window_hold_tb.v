// SPDX-License-Identifier: GPL-2.0-or-later
// window_hold_tb.cpp's top: fes_de25_hps_window's guards and rebase in front
// of fes_de25_window_arbiter, joined as fes_de25_pc386_standard joins them,
// the three ports on one clock. Bench only; no build uses it.
`include "fes_application.vh"
module window_hold_tb (
    input  wire         clk, reset, hold,
    input  wire [31:0]  grant_base,
    output wire [31:0]  window_base,
    output wire         p0_reset, p1_reset, p2_reset, drained, idle,
    output wire [2:0]   fault_port,
    input  wire [27:0]  p0_address,
    input  wire [7:0]   p0_burstcount,
    input  wire         p0_read, p0_write,
    input  wire [127:0] p0_writedata,
    input  wire [15:0]  p0_byteenable,
    output wire         p0_waitrequest,
    output wire [127:0] p0_readdata,
    output wire         p0_readdatavalid,
    input  wire [28:0]  p1_address,
    input  wire [7:0]   p1_burstcount,
    input  wire         p1_read, p1_write,
    input  wire [63:0]  p1_writedata,
    input  wire [7:0]   p1_byteenable,
    output wire         p1_waitrequest,
    output wire [63:0]  p1_readdata,
    output wire         p1_readdatavalid,
    input  wire [28:0]  p2_address,
    input  wire [7:0]   p2_burstcount,
    input  wire         p2_read, p2_write,
    input  wire [63:0]  p2_writedata,
    input  wire [7:0]   p2_byteenable,
    output wire         p2_waitrequest,
    output wire [63:0]  p2_readdata,
    output wire         p2_readdatavalid,
    output wire [31:0]  m_address,
    output wire [7:0]   m_burstcount,
    output wire         m_read, m_write,
    output wire [127:0] m_writedata,
    output wire [15:0]  m_byteenable,
    input  wire         m_waitrequest,
    input  wire [127:0] m_readdata,
    input  wire         m_readdatavalid,
    input  wire         m_writeresponsevalid
);
    assign window_base = `FES_APPLICATION_HPS_DDR_WINDOW_BASE;
    wire [31:0] w0_address, w1_address, w2_address;
    wire [7:0] w0_burstcount, w1_burstcount, w2_burstcount;
    wire w0_read, w0_write, w1_read, w1_write, w2_read, w2_write;
    wire [127:0] w0_writedata, w0_readdata;
    wire [15:0] w0_byteenable;
    wire [63:0] w1_writedata, w1_readdata, w2_writedata, w2_readdata;
    wire [7:0] w1_byteenable, w2_byteenable;
    wire w0_waitrequest, w0_readdatavalid, w1_waitrequest, w1_readdatavalid, w2_waitrequest, w2_readdatavalid;
    wire p0_drained, p1_drained, p2_drained;
    /* verilator lint_off PINCONNECTEMPTY */
    fes_de25_hps_window window (
        .reset_n(!reset), .hold(hold), .grant_valid(1'b1), .grant_base(grant_base),
        .fault_clear(1'b0), .fault_port(fault_port), .fault_no_grant(), .fault_outside(),
        .p0_clk(clk), .p0_reset(p0_reset), .p0_drained(p0_drained),
        .p0_address(p0_address), .p0_burstcount(p0_burstcount), .p0_waitrequest(p0_waitrequest),
        .p0_readdata(p0_readdata), .p0_readdatavalid(p0_readdatavalid), .p0_read(p0_read),
        .p0_writedata(p0_writedata), .p0_byteenable(p0_byteenable), .p0_write(p0_write),
        .p1_clk(clk), .p1_reset(p1_reset), .p1_drained(p1_drained),
        .p1_address(p1_address), .p1_burstcount(p1_burstcount), .p1_waitrequest(p1_waitrequest),
        .p1_readdata(p1_readdata), .p1_readdatavalid(p1_readdatavalid), .p1_read(p1_read),
        .p1_writedata(p1_writedata), .p1_byteenable(p1_byteenable), .p1_write(p1_write),
        .p2_clk(clk), .p2_reset(p2_reset), .p2_drained(p2_drained),
        .p2_address(p2_address), .p2_burstcount(p2_burstcount), .p2_waitrequest(p2_waitrequest),
        .p2_readdata(p2_readdata), .p2_readdatavalid(p2_readdatavalid), .p2_read(p2_read),
        .p2_writedata(p2_writedata), .p2_byteenable(p2_byteenable), .p2_write(p2_write),
        .m0_address(w0_address), .m0_burstcount(w0_burstcount), .m0_read(w0_read), .m0_write(w0_write),
        .m0_writedata(w0_writedata), .m0_byteenable(w0_byteenable), .m0_waitrequest(w0_waitrequest),
        .m0_readdata(w0_readdata), .m0_readdatavalid(w0_readdatavalid),
        .m1_address(w1_address), .m1_burstcount(w1_burstcount), .m1_read(w1_read), .m1_write(w1_write),
        .m1_writedata(w1_writedata), .m1_byteenable(w1_byteenable), .m1_waitrequest(w1_waitrequest),
        .m1_readdata(w1_readdata), .m1_readdatavalid(w1_readdatavalid),
        .m2_address(w2_address), .m2_burstcount(w2_burstcount), .m2_read(w2_read), .m2_write(w2_write),
        .m2_writedata(w2_writedata), .m2_byteenable(w2_byteenable), .m2_waitrequest(w2_waitrequest),
        .m2_readdata(w2_readdata), .m2_readdatavalid(w2_readdatavalid)
    );
    /* verilator lint_on PINCONNECTEMPTY */
    fes_de25_window_arbiter arbiter (
        .clk(clk), .reset(reset),
        .s0_address(w0_address), .s0_burstcount(w0_burstcount), .s0_read(w0_read), .s0_write(w0_write),
        .s0_writedata(w0_writedata), .s0_byteenable(w0_byteenable), .s0_waitrequest(w0_waitrequest),
        .s0_readdata(w0_readdata), .s0_readdatavalid(w0_readdatavalid),
        .s1_address(w1_address), .s1_burstcount(w1_burstcount), .s1_read(w1_read), .s1_write(w1_write),
        .s1_writedata(w1_writedata), .s1_byteenable(w1_byteenable), .s1_waitrequest(w1_waitrequest),
        .s1_readdata(w1_readdata), .s1_readdatavalid(w1_readdatavalid),
        .s2_address(w2_address), .s2_burstcount(w2_burstcount), .s2_read(w2_read), .s2_write(w2_write),
        .s2_writedata(w2_writedata), .s2_byteenable(w2_byteenable), .s2_waitrequest(w2_waitrequest),
        .s2_readdata(w2_readdata), .s2_readdatavalid(w2_readdatavalid),
        .m_address(m_address), .m_burstcount(m_burstcount), .m_read(m_read), .m_write(m_write),
        .m_writedata(m_writedata), .m_byteenable(m_byteenable), .m_waitrequest(m_waitrequest),
        .m_readdata(m_readdata), .m_readdatavalid(m_readdatavalid),
        .m_writeresponsevalid(m_writeresponsevalid), .idle(idle)
    );
    assign drained = p0_drained && p1_drained && p2_drained;
endmodule
