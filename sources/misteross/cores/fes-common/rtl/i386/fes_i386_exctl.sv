// SPDX-License-Identifier: MIT
// M2: EX's controls for the uop entering EX next (fes_i386_pkg::exctl_t).
// Decoding them a stage ahead keeps the opcode compares off EX's data paths:
// EX's adder operands, flag sources and result select see only registered
// one-hot selects (design rule 8). The bypass selects say which operands EX
// takes from the WB result register: those still pending with their
// producer one stage ahead (fes_i386_dops).
module fes_i386_exctl (
    input  fes_i386_pkg::pipe_t    s,
    output fes_i386_pkg::exctl_t   exc
);
    typedef fes_i386_pkg::uop_t uop_t;
    localparam logic [2:0] C_ALU = 3'd0, C_SHIFT = 3'd1, C_BRANCH = 3'd4, C_SEGMENT = 3'd5;

    uop_t       u;
    logic [2:0] cls;
    logic [4:0] op;
    logic       alu, arith, logic_op, incdec;
    assign u = s.uop;
    assign cls = 3'(u.cls);
    assign op = u.op;
    assign alu = cls == C_ALU;
    // ADD ADC SBB SUB CMP NEG; INC DEC; OR AND XOR TEST
    assign arith = alu && (op == 5'd0 || op == 5'd2 || op == 5'd3 || op == 5'd5 || op == 5'd7 || op == 5'd12);
    assign incdec = alu && (op == 5'd10 || op == 5'd11);
    assign logic_op = alu && (op == 5'd1 || op == 5'd4 || op == 5'd6 || op == 5'd8);

    fes_i386_pkg::exctl_t c;
    assign c.ok = u.ctl.fault == fes_i386_pkg::FAULT_NONE;
    assign c.byp_a = s.pend_a && s.dist_a == 3'd1;
    assign c.byp_b = s.pend_b && s.dist_b == 3'd1;
    assign c.byp_c = s.pend_c && s.dist_c == 3'd1;
    assign c.size8 = u.size == fes_i386_pkg::SIZE_8;
    assign c.size16 = u.size == fes_i386_pkg::SIZE_16;
    assign c.src8 = u.src_size == fes_i386_pkg::SIZE_8;
    assign c.src16 = u.src_size == fes_i386_pkg::SIZE_16;
    assign c.sub = (alu && (op == 5'd3 || op == 5'd5 || op == 5'd7 || op == 5'd11 || op == 5'd12)) || cls == C_BRANCH;
    assign c.x_zero = alu && op == 5'd12;
    assign c.y_one = incdec || cls == C_BRANCH;
    assign c.y_a = alu && op == 5'd12;
    assign c.cin_cf = alu && op == 5'd2;
    assign c.cin_ncf = alu && op == 5'd3;
    assign c.r_add = arith || incdec || cls == C_BRANCH;
    assign c.r_logic = logic_op || (alu && op == 5'd13);
    assign c.r_shift = cls == C_SHIFT;
    assign c.r_lea = alu && op == 5'd14;
    assign c.r_ext = (alu && op == 5'd15) || cls == C_SEGMENT;
    assign c.r_movsx = alu && op == 5'd16;
    assign c.r_setcc = alu && op == 5'd17;
    assign c.r_lahf = alu && op == 5'd30;
    assign c.r_sign = alu && op == 5'd31;
    assign c.r_mov = alu && op == 5'd9;
    assign c.l_or = op == 5'd1;
    assign c.l_xor = op == 5'd6;
    assign c.l_not = op == 5'd13;
    assign c.f_add = arith;
    assign c.f_incdec = incdec;
    assign c.f_logic = logic_op;
    assign c.f_shift = cls == C_SHIFT;
    assign c.f_flagop = alu && op == 5'd30;
    assign c.b_jcc = cls == C_BRANCH && op == 5'd0;
    assign c.b_jmp = cls == C_BRANCH && op == 5'd1;
    assign c.b_loop = cls == C_BRANCH && op == 5'd6;
    assign c.b_loope = cls == C_BRANCH && op == 5'd7;
    assign c.b_loopne = cls == C_BRANCH && op == 5'd8;
    assign c.b_jcxz = cls == C_BRANCH && op == 5'd9;
    assign exc = c;
endmodule
