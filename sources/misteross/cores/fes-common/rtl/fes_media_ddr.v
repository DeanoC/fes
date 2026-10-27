// SPDX-License-Identifier: GPL-2.0-or-later
// One fes.computer media unit kept in the core's fes.memory.hps-ddr window,
// through a 64-bit fes_hps_ddr port (word address = byte address / 8). The
// unit occupies 2^AW bytes from BASE, which the shell places in the window
// clear of the session display's slots.
//
// Writes come from the mailbox's held media write (ENABLE_MEDIA_BACKPRESSURE):
// a byte pair at a unit byte address, low byte first. The store issues it as
// one single-beat write with byte enables, or two when the pair straddles a
// word, and raises write_ready for one clock once the port has taken the
// last beat; the mailbox acknowledges on that clock. A request is latched
// once and the store rearms only after the mailbox drops it.
//
// The stream reads the unit back in order from byte 0. While stream_enable is
// high the store prefetches reads of up to BURST words into a ring of
// RING_WORDS words, never past stream_bytes, and presents one byte at a time:
// stream_data is valid with stream_valid and stream_take consumes it. Bytes
// follow takes at one a clock within a word, with one idle clock per word.
// Dropping stream_enable empties the ring. Reads already issued are discarded
// as they return, and the next enable starts again at byte 0. Writes take
// the port first; a shell writes a unit only while its stream is disabled.
// The fetch port reads single bytes anywhere in the unit. A request is held
// with its address until fetch_ready, which is high for every clock the
// address falls in the word last read; fetch_data is that byte. A miss reads
// the word with one single-beat read. Dropping a request is harmless, and any
// write discards the word. A shell uses either the stream or the fetch port
// on one store, never both.
// reset is the port's reset from fes_hps_ddr: the guard hides responses to
// reads issued before a hold, so all state restarts.
module fes_media_ddr #(
    parameter [31:0] BASE = 32'h31000000,
    parameter integer AW = 24,
    parameter integer BURST = 16,
    parameter integer RING_WORDS = 256
) (
    input  wire          clk,
    input  wire          reset,
    input  wire [AW-1:0] write_addr,
    input  wire [15:0]   write_data,
    input  wire [1:0]    write_enable,
    output wire          write_ready,
    input  wire          stream_enable,
    input  wire [AW:0]   stream_bytes,
    output wire          stream_valid,
    output wire [7:0]    stream_data,
    input  wire          stream_take,
    input  wire          fetch,
    input  wire [AW-1:0] fetch_addr,
    output wire          fetch_ready,
    output wire [7:0]    fetch_data,
    output reg  [28:0]   address = 29'd0,
    output reg  [7:0]    burstcount = 8'd1,
    output reg           read = 1'b0,
    output reg           write = 1'b0,
    output reg  [63:0]   writedata = 64'd0,
    output reg  [7:0]    byteenable = 8'd0,
    input  wire          waitrequest,
    input  wire [63:0]   readdata,
    input  wire          readdatavalid
);
    localparam integer RW = $clog2(RING_WORDS);
    localparam integer WW = AW - 2;   // word counts 0..2^(AW-3)
    localparam [28:0] BASE_WORD = BASE[31:3];

    // One command register. A command leaves on the clock the port takes it.
    wire accept = (read | write) & ~waitrequest;
    wire command_free = ~(read | write) | accept;

    // ------------------------------------------------------------------ writes
    localparam [1:0] W_IDLE = 2'd0, W_FIRST = 2'd1, W_SECOND = 2'd2, W_WAIT = 2'd3;
    reg [1:0] wstate = W_IDLE;
    reg seen = 1'b0;
    reg done = 1'b0;
    reg split = 1'b0;
    reg [AW-4:0] w_word = 0;
    reg [63:0] w_data = 64'd0;
    reg [7:0] w_enable = 8'd0;
    reg [7:0] w_high = 8'd0;
    wire [2:0] lane = write_addr[2:0];
    assign write_ready = done;
    wire write_command = command_free && (wstate == W_FIRST || wstate == W_SECOND);

    always @(posedge clk) begin
        done <= 1'b0;
        if (reset) begin
            wstate <= W_IDLE;
            seen <= 1'b0;
        end else begin
            if (!write_enable[0]) seen <= 1'b0;
            case (wstate)
                W_IDLE: if (write_enable[0] && !seen && !done) begin
                    seen <= 1'b1;
                    w_word <= write_addr[AW-1:3];
                    // Repeat the pair so the low byte sits in every lane of
                    // its parity; the enables pick the lanes.
                    w_data <= lane[0] ? {4{write_data[7:0], write_data[15:8]}}
                                      : {4{write_data[15:8], write_data[7:0]}};
                    w_enable <= (8'd1 << lane) |
                                (write_enable[1] && lane != 3'd7 ? 8'd2 << lane : 8'd0);
                    w_high <= write_data[15:8];
                    split <= write_enable[1] && lane == 3'd7;
                    wstate <= W_FIRST;
                end
                W_FIRST: if (command_free) wstate <= split ? W_SECOND : W_WAIT;
                W_SECOND: if (command_free) wstate <= W_WAIT;
                default: if (accept) begin
                    done <= 1'b1;
                    wstate <= W_IDLE;
                end
            endcase
        end
    end

    // ------------------------------------------------------------------ stream
    (* ramstyle = "M10K" *) reg [63:0] ring [0:RING_WORDS-1];
    reg [63:0] ring_q = 64'd0;
    reg [WW-1:0] fetch_word = 0;   // next word to request
    reg [WW-1:0] fill_word = 0;    // words stored
    reg [AW:0] take_byte = 0;      // bytes consumed
    reg [WW-1:0] ring_word = 0;    // word held in ring_q
    reg ring_ok = 1'b0;
    reg more = 1'b0;               // take_byte < stream_bytes, registered
    reg [8:0] outstanding = 9'd0;  // read words not yet returned
    reg flushing = 1'b0;
    wire [AW:0] rounded = stream_bytes + (AW+1)'(7);
    wire [WW-1:0] words = rounded[AW:3];
    wire [WW-1:0] take_word = take_byte[AW:3];
    wire [WW-1:0] left = words - fetch_word;
    wire [8:0] burst = left < WW'(BURST) ? 9'(left) : 9'(BURST);
    wire room = fetch_word - take_word <= WW'(RING_WORDS - BURST);
    wire read_command = command_free && !write_command && wstate == W_IDLE &&
                        stream_enable && !flushing && left != 0 && room && !fetch;

    // ------------------------------------------------------------------- fetch
    reg [AW-4:0] cache_word = 0;
    reg [63:0] cache_q = 64'd0;
    reg cache_ok = 1'b0;
    reg fetch_waiting = 1'b0;       // a fetch read has not returned
    reg fetch_stale = 1'b0;         // a write landed since it was issued
    reg [AW-4:0] pending_word = 0;
    wire fetch_hit = cache_ok && cache_word == fetch_addr[AW-1:3];
    assign fetch_ready = fetch && fetch_hit;
    assign fetch_data = cache_q[{fetch_addr[2:0], 3'd0} +: 8];
    wire fetch_command = command_free && !write_command && wstate == W_IDLE &&
                         fetch && !fetch_hit && !fetch_waiting;

    always @(posedge clk) begin
        if (reset) begin
            cache_ok <= 1'b0;
            fetch_waiting <= 1'b0;
            fetch_stale <= 1'b0;
        end else begin
            if (wstate == W_FIRST) begin
                cache_ok <= 1'b0;
                fetch_stale <= fetch_waiting;
            end
            if (fetch_command) begin
                fetch_waiting <= 1'b1;
                fetch_stale <= 1'b0;
                pending_word <= fetch_addr[AW-1:3];
            end else if (fetch_waiting && readdatavalid) begin
                fetch_waiting <= 1'b0;
                if (!fetch_stale && wstate != W_FIRST) begin
                    cache_q <= readdata;
                    cache_word <= pending_word;
                    cache_ok <= 1'b1;
                end
            end
        end
    end
    wire store = readdatavalid && stream_enable && !flushing;
    wire [8:0] outstanding_next = outstanding + (read && accept ? {1'b0, burstcount} : 9'd0) -
                                  (readdatavalid ? 9'd1 : 9'd0);

    assign stream_valid = stream_enable && !flushing && ring_ok && ring_word == take_word && more;
    // The end-of-stream compare runs on the position take_byte holds after
    // this clock. Both outcomes come from take_byte alone and the take only
    // selects between them, so the consumer's take decision does not lead a
    // carry chain.
    wire more_kept = take_byte < stream_bytes;
    wire more_taken = take_byte + (AW+1)'(1) < stream_bytes;
    assign stream_data = ring_q[{take_byte[2:0], 3'd0} +: 8];

    always @(posedge clk) begin
        if (store) ring[fill_word[RW-1:0]] <= readdata;
        ring_q <= ring[take_word[RW-1:0]];
    end

    always @(posedge clk) begin
        if (reset) begin
            fetch_word <= 0;
            fill_word <= 0;
            take_byte <= 0;
            ring_ok <= 1'b0;
            more <= 1'b0;
            outstanding <= 9'd0;
            flushing <= 1'b0;
        end else begin
            more <= !stream_enable ? stream_bytes != 0 :
                    stream_take && stream_valid ? more_taken : more_kept;
            outstanding <= outstanding_next;
            // ring_q is read from the take position with stores made before
            // this clock, so a word is presented only once it is stored.
            ring_word <= take_word;
            ring_ok <= take_word < fill_word;
            if (!stream_enable) begin
                fetch_word <= 0;
                fill_word <= 0;
                take_byte <= 0;
                ring_ok <= 1'b0;
                flushing <= outstanding_next != 9'd0;
            end else begin
                if (flushing && outstanding_next == 9'd0) flushing <= 1'b0;
                if (read_command) fetch_word <= fetch_word + WW'(burst);
                if (store) fill_word <= fill_word + 1'b1;
                if (stream_take && stream_valid) take_byte <= take_byte + 1'b1;
            end
        end
    end

    // --------------------------------------------------------------- commands
    always @(posedge clk) begin
        if (reset) begin
            read <= 1'b0;
            write <= 1'b0;
        end else if (command_free) begin
            read <= 1'b0;
            write <= 1'b0;
            if (write_command) begin
                write <= 1'b1;
                burstcount <= 8'd1;
                byteenable <= wstate == W_SECOND ? 8'd1 : w_enable;
                writedata <= wstate == W_SECOND ? {8{w_high}} : w_data;
                address <= BASE_WORD + 29'(w_word) + (wstate == W_SECOND ? 29'd1 : 29'd0);
            end else if (fetch_command) begin
                read <= 1'b1;
                burstcount <= 8'd1;
                address <= BASE_WORD + 29'(fetch_addr[AW-1:3]);
            end else if (read_command) begin
                read <= 1'b1;
                burstcount <= burst[7:0];
                address <= BASE_WORD + 29'(fetch_word);
            end
        end
    end
endmodule
