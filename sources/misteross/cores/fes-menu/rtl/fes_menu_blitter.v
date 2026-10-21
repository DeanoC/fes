// SPDX-License-Identifier: GPL-2.0-or-later
// Menu blitter: runs a command list from the reserved window into one menu
// slot. Commands are 32 bytes, four little-endian 64-bit words:
//   word 0: op [7:0], colour [63:32] as 0xAARRGGBB
//   word 1: x [15:0], y [31:16], w [47:32], h [63:48], already clipped
//   word 2: source window offset [31:0] of the first pixel, stride [47:32]
//   word 3: reserved, zero
// Ops: 0 END, 1 FILL (replace), 2 FILL_BLEND (colour source-over), 3 COPY
// (texture replace), 4 BLEND (texture source-over, straight alpha). Textures
// are B, G, R, A bytes; slots are B, G, R, X with X written as 0. Blending
// matches gfx.blendOver byte for byte.
//
// Two 64-bit Avalon-MM masters with word addresses (byte address / 8):
// commands and sources are read on the source port; destinations are read
// and written on the destination port. The blitter never writes what the
// source port reads, and one port keeps its transfers in order, so a blend
// always reads what the commands before it wrote.
//
// Each row is read, then computed into the write burst. A command outside
// the frame, a source outside the heap, a list outside the list region, an
// unknown op or a nonzero reserved field faults the blitter before it writes
// anything for that command; it stays faulted until reset. halt stops a run
// at the next row boundary without counting it. idle means nothing is owed
// on either port. completed counts finished runs.
module fes_menu_blitter #(
    parameter [31:0] WINDOW_BASE = 32'h30000000,
    parameter [31:0] SLOT_BYTES = 32'h00400000,
    parameter [31:0] LIST_OFFSET = 32'h00800000,
    parameter [31:0] LIST_BYTES = 32'h00100000,
    parameter [31:0] HEAP_OFFSET = 32'h01000000,
    parameter [31:0] HEAP_BYTES = 32'h04000000
) (
    input wire clk, rst, halt,
    input wire run_valid,
    input wire [31:0] list_offset,
    input wire [15:0] list_count,
    input wire target_slot,
    output wire run_ready, idle,
    output reg [15:0] completed = 16'd0,
    output reg faulted = 1'b0,
    output reg [2:0] fault_code = 3'd0,

    output wire [28:0] s_address,
    output wire [7:0] s_burstcount,
    output wire s_read,
    input wire s_waitrequest,
    input wire [63:0] s_readdata,
    input wire s_readdatavalid,

    output wire [28:0] d_address,
    output wire [7:0] d_burstcount,
    output wire d_read,
    output wire d_write,
    output wire [63:0] d_writedata,
    output wire [7:0] d_byteenable,
    input wire d_waitrequest,
    input wire [63:0] d_readdata,
    input wire d_readdatavalid
);
    localparam [10:0] WIDTH = 11'd1280;
    localparam [9:0] HEIGHT = 10'd720;
    localparam [28:0] ROW_WORDS = 29'd640;
    localparam [2:0] OP_END = 3'd0, OP_FILL_BLEND = 3'd2, OP_COPY = 3'd3, OP_BLEND = 3'd4;
    localparam [2:0] FAULT_LIST = 3'd1, FAULT_OP = 3'd2, FAULT_RECT = 3'd3, FAULT_SOURCE = 3'd4, FAULT_RESERVED = 3'd5;
    localparam [2:0] S_IDLE = 3'd0, S_FETCH = 3'd1, S_DECODE = 3'd2, S_SPAN = 3'd3, S_ROW = 3'd4,
        S_READ = 3'd5, S_WRITE = 3'd6, S_NEXT = 3'd7;

    reg [2:0] state = S_IDLE;
    assign idle = state == S_IDLE;
    assign run_ready = idle && !faulted && !halt;

    // Run.
    reg [31:0] list_byte = 32'd0;
    reg [15:0] commands_left = 16'd0;
    reg slot = 1'b0;
    // Command.
    reg [63:0] word [0:3];
    reg [2:0] op = 3'd0;
    reg [31:0] colour = 32'd0;
    reg [10:0] w = 11'd0;
    reg [9:0] h = 10'd0, row = 10'd0;
    reg [15:0] source_stride = 16'd0;
    reg [31:0] source_byte = 32'd0;
    reg [10:0] beats = 11'd0, source_beats_even = 11'd0, source_beats_odd = 11'd0;
    reg x_odd = 1'b0, last_even = 1'b0;
    // Heap check: stride * (h - 1) by shift and add.
    reg [25:0] span = 26'd0, multiplicand = 26'd0;
    reg [9:0] multiplier = 10'd0;
    // Row.
    reg [28:0] dst_word = 29'd0;
    reg [31:0] src_row_byte = 32'd0;
    reg phase = 1'b0;

    wire uses_source = op == OP_COPY || op == OP_BLEND;
    wire reads_destination = op == OP_FILL_BLEND || op == OP_BLEND;
    // A source on the other half of a beat than its destination is shifted
    // by one pixel; one starting on the odd half runs one beat ahead.
    wire aligned = !uses_source || phase == x_odd;
    wire [1:0] ahead = {1'b0, uses_source & phase & ~x_odd};

    // Readers.
    reg s_start = 1'b0, d_start = 1'b0;
    reg [28:0] s_base = 29'd0;
    reg [10:0] s_total = 11'd0, d_total = 11'd0;
    wire s_done, d_done, s_valid, d_valid;
    wire [9:0] s_index, d_index;
    wire [63:0] s_data, d_data;
    wire [28:0] d_read_address;
    wire [7:0] d_read_burstcount;
    fes_menu_blit_read source_reader (
        .clk(clk), .rst(rst), .start(s_start), .base(s_base), .total(s_total), .done(s_done),
        .data_valid(s_valid), .data_index(s_index), .data(s_data),
        .address(s_address), .burstcount(s_burstcount), .read(s_read),
        .waitrequest(s_waitrequest), .readdata(s_readdata), .readdatavalid(s_readdatavalid)
    );
    fes_menu_blit_read destination_reader (
        .clk(clk), .rst(rst), .start(d_start), .base(dst_word), .total(d_total), .done(d_done),
        .data_valid(d_valid), .data_index(d_index), .data(d_data),
        .address(d_read_address), .burstcount(d_read_burstcount), .read(d_read),
        .waitrequest(d_waitrequest), .readdata(d_readdata), .readdatavalid(d_readdatavalid)
    );
    wire reads_done = s_done && d_done && !s_start && !d_start;

    reg [63:0] src_buf [0:1023];
    reg [63:0] dst_buf [0:1023];
    always @(posedge clk) begin
        if (s_valid && state != S_FETCH) src_buf[s_index] <= s_data;
        if (s_valid && state == S_FETCH) word[s_index[1:0]] <= s_data;
        if (d_valid) dst_buf[d_index] <= d_data;
    end

    // Compute: stage 0 reads source beat t and destination beat t - ahead;
    // stage 1 picks each lane's source; the blend takes two more clocks.
    // Copies take the same path, so every beat reaches the FIFO in order.
    reg [10:0] t = 11'd0, s1_t = 11'd0;
    reg s1_valid = 1'b0, p1_keep = 1'b0, p2_keep = 1'b0;
    reg [63:0] src_q = 64'd0, dst_q = 64'd0, previous = 64'd0, copy1 = 64'd0, copy2 = 64'd0;
    always @(posedge clk) begin
        src_q <= src_buf[t[9:0]];
        dst_q <= dst_buf[t[9:0] - {8'd0, ahead}];
    end
    wire [31:0] lane0_source = !uses_source ? colour : aligned ? src_q[31:0] : previous[63:32];
    wire [31:0] lane1_source = !uses_source ? colour : aligned ? src_q[63:32] : src_q[31:0];
    wire [31:0] blend0, blend1;
    fes_menu_blend b0 (.clk(clk), .source(lane0_source), .destination(dst_q[31:0]), .result(blend0));
    fes_menu_blend b1 (.clk(clk), .source(lane1_source), .destination(dst_q[63:32]), .result(blend1));
    wire [63:0] result = reads_destination ? {blend1, blend0} : {8'd0, copy2[55:32], 8'd0, copy2[23:0]};

    // Write: computed beats queue in a four-entry FIFO whose head is on the
    // bus; a burst pauses while the FIFO refills.
    reg [10:0] k = 11'd0;
    reg in_burst = 1'b0;
    reg [7:0] burst_left = 8'd0;
    reg [28:0] write_address = 29'd0;
    reg [7:0] write_burstcount = 8'd0;
    reg [63:0] fifo [0:3];
    reg [1:0] fifo_head = 2'd0;
    reg [2:0] fifo_count = 3'd0;
    wire [10:0] write_left = beats - k;
    wire [7:0] write_length = write_left > 11'd128 ? 8'd128 : write_left[7:0];
    assign d_write = state == S_WRITE && in_burst && fifo_count != 3'd0;
    assign d_writedata = fifo[fifo_head];
    assign d_byteenable = (k == 11'd0 && x_odd ? 8'hf0 : 8'hff) & (k == beats - 11'd1 && last_even ? 8'h0f : 8'hff);
    assign d_address = state == S_WRITE ? write_address : d_read_address;
    assign d_burstcount = state == S_WRITE ? write_burstcount : d_read_burstcount;
    wire accept = d_write && !d_waitrequest;
    // Every beat in flight may need a FIFO entry: issue only if one stays free.
    wire [3:0] owed = {1'b0, fifo_count} + {3'd0, s1_valid} + {3'd0, p1_keep} + {3'd0, p2_keep} - {3'd0, accept};

    // Decode checks, in byte offsets within the window.
    wire [2:0] decoded_op = word[0][2:0];
    wire [10:0] dx = word[1][10:0], dw = word[1][42:32];
    wire [9:0] dy = word[1][25:16], dh = word[1][57:48];
    wire [31:0] d_source = word[2][31:0];
    wire [15:0] d_stride = word[2][47:32];
    wire op_known = word[0][7:3] == 5'd0 && decoded_op <= OP_BLEND;
    wire rect_ok = word[1][15:11] == 5'd0 && word[1][31:26] == 6'd0 && word[1][47:43] == 5'd0 &&
        word[1][63:58] == 6'd0 && dw != 11'd0 && dh != 10'd0 &&
        {1'b0, dx} + {1'b0, dw} <= {1'b0, WIDTH} && {1'b0, dy} + {1'b0, dh} <= {1'b0, HEIGHT};
    wire source_shape_ok = d_source[1:0] == 2'b00 && d_stride[1:0] == 2'b00 &&
        {5'd0, d_stride} >= {8'd0, dw, 2'b00} && d_source >= HEAP_OFFSET;
    wire reserved_ok = word[0][31:8] == 24'd0 && word[2][63:48] == 16'd0 && word[3] == 64'd0;
    wire [28:0] slot_word = WINDOW_BASE[31:3] + (slot ? SLOT_BYTES[31:3] : 29'd0);
    wire [10:0] last_beat = ({10'd0, dx[0]} + dw - 11'd1) >> 1;
    wire [33:0] source_end = {2'd0, source_byte} + {8'd0, span} + {21'd0, w, 2'b00};
    wire [31:0] list_start = WINDOW_BASE + list_offset;
    wire [31:0] list_next = list_byte + 32'd32;

    task fault;
        input [2:0] code;
        begin faulted <= 1'b1; fault_code <= code; state <= S_IDLE; end
    endtask
    task fetch;
        input [31:0] byte_address;
        begin s_base <= byte_address[31:3]; s_total <= 11'd4; s_start <= 1'b1; state <= S_FETCH; end
    endtask

    always @(posedge clk) begin
        s_start <= 1'b0; d_start <= 1'b0;
        if (rst) begin
            state <= S_IDLE; in_burst <= 1'b0; s1_valid <= 1'b0; p1_keep <= 1'b0; p2_keep <= 1'b0;
            faulted <= 1'b0; fault_code <= 3'd0; completed <= 16'd0;
        end else case (state)
            S_IDLE: if (run_valid && !faulted && !halt) begin
                if (list_offset[4:0] != 5'd0 || list_count == 16'd0 || list_offset < LIST_OFFSET ||
                    {1'b0, list_offset} + {12'd0, list_count, 5'd0} > {1'b0, LIST_OFFSET} + {1'b0, LIST_BYTES})
                    fault(FAULT_LIST);
                else begin
                    list_byte <= list_start; commands_left <= list_count; slot <= target_slot;
                    fetch(list_start);
                end
            end
            S_FETCH: if (reads_done) state <= S_DECODE;
            S_DECODE: begin
                if (!reserved_ok) fault(FAULT_RESERVED);
                else if (!op_known) fault(FAULT_OP);
                else if (decoded_op == OP_END) begin completed <= completed + 16'd1; state <= S_IDLE; end
                else if (!rect_ok) fault(FAULT_RECT);
                else if ((decoded_op == OP_COPY || decoded_op == OP_BLEND) && !source_shape_ok) fault(FAULT_SOURCE);
                else begin
                    op <= decoded_op; colour <= word[0][63:32];
                    w <= dw; h <= dh; row <= 10'd0;
                    source_byte <= d_source; source_stride <= d_stride;
                    src_row_byte <= WINDOW_BASE + d_source;
                    dst_word <= slot_word + {10'd0, dy, 9'd0} + {12'd0, dy, 7'd0} + {19'd0, dx[10:1]};
                    x_odd <= dx[0];
                    // The last pixel x + w - 1 is even, so its beat's upper half is outside.
                    last_even <= dx[0] ^ dw[0];
                    beats <= last_beat + 11'd1;
                    source_beats_even <= ((dw - 11'd1) >> 1) + 11'd1;
                    source_beats_odd <= (dw >> 1) + 11'd1;
                    span <= 26'd0; multiplicand <= {10'd0, d_stride}; multiplier <= dh - 10'd1;
                    state <= decoded_op == OP_COPY || decoded_op == OP_BLEND ? S_SPAN : S_ROW;
                end
            end
            S_SPAN: begin
                // The last source byte must stay inside the heap.
                if (multiplier == 10'd0) begin
                    if (source_end > {2'd0, HEAP_OFFSET} + {2'd0, HEAP_BYTES}) fault(FAULT_SOURCE);
                    else state <= S_ROW;
                end else begin
                    if (multiplier[0]) span <= span + multiplicand;
                    multiplicand <= multiplicand << 1; multiplier <= multiplier >> 1;
                end
            end
            S_ROW: begin
                phase <= src_row_byte[2];
                s_base <= src_row_byte[31:3]; s_total <= src_row_byte[2] ? source_beats_odd : source_beats_even;
                s_start <= uses_source;
                d_total <= beats; d_start <= reads_destination;
                state <= S_READ;
            end
            S_READ: if (reads_done) begin
                t <= 11'd0; s1_valid <= 1'b0; p1_keep <= 1'b0; p2_keep <= 1'b0; k <= 11'd0;
                fifo_head <= 2'd0; fifo_count <= 3'd0; state <= S_WRITE;
            end
            S_WRITE: begin
                if (t != beats + {9'd0, ahead} && owed <= 4'd3) begin
                    s1_t <= t; s1_valid <= 1'b1; t <= t + 11'd1;
                end else s1_valid <= 1'b0;
                if (s1_valid) previous <= src_q;
                p1_keep <= s1_valid && s1_t >= {9'd0, ahead};
                p2_keep <= p1_keep;
                copy1 <= {lane1_source, lane0_source}; copy2 <= copy1;
                if (p2_keep) fifo[fifo_head + fifo_count[1:0]] <= result;
                fifo_count <= fifo_count + {2'd0, p2_keep} - {2'd0, accept};
                if (accept) begin
                    fifo_head <= fifo_head + 2'd1; k <= k + 11'd1;
                    if (burst_left == 8'd1) in_burst <= 1'b0;
                    burst_left <= burst_left - 8'd1;
                    if (k + 11'd1 == beats) state <= S_NEXT;
                end else if (!in_burst && k != beats) begin
                    in_burst <= 1'b1; burst_left <= write_length;
                    write_address <= dst_word + {18'd0, k}; write_burstcount <= write_length;
                end
            end
            S_NEXT: begin
                if (row + 10'd1 != h) begin
                    if (halt) state <= S_IDLE;
                    else begin
                        row <= row + 10'd1; dst_word <= dst_word + ROW_WORDS;
                        src_row_byte <= src_row_byte + {16'd0, source_stride}; state <= S_ROW;
                    end
                end else if (commands_left == 16'd1) begin
                    completed <= completed + 16'd1; state <= S_IDLE;
                end else if (halt) state <= S_IDLE;
                else begin
                    commands_left <= commands_left - 16'd1; list_byte <= list_next;
                    fetch(list_next);
                end
            end
            default: state <= S_IDLE;
        endcase
    end
endmodule
