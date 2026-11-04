// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_de25_standard_expansion_harness.h"
#include "fes_de25_pr_control.hpp"
#include "fes_de25_computer.hpp"
#include "verilated.h"
#include <cstdint>
#include <iostream>
#include <stdexcept>
namespace reg = mister::native::generated;
static void need(bool ok, const char* message) {
    if (!ok) throw std::runtime_error(message);
}
struct Bench {
    Vfes_de25_standard_expansion_harness d;
    bool toggle = false;
    unsigned sequence = 0, pixel_phase = 0;
    Bench() {
        d.clk_sys = d.pixel_clk = d.reset_n = d.persona_b = 0;
        d.address = d.read = d.write = d.writedata = 0; d.byteenable = 15;
        d.pr_address = d.pr_read = d.pr_write = d.pr_writedata = 0;
        d.peek_address = 0x4000; d.eval(); cycles(8);
        d.reset_n = 1; cycles(40);
        need(d.frozen && d.machine_held, "cold controller must hold standard core");
        need(gp(1, 7, 0) == 0x680f, "standard capabilities differ");
        gp(2, 0, 1);
        need(!d.exec_reset && d.machine_held, "RUN must not bypass frozen expansion");
    }
    void half(bool high) {
        d.clk_sys = high;
        pixel_phase += 297;
        if (pixel_phase >= 400) { pixel_phase -= 400; d.pixel_clk = !d.pixel_clk; }
        d.eval();
        need(!d.unexpected_dma, "unallocated display issued a read");
        need(!d.reset_mismatch, "exported bus reset differs from the actual machine reset");
    }
    void tick() { half(false); half(true); }
    void cycles(unsigned count) { while (count--) tick(); }
    std::uint32_t access(bool pr, bool write, unsigned offset, std::uint32_t value = 0) {
        if (pr) {
            d.pr_address = offset; d.pr_writedata = value;
            d.pr_write = write; d.pr_read = !write;
        } else {
            d.address = offset; d.writedata = value;
            d.write = write; d.read = !write;
        }
        tick();
        need(pr ? (!d.pr_waitrequest && !d.pr_response &&
                    (write ? d.pr_writeresponsevalid : d.pr_readdatavalid)) :
                  (!d.waitrequest && !d.response &&
                    (write ? d.writeresponsevalid : d.readdatavalid)), "MMIO completion");
        auto result = pr ? d.pr_readdata : d.readdata;
        d.read = d.write = d.pr_read = d.pr_write = 0; tick();
        return result;
    }
    std::uint16_t gp(unsigned op, unsigned index, unsigned argument) {
        toggle = !toggle;
        access(false, true, reg::FesDe25ComputerGpoOffset,
               (toggle ? 0x80000000u : 0) | (op << 24) | (index << 16) | argument);
        for (unsigned n = 0; n < 100; ++n) {
            auto word = access(false, false, reg::FesDe25ComputerGpiOffset);
            need((word & 0xff000000u) == 0xf5000000u && !(word & 0x00400000u), "GP signature/error");
            if (bool(word & 0x00800000u) == toggle) return std::uint16_t(word);
        }
        throw std::runtime_error("GP deadline");
    }
    void command(unsigned op, std::uint32_t argument) {
        access(true, true, reg::FesDe25PrControlArgumentOffset, argument);
        access(true, true, reg::FesDe25PrControlCommandOffset,
               (op << reg::FesDe25PrControlCommandShift) | ++sequence);
        for (unsigned n = 0; n < 100; ++n) {
            need(access(true, false, reg::FesDe25PrControlFaultOffset) == 0, "PR controller fault");
            if (access(true, false, reg::FesDe25PrControlCompletedSeqOffset) == sequence) return;
        }
        throw std::runtime_error("PR command deadline");
    }
    void select(bool ram) {
        need(d.frozen, "persona selection outside Frozen");
        d.persona_b = ram;
        command(reg::FesDe25PrControlCommandResume,
                ram ? reg::FesDe25PrControlPersonaB : reg::FesDe25PrControlPersonaA);
        need(d.running, "persona did not enter Running");
        gp(2, 0, 1);
    }
    std::uint8_t peek(unsigned address) {
        d.peek_address = address; cycles(8); return d.peek_data;
    }
    void memory(bool ram) {
        bool low_write = false, high_write = false;
        for (unsigned n = 0; n < 100000 && !(low_write && high_write); ++n) {
            tick();
            const auto request = d.bus_request;
            if ((request & (1ull << 45)) && !(request & (1ull << 24)) && !(request & (1ull << 27))) {
                low_write |= (request & 0x00ffffffu) == 0x005a4000u;
                high_write |= (request & 0x00ffffffu) == 0x00a54400u;
            }
        }
        need(low_write && high_write, "actual standard CPU did not write both RAM addresses");
        gp(2, 0, 0); cycles(100);
        need(d.machine_held, "GP HOLD failed");
        const auto low = peek(0x4000), high = peek(0x4400);
        need(ram ? (low == 0x5a && high == 0xa5) : (low == high && (low == 0x5a || low == 0xa5)),
             "standard 1K/16K memory distinction failed");
        need(bool(d.static_response & (1u << 19)) == ram, "RAM persona presence differs");
        std::cout << "STANDARD_RAM persona=" << ram << " 4000=" << unsigned(low)
                  << " 4400=" << unsigned(high) << '\n';
        gp(2, 0, 1);
    }
    void freeze() {
        command(reg::FesDe25PrControlCommandFreeze, 0);
        need(d.frozen && d.machine_held, "Frozen failed to hold actual standard machine");
        const auto writes = d.pack_writes, response = d.static_response;
        cycles(100);
        need(d.pack_writes == writes && d.static_response == response,
             "RAM write or response changed after Frozen");
        need(!(d.bus_request & (1ull << 45)) && ((d.bus_request >> 24) & 63) == 63,
             "frozen bus was not registered idle");
    }
    void transport_fault() {
        select(true); cycles(10000);
        // An invalid read poisons the real atomic bridge, independently of
        // the still-running PR controller. Its reset must also fence the bus.
        d.address = reg::FesDe25ComputerSpan - 4; d.read = 1; tick();
        need(d.readdatavalid && d.response == 2, "invalid MMIO read was not rejected");
        d.read = 0; cycles(20);
        need(d.running && d.machine_held && !d.exec_reset,
             "transport failure did not hold standard core independently of RUN");
        need(access(false, false, reg::FesDe25ComputerStatusOffset) & reg::FesDe25ComputerStatusFault,
             "transport fault is not retained");
        const auto writes = d.pack_writes;
        cycles(100);
        need(d.pack_writes == writes && !(d.bus_request & (1ull << 45)) &&
             ((d.bus_request >> 24) & 63) == 63, "transport fault left RAM bus active");
        std::cout << "STANDARD_TRANSPORT_FAULT running=1 machineHeld=1 postDrainWrites=0\n";
    }
};
int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    try {
        Bench b;
        b.select(false); b.memory(false); b.freeze();
        b.select(true); b.memory(true); b.freeze();
        b.select(false); b.memory(false); b.freeze();
        for (unsigned phase = 0; phase < 6; ++phase) {
            b.select(true);
            unsigned deadline = 100000;
            while (deadline && !(b.d.bus_request & (1ull << 45) &&
                   !(b.d.bus_request & (1ull << 24)) && !(b.d.bus_request & (1ull << 27)))) {
                --deadline; b.tick();
            }
            need(deadline != 0, "live write deadline");
            b.cycles(phase); b.freeze();
            std::cout << "STANDARD_DRAIN phase=" << phase << " postFrozenWrites=0\n";
        }
        b.transport_fault();
        std::cout << "PASS standard core, atomic GP, empty/16K personas, reset/reselect and live write drain\n";
    } catch (const std::exception& error) {
        std::cerr << error.what() << '\n'; return 1;
    }
}
