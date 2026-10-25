// SPDX-License-Identifier: GPL-2.0-or-later
// Snapshot restore for fes.media.spectrum-snapshot 1.0, in front of the tape
// deck on the cassette unit's stream (README, "Snapshots").
//
// When the unit becomes ready, or the machine leaves reset with it ready, the
// first 8 bytes are read: "ZXRS", version 1, a class this model runs (0 48K
// on every model, 1 128K on the paged models, 2 +2A/+3 on the +2A/+3) and two
// zero bytes mark a restore stream. Anything else is a tape: the stream
// rewinds and the deck plays it as before.
//
// A restore stream resets the machine and then serves every CPU memory read,
// at any address, from the following bytes in order. A byte is consumed when
// its read ends, and WAIT holds the read while the media ring is empty.
// Restore ends once the image's last byte has been read, so the next opcode
// fetch comes from real memory. The host writes the stream (FogCast
// internal/spectrumsnapshots): it fills RAM, programs the border, AY and
// paging through OUT and ends with JP to the snapshot's PC.
module spectrum_snapshot #(
    parameter [7:0] MODEL = 8'd0
) (
    input  wire        clk,
    input  wire        reset,
    input  wire        ready,
    input  wire [24:0] size,
    output wire        stream_enable,
    input  wire        stream_valid,
    input  wire [7:0]  stream_data,
    output wire        stream_take,
    output wire        deck_ready,
    input  wire        deck_stream_enable,
    input  wire        deck_stream_take,
    input  wire        mem_read,
    output wire        machine_reset,
    output wire        restoring,
    output wire        restore_wait
);
    localparam [2:0] S_WAIT = 3'd0, S_PROBE = 3'd1, S_GAP = 3'd2, S_TAPE = 3'd3,
                     S_RESET = 3'd4, S_RUN = 3'd5, S_DONE = 3'd6;
    localparam [39:0] MAGIC = {8'h01, "SRXZ"};

    reg [2:0] state = S_WAIT;
    reg [5:0] count = 6'd0;
    reg [24:0] pos = 25'd0;
    reg [63:0] header = 64'd0;
    reg mem_read_q = 1'b0;

    wire probe_take = state == S_PROBE && stream_valid;
    wire [63:0] probed = {stream_data, header[63:8]};
    wire [7:0] machine = probed[47:40];
    wire runs = machine == 8'd0 || machine == 8'd1 && MODEL != 8'd0 || machine == 8'd2 && MODEL == 8'd2;
    wire restore_header = probed[39:0] == MAGIC && runs && probed[63:48] == 16'd0;
    wire read_done = mem_read_q && !mem_read;
    wire run_take = state == S_RUN && read_done && stream_valid;
    assign restoring = state == S_RUN;
    assign restore_wait = restoring && mem_read && !stream_valid;
    assign machine_reset = state == S_RESET;
    assign deck_ready = state == S_TAPE;
    assign stream_enable = state == S_TAPE ? deck_stream_enable :
                           state == S_PROBE || state == S_RESET || state == S_RUN;
    assign stream_take = state == S_TAPE ? deck_stream_take : probe_take || run_take;

    always @(posedge clk) begin
        mem_read_q <= mem_read;
        if (reset || !ready) begin
            state <= S_WAIT;
            count <= 6'd0;
        end else begin
            case (state)
                // The stream stays off for a few clocks, so the media ring
                // empties and the next read starts at byte 0.
                S_WAIT, S_GAP: begin
                    count <= count + 6'd1;
                    if (count == 6'd7) begin
                        count <= 6'd0;
                        pos <= 25'd0;
                        state <= state == S_GAP ? S_TAPE : size < 25'd8 ? S_GAP : S_PROBE;
                    end
                end
                S_PROBE: if (probe_take) begin
                    header <= probed;
                    pos <= pos + 25'd1;
                    if (pos == 25'd7) state <= restore_header && size > 25'd8 ? S_RESET : S_GAP;
                end
                S_RESET: begin
                    count <= count + 6'd1;
                    if (count == 6'd63) state <= S_RUN;
                end
                S_RUN: if (run_take) begin
                    pos <= pos + 25'd1;
                    if (pos + 25'd1 == size) state <= S_DONE;
                end
                default: ;
            endcase
        end
    end
endmodule
