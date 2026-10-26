// SPDX-License-Identifier: MIT
// fes_i386_cpu as far as Phase 1's register-only slice has built it
// (docs/i386-cpu-design.md): the front end (F1 to Q) on a memory port, then
// D, AG, M1, M2, EX and WB for the one-uop register and immediate forms that
// fes_i386_decode lists, with their memory forms on a data port that stands
// for the L1D's output. There is no cache, microcode or exception delivery
// yet.
//
//   DC  decode (fes_i386_decode, fes_i386_amode) from Q's output register
//   D   the GPR file and segment registers read; forwarding for data
//       operands (fes_i386_dops) and LEA's address (fes_i386_dfwd); EIP
//   AG  a memory operand's or LEA's address, a branch's target (fes_i386_agu)
//   M1  the segment limit check; the data port's address
//   M2  the loaded bytes, aligned and merged with older stores not yet written
//   EX  ALU, shifter, flags, branch resolution (fes_i386_ex)
//   WB  in-order commit, stores included
//
// Branches are predicted not taken. EX resolves them; a taken one redirects
// F1 from WB and flushes every younger uop, which costs 10 cycles.
//
// A load whose bytes have not arrived when it is in M2 freezes D to WB: every
// stage behind DC keeps its uop, and nothing retires, until they do (freeze).
// Nothing moves relative to anything else, so the forwarding distances, AG's
// late operands, the store merge and D's forwarding tables all hold as they
// are; the tables only need clearing on a flush.
//
// A fault (#UD, #GP(0), #SS(0)) or an encoding this build does not implement stops
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
    // Data: the L1D's output for now, an M10K-like port. M1 drives a load's
    // address, the port registers it on the edge M1 advances, and the 8 bytes
    // from the address's dword, as memory held them on that edge, arrive in
    // M2 once dmem_valid says so. A retiring store writes the bytes dmem_wmask
    // names, from its address's dword, on the edge it retires. The port reads
    // before it writes on the same edge.
    output logic [31:0]      dmem_addr,
    output logic             dmem_read,
    input  logic [7:0][7:0]  dmem_rdata,
    input  logic             dmem_valid,
    output logic             dmem_write,
    output logic [31:0]      dmem_waddr,
    output logic [7:0][7:0]  dmem_wdata,
    output logic [7:0]       dmem_wmask,
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
    localparam int SEG_CS = 1, SEG_SS = 2;

    // ---- Architectural state, written at WB -------------------------------------
    logic [7:0][31:0] gpr;
    logic [17:0]      eflags;
    logic [31:0]      eip;
    logic [5:0][15:0] seg_sel, seg_ar, seg_hi1;  // seg_hi1: base[31:16] + 1
    logic [5:0][31:0] seg_base, seg_limit;
    logic [15:0]      t0;  // microcode temporaries: a far transfer's selector and offset
    logic [31:0]      t1;
    // Each segment's limit less 0, 1 and 3, for 1-, 2- and 4-byte accesses,
    // and whether that is below zero, so the limit check is one compare.
    // Real-mode loads keep the limit, so only reset loads them.
    logic [5:0][2:0][31:0] seg_lim;
    logic [5:0][2:0]       seg_tiny;
    logic             code32;
    assign code32 = seg_ar[SEG_CS][14];

    // ---- Pipeline registers -------------------------------------------------------
    pipe_t ag_s, m1_s, m2_s, ex_s, wb_s;
    logic  wb_flush, wb_stop;
    // A uop that retires, faults or stops in WB this cycle, and whether it
    // flushes everything younger.
    assign wb_stop = wb_s.valid && (wb_s.uop.ctl.fault != fes_i386_pkg::FAULT_NONE
                                    || (wb_s.uop.cls == fes_i386_pkg::UOP_SPECIAL && wb_s.uop.op == 5'd1));

    // A load in M2 waiting for its bytes freezes D to WB (m2_load: M2 holds a
    // load, registered as it enters; one that fails the limit check in M1
    // waits too, then faults). WB waits as well, so a taken branch or a fault
    // there takes effect, and flushes, once the bytes arrive, even when the
    // load is on the wrong path.
    logic freeze, m2_load;
    assign freeze = m2_load && !dmem_valid;
    assign wb_flush = !freeze && (wb_s.valid && wb_s.taken || wb_stop);

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
    assign redirect = start == 2'd2 || (!freeze && wb_s.valid && wb_s.taken);
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
    logic     dc_branch, dc_stack;  // dc_branch: not used yet
    logic     q_two;                // a two-byte opcode: the ModRM is byte 2
    assign q_two = q.bytes[0] == 8'h0f;
    logic [2:0] q_stk, q_seq;       // dc_q.stk and .seq, apart so no field of dc_q reads another
    fes_i386_decode decode (
        .q, .code32, .stack(dc_stack), .uop(dc_q.uop), .opsize32(dc_q.opsize32), .lea(dc_q.lea), .lea_early(dc_q.lea_early),
        .lea_disp(dc_q.lea_disp), .branch(dc_branch), .consumes(dc_q.consumes), .reads_seg(dc_q.reads_seg),
        .seg_index(dc_q.seg_index), .stop(dc_q.stop), .stk(q_stk), .push_eip(dc_q.push_eip),
        .cs_base(dc_q.cs_base), .seq(q_seq), .sel_part(dc_q.sel_part), .far_jmp(dc_q.far_jmp), .far_chk(dc_q.far_chk),
        .far_sel(dc_q.far_sel)
    );
    // A stack operation's address is ESP, or EBP for LEAVE, with no index:
    // chosen here, so D's operand lookups start from dc's register.
    logic       am_has_base, am_has_index;
    logic [2:0] am_base;
    logic [1:0] am_scale;
    fes_i386_amode amode (
        .modrm(q_two ? q.bytes[2] : q.bytes[1]), .sib(q_two ? q.bytes[3] : q.bytes[2]), .addr32(dc_q.uop.mem.addr32), .has_base(am_has_base),
        .base(am_base), .has_index(am_has_index), .index(dc_q.index), .scale(am_scale), .stack(dc_stack)
    );
    assign dc_q.stk = q_stk;
    assign dc_q.has_base = q_stk != fes_i386_pkg::STK_NONE || am_has_base;
    assign dc_q.base = q_stk == fes_i386_pkg::STK_LEAVE ? 3'd5 : q_stk != fes_i386_pkg::STK_NONE ? 3'd4 : am_base;
    assign dc_q.has_index = q_stk == fes_i386_pkg::STK_NONE && am_has_index;
    assign dc_q.scale = q_stk == fes_i386_pkg::STK_NONE ? am_scale : 2'd0;
    assign dc_q.length = q.length;
    assign dc_q.seq = q_seq;
    assign dc_q.step = 3'd0;
    assign dc_q.more = q_seq != fes_i386_pkg::SEQ_NONE;
    assign dc_q.far_t = 1'b0;
    assign d_ready = !dc_skid_valid;
    // A fixed sequence's uops issue from dc in turn: as each but the last
    // issues, dc takes the next (fes_i386_seq).
    decoded_t dc_next;
    fes_i386_seq seq (.d(dc), .n(dc_next));
    assign dc_load = !dc_valid || (issue && !dc.more);
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
        if (!dc_valid || issue) dc <= dc_valid && dc.more ? dc_next : dc_skid_valid ? dc_skid : dc_q;
        if (!dc_skid_valid) dc_skid <= dc_q;
    end

    // ---- D ------------------------------------------------------------------------
    logic cur_valid;
    assign cur_valid = dc_valid && !d_stop;

    // EIP of the instruction in D, and of the next. In 16-bit code EIP is
    // not cut to 16 bits: an instruction that ends at offset FFFFh leaves
    // EIP 10000h. CS's limit is FFFFh after every real-mode CS load, and an
    // instruction that starts or runs past it raises #GP(0) at its fetch,
    // before anything its decode found (cs_gp). Under a larger limit the
    // general CS-limit check is not built yet, so that is
    // FAULT_UNIMPLEMENTED (wrap16). A faulting uop keeps its destination,
    // which nothing reads: it stops at WB, which writes nothing for a fault,
    // and D issues nothing after it (d_stop).
    logic [31:0] d_eip, d_eip_next;
    logic [16:0] eip16;
    logic        past, wrap16, cs_gp, seg_cs_ffff;
    assign eip16 = {1'b0, d_eip[15:0]} + {13'd0, dc.length};
    assign past = !code32 && (d_eip[31:16] != 16'd0 || eip16 > 17'h1_0000);
    assign cs_gp = past && seg_cs_ffff;
    assign wrap16 = past && !seg_cs_ffff;
    assign d_eip_next = code32 ? d_eip + {28'd0, dc.length} : {15'd0, eip16};

    uop_t        u;
    logic        opsize32, lea, lea_early, consumes, reads_seg, stop;
    logic [2:0]  seg_index;
    logic [31:0] lea_disp;

    // A stack operation addresses SS:ESP, or SS:EBP for LEAVE, at the stack's
    // width (SS.B), and writes ESP from AG: ESP less the size for a push, ESP
    // plus it (and RET's imm16) for a pop, EBP plus it for LEAVE.
    logic [2:0]  stk;
    logic        ss32, stack_wait;
    logic [31:0] st_size, st_disp, st_sp_disp;
    assign stk = dc.stk;
    assign ss32 = seg_ar[SEG_SS][14];
    assign st_size = dc.opsize32 ? 32'd4 : 32'd2;
    always_comb begin
        st_disp = 32'd0;
        st_sp_disp = st_size;
        case (stk)
            fes_i386_pkg::STK_PUSH: begin
                st_disp = -st_size;
                st_sp_disp = -st_size;
            end
            fes_i386_pkg::STK_RET_IMM: st_sp_disp = st_size + dc.uop.imm;
            fes_i386_pkg::STK_PEEK: st_disp = dc.sel_part ? st_size : 32'd0;
            fes_i386_pkg::STK_RETF: st_sp_disp = (st_size << 1) + dc.uop.imm;
            default: ;
        endcase
        if (!ss32) begin
            st_disp = {16'b0, st_disp[15:0]};
            st_sp_disp = {16'b0, st_sp_disp[15:0]};
        end
    end
    // The memory operand: whether it reads and writes, its segment, and the
    // limit check's operands for its size. An expand-down data segment,
    // which a real-mode load keeps from protected mode, is not built yet.
    logic        mem, mem_reads, mem_writes, addr_use, a_addr, a_addr32, expand_down, d_unimpl;
    logic [2:0]  mseg;
    logic [1:0]  msize;  // 1, 2 or 4 bytes as 0, 1, 2
    fes_i386_pkg::mem_kind_e mkind;
    assign mkind = dc.uop.mem.kind;
    assign a_addr = dc.lea || mkind != fes_i386_pkg::MEM_NONE;
    assign a_addr32 = stk != fes_i386_pkg::STK_NONE ? ss32 : dc.uop.mem.addr32;
    assign mem = mkind != fes_i386_pkg::MEM_NONE;
    assign mem_reads = mkind == fes_i386_pkg::MEM_LOAD || mkind == fes_i386_pkg::MEM_RMW;
    assign mem_writes = mkind == fes_i386_pkg::MEM_STORE || mkind == fes_i386_pkg::MEM_RMW;
    assign addr_use = a_addr;
    assign mseg = 3'(dc.uop.mem.seg);
    assign msize = dc.uop.mem.bytes[2] ? 2'd2 : dc.uop.mem.bytes[1] ? 2'd1 : 2'd0;
    assign expand_down = !seg_ar[mseg][3] && seg_ar[mseg][2];
    assign d_unimpl = wrap16 || (mem && expand_down);

    // A far transfer's selector and offset, from the instruction or from T0
    // and T1. In 16-bit code an offset past CS's limit raises #GP(0) at the
    // uop that checks it, the transfer's first to write anything; under a
    // larger limit that is FAULT_UNIMPLEMENTED, as wrap16.
    logic [15:0] far_sel;
    logic [31:0] far_off;
    logic        far_bad, t_busy, t_wait;
    assign far_sel = dc.far_t ? t0 : dc.far_sel;
    assign far_off = dc.far_t ? t1 : dc.uop.imm;
    assign far_bad = dc.far_chk && !code32 && far_off[31:16] != 16'd0;
    assign t_wait = dc.far_t && t_busy;
    always_comb begin
        u = dc.uop;
        if (stk != fes_i386_pkg::STK_NONE) u.mem.addr32 = ss32;
        if (d_unimpl && dc.uop.ctl.fault == fes_i386_pkg::FAULT_NONE) u.ctl.fault = fes_i386_pkg::FAULT_UNIMPLEMENTED;
        if (far_bad && dc.uop.ctl.fault == fes_i386_pkg::FAULT_NONE)
            u.ctl.fault = seg_cs_ffff ? fes_i386_pkg::FAULT_GP0 : fes_i386_pkg::FAULT_UNIMPLEMENTED;
        if (cs_gp) u.ctl.fault = fes_i386_pkg::FAULT_GP0;
    end
    assign opsize32 = dc.opsize32;
    assign lea = dc.lea;
    assign lea_early = dc.lea_early;
    // A far pointer's selector is past its offset by the operand size,
    // wrapped by address size.
    assign lea_disp = !dc.sel_part ? dc.lea_disp
                    : dc.uop.mem.addr32 ? dc.lea_disp + st_size : {16'b0, dc.lea_disp[15:0] + st_size[15:0]};
    assign consumes = dc.consumes;
    assign reads_seg = dc.reads_seg;
    assign seg_index = dc.seg_index;
    assign stop = dc.stop || d_unimpl || cs_gp || far_bad;

    // The producers in AG, M1, M2 and EX.
    logic [3:0]       prod_valid, prod_early, prod_sp;
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
    // ESP from a stack operation, unless its destination is ESP too (POP ESP).
    function automatic logic writes_sp(input pipe_t s);
        writes_sp = s.valid && s.uop.sp_write
                 && !(s.uop.dst.valid && s.uop.dst.space == fes_i386_pkg::RS_GPR && s.uop.dst.index[2:0] == 3'd4);
    endfunction
    assign prod_sp = {writes_sp(prod_ex), writes_sp(prod_m2), writes_sp(prod_m1), writes_sp(prod_ag)};

    logic d_writes;
    assign d_writes = cur_valid && u.dst.valid && u.dst.space == fes_i386_pkg::RS_GPR;

    // Data operands.
    logic [31:0] a_val, b_val, c_val;
    logic        a_pend, b_pend, c_pend;
    logic [2:0]  a_dist, b_dist, c_dist;
    fes_i386_dops dops (
        .clk, .reset(reset || wb_flush), .a_reg(u.src1.index[2:0]), .b_reg(u.src2.index[2:0]), .d_writes,
        .d_dst(u.dst.index[2:0]), .d_early(lea && lea_early), .advance(issue), .hold(freeze), .prod_valid, .prod_dst,
        .prod_early, .prod_sp, .gpr, .stage_value, .wb_sp(wb_s.sp_new), .a(a_val), .b(b_val), .c(c_val), .pend_a(a_pend), .pend_b(b_pend),
        .pend_c(c_pend), .dist_a(a_dist), .dist_b(b_dist), .dist_c(c_dist)
    );

    // A memory operand's or LEA's address; a stack operation's is ESP or EBP.
    logic        lea_stall, sp_wait, d_sp;
    logic [1:0]  late, late_hi;
    logic [31:0] row, last;
    logic [15:0] sp_hi;
    assign d_sp = cur_valid && u.sp_write;
    fes_i386_dfwd dfwd (
        .clk, .reset(reset || wb_flush), .has_base(a_addr && dc.has_base), .base(dc.base),
        .has_index(a_addr && dc.has_index), .index(dc.index), .scale(dc.scale),
        .addr32(a_addr32), .d_writes, .d_dst(u.dst.index[2:0]), .d_kind(lea && lea_early ? EARLY : LATE), .d_sp,
        .advance(issue), .hold(freeze), .prod_valid, .prod_dst, .prod_kind, .prod_sp, .gpr, .stage_value,
        .wb_sp(wb_s.sp_new), .row, .last, .late, .late_hi, .stall(lea_stall), .sp_hi, .sp_wait
    );

    // ESP as data waits while a stack operation that writes it is in AG: its
    // value reaches the M1 register next cycle. LEAVE on a 16-bit stack waits
    // for ESP's high half, which it keeps.
    logic sp_hazard;
    assign sp_hazard = writes_sp(ag_s) && (u.src1.valid && u.src1.index[2:0] == 3'd4
                                           || u.src2.valid && u.src2.index[2:0] == 3'd4);
    assign stack_wait = stk == fes_i386_pkg::STK_LEAVE && !ss32 && sp_wait;

    // A selector read waits for an older write of that segment register, and
    // a memory operand for its segment's base, which a real-mode load sets
    // at WB. d_seg_busy names the segments an older uop in AG to WB writes,
    // registered from what will be there next cycle, so each wait is one
    // select.
    function automatic logic writes_seg(input pipe_t s, input int seg);
        writes_seg = s.valid && (s.uop.dst.valid && s.uop.dst.space == fes_i386_pkg::RS_SEG
                                 && s.uop.dst.index[2:0] == 3'(seg) || s.uop.seg2 && s.uop.seg2_idx == 3'(seg));
    endfunction
    logic [5:0] d_seg_busy;
    function automatic logic writes_temp_u(input uop_t v);
        writes_temp_u = v.dst.valid && v.dst.space == fes_i386_pkg::RS_TEMP;
    endfunction
    function automatic logic writes_temp(input pipe_t s);
        writes_temp = s.valid && writes_temp_u(s.uop);
    endfunction
    logic       seg_hazard, mseg_hazard;
    assign seg_hazard = d_seg_busy[seg_index];
    assign mseg_hazard = d_seg_busy[mseg];

    // A load waits one cycle behind a store, whose data EX computes in the
    // cycle the load would be in M2. One cycle later the store is in WB,
    // and M2 takes its bytes from the WB register.
    logic store_hazard;
    assign store_hazard = ag_s.valid && (ag_s.uop.mem.kind == fes_i386_pkg::MEM_STORE
                                         || ag_s.uop.mem.kind == fes_i386_pkg::MEM_RMW) && mem_reads;

    // A flag reader waits one cycle behind a shift or rotate, whose flags
    // reach EX's running copy a cycle after the shift leaves EX (fes_i386_ex).
    logic flag_hazard;
    assign flag_hazard = ag_s.valid && ag_s.uop.cls == fes_i386_pkg::UOP_SHIFT
                      && consumes;

    // A flush needs no term here: it clears AG and the forwarding tables, a
    // taken branch's redirect resets DC and D's EIP on the same edge, and a
    // fault or HLT stops the CPU.
    assign issue = cur_valid && !(addr_use && lea_stall) && !(reads_seg && seg_hazard) && !(mem && mseg_hazard)
                && !store_hazard && !flag_hazard && !sp_hazard && !stack_wait && !t_wait && !freeze;

    always_ff @(posedge clk) begin
        if (reset || wb_flush) begin
            d_seg_busy <= 6'd0;
            t_busy <= 1'b0;
        end else if (!freeze) begin
            // T0 and T1 are written at WB, so a uop that reads them in D waits
            // for every older one that writes either to retire.
            t_busy <= issue && writes_temp_u(u) || writes_temp(ag_s) || writes_temp(m1_s) || writes_temp(m2_s)
                   || writes_temp(ex_s);
            for (int k = 0; k < 6; k++)
                d_seg_busy[k] <= (issue && (u.dst.valid && u.dst.space == fes_i386_pkg::RS_SEG && u.dst.index[2:0] == 3'(k)
                                            || u.seg2 && u.seg2_idx == 3'(k)))
                                || writes_seg(ag_s, k) || writes_seg(m1_s, k) || writes_seg(m2_s, k) || writes_seg(ex_s, k);
        end
        if (reset) begin
            d_stop <= 1'b0;
        end else if (redirect) begin
            d_stop <= 1'b0;
            d_eip <= redirect_eip;
        end else if (issue) begin
            d_stop <= stop;
            if (!dc.more) d_eip <= d_eip_next;
        end
    end

    // ---- D/AG register --------------------------------------------------------------
    logic        ag_addr32;
    logic [31:0] ag_row, ag_last, ag_disp, ag_seg_term;
    logic [15:0] ag_seg_hi, ag_seg_hi_plus1;
    logic [1:0]  ag_late, ag_late_hi;
    logic        ag_stack, ag_ss32, ag_leave;
    logic [31:0] ag_sp_disp;
    logic [15:0] ag_sp_hi;
    pipe_t       d_out;
    always_comb begin
        d_out = '0;
        d_out.valid = issue;
        d_out.uop = u;
        d_out.eip = d_eip;
        d_out.eip_next = d_eip_next;
        d_out.a = dc.cs_base ? seg_base[SEG_CS] : a_val;
        d_out.b = reads_seg ? {16'b0, seg_sel[seg_index]} : dc.push_eip ? d_eip_next
                : u.src2.valid ? b_val : u.imm;
        d_out.c = dc.far_jmp ? {16'b0, far_sel} : c_val;
        d_out.pend_a = u.src1.valid && a_pend;
        d_out.pend_b = u.src2.valid && b_pend;
        d_out.pend_c = u.count_cl && c_pend;
        d_out.dist_a = a_dist;
        d_out.dist_b = b_dist;
        d_out.dist_c = c_dist;
        d_out.early = lea && lea_early;
        d_out.mlimit = seg_lim[mseg][msize];
        d_out.mtiny = seg_tiny[mseg][msize];
    end
    always_ff @(posedge clk) begin
        if (reset || wb_flush) begin
            ag_s.valid <= 1'b0;
        end else if (!freeze) begin
            ag_s <= d_out;
        end
        if (!freeze) begin
            ag_stack <= stk != fes_i386_pkg::STK_NONE;
            ag_ss32 <= ss32;
            ag_leave <= stk == fes_i386_pkg::STK_LEAVE;
            ag_sp_disp <= st_sp_disp;
            ag_sp_hi <= sp_hi;
        end
        if (freeze) begin
            // AG keeps its uop's operands.
        end else if (addr_use) begin
            // LEA's offset, or a memory operand's address: its segment's
            // base, wrapped like the offset by address size.
            ag_addr32 <= u.mem.addr32;
            ag_row <= row;
            ag_last <= last;
            ag_late <= late;
            ag_late_hi <= late_hi;
            ag_disp <= stk != fes_i386_pkg::STK_NONE ? st_disp : lea_disp;
            ag_seg_term <= lea ? 32'd0 : u.mem.addr32 ? seg_base[mseg] : {16'b0, seg_base[mseg][15:0]};
            ag_seg_hi <= lea ? 16'd0 : seg_base[mseg][31:16];
            ag_seg_hi_plus1 <= lea ? 16'd0 : seg_hi1[mseg];
        end else if (dc.far_jmp) begin
            // A far jump's target: the new CS's base, selector * 16, plus
            // the offset, which is already its operand size's.
            ag_addr32 <= 1'b1;
            ag_row <= 32'd0;
            ag_last <= 32'd0;
            ag_late <= 2'd0;
            ag_late_hi <= 2'd0;
            ag_disp <= far_off;
            ag_seg_term <= {12'd0, far_sel, 4'd0};
            ag_seg_hi <= {12'd0, far_sel[15:12]};
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

    logic [31:0] linear, offset, base_op;
    fes_i386_agu agu (
        .addr32(ag_addr32), .seg_term(ag_seg_term), .seg_hi(ag_seg_hi), .seg_hi_plus1(ag_seg_hi_plus1),
        .row(ag_row), .last(ag_last), .disp(ag_disp), .late(ag_late), .late_hi(ag_late_hi),
        .late_m1(m1_s.result), .late_ex(ex_s.result), .late_wb(wb_s.result), .linear, .offset, .base_op
    );

    // A stack operation's new ESP: a third chain beside the AGU's two, on the
    // stack register as the AGU takes it (its low half on a 16-bit stack, a
    // late value included). A 16-bit stack keeps ESP's high half: the late
    // value's, else D's register value. LEAVE's is ESP's, not EBP's.
    logic [31:0] sp_sum, sp_new;
    logic [15:0] sp_hi_ag;
    assign sp_sum = base_op + ag_sp_disp;
    always_comb begin
        case (ag_late)
            2'd1: sp_hi_ag = m1_s.result[31:16];
            2'd2: sp_hi_ag = ex_s.result[31:16];
            2'd3: sp_hi_ag = wb_s.result[31:16];
            default: sp_hi_ag = ag_sp_hi;
        endcase
        if (ag_leave) sp_hi_ag = ag_sp_hi;
    end
    assign sp_new = ag_ss32 ? sp_sum : {sp_hi_ag, sp_sum[15:0]};

    pipe_t ag_out;
    always_comb begin
        ag_out = capture(ag_s, 3'd4, wb_s.result);
        // A stack operation's result until EX is its new ESP, so ESP forwards
        // from the M1, M2 and EX registers like any early result.
        ag_out.result = ag_stack ? sp_new : offset;
        ag_out.sp_new = sp_new;
        ag_out.target = linear;
        ag_out.target_eip = offset;
        ag_out.maddr = linear;
        ag_out.moff = offset;
    end
    always_ff @(posedge clk) begin
        if (reset || wb_flush) begin
            m1_s.valid <= 1'b0;
        end else if (!freeze) begin
            m1_s <= ag_out;
        end
    end

    // ---- M1 -----------------------------------------------------------------------
    // The segment limit check: the offset against the limit less the access's
    // bytes but one, which D picked (#SS(0) for SS, #GP(0) otherwise). A load
    // sends its address to the data port.
    function automatic logic reads_mem(input pipe_t s);
        reads_mem = s.uop.mem.kind == fes_i386_pkg::MEM_LOAD || s.uop.mem.kind == fes_i386_pkg::MEM_RMW;
    endfunction
    function automatic logic writes_mem(input pipe_t s);
        writes_mem = s.uop.mem.kind == fes_i386_pkg::MEM_STORE || s.uop.mem.kind == fes_i386_pkg::MEM_RMW;
    endfunction
    pipe_t m1_out;
    logic  m1_limit;
    assign m1_limit = m1_s.uop.mem.kind != fes_i386_pkg::MEM_NONE && (m1_s.moff > m1_s.mlimit || m1_s.mtiny);
    always_comb begin
        m1_out = capture(m1_s, 3'd3, wb_s.result);
        if (m1_limit && m1_s.uop.ctl.fault == fes_i386_pkg::FAULT_NONE)
            m1_out.uop.ctl.fault = m1_s.uop.mem.seg == fes_i386_pkg::SEG_SS ? fes_i386_pkg::FAULT_SS0
                                                                           : fes_i386_pkg::FAULT_GP0;
    end
    assign dmem_addr = m1_s.maddr;
    assign dmem_read = m1_s.valid && reads_mem(m1_s) && !freeze;

    // Stores older than the load that have not written when the port reads:
    // the store now in EX, which will be in WB when the load is in M2, and
    // the store now in WB, which writes on the edge the port reads and so
    // is missed (the retired store, rt_*, in M2). D keeps a load from
    // following a store directly (store_hazard), so no store in M2 is older.
    // M1 takes each one's distance from the load's address; M2 decodes it.
    logic [31:0] m2_dw, m2_dr;  // the load's address less the store's
    logic        m2_vw, m2_vr;
    always_ff @(posedge clk) if (!freeze) begin
        m2_dw <= m1_s.maddr - ex_s.maddr;
        m2_dr <= m1_s.maddr - wb_s.maddr;
        m2_vw <= ex_s.valid && writes_mem(ex_s);
        m2_vr <= wb_s.valid && writes_mem(wb_s) && wb_s.uop.ctl.fault == fes_i386_pkg::FAULT_NONE;
    end

    // ---- M2 -----------------------------------------------------------------------
    // The loaded bytes from the port, aligned to the access, with each byte an
    // older store writes taken from that store: the store in WB first, then
    // the retired one. EX takes the value through its bypass selects. M2 also
    // decodes EX's controls and prepares its operands (fes_i386_exctl).
    logic [31:0] rt_data;     // the store that retired on the last edge
    logic [3:0]  rt_bytes;
    logic        rt_valid;
    logic [3:0][7:0] ld_bytes;

    // Load byte i is store byte k when the load's address less the store's is
    // k - i: within -3..3, so its high bits are all zero or all one and its
    // low three bits are k - i.
    logic       near_w, near_r;
    logic [3:0] long_w, long_r;  // the store writes byte k
    assign near_w = m2_vw && (m2_dw[31:2] == 30'd0 || m2_dw[31:2] == '1);
    assign near_r = rt_valid && (m2_dr[31:2] == 30'd0 || m2_dr[31:2] == '1);
    assign long_w = {{2{wb_s.uop.mem.bytes[2]}}, |wb_s.uop.mem.bytes[2:1], 1'b1};
    assign long_r = {{2{rt_bytes[2]}}, |rt_bytes[2:1], 1'b1};
    for (genvar i = 0; i < 4; i++) begin : g_ld
        logic [3:0] hw, hr;
        logic [7:0] port, from_w, from_r;
        for (genvar k = 0; k < 4; k++) begin : g_k
            assign hw[k] = near_w && long_w[k] && m2_dw[2:0] == 3'(k - i);
            assign hr[k] = near_r && long_r[k] && m2_dr[2:0] == 3'(k - i);
        end
        assign port = dmem_rdata[3'(m2_s.maddr[1:0]) + 3'(i)];
        assign from_w = ({8{hw[0]}} & wb_s.result[7:0]) | ({8{hw[1]}} & wb_s.result[15:8])
                      | ({8{hw[2]}} & wb_s.result[23:16]) | ({8{hw[3]}} & wb_s.result[31:24]);
        assign from_r = ({8{hr[0]}} & rt_data[7:0]) | ({8{hr[1]}} & rt_data[15:8])
                      | ({8{hr[2]}} & rt_data[23:16]) | ({8{hr[3]}} & rt_data[31:24]);
        assign ld_bytes[i] = i >= m2_s.uop.mem.bytes ? 8'd0 : |hw ? from_w : |hr ? from_r : port;
    end

    pipe_t                m2_cap, m2_out;
    fes_i386_pkg::exctl_t m2_exc;
    logic [31:0]          m2_x, m2_y;
    assign m2_cap = capture(m2_s, 3'd2, wb_s.result);
    fes_i386_exctl exctl (.s(m2_cap), .exc(m2_exc), .x(m2_x), .y(m2_y));
    always_comb begin
        m2_out = m2_cap;
        m2_out.exc = m2_exc;
        m2_out.x = m2_x;
        m2_out.y = m2_y;
        m2_out.ld = ld_bytes;
    end
    always_ff @(posedge clk) begin
        if (reset || wb_flush) begin
            m2_s.valid <= 1'b0;
            m2_load <= 1'b0;
            ex_s.valid <= 1'b0;
        end else if (!freeze) begin
            m2_s <= m1_out;
            m2_load <= m1_s.valid && reads_mem(m1_s) && m1_s.uop.ctl.fault == fes_i386_pkg::FAULT_NONE;
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
    logic        ex_taken, ex_sh_out0, ex_sh_outn, ex_sh_cfw, ex_sh_wide, ex_def_co, ex_def_szap;
    logic [31:0] ex_ind_target, ex_ind_eip;
    pipe_t       ex_out;

    logic [7:0] wb_r8;
    logic       wb_sh_cf, wb_sh_of;
    assign wb_r8 = wb_s.uop.dst.hi8 ? wb_s.result[15:8] : wb_s.result[7:0];
    fes_i386_shflags shflags (
        .c(wb_s.exc.sh), .hi8(wb_s.uop.dst.hi8), .result(wb_s.result), .out0(wb_s.sh_out0), .outn(wb_s.sh_outn),
        .cfw(wb_s.sh_cfw), .wide(wb_s.sh_wide), .cf(wb_sh_cf), .of(wb_sh_of)
    );
    always_comb begin
        wb_flags = wb_s.eflags;
        if (wb_s.def_co) begin
            wb_flags[0] = wb_sh_cf;
            wb_flags[11] = wb_sh_of;
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

    // An operand whose producer is one stage ahead (exc.byp_a, xw_*, yw_*,
    // byp_c) comes from the WB result register beside EX's operand, and a
    // memory operand from the loaded value (xl, yl). An early
    // LEA's offset is its whole result, which EX's LEA select passes.
    fes_i386_ex ex (
        .u(ex_s.uop), .exc(ex_s.exc), .a(ex_s.a), .xr(ex_s.x), .yr(ex_s.y), .wb(wb_s.result), .ld(ex_s.ld),
        .ag_result(ex_s.result), .flags_in(ex_flags), .flags_keep, .result(ex_result), .flags_out(ex_flags_out),
.taken(ex_taken), .ind_target(ex_ind_target), .ind_eip(ex_ind_eip), .sh_out0(ex_sh_out0), .sh_outn(ex_sh_outn), .sh_cfw(ex_sh_cfw), .sh_wide(ex_sh_wide),
        .def_co(ex_def_co), .def_szap(ex_def_szap)
    );
    always_comb begin
        ex_out = ex_s;
        ex_out.result = ex_result;
        ex_out.eflags = ex_flags_out;
        ex_out.taken = ex_taken;
        // RET and JMP r/m: EX's adder gives the target.
        if (ex_s.exc.b_ind) begin
            ex_out.target = ex_ind_target;
            ex_out.target_eip = ex_ind_eip;
        end
        // In 16-bit code a RET or JMP r/m past offset FFFFh raises #GP(0)
        // at the branch, under CS's real-mode limit (FAULT_UNIMPLEMENTED
        // under a larger one, as wrap16 in D). A fault M1 found stands.
        if (ex_s.exc.b_ind && !code32 && ex_ind_eip[31:16] != 16'd0
            && ex_s.uop.ctl.fault == fes_i386_pkg::FAULT_NONE)
            ex_out.uop.ctl.fault = seg_cs_ffff ? fes_i386_pkg::FAULT_GP0 : fes_i386_pkg::FAULT_UNIMPLEMENTED;
        ex_out.sh_out0 = ex_sh_out0;
        ex_out.sh_outn = ex_sh_outn;
        ex_out.sh_cfw = ex_sh_cfw;
        ex_out.sh_wide = ex_sh_wide;
        ex_out.def_co = ex_def_co;
        ex_out.def_szap = ex_def_szap;
    end
    always_ff @(posedge clk) begin
        if (reset) begin
            ex_flags <= {init_eflags[17:2], 1'b1, init_eflags[0]};
        end else if (freeze) begin
            // EX's running copy waits with the uops.
        end else if (ex_s.valid && !wb_flush) begin
            ex_flags <= ex_flags_out;
        end else begin
            ex_flags <= flags_keep;
        end
        if (reset || wb_flush) begin
            wb_s.valid <= 1'b0;
        end else if (!freeze) begin
            wb_s <= ex_out;
        end
    end

    // ---- WB -----------------------------------------------------------------------
    logic halted;
    logic [2:0] wb_fault;
    // The segment register WB writes: the destination, or a second one with
    // the selector in T0 (a far pointer load) or c.
    logic        seg_w;
    logic [2:0]  seg_w_idx;
    logic [15:0] seg_w_sel;
    assign seg_w = wb_s.uop.dst.valid && wb_s.uop.dst.space == fes_i386_pkg::RS_SEG || wb_s.uop.seg2;
    assign seg_w_idx = wb_s.uop.seg2 ? wb_s.uop.seg2_idx : wb_s.uop.dst.index[2:0];
    assign seg_w_sel = !wb_s.uop.seg2 ? wb_s.result[15:0] : wb_s.uop.seg2_t ? t0 : wb_s.c[15:0];
    assign wb_fault = 3'(wb_s.uop.ctl.fault);

    // A store writes its bytes on the edge it retires: the result's low
    // bytes, moved to the address's place in its dword and the next.
    logic wb_store;
    assign wb_store = wb_s.valid && !reset && !freeze && !halted && wb_fault == 3'd0 && writes_mem(wb_s);
    assign dmem_write = wb_store;
    assign dmem_waddr = wb_s.maddr;
    always_comb begin
        dmem_wdata = 64'(wb_s.result) << {wb_s.maddr[1:0], 3'b000};
        dmem_wmask = (wb_s.uop.mem.bytes[2] ? 8'h0f : wb_s.uop.mem.bytes[1] ? 8'h03 : 8'h01) << wb_s.maddr[1:0];
    end
    always_ff @(posedge clk) if (reset || !freeze) begin
        rt_valid <= wb_store;
        rt_data <= wb_s.result;
        rt_bytes <= wb_s.uop.mem.bytes;
    end

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
            seg_cs_ffff <= init_limit[SEG_CS] == 32'h0000_ffff;
            for (int k = 0; k < 6; k++) begin
                seg_lim[k][0] <= init_limit[k];
                seg_lim[k][1] <= init_limit[k] - 32'd1;
                seg_lim[k][2] <= init_limit[k] - 32'd3;
                seg_tiny[k] <= {init_limit[k] < 32'd3, init_limit[k] < 32'd1, 1'b0};
            end
            halted <= 1'b0;
            trace_fault <= 1'b0;
            trace_vector <= 8'd0;
            trace_unimplemented <= 1'b0;
        end else if (wb_s.valid && !halted && !freeze) begin
            if (wb_fault != 3'd0) begin
                halted <= 1'b1;
                trace_fault <= wb_fault != 3'd4;
                trace_unimplemented <= wb_fault == 3'd4;
                trace_vector <= wb_fault == 3'd1 ? 8'd6 : wb_fault == 3'd2 ? 8'd13 : wb_fault == 3'd3 ? 8'd7
                              : wb_fault == 3'd5 ? 8'd12 : 8'd0;
            end else begin
                trace_retire <= wb_s.uop.ctl.end_of_instruction;
                eflags <= wb_flags;
                // EIP moves at an instruction's last uop, so a sequence that
                // faults part way leaves it at the instruction.
                if (wb_s.uop.ctl.end_of_instruction) eip <= wb_s.taken ? wb_s.target_eip : wb_s.eip_next;
                // A stack operation's ESP, which a destination of ESP overrides;
                // RETF's step, at the stack's width.
                if (wb_s.uop.sp_write) gpr[4] <= wb_s.sp_new;
                if (wb_s.uop.sp_add)
                    gpr[4] <= ss32 ? gpr[4] + wb_s.sp_new : {gpr[4][31:16], gpr[4][15:0] + wb_s.sp_new[15:0]};
                if (wb_s.uop.dst.valid && wb_s.uop.dst.space == fes_i386_pkg::RS_GPR)
                    gpr[wb_s.uop.dst.index[2:0]] <= wb_s.result;
                if (wb_s.uop.dst.valid && wb_s.uop.dst.space == fes_i386_pkg::RS_TEMP) begin
                    if (wb_s.uop.dst.index[3]) t1 <= wb_s.result;
                    else t0 <= wb_s.result[15:0];
                end
                if (seg_w) begin
                    // Real mode: the base is selector * 16; limit and attributes stay.
                    seg_sel[seg_w_idx] <= seg_w_sel;
                    seg_base[seg_w_idx] <= {12'd0, seg_w_sel, 4'd0};
                    seg_hi1[seg_w_idx] <= {11'd0, 5'(seg_w_sel[15:12]) + (* no_carry *) 5'd1};
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
