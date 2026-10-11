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
    logic [31:0] shift_input;
    logic [31:0] bitwise_result;

    assign bitwise_result = ((a ^ b) & {32{~funct3[0]}})
                          | ((a & b) & {32{funct3[1]}});

    assign sum = alt ? a - b : a + b;
    // Reverse input and output to share the barrel shifter.
    for (genvar i = 0; i < 32; i = i + 1) begin : g_shift_reverse
        assign shift_input[i] = funct3[2] ? a[i] : a[31-i];
        assign shift_left[i] = shift_right[31-i];
    end
    // Left shifts always use zero fill.
    assign shift_right = 32'($signed({alt & funct3[2] & a[31], shift_input}) >>> b[4:0]);
    assign eq = a == b;
    assign lt = (a[31] ^ b[31]) ? a[31] : ltu;
    assign ltu = a < b;

    always_comb begin
        case (funct3)
            3'd0: result = sum;
            3'd1: result = shift_left;
            3'd2: result = {31'd0, lt};
            3'd3: result = {31'd0, ltu};
            3'd5: result = shift_right;
            default: result = bitwise_result;
        endcase
    end
endmodule
