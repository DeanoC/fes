// SH7091 SCI register file and double-buffered serial paths.
module fes_sh7091_sci (
    input  logic        clk, reset, peripheral_tick,
    input  logic        request, dma_access,
    input  logic [3:0]  index,
    input  logic [3:0]  rmask, wstrb,
    input  logic [7:0]  wdata,
    output logic        rvalid,
    output logic [31:0] rdata,
    input  logic        rxd, sck_in,
    output logic        txd, txd_enable, sck_out, sck_enable,
    output logic [3:0]  irq,
    output logic [1:0]  dma_request, dma_ready
);
    logic [7:0] mode, control, baud, status, transmit_data, receive_data;
    logic [4:0] port_control;
    logic [7:3] status_seen;
    logic read_byte, write_byte, dma_tx_write, dma_rx_read, receive_error;
    logic tx_valid, tx_ready, tx_busy, rx_enabled, rx_valid, rx_frame, rx_parity, rx_mark;
    logic [7:0] rx_byte;

    logic async_tick, async_ready, async_busy, async_txd, async_sck;
    logic async_valid, async_frame, async_parity, async_mark;
    logic [7:0] async_byte;
    logic [14:0] baud_count, baud_limit;
    logic baud_tick, rx_meta, rx_sync, sck_meta, sck_sync, sck_previous;

    logic sync_clock, sync_run, sync_rise, sync_fall, sync_busy, sync_ready, sync_valid;
    logic [2:0] sync_tx_bit, sync_rx_bit;
    logic [1:0] sync_sample;
    logic [7:0] sync_tx_shift, sync_rx_shift;

    assign read_byte = request && wstrb == 0 && rmask == 4'h1;
    assign write_byte = request && wstrb == 4'h1;
    assign dma_tx_write = write_byte && dma_access && index == 3;
    assign dma_rx_read = read_byte && dma_access && index == 5;
    assign receive_error = |status[5:3];
    assign rx_enabled = control[4] && !receive_error;
    assign tx_valid = control[5] && !status[7] && (!mode[7] || !receive_error);

    assign baud_limit = (({6'b0, baud, 1'b0} + 15'd2) << {mode[1:0], 1'b0}) - 15'd1;
    assign baud_tick = peripheral_tick && baud_count == baud_limit;
    assign async_tick = control[1] ? sck_sync && !sck_previous : baud_tick;
    assign sync_run = mode[7] && (sync_busy || tx_valid || rx_enabled);
    assign sync_rise = control[1] ? sck_sync && !sck_previous : baud_tick && sync_run && !sync_clock;
    assign sync_fall = control[1] ? !sck_sync && sck_previous : baud_tick && sync_run && sync_clock;
    assign sync_ready = !sync_busy && sync_fall;

    assign tx_ready = mode[7] ? sync_ready : async_ready;
    assign tx_busy = mode[7] ? sync_busy : async_busy;
    assign txd_enable = control[5] || port_control[1];
    assign txd = control[5] ? (mode[7] ? sync_tx_shift[0] : async_txd) : port_control[0];
    assign sck_enable = !control[1] && (mode[7] || control[0] || port_control[3]);
    assign sck_out = mode[7] ? sync_clock : control[0] ? async_sck : port_control[2];
    assign rx_valid = mode[7] ? sync_valid : async_valid;
    assign rx_byte = mode[7] ? sync_rx_shift : async_byte;
    assign rx_frame = !mode[7] && async_frame;
    assign rx_parity = !mode[7] && async_parity;
    assign rx_mark = !mode[7] && mode[2] && async_mark;

    assign irq = {control[2] && status[2], control[7] && status[7],
                  control[6] && status[6] && !port_control[4], control[6] && receive_error};
    assign dma_ready = {status[6], status[7]};
    assign dma_request = {control[4] && status[6], control[5] && status[7]};

    fes_sh7091_serial_async asynchronous (
        .clk, .reset, .sample_tick(async_tick), .tx_enable(!mode[7] && control[5]),
        .rx_enable(!mode[7] && rx_enabled), .tx_pause(1'b0),
        .seven_bits(mode[6]), .parity_enable(mode[5]), .odd_parity(mode[4]), .two_stops(mode[3]),
        .multiprocessor(mode[2]), .tx_mark(status[0]), .tx_valid, .tx_data(transmit_data),
        .tx_ready(async_ready), .tx_busy(async_busy), .tx_pin(async_txd), .tx_clock(async_sck),
        .rx_pin(rxd), .rx_valid(async_valid), .rx_framing_error(async_frame), .rx_parity_error(async_parity),
        .rx_data(async_byte), .rx_mark(async_mark)
    );

    always_ff @(posedge clk) begin
        rx_meta <= rxd;
        rx_sync <= rx_meta;
        sck_meta <= sck_in;
        sck_sync <= sck_meta;
        sck_previous <= sck_sync;
        sync_valid <= 0;
        if (reset || !mode[7]) begin
            sync_busy <= 0;
            sync_clock <= 1;
            sync_tx_shift <= '1;
            sync_rx_bit <= 0;
            sync_sample <= 0;
        end else begin
            sync_sample <= {sync_sample[0], sync_rise};
            if (!sync_run || control[1])
                sync_clock <= 1;
            else if (baud_tick)
                sync_clock <= !sync_clock;

            if (!control[5] || receive_error)
                sync_busy <= 0;
            else if (sync_ready && tx_valid) begin
                sync_busy <= 1;
                sync_tx_shift <= transmit_data;
                sync_tx_bit <= 0;
            end else if (sync_busy) begin
                if (sync_fall)
                    sync_tx_shift <= {sync_tx_shift[7], sync_tx_shift[7:1]};
                if (sync_rise) begin
                    sync_tx_bit <= sync_tx_bit + 3'd1;
                    if (&sync_tx_bit)
                        sync_busy <= 0;
                end
            end

            if (!rx_enabled)
                sync_rx_bit <= 0;
            else if (control[1] ? sync_rise : sync_sample[1]) begin
                sync_rx_shift <= {rx_sync, sync_rx_shift[7:1]};
                sync_rx_bit <= sync_rx_bit + 3'd1;
                sync_valid <= &sync_rx_bit;
            end
        end
    end

    always_ff @(posedge clk) begin
        if (reset) begin
            rvalid <= 0;
            mode <= 0;
            control <= 0;
            baud <= '1;
            status <= 8'h84;
            status_seen <= 0;
            port_control <= 0;
            transmit_data <= '1;
            receive_data <= 0;
            baud_count <= 0;
        end else begin
            rvalid <= request && wstrb == 0;
            if (request && wstrb == 0) begin
                rdata <= 0;
                if (read_byte) case (index)
                    0: rdata[7:0] <= mode;
                    1: rdata[7:0] <= baud;
                    2: rdata[7:0] <= control;
                    3: rdata[7:0] <= transmit_data;
                    4: rdata[7:0] <= status;
                    5: rdata[7:0] <= receive_data;
                    7: rdata[7:0] <= {port_control[4], 3'b0, port_control[3],
                                      sck_enable ? sck_out : sck_sync, port_control[1], rx_sync};
                    default: ;
                endcase
            end
            if (write_byte) case (index)
                0: mode <= wdata;
                1: baud <= wdata;
                2: control <= wdata;
                3: transmit_data <= wdata;
                7: port_control <= {wdata[7], wdata[3:0]};
                default: ;
            endcase

            if (write_byte && (index == 0 || index == 1))
                baud_count <= 0;
            else if (peripheral_tick)
                baud_count <= baud_tick ? 15'b0 : baud_count + 15'd1;

            if (read_byte && index == 4)
                status_seen <= status[7:3];
            if (write_byte && index == 4) begin
                status[7:3] <= status[7:3] & (wdata[7:3] | ~status_seen);
                status[0] <= wdata[0];
                status_seen <= 0;
            end

            if (dma_tx_write || (write_byte && index == 4 && status_seen[7] && !wdata[7]))
                {status[7], status[2]} <= 2'b00;
            if (!control[5] || (tx_ready && tx_valid))
                status[7] <= 1;
            if (!control[5] || (!tx_busy && status[7] && !dma_tx_write &&
                               !(write_byte && index == 4 && status_seen[7] && !wdata[7])))
                status[2] <= 1;

            if (dma_rx_read)
                status[6] <= 0;
            if (rx_valid && (mode[7] || !mode[2] || !control[3] || rx_mark)) begin
                if (rx_mark)
                    control[3] <= 0;
                status[1] <= rx_mark;
                if (status[6] && !dma_rx_read)
                    status[5] <= 1;
                else begin
                    receive_data <= rx_byte;
                    status[4:3] <= {rx_frame, rx_parity};
                    status[6] <= !rx_frame && !rx_parity;
                end
            end
        end
    end
endmodule
