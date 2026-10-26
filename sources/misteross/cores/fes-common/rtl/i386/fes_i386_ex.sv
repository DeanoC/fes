// SPDX-License-Identifier: MIT
// EX: the late ALU, the funnel shifter, flags and branch resolution
// (docs/i386-cpu-design.md, "Execute"). One adder serves ADD, ADC, SUB, SBB,
// CMP, INC, DEC, NEG and LOOP's count; ZF of a sum comes from the operands,
// not from the sum, so no 32-input NOR follows the carry chain.
//
// a is the full value of src1, which is also the destination register: the
// result merges into it by size and byte lane, so EX hands WB the register's
// whole new value. b is src2 or the immediate; c is ECX for a CL count. An
// operand whose producer is one stage ahead comes from the WB result
// register (wb) instead. flags_in is EX's running copy of EFLAGS, which Jcc,
// SETcc, ADC, SBB, RCL, RCR and LAHF read with no forwarding; flags_keep is
// EFLAGS after every older uop, which a uop passes on where it writes no
// flag. They differ only in the cycle after a shift or rotate, when D holds
// any flag reader (fes_i386_cpu). A faulting uop changes nothing.
//
// A shift's flags leave the shifter off the flag path: EX keeps the old flags
// and hands WB the shifter's CF and OF and whether SF, ZF, AF and PF follow
// the result (sh_cf, sh_of, def_co, def_szap); fes_i386_cpu applies them
// from the WB register a cycle later.
//
// Every select here is a registered control from fes_i386_exctl, decoded in
// M2, so the opcode decode is off the data paths (design rule 8): the
// adder's operands take the bypass and byte extract, then their
// preparation; after the chain come the size's carry select and the flag
// and result selects.
//
// Results the manual leaves undefined follow the 386EX (Phase 0 divergence
// list): AND, OR, XOR and TEST clear AF; a shift by a nonzero count sets AF;
// shifts and rotates set OF and CF as fes_i386_shift says.
module fes_i386_ex (
    input  fes_i386_pkg::uop_t   u,
    input  fes_i386_pkg::exctl_t exc,
    input  logic [31:0]          a,
    input  logic [31:0]          b,
    input  logic [31:0]          c,
    input  logic [31:0]          wb,          // the WB result register
    input  logic [31:0]          ag_result,   // LEA's offset
    input  logic [17:0]          flags_in,
    input  logic [17:0]          flags_keep,
    output logic [31:0]          result,
    output logic [17:0]          flags_out,
    output logic                 taken,
    output logic                 sh_cf,
    output logic                 sh_of,
    output logic                 def_co,      // CF and OF are sh_cf and sh_of
    output logic                 def_szap     // SF, ZF and PF follow the result; AF is set
);
    localparam int CF = 0, PF = 2, AF = 4, ZF = 6, SF = 7, IF = 9, DF = 10, OF = 11;

    function automatic logic [31:0] extract(input logic [31:0] v, input logic s8, input logic s16, input logic hi8);
        extract = s8 ? {24'b0, hi8 ? v[15:8] : v[7:0]} : s16 ? {16'b0, v[15:0]} : v;
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

    logic [31:0] af, bf, cf_val, an, bn, bs, mask;
    assign af = exc.byp_a ? wb : a;
    assign bf = exc.byp_b ? wb : b;
    assign cf_val = exc.byp_c ? wb : c;
    assign an = extract(af, exc.size8, exc.size16, u.src1.hi8);
    assign bn = extract(bf, exc.size8, exc.size16, u.src2.hi8);
    assign bs = extract(bf, exc.src8, exc.src16, u.src2.hi8);
    assign mask = exc.size8 ? 32'h0000_00ff : exc.size16 ? 32'h0000_ffff : 32'hffff_ffff;

    // ---- Adder ----------------------------------------------------------------
    logic        cin, carry, top_x, top_y, top_s, add_cf, add_of, add_af, add_zf;
    logic [31:0] x, y;
    logic [32:0] sum;
    assign cin = exc.cin_cf ? flags_in[CF] : exc.cin_ncf ? !flags_in[CF] : exc.sub;
    assign x = exc.x_zero ? 32'd0 : an;
    assign y = (exc.y_one ? 32'd1 : exc.y_a ? an : bn) ^ ({32{exc.sub}} & mask);
    assign sum = {1'b0, x} + {1'b0, y} + {32'd0, cin};
    assign carry = exc.size8 ? sum[8] : exc.size16 ? sum[16] : sum[32];
    assign top_x = exc.size8 ? x[7] : exc.size16 ? x[15] : x[31];
    assign top_y = exc.size8 ? y[7] : exc.size16 ? y[15] : y[31];
    assign top_s = exc.size8 ? sum[7] : exc.size16 ? sum[15] : sum[31];
    assign add_cf = carry ^ exc.sub;
    assign add_of = (top_x == top_y) && (top_s != top_x);
    assign add_af = x[4] ^ y[4] ^ sum[4] ^ exc.sub;
    // The sum is zero when bit 0's propagate equals the carry in and every
    // higher bit's propagate equals the OR of the bit below it.
    logic [31:0] p, g;
    assign p = x ^ y;
    assign g = x | y;
    assign add_zf = &(~(p ^ {g[30:0], cin}) | ~mask);

    // ---- Logic ------------------------------------------------------------------
    logic [31:0] r_logic;
    assign r_logic = exc.l_or ? an | bn : exc.l_xor ? an ^ bn : exc.l_not ? ~an & mask : an & bn;

    // ---- Shifter ----------------------------------------------------------------
    logic [4:0]  count, s;
    logic [31:0] r_shift;
    logic        shifted;
    assign count = exc.sh.count_cl ? cf_val[4:0] : u.imm[4:0];
    assign s = exc.sh.count_cl ? cf_val[4:0] ^ {5{exc.sh.left}} : exc.sh.s_imm;
    assign shifted = exc.sh.count_cl ? cf_val[4:0] != 5'd0 : !exc.sh.imm_zero;
    fes_i386_shift shifter (
        .c(exc.sh), .count, .s, .a(an), .b(bn), .cf_in(flags_in[CF]), .r(r_shift), .cf(sh_cf), .of(sh_of)
    );
    assign def_co = exc.ok && exc.f_shift && shifted;
    assign def_szap = def_co && exc.sh.szap;

    // ---- Result -------------------------------------------------------------------
    logic [31:0] r_other, r;
    logic        cc_true;
    assign cc_true = cond(flags_in, u.ctl.cond);
    assign r_other = ({32{exc.r_logic}} & r_logic)
                   | ({32{exc.r_shift}} & r_shift)
                   | ({32{exc.r_lea}} & ag_result)
                   | ({32{exc.r_ext}} & bs)
                   | ({32{exc.r_movsx}} & (exc.src8 ? {{24{bs[7]}}, bs[7:0]} : {{16{bs[15]}}, bs[15:0]}))
                   | ({32{exc.r_setcc}} & {31'b0, cc_true})
                   | ({32{exc.r_lahf}} & {24'b0, flags_in[7:6], 1'b0, flags_in[4], 1'b0, flags_in[2], 1'b1, flags_in[0]})
                   | ({32{exc.r_sign}} & {32{exc.src8 ? bs[7] : exc.src16 ? bs[15] : bs[31]}})
                   | ({32{exc.r_mov}} & bn);
    assign r = exc.r_add ? sum[31:0] : r_other;

    // Merge into the destination's old value by size and byte lane.
    assign result = exc.size8 ? (u.dst.hi8 ? {af[31:16], r[7:0], af[7:0]} : {af[31:8], r[7:0]})
                  : exc.size16 ? {af[31:16], r[15:0]} : r;

    // ---- Flags ------------------------------------------------------------------
    // Each flag is an OR of its sources, each gated by a registered select,
    // or the old flag. SF, ZF and PF of a logic op come from the logic unit
    // alone, not the result select.
    logic       zf_l, sf_l, pf_l, fl_cf, fl_set;
    logic [3:0] fop;
    assign zf_l = (r_logic & mask) == 32'd0;
    assign sf_l = exc.size8 ? r_logic[7] : exc.size16 ? r_logic[15] : r_logic[31];
    assign pf_l = ~^r_logic[7:0];
    assign fop = u.imm[3:0];
    // The flag instructions: CLC STC CMC CLI STI CLD STD SAHF (LAHF writes none).
    logic f_cf, f_if, f_df, f_szap;
    assign f_cf = exc.f_flagop && (fop <= 4'd2 || fop == 4'd7);
    assign f_if = exc.f_flagop && (fop == 4'd3 || fop == 4'd4);
    assign f_df = exc.f_flagop && (fop == 4'd5 || fop == 4'd6);
    assign f_szap = exc.f_flagop && fop == 4'd7;
    assign fl_cf = fop == 4'd1 || (fop == 4'd2 && !flags_in[CF]) || (fop == 4'd7 && bn[0]);
    assign fl_set = fop == 4'd4 || fop == 4'd6;

    logic arith, any_cf, any_szap, any_of;
    assign arith = exc.f_add || exc.f_incdec;
    assign any_cf = exc.f_add || exc.f_logic || f_cf;
    assign any_of = arith || exc.f_logic;
    assign any_szap = arith || exc.f_logic || f_szap;
    always_comb begin
        flags_out = flags_keep;
        flags_out[1] = 1'b1;
        if (exc.ok) begin
            flags_out[CF] = any_cf ? (exc.f_add && add_cf) || (f_cf && fl_cf) : flags_keep[CF];
            flags_out[OF] = any_of ? arith && add_of : flags_keep[OF];
            flags_out[AF] = any_szap ? (arith && add_af) || (f_szap && bn[4]) : flags_keep[AF];
            flags_out[ZF] = any_szap ? (arith ? add_zf : f_szap ? bn[6] : zf_l) : flags_keep[ZF];
            flags_out[SF] = any_szap ? (arith ? top_s : f_szap ? bn[7] : sf_l) : flags_keep[SF];
            flags_out[PF] = any_szap ? (arith ? ~^sum[7:0] : f_szap ? bn[2] : pf_l) : flags_keep[PF];
            flags_out[IF] = f_if ? fl_set : flags_keep[IF];
            flags_out[DF] = f_df ? fl_set : flags_keep[DF];
        end
    end

    // ---- Branches ---------------------------------------------------------------
    // LOOP's count reaches zero when it was one, so the decision needs no sum.
    logic count_one, count_zero;
    assign count_one = an == 32'd1;
    assign count_zero = an == 32'd0;
    assign taken = exc.ok && ((exc.b_jcc && cc_true) || exc.b_jmp
                              || ((exc.b_loop || (exc.b_loope && flags_in[ZF]) || (exc.b_loopne && !flags_in[ZF]))
                                  && !count_one)
                              || (exc.b_jcxz && count_zero));
endmodule
