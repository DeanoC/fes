// SPDX-License-Identifier: MIT
// L1I (docs/i386-cpu-design.md, "Fetch and L1I"): 16 KiB, 4-way, 32-byte
// lines, indexed by linear bits 11:5 and tagged with the physical page, behind
// the front end's fetch port (fes_i386_fetchbuf). Paging is not built, so
// the physical address is the linear one with A20M and the reset alias
// applied (fes_i386_pkg::phys); the ITLB will replace that.
//
// F1 loads mem_block into the M10K address registers; every array also
// re-reads the block F2 holds on every other edge, so a fill shows up there
// a cycle after it is written. F2 compares the four tags with the block's
// physical page in eight replicated hit sets, one per two bytes, and selects
// the hitting way. Valid bits are flip-flops, read by F2's block, so a reset,
// FLUSH, a DMA write or a store clears them at once. A block in MMIO or a
// hole is not cached: it reads FFh bytes.
//
// A miss fills the line from the bus interface (fes_i386_biu): the victim
// (an invalid way, else the next of a round robin) is invalidated, the two
// beats written as they come, then the tags and, a cycle later so every
// tag read sees the new tag first, the valid bit. A fill finishes only when
// no store is in EX or WB: a store reads its snoop tags as it enters WB, so
// the stores after that see the new tags. A store WB commits into the line
// while it fills (fill_line, from the request on), or DMA writing it, or
// FLUSH, spoils the fill: its valid bit is not set, and the fetch misses
// again. The bus interface sends the read only once no write-buffer entry
// holds bytes of the line.
//
// Self-modifying code: each store reads the snoop copy of the tags for its
// set as it enters WB, and WB's commit clears the valid bit of a way that
// holds its line, on the next edge (the clear is registered, so the 512
// valid bits' enables do not wait on the commit). A store whose lanes run
// into the next line clears that line's set. Instructions already past F2,
// and a fetch in F2 the cycle after the commit, are not checked yet.
module fes_i386_l1i import fes_i386_pkg::*; (
    input  logic             clk,
    input  logic             reset,
    input  logic             a20m,
    input  logic [23:0][1:0] attr_upper,
    input  logic [6:0]       ram_top,
    // The fetch port.
    input  logic [27:0]      mem_block,
    input  logic             mem_load,
    output logic [15:0][7:0] mem_data,
    output logic             mem_valid,
    // Stores: the one entering WB on edges D to WB advance (st_ex_addr, its
    // linear address), and the one WB commits now, its physical line and
    // whether its lanes run into the next line (st_line1).
    input  logic             st_advance,
    input  logic [31:0]      st_ex_addr,
    input  logic             st_ex,      // a store is in EX
    input  logic             st_wb,      // ... or in WB
    input  logic             st_commit,
    input  logic [PA-6:0]    st_line0,
    input  logic [PA-6:0]    st_line1,
    input  logic             st_cross,
    // Invalidation by the bus interface.
    input  logic             inv_valid,
    input  logic [PA-6:0]    inv_line,
    input  logic             inv_all,
    // Fills.
    output logic             i_req,
    output logic [PA-6:0]    i_line,
    input  logic             i_beat,
    input  logic [127:0]     rdata
);
    localparam int TW = PA - 12;  // a tag: the physical page

    // ---- Arrays ------------------------------------------------------------------
    (* ramstyle = "M10K" *) logic [127:0]  data0 [0:255];
    (* ramstyle = "M10K" *) logic [127:0]  data1 [0:255];
    (* ramstyle = "M10K" *) logic [127:0]  data2 [0:255];
    (* ramstyle = "M10K" *) logic [127:0]  data3 [0:255];
    (* ramstyle = "M10K" *) logic [TW-1:0] tag0 [0:127];
    (* ramstyle = "M10K" *) logic [TW-1:0] tag1 [0:127];
    (* ramstyle = "M10K" *) logic [TW-1:0] tag2 [0:127];
    (* ramstyle = "M10K" *) logic [TW-1:0] tag3 [0:127];
    (* ramstyle = "M10K" *) logic [TW-1:0] snoop0 [0:127];
    (* ramstyle = "M10K" *) logic [TW-1:0] snoop1 [0:127];
    (* ramstyle = "M10K" *) logic [TW-1:0] snoop2 [0:127];
    (* ramstyle = "M10K" *) logic [TW-1:0] snoop3 [0:127];
    logic [127:0][3:0] valid;

    // ---- Fill state ----------------------------------------------------------------
    typedef enum logic [1:0] { F_IDLE, F_BEATS, F_TAGS, F_VALID } fill_e;
    fill_e         fill;
    logic [1:0]    victim;
    logic [1:0]    rr;
    logic          beat_n;
    logic          spoilt;
    logic [PA-6:0] f_line;
    logic          wr_data, wr_tag;
    logic [7:0]    wr_index;
    assign wr_data = fill == F_BEATS && i_beat;
    assign wr_index = {f_line[6:0], beat_n};
    assign wr_tag = fill == F_TAGS && !st_ex && !st_wb;
    assign i_req = fill == F_BEATS;
    assign i_line = f_line;

    // ---- F1 and F2 -------------------------------------------------------------------
    // blk: the block F2 holds; req: F2 holds a fetch (from the first load on).
    logic [27:0] blk_next, blk;
    logic        req;
    assign blk_next = mem_load ? mem_block : blk;
    always_ff @(posedge clk) begin
        blk <= blk_next;
        if (reset) req <= 1'b0;
        else if (mem_load) req <= 1'b1;
    end
    // The block's physical page, registered with the arrays' read: the next
    // block's when F1 loads one, else F2's kept. Eight copies, one per hit set
    // (kept apart: DeanoC/yosys#51).
    logic [PA:0]   p_new;
    logic [TW-1:0] page_new;
    attr_e         at_new, at;
    assign p_new = phys({mem_block, 4'd0}, a20m);
    assign page_new = p_new[PA-1:12];
    assign at_new = attr(p_new, attr_upper, ram_top);
    (* keep *) logic [7:0][TW-1:0] page;
    always_ff @(posedge clk) begin
        if (mem_load) begin
            page <= {8{page_new}};
            at <= at_new;
        end
    end

    logic [3:0][127:0]  way;
    logic [3:0][TW-1:0] tag;
    always_ff @(posedge clk) begin
        if (wr_data) begin
            case (victim)
                2'd0: data0[wr_index] <= rdata;
                2'd1: data1[wr_index] <= rdata;
                2'd2: data2[wr_index] <= rdata;
                default: data3[wr_index] <= rdata;
            endcase
        end
        way[0] <= data0[blk_next[7:0]];
        way[1] <= data1[blk_next[7:0]];
        way[2] <= data2[blk_next[7:0]];
        way[3] <= data3[blk_next[7:0]];
    end
    always_ff @(posedge clk) begin
        if (wr_tag) begin
            case (victim)
                2'd0: tag0[f_line[6:0]] <= f_line[PA-6:7];
                2'd1: tag1[f_line[6:0]] <= f_line[PA-6:7];
                2'd2: tag2[f_line[6:0]] <= f_line[PA-6:7];
                default: tag3[f_line[6:0]] <= f_line[PA-6:7];
            endcase
        end
        tag[0] <= tag0[blk_next[7:1]];
        tag[1] <= tag1[blk_next[7:1]];
        tag[2] <= tag2[blk_next[7:1]];
        tag[3] <= tag3[blk_next[7:1]];
    end

    logic [3:0] vf2;  // F2's set's valid bits
    assign vf2 = valid[blk[7:1]];
    logic cached;
    assign cached = at == ATTR_RAM || at == ATTR_ROM;
    logic [7:0][3:0] hit;  // hit[s][w] selects way w for bytes 2s and 2s + 1
    for (genvar s = 0; s < 8; s++) begin : g_set
        for (genvar w = 0; w < 4; w++) begin : g_way
            assign hit[s][w] = vf2[w] && tag[w] == page[s];
        end
        for (genvar i = 16 * s; i < 16 * s + 16; i++) begin : g_bit
            assign mem_data[i / 8][i % 8] = (way[0][i] & hit[s][0]) | (way[1][i] & hit[s][1])
                                          | (way[2][i] & hit[s][2]) | (way[3][i] & hit[s][3]) | !cached;
        end
    end
    assign mem_valid = !req || !cached || hit[0] != 4'd0;

    // ---- Snoop ----------------------------------------------------------------------
    logic [3:0][TW-1:0] snoop;
    always_ff @(posedge clk) begin
        if (wr_tag) begin
            case (victim)
                2'd0: snoop0[f_line[6:0]] <= f_line[PA-6:7];
                2'd1: snoop1[f_line[6:0]] <= f_line[PA-6:7];
                2'd2: snoop2[f_line[6:0]] <= f_line[PA-6:7];
                default: snoop3[f_line[6:0]] <= f_line[PA-6:7];
            endcase
        end
        if (st_advance) begin
            snoop[0] <= snoop0[st_ex_addr[11:5]];
            snoop[1] <= snoop1[st_ex_addr[11:5]];
            snoop[2] <= snoop2[st_ex_addr[11:5]];
            snoop[3] <= snoop3[st_ex_addr[11:5]];
        end
    end
    logic [3:0] st_hit;
    for (genvar w = 0; w < 4; w++) begin : g_snoop
        assign st_hit[w] = st_commit && snoop[w] == st_line0[PA-6:7];
    end

    // ---- Fills and valid bits -------------------------------------------------------------
    logic miss;
    assign miss = req && cached && hit[0] == 4'd0;
    logic [1:0] free;
    always_comb begin
        free = rr;
        for (int w = 3; w >= 0; w--) if (!vf2[w]) free = 2'(w);
    end

    // A fill's own line written while it fills spoils it.
    logic spoil, start, set_valid;
    assign spoil = inv_valid && inv_line == f_line || inv_all
                || st_commit && (st_line0 == f_line || st_cross && st_line1 == f_line);
    assign start = fill == F_IDLE && miss;
    assign set_valid = fill == F_VALID && !spoilt && !spoil;
    // A committed store's clears, registered: the way that held its line,
    // and the next line's set when its lanes run into it.
    logic       si_valid, si_cross;
    logic [3:0] si_hit;
    logic [6:0] si_set0, si_set1;
    always_ff @(posedge clk) begin
        si_valid <= st_commit && !reset;
        si_hit <= st_hit;
        si_cross <= st_cross;
        si_set0 <= st_line0[6:0];
        si_set1 <= st_line1[6:0];
    end
    // The valid bits: a fill's victim clears as it starts and sets as it
    // ends; DMA, FLUSH and stores clear. A store or DMA write of another line
    // of the set that ends a fill leaves the fill's bit set.
    for (genvar k = 0; k < 128; k++) begin : g_valid
        for (genvar w = 0; w < 4; w++) begin : g_way
            logic clear;
            assign clear = inv_all || inv_valid && inv_line[6:0] == 7'(k)
                        || si_valid && si_set0 == 7'(k) && si_hit[w]
                        || si_valid && si_cross && si_set1 == 7'(k)
                        || start && blk[7:1] == 7'(k) && free == 2'(w);
            always_ff @(posedge clk) begin
                if (reset) valid[k][w] <= 1'b0;
                else if (set_valid && f_line[6:0] == 7'(k) && victim == 2'(w)) valid[k][w] <= 1'b1;
                else if (clear) valid[k][w] <= 1'b0;
            end
        end
    end

    always_ff @(posedge clk) begin
        if (reset) begin
            fill <= F_IDLE;
            rr <= 2'd0;
        end else begin
            case (fill)
                F_IDLE: if (miss) begin
                    fill <= F_BEATS;
                    f_line <= {page[0], blk[7:1]};
                    victim <= free;
                    rr <= rr + 2'd1;
                    beat_n <= 1'b0;
                    spoilt <= 1'b0;
                end
                F_BEATS: if (i_beat) begin
                    beat_n <= 1'b1;
                    if (beat_n) fill <= F_TAGS;
                end
                F_TAGS: if (wr_tag) fill <= F_VALID;
                default: fill <= F_IDLE;
            endcase
            if (fill != F_IDLE && spoil) spoilt <= 1'b1;
        end
    end
endmodule
