// SPDX-License-Identifier: MIT
// L1D (docs/i386-cpu-design.md, "L1D", "Memory types" and "Stores and
// forwarding"): 16 KiB, 4-way, 32-byte lines, indexed by linear bits 11:5
// and tagged with the physical page, write-through with no write allocate,
// behind fes_i386_cpu's data port. Paging is not built: the physical address
// is the linear one with A20M and the reset alias (fes_i386_pkg::phys).
//
// The data is two banks of dwords, even and odd, so the port's eight bytes
// (the address's dword and the next) are one read of each. M1 registers a
// load's index into the M10K address registers (rd, on the edge M1
// advances) with its physical page and attribute; M2 compares the four tags
// and selects the hitting way. That is the fast path: a load of cacheable
// memory (RAM or read-only RAM) that hits and stays inside its line.
//
// Everything else takes the slow path, with D to WB frozen (valid low):
//   - a miss reads the line from the bus interface (fes_i386_biu), once no
//     write-buffer entry holds bytes of it, returns the load's bytes, and
//     fills the line: the victim (an invalid way, else a round robin's) is
//     invalidated, its rows written, then the tags, then the valid bit;
//   - an access that runs into the next line reads each part on its own: a
//     line from the bus interface (not filled), an MMIO dword, or FFh;
//   - uncached MMIO is read as dwords with the access's byte enables, once
//     the write buffer is empty, and only for the oldest uop (oldest: none
//     in EX or WB, and no interrupt being taken), so never on a wrong path.
//     An uncached load that is not the oldest takes no data at all: replay
//     marks it, and WB restarts its instruction (fes_i386_cpu), which then
//     runs alone;
//   - a hole reads FFh.
// A load that faulted in M1, or that M1 did not send (rd low), touches
// nothing.
//
// A store's line is looked up as it enters WB in a probe copy of the tags.
// WB's commit writes the bytes into a way that holds the line (st_commit,
// RAM only: a store to read-only RAM is dropped) through the data arrays'
// write port, the edge it retires; lanes running into the next line clear
// that line's set. A fill happens only with D to WB frozen, so no store
// commits while one runs; when a fill ends, the probe re-reads the tags for
// the store waiting in WB. DMA writes and FLUSH clear valid bits, and spoil
// a fill of their line under way (its valid bit is not set).
module fes_i386_l1d import fes_i386_pkg::*; (
    input  logic             clk,
    input  logic             reset,
    input  logic             a20m,
    input  logic [23:0][1:0] attr_upper,
    input  logic [6:0]       ram_top,
    input  logic             advance,   // D to WB move on this edge
    input  logic             flush,     // ... and WB flushes everything younger
    // M1: the load sent to the port on this edge.
    input  logic             rd,
    input  logic [31:0]      addr,
    input  logic [3:0]       bytes,
    input  logic             fault,
    // M2.
    input  logic             oldest,
    output logic             valid,
    output logic [7:0][7:0]  rdata,
    output logic             replay,
    // Stores: the one entering WB (its address on the edge D to WB advance),
    // the one in WB (for the probe's re-read), and the one WB commits now.
    input  logic [31:0]      st_ex_addr,
    input  logic [31:0]      st_wb_addr,
    input  logic             st_commit,
    input  logic [PA-6:0]    st_line0,
    input  logic [PA-6:0]    st_line1,
    input  attr_e            st_attr0,
    input  logic             st_cross,
    input  logic [2:0]       st_dword,
    input  logic [7:0][7:0]  st_data,
    input  logic [7:0]       st_mask,
    // Invalidation by the bus interface.
    input  logic             inv_valid,
    input  logic [PA-6:0]    inv_line,
    input  logic             inv_all,
    // Reads.
    output logic             d_req,
    output logic             d_mmio,
    output logic [PA-3:0]    d_addr,
    output logic [3:0]       d_be,
    input  logic             d_beat,
    input  logic [127:0]     bdata
);
    localparam int TW = PA - 12;

    // ---- M1 to M2 ----------------------------------------------------------------
    logic          q_rd, q_fault, q_two, q_cross;
    logic [11:0]   q_addr;
    logic [7:0]    q_be;         // the access's lanes of the port's eight bytes
    logic [TW-1:0] q_page;
    logic [PA-6:0] q_line0, q_line1;
    attr_e         q_at0, q_at1;
    logic [PA:0]   p0, p1;
    logic [7:0]    be8;
    assign p0 = phys(addr, a20m);
    assign p1 = phys({addr[31:5] + 27'd1, 5'd0}, a20m);
    assign be8 = 8'(((8'd1 << bytes) - 8'd1) << addr[1:0]);
    always_ff @(posedge clk) begin
        if (reset) begin
            q_rd <= 1'b0;
        end else if (advance) begin
            q_rd <= rd && !flush;
            q_fault <= fault;
            q_addr <= addr[11:0];
            q_be <= be8;
            q_two <= be8[7:4] != 4'd0;
            q_cross <= be8[7:4] != 4'd0 && addr[4:2] == 3'd7;
            q_page <= p0[PA-1:12];
            q_line0 <= p0[PA-1:5];
            q_line1 <= p1[PA-1:5];
            q_at0 <= attr(p0, attr_upper, ram_top);
            q_at1 <= attr(p1, attr_upper, ram_top);
        end
    end

    // ---- Arrays ------------------------------------------------------------------
    (* ramstyle = "M10K" *) logic [31:0]   ev0 [0:511];
    (* ramstyle = "M10K" *) logic [31:0]   ev1 [0:511];
    (* ramstyle = "M10K" *) logic [31:0]   ev2 [0:511];
    (* ramstyle = "M10K" *) logic [31:0]   ev3 [0:511];
    (* ramstyle = "M10K" *) logic [31:0]   od0 [0:511];
    (* ramstyle = "M10K" *) logic [31:0]   od1 [0:511];
    (* ramstyle = "M10K" *) logic [31:0]   od2 [0:511];
    (* ramstyle = "M10K" *) logic [31:0]   od3 [0:511];
    (* ramstyle = "M10K" *) logic [TW-1:0] tag0 [0:127];
    (* ramstyle = "M10K" *) logic [TW-1:0] tag1 [0:127];
    (* ramstyle = "M10K" *) logic [TW-1:0] tag2 [0:127];
    (* ramstyle = "M10K" *) logic [TW-1:0] tag3 [0:127];
    (* ramstyle = "M10K" *) logic [TW-1:0] probe0 [0:127];
    (* ramstyle = "M10K" *) logic [TW-1:0] probe1 [0:127];
    (* ramstyle = "M10K" *) logic [TW-1:0] probe2 [0:127];
    (* ramstyle = "M10K" *) logic [TW-1:0] probe3 [0:127];
    logic [127:0][3:0] valid_bits;

    // Each bank's write port: a fill's rows, or the committing store's dword.
    logic [3:0]       ev_we, od_we;
    logic [8:0]       ev_wa, od_wa;
    logic [31:0]      ev_wd, od_wd;
    logic [3:0]       ev_be, od_be;
    logic [8:0]       ev_ra, od_ra;
    assign od_ra = addr[11:3];
    assign ev_ra = addr[11:3] + 9'(addr[2]);
    logic [3:0][31:0] ev_q, od_q;
    always_ff @(posedge clk) begin
        for (int b = 0; b < 4; b++) begin
            if (ev_we[0] && ev_be[b]) ev0[ev_wa][8 * b +: 8] <= ev_wd[8 * b +: 8];
            if (ev_we[1] && ev_be[b]) ev1[ev_wa][8 * b +: 8] <= ev_wd[8 * b +: 8];
            if (ev_we[2] && ev_be[b]) ev2[ev_wa][8 * b +: 8] <= ev_wd[8 * b +: 8];
            if (ev_we[3] && ev_be[b]) ev3[ev_wa][8 * b +: 8] <= ev_wd[8 * b +: 8];
            if (od_we[0] && od_be[b]) od0[od_wa][8 * b +: 8] <= od_wd[8 * b +: 8];
            if (od_we[1] && od_be[b]) od1[od_wa][8 * b +: 8] <= od_wd[8 * b +: 8];
            if (od_we[2] && od_be[b]) od2[od_wa][8 * b +: 8] <= od_wd[8 * b +: 8];
            if (od_we[3] && od_be[b]) od3[od_wa][8 * b +: 8] <= od_wd[8 * b +: 8];
        end
        if (rd) begin
            ev_q[0] <= ev0[ev_ra];
            ev_q[1] <= ev1[ev_ra];
            ev_q[2] <= ev2[ev_ra];
            ev_q[3] <= ev3[ev_ra];
            od_q[0] <= od0[od_ra];
            od_q[1] <= od1[od_ra];
            od_q[2] <= od2[od_ra];
            od_q[3] <= od3[od_ra];
        end
    end

    // The tags: the lookup copy read with the data, the probe copy as a store
    // enters WB, or for the store in WB once a fill has ended (refresh).
    logic             wr_tag, refresh;
    logic [1:0]       victim;
    logic [3:0][TW-1:0] tag, probe;
    logic [6:0]       pr_set;
    assign pr_set = refresh ? st_wb_addr[11:5] : st_ex_addr[11:5];
    always_ff @(posedge clk) begin
        if (wr_tag) begin
            case (victim)
                2'd0: begin tag0[q_line0[6:0]] <= q_page; probe0[q_line0[6:0]] <= q_page; end
                2'd1: begin tag1[q_line0[6:0]] <= q_page; probe1[q_line0[6:0]] <= q_page; end
                2'd2: begin tag2[q_line0[6:0]] <= q_page; probe2[q_line0[6:0]] <= q_page; end
                default: begin tag3[q_line0[6:0]] <= q_page; probe3[q_line0[6:0]] <= q_page; end
            endcase
        end
        if (rd) begin
            tag[0] <= tag0[addr[11:5]];
            tag[1] <= tag1[addr[11:5]];
            tag[2] <= tag2[addr[11:5]];
            tag[3] <= tag3[addr[11:5]];
        end
        if (advance || refresh) begin
            probe[0] <= probe0[pr_set];
            probe[1] <= probe1[pr_set];
            probe[2] <= probe2[pr_set];
            probe[3] <= probe3[pr_set];
        end
    end

    // ---- M2: the fast path --------------------------------------------------------
    logic [3:0] vm2, hit;
    assign vm2 = valid_bits[q_addr[11:5]];
    for (genvar w = 0; w < 4; w++) begin : g_hit
        assign hit[w] = vm2[w] && tag[w] == q_page;
    end
    logic [7:0][7:0] waysel;
    always_comb begin
        waysel = '0;
        for (int w = 0; w < 4; w++)
            if (hit[w]) waysel = waysel | (q_addr[2] ? {ev_q[w], od_q[w]} : {od_q[w], ev_q[w]});
    end
    logic cached0, fast, unc;
    assign cached0 = q_at0 == ATTR_RAM || q_at0 == ATTR_ROM;
    assign fast = cached0 && !q_cross && hit != 4'd0;
    assign unc = q_at0 == ATTR_MMIO || q_cross && q_at1 == ATTR_MMIO;

    // ---- The slow path ----------------------------------------------------------
    typedef enum logic [2:0] { L_IDLE, L_PART, L_WAIT, L_FILL, L_TAGS, L_VALID, L_REFRESH, L_DONE } slow_e;
    slow_e          state;
    logic           part;       // the dword being read: the address's (0) or the next (1)
    logic           fill_it;    // the line is filled when read
    logic           beat_n, spoilt;
    logic [1:0]     rr, row;
    logic [7:0][31:0] lbuf;     // the line read
    logic [1:0][31:0] sbuf;     // the port's two dwords
    logic           go;
    assign go = q_rd && !q_fault && !fast && !(unc && !oldest);
    assign replay = q_rd && !q_fault && unc && !oldest && state == L_IDLE;
    assign valid = !q_rd || q_fault || fast || replay || state == L_DONE;
    assign rdata = state == L_DONE ? {sbuf[1], sbuf[0]} : waysel;

    // The current part: its attribute, line and dword in the line.
    attr_e          p_at;
    logic [PA-6:0]  p_line;
    logic [2:0]     p_dword, p_next;
    assign p_next = q_addr[4:2] + 3'd1;
    always_comb begin
        p_at = part && q_cross ? q_at1 : q_at0;
        p_line = part && q_cross ? q_line1 : q_line0;
        p_dword = part ? p_next : q_addr[4:2];
    end

    logic [1:0] free;
    always_comb begin
        free = rr;
        for (int w = 3; w >= 0; w--) if (!vm2[w]) free = 2'(w);
    end

    logic spoil, start, set_valid;
    assign spoil = inv_valid && inv_line == q_line0 || inv_all;
    assign start = state == L_IDLE && q_rd && go;
    assign set_valid = state == L_VALID && !spoilt && !spoil;
    assign wr_tag = state == L_TAGS;
    assign refresh = state == L_REFRESH;
    assign d_req = state == L_WAIT;

    // The part done: on to the next dword, the fill, or the end.
    function automatic slow_e after(input logic p, input logic two, input logic fill);
        after = !p && two ? L_PART : fill ? L_FILL : L_DONE;
    endfunction

    always_ff @(posedge clk) begin
        if (reset) begin
            state <= L_IDLE;
            rr <= 2'd0;
        end else begin
            case (state)
                L_IDLE: if (start) begin
                    state <= L_PART;
                    part <= 1'b0;
                    fill_it <= cached0 && !q_cross;
                    victim <= free;
                    if (cached0 && !q_cross) rr <= rr + 2'd1;
                    spoilt <= 1'b0;
                end
                L_PART: begin
                    beat_n <= 1'b0;
                    if (p_at == ATTR_HOLE) begin
                        sbuf[part] <= 32'hffff_ffff;
                        part <= 1'b1;
                        state <= after(part, q_two, fill_it);
                    end else if (part && !q_cross && p_at != ATTR_MMIO) begin
                        // The next dword of a line already read.
                        state <= after(part, q_two, fill_it);
                    end else begin
                        state <= L_WAIT;
                        d_mmio <= p_at == ATTR_MMIO;
                        d_addr <= {p_line, p_at == ATTR_MMIO ? p_dword : 3'd0};
                        d_be <= part ? q_be[7:4] : q_be[3:0];
                    end
                end
                L_WAIT: if (d_beat) begin
                    beat_n <= 1'b1;
                    if (d_mmio) begin
                        sbuf[part] <= bdata[31:0];
                        part <= 1'b1;
                        state <= after(part, q_two, fill_it);
                    end else if (!beat_n) begin
                        lbuf[3:0] <= bdata;
                    end else begin
                        lbuf[7:4] <= bdata;
                        // The dword, and the next when it is in the same line.
                        sbuf[part] <= p_dword[2] ? bdata[32 * p_dword[1:0] +: 32] : lbuf[p_dword];
                        if (!part && q_two && !q_cross)
                            sbuf[1] <= p_next[2] ? bdata[32 * p_next[1:0] +: 32] : lbuf[p_next];
                        part <= 1'b1;
                        state <= after(part, q_two, fill_it);
                    end
                end
                L_FILL: begin
                    row <= row + 2'd1;
                    if (row == 2'd3) state <= L_TAGS;
                end
                L_TAGS: state <= L_VALID;
                L_VALID: state <= L_REFRESH;
                L_REFRESH: state <= L_DONE;
                default: if (advance) state <= L_IDLE;  // L_DONE: the load leaves M2
            endcase
            if (state == L_IDLE) row <= 2'd0;
            if (state != L_IDLE && spoil) spoilt <= 1'b1;
        end
    end

    // ---- Writes: a fill's rows, or the committing store ---------------------------------
    logic [6:0]  st_set;
    logic [3:0]  vwb, st_hit;
    logic        st_ram;
    assign st_set = st_line0[6:0];
    assign vwb = valid_bits[st_set];
    assign st_ram = st_commit && st_attr0 == ATTR_RAM;
    for (genvar w = 0; w < 4; w++) begin : g_st
        assign st_hit[w] = st_ram && vwb[w] && probe[w] == st_line0[PA-6:7];
    end
    always_comb begin
        if (state == L_FILL) begin
            ev_we = 4'd1 << victim;
            od_we = 4'd1 << victim;
            ev_wa = {q_line0[6:0], row};
            od_wa = {q_line0[6:0], row};
            ev_wd = lbuf[{row, 1'b0}];
            od_wd = lbuf[{row, 1'b1}];
            ev_be = 4'hf;
            od_be = 4'hf;
        end else begin
            // The store's dword st_dword (lanes 0-3) and the next (lanes 4-7),
            // one in each bank; the next is in another line after dword 7.
            ev_we = st_hit;
            od_we = st_hit;
            od_wa = {st_set, st_dword[2:1]};
            ev_wa = {st_set, st_dword[0] ? st_dword[2:1] + 2'd1 : st_dword[2:1]};
            ev_wd = st_dword[0] ? st_data[7:4] : st_data[3:0];
            od_wd = st_dword[0] ? st_data[3:0] : st_data[7:4];
            ev_be = st_dword[0] ? (st_dword == 3'd7 ? 4'd0 : st_mask[7:4]) : st_mask[3:0];
            od_be = st_dword[0] ? st_mask[3:0] : st_mask[7:4];
        end
    end

    // The valid bits: a fill's victim clears as it starts and sets as it ends;
    // DMA, FLUSH and a store into the next line clear.
    for (genvar k = 0; k < 128; k++) begin : g_valid
        for (genvar w = 0; w < 4; w++) begin : g_way
            logic clear;
            assign clear = inv_all || inv_valid && inv_line[6:0] == 7'(k)
                        || st_commit && st_cross && st_line1[6:0] == 7'(k)
                        || start && cached0 && !q_cross && q_line0[6:0] == 7'(k) && free == 2'(w);
            always_ff @(posedge clk) begin
                if (reset) valid_bits[k][w] <= 1'b0;
                else if (set_valid && q_line0[6:0] == 7'(k) && victim == 2'(w)) valid_bits[k][w] <= 1'b1;
                else if (clear) valid_bits[k][w] <= 1'b0;
            end
        end
    end
endmodule
