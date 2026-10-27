// SPDX-License-Identifier: GPL-2.0-or-later
// ZX Spectrum tape deck for fes.media.spectrum-tape 2.0: plays a PZX 1.0
// image into the EAR bit (format: http://zxds.raxoft.cz/docs/pzx.txt).
//
// The image arrives in order as a byte stream from the media store. PULS,
// DATA, PAUS and STOP blocks play; PZXT, BRWS and any other block are
// skipped. Durations are T states of this machine, as PZX allows for the
// 128K. A pulse holds the level for its duration and then toggles it; a
// zero-length pulse toggles at once. PULS starts low; DATA and PAUS start at
// the level in bit 31 of their first word. STOP stops the tape (flag 1 only on
// the 48K). The end of the image, or a block running past it, stops the tape
// and rewinds it to byte 0, as libspectrum does. Ejecting the tape drops EAR.
//
// The decoder runs ahead of the pulse generator through a four-event queue.
// It folds block levels and zero-length pulses into the next pulse, STOP or
// end event, so the generator applies each event in one step on the T state
// that ends the previous pulse: every edge lands on a T-state edge and EAR
// never shows an intermediate level. underruns counts pulse ends that found
// no event queued (simulation).
//
// The tape starts and stops as Fuse 1.6.0's loader detection does. On every
// read of a ULA port the T states since the previous read and the change in
// B are compared. While stopped, 10 successive reads at most 500 T apart, each
// with B one up or one down, start it. While playing, 2 successive reads more
// than 1000 T apart or with B changed by more than one stop it.
module spectrum_tape #(
    parameter [0:0] IS_48K = 1'b0
) (
    input  wire        clk,
    input  wire        reset,
    input  wire        tick,
    input  wire        ready,
    input  wire [24:0] size,
    output wire        stream_enable,
    input  wire        stream_valid,
    input  wire [7:0]  stream_data,
    output wire        stream_take,
    input  wire        ula_read,
    input  wire [7:0]  cpu_b,
    output reg         ear = 1'b0,
    output reg         playing = 1'b0,
    output reg  [15:0] underruns = 16'd0
);
    // Tags as their four bytes arrive into the low end of a shift register.
    localparam [31:0] TAG_PULS = 32'h534c5550;
    localparam [31:0] TAG_DATA = 32'h41544144;
    localparam [31:0] TAG_PAUS = 32'h53554150;
    localparam [31:0] TAG_STOP = 32'h504f5453;

    localparam [1:0] E_PULSE = 2'd0, E_STOP = 2'd1, E_END = 2'd2;

    localparam [4:0] S_REWIND = 5'd0, S_HEAD = 5'd1, S_DISPATCH = 5'd2, S_PULS = 5'd3,
                     S_PULS_DUR = 5'd4, S_PULS_LOW = 5'd5, S_PULS_EMIT = 5'd6,
                     S_DATA_HEAD = 5'd7, S_DATA_SEQ = 5'd8, S_DATA_BYTE = 5'd9,
                     S_DATA_BIT = 5'd10, S_DATA_READ = 5'd11, S_DATA_EMIT = 5'd12,
                     S_DATA_NEXT = 5'd13, S_DATA_TAIL = 5'd14, S_PAUS = 5'd15,
                     S_PAUS_EMIT = 5'd16, S_STOP = 5'd17, S_STOP_EMIT = 5'd18,
                     S_SKIP = 5'd19, S_END = 5'd20, S_IDLE = 5'd21;

    // ------------------------------------------------------------ event queue
    // {kind, set, level, flip, count, duration}. The level a pulse starts at
    // is `level` if set, else the toggle that ended the previous pulse, then
    // inverted if flip (an odd number of zero-length pulses came first).
    reg [50:0] queue [0:3];
    reg [2:0] queue_in = 3'd0, queue_out = 3'd0;
    reg push = 1'b0;                 // push_event enters the queue next clock
    reg [50:0] push_event = 51'd0;
    wire queue_empty = queue_in == queue_out;
    wire queue_room = queue_in - queue_out + {2'd0, push} < 3'd4;
    wire [50:0] head = queue[queue_out[1:0]];
    wire [1:0] head_kind = head[50:49];
    wire head_set = head[48];
    wire head_level = head[47];
    wire head_flip = head[46];
    wire [14:0] head_count = head[45:31];
    wire [30:0] head_duration = head[30:0];

    // ---------------------------------------------------------------- decoder
    reg [4:0] state = S_REWIND;
    reg [1:0] rewind_wait = 2'd0;
    reg [63:0] acc = 64'd0;          // bytes shift in at the top
    reg [3:0] need = 4'd0;           // bytes still to gather for this field
    reg [24:0] pos = 25'd0;          // bytes consumed
    reg [32:0] block_end = 33'd0;
    reg [14:0] count = 15'd1;
    reg [14:0] high = 15'd0;
    reg [30:0] pulse = 31'd0;
    reg [30:0] bits_left = 31'd0;
    reg [15:0] tail = 16'd0;
    reg [7:0] p0 = 8'd0, p1 = 8'd0;
    reg [8:0] seq_i = 9'd0;
    reg [7:0] pulse_i = 8'd0;
    reg [7:0] current = 8'd0;
    reg [2:0] bit_n = 3'd0;
    reg pre_set = 1'b0, pre_level = 1'b0, pre_flip = 1'b0;
    reg rewind = 1'b0;               // the generator reached the end event
    wire stop_detected, detect_start;

    (* ramstyle = "M10K" *) reg [15:0] sequences [0:511];
    reg [15:0] seq_q = 16'd0;

    wire gathering = state == S_HEAD || state == S_PULS || state == S_PULS_DUR ||
                     state == S_PULS_LOW || state == S_DATA_HEAD || state == S_DATA_SEQ ||
                     state == S_DATA_BYTE && bits_left != 31'd0 || state == S_PAUS ||
                     state == S_STOP || state == S_SKIP && {8'd0, pos} < block_end;
    // A PULS entry starts only with two more bytes in the block, and a block
    // header only with eight more in the image.
    wire puls_open = {8'd0, pos} + 33'd2 <= block_end;
    wire puls_closed = state == S_PULS && need == 4'd2 && !puls_open;
    wire image_end = state == S_HEAD && need == 4'd8 && {1'b0, pos} + 26'd8 > {1'b0, size};
    assign stream_take = stream_valid && gathering && need != 4'd0 && !puls_closed && !image_end;
    assign stream_enable = ready && state != S_REWIND;
    wire [63:0] gathered = {stream_data, acc[63:8]};
    wire last = stream_take && need == 4'd1;
    wire [15:0] word = gathered[63:48];
    wire current_bit = current[7];
    wire [7:0] pulses = current_bit ? p1 : p0;
    wire [8:0] seq_total = {1'b0, p0} + {1'b0, p1};

    // A pulse of `length` T states, repeated `times`: zero-length pulses only
    // change the pending level; others wait for queue room.
    wire [30:0] emit_length = state == S_PULS_EMIT ? pulse :
                              state == S_DATA_EMIT ? {15'd0, seq_q} :
                              state == S_DATA_TAIL ? {15'd0, tail} : acc[62:32];
    wire [14:0] emit_times = state == S_PULS_EMIT ? count : 15'd1;
    wire emit_zero = emit_length == 31'd0;
    wire emit_done = emit_zero || queue_room;

    always @(posedge clk) begin
        seq_q <= sequences[{current_bit, pulse_i}];
        if (state == S_DATA_SEQ && last)
            sequences[seq_i < {1'b0, p0} ? {1'b0, seq_i[7:0]} : {1'b1, 8'(seq_i - {1'b0, p0})}] <= word;
    end

    always @(posedge clk) begin
        push <= 1'b0;
        if (reset || !ready || rewind) begin
            state <= S_REWIND;
            rewind_wait <= 2'd0;
            pos <= 25'd0;
            need <= 4'd0;
            pre_set <= 1'b0;
            pre_flip <= 1'b0;
        end else begin
            if (stream_take) begin
                acc <= gathered;
                need <= need - 4'd1;
                pos <= pos + 25'd1;
            end
            if ((state == S_PULS_EMIT || state == S_DATA_EMIT || state == S_PAUS_EMIT ||
                 state == S_DATA_TAIL && tail != 16'd0) && emit_done) begin
                if (emit_zero) pre_flip <= pre_flip ^ emit_times[0];
                else begin
                    push <= 1'b1;
                    push_event <= {E_PULSE, pre_set, pre_level, pre_flip, emit_times, emit_length};
                    pre_set <= 1'b0;
                    pre_flip <= 1'b0;
                end
            end
            case (state)
                S_REWIND: begin
                    // Hold the stream off long enough for the store to drop
                    // its ring and start again at byte 0.
                    rewind_wait <= rewind_wait + 2'd1;
                    if (rewind_wait == 2'd3) begin
                        state <= S_HEAD;
                        need <= 4'd8;
                    end
                end
                S_HEAD: begin
                    if (image_end) state <= S_END;
                    else if (last) begin
                        block_end <= {8'd0, pos} + 33'd1 + {1'b0, gathered[63:32]};
                        state <= S_DISPATCH;
                    end
                end
                S_DISPATCH: begin
                    if (block_end > {8'd0, size}) state <= S_END;
                    else case (acc[31:0])
                        TAG_PULS: begin
                            pre_set <= 1'b1;
                            pre_level <= 1'b0;
                            pre_flip <= 1'b0;
                            state <= S_PULS;
                            need <= 4'd2;
                        end
                        TAG_DATA: begin state <= S_DATA_HEAD; need <= 4'd8; end
                        TAG_PAUS: begin state <= S_PAUS; need <= 4'd4; end
                        TAG_STOP: begin state <= S_STOP; need <= 4'd2; end
                        default: state <= S_SKIP;
                    endcase
                end
                S_PULS: begin
                    if (puls_closed) state <= S_SKIP;
                    else if (last) begin
                        if (word > 16'h8000) begin
                            count <= word[14:0];
                            state <= S_PULS_DUR;
                            need <= 4'd2;
                        end else begin
                            count <= 15'd1;
                            if (word[15]) begin
                                high <= word[14:0];
                                state <= S_PULS_LOW;
                                need <= 4'd2;
                            end else begin
                                pulse <= {15'd0, word};
                                state <= S_PULS_EMIT;
                            end
                        end
                    end
                end
                S_PULS_DUR: if (last) begin
                    if (word[15]) begin
                        high <= word[14:0];
                        state <= S_PULS_LOW;
                        need <= 4'd2;
                    end else begin
                        pulse <= {15'd0, word};
                        state <= S_PULS_EMIT;
                    end
                end
                S_PULS_LOW: if (last) begin
                    pulse <= {high, word};
                    state <= S_PULS_EMIT;
                end
                S_PULS_EMIT: if (emit_done) begin
                    state <= S_PULS;
                    need <= 4'd2;
                end
                S_DATA_HEAD: if (last) begin
                    bits_left <= gathered[30:0];
                    pre_set <= 1'b1;
                    pre_level <= gathered[31];
                    pre_flip <= 1'b0;
                    tail <= gathered[47:32];
                    p0 <= gathered[55:48];
                    p1 <= gathered[63:56];
                    seq_i <= 9'd0;
                    need <= gathered[63:48] == 16'd0 ? 4'd1 : 4'd2;
                    state <= gathered[63:48] == 16'd0 ? S_DATA_BYTE : S_DATA_SEQ;
                end
                S_DATA_SEQ: if (last) begin
                    seq_i <= seq_i + 9'd1;
                    if (seq_i + 9'd1 == seq_total) begin
                        state <= S_DATA_BYTE;
                        need <= 4'd1;
                    end else need <= 4'd2;
                end
                S_DATA_BYTE: begin
                    if (bits_left == 31'd0) state <= S_DATA_TAIL;
                    else if (last) begin
                        current <= stream_data;
                        bit_n <= 3'd7;
                        state <= S_DATA_BIT;
                    end
                end
                S_DATA_BIT: begin
                    pulse_i <= 8'd0;
                    state <= pulses == 8'd0 ? S_DATA_NEXT : S_DATA_READ;
                end
                // The table entry for this pulse is read on this clock.
                S_DATA_READ: state <= S_DATA_EMIT;
                S_DATA_EMIT: if (emit_done) begin
                    pulse_i <= pulse_i + 8'd1;
                    state <= pulse_i + 8'd1 == pulses ? S_DATA_NEXT : S_DATA_READ;
                end
                S_DATA_NEXT: begin
                    current <= {current[6:0], 1'b0};
                    bits_left <= bits_left - 31'd1;
                    bit_n <= bit_n - 3'd1;
                    if (bits_left == 31'd1) state <= S_DATA_TAIL;
                    else if (bit_n == 3'd0) begin
                        state <= S_DATA_BYTE;
                        need <= 4'd1;
                    end else state <= S_DATA_BIT;
                end
                // A zero tail has no effect; emit_zero would wrongly toggle.
                S_DATA_TAIL: if (tail == 16'd0 || emit_done) state <= S_SKIP;
                S_PAUS: if (last) begin
                    pre_set <= 1'b1;
                    pre_level <= gathered[63];
                    pre_flip <= 1'b0;
                    state <= S_PAUS_EMIT;
                end
                S_PAUS_EMIT: if (emit_done) state <= S_SKIP;
                S_STOP: if (last) state <= word != 16'd1 || IS_48K ? S_STOP_EMIT : S_SKIP;
                S_STOP_EMIT, S_END: if (queue_room) begin
                    push <= 1'b1;
                    push_event <= {state == S_END ? E_END : E_STOP, pre_set, pre_level, pre_flip, 46'd0};
                    pre_set <= 1'b0;
                    pre_flip <= 1'b0;
                    state <= state == S_END ? S_IDLE : S_SKIP;
                end
                S_SKIP: begin
                    if ({8'd0, pos} >= block_end) begin
                        state <= S_HEAD;
                        need <= 4'd8;
                    end else if (need == 4'd0) need <= 4'd1;
                end
                default: ;
            endcase
        end
    end

    // -------------------------------------------------------- pulse generator
    reg active = 1'b0;
    reg pending = 1'b0;              // a pulse ended with no event to follow
    reg [30:0] remaining = 31'd0;
    reg [30:0] duration = 31'd0;
    reg [14:0] repeats = 15'd0;
    wire start = detect_start && !rewind;
    wire boundary = playing && active && tick && remaining == 31'd1 && repeats == 15'd0;
    wire pop = playing && !queue_empty && (boundary || !active);
    wire next_level = (head_set ? head_level : boundary || pending ? ~ear : ear) ^ head_flip;

    always @(posedge clk) begin
        if (push) queue[queue_in[1:0]] <= push_event;
    end

    always @(posedge clk) begin
        rewind <= 1'b0;
        if (reset || !ready || rewind) begin
            queue_in <= 3'd0;
            queue_out <= 3'd0;
            active <= 1'b0;
            pending <= 1'b0;
            playing <= 1'b0;
            if (reset || !ready) ear <= 1'b0;
        end else begin
            if (push) queue_in <= queue_in + 3'd1;
            if (stop_detected) playing <= 1'b0;
            else if (start) playing <= 1'b1;
            if (playing && active && tick) begin
                if (remaining != 31'd1) remaining <= remaining - 31'd1;
                else if (repeats != 15'd0) begin
                    ear <= ~ear;
                    repeats <= repeats - 15'd1;
                    remaining <= duration;
                end else if (queue_empty) begin
                    active <= 1'b0;
                    pending <= 1'b1;
                    underruns <= underruns + 16'd1;
                end
            end
            if (pop) begin
                queue_out <= queue_out + 3'd1;
                pending <= 1'b0;
                case (head_kind)
                    E_PULSE: begin
                        ear <= next_level;
                        active <= 1'b1;
                        remaining <= head_duration;
                        duration <= head_duration;
                        repeats <= head_count - 15'd1;
                    end
                    // Stop and end both give the last pulse its closing
                    // edge, as libspectrum does, then leave the level.
                    default: begin
                        ear <= next_level;
                        active <= 1'b0;
                        playing <= 1'b0;
                        rewind <= head_kind == E_END;
                    end
                endcase
            end
        end
    end

    // ------------------------------------------------------- loader detection
    // Fuse's loader_detect_loader, counted in this machine's T states.
    reg [9:0] since = 10'h3ff;
    reg [7:0] last_b = 8'h00;
    reg [3:0] successive = 4'd0;
    wire [7:0] b_diff = cpu_b - last_b;
    wire b_step = b_diff == 8'h01 || b_diff == 8'hff;
    wire loader_read = since <= 10'd500 && b_step;
    wire other_read = since > 10'd1000 || !(b_step || b_diff == 8'h00);
    assign stop_detected = ula_read && playing && other_read && successive != 4'd0;
    assign detect_start = ula_read && !playing && loader_read && successive == 4'd9 &&
                          state != S_REWIND;

    always @(posedge clk) begin
        if (reset) begin
            since <= 10'h3ff;
            last_b <= 8'h00;
            successive <= 4'd0;
        end else begin
            if (tick && since != 10'h3ff) since <= since + 10'd1;
            if (ula_read) begin
                since <= 10'd0;
                last_b <= cpu_b;
                if (playing) successive <= other_read && successive == 4'd0 ? 4'd1 : 4'd0;
                else successive <= loader_read && successive != 4'd9 ? successive + 4'd1 : 4'd0;
            end
            // Stopping or rewinding the tape restarts the count, as Fuse's
            // tape_stop does.
            if (pop && head_kind != E_PULSE) successive <= 4'd0;
        end
    end
endmodule
