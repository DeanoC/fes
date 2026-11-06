// SPDX-License-Identifier: MIT
// The branch target buffer (docs/i386-cpu-design.md, "Prediction" and
// "Phase 4: speed"): 512 entries, two ways of 256 sets, in M10K, indexed by
// linear bits 11:4 of a 16-byte fetch block and tagged with bits 31:12.
// An entry describes the branch whose last byte is in the block
// (btb_entry_t): where it ends, its target's linear address, a 2-bit
// counter, and whether it is a JMP or CALL rel, which is always taken.
//
// F1 loads the fetch block into the address registers with the L1I's; every
// array re-reads the block F2 holds on every other edge, as the L1I's do, so
// a write shows up a cycle later. F2 compares both ways' tags with its block
// here and its decision is registered by fes_i386_fetchbuf, so neither the
// compare nor the select is on F2's way select. An entry counts only when
// its branch's last byte is at or past the offset fetch entered the block
// at (enter: a redirect's or a target's offset, else 0), since no
// instruction run from there can end where it ends otherwise. When both ways
// count, one predicted taken wins, and of two the one that ends first. So
// the two ways can hold two branches of one block.
//
// WB writes one set a cycle (btb_write_t): an entry on a direct branch's
// retirement, or both ways cleared when a fetch check replays.
// The arrays start as M10K content does, zero: no entry valid. Nothing
// clears them at a reset: an entry is only a prediction, which D and WB
// check (fes_i386_cpu, "Branch prediction").
module fes_i386_btb import fes_i386_pkg::*; (
    input  logic        clk,
    input  logic        mem_load,
    input  logic [27:0] mem_block,
    input  logic [3:0]  enter,      // where fetch entered the block F2 holds
    input  btb_write_t  wr,
    // The block F2 holds.
    output logic        hit,
    output logic        way,
    output btb_entry_t  entry
);
    localparam int EW = 60;  // btb_entry_t's width ($bits of a type: not in Yosys)
    (* ramstyle = "M10K" *) logic [EW-1:0] ways0 [0:255];
    (* ramstyle = "M10K" *) logic [EW-1:0] ways1 [0:255];

    logic [27:0] blk_next, blk;
    assign blk_next = mem_load ? mem_block : blk;
    always_ff @(posedge clk) blk <= blk_next;

    btb_entry_t e0, e1;
    always_ff @(posedge clk) begin
        if (wr.valid && wr.ways[0]) ways0[wr.index] <= wr.entry;
        e0 <= ways0[blk_next[7:0]];
    end
    always_ff @(posedge clk) begin
        if (wr.valid && wr.ways[1]) ways1[wr.index] <= wr.entry;
        e1 <= ways1[blk_next[7:0]];
    end

    logic h0, h1, t0, t1, pick1;
    assign h0 = e0.valid && e0.tag == blk[27:8] && e0.last >= enter;
    assign h1 = e1.valid && e1.tag == blk[27:8] && e1.last >= enter;
    assign t0 = e0.uncond || e0.ctr[1];
    assign t1 = e1.uncond || e1.ctr[1];
    assign pick1 = h1 && (!h0 || t1 && (!t0 || e1.last < e0.last));
    assign hit = h0 || h1;
    assign way = pick1;
    assign entry = pick1 ? e1 : e0;
endmodule
