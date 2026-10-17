// SPDX-License-Identifier: GPL-2.0-or-later
// IBM 5160 PPI wiring and keyboard receiver. All signals use the 100 MHz
// fabric clock; keyboard inputs are synchronized here. The external keyboard
// must obey open-collector clock inhibit and data-line busy/CTS indications.
module pcxt_xtio (
    input wire clk, reset,
    input wire chip_select_n, read_enable_n, write_enable_n,
    input wire [1:0] address,
    input wire [7:0] wdata,
    output wire [7:0] rdata,
    input wire [7:0] switches,
    input wire parity_error, channel_error,
    output wire parity_enable, channel_check_enable,
    input wire keyboard_clock, keyboard_data,
    output wire keyboard_clock_low, keyboard_data_low, keyboard_clear,
    output reg keyboard_irq,
    input wire pit_out2,
    output wire pit_gate2, speaker
);
    wire [7:0] pb_out;
    wire pb_input;
    // Undriven PPI outputs do not enable speaker, parity checking or the
    // keyboard clock. Port B reads its pull-ups when programmed as input.
    wire [7:0] pb = pb_input ? 8'b0 : pb_out;
    wire unused_spare = pb[2];
    wire clear_keyboard = pb[7];
    assign keyboard_clear = clear_keyboard;
    reg [7:0] shift_data;
    reg [3:0] bits_received;
    reg [2:0] clock_sync;
    reg [1:0] data_sync;
    wire falling_clock = clock_sync[2:1] == 2'b10;
    assign keyboard_clock_low = !pb[6];
    assign keyboard_data_low = keyboard_irq;
    assign pit_gate2 = pb[0];
    assign speaker = pb[1] && pit_out2;
    assign parity_enable = !pb_input && !pb[4];
    assign channel_check_enable = !pb_input && !pb[5];
    always @(posedge clk) begin
        if (reset) begin
            clock_sync <= 3'b111; data_sync <= 2'b0;
            shift_data <= 0; bits_received <= 0; keyboard_irq <= 0;
        end else begin
            clock_sync <= {clock_sync[1:0], keyboard_clock && !keyboard_clock_low};
            data_sync <= {data_sync[0], keyboard_data && !keyboard_data_low};
            if (clear_keyboard) begin
                shift_data <= 0; bits_received <= 0; keyboard_irq <= 0;
            end else if (falling_clock && !keyboard_clock_low && !keyboard_irq) begin
                // A leading zero (the 83-key RTS pulse) does not enter the
                // shift register. Start=1, then eight bits LSB first, no parity.
                if (bits_received == 0) begin
                    if (data_sync[1]) bits_received <= 1;
                end else begin
                    shift_data <= {data_sync[1], shift_data[7:1]};
                    if (bits_received == 8) begin
                        bits_received <= 0; keyboard_irq <= 1;
                    end else bits_received <= bits_received + 1'b1;
                end
            end
        end
    end
    wire [7:0] pc_in = {parity_error, channel_error, pit_out2, 1'b0,
                       (pb[3] ? switches[7:4] : switches[3:0])};
    /* verilator lint_off PINCONNECTEMPTY */
    KF8255 ppi (
        .clock(clk), .reset(reset), .chip_select_n(chip_select_n),
        .read_enable_n(read_enable_n), .write_enable_n(write_enable_n),
        .address(address), .data_bus_in(wdata), .data_bus_out(rdata),
        .port_a_in(clear_keyboard ? 8'hff : shift_data), .port_a_out(), .port_a_io(),
        .port_b_in(8'hff), .port_b_out(pb_out), .port_b_io(pb_input),
        .port_c_in(pc_in), .port_c_out(), .port_c_io()
    );
    /* verilator lint_on PINCONNECTEMPTY */
endmodule
