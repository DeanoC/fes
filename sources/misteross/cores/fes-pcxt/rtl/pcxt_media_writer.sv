// SPDX-License-Identifier: GPL-2.0-or-later
// Complete each held little-endian mailbox word only after both physical byte
// writes retire. A chunk may end on an odd byte, so the next pair may start
// at an odd address. The writer itself is independent of guest reset.
module pcxt_media_writer (
    input wire clk, cold_reset,
    input wire [18:0] source_addr,
    input wire [15:0] source_data,
    input wire [1:0] source_enable,
    output wire source_ready,
    output wire disk_valid,
    output wire [18:0] disk_address,
    output wire [7:0] disk_data,
    input wire disk_ready, disk_done
);
    typedef enum logic [2:0] {IDLE, FIRST, WAIT_FIRST, GAP, SECOND, WAIT_SECOND, COMPLETE} state_t;
    state_t state;
    reg [18:0] address;
    reg [15:0] data;
    reg second_byte;
    assign source_ready = state == COMPLETE;
    assign disk_valid = state == FIRST || state == SECOND;
    assign disk_address = state == SECOND ? address + 19'd1 : address;
    assign disk_data = state == SECOND ? data[15:8] : data[7:0];
    always @(posedge clk) begin
        if (cold_reset) begin
            state <= IDLE; address <= 0; data <= 0; second_byte <= 0;
        end else case (state)
            IDLE: if (source_enable[0]) begin
                address <= source_addr; data <= source_data;
                second_byte <= source_enable[1]; state <= FIRST;
            end
            FIRST: if (disk_ready) state <= WAIT_FIRST;
            WAIT_FIRST: if (disk_done) state <= second_byte ? GAP : COMPLETE;
            GAP: state <= SECOND;
            SECOND: if (disk_ready) state <= WAIT_SECOND;
            WAIT_SECOND: if (disk_done) state <= COMPLETE;
            COMPLETE: if (!(|source_enable)) state <= IDLE;
            default: state <= IDLE;
        endcase
    end
endmodule
