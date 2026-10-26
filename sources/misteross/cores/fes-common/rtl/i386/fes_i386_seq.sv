// fes_i386_seq: the next uop of a fixed sequence (fes_i386_pkg SEQ_*).
//
// DC decodes an instruction to its first uop. An instruction that takes a
// fixed sequence of uops, decoded without the microcode ROM, keeps its place
// in dc while they issue: as D issues each but the last, dc takes the next
// one from here, worked out from dc itself. Every field D reads is then a
// register at every step, and D issues the sequence one uop per cycle.
//
// Each step changes only the fields that differ from the uop before, so a
// bit the sequence keeps is a hold, which DC's input select absorbs. The
// one-bit controls below are set outright at every step instead: a hold on
// one becomes an enable that Yosys builds after D's issue, a level deeper.
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
module fes_i386_seq (
    input  fes_i386_pkg::decoded_t d,
    output fes_i386_pkg::decoded_t n
);
    // A push of CS, from the probe before it.
    function automatic void to_push_cs(input fes_i386_pkg::decoded_t p, inout fes_i386_pkg::decoded_t q);
        q.uop.sp_write = 1'b1;
        q.uop.op = 5'd15;  // ALU_MOVZX of the selector
        q.uop.src_size = fes_i386_pkg::SIZE_16;
        q.uop.mem.kind = fes_i386_pkg::MEM_STORE;
        q.uop.mem.in_a = 1'b1;
        q.uop.mem.bytes = p.opsize32 ? 4'd4 : 4'd2;
        q.stk = fes_i386_pkg::STK_PUSH;
        q.reads_seg = 1'b1;
        q.seg_index = 3'd1;
    endfunction
    // A push of the next EIP, from a push of CS.
    function automatic void to_push_eip(input fes_i386_pkg::decoded_t p, inout fes_i386_pkg::decoded_t q);
        q.uop.op = 5'd9;  // ALU_MOV
        q.uop.src_size = p.uop.size;
        q.uop.mem.bytes = p.opsize32 ? 4'd4 : 4'd2;
        q.push_eip = 1'b1;
    endfunction
    // The far jump that ends a far transfer.
    function automatic void to_far_jump(inout fes_i386_pkg::decoded_t q);
        q.uop.sp_write = 1'b0;
        q.uop.cls = fes_i386_pkg::UOP_BRANCH;
        q.uop.op = 5'd1;  // BR_JMP
        q.uop.mem.kind = fes_i386_pkg::MEM_NONE;
        q.uop.seg2 = 1'b1;
        q.uop.ctl.end_of_instruction = 1'b1;
        q.wseg = 6'b000010;  // CS
        q.stk = fes_i386_pkg::STK_NONE;
        q.far_jmp = 1'b1;
    endfunction

    always_comb begin
        n = d;
        n.step = d.step + 3'd1;
        n.more = 1'b0;
        n.uop.sp_add = 1'b0;
        n.uop.seg2 = 1'b0;
        n.uop.ctl.end_of_instruction = 1'b0;
        n.sel_part = 1'b0;
        n.far_jmp = 1'b0;
        n.far_chk = 1'b0;
        n.far_t = 1'b0;
        n.push_eip = 1'b0;
        n.reads_seg = 1'b0;
        n.wseg = 6'd0;
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
                if (d.step == 3'd0) begin
                    to_push_cs(d, n);
                    n.more = 1'b1;
                end else if (d.step == 3'd1) begin
                    to_push_eip(d, n);
                    n.more = 1'b1;
                end else begin
                    to_far_jump(n);
                end
            end
            fes_i386_pkg::SEQ_RETF: begin
                if (d.step == 3'd0) begin
                    // The selector, in the slot past the offset, into T0.
                    n.uop.dst.index = 4'd0;
                    n.uop.mem.bytes = 4'd2;
                    n.sel_part = 1'b1;
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
                if (d.step == 3'd0) begin
                    // The offset into T1.
                    n.uop.dst.index = 4'b1000;
                    n.uop.mem.bytes = d.opsize32 ? 4'd4 : 4'd2;
                    n.more = 1'b1;
                end else if (d.seq == fes_i386_pkg::SEQ_JMPFM || d.step == 3'd4) begin
                    to_far_jump(n);
                    n.uop.dst.valid = 1'b0;
                    n.far_chk = d.seq == fes_i386_pkg::SEQ_JMPFM;
                    n.far_t = 1'b1;
                end else if (d.step == 3'd1) begin
                    // The probe of the return address's lower slot, at
                    // SS:ESP less twice the size, which checks the offset.
                    n.uop.dst.valid = 1'b0;
                    n.uop.mem.seg = fes_i386_pkg::SEG_SS;
                    n.uop.mem.bytes = d.opsize32 ? 4'd4 : 4'd2;
                    n.stk = fes_i386_pkg::STK_PROBE;
                    n.has_base = 1'b1;
                    n.base = 3'd4;
                    n.has_index = 1'b0;
                    n.scale = 2'd0;
                    n.far_chk = 1'b1;
                    n.far_t = 1'b1;
                    n.more = 1'b1;
                end else if (d.step == 3'd2) begin
                    to_push_cs(d, n);
                    n.more = 1'b1;
                end else begin
                    to_push_eip(d, n);
                    n.more = 1'b1;
                end
            end
            fes_i386_pkg::SEQ_LPTR: begin
                n.uop.dst.space = fes_i386_pkg::RS_GPR;
                n.uop.mem.bytes = d.opsize32 ? 4'd4 : 4'd2;
                n.uop.seg2 = 1'b1;
                n.uop.ctl.end_of_instruction = 1'b1;
                n.wseg = 6'(1) << d.uop.seg2_idx;
            end
            default: ;
        endcase
    end
endmodule
