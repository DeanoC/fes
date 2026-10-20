// SPDX-License-Identifier: GPL-2.0-or-later
// Portable motherboard + registered eight-slot fixture; storage remains external.
module motherboard_top (
    input wire clk, reset, guest_ce,
    input wire cycle_valid,
    output wire cycle_ready,
    input wire [19:0] cycle_address,
    input wire [7:0] cycle_wdata,
    input wire [1:0] cycle_kind,
    input wire cycle_write, cycle_lock, cpu_bus_lock,
    input wire [3:1] external_drq,
    output wire [1:0] dma_fault,
    output wire dma_owner, refresh_done,
    output wire aen,
    output wire [1:0] dma_channel,
    output wire terminal_count,
    output wire [47:0] observed_request,
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
    input wire [7:0] switches,
    input wire parity_error, channel_error,
    output wire parity_enable, channel_check_enable,
    input wire keyboard_clock, keyboard_data,
    // Raw cable pins are used when external_keyboard is set. Otherwise the
    // integrated serializer drives them and may retire code_valid.
    input wire external_keyboard,
    input wire code_valid,
    input wire [7:0] code,
    output wire code_ready,
    output wire keyboard_clock_low, keyboard_data_low, keyboard_irq, keyboard_clear,
    output wire speaker, audio_valid,
    output wire signed [15:0] audio_sample,
    input wire [2:0] inject,
    output wire intr,
    output wire [2:0] pit_out,
    output wire pit_clock
);
    wire isa_valid, isa_ready, isa_done;
    wire [7:0] isa_rdata, irq;
    wire [1:0] isa_fault;
    wire [3:1] drq;
    wire ser_clock, ser_data;
    wire board_clock = external_keyboard ? keyboard_clock : ser_clock;
    wire board_data = external_keyboard ? keyboard_data : ser_data;
    pcxt_xt_keyboard serializer (
        .clk(clk), .reset(reset),
        .code_valid(code_valid && !external_keyboard), .code(code), .code_ready(code_ready),
        .clock_low(keyboard_clock_low), .data_low(keyboard_data_low),
        .keyboard_clear(keyboard_clear),
        .keyboard_clock(ser_clock), .keyboard_data(ser_data)
    );
    pcxt_motherboard board (
        .clk(clk), .reset(reset), .guest_ce(guest_ce),
        .cycle_valid(cycle_valid), .cycle_ready(cycle_ready),
        .cycle_address(cycle_address), .cycle_wdata(cycle_wdata),
        .cycle_kind(cycle_kind), .cycle_write(cycle_write), .cycle_lock(cycle_lock), .cpu_bus_lock(cpu_bus_lock),
        .cycle_done(cycle_done), .cycle_rdata(cycle_rdata), .cycle_fault(cycle_fault),
        .address(address), .wdata(wdata), .kind(kind),
        .write_cycle(write_cycle), .locked(locked), .aen(aen),
        .dma_channel(dma_channel), .terminal_count(terminal_count),
        .isa_drq(drq | external_drq), .dma_fault(dma_fault), .refresh_done(refresh_done), .dma_owner(dma_owner),
        .memory_valid(memory_valid), .memory_ready(memory_ready),
        .memory_done(memory_done), .memory_rdata(memory_rdata),
        .isa_valid(isa_valid), .isa_ready(isa_ready), .isa_done(isa_done),
        .isa_rdata(isa_rdata), .isa_fault(isa_fault), .isa_irq(irq[7:2] | external_irq),
        .switches(switches), .parity_error(parity_error), .channel_error(channel_error),
        .parity_enable(parity_enable), .channel_check_enable(channel_check_enable),
        .keyboard_clock(board_clock), .keyboard_data(board_data),
        .keyboard_clock_low(keyboard_clock_low), .keyboard_data_low(keyboard_data_low),
        .keyboard_clear(keyboard_clear),
        .keyboard_irq(keyboard_irq), .speaker(speaker),
        .audio_valid(audio_valid), .audio_sample(audio_sample), .intr(intr),
        .pit_out(pit_out), .pit_clock(pit_clock)
    );
    /* verilator lint_off PINCONNECTEMPTY */
    isa_top isa (
        .clk(clk), .reset(reset), .cycle_valid(isa_valid), .cycle_ready(isa_ready),
        .cycle_address(address), .cycle_wdata(wdata), .cycle_kind(kind),
        .cycle_write(write_cycle), .cycle_aen(aen), .cycle_lock(locked),
        .cycle_dma_channel(dma_channel), .cycle_terminal_count(terminal_count), .guest_ce(guest_ce),
        .cycle_done(isa_done), .cycle_rdata(isa_rdata), .cycle_fault(isa_fault),
        .irq(irq), .drq(drq), .inject(inject), .observed_request(observed_request), .reset_side_effect()
    );
    /* verilator lint_on PINCONNECTEMPTY */
    // Slot IRQ0/1 are deliberately not connected to the motherboard PIC.
    wire unused_irq = ^irq[1:0];
endmodule
