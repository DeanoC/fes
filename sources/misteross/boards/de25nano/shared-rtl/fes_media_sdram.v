// SPDX-License-Identifier: GPL-2.0-or-later
// One fes.computer media unit kept in the board SDRAM, through the 16-bit
// single-word port of fes_de25_sdram (byte addresses, one halfword a command).
// The unit occupies 2^AW bytes from BASE. The client side is fes_media_ddr's:
// a core that keeps its media in HPS DDR on the DE10 keeps it here unchanged.
//
// Writes come from the mailbox's held media write (ENABLE_MEDIA_BACKPRESSURE):
// a byte pair at a unit byte address, low byte first. An even address is one
// halfword write; an odd one is a byte write to each of two halfwords.
// write_ready is high for one clock after the last write response, and the
// mailbox acknowledges on that clock. A request is taken once and the store
// rearms only after the mailbox drops it.
//
// The stream reads the unit back in order from byte 0. While stream_enable is
// high the store reads ahead into an eight-byte queue, never past
// stream_bytes: stream_data is valid with stream_valid and stream_take
// consumes it. Dropping stream_enable empties the queue, a read in flight is
// discarded when it returns, and the next enable starts again at byte 0.
// The fetch port reads single bytes anywhere in the unit. A request is held
// with its address until fetch_ready, which is high for every clock the
// address falls in the halfword last read; fetch_data is that byte. Dropping
// a request is harmless and any write discards the halfword. Writes go first,
// then a fetch, then the stream. No FPGA-family primitive belongs here.
module fes_media_sdram #(
    parameter [31:0] BASE = 32'd0,
    parameter integer AW = 24
) (
    input  wire          clk,
    input  wire          reset,
    input  wire [AW-1:0] write_addr,
    input  wire [15:0]   write_data,
    input  wire [1:0]    write_enable,
    // Kept out of a synchronous clear: busy would otherwise clear this
    // register on one polarity and others on the opposite one.
    (* altera_attribute = "-name ALLOW_SYNCH_CTRL_USAGE OFF" *)
    output reg           write_ready = 1'b0,
    input  wire          stream_enable,
    input  wire [AW:0]   stream_bytes,
    output wire          stream_valid,
    output wire [7:0]    stream_data,
    input  wire          stream_take,
    input  wire          fetch,
    input  wire [AW-1:0] fetch_addr,
    output wire          fetch_ready,
    output wire [7:0]    fetch_data,
    output wire [31:0]   address,
    output reg           read = 1'b0,
    output reg           write = 1'b0,
    output reg  [15:0]   writedata = 16'd0,
    output reg  [1:0]    byteenable = 2'b00,
    input  wire          waitrequest,
    input  wire [15:0]   readdata,
    input  wire          readdatavalid,
    input  wire          writeresponsevalid
);
    localparam [1:0] OWNER_WRITE = 2'd0, OWNER_FETCH = 2'd1, OWNER_STREAM = 2'd2;
    reg busy = 1'b0;          // a command is issued or awaiting its response
    reg [1:0] owner = OWNER_WRITE;
    reg [AW-2:0] word = 0;    // halfword of the command
    assign address = BASE + {{(31-AW){1'b0}}, word, 1'b0};

    // ------------------------------------------------------------------ writes
    reg seen = 1'b0, second = 1'b0;
    reg [AW-1:0] w_addr = 0;
    reg [15:0] w_data = 16'd0;
    reg w_pair = 1'b0;
    wire [AW-1:0] w_next = w_addr + 1'b1;

    // ------------------------------------------------------------------- fetch
    reg cache_valid = 1'b0;
    reg [AW-2:0] cache_word = 0;
    reg [15:0] cache_data = 16'd0;
    wire fetch_hit = cache_valid && cache_word == fetch_addr[AW-1:1];
    assign fetch_ready = fetch && fetch_hit;
    assign fetch_data = fetch_addr[0] ? cache_data[15:8] : cache_data[7:0];

    // ------------------------------------------------------------------ stream
    reg [7:0] queue [0:7];
    reg [2:0] head = 3'd0, tail = 3'd0;
    reg [3:0] count = 4'd0;
    reg [AW:0] next_byte = 0;   // next unit byte to read
    reg discard = 1'b0;         // the read in flight belongs to an old stream
    assign stream_valid = stream_enable && count != 4'd0;
    assign stream_data = queue[head];
    wire stream_more = stream_enable && next_byte < stream_bytes && count <= 4'd6;
    wire last_single = stream_bytes - next_byte == 1;
    wire take = stream_valid && stream_take;

    wire start_write = write_enable[0] && !seen;
    wire start_fetch = fetch && !fetch_hit;
    wire accepted = (read || write) && !waitrequest;

    always @(posedge clk) begin
        write_ready <= 1'b0;
        if (!write_enable[0]) seen <= 1'b0;
        if (take) begin
            head <= head + 3'd1;
            count <= count - 4'd1;
        end
        if (!stream_enable) begin
            head <= 3'd0;
            tail <= 3'd0;
            count <= 4'd0;
            next_byte <= 0;
            if (busy && owner == OWNER_STREAM) discard <= 1'b1;
        end
        if (accepted) begin
            read <= 1'b0;
            write <= 1'b0;
        end
        if (!busy) begin
            if (start_write) begin
                seen <= 1'b1;
                busy <= 1'b1;
                owner <= OWNER_WRITE;
                cache_valid <= 1'b0;
                w_addr <= write_addr;
                w_data <= write_data;
                w_pair <= write_enable[1];
                second <= 1'b0;
                word <= write_addr[AW-1:1];
                write <= 1'b1;
                if (write_addr[0]) begin
                    writedata <= {write_data[7:0], 8'd0};
                    byteenable <= 2'b10;
                end else begin
                    writedata <= write_data;
                    byteenable <= {write_enable[1], 1'b1};
                end
            end else if (start_fetch) begin
                busy <= 1'b1;
                owner <= OWNER_FETCH;
                word <= fetch_addr[AW-1:1];
                read <= 1'b1;
            end else if (stream_more) begin
                busy <= 1'b1;
                owner <= OWNER_STREAM;
                discard <= 1'b0;
                word <= next_byte[AW-1:1];
                read <= 1'b1;
            end
        end else if (owner == OWNER_WRITE) begin
            if (writeresponsevalid) begin
                if (w_addr[0] && w_pair && !second) begin
                    // The high byte of an odd pair is the next halfword's low lane.
                    second <= 1'b1;
                    word <= w_next[AW-1:1];
                    writedata <= {8'd0, w_data[15:8]};
                    byteenable <= 2'b01;
                    write <= 1'b1;
                end else begin
                    busy <= 1'b0;
                    write_ready <= 1'b1;
                end
            end
        end else if (readdatavalid) begin
            busy <= 1'b0;
            if (owner == OWNER_FETCH) begin
                cache_valid <= 1'b1;
                cache_word <= word;
                cache_data <= readdata;
            end else if (!discard && stream_enable) begin
                queue[tail] <= readdata[7:0];
                if (last_single) begin
                    tail <= tail + 3'd1;
                    count <= count + 4'd1 - {3'd0, take};
                    next_byte <= next_byte + 1'b1;
                end else begin
                    queue[tail + 3'd1] <= readdata[15:8];
                    tail <= tail + 3'd2;
                    count <= count + 4'd2 - {3'd0, take};
                    next_byte <= next_byte + 2'd2;
                end
            end
        end
        if (reset) begin
            busy <= 1'b0;
            read <= 1'b0;
            write <= 1'b0;
            seen <= 1'b0;
            cache_valid <= 1'b0;
            head <= 3'd0;
            tail <= 3'd0;
            count <= 4'd0;
            next_byte <= 0;
            discard <= 1'b0;
        end
    end
endmodule
