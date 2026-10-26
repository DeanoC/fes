// SPDX-License-Identifier: MIT
// D: data operands for EX (docs/i386-cpu-design.md, "Registers, address
// generation and the early ALU"). D reads src1, src2 and ECX (a CL count)
// from the flip-flop register file or from the M1, M2, EX and WB result
// registers, like fes_i386_dfwd does for addresses, with the selects worked
// out a cycle ahead per GPR.
//
// A data operand never stalls D. If its youngest producer has not computed
// it, the operand is pending and records how far ahead that producer is
// when the uop leaves D: dist 1-4 for a producer then in AG, M1, M2 or EX.
// No stage behind D holds, so the producer reaches WB, its result in the WB
// register, when the uop is dist stages before WB:
//   dist 4  AG takes it into the AG/M1 register
//   dist 3  M1, into the M1/M2 register
//   dist 2  M2, into the M2/EX register
//   dist 1  EX takes it from the WB register beside its operand
// So a late result reaches the next uop's ALU with no bubble, and the only
// forwarding mux in EX is that 2:1 select from the WB register. An early
// result (LEA) is ready in the M1 register from the cycle after AG.
//
// The table follows the pipeline, as fes_i386_dfwd's does: if a stage
// behind D ever holds, both must be written from the held producers.
module fes_i386_dops (
    input  logic             clk,
    input  logic             reset,         // also on a flush, which kills every producer
    // The uop in D: its source registers ...
    input  logic [2:0]       a_reg,
    input  logic [2:0]       b_reg,
    // ... the GPR it writes, whether its result is early, and whether it
    // leaves D on this edge.
    input  logic             d_writes,
    input  logic [2:0]       d_dst,
    input  logic             d_early,
    input  logic             advance,
    // The producers now in AG, M1, M2 and EX, oldest last.
    input  logic [3:0]       prod_valid,    // writes a GPR
    input  logic [3:0][2:0]  prod_dst,
    input  logic [3:0]       prod_early,
    input  logic [7:0][31:0] gpr,
    input  logic [3:0][31:0] stage_value,   // M1, M2, EX and WB result registers
    output logic [31:0]      a,
    output logic [31:0]      b,
    output logic [31:0]      c,             // ECX
    output logic             pend_a,
    output logic             pend_b,
    output logic             pend_c,
    output logic [2:0]       dist_a,
    output logic [2:0]       dist_b,
    output logic [2:0]       dist_c
);
    // ---- The table, for next cycle ----------------------------------------
    // Per GPR: which result register holds its value (one-hot M1, M2, EX, WB;
    // none: the register file), or that it is pending and how far ahead its
    // producer is.
    logic [7:0][3:0] ready_next, ready;
    logic [7:0]      pend_next, pend_now;
    logic [7:0][2:0] dist_next, dist_now;
    logic [7:0]      in_ag;  // the uop now in D writes this GPR
    logic            left;

    for (genvar r = 0; r < 8; r++) begin : g_table
        always_comb begin
            ready_next[r] = 4'b0000;
            pend_next[r] = 1'b0;
            dist_next[r] = 3'd0;
            // Oldest first, so the youngest producer decides.
            if (prod_valid[3] && prod_dst[3] == 3'(r)) begin
                // In WB next cycle, computed.
                ready_next[r] = 4'b1000;
                pend_next[r] = 1'b0;
            end
            if (prod_valid[2] && prod_dst[2] == 3'(r)) begin
                // In EX next cycle.
                ready_next[r] = prod_early[2] ? 4'b0100 : 4'b0000;
                pend_next[r] = !prod_early[2];
                dist_next[r] = 3'd4;
            end
            if (prod_valid[1] && prod_dst[1] == 3'(r)) begin
                // In M2 next cycle.
                ready_next[r] = prod_early[1] ? 4'b0010 : 4'b0000;
                pend_next[r] = !prod_early[1];
                dist_next[r] = 3'd3;
            end
            if (prod_valid[0] && prod_dst[0] == 3'(r)) begin
                // In M1 next cycle.
                ready_next[r] = prod_early[0] ? 4'b0001 : 4'b0000;
                pend_next[r] = !prod_early[0];
                dist_next[r] = 3'd2;
            end
        end
    end

    always_ff @(posedge clk) begin
        if (reset) begin
            ready <= '0;
            pend_now <= '0;
            in_ag <= '0;
            left <= 1'b0;
        end else begin
            ready <= ready_next;
            pend_now <= pend_next;
            for (int r = 0; r < 8; r++) in_ag[r] <= d_writes && d_dst == 3'(r);
            left <= advance;
        end
        dist_now <= dist_next;
    end

    // ---- D --------------------------------------------------------------------
    // The uop that left D last cycle, now in AG, is younger than everything
    // in the table; its result is never ready yet.
    logic [7:0][31:0] value;
    logic [7:0]       pend;
    logic [7:0][2:0]  ahead;
    for (genvar r = 0; r < 8; r++) begin : g_value
        assign value[r] = ({32{ready[r][0]}} & stage_value[0]) | ({32{ready[r][1]}} & stage_value[1])
                        | ({32{ready[r][2]}} & stage_value[2]) | ({32{ready[r][3]}} & stage_value[3])
                        | ({32{ready[r] == 4'b0000}} & gpr[r]);
        assign pend[r] = (left && in_ag[r]) || pend_now[r];
        assign ahead[r] = left && in_ag[r] ? 3'd1 : dist_now[r];
    end

    assign a = value[a_reg];
    assign b = value[b_reg];
    assign c = value[1];
    assign pend_a = pend[a_reg];
    assign pend_b = pend[b_reg];
    assign pend_c = pend[1];
    assign dist_a = ahead[a_reg];
    assign dist_b = ahead[b_reg];
    assign dist_c = ahead[1];
endmodule
