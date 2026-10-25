// SPDX-License-Identifier: MIT
// Q: length select, prefix accumulator and aligner (docs/i386-cpu-design.md,
// "Length decode and alignment").
//
// start is the buffer position of the next byte to consume. Each cycle Q
// selects the per-byte decode at start and either
//   - consumes one prefix into the accumulator, when the byte at start is a
//     prefix followed by another prefix; or
//   - issues the instruction at start (its last prefix, if any, is part of
//     the per-byte decode) and clears the accumulator.
// So the last prefix before an opcode is free and each earlier one costs a
// cycle. Every per-position quantity the loop needs (next start, whether the
// bytes are present) is computed beside its position from registers, so the
// loop itself is one 32:1 select.
//
// An instruction may use bytes of the next half in the cycle F2 writes them;
// the aligner takes those bytes from the incoming block.
//
// code32 is CS.D. A change of code32 needs a redirect, as does every flush.
module fes_i386_qalign (
    input  logic                   clk,
    input  logic                   reset,
    input  logic                   redirect,
    input  logic [31:0]            redirect_linear,
    input  logic                   code32,
    input  logic [1:0]             half_valid,
    input  logic [1:0]             tail_ok,
    input  logic [1:0]             filling,
    input  logic [1:0][27:0]       half_block,
    input  logic [31:0][7:0]       bytes,
    input  logic [31:0]            dec_prefix,
    input  logic [31:0]            dec_run,
    input  logic [31:0]            dec_opsize,
    input  logic [31:0]            dec_adsize,
    input  logic [31:0][15:0]      dec_len,
    input  logic [15:0][7:0]       fill_data,
    output logic [1:0]             release_half,
    output logic                   q_valid,
    output fes_i386_pkg::fetched_t q,
    input  logic                   d_ready
);
    typedef fes_i386_pkg::prefixes_t prefixes_t;

    logic [4:0]  start;
    prefixes_t   acc;
    logic [3:0]  acc_count;    // prefixes consumed; at most 13
    logic [31:0] acc_linear;   // linear address of the first consumed prefix

    // ---- Per-position view of the buffer ----------------------------------
    logic [31:0]      ok;
    logic [31:0][4:0] next;
    logic [31:0][3:0] len;

    for (genvar j = 0; j < 32; j++) begin : g_pos
        localparam int H = j / 16;
        localparam int K = j % 16;
        logic        os32, as32;
        logic [15:0] lengths;
        logic [4:0]  fin;  // K + length; above 16 the instruction ends in the other half

        assign os32 = code32 ^ (acc.opsize | dec_opsize[j]);
        assign as32 = code32 ^ (acc.adsize | dec_adsize[j]);
        assign lengths = dec_len[j];
        assign len[j] = lengths[{os32, as32, 2'b00} +: 4];
        assign fin = fes_i386_pkg::small_add(5'(K), {1'b0, len[j]});
        assign next[j] = dec_run[j] ? 5'(j + 1) : fes_i386_pkg::small_add(5'(j), {1'b0, len[j]});
        assign ok[j] = half_valid[H] && (K < 12 || tail_ok[H])
                    && (dec_run[j] || !fin[4] || fin[3:0] == 4'd0 || half_valid[1 - H] || filling[1 - H]);
    end

    // ---- The loop -----------------------------------------------------------
    logic       run_here, prefix_here;
    logic       out_free, too_long_run, issue, consume, advance;
    logic [4:0] start_next;

    assign run_here = dec_run[start];
    assign prefix_here = dec_prefix[start];
    assign out_free = !q_valid || d_ready;
    // A prefix run that reaches a 16th byte before its opcode cannot fit in
    // 15 bytes; issue it so D raises #GP(0) instead of consuming forever.
    assign too_long_run = run_here && acc_count >= 4'd13;
    assign issue = !redirect && ok[start] && (!run_here || too_long_run) && out_free;
    assign consume = !redirect && ok[start] && run_here && !too_long_run;
    assign advance = issue || consume;
    assign start_next = next[start];
    assign release_half[0] = advance && !start[4] && start_next[4];
    assign release_half[1] = advance && start[4] && !start_next[4];

    // ---- Aligner ------------------------------------------------------------
    logic [31:0][7:0] source;
    logic [12:0][7:0] rotated;
    logic [31:0]      linear_here;
    logic [4:0]       total;     // at most 13 + 12, so bit 4 means over 15 bytes
    logic [4:0]       prefix_count, acc_count_next;
    prefixes_t        prefixes;

    for (genvar j = 0; j < 32; j++) begin : g_source
        assign source[j] = half_valid[j / 16] ? bytes[j] : fill_data[j % 16];
    end
    assign rotated = 104'({source, source} >> {start, 3'b000});
    assign linear_here = {half_block[start[4]], start[3:0]};
    assign total = fes_i386_pkg::small_add({1'b0, acc_count}, {1'b0, len[start]});
    assign prefix_count = fes_i386_pkg::small_add({1'b0, acc_count}, {4'b0, prefix_here});
    assign acc_count_next = fes_i386_pkg::small_add({1'b0, acc_count}, 5'd1);
    assign prefixes = prefix_here ? fes_i386_pkg::add_prefix(acc, rotated[0]) : acc;

    always_ff @(posedge clk) begin
        if (reset) begin
            start <= 5'd0;
        end else if (redirect) begin
            start <= redirect_linear[4:0];
        end else if (advance) begin
            start <= start_next;
        end

        if (reset || redirect || issue) begin
            acc <= '0;
            acc_count <= 4'd0;
        end else if (consume) begin
            acc <= fes_i386_pkg::add_prefix(acc, rotated[0]);
            acc_count <= acc_count_next[3:0];
            if (acc_count == 4'd0) acc_linear <= linear_here;
        end

        if (reset || redirect) begin
            q_valid <= 1'b0;
        end else if (out_free) begin
            q_valid <= issue;
        end

        // The record loads whenever D can take one, so the loop's issue
        // decision drives only q_valid; q is meaningful when q_valid is set.
        if (out_free) begin
            q.linear <= acc_count == 4'd0 ? linear_here : acc_linear;
            q.length <= total[3:0];
            q.prefix_count <= prefix_count[3:0];
            q.prefixes <= prefixes;
            q.too_long <= too_long_run || total[4];
            q.bytes <= prefix_here ? rotated[11:1] : rotated[10:0];
        end
    end
endmodule
