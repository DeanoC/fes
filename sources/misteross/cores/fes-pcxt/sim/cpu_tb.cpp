// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vcpu_top.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <fstream>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

static void require(bool ok, const std::string& why) {
    if (!ok) throw std::runtime_error(why);
}
struct Cycle {
    uint32_t address;
    uint8_t data, kind;
    bool write, lock;
    bool operator==(const Cycle& x) const {
        return address == x.address && data == x.data && kind == x.kind &&
               write == x.write && lock == x.lock;
    }
};
struct Bench {
    Vcpu_top dut;
    std::array<uint8_t, 1 << 20> memory{};
    std::vector<uint8_t> rom;
    Cycle current{}, pending{};
    bool busy=false, local_pending=false, passed=false;
    uint32_t rng=1, ticks=0, transfers=0, inta=0, locked_memory=0;
    uint32_t delay=0, interrupt_delay=0, nmi_ticks=0, ce_count=0;
    unsigned wait_limit=0;
    uint8_t marker=0;
    bool timing_mode=false, timing_interval_active=false;
    unsigned timing_groups=0, interval_ticks=0, interval_ce=0;
    bool saw_reset_fetch=false, saw_io_ffff=false, saw_io_zero=false;
    bool halt_seen=false, halt_held=false;

    explicit Bench(const std::vector<uint8_t>& image, bool timing=false): rom(image), timing_mode(timing) {}
    uint32_t random() { rng ^= rng << 13; rng ^= rng >> 17; rng ^= rng << 5; return rng; }
    Cycle signals() const { return {dut.address, uint8_t(dut.wdata), uint8_t(dut.kind),
                                   bool(dut.write_cycle), bool(dut.locked)}; }
    void reset(unsigned waits, uint32_t seed, unsigned reset_clocks=32) {
        memory.fill(0);
        std::copy(rom.begin(), rom.end(), memory.begin()+0xf0000);
        busy=local_pending=passed=false;
        ticks=transfers=inta=locked_memory=interrupt_delay=nmi_ticks=ce_count=0;
        marker=0; rng=seed; wait_limit=waits;
        timing_groups=interval_ticks=interval_ce=0; timing_interval_active=false;
        saw_reset_fetch=saw_io_ffff=saw_io_zero=false;
        halt_seen=halt_held=false;
        dut.reset=1; dut.intr=0; dut.nmi=0; dut.inject=0;
        dut.memory_done=0; dut.memory_ready=0; dut.memory_rdata=0xff;
        for (unsigned i=0;i<reset_clocks;i++) { dut.clk=0; dut.eval(); dut.clk=1; dut.eval(); }
        dut.reset=0;
    }
    uint8_t access(const Cycle& c) {
        if (c.kind == 2) { ++inta; dut.intr=0; return 0x60; }
        if (c.kind == 1) {
            require(c.address == 0x80 && c.write, "unexpected local I/O");
            marker=c.data;
            if (timing_mode && marker>=0x50 && marker<=0x57) {
                require(marker==0x50+2*timing_groups+unsigned(timing_interval_active),
                        "timing marker out of order");
                if (!(marker & 1)) {
                    interval_ticks=ticks; interval_ce=ce_count; timing_interval_active=true;
                }
                else {
                    require(interval_ticks!=0, "timing interval has no start");
                    std::cout << "EU timing group=" << timing_groups
                              << " core_cycles=" << ticks-interval_ticks
                              << " guest_cycles=" << ce_count-interval_ce
                              << " waits=" << wait_limit << '\n';
                    timing_interval_active=false; ++timing_groups;
                }
                return 0xff;
            }
            if (marker==0x40 || marker==0x41) interrupt_delay=0;
            if (marker == 0xee) throw std::runtime_error("diagnostic failure group " +
                                                        std::to_string(memory[0x201]));
            if (marker == 0xaa) passed=true;
            require(marker==0x40 || marker==0x41 || marker==0xaa, "unexpected marker");
            return 0xff;
        }
        if (c.write && c.address < 0xa0000) memory[c.address]=c.data;
        return memory[c.address];
    }
    void tick() {
        ++ticks;
        dut.memory_done=0;
        dut.memory_ready=!local_pending && (!wait_limit || (random() & 3) != 0);
        if (local_pending) {
            if (delay) --delay;
            else {
                dut.memory_rdata=access(pending);
                dut.memory_done=1;
                local_pending=false;
            }
        }
        if ((marker==0x40 || marker==0x41) && dut.halted && !interrupt_delay) {
            interrupt_delay=ticks+100;
            halt_seen=true;
            halt_held=true;
        }
        if (halt_seen && interrupt_delay && ticks<interrupt_delay && !dut.halted)
            halt_held=false;
        if (interrupt_delay && ticks==interrupt_delay) {
            require(halt_seen && halt_held && dut.halted,
                    "halted dropped while the CPU was still waiting");
            if (marker==0x40) dut.intr=1;
            else { dut.nmi=1; nmi_ticks=ticks+100; }
        }
        if (nmi_ticks && ticks==nmi_ticks) dut.nmi=0;
        dut.clk=0; dut.eval();
        if (dut.guest_ce) ++ce_count;
        if (busy) require(signals()==current, "request changed before completion");
        if (dut.accepted) {
            require(!busy, "duplicate/outstanding CPU transaction");
            current=signals(); busy=true; ++transfers;
            if (transfers==1) {
                require(current.address==0xffff0 && current.kind==0 && !current.write,
                        "reset did not fetch at FFFF0");
                saw_reset_fetch=true;
            }
            if (current.kind==0 && current.lock) ++locked_memory;
            if (current.kind==2) require(current.lock, "INTA acknowledge was not locked");
            if (current.kind==1 && current.address==0xffff && current.write) {
                require(current.data==0x76, "word I/O low byte"); saw_io_ffff=true;
            }
            if (current.kind==1 && current.address==0 && current.write) {
                require(saw_io_ffff && current.data==0x98, "word I/O wrapping high byte");
                saw_io_zero=true;
            }
            if (dut.memory_valid) {
                require(!local_pending, "overlapping memory request");
                pending=current; local_pending=true;
                delay=wait_limit ? random() % wait_limit : 0;
            }
        }
        if (dut.completed) { require(busy, "completion without transaction"); busy=false; }
        dut.clk=1; dut.eval();
    }
    void run() {
        while (!passed && !dut.fault && ticks < 2000000) tick();
        require(!dut.fault, "CPU stopped on fabric fault");
        require(passed, "diagnostic timeout at marker " + std::to_string(marker));
        // The physical guest clock must remain independent of EU phases/waits.
        require(ce_count == (uint64_t(ticks)*21/220+1)/2, "guest clock drift");
        if (timing_mode) {
            require(timing_groups==4, "missing timing workloads");
            std::cout << "EU timing workloads passed: " << ticks << " fabric clocks\n";
            return;
        }
        require(inta==2, "INTR must produce exactly two INTA transactions");
        require(!dut.halted, "halted stayed set after interrupt wakeup");
        require(locked_memory>=2, "LOCK prefix missing from memory read/write");
        require(saw_reset_fetch && saw_io_ffff && saw_io_zero, "missing bus coverage");
        require(memory[0x500]==2 && memory[0x501]==1, "interrupt handlers/IRET");
        require(dut.irq==0x20 && dut.drq==2, "probe interrupt/DMA request propagation");
        std::cout << "8088 diagnostic passed: " << transfers << " byte cycles, "
                  << ticks << " fabric clocks, wait limit " << wait_limit << '\n';
    }
};
int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    try {
        require(argc==2 || (argc==3 && std::string(argv[2])=="timing"),
                "usage: Vcpu_top diagnostic.bin [timing]");
        std::ifstream input(argv[1], std::ios::binary);
        std::vector<uint8_t> rom((std::istreambuf_iterator<char>(input)), {});
        require(rom.size()==65536, "diagnostic ROM must be exactly 64 KiB");
        Bench bench(rom, argc==3);
        for (unsigned wait : {0, 31, 173}) {
            bench.reset(wait, wait+1);
            bench.run();
        }
        if (bench.timing_mode) return 0;
        // Cancel an in-flight fetch, then prove a fresh reset starts cleanly.
        for (unsigned phase : {1, 20, 100}) {
            bench.reset(250, phase);
            while (!bench.local_pending) bench.tick();
            bench.delay=250;
            for (unsigned i=0;i<phase;i++) bench.tick();
            bench.reset(31, phase);
            bench.run();
        }
        // A short reset must replace a stale in-flight microword as well as
        // the PC. Exercise it after actual execution, not just the first fetch.
        for (unsigned width : {1, 2}) {
            bench.reset(0, 1);
            for (unsigned i=0;i<10000;i++) bench.tick();
            bench.reset(0, 1, width); bench.run();
        }
        // A conflict must stop the CPU, with no repeated transfers; reset recovers.
        bench.reset(0, 1); bench.dut.inject=1;
        while (!bench.dut.fault && bench.ticks<500000) bench.tick();
        require(bench.dut.fault==1, "ISA conflict did not latch CPU fault");
        auto before=bench.transfers;
        for (unsigned i=0;i<1000;i++) bench.tick();
        require(bench.transfers==before, "faulted CPU kept issuing requests");
        bench.reset(0, 1); bench.run();
        std::cout << "8088 reset cancellation and ISA fault recovery passed\n";
    } catch (const std::exception& e) {
        std::cerr << "FAIL: " << e.what() << '\n'; return 1;
    }
}
