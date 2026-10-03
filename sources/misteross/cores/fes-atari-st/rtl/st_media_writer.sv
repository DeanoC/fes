// SPDX-License-Identifier: GPL-3.0-or-later
// The GP mailbox holds each little-endian byte pair until this adapter has
// stored it in the big-endian SDRAM buffer. Odd chunk boundaries can split
// one mailbox word over two physical words. CPU Hold never resets this path.
module st_media_writer (
    input wire clk, cold_reset,
    input wire [19:0] source_addr,
    input wire [15:0] source_data,
    input wire [1:0] source_enable,
    output wire source_ready,
    output wire memory_req,
    output wire [19:1] memory_addr,
    output wire [15:0] memory_wdata,
    output wire [1:0] memory_byte_enable,
    input wire memory_ready
);
    typedef enum logic [2:0] { IDLE, FIRST, GAP, SECOND, COMPLETE } state_t;
    state_t state;
    reg [19:1] address;
    reg [15:0] data;
    reg [1:0] enable;
    reg odd;
    wire source_req = |source_enable;
    assign source_ready = state == COMPLETE;
    assign memory_req = state == FIRST || state == SECOND;
    assign memory_addr = state == SECOND ? address + 1'b1 : address;
    assign memory_wdata = state == SECOND ? {data[15:8], 8'd0} :
                          odd ? {8'd0, data[7:0]} : {data[7:0], data[15:8]};
    assign memory_byte_enable = state == SECOND ? {enable[1], 1'b0} :
                                odd ? {1'b0, enable[0]} : {enable[0], enable[1]};
    always @(posedge clk) begin
        if (cold_reset) begin
            state <= IDLE; address <= 0; data <= 0; enable <= 0; odd <= 0;
        end else case (state)
            IDLE: if (source_req) begin
                address <= source_addr[19:1]; odd <= source_addr[0];
                data <= source_data; enable <= source_enable; state <= FIRST;
            end
            FIRST: if (memory_ready) state <= odd && enable[1] ? GAP : COMPLETE;
            GAP: state <= SECOND;
            SECOND: if (memory_ready) state <= COMPLETE;
            COMPLETE: if (!source_req) state <= IDLE;
            default: state <= IDLE;
        endcase
    end
endmodule
