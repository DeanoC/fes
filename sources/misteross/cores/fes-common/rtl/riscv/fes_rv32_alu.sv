// SPDX-License-Identifier: MIT
// RV32I integer operations and branch comparisons for fes_rv32_cpu.
// funct3 selects the operation as the OP/OP-IMM encodings do; alt is
// funct7[5] (SUB instead of ADD, SRA instead of SRL).
module fes_rv32_alu (
    input  logic [31:0] a,
    input  logic [31:0] b,
    input  logic [2:0]  funct3,
    input  logic        alt,
    output logic [31:0] result,
    output logic        eq,
    output logic        lt,
    output logic        ltu
);
    logic [31:0] sum;
    logic [31:0] shift_left;
    logic [31:0] shift_right;
    logic [31:0] shift_right_arith;

    assign sum = alt ? a - b : a + b;
    assign shift_left = a << b[4:0];
    assign shift_right = a >> b[4:0];
    // Keep the arithmetic shift in its own assignment: inside a wider
    // unsigned expression $signed() would be lost and >>> becomes logical.
    assign shift_right_arith = $signed(a) >>> b[4:0];
    assign eq = a == b;
    assign lt = $signed(a) < $signed(b);
    assign ltu = a < b;

    always_comb begin
        case (funct3)
            3'd0: result = sum;
            3'd1: result = shift_left;
            3'd2: result = {31'd0, lt};
            3'd3: result = {31'd0, ltu};
            3'd4: result = a ^ b;
            3'd5: result = alt ? shift_right_arith : shift_right;
            3'd6: result = a | b;
            default: result = a & b;
        endcase
    end
endmodule
