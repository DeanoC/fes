// SPDX-License-Identifier: GPL-3.0-or-later
// MC6850 register/byte transport. The peer sees whole serial characters,
// delayed by the programmed clock divider and frame length, not pin bits.
// Motorola MC6850 data sheet: control/status, word select and overrun rules.
// https://www.buyicnow.com/files/datasheet/ADAPTER/1064.pdf
// ST keyboard and MIDI use a 500 kHz reference, divided by 64 and 16.
// CTS/DCD are wired active; no modem or serial-pin interface is claimed.
module st_acia #(
    parameter integer SYSTEM_CLOCK_HZ = 52_224_000,
    parameter integer SERIAL_CLOCK_HZ = 500_000
) (
    input wire clk, reset,
    input wire bus_req, bus_reg, bus_write,
    input wire [7:0] bus_wdata,
    output reg [7:0] bus_rdata,
    output reg bus_ack,
    output wire irq,
    output wire tx_valid,
    output wire [7:0] tx_data,
    input wire tx_ready,
    input wire rx_valid,
    input wire [7:0] rx_data,
    input wire rx_frame_error, rx_parity_error,
    output wire rx_ready
);
    reg [7:0] control, receive_data, transmit_data, shift_data;
    reg receive_full, transmit_full, tx_busy, rx_busy;
    reg frame_error, parity_error, overrun_pending, overrun, status_read;
    reg [7:0] rx_shift_data;
    reg rx_shift_frame, rx_shift_parity;
    reg [9:0] tx_remaining, rx_remaining;
    reg [31:0] clock_phase;
    wire [32:0] clock_sum = {1'b0, clock_phase} + 33'(SERIAL_CLOCK_HZ);
    wire serial_tick = clock_sum >= 33'(SYSTEM_CLOCK_HZ);
    wire master_reset = control[1:0] == 2'b11;
    wire bus_first = bus_req && !bus_ack;
    wire control_reset = bus_first && bus_write && !bus_reg && bus_wdata[1:0] == 2'b11;
    wire data_read = bus_first && !bus_write && bus_reg;
    wire [6:0] divider = control[1:0] == 0 ? 7'd1 :
                        control[1:0] == 1 ? 7'd16 : 7'd64;
    wire [3:0] frame_bits = control[4:2] == 2 || control[4:2] == 3 ||
                            control[4:2] == 5 ? 4'd10 : 4'd11;
    wire [9:0] character_ticks = 10'(divider) * 10'(frame_bits);
    wire tdre = !transmit_full && !master_reset;
    wire parity_selected = control[4:2] != 4 && control[4:2] != 5;
    assign irq = !reset && !master_reset &&
                 ((control[7] && (receive_full || overrun)) ||
                  (control[6:5] == 2'b01 && tdre));
    wire [7:0] status = {irq, parity_error, overrun, frame_error, 2'b00, tdre, receive_full};
    assign tx_valid = tx_busy && tx_remaining == 0 && !reset && !master_reset;
    assign tx_data = shift_data;
    assign rx_ready = !rx_busy && !reset && !master_reset;

    always @(posedge clk) begin
        if (reset) begin
            control <= 8'h03;
            receive_data <= 0; transmit_data <= 0; shift_data <= 0;
            receive_full <= 0; transmit_full <= 0; tx_busy <= 0; rx_busy <= 0;
            frame_error <= 0; parity_error <= 0; overrun_pending <= 0;
            overrun <= 0; status_read <= 0; rx_shift_data <= 0;
            rx_shift_frame <= 0; rx_shift_parity <= 0;
            tx_remaining <= 0; rx_remaining <= 0; clock_phase <= 0;
            bus_ack <= 0; bus_rdata <= 0;
        end else begin
            clock_phase <= serial_tick ? 32'(clock_sum - 33'(SYSTEM_CLOCK_HZ)) : clock_sum[31:0];
            if (!bus_req) bus_ack <= 0;
            if (bus_first) begin
                bus_ack <= 1;
                bus_rdata <= bus_reg ? receive_data : status;
                if (bus_write && !bus_reg) control <= bus_wdata;
            end
            if (master_reset || control_reset) begin
                receive_full <= 0; transmit_full <= 0; tx_busy <= 0; rx_busy <= 0;
                frame_error <= 0; parity_error <= 0; overrun_pending <= 0;
                overrun <= 0; status_read <= 0;
                tx_remaining <= 0; rx_remaining <= 0;
            end else begin
                if (serial_tick) begin
                    if (tx_remaining != 0) tx_remaining <= tx_remaining - 1'b1;
                    if (rx_remaining != 0) rx_remaining <= rx_remaining - 1'b1;
                end
                if (tx_valid && tx_ready) tx_busy <= 0;
                // One holding register plus one shift register, like the ACIA.
                if (!tx_busy && transmit_full && control[6:5] != 2'b11) begin
                    shift_data <= transmit_data;
                    tx_busy <= 1; tx_remaining <= character_ticks;
                    transmit_full <= 0;
                end
                if (bus_first && bus_write && bus_reg) begin
                    transmit_data <= control[4] ? bus_wdata : {1'b0, bus_wdata[6:0]};
                    transmit_full <= 1;
                end
                if (rx_valid && rx_ready) begin
                    rx_shift_data <= control[4] ? rx_data : {1'b0, rx_data[6:0]};
                    rx_shift_frame <= rx_frame_error;
                    rx_shift_parity <= parity_selected && rx_parity_error;
                    rx_remaining <= character_ticks; rx_busy <= 1;
                end
                if (bus_first && !bus_write && !bus_reg && overrun) status_read <= 1;
                if (data_read) begin
                    frame_error <= 0; parity_error <= 0;
                    if (overrun_pending) begin
                        // The valid byte is preserved. OVRN is exposed only
                        // after it is read; status then data clears the loss.
                        overrun_pending <= 0; overrun <= 1; receive_full <= 1;
                        status_read <= 0;
                    end else if (!overrun || status_read) begin
                        receive_full <= 0; overrun <= 0; status_read <= 0;
                    end
                end
                if (rx_busy && rx_remaining == 0) begin
                    rx_busy <= 0;
                    if (receive_full && !data_read) overrun_pending <= 1;
                    else begin
                        receive_data <= rx_shift_data; receive_full <= 1;
                        frame_error <= rx_shift_frame; parity_error <= rx_shift_parity;
                    end
                end
            end
        end
    end
endmodule
