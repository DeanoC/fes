# SPDX-License-Identifier: MIT
"""machine_tb --census (cores/fes-pc386/sim/census.h): system-register stems,
the page walk for stems and gates, task switches by source, V86 entries and
IOPL-sensitive outcomes, I/O through the bitmap and the TLB models, against
hand-built oracle states; and scripts/pc386_census_summary.py's rows. No
simulation."""
import contextlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

from scripts import pc386_census_summary as summary
from scripts import sim_fes_pc386 as sim

HEADER = sim.CORE / 'sim/census.h'
ORACLE = sim.COMMON / 'sim/i386'
COMPILER = shutil.which(os.environ.get('CXX', 'c++'))

DRIVER = r'''
#include <cassert>
#include <fstream>
#include <vector>

#include "census.h"

using namespace fes_i386;
using pc386_census::Census;

static std::vector<uint8_t> ram(1 << 20);
static void put32(uint32_t at, uint32_t v) { for (int n = 0; n < 4; ++n) ram[at + n] = uint8_t(v >> (8 * n)); }
static void put(uint32_t at, std::initializer_list<uint8_t> bytes) { for (uint8_t b : bytes) ram[at++] = b; }
// An IDT gate at 0x800 + vector * 8.
static void gate(unsigned vector, unsigned type, unsigned dpl, uint16_t selector) {
    const uint32_t at = 0x800 + vector * 8;
    put(at, {0, 0, uint8_t(selector), uint8_t(selector >> 8), 0, uint8_t(0x80 | dpl << 5 | type), 0, 0});
}
static State pm(uint32_t eip, unsigned cpl = 0) {
    State s;
    s.cr0 = 1;
    s.cpl = uint8_t(cpl);
    s.eip = eip;
    s.seg[SEG_CS].base = 0;
    s.idtr.base = 0x800;
    s.tr.selector = 0x28;
    s.tr.base = 0x4000;
    s.tr.limit = 0x67;
    s.tr.ar = 0x8b;
    return s;
}
static StepResult retired() { StepResult r; r.retired = true; return r; }
static StepResult with(EventKind kind, uint8_t vector, uint32_t error = 0, bool done = false) {
    StepResult r;
    r.retired = done;
    r.events.push_back({kind, vector, error, 0});
    return r;
}
static void step(Census &c, const State &before, const State &after, const StepResult &r) {
    c.begin(before, "site");
    c.end(before, after, r);
}

int main(int, char **argv) {
    // System-register stems.
    {
        const uint8_t mov_cr3[] = {0x0f, 0x22, 0xd8}, mov_dr7[] = {0x0f, 0x23, 0xf8}, crd[] = {0x66, 0x0f, 0x20, 0xc0},
                      tr6[] = {0x0f, 0x26, 0xf0}, other[] = {0x0f, 0x25, 0xc0};
        assert(pc386_census::stem(mov_cr3, 3, true) == "0F22.3");
        assert(pc386_census::stem(mov_cr3, 3) == "0F22");  // the RTL stops' naming
        assert(pc386_census::stem(mov_dr7, 3, true) == "0F23.7");
        assert(pc386_census::stem(crd, 4, true) == "660F20.0");
        assert(pc386_census::stem(tr6, 3, true) == "0F26.6");
        assert(pc386_census::stem(other, 3, true) == "0F25");
        assert(pc386_census::opcode_of("660F22.3:pm0") == "0F22.3");
        assert(pc386_census::opcode_of("67660F00.3:pm0") == "0F00.3");
    }
    Census census([](uint32_t at) { return at < ram.size() ? ram[at] : uint8_t(0xff); });

    // The page walk: CR3 at 0x10000, linear 0x00400000-0x00400fff on
    // physical 0x30000, 0x00401000 not present.
    put32(0x10000 + (0x00400000u >> 22) * 4, 0x11000 | 7);
    put32(0x11000 + 0 * 4, 0x30000 | 7);
    put32(0x11000 + 1 * 4, 0);
    {
        State s = pm(0);
        s.cr0 |= 0x80000000u;
        s.cr3 = 0x10000;
        uint32_t physical;
        assert(census.translate(s, 0x00400123, physical) && physical == 0x30123);
        assert(!census.translate(s, 0x00401000, physical));
        assert(census.linear_byte(s, 0x00401000) == 0xff);
        // The stem is read through the tables: MOV CR3, EAX at 0x30010.
        put(0x30010, {0x0f, 0x22, 0xd8});
        s.eip = 0x00400010;
        assert(census.begin(s, "x") == "0F22.3:pm0");
        // A CR3 write flushes the TLBs.
        StepResult r = retired();
        r.reads.push_back({0x00400200, 0x30200, 4});
        census.end(s, s, r);
        assert(census.itlb.accesses == 1 && census.dtlb.accesses == 1);
        assert(census.itlb.pages.empty() && census.dtlb.pages.empty());
        // Without a CR3 write the pages stay; a page-crossing access touches both.
        s.eip = 0x00400020;
        put(0x30020, {0x90});
        StepResult nop = retired();
        nop.writes.push_back({0x00400ffe, 0, 4});
        step(census, s, s, nop);
        assert(census.itlb.pages.size() == 1 && census.dtlb.pages.size() == 2);
        assert(census.paged_steps == 2 && census.page_cross == 1);
    }

    // The TLB model: 16 entries, LRU.
    {
        pc386_census::Tlb t(16);
        for (uint32_t p = 0; p < 16; ++p) t.touch(p);
        t.touch(0);   // hit, now most recent
        t.touch(16);  // evicts 1
        t.touch(0);
        assert(t.walks == 17);
        t.touch(1);
        assert(t.walks == 18 && t.accesses == 20);
    }

    // Task switches by source.
    put(0x100, {0xea, 0, 0, 0, 0, 0x30, 0});   // JMP FAR 0030:0
    put(0x110, {0x0f, 0x00, 0xd8});             // LTR AX
    put(0x120, {0xcf});                         // IRET
    put(0x130, {0x66, 0xcf});                   // IRETD
    put(0x140, {0x9a, 0, 0, 0, 0, 0x30, 0});   // CALL FAR
    {
        State before = pm(0x100), after = before;
        after.tr.selector = 0x30;
        after.tr.base = 0x5000;
        after.cpl = 3;
        put(0x5066, {0x68, 0x00});
        step(census, before, after, retired());
        assert(census.counts.at("ts:jmp:EA:pm0:tss=0b:nt=0:cr3chg=0:ldtchg=0:vm=0:cpl=0>3") == 1);
        assert(census.counts.at("tss-iomap:base=0068:limit=00067") == 1);
        assert(census.counts.at("tr:0030:EA:pm0") == 1);

        // LTR changes TR but is no task switch.
        State ltr = pm(0x110), loaded = ltr;
        loaded.tr.selector = 0x30;
        step(census, ltr, loaded, retired());
        for (const auto &[key, n] : census.counts) assert(key.find("ts:other") == std::string::npos);

        // CALL into a 32-bit TSS with paging on flushes the TLBs, though
        // CR3 keeps its value.
        State call = pm(0x140), called = call;
        call.cr0 = called.cr0 = 0x80000001u;
        call.cr3 = called.cr3 = 0x10000;
        call.eip = called.eip = 0x00400040;
        put(0x30040, {0x9a, 0, 0, 0, 0, 0x30, 0});
        called.tr.selector = 0x30;
        called.tr.base = 0x5000;
        called.eflags |= 0x4000;
        StepResult r = retired();
        r.writes.push_back({0x00400800, 0, 4});
        step(census, call, called, r);
        assert(census.counts.at("ts:call:9A:pm0:tss=0b:nt=1:cr3chg=0:ldtchg=0:vm=0:cpl=0>0") == 1);
        assert(census.dtlb.pages.empty() && census.itlb.pages.empty());

        // IRET with NT back to the old task.
        State iret = pm(0x120), back = iret;
        iret.eflags |= 0x4000;
        back.tr.selector = 0x30;
        step(census, iret, back, retired());
        assert(census.counts.at("ts:iret:CF:pm0:tss=0b:nt=0:cr3chg=0:ldtchg=0:vm=0:cpl=0>0") == 1);

        // #PF through a task gate; a fault through an interrupt gate is no switch.
        gate(14, 5, 0, 0x40);
        State faulting = pm(0x150), handler = faulting;
        put(0x150, {0x8b, 0x00});
        handler.tr.selector = 0x40;
        handler.eflags |= 0x4000;
        step(census, faulting, handler, with(EventKind::Exception, 14, 6));
        assert(census.counts.at("ts:exception:14:tss=0b:nt=1:cr3chg=0:ldtchg=0:vm=0:cpl=0>0") == 1);
        assert(census.counts.at("event:exception:14:gate=05:dpl=0:sel=0040:err=0006:cpl=0>0") == 1);
    }

    // V86: entry by IRETD, IOPL-sensitive outcomes, I/O through the bitmap.
    {
        State iretd = pm(0x130), v86 = iretd;
        v86.eflags |= 0x20000 | 0x3000;
        v86.cpl = 3;
        step(census, iretd, v86, retired());
        assert(census.counts.at("v86:enter:iret:66CF:pm0:iopl=3") == 1);

        gate(0x21, 0xe, 3, 0x08);   // DPL 3: straight to the monitor
        gate(0x10, 0xe, 0, 0x08);   // DPL 0: #GP(0x10 * 8 + 2)
        State v = pm(0x200, 3);
        v.eflags |= 0x20000;  // IOPL 0
        put(0x200, {0xfa});         // CLI
        put(0x210, {0xcd, 0x21});   // INT 21h
        put(0x220, {0xcd, 0x10});   // INT 10h
        put(0x230, {0xee});         // OUT DX, AL
        put(0x240, {0xfb});         // STI
        State after = v;
        step(census, v, after, with(EventKind::Exception, 13, 0));
        assert(census.counts.at("v86:sens:FA:iopl=0:gp=0000") == 1);
        assert(census.counts.at("event:exception:13:v86:gate=00:dpl=0:sel=0000:err=0000:cpl=3>3") == 1);
        v.eflags |= 0x3000;
        v.eip = 0x210;
        step(census, v, v, with(EventKind::SoftwareInterrupt, 0x21, 0, true));
        assert(census.counts.at("v86:sens:CD:iopl=3:int") == 1);
        v.eip = 0x220;
        step(census, v, v, with(EventKind::Exception, 13, 0x82));
        assert(census.counts.at("v86:sens:CD:iopl=3:gp=0082") == 1);
        v.eip = 0x230;
        step(census, v, v, retired());
        assert(census.counts.at("io-bitmap:v86:ok") == 1);
        // An INTR taken before STI ran is no STI outcome.
        v.eip = 0x240;
        step(census, v, v, with(EventKind::ExternalInterrupt, 8));
        for (const auto &[key, n] : census.counts) assert(key.find("v86:sens:FB") == std::string::npos);
        assert(census.counts.at("v86:steps:iopl=0") == 1);
        assert(census.counts.at("v86:steps:iopl=3") == 4);
        // I/O at CPL 3 above IOPL in protected mode reads the bitmap too.
        State user = pm(0x230, 3);
        step(census, user, user, with(EventKind::Exception, 13, 0));
        assert(census.counts.at("io-bitmap:pm3:gp=0000") == 1);
    }

    // DR7 changes by value.
    {
        State before = pm(0x300), after = before;
        put(0x300, {0x0f, 0x23, 0xf8});
        after.dr7 = 0x401;
        step(census, before, after, retired());
        assert(census.counts.at("0F23.7:pm0") == 1);
        assert(census.counts.at("dr7:00000401:0F23.7:pm0") == 1);
    }

    std::ofstream out(argv[1]);
    census.write(out);
    return 0;
}
'''


@unittest.skipUnless(COMPILER, 'requires a C++ compiler')
class HeaderTest(unittest.TestCase):
    def test_the_driver(self):
        with tempfile.TemporaryDirectory() as tmp:
            source, binary, output = Path(tmp, 'census_test.cpp'), Path(tmp, 'census_test'), Path(tmp, 'census.txt')
            source.write_text(DRIVER)
            built = subprocess.run([COMPILER, '-std=c++17', '-Wall', '-Wextra', '-Werror', '-UNDEBUG',
                                    f'-I{HEADER.parent}', f'-I{ORACLE}', str(source), '-o', str(binary)],
                                   capture_output=True, text=True)
            self.assertEqual(built.returncode, 0, built.stderr)
            ran = subprocess.run([str(binary), str(output)], capture_output=True, text=True)
            self.assertEqual(ran.returncode, 0, ran.stderr)
            lines = output.read_text().splitlines()
            keys = [line.rsplit(' ', 2)[0] for line in lines]
            self.assertEqual(keys, sorted(keys))
            self.assertIn('tlb:dtlb16:accesses 4 -', lines)
            self.assertIn('tlb:itlb8:walks 3 -', lines)
            self.assertIn('0F22.3:pm0 1 x', lines)
            row = summary.summarize(summary.load(output))
            self.assertEqual(row['task_switches'], {'call': 1, 'exception:14': 1, 'iret': 1, 'jmp': 1})
            self.assertEqual(row['sysreg'], {'cr3': {'write': 1}, 'dr7': {'write': 1}})
            self.assertEqual(row['v86_entries'], {'iret': 1})
            self.assertEqual(row['pf_by_error'], {'0006': 1})
            self.assertEqual(row['ltr'], 1)
            self.assertEqual(row['tlb']['itlb8'], {'accesses': 3, 'walks': 3, 'walks_per_100': 100.0})


class SummaryTest(unittest.TestCase):
    CENSUS = '\n'.join([
        '0F22.3:pm0 4 0008:00000100', '660F22.3:pm0 1 0008:00000104', '0F20.0:pm0 2 0008:00000108',
        '0F23.7:pm0 3 0008:00000110', '0F21.6 1 0000:00000200', '0F01.7:pm0 0 0008:00000120',
        'cr0.pg:0F22.0:pm0 2 0008:00000130',
        'ts:jmp:EA:pm0:tss=0b:nt=0:cr3chg=0:ldtchg=0:vm=0:cpl=0>3 5 0008:00000140',
        'ts:exception:14:tss=0b:nt=1:cr3chg=0:ldtchg=1:vm=0:cpl=0>0 2 0008:00000150',
        'v86:enter:task:66CF:pm0:iopl=3 1 0008:00000160', 'v86:enter:iret:66CF:pm0:iopl=3 7 0008:00000160',
        'v86:sens:FA:iopl=3:ok 9 1000:00000010', 'v86:steps:iopl=3 100 1000:00000000',
        'event:exception:14:paged:gate=05:dpl=0:sel=0040:err=0002:cpl=0>0 2 0008:00000150',
        'tlb:dtlb16:accesses 2000 -', 'tlb:dtlb16:walks 3 -', 'tlb:itlb8:accesses 0 -', 'tlb:itlb8:walks 0 -',
        'paging:steps 50 -', 'paging:page-cross 1 -']) + '\n'

    def test_a_row(self):
        with tempfile.TemporaryDirectory() as tmp:
            Path(tmp, 'census.txt').write_text(self.CENSUS)
            Path(tmp, 'report.json').write_text(json.dumps({'end': {'reason': 'limit', 'seconds': 20.0}}))
            row = summary.run('t', Path(tmp))
        self.assertEqual(row['sysreg'], {'cr0': {'read': 2}, 'cr3': {'write': 5}, 'dr6': {'read': 1},
                                         'dr7': {'write': 3}})
        self.assertEqual(row['pg_toggles'], 2)
        self.assertEqual(row['task_switches'], {'exception:14': 2, 'jmp': 5})
        self.assertEqual(row['v86_entries'], {'iret': 7, 'task': 1})
        self.assertEqual(row['v86_sensitive'], {'FA:iopl=3:ok': 9})
        self.assertEqual(row['pf_by_error'], {'0002': 2})
        self.assertEqual(row['tlb'], {'dtlb16': {'accesses': 2000, 'walks': 3, 'walks_per_100': 0.15},
                                      'itlb8': {'accesses': 0, 'walks': 0, 'walks_per_100': None}})
        self.assertEqual((row['end'], row['seconds'], row['paged_steps']), ('limit', 20.0, 50))

    def test_the_rows_are_stable(self):
        with tempfile.TemporaryDirectory() as tmp:
            Path(tmp, 'census.txt').write_text(self.CENSUS)
            out1, out2 = Path(tmp, 'a.jsonl'), Path(tmp, 'b.jsonl')
            with contextlib.redirect_stdout(io.StringIO()):
                summary.main(['--out', str(out1), f't={tmp}', f'missing={tmp}/none'])
                summary.main(['--out', str(out2), f't={tmp}', f'missing={tmp}/none'])
            self.assertEqual(out1.read_text(), out2.read_text())
            rows = [json.loads(line) for line in out1.read_text().splitlines()]
            self.assertEqual(rows[1], {'title': 'missing', 'missing': True})


if __name__ == '__main__':
    unittest.main()
