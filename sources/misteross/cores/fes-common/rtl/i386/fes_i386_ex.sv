// SPDX-License-Identifier: MIT
// EX: the late ALU, the funnel shifter, flags and branch resolution
// (docs/i386-cpu-design.md, "Execute"). One adder serves ADD, ADC, SUB, SBB,
// CMP, INC, DEC, NEG and LOOP's count; ZF of a sum comes from the operands,
// not from the sum, so no 32-input NOR follows the carry chain.
//
// M2 prepares the operands (fes_i386_exctl): x is src1 and y src2 or what
// the operation adds, at its size from bit 0, y inverted for a subtract. An
// operand whose producer is one stage ahead comes from the WB result register
// (wb), ORed into x or XORed into y by a registered bypass select. a is
// src1's whole value, which is also the destination register: the result
// merges into it by size and byte lane, so EX hands WB the register's whole
// new value. flags_in is EX's running copy of EFLAGS, which Jcc, SETcc, ADC,
// SBB, RCL, RCR and LAHF read with no forwarding; flags_keep is EFLAGS after
// every older uop, which a uop passes on where it writes no flag. They differ
// only in the cycle after a shift or rotate, when D holds any flag reader
// (fes_i386_cpu). A normal faulting uop changes nothing; a divide error
// carries the flags that the multiply/divide unit has already produced.
//
// A shift's flags leave the shifter off the flag path: EX keeps the old flags
// and hands WB the bits the shifter's CF and OF come from, and whether SF,
// ZF, AF and PF follow the result (sh_*, def_co, def_szap). fes_i386_cpu
// applies them from the WB register a cycle later (fes_i386_shflags).
//
// Every select here is a registered control from fes_i386_exctl, decoded in
// M2, so the opcode decode is off the data paths (design rule 8). The adder's
// operands are one level from their registers. Above an add's size x is ones
// and y zeros, so its carry out is sum[32] at every size. A second, parallel
// chain adds the operands with the size's top bit filled the same way: its
// carry out is the carry into the top bit, which gives OF and SF with no size
// select after either chain. Each flag and each lane of the result takes a
// chain's bit through one gate, beside a value worked out without it.
//
// Results the manual leaves undefined follow the 386EX (Phase 0 divergence
// list): AND, OR, XOR and TEST clear AF; a shift by a nonzero count sets AF;
// shifts and rotates set OF and CF as fes_i386_shflags says.
//
// MUL, IMUL, DIV, IDIV, AAM and AAD take their flags from the multiply and
// divide unit (fes_i386_muldiv, md_flags); their result comes through LEA's
// select, from the pipe record's result field, which fes_i386_cpu fills
// from the unit.
module fes_i386_ex (
    input  fes_i386_pkg::uop_t   u,
    input  fes_i386_pkg::exctl_t exc,
    input  logic [31:0]          a,
    input  logic [31:0]          xr,          // M2's x and y
    input  logic [31:0]          yr,
    input  logic [31:0]          wb,          // the WB result register
    input  logic [31:0]          ld,          // the loaded value (M2)
    input  logic [31:0]          ag_result,   // LEA's offset; a multiply's or divide's result
    input  logic [17:0]          flags_in,
    input  logic [17:0]          flags_keep,
    input  logic [11:0]          md_flags,
    output logic [31:0]          result,
    output logic [17:0]          flags_out,
    output logic                 taken,
    output logic [31:0]          ind_target,  // RET, JMP r/m: the target's linear address and EIP
    output logic [31:0]          ind_eip,
    output logic                 sh_out0,
    output logic                 sh_outn,
    output logic                 sh_cfw,
    output logic                 sh_wide,
    output logic                 def_co,      // CF and OF come from sh_* (fes_i386_shflags)
    output logic                 def_szap     // SF, ZF and PF follow the result; AF is set
);
    localparam int CF = 0, PF = 2, AF = 4, ZF = 6, SF = 7, TF = 8, IF = 9, DF = 10, OF = 11;

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

    // ---- Operands -----------------------------------------------------------------
    logic [31:0] af, x, y, mask;
    assign af = exc.byp_a ? wb : a;
    logic [31:0] xs, ys;  // what x and y take in: the WB result or the loaded value
    assign xs = exc.xl ? ld : wb;
    assign ys = exc.yl ? ld : wb;
    assign x = xr | {{16{exc.xw32}} & xs[31:16], {8{exc.xw16}} & xs[15:8],
                     {8{exc.xw_lo}} & xs[7:0] | {8{exc.xw_hi}} & xs[15:8]};
    assign y = yr ^ {{16{exc.yw32}} & ys[31:16], {8{exc.yw16}} & ys[15:8],
                     {8{exc.yw_lo}} & ys[7:0] | {8{exc.yw_hi}} & ys[15:8]};
    assign mask = exc.size8 ? 32'h0000_00ff : exc.size16 ? 32'h0000_ffff : 32'hffff_ffff;

    // ---- Adder ----------------------------------------------------------------
    logic        cin, p_top;
    logic [31:0] top, x2, y2;
    logic [32:0] sum, sum2;
    assign cin = exc.cin_cf ? flags_in[CF] : exc.cin_ncf ? !flags_in[CF] : exc.sub;
    assign sum = {1'b0, x} + {1'b0, y} + {32'd0, cin};
    // The size's top bit and everything above it.
    assign top = exc.size8 ? 32'hffff_ff80 : exc.size16 ? 32'hffff_8000 : 32'h8000_0000;
    assign x2 = x | top;
    assign y2 = y & ~top;
    assign sum2 = {1'b0, x2} + {1'b0, y2} + {32'd0, cin};
    assign p_top = exc.size8 ? x[7] ^ y[7] : exc.size16 ? x[15] ^ y[15] : x[31] ^ y[31];
    // The sum is zero when bit 0's propagate equals the carry in and every
    // higher bit's propagate equals the OR of the bit below it.
    logic [31:0] p, g;
    logic        add_zf;
    assign p = x ^ y;
    assign g = x | y;
    assign add_zf = &(~(p ^ {g[30:0], cin}) | ~mask);

    // ---- Logic ------------------------------------------------------------------
    logic [31:0] r_logic;
    assign r_logic = exc.l_or ? x | y : exc.l_xor ? x ^ y : x & y;

    // ---- Shifter ----------------------------------------------------------------
    logic [4:0]  count, s;
    logic [33:0] out;
    assign count = exc.byp_c ? wb[4:0] : exc.sh.count;
    assign s = exc.byp_c ? wb[4:0] ^ {5{exc.sh.left}} : exc.sh.s;
    fes_i386_shift shifter (.c(exc.sh), .s, .a(x), .b(y), .cf_in(flags_in[CF]), .out);
    assign sh_out0 = out[0];
    assign sh_outn = exc.sh.z8 ? out[9] : exc.sh.z16 ? out[17] : out[33];
    assign sh_wide = (exc.sh.cf_shl || exc.sh.cf_shr) && exc.sh.z8 && (count == 5'd16 || count == 5'd24);
    assign sh_cfw = exc.sh.cf_shl ? x[0] : x[7];
    assign def_co = exc.ok && exc.f_shift && count != 5'd0;
    assign def_szap = def_co && exc.sh.szap;

    // ---- Result -------------------------------------------------------------------
    // Each lane ORs the sum, the shift and the other results, each gated by
    // its registered lane select, with the old value where the size leaves it.
    logic [31:0] r_other, r_shift;
    logic        cc_true;
    assign cc_true = cond(flags_in, u.ctl.cond);
    assign r_other = ({32{exc.r_logic}} & r_logic)
                   | ({32{exc.r_lea}} & ag_result)
                   | ({32{exc.r_y}} & y)
                   | ({32{exc.r_movsx}} & (exc.src8 ? {{24{y[7]}}, y[7:0]} : {{16{y[15]}}, y[15:0]}))
                   | ({32{exc.r_setcc}} & {31'b0, cc_true})
                   | ({32{exc.r_lahf}} & {24'b0, flags_in[7:6], 1'b0, flags_in[4], 1'b0, flags_in[2], 1'b1, flags_in[0]})
                   | ({32{exc.r_sign}} & {32{exc.src8 ? y[7] : exc.src16 ? y[15] : y[31]}})
                   | ({32{exc.r_flags}} & {16'b0, flags_in[15:0]});
    assign r_shift = out[32:1];
    assign result[7:0] = ({8{exc.add_lo}} & sum[7:0]) | ({8{exc.sh_lo}} & r_shift[7:0])
                       | ({8{exc.ot_lo}} & r_other[7:0]) | ({8{exc.kp_lo}} & af[7:0]);
    assign result[15:8] = ({8{exc.add_mid}} & sum[15:8]) | ({8{exc.add_h8}} & sum[7:0])
                        | ({8{exc.sh_mid}} & r_shift[15:8]) | ({8{exc.sh_h8}} & r_shift[7:0])
                        | ({8{exc.ot_mid}} & r_other[15:8]) | ({8{exc.ot_h8}} & r_other[7:0])
                        | ({8{exc.kp_mid}} & af[15:8]);
    assign result[31:16] = ({16{exc.add_hi}} & sum[31:16]) | ({16{exc.sh_hi}} & r_shift[31:16])
                         | ({16{exc.ot_hi}} & r_other[31:16]) | ({16{exc.kp_hi}} & af[31:16]);

    // ---- Flags ------------------------------------------------------------------
    // Each flag is its adder value where the adder sets it, else a value
    // worked out off the chains: the logic unit's, a flag instruction's, or
    // the old flag. SF, ZF and PF of a logic op come from the logic unit alone.
    logic zf_l, sf_l, pf_l, fl_cf, add, arith;
    assign zf_l = r_logic == 32'd0;
    assign sf_l = exc.size8 ? r_logic[7] : exc.size16 ? r_logic[15] : r_logic[31];
    assign pf_l = ~^r_logic[7:0];
    assign fl_cf = exc.fl_cf || (exc.fl_cmc && !flags_in[CF]) || (exc.f_szap && y[0]);
    assign add = exc.ok && exc.f_add;
    assign arith = exc.ok && exc.f_arith;

    logic [17:0] rest;
    always_comb begin
        rest = flags_keep;
        rest[1] = 1'b1;
        if (exc.ok) begin
            if (exc.f_logic) begin
                rest[CF] = 1'b0;
                rest[OF] = 1'b0;
                rest[AF] = 1'b0;
                rest[ZF] = zf_l;
                rest[SF] = sf_l;
                rest[PF] = pf_l;
            end
            if (exc.f_cf) rest[CF] = fl_cf;
            if (exc.f_szap) begin
                rest[AF] = y[4];
                rest[ZF] = y[6];
                rest[SF] = y[7];
                rest[PF] = y[2];
            end
            if (exc.f_if) rest[IF] = exc.fl_set;
            if (exc.f_df) rest[DF] = exc.fl_set;
            // POPF and IRET: every flag a real-mode load writes, bits 14:0
            // but the reserved 1, 3 and 5; RF and VM stay.
            if (exc.f_load) begin
                rest[CF] = y[0];
                rest[PF] = y[2];
                rest[AF] = y[4];
                rest[ZF] = y[6];
                rest[SF] = y[7];
                rest[TF] = y[8];
                rest[IF] = y[9];
                rest[DF] = y[10];
                rest[OF] = y[11];
                rest[14:12] = y[14:12];
            end
            if (exc.f_clrit) begin
                rest[IF] = 1'b0;
                rest[TF] = 1'b0;
            end
            if (exc.md_fl) begin
                rest[CF] = md_flags[CF];
                rest[PF] = md_flags[PF];
                rest[AF] = md_flags[AF];
                rest[ZF] = md_flags[ZF];
                rest[SF] = md_flags[SF];
                rest[OF] = md_flags[OF];
            end
        end
    end
    always_comb begin
        flags_out = rest;
        flags_out[CF] = add ? sum[32] ^ exc.sub : rest[CF];
        flags_out[OF] = arith ? sum[32] ^ sum2[32] : rest[OF];
        flags_out[SF] = arith ? sum2[32] ^ p_top : rest[SF];
        flags_out[AF] = arith ? sum[4] ^ p[4] ^ exc.sub : rest[AF];
        flags_out[ZF] = arith ? add_zf : rest[ZF];
        flags_out[PF] = arith ? ~^sum[7:0] : rest[PF];
    end

    // ---- Branches ---------------------------------------------------------------
    // LOOP's count reaches zero when it was one, so the decision needs no sum.
    logic count_one, count_zero;
    assign count_one = x == 32'd1;
    assign count_zero = x == 32'd0;
    assign ind_target = sum[31:0];
    assign ind_eip = y;
    // A REPE or REPNE CMPS or SCAS element ends the instruction, branching
    // to the next, when its own compare's ZF says so.
    assign taken = exc.ok && ((exc.b_jcc && cc_true) || exc.b_jmp || exc.b_ind
                              || ((exc.b_loop || (exc.b_loope && flags_in[ZF]) || (exc.b_loopne && !flags_in[ZF]))
                                  && !count_one)
                              || (exc.b_jcxz && count_zero)
                              || (exc.b_rpe && !add_zf) || (exc.b_rpne && add_zf));
endmodule
