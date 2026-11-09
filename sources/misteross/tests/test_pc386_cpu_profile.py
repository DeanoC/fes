# SPDX-License-Identifier: MIT
"""machine_tb --cpu-profile: cpu_profile.h's clock buckets, classes and page
flips against a model CPU, its snapshots through scripts/pc386_cpu_profile.py,
and the latency bench's measurement. No simulation; the short window is
sim_fes_pc386.py --suite cpu-profile."""
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import textwrap
import unittest

from scripts import pc386_cpu_profile as profile
from scripts import sim_fes_pc386 as sim
from scripts import test_fes_i386_latency as latency

HEADER = sim.CORE / 'sim/cpu_profile.h'
VLT = sim.CORE / 'sim/cpu_profile.vlt'
COMPILER = shutil.which(os.environ.get('CXX', 'c++'))
# The modules cpu_profile.vlt reads, by their instance's path in
# fes_i386_cpu's Verilated names.
MODULES = {'fes_i386_cpu': '', 'fes_i386_qalign': 'frontend__DOT__align__DOT__',
           'fes_i386_fetchbuf': 'frontend__DOT__fetch__DOT__'}
NUMERIC = ('prof_agi', 'prof_dcls', 'frontend__DOT__align__DOT__start', 'frontend__DOT__align__DOT__start_next',
           'frontend__DOT__fetch__DOT__half_valid', 'frontend__DOT__fetch__DOT__tail_ok')  # wider than a bit


def vlt_entries() -> list[tuple[str, str]]:
    return re.findall(r'public_flat_rd -module "(\w+)" -var "(\w+)"', VLT.read_text())


def vlt_signals() -> list[str]:
    """Each signal's name below fes_i386_cpu__DOT__."""
    return [MODULES[module] + name for module, name in vlt_entries()]


class SignalTest(unittest.TestCase):
    def test_the_config_exposes_exactly_what_the_header_reads(self):
        text = HEADER.read_text()
        read = (set(re.findall(r'\bS\((\w+)\)', text)) | set(re.findall(r'r->fes_i386_cpu__DOT__(\w+)', text))
                | {MODULES['fes_i386_qalign'] + n for n in re.findall(r'\bQA\((\w+)\)', text)}
                | {MODULES['fes_i386_fetchbuf'] + n for n in re.findall(r'\bFB\((\w+)\)', text)})
        read -= {prefix + 'name' for prefix in MODULES.values()}  # the macros' own parameter
        self.assertEqual(read, set(vlt_signals()))

    def test_every_signal_is_declared_in_its_module(self):
        for module, name in vlt_entries():
            with self.subTest(module=module, name=name):
                self.assertIn(module, MODULES)
                rtl = (sim.COMMON / f'rtl/i386/{module}.sv').read_text()
                self.assertRegex(rtl, rf'(?m)^\s*(output\s+)?(logic|wire|pipe_t)\b[^;]*\b{name}\b')

    def test_the_machine_bench_builds_the_cpu_with_the_config(self):
        self.assertIn("CPU_RTL + (CORE / 'sim/cpu_profile.vlt',)", Path(sim.__file__).read_text())


# The driver: a model CPU with every exposed signal, the cases below, and a
# profile file the Python side reads back.
DRIVER = r'''
#include <cassert>
#include <cstring>

#define CPU_PROFILE_PIPE_MSB 907
#include "cpu_profile.h"
struct Root {
#define F(n) bool fes_i386_cpu__DOT__##n = false;
#define N(n) uint8_t fes_i386_cpu__DOT__##n = 0;
SIGNALS
#undef N
#undef F
    uint32_t fes_i386_cpu__DOT__ag_s[29] = {}, fes_i386_cpu__DOT__m1_s[29] = {}, fes_i386_cpu__DOT__m2_s[29] = {},
             fes_i386_cpu__DOT__ex_s[29] = {}, fes_i386_cpu__DOT__wb_s[29] = {};
};
struct Cpu { Root root; Root *rootp = &root; };

static unsigned bucket_of(CpuProfile &p, Cpu &cpu, bool mmio = false) {
    uint64_t before[CpuProfile::NBUCKET];
    std::memcpy(before, p.bucket, sizeof before);
    p.sample(cpu, mmio, false);
    for (unsigned b = 0; b < CpuProfile::NBUCKET; ++b)
        if (p.bucket[b] != before[b]) return b;
    return CpuProfile::NBUCKET;
}
#define R(n) cpu.root.fes_i386_cpu__DOT__##n

int main(int, char **argv) {
    // Buckets, in priority order.
    {
        CpuProfile p;
        Cpu cpu;
        R(issue) = true; R(d_first) = true; R(freeze) = true; R(mem_valid) = true;
        assert(bucket_of(p, cpu) == CpuProfile::ISSUE_FIRST);
        R(d_first) = false;
        assert(bucket_of(p, cpu) == CpuProfile::ISSUE_MORE);
        R(issue) = false; R(halted) = true;
        assert(bucket_of(p, cpu) == CpuProfile::HALT);
        R(halted) = false; R(m2_load) = true;
        assert(bucket_of(p, cpu) == CpuProfile::FRZ_L1D);
        assert(p.l1d_waits == 1);
        assert(bucket_of(p, cpu) == CpuProfile::FRZ_L1D);
        assert(p.l1d_waits == 1);  // one miss, two clocks
        assert(bucket_of(p, cpu, true) == CpuProfile::FRZ_MMIO);
        R(m2_load) = false; R(md_busy) = true;
        assert(bucket_of(p, cpu) == CpuProfile::FRZ_MD);
        R(md_busy) = false; R(io_wait) = true;
        assert(bucket_of(p, cpu) == CpuProfile::FRZ_IO);
        R(io_wait) = false; R(int_wait) = true;
        assert(bucket_of(p, cpu) == CpuProfile::FRZ_INTA);
        R(int_wait) = false;
        assert(bucket_of(p, cpu) == CpuProfile::FRZ_STORE);
        R(freeze) = false;
        assert(bucket_of(p, cpu) == CpuProfile::FE_EMPTY);
        R(mem_valid) = false;
        assert(bucket_of(p, cpu) == CpuProfile::FE_L1I);
        // A redirect: the front end refills, and an L1I miss on the way
        // makes the rest of the refill REFILL_L1I.
        R(mem_valid) = true; R(dc_valid) = true; R(d_stop) = true; R(redirect) = true;
        assert(bucket_of(p, cpu) == CpuProfile::D_STOP);
        R(redirect) = false; R(dc_valid) = false;
        assert(bucket_of(p, cpu) == CpuProfile::REFILL);
        R(mem_valid) = false;
        assert(bucket_of(p, cpu) == CpuProfile::REFILL_L1I);
        R(mem_valid) = true;
        assert(bucket_of(p, cpu) == CpuProfile::REFILL_L1I);
        R(issue) = true;
        assert(bucket_of(p, cpu) == CpuProfile::ISSUE_MORE);
        R(issue) = false;
        assert(bucket_of(p, cpu) == CpuProfile::FE_EMPTY);
        assert(p.redirects == 1);
        // D's hazards.
        R(dc_valid) = true; R(d_stop) = false;
        struct { bool *on; unsigned want; } held[] = {
            {&R(gov_block), CpuProfile::GOV}, {&R(lea_stall), CpuProfile::AGI}, {&R(seg_hazard), CpuProfile::SEG},
            {&R(store_hazard), CpuProfile::STLD}, {&R(flag_hazard), CpuProfile::FLAG},
            {&R(sp_hazard), CpuProfile::SP}, {&R(t_wait), CpuProfile::TEMP}, {&R(uc_wait), CpuProfile::UROM},
            {&R(str_wait), CpuProfile::STR}, {&R(io_hazard), CpuProfile::IO_SERIAL},
            {&R(ser_wait), CpuProfile::REPLAY_SERIAL}};
        R(addr_use) = true; R(reads_seg) = true;
        for (auto &h : held) *h.on = true;
        for (auto &h : held) {
            assert(bucket_of(p, cpu) == h.want);
            *h.on = false;
        }
        assert(bucket_of(p, cpu) == CpuProfile::OTHER_WAIT);
        p.sample(cpu, false, true);
        p.parked(true);
        p.parked(false);
        uint64_t sum = 0;
        for (uint64_t b : p.bucket) sum += b;
        assert(sum == p.sampled);
        assert(p.bucket[CpuProfile::RESET] == 1 && p.bucket[CpuProfile::PARKED_HALT] == 1
               && p.bucket[CpuProfile::PARKED_IO] == 1);
    }
    // The AGI split: the shadow of AG to EX keeps each issued uop's class.
    {
        CpuProfile p;
        Cpu cpu;
        R(mem_valid) = true; R(dc_valid) = true;
        R(issue) = true; R(prof_dcls) = CpuProfile::A_ADD; R(prof_dimm) = true;
        assert(bucket_of(p, cpu) == CpuProfile::ISSUE_MORE);
        R(issue) = false; R(prof_dcls) = 0; R(prof_dimm) = false;
        R(addr_use) = true; R(lea_stall) = true; R(prof_agi) = 0;
        assert(bucket_of(p, cpu) == CpuProfile::AGI);  // the ADD in AG
        R(prof_agi) = 1;
        assert(bucket_of(p, cpu) == CpuProfile::AGI);  // in M1
        R(freeze) = true;
        assert(bucket_of(p, cpu) == CpuProfile::FRZ_STORE);  // the shadow holds
        R(freeze) = false; R(prof_agi) = 2;
        assert(bucket_of(p, cpu) == CpuProfile::AGI);  // in M2
        R(prof_agi) = 4;
        assert(bucket_of(p, cpu) == CpuProfile::AGI);  // no producer
        const auto &a = p.agi[CpuProfile::A_ADD];
        assert(a.stage[0] == 1 && a.stage[1] == 1 && a.stage[2] == 1 && a.ready == 3 && a.imm == 3 && a.w32 == 0);
        assert(p.agi[CpuProfile::A_NONE].stage[4] == 1);
        // A 32-bit MOV does not wait for its destination's old value; a
        // 16-bit one, or a source that is late, is not ready.
        R(addr_use) = false; R(lea_stall) = false; R(issue) = true;
        R(prof_dcls) = CpuProfile::A_MOV; R(prof_ds1) = true; R(prof_d32) = true;
        p.sample(cpu, false, false);
        assert(p.shadow[0].cls == CpuProfile::A_MOV && p.shadow[0].ready);
        R(prof_d32) = false;
        p.sample(cpu, false, false);
        assert(!p.shadow[0].ready && p.shadow[1].ready);
        R(prof_ds1) = false; R(prof_ds2) = true; R(prof_d32) = true;
        p.sample(cpu, false, false);
        assert(!p.shadow[0].ready);
    }
    // The front-end split: D's empty clock is charged to Q's reason two
    // clocks before; the q counts take every clock as it happens.
    {
        CpuProfile p;
        Cpu cpu;
#define QA(n) R(frontend__DOT__align__DOT__##n)
#define FB(n) R(frontend__DOT__fetch__DOT__##n)
        R(mem_valid) = true; FB(active) = true; FB(half_valid) = 1; R(dc_valid) = true; R(issue) = true;
        QA(out_free) = true;
        auto fe = [&](unsigned r) { return p.fe[3][r]; };  // fe_empty
        QA(issue) = true;
        p.sample(cpu, false, false);                       // t0: Q issues
        QA(issue) = false; QA(consume) = true;
        p.sample(cpu, false, false);                       // t1: a prefix
        QA(consume) = false; QA(start) = 20;
        p.sample(cpu, false, false);                       // t2: half 1 not filled, since the reset's redirect
        R(dc_valid) = false; R(issue) = false;
        QA(start) = 13;
        assert(bucket_of(p, cpu) == CpuProfile::FE_EMPTY);  // t3: Q's t1
        assert(fe(CpuProfile::F_PREFIX) == 1);
        assert(bucket_of(p, cpu) == CpuProfile::FE_EMPTY);  // t4: Q's t2
        assert(fe(CpuProfile::fe_wait(CpuProfile::W_HALF, CpuProfile::X_RD, false)) == 1);
        assert(p.q[CpuProfile::fe_wait(CpuProfile::W_TAIL, CpuProfile::X_RD, false)] == 2);  // t3, t4 at byte 13
        // A predicted jump within its block, then a jump_clear; an L1I wait
        // marks every wait until Q issues.
        QA(start) = 2; QA(advance) = true; QA(taken) = true; QA(prof_tself) = true; QA(jump_clear) = true;
        p.sample(cpu, false, false);                       // t5: half 0 is there, tail not needed: cross
        QA(advance) = false; QA(taken) = false; QA(jump_clear) = false; R(mem_valid) = false;
        p.sample(cpu, false, false);                       // t6: avail cleared by the jump
        R(mem_valid) = true; QA(start) = 18;
        p.sample(cpu, false, false);                       // t7: half 1, after the jump, the L1I waited
        QA(issue) = true;
        p.sample(cpu, false, false);                       // t8: Q issues, which clears the L1I mark
        QA(issue) = false; QA(out_free) = false;
        p.sample(cpu, false, false);                       // t9: Q held
        assert(fe(CpuProfile::fe_wait(CpuProfile::W_CROSS, CpuProfile::X_RD, false)) == 1);   // t5 at t7
        assert(fe(CpuProfile::F_JCLR) == 1);                                                  // t6 at t8
        assert(fe(CpuProfile::fe_wait(CpuProfile::W_HALF, CpuProfile::X_TKSELF, true)) == 1); // t7 at t9
        assert(p.q_taken == 1 && p.q_tself == 1 && p.q[CpuProfile::Q_HELD] == 1 && p.q[CpuProfile::Q_ISSUE] == 2);
        p.sample(cpu, false, false);
        p.sample(cpu, false, false);                       // Q's t8 issue and t9 hold leave D empty: unattributed
        assert(fe(CpuProfile::F_UNATTR) == 2);
        // A redirect: its next two empty clocks are its own.
        R(redirect) = true; R(dc_valid) = true; R(issue) = true;
        p.sample(cpu, false, false);
        R(redirect) = false; R(dc_valid) = false; R(issue) = false;
        p.sample(cpu, false, false);
        p.sample(cpu, false, false);
        assert(p.fe[0][CpuProfile::F_REDIRECT] == 2 && fe(CpuProfile::F_UNATTR) == 2);
        // An L1I wait before a redirect does not mark the new stream's waits.
        const unsigned half_rd = CpuProfile::fe_wait(CpuProfile::W_HALF, CpuProfile::X_RD, false);
        const uint64_t before = p.q[half_rd];
        QA(start) = 20; QA(out_free) = true; R(mem_valid) = false;
        p.sample(cpu, false, false);
        R(redirect) = true; R(dc_valid) = true; R(issue) = true;
        p.sample(cpu, false, false);
        R(redirect) = false; R(mem_valid) = true;
        p.sample(cpu, false, false);  // waits for half 1 after the redirect, F2 hit
        assert(p.q[half_rd] == before + 1);
        uint64_t sum = 0;
        for (uint64_t n : p.fe[3]) sum += n;
        assert(sum == p.bucket[CpuProfile::FE_EMPTY]);
        assert(CpuProfile::fe_name(CpuProfile::fe_wait(CpuProfile::W_TAIL, CpuProfile::X_SEQ1, true)) == "tail_seq1_l1i");
#undef FB
#undef QA
    }
    // Classes.
    {
        unsigned extra = 0;
        auto c = [&](std::initializer_list<uint8_t> b) {
            uint8_t bytes[15] = {};
            unsigned n = 0;
            for (uint8_t x : b) bytes[n++] = x;
            return CpuProfile::classify(bytes, n, extra);
        };
        assert(c({0x75, 0xfe}) == CpuProfile::C_JCC);
        assert(c({0x0f, 0x85, 0, 0}) == CpuProfile::C_JCC);
        assert(c({0xe8, 0, 0}) == CpuProfile::C_CALL);
        assert(c({0xff, 0xd3}) == CpuProfile::C_CALL_IND);
        assert(c({0xff, 0x16, 0, 0}) == CpuProfile::C_CALL_IND);
        assert(c({0xff, 0x1e, 0, 0}) == CpuProfile::C_FAR_CALL);
        assert(c({0x9a, 0, 0, 0, 0}) == CpuProfile::C_FAR_CALL);
        assert(c({0xff, 0xe0}) == CpuProfile::C_JMP_IND);
        assert(c({0xc3}) == CpuProfile::C_RET);
        assert(c({0xcb}) == CpuProfile::C_RETF);
        assert(c({0xe2, 0xfe}) == CpuProfile::C_LOOP);
        assert(c({0xf3, 0xa5}) == CpuProfile::C_REP_STR);
        assert(c({0xa5}) == CpuProfile::C_STR);
        assert(c({0xf7, 0xf1}) == CpuProfile::C_DIV);
        assert(c({0xf7, 0xe1}) == CpuProfile::C_MUL);
        assert(c({0x8b, 0x07}) == CpuProfile::C_LOAD_OP);
        assert(c({0x8b, 0xc3}) == CpuProfile::C_OTHER);
        assert(c({0x8d, 0x07}) == CpuProfile::C_OTHER);  // LEA does not load
        assert(c({0x66, 0x26, 0x8b, 0x07}) == CpuProfile::C_LOAD_OP && extra == 1);
    }
    // Page flips: a word OUT at 3D4h, byte OUTs, and another register.
    {
        CrtcFrameTracker t;
        assert(t.observe(true, 0x3d4, 2, 0x000c));
        assert(!t.observe(true, 0x3d4, 2, 0x000d));
        assert(!t.observe(true, 0x3d5, 1, 0));
        assert(!t.observe(true, 0x3d4, 1, 0x0c));
        assert(!t.observe(false, 0x3d5, 1, 0));
        assert(t.observe(true, 0x3d5, 1, 0x20));
    }
    // Retirements: a taken branch's redirect is its target's gap; the
    // handover starts the next gap.
    {
        CpuProfile p;
        const uint8_t jnz[] = {0x75, 0xfc}, add[] = {0x01, 0xd8};
        p.begin(1000);
        p.retire(1003, 0x100, 0x102, add, 2, false, false);
        p.retire(1005, 0x102, 0x0fe, jnz, 2, false, false);
        p.retire(1017, 0x0fe, 0x100, add, 2, false, true);
        assert(p.cls[CpuProfile::C_OTHER].n == 2 && p.cls[CpuProfile::C_OTHER].own == 15);
        assert(p.cls[CpuProfile::C_JCC].taken == 1 && p.cls[CpuProfile::C_JCC].own == 2);
        assert(p.cls[CpuProfile::C_JCC].next == 12);
        assert(p.retired == 3 && p.pm_retired == 1);
        p.retire(1019, 0x102, 0x104, jnz, 2, false, false);  // not taken
        assert(p.cls[CpuProfile::C_JCC].taken == 1);
        p.retire(1030, 0x200, 0x300, add, 0, true, false);  // a delivery is taken
        p.begin(5000);
        p.retire(5004, 0x300, 0x301, add, 1, false, false);
        assert(p.cls[CpuProfile::C_DELIVERY].next == 0 && p.cls[CpuProfile::C_OTHER].own == 19);
    }
    // The RTL's indirect counts: predicted, and redirected.
    {
        CpuProfile p;
        Cpu cpu;
        R(prof_ind) = true; R(prof_pt) = true; R(prof_redir) = true;
        p.sample(cpu, false, false);
        R(prof_pt) = false;
        p.sample(cpu, false, false);
        assert(p.ind_pt == 1 && p.ind_mis == 2 && p.ret == 0);
    }
    // A profile file for the Python side: two snapshots.
    {
        CpuProfile p;
        Cpu cpu;
        FILE *out = std::fopen(argv[1], "w");
        p.begin(100);
        assert(CpuProfile::in_flight(cpu) == 0);
        cpu.root.fes_i386_cpu__DOT__ag_s[907 / 32] = 1u << (907 % 32);  // valid only
        cpu.root.fes_i386_cpu__DOT__wb_s[907 / 32] = ~(1u << (907 % 32)) & 0xfffu;  // not valid
        cpu.root.fes_i386_cpu__DOT__m2_s[907 / 32] = 1u << (907 % 32);
        assert(CpuProfile::in_flight(cpu) == 2);
        p.dump(out, 100, 0.000001, CpuProfile::in_flight(cpu));
        const uint8_t jnz[] = {0x75, 0xfc};
        R(issue) = true; R(d_first) = true; R(wb_commit) = true; R(mem_valid) = true;
        for (int n = 0; n < 6; ++n) {
            R(wb_flush) = n == 2;
            p.sample(cpu, false, false);
        }
        R(wb_flush) = false;
        R(wb_commit) = false;
        p.sample(cpu, false, false);  // a wrong-path uop
        R(issue) = false; R(dc_valid) = false; R(redirect) = true;
        p.sample(cpu, false, false);
        R(redirect) = false;
        p.sample(cpu, false, false);
        p.sample(cpu, false, false);
        p.retire(105, 0x10, 0x0e, jnz, 2, false, false);
        p.retire(110, 0x0e, 0x10, jnz, 2, false, false);
        // Indirect sites: CALL BX to 0x40, 0x40 again, then 0x50; JMP AX once.
        const uint8_t callbx[] = {0xff, 0xd3}, jmpax[] = {0xff, 0xe0};
        p.retire(110, 0x20, 0x40, callbx, 2, false, false);
        p.retire(110, 0x20, 0x40, callbx, 2, false, false);
        p.retire(110, 0x20, 0x50, callbx, 2, false, false);
        p.retire(110, 0x30, 0x60, jmpax, 2, false, false);
        assert(p.ind_call == 3 && p.ind_call_rep == 1 && p.ind_jmp == 1 && p.ind_jmp_rep == 0);
        cpu.root.fes_i386_cpu__DOT__ag_s[907 / 32] = 0;
        cpu.root.fes_i386_cpu__DOT__m2_s[907 / 32] = 0;
        cpu.root.fes_i386_cpu__DOT__ex_s[907 / 32] = 1u << (907 % 32);
        p.dump(out, 110, 0.000002, CpuProfile::in_flight(cpu));
        std::fclose(out);
    }
    return 0;
}
'''


@unittest.skipUnless(COMPILER, 'requires a C++ compiler')
class HeaderTest(unittest.TestCase):
    def test_buckets_classes_flips_and_snapshots(self):
        signals = ' '.join(f'{"N" if name in NUMERIC else "F"}({name})' for name in vlt_signals()
                           if name not in sim.PIPE_STAGES)
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            source = work / 'driver.cpp'
            source.write_text(DRIVER.replace('SIGNALS', signals))
            binary = work / 'driver'
            built = subprocess.run([COMPILER, '-std=c++17', '-Wall', '-Wextra', '-Werror', '-UNDEBUG',
                                    f'-I{HEADER.parent}', str(source), '-o', str(binary)],
                                   capture_output=True, text=True)
            self.assertEqual(built.returncode, 0, built.stderr)
            output = work / 'cpu.prof'
            ran = subprocess.run([str(binary), str(output)], capture_output=True, text=True)
            self.assertEqual(ran.returncode, 0, ran.stderr)
            w = profile.window(profile.load(output.read_text().splitlines()))
        self.assertEqual(w['from_cycle'], 100)
        self.assertEqual(w['cycles'], 10)
        self.assertEqual(w['indirect_sites'], [['00000020', 3, 1, 2, 'call_ind'], ['00000030', 1, 0, 1, 'jmp_ind']])
        p = w['prediction']
        self.assertEqual((p['ind_call'], p['ind_call_last_target'], p['ind_jmp'], p['ind_jmp_last_target']),
                         (3, 1, 1, 0))
        self.assertIn('indirect CALLs 3 (1 to their last target)', profile.report(w))
        self.assertEqual(w['rtl_clocks'], 10)
        self.assertEqual(w['causes']['issued uops'], 7)
        self.assertEqual(w['commits'], 6)
        self.assertEqual((w['in_flight_start'], w['in_flight_end']), (2, 1))
        self.assertEqual(w['flush_cycles'], 1)
        self.assertEqual(w['causes']['redirect: refill'], 2)
        self.assertEqual(w['causes']['fetch and L1I'], 1)
        self.assertEqual(sum(w['causes'].values()), w['rtl_clocks'])
        self.assertEqual(w['redirects_by_class'], {'jcc': {'taken': 1, 'clocks': 5, 'per_taken': 5.0},
                                                   'call_ind': {'taken': 3, 'clocks': 0, 'per_taken': 0.0},
                                                   'jmp_ind': {'taken': 1, 'clocks': 0, 'per_taken': 0.0}})
        self.assertEqual(sorted(site[:3] for site in w['sites']),
                         [['0000000e', 1, 5], ['00000010', 1, 5], ['00000020', 3, 0], ['00000030', 1, 0]])


def snapshot(at: float, cycles: int, sampled: int, buckets: dict, commits: int = 0, in_flight: int = 0,
             flush_cycles: int = 0) -> str:
    names = dict.fromkeys((b for keys in profile.CAUSES.values() for b in keys), 0)
    names.update(dict.fromkeys(profile.ISSUE, 0))
    names.update(buckets)
    return textwrap.dedent(f'''\
        T {at} cycles={cycles} sampled={sampled} retired={commits} pm_retired=0 commits={commits} flush_cycles={flush_cycles} redirects=0 l1d_waits=0 l1i_waits=0 frames=0 polls_3da=0 io=0 prefixes=0 in_flight={in_flight} br=0 br_taken=0 br_pt=0 br_pt_taken=0 fetch_replays=0 ret=0 ret_pt=0 ret_mis=0 ind_call=0 ind_call_rep=0 ind_jmp=0 ind_jmp_rep=0 ind_pt=0 ind_mis=0
        B {at} ''') + ' '.join(f'{k}={v}' for k, v in names.items()) + '\n'


class WindowTest(unittest.TestCase):
    def test_a_window_is_the_difference_of_two_snapshots(self):
        text = (snapshot(1.0, 100, 0, {}) + snapshot(2.0, 300, 200, {'issue_first': 150, 'agi': 50}, 140)
                + snapshot(3.0, 500, 400, {'issue_first': 300, 'agi': 60, 'halt': 40}, 290))
        snaps = profile.load(text.splitlines())
        w = profile.window(snaps, 1.5, 2.5)
        self.assertEqual((w['from'], w['to'], w['cycles']), (1.0, 3.0, 400))
        w = profile.window(snaps, 2.0, 3.0)
        self.assertEqual(w['cycles'], 200)
        self.assertEqual(w['causes']['interlock: AGI'], 10)
        self.assertEqual(w['causes']['halt'], 40)
        self.assertEqual(w['causes']['issued uops'], 150)
        self.assertIn('interlock: AGI', profile.report(w))

    def test_the_agi_split_sums_to_agi_and_counts_early_alu_producers(self):
        a1 = 'A 1.0 add ag=0 m1=0 m2=0 ex=0 none=0 ready=0 imm=0\nA 1.0 load ag=0 m1=0 m2=0 ex=0 none=0 ready=0 imm=0\n'
        a2 = 'A 2.0 add ag=30 m1=5 m2=0 ex=0 none=0 ready=32 imm=20\nA 2.0 load ag=10 m1=5 m2=0 ex=0 none=0 ready=15 imm=0\n'
        text = snapshot(1.0, 0, 0, {}) + a1 + snapshot(2.0, 50, 50, {'agi': 50}) + a2
        w = profile.window(profile.load(text.splitlines()))
        self.assertEqual(w['agi']['clocks'], 50)
        self.assertEqual(w['agi']['early'], 32)  # a load's ready does not count
        self.assertEqual(w['agi']['by_class']['add']['clocks'], 35)
        self.assertIn('early ALU would run', profile.report(w))
        bad = text.replace('m1=5 m2=0 ex=0 none=0 ready=15', 'm1=6 m2=0 ex=0 none=0 ready=15')
        with self.assertRaises(profile.ProfileError):
            profile.window(profile.load(bad.splitlines()))

    def test_the_front_end_split_sums_to_each_bucket(self):
        def f(at, empty, l1i):
            lines = ''.join(f'F {at} {k} prefix=0 half_tk=0 half_tk_l1i=0\n' for k in ('refill', 'refill_l1i', 'fe_l1i'))
            return lines + f'F {at} fe_empty prefix={empty} half_tk={l1i} half_tk_l1i={l1i}\nQ {at} issue=0 held=0 prefix={empty}\n'
        text = snapshot(1.0, 0, 0, {}) + f(1.0, 0, 0) + snapshot(2.0, 10, 10, {'fe_empty': 7, 'halt': 3}) + f(2.0, 3, 2)
        w = profile.window(profile.load(text.splitlines()))
        rows = w['front_end']['by_reason']
        self.assertEqual((rows['prefix']['fe_empty'], rows['half_tk']['fe_empty'], rows['half_tk']['l1i']), (3, 4, 2))
        self.assertEqual(rows['prefix']['latent'], 3)
        self.assertIn('front end empty 7 clocks', profile.report(w))
        bad = text.replace('fe_empty prefix=3', 'fe_empty prefix=4')
        with self.assertRaises(profile.ProfileError):
            profile.window(profile.load(bad.splitlines()))

    def test_flush_of_pre_window_uop_is_reported_separately(self):
        # This uop was issued before the window. Its flush must not be counted
        # as a wrong-path issue from this window or make issued clocks negative.
        text = (snapshot(1.0, 0, 0, {}, 0, 1)
                + snapshot(2.0, 10, 10, {'halt': 10}, 0, 0, flush_cycles=1))
        w = profile.window(profile.load(text.splitlines()))
        self.assertEqual(w['issued'], 0)
        self.assertEqual(w['commits'], 0)
        self.assertEqual((w['in_flight_start'], w['in_flight_end']), (1, 0))
        self.assertEqual(w['flush_cycles'], 1)
        self.assertEqual(w['causes']['issued uops'], 0)
        self.assertNotIn('wrong_path_uops', w)
        self.assertIn('flush cycles 1', profile.report(w))
        self.assertEqual(sum(w['causes'].values()), w['rtl_clocks'])

    def test_buckets_must_hold_every_sampled_clock(self):
        text = snapshot(1.0, 0, 0, {}) + snapshot(2.0, 10, 10, {'agi': 9})
        with self.assertRaises(profile.ProfileError):
            profile.window(profile.load(text.splitlines()))

    def test_one_snapshot_is_not_a_window(self):
        with self.assertRaises(profile.ProfileError):
            profile.window(profile.load(snapshot(1.0, 0, 0, {}).splitlines()))

    def test_every_bucket_is_in_one_cause(self):
        named = [b for keys in profile.CAUSES.values() for b in keys]
        self.assertEqual(len(named), len(set(named)))
        buckets = re.search(r'bucket_name\[NBUCKET\] = \{(.*?)\};', HEADER.read_text(), re.S).group(1)
        self.assertEqual(set(named), set(re.findall(r'"(\w+)"', buckets)))


class LatencyTest(unittest.TestCase):
    def test_every_loop_has_an_expected_value_and_marks(self):
        self.assertEqual([name for name, _, _ in latency.LOOPS], list(latency.EXPECTED))
        program = latency.PROGRAM.read_text()
        for n in range(2 * len(latency.LOOPS)):
            self.assertRegex(program, rf'(?m)^\s*mark {n}$')

    def test_per_case_takes_the_bare_loop_out(self):
        cycles = {name: 12 * iterations + cases * 11 * iterations for name, iterations, cases in latency.LOOPS}
        rows = latency.measure(cycles)
        self.assertEqual(rows[0]['per_iteration'], 12.0)
        self.assertNotIn('per_case', rows[0])
        self.assertTrue(all(row['per_case'] == 11.0 for row in rows[1:]))
        self.assertEqual(latency.failures(rows, cycles), [])
        changed = dict(cycles, call_ret=cycles['call_ret'] - 8000)
        self.assertEqual(len(latency.failures(rows, changed)), 1)


if __name__ == '__main__':
    unittest.main()
