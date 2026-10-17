// SPDX-License-Identifier: GPL-2.0-or-later
// Portable motherboard + registered eight-slot fixture; storage remains external.
module motherboard_top (
    input wire clk, reset, guest_ce,
    input wire cycle_valid,
    output wire cycle_ready,
    input wire [19:0] cycle_address,
    input wire [7:0] cycle_wdata,
    input wire [1:0] cycle_kind,
    input wire cycle_write, cycle_lock,
    output wire cycle_done,
    output wire [7:0] cycle_rdata,
    output wire [1:0] cycle_fault,
    output wire [19:0] address,
    output wire [7:0] wdata,
    output wire [1:0] kind,
    output wire write_cycle, locked,
    output wire memory_valid,
    input wire memory_ready, memory_done,
    input wire [7:0] memory_rdata,
    input wire [7:2] external_irq,
    input wire keyboard_irq, pit_gate2,
    input wire [2:0] inject,
    output wire intr,
    output wire [2:0] pit_out,
    output wire pit_clock
);
    wire isa_valid, isa_ready, isa_done;
    wire [7:0] isa_rdata, irq;
    wire [1:0] isa_fault;
    pcxt_motherboard board (
        .clk(clk), .reset(reset), .guest_ce(guest_ce),
        .cycle_valid(cycle_valid), .cycle_ready(cycle_ready),
        .cycle_address(cycle_address), .cycle_wdata(cycle_wdata),
        .cycle_kind(cycle_kind), .cycle_write(cycle_write), .cycle_lock(cycle_lock),
        .cycle_done(cycle_done), .cycle_rdata(cycle_rdata), .cycle_fault(cycle_fault),
        .address(address), .wdata(wdata), .kind(kind),
        .write_cycle(write_cycle), .locked(locked),
        .memory_valid(memory_valid), .memory_ready(memory_ready),
        .memory_done(memory_done), .memory_rdata(memory_rdata),
        .isa_valid(isa_valid), .isa_ready(isa_ready), .isa_done(isa_done),
        .isa_rdata(isa_rdata), .isa_fault(isa_fault), .isa_irq(irq[7:2] | external_irq),
        .keyboard_irq(keyboard_irq), .pit_gate2(pit_gate2), .intr(intr),
        .pit_out(pit_out), .pit_clock(pit_clock)
    );
    /* verilator lint_off PINCONNECTEMPTY */
    isa_top isa (
        .clk(clk), .reset(reset), .cycle_valid(isa_valid), .cycle_ready(isa_ready),
        .cycle_address(address), .cycle_wdata(wdata), .cycle_kind(kind),
        .cycle_write(write_cycle), .cycle_aen(1'b0), .cycle_lock(locked),
        .cycle_dma_channel(2'b0), .cycle_terminal_count(1'b0), .guest_ce(guest_ce),
        .cycle_done(isa_done), .cycle_rdata(isa_rdata), .cycle_fault(isa_fault),
        .irq(irq), .drq(), .inject(inject), .observed_request(), .reset_side_effect()
    );
    /* verilator lint_on PINCONNECTEMPTY */
    // Slot IRQ0/1 are deliberately not connected to the motherboard PIC.
    wire unused_irq = ^irq[1:0];
endmodule
