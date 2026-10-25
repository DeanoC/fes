// SPDX-License-Identifier: MIT
// EX: the late ALU, the funnel shifter, flags and branch resolution
// (docs/i386-cpu-design.md, "Execute"). One adder serves ADD, ADC, SUB, SBB,
// CMP, INC, DEC, NEG and LOOP's count; ZF of a sum comes from the operands,
// not from the sum, so no 32-input NOR follows the carry chain.
//
// a is the full value of src1, which is also the destination register: the
// result merges into it by size and byte lane, so EX hands WB the register's
// whole new value. b is src2 or the immediate; c is ECX for a CL count.
// flags_in is EFLAGS after every older uop (EX's running copy), so Jcc,
// SETcc, ADC, SBB, RCL and RCR read it with no forwarding. A faulting uop
// changes nothing.
//
// Results the manual leaves undefined follow the 386EX (Phase 0 divergence
// list): AND, OR, XOR and TEST clear AF; a shift by a nonzero count sets AF;
// shifts and rotates set OF and CF as fes_i386_shift says.
module fes_i386_ex (
    input  fes_i386_pkg::uop_t u,
    input  logic [31:0]        a,
    input  logic [31:0]        b,
    input  logic [31:0]        c,
    input  logic [31:0]        ag_result,   // LEA's offset
    input  logic [17:0]        flags_in,
    output logic [31:0]        result,
    output logic [17:0]        flags_out,
    output logic               taken
);
    localparam logic [2:0] C_ALU = 3'd0, C_SHIFT = 3'd1, C_BRANCH = 3'd4, C_SEGMENT = 3'd5;
    localparam int CF = 0, PF = 2, AF = 4, ZF = 6, SF = 7, IF = 9, DF = 10, OF = 11;

    logic [2:0] cls;
    logic [4:0] op;
    logic [1:0] size, src_size;
    logic       ok;  // no fault
    assign cls = 3'(u.cls);
    assign op = u.op;
    assign size = 2'(u.size);
    assign src_size = 2'(u.src_size);
    assign ok = u.ctl.fault == fes_i386_pkg::FAULT_NONE;

    function automatic logic [31:0] extract(input logic [31:0] v, input logic [1:0] sz, input logic hi8);
        case (sz)
            2'd0: extract = {24'b0, hi8 ? v[15:8] : v[7:0]};
            2'd1: extract = {16'b0, v[15:0]};
            default: extract = v;
        endcase
    endfunction

    function automatic logic [31:0] size_mask(input logic [1:0] sz);
        size_mask = sz == 2'd0 ? 32'h0000_00ff : sz == 2'd1 ? 32'h0000_ffff : 32'hffff_ffff;
    endfunction

    function automatic logic cond(input logic [17:0] f, input logic [3:0] cc);
        logic t;
        case (cc[3:1])
            3'd0: t = f[11];                          // O
            3'd1: t = f[0];                           // B
            3'd2: t = f[6];                           // Z
            3'd3: t = f[0] | f[6];                    // BE
            3'd4: t = f[7];                           // S
            3'd5: t = f[2];                           // P
            3'd6: t = f[7] ^ f[11];                   // L
            default: t = (f[7] ^ f[11]) | f[6];       // LE
        endcase
        cond = t ^ cc[0];
    endfunction

    logic [31:0] an, bn, bs, mask;
    logic [4:0]  topi;  // the operand's top bit
    assign an = extract(a, size, u.src1.hi8);
    assign bn = extract(b, size, u.src2.hi8);
    assign bs = extract(b, src_size, u.src2.hi8);
    assign mask = size_mask(size);
    assign topi = size == 2'd0 ? 5'd7 : size == 2'd1 ? 5'd15 : 5'd31;

    // ---- Adder ----------------------------------------------------------------
    logic        is_add, inc, dec, neg, loop, sub, cin;
    logic [31:0] x, y, yb;
    logic [32:0] sum;
    logic [31:0] r_add;
    logic        carry, add_cf, add_of, add_af, add_zf;
    assign inc = cls == C_ALU && op == 5'd10;
    assign dec = cls == C_ALU && op == 5'd11;
    assign neg = cls == C_ALU && op == 5'd12;
    assign loop = cls == C_BRANCH;
    assign is_add = cls == C_ALU && (op == 5'd0 || op == 5'd2 || op == 5'd3 || op == 5'd5 || op == 5'd7 || inc || dec || neg);
    assign sub = op == 5'd3 || op == 5'd5 || op == 5'd7 || dec || neg || loop;
    assign cin = op == 5'd2 ? flags_in[CF] : op == 5'd3 ? !flags_in[CF] : sub;
    assign x = neg ? 32'd0 : an;
    assign yb = inc || dec || loop ? 32'd1 : neg ? an : bn;
    assign y = (sub ? ~yb : yb) & mask;
    assign sum = {1'b0, x} + {1'b0, y} + {32'd0, cin};
    assign r_add = sum[31:0] & mask;
    assign carry = size == 2'd0 ? sum[8] : size == 2'd1 ? sum[16] : sum[32];
    assign add_cf = sub ? !carry : carry;
    assign add_of = (x[topi] == y[topi]) && (sum[{1'b0, topi}] != x[topi]);
    assign add_af = x[4] ^ y[4] ^ sum[4] ^ sub;
    // The sum is zero when bit 0's propagate equals the carry in and every
    // higher bit's propagate equals the OR of the bit below it.
    logic [31:0] p, g, zbits;
    assign p = x ^ y;
    assign g = x | y;
    assign zbits = ~(p ^ {g[30:0], cin}) | ~mask;
    assign add_zf = &zbits;

    // ---- Logic ------------------------------------------------------------------
    logic [31:0] r_logic;
    always_comb begin
        case (op)
            5'd1: r_logic = an | bn;
            5'd6: r_logic = an ^ bn;
            5'd13: r_logic = ~an & mask;
            default: r_logic = an & bn;  // AND, TEST
        endcase
    end

    // ---- Shifter ----------------------------------------------------------------
    logic [4:0]  count;
    logic [31:0] r_shift;
    logic        sh_cf, sh_of, rotate;
    assign count = u.count_cl ? c[4:0] : u.imm[4:0];
    assign rotate = op[4:2] == 3'b000;  // ROL ROR RCL RCR
    fes_i386_shift shifter (
        .op(op[3:0]), .size, .count, .a(an), .b(bn), .cf_in(flags_in[CF]), .r(r_shift), .cf(sh_cf), .of(sh_of)
    );

    // ---- Result -------------------------------------------------------------------
    logic [31:0] r;
    logic        cc_true;
    assign cc_true = cond(flags_in, u.ctl.cond);
    always_comb begin
        r = bn;
        case (cls)
            C_ALU: begin
                case (op)
                    5'd0, 5'd2, 5'd3, 5'd5, 5'd7, 5'd10, 5'd11, 5'd12: r = r_add;
                    5'd1, 5'd4, 5'd6, 5'd8, 5'd13: r = r_logic;
                    5'd14: r = ag_result;                                         // LEA
                    5'd15: r = bs;                                                // MOVZX
                    5'd16: r = src_size == 2'd0 ? {{24{bs[7]}}, bs[7:0]} : {{16{bs[15]}}, bs[15:0]};  // MOVSX
                    5'd17: r = {31'b0, cc_true};                                  // SETcc
                    5'd30: r = {24'b0, flags_in[7:6], 1'b0, flags_in[4], 1'b0, flags_in[2], 1'b1, flags_in[0]};  // LAHF
                    5'd31: r = {32{src_size == 2'd2 ? bs[31] : src_size == 2'd1 ? bs[15] : bs[7]}};  // SIGN
                    default: r = bn;                                              // MOV
                endcase
            end
            C_SHIFT: r = count == 5'd0 ? an : r_shift;
            C_BRANCH: r = r_add;                                                  // LOOP's count
            C_SEGMENT: r = bs;
            default: ;
        endcase
    end

    // Merge into the destination's old value by size and byte lane.
    always_comb begin
        case (size)
            2'd0: result = u.dst.hi8 ? {a[31:16], r[7:0], a[7:0]} : {a[31:8], r[7:0]};
            2'd1: result = {a[31:16], r[15:0]};
            default: result = r;
        endcase
    end

    // ---- Flags ------------------------------------------------------------------
    logic       zf_r, sf_r, pf_r;
    logic [3:0] fop;
    assign zf_r = (r & mask) == 32'd0;
    assign sf_r = r[topi];
    assign pf_r = ~^r[7:0];
    assign fop = u.imm[3:0];
    always_comb begin
        flags_out = flags_in;
        flags_out[1] = 1'b1;
        if (ok) begin
            case (cls)
                C_ALU: begin
                    case (op)
                        5'd0, 5'd2, 5'd3, 5'd5, 5'd7, 5'd12: begin
                            flags_out[CF] = add_cf;
                            flags_out[OF] = add_of;
                            flags_out[AF] = add_af;
                            flags_out[ZF] = add_zf;
                            flags_out[SF] = sf_r;
                            flags_out[PF] = pf_r;
                        end
                        5'd10, 5'd11: begin  // INC, DEC keep CF
                            flags_out[OF] = add_of;
                            flags_out[AF] = add_af;
                            flags_out[ZF] = add_zf;
                            flags_out[SF] = sf_r;
                            flags_out[PF] = pf_r;
                        end
                        5'd1, 5'd4, 5'd6, 5'd8: begin
                            flags_out[CF] = 1'b0;
                            flags_out[OF] = 1'b0;
                            flags_out[AF] = 1'b0;
                            flags_out[ZF] = zf_r;
                            flags_out[SF] = sf_r;
                            flags_out[PF] = pf_r;
                        end
                        5'd30: begin
                            case (fop)
                                4'd0: flags_out[CF] = 1'b0;
                                4'd1: flags_out[CF] = 1'b1;
                                4'd2: flags_out[CF] = !flags_in[CF];
                                4'd3: flags_out[IF] = 1'b0;
                                4'd4: flags_out[IF] = 1'b1;
                                4'd5: flags_out[DF] = 1'b0;
                                4'd6: flags_out[DF] = 1'b1;
                                4'd7: begin  // SAHF
                                    flags_out[CF] = bn[0];
                                    flags_out[PF] = bn[2];
                                    flags_out[AF] = bn[4];
                                    flags_out[ZF] = bn[6];
                                    flags_out[SF] = bn[7];
                                end
                                default: ;
                            endcase
                        end
                        default: ;
                    endcase
                end
                C_SHIFT: begin
                    if (count != 5'd0) begin
                        flags_out[CF] = sh_cf;
                        flags_out[OF] = sh_of;
                        if (!rotate) begin
                            flags_out[AF] = 1'b1;
                            flags_out[ZF] = zf_r;
                            flags_out[SF] = sf_r;
                            flags_out[PF] = pf_r;
                        end
                    end
                end
                default: ;
            endcase
        end
    end

    // ---- Branches ---------------------------------------------------------------
    // LOOP's count reaches zero when it was one, so the decision needs no sum.
    logic count_one, count_zero;
    assign count_one = an == 32'd1;
    assign count_zero = an == 32'd0;
    always_comb begin
        taken = 1'b0;
        if (ok && cls == C_BRANCH) begin
            case (op)
                5'd0: taken = cc_true;                                  // Jcc
                5'd1: taken = 1'b1;                                     // JMP
                5'd6: taken = !count_one;                               // LOOP
                5'd7: taken = !count_one && flags_in[ZF];               // LOOPE
                5'd8: taken = !count_one && !flags_in[ZF];              // LOOPNE
                5'd9: taken = count_zero;                               // JCXZ
                default: ;
            endcase
        end
    end
endmodule
