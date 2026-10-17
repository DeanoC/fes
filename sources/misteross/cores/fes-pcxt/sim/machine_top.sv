// SPDX-License-Identifier: GPL-2.0-or-later
module machine_top (
    input wire clk, reset,
    output wire memory_valid,
    input wire memory_ready, memory_done,
    input wire [7:0] memory_rdata,
    output wire [19:0] address,
    output wire [7:0] wdata,
    output wire write_cycle,
    output wire [1:0] fault,
    output wire guest_ce, halted, intr,
    output wire accepted, completed,
    output wire [1:0] cycle_kind,
    output wire [7:0] cycle_rdata,
    output wire cycle_lock,
    output wire [2:0] pit_out
);
    wire cycle_valid, cycle_ready, cycle_done, cycle_write;
    wire [19:0] cycle_address;
    wire [7:0] cycle_wdata;
    wire [1:0] cycle_fault;
    assign accepted = cycle_valid && cycle_ready;
    assign completed = cycle_done;
    pcxt_cpu cpu (
        .clk(clk), .reset(reset), .intr(intr), .nmi(1'b0),
        .cycle_valid(cycle_valid), .cycle_ready(cycle_ready),
        .cycle_address(cycle_address), .cycle_wdata(cycle_wdata),
        .cycle_kind(cycle_kind), .cycle_write(cycle_write), .cycle_lock(cycle_lock),
        .cycle_done(cycle_done), .cycle_rdata(cycle_rdata), .cycle_fault(cycle_fault),
        .fault(fault), .guest_ce(guest_ce), .halted(halted)
    );
    /* verilator lint_off PINCONNECTEMPTY */
    motherboard_top motherboard (
        .clk(clk), .reset(reset), .guest_ce(guest_ce),
        .cycle_valid(cycle_valid), .cycle_ready(cycle_ready),
        .cycle_address(cycle_address), .cycle_wdata(cycle_wdata),
        .cycle_kind(cycle_kind), .cycle_write(cycle_write), .cycle_lock(cycle_lock),
        .cycle_done(cycle_done), .cycle_rdata(cycle_rdata), .cycle_fault(cycle_fault),
        .address(address), .wdata(wdata), .kind(), .write_cycle(write_cycle), .locked(),
        .memory_valid(memory_valid), .memory_ready(memory_ready),
        .memory_done(memory_done), .memory_rdata(memory_rdata),
        .external_irq(6'b0), .keyboard_irq(1'b0), .pit_gate2(1'b0), .inject(3'b0),
        .intr(intr), .pit_out(pit_out), .pit_clock()
    );
    /* verilator lint_on PINCONNECTEMPTY */
endmodule
