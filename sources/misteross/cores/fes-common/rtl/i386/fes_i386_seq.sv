// fes_i386_seq: the next uop of a fixed sequence (fes_i386_pkg SEQ_*).
//
// DC decodes an instruction to its first uop. An instruction that takes a
// fixed sequence of uops, decoded without the microcode ROM, keeps its place
// in dc while they issue: as D issues each but the last, dc takes the next
// one from here, worked out from dc itself. Every field D reads is then a
// register at every step, and D issues the sequence one uop per cycle.
//
// Each step changes only the fields that differ from the uop before, so a
// bit the sequence keeps is a hold, which DC's input select absorbs.
//
// SEQ_CALL (CALL rel): the push of the next EIP, then JMP rel by the
// displacement the push carries in its imm, which a push does not use. The
// push already has the jump's sizes, EIP length and address size.
module fes_i386_seq (
    input  fes_i386_pkg::decoded_t d,
    output fes_i386_pkg::decoded_t n
);
    always_comb begin
        n = d;
        n.step = d.step + 3'd1;
        n.more = 1'b0;
        case (d.seq)
            fes_i386_pkg::SEQ_CALL: begin
                n.uop.sp_write = 1'b0;
                n.uop.cls = fes_i386_pkg::UOP_BRANCH;
                n.uop.op = 5'd1;  // BR_JMP
                n.uop.mem.kind = fes_i386_pkg::MEM_NONE;
                n.uop.ctl.end_of_instruction = 1'b1;
                n.stk = fes_i386_pkg::STK_NONE;
                n.push_eip = 1'b0;
            end
            default: ;
        endcase
    end
endmodule
