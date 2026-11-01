// SPDX-License-Identifier: MIT
// fes_i386_cpu as far as Phase 1 has built it (docs/i386-cpu-design.md): the
// front end (F1 to Q) behind the L1I, then D, AG, M1, M2, EX and WB for the
// real-mode instructions fes_i386_decode lists, with their memory forms on
// the L1D, and the bus interface on HPS DDR port 0 (fes_i386_l1i,
// fes_i386_l1d, fes_i386_biu). There is no L2 or paging yet.
//
//   DC  decode (fes_i386_decode, fes_i386_amode) from Q's output register
//   D   the GPR file and segment registers read; forwarding for data
//       operands (fes_i386_dops) and LEA's address (fes_i386_dfwd); EIP
//   AG  a memory operand's or LEA's address, a branch's target (fes_i386_agu)
//   M1  the segment limit check; the data port's address
//   M2  the loaded bytes, aligned and merged with older stores not yet written
//   EX  ALU, shifter, flags, branch resolution (fes_i386_ex); multiply and
//       divide (fes_i386_muldiv), over several cycles
//   WB  in-order commit, stores and I/O accesses included
//
// Branches are predicted not taken. EX resolves them; a taken one redirects
// F1 from WB and flushes every younger uop, which costs 10 cycles.
//
// A load whose bytes have not arrived when it is in M2 freezes D to WB: every
// stage behind DC keeps its uop, and nothing retires, until they do (freeze).
// Nothing moves relative to anything else, so the forwarding distances, AG's
// late operands, the store merge and D's forwarding tables all hold as they
// are; the tables only need clearing on a flush. A multiply or divide in EX
// freezes them the same way while it works, an I/O access in WB until the
// machine completes it, and a store in WB until the write buffer has room.
//
// An uncached load (MMIO) reads its device only as the oldest uop, with
// nothing in EX or WB, so never on a wrong path. One that reaches M2 behind
// older uops takes no data: the L1D marks it (FAULT_REPLAY), and when it
// reaches WB, WB flushes everything younger and restarts the front end at
// its instruction, which then issues one uop at a time (serial) until it
// ends. Microcode writes nothing architectural before its last uop that can
// fault, so restarting an instruction is safe wherever a fault is.
//
// A store or DMA write into the bytes the front end has fetched restarts the
// instruction after the one in WB the same way, once that one ends, so it
// runs the new bytes (self-modifying code, the coherent profile; queue16
// leaves the 16 bytes after a storing instruction as fetched).
//
// A fault (#UD, #GP(0), #SS(0), #NM, #DE) is taken at WB with nothing of its
// instruction retired: WB flushes, and DC takes the exception's entry into
// the microcode routine INT uses, which pushes FLAGS, CS and the faulting
// instruction's IP and jumps through the vector table. An encoding this
// build does not implement stops the CPU at WB, and so does a fault while an
// exception is delivered (double fault and shutdown are not built). HLT
// halts it until an interrupt.
//
// INTR and NMI are taken at an instruction boundary, or in HLT: WB flushes as
// for a fault, with nothing of the next instruction retired, and DC takes the
// same routine's entry with the interrupt's vector, from INTA for INTR, so
// the routine pushes the next instruction's IP.
//
// Protected mode (CR0.PE) at CPL 0, without gates, the IDT, the LDT or
// paging: MOV CRn, LMSW, SMSW, LGDT, SGDT, LIDT and SIDT; segment loads from
// the GDT's descriptors, with their checks, error codes and accessed bit, in
// microcode; and far JMP to a code segment. A real-mode segment load keeps a
// data segment's cached limit and attributes (unreal mode), and sets CS's to
// the 386's real-mode values. An exception or interrupt while PE is set
// stops the CPU (trace_pm_stop), as delivery through the IDT is not built.
//
// init_* is the architectural state reset loads. A machine ties it to the
// 386 reset state; the lockstep harness loads a test's initial state.
// trace_* and arch_* show each retirement: on the edge an instruction
// retires, trace_retire rises and arch_* hold the state after it.
module fes_i386_cpu #(
    parameter logic [31:0] RAM_BASE = 32'h3400_0000  // the HPS address of guest physical 0
) (
    input  logic             clk,
    input  logic             cold_reset,   // the machine's start: also empties the write buffer
    input  logic             reset,
    input  logic [7:0][31:0] init_gpr,
    input  logic [31:0]      init_eflags,
    input  logic [31:0]      init_cr0,     // of which this build holds PE, MP, EM, TS, ET and PG
    input  logic [31:0]      init_cr2,
    input  logic [31:0]      init_cr3,
    input  logic [31:0]      init_gdtr_base,
    input  logic [15:0]      init_gdtr_limit,
    input  logic [31:0]      init_idtr_base,
    input  logic [15:0]      init_idtr_limit,
    input  logic [31:0]      init_eip,
    input  logic [5:0][15:0] init_sel,     // fes_i386_pkg::seg_e order
    input  logic [5:0][31:0] init_base,
    input  logic [5:0][31:0] init_limit,
    input  logic [5:0][15:0] init_ar,      // descriptor bits 8-23, the limit nibble clear
    // A20M, and the attribute map: 640 KiB to 1 MiB in 16 KiB slots
    // (fes_i386_pkg::attr_e), RAM elsewhere below ram_top MiB.
    input  logic             a20m,
    input  logic [23:0][1:0] attr_upper,
    input  logic [6:0]       ram_top,
    // The self-modifying-code profile, sampled at reset: coherent (0) or
    // queue16 (1); see Self-modifying code.
    input  logic             queue16,
    // HPS DDR port 0 (fes.memory.hps-ddr), guest physical 0 at RAM_BASE.
    output logic [27:0]      ddr_address,
    output logic [7:0]       ddr_burstcount,
    output logic             ddr_read,
    output logic             ddr_write,
    output logic [127:0]     ddr_writedata,
    output logic [15:0]      ddr_byteenable,
    input  logic             ddr_waitrequest,
    input  logic [127:0]     ddr_readdata,
    input  logic             ddr_readdatavalid,
    // MMIO: blocking dword reads with byte enables and posted writes, in
    // order (fes_i386_biu), at physical dword addresses.
    output logic             mmio_valid,
    output logic             mmio_write,
    output logic [fes_i386_pkg::PA-3:0] mmio_address,
    output logic [3:0]       mmio_byteenable,
    output logic [31:0]      mmio_wdata,
    input  logic             mmio_ready,
    input  logic             mmio_rvalid,
    input  logic [31:0]      mmio_rdata,
    // Machine DMA: one physical byte through the write buffer's order,
    // coherent with both L1s; held until dma_done.
    input  logic             dma_valid,
    input  logic             dma_write,
    input  logic [23:0]      dma_address,
    input  logic [7:0]       dma_wdata,
    output logic             dma_done,
    output logic [7:0]       dma_rdata,
    // FLUSH: empty the write buffer and invalidate both L1s; held until done.
    input  logic             flush_request,
    output logic             flush_done,
    // I/O: the access of the uop in WB, at commit (io_req), held until the
    // machine completes it (io_ack, IN's data in io_rdata on that edge). The
    // port, the size (a byte, word or dword: 0, 1, 2) and OUT's data stay put
    // while io_req is high; a request after an ack is the next access.
    output logic             io_req,
    output logic             io_write,
    output logic [15:0]      io_port,
    output logic [1:0]       io_size,
    output logic [31:0]      io_wdata,
    input  logic             io_ack,
    input  logic [31:0]      io_rdata,
    // Interrupts: INTR, a level the machine's PICs hold until the CPU
    // acknowledges it, and NMI, taken on its rising edge. Taking INTR, the
    // CPU asks for the vector (inta_req) and holds the request until the
    // machine returns it (inta_ack, the vector in inta_vector on that edge).
    input  logic             intr,
    input  logic             nmi,
    output logic             inta_req,
    input  logic             inta_ack,
    input  logic [7:0]       inta_vector,
    output logic             trace_retire,
    output logic             trace_exception,      // the retirement delivered exception trace_vector
    output logic [7:0]       trace_vector,
    output logic             trace_halt,           // stopped: in HLT, until an interrupt wakes it, or ...
    output logic             trace_unimplemented,  // ... for good: an encoding not built yet, or a fault in a
                                                   // delivery (trace_vector 8: double fault), or ...
    output logic             trace_pm_stop,        // ... in protected mode, exception trace_vector with error code
                                                   // trace_error, or an NMI (vector 2) or INTR (vector FFh), whose
                                                   // delivery through the IDT is not built
    output logic [15:0]      trace_error,
    output logic             trace_issue,          // D issued this cycle (lockstep observability)
    output logic             trace_md_wait,        // D to WB wait for a multiply or divide in EX (ditto)
    output logic             trace_fetch,          // F1 sent a block to the L1I (ditto) ...
    output logic [27:0]      trace_fetch_block,    // ... this one, linear address / 16
    output logic             trace_dread,          // M1 sent a load to the L1D (ditto)
    output logic [31:0]      trace_dread_addr,     // ... its linear byte address
    output logic             trace_mem_wait,       // D to WB wait for the L1D or the write buffer, or F2 for the L1I
    output logic             trace_mem_idle,       // the bus interface has nothing to do or finish
    output logic             trace_replay,         // WB restarts an instruction for an uncached load
    output logic             trace_smc,            // ... for self-modifying code (see Self-modifying code)
    output logic             trace_smc_open,       // a DMA write into fetched bytes now restarts the next instruction (ditto)
    output logic             trace_ex_store,       // a store in EX, one stage before it can retire
    // The store WB commits: the bytes trace_store_mask names of the eight
    // from trace_store_addr's dword, at linear addresses.
    output logic             trace_store,
    output logic [31:0]      trace_store_addr,
    output logic [7:0][7:0]  trace_store_data,
    output logic [7:0]       trace_store_mask,
    output logic [7:0][31:0] arch_gpr,
    output logic [31:0]      arch_eflags,
    output logic [31:0]      arch_cr0,     // as MOV r32, CR0 reads it: bits 30-5 ones, as Bochs's 386
    output logic [31:0]      arch_cr2,
    output logic [31:0]      arch_cr3,
    output logic [31:0]      arch_gdtr_base,
    output logic [15:0]      arch_gdtr_limit,
    output logic [31:0]      arch_idtr_base,
    output logic [15:0]      arch_idtr_limit,
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
    localparam int PA = fes_i386_pkg::PA;

    // The L1I's fetch port: mem_data is the block at the address registered on
    // the last edge with mem_load, once mem_valid says it is there. The L1D's
    // port: M1 drives a load's address, the L1D registers it on the edge M1
    // advances, and the 8 bytes from the address's dword arrive in M2 once
    // dmem_valid says so. A retiring store writes the bytes dmem_wmask names,
    // from its address's dword, on the edge it retires; the L1D reads before
    // it writes on the same edge.
    logic [27:0]      mem_block;
    logic             mem_load;
    logic [15:0][7:0] mem_data;
    logic             mem_valid;
    logic [31:0]      dmem_addr;
    logic             dmem_read;
    logic [7:0][7:0]  dmem_rdata;
    logic             dmem_valid;
    logic             dmem_write;
    logic [31:0]      dmem_waddr;
    logic [7:0][7:0]  dmem_wdata;
    logic [7:0]       dmem_wmask;

    // ---- Architectural state, written at WB -------------------------------------
    logic [7:0][31:0] gpr;
    logic [17:0]      eflags;
    logic [31:0]      eip;
    logic [5:0][15:0] seg_sel, seg_ar, seg_hi1;  // seg_hi1: base[31:16] + 1
    logic [5:0][31:0] seg_base, seg_limit;
    // Microcode temporaries: a far transfer's selector (T0) and offset (T1),
    // a segment load's selector (T0) and descriptor (T2, T3).
    logic [15:0]      t0;
    logic [31:0]      t1, t2, t3;
    // CR0 as this build holds it: PE, MP, EM, TS, ET and PG. WAIT raises
    // #NM when MP and TS are both set, CLTS clears TS, and MOV CR0 and LMSW
    // write it (SYSOP_CR0, SYSOP_LMSW). CR0 reads with bits 30-5 set, as
    // Bochs's 386 (cr0_rd). CR2 and CR3 hold what MOV writes.
    logic             cr0_pe, cr0_mp, cr0_em, cr0_ts, cr0_et, cr0_pg;
    logic [31:0]      cr0_rd, cr2, cr3;
    assign cr0_rd = {cr0_pg, 26'h3ff_ffff, cr0_et, cr0_ts, cr0_em, cr0_mp, cr0_pe};
    // GDTR and IDTR, with GDTR's limit less 7 (a descriptor's last byte) and
    // IDTR's less 3 (a real-mode vector's), and whether those are below zero.
    logic [31:0]      gdtr_base, idtr_base;
    logic [15:0]      gdtr_limit, idtr_limit, gdt_lim7, idt_lim3;
    logic             gdt_tiny7, idt_tiny3;
    // Each segment's access check, so the check in M1 is one compare or two
    // (fes_i386_pkg::seg_chk): the limit less 0, 1 and 3 for 1-, 2- and
    // 4-byte accesses, or an expand-down segment's upper bound so less, and
    // whether that is below zero (lim, tiny); an expand-down segment's limit
    // (low, expd); and whether it refuses reads (not present, or execute-only
    // code) or writes (not present, code, or read-only data) (rbad, wbad).
    // A real-mode load keeps a segment's limit and type, setting P and S.
    logic [5:0][2:0][31:0] seg_lim;
    logic [5:0][2:0]       seg_tiny;
    logic [5:0][31:0]      seg_low;
    logic [5:0]            seg_expd, seg_rbad, seg_wbad;
    logic             code32;
    assign code32 = seg_ar[SEG_CS][14];
    // EX's running EFLAGS (see EX), which D reads for a string's DF.
    logic [17:0]      ex_flags;

    // ---- Pipeline registers -------------------------------------------------------
    pipe_t ag_s, m1_s, m2_s, ex_s, wb_s;
    // halted: in HLT (hlt) or stopped for good (trace_unimplemented), kept as a
    // register of its own, which D's issue and the ports read.
    logic  wb_flush, wb_stop, halted, hlt;
    // A uop that retires, faults or stops in WB this cycle, and whether it
    // flushes everything younger.
    assign wb_stop = wb_s.valid && (wb_s.uop.ctl.fault != fes_i386_pkg::FAULT_NONE
                                    || (wb_s.uop.cls == fes_i386_pkg::UOP_SPECIAL && wb_s.uop.op == 5'd1));

    // A load in M2 waiting for its bytes freezes D to WB (m2_load: M2 holds a
    // load, registered as it enters; one that fails the limit check in M1
    // waits too, then faults). WB waits as well, so a taken branch or a fault
    // there takes effect, and flushes, once the bytes arrive, even when the
    // load is on the wrong path. So does a multiply or divide while it works
    // in EX (md_busy, registered as it enters EX; see EX), an I/O access in
    // WB until the machine has completed it (io_wait, set as the uop enters
    // WB and cleared on the completing edge; see WB), and an INTR being taken
    // until the machine has returned its vector (int_wait, set at the
    // boundary and cleared on the returning edge; see Interrupts), and a
    // store in WB while the write buffer has no room for it (st_wait, see
    // WB). Each wait is one register, so the freeze stays one LUT of six.
    logic freeze, m2_load, md_busy, io_wait, int_take, int_nmi, int_wait, st_wait;
    logic int_next;  // an interrupt is taken on this edge (see Interrupts)
    assign freeze = m2_load && !dmem_valid || md_busy || io_wait || int_wait || st_wait;

    // A fault in WB is delivered (exc_take): WB flushes, the front end and DC
    // restart, and DC takes the exception's entry the cycle after
    // (exc_inject). exc_active holds until the delivery's last uop retires; a
    // fault before then stops the CPU instead.
    logic       exc_take, exc_active, exc_inject;
    logic [7:0] exc_vec;

    // What WB does with its uop once D to WB are not frozen, decided as the
    // uop enters WB (see WB): wb_flushes, flush everything younger (a taken
    // branch, a fault, HLT); wb_redir, restart the front end and DC (a taken
    // branch, or a fault it delivers); wb_deliver, deliver its fault. So each
    // of these is one gate from freeze. An interrupt being taken (int_take)
    // flushes and restarts them in place of the uop's own decisions
    // (int_go).
    logic wb_flushes, wb_redir, wb_deliver, int_go;
    assign wb_flush = !freeze && (wb_flushes || int_take);
    assign exc_take = !freeze && wb_deliver && !int_take;
    assign int_go = !freeze && int_take;
    // A replay in WB (rp_go) flushes as a fault does but delivers nothing: the
    // front end restarts at its instruction through start, two cycles on,
    // when start_linear holds CS.base + EIP, and D issues nothing until then.
    logic rp_go;
    assign rp_go = !freeze && wb_flushes && !int_take && wb_s.valid
                && wb_s.uop.ctl.fault == fes_i386_pkg::FAULT_REPLAY;
    // A replay of an instruction after a store into the bytes the front end
    // holds (see Self-modifying code): smc_due once the storing instruction
    // has ended, smc_defer while queue16 waits for an instruction past its
    // window, smc_mark as the instruction's first uop enters WB, wb_smc while
    // it is there, smc_go as WB restarts it.
    logic smc_due, smc_defer, smc_mark, wb_smc, smc_go;

    // ---- Start and redirect -------------------------------------------------------
    // After reset the front end starts at CS.base + EIP, one cycle after the
    // sum is registered.
    logic [1:0]  start;
    logic [31:0] cs_eip, start_linear;
    logic        redirect;
    logic [31:0] redirect_linear, redirect_eip;
    assign cs_eip = seg_base[SEG_CS] + eip;
    always_ff @(posedge clk) begin
        if (reset || rp_go) begin
            start <= 2'd1;
        end else if (start != 2'd0) begin
            start <= start + 2'd1;
        end
        start_linear <= cs_eip;
    end
    // A delivery refetches from the faulting instruction, or the one before
    // (start_linear is a cycle old), which the delivery's far jump discards.
    // D's EIP is the faulting instruction's, cut to 16 bits: the IP the
    // routine pushes, which D's CS-limit check then passes. An interrupt's
    // is the next instruction's. The address selects key on WB's fault field
    // and int_take, registers (a taken branch has neither), so the fetch
    // address stays off exc_take's path.
    logic wb_faulted;
    assign wb_faulted = wb_s.uop.ctl.fault != fes_i386_pkg::FAULT_NONE;
    assign redirect = start == 2'd2 || !freeze && (wb_redir || int_take);
    assign redirect_linear = start == 2'd2 || wb_faulted || int_take ? start_linear : wb_s.target;
    assign redirect_eip = start == 2'd2 ? eip : wb_faulted || int_take ? {16'd0, eip[15:0]} : wb_s.target_eip;

    // ---- F1 to Q ------------------------------------------------------------------
    logic        q_valid, d_ready, fe_mem_load;
    logic [27:0] fetch_next;
    fetched_t    q;
    fes_i386_frontend frontend (
        .clk, .reset, .redirect, .redirect_linear, .code32, .mem_block, .mem_load(fe_mem_load), .fetch_next,
        .mem_data, .mem_valid, .q_valid, .q, .d_ready
    );
    assign mem_load = fe_mem_load && !halted && !wb_stop;

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
    logic [2:0] q_stk;              // dc_q.stk and .seq, apart so no field of dc_q reads another
    logic [4:0] q_seq;
    logic       q_direct, q_xlat;   // no base or index (a moffs, the vector's entry); XLAT's [BX + AL]
    uop_t       q_uop;              // dc_q.uop, .lea and the address registers, for dc_q.rd_str
    logic       q_lea, q_has_base, q_has_index;
    logic [2:0] q_base, q_index;
    fes_i386_decode decode (
        .q, .code32, .stack(dc_stack), .wait_nm(cr0_mp && cr0_ts), .pe(cr0_pe), .uop(q_uop), .opsize32(dc_q.opsize32), .lea(q_lea), .lea_early(dc_q.lea_early),
        .lea_disp(dc_q.lea_disp), .branch(dc_branch), .consumes(dc_q.consumes), .reads_seg(dc_q.reads_seg),
        .seg_index(dc_q.seg_index), .stop(dc_q.stop), .stk(q_stk), .push_eip(dc_q.push_eip),
        .cs_base(dc_q.cs_base), .seq(q_seq), .sel_part(dc_q.sel_part), .far_jmp(dc_q.far_jmp), .far_chk(dc_q.far_chk),
        .far_sel(dc_q.far_sel), .wseg(dc_q.wseg), .upc(dc_q.upc), .direct(q_direct), .xlat(q_xlat),
        .m_seg(dc_q.m_seg), .slot(dc_q.slot), .str_op(dc_q.str_op), .str_rep(dc_q.str_rep), .ecnt(dc_q.ecnt),
        .b_sys(dc_q.b_sys), .sys_idx(dc_q.sys_idx)
    );
    // A stack operation's address is ESP, or EBP for LEAVE, with no index:
    // chosen here, so D's operand lookups start from dc's register.
    logic       am_has_base, am_has_index;
    logic [2:0] am_base, am_index;
    logic [1:0] am_scale;
    fes_i386_amode amode (
        .modrm(q_two ? q.bytes[2] : q.bytes[1]), .sib(q_two ? q.bytes[3] : q.bytes[2]), .addr32(q_uop.mem.addr32), .has_base(am_has_base),
        .base(am_base), .has_index(am_has_index), .index(am_index), .scale(am_scale), .stack(dc_stack)
    );
    assign dc_q.uop = q_uop;
    assign dc_q.lea = q_lea;
    assign dc_q.stk = q_stk;
    // XLAT's address is BX (or EBX) plus AL, zero-extended (index8).
    assign q_has_base = q_stk != fes_i386_pkg::STK_NONE || am_has_base && !q_direct || q_xlat;
    assign q_base = q_stk == fes_i386_pkg::STK_LEAVE ? 3'd5 : q_stk != fes_i386_pkg::STK_NONE ? 3'd4
                  : q_xlat ? 3'd3 : am_base;
    assign q_has_index = q_stk == fes_i386_pkg::STK_NONE && (am_has_index && !q_direct || q_xlat);
    assign q_index = q_xlat ? 3'd0 : am_index;
    assign dc_q.has_base = q_has_base;
    assign dc_q.base = q_base;
    assign dc_q.has_index = q_has_index;
    assign dc_q.index = q_index;
    assign dc_q.rd_str = fes_i386_pkg::str_reads(q_uop, q_lea, q_has_base, q_base, q_has_index, q_index);
    assign dc_q.scale = q_stk == fes_i386_pkg::STK_NONE && !q_xlat ? am_scale : 2'd0;
    assign dc_q.index8 = q_xlat;
    // The ModRM operand's address registers, kept for a later uop when a
    // stack uop holds the fields above (POP r/m).
    assign dc_q.m_has_base = am_has_base && !q_direct;
    assign dc_q.m_base = am_base;
    assign dc_q.m_has_index = am_has_index && !q_direct;
    assign dc_q.m_scale = am_scale;
    assign dc_q.esp_adj = 6'd0;
    assign dc_q.esp_wrap = 4'd0;
    assign dc_q.length = q.length;
    assign dc_q.seq = q_seq;
    assign dc_q.step = 3'd0;
    assign dc_q.more = q_seq != fes_i386_pkg::SEQ_NONE;
    assign dc_q.far_t = 1'b0;
    assign dc_q.a_t1 = 1'b0;
    assign dc_q.b_t1 = 1'b0;
    assign dc_q.disp_t1 = 1'b0;
    assign dc_q.t_rd = 1'b0;
    assign dc_q.popad_esp = 1'b0;
    assign dc_q.b_far = 1'b0;
    assign dc_q.plus2 = 1'b0;
    assign dc_q.nochk = 1'b0;
    // The string engine's first uop loads D's copies; the rest are
    // fes_i386_seq's.
    assign dc_q.str_load = q_seq == fes_i386_pkg::SEQ_STRING || q_seq == fes_i386_pkg::SEQ_ENTER;
    assign dc_q.str_a = 1'b0;
    assign dc_q.str_b = 1'b0;
    assign dc_q.str_two = 1'b0;
    assign dc_q.str_fin = 1'b0;
    assign dc_q.str_cnt = 1'b0;
    assign dc_q.b_fp = 1'b0;
    assign dc_q.x_prev = 1'b0;
    assign d_ready = !dc_skid_valid;
    // A fixed sequence's uops issue from dc in turn: as each but the last
    // issues, dc takes the next (fes_i386_seq), whose rewritten fields come
    // from the instruction's first uop, which dc0 keeps, so none of dc's is
    // held at some steps and not others.
    // A microcode routine's next uop is the ROM's word at dc.upc, read as an
    // M10K with a registered output: it is there the cycle after dc settles
    // (rom_ok: dc held over the last edge), so D waits a cycle for each
    // (uc_wait). Both are registers, off the M10K's pins.
    decoded_t dc_next, dc0;
    fes_i386_pkg::ucode_t rom_q;
    logic       rom_ok, uc_wait;
    fes_i386_ucode_rom ucode (.clk, .addr(dc.upc), .q(rom_q));
    always_ff @(posedge clk) rom_ok <= dc_valid && !issue;
    assign uc_wait = dc.seq == fes_i386_pkg::SEQ_UCODE && dc.more && !rom_ok;
    logic [2:0] str_cnt3;  // the string engine's count is 2, 1, 0 (D)
    fes_i386_seq seq (.d(dc), .d0(dc0), .w(rom_q), .cnt(str_cnt3), .n(dc_next));
    // An exception's entry: a NOP that starts the ROM's vector routine with
    // the fault's vector, at length 0, so the routine pushes D's EIP, the
    // faulting instruction's. It goes in through dc_skid the cycle after the
    // fault's flush, which has emptied DC and Q.
    decoded_t dc_exc;
    always_comb begin
        dc_exc = '0;
        dc_exc.uop.cls = fes_i386_pkg::UOP_SPECIAL;  // SPECIAL_NOP
        dc_exc.far_sel = {6'd0, exc_vec, 2'b00};
        dc_exc.upc = fes_i386_ucode_pkg::UC_VECTOR;
        dc_exc.seq = fes_i386_pkg::SEQ_UCODE;
        dc_exc.more = 1'b1;
    end
    // A string's first element ends the instruction when its count is 0 or 1
    // (s_end1, see the string engine), which seq does not know when it
    // builds that element.
    logic dc_more, s_end1, s_kill1;
    assign dc_more = dc.more && !s_end1;
    assign dc_load = !dc_valid || (issue && !dc_more);
    always_ff @(posedge clk) begin
        if (reset || redirect) begin
            dc_valid <= 1'b0;
            dc_skid_valid <= 1'b0;
        end else begin
            if (dc_load) dc_valid <= dc_skid_valid || q_valid;
            if (exc_inject) begin
                dc_skid_valid <= 1'b1;
            end else if (dc_skid_valid) begin
                if (dc_load) dc_skid_valid <= 1'b0;
            end else if (q_valid && !dc_load) begin
                dc_skid_valid <= 1'b1;
            end
        end
        if (!dc_valid || issue) dc <= dc_valid && dc_more ? dc_next : dc_skid_valid ? dc_skid : dc_q;
        if (dc_load) dc0 <= dc_skid_valid ? dc_skid : dc_q;
        if (exc_inject) dc_skid <= dc_exc;
        else if (!dc_skid_valid) dc_skid <= dc_q;
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
    logic        past, wrap16, cs_gp, seg_cs_ffff, seg_cs_small, seg_cs_flat, seg_ss_rm, seg_ss_big, cs_odd;
    // The CS-limit check is built for 16-bit code under a limit of FFFFh or
    // more (past it, wrap16), and 32-bit code under limit FFFFFFFFh (flat);
    // code under a smaller limit is FAULT_UNIMPLEMENTED (cs_odd).
    // seg_cs_ffff, seg_cs_small and seg_cs_flat are registered as CS is
    // written.
    assign cs_odd = code32 ? !seg_cs_flat : seg_cs_small;
    assign eip16 = {1'b0, d_eip[15:0]} + {13'd0, dc.length};
    assign past = !code32 && (d_eip[31:16] != 16'd0 || eip16 > 17'h1_0000);
    assign cs_gp = past && seg_cs_ffff;
    assign wrap16 = past && !seg_cs_ffff;
    assign d_eip_next = code32 ? d_eip + {28'd0, dc.length} : {15'd0, eip16};

    // A 32-bit near CALL in 16-bit code does not truncate its target. Check it
    // on the first (push) uop so an out-of-limit target faults before WB can
    // commit the return-address store or ESP update. The next EIP is EIP's
    // low half plus the length, so the target is that plus imm, and its high
    // half is imm's plus what the low halves carry: one sum of the three low
    // halves (a 3:2 row, then one carry chain), beside two compares of imm's
    // high half. A carry of 2 needs the next EIP past 10000h, which faults
    // the same way (cs_gp, wrap16), so the sum's bit 17 is left out.
    logic [15:0] call_s, call_k;
    logic [16:0] call_low;
    logic        call_target_zero, call_target_past, call_target_gp, call_target_unimpl;
    assign call_s = d_eip[15:0] ^ dc.uop.imm[15:0] ^ {12'd0, dc.length};
    assign call_k = d_eip[15:0] & dc.uop.imm[15:0] | (d_eip[15:0] | dc.uop.imm[15:0]) & {12'd0, dc.length};
    assign call_low = {1'b0, call_s} + {call_k, 1'b0};
    assign call_target_zero = call_low[16] ? dc.uop.imm[31:16] == 16'hffff : dc.uop.imm[31:16] == 16'd0;
    assign call_target_past = dc.seq == fes_i386_pkg::SEQ_CALL && dc.step == 3'd0 && dc.opsize32 && !code32
                           && !call_target_zero;
    assign call_target_gp = call_target_past && seg_cs_ffff;
    assign call_target_unimpl = call_target_past && !seg_cs_ffff;

    uop_t        u;
    logic        opsize32, lea, lea_early, consumes, reads_seg, stop;
    // The string engine's copies (see below).
    logic [31:0] r_src, r_dst, r_fp, r_cnt, s_tgt;
    logic        s_down;
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
    // The stack's width for this uop: SS.B, but for a protected-mode POP
    // Sreg's step of ESP (nochk) the width its read of the selector had (a
    // STK_PEEK uop, the last before it), as Bochs pops before the load.
    logic sst, pop_ss32;
    assign sst = dc.nochk ? pop_ss32 : ss32;
    always_ff @(posedge clk) if (issue && stk == fes_i386_pkg::STK_PEEK) pop_ss32 <= ss32;
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
            // A slot, as a shift of the signed slot (no carry chain); its
            // step of ESP goes past the eight slots, down from below ESP.
            fes_i386_pkg::STK_PEEK: begin
                st_disp = dc.opsize32 ? {{26{dc.slot[3]}}, dc.slot, 2'b00} : {{27{dc.slot[3]}}, dc.slot, 1'b0};
                st_sp_disp = dc.slot[3] ? -(st_size << 3) : st_size << 3;
            end
            // A probe reads the lowest slot: INT's three words, or a far
            // call's two pushes below ESP.
            fes_i386_pkg::STK_PROBE: st_disp = dc.uop.probe == fes_i386_pkg::PROBE_INT ? -32'd6 : -(st_size << 1);
            fes_i386_pkg::STK_RETF: st_sp_disp = (dc.slot[0] ? st_size * 32'd3 : st_size << 1) + dc.uop.imm;
            default: ;
        endcase
        if (!sst) begin
            st_disp = {16'b0, st_disp[15:0]};
            st_sp_disp = {16'b0, st_sp_disp[15:0]};
        end
    end
    // The memory operand: whether it reads and writes, its segment, and the
    // limit check's operands for its size.
    logic        mem, mem_reads, mem_writes, addr_use, a_addr, a_addr32, d_unimpl;
    logic [2:0]  mseg;
    logic [1:0]  msize;  // 1, 2 or 4 bytes as 0, 1, 2
    fes_i386_pkg::mem_kind_e mkind;
    assign mkind = dc.uop.mem.kind;
    assign a_addr = dc.lea || mkind != fes_i386_pkg::MEM_NONE;
    assign a_addr32 = stk != fes_i386_pkg::STK_NONE ? sst : dc.uop.mem.addr32;
    assign mem = mkind != fes_i386_pkg::MEM_NONE;
    assign mem_reads = mkind == fes_i386_pkg::MEM_LOAD || mkind == fes_i386_pkg::MEM_RMW;
    assign mem_writes = mkind == fes_i386_pkg::MEM_STORE || mkind == fes_i386_pkg::MEM_RMW;
    assign addr_use = a_addr;
    assign mseg = 3'(dc.uop.mem.seg);
    assign msize = dc.uop.mem.bytes[2] ? 2'd2 : dc.uop.mem.bytes[1] ? 2'd1 : 2'd0;
    // INT's probe assumes SS's real-mode limit, FFFFh, under which only a
    // slot that straddles the 64 KiB wrap faults.
    logic str_ss, str_expd;  // the string engine's second address is SS's (ENTER), else ES's; expand-down
    logic [2:0] str_dseg;
    assign str_ss = dc.seq == fes_i386_pkg::SEQ_ENTER;
    assign str_dseg = str_ss ? 3'(SEG_SS) : 3'(fes_i386_pkg::SEG_ES);
    assign str_expd = seg_expd[str_dseg];
    // A count-zero REP's first element is rewritten to a NOP below. Its
    // unused addresses must not raise an unsupported-segment or wrap fault.
    // INT's probe is built for a 16-bit stack under SS's real-mode limit
    // (seg_ss_rm), or one whose limit passes 10000h (seg_ss_big), where no
    // slot can fault.
    assign d_unimpl = !s_kill1 && (wrap16 || cs_odd || call_target_unimpl
                   || (dc.uop.probe != fes_i386_pkg::PROBE_NONE && !seg_ss_rm && !seg_ss_big));

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
    assign t_wait = dc.t_rd && t_busy;
    // A protected-mode segment load reads nothing for a null selector, and
    // writes the accessed bit only when the descriptor's is clear (T3's bit
    // 8). A CR0 write (T1) with PG raises #GP(0) when PE is clear and stops
    // as not built when it is set: paging is not built.
    logic t0_null, cr0_gp, cr0_pg_unimpl;
    assign t0_null = t0[15:2] == 14'd0;
    assign cr0_gp = dc.uop.cls == fes_i386_pkg::UOP_SYSTEM && dc.uop.op == 5'(fes_i386_pkg::SYSOP_CR0) && t1[31]
                 && !t1[0];
    assign cr0_pg_unimpl = dc.uop.cls == fes_i386_pkg::UOP_SYSTEM && dc.uop.op == 5'(fes_i386_pkg::SYSOP_CR0) && t1[31]
                        && t1[0];
    always_comb begin
        u = dc.uop;
        if (stk != fes_i386_pkg::STK_NONE) u.mem.addr32 = sst;
        if (dc.uop.desc != 2'd0 && (t0_null || dc.uop.desc == 2'd3 && t3[8])) u.mem.kind = fes_i386_pkg::MEM_NONE;
        // POPAD's ESP image counts only on a 16-bit stack.
        if (dc.popad_esp && !(dc.opsize32 && !ss32)) u.dst.valid = 1'b0;
        if (!s_kill1 && d_unimpl && dc.uop.ctl.fault == fes_i386_pkg::FAULT_NONE)
            u.ctl.fault = fes_i386_pkg::FAULT_UNIMPLEMENTED;
        if (!s_kill1 && far_bad && dc.uop.ctl.fault == fes_i386_pkg::FAULT_NONE)
            u.ctl.fault = seg_cs_ffff ? fes_i386_pkg::FAULT_GP0 : fes_i386_pkg::FAULT_UNIMPLEMENTED;
        if (!s_kill1 && (cs_gp || call_target_gp)) u.ctl.fault = fes_i386_pkg::FAULT_GP0;
        if (cr0_gp && u.ctl.fault == fes_i386_pkg::FAULT_NONE) u.ctl.fault = fes_i386_pkg::FAULT_GP0;
        if (cr0_pg_unimpl && u.ctl.fault == fes_i386_pkg::FAULT_NONE) u.ctl.fault = fes_i386_pkg::FAULT_UNIMPLEMENTED;
        u.str_down = s_down;
    end
    assign opsize32 = dc.opsize32;
    assign lea = dc.lea;
    assign lea_early = dc.lea_early;
    // A far pointer's selector is past its offset by the operand size,
    // wrapped by address size.
    // The interrupt vector's entry is at vector * 4 (far_sel), its selector
    // at + 2.
    // POP m's store through ESP reads ESP after the pop, which has not
    // moved it: the store waits for T1, so every older uop has retired and
    // ESP is the register file's. The displacement gains the operand size,
    // scaled as ESP is (esp_adj), less 10000h so scaled (esp_wrap) when a
    // 16-bit stack's SP wraps, which it does only from the top slot, since
    // a slot past FFFFh faults first.
    logic        sp_wraps;
    logic [31:0] disp_add;
    assign sp_wraps = !ss32 && (dc.opsize32 ? &gpr[4][15:2] : &gpr[4][15:1]);
    // A descriptor's part is at its selector's index times 8 in the table:
    // its low dword, its high dword (+4) or its access byte (+5). LGDT's and
    // SGDT's base is 2 bytes past the ModRM operand (plus2).
    assign disp_add = dc.plus2 ? 32'd2 : dc.sel_part ? st_size
                    : {sp_wraps ? -{12'd0, dc.esp_wrap} : 16'd0, 10'd0, dc.esp_adj};
    assign lea_disp = dc.uop.desc != 2'd0 ? {16'b0, t0[15:3], dc.uop.desc[1], 1'b0, dc.uop.desc[0] && dc.uop.desc[1]}
                    : dc.uop.mem.system ? {16'b0, dc.far_sel[15:2], dc.sel_part, 1'b0}
                    : !dc.sel_part && !dc.plus2 && dc.esp_adj == 6'd0 ? dc.lea_disp
                    : dc.uop.mem.addr32 ? dc.lea_disp + disp_add : {16'b0, dc.lea_disp[15:0] + disp_add[15:0]};
    assign consumes = dc.consumes;
    assign reads_seg = dc.reads_seg;
    assign seg_index = dc.seg_index;
    assign stop = !s_kill1 && (dc.stop || d_unimpl || cs_gp || far_bad || call_target_gp || cr0_gp || cr0_pg_unimpl);

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
    assign d_writes = cur_valid && u.dst.valid && u.dst.space == fes_i386_pkg::RS_GPR && !s_kill1;

    // Data operands.
    logic [31:0] a_val, b_val, c_val;
    logic        a_pend, b_pend, c_pend;
    logic [7:0]  d_pend;
    logic [7:0][31:0] d_value;
    logic [2:0]  a_dist, b_dist, c_dist;
    fes_i386_dops dops (
        .clk, .reset(reset || wb_flush), .a_reg(u.src1.index[2:0]), .b_reg(u.src2.index[2:0]), .d_writes,
        .d_dst(u.dst.index[2:0]), .d_early(lea && lea_early), .advance(issue), .hold(freeze), .prod_valid, .prod_dst,
        .prod_early, .prod_sp, .gpr, .stage_value, .wb_sp(wb_s.sp_new), .a(a_val), .b(b_val), .c(c_val), .pend_a(a_pend), .pend_b(b_pend),
        .pend_c(c_pend), .dist_a(a_dist), .dist_b(b_dist), .dist_c(c_dist), .pend_all(d_pend), .value_all(d_value)
    );

    // A memory operand's or LEA's address; a stack operation's is ESP or EBP.
    logic        lea_stall, sp_wait, d_sp;
    logic [1:0]  late, late_hi;
    logic [31:0] row, last;
    logic [15:0] sp_hi;
    assign d_sp = cur_valid && u.sp_write;
    fes_i386_dfwd dfwd (
        .clk, .reset(reset || wb_flush), .has_base(a_addr && dc.has_base), .base(dc.base),
        .has_index(a_addr && dc.has_index), .index(dc.index), .index8(dc.index8), .scale(dc.scale),
        .addr32(a_addr32), .d_writes, .d_dst(u.dst.index[2:0]), .d_kind(lea && lea_early ? EARLY : LATE), .d_sp,
        .advance(issue), .hold(freeze), .prod_valid, .prod_dst, .prod_kind, .prod_sp, .gpr, .stage_value,
        .wb_sp(wb_s.sp_new), .row, .last, .late, .late_hi, .stall(lea_stall), .sp_hi, .sp_wait
    );

    // ---- The string engine ----------------------------------------------------
    // While a string instruction or ENTER runs, D keeps copies of the next
    // source and destination offsets (r_src, r_dst), of the count (r_cnt)
    // and of ENTER's frame pointer (r_fp), so each step's address is a
    // register here and its uops issue without forwarding (fes_i386_seq).
    // A string's first uop loads them from ESI, EDI and ECX (CX by address
    // size), which it waits for, with DF: the elements then step by the
    // access's bytes, down when DF is set. It also keeps the next
    // instruction's linear address (s_tgt), where a REPE or REPNE element
    // that ends the instruction branches. ENTER's first uop, the push of
    // EBP, loads them from ESP and EBP. An offset wraps at its address's
    // width (a_addr32: the instruction's for a string, the stack's for
    // ENTER), keeping the copy's high half.
    logic [31:0] str_step, str_fin_off, str_off, str_daddr, str_dlim, c_count;
    logic        str_dbad;
    function automatic logic [31:0] keep_hi(input logic [31:0] old, input logic [31:0] v, input logic w32);
        keep_hi = w32 ? v : {old[31:16], v[15:0]};
    endfunction
    assign str_step = s_down ? -{28'd0, dc.uop.mem.bytes} : {28'd0, dc.uop.mem.bytes};
    // ENTER's last uop: ESP from the next push slot plus imm (the size less
    // imm16), which is also the slot its check reads.
    assign str_fin_off = keep_hi(r_dst, r_dst + dc.uop.imm, a_addr32);
    always_comb begin
        str_off = dc.str_a ? r_src : dc.str_fin ? str_fin_off : r_dst;
        if (!a_addr32) str_off = {16'b0, str_off[15:0]};
    end
    // A second address (st2): the destination copy's, in ES (SS for ENTER),
    // with its limit checked here; M1 raises that after the first's.
    logic [31:0] str_doff;
    assign str_doff = a_addr32 ? r_dst : {16'b0, r_dst[15:0]};
    assign str_daddr = seg_base[str_dseg] + str_doff;
    assign str_dlim = seg_lim[str_dseg][msize];
    assign str_dbad = str_doff > str_dlim || seg_tiny[str_dseg][msize] || seg_wbad[str_dseg]
                   || str_expd && str_doff <= seg_low[str_dseg];
    // The count, for fes_i386_seq, from registers: whether the copy is 0, 1
    // or 2 (r_c0, r_c1, r_c2, kept with it), or at ENTER's first uop its
    // level. A string's first element is built without it: when the string's
    // first uop issues, the count is ECX (CX by address size) through D's
    // forwarding, on its way into the copy. So that uop latches whether the
    // first element ends the instruction (s_end1: the count is 0, or 1 but
    // for CMPS, whose first load is not an element's end) and whether it does
    // nothing (s_kill1: 0). D then issues that element as the instruction's
    // last, or as a NOP that ends it, in the cycle it would anyway.
    logic r_c0, r_c1, r_c2;
    assign c_count = dc.uop.mem.addr32 ? d_value[1] : {16'b0, d_value[1][15:0]};
    assign str_cnt3 = dc.str_load && str_ss ? {dc.ecnt == 5'd2, dc.ecnt == 5'd1, dc.ecnt == 5'd0} : {r_c2, r_c1, r_c0};
    always_ff @(posedge clk) begin
        // The copies come from their registers' values, which are fixed:
        // ESP and EBP, or ESI and EDI, with no select by register number.
        if (issue && dc.str_load && str_ss) begin
            r_src <= keep_hi(d_value[5], d_value[5] - st_size, ss32);
            r_dst <= keep_hi(d_value[4], d_value[4] - (st_size << 1), ss32);
            r_fp <= keep_hi(d_value[4], d_value[4] - st_size, ss32);
            r_cnt <= {27'd0, dc.ecnt} - 32'd1;
            {r_c2, r_c1, r_c0} <= {dc.ecnt == 5'd3, dc.ecnt == 5'd2, dc.ecnt == 5'd1};
            s_down <= 1'b1;
        end else if (issue && dc.str_load) begin
            r_src <= dc.uop.mem.addr32 ? d_value[6] : {16'b0, d_value[6][15:0]};
            r_dst <= dc.uop.mem.addr32 ? d_value[7] : {16'b0, d_value[7][15:0]};
            r_cnt <= c_count;
            {r_c2, r_c1, r_c0} <= {c_count == 32'd2, c_count == 32'd1, c_count == 32'd0};
            s_down <= ex_flags[10];
            s_tgt <= seg_base[SEG_CS] + d_eip_next;
        end else if (issue) begin
            if (dc.str_a) r_src <= keep_hi(r_src, r_src + str_step, a_addr32);
            if (dc.str_b || dc.str_two) r_dst <= keep_hi(r_dst, r_dst + str_step, a_addr32);
            if (dc.str_cnt) begin
                r_cnt <= r_cnt - 32'd1;
                {r_c2, r_c1, r_c0} <= {r_cnt == 32'd3, r_cnt == 32'd2, r_cnt == 32'd1};
            end
        end
    end
    logic s_rep;  // the string in dc repeats
    assign s_rep = dc.str_rep != 2'd0;
    always_ff @(posedge clk) begin
        if (reset || redirect) begin
            s_end1 <= 1'b0;
            s_kill1 <= 1'b0;
        end else if (issue) begin
            s_kill1 <= dc.str_load && !str_ss && s_rep && c_count == 32'd0;
            s_end1 <= dc.str_load && !str_ss && s_rep
                   && (c_count == 32'd0 || c_count == 32'd1 && dc.str_op != 3'(fes_i386_pkg::STR_CMPS));
        end
    end

    // A string's first uop waits until ESI, EDI and ECX are in registers,
    // no older uop that writes DF is in AG to EX (EX's running flags then
    // hold the DF the string runs with), and neither its segment nor ES is
    // being written. Its elements then wait on nothing D checks, which
    // keeps a CMPS element's compare a cycle behind its first load.
    // ENTER's waits until ESP and EBP are. The registers are fixed, so each
    // wait reads dops' bit for its register, with no select.
    logic df_busy, str_wait;
    assign str_wait = dc.str_load && (str_ss ? d_pend[4] || d_pend[5]
                                             : dc.uop.src1.valid && d_pend[6] || dc.uop.src2.valid && d_pend[7]
                                               || dc.uop.count_cl && d_pend[1] || df_busy || d_seg_busy[mseg]
                                               || d_seg_busy[fes_i386_pkg::SEG_ES]);
    // A uop that reads ESI, EDI or ECX (dc.rd_str, decoded) waits while a
    // string element that steps it is in AG to WB: WB makes the step, which
    // no forwarding sees.
    logic [2:0] str_busy;  // ESI, EDI, ECX
    function automatic logic [2:0] str_steps(input pipe_t s);
        str_steps = s.valid ? {s.uop.str_cx, s.uop.str_di, s.uop.str_si} : 3'd0;
    endfunction
    function automatic logic writes_df(input uop_t v);
        writes_df = v.cls == fes_i386_pkg::UOP_ALU && v.op == 5'd30
                 && (v.imm[3:0] == 4'd5 || v.imm[3:0] == 4'd6 || v.imm[3:0] == 4'd9);  // CLD, STD, LOAD
    endfunction
    logic str_hazard;
    assign str_hazard = |(dc.rd_str & str_busy);

    // An I/O instruction's access is at WB, and IN's data reaches only WB, so
    // nothing younger issues until it has retired (io_busy, as t_busy): no
    // load reads memory an OUT may remap, and nothing reads IN's data before
    // it is in the register file. A REP INS or OUTS's own elements stream;
    // the next instruction's first uop, never an element, waits.
    logic io_busy, io_hazard;
    function automatic logic does_io(input pipe_t s);
        does_io = s.valid && s.uop.io != 2'd0;
    endfunction
    assign io_hazard = io_busy && !(dc.str_a || dc.str_b);

    // What AG's uop is, for the holds below, registered as it enters AG: a
    // store, a shift or rotate, a stack operation that writes ESP.
    logic ag_store, ag_shift, ag_wsp;

    // ESP as data waits while a stack operation that writes it is in AG: its
    // value reaches the M1 register next cycle. LEAVE on a 16-bit stack waits
    // for ESP's high half, which it keeps.
    logic sp_hazard;
    assign sp_hazard = ag_wsp && (u.src1.valid && u.src1.index[2:0] == 3'd4
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
    assign store_hazard = ag_store && mem_reads;

    // A flag reader waits one cycle behind a shift or rotate, whose flags
    // reach EX's running copy a cycle after the shift leaves EX (fes_i386_ex).
    logic flag_hazard;
    assign flag_hazard = ag_shift && consumes;

    // A flush needs no term here: it clears AG and the forwarding tables, a
    // taken branch's redirect resets DC and D's EIP on the same edge, and a
    // fault or HLT stops the CPU. Once it has (halted), D issues nothing; in
    // the cycle the fault or HLT is in WB, its flush clears what D issues.
    // After a replay, D issues the instruction one uop at a time (serial),
    // each once nothing is left in AG to WB (ser_wait), so its uncached load
    // reaches M2 as the oldest uop. A CMPS element's second load issues right
    // behind its first, whose result it takes one stage ahead (x_prev); it
    // then counts as the oldest behind it (oldest).
    logic serial, ser_wait;
    assign issue = cur_valid && !(addr_use && lea_stall) && !(reads_seg && seg_hazard) && !(mem && mseg_hazard)
                && !store_hazard && !flag_hazard && !sp_hazard && !stack_wait && !t_wait && !uc_wait && !str_wait
                && !str_hazard && !io_hazard && !(ser_wait && !dc.x_prev) && !freeze && !halted;
    assign trace_issue = issue;
    assign trace_md_wait = md_busy;

    always_ff @(posedge clk) begin
        if (reset || wb_flush) begin
            d_seg_busy <= 6'd0;
            t_busy <= 1'b0;
            str_busy <= 3'd0;
            df_busy <= 1'b0;
            io_busy <= 1'b0;
            ser_wait <= 1'b0;
        end else if (!freeze) begin
            io_busy <= issue && u.io != 2'd0 && !s_kill1 || does_io(ag_s) || does_io(m1_s) || does_io(m2_s)
                    || does_io(ex_s);
            ser_wait <= serial && (issue || ag_s.valid || m1_s.valid || m2_s.valid || ex_s.valid);
            str_busy <= (issue && !s_kill1 ? {u.str_cx, u.str_di, u.str_si} : 3'd0) | str_steps(ag_s) | str_steps(m1_s) | str_steps(m2_s) | str_steps(ex_s);
            df_busy <= issue && writes_df(u) || ag_s.valid && writes_df(ag_s.uop) || m1_s.valid && writes_df(m1_s.uop)
                    || m2_s.valid && writes_df(m2_s.uop);
            // T0 and T1 are written at WB, so a uop that reads them in D waits
            // for every older one that writes either to retire.
            t_busy <= issue && writes_temp_u(u) || writes_temp(ag_s) || writes_temp(m1_s) || writes_temp(m2_s)
                   || writes_temp(ex_s);
            for (int k = 0; k < 6; k++)
                d_seg_busy[k] <= issue && dc.wseg[k]
                                || writes_seg(ag_s, k) || writes_seg(m1_s, k) || writes_seg(m2_s, k) || writes_seg(ex_s, k);
        end
        if (reset) begin
            d_stop <= 1'b0;
        end else if (redirect) begin
            d_stop <= 1'b0;
            d_eip <= redirect_eip;
        end else if (rp_go) begin
            d_stop <= 1'b1;  // until the restart's redirect
        end else if (issue) begin
            d_stop <= stop;
            if (!dc_more) d_eip <= d_eip_next;
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
    // A system register as src2's value (decoded_t b_sys).
    logic [31:0] sys_val;
    always_comb begin
        case (fes_i386_pkg::sys_rd_e'(dc.sys_idx))
            fes_i386_pkg::SYS_RD_CR0: sys_val = cr0_rd;
            fes_i386_pkg::SYS_RD_CR2: sys_val = cr2;
            fes_i386_pkg::SYS_RD_CR3: sys_val = cr3;
            fes_i386_pkg::SYS_RD_GDTL: sys_val = {16'd0, gdtr_limit};
            fes_i386_pkg::SYS_RD_GDTB: sys_val = gdtr_base;
            fes_i386_pkg::SYS_RD_IDTL: sys_val = {16'd0, idtr_limit};
            default: sys_val = idtr_base;
        endcase
    end
    always_comb begin
        d_out = '0;
        d_out.valid = issue;
        d_out.uop = u;
        d_out.eip = d_eip;
        d_out.eip_next = d_eip_next;
        d_out.a = dc.cs_base ? seg_base[SEG_CS] : dc.a_t1 ? t1 : a_val;
        d_out.b = reads_seg ? {16'b0, seg_sel[seg_index]} : dc.push_eip ? d_eip_next : dc.b_t1 ? t1
                : dc.b_fp ? r_fp : dc.b_sys ? sys_val : dc.b_far ? {16'b0, dc.far_sel}
                : dc.uop.desc == 2'd3 ? {24'b0, t3[15:9], 1'b1} : u.src2.valid ? b_val : u.imm;
        d_out.c = dc.far_jmp ? {16'b0, far_sel} : c_val;
        // A CMPS element's compare takes its first load's result one stage
        // ahead (x_prev).
        d_out.pend_a = dc.x_prev || u.src1.valid && a_pend;
        d_out.pend_b = u.src2.valid && b_pend;
        d_out.pend_c = u.count_cl && c_pend;
        d_out.dist_a = dc.x_prev ? 3'd1 : a_dist;
        d_out.dist_b = b_dist;
        d_out.dist_c = c_dist;
        d_out.early = lea && lea_early;
        // A system access (mem.system) is checked against its table's limit,
        // at its first read: a real-mode vector's offset against IDTR's
        // limit less 3, a descriptor's low dword against GDTR's less 7. Its
        // other parts, and POP Sreg's step of ESP (nochk), are not checked.
        // Any other access is checked against its segment (seg_chk_t).
        // LLDT is not built, so LDTR keeps its reset value, base 0 and
        // limit FFFFh, which no descriptor of a TI=1 selector passes.
        d_out.mlimit = dc.uop.desc == 2'd1 && !t0[2] ? {16'b0, gdt_lim7}
                     : dc.uop.mem.system && dc.uop.desc == 2'd0 && !dc.sel_part
                       ? {16'b0, idt_lim3} : dc.uop.mem.system || dc.nochk ? 32'hffff_ffff : seg_lim[mseg][msize];
        d_out.mtiny = dc.uop.desc == 2'd1 && !t0[2] ? gdt_tiny7 : dc.uop.mem.system && dc.uop.desc == 2'd0 && !dc.sel_part
                      ? idt_tiny3 : !dc.uop.mem.system && !dc.nochk
                        && (seg_tiny[mseg][msize] || mem_reads && seg_rbad[mseg] || mem_writes && seg_wbad[mseg]);
        d_out.mlow = seg_low[mseg];
        d_out.mexpd = !dc.uop.mem.system && !dc.nochk && seg_expd[mseg];
        // Loads whose data nothing uses: probes, ENTER's last check, POP m's
        // pop into nothing, and the multiply and divide unit's LOAD and HI.
        d_out.nodata = dc.stk == fes_i386_pkg::STK_PROBE || dc.str_fin || dc.nochk
                    || dc.seq == 5'(fes_i386_pkg::SEQ_POPM) && dc.stk == fes_i386_pkg::STK_POP
                    || (u.op == 5'd4 || u.op == 5'd5) && (u.cls == fes_i386_pkg::UOP_MUL || u.cls == fes_i386_pkg::UOP_DIV);
        // A st2 uop's store address and its fault; the next instruction for
        // a REPE or REPNE element. AG keeps them.
        d_out.target = dc.str_two ? str_daddr : s_tgt;
        d_out.dfault = dc.str_two && str_dbad && !s_kill1 ? (str_ss ? fes_i386_pkg::FAULT_SS0 : fes_i386_pkg::FAULT_GP0)
                                                          : fes_i386_pkg::FAULT_NONE;
        // A string's first element with a count of 1 is the last, with no
        // REPE or REPNE exit; with a count of 0, a NOP that ends the
        // instruction.
        if (s_end1) begin
            d_out.uop.ctl.end_of_instruction = 1'b1;
            d_out.uop.rep_stop = 2'd0;
            d_out.uop.eoi_taken = 1'b0;
        end
        if (s_kill1) begin
            d_out.uop.cls = fes_i386_pkg::UOP_SPECIAL;  // SPECIAL_NOP
            d_out.uop.op = 5'd0;
            d_out.uop.ctl.fault = fes_i386_pkg::FAULT_NONE;
            d_out.uop.mem.kind = fes_i386_pkg::MEM_NONE;
            d_out.uop.dst.valid = 1'b0;
            d_out.uop.flags = '0;
            d_out.uop.str_si = 1'b0;
            d_out.uop.str_di = 1'b0;
            d_out.uop.str_cx = 1'b0;
            d_out.uop.st2 = 1'b0;
            d_out.uop.io = 2'd0;
        end
    end
    always_ff @(posedge clk) begin
        if (reset || wb_flush) begin
            ag_s.valid <= 1'b0;
            ag_store <= 1'b0;
            ag_shift <= 1'b0;
            ag_wsp <= 1'b0;
        end else if (!freeze) begin
            ag_s <= d_out;
            ag_store <= issue && mem_writes && !s_kill1;
            ag_shift <= issue && u.cls == fes_i386_pkg::UOP_SHIFT;
            ag_wsp <= writes_sp(d_out);
        end
        if (!freeze) begin
            ag_stack <= stk != fes_i386_pkg::STK_NONE;
            ag_ss32 <= sst;
            ag_leave <= stk == fes_i386_pkg::STK_LEAVE;
            // ENTER's last uop: ESP from the engine's offset, its high half
            // the copy's.
            ag_sp_disp <= dc.str_fin ? str_off : st_sp_disp;
            ag_sp_hi <= dc.str_fin ? r_dst[31:16] : sp_hi;
        end
        if (freeze) begin
            // AG keeps its uop's operands.
        end else if (addr_use || dc.far_jmp) begin
            // LEA's offset, or a memory operand's address: its segment's
            // base, wrapped like the offset by address size. A far jump's
            // target: the new CS's base, selector * 16, plus the offset,
            // which is already its operand size's; with no base or index
            // looked up, the rows are zero.
            ag_addr32 <= dc.far_jmp || u.mem.addr32 || dc.uop.mem.system;
            ag_row <= row;
            ag_last <= last;
            ag_late <= late;
            ag_late_hi <= late_hi;
            // A string engine uop has no address registers: its offset is
            // the displacement. A bit test's displacement is T1 (disp_t1,
            // far_off with far_t).
            ag_disp <= dc.far_jmp || dc.disp_t1 ? far_off : dc.str_a || dc.str_b || dc.str_fin ? str_off
                     : stk != fes_i386_pkg::STK_NONE ? st_disp : lea_disp;
            // A system access: its table's base (GDTR's for a descriptor, or
            // the LDT's at its reset base 0 for a TI=1 selector; IDTR's for a
            // real-mode vector), with no wrap. A protected-mode far jump: the
            // descriptor's base, from T2 and T3.
            ag_seg_term <= dc.far_jmp ? (dc.uop.seg2_pm ? {t3[31:24], t3[7:0], t2[31:16]} : {12'd0, far_sel, 4'd0})
                         : lea ? 32'd0 : dc.uop.mem.system ? (dc.uop.desc != 2'd0 ? (t0[2] ? 32'd0 : gdtr_base) : idtr_base)
                         : u.mem.addr32 ? seg_base[mseg] : {16'b0, seg_base[mseg][15:0]};
            ag_seg_hi <= lea || dc.uop.mem.system ? 16'd0 : seg_base[mseg][31:16];
            ag_seg_hi_plus1 <= lea || dc.uop.mem.system ? 16'd0 : seg_hi1[mseg];
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
        // D's target for a st2 uop (its store) or a REPE or REPNE element
        // (the next instruction, also its EIP).
        ag_out.target = ag_s.uop.st2 || ag_s.uop.rep_stop != 2'd0 ? ag_s.target : linear;
        ag_out.target_eip = ag_s.uop.rep_stop != 2'd0 ? ag_s.eip_next : offset;
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
        writes_mem = s.uop.mem.kind == fes_i386_pkg::MEM_STORE || s.uop.mem.kind == fes_i386_pkg::MEM_RMW || s.uop.st2;
    endfunction
    // Where a store writes: its memory operand, or for a st2 uop (which
    // loads there) its second address.
    function automatic logic [31:0] saddr(input pipe_t s);
        saddr = s.uop.st2 ? s.target : s.maddr;
    endfunction
    pipe_t m1_out;
    logic  m1_limit;
    // INT's probe reads its lowest slot. Under SS's limit of FFFFh a slot
    // faults only by straddling the 64 KiB wrap, which one of INT's three
    // words does when they wrap (the lowest past FFFAh) at an odd offset:
    // then the probe faults, before INT writes anything. M1 records it
    // (pbad) and M2 raises #SS(0) from that register, off M1's fault path;
    // a probe writes nothing and never branches, so a stage later is as good.
    // Under a limit past 10000h (seg_ss_big) no slot faults. SS is current:
    // the probe waits in D for an older write of it.
    logic m1_probe_bad;
    assign m1_probe_bad = m1_s.uop.probe == fes_i386_pkg::PROBE_INT && !m1_s.uop.mem.addr32
                       && m1_s.moff[15:0] > 16'hfffa && m1_s.moff[0] && seg_ss_rm;
    logic [31:0] m1_bound_low;
    logic        m1_bound_wrap;
    // BOUND's first load is the upper slot, addressed one operand past the
    // lower slot. Check the whole pair before touching either slot: the lower
    // offset may have wrapped while forming this upper address, and reading
    // the upper slot first could otherwise cause an MMIO side effect before
    // the lower slot raises #GP/#SS.
    assign m1_bound_low = m1_s.uop.mem.addr32 ? m1_s.moff - 32'(m1_s.uop.mem.bytes)
                                               : {16'd0, m1_s.moff[15:0] - 16'(m1_s.uop.mem.bytes)};
    assign m1_bound_wrap = m1_bound_low > m1_s.mlimit || m1_s.mtiny || m1_s.mexpd && m1_bound_low <= m1_s.mlow;
    assign m1_limit = m1_s.uop.mem.kind != fes_i386_pkg::MEM_NONE
                   && (m1_s.moff > m1_s.mlimit || m1_s.mtiny || m1_s.mexpd && m1_s.moff <= m1_s.mlow
                       || m1_s.uop.bound_pair && m1_bound_wrap);
    // A protected-mode segment load's checks (segchk) come from the staged
    // descriptor (ds_fault_*).
    fes_i386_pkg::fault_e m1_segchk;
    assign m1_segchk = m1_s.uop.segchk == 2'd1 ? ds_fault_data : m1_s.uop.segchk == 2'd2 ? ds_fault_ss
                     : m1_s.uop.segchk == 2'd3 ? ds_fault_cs : fes_i386_pkg::FAULT_NONE;
    always_comb begin
        m1_out = capture(m1_s, 3'd3, wb_s.result);
        m1_out.pbad = m1_probe_bad;
        // A st2 uop's second address faults after its first. A descriptor
        // past GDTR's limit raises #GP(selector), a vector's entry past
        // IDTR's #GP(0).
        if (m1_limit && m1_s.uop.ctl.fault == fes_i386_pkg::FAULT_NONE)
            m1_out.uop.ctl.fault = m1_s.uop.desc != 2'd0 ? fes_i386_pkg::FAULT_GP_SEL
                                 : !m1_s.uop.mem.system && m1_s.uop.mem.seg == fes_i386_pkg::SEG_SS ? fes_i386_pkg::FAULT_SS0
                                 : fes_i386_pkg::FAULT_GP0;
        else if (m1_s.uop.ctl.fault == fes_i386_pkg::FAULT_NONE && m1_s.uop.segchk != 2'd0)
            m1_out.uop.ctl.fault = m1_segchk;
        else if (m1_s.uop.ctl.fault == fes_i386_pkg::FAULT_NONE)
            m1_out.uop.ctl.fault = m1_s.dfault;
    end
    assign dmem_addr = m1_s.maddr;
    assign dmem_read = m1_s.valid && reads_mem(m1_s) && !freeze && !halted && !wb_stop;
    // The load touches nothing in the L1D when it faults here, or when it
    // uses nothing it reads (nodata), so it never reads a device twice or
    // for nothing, and a probe never replays a delivery.
    logic dmem_fault, l1d_replay;
    assign dmem_fault = m1_out.uop.ctl.fault != fes_i386_pkg::FAULT_NONE || m1_probe_bad || m1_s.nodata;

    // Stores older than the load that have not written when the port reads:
    // the store now in EX, which will be in WB when the load is in M2, and
    // the store now in WB, which writes on the edge the port reads and so
    // is missed (the retired store, rt_*, in M2). D keeps a load from
    // following a store directly (store_hazard), unless that store is a
    // st2 uop's (a MOVS element's), whose data is its own load: then the
    // store in M2 is older too, and in EX, beside the load in M2, it holds
    // its data (pipe_t ld). M1 takes each one's distance from the load's
    // address; M2 decodes it. Both are physical, as the L1D and DDR see
    // them, so a store and a load through two aliases of a byte (A20M, the
    // reset alias) meet. A store's byte is taken only if it writes RAM (ram_bytes,
    // worked out as the store is in EX, or in M2 for a st2 uop): one to
    // read-only RAM or a hole is dropped, and MMIO is the device's, so the
    // load reads the memory or the device. An access that crosses a 1 MiB
    // boundary under A20M, or into the reset alias, translates every byte
    // independently so the bytes after the wrap still match.
    function automatic logic [3:0] ram_bytes(input logic [31:0] a, input logic a20m_in,
                                             input logic [23:0][1:0] upper, input logic [6:0] top);
        fes_i386_pkg::attr_e at0, at1;
        at0 = fes_i386_pkg::attr(fes_i386_pkg::phys(a, a20m_in), upper, top);
        at1 = fes_i386_pkg::attr(fes_i386_pkg::phys({a[31:5] + 27'd1, 5'd0}, a20m_in), upper, top);
        for (int k = 0; k < 4; k++)
            ram_bytes[k] = (6'(a[4:0]) + 6'(k) > 6'd31 ? at1 : at0) == fes_i386_pkg::ATTR_RAM;
    endfunction
    logic        m2_ve, m2_vw, m2_vr;
    logic [3:0]  m2_re, m2_rw, m2_rr;  // the store's bytes that write RAM
    logic [3:0]  ex_ram, wb_ram;       // ... for the store in EX, and in WB
    logic [3:0][3:0] m2_hit_e, m2_hit_w, m2_hit_r;
    logic [3:0][3:0] hit_e, hit_w, hit_r;
    always_comb begin
        for (int i = 0; i < 4; i++) begin
            for (int k = 0; k < 4; k++) begin
                hit_e[i][k] = fes_i386_pkg::phys(m1_s.maddr + 32'(i), a20m)
                            == fes_i386_pkg::phys(m2_s.target + 32'(k), a20m);
                hit_w[i][k] = fes_i386_pkg::phys(m1_s.maddr + 32'(i), a20m)
                            == fes_i386_pkg::phys(saddr(ex_s) + 32'(k), a20m);
                hit_r[i][k] = fes_i386_pkg::phys(m1_s.maddr + 32'(i), a20m)
                            == fes_i386_pkg::phys(saddr(wb_s) + 32'(k), a20m);
            end
        end
    end
    assign ex_ram = ram_bytes(saddr(ex_s), a20m, attr_upper, ram_top);
    always_ff @(posedge clk) if (!freeze) begin
        m2_hit_e <= hit_e;
        m2_hit_w <= hit_w;
        m2_hit_r <= hit_r;
        m2_ve <= m2_s.valid && m2_s.uop.st2;
        m2_vw <= ex_s.valid && writes_mem(ex_s);
        m2_vr <= wb_s.valid && writes_mem(wb_s) && wb_s.uop.ctl.fault == fes_i386_pkg::FAULT_NONE;
        m2_re <= ram_bytes(m2_s.target, a20m, attr_upper, ram_top);
        m2_rw <= ex_ram;
        m2_rr <= wb_ram;
        wb_ram <= ex_ram;
    end

    // ---- M2 -----------------------------------------------------------------------
    // The loaded bytes from the port, aligned to the access, with each byte an
    // older store writes taken from that store: a st2 store in EX first,
    // then the store in WB, then the retired one. EX takes the value through its bypass selects. M2 also
    // decodes EX's controls and prepares its operands (fes_i386_exctl).
    logic [31:0] rt_data;     // the store that retired on the last edge
    logic [3:0]  rt_bytes;
    logic        rt_valid;
    logic [3:0][7:0] ld_bytes;

    // The hit matrices compare each byte's translated physical address, so
    // A20M and the reset shadow are handled even when an access wraps.
    // long_*: the store writes byte k, to RAM.
    logic [3:0] long_e, long_w, long_r;  // the store writes byte k
    assign long_e = {{2{ex_s.uop.mem.bytes[2]}}, |ex_s.uop.mem.bytes[2:1], 1'b1} & m2_re;
    assign long_w = {{2{wb_s.uop.mem.bytes[2]}}, |wb_s.uop.mem.bytes[2:1], 1'b1} & m2_rw;
    assign long_r = {{2{rt_bytes[2]}}, |rt_bytes[2:1], 1'b1} & m2_rr;
    for (genvar i = 0; i < 4; i++) begin : g_ld
        logic [3:0] he, hw, hr;
        logic [7:0] port, from_e, from_w, from_r;
        for (genvar k = 0; k < 4; k++) begin : g_k
            assign he[k] = m2_ve && long_e[k] && m2_hit_e[i][k];
            assign hw[k] = m2_vw && long_w[k] && m2_hit_w[i][k];
            assign hr[k] = rt_valid && long_r[k] && m2_hit_r[i][k];
        end
        assign port = dmem_rdata[3'(m2_s.maddr[1:0]) + 3'(i)];
        assign from_e = ({8{he[0]}} & ex_s.ld[7:0]) | ({8{he[1]}} & ex_s.ld[15:8])
                      | ({8{he[2]}} & ex_s.ld[23:16]) | ({8{he[3]}} & ex_s.ld[31:24]);
        assign from_w = ({8{hw[0]}} & wb_s.result[7:0]) | ({8{hw[1]}} & wb_s.result[15:8])
                      | ({8{hw[2]}} & wb_s.result[23:16]) | ({8{hw[3]}} & wb_s.result[31:24]);
        assign from_r = ({8{hr[0]}} & rt_data[7:0]) | ({8{hr[1]}} & rt_data[15:8])
                      | ({8{hr[2]}} & rt_data[23:16]) | ({8{hr[3]}} & rt_data[31:24]);
        assign ld_bytes[i] = i >= m2_s.uop.mem.bytes ? 8'd0 : |he ? from_e : |hw ? from_w : |hr ? from_r : port;
    end

    pipe_t                m2_cap, m2_out;
    fes_i386_pkg::exctl_t m2_exc;
    logic [31:0]          m2_x, m2_y;
    // A replayed load changes nothing in EX (fes_i386_exctl's ok), as a
    // fault does.
    always_comb begin
        m2_cap = capture(m2_s, 3'd2, wb_s.result);
        if (l1d_replay && m2_cap.uop.ctl.fault == fes_i386_pkg::FAULT_NONE)
            m2_cap.uop.ctl.fault = fes_i386_pkg::FAULT_REPLAY;
    end
    fes_i386_exctl exctl (.s(m2_cap), .exc(m2_exc), .x(m2_x), .y(m2_y));
    always_comb begin
        m2_out = m2_cap;
        m2_out.exc = m2_exc;
        m2_out.x = m2_x;
        m2_out.y = m2_y;
        m2_out.ld = ld_bytes;
        // A HI uop's result is the half the multiply and divide unit kept
        // (fes_i386_muldiv), done before the HI leaves M2.
        if (m2_cap.uop.op == 5'd4 && (m2_cap.uop.cls == fes_i386_pkg::UOP_MUL || m2_cap.uop.cls == fes_i386_pkg::UOP_DIV))
            m2_out.result = md_hi;
        if (m2_cap.pbad && m2_cap.uop.ctl.fault == fes_i386_pkg::FAULT_NONE) m2_out.uop.ctl.fault = fes_i386_pkg::FAULT_SS0;
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
        end else if (md_last) begin
            // The multiply's or divide's result, and #DE for a divide whose
            // quotient overflows or whose divisor is zero, with the flags it
            // has changed.
            ex_s.result <= md_result;
            if (ex_s.exc.md_de && md_de) ex_s.uop.ctl.fault <= fes_i386_pkg::FAULT_DE;
        end
    end

    // ---- EX -----------------------------------------------------------------------
    // EFLAGS after every uop that has passed EX: no younger uop changes it,
    // and a flush only kills uops younger than WB, which have not. A shift
    // in WB still owes its flags (wb_flags): ex_flags lacks them for that
    // cycle, which no flag reader sees (flag_hazard), and takes them at its
    // end. A normal faulting uop changes no flags in EX; a divide error is
    // raised after the multiply/divide unit's flags reach this running copy.
    logic [17:0] ex_flags_out, wb_flags, flags_keep;
    logic [31:0] ex_result;
    logic [31:0] md_result, md_hi;
    logic [11:0] md_flags;
    logic        md_last, md_de, md_load;
    assign md_load = ex_s.valid && ex_s.exc.md && ex_s.uop.op == 5'd5;  // MULDIV_LOAD
    fes_i386_muldiv muldiv (
        .clk, .go(md_busy), .u(ex_s.uop), .ar(ex_s.a), .byp_a(ex_s.exc.byp_a), .yr(ex_s.y), .wb(wb_s.result), .ld(ex_s.ld),
        .yw({ex_s.exc.yw32, ex_s.exc.yw16, ex_s.exc.yw_lo, ex_s.exc.yw_hi, ex_s.exc.yl}), .ld_en(md_load), .last(md_last), .result(md_result),
        .de(md_de), .flags(md_flags), .hi(md_hi)
    );
    logic        ex_taken, ex_br, ex_sh_out0, ex_sh_outn, ex_sh_cfw, ex_sh_wide, ex_def_co, ex_def_szap;
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

    // A multiply or divide works in EX over several cycles (fes_i386_muldiv),
    // from EX's operands, which stay put while D to WB freeze. md_busy is set
    // as the uop enters EX and holds until the unit's last cycle, which
    // writes the unit's result into the uop's record (EX takes it through
    // LEA's select) and #DE into its fault; the uop leaves on the cycle after,
    // with the unit's flags. A flush waiting in WB cuts the wait short: the
    // uop is on the wrong path. A 16- or 32-bit DIV's LOAD uop hands the unit
    // EDX while it is in EX.
    always_ff @(posedge clk) begin
        if (reset || wb_flush) md_busy <= 1'b0;
        else if (md_busy) md_busy <= !md_last && !wb_flushes && !int_take;
        else if (!freeze)
            md_busy <= m2_out.valid && m2_out.exc.md_wait && m2_out.uop.ctl.fault == fes_i386_pkg::FAULT_NONE;
    end

    // An operand whose producer is one stage ahead (exc.byp_a, xw_*, yw_*,
    // byp_c) comes from the WB result register beside EX's operand, and a
    // memory operand from the loaded value (xl, yl). An early
    // LEA's offset is its whole result, which EX's LEA select passes.
    fes_i386_ex ex (
        .u(ex_s.uop), .exc(ex_s.exc), .a(ex_s.a), .xr(ex_s.x), .yr(ex_s.y), .wb(wb_s.result), .ld(ex_s.ld),
        .ag_result(ex_s.result), .flags_in(ex_flags), .flags_keep, .md_flags, .result(ex_result),
        .flags_out(ex_flags_out),
.taken(ex_taken), .br(ex_br), .ind_target(ex_ind_target), .ind_eip(ex_ind_eip), .sh_out0(ex_sh_out0), .sh_outn(ex_sh_outn), .sh_cfw(ex_sh_cfw), .sh_wide(ex_sh_wide),
        .def_co(ex_def_co), .def_szap(ex_def_szap)
    );
    always_comb begin
        ex_out = ex_s;
        ex_out.result = ex_result;
        ex_out.eflags = ex_flags_out;
        // An I/O uop's port (b) and OUT's data or IN's merge (a) are used at
        // WB, from its record: one its producer a stage ahead has just
        // computed is taken from the WB register here, as EX's operands are.
        if (ex_s.uop.io != 2'd0) begin
            if (ex_s.pend_a && ex_s.dist_a == 3'd1) ex_out.a = wb_s.result;
            if (ex_s.pend_b && ex_s.dist_b == 3'd1) ex_out.b = wb_s.result;
        end
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
        // A faulting branch terminates the pipeline without redirecting to its
        // rejected target. This also covers faults already found in M1.
        ex_out.taken = ex_taken && ex_out.uop.ctl.fault == fes_i386_pkg::FAULT_NONE;
        // BOUND's #BR, after the taken select, which a compare never sets.
        if (ex_br && ex_s.uop.ctl.fault == fes_i386_pkg::FAULT_NONE) ex_out.uop.ctl.fault = fes_i386_pkg::FAULT_BR;
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
        end else if (int_take || smc_go) begin
            // An interrupt, or a restart for self-modifying code, discards
            // the uops EX has run since the boundary.
            ex_flags <= eflags;
        end else if (ex_s.valid && !wb_flush) begin
            ex_flags <= ex_flags_out;
        end else begin
            ex_flags <= flags_keep;
        end
        if (reset || wb_flush) begin
            wb_s.valid <= 1'b0;
            wb_smc <= 1'b0;
            io_wait <= 1'b0;
        end else if (!freeze) begin
            wb_s <= ex_out;
            // The next instruction's first uop after a store into the bytes
            // the front end holds restarts, whatever it was to do.
            if (smc_mark) begin
                wb_s.uop.ctl.fault <= fes_i386_pkg::FAULT_REPLAY;
                wb_s.taken <= 1'b0;
            end
            wb_smc <= smc_mark && ex_s.valid;
            io_wait <= ex_s.valid && ex_out.uop.io != 2'd0 && ex_out.uop.ctl.fault == fes_i386_pkg::FAULT_NONE && !int_next
                    && !smc_mark;
        end else if (io_wait && io_ack) begin
            io_wait <= 1'b0;
        end
    end

    // ---- WB -----------------------------------------------------------------------
    logic [3:0] wb_fault;
    // The segment register WB writes: the destination, or a second one with
    // the selector in T0 (a far pointer load) or c.
    logic        seg_w;
    logic [2:0]  seg_w_idx;
    logic [15:0] seg_w_sel;
    assign seg_w = wb_s.uop.dst.valid && wb_s.uop.dst.space == fes_i386_pkg::RS_SEG || wb_s.uop.seg2;
    assign seg_w_idx = wb_s.uop.seg2 ? wb_s.uop.seg2_idx : wb_s.uop.dst.index[2:0];
    assign seg_w_sel = !wb_s.uop.seg2 ? wb_s.result[15:0] : wb_s.uop.seg2_t ? t0 : wb_s.c[15:0];
    assign wb_fault = wb_s.uop.ctl.fault;
    // The uop ends its instruction: its last, or INTO's JNO when taken.
    logic wb_eoi;
    assign wb_eoi = wb_s.uop.ctl.end_of_instruction || wb_s.taken && wb_s.uop.eoi_taken;

    // A store writes its bytes on the edge it retires: the result's low
    // bytes, moved to the address's place in its dword and the next.
    // The I/O access, while the uop that makes it is in WB and until the
    // machine completes it; the uop retires on the edge after, with IN's
    // data from io_data. The port is DX or imm8, OUT's data eAX (a) or, for
    // OUTS, the loaded value (its result). It goes to the machine once the
    // write buffer is empty, so every older store has reached memory or its
    // device first (a DMA buffer written before the OUT that starts the DMA,
    // a VGA store before a VGA register write).
    logic [31:0] io_data;
    logic        wb_empty;
    assign io_req = io_wait && wb_empty;
    assign io_write = wb_s.uop.io == 2'(fes_i386_pkg::IO_OUT);
    assign io_port = wb_s.uop.src2.valid ? wb_s.b[15:0] : wb_s.uop.imm[15:0];
    assign io_size = 2'(wb_s.uop.size);
    assign io_wdata = wb_s.uop.mem.kind == fes_i386_pkg::MEM_LOAD ? wb_s.result : wb_s.a;
    always_ff @(posedge clk) if (io_wait && io_ack) io_data <= io_rdata;
    // IN's data: merged into eAX's old value by size, or stored (INS).
    logic [31:0] wb_in, wb_sdata;
    assign wb_in = wb_s.uop.size == fes_i386_pkg::SIZE_8 ? {wb_s.a[31:8], io_data[7:0]}
                 : wb_s.uop.size == fes_i386_pkg::SIZE_16 ? {wb_s.a[31:16], io_data[15:0]} : io_data;
    assign wb_sdata = wb_s.uop.io == 2'(fes_i386_pkg::IO_IN) ? io_data : wb_s.result;

    logic wb_store;
    assign wb_store = wb_s.valid && !reset && !freeze && !halted && !int_take && wb_fault == fes_i386_pkg::FAULT_NONE && writes_mem(wb_s);
    logic [31:0] wb_saddr;
    assign wb_saddr = saddr(wb_s);
    assign dmem_write = wb_store;
    assign dmem_waddr = wb_saddr;
    always_comb begin
        dmem_wdata = 64'(wb_sdata) << {wb_saddr[1:0], 3'b000};
        dmem_wmask = (wb_s.uop.mem.bytes[2] ? 8'h0f : wb_s.uop.mem.bytes[1] ? 8'h03 : 8'h01) << wb_saddr[1:0];
    end
    assign trace_store = dmem_write;
    assign trace_store_addr = dmem_waddr;
    assign trace_store_data = dmem_wdata;
    assign trace_store_mask = dmem_wmask;

    // The store's physical line and the next one's, with their attributes,
    // registered as it enters WB; whether its lanes run into the next line.
    logic [PA:0]          wb_p0, wb_p1;
    fes_i386_pkg::attr_e  wb_at0, wb_at1;
    logic                 wb_cross;
    logic [31:0]          ex_saddr;
    assign ex_saddr = saddr(ex_s);
    always_ff @(posedge clk) if (!freeze) begin
        wb_p0 <= fes_i386_pkg::phys(ex_saddr, a20m);
        wb_p1 <= fes_i386_pkg::phys({ex_saddr[31:5] + 27'd1, 5'd0}, a20m);
    end
    assign wb_at0 = fes_i386_pkg::attr(wb_p0, attr_upper, ram_top);
    assign wb_at1 = fes_i386_pkg::attr(wb_p1, attr_upper, ram_top);
    assign wb_cross = wb_saddr[4:2] == 3'd7 && dmem_wmask[7:4] != 4'd0;

    // The store in WB waits while the write buffer will not have room for it
    // (two entries) once the store committing on this edge has entered it
    // (fes_i386_biu): st_wait, one register, as the store enters WB or while
    // it waits.
    logic wb_room_next, st_next;
    assign st_next = !(reset || wb_flush) && (freeze ? wb_s.valid && writes_mem(wb_s) : ex_s.valid && writes_mem(ex_s));
    always_ff @(posedge clk) st_wait <= st_next && !wb_room_next;
    always_ff @(posedge clk) if (reset || !freeze) begin
        rt_valid <= wb_store;
        rt_data <= wb_sdata;
        rt_bytes <= wb_s.uop.mem.bytes;
    end

    // A uop that enters WB faulting is delivered unless a delivery is under
    // way then: exc_active, unless the delivery's last uop retires now. A
    // flush clears what enters WB, so a delivery starting now needs no term.
    // Once halted, nothing enters WB.
    logic ex_deliver, exc_busy;
    assign ex_deliver = ex_out.uop.ctl.fault != fes_i386_pkg::FAULT_NONE
                     && ex_out.uop.ctl.fault != fes_i386_pkg::FAULT_UNIMPLEMENTED
                     && ex_out.uop.ctl.fault != fes_i386_pkg::FAULT_REPLAY;
    assign exc_busy = exc_active && !(wb_s.valid && wb_fault == fes_i386_pkg::FAULT_NONE && wb_eoi);
    always_ff @(posedge clk) begin
        if (reset || wb_flush) begin
            wb_flushes <= 1'b0;
            wb_redir <= 1'b0;
            wb_deliver <= 1'b0;
        end else if (!freeze) begin
            wb_flushes <= ex_s.valid && (smc_mark || ex_out.taken || ex_out.uop.ctl.fault != fes_i386_pkg::FAULT_NONE
                                         || ex_s.uop.cls == fes_i386_pkg::UOP_SPECIAL && ex_s.uop.op == 5'd1);
            // In protected mode a fault is not delivered: it stops the CPU.
            wb_redir <= ex_s.valid && !smc_mark && (ex_out.taken || ex_deliver && !exc_busy && !cr0_pe);
            wb_deliver <= ex_s.valid && !smc_mark && ex_deliver && !exc_busy && !cr0_pe;
        end
    end

    // A string register's step at WB, wrapped by address size: ESI and EDI
    // by the access's bytes, down or up; ECX down by one.
    logic [31:0] wb_str_by;
    assign wb_str_by = wb_s.uop.str_down ? -{28'd0, wb_s.uop.mem.bytes} : {28'd0, wb_s.uop.mem.bytes};
    function automatic logic [31:0] str_wb_step(input logic [31:0] r, input logic [31:0] by, input logic w32);
        str_wb_step = w32 ? r + by : {r[31:16], r[15:0] + by[15:0]};
    endfunction

    // ---- Interrupts ---------------------------------------------------------------
    // INTR and NMI are taken at an instruction boundary: on the edge an
    // instruction's last uop retires, a delivery's included, or a REP
    // string element's last uop (str_cx), or while the CPU is in HLT. NMI
    // comes first, unless one is being handled (blocked
    // until IRET's last uop retires); INTR if IF is set after the
    // instruction. Neither is taken after MOV SS or POP SS, unless that one
    // follows another, nor after an STI that set IF: the shadow, as Bochs
    // keeps it. int_take holds from that edge until the delivery starts. An
    // INTR first asks the machine for its vector (inta_req), D to WB frozen
    // until it answers. Then WB flushes, as for a fault, with nothing of the
    // uop in it retired, and DC takes the vector routine's entry with D's EIP
    // the next instruction's (int_go). Between a REP string's elements that
    // is the string's own, since WB moves EIP only as an instruction ends:
    // the elements done have stepped ESI, EDI and ECX, so IRET returns to
    // the string, which runs on from there. An NMI's vector is 2.
    logic       nmi_q, nmi_pend, nmi_edge, nmi_block, movss_sh;
    logic [7:0] int_vec;
    logic       wb_commit, at_boundary, in_hlt, wb_ss_load, wb_sti, sh_ss, int_shadow, nmi_ok, int_if;
    assign wb_commit = wb_s.valid && !halted && !freeze && !int_take && wb_fault == fes_i386_pkg::FAULT_NONE;
    assign at_boundary = wb_commit && (wb_eoi || wb_s.uop.str_cx);
    assign in_hlt = hlt && !int_take;
    assign wb_ss_load = wb_s.uop.dst.valid && wb_s.uop.dst.space == fes_i386_pkg::RS_SEG
                     && wb_s.uop.dst.index[2:0] == 3'(SEG_SS) || wb_s.uop.shadow;
    assign wb_sti = wb_s.uop.cls == fes_i386_pkg::UOP_ALU && wb_s.uop.op == 5'd30 && wb_s.uop.imm[3:0] == 4'd4;
    assign sh_ss = wb_ss_load && !movss_sh;
    assign int_shadow = sh_ss || wb_sti && !eflags[9];
    assign nmi_edge = nmi && !nmi_q;
    assign nmi_ok = (nmi_pend || nmi_edge) && (!nmi_block || at_boundary && wb_s.uop.iret);
    assign int_if = at_boundary ? wb_flags[9] : eflags[9];
    // In protected mode an interrupt is not taken: it stops the CPU (pm_int),
    // with the instruction before it retired and no INTA. At the boundary of
    // a CR0 write that is the mode the write leaves (pe_after).
    logic int_any, pm_int, pe_after, wb_cr0w;
    assign wb_cr0w = wb_s.uop.cls == fes_i386_pkg::UOP_SYSTEM
                  && (wb_s.uop.op == 5'(fes_i386_pkg::SYSOP_CR0) || wb_s.uop.op == 5'(fes_i386_pkg::SYSOP_LMSW));
    assign pe_after = at_boundary && wb_cr0w ? t1[0] || cr0_pe && wb_s.uop.op == 5'(fes_i386_pkg::SYSOP_LMSW) : cr0_pe;
    assign int_any = (at_boundary && !int_shadow || in_hlt) && (nmi_ok || intr && int_if);
    assign int_next = int_any && !pe_after;
    assign pm_int = int_any && pe_after;
    assign inta_req = int_wait;
    always_ff @(posedge clk) begin
        nmi_q <= nmi;
        if (reset) begin
            nmi_pend <= 1'b0;
            nmi_block <= 1'b0;
            movss_sh <= 1'b0;
            int_take <= 1'b0;
            int_nmi <= 1'b0;
            int_wait <= 1'b0;
        end else begin
            if (at_boundary) movss_sh <= sh_ss;
            if (at_boundary && wb_s.uop.iret) nmi_block <= 1'b0;
            // An edge stays pending until an NMI is taken; one that comes
            // while another is pending, or as it is taken, is that one.
            if (int_next && nmi_ok) nmi_pend <= 1'b0;
            else if (nmi_edge) nmi_pend <= 1'b1;
            if (int_next) begin
                int_take <= 1'b1;
                int_nmi <= nmi_ok;
                int_wait <= !nmi_ok;
                if (nmi_ok) nmi_block <= 1'b1;
            end else if (int_go) begin
                int_take <= 1'b0;
            end
            if (int_wait && inta_ack) begin
                int_wait <= 1'b0;
                int_vec <= inta_vector;
            end
        end
    end

    // trace_vector: the delivered fault's or interrupt's vector, or 8 once a
    // fault in a delivery has stopped the CPU, or in protected mode the
    // vector that stopped it (trace_pm_stop), with the error code of a fault
    // that names a selector (trace_error).
    always_ff @(posedge clk) exc_inject <= !reset && (exc_take || int_go);
    assign trace_vector = exc_vec;
    function automatic logic [7:0] fault_vector(input fes_i386_pkg::fault_e f);
        case (f)
            fes_i386_pkg::FAULT_UD: fault_vector = 8'd6;
            fes_i386_pkg::FAULT_GP0, fes_i386_pkg::FAULT_GP_SEL: fault_vector = 8'd13;
            fes_i386_pkg::FAULT_NM: fault_vector = 8'd7;
            fes_i386_pkg::FAULT_DE: fault_vector = 8'd0;
            fes_i386_pkg::FAULT_BR: fault_vector = 8'd5;
            fes_i386_pkg::FAULT_NP_SEL: fault_vector = 8'd11;
            default: fault_vector = 8'd12;  // FAULT_SS0, FAULT_SS_SEL
        endcase
    endfunction
    // SS can take INT's probe: a writable, expand-up 16-bit stack under the
    // real-mode limit FFFFh (the probe's wrap rule), or a limit past 10000h,
    // where no slot of a 16-bit stack can fault.
    function automatic logic ss_rm(input logic [31:0] limit, input logic [15:0] ar);
        ss_rm = limit == 32'h0000_ffff && ar[7] && ar[4] && !ar[3] && !ar[2] && ar[1] && !ar[14];
    endfunction
    function automatic logic ss_big(input logic [31:0] limit, input logic [15:0] ar);
        ss_big = limit[31:16] != 16'd0 && ar[7] && ar[4] && !ar[3] && !ar[2] && ar[1] && !ar[14];
    endfunction
    // A real-mode segment load's new access rights: P and S set, the rest
    // kept, as Bochs's load_seg_reg; for CS the 386's real-mode values,
    // limit FFFFh and access rights 93h (docs/i386-cpu-design.md,
    // "Compatibility contract").
    logic [15:0] rm_ar;
    logic [31:0] seg_w_limit;
    logic [15:0] seg_w_ar;
    assign rm_ar = seg_ar[seg_w_idx] | 16'h0090;
    assign seg_w_limit = wb_s.uop.seg2_pm ? ds_limit : seg_w_idx == 3'(SEG_CS) ? 32'h0000_ffff : seg_limit[seg_w_idx];
    assign seg_w_ar = wb_s.uop.seg2_pm ? ds_ar : seg_w_idx == 3'(SEG_CS) ? 16'h0093 : rm_ar;
    always_ff @(posedge clk) begin
        trace_retire <= 1'b0;
        trace_exception <= 1'b0;
        if (reset) begin
            gpr <= init_gpr;
            eflags <= {init_eflags[17:2], 1'b1, init_eflags[0]};
            eip <= init_eip;
            cr0_pe <= init_cr0[0];
            cr0_mp <= init_cr0[1];
            cr0_em <= init_cr0[2];
            cr0_ts <= init_cr0[3];
            cr0_et <= init_cr0[4];
            cr0_pg <= init_cr0[31];
            cr2 <= init_cr2;
            cr3 <= init_cr3;
            gdtr_base <= init_gdtr_base;
            gdtr_limit <= init_gdtr_limit;
            gdt_lim7 <= init_gdtr_limit - 16'd7;
            gdt_tiny7 <= init_gdtr_limit < 16'd7;
            idtr_base <= init_idtr_base;
            idtr_limit <= init_idtr_limit;
            idt_lim3 <= init_idtr_limit - 16'd3;
            idt_tiny3 <= init_idtr_limit < 16'd3;
            seg_sel <= init_sel;
            seg_base <= init_base;
            seg_limit <= init_limit;
            seg_ar <= init_ar;
            for (int k = 0; k < 6; k++) seg_hi1[k] <= init_base[k][31:16] + 16'd1;
            seg_cs_ffff <= init_limit[SEG_CS] == 32'h0000_ffff;
            seg_cs_flat <= init_limit[SEG_CS] == 32'hffff_ffff;
            seg_cs_small <= init_limit[SEG_CS] < 32'h0000_ffff;
            seg_ss_rm <= ss_rm(init_limit[SEG_SS], init_ar[SEG_SS]);
            seg_ss_big <= ss_big(init_limit[SEG_SS], init_ar[SEG_SS]);
            for (int k = 0; k < 6; k++)
                {seg_lim[k], seg_tiny[k], seg_low[k], seg_expd[k], seg_rbad[k], seg_wbad[k]}
                    <= fes_i386_pkg::seg_chk(init_limit[k], init_ar[k]);
            hlt <= 1'b0;
            halted <= 1'b0;
            trace_unimplemented <= 1'b0;
            trace_pm_stop <= 1'b0;
            trace_error <= 16'd0;
            exc_active <= 1'b0;
            exc_vec <= 8'd0;
        end else if (int_go) begin
            exc_active <= 1'b1;
            exc_vec <= int_nmi ? 8'd2 : int_vec;
            hlt <= 1'b0;
            halted <= 1'b0;
        end else if (wb_s.valid && !halted && !freeze) begin
            if (exc_take) begin
                exc_active <= 1'b1;
                exc_vec <= fault_vector(fes_i386_pkg::fault_e'(wb_fault));
            end else if (wb_fault == fes_i386_pkg::FAULT_REPLAY) begin
                // A replay: nothing retires, and the front end restarts (rp_go).
            end else if (wb_fault != fes_i386_pkg::FAULT_NONE) begin
                // An encoding not built, a fault in a delivery, or a fault in
                // protected mode, whose delivery is not built.
                halted <= 1'b1;
                trace_unimplemented <= 1'b1;
                trace_pm_stop <= cr0_pe && !exc_active && wb_fault != fes_i386_pkg::FAULT_UNIMPLEMENTED;
                exc_vec <= wb_fault == fes_i386_pkg::FAULT_UNIMPLEMENTED ? 8'd0
                         : cr0_pe && !exc_active ? fault_vector(fes_i386_pkg::fault_e'(wb_fault)) : 8'd8;
                trace_error <= wb_fault == fes_i386_pkg::FAULT_GP_SEL || wb_fault == fes_i386_pkg::FAULT_NP_SEL
                            || wb_fault == fes_i386_pkg::FAULT_SS_SEL ? {t0[15:2], 2'b00} : 16'd0;
            end else begin
                trace_retire <= wb_eoi;
                trace_exception <= wb_eoi && exc_active;
                if (wb_eoi) exc_active <= 1'b0;
                eflags <= wb_flags;
                // EIP moves at an instruction's last uop, so a sequence that
                // faults part way leaves it at the instruction.
                if (wb_eoi) eip <= wb_s.taken ? wb_s.target_eip : wb_s.eip_next;
                // A stack operation's ESP, which a destination of ESP overrides;
                // RETF's step, at the stack's width.
                if (wb_s.uop.sp_write) gpr[4] <= wb_s.sp_new;
                if (wb_s.uop.sp_add)
                    gpr[4] <= ss32 ? gpr[4] + wb_s.sp_new : {gpr[4][31:16], gpr[4][15:0] + wb_s.sp_new[15:0]};
                if (wb_s.uop.dst.valid && wb_s.uop.dst.space == fes_i386_pkg::RS_GPR)
                    gpr[wb_s.uop.dst.index[2:0]] <= wb_s.uop.io == 2'(fes_i386_pkg::IO_IN) ? wb_in : wb_s.result;
                // A string element's steps of ESI, EDI and ECX (CX by
                // address size).
                if (wb_s.uop.str_si) gpr[6] <= str_wb_step(gpr[6], wb_str_by, wb_s.uop.mem.addr32);
                if (wb_s.uop.str_di) gpr[7] <= str_wb_step(gpr[7], wb_str_by, wb_s.uop.mem.addr32);
                if (wb_s.uop.str_cx) gpr[1] <= str_wb_step(gpr[1], 32'hffff_ffff, wb_s.uop.mem.addr32);
                // The system uops: CLTS, the CR0, CR2 and CR3 writes from T1,
                // LMSW's (which cannot clear PE), and GDTR's and IDTR's from T0
                // and T1.
                if (wb_s.uop.cls == fes_i386_pkg::UOP_SYSTEM) begin
                    case (wb_s.uop.op)
                        5'(fes_i386_pkg::SYSOP_CR0): begin
                            {cr0_pg, cr0_et, cr0_ts, cr0_em, cr0_mp, cr0_pe} <= {t1[31], t1[4:0]};
                            // Back to real mode, CS becomes a writable data
                            // segment (P, S, type 3), its DPL, limit, G and
                            // D/B kept, as Bochs's handleCpuModeChange makes it.
                            if (cr0_pe && !t1[0]) begin
                                seg_ar[SEG_CS] <= {seg_ar[SEG_CS][15:8], 1'b1, seg_ar[SEG_CS][6:5], 5'b10011};
                                seg_rbad[SEG_CS] <= 1'b0;
                                seg_wbad[SEG_CS] <= 1'b0;
                            end
                        end
                        5'(fes_i386_pkg::SYSOP_LMSW): begin
                            {cr0_ts, cr0_em, cr0_mp} <= t1[3:1];
                            cr0_pe <= cr0_pe || t1[0];
                        end
                        5'(fes_i386_pkg::SYSOP_CR2): cr2 <= t1;
                        5'(fes_i386_pkg::SYSOP_CR3): cr3 <= t1;
                        5'(fes_i386_pkg::SYSOP_GDTR): begin
                            gdtr_limit <= t0;
                            gdtr_base <= {wb_s.uop.size == fes_i386_pkg::SIZE_32 ? t1[31:24] : 8'd0, t1[23:0]};
                            gdt_lim7 <= t0 - 16'd7;
                            gdt_tiny7 <= t0 < 16'd7;
                        end
                        5'(fes_i386_pkg::SYSOP_IDTR): begin
                            idtr_limit <= t0;
                            idtr_base <= {wb_s.uop.size == fes_i386_pkg::SIZE_32 ? t1[31:24] : 8'd0, t1[23:0]};
                            idt_lim3 <= t0 - 16'd3;
                            idt_tiny3 <= t0 < 16'd3;
                        end
                        default: cr0_ts <= 1'b0;  // SYSOP_CLTS
                    endcase
                end
                if (wb_s.uop.dst.valid && wb_s.uop.dst.space == fes_i386_pkg::RS_TEMP) begin
                    if (!wb_s.uop.dst.index[3]) t0 <= wb_s.result[15:0];
                    else if (wb_s.uop.dst.index[1]) t3 <= wb_s.result;
                    else if (wb_s.uop.dst.index[0]) t2 <= wb_s.result;
                    else t1 <= wb_s.result;
                end
                if (seg_w) begin
                    // Protected mode: the segment from T0's descriptor (the
                    // staged ds_*), a CS selector's RPL 0 (CPL). Real mode: the
                    // base is selector * 16; the limit and type stay (unreal
                    // mode) with P and S set, but CS's are the 386's (rm_ar).
                    if (wb_s.uop.seg2_pm) begin
                        seg_sel[seg_w_idx] <= seg_w_idx == 3'(SEG_CS) ? {t0[15:2], 2'b00} : t0;
                        seg_base[seg_w_idx] <= ds_base;
                        seg_hi1[seg_w_idx] <= ds_hi1;
                        {seg_lim[seg_w_idx], seg_tiny[seg_w_idx], seg_low[seg_w_idx], seg_expd[seg_w_idx],
                         seg_rbad[seg_w_idx], seg_wbad[seg_w_idx]} <= ds_chk;
                    end else begin
                        seg_sel[seg_w_idx] <= seg_w_sel;
                        seg_base[seg_w_idx] <= {12'd0, seg_w_sel, 4'd0};
                        seg_hi1[seg_w_idx] <= {11'd0, 5'(seg_w_sel[15:12]) + (* no_carry *) 5'd1};
                        if (seg_w_idx == 3'(SEG_CS)) begin
                            {seg_lim[SEG_CS], seg_tiny[SEG_CS], seg_low[SEG_CS], seg_expd[SEG_CS], seg_rbad[SEG_CS],
                             seg_wbad[SEG_CS]} <= fes_i386_pkg::seg_chk(32'h0000_ffff, 16'h0093);
                        end else begin
                            seg_rbad[seg_w_idx] <= rm_ar[3] && !rm_ar[1];
                            seg_wbad[seg_w_idx] <= rm_ar[3] || !rm_ar[1];
                        end
                    end
                    seg_limit[seg_w_idx] <= seg_w_limit;
                    seg_ar[seg_w_idx] <= seg_w_ar;
                    if (seg_w_idx == 3'(SEG_CS)) begin
                        seg_cs_ffff <= wb_s.uop.seg2_pm ? ds_ffff : 1'b1;
                        seg_cs_flat <= wb_s.uop.seg2_pm && ds_flat;
                        seg_cs_small <= wb_s.uop.seg2_pm && ds_small;
                    end
                    if (seg_w_idx == 3'(SEG_SS)) begin
                        seg_ss_rm <= ss_rm(seg_w_limit, seg_w_ar);
                        seg_ss_big <= ss_big(seg_w_limit, seg_w_ar);
                    end
                end
                if (wb_stop) begin
                    hlt <= 1'b1;
                    halted <= 1'b1;
                end
            end
        end
        if (!reset && pm_int) begin
            halted <= 1'b1;
            trace_unimplemented <= 1'b1;
            trace_pm_stop <= 1'b1;
            exc_vec <= nmi_ok ? 8'd2 : 8'hff;
            trace_error <= 16'd0;
        end
    end
    assign trace_halt = halted;

    // ---- Protected mode: a segment load's descriptor ---------------------------------
    // A protected-mode segment load's selector is in T0, its descriptor's
    // dwords in T2 and T3, a far jump's offset in T1 (fes_i386_ucode.toml).
    // Each cycle registers what the load writes and the checks it raises
    // from them (ds_*), at CPL 0: the uops that use them issue only once
    // those temporaries' writers have retired (t_rd), and no younger uop
    // writes one before they retire, so these registers hold the load's
    // descriptor when its check reaches M1 and its write reaches WB. A null
    // selector (index 0, TI 0) loads a null cache, base and limit 0 and
    // access rights 10h (S set, not present), as Bochs's load_null_selector.
    // The checks follow Bochs's load_seg_reg (data segments and SS) and
    // jump_protected with check_cs and branch_far (CS):
    //   data: none for null; a system descriptor or execute-only code,
    //         #GP(selector); data or non-conforming code with DPL below RPL,
    //         #GP(selector); not present, #NP(selector).
    //   SS:   null, #GP(0); RPL not 0, a system descriptor, code or read-only
    //         data, DPL not 0, #GP(selector); not present, #SS(selector).
    //   CS:   null, #GP(0); a system descriptor with DPL below RPL,
    //         #GP(selector), else a TSS, task gate or call gate not built,
    //         anything else #GP(selector); data, #GP(selector);
    //         non-conforming with DPL or RPL not 0, or conforming with DPL
    //         not 0, #GP(selector); not present, #NP(selector); an offset
    //         past the limit, #GP(0).
    logic        dd_null, dd_p, dd_s, dd_code, dd_conf, dd_rw, dd_gate;
    logic [1:0]  dd_dpl, dd_rpl;
    logic [3:0]  dd_type;
    logic [19:0] dd_lim20;
    logic [31:0] dd_limit, dd_base;
    logic [15:0] dd_ar;
    assign dd_null = t0[15:2] == 14'd0;
    assign dd_rpl = t0[1:0];
    assign dd_p = t3[15];
    assign dd_dpl = t3[14:13];
    assign dd_s = t3[12];
    assign dd_type = t3[11:8];
    assign dd_code = dd_type[3];
    assign dd_conf = dd_type[2];
    assign dd_rw = dd_type[1];
    assign dd_gate = dd_type == 4'd1 || dd_type == 4'd9 || dd_type == 4'd5 || dd_type == 4'd4 || dd_type == 4'd12;
    assign dd_lim20 = {t3[19:16], t2[15:0]};
    assign dd_limit = t3[23] ? {dd_lim20, 12'hfff} : {12'd0, dd_lim20};
    assign dd_base = {t3[31:24], t3[7:0], t2[31:16]};
    // The 386 has no L bit (descriptor bit 21): Bochs's cache drops it.
    assign dd_ar = {t3[23:22], 1'b0, t3[20], 4'd0, t3[15:9], 1'b1};
    logic                   ds_ffff, ds_flat, ds_small;
    logic [31:0]            ds_base, ds_limit;
    logic [15:0]            ds_ar, ds_hi1;
    fes_i386_pkg::seg_chk_t ds_chk;
    fes_i386_pkg::fault_e   ds_fault_data, ds_fault_ss, ds_fault_cs;
    always_ff @(posedge clk) begin
        ds_base <= dd_null ? 32'd0 : dd_base;
        ds_limit <= dd_null ? 32'd0 : dd_limit;
        ds_ar <= dd_null ? 16'h0010 : dd_ar;
        ds_hi1 <= (dd_null ? 16'd0 : dd_base[31:16]) + 16'd1;
        ds_chk <= fes_i386_pkg::seg_chk(dd_null ? 32'd0 : dd_limit, dd_null ? 16'h0010 : dd_ar);
        ds_ffff <= !dd_null && dd_limit == 32'h0000_ffff;
        ds_flat <= !dd_null && dd_limit == 32'hffff_ffff;
        ds_small <= dd_null || dd_limit < 32'h0000_ffff;
        ds_fault_data <= dd_null ? fes_i386_pkg::FAULT_NONE
                       : !dd_s || dd_code && !dd_rw || (!dd_code || !dd_conf) && dd_dpl < dd_rpl ? fes_i386_pkg::FAULT_GP_SEL
                       : !dd_p ? fes_i386_pkg::FAULT_NP_SEL : fes_i386_pkg::FAULT_NONE;
        ds_fault_ss <= dd_null ? fes_i386_pkg::FAULT_GP0
                     : dd_rpl != 2'd0 || !dd_s || dd_code || !dd_rw || dd_dpl != 2'd0 ? fes_i386_pkg::FAULT_GP_SEL
                     : !dd_p ? fes_i386_pkg::FAULT_SS_SEL : fes_i386_pkg::FAULT_NONE;
        ds_fault_cs <= dd_null ? fes_i386_pkg::FAULT_GP0
                     : !dd_s ? (dd_dpl >= dd_rpl && dd_gate ? fes_i386_pkg::FAULT_UNIMPLEMENTED : fes_i386_pkg::FAULT_GP_SEL)
                     : !dd_code || (dd_conf ? dd_dpl != 2'd0 : dd_dpl != 2'd0 || dd_rpl != 2'd0) ? fes_i386_pkg::FAULT_GP_SEL
                     : !dd_p ? fes_i386_pkg::FAULT_NP_SEL : t1 > dd_limit ? fes_i386_pkg::FAULT_GP0 : fes_i386_pkg::FAULT_NONE;
    end

    // Serial issue after a replay for an uncached load, until its
    // instruction ends or anything else flushes.
    always_ff @(posedge clk) begin
        if (reset || wb_flush && !rp_go) serial <= 1'b0;
        else if (rp_go) serial <= !wb_smc;
        else if (wb_commit && wb_eoi) serial <= 1'b0;
    end

    // ---- Memory: L1I, L1D and the bus interface -------------------------------------
    logic              st_ex, st_wb, oldest, unc_start;
    logic [PA-6:0]     inv_line;
    logic              inv_valid, inv_all;
    logic              d_req, d_mmio, d_beat, i_req, i_beat;
    logic [PA-3:0]     d_addr;
    logic [3:0]        d_be;
    logic [PA-6:0]     i_line;
    logic [127:0]      bus_rdata;
    assign st_ex = ex_s.valid && writes_mem(ex_s);
    assign st_wb = wb_s.valid && writes_mem(wb_s);
    // An uncached load may read its device once nothing older can flush it
    // or still has a store to make: EX and WB hold nothing, or only earlier
    // uops of its own instruction that neither store, end an instruction or
    // element (no interrupt boundary), branch, fault, nor multiply or divide
    // (IRET's or POPA's loads after its first). Nor while the instruction
    // the load is part of may restart for self-modifying code (smc_due,
    // smc_defer), which would read the device again; a load that replays
    // meanwhile ends queue16's wait, as any flush does.
    function automatic logic own_load(input pipe_t s);
        own_load = !s.uop.ctl.end_of_instruction && !s.uop.str_cx && !writes_mem(s) && s.uop.io == 2'd0
                && s.uop.ctl.fault == fes_i386_pkg::FAULT_NONE && s.uop.cls != fes_i386_pkg::UOP_BRANCH
                && s.uop.cls != fes_i386_pkg::UOP_MUL && s.uop.cls != fes_i386_pkg::UOP_DIV;
    endfunction
    assign oldest = !int_take && !smc_due && !smc_defer && (!ex_s.valid || own_load(ex_s))
                 && (!wb_s.valid || own_load(wb_s) && !wb_flushes);
    assign trace_fetch = mem_load;
    assign trace_fetch_block = mem_block;
    assign trace_dread = dmem_read;
    assign trace_dread_addr = dmem_addr;
    assign trace_mem_wait = m2_load && !dmem_valid || st_wait || !mem_valid;
    assign trace_replay = rp_go && !wb_smc;
    assign trace_smc = smc_go;
    assign trace_smc_open = smc_open && !unc_start;
    assign trace_ex_store = st_ex;

    fes_i386_l1i l1i (
        .clk, .reset, .a20m, .attr_upper, .ram_top, .mem_block, .mem_load, .mem_data, .mem_valid,
        .st_advance(!freeze), .st_ex_addr(ex_saddr), .st_ex, .st_wb, .st_commit(wb_store),
        .st_line0(wb_p0[PA-1:5]), .st_line1(wb_p1[PA-1:5]), .st_cross(wb_cross),
        .inv_valid, .inv_line, .inv_all, .i_req, .i_line, .i_beat, .rdata(bus_rdata)
    );

    fes_i386_l1d l1d (
        .clk, .reset, .a20m, .attr_upper, .ram_top, .advance(!freeze), .flush(wb_flush), .ag_index(linear[11:5]),
        .rd(dmem_read), .addr(dmem_addr), .bytes(m1_s.uop.mem.bytes), .fault(dmem_fault),
        .oldest, .valid(dmem_valid), .rdata(dmem_rdata), .replay(l1d_replay), .unc_start,
        .st_ex_addr(ex_saddr), .st_wb_addr(wb_saddr), .st_commit(wb_store),
        .st_line0(wb_p0[PA-1:5]), .st_line1(wb_p1[PA-1:5]), .st_attr0(wb_at0), .st_cross(wb_cross),
        .st_dword(wb_saddr[4:2]), .st_data(dmem_wdata), .st_mask(dmem_wmask),
        .inv_valid, .inv_line, .inv_all, .d_req, .d_mmio, .d_addr, .d_be, .d_beat, .bdata(bus_rdata)
    );

    fes_i386_biu #(.RAM_BASE(RAM_BASE)) biu (
        .clk, .cold_reset, .reset, .attr_upper, .ram_top,
        .st_valid(wb_store), .st_line0(wb_p0[PA-1:5]), .st_line1(wb_p1[PA-1:5]), .st_attr0(wb_at0), .st_attr1(wb_at1),
        .st_dword(wb_saddr[4:2]), .st_data(dmem_wdata), .st_mask(dmem_wmask), .wb_room_next, .wb_empty,
        .drain(io_wait), .d_req, .d_mmio, .d_addr, .d_be, .d_beat, .i_req, .i_line, .i_beat, .rdata(bus_rdata),
        .inv_valid, .inv_line, .inv_all,
        .ddr_address, .ddr_burstcount, .ddr_read, .ddr_write, .ddr_writedata, .ddr_byteenable, .ddr_waitrequest,
        .ddr_readdata, .ddr_readdatavalid,
        .mmio_valid, .mmio_write, .mmio_address, .mmio_byteenable, .mmio_wdata, .mmio_ready, .mmio_rvalid,
        .mmio_rdata,
        .dma_valid, .dma_write, .dma_address, .dma_wdata, .dma_done, .dma_rdata, .flush_request, .flush_done,
        .idle(trace_mem_idle)
    );

    // ---- Self-modifying code -------------------------------------------------------
    // Fetch is sequential from the last redirect, and every redirect comes
    // from WB, so the instructions after the one in WB are the bytes from
    // where it starts to the end of the block F1 loads next (fetch_next):
    // one run of lines, holding every block F2 may still read from the L1I
    // before a clear of a line there lands. A store committed into one of
    // them, or a DMA write (inv_valid), may have changed bytes the front end
    // already holds. Then an instruction after it restarts: its first uop
    // takes FAULT_REPLAY as it enters WB (smc_mark), WB flushes it and
    // everything younger, and the front end restarts at it through start,
    // as for an uncached load but without serial issue (smc_go). By then the
    // L1I has cleared the line, and its fill waits for the store's entry in
    // the write buffer, so the instruction runs the new bytes. The
    // instruction that stored completes as it was decoded.
    //
    // Which instruction: under the coherent profile, and for a DMA write,
    // the next one not begun. That is the next once the instruction in WB
    // ends (smc_pend until it does, smc_due after), or, while WB is empty
    // between instructions (smc_open), the one whose first uop enters WB
    // next: smc_first says the last uop WB took ended an instruction, and
    // uc_live that the next one's uncached load has not read its device. A
    // load that starts to read as the write lands (unc_start) has begun its
    // instruction too: it starts with D to WB frozen, so nothing enters WB
    // then, and the write waits for that instruction's end. A delivery's
    // uops are no instruction's, and an instruction restarted for
    // self-modifying code is not restarted again before it enters WB
    // (smc_first low after smc_go): a DMA write that lands meanwhile
    // restarts the one after it. So a stream of DMA writes into the run
    // cannot hold the CPU, and the restarted instruction may run bytes from
    // before the later write. Under queue16 the 16 bytes after the storing
    // instruction (from eip_next) keep what the front end holds: a store
    // with no byte past them (wb_q16, worked out in EX), which may lie
    // behind them, its own encoding among them, where nothing younger is,
    // needs nothing. One with a byte past them restarts the first
    // instruction to reach past them (smc_defer, the window's end plus one
    // in smc_t), checked as each instruction's first uop enters WB, at the
    // boundary before it or later (smc_chk), as a 386 refetches once it runs
    // past its queue. The instruction right after the storing one starts at
    // the window and is at most 15 bytes, so the check needs only the
    // registered smc_defer and smc_t.
    //
    // Lines compare by bits 19:5, modulo 1 MiB, where linear and physical
    // agree under A20M and for the shadow at FFFF0000h; an alias 1 MiB away
    // restarts for nothing. The run starts at smc_lo: CS.base + EIP a cycle
    // old, the instruction in WB or one before it, except on the cycle after
    // a redirect, when EIP has just moved and the run starts at the
    // redirect's line, where F1 has just loaded. Every input but unc_start
    // is a register, unc_start reaches only smc_pend and smc_due, and the
    // decision reaches only registers (smc_pend, smc_due, smc_defer,
    // smc_chk, and WB's entry through smc_mark), so the freeze and WB's
    // flush keep their depth.
    logic        q16, wb_q16, smc_pend, smc_chk, smc_store, smc_dma, smc_bnd, smc_now, smc_late, smc_past;
    logic        smc_first, uc_live, smc_open;
    logic [14:0] smc_lo, smc_span;
    logic [19:0] ex_next_lin, wb_next_lin, ex_q16_off, smc_t, smc_reach;
    // line - lo <= span, as the borrow of a subtraction, so both are carry
    // chains.
    function automatic logic smc_in(input logic [14:0] line, input logic [14:0] lo, input logic [14:0] span);
        logic [15:0] over;
        over = {1'b0, span} - {1'b0, 15'(line - lo)};
        smc_in = !over[15];
    endfunction
    assign smc_span = fetch_next[15:1] - smc_lo;
    assign smc_store = wb_store && !(q16 && wb_q16)
                    && (smc_in(wb_p0[19:5], smc_lo, smc_span) || wb_cross && smc_in(wb_p1[19:5], smc_lo, smc_span));
    assign smc_dma = inv_valid && smc_in(inv_line[14:0], smc_lo, smc_span);
    assign smc_open = !wb_s.valid && smc_first && !uc_live;
    assign smc_bnd = wb_commit && wb_eoi || smc_open;
    assign smc_now = smc_pend || smc_dma || smc_store && !q16;
    assign smc_late = smc_defer || smc_store && q16;
    // The instruction in EX (its next instruction's linear address) reaches
    // past the window.
    assign ex_next_lin = seg_base[SEG_CS][19:0] + ex_s.eip_next[19:0];
    assign smc_reach = ex_next_lin - smc_t;
    assign smc_past = !smc_reach[19];
    assign smc_mark = smc_due || smc_bnd && smc_now || (smc_bnd || smc_chk) && smc_defer && ex_s.valid && smc_past;
    assign smc_go = rp_go && wb_smc;
    assign ex_q16_off = ex_saddr[19:0] - ex_next_lin;
    always_ff @(posedge clk) begin
        if (reset) q16 <= queue16;
        if (!freeze) begin
            wb_next_lin <= ex_next_lin;
            wb_q16 <= ex_q16_off[19]
                   || ex_q16_off[18:4] == 15'd0
                      && ex_q16_off[3:0] <= (ex_s.uop.mem.bytes[2] ? 4'd12 : ex_s.uop.mem.bytes[1] ? 4'd14 : 4'd15);
        end
        if (!smc_defer) smc_t <= wb_next_lin + 20'd17;
        smc_lo <= redirect ? redirect_linear[19:5] : cs_eip[19:5];
        // A mid-instruction branch's commit leaves smc_first low, as the uops
        // after it are its instruction's.
        if (reset) smc_first <= 1'b1;
        else if (exc_take || int_go || smc_go) smc_first <= 1'b0;
        else if (rp_go) smc_first <= 1'b1;
        else if (wb_commit) smc_first <= wb_eoi;
        if (reset || wb_flush || wb_commit && wb_eoi) uc_live <= 1'b0;
        else if (unc_start) uc_live <= 1'b1;
        if (reset || wb_flush) begin
            smc_pend <= 1'b0;
            smc_due <= 1'b0;
            smc_defer <= 1'b0;
            smc_chk <= 1'b0;
        end else begin
            smc_pend <= smc_now && (!smc_bnd || unc_start);
            smc_due <= smc_mark && !(!freeze && ex_s.valid) && !unc_start;
            smc_defer <= smc_late;
            smc_chk <= (smc_bnd || smc_chk) && smc_late && !(!freeze && ex_s.valid);
        end
    end

    assign arch_gpr = gpr;
    assign arch_eflags = {14'd0, eflags};
    assign arch_cr0 = cr0_rd;
    assign arch_cr2 = cr2;
    assign arch_cr3 = cr3;
    assign arch_gdtr_base = gdtr_base;
    assign arch_gdtr_limit = gdtr_limit;
    assign arch_idtr_base = idtr_base;
    assign arch_idtr_limit = idtr_limit;
    assign arch_eip = eip;
    assign arch_sel = seg_sel;
    assign arch_base = seg_base;
    assign arch_limit = seg_limit;
    assign arch_ar = seg_ar;
endmodule
