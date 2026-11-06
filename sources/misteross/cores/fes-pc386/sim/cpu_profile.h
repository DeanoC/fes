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
// cpu_profile.vlt lists. A stage register is a packed pipe_t with valid in
// its top bit, CPU_PROFILE_PIPE_MSB, which the bench build reads from the
// Verilated model into cpu_profile_layout.h.
#pragma once

#include <algorithm>
#include <cstdint>
#include <cstdio>
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
    struct Counts { uint64_t n = 0, taken = 0, own = 0, next = 0; };
    struct Site { uint64_t n = 0, cycles = 0; uint8_t c = 0; };

    uint64_t bucket[NBUCKET] = {};
    uint64_t commits = 0, flush_cycles = 0, redirects = 0, l1d_waits = 0, l1i_waits = 0, retired = 0, pm_retired = 0;
    uint64_t frames = 0, polls_3da = 0, io_accesses = 0;
    uint64_t prefixes = 0;  // prefix bytes before an instruction's last one
    // Direct branches retired (Jcc, LOOP and its kin, JCXZ, JMP and CALL rel):
    // taken; predicted taken; predicted taken and taken; and fetch-check
    // replays (fes_i386_cpu, "Branch prediction").
    uint64_t br = 0, br_taken = 0, br_pt = 0, br_pt_taken = 0, fetch_replays = 0;
    uint64_t sampled = 0;   // clocks classified
    Counts cls[NCLASS];
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
    }

    // One system clock, after the CPU model's eval at the clock's low phase.
    template <typename Cpu> void sample(Cpu &cpu, bool mmio_busy, bool resetting) {
        ++sampled;
        if (resetting) { ++bucket[RESET]; return; }
        auto *r = cpu.rootp;
#define S(name) (r->fes_i386_cpu__DOT__##name)
        const bool redirect = S(redirect);
        if (S(wb_commit)) ++commits;
        if (S(prof_br)) {
            ++br;
            br_taken += S(prof_taken);
            br_pt += S(prof_pt);
            br_pt_taken += S(prof_pt) && S(prof_taken);
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
        else if (S(addr_use) && S(lea_stall)) b = AGI;
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
#undef S
        ++bucket[b];
        if (redirect) { ++redirects; since_redirect = true; refill_l1i = false; }
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

    // One snapshot: T (counters, and the uops in flight), B (buckets), C (one
    // per class) and S (every site retired since the last dump) lines, each
    // starting with the simulated time.
    void dump(FILE *out, uint64_t cycles, double seconds, unsigned in_flight) {
        std::fprintf(out, "T %.6f cycles=%llu sampled=%llu retired=%llu pm_retired=%llu commits=%llu flush_cycles=%llu "
                     "redirects=%llu l1d_waits=%llu l1i_waits=%llu frames=%llu polls_3da=%llu io=%llu prefixes=%llu "
                     "in_flight=%u br=%llu br_taken=%llu br_pt=%llu br_pt_taken=%llu fetch_replays=%llu\n",
                     seconds, (unsigned long long)cycles, (unsigned long long)sampled, (unsigned long long)retired,
                     (unsigned long long)pm_retired, (unsigned long long)commits, (unsigned long long)flush_cycles,
                     (unsigned long long)redirects, (unsigned long long)l1d_waits, (unsigned long long)l1i_waits,
                     (unsigned long long)frames, (unsigned long long)polls_3da, (unsigned long long)io_accesses,
                     (unsigned long long)prefixes, in_flight, (unsigned long long)br, (unsigned long long)br_taken,
                     (unsigned long long)br_pt, (unsigned long long)br_pt_taken, (unsigned long long)fetch_replays);
        std::fprintf(out, "B %.6f", seconds);
        for (int b = 0; b < NBUCKET; ++b) std::fprintf(out, " %s=%llu", bucket_name[b], (unsigned long long)bucket[b]);
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
