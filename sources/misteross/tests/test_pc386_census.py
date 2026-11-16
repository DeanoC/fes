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
        // machine_tb's RTL STOP and GAP lines and its retire path read an
        // instruction's bytes through the same walk (#1707): 0F FF at linear
        // 0x00400ff8 is on physical 0x30ff8, not 0x400ff8, and the bytes
        // past the page, which is not present, read FFh.
        const pc386_census::PhysicalByte bus = [](uint32_t at) { return at < ram.size() ? ram[at] : uint8_t(0xff); };
        put(0x30ff8, {0x0f, 0xff, 0x90});
        uint8_t bytes[15];
        pc386_census::fetch(s.cr0, s.cr3, 0x00400ff8, bus, bytes);
        assert(bytes[0] == 0x0f && bytes[1] == 0xff && bytes[2] == 0x90 && bytes[7] == 0 && bytes[8] == 0xff);
        assert(pc386_census::stem(bytes, 2) == "0FFF");
        // Without paging the bytes are at the linear address, across pages.
        ram[0x31000] = 0x42;
        pc386_census::fetch(s.cr0 & ~0x80000000u, s.cr3, 0x30ff8, bus, bytes);
        assert(bytes[0] == 0x0f && bytes[1] == 0xff && bytes[8] == 0x42);
        // A walk that crosses into a present page reads it.
        put32(0x11000 + 1 * 4, 0x32000 | 7);
        ram[0x32000] = 0x37;
        pc386_census::fetch(s.cr0, s.cr3, 0x00400ff8, bus, bytes);
        assert(bytes[1] == 0xff && bytes[8] == 0x37);
        put32(0x11000 + 1 * 4, 0);
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

    // The x87 game census keys (the 15 November record's method): the control
    // word per form, by second and on a write, the significand bucket of
    // ST(0), and arithmetic by precision and code page.
    {
        Census c([](uint32_t at) { return at < ram.size() ? ram[at] : uint8_t(0xff); });
        auto x87 = [](uint16_t control, uint16_t status, uint64_t significand) {
            X87State st;
            st.control = control;
            st.status = status;  // TOP in bits 13-11
            st.r[(status >> 11) & 7].significand = significand;
            return st;
        };
        // FMUL m32 (D8 /1) at PC=24, round to nearest, all masked (003Fh),
        // leaving a 24-bit significand: bit 40 set, nothing below it.
        put(0x200, {0xd8, 0x0d, 0, 0, 0, 0});
        State s0 = pm(0x200, 3);
        s0.seg[SEG_CS].selector = 0x1b;
        c.begin(s0, "site");
        c.x87(s0, s0, retired(), x87(0x003f, 0x0000, uint64_t(1) << 40), x87(0x003f, 0x0000, uint64_t(1) << 40), 7.5);
        assert(c.counts.at("x87op:d8.m1:pm3") == 1);
        assert(c.counts.at("x87cwform:d8.m1:003f") == 1);
        assert(c.counts.at("x87cwsec:7:003f") == 1);
        assert(c.counts.at("x87sig:d8.m1:le24") == 1);
        assert(c.counts.at("x87cw:mul:003f") == 1);
        assert(c.counts.at("x87pcsite:0:001b:00000") == 1);
        // FILD m32 (DB /0) under the same control word, leaving a significand
        // with bit 0 set: a 64-bit significand, which precision control does
        // not touch. This is the case that makes a PC=24 title insufficient
        // for a 24-bit datapath.
        put(0x210, {0xdb, 0x05, 0, 0, 0, 0});
        State s1 = pm(0x210, 3);
        s1.seg[SEG_CS].selector = 0x1b;
        c.begin(s1, "site");
        c.x87(s1, s1, retired(), x87(0x003f, 0x0000, 1), x87(0x003f, 0x0000, 1), 7.9);
        assert(c.counts.at("x87sig:db.m0:le64") == 1);
        assert(c.counts.at("x87cwsec:7:003f") == 2);
        // No arithmetic site key for a load.
        assert(c.counts.count("x87pcsite:0:001b:00000") == 1 && c.counts.at("x87pcsite:0:001b:00000") == 1);
        // A zero significand is its own bucket, not le24.
        put(0x218, {0xd9, 0xee});  // FLDZ
        State s2 = pm(0x218, 3);
        c.begin(s2, "site");
        c.x87(s2, s2, retired(), x87(0x003f, 0x0000, 0), x87(0x003f, 0x0000, 0), 8.0);
        assert(c.counts.at("x87sig:d9ee:zero") == 1);
        // FLDCW (D9 /5): a write is counted even when it changes nothing.
        put(0x220, {0xd9, 0x2d, 0, 0, 0, 0});
        State s3 = pm(0x220, 3);
        c.begin(s3, "site");
        c.x87(s3, s3, retired(), x87(0x003f, 0x0000, 0), x87(0x003f, 0x0000, 0), 9.0);
        assert(c.counts.at("x87cwset:d9.m5:003f>003f") == 1);
        // FLDCW to PC=64 round-to-zero (0F3Fh): the old value is the one the
        // instruction itself ran under.
        c.begin(s3, "site");
        c.x87(s3, s3, retired(), x87(0x003f, 0x0000, 0), x87(0x0f3f, 0x0000, 0), 9.1);
        assert(c.counts.at("x87cwset:d9.m5:003f>0f3f") == 1);
        // FSTP m80 (DB /7): the explicit extended store.
        put(0x230, {0xdb, 0x3d, 0, 0, 0, 0});
        State s4 = pm(0x230, 3);
        c.begin(s4, "site");
        c.x87(s4, s4, retired(), x87(0x003f, 0x0000, uint64_t(1) << 63), x87(0x003f, 0x0000, uint64_t(1) << 63), 9.2);
        assert(c.counts.at("x87op:db.m7:pm3") == 1);
        // A popping store (FSTP m32, D9 /3) takes its value from the state
        // *before* the step: after it, ST(0) is the register underneath. Here
        // the stored value has a 64-bit significand and what is left behind
        // has a 24-bit one, so reading the wrong side would report le24.
        put(0x238, {0xd9, 0x1d, 0, 0, 0, 0});
        State sp = pm(0x238, 3);
        c.begin(sp, "site");
        c.x87(sp, sp, retired(), x87(0x003f, 0x0000, 3), x87(0x003f, 1 << 11, uint64_t(1) << 40), 9.25);
        assert(c.counts.at("x87sig:d9.m3:le64") == 1);
        assert(c.counts.count("x87sig:d9.m3:le24") == 0);
        // An empty slot is its own bucket, not a width: a tag of 3 is empty,
        // and its contents are stale.
        put(0x240, {0xd9, 0x15, 0, 0, 0, 0});  // FST m32
        State se = pm(0x240, 3);
        {
            X87State st;
            st.control = 0x003f;
            st.tag = 0xffff;  // every slot empty
            st.r[0].significand = ~uint64_t(0);
            c.begin(se, "site");
            c.x87(se, se, retired(), st, st, 9.26);
        }
        assert(c.counts.at("x87sig:d9.m2:empty") == 1);
        // DD /3 is FSTP ST(i): a popping store in REGISTER form, so its value
        // is also only in the state before the step. Reading after the pop
        // was a measured bug (the review's finding): here the stored value is
        // wide and what is left behind is narrow.
        put(0x244, {0xdd, 0xdb});  // FSTP ST(3)
        State sr = pm(0x244, 3);
        c.begin(sr, "site");
        c.x87(sr, sr, retired(), x87(0x003f, 0x0000, 3), x87(0x003f, 1 << 11, uint64_t(1) << 40), 9.27);
        assert(c.counts.at("x87sig:dd.r3:le64") == 1);
        assert(c.counts.count("x87sig:dd.r3:le24") == 0);
        // FST ST(i) (DD /2) does not pop, but is read from the same side.
        put(0x248, {0xdd, 0xd3});
        State sr2 = pm(0x248, 3);
        c.begin(sr2, "site");
        c.x87(sr2, sr2, retired(), x87(0x003f, 0x0000, 3), x87(0x003f, 0x0000, 3), 9.28);
        assert(c.counts.at("x87sig:dd.r2:le64") == 1);
        // IRQ 13 delivery is counted in every mode, including real mode, and
        // notes whether ES was already set. A step that only delivers an
        // interrupt counts no form.
        {
            State ri;  // real mode
            ri.eip = 0x300;
            put(0x300, {0x90});
            StepResult irq = with(EventKind::ExternalInterrupt, 0x75, 0, false);
            X87State es;
            es.control = 0x003f;
            es.status = 0x0080;  // ES set
            c.begin(ri, "site");
            c.x87(ri, ri, irq, es, es, 9.29);
            assert(c.counts.at("x87irq13:75:rm:es") == 1);
            assert(c.counts.at("x87errintr:75:rm") == 1);
            // The slave PIC's vector base is programmable, so a delivery with
            // an error pending is counted whatever its vector. That key is a
            // SUPERSET -- an unrelated interrupt with ES set lands in it too --
            // and reliance is read from it because it cannot miss a delivery.
            StepResult other = with(EventKind::ExternalInterrupt, 0x08, 0, false);
            c.begin(ri, "site");
            c.x87(ri, ri, other, es, es, 9.30);
            assert(c.counts.at("x87errintr:08:rm") == 1);
            assert(c.counts.count("x87irq13:08:rm:es") == 0);
            // An NMI with an error pending is NOT counted: an NMI cannot be
            // IRQ 13, and the superset is meant to over-report maskable
            // interrupts, not every event.
            c.begin(ri, "site");
            c.x87(ri, ri, with(EventKind::Nmi, 0x02, 0, false), es, es, 9.305);
            assert(c.counts.count("x87errintr:02:rm") == 0);
            // With no error pending, only the BIOS-default vector is counted.
            X87State clear;
            clear.control = 0x003f;
            c.begin(ri, "site");
            c.x87(ri, ri, with(EventKind::ExternalInterrupt, 0x08, 0, false), clear, clear, 9.31);
            assert(c.counts.count("x87errintr:08:rm") == 1);  // unchanged
        }
        // FDIV m64 (DC /6) at PC=64 on another page: the site key separates them.
        put(0x1240, {0xdc, 0x35, 0, 0, 0, 0});
        State s5 = pm(0x1240, 3);
        s5.seg[SEG_CS].selector = 0x1b;
        c.begin(s5, "site");
        c.x87(s5, s5, retired(), x87(0x0f3f, 0x0000, 3), x87(0x0f3f, 0x0000, 3), 9.3);
        assert(c.counts.at("x87pcsite:3:001b:00001") == 1);
        assert(c.counts.at("x87cw:div:0f3f") == 1);
        // ST(0) follows TOP: with TOP = 3 the bucket is r[3]'s.
        put(0x250, {0xd8, 0xc1});  // FADD ST, ST(1)
        State s6 = pm(0x250, 3);
        c.begin(s6, "site");
        c.x87(s6, s6, retired(), x87(0x003f, 3 << 11, 0), x87(0x003f, 3 << 11, uint64_t(1) << 30), 9.4);
        assert(c.counts.at("x87sig:d8.r0:le53") == 1);
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

    X87_CENSUS = '\n'.join([
        'x87op:d8.m1:pm3 100 001b:00001000', 'x87op:d9.m0:pm3 40 001b:00001010',
        'x87op:dd.m3:pm3 10 001b:00001020', 'x87op:db.m0:pm3 50 001b:00001030',
        'x87op:d9.r1:pm3 30 001b:00001040', 'x87op:dbe3:rm 2 0000:00000100',
        'x87cwform:d8.m1:003f 90 001b:00001000', 'x87cwform:d8.m1:0f3f 10 001b:00001000',
        'x87cwform:d9.m0:003f 40 001b:00001010', 'x87cwform:db.m0:003f 50 001b:00001030',
        'x87cwsec:0:003f 10 -', 'x87cwsec:1:0f3f 20 -',
        'x87cwset:d9.m5:037f>003f 1 001b:00001050', 'x87cwset:dbe3:ffff>037f 2 0000:00000100',
        'x87sig:d8.m1:le24 95 001b:00001000', 'x87sig:d8.m1:le53 5 001b:00001000',
        'x87sig:db.m0:le24 50 001b:00001030',
        'x87pcsite:0:001b:00001 90 001b:00001000', 'x87pcsite:3:001b:00002 10 001b:00002000',
        'x87es:d8.m1:pm3:ne=1 1 001b:00001000',
        'x87exc:d8.m1:raised=20:unmasked=00 3 001b:00001000',
        'x87wait:pm3 7 001b:00001060']) + '\n'

    def test_the_x87_row(self):
        """The census keys this record added, and the committed verdict."""
        with tempfile.TemporaryDirectory() as tmp:
            Path(tmp, 'census.txt').write_text(self.X87_CENSUS)
            row = summary.run('t', Path(tmp))['x87']
        self.assertEqual(row['retired'], 232)
        self.assertEqual(row['by_class'], {'arith': 100, 'control': 2, 'fxch': 30, 'load': 90, 'store': 10})
        # The operand widths come from the form, not from new instrumentation.
        self.assertEqual(row['by_operand'], {'int32': 50, 'real32': 140, 'real64': 10})
        self.assertEqual(row['unknown_forms'], {})
        # Precision and rounding control over the arithmetic forms only.
        self.assertEqual(row['pc_arith'], {'24': 90, '64': 10})
        self.assertEqual(row['rc_arith'], {'nearest': 90, 'zero': 10})
        self.assertEqual(row['pc_all'], {'24': 180, '64': 10})
        self.assertEqual(row['cw_by_second'], {'0': ['pc24/nearest'], '1': ['pc64/zero']})
        self.assertEqual(row['cw_writes'], {'d9.m5:037f>003f': 1, 'dbe3:ffff>037f': 2})
        self.assertEqual(row['pc_sites'], {'24': {'001b:00001': 90}, '64': {'001b:00002': 10}})
        self.assertEqual(row['exception_masks'], {'3f': 190})
        self.assertEqual(row['es_set'], {'d8.m1:pm3:ne=1': 1})
        self.assertEqual(row['fwait'], 7)
        # Two independent reasons, each named with its count.
        self.assertFalse(row['pc24_sufficient'])
        self.assertEqual(row['pc24_insufficient_because'],
                         ['10 arithmetic ESCs at PC=64', '5 steps left ST(0) with a significand in le53'])

    def test_pc24_is_not_sufficient_on_a_wide_load_alone(self):
        """The counterexample: every arithmetic ESC at PC=24, and still not
        sufficient, because a load put a 64-bit significand in a register.
        Precision control does not act on loads."""
        census = ('x87op:d8.m1:pm3 100 -\nx87op:db.m0:pm3 50 -\n'
                  'x87cwform:d8.m1:003f 100 -\nx87cwform:db.m0:003f 50 -\n'
                  'x87sig:d8.m1:le24 100 -\nx87sig:db.m0:le64 50 -\n')
        with tempfile.TemporaryDirectory() as tmp:
            Path(tmp, 'census.txt').write_text(census)
            row = summary.run('t', Path(tmp))['x87']
        self.assertEqual(row['pc_arith'], {'24': 100})
        self.assertFalse(row['pc24_sufficient'])
        self.assertEqual(row['pc24_insufficient_because'],
                         ['50 steps left ST(0) with a significand in le64'])
        # The same run with that load narrow is sufficient, so the verdict is
        # not simply always false.
        with tempfile.TemporaryDirectory() as tmp:
            Path(tmp, 'census.txt').write_text(census.replace('db.m0:le64', 'db.m0:le24'))
            row = summary.run('t', Path(tmp))['x87']
        self.assertTrue(row['pc24_sufficient'])

    def test_a_probe_only_title_has_no_verdict(self):
        """A title that only probes computes nothing, so sufficiency does not
        apply to it; this is the DOOM row's shape."""
        census = ('x87op:dbe3:rm 1 -\nx87op:dfe0:rm 4 -\nx87op:d9.m7:rm 3 -\n'
                  'x87cwform:dbe3:037f 1 -\nx87sig:dbe3:zero 1 -\n')
        with tempfile.TemporaryDirectory() as tmp:
            Path(tmp, 'census.txt').write_text(census)
            row = summary.run('t', Path(tmp))['x87']
        self.assertTrue(row['probe_only'])
        self.assertIsNone(row['pc24_sufficient'])
        self.assertEqual(row['by_class'], {'control': 8})

    def test_the_infinity_test_is_probe_only(self):
        """The 287/387 infinity test (FLD1, FLDZ, FDIVP, FCHS, FCOMPP) is
        register-only, so a title that runs it and the detection probe and
        nothing else is probe-only, even though FDIVP classes as arithmetic.
        This is the shape the prior census found for DOOM and seven others."""
        census = ('x87op:dbe3:rm 1 -\nx87op:dfe0:rm 4 -\nx87op:d9.m7:rm 3 -\n'
                  'x87op:d9e8:rm 1 -\nx87op:d9ee:rm 1 -\nx87op:de.r7:rm 1 -\n'
                  'x87op:d9e0:rm 1 -\nx87op:ded9:rm 1 -\n'
                  'x87cw:div:037f 1 -\n')
        with tempfile.TemporaryDirectory() as tmp:
            Path(tmp, 'census.txt').write_text(census)
            row = summary.run('t', Path(tmp))['x87']
        self.assertEqual(row['memory_fp_ops'], 0)
        self.assertTrue(row['probe_only'])
        self.assertIsNone(row['pc24_sufficient'])
        # One FLD m32 is enough to make it a computing title.
        with tempfile.TemporaryDirectory() as tmp:
            Path(tmp, 'census.txt').write_text(census + 'x87op:d9.m0:rm 1 -\n')
            row = summary.run('t', Path(tmp))['x87']
        self.assertEqual(row['memory_fp_ops'], 1)
        self.assertFalse(row['probe_only'])

    def test_frndint_is_not_in_the_precision_distribution(self):
        """FRNDINT (d9fc) rounds under rounding control and ignores precision
        control, so it must not appear in pc_arith. Counting it there was a
        measured error: it put Quake's PC=64 arithmetic at 483,513 where the
        add/multiply/divide/square-root total is 462,063, the difference being
        exactly its 25,400 executions."""
        census = ('x87op:d8.m1:pm3 100 -\nx87op:d9fc:pm3 7 -\n'
                  'x87cwform:d8.m1:003f 100 -\nx87cwform:d9fc:0f3f 7 -\n')
        with tempfile.TemporaryDirectory() as tmp:
            Path(tmp, 'census.txt').write_text(census)
            row = summary.run('t', Path(tmp))['x87']
        # Counted in the mix, under its own class.
        self.assertEqual(row['by_class'], {'arith': 100, 'round': 7})
        # Absent from the precision distribution, which is add/sub/mul/div/sqrt.
        self.assertEqual(row['pc_arith'], {'24': 100})
        # And so it cannot by itself make a title insufficient.
        self.assertTrue(row['pc24_sufficient'])

    def test_dd_register_forms_are_stores_and_compares(self):
        """DD /2 and /3 are FST and FSTP ST(i), and DD /4 and /5 are FUCOM and
        FUCOMP ST(i). Classing the whole DD register group as `simple`
        understated Quake's store share by about 1.9 points and dropped
        thousands of compares."""
        census = ('x87op:dd.r0:pm3 1 -\nx87op:dd.r2:pm3 7 -\nx87op:dd.r3:pm3 615685 -\n'
                  'x87op:dde0:pm3 11 -\nx87op:ddea:pm3 13 -\n')
        with tempfile.TemporaryDirectory() as tmp:
            Path(tmp, 'census.txt').write_text(census)
            row = summary.run('t', Path(tmp))['x87']
        self.assertEqual(row['by_class'], {'compare': 24, 'simple': 1, 'store': 615692})
        # They move no memory operand, so they do not make a title computing
        # by the memory-operand rule, but FXCH and transcendentals aside the
        # mix now shows them where they belong.
        self.assertEqual(row['memory_fp_ops'], 0)

    def test_frndint_pc_comes_from_frndint_rows(self):
        """The FRNDINT precision column must come from FRNDINT's own rows. Taken
        from the census's `other` class it reported 19.6M operations for Quake
        beside an actual FRNDINT count of 25,400, because `other` holds every
        load, store, compare and control form."""
        census = ('x87op:d9fc:pm3 7 -\nx87op:d9.m0:pm3 500 -\n'
                  'x87cwform:d9fc:003f 4 -\nx87cwform:d9fc:0f3f 3 -\n'
                  'x87cw:other:0f3f 500 -\n')
        with tempfile.TemporaryDirectory() as tmp:
            Path(tmp, 'census.txt').write_text(census)
            row = summary.run('t', Path(tmp))['x87']
        self.assertEqual(row['frndint'], 7)
        self.assertEqual(row['frndint_by_pc'], {'24': 4, '64': 3})
        # Not the `other` class's 500.
        self.assertEqual(row['pc_by_class']['other'], {'64': 500})

    def test_exception_reliance_counts_irq13_directly(self):
        """The committed rule is ES set *or* IRQ 13 delivered. A title that
        restores state with ES already set takes the interrupt without a fresh
        ES transition, so the delivery is counted from its own key."""
        with tempfile.TemporaryDirectory() as tmp:
            Path(tmp, 'census.txt').write_text('x87op:d8.m1:pm3 5 -\n')
            row = summary.run('t', Path(tmp))['x87']
        self.assertEqual(row['irq13'], 0)
        self.assertFalse(row['relies_on_exceptions'])
        with tempfile.TemporaryDirectory() as tmp:
            Path(tmp, 'census.txt').write_text('x87op:d8.m1:pm3 5 -\nx87irq13:75:pm3:es 2 -\n')
            row = summary.run('t', Path(tmp))['x87']
        self.assertEqual(row['irq13'], 2)
        self.assertTrue(row['relies_on_exceptions'])
        # The vector-independent superset also establishes reliance, because
        # the slave PIC's base is programmable and IRQ 13 need not be 75h.
        with tempfile.TemporaryDirectory() as tmp:
            Path(tmp, 'census.txt').write_text('x87op:d8.m1:pm3 5 -\nx87errintr:51:pm0 1 -\n')
            row = summary.run('t', Path(tmp))['x87']
        self.assertEqual(row['irq13'], 0)
        self.assertEqual(row['interrupts_with_error_pending'], 1)
        self.assertTrue(row['relies_on_exceptions'])

    @staticmethod
    def census_form(op: int, modrm: int) -> str:
        """census.h's form name for an ESC, reimplemented from the header's
        rule so the inventory can be checked against what it can emit."""
        reg = (modrm >> 3) & 7
        form = f'{op:02x}'
        if (modrm >> 6) != 3:
            return form + f'.m{reg}'
        if ((op == 0xd9 and reg >= 2) or (op == 0xdb and reg == 4) or (op == 0xde and reg == 3)
                or (op == 0xdf and reg == 4) or (op == 0xda and reg == 5) or (op == 0xdd and reg >= 4)):
            return form + f'{modrm:02x}'
        return form + f'.r{reg}'

    def test_the_form_inventory_covers_everything_the_census_can_emit(self):
        """Every form census.h can emit must be in X87_FORMS, so nothing is
        counted as `unknown` and nothing is silently missing from the
        generated never-executed list.

        This check is the one that should have existed from the start: three
        separate review rounds found gaps in this inventory by inspection --
        the m16 integer arithmetic forms (`de.m0`-`de.m7`) and FNOP
        (`d9d0`) -- and each time a hand-maintained table was the cause.
        """
        missing = sorted({self.census_form(op, modrm)
                          for op in range(0xd8, 0xe0) for modrm in range(0x100)} - set(summary.X87_FORMS))
        self.assertEqual(missing, [], f'forms the census can emit but the inventory lacks: {missing}')

    def test_fnop_is_in_the_inventory(self):
        """D9 D0 is FNOP, a valid 387 instruction the census emits as `d9d0`.
        Its absence kept it out of the generated never-executed list."""
        self.assertEqual(summary.X87_FORMS['d9d0'], ('simple', 'none'))
        self.assertEqual(summary.x87_name('d9d0'), 'FNOP (`d9d0`)')

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
