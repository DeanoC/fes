// SPDX-License-Identifier: MIT
// M2: EX's controls for the uop entering EX next (fes_i386_pkg::exctl_t),
// and its operands x and y. Decoding them a stage ahead keeps the opcode
// compares off EX's data paths: EX's adder operands, flag sources and result
// select see only registered one-hot selects (design rule 8).
//
// x is src1 and y is src2, the immediate, or the constant an operation adds,
// each cut to the operation's size, its byte lane moved to bits 7:0. For the
// adder y is inverted when it subtracts, and NEG takes src1 as y. Above the
// size an add's x is ones and y zeros, so its carry out reaches bit 32 at every
// size. An operand still pending with its producer one stage ahead is left
// out (its inversion stays in): EX ORs it into x, or XORs it into y, from the
// WB result register through the bypass selects (fes_i386_dops). A memory
// operand is left out the same way, and EX takes it from the loaded value M2
// registers (pipe_t ld) through the same selects (xl, yl).
module fes_i386_exctl (
    input  fes_i386_pkg::pipe_t    s,
    output fes_i386_pkg::exctl_t   exc,
    output logic [31:0]            x,
    output logic [31:0]            y
);
    typedef fes_i386_pkg::uop_t uop_t;
    localparam logic [2:0] C_ALU = 3'd0, C_SHIFT = 3'd1, C_MUL = 3'd2, C_DIV = 3'd3, C_BRANCH = 3'd4, C_SEGMENT = 3'd5;

    function automatic logic [31:0] extract(input logic [31:0] v, input logic s8, input logic s16, input logic hi8);
        extract = s8 ? {24'b0, hi8 ? v[15:8] : v[7:0]} : s16 ? {16'b0, v[15:0]} : v;
    endfunction

    uop_t       u;
    logic [2:0] cls;
    logic [4:0] op;
    logic       alu, arith, logic_op, incdec, neg, lnot, ext, size8, size16, src8, src16, w8, w16, bt, scan;
    assign u = s.uop;
    assign cls = 3'(u.cls);
    assign op = u.op;
    assign alu = cls == C_ALU;
    // ADD ADC SBB SUB CMP NEG; INC DEC; OR AND XOR TEST
    assign arith = alu && (op == 5'd0 || op == 5'd2 || op == 5'd3 || op == 5'd5 || op == 5'd7 || op == 5'd12);
    assign incdec = alu && (op == 5'd10 || op == 5'd11);
    assign logic_op = alu && (op == 5'd1 || op == 5'd4 || op == 5'd6 || op == 5'd8);
    assign neg = alu && op == 5'd12;
    assign lnot = alu && op == 5'd13;
    assign bt = alu && op >= 5'd19 && op <= 5'd22;   // BT, BTS, BTR, BTC
    assign scan = alu && (op == 5'd23 || op == 5'd24);  // BSF, BSR
    assign size8 = u.size == fes_i386_pkg::SIZE_8;
    assign size16 = u.size == fes_i386_pkg::SIZE_16;
    assign src8 = u.src_size == fes_i386_pkg::SIZE_8;
    assign src16 = u.src_size == fes_i386_pkg::SIZE_16;
    // MOVZX, MOVSX, SIGN, BITDISP and a segment read take src2 at its own size.
    assign ext = (alu && (op == 5'd15 || op == 5'd16 || op == 5'd31 || op == 5'd18)) || cls == C_SEGMENT;
    assign w8 = ext ? src8 : size8;
    assign w16 = ext ? src16 : size16;

    fes_i386_pkg::exctl_t c;
    logic sub, y_one, fill, byp_a, byp_b, ya, h8;
    logic [31:0] mask, an, bn;
    assign c.ok = u.ctl.fault == fes_i386_pkg::FAULT_NONE;
    logic lda, ldb;  // the memory operand is src1, src2
    assign lda = (u.mem.kind == fes_i386_pkg::MEM_LOAD || u.mem.kind == fes_i386_pkg::MEM_RMW) && u.mem.in_a;
    assign ldb = u.mem.kind == fes_i386_pkg::MEM_LOAD && !u.mem.in_a;
    assign byp_a = s.pend_a && s.dist_a == 3'd1 || lda;
    assign byp_b = s.pend_b && s.dist_b == 3'd1 || ldb;
    assign c.byp_a = s.pend_a && s.dist_a == 3'd1;
    assign c.xl = lda;
    assign c.yl = ya ? lda : ldb;
    // A bit test's bit number is src2 or imm, through the shifter's count
    // and its bypass from WB.
    assign c.byp_c = bt ? s.pend_b && s.dist_b == 3'd1 : s.pend_c && s.dist_c == 3'd1;
    assign c.size8 = size8;
    assign c.size16 = size16;
    assign c.src8 = src8;
    assign c.src16 = src16;
    // RET and JMP r/m add CS's base, all of it, to the target EIP at the
    // operation's size: no subtract, no fill above the size.
    logic ind;
    assign ind = cls == C_BRANCH && (op == 5'd10 || op == 5'd11);
    assign sub = (alu && (op == 5'd3 || op == 5'd5 || op == 5'd7 || op == 5'd11 || op == 5'd12))
              || (cls == C_BRANCH && !ind);
    assign c.sub = sub;
    assign c.cin_cf = alu && op == 5'd2;
    assign c.cin_ncf = alu && op == 5'd3;

    // ---- Operands -------------------------------------------------------------
    assign mask = size8 ? 32'h0000_00ff : size16 ? 32'h0000_ffff : 32'hffff_ffff;
    assign an = ind ? s.a : extract(s.a, size8, size16, u.src1.hi8);
    assign bn = extract(s.b, w8, w16, u.src2.hi8);
    assign y_one = incdec || (cls == C_BRANCH && !ind);
    assign ya = neg;                       // y is src1
    assign fill = arith || incdec;
    assign x = (neg || byp_a ? 32'd0 : an) | ({32{fill}} & ~mask);
    assign y = (y_one ? 32'd1 : lnot ? mask : ya ? (byp_a ? 32'd0 : an) : byp_b ? 32'd0 : bn) ^ ({32{sub}} & mask);
    assign h8 = size8 && u.src1.hi8;
    assign c.xw_lo = byp_a && !neg && !h8;
    assign c.xw_hi = byp_a && !neg && h8;
    assign c.xw16 = byp_a && !neg && !size8;
    assign c.xw32 = byp_a && !neg && !size8 && !size16;
    // y bypasses src1 for NEG, src2 for anything else that reads it.
    logic yb, yh;
    assign yb = !y_one && !lnot && (ya ? byp_a : byp_b);
    assign yh = ya ? h8 : w8 && u.src2.hi8;
    assign c.yw_lo = yb && !yh;
    assign c.yw_hi = yb && yh;
    assign c.yw16 = yb && !(ya ? size8 : w8);
    assign c.yw32 = yb && !(ya ? size8 || size16 : w8 || w16);

    // ---- The multiply and divide unit (fes_i386_muldiv) ------------------------
    // Its uops take their whole result, already merged into the destination's
    // old value, from the pipe record's result field, which fes_i386_cpu
    // fills from the unit, through LEA's select; and their flags from the unit
    // where they write any (not HI or LOAD). HI's result is the kept half,
    // which the lanes merge into EDX by size as any other. All but HI and
    // LOAD work over several cycles.
    logic md, md_step;
    assign md = cls == C_MUL || cls == C_DIV;
    assign md_step = op == 5'd4 || op == 5'd5;  // MULDIV_HI, MULDIV_LOAD
    assign c.md = md;
    assign c.md_fl = md && u.flags.write.oszap;
    assign c.md_wait = md && !md_step;
    assign c.md_de = cls == C_DIV && !md_step;

    // ---- The result's lanes -----------------------------------------------------
    logic r_add, r_shift, r_other, dh8, whole;
    assign r_add = arith || incdec || cls == C_BRANCH;
    assign r_shift = cls == C_SHIFT;
    assign r_other = !r_add && !r_shift;
    assign dh8 = size8 && u.dst.hi8;
    assign whole = !size8 && !size16 || md && op != 5'd4;  // MULDIV_HI
    assign c.add_lo = r_add && !dh8;
    assign c.add_mid = r_add && !size8;
    assign c.add_h8 = r_add && dh8;
    assign c.add_hi = r_add && !size8 && !size16;
    assign c.sh_lo = r_shift && !dh8;
    assign c.sh_mid = r_shift && !size8;
    assign c.sh_h8 = r_shift && dh8;
    assign c.sh_hi = r_shift && !size8 && !size16;
    assign c.ot_lo = r_other && !dh8;
    assign c.ot_mid = r_other && (!size8 || md);
    assign c.ot_h8 = r_other && dh8;
    assign c.ot_hi = r_other && whole;
    assign c.kp_lo = dh8;
    assign c.kp_mid = size8 && !dh8 && !md;
    assign c.kp_hi = !whole;
    assign c.r_logic = logic_op || lnot;
    assign c.r_lea = alu && op == 5'd14 || md;
    assign c.r_y = (alu && (op == 5'd15 || op == 5'd9)) || cls == C_SEGMENT;
    assign c.r_movsx = alu && op == 5'd16;
    assign c.r_setcc = alu && op == 5'd17;
    assign c.r_sign = alu && op == 5'd31;
    assign c.l_or = op == 5'd1;
    assign c.l_xor = op == 5'd6 || op == 5'd13;   // NOT is x XOR the size's ones
    assign c.r_bt = bt;
    assign c.bt_or = op == 5'd20;    // BTS
    assign c.bt_andn = op == 5'd21;  // BTR; BTC flips
    assign c.r_btd = alu && op == 5'd18;
    assign c.r_scan = scan;
    assign c.bsr = op == 5'd24;

    // ---- Flags --------------------------------------------------------------------
    // The flag instructions: CLC STC CMC CLI STI CLD STD SAHF (LAHF writes none).
    logic       flagop;
    logic [3:0] fop;
    assign flagop = alu && op == 5'd30;
    assign fop = u.imm[3:0];
    assign c.r_lahf = flagop && fop == 4'd8;
    assign c.r_flags = flagop && (fop == 4'd10 || fop == 4'd11);
    // An arithmetic or logic op writes the flags its uop says it writes:
    // XCHG's XORs and SALC's SBB write none.
    assign c.f_add = arith && u.flags.write.cf;
    assign c.f_arith = (arith || incdec) && u.flags.write.oszap;
    assign c.f_logic = logic_op && u.flags.write.oszap;
    assign c.f_shift = cls == C_SHIFT;
    assign c.f_cf = flagop && (fop <= 4'd2 || fop == 4'd7);
    assign c.f_if = flagop && (fop == 4'd3 || fop == 4'd4);
    assign c.f_df = flagop && (fop == 4'd5 || fop == 4'd6);
    assign c.f_szap = flagop && fop == 4'd7;
    assign c.f_load = flagop && fop == 4'd9;
    assign c.f_clrit = flagop && fop == 4'd11;
    assign c.fl_cf = fop == 4'd1;
    assign c.fl_cmc = fop == 4'd2;
    assign c.fl_set = fop == 4'd4 || fop == 4'd6;
    assign c.f_bt = bt && u.flags.write.cf;

    assign c.b_jcc = cls == C_BRANCH && op == 5'd0;
    assign c.b_jmp = cls == C_BRANCH && op == 5'd1;
    assign c.b_loop = cls == C_BRANCH && op == 5'd6;
    assign c.b_loope = cls == C_BRANCH && op == 5'd7;
    assign c.b_loopne = cls == C_BRANCH && op == 5'd8;
    assign c.b_jcxz = cls == C_BRANCH && op == 5'd9;
    assign c.b_rpe = u.rep_stop[0];
    assign c.b_rpne = u.rep_stop[1];
    assign c.b_ind = ind;

    // The shifter's selects (fes_i386_shift): direction, size and fill.
    localparam logic [4:0] ROL = 5'd0, ROR = 5'd1, RCL = 5'd2, RCR = 5'd3, SHL = 5'd4, SHR = 5'd5, SAL = 5'd6,
                           SAR = 5'd7, SHLD = 5'd8, SHRD = 5'd9;
    logic       shift, left;
    logic [2:0] z;
    logic [4:0] kind, count;
    assign shift = cls == C_SHIFT;
    assign left = op == ROL || op == RCL || op == SHL || op == SAL || op == SHLD;
    assign z = {u.size == fes_i386_pkg::SIZE_32, size16, size8};
    // The operand alone, rotate, rotate through carry, sign, the source.
    assign kind = {op == SHLD || op == SHRD, op == SAR, op == RCL || op == RCR, op == ROL || op == ROR, 1'b1};
    logic [29:0] sel;
    always_comb begin
        for (int d = 0; d < 2; d++)
            for (int k = 0; k < 3; k++)
                for (int f = 0; f < 5; f++)
                    sel[d * 15 + k * 5 + f] = shift && left == (d == 1) && z[k] && kind[f];
    end
    assign count = bt ? s.b[4:0] : u.count_cl ? s.c[4:0] : u.imm[4:0];
    assign c.sh.sel = sel;
    assign c.sh.left = left;
    assign c.sh.z8 = z[0];
    assign c.sh.z16 = z[1];
    assign c.sh.z32 = z[2];
    assign c.sh.count = count;
    assign c.sh.s = left ? ~count : count;
    assign c.sh.cf_rol = op == ROL;
    assign c.sh.cf_ror = op == ROR;
    assign c.sh.cf_shl = op == SHL || op == SAL;
    assign c.sh.cf_shr = op == SHR;
    assign c.sh.szap = op[4:2] != 3'b000;
    assign exc = c;
endmodule
