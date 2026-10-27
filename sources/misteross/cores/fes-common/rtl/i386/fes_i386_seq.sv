// fes_i386_seq: the next uop of a fixed sequence (fes_i386_pkg SEQ_*).
//
// DC decodes an instruction to its first uop. An instruction that takes a
// fixed sequence of uops, decoded without the microcode ROM, keeps its place
// in dc while they issue: as D issues each but the last, dc takes the next
// one from here. Every field D reads is then a register at every step, and D
// issues the sequence one uop per cycle.
//
// Every field a sequence rewrites is set outright at every step, from the
// instruction's first uop (d0, which DC keeps as dc0) and the step: the uop
// at step k has every change of the steps before it again. None is kept from
// dc itself, since a field held at some steps would get the step's condition
// in its enable, which Yosys builds after D's issue. So dc's fields load on
// two enables: these at every issue, the rest of the instruction at its
// last. The per-step controls (the first block below) are reset at every
// step.
//
// SEQ_CALL (CALL rel): the push of the next EIP, then JMP rel by the
// displacement the push carries in its imm, which a push does not use. The
// push already has the jump's sizes, EIP length and address size.
//
// SEQ_LPTR (LES, LDS, LSS, LFS, LGS): the selector's word into T0, then the
// offset into the register, with the segment written from T0 at WB.
//
// The far transfers end in a far jump (far_jmp), which writes CS from the
// selector in c. Nothing is written before the uop that checks the offset
// against CS's limit (far_chk):
//   SEQ_CALLF  (CALL ptr): a probe (checks), push CS, push the next EIP,
//              jump.
//   SEQ_RETF   (RETF, RETF imm16): the offset into T1 and the selector into
//              T0, ESP unmoved; the jump checks, and WB steps ESP.
//   SEQ_JMPFM  (JMP m16:16/32): the selector into T0, the offset into T1,
//              jump (checks).
//   SEQ_CALLFM (CALL m16:16/32): the selector into T0, the offset into T1,
//              a probe (checks), push CS, push the next EIP, jump.
// A far call's probe reads the return address's lower slot, so a push that
// would cross the stack's limit faults before the first one writes; the
// push of CS checks the higher slot.
// A uop that takes the selector and offset from T0 and T1 (far_t) waits in
// D until the loads have retired.
//
// SEQ_UCODE: a microcode routine. Its uops after the first come from the
// ROM (fes_i386_ucode_rom, word w, at d.upc), each expanded here into dc
// with the instruction's operand size, vector and memory operand kept.
//
// The moves through T1, each waiting for T1's load to retire (b_t1):
//   SEQ_XCHGM  (XCHG r/m, r with a memory operand): the operand into T1;
//              the register stored to it; the register from T1.
//   SEQ_CALLI  (CALL r/m near): the target into T1; push the next EIP, which
//              checks T1 against CS's limit first (far_chk); jump to T1.
//   SEQ_PUSHM  (PUSH m): the operand into T1; push T1.
//   SEQ_POPM   (POP m): the stack's slot into T1, ESP unmoved; T1 stored to
//              the operand (its address fields restored from m_*); a pop
//              into nothing, which steps ESP. A store that faults so leaves
//              ESP as it was, as the 386EX does.
// SEQ_XCHG (XCHG r, r): three XORs without flags, each into the register
// the one before read.
//
// The string engine. D keeps copies of the next source and destination
// offsets and of the count while the instruction runs (fes_i386_cpu): a
// step's address is a copy (str_a, str_b), not a register D forwards, so its
// uops issue one per cycle. cnt says whether the copy of the count is 0, 1
// or 2, from registers; at ENTER's first step, whether its level is.
//   SEQ_STRING: the first uop, which DC decodes, loads the copies (str_load).
//     Then one element per count under REP, else one. The first element is
//     built without the count, which D has not loaded yet: under REP it
//     looks like one of several, and D ends the instruction there when the
//     count is 1, or issues it as a NOP that ends the instruction when the
//     count is 0 (fes_i386_cpu, s_end1). An element's last uop steps ESI,
//     EDI and ECX at WB (str_si, str_di, str_cx), so a fault part way
//     leaves them at the elements done.
//       STOS: store eAX at ES:EDI.     LODS: eAX from seg:ESI.
//       SCAS: compare eAX with ES:EDI. MOVS: seg:ESI to ES:EDI, one uop
//       (st2: its store goes to the destination copy's address).
//       CMPS: seg:ESI's value as a uop's result (step 1), then its compare
//       with ES:EDI (step 2), which takes that result one stage ahead
//       (x_prev): D issues step 2 in the cycle after step 1, since nothing
//       it waits on can change between them.
//     Under REPE or REPNE a CMPS or SCAS element but the last ends the
//     instruction when ZF says so (rep_stop), as a taken branch to the next
//     one, which flushes the elements D has issued after it.
//   SEQ_ENTER: the push of EBP (step 0, DC's), which loads the copies: the
//     source is EBP less the size, the destination ESP less twice the size
//     and the frame pointer ESP less the size. Then level - 1 copies of a
//     slot below EBP to the next push slot (step 1), the push of the frame
//     pointer (step 2) if the level is not 0, and the last uop (step 3):
//     EBP from the frame pointer, ESP from the destination copy plus imm,
//     and a check of the slot there, as the 386EX makes. ESP and EBP change
//     only there, so a fault before leaves them, the pushes already written.
module fes_i386_seq (
    input  fes_i386_pkg::decoded_t d,   // the uop dc holds
    input  fes_i386_pkg::decoded_t d0,  // the instruction's first uop
    input  fes_i386_pkg::ucode_t   w,
    input  logic [2:0]             cnt, // the string engine's count is 2, 1, 0 (bits 2:0)
    output fes_i386_pkg::decoded_t n
);
    // A ROM word's size: the operand size, 16, 32 or 8 bits (size_e).
    function automatic fes_i386_pkg::size_e uc_size(input logic [1:0] s, input logic osz32);
        uc_size = s == 2'd0 ? (osz32 ? fes_i386_pkg::SIZE_32 : fes_i386_pkg::SIZE_16)
                : s == 2'd1 ? fes_i386_pkg::SIZE_16 : s == 2'd2 ? fes_i386_pkg::SIZE_32 : fes_i386_pkg::SIZE_8;
    endfunction
    // The changes to the rewritten fields, each applied at its step and at
    // every step after it. A push of CS, after the probe before it (and its
    // control, reads_seg, at its own step only).
    function automatic void to_push_cs(input fes_i386_pkg::decoded_t p, inout fes_i386_pkg::decoded_t q);
        q.uop.sp_write = 1'b1;
        q.uop.op = 5'd15;  // ALU_MOVZX of the selector
        q.uop.src_size = fes_i386_pkg::SIZE_16;
        q.uop.mem.kind = fes_i386_pkg::MEM_STORE;
        q.uop.mem.in_a = 1'b1;
        q.uop.mem.bytes = p.opsize32 ? 4'd4 : 4'd2;
        q.stk = fes_i386_pkg::STK_PUSH;
        q.seg_index = 3'd1;
    endfunction
    // A push of the next EIP, after a push of CS (push_eip at its own step).
    function automatic void to_push_eip(input fes_i386_pkg::decoded_t p, inout fes_i386_pkg::decoded_t q);
        q.uop.op = 5'd9;  // ALU_MOV
        q.uop.src_size = q.uop.size;
        q.uop.mem.bytes = p.opsize32 ? 4'd4 : 4'd2;
    endfunction
    // A store's or a register move's size, from the access's bytes (the
    // first uop's load into T1 is 32 bits).
    function automatic fes_i386_pkg::size_e by_bytes(input logic [3:0] b);
        by_bytes = b[0] ? fes_i386_pkg::SIZE_8 : b[1] ? fes_i386_pkg::SIZE_16 : fes_i386_pkg::SIZE_32;
    endfunction
    // A stack slot's address: ESP, no index.
    function automatic void at_stack(inout fes_i386_pkg::decoded_t q);
        q.uop.mem.seg = fes_i386_pkg::SEG_SS;
        q.has_base = 1'b1;
        q.base = 3'd4;
        q.has_index = 1'b0;
        q.scale = 2'd0;
    endfunction
    // The far jump that ends a far transfer, and its controls.
    function automatic void to_far_jump(inout fes_i386_pkg::decoded_t q);
        q.uop.sp_write = 1'b0;
        q.uop.cls = fes_i386_pkg::UOP_BRANCH;
        q.uop.op = 5'd1;  // BR_JMP
        q.uop.mem.kind = fes_i386_pkg::MEM_NONE;
        q.stk = fes_i386_pkg::STK_NONE;
        q.uop.seg2 = 1'b1;
        q.uop.ctl.end_of_instruction = 1'b1;
        q.wseg = 6'b000010;  // CS
        q.far_jmp = 1'b1;
    endfunction
    // A GPR operand.
    function automatic fes_i386_pkg::reg_ref_t gpr(input logic valid, input logic [2:0] index);
        fes_i386_pkg::reg_ref_t r;
        r.valid = valid;
        r.space = fes_i386_pkg::RS_GPR;
        r.index = {1'b0, index};
        r.hi8 = 1'b0;
        return r;
    endfunction
    // A string engine uop: no address registers, its offset from D's copies.
    function automatic void engine(inout fes_i386_pkg::decoded_t q);
        q.uop.cls = fes_i386_pkg::UOP_ALU;
        q.uop.count_cl = 1'b0;
        q.uop.src1 = gpr(1'b0, 3'd0);
        q.uop.src2 = gpr(1'b0, 3'd0);
        q.uop.dst = gpr(1'b0, 3'd0);
        q.uop.mem.in_a = 1'b0;
        q.has_base = 1'b0;
        q.has_index = 1'b0;
        q.scale = 2'd0;
    endfunction

    // SEQ_STRING: the instruction repeats (REP), and ends early by ZF
    // (REPE and REPNE CMPS and SCAS); the next uop ends an element, and is
    // the instruction's last.
    logic s_rep, s_zrep, s_cmps, s_end, s_last;
    assign s_rep = d0.str_rep != 2'd0;
    assign s_cmps = d0.str_op == 3'(fes_i386_pkg::STR_CMPS);
    assign s_zrep = s_rep && (s_cmps || d0.str_op == 3'(fes_i386_pkg::STR_SCAS));
    assign s_end = !s_cmps || d.step == 3'd1;
    assign s_last = !s_rep || d.step != 3'd0 && (s_cmps ? cnt[1] : cnt[2]);

    always_comb begin
        n = d;
        n.step = d.step + 3'd1;
        n.upc = d.upc + 8'd1;  // read only by SEQ_UCODE
        // The per-step controls.
        n.more = 1'b0;
        n.uop.sp_add = 1'b0;
        n.uop.eoi_taken = 1'b0;
        n.uop.seg2 = 1'b0;
        n.uop.ctl.end_of_instruction = 1'b0;
        n.sel_part = 1'b0;
        n.far_jmp = 1'b0;
        n.far_chk = 1'b0;
        n.far_t = 1'b0;
        n.push_eip = 1'b0;
        n.reads_seg = 1'b0;
        n.wseg = 6'd0;
        n.slot = 4'd0;
        n.a_t1 = 1'b0;
        n.b_t1 = 1'b0;
        n.popad_esp = 1'b0;
        n.str_load = 1'b0;
        n.str_a = 1'b0;
        n.str_b = 1'b0;
        n.str_two = 1'b0;
        n.str_fin = 1'b0;
        n.str_cnt = 1'b0;
        n.b_fp = 1'b0;
        n.x_prev = 1'b0;
        n.uop.str_si = 1'b0;
        n.uop.str_di = 1'b0;
        n.uop.str_cx = 1'b0;
        n.uop.str_down = 1'b0;
        n.uop.st2 = 1'b0;
        n.uop.rep_stop = 2'd0;
        // The rewritten fields, from the first uop.
        n.uop.sp_write = d0.uop.sp_write;
        n.uop.cls = d0.uop.cls;
        n.uop.op = d0.uop.op;
        n.uop.src1 = d0.uop.src1;
        n.uop.src2 = d0.uop.src2;
        n.uop.dst = d0.uop.dst;
        n.uop.size = d0.uop.size;
        n.uop.src_size = d0.uop.src_size;
        n.uop.count_cl = d0.uop.count_cl;
        n.uop.imm = d0.uop.imm;
        n.uop.mem.kind = d0.uop.mem.kind;
        n.uop.mem.bytes = d0.uop.mem.bytes;
        n.uop.mem.seg = d0.uop.mem.seg;
        n.uop.mem.system = d0.uop.mem.system;
        n.uop.mem.in_a = d0.uop.mem.in_a;
        n.uop.probe = d0.uop.probe;
        n.uop.seg2_idx = d0.uop.seg2_idx;
        n.uop.seg2_t = d0.uop.seg2_t;
        n.uop.flags = d0.uop.flags;
        n.uop.ctl.fault = d0.uop.ctl.fault;
        n.consumes = d0.consumes;
        n.lea = d0.lea;
        n.has_base = d0.has_base;
        n.base = d0.base;
        n.has_index = d0.has_index;
        n.scale = d0.scale;
        n.stk = d0.stk;
        n.seg_index = d0.seg_index;
        n.cs_base = d0.cs_base;
        case (d.seq)
            fes_i386_pkg::SEQ_CALL: begin
                n.uop.sp_write = 1'b0;
                n.uop.cls = fes_i386_pkg::UOP_BRANCH;
                n.uop.op = 5'd1;  // BR_JMP
                n.uop.mem.kind = fes_i386_pkg::MEM_NONE;
                n.uop.ctl.end_of_instruction = 1'b1;
                n.stk = fes_i386_pkg::STK_NONE;
            end
            fes_i386_pkg::SEQ_CALLF: begin
                to_push_cs(d, n);
                if (d.step == 3'd0) begin
                    n.reads_seg = 1'b1;
                    n.more = 1'b1;
                end else begin
                    to_push_eip(d, n);
                    if (d.step == 3'd1) begin
                        n.push_eip = 1'b1;
                        n.more = 1'b1;
                    end else begin
                        to_far_jump(n);
                    end
                end
            end
            fes_i386_pkg::SEQ_RETF: begin
                // The selector, in the slot past the offset, into T0. The
                // slot is read whole, so its limit check is operand-sized,
                // as the 386EX's is.
                n.uop.dst.index = 4'd0;
                if (d.step == 3'd0) begin
                    n.slot = 4'd1;
                    n.more = 1'b1;
                end else begin
                    to_far_jump(n);
                    n.uop.dst.valid = 1'b0;
                    n.uop.sp_add = 1'b1;
                    n.stk = fes_i386_pkg::STK_RETF;
                    n.far_chk = 1'b1;
                    n.far_t = 1'b1;
                end
            end
            fes_i386_pkg::SEQ_JMPFM, fes_i386_pkg::SEQ_CALLFM: begin
                // The offset into T1.
                n.uop.dst.index = 4'b1000;
                n.uop.mem.bytes = d.opsize32 ? 4'd4 : 4'd2;
                if (d.step == 3'd0) begin
                    n.more = 1'b1;
                end else if (d.seq == fes_i386_pkg::SEQ_JMPFM) begin
                    to_far_jump(n);
                    n.uop.dst.valid = 1'b0;
                    n.far_chk = 1'b1;
                    n.far_t = 1'b1;
                end else begin
                    // The probe of the return address's lower slot, at
                    // SS:ESP less twice the size, which checks the offset.
                    n.uop.dst.valid = 1'b0;
                    n.stk = fes_i386_pkg::STK_PROBE;
                    at_stack(n);
                    if (d.step == 3'd1) begin
                        n.far_chk = 1'b1;
                        n.far_t = 1'b1;
                        n.more = 1'b1;
                    end else begin
                        to_push_cs(d, n);
                        if (d.step == 3'd2) begin
                            n.reads_seg = 1'b1;
                            n.more = 1'b1;
                        end else begin
                            to_push_eip(d, n);
                            if (d.step == 3'd3) begin
                                n.push_eip = 1'b1;
                                n.more = 1'b1;
                            end else begin
                                to_far_jump(n);
                                n.far_t = 1'b1;
                            end
                        end
                    end
                end
            end
            fes_i386_pkg::SEQ_UCODE: begin
                n.more = !w.last;
                n.uop.cls = fes_i386_pkg::uop_class_e'(w.cls);
                n.uop.op = w.op;
                n.uop.imm = {28'd0, w.imm};
                n.uop.size = uc_size(w.size, d.opsize32);
                n.uop.src_size = uc_size(w.src_size, d.opsize32);
                n.uop.count_cl = 1'b0;
                n.uop.src1 = '{valid: w.s1, space: fes_i386_pkg::RS_GPR, index: {1'b0, w.s1_idx}, hi8: 1'b0};
                n.uop.src2 = '{valid: w.s2, space: fes_i386_pkg::RS_GPR, index: {1'b0, w.s2_idx}, hi8: 1'b0};
                n.uop.dst = '{valid: w.d, space: fes_i386_pkg::reg_space_e'(w.d_space), index: w.d_idx, hi8: 1'b0};
                n.uop.mem.kind = fes_i386_pkg::mem_kind_e'(w.mkind);
                n.uop.mem.bytes = w.mbytes == 2'd0 ? (d.opsize32 ? 4'd4 : 4'd2)
                                : w.mbytes == 2'd1 ? 4'd2 : w.mbytes == 2'd2 ? 4'd4 : 4'd1;
                n.uop.mem.in_a = w.in_a;
                n.uop.mem.system = w.ivt;
                // A stack access is SS's; the vector table's reads come
                // before any, so they have the first uop's segment.
                if (w.stk != 3'd0) n.uop.mem.seg = fes_i386_pkg::SEG_SS;
                n.uop.probe = w.probe;
                n.uop.sp_write = w.stk == 3'(fes_i386_pkg::STK_PUSH) || w.stk == 3'(fes_i386_pkg::STK_POP) || w.sp_step;
                n.uop.sp_add = w.sp_add;
                n.uop.seg2 = w.far_jmp;
                n.uop.seg2_idx = 3'd1;  // CS
                n.uop.seg2_t = 1'b0;
                n.uop.flags = '{read: '{oszap: w.rd_oszap, cf: w.rd_cf}, write: '{oszap: w.wr_oszap, cf: w.wr_cf}};
                n.uop.ctl.end_of_instruction = w.last;
                n.uop.ctl.fault = fes_i386_pkg::FAULT_NONE;
                n.stk = w.stk;
                n.slot = w.slot;
                n.sel_part = w.sel_part;
                n.consumes = w.rd_cf || w.rd_oszap;
                n.lea = 1'b0;
                // A stack operation's address is ESP; the vector's entry
                // has neither base nor index.
                n.has_base = w.stk != 3'd0;
                n.base = 3'd4;
                n.has_index = 1'b0;
                n.scale = 2'd0;
                n.push_eip = w.b == 2'd1;
                n.a_t1 = w.a_t1;
                n.popad_esp = w.popad_esp;
                n.reads_seg = w.b == 2'd2;
                n.seg_index = w.seg;
                n.far_jmp = w.far_jmp;
                n.far_chk = w.far_chk;
                n.far_t = w.far_t;
                n.wseg = w.far_jmp ? 6'b000010 : 6'd0;
            end
            fes_i386_pkg::SEQ_XCHG: begin
                // Odd steps swap the first uop's registers.
                if (d.step == 3'd0) begin
                    n.uop.src1 = d0.uop.src2;
                    n.uop.src2 = d0.uop.src1;
                    n.uop.dst.index = d0.uop.src2.index;
                    n.uop.dst.hi8 = d0.uop.src2.hi8;
                    n.more = 1'b1;
                end else begin
                    n.uop.dst.index = d0.uop.src1.index;
                    n.uop.dst.hi8 = d0.uop.src1.hi8;
                    n.uop.ctl.end_of_instruction = 1'b1;
                end
            end
            fes_i386_pkg::SEQ_XCHGM: begin
                // The first uop carries the register in src1, unread.
                n.uop.size = by_bytes(d0.uop.mem.bytes);
                n.uop.mem.in_a = 1'b1;
                if (d.step == 3'd0) begin
                    n.uop.src1.valid = 1'b0;
                    n.uop.src2 = '{valid: 1'b1, space: fes_i386_pkg::RS_GPR, index: d0.uop.src1.index,
                                   hi8: d0.uop.src1.hi8};
                    n.uop.dst.valid = 1'b0;
                    n.uop.mem.kind = fes_i386_pkg::MEM_STORE;
                    n.more = 1'b1;
                end else begin
                    n.uop.src1 = '{valid: 1'b1, space: fes_i386_pkg::RS_GPR, index: d0.uop.src1.index,
                                   hi8: d0.uop.src1.hi8};
                    n.uop.src2 = '{valid: 1'b0, space: fes_i386_pkg::RS_GPR, index: 4'd0, hi8: 1'b0};
                    n.uop.dst = n.uop.src1;
                    n.uop.mem.kind = fes_i386_pkg::MEM_NONE;
                    n.b_t1 = 1'b1;
                    n.uop.ctl.end_of_instruction = 1'b1;
                end
            end
            fes_i386_pkg::SEQ_CALLI: begin
                // The push of the next EIP, then (its address and sizes
                // kept) the jump to T1.
                n.uop.size = d.opsize32 ? fes_i386_pkg::SIZE_32 : fes_i386_pkg::SIZE_16;
                n.uop.src_size = n.uop.size;
                n.uop.src2.valid = 1'b0;
                n.uop.dst.valid = 1'b0;
                n.uop.mem.in_a = 1'b1;
                n.uop.mem.bytes = d.opsize32 ? 4'd4 : 4'd2;
                at_stack(n);
                if (d.step == 3'd0) begin
                    n.uop.op = 5'd9;  // ALU_MOV
                    n.uop.mem.kind = fes_i386_pkg::MEM_STORE;
                    n.uop.sp_write = 1'b1;
                    n.stk = fes_i386_pkg::STK_PUSH;
                    n.push_eip = 1'b1;
                    n.far_chk = 1'b1;
                    n.far_t = 1'b1;
                    n.more = 1'b1;
                end else begin
                    n.uop.cls = fes_i386_pkg::UOP_BRANCH;
                    n.uop.op = 5'd11;  // BR_JMP_IND
                    n.uop.sp_write = 1'b0;
                    n.uop.mem.kind = fes_i386_pkg::MEM_NONE;
                    n.stk = fes_i386_pkg::STK_NONE;
                    n.cs_base = 1'b1;
                    n.b_t1 = 1'b1;
                    n.uop.ctl.end_of_instruction = 1'b1;
                end
            end
            fes_i386_pkg::SEQ_PUSHM: begin
                n.uop.size = d.opsize32 ? fes_i386_pkg::SIZE_32 : fes_i386_pkg::SIZE_16;
                n.uop.src_size = n.uop.size;
                n.uop.dst.valid = 1'b0;
                n.uop.mem.kind = fes_i386_pkg::MEM_STORE;
                n.uop.mem.in_a = 1'b1;
                n.uop.sp_write = 1'b1;
                n.stk = fes_i386_pkg::STK_PUSH;
                at_stack(n);
                n.b_t1 = 1'b1;
                n.uop.ctl.end_of_instruction = 1'b1;
            end
            fes_i386_pkg::SEQ_POPM: begin
                n.uop.size = d.opsize32 ? fes_i386_pkg::SIZE_32 : fes_i386_pkg::SIZE_16;
                n.uop.src_size = n.uop.size;
                n.uop.dst.valid = 1'b0;
                if (d.step == 3'd0) begin
                    n.uop.mem.kind = fes_i386_pkg::MEM_STORE;
                    n.uop.mem.in_a = 1'b1;
                    n.uop.mem.seg = fes_i386_pkg::seg_e'(d.m_seg);
                    n.stk = fes_i386_pkg::STK_NONE;
                    n.has_base = d.m_has_base;
                    n.base = d.m_base;
                    n.has_index = d.m_has_index;
                    n.scale = d.m_scale;
                    n.b_t1 = 1'b1;
                    n.more = 1'b1;
                end else begin
                    n.uop.mem.kind = fes_i386_pkg::MEM_LOAD;
                    n.uop.mem.in_a = 1'b0;
                    n.uop.sp_write = 1'b1;
                    n.stk = fes_i386_pkg::STK_POP;
                    at_stack(n);
                    n.uop.ctl.end_of_instruction = 1'b1;
                end
            end
            fes_i386_pkg::SEQ_STRING: begin
                // Step 1 is an element, or CMPS's first load; step 2 CMPS's
                // compare.
                engine(n);
                n.step = s_cmps && d.step == 3'd1 ? 3'd2 : 3'd1;
                n.uop.op = 5'd9;  // ALU_MOV
                n.uop.mem.kind = fes_i386_pkg::MEM_LOAD;
                n.uop.flags = '0;
                case (d0.str_op)
                    3'(fes_i386_pkg::STR_STOS): begin
                        n.uop.src2 = gpr(1'b1, 3'd0);
                        n.uop.mem.kind = fes_i386_pkg::MEM_STORE;
                        n.uop.mem.in_a = 1'b1;
                        n.uop.mem.seg = fes_i386_pkg::SEG_ES;
                        n.str_b = 1'b1;
                    end
                    3'(fes_i386_pkg::STR_LODS): begin
                        n.uop.src1 = gpr(1'b1, 3'd0);
                        n.uop.dst = gpr(1'b1, 3'd0);
                        n.str_a = 1'b1;
                    end
                    3'(fes_i386_pkg::STR_SCAS): begin
                        n.uop.op = 5'd7;  // ALU_CMP
                        n.uop.src1 = gpr(1'b1, 3'd0);
                        n.uop.mem.seg = fes_i386_pkg::SEG_ES;
                        n.uop.flags.write.oszap = 1'b1;
                        n.uop.flags.write.cf = 1'b1;
                        n.str_b = 1'b1;
                    end
                    3'(fes_i386_pkg::STR_MOVS): begin
                        n.uop.st2 = 1'b1;
                        n.str_a = 1'b1;
                        n.str_two = 1'b1;
                    end
                    default: begin  // STR_CMPS
                        if (d.step == 3'd1) begin
                            n.uop.op = 5'd7;  // ALU_CMP
                            n.uop.mem.seg = fes_i386_pkg::SEG_ES;
                            n.uop.flags.write.oszap = 1'b1;
                            n.uop.flags.write.cf = 1'b1;
                            n.x_prev = 1'b1;
                            n.str_b = 1'b1;
                        end else begin
                            n.str_a = 1'b1;
                        end
                    end
                endcase
                if (s_end) begin
                    n.uop.str_si = d0.str_op != 3'(fes_i386_pkg::STR_STOS)
                                && d0.str_op != 3'(fes_i386_pkg::STR_SCAS);
                    n.uop.str_di = d0.str_op != 3'(fes_i386_pkg::STR_LODS);
                    n.uop.str_cx = s_rep;
                    n.str_cnt = s_rep;
                    n.uop.ctl.end_of_instruction = s_last;
                    n.more = !s_last;
                    if (s_zrep && !s_last) begin
                        n.uop.rep_stop = d0.str_rep == 2'(fes_i386_pkg::REP_E) ? 2'b01 : 2'b10;
                        n.uop.eoi_taken = 1'b1;
                    end
                end else begin
                    n.more = 1'b1;
                end
            end
            fes_i386_pkg::SEQ_ENTER: begin
                // Step 1 copies a slot, step 2 pushes the frame pointer,
                // step 3 is the last.
                engine(n);
                n.uop.op = 5'd9;  // ALU_MOV
                n.uop.flags = '0;
                if (d.step == 3'd2 || d.step == 3'd0 && cnt[0]) begin
                    n.step = 3'd3;
                    n.uop.src1 = gpr(1'b1, 3'd5);
                    n.uop.dst = gpr(1'b1, 3'd5);
                    n.uop.mem.kind = fes_i386_pkg::MEM_LOAD;
                    n.uop.mem.in_a = 1'b1;
                    n.uop.sp_write = 1'b1;
                    n.b_fp = 1'b1;
                    n.str_fin = 1'b1;
                    n.uop.ctl.end_of_instruction = 1'b1;
                end else if (cnt[1]) begin
                    n.step = 3'd2;
                    n.uop.mem.kind = fes_i386_pkg::MEM_STORE;
                    n.uop.mem.in_a = 1'b1;
                    n.b_fp = 1'b1;
                    n.str_b = 1'b1;
                    n.more = 1'b1;
                end else begin
                    n.step = 3'd1;
                    n.uop.mem.kind = fes_i386_pkg::MEM_LOAD;
                    n.uop.st2 = 1'b1;
                    n.str_a = 1'b1;
                    n.str_two = 1'b1;
                    n.str_cnt = 1'b1;
                    n.more = 1'b1;
                end
            end
            fes_i386_pkg::SEQ_LPTR: begin
                n.uop.dst.space = fes_i386_pkg::RS_GPR;
                n.uop.mem.bytes = d.opsize32 ? 4'd4 : 4'd2;
                n.uop.seg2 = 1'b1;
                n.uop.ctl.end_of_instruction = 1'b1;
                n.wseg = 6'(1) << d0.uop.seg2_idx;
            end
            default: ;
        endcase
        n.rd_str = fes_i386_pkg::str_reads(n.uop, n.lea, n.has_base, n.base, n.has_index, n.index);
    end
endmodule
