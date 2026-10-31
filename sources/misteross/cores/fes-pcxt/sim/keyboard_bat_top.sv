// SPDX-License-Identifier: GPL-2.0-or-later
module keyboard_bat_top (
    input wire clk,
    // Imported PPI reset is asynchronous; the fixture holds the shared reset
    // across fabric edges, as the existing motherboard fixtures do.
    /* verilator lint_off SYNCASYNCNET */
    input wire reset,
    /* verilator lint_on SYNCASYNCNET */
    input wire chip_select_n, read_enable_n, write_enable_n,
    input wire [1:0] address,
    input wire [7:0] wdata,
    output wire [7:0] rdata,
    output wire keyboard_irq, clock_low, data_low, keyboard_clear,
    output wire keyboard_clock, keyboard_data
);
    pcxt_xt_keyboard_bat keyboard (
        .clk(clk), .reset(reset), .clock_low(clock_low), .data_low(data_low),
        .keyboard_clear(keyboard_clear), .keyboard_clock(keyboard_clock), .keyboard_data(keyboard_data)
    );
    /* verilator lint_off PINCONNECTEMPTY */
    pcxt_xtio motherboard (
        .clk(clk), .reset(reset), .chip_select_n(chip_select_n),
        .read_enable_n(read_enable_n), .write_enable_n(write_enable_n),
        .address(address), .wdata(wdata), .rdata(rdata), .switches(8'h2d),
        .parity_error(1'b0), .channel_error(1'b0), .parity_enable(), .channel_check_enable(),
        .keyboard_clock(keyboard_clock), .keyboard_data(keyboard_data),
        .keyboard_clock_low(clock_low), .keyboard_data_low(data_low),
        .keyboard_clear(keyboard_clear), .keyboard_irq(keyboard_irq),
        .pit_out2(1'b0), .pit_gate2(), .speaker()
    );
    /* verilator lint_on PINCONNECTEMPTY */
endmodule
