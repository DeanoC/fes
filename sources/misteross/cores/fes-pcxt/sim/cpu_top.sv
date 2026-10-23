// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_pcxt_isa.vh"
// CPU integration fixture. RAM/ROM and the interrupt vector source are supplied
// by the testbench; all other bytes traverse the real registered ISA fabric.
module cpu_top (
    input wire clk, reset, intr, nmi,
    output wire memory_valid,
    input wire memory_ready,
    output wire [19:0] address,
    output wire [7:0] wdata,
    output wire [1:0] kind,
    output wire write_cycle, locked,
    input wire memory_done,
    input wire [7:0] memory_rdata,
    output wire accepted, completed,
    output wire [7:0] rdata,
    output wire [1:0] fault,
    output wire [7:0] irq,
    output wire [2:0] drq,
    output wire guest_ce, halted,
    input wire [2:0] inject
);

    wire cycle_valid, cycle_ready, cycle_done;
    wire [1:0] cycle_fault;
    wire local_target = (kind == 0 && (address < 20'ha0000 || address >= 20'hf0000)) ||
                       kind == 2 || (kind == 1 && address == 20'h00080);
    wire isa_ready, isa_done;
    wire [7:0] isa_rdata;
    wire [1:0] isa_fault;
    assign memory_valid = cycle_valid && local_target;
    assign cycle_ready = local_target ? memory_ready : isa_ready;
    assign cycle_done = local_target ? memory_done : isa_done;
    assign rdata = local_target ? memory_rdata : isa_rdata;
    assign cycle_fault = local_target ? 2'b0 : isa_fault;
    assign accepted = cycle_valid && cycle_ready;
    assign completed = cycle_done;
    wire unused_bus_locked;
    pcxt_cpu cpu (
        .clk(clk), .reset(reset), .intr(intr), .nmi(nmi),
        .cycle_valid(cycle_valid), .cycle_ready(cycle_ready),
        .cycle_address(address), .cycle_wdata(wdata), .cycle_kind(kind),
        .cycle_write(write_cycle), .cycle_lock(locked), .cycle_done(cycle_done),
        .cycle_rdata(rdata), .cycle_fault(cycle_fault), .fault(fault),
        .guest_ce(guest_ce), .halted(halted), .bus_locked(unused_bus_locked)
    );
    /* verilator lint_off PINCONNECTEMPTY */
    isa_top isa (
        .clk(clk), .reset(reset), .cycle_valid(cycle_valid && !local_target),
        .cycle_ready(isa_ready), .cycle_address(address), .cycle_wdata(wdata),
        .cycle_kind(kind), .cycle_write(write_cycle), .cycle_aen(1'b0),
        .cycle_lock(locked), .cycle_dma_channel(2'b0), .cycle_terminal_count(1'b0),
        .guest_ce(guest_ce), .cycle_done(isa_done), .cycle_rdata(isa_rdata),
        .cycle_fault(isa_fault), .irq(irq), .drq(drq), .inject(inject),
        .observed_request(), .reset_side_effect()
    );
    /* verilator lint_on PINCONNECTEMPTY */
endmodule
