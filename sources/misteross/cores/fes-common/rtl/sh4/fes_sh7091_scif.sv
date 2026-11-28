// SH7091 asynchronous serial controller with 16-byte FIFOs.
module fes_sh7091_scif (
    input  logic        clk, reset, peripheral_tick, stopped, external_tick,
    input  logic        request, dma_access,
    input  logic [3:0]  index,
    input  logic [3:0]  rmask, wstrb,
    input  logic [7:0]  wdata,
    output logic        rvalid,
    output logic [31:0] rdata,
    input  logic        rxd, cts_n,
    output logic        txd, txd_enable, rts_n,
    output logic [3:0]  irq,
    output logic [1:0]  dma_request, dma_ready
);
    logic [6:0] mode;
    logic [7:0] control, baud, fifo_control;
    logic [5:0] port_control;
    logic [7:0] status, status_seen;
    logic overrun, overrun_seen;
    logic [7:0] tx_fifo [0:15];
    logic [9:0] rx_fifo [0:15];
    logic [3:0] tx_head, tx_tail, rx_head, rx_tail;
    logic [4:0] tx_count, rx_count, next_tx_count, next_rx_count;
    logic [3:0] parity_count, framing_count;
    logic [4:0] rx_trigger, tx_trigger;
    logic tx_valid, tx_ready, tx_busy, rx_valid, rx_frame, rx_parity, serial_pin, rx_pin;
    logic [7:0] rx_byte;
    logic unused_tx_clock, unused_rx_mark;
    logic tx_put, tx_get, rx_put, rx_get, rx_attempt;
    logic read_byte, read_word, write_byte, write_word;
    logic status_read, status_write, reset_tx_fifo, reset_rx_fifo;
    logic [7:0] inactivity;
    logic [8:0] low_samples;
    logic rx_meta, rx_sync, cts_meta, cts_sync;
    logic [7:0] frame_samples;
    logic time_out, break_received, sample_tick;
    logic [14:0] baud_counter, baud_limit;

    assign read_byte = request && !stopped && wstrb == 0 && rmask == 4'h1;
    assign read_word = request && !stopped && wstrb == 0 && rmask == 4'h3;
    assign write_byte = request && !stopped && wstrb == 4'h1;
    assign write_word = request && !stopped && wstrb == 4'h3;
    assign status_read = read_word && index == 4;
    assign status_write = write_word && index == 4;
    assign reset_tx_fifo = fifo_control[2] || (write_word && index == 6 && wdata[2]);
    assign reset_rx_fifo = fifo_control[1] || (write_word && index == 6 && wdata[1]);

    assign tx_valid = tx_count != 0 && !reset_tx_fifo;
    assign tx_get = tx_valid && tx_ready;
    assign tx_put = write_byte && index == 3 && !reset_tx_fifo && (tx_count < 16 || tx_get);
    assign rx_get = read_byte && index == 5 && !reset_rx_fifo && rx_count != 0;
    assign rx_attempt = rx_valid && !reset_rx_fifo && !break_received;
    assign rx_put = rx_attempt && (rx_count < 16 || rx_get);
    assign next_tx_count = tx_count + {4'b0, tx_put} - {4'b0, tx_get};
    assign next_rx_count = rx_count + {4'b0, rx_put} - {4'b0, rx_get};
    assign rx_pin = fifo_control[0] ? txd : rx_sync;
    assign txd_enable = control[5] || port_control[1];
    assign txd = control[5] ? serial_pin : port_control[0];
    assign rts_n = fifo_control[3] ? rx_count >= 15 : port_control[5] && port_control[4];

    assign baud_limit = (({6'b0, baud, 1'b0} + 15'd2) << {mode[1:0], 1'b0}) - 15'd1;
    assign sample_tick = !stopped && (control[1] ? external_tick : peripheral_tick && baud_counter == baud_limit);
    assign frame_samples = (mode[6] ? 8'd144 : 8'd160) + (mode[5] ? 8'd16 : 8'd0);
    assign break_received = low_samples >= {frame_samples, 1'b0} - 9'd8 && !rx_pin;
    assign time_out = inactivity == 8'd248 && !rx_valid; // Stop-bit midpoint plus 15 bits.

    always_comb begin
        case (fifo_control[7:6])
            0: rx_trigger = 1;
            1: rx_trigger = 4;
            2: rx_trigger = 8;
            default: rx_trigger = 14;
        endcase
        case (fifo_control[5:4])
            0: tx_trigger = 8;
            1: tx_trigger = 4;
            2: tx_trigger = 2;
            default: tx_trigger = 1;
        endcase
    end
    assign irq = {control[7] && status[5], (control[6] || control[3]) && (status[4] || overrun),
                  control[6] && (status[1] || status[0]), (control[6] || control[3]) && status[7]};
    assign dma_ready = {rx_count != 0 && !fifo_control[1], tx_count < 16 && !fifo_control[2]};
    assign dma_request = {control[4] && (status[1] || status[0]), control[5] && status[5]};

    fes_sh7091_serial_async shift (
        .clk, .reset, .sample_tick, .tx_enable(control[5]), .rx_enable(control[4]),
        .tx_pause(fifo_control[3] && (fifo_control[0] ? rts_n : cts_sync)),
        .seven_bits(mode[6]), .parity_enable(mode[5]), .odd_parity(mode[4]), .two_stops(mode[3]),
        .multiprocessor(1'b0), .tx_mark(1'b0), .tx_valid, .tx_data(tx_fifo[tx_head]), .tx_ready, .tx_busy, .tx_pin(serial_pin), .tx_clock(unused_tx_clock),
        .rx_pin, .rx_valid, .rx_framing_error(rx_frame), .rx_parity_error(rx_parity), .rx_data(rx_byte), .rx_mark(unused_rx_mark)
    );

    always_ff @(posedge clk) begin
        rx_meta <= rxd;
        rx_sync <= rx_meta;
        cts_meta <= cts_n;
        cts_sync <= cts_meta;
        if (reset) begin
            rvalid <= 0;
            mode <= 0;
            baud <= '1;
            control <= 0;
            port_control <= 0;
            fifo_control <= 0;
            status <= 8'h60;
            status_seen <= 0;
            overrun <= 0;
            overrun_seen <= 0;
            tx_head <= 0;
            tx_tail <= 0;
            tx_count <= 0;
            rx_head <= 0;
            rx_tail <= 0;
            rx_count <= 0;
            parity_count <= 0;
            framing_count <= 0;
            baud_counter <= 0;
            inactivity <= 0;
            low_samples <= 0;
        end else begin
            rvalid <= request && wstrb == 0;
            if (request && wstrb == 0) begin
                rdata <= 0;
                case (index)
                    0: if (read_word) rdata[6:0] <= mode;
                    1: if (read_byte) rdata[7:0] <= baud;
                    2: if (read_word) rdata[7:0] <= control;
                    4: if (status_read) rdata[15:0] <= {parity_count, framing_count, status};
                    5: if (rx_get) rdata[7:0] <= rx_fifo[rx_head][7:0];
                    6: if (read_word) rdata[7:0] <= fifo_control;
                    7: if (read_word) rdata[15:0] <= {3'b0, tx_count, 3'b0, rx_count};
                    8: if (read_word)
                        rdata[7:0] <= {port_control[5], rts_n, port_control[3],
                                       port_control[3] ? port_control[2] : cts_sync, 2'b0, port_control[1], rx_pin};
                    9: if (read_word) rdata[0] <= overrun;
                    default: ;
                endcase
            end

            if (write_word) case (index)
                0: mode <= wdata[6:0] & 7'h7b;
                2: control <= wdata[7:0] & 8'hfa;
                6: fifo_control <= wdata[7:0];
                8: port_control <= {wdata[7:4], wdata[1:0]};
                default: ;
            endcase
            if (write_byte && index == 1) begin
                baud_counter <= 0;
                baud <= wdata[7:0];
            end else if (peripheral_tick && !stopped)
                baud_counter <= baud_counter == baud_limit ? 15'b0 : baud_counter + 15'd1;

            if (reset_tx_fifo) begin
                tx_count <= 0;
                tx_head <= 0;
                tx_tail <= 0;
            end else begin
                tx_count <= next_tx_count;
                if (tx_put) begin
                    tx_fifo[tx_tail] <= wdata[7:0];
                    tx_tail <= tx_tail + 4'd1;
                end
                if (tx_get)
                    tx_head <= tx_head + 4'd1;
            end
            if (reset_rx_fifo) begin
                rx_count <= 0;
                rx_head <= 0;
                rx_tail <= 0;
                parity_count <= 0;
                framing_count <= 0;
            end else begin
                rx_count <= next_rx_count;
                if (rx_put) begin
                    rx_fifo[rx_tail] <= {rx_frame, rx_parity, rx_byte};
                    rx_tail <= rx_tail + 4'd1;
                end
                if (rx_get)
                    rx_head <= rx_head + 4'd1;
                parity_count <= parity_count + {3'b0, rx_put && rx_parity} -
                                {3'b0, rx_get && rx_fifo[rx_head][8]};
                framing_count <= framing_count + {3'b0, rx_put && rx_frame} -
                                 {3'b0, rx_get && rx_fifo[rx_head][9]};
            end

            if (status_read)
                status_seen <= status;
            if (status_write) begin
                status <= status & (wdata[7:0] | ~status_seen | 8'h0c);
                status_seen <= 0;
            end
            if (rx_get)
                status[3:2] <= rx_fifo[rx_head][9:8];
            if (rx_attempt && (rx_frame || rx_parity))
                status[7] <= 1;
            if (tx_put && dma_access) begin
                status[6] <= 0;
                if (next_tx_count > tx_trigger)
                    status[5] <= 0;
            end else if (!control[5] || (!tx_busy && next_tx_count == 0))
                status[6] <= 1;
            if (next_tx_count <= tx_trigger || reset_tx_fifo)
                status[5] <= 1;
            if (break_received)
                status[4] <= 1;
            if (rx_get && dma_access && next_rx_count == 0)
                status[0] <= 0;
            else if (time_out && rx_count != 0 && rx_count < rx_trigger)
                status[0] <= 1;
            if (next_rx_count >= rx_trigger)
                status[1] <= 1;
            else if (rx_get && dma_access)
                status[1] <= 0;

            if (read_word && index == 9)
                overrun_seen <= overrun;
            if (write_word && index == 9 && !wdata[0] && overrun_seen) begin
                overrun <= 0;
                overrun_seen <= 0;
            end
            if (rx_attempt && !rx_put) begin
                overrun <= 1;
                overrun_seen <= 0;
            end

            if (rx_valid)
                inactivity <= 0;
            else if (sample_tick && !time_out)
                inactivity <= inactivity + 8'd1;
            if (rx_pin || !control[4])
                low_samples <= 0;
            else if (sample_tick && !(&low_samples))
                low_samples <= low_samples + 9'd1;
        end
    end
endmodule
