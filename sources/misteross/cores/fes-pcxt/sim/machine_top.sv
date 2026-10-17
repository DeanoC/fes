// SPDX-License-Identifier: GPL-2.0-or-later
module machine_top (
    input wire clk, reset,
    input wire code_valid,
    input wire [7:0] code,
    output wire code_ready,
    output wire speaker, audio_valid,
    output wire signed [15:0] audio_sample,
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
    wire keyboard_clock, keyboard_data, keyboard_clock_low, keyboard_data_low;
    pcxt_xt_keyboard keyboard (
        .clk(clk), .reset(reset), .code_valid(code_valid), .code(code), .code_ready(code_ready),
        .clock_low(keyboard_clock_low), .data_low(keyboard_data_low),
        .keyboard_clock(keyboard_clock), .keyboard_data(keyboard_data)
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
        .external_irq(6'b0), .inject(3'b0), .switches(8'h2d),
        .parity_error(1'b0), .channel_error(1'b0), .parity_enable(), .channel_check_enable(),
        .keyboard_clock(keyboard_clock), .keyboard_data(keyboard_data),
        .keyboard_clock_low(keyboard_clock_low), .keyboard_data_low(keyboard_data_low),
        .keyboard_irq(), .speaker(speaker), .audio_valid(audio_valid), .audio_sample(audio_sample),
        .intr(intr), .pit_out(pit_out), .pit_clock()
    );
    /* verilator lint_on PINCONNECTEMPTY */
endmodule
