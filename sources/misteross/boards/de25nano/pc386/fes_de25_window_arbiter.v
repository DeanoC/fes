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
// A write burst is complete when its write response returns (Avalon
// writeresponsevalid, one per burst). idle proves nothing is owed: no write
// burst open or unanswered, no read beat outstanding or buffered. Only then
// may the window's grant change.
//
// Commands are granted round robin, one burst at a time; a write burst holds
// the grant until its last beat. Responses return in issue order on the one
// master, so an order FIFO of {port, beats} routes them. All addresses are
// byte addresses (the window adapter's rebased ones). One clock domain.
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
    // 64-bit ports: the 128-bit burst.
    wire [7:0] beats1 = (s1_burstcount + {7'd0, s1_address[3]} + 8'd1) >> 1;
    wire [7:0] beats2 = (s2_burstcount + {7'd0, s2_address[3]} + 8'd1) >> 1;
    // Read credit: a 64-bit port's FIFO must hold every beat it owes.
    reg [9:0] owed1 = 10'd0, owed2 = 10'd0;
    wire [10:0] need1 = {1'b0, owed1} + {3'd0, beats1};
    wire [10:0] need2 = {1'b0, owed2} + {3'd0, beats2};
    wire room1 = need1 <= DEPTH[10:0];
    wire room2 = need2 <= DEPTH[10:0];
    reg [ORDER_BITS:0] order_count = 0;
    wire order_room = order_count != (1 << ORDER_BITS);
    wire [2:0] ready = (want_write | (want_read & {room2, room1, 1'b1} & {3{order_room}}));

    // ---- Grant: round robin, held through a write burst
    reg [1:0] last = 2'd2;
    reg busy = 1'b0;            // a write burst is in progress
    reg [1:0] owner = 2'd0;
    function [1:0] pick(input [2:0] r, input [1:0] after);
        case (after)
            2'd0: pick = r[1] ? 2'd1 : r[2] ? 2'd2 : 2'd0;
            2'd1: pick = r[2] ? 2'd2 : r[0] ? 2'd0 : 2'd1;
            default: pick = r[0] ? 2'd0 : r[1] ? 2'd1 : 2'd2;
        endcase
    endfunction
    wire [1:0] chosen = pick(ready, last);
    // Avalon: a command refused under waitrequest stays presented, unchanged,
    // until it is taken, so the choice is held while it stalls.
    reg stalled = 1'b0;
    reg [1:0] stalled_sel = 2'd0;
    wire [1:0] sel = busy ? owner : stalled ? stalled_sel : chosen;
    wire any = busy || |ready;
    wire sel_read = !busy && want_read[sel] && ready[sel];
    wire sel_write = busy || (want_write[sel] && ready[sel]);

    // ---- Write burst state
    reg [7:0] left = 8'd0;      // upstream beats still to come, after this one
    reg lane = 1'b0;            // the 64-bit lane of the next upstream beat
    reg [63:0] low_data = 64'd0;
    reg [7:0] low_be = 8'd0;
    reg [31:0] w_address = 32'd0;
    reg [7:0] w_beats = 8'd0;
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

    assign m_read = any && sel_read;
    assign m_write = any && emits;
    assign m_address = busy ? w_address : cmd_address;
    assign m_burstcount = busy ? w_beats : cmd_beats;
    assign m_writedata = wide_data;
    assign m_byteenable = wide_be;
    // An upstream beat is taken when it is held, or when its wide beat is.
    wire take = any && (holds || ((sel_read || emits) && !m_waitrequest));
    assign s0_waitrequest = !(take && sel == 2'd0);
    assign s1_waitrequest = !(take && sel == 2'd1);
    assign s2_waitrequest = !(take && sel == 2'd2);

    // ---- Order FIFO: {port, 128-bit beats, first lane, last upper valid}
    localparam integer OW = 2 + 8 + 1 + 1;
    reg [OW-1:0] order [0:(1 << ORDER_BITS)-1];
    reg [ORDER_BITS-1:0] order_wr = 0, order_rd = 0;
    wire push_order = take && sel_read;
    wire [7:0] up_rburst = up_burst;
    wire first_lane = sel == 2'd0 ? 1'b0 : up_address[3];
    wire last_hi = sel == 2'd0 ? 1'b1 : ((up_rburst[0] ^ first_lane) == 1'b0);  // lane0 + N even
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
            busy <= 1'b0; last <= 2'd2; order_wr <= 0; order_rd <= 0; order_count <= 0; resp_beat <= 8'd0;
        end else begin
            if (take) begin
                last <= sel;
                if (sel_write) begin
                    if (first_beat) begin
                        owner <= sel; w_address <= cmd_address; w_beats <= cmd_beats;
                        busy <= !beat_last; left <= up_burst; lane <= ~up_address[3];
                    end else begin
                        busy <= !beat_last; lane <= ~lane;
                    end
                    left <= (first_beat ? up_burst : left) - 8'd1;
                    if (holds) begin low_data <= up_wdata; low_be <= up_be; end
                end
            end
            if (push_order) begin
                order[order_wr] <= {sel, cmd_beats, first_lane, last_hi};
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
    // Credit: owed counts 128-bit beats issued and not yet popped.
    always @(posedge clk) begin
        if (reset) begin
            owed1 <= 10'd0; owed2 <= 10'd0;
        end else begin
            owed1 <= owed1 + ((push_order && sel == 2'd1) ? {2'd0, beats1} : 10'd0) - {9'd0, pop1};
            owed2 <= owed2 + ((push_order && sel == 2'd2) ? {2'd0, beats2} : 10'd0) - {9'd0, pop2};
        end
    end
    // Write bursts accepted in full and not yet answered.
    reg [7:0] writes_owed = 8'd0;
    wire write_done = take && sel_write && beat_last;
    always @(posedge clk)
        if (reset) writes_owed <= 8'd0;
        else writes_owed <= writes_owed + {7'd0, write_done} - {7'd0, m_writeresponsevalid};
    assign idle = !busy && order_count == 0 && owed1 == 10'd0 && owed2 == 10'd0 && writes_owed == 8'd0;
    wire unused = ^{up_address[2:0], up_rburst[7:1]};
endmodule
