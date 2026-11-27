// SPDX-License-Identifier: GPL-2.0-or-later
// Exercises the board-local restart hold with the real DE25 window guards.
// The request generator stands in for a running pc386 CPU: its DDR request is
// suppressed by the same reset that the standard wrapper feeds to pc386_system.
`include "fes_application.vh"
module pc386_reset_hold_tb (
    input wire clk, machine_reset_n,
    input wire mailbox_exec_reset, machine_ddr_hold, grant_valid, cpu_request, verify_request,
    output wire reset_hold, system_reset, port_request, port_drained, verify_issued,
    output wire [2:0] fault_port,
    output wire fault_no_grant, fault_outside
);
    wire p0_guard_reset, p1_guard_reset, p2_guard_reset;
    wire p0_waitrequest, p0_readdatavalid, p1_readdatavalid, p2_readdatavalid;
    wire [127:0] p0_readdata;
    wire [63:0] p1_readdata, p2_readdata;
    wire [31:0] m0_address, m1_address, m2_address;
    wire [7:0] m0_burstcount, m1_burstcount, m2_burstcount;
    wire m0_read, m0_write, m1_read, m1_write, m2_read, m2_write;
    wire [127:0] m0_writedata;
    wire [15:0] m0_byteenable;
    wire [63:0] m1_writedata, m2_writedata;
    wire [7:0] m1_byteenable, m2_byteenable;

    fes_de25_pc386_reset_hold restart_hold (
        .clk(clk), .machine_reset_n(machine_reset_n),
        .grant_valid(grant_valid), .execution_held(mailbox_exec_reset),
        .ports_held(machine_ddr_hold), .held(reset_hold)
    );
    assign system_reset = reset_hold || mailbox_exec_reset || p0_guard_reset;
    assign port_request = cpu_request && !system_reset;
    assign verify_issued = m2_read;

    /* verilator lint_off PINCONNECTEMPTY */
    fes_de25_hps_window window (
        .reset_n(machine_reset_n), .hold(machine_ddr_hold || reset_hold),
        .grant_valid(grant_valid), .grant_base(32'h40000000), .fault_clear(1'b0),
        .fault_port(fault_port), .fault_no_grant(fault_no_grant), .fault_outside(fault_outside),
        .p0_clk(clk), .p0_reset(p0_guard_reset), .p0_drained(port_drained),
        .p0_address(28'h3000000), .p0_burstcount(8'd1),
        .p0_waitrequest(p0_waitrequest), .p0_readdata(p0_readdata), .p0_readdatavalid(p0_readdatavalid),
        .p0_read(port_request), .p0_writedata(128'd0), .p0_byteenable(16'hffff), .p0_write(1'b0),
        .p1_clk(clk), .p1_reset(p1_guard_reset), .p1_drained(), .p1_address(29'd0),
        .p1_burstcount(8'd1), .p1_waitrequest(), .p1_readdata(p1_readdata), .p1_readdatavalid(p1_readdatavalid),
        .p1_read(1'b0), .p1_writedata(64'd0), .p1_byteenable(8'hff), .p1_write(1'b0),
        .p2_clk(clk), .p2_reset(p2_guard_reset), .p2_drained(), .p2_address(29'h6000000),
        .p2_burstcount(8'd1), .p2_waitrequest(), .p2_readdata(p2_readdata), .p2_readdatavalid(p2_readdatavalid),
        .p2_read(verify_request && !reset_hold && !p2_guard_reset),
        .p2_writedata(64'd0), .p2_byteenable(8'hff), .p2_write(1'b0),
        .m0_address(m0_address), .m0_burstcount(m0_burstcount), .m0_read(m0_read), .m0_write(m0_write),
        .m0_writedata(m0_writedata), .m0_byteenable(m0_byteenable), .m0_waitrequest(1'b0),
        .m0_readdata(128'd0), .m0_readdatavalid(1'b0),
        .m1_address(m1_address), .m1_burstcount(m1_burstcount), .m1_read(m1_read), .m1_write(m1_write),
        .m1_writedata(m1_writedata), .m1_byteenable(m1_byteenable), .m1_waitrequest(1'b0),
        .m1_readdata(64'd0), .m1_readdatavalid(1'b0),
        .m2_address(m2_address), .m2_burstcount(m2_burstcount), .m2_read(m2_read), .m2_write(m2_write),
        .m2_writedata(m2_writedata), .m2_byteenable(m2_byteenable), .m2_waitrequest(1'b0),
        .m2_readdata(64'd0), .m2_readdatavalid(1'b0)
    );
    /* verilator lint_on PINCONNECTEMPTY */
endmodule
