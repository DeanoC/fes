// SPDX-License-Identifier: GPL-2.0-or-later
// Real 8088 + motherboard + registered ISA display cards. Storage/font external.
module pcxt_machine #(parameter bit DIAGNOSTIC_PROBE = 0) (
    input wire clk, reset,
    output wire memory_valid,
    input wire memory_ready, memory_done,
    input wire [7:0] memory_rdata,
    output wire [19:0] address,
    output wire [7:0] wdata,
    output wire write_cycle,
    output wire [1:0] fault,
    output wire guest_ce, halted,
    output wire accepted, completed,
    output wire [1:0] cycle_kind,
    output wire [7:0] cycle_rdata,
    output wire [11:0] cga_font_address, mda_font_address,
    input wire [7:0] cga_font_data, mda_font_data,
    output wire [1:0] pixel_ce, de, hs, vs, sof, eol,
    output wire [7:0] rgbi,
    output wire dma_owner, refresh_done, audio_valid,
    output wire signed [15:0] audio_sample
);
    wire [1:0] cpu_fault, dma_fault;
    assign fault = cpu_fault | dma_fault;
    wire cycle_valid, cycle_ready, cycle_done, cycle_write, cycle_lock, bus_locked, intr;
    wire [19:0] cycle_address;
    wire [7:0] cycle_wdata;
    wire [1:0] cycle_fault;
    wire isa_valid, isa_ready, isa_done, aen, terminal_count, locked;
    wire [1:0] isa_fault, kind, dma_channel;
    wire [7:0] isa_rdata, irq;
    wire [2:0] drq;
    assign accepted = cycle_valid && cycle_ready;
    assign completed = cycle_done;
    pcxt_cpu cpu (
        .clk(clk), .reset(reset), .intr(intr), .nmi(1'b0),
        .cycle_valid(cycle_valid), .cycle_ready(cycle_ready),
        .cycle_address(cycle_address), .cycle_wdata(cycle_wdata),
        .cycle_kind(cycle_kind), .cycle_write(cycle_write), .cycle_lock(cycle_lock),
        .cycle_done(cycle_done), .cycle_rdata(cycle_rdata), .cycle_fault(cycle_fault),
        .fault(cpu_fault), .guest_ce(guest_ce), .halted(halted), .bus_locked(bus_locked)
    );
    /* verilator lint_off PINCONNECTEMPTY */
    pcxt_motherboard board (
        .clk(clk), .reset(reset), .guest_ce(guest_ce),
        .cycle_valid(cycle_valid), .cycle_ready(cycle_ready),
        .cycle_address(cycle_address), .cycle_wdata(cycle_wdata),
        .cycle_kind(cycle_kind), .cycle_write(cycle_write), .cycle_lock(cycle_lock),
        .cycle_done(cycle_done), .cycle_rdata(cycle_rdata), .cycle_fault(cycle_fault),
        .address(address), .wdata(wdata), .kind(kind), .write_cycle(write_cycle), .locked(locked),
        .memory_valid(memory_valid), .memory_ready(memory_ready),
        .memory_done(memory_done), .memory_rdata(memory_rdata),
        .cpu_bus_lock(bus_locked), .isa_drq(drq), .dma_fault(dma_fault),
        .dma_owner(dma_owner), .refresh_done(refresh_done), .aen(aen), .dma_channel(dma_channel),
        .terminal_count(terminal_count),
        .isa_valid(isa_valid), .isa_ready(isa_ready), .isa_done(isa_done),
        .isa_rdata(isa_rdata), .isa_fault(isa_fault), .isa_irq(irq[7:2]),
        .switches(8'h2d), .parity_error(1'b0), .channel_error(1'b0),
        .parity_enable(), .channel_check_enable(),
        .keyboard_clock(1'b1), .keyboard_data(1'b0),
        .keyboard_clock_low(), .keyboard_data_low(), .keyboard_clear(), .keyboard_irq(),
        .speaker(), .audio_valid(audio_valid), .audio_sample(audio_sample), .intr(intr), .pit_out(), .pit_clock()
    );
    pcxt_display_bus #(.DIAGNOSTIC_PROBE(DIAGNOSTIC_PROBE)) isa (
        .clk(clk), .reset(reset), .cycle_valid(isa_valid), .cycle_ready(isa_ready),
        .cycle_address(address), .cycle_wdata(wdata), .cycle_kind(kind),
        .cycle_write(write_cycle), .cycle_aen(aen), .cycle_lock(locked),
        .cycle_dma_channel(dma_channel), .cycle_terminal_count(terminal_count), .guest_ce(guest_ce),
        .cycle_done(isa_done), .cycle_rdata(isa_rdata), .cycle_fault(isa_fault), .irq(irq), .drq(drq),
        .light_pen_trigger(1'b0), .light_pen_switch(1'b0),
        .cga_font_address(cga_font_address), .mda_font_address(mda_font_address),
        .cga_font_data(cga_font_data), .mda_font_data(mda_font_data),
        .pixel_ce(pixel_ce), .de(de), .hs(hs), .vs(vs), .sof(sof), .eol(eol), .rgbi(rgbi),
        .unsupported_interlace()
    );
    /* verilator lint_on PINCONNECTEMPTY */
    wire unused_irq = ^irq[1:0];
endmodule
