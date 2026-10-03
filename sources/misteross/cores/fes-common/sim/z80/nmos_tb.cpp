// SPDX-License-Identifier: GPL-2.0-or-later
// Original pin-level programs and timing expectations from Zilog UM0080:
// https://www.zilog.com/docs/z80/um0080.pdf
// No CPU or emulator implementation is used as an execution oracle.
#include "Vfes_z80_nmos.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <functional>
#include <initializer_list>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

static unsigned assertions = 0;
static std::string context;
static void require(bool ok, const std::string& message) {
    ++assertions;
    if (!ok) throw std::runtime_error(context + ": " + message);
}
static void equal(unsigned actual, unsigned expected, const std::string& message) {
    require(actual == expected, message + " got " + std::to_string(actual) +
            ", expected " + std::to_string(expected));
}

struct Refresh { uint16_t address; uint64_t time; };
struct Retirement { uint16_t pc; uint64_t time; };
class Machine {
public:
    Vfes_z80_nmos dut;
    std::array<uint8_t,65536> memory{};
    std::array<uint8_t,65536> ports{};
    std::vector<Refresh> refreshes;
    std::vector<Retirement> retirements;
    uint64_t time = 0, m1_start = 0, irq_start = 0, nmi_start = 0;
    uint8_t vector = 0xff;
    unsigned irq_count = 0, memory_writes = 0, port_writes = 0;
    bool old_wr = true, old_refresh = true, old_m1 = true, old_iorq = true;
    bool old_halt = true;

    Machine() {
        dut.clk = 0; dut.ce_p = 0; dut.ce_n = 0;
        dut.wait_n = 1; dut.int_n = 1; dut.nmi_n = 1; dut.busrq_n = 1;
        dut.din = 0; dut.reset = 1;
        for (unsigned n = 0; n < 3; ++n) half(false,false);
        dut.reset = 0;
        settle();
        observe();
    }
    void put(std::initializer_list<uint8_t> bytes, uint16_t address = 0) {
        for (uint8_t byte : bytes) memory[address++] = byte;
        settle();
    }
    void put(const std::vector<uint8_t>& bytes, uint16_t address = 0) {
        for (uint8_t byte : bytes) memory[address++] = byte;
        settle();
    }
    void settle() {
        dut.eval();
        dut.din = !dut.iorq_n && !dut.m1_n ? vector :
                  !dut.iorq_n && !dut.rd_n ? ports[dut.a] : memory[dut.a];
        dut.eval();
    }
    void observe() {
        if (!dut.reset) require(!dut.illegal, "unexpected illegal opcode");
        if (!dut.m1_n && old_m1) m1_start = time;
        if (!dut.iorq_n && old_iorq && !dut.m1_n) {
            ++irq_count;
            irq_start = m1_start;
            require(dut.rd_n, "interrupt acknowledge must not assert RD");
        }
        if (!dut.rfsh_n && old_refresh) {
            refreshes.push_back({uint16_t(dut.a),time});
            require(dut.rd_n && dut.iorq_n, "refresh must not read an operand or port");
        }
        if (!dut.wr_n && old_wr) {
            require(dut.busak_n, "CPU cannot write while DMA owns the bus");
            if (!dut.mreq_n) {
                memory[dut.a] = dut.dout;
                ++memory_writes;
            } else if (!dut.iorq_n) {
                ports[dut.a] = dut.dout;
                ++port_writes;
            } else require(false, "WR requires memory or I/O selection");
        }
        if (dut.halt_n && !old_halt && !dut.m1_n) nmi_start = m1_start;
        old_wr = dut.wr_n; old_refresh = dut.rfsh_n;
        old_m1 = dut.m1_n; old_iorq = dut.iorq_n; old_halt = dut.halt_n;
    }
    void half(bool positive, bool negative) {
        dut.clk = 0; dut.ce_p = positive; dut.ce_n = negative;
        settle();
        dut.clk = 1;
        dut.eval();
        settle();
        observe();
        if (positive && dut.retired) retirements.push_back({uint16_t(dut.retire_pc),time});
        dut.clk = 0; dut.ce_p = 0; dut.ce_n = 0;
        settle();
    }
    void tick() {
        half(false,true);
        ++time;
        half(true,false);
    }
    void until(const std::function<bool()>& done, unsigned bound = 20000) {
        for (unsigned n = 0; !done() && n < bound; ++n) tick();
        require(done(), "execution timeout at PC=" + std::to_string(dut.debug_pc));
    }
    void step(unsigned count = 1) {
        const size_t target = retirements.size() + count;
        until([&]{ return retirements.size() >= target; });
        equal(retirements.size(), target, "retirement count");
    }
    void halt() { until([&]{ return !dut.halt_n; }); }
    bool inactive() const {
        return dut.mreq_n && dut.iorq_n && dut.rd_n && dut.wr_n;
    }
};

static void original_program() {
    context = "original mixed program";
    Machine m;
    m.put({0x31,0x00,0x90, 0x21,0x00,0x40, 0x01,0x03,0x00, 0x11,0x00,0x41,
           0x3e,0x81, 0x77, 0x23, 0x36,0x7f, 0x34, 0x2b, 0xed,0xb0,
           0xdd,0x21,0x00,0x42, 0xdd,0x36,0x01,0x80,
           0xdd,0xcb,0x01,0x06, 0xdd,0x7e,0x01, 0xd3,0x20, 0xdb,0x21,
           0xcd,0x00,0x01, 0x32,0x00,0x43, 0x76});
    m.put({0xee,0xff,0xc9},0x0100);
    m.memory[0x4002] = 0x55;
    m.ports[0x0121] = 0xa6;
    m.halt();
    equal(m.memory[0x4100],0x81,"block copy byte 0");
    equal(m.memory[0x4101],0x80,"block copy byte 1");
    equal(m.memory[0x4102],0x55,"block copy byte 2");
    equal(m.memory[0x4201],0x01,"indexed CB writeback");
    equal(m.ports[0x0120],0x01,"full 16-bit OUT address");
    equal(m.port_writes,1,"one external OUT transaction");
    equal(m.memory[0x4300],0x59,"subroutine result");
    equal(m.dut.debug_af,0x590c,"final accumulator and flags");
    equal(m.dut.debug_sp,0x9000,"CALL/RET stack balance");
    equal(m.dut.debug_hl,0x4003,"block source cursor");
    equal(m.dut.debug_de,0x4103,"block destination cursor");
    equal(m.dut.debug_bc,0,"block remaining count");
}

static void timings() {
    struct Example { const char *name; std::vector<uint8_t> bytes; unsigned ticks; };
    const std::vector<Example> cases = {
        {"NOP",{0x00},4}, {"LD B,n",{0x06,0x05},7},
        {"LD BC,nn",{0x01,0x34,0x12},10}, {"LD (HL),B",{0x70},7},
        {"LD A,(HL)",{0x7e},7}, {"INC (HL)",{0x34},11},
        {"DEC (HL)",{0x35},11}, {"ADD A,n",{0xc6,0x20},7},
        {"ADC A,B",{0x88},4}, {"ADD HL,BC",{0x09},11},
        {"INC BC",{0x03},6}, {"LD SP,HL",{0xf9},6},
        {"PUSH BC",{0xc5},11}, {"POP BC",{0xc1},10},
        {"RLC B",{0xcb,0x00},8}, {"BIT (HL)",{0xcb,0x46},12},
        {"RLC (HL)",{0xcb,0x06},15}, {"NEG",{0xed,0x44},8},
        {"ADC HL,BC",{0xed,0x4a},15}, {"RLD",{0xed,0x6f},18},
        {"LD A,I",{0xed,0x57},9}, {"LD (nn),BC",{0xed,0x43,0x00,0x44},20},
        {"LDI",{0xed,0xa0},16}, {"CPI",{0xed,0xa1},16},
        {"INI",{0xed,0xa2},16}, {"OUTI",{0xed,0xa3},16},
        {"IN A,(n)",{0xdb,0x20},11}, {"OUT (n),A",{0xd3,0x20},11},
        {"LD IX,nn",{0xdd,0x21,0x34,0x12},14},
        {"LD A,(IX+d)",{0xdd,0x7e,0x01},19},
        {"LD (IX+d),n",{0xdd,0x36,0x01,0x20},19},
        {"INC (IX+d)",{0xdd,0x34,0x01},23},
        {"BIT (IX+d)",{0xdd,0xcb,0x01,0x46},20},
        {"RLC (IX+d)",{0xdd,0xcb,0x01,0x06},23},
        {"RET Z untaken",{0xc8},5}, {"RET NZ taken",{0xc0},11},
        {"RET",{0xc9},10}, {"CALL",{0xcd,0x00,0x01},17},
        {"JP",{0xc3,0x00,0x01},10}, {"JR taken",{0x18,0x02},12},
        {"JR Z untaken",{0x28,0x02},7}, {"DJNZ taken",{0x10,0x02},13},
        {"RST",{0xff},11}, {"EX (SP),HL",{0xe3},19},
        {"HALT",{0x76},4}
    };
    for (const auto& c : cases) {
        context = std::string("T states: ") + c.name;
        Machine m;
        const std::vector<uint8_t> setup = {0x31,0x00,0x90,0x21,0x00,0x40,
            0x01,0x02,0x02,0x11,0x00,0x41,0x3e,0x10,
            0xdd,0x21,0x00,0x40,0xfd,0x21,0x00,0x41};
        m.put(setup);
        m.put(c.bytes, setup.size());
        m.memory[0x4000] = 0x81; m.memory[0x4001] = 0x19;
        m.memory[0x9000] = 0; m.memory[0x9001] = 1;
        m.step(7);
        const uint64_t begin = m.time;
        m.step();
        equal(m.time-begin,c.ticks,"instruction duration");
    }
    // A repeated block retires each interruptible iteration. Its final pass is
    // shorter by five T states; interrupt recognition must use that boundary.
    for (uint8_t op : {uint8_t(0xb0),uint8_t(0xb1),uint8_t(0xb2),uint8_t(0xb3)}) {
        context = "repeated block iteration timing";
        Machine m;
        m.put({0x21,0x00,0x40,0x11,0x00,0x41,0x01,0x02,0x02,0x3e,0x10,0xed,op});
        // CP/LD decrement BC; IO decrements just B. Start with two iterations.
        if (op == 0xb0 || op == 0xb1) m.memory[8] = 0;
        m.memory[0x4000]=0x81; m.memory[0x4001]=0x22;
        m.step(4);
        const auto begin=m.time;
        m.step(); equal(m.time-begin,21,"repeated first iteration");
        const auto final=m.time;
        m.step(); equal(m.time-final,16,"repeated final iteration");
    }
}

static void refresh_and_halt() {
    context = "refresh sequence and HALT cadence";
    Machine m;
    m.put({0x00,0x00,0x76,0x03}); // Halted fetch must ignore INC BC's extra T.
    m.halt();
    m.until([&]{ return m.refreshes.size() >= 8; });
    equal(m.dut.debug_pc,3,"HALT keeps the next-instruction PC");
    equal(m.dut.debug_bc,0,"HALT ignores fetched opcodes");
    for (unsigned i = 0; i < 8; ++i) {
        // The physical NMOS refresh trace presents the old R value; its
        // architectural low-seven-bit increment completes with the M1 cycle.
        equal(m.refreshes[i].address,i,"refresh presents pre-increment R");
        if (i != 0) equal(m.refreshes[i].time-m.refreshes[i-1].time,4,"HALT M1 cadence");
    }
    context = "refresh R bit 7 and I address";
    Machine high;
    high.put({0x3e,0x91,0xed,0x4f,0x3e,0x42,0xed,0x47,0x00,0x76});
    high.step(4);
    const auto start=high.refreshes.size();
    high.step(2);
    require(high.refreshes.size() >= start+2,"NOP and HALT each refresh once");
    equal(high.refreshes[start].address,0x4294,"refresh presents programmed pre-increment R");
    equal(high.dut.debug_ir,0x4296,"architectural R advances after both M1 cycles");
    equal(high.refreshes[start].address >> 8,0x42,"I supplies upper refresh address");
    require(high.refreshes[start].address & 0x80,"R bit 7 remains programmed");
    equal((high.refreshes[start+1].address-high.refreshes[start].address)&0x7f,1,
          "low seven refresh bits advance independently");
}

static void memory_wait_and_dma() {
    context = "operand WAIT through complete wrapper";
    Machine m;
    m.put({0x3a,0x00,0x40,0x76}); m.memory[0x4000]=0x5a;
    m.until([&]{ return !m.dut.mreq_n && !m.dut.rd_n && m.dut.m1_n && m.dut.a==0x4000; });
    const auto pc=m.dut.debug_pc;
    m.dut.wait_n=0;
    for (unsigned t=0;t<5;++t) {
        m.tick();
        require(!m.dut.mreq_n && !m.dut.rd_n && m.dut.a==0x4000,
                "WAIT preserves operand strobes and address");
        equal(m.dut.debug_pc,pc,"WAIT preserves PC");
        require(m.retirements.empty(),"WAIT cannot retire an instruction");
    }
    m.dut.wait_n=1; m.step();
    equal(m.time,18,"five waits extend thirteen-state load");
    equal(m.dut.debug_af>>8,0x5a,"WAIT release reads correct operand");

    context = "DMA through complete wrapper";
    Machine dma;
    dma.put({0x00,0xc3,0x00,0x00});
    dma.tick(); dma.dut.busrq_n=0;
    dma.until([&]{ return !dma.dut.busak_n; },20);
    require(dma.inactive(),"DMA grant releases every transfer strobe");
    const auto held_pc=dma.dut.debug_pc, held_ir=dma.dut.debug_ir;
    const auto retired=dma.retirements.size(), refreshed=dma.refreshes.size();
    for (unsigned t=0;t<12;++t) {
        dma.tick();
        require(!dma.dut.busak_n && dma.inactive(),"DMA holds bus controls inactive");
        equal(dma.dut.debug_pc,held_pc,"DMA freezes PC");
        equal(dma.dut.debug_ir,held_ir,"DMA freezes refresh counter");
    }
    equal(dma.retirements.size(),retired,"DMA cannot retire instructions");
    equal(dma.refreshes.size(),refreshed,"DMA cannot emit refresh cycles");
    dma.dut.busrq_n=1;
    dma.until([&]{ return dma.retirements.size()>retired; });
    require(dma.dut.busak_n,"execution resumes after DMA release");
}

static void interrupts() {
    context="IM1 HALT wake and return";
    Machine im1;
    im1.put({0x31,0x00,0x90,0xed,0x56,0xfb,0x76,0x00});
    im1.put({0x3e,0x77,0xed,0x4d},0x38);
    im1.halt();
    const auto return_pc=im1.dut.debug_pc;
    im1.dut.int_n=0;
    im1.until([&]{ return im1.dut.debug_pc==0x38 && im1.dut.debug_sp==0x8ffe; });
    equal(im1.time-im1.irq_start,13,"IM1 interrupt entry duration");
    equal(im1.irq_count,1,"one interrupt acknowledge");
    require(im1.dut.halt_n,"INT wakes HALT");
    equal(im1.memory[0x8ffe],return_pc&255,"IRQ stack return low byte");
    equal(im1.memory[0x8fff],return_pc>>8,"IRQ stack return high byte");
    im1.dut.int_n=1;
    im1.step(2);
    equal(im1.dut.debug_pc,return_pc,"RETI returns after HALT");
    equal(im1.dut.debug_af>>8,0x77,"ISR accumulator result");
    equal(im1.dut.debug_sp,0x9000,"ISR stack balance");

    context="IM2 vector entry duration";
    Machine im2;
    im2.put({0x31,0x00,0x90,0x3e,0x80,0xed,0x47,0xed,0x5e,0xfb,0x76});
    im2.memory[0x8020]=0x34; im2.memory[0x8021]=0x12;
    im2.vector=0x20;
    im2.halt(); im2.dut.int_n=0;
    im2.until([&]{ return im2.dut.debug_pc==0x1234 && im2.dut.debug_sp==0x8ffe; });
    equal(im2.time-im2.irq_start,19,"IM2 acknowledge, stack and vector duration");

    context="NMI HALT wake and RETN";
    Machine nmi;
    nmi.put({0x31,0x00,0x90,0xfb,0x76,0x03});
    nmi.put({0x3e,0x99,0xed,0x45},0x66);
    nmi.halt(); const auto nmi_return=nmi.dut.debug_pc;
    nmi.dut.nmi_n=0; nmi.tick(); nmi.dut.nmi_n=1;
    nmi.until([&]{ return nmi.dut.debug_pc==0x66 && nmi.dut.debug_sp==0x8ffe; });
    equal(nmi.time-nmi.nmi_start,11,"NMI acknowledge and stack duration");
    require(nmi.dut.halt_n,"NMI wakes HALT");
    equal(nmi.irq_count,0,"NMI must not perform IRQ IORQ acknowledge");
    nmi.step(2);
    equal(nmi.dut.debug_pc,nmi_return,"RETN returns after HALT");
    equal(nmi.dut.debug_sp,0x9000,"NMI stack balance");
    equal((nmi.dut.debug_iff>>1)&3,3,"RETN restores IFF1 from IFF2");

    // IM0 has two extra T states relative to the injected instruction. CALL
    // operands are ordinary memory reads; the injected opcode does not consume PC.
    for (uint8_t injected : {uint8_t(0x00),uint8_t(0xff),uint8_t(0xcd)}) {
        context="IM0 injected opcode timing";
        Machine im0;
        im0.put({0x31,0x00,0x90,0xfb,0x00,0x00,0x01,0x00});
        im0.vector=injected;
        im0.step(2); // EI's successor is still protected.
        im0.dut.int_n=0;
        im0.step();
        const auto begin=im0.time;
        im0.step();
        equal(im0.time-begin,injected==0 ? 6 : injected==0xff ? 13 : 19,
              "IM0 injected instruction adds exactly two T states");
        equal(im0.irq_count,1,"IM0 performs one acknowledge");
    }
}

static void dma_interrupt_priority() {
    for (bool cancelled : {false,true}) {
        context=cancelled ? "cancelled INT during DMA" : "sustained INT during DMA";
        Machine m;
        m.put({0x31,0x00,0x90,0xfb,0x00,0x76});
        m.step(2);
        m.dut.int_n=0; m.dut.busrq_n=0;
        m.until([&]{ return !m.dut.busak_n; });
        equal(m.irq_count,0,"DMA must precede IRQ acknowledge");
        require(m.dut.debug_iff&2,"DMA must not prematurely clear IFF1");
        if (cancelled) m.dut.int_n=1;
        for(unsigned t=0;t<5;++t) m.tick();
        m.dut.busrq_n=1;
        if (cancelled) {
            m.halt();
            equal(m.irq_count,0,"cancelled IRQ must not acknowledge after DMA");
            require(m.dut.debug_iff&2,"cancelled IRQ preserves IFF1");
        } else {
            m.until([&]{ return m.dut.debug_pc==0x38 && m.dut.debug_sp==0x8ffe; });
            equal(m.irq_count,1,"sustained IRQ must acknowledge once after DMA");
            equal(m.time-m.irq_start,13,"resumed IM0 RST interrupt duration");
        }
    }
    context="pending NMI takes priority after DMA";
    Machine nmi;
    nmi.put({0x31,0x00,0x90,0xfb,0x00,0x76});
    nmi.put({0xed,0x45},0x66);
    nmi.step(2); nmi.dut.int_n=0; nmi.dut.busrq_n=0;
    nmi.until([&]{ return !nmi.dut.busak_n; });
    nmi.dut.nmi_n=0; nmi.tick(); nmi.dut.nmi_n=1;
    nmi.tick(); nmi.tick();
    equal(nmi.irq_count,0,"DMA cannot recognize the lower-priority IRQ");
    nmi.dut.busrq_n=1;
    nmi.until([&]{ return nmi.dut.debug_pc==0x66 && nmi.dut.debug_sp==0x8ffe; });
    equal(nmi.irq_count,0,"NMI must precede sustained IRQ at DMA release");
    equal(nmi.memory[0x8ffe],5,"resumed NMI saves next-instruction PC");
    equal((nmi.dut.debug_iff>>1)&3,2,"NMI clears IFF1 and preserves IFF2");
    nmi.step(); // RETN restores IFF1 and admits the still-active IRQ.
    nmi.until([&]{ return nmi.dut.debug_pc==0x38 && nmi.dut.debug_sp==0x8ffe; });
    equal(nmi.irq_count,1,"sustained IRQ follows RETN restoring IFF1");

    context="DMA priority despite late BUSRQ release";
    Machine late;
    late.put({0x31,0x00,0x90,0xfb,0x00,0x76});
    late.step(2); late.dut.int_n=0;
    late.tick(); late.tick(); // NOP T2 and T3.
    late.dut.busrq_n=0; late.tick(); // Positive final T4 samples BUSRQ.
    late.dut.busrq_n=1; late.tick(); // Sample still forces DMA grant.
    require(!late.dut.busak_n && (late.dut.debug_iff&2),
            "sampled DMA must block IRQ even after raw BUSRQ is released");
    late.dut.int_n=1; late.halt();
    equal(late.irq_count,0,"IRQ cancelled before sampled DMA releases must be discarded");
}

static void injected_prefixes() {
    struct Example { uint8_t prefix; std::vector<uint8_t> bytes; unsigned ticks; };
    for (const auto& c : std::array<Example,4>{{
            {0xcb,{0x00},10}, {0xed,{0x44},10},
            {0xdd,{0x21,0x34,0x12},16}, {0xfd,{0x21,0x78,0x56},16}}}) {
        context="IM0 injected prefix " + std::to_string(c.prefix);
        Machine m;
        m.put({0x31,0x00,0x90,0xfb,0x00});
        m.put(c.bytes,5);
        m.vector=c.prefix;
        m.step(2); m.dut.int_n=0; m.step();
        const auto begin=m.time;
        m.step();
        equal(m.time-begin,c.ticks,"prefix acknowledge plus ordinary operand cycles");
        equal(m.irq_count,1,"prefix must not issue a second interrupt acknowledge");
        equal(m.dut.debug_pc,5+c.bytes.size(),"injected prefix does not advance PC");
        equal(m.retirements.back().pc,5,"injected instruction retirement address");
        if(c.prefix==0xdd) equal(m.dut.debug_ix,0x1234,"injected DD selects IX");
        if(c.prefix==0xfd) equal(m.dut.debug_iy,0x5678,"injected FD selects IY");
    }
}

static void halted_dma_priority() {
    for (unsigned event=0;event<3;++event) {
        context="HALT DMA interrupt priority " + std::to_string(event);
        Machine m;
        m.put({0x31,0x00,0x90,0xfb,0x76});
        m.put({0xed,0x45},0x66);
        m.halt();
        m.dut.int_n=0; m.dut.busrq_n=0;
        m.until([&]{ return !m.dut.busak_n; });
        require(!m.dut.halt_n,"DMA must retain HALT before an interrupt is admitted");
        require(m.dut.debug_iff&2,"HALTed DMA must preserve IFF1");
        equal(m.irq_count,0,"HALTed DMA must precede interrupt acknowledge");
        if(event==0) m.dut.int_n=1;
        if(event==2) {
            m.dut.nmi_n=0; m.tick(); m.dut.nmi_n=1;
        }
        for(unsigned t=0;t<5;++t) {
            m.tick();
            require(!m.dut.halt_n && !m.dut.busak_n,"DMA holds HALT until bus release");
        }
        m.dut.busrq_n=1;
        if(event==0) {
            for(unsigned t=0;t<12;++t) m.tick();
            require(!m.dut.halt_n && (m.dut.debug_iff&2),
                    "cancelled INT leaves resumed CPU HALTed and enabled");
            equal(m.irq_count,0,"cancelled HALT interrupt cannot acknowledge");
        } else if(event==1) {
            m.until([&]{ return m.dut.debug_pc==0x38 && m.dut.debug_sp==0x8ffe; });
            equal(m.irq_count,1,"sustained HALT interrupt acknowledges after DMA");
            require(m.dut.halt_n,"accepted IRQ releases HALT");
        } else {
            m.until([&]{ return m.dut.debug_pc==0x66 && m.dut.debug_sp==0x8ffe; });
            equal(m.irq_count,0,"pending NMI precedes INT after HALTed DMA");
            equal(m.memory[0x8ffe],5,"HALT NMI saves next-instruction PC");
            m.step();
            m.until([&]{ return m.dut.debug_pc==0x38 && m.dut.debug_sp==0x8ffe; });
            equal(m.irq_count,1,"RETN admits sustained IRQ after HALTed DMA");
        }
    }
}

static void nmi_during_ei() {
    // David Banks's physical-device analysis identifies IFF2=0 in this case
    // as a transistor simulator fidelity failure, not NMOS CPU behavior:
    // https://github.com/hoglet67/Z80Decoder/wiki/NMI-during-EI-Anomaly
    context="NMI during EI preserves enabled IFF2";
    Machine m;
    m.put({0x31,0x00,0x90,0xfb,0x00,0x76});
    m.put({0xed,0x45},0x66);
    m.step();
    m.dut.nmi_n=0; m.dut.int_n=0; m.tick(); m.dut.nmi_n=1;
    m.step();
    equal(m.dut.debug_iff&6,4,"EI plus NMI clears IFF1 and sets IFF2");
    m.until([&]{ return m.dut.debug_pc==0x66 && m.dut.debug_sp==0x8ffe; });
    equal(m.irq_count,0,"NMI during EI takes priority over INT");
    equal(m.memory[0x8ffe],4,"NMI returns to EI successor");
    m.step();
    m.until([&]{ return m.dut.debug_pc==0x38 && m.dut.debug_sp==0x8ffe; });
    equal(m.irq_count,1,"RETN restores EI enabling and admits INT");
}

static void internal_machine_boundaries() {
    context="ADD HL grants DMA between its two internal machine cycles";
    Machine add;
    add.put({0x21,0x00,0x40,0x01,0x02,0x00,0x09,0x76});
    add.step(2);
    const auto begin=add.time, retired=add.retirements.size();
    for(unsigned t=0;t<4;++t) add.tick();
    add.dut.busrq_n=0;
    add.until([&]{ return !add.dut.busak_n; });
    equal(add.time-begin,8,"ADD HL fetch4T plus first internal4T boundary");
    equal(add.retirements.size(),retired,"ADD HL cannot retire before internal3T tail");
    add.dut.busrq_n=1; add.tick();
    equal(add.dut.a,2,"ADD HL internal cycle retains the fetch refresh address");
    require(add.inactive(),"arithmetic internal cycle emits no memory strobe");
    add.step();
    equal(add.dut.debug_hl,0x4002,"arithmetic result survives intervening DMA");

    context="indexed displacement grants DMA before five internal T states";
    Machine indexed;
    indexed.put({0xdd,0x21,0x00,0x40,0xdd,0x7e,0x03,0x76});
    indexed.memory[0x4003]=0x5a;
    indexed.step();
    const auto indexed_begin=indexed.time, indexed_retired=indexed.retirements.size();
    for(unsigned t=0;t<8;++t) indexed.tick();
    indexed.dut.busrq_n=0;
    indexed.until([&]{ return !indexed.dut.busak_n; });
    equal(indexed.time-indexed_begin,11,"indexed fetch4T+4T then displacement3T boundary");
    equal(indexed.retirements.size(),indexed_retired,"indexed load is incomplete at displacement boundary");
    require(indexed.inactive(),"DMA precedes indexed address-calculation internal cycle");
    indexed.dut.busrq_n=1;
    indexed.tick(); // DMA release precedes internal-cycle T1.
    for(unsigned t=0;t<5;++t) {
        require(indexed.inactive(),"indexed internal5T cannot read the operand early");
        equal(indexed.dut.a,6,"indexed internal cycle retains displacement operand address");
        indexed.tick();
    }
    indexed.step();
    equal(indexed.dut.debug_af>>8,0x5a,"indexed load survives displacement-boundary DMA");

    for(uint8_t op : {uint8_t(0x18),uint8_t(0x10)}) {
        context=op==0x18 ? "JR displacement machine boundary" : "DJNZ displacement machine boundary";
        Machine branch;
        branch.put({0x06,0x02,op,0x02,0x00,0x00,0x76});
        branch.step();
        const auto branch_begin=branch.time, branch_retired=branch.retirements.size();
        for(unsigned t=0;t<(op==0x18 ? 4u : 5u);++t) branch.tick();
        branch.dut.busrq_n=0;
        branch.until([&]{ return !branch.dut.busak_n; });
        equal(branch.time-branch_begin,op==0x18 ? 7 : 8,
              "taken branch grants DMA after displacement3T");
        equal(branch.retirements.size(),branch_retired,
              "taken branch cannot retire before internal5T");
        branch.dut.busrq_n=1; branch.tick();
        equal(branch.dut.a,3,"branch internal cycle retains displacement operand address");
        branch.step();
        equal(branch.dut.debug_pc,6,"taken branch target survives intervening DMA");
        if(op==0x10) equal(branch.dut.debug_bc>>8,1,"DJNZ decrements exactly once");
    }

    for(uint8_t op : {uint8_t(0xa1),uint8_t(0xa9),uint8_t(0xb1),uint8_t(0xb9),
                      uint8_t(0x67),uint8_t(0x6f)}) {
        context="ED internal machine boundary " + std::to_string(op);
        Machine ed;
        ed.put({0x21,0x00,0x40,0x01,0x01,0x00,0x3e,0x10,0xed,op,0x76});
        ed.memory[0x4000]=0x21;
        ed.step(3);
        const auto ed_begin=ed.time, ed_retired=ed.retirements.size();
        for(unsigned t=0;t<8;++t) ed.tick();
        ed.dut.busrq_n=0;
        ed.until([&]{ return !ed.dut.busak_n; });
        equal(ed.time-ed_begin,11,"ED instruction grants DMA after operand3T read");
        equal(ed.retirements.size(),ed_retired,"ED internal work must complete after DMA");
        ed.dut.busrq_n=1; ed.tick();
        equal(ed.dut.a,0x4000,"compare and nibble-rotate internal cycles retain operand address");
        ed.step();
        if(op==0x67 || op==0x6f) {
            equal(ed.memory_writes,1,"nibble rotate writes operand once after DMA");
            equal(ed.memory[0x4000],op==0x67 ? 0x02 : 0x10,"nibble rotation result");
            equal(ed.dut.debug_af>>8,op==0x67 ? 0x11 : 0x12,"nibble rotation accumulator");
        } else {
            equal(ed.dut.debug_bc,0,"block compare decrements counter once");
            equal(ed.dut.debug_hl,op&8 ? 0x3fff : 0x4001,"block compare source cursor");
        }
    }
}

static void interrupt_sample_edge() {
    // UM0080 pp12/14: INT is sampled at the positive start of the final T,
    // with recognition/HALT exit occurring on the following positive edge.
    for(bool halted : {false,true}) for(bool late_assert : {false,true}) {
        context=std::string(halted ? "HALT" : "instruction") +
                (late_assert ? " INT after final-T sample" : " INT released after final-T sample");
        Machine m;
        m.put({0x31,0x00,0x90,0xed,0x56,0xfb,0x00,0x00,0x00,0x76});
        if(halted) m.halt(); else m.step(4);
        m.dut.int_n=late_assert ? 1 : 0;
        for(unsigned t=0;t<3;++t) m.tick();
        m.dut.int_n=late_assert ? 0 : 1;
        m.tick();
        if(late_assert) {
            require(m.dut.debug_iff&2,"late INT waits for next instruction sample");
            if(halted) require(!m.dut.halt_n,"late INT cannot exit HALT at prior sample");
        } else {
            require(!(m.dut.debug_iff&2),"sampled INT remains accepted after release");
            if(halted) require(m.dut.halt_n,"sampled INT exits HALT after release");
        }
        m.until([&]{ return m.dut.debug_pc==0x38 && m.dut.debug_sp==0x8ffe; });
        equal(m.irq_count,1,"accepted sample emits one interrupt acknowledge");
        equal(m.memory[0x8ffe],halted ? 10 : late_assert ? 9 : 8,
              "interrupt return PC identifies sampled instruction boundary");
    }
    for(bool halted : {false,true}) for(bool late_assert : {false,true}) {
        context=std::string(halted ? "HALT" : "instruction") +
                (late_assert ? " NMI after final-T sample" : " NMI before final-T sample");
        Machine m;
        m.put({0x31,0x00,0x90,0xfb,0x00,0x00,0x00,0x76});
        if(halted) m.halt(); else m.step(3);
        for(unsigned t=0;t<(late_assert ? 3u : 2u);++t) m.tick();
        m.dut.nmi_n=0; m.tick(); m.dut.nmi_n=1;
        if(!late_assert) m.tick();
        if(late_assert) {
            require(m.dut.debug_iff&2,"late NMI waits for next instruction sample");
            if(halted) require(!m.dut.halt_n,"late NMI cannot exit HALT at prior sample");
        } else {
            require(!(m.dut.debug_iff&2),"NMI sampled before final T is admitted");
            if(halted) require(m.dut.halt_n,"sampled NMI exits HALT on following edge");
        }
        m.until([&]{ return m.dut.debug_pc==0x66 && m.dut.debug_sp==0x8ffe; });
        equal(m.irq_count,0,"NMI sample emits no interrupt IORQ acknowledge");
        equal(m.memory[0x8ffe],halted ? 8 : late_assert ? 7 : 6,
              "NMI return PC identifies sampled instruction boundary");
    }
}

static void interrupt_parity_sample() {
    for(uint8_t op : {uint8_t(0x57),uint8_t(0x5f)}) {
        for(bool late_assert : {false,true}) {
            context="LD A,I/R parity at INT sampling edge " + std::to_string(op) +
                    (late_assert ? " late assertion" : " early release");
            Machine m;
            m.put({0x31,0x00,0x90,0x3e,0x42,0xed,0x47,0xed,0x56,0xfb,0x00,
                   0xed,op,0x00,0x76});
            m.step(6);
            m.dut.int_n=late_assert ? 1 : 0;
            for(unsigned t=0;t<8;++t) m.tick(); // First M1 4T, then final-T5 sample.
            m.dut.int_n=late_assert ? 0 : 1;
            m.tick();
            equal(bool(m.dut.debug_af&4),late_assert,
                  "parity clears exactly when the sampled INT is accepted");
            equal(bool(m.dut.debug_iff&2),late_assert,
                  "IFF1 acceptance and parity use the same INT sample");
            m.until([&]{ return m.dut.debug_pc==0x38 && m.dut.debug_sp==0x8ffe; });
            equal(m.irq_count,1,"parity test emits one interrupt acknowledge");
            equal(m.memory[0x8ffe],late_assert ? 14 : 13,
                  "parity test returns after the instruction that accepted INT");
        }
        context="LD A,I/R parity with NMI priority " + std::to_string(op);
        Machine nmi;
        nmi.put({0x31,0x00,0x90,0x3e,0x42,0xed,0x47,0xed,0x56,0xfb,0x00,
                 0xed,op,0x00,0x76});
        nmi.step(6); nmi.dut.int_n=0;
        for(unsigned t=0;t<7;++t) nmi.tick();
        nmi.dut.nmi_n=0; nmi.tick(); nmi.dut.nmi_n=1; nmi.tick();
        require(nmi.dut.debug_af&4,"NMI priority preserves LD A,I/R IFF2 parity");
        equal(nmi.dut.debug_iff&6,4,"NMI priority clears only IFF1");
        nmi.until([&]{ return nmi.dut.debug_pc==0x66 && nmi.dut.debug_sp==0x8ffe; });
        equal(nmi.irq_count,0,"NMI priority suppresses maskable acknowledge");
        equal(nmi.memory[0x8ffe],13,"NMI returns after LD A,I/R");
    }
}

static void warm_reset_preserves_registers() {
    context="NMOS warm reset preserves both register banks";
    Machine m;
    m.put({0x31,0x00,0x90,
           0x01,0x57,0x13,0xc5,0xf1,0x08,
           0x01,0x68,0x24,0xc5,0xf1,0x08,
           0x01,0x22,0x11,0x11,0x44,0x33,0x21,0x66,0x55,0xd9,
           0x01,0x88,0x77,0x11,0xaa,0x99,0x21,0xcc,0xbb,0xd9,
           0xdd,0x21,0xee,0xdd,0xfd,0x21,0x01,0xff,
           0x31,0x34,0x82,0x3e,0x42,0xed,0x47,0xed,0x4f,0xfb,0x76});
    m.halt();
    equal(m.dut.debug_af,0x4257,"configured primary AF");
    equal(m.dut.debug_bc,0x1122,"configured primary BC");
    equal(m.dut.debug_de,0x3344,"configured primary DE");
    equal(m.dut.debug_hl,0x5566,"configured primary HL");
    require(m.dut.debug_ir!=0 && (m.dut.debug_iff&6)==6,
            "reset test must start with active IR and interrupt state");
    m.dut.reset=1;
    m.half(false,false);
    equal(m.dut.debug_pc,0,"warm reset clears PC");
    equal(m.dut.debug_ir,0,"warm reset clears I/R");
    equal(m.dut.debug_iff,0,"warm reset clears interrupt state");
    require(m.dut.halt_n && m.inactive() && m.dut.busak_n,
            "warm reset exits HALT and releases bus controls");
    equal(m.dut.debug_af,0x4257,"warm reset preserves AF");
    equal(m.dut.debug_bc,0x1122,"warm reset preserves BC");
    equal(m.dut.debug_de,0x3344,"warm reset preserves DE");
    equal(m.dut.debug_hl,0x5566,"warm reset preserves HL");
    equal(m.dut.debug_ix,0xddee,"warm reset preserves IX");
    equal(m.dut.debug_iy,0xff01,"warm reset preserves IY");
    equal(m.dut.debug_sp,0x8234,"warm reset preserves SP");
    m.put({0x08,0xd9,0x08,0xd9,0x76});
    m.dut.reset=0; m.settle(); m.observe();
    m.step(); equal(m.dut.debug_af,0x2468,"warm reset preserves alternate AF");
    m.step();
    equal(m.dut.debug_bc,0x7788,"warm reset preserves alternate BC");
    equal(m.dut.debug_de,0x99aa,"warm reset preserves alternate DE");
    equal(m.dut.debug_hl,0xbbcc,"warm reset preserves alternate HL");
    m.step(); equal(m.dut.debug_af,0x4257,"AF exchange remains reversible");
    m.step();
    equal(m.dut.debug_bc,0x1122,"BC exchange remains reversible");
    equal(m.dut.debug_de,0x3344,"DE exchange remains reversible");
    equal(m.dut.debug_hl,0x5566,"HL exchange remains reversible");
}

int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    unsigned failures=0;
    for (const auto& test : std::array<std::function<void()>,13>{
            original_program,timings,refresh_and_halt,memory_wait_and_dma,interrupts,
            dma_interrupt_priority,injected_prefixes,halted_dma_priority,
            nmi_during_ei,internal_machine_boundaries,interrupt_sample_edge,interrupt_parity_sample,
            warm_reset_preserves_registers}) {
        try { test(); }
        catch (const std::exception& e) {
            ++failures;
            std::cerr<<"NMOS complete Z80: "<<e.what()<<'\n';
        }
    }
    if (!failures) std::cout<<"NMOS complete Z80: "<<assertions<<" checks passed\n";
    return failures ? 1 : 0;
}
