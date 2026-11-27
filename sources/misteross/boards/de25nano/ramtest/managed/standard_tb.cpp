// SPDX-License-Identifier: GPL-2.0-or-later
// Three-clock managed shell and SDRAM pin model, adapted from the Spectrum bench.
#include "Vfes_de25_ramtest_standard.h"
#include "Vfes_de25_ramtest_standard___024root.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <map>
#include <string>
#include <vector>
constexpr uint64_t kSysHalfPs=9574, kBusHalfPs=5000, kPixelHalfPs=6734;
constexpr uint32_t kGpo=0x0c, kGpi=0x10;
[[noreturn]] void fail(const std::string& s) { std::cerr << "FAIL " << s << '\n'; std::exit(1); }
void require(bool c, const std::string& s) { if (!c) fail(s); }
struct Board {
    Vfes_de25_ramtest_standard dut;
    Vfes_de25_ramtest_standard___024root& root;
    uint64_t next_sys = 0, next_pixel = 0, next_bus = 0;
    bool toggle = false;
    // Logical CL2/BL1 SDRAM, as the ramtest controller bench models it: the
    // chips sample on the fabric falling edge, and read data is valid from
    // the edge after the command until the one after that.
    std::array<std::array<int, 4>, 2> rows;
    std::map<uint32_t, uint16_t> memory;
    unsigned edge = 0, due = 0, expires = 0;
    uint16_t pending = 0;
    bool have_pending = false, dq_valid = false, corrupt_reads = false;
    unsigned display_pending = 0;
    bool display_reply = false;
    bool machine_clock_stopped = false;

    Board() : root(*dut.rootp) {
        for (auto& chip : rows) chip.fill(-1);
        dut.clk_sys = 0;
        dut.machine_clk = 0;
        dut.pixel_clk = 0;
        dut.reset_n = 0;
        dut.pixel_reset_n = 0;
        dut.video_ready = 1;
        dut.stream_ready = 1;
        dut.clock_fault = 0;
        dut.computer_read = 0;
        dut.computer_write = 0;
        dut.scanout_read = 0;
        dut.scanout_write = 0;
        dut.avm_waitrequest = 0;
        dut.avm_readdatavalid = 0;
        dut.dram_dq_i = 0;
        dut.eval();
        run_clocks(8);
        dut.reset_n = 1;
        dut.pixel_reset_n = 1;
        run_clocks(8);
    }
    void sdram_falling() {
        ++edge;
        if (dq_valid && edge == expires) {
            dq_valid = false;
            dut.dram_dq_i = 0xdead;
            dut.eval();
        }
        if (have_pending && edge == due) {
            dut.dram_dq_i = pending;
            have_pending = false;
            dq_valid = true;
            expires = edge + 1;
            dut.eval();
        }
        const unsigned command = (dut.dram_ras_n << 2) | (dut.dram_cas_n << 1) | dut.dram_we_n;
        for (unsigned chip = 0; chip < 2; ++chip) {
            if ((dut.dram_cs_n & (1u << chip)) || !dut.dram_cke) continue;
            auto& row = rows[chip][dut.dram_ba];
            if (command == 3) {
                require(row < 0, "SDRAM ACT on an open bank");
                row = dut.dram_addr;
            } else if (command == 2) {
                if (dut.dram_addr & 0x400) rows[chip].fill(-1);
                else row = -1;
            } else if (command == 1) {
                for (int open : rows[chip]) require(open < 0, "SDRAM refresh on an open bank");
            } else if (command == 4 || command == 5) {
                require(row >= 0, "SDRAM data command without an active bank");
                const uint32_t address = (chip << 25) | (unsigned(dut.dram_ba) << 23) |
                                         (unsigned(row) << 10) | (dut.dram_addr & 1023);
                if (command == 4) {
                    require(dut.dram_dq_oe, "SDRAM WRITE with floating DQ");
                    uint16_t& value = memory[address];
                    for (unsigned lane = 0; lane < 2; ++lane)
                        if (!(dut.dram_dqm & (1u << lane))) {
                            const unsigned mask = 255u << (8 * lane);
                            value = (value & ~mask) | (dut.dram_dq_o & mask);
                        }
                } else {
                    require(!dut.dram_dq_oe && !have_pending, "overlapping SDRAM READ");
                    pending = memory[address] ^ (corrupt_reads ? 1 : 0);
                    due = edge + 1;
                    have_pending = true;
                }
            } else {
                require(command == 0 || command == 7, "unsupported SDRAM command");
            }
        }
    }
    // Advance to the next edge of the three unrelated clocks: 0 pixel or a
    // falling edge, 1 a machine rise, 2 a peripheral (computer slave) rise.
    int step() {
        if (next_pixel < next_sys && next_pixel < next_bus) {
            next_pixel += kPixelHalfPs;
            dut.pixel_clk = !dut.pixel_clk;
            if (dut.pixel_clk) {
                dut.avm_readdatavalid = display_reply && display_pending != 0;
                dut.avm_response = 0;
                if (dut.avm_readdatavalid) --display_pending;
                if (dut.avm_read && !dut.avm_waitrequest) display_pending += dut.avm_burstcount;
            }
            dut.eval();
            return 0;
        }
        if (next_bus < next_sys) {
            next_bus += kBusHalfPs;
            dut.clk_sys = !dut.clk_sys;
            dut.eval();
            return dut.clk_sys ? 2 : 0;
        }
        next_sys += kSysHalfPs;
        if (machine_clock_stopped) return 0;
        dut.machine_clk = !dut.machine_clk;
        dut.eval();
        if (!dut.machine_clk) sdram_falling();
        return dut.machine_clk ? 1 : 0;
    }
    void run_clocks(int clocks) {
        for (int i = 0; i < clocks;) i += step() == 1;
    }
    void run_bus(int clocks) {
        for (int i = 0; i < clocks;) i += step() == 2;
    }
    void write(uint32_t address, uint32_t value) {
        dut.computer_address = address;
        dut.computer_writedata = value;
        dut.computer_byteenable = 0xf;
        dut.computer_write = 1;
        dut.eval();
        for (int i = 0; i < 16; ++i) {
            const bool accepted = !dut.computer_waitrequest;
            run_bus(1);
            if (accepted) {
                dut.computer_write = 0;
                require(dut.computer_writeresponsevalid && dut.computer_response == 0,
                        "computer write refused");
                return;
            }
        }
        fail("computer write stalled");
    }
    uint32_t read(uint32_t address, bool expect_fault = false) {
        dut.computer_address = address;
        dut.computer_read = 1;
        dut.eval();
        for (int i = 0; i < 80; ++i) {
            const bool accepted = !dut.computer_waitrequest;
            run_bus(1);
            if (accepted) {
                dut.computer_read = 0;
                require(dut.computer_readdatavalid && dut.computer_response == (expect_fault ? 2 : 0),
                        "computer read refused");
                return dut.computer_readdata;
            }
        }
        fail("computer read stalled");
    }
    // One atomic GPO submission with a new toggle, then the matching ACK.
    uint32_t request(uint32_t opcode, uint32_t index, uint32_t argument) {
        toggle = !toggle;
        write(kGpo, (opcode << 24) | (index << 16) | argument | (toggle ? 0x80000000u : 0));
        for (int i = 0; i < 4096; ++i) {
            const uint32_t word = read(kGpi);
            if ((word >> 24) == 0xf5 && ((word >> 23) & 1u) == static_cast<uint32_t>(toggle))
                return word;
        }
        fail("mailbox did not acknowledge");
    }
    uint16_t ok(uint32_t opcode, uint32_t index, uint32_t argument, const char* what) {
        const uint32_t word = request(opcode, index, argument);
        if (word & 0x400000u)
            fail(std::string(what) + " rejected with error " + std::to_string(word & 0xffff));
        return static_cast<uint16_t>(word);
    }

};
int main(int argc, char** argv) {
 Verilated::commandArgs(argc, argv);
 {
  Board b;
  require(b.ok(1,0,0,"identity") == 0x4546, "identity differs");
  require((b.ok(1,7,0,"capabilities") & 0x200f) == 0x200f, "shell capabilities differ");
  b.ok(2,0,1,"release");
  bool pass=false;
  for (unsigned i=0; i<6000000; ++i) {
   b.run_clocks(1);
   auto flags=b.root.fes_de25_ramtest_standard__DOT__scanner__DOT__result_flags;
   if (flags & 8) fail("SDRAM scan fault");
   if (flags & 4) { pass=true; break; }
  }
  require(pass && !b.memory.empty(), "SDRAM scan did not pass after writes");
  b.ok(2,0,0,"hold after drain");
  b.ok(2,0,1,"release following PASS");
  b.run_clocks(100);
  require(b.root.fes_de25_ramtest_standard__DOT__status_video__DOT__status == 1,
          "rendered status did not leave prior PASS for the fresh scan");
  bool fresh_pass=false;
  for (unsigned i=0; i<6000000; ++i) {
   b.run_clocks(1);
   if (b.root.fes_de25_ramtest_standard__DOT__scanner__DOT__result_flags & 4) { fresh_pass=true; break; }
  }
  require(fresh_pass, "fresh scan after PASS did not finish");
  b.ok(2,0,0,"hold after fresh pass");
 }
 {
  Board b; b.corrupt_reads=true; b.ok(2,0,1,"release corrupt model");
  bool failed=false;
  for (unsigned i=0; i<6000000; ++i) {
   b.run_clocks(1);
   auto flags=b.root.fes_de25_ramtest_standard__DOT__scanner__DOT__result_flags;
   if (flags & 4) fail("corrupted read was reported as pass");
   if (flags & 8) { failed=true; break; }
  }
  require(failed, "corrupted read was not reported");
  b.ok(2,0,0,"hold failed test");
 }
 {
  Board b; b.ok(2,0,1,"release stop case");
  for (unsigned i=0; i<10000 && b.memory.empty(); ++i) b.run_clocks(1);
  require(!b.memory.empty(), "stop case never began");
  b.ok(2,0,0,"hold active scan");
  b.run_clocks(50);
  const auto flags=b.root.fes_de25_ramtest_standard__DOT__scanner__DOT__result_flags;
  require(!(flags & 4) && b.root.fes_de25_ramtest_standard__DOT__scanner__DOT__stopped &&
          b.root.fes_de25_ramtest_standard__DOT__scan_drained,
          "interrupted scan passed or hold completed before drain");
  b.ok(2,0,1,"release for a new scan");
  b.run_clocks(50);
  require(!(b.root.fes_de25_ramtest_standard__DOT__scanner__DOT__result_flags & 4),
          "release reused the old result");
  bool passed_again=false;
  for (unsigned i=0; i<6000000; ++i) {
   b.run_clocks(1);
   if (b.root.fes_de25_ramtest_standard__DOT__scanner__DOT__result_flags & 4) { passed_again=true; break; }
  }
  require(passed_again,"second release did not run a fresh scan");
 }
 std::cout << "PASS managed SDRAM shell: identity, launch, pin-model scan, corruption and hold/drain\n";
}
