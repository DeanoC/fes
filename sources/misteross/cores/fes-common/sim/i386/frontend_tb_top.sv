// SPDX-License-Identifier: MIT
// Test wrapper for frontend_tb.cpp: fes_i386_frontend with Q's instruction
// record split into plain ports.
module frontend_tb_top (
    input  logic             clk,
    input  logic             reset,
    input  logic             redirect,
    input  logic [31:0]      redirect_linear,
    input  logic             code32,
    output logic [27:0]      mem_block,
    output logic             mem_load,
    input  logic [127:0]     mem_data,
    output logic             q_valid,
    input  logic             d_ready,
    output logic [31:0]      q_linear,
    output logic [3:0]       q_length,
    output logic [3:0]       q_prefix_count,
    output logic             q_opsize,
    output logic             q_adsize,
    output logic             q_seg_valid,
    output logic [2:0]       q_seg,
    output logic [1:0]       q_rep,
    output logic             q_lock,
    output logic             q_too_long,
    output logic [87:0]      q_bytes
);
    fes_i386_pkg::fetched_t q;

    fes_i386_frontend dut (
        .clk, .reset, .redirect, .redirect_linear, .code32, .mem_block, .mem_load,
        .mem_data(mem_data), .q_valid, .q, .d_ready
    );

    assign q_linear = q.linear;
    assign q_length = q.length;
    assign q_prefix_count = q.prefix_count;
    assign q_opsize = q.prefixes.opsize;
    assign q_adsize = q.prefixes.adsize;
    assign q_seg_valid = q.prefixes.seg_valid;
    assign q_seg = q.prefixes.seg;
    assign q_rep = q.prefixes.rep;
    assign q_lock = q.prefixes.lock;
    assign q_too_long = q.too_long;
    assign q_bytes = q.bytes;
endmodule
