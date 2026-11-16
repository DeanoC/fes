// SPDX-License-Identifier: GPL-3.0-or-later
// Fixed, read-only XRGB window source for the owned embedded read endpoint.
// Completion means delivery of this frame, never drain or memory retirement.
module fes_embedded_window_reader #(
    parameter [31:0] WINDOW_BASE = 32'h94000000,
    parameter [31:0] WINDOW_BYTES = 32'd8388608,
    parameter [31:0] ROW_BYTES = 32'd5120,
    parameter [31:0] STRIDE_BYTES = 32'd5120,
    parameter [31:0] ROWS = 32'd720
)(
    input wire clk, reset_n,
    input wire start,
    output wire start_ready,
    output reg busy,
    output wire complete,
    output reg aborted, fault,
    output reg [31:0] requested_bytes, delivered_bytes,
    output wire reader_valid,
    output wire [31:0] reader_address,
    output wire [7:0] reader_length,
    input wire reader_ready, reader_abort,
    input wire [63:0] reader_generation,
    input wire reader_rvalid,
    input wire [127:0] reader_rdata,
    output wire reader_rready,
    output wire stream_valid,
    output wire [127:0] stream_data,
    output wire stream_first, stream_row_last, stream_last,
    input wire stream_ready
);
    localparam [63:0] FRAME_BYTES = 64'(ROW_BYTES) * 64'(ROWS);
    localparam [63:0] SPAN_BYTES = ROWS == 0 ? 64'd0 :
        (64'(ROWS) - 64'd1) * 64'(STRIDE_BYTES) + 64'(ROW_BYTES);
    localparam CONFIG_VALID = WINDOW_BASE[3:0] == 0 && WINDOW_BYTES != 0 &&
        WINDOW_BYTES <= 32'd16777216 && WINDOW_BYTES[3:0] == 0 &&
        ROW_BYTES >= 16 && ROW_BYTES <= 16384 && ROW_BYTES[3:0] == 0 &&
        STRIDE_BYTES >= ROW_BYTES && STRIDE_BYTES <= 16384 && STRIDE_BYTES[3:0] == 0 &&
        ROWS != 0 && ROWS <= 2160 && SPAN_BYTES <= 64'(WINDOW_BYTES) &&
        64'(WINDOW_BASE) + 64'(WINDOW_BYTES) <= 64'h100000000;

    reg completed;
    reg [63:0] active_generation, retired_generation;
    reg saw_retirement;
    reg [31:0] request_row, request_offset, delivered_offset;
    wire [63:0] address_now = 64'(WINDOW_BASE) +
        64'(request_row) * 64'(STRIDE_BYTES) + 64'(request_offset);
    wire [31:0] row_left = ROW_BYTES - request_offset;
    wire [31:0] page_left = 32'd4096 - {20'd0, address_now[11:0]};
    wire [31:0] burst_bytes = row_left < page_left ? row_left : page_left;
    assign reader_address = address_now[31:0];
    assign reader_length = 8'((burst_bytes >> 4) - 32'd1);
    wire same_owner = reader_generation == active_generation;
    assign reader_valid = reset_n && CONFIG_VALID && busy && same_owner && !fault && !reader_abort &&
        64'(requested_bytes) < FRAME_BYTES;
    // Resetting this source alone cannot adopt an already-enabled endpoint.
    // Observe retirement followed by a new admitted endpoint generation.
    // Normal full frames can then repeat within that owned generation.
    assign start_ready = reset_n && CONFIG_VALID && !busy && !fault && !reader_abort &&
        saw_retirement && reader_generation > retired_generation;
    assign reader_rready = reset_n && busy && same_owner && !fault && !reader_abort && stream_ready;
    assign stream_valid = reset_n && busy && same_owner && !fault && !reader_abort && reader_rvalid;
    assign stream_data = reader_rdata;
    assign stream_first = stream_valid && delivered_bytes == 0;
    assign stream_row_last = stream_valid && delivered_offset == ROW_BYTES - 16;
    assign stream_last = stream_valid && 64'(delivered_bytes) == FRAME_BYTES - 64'd16;
    assign complete = reset_n && completed && same_owner && !reader_abort && !fault;

    always @(posedge clk) begin
        if (!reset_n) begin
            busy <= 0; completed <= 0; aborted <= 0; fault <= !CONFIG_VALID;
            requested_bytes <= 0; delivered_bytes <= 0;
            request_row <= 0; request_offset <= 0; delivered_offset <= 0;
            active_generation <= 0;
            retired_generation <= 0; saw_retirement <= 0;
        end else begin
            completed <= 0;
            if (reader_generation < active_generation) begin
                // A reset of only the endpoint loses its outstanding ledger.
                // That is not a reader drain or an owned generation change.
                fault <= 1; aborted <= 1; busy <= 0;
            end else if (reader_abort) begin
                // The endpoint retains any AXI address already offered and
                // drains its responses. This source only retires delivery.
                if (busy) aborted <= 1;
                busy <= 0;
                saw_retirement <= 1;
                retired_generation <= reader_generation;
            end else if (!fault) begin
                if (start && start_ready) begin
                    busy <= 1; aborted <= 0;
                    active_generation <= reader_generation;
                    requested_bytes <= 0; delivered_bytes <= 0;
                    request_row <= 0; request_offset <= 0; delivered_offset <= 0;
                end
                if (reader_valid && reader_ready) begin
                    requested_bytes <= requested_bytes + burst_bytes;
                    if (request_offset + burst_bytes == ROW_BYTES) begin
                        request_row <= request_row + 1'b1;
                        request_offset <= 0;
                    end else request_offset <= request_offset + burst_bytes;
                end
                if ((busy && !same_owner) ||
                    (reader_rvalid && (!busy || delivered_bytes >= requested_bytes))) begin
                    // No other producer may feed this source's owned reader.
                    // Do not turn an unsolicited or excess beat into a frame.
                    fault <= 1; busy <= 0;
                end else if (stream_valid && stream_ready) begin
                    delivered_bytes <= delivered_bytes + 32'd16;
                    delivered_offset <= stream_row_last ? 32'd0 : delivered_offset + 32'd16;
                    if (stream_last) begin
                        if (64'(requested_bytes) != FRAME_BYTES) fault <= 1;
                        else completed <= 1;
                        busy <= 0;
                    end
                end
            end
        end
    end
endmodule
