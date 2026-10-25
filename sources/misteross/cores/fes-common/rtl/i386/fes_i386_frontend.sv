// SPDX-License-Identifier: MIT
// The fes_i386_cpu front end as far as Phase 1's first slice: F1, F2, the
// fetch buffer with its length decoders, and Q. Fetch reads a memory model
// (an M10K-like port) instead of L1I; there is no branch prediction, so fetch
// is sequential from the last redirect. Q hands D one instruction per cycle
// when no earlier prefix needs a cycle of its own.
module fes_i386_frontend (
    input  logic                   clk,
    input  logic                   reset,
    input  logic                   redirect,
    input  logic [31:0]            redirect_linear,
    input  logic                   code32,
    output logic [27:0]            mem_block,
    output logic                   mem_load,
    input  logic [15:0][7:0]       mem_data,
    output logic                   q_valid,
    output fes_i386_pkg::fetched_t q,
    input  logic                   d_ready
);
    logic [1:0]        release_half, half_valid, tail_ok, filling;
    logic [1:0][27:0]  half_block;
    logic [31:0][7:0]  bytes;
    logic [31:0]       dec_prefix, dec_run, dec_opsize, dec_adsize;
    logic [31:0][15:0] dec_len;

    fes_i386_fetchbuf fetch (
        .clk, .reset, .redirect, .redirect_linear, .mem_block, .mem_load, .mem_data,
        .release_half, .half_valid, .tail_ok, .filling, .half_block, .bytes,
        .dec_prefix, .dec_run, .dec_opsize, .dec_adsize, .dec_len
    );

    fes_i386_qalign align (
        .clk, .reset, .redirect, .redirect_linear, .code32, .half_valid, .tail_ok, .filling,
        .half_block, .bytes, .dec_prefix, .dec_run, .dec_opsize, .dec_adsize, .dec_len,
        .fill_data(mem_data), .release_half, .q_valid, .q, .d_ready
    );
endmodule
