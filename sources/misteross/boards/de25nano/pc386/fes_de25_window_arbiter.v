// SPDX-License-Identifier: GPL-2.0-or-later
// The pc386 window's three ports as one 128-bit Avalon master, so the board
// fabric carries one window master into F2SDRAM.
//
// s0 is 128-bit and passes through. s1 and s2 are 64-bit: a burst of N
// 64-bit beats at byte address B becomes one burst of M 128-bit beats at
// B & ~15, where M = (B[3] + N + 1) / 2 -- the same bytes, two to a beat.
// A write pairs the 64-bit beats into 128-bit ones; the half a 64-bit burst
// does not cover (below its first beat, above its last) goes with no byte
// enabled. A read's 128-bit beats land in the port's response FIFO (M20K)
// with the halves the burst covers marked, and leave it one 64-bit beat a
// clock. Avalon read data cannot be held back, so a 64-bit port issues a
// read only while its FIFO has room for every beat it still owes: the FIFO
// depth bounds that port's outstanding reads.
//
// No burst on the master crosses a 4 KiB boundary (the AXI rule,
// fes.fabric.de25-hps-window). A 128-bit burst that would is issued as two:
// to the boundary, then from it. 128 beats are 2 KiB, so there is one
// boundary at most. A read is taken from its port with its second burst, and
// its credit with its first. A write's beats run on, and the master's burst
// starts again at the boundary.
//
// A write burst is complete when its write response returns (Avalon
// writeresponsevalid, one per burst on the master). idle proves nothing is
// owed: no write burst open or unanswered, no read half issued, no read beat
// outstanding or buffered. Only then may the window's grant change.
//
// Commands are granted round robin, one burst at a time; a write burst holds
// the grant until its last beat. A beat is taken only while its port presents
// it: Avalon lets a master drop write between beats, and fes_hps_ddr_guard
// does for a clock when a hold cuts a burst, before it sends the rest with no
// byte enabled. Responses return in issue order on the one master, so an
// order FIFO of {port, beats} routes them. All addresses are byte addresses
// (the window adapter's rebased ones). One clock domain.
module fes_de25_window_arbiter #(
    parameter integer FIFO_BITS = 7,    // response FIFO depth 2^FIFO_BITS 128-bit beats, per 64-bit port
    parameter integer ORDER_BITS = 5    // outstanding read bursts across the three ports
) (
    input  wire         clk, reset,
    input  wire [31:0]  s0_address,
    input  wire [7:0]   s0_burstcount,
    input  wire         s0_read, s0_write,
    input  wire [127:0] s0_writedata,
    input  wire [15:0]  s0_byteenable,
    output wire         s0_waitrequest,
    output wire [127:0] s0_readdata,
    output wire         s0_readdatavalid,
    input  wire [31:0]  s1_address,
    input  wire [7:0]   s1_burstcount,
    input  wire         s1_read, s1_write,
    input  wire [63:0]  s1_writedata,
    input  wire [7:0]   s1_byteenable,
    output wire         s1_waitrequest,
    output wire [63:0]  s1_readdata,
    output wire         s1_readdatavalid,
    input  wire [31:0]  s2_address,
    input  wire [7:0]   s2_burstcount,
    input  wire         s2_read, s2_write,
    input  wire [63:0]  s2_writedata,
    input  wire [7:0]   s2_byteenable,
    output wire         s2_waitrequest,
    output wire [63:0]  s2_readdata,
    output wire         s2_readdatavalid,
    output wire [31:0]  m_address,
    output wire [7:0]   m_burstcount,
    output wire         m_read, m_write,
    output wire [127:0] m_writedata,
    output wire [15:0]  m_byteenable,
    input  wire         m_waitrequest,
    input  wire [127:0] m_readdata,
    input  wire         m_readdatavalid,
    input  wire         m_writeresponsevalid,
    output wire         idle
);
    localparam integer DEPTH = 1 << FIFO_BITS;

    // ---- Per-port command view
    wire [2:0] want_read = {s2_read, s1_read, s0_read};
    wire [2:0] want_write = {s2_write, s1_write, s0_write};
    // 64-bit ports: the 128-bit burst. The sum takes nine bits: 255 beats, or
    // 254 from the upper lane, reach 256 before the halving gives 128.
    wire [8:0] sum1 = {1'b0, s1_burstcount} + {8'd0, s1_address[3]} + 9'd1;
    wire [8:0] sum2 = {1'b0, s2_burstcount} + {8'd0, s2_address[3]} + 9'd1;
    wire [7:0] beats1 = sum1[8:1];
    wire [7:0] beats2 = sum2[8:1];
    // 4 KiB: a command crosses when its last beat lies past its page, counted
    // in the port's own beats from its own address, beside the credit check.
    wire [8:0] end0 = {1'b0, s0_address[11:4]} + {1'b0, s0_burstcount} - 9'd1;
    wire [9:0] end1 = {1'b0, s1_address[11:3]} + {2'b0, s1_burstcount} - 10'd1;
    wire [9:0] end2 = {1'b0, s2_address[11:3]} + {2'b0, s2_burstcount} - 10'd1;
    wire [2:0] crosses = {end2[9], end1[9], end0[8]};
    // Read credit: a 64-bit port's FIFO must hold every beat it owes.
    reg [9:0] owed1 = 10'd0, owed2 = 10'd0;
    wire [10:0] need1 = {1'b0, owed1} + {3'd0, beats1};
    wire [10:0] need2 = {1'b0, owed2} + {3'd0, beats2};
    wire room1 = need1 <= DEPTH[10:0];
    wire room2 = need2 <= DEPTH[10:0];
    reg [ORDER_BITS:0] order_count = 0;
    wire order_room = order_count != (1 << ORDER_BITS);
    wire [2:0] ready_read = want_read & {room2, room1, 1'b1} & {3{order_room}};
    wire [2:0] ready_write = want_write;
    wire [2:0] ready = ready_write | ready_read;

    // ---- Grant: display-priority within direction + direction batching to avoid
    // excessive R/W flushes in Qsys PREVENT_HAZARDS limiters, held through a
    // write burst and a split read.
    localparam [4:0] READ_BATCH = 5'd16, WRITE_BATCH = 5'd8;
    reg last_dir = 1'b0;        // 0 = read, 1 = write
    reg [4:0] dir_streak = 5'd0;
    reg [3:0] cpu_rd_wait = 4'd0, cpu_wr_wait = 4'd0;
    reg busy = 1'b0;            // a write burst is in progress
    reg split = 1'b0;           // a read's first burst is issued; its second is next
    reg [1:0] owner = 2'd0;     // the port busy or split serves
    wire any_rd = |ready_read;
    wire any_wr = |ready_write;
    // Reads: s1 (VGA VRAM prefetch) > s2 (720p presenter) > s0 (CPU), with starvation guard.
    wire [1:0] rd_pick = (cpu_rd_wait == 4'd15 && ready_read[0]) ? 2'd0 :
                         ready_read[1] ? 2'd1 : ready_read[2] ? 2'd2 : 2'd0;
    // Writes: s2 (VGA capture) > s1 (CPU VRAM) > s0 (CPU RAM), with starvation guard.
    wire [1:0] wr_pick = (cpu_wr_wait == 4'd15 && ready_write[0]) ? 2'd0 :
                         ready_write[2] ? 2'd2 : ready_write[1] ? 2'd1 : 2'd0;
    wire pick_write = !any_rd ? 1'b1 : !any_wr ? 1'b0 :
                      last_dir ? (dir_streak < WRITE_BATCH) : (dir_streak >= READ_BATCH);
    wire [1:0] chosen = pick_write ? wr_pick : rd_pick;
    // Avalon: a command refused under waitrequest stays presented, unchanged,
    // until it is taken, so the choice is held while it stalls.
    reg stalled = 1'b0;
    reg [1:0] stalled_sel = 2'd0;
    wire [1:0] sel = busy || split ? owner : stalled ? stalled_sel : chosen;
    // A write burst's next beat is its owner's, and only while the owner
    // presents it. A split read's second burst needs only an order entry: the
    // first took the port's credit for the whole read.
    wire sel_read = split ? want_read[sel] && order_room : !busy && want_read[sel] && ready[sel];
    wire sel_write = busy ? want_write[sel] : !split && want_write[sel] && ready[sel];

    // ---- Write burst state
    reg [7:0] left = 8'd0;      // upstream beats still to come, after this one
    reg lane = 1'b0;            // the 64-bit lane of the next upstream beat
    reg [63:0] low_data = 64'd0;
    reg [7:0] low_be = 8'd0;
    // The master's burst, presented while busy or split: its address and
    // length, its beats still to send, and the beats after it from the next page.
    reg [31:0] w_address = 32'd0;
    reg [7:0] w_beats = 8'd0, w_left = 8'd0, w_rest = 8'd0;
    wire first_beat = !busy;
    wire [7:0] up_burst = sel == 2'd0 ? s0_burstcount : sel == 2'd1 ? s1_burstcount : s2_burstcount;
    wire [31:0] up_address = sel == 2'd0 ? s0_address : sel == 2'd1 ? s1_address : s2_address;
    wire beat_lane = first_beat ? up_address[3] : lane;
    wire beat_last = first_beat ? up_burst == 8'd1 : left == 8'd1;
    wire [63:0] up_wdata = sel == 2'd1 ? s1_writedata : s2_writedata;
    wire [7:0] up_be = sel == 2'd1 ? s1_byteenable : s2_byteenable;
    // A 64-bit lower-lane beat that is not the burst's last is only held.
    wire holds = sel != 2'd0 && sel_write && beat_lane == 1'b0 && !beat_last;
    wire emits = sel_write && !holds;
    wire [127:0] wide_data = sel == 2'd0 ? s0_writedata :
                             beat_lane ? {up_wdata, low_data} : {64'd0, up_wdata};
    wire [15:0] wide_be = sel == 2'd0 ? s0_byteenable :
                          beat_lane ? {up_be, (first_beat ? 8'd0 : low_be)} : {8'd0, up_be};
    wire [7:0] cmd_beats = sel == 2'd0 ? s0_burstcount : sel == 2'd1 ? beats1 : beats2;
    wire [31:0] cmd_address = sel == 2'd0 ? s0_address : {up_address[31:4], 4'd0};
    // A crossing command's first burst runs to its page's end.
    wire cmd_crosses = crosses[sel];
    wire [7:0] page_beats = cmd_crosses ? 8'd0 - cmd_address[11:4] : cmd_beats;
    wire [7:0] rest_beats = cmd_beats - page_beats;
    // The master's burst this beat belongs to, and the page after it.
    wire [31:0] cur_address = busy ? w_address : cmd_address;
    wire [7:0] cur_beats = busy ? w_beats : page_beats;
    wire [7:0] cur_left = busy ? w_left : page_beats;
    wire [7:0] cur_rest = busy ? w_rest : rest_beats;
    wire burst_end = cur_left == 8'd1;
    wire [31:0] next_page = {cur_address[31:12] + 20'd1, 12'd0};

    assign m_read = sel_read;
    assign m_write = emits;
    assign m_address = busy || split ? w_address : cmd_address;
    assign m_burstcount = busy || split ? w_beats : page_beats;
    assign m_writedata = wide_data;
    assign m_byteenable = wide_be;
    // An upstream beat is taken when it is held, or when its wide beat is; a
    // read, when its last burst is.
    wire read_issued = sel_read && !m_waitrequest;
    wire read_taken = read_issued && (split || !cmd_crosses);
    wire wide_taken = emits && !m_waitrequest;
    wire take = holds || wide_taken || read_taken;
    assign s0_waitrequest = !(take && sel == 2'd0);
    assign s1_waitrequest = !(take && sel == 2'd1);
    assign s2_waitrequest = !(take && sel == 2'd2);

    // ---- Order FIFO: {port, 128-bit beats, first lane, last upper valid}
    localparam integer OW = 2 + 8 + 1 + 1;
    reg [OW-1:0] order [0:(1 << ORDER_BITS)-1];
    reg [ORDER_BITS-1:0] order_wr = 0, order_rd = 0;
    wire push_order = read_issued;
    wire first_lane = sel == 2'd0 ? 1'b0 : up_address[3];
    wire last_hi = sel == 2'd0 ? 1'b1 : ((up_burst[0] ^ first_lane) == 1'b0);  // lane0 + N even
    // A split read's first burst ends at the page's end and its second starts
    // there, each with both halves.
    wire [OW-1:0] order_entry = split ? {sel, w_beats, 1'b0, last_hi} :
                                        {sel, page_beats, first_lane, cmd_crosses || last_hi};
    wire [OW-1:0] head = order[order_rd];
    reg [7:0] resp_beat = 8'd0;   // beats of the head command already returned
    wire [1:0] head_port = head[OW-1:OW-2];
    wire [7:0] head_beats = head[9:2];
    wire head_first = head[1];
    wire head_last_hi = head[0];
    wire resp_last = resp_beat + 8'd1 == head_beats;
    wire pop_order = m_readdatavalid && resp_last;

    always @(posedge clk) begin
        if (reset) stalled <= 1'b0;
        else begin
            stalled <= (m_read || m_write) && m_waitrequest && !busy;
            if ((m_read || m_write) && m_waitrequest && !busy) stalled_sel <= sel;
        end
    end
    always @(posedge clk) begin
        if (reset) begin
            busy <= 1'b0; split <= 1'b0; last_dir <= 1'b0; dir_streak <= 5'd0;
            cpu_rd_wait <= 4'd0; cpu_wr_wait <= 4'd0;
            order_wr <= 0; order_rd <= 0; order_count <= 0;
            resp_beat <= 8'd0;
        end else begin
            if (read_issued && !split) begin
                last_dir <= 1'b0;
                dir_streak <= !last_dir ? (dir_streak == 5'd31 ? 5'd31 : dir_streak + 5'd1) : 5'd1;
                if (sel == 2'd0) cpu_rd_wait <= 4'd0;
                else if (ready_read[0] && cpu_rd_wait != 4'd15) cpu_rd_wait <= cpu_rd_wait + 4'd1;
            end
            if (take && sel_write) begin
                if (first_beat) begin
                    last_dir <= 1'b1;
                    dir_streak <= last_dir ? (dir_streak == 5'd31 ? 5'd31 : dir_streak + 5'd1) : 5'd1;
                    if (sel == 2'd0) cpu_wr_wait <= 4'd0;
                    else if (ready_write[0] && cpu_wr_wait != 4'd15) cpu_wr_wait <= cpu_wr_wait + 4'd1;
                    owner <= sel; lane <= ~up_address[3];
                end else begin
                    lane <= ~lane;
                end
                busy <= !beat_last;
                left <= (first_beat ? up_burst : left) - 8'd1;
                if (holds) begin low_data <= up_wdata; low_be <= up_be; end
                // At a burst's last beat the next, if any, starts at the next page.
                if (wide_taken && burst_end) begin
                    w_address <= next_page; w_beats <= cur_rest; w_left <= cur_rest; w_rest <= 8'd0;
                end else begin
                    w_address <= cur_address; w_beats <= cur_beats; w_rest <= cur_rest;
                    w_left <= cur_left - {7'd0, wide_taken};
                end
            end
            if (read_issued) begin
                split <= !split && cmd_crosses;
                if (!split && cmd_crosses) begin
                    owner <= sel; w_address <= next_page; w_beats <= rest_beats;
                end
            end
            if (push_order) begin
                order[order_wr] <= order_entry;
                order_wr <= order_wr + 1'b1;
            end
            if (m_readdatavalid) resp_beat <= resp_last ? 8'd0 : resp_beat + 8'd1;
            if (pop_order) order_rd <= order_rd + 1'b1;
            order_count <= order_count + {{ORDER_BITS{1'b0}}, push_order} - {{ORDER_BITS{1'b0}}, pop_order};
        end
    end

    // ---- Responses
    assign s0_readdata = m_readdata;
    assign s0_readdatavalid = m_readdatavalid && head_port == 2'd0;
    wire lo_valid = !(resp_beat == 8'd0 && head_first);
    wire hi_valid = !(resp_last && !head_last_hi);
    wire push1 = m_readdatavalid && head_port == 2'd1;
    wire push2 = m_readdatavalid && head_port == 2'd2;
    wire pop1, pop2;
    fes_de25_window_unpack #(.BITS(FIFO_BITS)) unpack1 (
        .clk(clk), .reset(reset), .push(push1), .data(m_readdata), .lo_valid(lo_valid), .hi_valid(hi_valid),
        .readdata(s1_readdata), .readdatavalid(s1_readdatavalid), .popped(pop1));
    fes_de25_window_unpack #(.BITS(FIFO_BITS)) unpack2 (
        .clk(clk), .reset(reset), .push(push2), .data(m_readdata), .lo_valid(lo_valid), .hi_valid(hi_valid),
        .readdata(s2_readdata), .readdatavalid(s2_readdatavalid), .popped(pop2));
    // Credit: owed counts 128-bit beats issued and not yet popped. A read
    // takes its whole credit with its first burst.
    wire credit = push_order && !split;
    always @(posedge clk) begin
        if (reset) begin
            owed1 <= 10'd0; owed2 <= 10'd0;
        end else begin
            owed1 <= owed1 + ((credit && sel == 2'd1) ? {2'd0, beats1} : 10'd0) - {9'd0, pop1};
            owed2 <= owed2 + ((credit && sel == 2'd2) ? {2'd0, beats2} : 10'd0) - {9'd0, pop2};
        end
    end
    // The master's write bursts sent in full and not yet answered.
    reg [7:0] writes_owed = 8'd0;
    wire write_done = wide_taken && burst_end;
    always @(posedge clk)
        if (reset) writes_owed <= 8'd0;
        else writes_owed <= writes_owed + {7'd0, write_done} - {7'd0, m_writeresponsevalid};
    assign idle = !busy && !split && order_count == 0 && owed1 == 10'd0 && owed2 == 10'd0 &&
                  writes_owed == 8'd0;
    wire unused = ^{up_address[2:0], sum1[0], sum2[0], end0[7:0], end1[8:0], end2[8:0]};
endmodule
