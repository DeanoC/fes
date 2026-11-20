// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_application.vh"
// fes.memory.hps-ddr 1.0's three ports on the DE25: the pc386 window in HPS
// DDR through three F2SDRAM Avalon masters (byte addresses), one per port.
//
// The port list is fes_hps_ddr's, so pc386_system connects as it does on the
// DE10. Each port keeps that wrapper's semantics through the same
// fes_hps_ddr_guard: hold is synchronized into the port clock and returned as
// pN_reset, the guard finishes or drains a started burst, and pN_drained
// proves nothing is in flight. The DE10 masters address the fixed window at
// FES_APPLICATION_HPS_DDR_WINDOW_BASE. Here each command is rebased onto the
// runtime's grant (grant_base, GRANT_BYTES contiguous): the guard's word
// address becomes a byte offset into the window, which must lie inside the
// grant with its whole burst, and grant_base is added. A command outside it
// sets the sticky fault and is not issued: its read data is never returned,
// so the machine stays waiting, held by the board until the runtime resets it.
// Without a valid grant every command faults.
module fes_de25_hps_window #(
    parameter [31:0] GRANT_BYTES = 32'h10000000
) (
    input  wire         hold,
    input  wire         grant_valid,
    input  wire [31:0]  grant_base,
    // Sticky until fault_clear (machine clock, level): the ports that
    // faulted, and why -- no valid grant, or outside the span.
    input  wire         fault_clear,
    output wire [2:0]   fault_port,
    output wire         fault_no_grant, fault_outside,

    input  wire         p0_clk,
    output wire         p0_reset,
    output wire         p0_drained,
    input  wire [27:0]  p0_address,
    input  wire [7:0]   p0_burstcount,
    output wire         p0_waitrequest,
    output wire [127:0] p0_readdata,
    output wire         p0_readdatavalid,
    input  wire         p0_read,
    input  wire [127:0] p0_writedata,
    input  wire [15:0]  p0_byteenable,
    input  wire         p0_write,

    input  wire         p1_clk,
    output wire         p1_reset,
    output wire         p1_drained,
    input  wire [28:0]  p1_address,
    input  wire [7:0]   p1_burstcount,
    output wire         p1_waitrequest,
    output wire [63:0]  p1_readdata,
    output wire         p1_readdatavalid,
    input  wire         p1_read,
    input  wire [63:0]  p1_writedata,
    input  wire [7:0]   p1_byteenable,
    input  wire         p1_write,

    input  wire         p2_clk,
    output wire         p2_reset,
    output wire         p2_drained,
    input  wire [28:0]  p2_address,
    input  wire [7:0]   p2_burstcount,
    output wire         p2_waitrequest,
    output wire [63:0]  p2_readdata,
    output wire         p2_readdatavalid,
    input  wire         p2_read,
    input  wire [63:0]  p2_writedata,
    input  wire [7:0]   p2_byteenable,
    input  wire         p2_write,

    // The F2SDRAM masters, byte addressed, one per port in its port clock.
    output wire [31:0]  m0_address,
    output wire [7:0]   m0_burstcount,
    output wire         m0_read, m0_write,
    output wire [127:0] m0_writedata,
    output wire [15:0]  m0_byteenable,
    input  wire         m0_waitrequest,
    input  wire [127:0] m0_readdata,
    input  wire         m0_readdatavalid,
    output wire [31:0]  m1_address,
    output wire [7:0]   m1_burstcount,
    output wire         m1_read, m1_write,
    output wire [63:0]  m1_writedata,
    output wire [7:0]   m1_byteenable,
    input  wire         m1_waitrequest,
    input  wire [63:0]  m1_readdata,
    input  wire         m1_readdatavalid,
    output wire [31:0]  m2_address,
    output wire [7:0]   m2_burstcount,
    output wire         m2_read, m2_write,
    output wire [63:0]  m2_writedata,
    output wire [7:0]   m2_byteenable,
    input  wire         m2_waitrequest,
    input  wire [63:0]  m2_readdata,
    input  wire         m2_readdatavalid
);
    localparam [31:0] WINDOW_BASE = `FES_APPLICATION_HPS_DDR_WINDOW_BASE;

    (* async_reg = "true" *) reg [1:0] p0_hold_sync = 2'b11;
    (* async_reg = "true" *) reg [1:0] p1_hold_sync = 2'b11;
    (* async_reg = "true" *) reg [1:0] p2_hold_sync = 2'b11;
    always @(posedge p0_clk) p0_hold_sync <= {p0_hold_sync[0], hold};
    always @(posedge p1_clk) p1_hold_sync <= {p1_hold_sync[0], hold};
    always @(posedge p2_clk) p2_hold_sync <= {p2_hold_sync[0], hold};
    assign p0_reset = p0_hold_sync[1];
    assign p1_reset = p1_hold_sync[1];
    assign p2_reset = p2_hold_sync[1];

    wire [27:0] g0_address;
    wire [28:0] g1_address, g2_address;
    wire [7:0] g0_burstcount, g1_burstcount, g2_burstcount;
    wire g0_read, g0_write, g1_read, g1_write, g2_read, g2_write;
    wire g0_ok, g1_ok, g2_ok;
    fes_hps_ddr_guard #(.DATA_W(128), .ADDR_W(28)) port0 (
        .clk(p0_clk), .hold(p0_reset), .drained(p0_drained),
        .address(p0_address), .burstcount(p0_burstcount), .waitrequest(p0_waitrequest),
        .readdata(p0_readdata), .readdatavalid(p0_readdatavalid), .read(p0_read),
        .writedata(p0_writedata), .byteenable(p0_byteenable), .write(p0_write),
        .m_address(g0_address), .m_burstcount(g0_burstcount),
        .m_waitrequest(m0_waitrequest || !g0_ok), .m_readdata(m0_readdata),
        .m_readdatavalid(m0_readdatavalid), .m_read(g0_read),
        .m_writedata(m0_writedata), .m_byteenable(m0_byteenable), .m_write(g0_write)
    );
    fes_hps_ddr_guard #(.DATA_W(64), .ADDR_W(29)) port1 (
        .clk(p1_clk), .hold(p1_reset), .drained(p1_drained),
        .address(p1_address), .burstcount(p1_burstcount), .waitrequest(p1_waitrequest),
        .readdata(p1_readdata), .readdatavalid(p1_readdatavalid), .read(p1_read),
        .writedata(p1_writedata), .byteenable(p1_byteenable), .write(p1_write),
        .m_address(g1_address), .m_burstcount(g1_burstcount),
        .m_waitrequest(m1_waitrequest || !g1_ok), .m_readdata(m1_readdata),
        .m_readdatavalid(m1_readdatavalid), .m_read(g1_read),
        .m_writedata(m1_writedata), .m_byteenable(m1_byteenable), .m_write(g1_write)
    );
    fes_hps_ddr_guard #(.DATA_W(64), .ADDR_W(29)) port2 (
        .clk(p2_clk), .hold(p2_reset), .drained(p2_drained),
        .address(p2_address), .burstcount(p2_burstcount), .waitrequest(p2_waitrequest),
        .readdata(p2_readdata), .readdatavalid(p2_readdatavalid), .read(p2_read),
        .writedata(p2_writedata), .byteenable(p2_byteenable), .write(p2_write),
        .m_address(g2_address), .m_burstcount(g2_burstcount),
        .m_waitrequest(m2_waitrequest || !g2_ok), .m_readdata(m2_readdata),
        .m_readdatavalid(m2_readdatavalid), .m_read(g2_read),
        .m_writedata(m2_writedata), .m_byteenable(m2_byteenable), .m_write(g2_write)
    );

    // A command's window offset and its end, from the guard's word address.
    // The grant crosses into each port clock as a quasi-static value: the
    // runtime sets it while every port is held.
    wire [32:0] o0 = {1'b0, g0_address, 4'b0} - {1'b0, WINDOW_BASE};
    wire [32:0] o1 = {1'b0, g1_address, 3'b0} - {1'b0, WINDOW_BASE};
    wire [32:0] o2 = {1'b0, g2_address, 3'b0} - {1'b0, WINDOW_BASE};
    wire [33:0] e0 = {1'b0, o0} + {22'd0, g0_burstcount, 4'b0};
    wire [33:0] e1 = {1'b0, o1} + {23'd0, g1_burstcount, 3'b0};
    wire [33:0] e2 = {1'b0, o2} + {23'd0, g2_burstcount, 3'b0};
    assign g0_ok = grant_valid && !o0[32] && e0 <= {2'b0, GRANT_BYTES};
    assign g1_ok = grant_valid && !o1[32] && e1 <= {2'b0, GRANT_BYTES};
    assign g2_ok = grant_valid && !o2[32] && e2 <= {2'b0, GRANT_BYTES};
    assign m0_address = grant_base + o0[31:0];
    assign m1_address = grant_base + o1[31:0];
    assign m2_address = grant_base + o2[31:0];
    assign {m0_burstcount, m1_burstcount, m2_burstcount} = {g0_burstcount, g1_burstcount, g2_burstcount};
    assign m0_read = g0_read && g0_ok;
    assign m0_write = g0_write && g0_ok;
    assign m1_read = g1_read && g1_ok;
    assign m1_write = g1_write && g1_ok;
    assign m2_read = g2_read && g2_ok;
    assign m2_write = g2_write && g2_ok;

    // Sticky. The three ports share one clock on the DE25 board, so the
    // record is kept in p0_clk; the shell ties p1_clk and p2_clk to it.
    wire bad0 = (g0_read || g0_write) && !g0_ok;
    wire bad1 = (g1_read || g1_write) && !g1_ok;
    wire bad2 = (g2_read || g2_write) && !g2_ok;
    reg [2:0] port_fault = 3'b000;
    reg no_grant = 1'b0, outside = 1'b0;
    always @(posedge p0_clk)
        if (fault_clear) begin
            port_fault <= 3'b000; no_grant <= 1'b0; outside <= 1'b0;
        end else begin
            port_fault <= port_fault | {bad2, bad1, bad0};
            if ((bad0 || bad1 || bad2) && !grant_valid) no_grant <= 1'b1;
            if ((bad0 || bad1 || bad2) && grant_valid) outside <= 1'b1;
        end
    assign fault_port = port_fault;
    assign fault_no_grant = no_grant;
    assign fault_outside = outside;
endmodule
