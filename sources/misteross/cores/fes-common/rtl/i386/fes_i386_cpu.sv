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
                                                   // delivery (trace_vector 8: double fault)
    output logic             trace_issue,          // D issued this cycle (lockstep observability)
    output logic             trace_md_wait,        // D to WB wait for a multiply or divide in EX (ditto)
    output logic             trace_fetch,          // F1 sent a block to the L1I (ditto)
    output logic             trace_dread,          // M1 sent a load to the L1D (ditto)
    output logic             trace_mem_wait,       // D to WB wait for the L1D or the write buffer, or F2 for the L1I
    output logic             trace_mem_idle,       // the bus interface has nothing to do or finish
    output logic             trace_replay,         // WB restarts an instruction for an uncached load
    // The store WB commits: the bytes trace_store_mask names of the eight
    // from trace_store_addr's dword, at linear addresses.
    output logic             trace_store,
    output logic [31:0]      trace_store_addr,
    output logic [7:0][7:0]  trace_store_data,
    output logic [7:0]       trace_store_mask,
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
    logic [15:0]      t0;  // microcode temporaries: a far transfer's selector and offset
    logic [31:0]      t1;
    // Each segment's limit less 0, 1 and 3, for 1-, 2- and 4-byte accesses,
    // and whether that is below zero, so the limit check is one compare.
    // Real-mode loads keep the limit, so only reset loads them.
    logic [5:0][2:0][31:0] seg_lim;
    logic [5:0][2:0]       seg_tiny;
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

    // ---- Start and redirect -------------------------------------------------------
    // After reset the front end starts at CS.base + EIP, one cycle after the
    // sum is registered.
    logic [1:0]  start;
    logic [31:0] start_linear;
    logic        redirect;
    logic [31:0] redirect_linear, redirect_eip;
    always_ff @(posedge clk) begin
        if (reset || rp_go) begin
            start <= 2'd1;
        end else if (start != 2'd0) begin
            start <= start + 2'd1;
        end
        start_linear <= seg_base[SEG_CS] + eip;
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
    logic     q_valid, d_ready, fe_mem_load;
    fetched_t q;
    fes_i386_frontend frontend (
        .clk, .reset, .redirect, .redirect_linear, .code32, .mem_block, .mem_load(fe_mem_load), .mem_data, .mem_valid,
        .q_valid, .q, .d_ready
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
        .q, .code32, .stack(dc_stack), .uop(q_uop), .opsize32(dc_q.opsize32), .lea(q_lea), .lea_early(dc_q.lea_early),
        .lea_disp(dc_q.lea_disp), .branch(dc_branch), .consumes(dc_q.consumes), .reads_seg(dc_q.reads_seg),
        .seg_index(dc_q.seg_index), .stop(dc_q.stop), .stk(q_stk), .push_eip(dc_q.push_eip),
        .cs_base(dc_q.cs_base), .seq(q_seq), .sel_part(dc_q.sel_part), .far_jmp(dc_q.far_jmp), .far_chk(dc_q.far_chk),
        .far_sel(dc_q.far_sel), .wseg(dc_q.wseg), .upc(dc_q.upc), .direct(q_direct), .xlat(q_xlat),
        .m_seg(dc_q.m_seg), .slot(dc_q.slot), .str_op(dc_q.str_op), .str_rep(dc_q.str_rep), .ecnt(dc_q.ecnt)
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
    logic        past, wrap16, cs_gp, seg_cs_ffff, seg_ss_ffff;
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
    // INT's probe assumes SS's real-mode limit, FFFFh, under which only a
    // slot that straddles the 64 KiB wrap faults.
    logic str_ss, str_expd;  // the string engine's second address is SS's (ENTER), else ES's; expand-down
    logic [2:0] str_dseg;
    assign str_ss = dc.seq == fes_i386_pkg::SEQ_ENTER;
    assign str_dseg = str_ss ? 3'(SEG_SS) : 3'(fes_i386_pkg::SEG_ES);
    assign str_expd = !seg_ar[str_dseg][3] && seg_ar[str_dseg][2];
    // A count-zero REP's first element is rewritten to a NOP below. Its
    // unused addresses must not raise an unsupported-segment or wrap fault.
    assign d_unimpl = !s_kill1 && (wrap16 || (mem && expand_down) || (dc.str_two && str_expd) || call_target_unimpl
                   || (dc.uop.probe != fes_i386_pkg::PROBE_NONE && !seg_ss_ffff));

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
    always_comb begin
        u = dc.uop;
        if (stk != fes_i386_pkg::STK_NONE) u.mem.addr32 = ss32;
        // POPAD's ESP image counts only on a 16-bit stack.
        if (dc.popad_esp && !(dc.opsize32 && !ss32)) u.dst.valid = 1'b0;
        if (!s_kill1 && d_unimpl && dc.uop.ctl.fault == fes_i386_pkg::FAULT_NONE)
            u.ctl.fault = fes_i386_pkg::FAULT_UNIMPLEMENTED;
        if (!s_kill1 && far_bad && dc.uop.ctl.fault == fes_i386_pkg::FAULT_NONE)
            u.ctl.fault = seg_cs_ffff ? fes_i386_pkg::FAULT_GP0 : fes_i386_pkg::FAULT_UNIMPLEMENTED;
        if (!s_kill1 && (cs_gp || call_target_gp)) u.ctl.fault = fes_i386_pkg::FAULT_GP0;
        u.str_down = s_down;
    end
    assign opsize32 = dc.opsize32;
    assign lea = dc.lea;
    assign lea_early = dc.lea_early;
    // A far pointer's selector is past its offset by the operand size,
    // wrapped by address size.
    // The interrupt vector's entry is at vector * 4 (far_sel), its selector
    // at + 2.
    assign lea_disp = dc.uop.mem.system ? {16'b0, dc.far_sel[15:2], dc.sel_part, 1'b0}
                    : !dc.sel_part ? dc.lea_disp
                    : dc.uop.mem.addr32 ? dc.lea_disp + st_size : {16'b0, dc.lea_disp[15:0] + st_size[15:0]};
    assign consumes = dc.consumes;
    assign reads_seg = dc.reads_seg;
    assign seg_index = dc.seg_index;
    assign stop = !s_kill1 && (dc.stop || d_unimpl || cs_gp || far_bad || call_target_gp);

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
    assign str_daddr = seg_base[str_dseg] + (a_addr32 ? r_dst : {16'b0, r_dst[15:0]});
    assign str_dlim = seg_lim[str_dseg][msize];
    assign str_dbad = (a_addr32 ? r_dst : {16'b0, r_dst[15:0]}) > str_dlim || seg_tiny[str_dseg][msize];
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
    // reaches M2 as the oldest uop.
    logic serial, ser_wait;
    assign issue = cur_valid && !(addr_use && lea_stall) && !(reads_seg && seg_hazard) && !(mem && mseg_hazard)
                && !store_hazard && !flag_hazard && !sp_hazard && !stack_wait && !t_wait && !uc_wait && !str_wait
                && !str_hazard && !io_hazard && !ser_wait && !freeze && !halted;
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
    always_comb begin
        d_out = '0;
        d_out.valid = issue;
        d_out.uop = u;
        d_out.eip = d_eip;
        d_out.eip_next = d_eip_next;
        d_out.a = dc.cs_base ? seg_base[SEG_CS] : dc.a_t1 ? t1 : a_val;
        d_out.b = reads_seg ? {16'b0, seg_sel[seg_index]} : dc.push_eip ? d_eip_next : dc.b_t1 ? t1
                : dc.b_fp ? r_fp : u.src2.valid ? b_val : u.imm;
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
        // The interrupt vector's entry (mem.system): IDTR's real-mode base 0
        // and limit 3FFh, which a vector's entry never passes.
        d_out.mlimit = dc.uop.mem.system ? 32'hffff_ffff : seg_lim[mseg][msize];
        d_out.mtiny = !dc.uop.mem.system && seg_tiny[mseg][msize];
        // Loads whose data nothing uses: probes, ENTER's last check, POP m's
        // pop into nothing, and the multiply and divide unit's LOAD and HI.
        d_out.nodata = dc.stk == fes_i386_pkg::STK_PROBE || dc.str_fin
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
            ag_ss32 <= ss32;
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
            ag_addr32 <= dc.far_jmp || u.mem.addr32;
            ag_row <= row;
            ag_last <= last;
            ag_late <= late;
            ag_late_hi <= late_hi;
            // A string engine uop has no address registers: its offset is
            // the displacement. A bit test's displacement is T1 (disp_t1,
            // far_off with far_t).
            ag_disp <= dc.far_jmp || dc.disp_t1 ? far_off : dc.str_a || dc.str_b || dc.str_fin ? str_off
                     : stk != fes_i386_pkg::STK_NONE ? st_disp : lea_disp;
            ag_seg_term <= dc.far_jmp ? {12'd0, far_sel, 4'd0} : lea || dc.uop.mem.system ? 32'd0
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
    logic m1_probe_bad;
    assign m1_probe_bad = m1_s.uop.probe == fes_i386_pkg::PROBE_INT && !m1_s.uop.mem.addr32
                       && m1_s.moff[15:0] > 16'hfffa && m1_s.moff[0];
    assign m1_limit = m1_s.uop.mem.kind != fes_i386_pkg::MEM_NONE && (m1_s.moff > m1_s.mlimit || m1_s.mtiny);
    always_comb begin
        m1_out = capture(m1_s, 3'd3, wb_s.result);
        m1_out.pbad = m1_probe_bad;
        // A st2 uop's second address faults after its first.
        if (m1_limit && m1_s.uop.ctl.fault == fes_i386_pkg::FAULT_NONE)
            m1_out.uop.ctl.fault = m1_s.uop.mem.seg == fes_i386_pkg::SEG_SS ? fes_i386_pkg::FAULT_SS0
                                                                           : fes_i386_pkg::FAULT_GP0;
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
    // address; M2 decodes it.
    logic [31:0] m2_de, m2_dw, m2_dr;  // the load's address less the store's
    logic        m2_ve, m2_vw, m2_vr;
    always_ff @(posedge clk) if (!freeze) begin
        m2_de <= m1_s.maddr - m2_s.target;
        m2_dw <= m1_s.maddr - saddr(ex_s);
        m2_dr <= m1_s.maddr - saddr(wb_s);
        m2_ve <= m2_s.valid && m2_s.uop.st2;
        m2_vw <= ex_s.valid && writes_mem(ex_s);
        m2_vr <= wb_s.valid && writes_mem(wb_s) && wb_s.uop.ctl.fault == fes_i386_pkg::FAULT_NONE;
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

    // Load byte i is store byte k when the load's address less the store's is
    // k - i: within -3..3, so its high bits are all zero or all one and its
    // low three bits are k - i.
    logic       near_e, near_w, near_r;
    logic [3:0] long_e, long_w, long_r;  // the store writes byte k
    assign near_e = m2_ve && (m2_de[31:2] == 30'd0 || m2_de[31:2] == '1);
    assign near_w = m2_vw && (m2_dw[31:2] == 30'd0 || m2_dw[31:2] == '1);
    assign near_r = rt_valid && (m2_dr[31:2] == 30'd0 || m2_dr[31:2] == '1);
    assign long_e = {{2{ex_s.uop.mem.bytes[2]}}, |ex_s.uop.mem.bytes[2:1], 1'b1};
    assign long_w = {{2{wb_s.uop.mem.bytes[2]}}, |wb_s.uop.mem.bytes[2:1], 1'b1};
    assign long_r = {{2{rt_bytes[2]}}, |rt_bytes[2:1], 1'b1};
    for (genvar i = 0; i < 4; i++) begin : g_ld
        logic [3:0] he, hw, hr;
        logic [7:0] port, from_e, from_w, from_r;
        for (genvar k = 0; k < 4; k++) begin : g_k
            assign he[k] = near_e && long_e[k] && m2_de[2:0] == 3'(k - i);
            assign hw[k] = near_w && long_w[k] && m2_dw[2:0] == 3'(k - i);
            assign hr[k] = near_r && long_r[k] && m2_dr[2:0] == 3'(k - i);
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
.taken(ex_taken), .ind_target(ex_ind_target), .ind_eip(ex_ind_eip), .sh_out0(ex_sh_out0), .sh_outn(ex_sh_outn), .sh_cfw(ex_sh_cfw), .sh_wide(ex_sh_wide),
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
        end else if (int_take) begin
            // An interrupt discards the uops EX has run since the boundary.
            ex_flags <= eflags;
        end else if (ex_s.valid && !wb_flush) begin
            ex_flags <= ex_flags_out;
        end else begin
            ex_flags <= flags_keep;
        end
        if (reset || wb_flush) begin
            wb_s.valid <= 1'b0;
            io_wait <= 1'b0;
        end else if (!freeze) begin
            wb_s <= ex_out;
            io_wait <= ex_s.valid && ex_out.uop.io != 2'd0 && ex_out.uop.ctl.fault == fes_i386_pkg::FAULT_NONE && !int_next;
        end else if (io_wait && io_ack) begin
            io_wait <= 1'b0;
        end
    end

    // ---- WB -----------------------------------------------------------------------
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
    assign wb_store = wb_s.valid && !reset && !freeze && !halted && !int_take && wb_fault == 3'd0 && writes_mem(wb_s);
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
    // (two entries) on this edge's next cycle: st_wait, one register, as the
    // store enters WB or while it waits.
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
    assign exc_busy = exc_active && !(wb_s.valid && wb_fault == 3'd0 && wb_eoi);
    always_ff @(posedge clk) begin
        if (reset || wb_flush) begin
            wb_flushes <= 1'b0;
            wb_redir <= 1'b0;
            wb_deliver <= 1'b0;
        end else if (!freeze) begin
            wb_flushes <= ex_s.valid && (ex_out.taken || ex_out.uop.ctl.fault != fes_i386_pkg::FAULT_NONE
                                         || ex_s.uop.cls == fes_i386_pkg::UOP_SPECIAL && ex_s.uop.op == 5'd1);
            wb_redir <= ex_s.valid && (ex_out.taken || ex_deliver && !exc_busy);
            wb_deliver <= ex_s.valid && ex_deliver && !exc_busy;
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
    assign wb_commit = wb_s.valid && !halted && !freeze && !int_take && wb_fault == 3'd0;
    assign at_boundary = wb_commit && (wb_eoi || wb_s.uop.str_cx);
    assign in_hlt = hlt && !int_take;
    assign wb_ss_load = wb_s.uop.dst.valid && wb_s.uop.dst.space == fes_i386_pkg::RS_SEG
                     && wb_s.uop.dst.index[2:0] == 3'(SEG_SS);
    assign wb_sti = wb_s.uop.cls == fes_i386_pkg::UOP_ALU && wb_s.uop.op == 5'd30 && wb_s.uop.imm[3:0] == 4'd4;
    assign sh_ss = wb_ss_load && !movss_sh;
    assign int_shadow = sh_ss || wb_sti && !eflags[9];
    assign nmi_edge = nmi && !nmi_q;
    assign nmi_ok = (nmi_pend || nmi_edge) && (!nmi_block || at_boundary && wb_s.uop.iret);
    assign int_if = at_boundary ? wb_flags[9] : eflags[9];
    assign int_next = (at_boundary && !int_shadow || in_hlt) && (nmi_ok || intr && int_if);
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
    // fault in a delivery has stopped the CPU.
    always_ff @(posedge clk) exc_inject <= !reset && (exc_take || int_go);
    assign trace_vector = exc_vec;
    always_ff @(posedge clk) begin
        trace_retire <= 1'b0;
        trace_exception <= 1'b0;
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
            seg_ss_ffff <= init_limit[SEG_SS] == 32'h0000_ffff;
            for (int k = 0; k < 6; k++) begin
                seg_lim[k][0] <= init_limit[k];
                seg_lim[k][1] <= init_limit[k] - 32'd1;
                seg_lim[k][2] <= init_limit[k] - 32'd3;
                seg_tiny[k] <= {init_limit[k] < 32'd3, init_limit[k] < 32'd1, 1'b0};
            end
            hlt <= 1'b0;
            halted <= 1'b0;
            trace_unimplemented <= 1'b0;
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
                exc_vec <= wb_fault == 3'd1 ? 8'd6 : wb_fault == 3'd2 ? 8'd13 : wb_fault == 3'd3 ? 8'd7
                         : wb_fault == 3'd6 ? 8'd0 : 8'd12;
            end else if (wb_fault == 3'(fes_i386_pkg::FAULT_REPLAY)) begin
                // A replay: nothing retires, and the front end restarts (rp_go).
            end else if (wb_fault != 3'd0) begin
                // An encoding not built, or a fault in a delivery.
                halted <= 1'b1;
                trace_unimplemented <= 1'b1;
                exc_vec <= wb_fault == 3'd4 ? 8'd0 : 8'd8;
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
                if (wb_stop) begin
                    hlt <= 1'b1;
                    halted <= 1'b1;
                end
            end
        end
    end
    assign trace_halt = halted;

`ifdef FES_I386_DEBUG
    always_ff @(posedge clk) begin
        if (wb_s.valid && !freeze) $display("WB eip=%h cls=%0d op=%0d fault=%0d result=%h ld=%h dst=%0d/%0d eoi=%b taken=%b tgt=%h/%h t1=%h flush=%b redir=%b",
            wb_s.eip, wb_s.uop.cls, wb_s.uop.op, wb_s.uop.ctl.fault, wb_s.result, wb_s.ld, wb_s.uop.dst.space, wb_s.uop.dst.index,
            wb_eoi, wb_s.taken, wb_s.target, wb_s.target_eip, t1, wb_flush, redirect);
        if (redirect) $display("REDIRECT linear=%h eip=%h start=%0d", redirect_linear, redirect_eip, start);
    end
`endif
    // Serial issue after a replay, until its instruction ends or anything
    // else flushes.
    always_ff @(posedge clk) begin
        if (reset || wb_flush && !rp_go) serial <= 1'b0;
        else if (rp_go) serial <= 1'b1;
        else if (wb_commit && wb_eoi) serial <= 1'b0;
    end

    // ---- Memory: L1I, L1D and the bus interface -------------------------------------
    logic              st_ex, st_wb, oldest;
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
    // (IRET's or POPA's loads after its first).
    function automatic logic own_load(input pipe_t s);
        own_load = !s.uop.ctl.end_of_instruction && !s.uop.str_cx && !writes_mem(s) && s.uop.io == 2'd0
                && s.uop.ctl.fault == fes_i386_pkg::FAULT_NONE && s.uop.cls != fes_i386_pkg::UOP_BRANCH
                && s.uop.cls != fes_i386_pkg::UOP_MUL && s.uop.cls != fes_i386_pkg::UOP_DIV;
    endfunction
    assign oldest = !int_take && (!ex_s.valid || own_load(ex_s)) && (!wb_s.valid || own_load(wb_s) && !wb_flushes);
    assign trace_fetch = mem_load;
    assign trace_dread = dmem_read;
    assign trace_mem_wait = m2_load && !dmem_valid || st_wait || !mem_valid;
    assign trace_replay = rp_go;

    fes_i386_l1i l1i (
        .clk, .reset, .a20m, .attr_upper, .ram_top, .mem_block, .mem_load, .mem_data, .mem_valid,
        .st_advance(!freeze), .st_ex_addr(ex_saddr), .st_ex, .st_wb, .st_commit(wb_store),
        .st_line0(wb_p0[PA-1:5]), .st_line1(wb_p1[PA-1:5]), .st_cross(wb_cross),
        .inv_valid, .inv_line, .inv_all, .i_req, .i_line, .i_beat, .rdata(bus_rdata)
    );

    fes_i386_l1d l1d (
        .clk, .reset, .a20m, .attr_upper, .ram_top, .advance(!freeze), .flush(wb_flush),
        .rd(dmem_read), .addr(dmem_addr), .bytes(m1_s.uop.mem.bytes), .fault(dmem_fault),
        .oldest, .valid(dmem_valid), .rdata(dmem_rdata), .replay(l1d_replay),
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

    assign arch_gpr = gpr;
    assign arch_eflags = {14'd0, eflags};
    assign arch_eip = eip;
    assign arch_sel = seg_sel;
    assign arch_base = seg_base;
    assign arch_limit = seg_limit;
    assign arch_ar = seg_ar;
endmodule
