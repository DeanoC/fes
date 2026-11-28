// Integer operations that use the extra execution stage. The CPU keeps the
// decoded instruction and operands stable until retirement.
module fes_sh4_integer (
    input  logic        clk,
    input  logic        capture,
    input  logic [15:0] ir,
    output logic        recognized,
    output logic        use_r0,
    input  logic [31:0] lhs, rhs,
    input  logic [2:0]  flags,       // M, Q, T
    output logic [31:0] result,
    output logic        write_gpr,
    output logic [3:0]  flag_mask,
    output logic [3:0]  flag_value
);
    typedef enum logic [5:0] {
        ILLEGAL, ADDV, SUBV, NEG_OP, NEGC, DT, CMP_PZ, CMP_PL, CMP_STR,
        DIV0U, DIV0S, DIV1, SETS, CLRS,
        EXTUB, EXTUW, EXTSB, EXTSW, SWAPB, SWAPW, XTRCT,
        TSTI, ANDI, XORI, ORI, ROTL, ROTR, ROTCL, ROTCR,
        SHLL2, SHLR2, SHLL8, SHLR8, SHLL16, SHLR16, SHAD, SHLD
    } kind_t;

    kind_t decoded, kind;
    logic is_m, is_q, is_t;
    logic subtract, borrow_in;
    logic [31:0] left_add, right_add;
    logic [32:0] sum;
    logic divide_q;
    logic [4:0] shift_right;
    logic signed [32:0] right_operand;

    always_comb begin
        casez (ir)
            16'h3??f: decoded = ADDV;
            16'h3??b: decoded = SUBV;
            16'h6??b: decoded = NEG_OP;
            16'h6??a: decoded = NEGC;
            16'h4?10: decoded = DT;
            16'h4?11: decoded = CMP_PZ;
            16'h4?15: decoded = CMP_PL;
            16'h2??c: decoded = CMP_STR;
            16'h0019: decoded = DIV0U;
            16'h2??7: decoded = DIV0S;
            16'h3??4: decoded = DIV1;
            16'h0058: decoded = SETS;
            16'h0048: decoded = CLRS;
            16'h6??c: decoded = EXTUB;
            16'h6??d: decoded = EXTUW;
            16'h6??e: decoded = EXTSB;
            16'h6??f: decoded = EXTSW;
            16'h6??8: decoded = SWAPB;
            16'h6??9: decoded = SWAPW;
            16'h2??d: decoded = XTRCT;
            16'hc8??: decoded = TSTI;
            16'hc9??: decoded = ANDI;
            16'hca??: decoded = XORI;
            16'hcb??: decoded = ORI;
            16'h4?04: decoded = ROTL;
            16'h4?05: decoded = ROTR;
            16'h4?24: decoded = ROTCL;
            16'h4?25: decoded = ROTCR;
            16'h4?08: decoded = SHLL2;
            16'h4?09: decoded = SHLR2;
            16'h4?18: decoded = SHLL8;
            16'h4?19: decoded = SHLR8;
            16'h4?28: decoded = SHLL16;
            16'h4?29: decoded = SHLR16;
            16'h4??c: decoded = SHAD;
            16'h4??d: decoded = SHLD;
            default: decoded = ILLEGAL;
        endcase
    end
    assign recognized = decoded != ILLEGAL;
    assign use_r0 = decoded inside {TSTI, ANDI, XORI, ORI};
    always_ff @(posedge clk)
        if (capture)
            kind <= decoded;

    assign is_m = flags[2];
    assign is_q = flags[1];
    assign is_t = flags[0];
    assign subtract = kind inside {SUBV, NEG_OP, NEGC, DT} || (kind == DIV1 && is_q == is_m);
    assign borrow_in = kind == NEGC && is_t;
    assign left_add = kind == DIV1 ? {lhs[30:0], is_t} :
                      kind inside {NEG_OP, NEGC} ? 32'b0 : lhs;
    assign right_add = kind == DT ? 32'd1 : rhs;
    assign sum = subtract ? {1'b0, left_add} - {1'b0, right_add} - {32'b0, borrow_in} :
                            {1'b0, left_add} + {1'b0, right_add};
    // DIV1 chooses addition/subtraction using the old Q and M, then derives
    // the new Q using the dividend sign and the carry or borrow of that step.
    assign divide_q = lhs[31] ^ sum[32] ^ is_m;
    assign shift_right = 5'b0 - rhs[4:0];
    assign right_operand = $signed({kind == SHAD && lhs[31], lhs});

    always_comb begin
        result = 32'b0;
        write_gpr = 1'b1;
        flag_mask = 4'b0000;
        flag_value = 4'b0000;
        case (kind)
            ADDV, SUBV, NEGC, DT: begin
                result = sum[31:0];
                flag_mask[0] = 1'b1;
                case (kind)
                    ADDV: flag_value[0] = lhs[31] == rhs[31] && result[31] != lhs[31];
                    SUBV: flag_value[0] = lhs[31] != rhs[31] && result[31] != lhs[31];
                    NEGC: flag_value[0] = sum[32];
                    default: flag_value[0] = lhs == 32'd1;
                endcase
            end
            NEG_OP: result = sum[31:0];
            CMP_PZ, CMP_PL, CMP_STR, TSTI: begin
                write_gpr = 1'b0;
                flag_mask[0] = 1'b1;
                case (kind)
                    CMP_PZ: flag_value[0] = !lhs[31];
                    CMP_PL: flag_value[0] = !lhs[31] && lhs != 0;
                    CMP_STR: flag_value[0] = lhs[31:24] == rhs[31:24] ||
                                             lhs[23:16] == rhs[23:16] ||
                                             lhs[15:8] == rhs[15:8] || lhs[7:0] == rhs[7:0];
                    default: flag_value[0] = (lhs & rhs) == 0;
                endcase
            end
            DIV0U, DIV0S: begin
                write_gpr = 1'b0;
                flag_mask = 4'b1101;
                if (kind == DIV0S)
                    flag_value = {rhs[31], lhs[31], 1'b0, lhs[31] ^ rhs[31]};
            end
            DIV1: begin
                result = sum[31:0];
                flag_mask = 4'b0101;
                flag_value = {1'b0, divide_q, 1'b0, divide_q == is_m};
            end
            SETS, CLRS: begin
                write_gpr = 1'b0;
                flag_mask[1] = 1'b1;
                flag_value[1] = kind == SETS;
            end
            EXTUB: result = {24'b0, rhs[7:0]};
            EXTUW: result = {16'b0, rhs[15:0]};
            EXTSB: result = {{24{rhs[7]}}, rhs[7:0]};
            EXTSW: result = {{16{rhs[15]}}, rhs[15:0]};
            SWAPB: result = {rhs[31:16], rhs[7:0], rhs[15:8]};
            SWAPW: result = {rhs[15:0], rhs[31:16]};
            XTRCT: result = {rhs[15:0], lhs[31:16]};
            ANDI: result = lhs & rhs;
            XORI: result = lhs ^ rhs;
            ORI: result = lhs | rhs;
            ROTL, ROTCL: begin
                result = {lhs[30:0], kind == ROTCL ? is_t : lhs[31]};
                flag_mask[0] = 1'b1;
                flag_value[0] = lhs[31];
            end
            ROTR, ROTCR: begin
                result = {kind == ROTCR ? is_t : lhs[0], lhs[31:1]};
                flag_mask[0] = 1'b1;
                flag_value[0] = lhs[0];
            end
            SHLL2: result = lhs << 2;
            SHLR2: result = lhs >> 2;
            SHLL8: result = lhs << 8;
            SHLR8: result = lhs >> 8;
            SHLL16: result = lhs << 16;
            SHLR16: result = lhs >> 16;
            SHAD, SHLD: begin
                if (!rhs[31])
                    result = lhs << rhs[4:0];
                else if (rhs[4:0] == 0)
                    result = {32{right_operand[32]}};
                else
                    result = 32'(right_operand >>> shift_right);
            end
            default: write_gpr = 1'b0;
        endcase
    end
endmodule
