// SPDX-License-Identifier: MIT
// Q: length select, prefix accumulator and aligner (docs/i386-cpu-design.md,
// "Length decode and alignment").
//
// start is the buffer position of the next byte to consume. Each cycle Q
// selects the per-byte decode at start (from L) and either
//   - consumes one prefix into the accumulator, when the byte at start is a
//     prefix followed by another prefix; or
//   - issues the instruction at start (its last prefix, if any, is part of
//     the per-byte decode) and clears the accumulator.
// So the last prefix before an opcode is free and each earlier one costs a
// cycle. L has already added each length to its byte's position for every
// accumulator variant, so the loop picks a next start by the accumulator's
// 66h/67h flags and then by start: a select, with no add. L counts ends from
// the half before the block it decodes; position j's byte came through L with
// its own half (bytes 0-11) or with the next one (bytes 12-15), which fixes
// how an end maps back to a buffer position.
//
// Whether the instruction at a byte is in the buffer (fes_i386_fetchbuf's
// avail) is a register per byte and variant, so the loop's check is part of
// the same select. An instruction may use bytes of the next half in the cycle
// L2 writes them; the aligner takes those bytes from L2's register.
module fes_i386_qalign import fes_i386_pkg::*; (
    input  logic                   clk,
    input  logic                   reset,
    input  logic                   redirect,
    input  logic [31:0]            redirect_linear,
    input  logic [1:0]             half_valid,
    input  logic [1:0][27:0]       half_block,
    input  logic [31:0][7:0]       bytes,
    input  lbyte_t [31:0]          dec,
    input  logic [31:0][3:0]       avail,
    input  logic [15:0][7:0]       fill_data,
    output logic [1:0]             release_half,
    output logic                   start_half,
    output logic                   q_valid,
    output fetched_t               q,
    input  logic                   d_ready
);
    logic [4:0]  start;
    prefixes_t   acc;
    logic [3:0]  acc_count;    // prefixes consumed; at most 13
    logic [31:0] acc_linear;   // linear address of the first consumed prefix

    // ---- Per-position view of the buffer ----------------------------------
    logic [1:0]       variant;  // {66h seen, 67h seen} among consumed prefixes
    logic [31:0]      ok;
    logic [31:0][4:0] next;
    logic [31:0][3:0] len;

    assign variant = {acc.opsize, acc.adsize};
    for (genvar j = 0; j < 32; j++) begin : g_pos
        localparam int H = j / 16;
        localparam int K = j % 16;
        lbyte_t     d;
        logic [3:0] usable;
        logic [4:0] fin;  // where the instruction ends, counted as L counts

        assign d = dec[j];
        assign usable = avail[j];
        assign len[j] = d.len[variant];
        assign fin = d.fin[variant];
        // L2 wrote this byte while filling half (K < 12 ? H : 1 - H); its count
        // starts at the other half's byte 0.
        assign next[j] = {!(fin[4] ^ ((K < 12) == (H != 0))), fin[3:0]};
        assign ok[j] = usable[variant];
    end

    // ---- The loop -----------------------------------------------------------
    logic       run_here, prefix_here;
    logic       out_free, too_long_run, issue, consume, advance;
    logic [4:0] start_next;

    lbyte_t at_start;  // one select for both fields: DeanoC/yosys#59
    assign at_start = dec[start];
    assign run_here = at_start.run;
    assign prefix_here = at_start.prefix;
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
    assign start_half = start[4];

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
    // Counts of a few bits: LUTs, not a carry chain.
    assign total = 5'(acc_count) + (* no_carry *) 5'(len[start]);
    assign prefix_count = 5'(acc_count) + (* no_carry *) 5'(prefix_here);
    assign acc_count_next = 5'(acc_count) + (* no_carry *) 5'd1;
    assign prefixes = prefix_here ? add_prefix(acc, rotated[0]) : acc;

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
            acc <= add_prefix(acc, rotated[0]);
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
