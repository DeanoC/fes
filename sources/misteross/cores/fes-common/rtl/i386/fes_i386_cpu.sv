// SPDX-License-Identifier: MIT
// fes_i386_cpu as far as Phase 1's register-only slice has built it
// (docs/i386-cpu-design.md): the front end (F1 to Q) on a memory port, then
// D, AG, M1, M2, EX and WB for the one-uop register and immediate forms that
// fes_i386_decode lists. M1 and M2 carry uops through; there is no data
// memory, cache, microcode or exception delivery yet.
//
//   DC  decode (fes_i386_decode, fes_i386_amode) from Q's output register
//   D   the GPR file and segment registers read; forwarding for data
//       operands (fes_i386_dops) and LEA's address (fes_i386_dfwd); EIP
//   AG  LEA's address and a branch's target (fes_i386_agu)
//   EX  ALU, shifter, flags, branch resolution (fes_i386_ex)
//   WB  in-order commit
//
// Branches are predicted not taken. EX resolves them; a taken one redirects
// F1 from WB and flushes every younger uop, which costs 10 cycles. No stage
// behind D holds, so D's forwarding tables only need clearing on a flush.
//
// A fault (#UD, #GP(0)) or an encoding this build does not implement stops
// the CPU at WB with nothing retired, and so does HLT after it retires;
// delivery waits for the microcode slice.
//
// init_* is the architectural state reset loads. A machine ties it to the
// 386 reset state; the lockstep harness loads a test's initial state.
// trace_* and arch_* show each retirement: on the edge an instruction
// retires, trace_retire rises and arch_* hold the state after it.
module fes_i386_cpu (
    input  logic             clk,
    input  logic             reset,
    input  logic [7:0][31:0] init_gpr,
    input  logic [31:0]      init_eflags,
    input  logic [31:0]      init_eip,
    input  logic [5:0][15:0] init_sel,     // fes_i386_pkg::seg_e order
    input  logic [5:0][31:0] init_base,
    input  logic [5:0][31:0] init_limit,
    input  logic [5:0][15:0] init_ar,      // descriptor bits 8-23, the limit nibble clear
    // Fetch: the L1I's way-selected block, an M10K with address stall whose
    // data may arrive late (mem_valid).
    output logic [27:0]      mem_block,
    output logic             mem_load,
    input  logic [15:0][7:0] mem_data,
    input  logic             mem_valid,
    output logic             trace_retire,
    output logic             trace_halt,           // stopped: HLT retired or a fault was raised
    output logic             trace_fault,          // ... a fault, which is not delivered yet
    output logic [7:0]       trace_vector,
    output logic             trace_unimplemented,  // ... on an encoding not built yet
    output logic [7:0][31:0] arch_gpr,
    output logic [31:0]      arch_eflags,
    output logic [31:0]      arch_eip,
    output logic [5:0][15:0] arch_sel,
    output logic [5:0][31:0] arch_base,
    output logic [5:0][31:0] arch_limit,
    output logic [5:0][15:0] arch_ar
);
    typedef fes_i386_pkg::pipe_t pipe_t;
    typedef fes_i386_pkg::uop_t uop_t;
    typedef fes_i386_pkg::fetched_t fetched_t;
    typedef fes_i386_pkg::decoded_t decoded_t;

    localparam logic [1:0] EARLY = 2'd0, LATE = 2'd2;
    localparam int SEG_CS = 1;

    // ---- Architectural state, written at WB -------------------------------------
    logic [7:0][31:0] gpr;
    logic [17:0]      eflags;
    logic [31:0]      eip;
    logic [5:0][15:0] seg_sel, seg_ar, seg_hi1;  // seg_hi1: base[31:16] + 1
    logic [5:0][31:0] seg_base, seg_limit;
    logic             code32;
    assign code32 = seg_ar[SEG_CS][14];

    // ---- Pipeline registers -------------------------------------------------------
    pipe_t ag_s, m1_s, m2_s, ex_s, wb_s;
    logic  wb_flush, wb_stop;
    // A uop that retires, faults or stops in WB this cycle, and whether it
    // flushes everything younger.
    assign wb_stop = wb_s.valid && (wb_s.uop.ctl.fault != fes_i386_pkg::FAULT_NONE
                                    || (wb_s.uop.cls == fes_i386_pkg::UOP_SPECIAL && wb_s.uop.op == 5'd1));
    assign wb_flush = wb_s.valid && wb_s.taken || wb_stop;

    // ---- Start and redirect -------------------------------------------------------
    // After reset the front end starts at CS.base + EIP, one cycle after the
    // sum is registered.
    logic [1:0]  start;
    logic [31:0] start_linear;
    logic        redirect;
    logic [31:0] redirect_linear, redirect_eip;
    always_ff @(posedge clk) begin
        if (reset) begin
            start <= 2'd1;
        end else if (start != 2'd0) begin
            start <= start + 2'd1;
        end
        start_linear <= seg_base[SEG_CS] + eip;
    end
    assign redirect = start == 2'd2 || (wb_s.valid && wb_s.taken);
    assign redirect_linear = start == 2'd2 ? start_linear : wb_s.target;
    assign redirect_eip = start == 2'd2 ? eip : wb_s.target_eip;

    // ---- F1 to Q ------------------------------------------------------------------
    logic     q_valid, d_ready;
    fetched_t q;
    fes_i386_frontend frontend (
        .clk, .reset, .redirect, .redirect_linear, .code32, .mem_block, .mem_load, .mem_data, .mem_valid,
        .q_valid, .q, .d_ready
    );

    // ---- DC ---------------------------------------------------------------------
    // Q's output register is decoded into a two-entry buffer: dc holds the
    // instruction D issues, and dc_skid takes Q's instruction when dc cannot.
    // Q may load its output whenever dc_skid is empty, so D's stall stays out
    // of Q's loop, and everything D's issue waits on starts from a register.
    decoded_t dc, dc_skid, dc_q;
    logic     dc_valid, dc_skid_valid;
    logic     d_stop;   // D issued a uop nothing after which can retire
    logic     issue;
    logic     dc_load;  // dc takes the next instruction
    logic     dc_branch, dc_stack;  // not used yet
    fes_i386_decode decode (
        .q, .code32, .uop(dc_q.uop), .opsize32(dc_q.opsize32), .lea(dc_q.lea), .lea_early(dc_q.lea_early),
        .lea_disp(dc_q.lea_disp), .branch(dc_branch), .consumes(dc_q.consumes), .reads_seg(dc_q.reads_seg),
        .seg_index(dc_q.seg_index), .stop(dc_q.stop)
    );
    fes_i386_amode amode (
        .modrm(q.bytes[1]), .sib(q.bytes[2]), .addr32(dc_q.uop.mem.addr32), .has_base(dc_q.has_base),
        .base(dc_q.base), .has_index(dc_q.has_index), .index(dc_q.index), .scale(dc_q.scale), .stack(dc_stack)
    );
    assign dc_q.length = q.length;
    assign d_ready = !dc_skid_valid;
    assign dc_load = !dc_valid || issue;
    always_ff @(posedge clk) begin
        if (reset || redirect) begin
            dc_valid <= 1'b0;
            dc_skid_valid <= 1'b0;
        end else begin
            if (dc_load) dc_valid <= dc_skid_valid || q_valid;
            if (dc_skid_valid) begin
                if (dc_load) dc_skid_valid <= 1'b0;
            end else if (q_valid && !dc_load) begin
                dc_skid_valid <= 1'b1;
            end
        end
        if (dc_load) dc <= dc_skid_valid ? dc_skid : dc_q;
        if (!dc_skid_valid) dc_skid <= dc_q;
    end

    // ---- D ------------------------------------------------------------------------
    logic cur_valid;
    assign cur_valid = dc_valid && !d_stop;

    // EIP of the instruction in D, and of the next. In 16-bit code an
    // instruction whose next EIP passes FFFFh is FAULT_UNIMPLEMENTED: fetch
    // would have to wrap to the segment's start, and the CS-limit check that
    // goes with it is not built yet (wrap16). Such a uop keeps its
    // destination, which nothing reads: it stops at WB, which writes nothing
    // for a fault, and D issues nothing after it (d_stop).
    logic [31:0] d_eip, d_eip_next;
    logic [16:0] eip16;
    logic        wrap16;
    assign eip16 = {1'b0, d_eip[15:0]} + {13'd0, dc.length};
    assign wrap16 = !code32 && eip16[16];
    assign d_eip_next = code32 ? d_eip + {28'd0, dc.length} : {16'd0, eip16[15:0]};

    uop_t        u;
    logic        opsize32, lea, lea_early, consumes, reads_seg, stop;
    logic [2:0]  seg_index;
    logic [31:0] lea_disp;
    always_comb begin
        u = dc.uop;
        if (wrap16 && dc.uop.ctl.fault == fes_i386_pkg::FAULT_NONE) u.ctl.fault = fes_i386_pkg::FAULT_UNIMPLEMENTED;
    end
    assign opsize32 = dc.opsize32;
    assign lea = dc.lea;
    assign lea_early = dc.lea_early;
    assign lea_disp = dc.lea_disp;
    assign consumes = dc.consumes;
    assign reads_seg = dc.reads_seg;
    assign seg_index = dc.seg_index;
    assign stop = dc.stop || wrap16;

    // The producers in AG, M1, M2 and EX.
    logic [3:0]       prod_valid, prod_early;
    logic [3:0][2:0]  prod_dst;
    logic [3:0][1:0]  prod_kind;
    logic [3:0][31:0] stage_value;
    pipe_t            prod_ag, prod_m1, prod_m2, prod_ex;  // whole records: DeanoC/yosys#52
    assign prod_ag = ag_s;
    assign prod_m1 = m1_s;
    assign prod_m2 = m2_s;
    assign prod_ex = ex_s;
    always_comb begin
        prod_valid = {prod_ex.valid && prod_ex.uop.dst.valid && prod_ex.uop.dst.space == fes_i386_pkg::RS_GPR,
                      prod_m2.valid && prod_m2.uop.dst.valid && prod_m2.uop.dst.space == fes_i386_pkg::RS_GPR,
                      prod_m1.valid && prod_m1.uop.dst.valid && prod_m1.uop.dst.space == fes_i386_pkg::RS_GPR,
                      prod_ag.valid && prod_ag.uop.dst.valid && prod_ag.uop.dst.space == fes_i386_pkg::RS_GPR};
        prod_dst = {prod_ex.uop.dst.index[2:0], prod_m2.uop.dst.index[2:0], prod_m1.uop.dst.index[2:0],
                    prod_ag.uop.dst.index[2:0]};
        prod_early = {prod_ex.early, prod_m2.early, prod_m1.early, prod_ag.early};
        prod_kind = {prod_ex.early ? EARLY : LATE, prod_m2.early ? EARLY : LATE,
                     prod_m1.early ? EARLY : LATE, prod_ag.early ? EARLY : LATE};
        stage_value = {wb_s.result, ex_s.result, m2_s.result, m1_s.result};
    end

    logic d_writes;
    assign d_writes = cur_valid && u.dst.valid && u.dst.space == fes_i386_pkg::RS_GPR;

    // Data operands.
    logic [31:0] a_val, b_val, c_val;
    logic        a_pend, b_pend, c_pend;
    logic [2:0]  a_dist, b_dist, c_dist;
    fes_i386_dops dops (
        .clk, .reset(reset || wb_flush), .a_reg(u.src1.index[2:0]), .b_reg(u.src2.index[2:0]), .d_writes,
        .d_dst(u.dst.index[2:0]), .d_early(lea && lea_early), .advance(issue), .prod_valid, .prod_dst,
        .prod_early, .gpr, .stage_value, .a(a_val), .b(b_val), .c(c_val), .pend_a(a_pend), .pend_b(b_pend),
        .pend_c(c_pend), .dist_a(a_dist), .dist_b(b_dist), .dist_c(c_dist)
    );

    // LEA's address.
    logic        lea_stall;
    logic [1:0]  late, late_hi;
    logic [31:0] row, last;
    fes_i386_dfwd dfwd (
        .clk, .reset(reset || wb_flush), .has_base(lea && dc.has_base), .base(dc.base),
        .has_index(lea && dc.has_index), .index(dc.index), .scale(dc.scale), .addr32(u.mem.addr32), .d_writes, .d_dst(u.dst.index[2:0]),
        .d_kind(lea && lea_early ? EARLY : LATE), .advance(issue), .prod_valid, .prod_dst, .prod_kind, .gpr,
        .stage_value, .row, .last, .late, .late_hi, .stall(lea_stall)
    );

    // A selector read waits for an older write of that segment register.
    function automatic logic writes_seg(input pipe_t s, input logic [2:0] seg);
        writes_seg = s.valid && s.uop.dst.valid && s.uop.dst.space == fes_i386_pkg::RS_SEG
                  && s.uop.dst.index[2:0] == seg;
    endfunction
    logic seg_hazard;
    assign seg_hazard = writes_seg(ag_s, seg_index) || writes_seg(m1_s, seg_index) || writes_seg(m2_s, seg_index)
                     || writes_seg(ex_s, seg_index) || writes_seg(wb_s, seg_index);

    // A flag reader waits one cycle behind a shift or rotate, whose flags
    // reach EX's running copy a cycle after the shift leaves EX (fes_i386_ex).
    logic flag_hazard;
    assign flag_hazard = ag_s.valid && ag_s.uop.cls == fes_i386_pkg::UOP_SHIFT
                      && consumes;

    assign issue = cur_valid && !(lea && lea_stall) && !(reads_seg && seg_hazard) && !flag_hazard && !wb_flush;

    always_ff @(posedge clk) begin
        if (reset) begin
            d_stop <= 1'b0;
        end else if (redirect) begin
            d_stop <= 1'b0;
            d_eip <= redirect_eip;
        end else if (issue) begin
            d_stop <= stop;
            d_eip <= d_eip_next;
        end
    end

    // ---- D/AG register --------------------------------------------------------------
    logic        ag_addr32;
    logic [31:0] ag_row, ag_last, ag_disp, ag_seg_term;
    logic [15:0] ag_seg_hi, ag_seg_hi_plus1;
    logic [1:0]  ag_late, ag_late_hi;
    pipe_t       d_out;
    always_comb begin
        d_out = '0;
        d_out.valid = issue;
        d_out.uop = u;
        d_out.eip = d_eip;
        d_out.eip_next = d_eip_next;
        d_out.a = a_val;
        d_out.b = reads_seg ? {16'b0, seg_sel[seg_index]} : u.src2.valid ? b_val : u.imm;
        d_out.c = c_val;
        d_out.pend_a = u.src1.valid && a_pend;
        d_out.pend_b = u.src2.valid && b_pend;
        d_out.pend_c = u.count_cl && c_pend;
        d_out.dist_a = a_dist;
        d_out.dist_b = b_dist;
        d_out.dist_c = c_dist;
        d_out.early = lea && lea_early;
    end
    always_ff @(posedge clk) begin
        if (reset || wb_flush) begin
            ag_s.valid <= 1'b0;
        end else begin
            ag_s <= d_out;
        end
        if (lea) begin
            ag_addr32 <= u.mem.addr32;
            ag_row <= row;
            ag_last <= last;
            ag_late <= late;
            ag_late_hi <= late_hi;
            ag_disp <= lea_disp;
            ag_seg_term <= 32'd0;
            ag_seg_hi <= 16'd0;
            ag_seg_hi_plus1 <= 16'd0;
        end else begin
            // A branch's target: CS.base + EIP + length + displacement,
            // wrapped by operand size.
            ag_addr32 <= opsize32;
            ag_row <= opsize32 ? d_eip : {16'b0, d_eip[15:0]};
            ag_last <= {28'd0, dc.length};
            ag_late <= 2'd0;
            ag_late_hi <= 2'd0;
            ag_disp <= u.imm;
            ag_seg_term <= opsize32 ? seg_base[SEG_CS] : {16'b0, seg_base[SEG_CS][15:0]};
            ag_seg_hi <= seg_base[SEG_CS][31:16];
            ag_seg_hi_plus1 <= seg_hi1[SEG_CS];
        end
    end

    // ---- AG -----------------------------------------------------------------------
    // A pending operand whose producer is now in WB is taken from the WB
    // register (dist 4 at AG, 3 at M1, 2 at M2; at dist 1 EX selects it).
    function automatic pipe_t capture(input pipe_t s, input logic [2:0] at, input logic [31:0] wb_value);
        pipe_t t;
        t = s;
        if (s.pend_a && s.dist_a == at) begin t.a = wb_value; t.pend_a = 1'b0; end
        if (s.pend_b && s.dist_b == at) begin t.b = wb_value; t.pend_b = 1'b0; end
        if (s.pend_c && s.dist_c == at) begin t.c = wb_value; t.pend_c = 1'b0; end
        capture = t;
    endfunction

    logic [31:0] linear, offset;
    fes_i386_agu agu (
        .addr32(ag_addr32), .seg_term(ag_seg_term), .seg_hi(ag_seg_hi), .seg_hi_plus1(ag_seg_hi_plus1),
        .row(ag_row), .last(ag_last), .disp(ag_disp), .late(ag_late), .late_hi(ag_late_hi),
        .late_m1(m1_s.result), .late_ex(ex_s.result), .late_wb(wb_s.result), .linear, .offset
    );

    pipe_t ag_out;
    always_comb begin
        ag_out = capture(ag_s, 3'd4, wb_s.result);
        ag_out.result = offset;
        ag_out.target = linear;
        ag_out.target_eip = offset;
    end
    always_ff @(posedge clk) begin
        if (reset || wb_flush) begin
            m1_s.valid <= 1'b0;
        end else begin
            m1_s <= ag_out;
        end
    end

    // ---- M1 and M2: no memory yet -------------------------------------------------
    // M2 also decodes EX's controls (fes_i386_exctl).
    pipe_t                m2_cap, m2_out;
    fes_i386_pkg::exctl_t m2_exc;
    assign m2_cap = capture(m2_s, 3'd2, wb_s.result);
    fes_i386_exctl exctl (.s(m2_cap), .exc(m2_exc));
    always_comb begin
        m2_out = m2_cap;
        m2_out.exc = m2_exc;
    end
    always_ff @(posedge clk) begin
        if (reset || wb_flush) begin
            m2_s.valid <= 1'b0;
            ex_s.valid <= 1'b0;
        end else begin
            m2_s <= capture(m1_s, 3'd3, wb_s.result);
            ex_s <= m2_out;
        end
    end

    // ---- EX -----------------------------------------------------------------------
    // EFLAGS after every uop that has passed EX: no younger uop changes it,
    // and a flush only kills uops younger than WB, which have not. A shift
    // in WB still owes its flags (wb_flags): ex_flags lacks them for that
    // cycle, which no flag reader sees (flag_hazard), and takes them at its
    // end.
    logic [17:0] ex_flags, ex_flags_out, wb_flags, flags_keep;
    logic [31:0] ex_result;
    logic        ex_taken, ex_sh_cf, ex_sh_of, ex_def_co, ex_def_szap;
    pipe_t       ex_out;

    logic [7:0] wb_r8;
    assign wb_r8 = wb_s.uop.dst.hi8 ? wb_s.result[15:8] : wb_s.result[7:0];
    always_comb begin
        wb_flags = wb_s.eflags;
        if (wb_s.def_co) begin
            wb_flags[0] = wb_s.sh_cf;
            wb_flags[11] = wb_s.sh_of;
        end
        if (wb_s.def_szap) begin
            wb_flags[2] = ~^wb_r8;
            wb_flags[4] = 1'b1;
            wb_flags[6] = wb_s.exc.size8 ? wb_r8 == 8'd0 : wb_s.exc.size16 ? wb_s.result[15:0] == 16'd0
                                                                            : wb_s.result == 32'd0;
            wb_flags[7] = wb_s.exc.size8 ? wb_r8[7] : wb_s.exc.size16 ? wb_s.result[15] : wb_s.result[31];
        end
    end
    assign flags_keep = wb_s.valid && (wb_s.def_co || wb_s.def_szap) ? wb_flags : ex_flags;

    // An operand whose producer is one stage ahead (exc.byp_*) comes from the
    // WB result register beside EX's operand.
    fes_i386_ex ex (
        .u(ex_s.uop), .exc(ex_s.exc), .a(ex_s.a), .b(ex_s.b), .c(ex_s.c), .wb(wb_s.result),
        .ag_result(ex_s.result), .flags_in(ex_flags), .flags_keep, .result(ex_result), .flags_out(ex_flags_out),
        .taken(ex_taken), .sh_cf(ex_sh_cf), .sh_of(ex_sh_of), .def_co(ex_def_co), .def_szap(ex_def_szap)
    );
    always_comb begin
        ex_out = ex_s;
        ex_out.result = ex_s.early ? ex_s.result : ex_result;
        ex_out.eflags = ex_flags_out;
        ex_out.taken = ex_taken;
        ex_out.sh_cf = ex_sh_cf;
        ex_out.sh_of = ex_sh_of;
        ex_out.def_co = ex_def_co;
        ex_out.def_szap = ex_def_szap;
    end
    always_ff @(posedge clk) begin
        if (reset) begin
            ex_flags <= {init_eflags[17:2], 1'b1, init_eflags[0]};
        end else if (ex_s.valid && !wb_flush) begin
            ex_flags <= ex_flags_out;
        end else begin
            ex_flags <= flags_keep;
        end
        if (reset || wb_flush) begin
            wb_s.valid <= 1'b0;
        end else begin
            wb_s <= ex_out;
        end
    end

    // ---- WB -----------------------------------------------------------------------
    logic halted;
    logic [2:0] wb_fault;
    assign wb_fault = 3'(wb_s.uop.ctl.fault);
    always_ff @(posedge clk) begin
        trace_retire <= 1'b0;
        if (reset) begin
            gpr <= init_gpr;
            eflags <= {init_eflags[17:2], 1'b1, init_eflags[0]};
            eip <= init_eip;
            seg_sel <= init_sel;
            seg_base <= init_base;
            seg_limit <= init_limit;
            seg_ar <= init_ar;
            for (int k = 0; k < 6; k++) seg_hi1[k] <= init_base[k][31:16] + 16'd1;
            halted <= 1'b0;
            trace_fault <= 1'b0;
            trace_vector <= 8'd0;
            trace_unimplemented <= 1'b0;
        end else if (wb_s.valid && !halted) begin
            if (wb_fault != 3'd0) begin
                halted <= 1'b1;
                trace_fault <= wb_fault != 3'd4;
                trace_unimplemented <= wb_fault == 3'd4;
                trace_vector <= wb_fault == 3'd1 ? 8'd6 : wb_fault == 3'd2 ? 8'd13 : wb_fault == 3'd3 ? 8'd7 : 8'd0;
            end else begin
                trace_retire <= wb_s.uop.ctl.end_of_instruction;
                eflags <= wb_flags;
                eip <= wb_s.taken ? wb_s.target_eip : wb_s.eip_next;
                if (wb_s.uop.dst.valid && wb_s.uop.dst.space == fes_i386_pkg::RS_GPR)
                    gpr[wb_s.uop.dst.index[2:0]] <= wb_s.result;
                if (wb_s.uop.dst.valid && wb_s.uop.dst.space == fes_i386_pkg::RS_SEG) begin
                    // Real mode: the base is selector * 16; limit and attributes stay.
                    seg_sel[wb_s.uop.dst.index[2:0]] <= wb_s.result[15:0];
                    seg_base[wb_s.uop.dst.index[2:0]] <= {12'd0, wb_s.result[15:0], 4'd0};
                    seg_hi1[wb_s.uop.dst.index[2:0]] <= {11'd0, 5'(wb_s.result[15:12]) + (* no_carry *) 5'd1};
                end
                if (wb_stop) halted <= 1'b1;
            end
        end
    end
    assign trace_halt = halted;

    assign arch_gpr = gpr;
    assign arch_eflags = {14'd0, eflags};
    assign arch_eip = eip;
    assign arch_sel = seg_sel;
    assign arch_base = seg_base;
    assign arch_limit = seg_limit;
    assign arch_ar = seg_ar;
endmodule
