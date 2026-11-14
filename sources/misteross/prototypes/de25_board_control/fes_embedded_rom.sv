// SPDX-License-Identifier: GPL-3.0-or-later
// One configuration-scoped ROM image. The platform owns source identity,
// memory lifetime, reader drain and the CPU's reset gate. No bus master here.
module fes_embedded_rom #(
    parameter integer ROM_BYTES = 8192
) (
    input wire clk,
    input wire reset_n,
    input wire cpu_held,
    input wire start,
    input wire abort_load,
    input wire [31:0] expected_crc32,
    input wire stream_valid,
    output wire stream_ready,
    input wire [127:0] stream_data,
    input wire stream_last,
    output wire loaded,
    output wire failed,
    output reg [13:0] received,
    output reg [13:0] written,
    output wire [31:0] observed_crc32,
    input wire [12:0] read_address,
    output reg [7:0] read_data
);
    localparam [2:0] IDLE=0, RECEIVE=1, WRITE=2, DONE=3, FAILED=4;
    localparam integer ADDRESS_BITS = $clog2(ROM_BYTES);
    reg [2:0] state;
    reg [127:0] pending;
    reg [3:0] byte_index;
    reg [31:0] crc, expected;
    reg [7:0] memory [0:ROM_BYTES-1];
    assign loaded = state == DONE;
    assign failed = state == FAILED;
    assign stream_ready = state == RECEIVE && cpu_held && !start && !abort_load && reset_n;
    assign observed_crc32 = crc ^ 32'hffffffff;

    function automatic [31:0] crc_byte(input [31:0] before_crc, input [7:0] data);
        reg [31:0] value;
        integer i;
        begin
            value = before_crc ^ {24'd0, data};
            for (i=0; i<8; i=i+1)
                value = (value >> 1) ^ (value[0] ? 32'hedb88320 : 32'd0);
            crc_byte = value;
        end
    endfunction

    initial begin
        if (ROM_BYTES != 4096 && ROM_BYTES != 8192)
            $error("embedded ROM requires an exact 4 KiB or 8 KiB image");
    end

    // The read port has one clock of latency. No partial/failed image is
    // visible, including when a reset, abort or lost Hold arrives on a write.
    always @(posedge clk) begin
        if (reset_n && loaded && !abort_load && !start && {1'b0,read_address} < 14'(ROM_BYTES))
            read_data <= memory[read_address[ADDRESS_BITS-1:0]];
        else
            read_data <= 8'd0;
        if (!reset_n) begin
            state <= IDLE;
            pending <= 0;
            byte_index <= 0;
            received <= 0;
            written <= 0;
            crc <= 32'hffffffff;
            expected <= 0;
        end else if (state == FAILED) begin
            // A new Start never repairs uncertain or rejected contents.
            state <= FAILED;
        end else if (abort_load && (state != IDLE || start)) begin
            state <= FAILED;
        end else if (!cpu_held && state != DONE) begin
            state <= FAILED;
        end else if (start) begin
            if (state != IDLE) state <= FAILED;
            else begin
                expected <= expected_crc32;
                state <= RECEIVE;
            end
        end else if (state == RECEIVE && stream_valid && stream_ready) begin
            // Full-width little-endian beats only. LAST must describe exactly
            // the configured image; reject the whole beat before any write.
            if (received >= 14'(ROM_BYTES) || stream_last != (received + 14'd16 == 14'(ROM_BYTES)))
                state <= FAILED;
            else begin
                pending <= stream_data;
                received <= received + 14'd16;
                byte_index <= 0;
                state <= WRITE;
            end
        end else if (state == WRITE) begin
            if (written >= 14'(ROM_BYTES)) state <= FAILED;
            else begin
                memory[written[ADDRESS_BITS-1:0]] <= pending[7:0];
                pending <= pending >> 8;
                written <= written + 14'd1;
                crc <= crc_byte(crc, pending[7:0]);
                byte_index <= byte_index + 4'd1;
                if (written + 14'd1 == 14'(ROM_BYTES)) begin
                    if ((crc_byte(crc, pending[7:0]) ^ 32'hffffffff) == expected)
                        state <= DONE;
                    else state <= FAILED;
                end else if (byte_index == 4'd15) state <= RECEIVE;
            end
        end
    end
endmodule
