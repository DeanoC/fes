// SPDX-License-Identifier: MIT
// L1D (docs/i386-cpu-design.md, "L1D", "Memory types" and "Stores and
// forwarding"): 16 KiB, 4-way, 32-byte lines, indexed by linear bits 11:5
// and tagged with the physical page, write-through with no write allocate,
// behind fes_i386_cpu's data port. M1 gives the access's physical pages
// (m1_pg): with paging off phys of the linear address, with paging on the
// DTLB's (fes_i386_cpu, "Paging").
//
// The data is two banks of dwords, even and odd, so the port's eight bytes
// (the address's dword and the next) are one read of each. The lookup tags
// are read a stage ahead, for AG's address on the edge a uop enters M1
// (ag_index); M1 compares them with its physical page and registers the hit
// for M2, so D to WB's freeze never waits on an M10K output. M1 registers a
// load's index into the data arrays' address registers (rd, on the edge M1
// advances) with its physical page and attribute, and M2 selects the
// hitting way. That is the fast path: a load of cacheable memory (RAM or
// read-only RAM) that hits and stays inside its line.
//
// Everything else takes the slow path, with D to WB frozen (valid low):
//   - a miss reads the line from the bus interface (fes_i386_biu), once no
//     write-buffer entry holds bytes of it, returns the load's bytes, and
//     fills the line: the victim (an invalid way, else a round robin's) is
//     invalidated, its rows written, then the tags, then the valid bit;
//   - an access that runs into the next line reads each part on its own: a
//     line from the bus interface (not filled), an MMIO dword, or FFh;
//   - uncached MMIO is read as dwords with the access's byte enables, once
//     the write buffer is empty, and only for the oldest uop (oldest:
//     nothing older can flush it or has a store to make; fes_i386_cpu), so
//     never on a wrong path. An uncached load waits a cycle first (L_UNC),
//     so the freeze does not wait on oldest; then, if it is not the oldest,
//     it takes no data at all: replay marks it (L_REPLAY), and WB restarts
//     its instruction (fes_i386_cpu), which then runs alone;
//   - a hole reads FFh.
// A load that faulted in M1, one that uses nothing it reads (a probe; both
// fault), or a load M1 did not send (rd low), touches nothing.
//
// Paging. With paging on, M1 compares each of the set's four tags with the
// physical page of every DTLB entry at once (tlb_pages), and a way hits when
// the entry the linear page matches (tlb_ma) holds its tag: the compares are
// as deep as the one per way M1 makes with paging off, and an AND-OR follows.
// A uop the DTLB cannot serve (tx: a page it does not hold, a right it does
// not give, a store to a page with D clear, a page a third or fourth one, a
// load that may meet an older store through an alias, or an instruction's
// fetch walk) waits as an uncached load does (L_UNC) and then, as the oldest
// uop, walks each page it uses (the walker below), else replays. The walk
// reads the PDE and the PTE through the bus interface, from memory once no
// write-buffer entry holds their line, faults (#PF) or sets A, and D for a
// write, with a dword store through the write buffer (pw, which clears that
// line's set here and in the L1I), and fills the DTLB or ITLB (tf). Then the
// access reads memory for its bytes, as the slow path does, without a fill
// or forwarding (walked), since nothing older can store; a store, or a
// fetch walk (which replays), is done.
//
// A store's line is looked up as it enters WB in a probe copy of the tags.
// WB's commit writes the bytes into a way that holds the line (st_commit,
// RAM only: a store to read-only RAM is dropped) through the data arrays'
// write port, the edge it retires; lanes running into the next line clear
// that line's set. A fill happens only with D to WB frozen, so no store
// commits while one runs; when a fill ends, the probe re-reads the tags for
// the store waiting in WB, and the lookup copy M1's. DMA writes and FLUSH
// clear valid bits, and spoil a fill of their line under way (its valid bit
// is not set); a clear of a set drops the hit registered for it.
module fes_i386_l1d import fes_i386_pkg::*; (
    input  logic             clk,
    input  logic             reset,
    input  logic             a20m,
    input  logic [23:0][1:0] attr_upper,
    input  logic [6:0]       ram_top,
    input  logic             advance,   // D to WB move on this edge
    input  logic             flush,     // ... and WB flushes everything younger
    input  logic [6:0]       ag_index,  // AG's address, bits 11:5, entering M1 on this edge
    // M1: the load sent to the port on this edge.
    input  logic             rd,
    input  logic [31:0]      addr,
    input  logic [3:0]       bytes,
    input  logic             fault,      // ... it faults, or uses nothing it reads: no access
    // M2.
    input  logic             oldest,
    output logic             valid,
    output logic [7:0][7:0]  rdata,
    output logic             replay,
    output logic             unc_start,  // an uncached load starts to read its device (start_u)
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
    input  logic [127:0]     bdata,
    // Paging (see Paging): CR0.PG and CR3's page directory.
    input  logic             pg,
    input  logic [19:0]      cr3_pd,
    // M1's translation: the pages of the memory operand (A and the next,
    // A1) and of a st2 store (T, T1), and A's attribute and its next line's;
    // with paging on, every DTLB entry's page and A's match, for the hit.
    input  logic [3:0][PA-12:0]  m1_pg,
    input  attr_e                m1_at0,
    input  attr_e                m1_at1,
    input  logic [15:0][PA-12:0] tlb_pages,
    input  logic [15:0]          tlb_ma,
    // M1's uop needs the walker (tx): A's linear address (or a fetch walk's
    // page), T's, the pages it uses (A, A1, T, T1) and writes, whether the
    // access is a user one, and whether it is a fetch walk.
    input  logic             tx,
    input  logic [31:0]      tx_a,
    input  logic [31:0]      tx_t,
    input  logic [3:0]       tx_need,
    input  logic [3:0]       tx_wr,
    input  logic             tx_user,
    input  logic             tx_ifw,
    input  logic             wb_empty,   // the write buffer is empty (fes_i386_biu)
    // M2: its pages, a #PF (with CR2 and the error code), and whether it
    // walked (its bytes are memory's, with nothing forwarded).
    output logic [3:0][PA-12:0] pg_out,
    output logic             pf,
    output logic [31:0]      pf_lin,
    output logic [2:0]       pf_err,
    output logic             walked,
    // The walker's TLB fill (the ITLB's for a fetch walk) and A/D store.
    output logic             tf_valid,
    output logic             tf_itlb,
    output tlb_entry_t       tf_e,
    output logic             pw_valid,
    output logic [PA-1:0]    pw_addr,
    output logic [31:0]      pw_data,
    output attr_e            pw_at
);
    localparam int TW = PA - 12;

    // ---- M1 to M2 ----------------------------------------------------------------
    // p0's page is A's; the next line is in A1 past the page's last line.
    logic          q_rd, q_fault, q_two, q_cross;
    logic [11:0]   q_addr;
    logic [7:0]    q_be;         // the access's lanes of the port's eight bytes
    logic [TW-1:0] q_page;
    logic [PA-6:0] q_line0, q_line1;
    attr_e         q_at0, q_at1;
    logic [TW-1:0] page0;
    logic [PA-6:0] line1;
    logic [7:0]    be8;
    assign page0 = m1_pg[0][TW-1:0];
    assign line1 = addr[11:5] == 7'h7f ? {m1_pg[1][TW-1:0], 7'd0} : {page0, addr[11:5] + 7'd1};
    assign be8 = 8'(((8'd1 << bytes) - 8'd1) << addr[1:0]);
    // The walker's registers, which M2's uop takes on the same edge.
    logic          q_tx, q_user, q_ifw;
    logic [31:0]   q_ta, q_tt;
    logic [3:0]    q_need, q_wr;
    logic [3:0][PA-12:0] q_pg;
    assign pg_out = q_pg;
    assign walked = q_tx;
    // The walker updates the pages and, before the access's read, its lines.
    logic          w_lines;
    logic [3:0]    w_pgw;
    logic [PA-12:0] w_pp;
    logic [PA-6:0] w_line1;
    assign w_line1 = q_addr[11:5] == 7'h7f ? {q_pg[1][TW-1:0], 7'd0} : {q_pg[0][TW-1:0], q_addr[11:5] + 7'd1};
    always_ff @(posedge clk) begin
        if (reset) begin
            q_rd <= 1'b0;
            q_tx <= 1'b0;
        end else if (advance) begin
            q_rd <= rd && !flush;
            q_fault <= fault;
            q_addr <= addr[11:0];
            q_be <= be8;
            q_two <= be8[7:4] != 4'd0;
            q_cross <= be8[7:4] != 4'd0 && addr[4:2] == 3'd7;
            q_page <= page0;
            q_line0 <= {page0, addr[11:5]};
            q_line1 <= line1;
            q_at0 <= m1_at0;
            q_at1 <= m1_at1;
            q_tx <= tx && !flush;
            q_ta <= tx_a;
            q_tt <= tx_t;
            q_need <= tx_need;
            q_wr <= tx_wr;
            q_user <= tx_user;
            q_ifw <= tx_ifw;
            q_pg <= m1_pg;
        end else begin
            for (int k = 0; k < 4; k++) if (w_pgw[k]) q_pg[k] <= w_pp;
            if (w_lines) begin
                q_page <= q_pg[0][TW-1:0];
                q_line0 <= {q_pg[0][TW-1:0], q_addr[11:5]};
                q_line1 <= w_line1;
                q_at0 <= attr({q_pg[0], 12'd0}, attr_upper, ram_top);
                q_at1 <= attr({q_addr[11:5] == 7'h7f ? q_pg[1] : q_pg[0], 12'd0}, attr_upper, ram_top);
            end
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

    // The tags: the lookup copy for AG's address as a uop enters M1, or for
    // M1's once a fill has ended (refresh); the probe copy as a store enters
    // WB, or for the store in WB once a fill has ended. A refresh is never on
    // an edge D to WB advance: valid is low through the fill.
    logic             wr_tag, refresh;
    logic [1:0]       victim;
    logic [3:0][TW-1:0] tag, probe;
    logic [6:0]       lk_set, pr_set;
    assign lk_set = refresh ? addr[11:5] : ag_index;
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
        if (advance || refresh) begin
            tag[0] <= tag0[lk_set];
            tag[1] <= tag1[lk_set];
            tag[2] <= tag2[lk_set];
            tag[3] <= tag3[lk_set];
        end
        if (advance || refresh) begin
            probe[0] <= probe0[pr_set];
            probe[1] <= probe1[pr_set];
            probe[2] <= probe2[pr_set];
            probe[3] <= probe3[pr_set];
        end
    end

    // ---- M1: the tag compare -----------------------------------------------------
    // The hit goes to M2 in a register, and so do the set's valid bits (vm2,
    // for the victim). Valid bits are set only by a fill, with D to WB frozen;
    // a clear of a set on any edge drops what is registered for it, as the
    // valid bits themselves drop.
    // With paging on, way w hits when the entry A matches holds its tag:
    // every entry's page against every tag, then an AND-OR with the match.
    logic [3:0] vm1, hit_m1, hit, vm2;
    logic [3:0][15:0] eqv;
    assign vm1 = valid_bits[addr[11:5]];
    for (genvar w = 0; w < 4; w++) begin : g_hit
        for (genvar i = 0; i < 16; i++) begin : g_ent
            assign eqv[w][i] = tag[w] == tlb_pages[i][TW-1:0];
        end
        assign hit_m1[w] = vm1[w] && (pg ? (tlb_ma & eqv[w]) != 16'd0 : tag[w] == page0);
    end
    logic clr_m1, clr_m2;  // M1's set, or M2's, is cleared on this edge
    assign clr_m1 = inv_all || inv_valid && inv_line[6:0] == addr[11:5] || st_commit && st_cross && st_line1[6:0] == addr[11:5]
                 || pw_valid && pw_addr[11:5] == addr[11:5];
    assign clr_m2 = inv_all || inv_valid && inv_line[6:0] == q_addr[11:5] || st_commit && st_cross && st_line1[6:0] == q_addr[11:5]
                 || pw_valid && pw_addr[11:5] == q_addr[11:5];
    always_ff @(posedge clk) begin
        if (reset) begin
            hit <= 4'd0;
            vm2 <= 4'd0;
        end else if (advance) begin
            hit <= clr_m1 ? 4'd0 : hit_m1;
            vm2 <= clr_m1 ? 4'd0 : vm1;
        end else if (clr_m2) begin
            hit <= 4'd0;
            vm2 <= 4'd0;
        end
    end

    // ---- M2: the fast path --------------------------------------------------------
    logic [7:0][7:0] waysel;
    always_comb begin
        waysel = '0;
        for (int w = 0; w < 4; w++)
            if (hit[w]) waysel = waysel | (q_addr[2] ? {ev_q[w], od_q[w]} : {od_q[w], ev_q[w]});
    end
    typedef enum logic [4:0] {
        L_IDLE, L_UNC, L_PART, L_WAIT, L_FILL, L_TAGS, L_VALID, L_REFRESH, L_DONE, L_REPLAY, L_PF,
        W_NEXT, W_RD, W_WAIT, W_CHK, W_APDE, W_APTE, W_FILL
    } slow_e;
    // Only before the slow path starts: its fill sets a valid bit that the
    // arrays' outputs, read before the fill, do not match.
    logic cached0, fast, unc, idle;
    assign cached0 = q_at0 == ATTR_RAM || q_at0 == ATTR_ROM;
    assign fast = cached0 && !q_cross && hit != 4'd0 && idle && !q_tx;
    assign unc = q_at0 == ATTR_MMIO || q_cross && q_at1 == ATTR_MMIO;

    // ---- The slow path ----------------------------------------------------------
    slow_e          state;
    assign idle = state == L_IDLE;
    logic           part;       // the dword being read: the address's (0) or the next (1)
    logic           fill_it;    // the line is filled when read
    logic           beat_n, spoilt;
    logic [1:0]     rr, row;
    logic [7:0][31:0] lbuf;     // the line read
    logic [1:0][31:0] sbuf;     // the port's two dwords
    logic           go;
    assign go = q_rd && !q_fault && !fast;
    assign replay = state == L_REPLAY;
    assign pf = state == L_PF;
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

    // The slow path starts: at once (start_c), or for an uncached load once
    // it has waited its cycle and is the oldest (start_u). Only start_c can
    // fill.
    // A uop for the walker (q_tx) waits its cycle in L_UNC too, then walks
    // as the oldest (walk_go), or replays.
    logic spoil, start, start_c, start_u, set_valid, walk_go;
    assign spoil = inv_valid && inv_line == q_line0 || inv_all;
    assign start_c = state == L_IDLE && go && !unc && !q_tx;
    assign start_u = state == L_UNC && oldest && !q_tx;
    assign walk_go = state == L_UNC && oldest && q_tx;
    assign start = start_c || start_u;
    assign unc_start = start_u || walk_go;
    assign set_valid = state == L_VALID && !spoilt && !spoil;
    assign wr_tag = state == L_TAGS;
    assign refresh = state == L_REFRESH;
    assign d_req = state == L_WAIT || state == W_WAIT;

    // ---- The walker -----------------------------------------------------------
    // Each page the uop uses (q_need: A, A1, T, T1, lowest first, the order
    // Bochs translates them in), one at a time: the PDE at CR3's page plus
    // bits 31:22 times 4, then the PTE at the PDE's page plus bits 21:12
    // times 4, each a dword read through the bus interface (a hole's reads
    // all ones). A PDE or PTE not present, or a user access to a page the two
    // together do not give to user mode, or a user write to one they do not
    // make writable, is #PF (Bochs's translate_linear_legacy): CR2 the page's
    // address (A's and T's the access's own, A1's and T1's their first
    // byte), the error code P (a protection fault), W/R and U/S. Otherwise A
    // is set in the PDE, then A, and D for a write, in the PTE, where clear
    // (update_access_dirty), each a dword store once the write buffer is
    // empty; and the entry fills the DTLB, or the ITLB for a fetch walk.
    logic [1:0]  wk;           // the page being walked
    logic [3:0]  w_left;       // the pages still to walk
    logic [31:0] w_lin;
    logic        w_wr, w_lvl;  // a write; the PTE (1) or the PDE (0)
    logic [PA:0] w_pa, w_pde_pa, w_pte_pa;
    logic [31:0] w_ent, w_pde, w_pte;
    logic        w_beat;
    attr_e       w_at;
    logic [1:0]  w_k;
    logic [31:0] w_klin;
    always_comb begin
        w_k = w_left[0] ? 2'd0 : w_left[1] ? 2'd1 : w_left[2] ? 2'd2 : 2'd3;
        case (w_k)
            2'd0: w_klin = q_ta;
            2'd1: w_klin = {q_ta[31:12] + 20'd1, 12'd0};
            2'd2: w_klin = q_tt;
            default: w_klin = {q_tt[31:12] + 20'd1, 12'd0};
        endcase
    end
    assign w_at = attr(w_pa, attr_upper, ram_top);
    logic [PA:0] w_ppa;
    assign w_ppa = phys({w_pte[31:12], 12'd0}, a20m);
    assign w_pp = w_ppa[PA:12];
    assign w_pgw = state == W_FILL ? 4'd1 << wk : 4'd0;
    assign w_lines = state == W_NEXT && w_left == 4'd0;
    assign tf_valid = state == W_FILL;
    assign tf_itlb = q_ifw;
    always_comb begin
        tf_e.lp = w_lin[31:12];
        tf_e.pp = w_pp;
        tf_e.at = attr({w_pp, 12'd0}, attr_upper, ram_top);
        tf_e.u = w_pde[2] && w_pte[2];
        tf_e.w = w_pde[1] && w_pte[1];
        tf_e.d = w_pte[6] || w_wr;
    end

    // The part done: on to the next dword, the fill, or the end.
    function automatic slow_e after(input logic p, input logic two, input logic fill);
        after = !p && two ? L_PART : fill ? L_FILL : L_DONE;
    endfunction

    slow_e state_n;
    always_comb begin
        state_n = state;
        case (state)
            L_IDLE, L_UNC:
                if (walk_go) state_n = W_NEXT;
                else if (start) state_n = L_PART;
                else if (state == L_UNC) state_n = L_REPLAY;
                else if (q_tx || go && unc) state_n = L_UNC;
            // The walker; at its end the access reads its bytes (a load), or
            // is done (a store), or replays (a fetch walk).
            W_NEXT: state_n = w_left != 4'd0 ? W_RD : q_ifw ? L_REPLAY : q_rd && !q_fault ? L_PART : L_DONE;
            W_RD: state_n = w_at == ATTR_HOLE ? W_CHK : W_WAIT;
            W_WAIT: if (d_beat && (d_mmio || w_beat)) state_n = W_CHK;
            W_CHK: state_n = !w_ent[0] || w_lvl && q_user && (!(w_pde[2] && w_ent[2]) || w_wr && !(w_pde[1] && w_ent[1]))
                           ? L_PF : w_lvl ? W_APDE : W_RD;
            W_APDE: if (w_pde[5] || wb_empty && !pw_valid) state_n = W_APTE;
            W_APTE: if (w_pte[5] && (w_pte[6] || !w_wr) || wb_empty && !pw_valid) state_n = W_FILL;
            W_FILL: state_n = W_NEXT;
            // A hole, or the next dword of a line already read, is done at
            // once; anything else is read.
            L_PART: state_n = p_at == ATTR_HOLE || part && !q_cross && p_at != ATTR_MMIO
                            ? after(part, q_two, fill_it) : L_WAIT;
            L_WAIT: if (d_beat && (d_mmio || beat_n)) state_n = after(part, q_two, fill_it);
            L_FILL: if (row == 2'd3) state_n = L_TAGS;
            L_TAGS: state_n = L_VALID;
            L_VALID: state_n = L_REFRESH;
            L_REFRESH: state_n = L_DONE;
            default: if (advance) state_n = L_IDLE;  // L_DONE, L_REPLAY or L_PF: the uop leaves M2
        endcase
        if (reset) state_n = L_IDLE;
    end

    // valid's complement, registered from the next cycle's state so D to
    // WB's freeze waits on one register: on an edge D to WB advance, the load
    // M1 sends when it will not hit; otherwise M2's load until its slow path
    // ends or it replays, or once a clear drops its hit.
    logic  stall;
    always_ff @(posedge clk) begin
        if (reset)
            stall <= 1'b0;
        else if (advance)
            stall <= !flush && (tx || rd && !fault && !((m1_at0 == ATTR_RAM || m1_at0 == ATTR_ROM)
                                                        && !(be8[7:4] != 4'd0 && addr[4:2] == 3'd7) && !clr_m1
                                                        && hit_m1 != 4'd0));
        else
            stall <= (q_rd && !q_fault || q_tx) && !(state_n == L_DONE || state_n == L_REPLAY || state_n == L_PF
                                                     || state_n == L_IDLE && cached0 && !q_cross && !clr_m2 && hit != 4'd0
                                                        && !q_tx);
    end
    assign valid = !stall;

    always_ff @(posedge clk) begin
        if (reset) begin
            rr <= 2'd0;
        end else begin
            case (state)
                L_IDLE, L_UNC: if (start) begin
                    part <= 1'b0;
                    fill_it <= start_c && cached0 && !q_cross;
                    victim <= free;
                    if (start_c && cached0 && !q_cross) rr <= rr + 2'd1;
                    spoilt <= 1'b0;
                end
                L_PART: begin
                    beat_n <= 1'b0;
                    if (p_at == ATTR_HOLE) begin
                        sbuf[part] <= 32'hffff_ffff;
                        part <= 1'b1;
                    end else if (!(part && !q_cross && p_at != ATTR_MMIO)) begin
                        // Not the next dword of a line already read: a read.
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
                    end else if (!beat_n) begin
                        lbuf[3:0] <= bdata;
                    end else begin
                        lbuf[7:4] <= bdata;
                        // The dword, and the next when it is in the same line.
                        sbuf[part] <= p_dword[2] ? bdata[32 * p_dword[1:0] +: 32] : lbuf[p_dword];
                        if (!part && q_two && !q_cross)
                            sbuf[1] <= p_next[2] ? bdata[32 * p_next[1:0] +: 32] : lbuf[p_next];
                        part <= 1'b1;
                    end
                end
                L_FILL: row <= row + 2'd1;
                W_NEXT: begin
                    if (w_left != 4'd0) begin
                        wk <= w_k;
                        w_lin <= w_klin;
                        w_wr <= q_wr[w_k];
                        w_lvl <= 1'b0;
                        w_pa <= phys({cr3_pd, w_klin[31:22], 2'b00}, a20m);
                        w_pde_pa <= phys({cr3_pd, w_klin[31:22], 2'b00}, a20m);
                        w_left[w_k] <= 1'b0;
                    end else begin
                        // The access's read, as the slow path's, unfilled.
                        part <= 1'b0;
                        fill_it <= 1'b0;
                        spoilt <= 1'b0;
                    end
                end
                W_RD: begin
                    w_beat <= 1'b0;
                    w_ent <= 32'hffff_ffff;
                    d_mmio <= w_at == ATTR_MMIO;
                    d_addr <= {w_pa[PA-1:5], w_at == ATTR_MMIO ? w_pa[4:2] : 3'd0};
                    d_be <= 4'hf;
                end
                W_WAIT: if (d_beat) begin
                    w_beat <= 1'b1;
                    if (d_mmio) w_ent <= bdata[31:0];
                    else if (w_beat == w_pa[4]) w_ent <= bdata[32 * w_pa[3:2] +: 32];
                end
                W_CHK: begin
                    if (!w_lvl) begin
                        w_pde <= w_ent;
                        w_pa <= phys({w_ent[31:12], w_lin[21:12], 2'b00}, a20m);
                        w_pte_pa <= phys({w_ent[31:12], w_lin[21:12], 2'b00}, a20m);
                    end else begin
                        w_pte <= w_ent;
                    end
                    w_lvl <= 1'b1;
                    pf_lin <= w_lin;
                    pf_err <= {q_user, w_wr, w_ent[0]};
                end
                default: ;
            endcase
            if (walk_go) w_left <= q_need;
            if (state == L_IDLE) row <= 2'd0;
            if (state != L_IDLE && spoil) spoilt <= 1'b1;
        end
    end
    always_ff @(posedge clk) state <= state_n;

    // The A/D stores: one cycle each, once the write buffer is empty.
    always_ff @(posedge clk) begin
        pw_valid <= 1'b0;
        if (!reset && state == W_APDE && !w_pde[5] && wb_empty && !pw_valid) begin
            pw_valid <= 1'b1;
            pw_addr <= w_pde_pa[PA-1:0];
            pw_data <= w_pde | 32'h20;
            pw_at <= attr(w_pde_pa, attr_upper, ram_top);
        end
        if (!reset && state == W_APTE && !(w_pte[5] && (w_pte[6] || !w_wr)) && wb_empty && !pw_valid) begin
            pw_valid <= 1'b1;
            pw_addr <= w_pte_pa[PA-1:0];
            pw_data <= w_pte | (w_wr ? 32'h60 : 32'h20);
            pw_at <= attr(w_pte_pa, attr_upper, ram_top);
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
                        || pw_valid && pw_addr[11:5] == 7'(k)
                        || start_c && cached0 && !q_cross && q_line0[6:0] == 7'(k) && free == 2'(w);
            always_ff @(posedge clk) begin
                if (reset) valid_bits[k][w] <= 1'b0;
                else if (set_valid && q_line0[6:0] == 7'(k) && victim == 2'(w)) valid_bits[k][w] <= 1'b1;
                else if (clear) valid_bits[k][w] <= 1'b0;
            end
        end
    end
endmodule
