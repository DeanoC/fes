// SPDX-License-Identifier: MIT
// Original FES implementation from published behavior; no CPU RTL was consulted.
// Documented behavior: Zilog Z80 CPU User Manual UM008011-0816.
// NMOS corrections: Sean Young, The Undocumented Z80 Documented v0.90;
// David Banks, Z80Decoder research, Undocumented Flags (SCF/CCF).
//
// F = {S,Z,Y,H,X,P/V,N,C}. Unary, shift and BIT operations consume a;
// binary arithmetic/logical operations consume a and b. CP returns a-b but
// its caller must suppress register writeback. BIT returns the original a.
// xy_source supplies register data or the appropriate WZ/address high byte
// for NMOS BIT. q means the preceding instruction modified flags; the CPU
// must clear it for an intervening DD/FD prefix before SCF/CCF.
// NMOS=0 clears undefined flag outputs, including BIT S/PV. SLL and
// unassigned operation numbers return a and preserve all input flags.
module fes_z80_alu #(
    parameter bit NMOS = 1'b1
) (
    input  logic [4:0] op,
    input  logic [7:0] a,
    input  logic [7:0] b,
    input  logic [7:0] flags_in,
    input  logic [7:0] xy_source,
    input  logic [2:0] bit_index,
    input  logic       q,
    output logic [7:0] result,
    output logic [7:0] flags_out
);
    localparam logic [4:0]
        OP_ADD  = 5'd0,  OP_ADC  = 5'd1,  OP_SUB  = 5'd2,
        OP_SBC  = 5'd3,  OP_AND  = 5'd4,  OP_XOR  = 5'd5,
        OP_OR   = 5'd6,  OP_CP   = 5'd7,  OP_INC  = 5'd8,
        OP_DEC  = 5'd9,  OP_DAA  = 5'd10, OP_RLC  = 5'd11,
        OP_RRC  = 5'd12, OP_RL   = 5'd13, OP_RR   = 5'd14,
        OP_SLA  = 5'd15, OP_SRA  = 5'd16, OP_SLL  = 5'd17,
        OP_SRL  = 5'd18, OP_BIT  = 5'd19, OP_NEG  = 5'd20,
        OP_RLCA = 5'd21, OP_RRCA = 5'd22, OP_RLA  = 5'd23,
        OP_RRA  = 5'd24, OP_CPL  = 5'd25, OP_SCF  = 5'd26,
        OP_CCF  = 5'd27;

    logic [7:0] arithmetic_a, arithmetic_b;
    logic       subtract, carry_in;
    logic [8:0] arithmetic_sum;
    logic [7:0] shift_result;
    logic       shift_carry;
    logic       shift_left_fill, shift_right_fill;
    logic [7:0] daa_correction;
    logic       daa_carry;
    logic [1:0] carry_xy;
    logic       unused_inputs;

    // NMOS DAA also compares invalid BCD digits after subtraction. The
    // documented variant needs only C/H in the subtraction case. Both
    // implement all rows of Zilog's table for valid BCD arithmetic.
    always_comb begin
        daa_correction = 8'h00;
        if (flags_in[4] || ((NMOS || !flags_in[1]) && a[3:0] > 4'd9))
            daa_correction[3:0] = 4'h6;
        daa_carry = flags_in[0] || ((NMOS || !flags_in[1]) && a > 8'h99);
        if (daa_carry)
            daa_correction[7:4] = 4'h6;
    end

    // One add/subtract carry chain serves binary arithmetic, INC/DEC,
    // NEG and DAA. The ninth carry is inverted to obtain subtraction borrow.
    always_comb begin
        arithmetic_a = a;
        arithmetic_b = b;
        subtract = 1'b0;
        carry_in = 1'b0;
        case (op)
            OP_ADC: carry_in = flags_in[0];
            OP_SUB, OP_CP: begin
                subtract = 1'b1;
                carry_in = 1'b1;
            end
            OP_SBC: begin
                subtract = 1'b1;
                carry_in = !flags_in[0];
            end
            OP_INC: arithmetic_b = 8'h01;
            OP_DEC: begin
                arithmetic_b = 8'h01;
                subtract = 1'b1;
                carry_in = 1'b1;
            end
            OP_NEG: begin
                arithmetic_a = 8'h00;
                arithmetic_b = a;
                subtract = 1'b1;
                carry_in = 1'b1;
            end
            OP_DAA: begin
                arithmetic_b = daa_correction;
                subtract = flags_in[1];
                carry_in = flags_in[1];
            end
            default: begin end
        endcase
    end
    assign arithmetic_sum = {1'b0, arithmetic_a}
                          + {1'b0, arithmetic_b ^ {8{subtract}}}
                          + {8'h00, carry_in};

    // Every left shift/rotate has an odd opcode; every right one is even.
    // The output decoder selects this datapath only for those operations.
    assign shift_left_fill =
        (((op == OP_RLC) || (op == OP_RLCA)) && a[7]) ||
        (((op == OP_RL)  || (op == OP_RLA))  && flags_in[0]) ||
        (op == OP_SLL);
    assign shift_right_fill =
        (((op == OP_RRC) || (op == OP_RRCA)) && a[0]) ||
        (((op == OP_RR)  || (op == OP_RRA))  && flags_in[0]) ||
        ((op == OP_SRA) && a[7]);
    assign shift_result = op[0] ? {a[6:0], shift_left_fill}
                                : {shift_right_fill, a[7:1]};
    assign shift_carry = op[0] ? a[7] : a[0];

    assign carry_xy = {a[5], a[3]} | (q ? 2'b00 : {flags_in[5], flags_in[3]});
    // Keep the byte-wide source interface while only two flag bits are used.
    // These inputs also become unused after specializing the documented ALU.
    assign unused_inputs = &{1'b0, xy_source, carry_xy, q};

    always_comb begin
        result = a;
        flags_out = flags_in;
        case (op)
            OP_ADD, OP_ADC, OP_SUB, OP_SBC, OP_CP,
            OP_INC, OP_DEC, OP_NEG: begin
                result = arithmetic_sum[7:0];
                flags_out = 8'h00;
                flags_out[4] = arithmetic_a[4] ^ arithmetic_b[4] ^ result[4];
                flags_out[2] = !(arithmetic_a[7] ^ arithmetic_b[7] ^ subtract)
                             && (arithmetic_a[7] ^ result[7]);
                flags_out[1] = subtract;
                flags_out[0] = arithmetic_sum[8] ^ subtract;
                if (op == OP_INC || op == OP_DEC)
                    flags_out[0] = flags_in[0];
                if (op == OP_CP) begin
                    flags_out[5] = NMOS && b[5];
                    flags_out[3] = NMOS && b[3];
                end
            end
            OP_AND, OP_XOR, OP_OR: begin
                case (op)
                    OP_AND: result = a & b;
                    OP_XOR: result = a ^ b;
                    default: result = a | b;
                endcase
                flags_out = 8'h00;
                flags_out[4] = op == OP_AND;
            end
            OP_DAA: begin
                result = arithmetic_sum[7:0];
                flags_out = 8'h00;
                flags_out[4] = a[4] ^ result[4];
                flags_out[1] = flags_in[1];
                flags_out[0] = daa_carry;
            end
            OP_RLC, OP_RRC, OP_RL, OP_RR, OP_SLA, OP_SRA, OP_SRL: begin
                result = shift_result;
                flags_out = 8'h00;
                flags_out[0] = shift_carry;
            end
            OP_SLL: if (NMOS) begin
                result = shift_result;
                flags_out = 8'h00;
                flags_out[0] = shift_carry;
            end
            OP_BIT: begin
                flags_out = {NMOS && bit_index == 3'd7 && a[7],
                             !a[bit_index], NMOS && xy_source[5], 1'b1,
                             NMOS && xy_source[3], NMOS && !a[bit_index],
                             1'b0, flags_in[0]};
            end
            OP_RLCA, OP_RRCA, OP_RLA, OP_RRA: begin
                result = shift_result;
                flags_out = {flags_in[7:6], NMOS && result[5], 1'b0,
                             NMOS && result[3], flags_in[2], 1'b0, shift_carry};
            end
            OP_CPL: begin
                result = ~a;
                flags_out = {flags_in[7:6], NMOS && result[5], 1'b1,
                             NMOS && result[3], flags_in[2], 1'b1, flags_in[0]};
            end
            OP_SCF: begin
                flags_out = {flags_in[7:6], NMOS && carry_xy[1], 1'b0,
                             NMOS && carry_xy[0], flags_in[2], 1'b0, 1'b1};
            end
            OP_CCF: begin
                flags_out = {flags_in[7:6], NMOS && carry_xy[1], flags_in[0],
                             NMOS && carry_xy[0], flags_in[2], 1'b0, !flags_in[0]};
            end
            default: begin end
        endcase
        if (op <= OP_DAA || (op >= OP_RLC && op <= OP_SRL &&
                            (NMOS || op != OP_SLL)) || op == OP_NEG) begin
            flags_out[7] = result[7];
            flags_out[6] = result == 8'h00;
            flags_out[5] = NMOS && (op == OP_CP ? b[5] : result[5]);
            flags_out[3] = NMOS && (op == OP_CP ? b[3] : result[3]);
            if ((op >= OP_AND && op <= OP_OR) || op == OP_DAA ||
                (op >= OP_RLC && op <= OP_SRL))
                flags_out[2] = ~^result;
        end
    end
endmodule
