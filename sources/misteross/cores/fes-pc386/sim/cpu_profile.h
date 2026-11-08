// SPDX-License-Identifier: MIT
// machine_tb --cpu-profile: where fes_i386_cpu's clocks go (bench only).
//
// sample() reads D's issue and wait terms, which cpu_profile.vlt makes
// readable (public_flat_rd) so the RTL is unchanged, and puts every system
// clock in one bucket, in priority order: an issued uop (an instruction's
// first or a later one), halted, frozen (L1D miss, MMIO, multiply or divide,
// I/O, INTA, else a full store buffer), front end empty (refill after a
// redirect, with or without an L1I miss on the way, an L1I miss, else
// empty), then the term that held D (D stop, the governor, AGI, segment
// write, store-to-load, flags after a shift, ESP, temp register, microcode
// ROM, string set-up, I/O serialisation, replay serialisation). A data load
// never holds D: the load is at M1-M2, ahead of the ALU at EX, so the only
// load-use interlock is a load's result into an address, which is AGI.
//
// retire() charges each retirement the clocks since the previous one to its
// instruction class ("own"); a taken instruction's redirect is the next
// retirement's gap, charged to the taken instruction's class ("next").
// begin() starts the accounting at the handover from the oracle, so a window
// may start there. dump() appends one snapshot of every counter; a window is
// the difference of two (scripts/pc386_cpu_profile.py).
//
// Issue, commit, in-flight and flush counters are reported independently.
// A window can start or end partway through a uop's lifetime, so their deltas
// cannot assign flushes to the uops issued in that window. A flush cycle is
// counted directly when wb_flush is asserted during sampling.
//
// This header needs no Verilator header: sample() and in_flight() take any
// CPU model whose rootp has the fes_i386_cpu__DOT__<name> fields
// cpu_profile.vlt lists.
//
// AGI split: each AGI clock is charged to the producer that holds D's
// address operand (fes_i386_cpu prof_agi: the stage, AG to EX, of the
// youngest late producer of the base or index). A shadow of AG to EX, which
// advances with the pipeline (D to WB only ever hold together), keeps each
// uop's producer class (prof_dcls, AgiClass) from when it was in D, and
// whether its register sources would be in a register by the time it is in
// AG, so that an early ALU there could have executed it (ready).
//
// Front-end split: each clock D finds DC empty (the refill, refill_l1i,
// fe_l1i and fe_empty buckets) is charged to why Q did not issue two clocks
// before: Q's issue loads its output register on the edge, and DC takes that
// on the next (fes_i386_cpu, DC), so D is empty at t only if Q, with room in
// its output register, issued nothing at t - 2, or a redirect at t - 1 or
// t - 2 emptied Q and DC. Q's reasons, read from
// fes_i386_qalign and fes_i386_fetchbuf (bench only, as above), in priority
// order: a redirect; a prefix consumed (each prefix but the last costs a
// cycle); avail cleared by a taken jump that started in the half before its
// last byte's (jump_clear), when the bytes at Q's start are otherwise there;
// else Q's bytes are not in the buffer (avail clear), because its half has
// not been filled (half), it is at bytes 12-15 whose decode waits for the
// next block's L1 pass (tail), or the instruction runs past its half and the
// next half has not arrived (cross). Each wait is split by how Q entered the
// half it waits in (FeCtx: sequentially, the first sequential half after a
// jump, a predicted taken jump, one whose target is the branch's own block,
// a redirect) and by whether F2 has waited on the L1I since Q last issued
// or the last redirect (l1i). A clock Q issued or held two clocks before is unattributed. Every
// clock Q did not issue is also counted by its reason as it happens (q),
// whatever D did then: the bubbles D's waits hide today.
//
// A stage register is a packed pipe_t with valid in
// its top bit, CPU_PROFILE_PIPE_MSB, which the bench build reads from the
// Verilated model into cpu_profile_layout.h.
#pragma once

#include <algorithm>
#include <cstdint>
#include <cstdio>
#include <string>
#include <unordered_map>
#include <utility>
#include <vector>

// A page flip is a byte write to CRTC start-address high (0Ch) at 3D5h,
// after a byte write to the CRTC index port 3D4h selected it. Multi-byte I/O
// is seen as byte lanes at consecutive ports, so a word OUT at 3D4h selects
// the register and writes its data.
struct CrtcFrameTracker {
    bool observe(bool write, uint16_t port, unsigned length, uint32_t value) {
        if (!write) return false;
        bool frame = false;
        const unsigned lanes = length < sizeof(value) ? length : unsigned(sizeof(value));
        for (unsigned lane = 0; lane < lanes; ++lane) {
            const unsigned lane_port = unsigned(port) + lane;
            const uint8_t lane_value = uint8_t(value >> (8 * lane));
            if (lane_port == 0x3d4) {
                crtc_index = uint8_t(lane_value & 0x3f);
                crtc_index_valid = true;
            } else if (lane_port == 0x3d5 && crtc_index_valid && crtc_index == 0x0c) {
                frame = true;
            }
        }
        return frame;
    }

private:
    uint8_t crtc_index = 0;
    bool crtc_index_valid = false;
};

struct CpuProfile {
    enum Bucket {
        ISSUE_FIRST, ISSUE_MORE, HALT, FRZ_L1D, FRZ_MMIO, FRZ_MD, FRZ_IO, FRZ_INTA, FRZ_STORE,
        REFILL, REFILL_L1I, FE_L1I, FE_EMPTY, D_STOP, GOV, AGI, SEG, STLD, FLAG, SP, TEMP, UROM, STR,
        IO_SERIAL, REPLAY_SERIAL, OTHER_WAIT, PARKED_IO, PARKED_HALT, RESET, NBUCKET
    };
    static constexpr const char *bucket_name[NBUCKET] = {
        "issue_first", "issue_more", "halt", "frz_l1d", "frz_mmio", "frz_md", "frz_io", "frz_inta", "frz_store",
        "refill", "refill_l1i", "fe_l1i", "fe_empty", "d_stop", "gov", "agi", "seg", "st_ld", "flag", "sp",
        "temp", "urom", "str", "io_serial", "replay_serial", "other_wait", "parked_io", "parked_halt", "reset"};
    enum Class {
        C_JCC, C_JMP, C_JMP_IND, C_CALL, C_CALL_IND, C_RET, C_FAR_CALL, C_FAR_JMP, C_RETF, C_INT, C_IRET,
        C_LOOP, C_JCXZ, C_REP_STR, C_STR, C_IO, C_MUL, C_DIV, C_SHIFT, C_PUSHPOP, C_SEGLOAD, C_LOAD_OP, C_OTHER,
        C_DELIVERY, NCLASS
    };
    static constexpr const char *class_name[NCLASS] = {
        "jcc", "jmp", "jmp_ind", "call", "call_ind", "ret", "far_call", "far_jmp", "retf", "int", "iret",
        "loop", "jcxz", "rep_str", "str", "io", "mul", "div", "shift", "pushpop", "segload", "mem_op", "other",
        "delivery"};
    // The producer classes of fes_i386_cpu prof_dcls: register and immediate
    // forms (no memory operand) of MOV, ADD/INC/DEC, AND/OR/XOR/NOT, LEA at a
    // 16-bit operand size, MOVZX/MOVSX; a load (MOV, MOVZX or MOVSX from
    // memory), an ALU op with a memory source, POP; shifts, multiply and
    // divide, the other ALU ops, branches (LOOP's ECX), everything else, a
    // 32-bit LEA (already early); SUB, ADC/SBB/NEG, and XOR or SUB of a
    // register with itself (zero).
    enum AgiClass {
        A_NONE, A_MOV, A_ADD, A_LOGIC, A_LEA16, A_EXT, A_LOAD, A_LOAD_OP, A_POP, A_SHIFT, A_MULDIV,
        A_ALU_OTHER, A_BRANCH, A_OTHER, A_LEA32, A_SUB, A_ADC, A_ZERO, NAGI
    };
    static constexpr const char *agi_name[NAGI] = {
        "none", "mov", "add", "logic", "lea16", "ext", "load", "load_op", "pop", "shift", "muldiv",
        "alu_other", "branch", "other", "lea32", "sub", "adc", "zero"};
    // Front-end split: Q's reasons (F_PREFIX to F_UNATTR, then a wait per
    // FeWait, FeCtx and L1I flag), and for the q counts, Q issued or held.
    enum FeWait { W_HALF, W_TAIL, W_CROSS, NWAIT };
    enum FeCtx { X_SEQ, X_SEQ1, X_TK, X_TKSELF, X_RD, NCTX };
    enum FeSpecial { F_PREFIX, F_JCLR, F_REDIRECT, F_UNATTR, F_WAIT };
    static constexpr unsigned NFE = F_WAIT + NWAIT * NCTX * 2;
    static constexpr unsigned Q_ISSUE = NFE, Q_HELD = NFE + 1, NQ = NFE + 2;
    static constexpr const char *fe_special[F_WAIT] = {"prefix", "jclr", "redirect", "unattr"};
    static constexpr const char *fe_wait_name[NWAIT] = {"half", "tail", "cross"};
    static constexpr const char *fe_ctx_name[NCTX] = {"seq", "seq1", "tk", "tkself", "rd"};
    static constexpr unsigned fe_wait(unsigned w, unsigned x, bool l1i) { return F_WAIT + (w * NCTX + x) * 2 + l1i; }
    static std::string fe_name(unsigned i) {
        if (i == Q_ISSUE) return "issue";
        if (i == Q_HELD) return "held";
        if (i < F_WAIT) return fe_special[i];
        const unsigned k = i - F_WAIT;
        return std::string(fe_wait_name[k / 2 / NCTX]) + "_" + fe_ctx_name[k / 2 % NCTX] + (k % 2 ? "_l1i" : "");
    }
    // The buckets the front-end split covers, in its F lines' order.
    static constexpr Bucket fe_bucket[4] = {REFILL, REFILL_L1I, FE_L1I, FE_EMPTY};
    // Per class: AGI clocks by the producer's stage (AG, M1, M2, EX, none),
    // those whose producer was ready for an early ALU, those whose producer
    // had no second register source (imm, or a one-operand op), and those
    // whose producer was ready and wrote 32 bits (w32).
    struct Agi { uint64_t stage[5] = {}, ready = 0, imm = 0, w32 = 0; };
    struct Shadow { uint8_t cls = A_NONE; bool ready = false, imm = false, w32 = false; };
    struct Counts { uint64_t n = 0, taken = 0, own = 0, next = 0; };
    struct Site { uint64_t n = 0, cycles = 0; uint8_t c = 0; };

    uint64_t bucket[NBUCKET] = {};
    uint64_t commits = 0, flush_cycles = 0, redirects = 0, l1d_waits = 0, l1i_waits = 0, retired = 0, pm_retired = 0;
    uint64_t frames = 0, polls_3da = 0, io_accesses = 0;
    uint64_t prefixes = 0;  // prefix bytes before an instruction's last one
    // Direct branches retired (Jcc, LOOP and its kin, JCXZ, JMP and CALL rel):
    // taken; predicted taken; predicted taken and taken; and fetch-check
    // replays (fes_i386_cpu, "Branch prediction"). Near RETs retired: those
    // the return stack predicted, and those that redirected (not predicted,
    // or predicted to another target).
    uint64_t br = 0, br_taken = 0, br_pt = 0, br_pt_taken = 0, fetch_replays = 0;
    uint64_t ret = 0, ret_pt = 0, ret_mis = 0;
    uint64_t sampled = 0;   // clocks classified
    Counts cls[NCLASS];
    Agi agi[NAGI];
    Shadow shadow[4];  // AG, M1, M2, EX
    uint64_t fe[4][NFE] = {};  // fe_bucket's clocks by Q's reason two clocks before
    uint64_t q[NQ] = {};       // every clock by Q's reason
    uint64_t q_taken = 0, q_tself = 0;  // Q's predicted taken jumps, and those within one block
    unsigned q_hist[2] = {F_UNATTR, F_UNATTR};  // Q's reason one and two clocks ago
    uint8_t q_ctx = X_RD;
    bool q_jclr = false, q_l1i = false;
    std::unordered_map<uint32_t, Site> sites;  // this dump interval's, by linear start
    CrtcFrameTracker crtc_frame;
    uint64_t last_retire = 0;
    int pending = -1;  // the class of a taken instruction whose target has not retired
    bool since_redirect = false, was_l1d = false, was_l1i = false, refill_l1i = false;

    // The handover: the next retirement's gap runs from now, not from the
    // last retirement before the oracle ran.
    void begin(uint64_t now) {
        last_retire = now;
        pending = -1;
        since_redirect = was_l1d = was_l1i = refill_l1i = false;
        q_hist[0] = q_hist[1] = F_UNATTR;
    }

    // One system clock, after the CPU model's eval at the clock's low phase.
    template <typename Cpu> void sample(Cpu &cpu, bool mmio_busy, bool resetting) {
        ++sampled;
        if (resetting) {
            ++bucket[RESET];
            q_hist[0] = q_hist[1] = F_REDIRECT;
            q_ctx = X_RD;
            q_jclr = q_l1i = false;
            return;
        }
        auto *r = cpu.rootp;
#define S(name) (r->fes_i386_cpu__DOT__##name)
#define QA(name) S(frontend__DOT__align__DOT__##name)
#define FB(name) S(frontend__DOT__fetch__DOT__##name)
        const bool redirect = S(redirect);
        // Q's reason this clock (see Front-end split).
        if (FB(active) && !S(mem_valid)) q_l1i = true;
        unsigned qr;
        const unsigned start = QA(start) & 31u, h = start >> 4, half_valid = FB(half_valid), tail_ok = FB(tail_ok);
        const bool here = (half_valid >> h & 1u) && ((start & 15u) < 12 || (tail_ok >> h & 1u));
        if (redirect) qr = F_REDIRECT;
        else if (QA(issue)) qr = Q_ISSUE;
        else if (!QA(out_free)) qr = Q_HELD;
        else if (QA(consume)) qr = F_PREFIX;
        else if (q_jclr && here) qr = F_JCLR;
        else {
            const unsigned w = !(half_valid >> h & 1u) ? W_HALF : !here ? W_TAIL : W_CROSS;
            qr = fe_wait(w, q_ctx, q_l1i);
        }
        ++q[qr];
        // A redirect replaces F2's request, so an old path's L1I wait does
        // not mark the new stream's.
        if (QA(issue) || redirect) q_l1i = false;
        q_jclr = QA(jump_clear);
        if (redirect) q_ctx = X_RD;
        else if (QA(advance)) {
            if (QA(taken)) {
                const bool self = QA(prof_tself);
                q_ctx = self ? X_TKSELF : X_TK;
                ++q_taken;
                q_tself += self;
            } else if ((start ^ QA(start_next)) & 16u) {
                // The first half after a jump or redirect, then the rest.
                q_ctx = q_ctx == X_SEQ || q_ctx == X_SEQ1 ? X_SEQ : X_SEQ1;
            }
        }
        const unsigned q_then = q_hist[1];
        // A redirect empties DC on its edge, so the clock after it is the
        // redirect's too, whatever Q did the clock before it.
        q_hist[1] = redirect ? unsigned(F_REDIRECT) : q_hist[0];
        q_hist[0] = qr;
        if (S(wb_commit)) ++commits;
        if (S(prof_br)) {
            ++br;
            br_taken += S(prof_taken);
            br_pt += S(prof_pt);
            br_pt_taken += S(prof_pt) && S(prof_taken);
        }
        if (S(prof_ret)) {
            ++ret;
            ret_pt += S(prof_pt);
            ret_mis += S(prof_ret_mis);
        }
        if (S(prof_frp)) ++fetch_replays;
        if (S(wb_flush)) ++flush_cycles;
        const bool l1d = S(freeze) && S(m2_load) && !S(dmem_valid) && !mmio_busy;
        if (l1d && !was_l1d) ++l1d_waits;
        was_l1d = l1d;
        const bool l1i = !S(mem_valid);
        if (l1i && !was_l1i) ++l1i_waits;
        was_l1i = l1i;
        Bucket b;
        if (S(issue)) {
            b = S(d_first) ? ISSUE_FIRST : ISSUE_MORE;
            since_redirect = false;
        } else if (S(halted)) b = HALT;
        else if (S(freeze)) {
            if (S(m2_load) && !S(dmem_valid)) b = mmio_busy ? FRZ_MMIO : FRZ_L1D;
            else if (S(md_busy)) b = FRZ_MD;
            else if (S(io_wait)) b = FRZ_IO;
            else if (S(int_wait)) b = FRZ_INTA;
            else b = FRZ_STORE;
        } else if (!S(dc_valid)) {
            if (since_redirect) b = l1i || refill_l1i ? REFILL_L1I : REFILL;
            else b = l1i ? FE_L1I : FE_EMPTY;
            if (since_redirect && l1i) refill_l1i = true;
        } else if (S(d_stop)) b = D_STOP;
        else if (S(gov_block)) b = GOV;
        else if (S(addr_use) && S(lea_stall)) {
            b = AGI;
            const unsigned at = S(prof_agi) < 4 ? S(prof_agi) : 4;
            const Shadow sh = at < 4 ? shadow[at] : Shadow{};
            Agi &a = agi[sh.cls < NAGI ? unsigned(sh.cls) : unsigned(A_OTHER)];
            ++a.stage[at];
            a.ready += sh.ready;
            a.imm += sh.imm;
            a.w32 += sh.ready && sh.w32;
        }
        else if ((S(reads_seg) && S(seg_hazard)) || (S(mem) && S(mseg_hazard))) b = SEG;
        else if (S(store_hazard)) b = STLD;
        else if (S(flag_hazard)) b = FLAG;
        else if (S(sp_hazard) || S(stack_wait)) b = SP;
        else if (S(t_wait)) b = TEMP;
        else if (S(uc_wait)) b = UROM;
        else if (S(str_wait) || S(str_hazard)) b = STR;
        else if (S(io_hazard)) b = IO_SERIAL;
        else if (S(ser_wait)) b = REPLAY_SERIAL;
        else b = OTHER_WAIT;
        ++bucket[b];
        for (unsigned i = 0; i < 4; ++i)
            if (b == fe_bucket[i]) ++fe[i][q_then < NFE ? q_then : unsigned(F_UNATTR)];
        if (redirect) { ++redirects; since_redirect = true; refill_l1i = false; }
        if (!S(freeze)) {
            for (int i = 3; i > 0; --i) shadow[i] = shadow[i - 1];
            Shadow d;
            if (S(issue)) {
                d.cls = uint8_t(S(prof_dcls));
                d.imm = S(prof_dimm);
                d.w32 = S(prof_d32);
                // A 32-bit MOV does not read its destination (src1).
                d.ready = !S(prof_ds2) && (!S(prof_ds1) || (d.cls == A_MOV && S(prof_d32)));
            }
            shadow[0] = d;
        }
#undef FB
#undef QA
#undef S
    }
#ifdef CPU_PROFILE_PIPE_MSB
    // The uops in AG, M1, M2, EX and WB, after the clock's edge.
    template <typename Cpu> static unsigned in_flight(Cpu &cpu) {
        auto *r = cpu.rootp;
        auto valid = [](const auto &stage) {
            return unsigned(stage[CPU_PROFILE_PIPE_MSB / 32] >> (CPU_PROFILE_PIPE_MSB % 32)) & 1u;
        };
        return valid(r->fes_i386_cpu__DOT__ag_s) + valid(r->fes_i386_cpu__DOT__m1_s)
               + valid(r->fes_i386_cpu__DOT__m2_s) + valid(r->fes_i386_cpu__DOT__ex_s)
               + valid(r->fes_i386_cpu__DOT__wb_s);
    }
#endif
    // A clock the bench runs the machine alone while the CPU waits on an I/O
    // answer or sleeps in HLT (RtlCpu's parked clocks).
    void parked(bool asleep) { ++sampled; ++bucket[asleep ? PARKED_HALT : PARKED_IO]; }
    void io(bool write, uint16_t port, unsigned length, uint32_t wdata) {
        ++io_accesses;
        if (crtc_frame.observe(write, port, length, wdata)) ++frames;
        if (!write && port == 0x3da) ++polls_3da;
    }

    static Class classify(const uint8_t *b, unsigned length, unsigned &extra_prefixes) {
        unsigned at = 0;
        bool rep = false;
        for (; at < length; ++at) {
            const uint8_t p = b[at];
            if (p == 0xf2 || p == 0xf3) rep = true;
            else if (!(p == 0x66 || p == 0x67 || p == 0xf0 || p == 0x26 || p == 0x2e || p == 0x36 || p == 0x3e
                       || p == 0x64 || p == 0x65)) break;
        }
        extra_prefixes = at > 1 ? at - 1 : 0;
        if (at >= length) return C_OTHER;
        const uint8_t op = b[at];
        const uint8_t reg = at + 1 < length ? (b[at + 1] >> 3) & 7 : 0;
        const bool rm_mem = at + 1 < length && (b[at + 1] >> 6) != 3;
        if (op == 0x0f) {
            const uint8_t op2 = at + 1 < length ? b[at + 1] : 0;
            if (op2 >= 0x80 && op2 <= 0x8f) return C_JCC;
            if (op2 == 0xaf) return C_MUL;
            if (op2 == 0xa4 || op2 == 0xa5 || op2 == 0xac || op2 == 0xad) return C_SHIFT;
            if (op2 == 0xa0 || op2 == 0xa1 || op2 == 0xa8 || op2 == 0xa9) return C_PUSHPOP;
            if (op2 == 0xb2 || op2 == 0xb4 || op2 == 0xb5) return C_SEGLOAD;
            return C_OTHER;
        }
        if (op >= 0x70 && op <= 0x7f) return C_JCC;
        if (op == 0xeb || op == 0xe9) return C_JMP;
        if (op == 0xe8) return C_CALL;
        if (op == 0xc3 || op == 0xc2) return C_RET;
        if (op == 0x9a) return C_FAR_CALL;
        if (op == 0xea) return C_FAR_JMP;
        if (op == 0xca || op == 0xcb) return C_RETF;
        if (op == 0xcc || op == 0xcd || op == 0xce) return C_INT;
        if (op == 0xcf) return C_IRET;
        if (op >= 0xe0 && op <= 0xe2) return C_LOOP;
        if (op == 0xe3) return C_JCXZ;
        if (op == 0xff) {
            if (reg == 2) return C_CALL_IND;
            if (reg == 3) return C_FAR_CALL;
            if (reg == 4) return C_JMP_IND;
            if (reg == 5) return C_FAR_JMP;
            if (reg == 6) return C_PUSHPOP;
        }
        if ((op >= 0xa4 && op <= 0xa7) || (op >= 0xaa && op <= 0xaf) || (op >= 0x6c && op <= 0x6f))
            return rep ? C_REP_STR : C_STR;
        if ((op >= 0xe4 && op <= 0xe7) || (op >= 0xec && op <= 0xef)) return C_IO;
        if (op == 0xf6 || op == 0xf7) {
            if (reg == 4 || reg == 5) return C_MUL;
            if (reg == 6 || reg == 7) return C_DIV;
        }
        if (op == 0x69 || op == 0x6b) return C_MUL;
        if (op == 0xc0 || op == 0xc1 || (op >= 0xd0 && op <= 0xd3)) return C_SHIFT;
        if ((op >= 0x50 && op <= 0x61) || op == 0x68 || op == 0x6a || op == 0x8f || op == 0x9c || op == 0x9d
            || op == 0x06 || op == 0x07 || op == 0x0e || op == 0x16 || op == 0x17 || op == 0x1e || op == 0x1f
            || op == 0xc8 || op == 0xc9)
            return C_PUSHPOP;
        if (op == 0x8e || op == 0xc4 || op == 0xc5) return C_SEGLOAD;
        // A ModRM memory operand among the ALU, MOV and test forms.
        const bool modrm = op < 0x40 ? (op & 7) < 4 : (op >= 0x80 && op <= 0x8b) || op == 0xc6 || op == 0xc7
                           || op == 0xfe || op == 0xff || op == 0xf6 || op == 0xf7;
        if (modrm && rm_mem) return C_LOAD_OP;
        if (op >= 0xa0 && op <= 0xa3) return C_LOAD_OP;
        return C_OTHER;
    }

    // A retirement at clock now: the instruction at linear start, of length
    // bytes, after which the CPU went to next. A delivery (an exception or
    // interrupt) counts as taken.
    void retire(uint64_t now, uint32_t start, uint32_t next, const uint8_t *bytes, unsigned length, bool delivery,
                bool pe) {
        ++retired;
        if (pe) ++pm_retired;
        const uint64_t gap = now - last_retire;
        last_retire = now;
        unsigned extra = 0;
        const Class c = delivery ? C_DELIVERY : classify(bytes, length, extra);
        prefixes += extra;
        Counts &k = cls[c];
        ++k.n;
        k.own += gap;
        if (pending >= 0) {
            cls[pending].next += gap;
            pending = -1;
        }
        if (delivery || next != start + length) {
            ++k.taken;
            pending = c;
        }
        Site &s = sites[start];
        ++s.n;
        s.cycles += gap;
        s.c = uint8_t(c);
    }

    // One snapshot: T (counters, and the uops in flight), B (buckets), A (the
    // AGI split), F (the front-end split, one per fe_bucket), Q (Q's
    // reasons), C (one per class) and S (every site retired since the last
    // dump) lines, each starting with the simulated time.
    void dump(FILE *out, uint64_t cycles, double seconds, unsigned in_flight) {
        std::fprintf(out, "T %.6f cycles=%llu sampled=%llu retired=%llu pm_retired=%llu commits=%llu flush_cycles=%llu "
                     "redirects=%llu l1d_waits=%llu l1i_waits=%llu frames=%llu polls_3da=%llu io=%llu prefixes=%llu "
                     "in_flight=%u br=%llu br_taken=%llu br_pt=%llu br_pt_taken=%llu fetch_replays=%llu "
                     "ret=%llu ret_pt=%llu ret_mis=%llu q_taken=%llu q_tself=%llu\n",
                     seconds, (unsigned long long)cycles, (unsigned long long)sampled, (unsigned long long)retired,
                     (unsigned long long)pm_retired, (unsigned long long)commits, (unsigned long long)flush_cycles,
                     (unsigned long long)redirects, (unsigned long long)l1d_waits, (unsigned long long)l1i_waits,
                     (unsigned long long)frames, (unsigned long long)polls_3da, (unsigned long long)io_accesses,
                     (unsigned long long)prefixes, in_flight, (unsigned long long)br, (unsigned long long)br_taken,
                     (unsigned long long)br_pt, (unsigned long long)br_pt_taken, (unsigned long long)fetch_replays,
                     (unsigned long long)ret, (unsigned long long)ret_pt, (unsigned long long)ret_mis,
                     (unsigned long long)q_taken, (unsigned long long)q_tself);
        std::fprintf(out, "B %.6f", seconds);
        for (int b = 0; b < NBUCKET; ++b) std::fprintf(out, " %s=%llu", bucket_name[b], (unsigned long long)bucket[b]);
        std::fprintf(out, "\n");
        for (int c = 0; c < NAGI; ++c)
            std::fprintf(out, "A %.6f %s ag=%llu m1=%llu m2=%llu ex=%llu none=%llu ready=%llu imm=%llu w32=%llu\n", seconds,
                         agi_name[c], (unsigned long long)agi[c].stage[0], (unsigned long long)agi[c].stage[1],
                         (unsigned long long)agi[c].stage[2], (unsigned long long)agi[c].stage[3],
                         (unsigned long long)agi[c].stage[4], (unsigned long long)agi[c].ready,
                         (unsigned long long)agi[c].imm, (unsigned long long)agi[c].w32);
        for (unsigned i = 0; i < 4; ++i) {
            std::fprintf(out, "F %.6f %s", seconds, bucket_name[fe_bucket[i]]);
            for (unsigned c = 0; c < NFE; ++c) std::fprintf(out, " %s=%llu", fe_name(c).c_str(), (unsigned long long)fe[i][c]);
            std::fprintf(out, "\n");
        }
        std::fprintf(out, "Q %.6f", seconds);
        for (unsigned c = 0; c < NQ; ++c) std::fprintf(out, " %s=%llu", fe_name(c).c_str(), (unsigned long long)q[c]);
        std::fprintf(out, "\n");
        for (int c = 0; c < NCLASS; ++c)
            std::fprintf(out, "C %.6f %s n=%llu taken=%llu own=%llu next=%llu\n", seconds, class_name[c],
                         (unsigned long long)cls[c].n, (unsigned long long)cls[c].taken,
                         (unsigned long long)cls[c].own, (unsigned long long)cls[c].next);
        std::vector<std::pair<uint32_t, Site>> top(sites.begin(), sites.end());
        std::sort(top.begin(), top.end(), [](const auto &a, const auto &b) {
            return a.second.cycles != b.second.cycles ? a.second.cycles > b.second.cycles : a.first < b.first;
        });
        for (const auto &[at, s] : top)
            std::fprintf(out, "S %.6f %08x %s n=%llu cycles=%llu\n", seconds, at, class_name[s.c],
                         (unsigned long long)s.n, (unsigned long long)s.cycles);
        sites.clear();
        std::fflush(out);
    }
};
