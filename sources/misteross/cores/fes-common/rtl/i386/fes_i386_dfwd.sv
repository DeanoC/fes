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
    input  logic              has_base,
    input  logic [2:0]        base,
    input  logic              has_index,
    input  logic [2:0]        index,
    input  logic [1:0]        scale,
    input  logic              addr32,
    input  logic [7:0][31:0]  gpr,
    // Producers in flight, youngest first: 0 AG, 1 M1, 2 M2, 3 EX, 4 WB.
    input  logic [4:0]        prod_valid,   // writes a GPR
    input  logic [4:0][2:0]   prod_dst,
    input  logic [4:0]        prod_ready,   // its value is in its stage's result register
    input  logic [4:0]        prod_late,    // its value completes this cycle
    input  logic [3:0][31:0]  stage_value,  // M1, M2, EX and WB result registers
    output logic [31:0]       row,
    output logic [31:0]       last,
    output logic [1:0]        late,
    output logic [1:0]        late_hi,
    output logic              stall
);
    // One operand: its value now, or that it is late (and from where), or a stall.
    typedef struct packed {
        logic [31:0] value;
        logic        late;
        logic [1:0]  source;
        logic        stall;
    } operand_t;

    function automatic operand_t fetch(input logic present, input logic [2:0] number,
                                       input logic [7:0][31:0] file,
                                       input logic [4:0] valid, input logic [4:0][2:0] dst,
                                       input logic [4:0] ready, input logic [4:0] later,
                                       input logic [3:0][31:0] values);
        operand_t r;
        logic     found;
        r.value = file[number];
        r.late = 1'b0;
        r.source = 2'd0;
        r.stall = 1'b0;
        found = 1'b0;
        for (int s = 0; s < 5; s++) begin
            if (!found && valid[s] && dst[s] == number) begin
                found = 1'b1;
                if (s != 0 && ready[s]) begin
                    r.value = values[s - 1];
                end else if (later[s] && (s == 0 || s == 2 || s == 3)) begin
                    r.late = 1'b1;
                    r.source = s == 0 ? 2'd1 : s == 2 ? 2'd2 : 2'd3;
                end else begin
                    r.stall = 1'b1;
                end
            end
        end
        if (!present) begin
            r.value = 32'd0;
            r.late = 1'b0;
            r.stall = 1'b0;
        end
        fetch = r;
    endfunction

    operand_t    b, x;
    logic [31:0] base_value, index_value, scaled;

    assign b = fetch(has_base, base, gpr, prod_valid, prod_dst, prod_ready, prod_late, stage_value);
    assign x = fetch(has_index, index, gpr, prod_valid, prod_dst, prod_ready, prod_late, stage_value);
    assign base_value = addr32 ? b.value : {16'b0, b.value[15:0]};
    assign index_value = addr32 ? x.value : {16'b0, x.value[15:0]};
    assign scaled = index_value << scale;

    assign stall = b.stall || x.stall || (b.late && x.late) || (x.late && scale != 2'd0);
    assign row = x.late ? base_value : scaled;
    assign last = x.late || b.late ? 32'd0 : base_value;
    assign late = x.late ? x.source : b.late ? b.source : 2'd0;
    assign late_hi = addr32 ? late : 2'd0;
endmodule
