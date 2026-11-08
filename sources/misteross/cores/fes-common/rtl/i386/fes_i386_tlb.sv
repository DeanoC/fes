// SPDX-License-Identifier: MIT
// A TLB (docs/i386-cpu-design.md, "Paging"): N entries, fully associative, in
// flip-flops, each a linear page and what the walker found for it
// (fes_i386_pkg::tlb_entry_t). P lookup ports compare their linear page with
// every entry at once: hit, the matching entry one-hot (match) and its fields
// (q). The DTLB (16 entries, two ports, M1) and the ITLB (8, one port, F1)
// are both this module.
//
// The walker (fes_i386_l1d) fills one entry at a time: the entry that
// already holds the page if one does, so a page is never held twice (a
// walk that sets D refills a read's entry), else the first invalid entry,
// else the next of a round robin. A flush clears every entry: a CR3 write,
// a CR0 write that changes PG, and anything that changes what phys and attr
// gave an entry (A20M, the attribute map), as fes_i386_cpu decides.
module fes_i386_tlb import fes_i386_pkg::*; #(
    parameter int N = 16,
    parameter int P = 2
) (
    input  logic                  clk,
    input  logic                  flush,
    input  logic [P-1:0][19:0]    lk_lp,
    output logic [P-1:0]          hit,
    output logic [P-1:0][N-1:0]   match,
    output tlb_entry_t [P-1:0]    q,
    // Every entry's physical page, for the L1D's compare in parallel with its
    // tags (fes_i386_l1d).
    output logic [N-1:0][PA-12:0] pages,
    input  logic                  fill,
    input  tlb_entry_t            fill_e
);
    localparam int IW = $clog2(N);
    tlb_entry_t [N-1:0] e;
    logic [N-1:0]       v;
    logic [IW-1:0]      rr;

    for (genvar k = 0; k < N; k++) begin : g_pages
        assign pages[k] = e[k].pp;
    end

    for (genvar p = 0; p < P; p++) begin : g_port
        for (genvar k = 0; k < N; k++) begin : g_cmp
            assign match[p][k] = v[k] && e[k].lp == lk_lp[p];
        end
        assign hit[p] = match[p] != '0;
        // The matching entry's fields, an AND-OR over the one-hot match.
        always_comb begin
            q[p] = '0;
            for (int k = 0; k < N; k++) if (match[p][k]) q[p] = q[p] | e[k];
        end
    end

    // The entry a fill writes.
    logic [N-1:0] same;
    logic [IW-1:0] slot;
    always_comb begin
        for (int k = 0; k < N; k++) same[k] = v[k] && e[k].lp == fill_e.lp;
        slot = rr;
        for (int k = N - 1; k >= 0; k--) if (!v[k]) slot = IW'(k);
        for (int k = N - 1; k >= 0; k--) if (same[k]) slot = IW'(k);
    end

    always_ff @(posedge clk) begin
        if (flush) begin
            v <= '0;
            rr <= '0;
        end else if (fill) begin
            v[slot] <= 1'b1;
            e[slot] <= fill_e;
            if (same == '0 && &v) rr <= rr + IW'(1);
        end
    end
endmodule
