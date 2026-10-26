// SPDX-License-Identifier: MIT
// D: address operands for AG (docs/i386-cpu-design.md, "Registers, address
// generation and the early ALU"). D reads the flip-flop register file, takes
// the youngest in-flight value of each address register from the M1, M2, EX
// and WB result registers, and scales the index, so AG starts from registered
// operands.
//
// A value that completes in the cycle the uop leaves D arrives late, from the
// register it lands in when the uop is in AG:
//   the previous uop's early-ALU result (producer in AG)  -> M1 register
//   load data (producer in M2)                            -> EX register
//   a late-ALU result (producer in EX)                    -> WB register
// At most one late value reaches AG, entering its last 3:2 row as a base or an
// unscaled index. D stalls the uop when a late value would be a scaled index,
// when both operands would be late, or when a producer's value is further off.
//
// The forwarding selects are worked out a cycle ahead, per GPR rather than per
// uop: each cycle a table records, for every GPR, where its youngest value will
// be next cycle among the producers then in M1, M2, EX and WB (the uops now in
// AG, M1, M2 and EX): in a result register, late from the EX or WB register,
// further off, or only in the register file. The uop now in D, which is in AG
// next cycle if it leaves D, is recorded beside the table with whether it
// left. So D's cycle needs no tag compare: it applies each GPR's registered
// select to the result registers, beside the addressing decode, and picks its
// base and index by register number.
//
// The table follows the pipeline. D to WB only ever hold together (hold),
// when nothing moves, so the table keeps its state.
//
// D hands AG:
//   row   the operand for AG's first 3:2 row: the scaled index, or the base
//         when the index is the late operand
//   last  the operand for AG's last row when no value is late: the base
//   late  which register supplies AG's last-row operand instead of last:
//         0 none, 1 M1, 2 EX, 3 WB
// With a 16-bit address size every operand is the register's low half,
// zero-extended, and late_hi is 0 so AG takes the late value's high half
// from last, which is zero.
module fes_i386_dfwd (
    input  logic              clk,
    input  logic              reset,
    // The uop in D: its address registers (fes_i386_amode) ...
    input  logic              has_base,
    input  logic [2:0]        base,
    input  logic              has_index,
    input  logic [2:0]        index,
    input  logic [1:0]        scale,
    input  logic              addr32,
    // ... the GPR it writes and when that value is ready: 0 early (end of AG),
    // 1 load (end of M2), 2 late (end of EX) ...
    input  logic              d_writes,
    input  logic [2:0]        d_dst,
    input  logic [1:0]        d_kind,
    input  logic              d_sp,         // ... and whether it writes ESP early (a stack operation)
    // ... and whether it leaves D on this edge.
    input  logic              advance,
    input  logic              hold,         // D to WB are frozen: the table keeps its state
    // The producers now in AG, M1, M2 and EX, oldest last.
    input  logic [3:0]        prod_valid,   // writes a GPR
    input  logic [3:0][2:0]   prod_dst,
    input  logic [3:0][1:0]   prod_kind,
    input  logic [3:0]        prod_sp,      // writes ESP early from AG, not as its dst
    input  logic [7:0][31:0]  gpr,
    input  logic [3:0][31:0]  stage_value,  // M1, M2, EX and WB result registers
    input  logic [31:0]       wb_sp,        // the WB register's ESP from AG (pipe_t sp_new)
    output logic [31:0]       row,
    output logic [31:0]       last,
    output logic [1:0]        late,
    output logic [1:0]        late_hi,
    output logic              stall,
    // ESP's high half for a 16-bit stack, and whether it is not yet in a
    // register (LEAVE, whose base is EBP, waits for it then).
    output logic [15:0]       sp_hi,
    output logic              sp_wait
);
    localparam logic [1:0] EARLY = 2'd0, LOAD = 2'd1, LATE = 2'd2;

    // ---- The table, for next cycle ----------------------------------------
    // Per GPR: which result register holds its value (one-hot M1, M2, EX, WB;
    // none: the register file), or that it is late and from where (2 EX,
    // 3 WB), or that it is further off.
    logic [7:0][3:0] ready_next, ready;
    logic [7:0]      late_next, late_now, wait_next, wait_now;
    logic [7:0]      sp_wb_next, sp_wb;  // ESP from WB's sp_new, not its result
    logic [7:0][1:0] source_next, source_now;
    logic [7:0]      in_ag;  // the uop now in D writes this GPR
    logic [7:0]      ag_early;  // ... and its value is early
    logic            left;

    for (genvar r = 0; r < 8; r++) begin : g_table
        always_comb begin
            ready_next[r] = 4'b0000;
            late_next[r] = 1'b0;
            source_next[r] = 2'd0;
            wait_next[r] = 1'b0;
            sp_wb_next[r] = 1'b0;
            // Oldest first, so the youngest producer decides. A stack
            // operation's ESP is early.
            if (prod_valid[3] && prod_dst[3] == 3'(r)) begin
                // In WB next cycle.
                ready_next[r] = 4'b1000;
            end
            if (r == 4 && prod_sp[3]) begin
                ready_next[r] = 4'b1000;
                sp_wb_next[r] = 1'b1;
            end
            if (r == 4 && prod_sp[2]) begin
                ready_next[r] = 4'b0100;
                sp_wb_next[r] = 1'b0;
            end
            if (prod_valid[2] && prod_dst[2] == 3'(r)) begin
                // In EX next cycle.
                ready_next[r] = prod_kind[2] == LATE ? 4'b0000 : 4'b0100;
                late_next[r] = prod_kind[2] == LATE;
                source_next[r] = 2'd3;
                wait_next[r] = 1'b0;
            end
            if (r == 4 && prod_sp[1]) begin
                ready_next[r] = 4'b0010;
                late_next[r] = 1'b0;
                wait_next[r] = 1'b0;
                sp_wb_next[r] = 1'b0;
            end
            if (prod_valid[1] && prod_dst[1] == 3'(r)) begin
                // In M2 next cycle.
                ready_next[r] = prod_kind[1] == EARLY ? 4'b0010 : 4'b0000;
                late_next[r] = prod_kind[1] == LOAD;
                source_next[r] = 2'd2;
                wait_next[r] = prod_kind[1] != EARLY && prod_kind[1] != LOAD;
            end
            if (r == 4 && prod_sp[0]) begin
                ready_next[r] = 4'b0001;
                late_next[r] = 1'b0;
                wait_next[r] = 1'b0;
                sp_wb_next[r] = 1'b0;
            end
            if (prod_valid[0] && prod_dst[0] == 3'(r)) begin
                // In M1 next cycle.
                ready_next[r] = prod_kind[0] == EARLY ? 4'b0001 : 4'b0000;
                late_next[r] = 1'b0;
                source_next[r] = 2'd0;
                wait_next[r] = prod_kind[0] != EARLY;
            end
        end
    end

    always_ff @(posedge clk) begin
        if (reset) begin
            ready <= '0;
            late_now <= '0;
            wait_now <= '0;
            sp_wb <= '0;
            in_ag <= '0;
            left <= 1'b0;
        end else if (!hold) begin
            ready <= ready_next;
            late_now <= late_next;
            wait_now <= wait_next;
            sp_wb <= sp_wb_next;
            for (int r = 0; r < 8; r++) in_ag[r] <= d_writes && d_dst == 3'(r) || r == 4 && d_sp;
            left <= advance;
        end
        if (!hold) begin
            source_now <= source_next;
            // The destination decides when it is the register; ESP from a
            // stack operation is early.
            for (int r = 0; r < 8; r++) ag_early[r] <= d_writes && d_dst == 3'(r) ? d_kind == EARLY : 1'b1;
        end
    end

    // ---- D --------------------------------------------------------------------
    // Each GPR's value as the table selects it, beside the addressing decode.
    logic [7:0][31:0] value;
    for (genvar r = 0; r < 8; r++) begin : g_value
        assign value[r] = ({32{ready[r][0]}} & stage_value[0]) | ({32{ready[r][1]}} & stage_value[1])
                        | ({32{ready[r][2]}} & stage_value[2]) | ({32{ready[r][3] && !sp_wb[r]}} & stage_value[3])
                        | ({32{ready[r][3] && sp_wb[r]}} & wb_sp) | ({32{ready[r] == 4'b0000}} & gpr[r]);
    end

    // The base and index by register number. The uop that left D last cycle,
    // now in AG, is younger than everything in the table.
    logic        base_ag, index_ag;
    logic        base_late, index_late, base_wait, index_wait;
    logic [1:0]  base_source, index_source;
    logic [31:0] base_value, index_value, scaled;

    assign base_ag = left && in_ag[base];
    assign index_ag = left && in_ag[index];
    assign base_late = has_base && (base_ag ? ag_early[base] : late_now[base]);
    assign index_late = has_index && (index_ag ? ag_early[index] : late_now[index]);
    assign base_wait = has_base && (base_ag ? !ag_early[base] : wait_now[base]);
    assign index_wait = has_index && (index_ag ? !ag_early[index] : wait_now[index]);
    assign base_source = base_ag ? 2'd1 : source_now[base];
    assign index_source = index_ag ? 2'd1 : source_now[index];
    assign base_value = !has_base ? 32'd0 : addr32 ? value[base] : {16'b0, value[base][15:0]};
    assign index_value = !has_index ? 32'd0 : addr32 ? value[index] : {16'b0, value[index][15:0]};
    assign scaled = index_value << scale;

    assign stall = base_wait || index_wait || (base_late && index_late) || (index_late && scale != 2'd0);
    assign row = index_late ? base_value : scaled;
    assign last = index_late || base_late ? 32'd0 : base_value;
    assign late = index_late ? index_source : base_late ? base_source : 2'd0;
    assign late_hi = addr32 ? late : 2'd0;
    assign sp_hi = value[4][31:16];
    assign sp_wait = left && in_ag[4] || late_now[4] || wait_now[4];
endmodule
