// Common asynchronous shifter for the SCI and SCIF front ends. The caller
// supplies a synchronous 16x baud enable and maintains the data registers.
module fes_sh7091_serial_async (
    input  logic        clk, reset, sample_tick,
    input  logic        tx_enable, rx_enable, tx_pause,
    input  logic        seven_bits, parity_enable, odd_parity, two_stops,
    input  logic        multiprocessor, tx_mark,
    input  logic        tx_valid,
    input  logic [7:0]  tx_data,
    output logic        tx_ready, tx_busy, tx_pin, tx_clock,
    input  logic        rx_pin,
    output logic        rx_valid, rx_framing_error, rx_parity_error, rx_mark,
    output logic [7:0]  rx_data
);
    logic [10:0] tx_frame, tx_shift;
    logic [3:0] tx_phase, tx_remaining, tx_length;
    logic parity, extra_bit, extra_value;

    typedef enum logic [2:0] {RX_IDLE, RX_START, RX_DATA, RX_PARITY, RX_STOP} rx_state_t;
    rx_state_t rx_state;
    logic rx_meta, rx_sync, accumulated_parity, sampled_parity_error;
    logic [3:0] rx_phase;
    logic [2:0] rx_bit;

    assign parity = (^tx_data[6:0]) ^ (!seven_bits && tx_data[7]) ^ odd_parity;
    assign extra_bit = multiprocessor || parity_enable;
    assign extra_value = multiprocessor ? tx_mark : parity;
    assign tx_clock = !tx_busy || tx_phase[3];
    assign tx_ready = tx_enable && !tx_pause && sample_tick &&
                      (!tx_busy || (tx_remaining == 1 && (&tx_phase)));
    assign tx_busy = tx_remaining != 0;
    assign tx_pin = tx_enable && tx_busy ? tx_shift[0] : 1'b1;
    assign tx_length = (seven_bits ? 4'd9 : 4'd10) + {3'b0, extra_bit} + {3'b0, two_stops};
    always_comb begin
        tx_frame = '1;
        tx_frame[0] = 0;
        tx_frame[7:1] = tx_data[6:0];
        if (seven_bits)
            tx_frame[8] = extra_bit ? extra_value : 1'b1;
        else begin
            tx_frame[8] = tx_data[7];
            tx_frame[9] = extra_bit ? extra_value : 1'b1;
        end
    end

    always_ff @(posedge clk) begin
        if (reset || !tx_enable) begin
            tx_remaining <= 0;
            tx_phase <= 0;
        end else if (sample_tick) begin
            if (tx_ready && tx_valid) begin
                tx_shift <= tx_frame;
                tx_remaining <= tx_length;
                tx_phase <= 0;
            end else if (tx_busy) begin
                tx_phase <= tx_phase + 4'd1;
                if (&tx_phase) begin
                    tx_shift <= {1'b1, tx_shift[10:1]};
                    tx_remaining <= tx_remaining - 4'd1;
                end
            end
        end
    end

    always_ff @(posedge clk) begin
        rx_valid <= 0;
        rx_meta <= rx_pin;
        rx_sync <= rx_meta;
        if (reset || !rx_enable) begin
            rx_state <= RX_IDLE;
            rx_phase <= 0;
            rx_mark <= 0;
        end else if (sample_tick) begin
            rx_phase <= rx_phase - 4'd1;
            case (rx_state)
                RX_IDLE: if (!rx_sync) begin
                    rx_phase <= 4'd7;
                    rx_state <= RX_START;
                end
                RX_START: if (rx_phase == 0) begin
                    if (rx_sync)
                        rx_state <= RX_IDLE;
                    else begin
                        rx_bit <= 0;
                        rx_data <= 0;
                        accumulated_parity <= odd_parity;
                        sampled_parity_error <= 0;
                        rx_state <= RX_DATA;
                    end
                end
                RX_DATA: if (rx_phase == 0) begin
                    rx_data[rx_bit] <= rx_sync;
                    accumulated_parity <= accumulated_parity ^ rx_sync;
                    rx_bit <= rx_bit + 3'd1;
                    if (rx_bit == (seven_bits ? 3'd6 : 3'd7))
                        rx_state <= extra_bit ? RX_PARITY : RX_STOP;
                end
                RX_PARITY: if (rx_phase == 0) begin
                    rx_mark <= multiprocessor && rx_sync;
                    sampled_parity_error <= !multiprocessor && rx_sync != accumulated_parity;
                    rx_state <= RX_STOP;
                end
                RX_STOP: if (rx_phase == 0) begin
                    rx_valid <= 1;
                    rx_framing_error <= !rx_sync;
                    rx_parity_error <= sampled_parity_error;
                    rx_state <= RX_IDLE;
                end
                default: rx_state <= RX_IDLE;
            endcase
        end
    end
endmodule
