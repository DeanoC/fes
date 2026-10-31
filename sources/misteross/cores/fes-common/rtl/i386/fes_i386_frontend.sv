// SPDX-License-Identifier: MIT
// The fes_i386_cpu front end as far as Phase 1 has built it: F1, F2, L (the
// length decoders), the fetch buffer and Q. Fetch reads a memory model
// (an M10K-like port whose data may arrive late) instead of L1I; there is no
// branch prediction, so fetch is sequential from the last redirect. Q hands D
// one instruction per cycle when no earlier prefix needs a cycle of its own.
module fes_i386_frontend import fes_i386_pkg::*; (
    input  logic                   clk,
    input  logic                   reset,
    input  logic                   redirect,
    input  logic [31:0]            redirect_linear,
    input  logic                   code32,
    output logic [27:0]            mem_block,
    output logic                   mem_load,
    output logic [27:0]            fetch_next,  // the block F1 loads next, unless a redirect comes first
    input  logic [15:0][7:0]       mem_data,
    input  logic                   mem_valid,
    output logic                   q_valid,
    output fetched_t               q,
    input  logic                   d_ready
);
    logic [1:0]        release_half, half_valid;
    logic              start_half;
    logic [15:0][7:0]  fill_bytes;
    logic [1:0][27:0]  half_block;
    logic [31:0][7:0]  bytes;
    lbyte_t [31:0]     dec;
    logic [31:0][3:0]  avail;

    fes_i386_fetchbuf fetch (
        .clk, .reset, .redirect, .redirect_linear, .code32, .mem_block, .mem_load, .fetch_next, .mem_data,
        .mem_valid, .release_half, .q_half(start_half), .half_valid, .fill_bytes, .half_block,
        .bytes, .dec, .avail
    );

    fes_i386_qalign align (
        .clk, .reset, .redirect, .redirect_linear, .half_valid, .half_block, .bytes, .dec, .avail,
        .fill_data(fill_bytes), .release_half, .start_half,
        .q_valid, .q, .d_ready
    );
endmodule
