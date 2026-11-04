// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_de25_standard_expansion_harness.h"
#include "fes_de25_pr_control.hpp"
#include "fes_de25_computer.hpp"
#include "native/de25_pr_control.hpp"
#include "native/fes_gp.hpp"
#include "native/hardware.hpp"
#include "native/linux/mmio.hpp"
#include "de25_standard_descriptor.hpp"
#include "verilated.h"
#include <cstdint>
#include <iostream>
#include <stdexcept>
namespace reg = mister::native::generated;
using namespace mister;
using namespace mister::native;
static void need(bool ok, const char* message) {
    if (!ok) throw std::runtime_error(message);
}
static void okay(const Error& error) {
    if (!error.ok()) throw std::runtime_error(error.phase + ": " + error.message);
}
struct Bench : Clock {
    Vfes_de25_standard_expansion_harness d;
    unsigned pixel_phase = 0, callbacks = 0, pr_writes = 0;
    std::uint64_t edges = 0;
    struct Endpoint : Mmio {
        Bench& bench;
        bool pr;
        Endpoint(Bench& b, bool p) : bench(b), pr(p) {}
        Error Read32(std::uint32_t offset, std::uint32_t* value) override {
            *value = bench.access(pr, false, offset); return {};
        }
        Error Write32(std::uint32_t offset, std::uint32_t value) override {
            bench.access(pr, true, offset, value); return {};
        }
    } computer_io, pr_io;
    FesGp gp;
    FesGpCoreDriver driver;
    De25PrControl pr;
    CoreDescriptor descriptor = mister_test::De25StandardDescriptor();
    CoreDriverContext context;
    Bench() : computer_io(*this, false), pr_io(*this, true),
              gp(computer_io, *this, FesGpTransport::de25_computer),
              driver(gp), pr(pr_io, *this) {
        d.clk_sys = d.pixel_clk = d.reset_n = d.persona_b = 0;
        d.address = d.read = d.write = d.writedata = 0; d.byteenable = 15;
        d.pr_address = d.pr_read = d.pr_write = d.pr_writedata = 0;
        d.peek_address = 0x4000; d.eval(); cycles(8);
        d.reset_n = 1; cycles(40);
        need(d.frozen && d.machine_held, "cold controller must hold standard core");
        descriptor.build.id = "00112233445566778899aabbccddeeff";
        context.descriptor = &descriptor; context.expected_core = "fes.zx81";
        context.generation = 1;
        driver.BeginSession();
        okay(driver.Identify(context, deadline()).error);
        okay(driver.WaitVideoReady(deadline()));
        De25PrIdentity identity;
        okay(pr.Identify(descriptor.build.id, deadline(), &identity));
        need(identity.frozen, "client did not identify Frozen");
        start();
        need(!d.exec_reset && d.machine_held, "RUN must not bypass frozen expansion");
    }
    std::uint64_t NowMs() const override { return edges / 100000; }
    std::uint64_t deadline() const { return NowMs() + 1000; }
    void start() { okay(driver.Start(context, deadline()).error); }
    void hold() { okay(driver.Quiesce(context, deadline()).error); }
    void half(bool high) {
        d.clk_sys = high;
        pixel_phase += 297;
        if (pixel_phase >= 400) { pixel_phase -= 400; d.pixel_clk = !d.pixel_clk; }
        d.eval();
        need(!d.unexpected_dma, "unallocated display issued a read");
        need(!d.reset_mismatch, "exported bus reset differs from the actual machine reset");
    }
    void tick() { half(false); half(true); ++edges; }
    void cycles(unsigned count) { while (count--) tick(); }
    std::uint32_t access(bool pr, bool write, unsigned offset, std::uint32_t value = 0) {
        if (pr && write) ++pr_writes;
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
    void select(bool ram, bool run = true) {
        need(d.frozen, "persona selection outside Frozen");
        okay(pr.Reconfigure(ram ? reg::FesDe25PrControlPersonaB : reg::FesDe25PrControlPersonaA,
            [&] {
                need(d.frozen && d.machine_held, "configuration callback before actual drain");
                ++callbacks; d.persona_b = ram; cycles(8); return Error{};
            }, deadline()));
        need(d.running, "persona did not enter Running");
        if (run) start();
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
        hold(); cycles(100);
        need(d.machine_held, "GP HOLD failed");
        const auto low = peek(0x4000), high = peek(0x4400);
        need(ram ? (low == 0x5a && high == 0xa5) : (low == high && (low == 0x5a || low == 0xa5)),
             "standard 1K/16K memory distinction failed");
        need(bool(d.static_response & (1u << 19)) == ram, "RAM persona presence differs");
        std::cout << "STANDARD_RAM persona=" << ram << " 4000=" << unsigned(low)
                  << " 4400=" << unsigned(high) << '\n';
        start();
    }
    void freeze() {
        okay(pr.Freeze(deadline()));
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
    void failed_configuration(bool callback_failure) {
        const auto result = pr.Reconfigure(reg::FesDe25PrControlPersonaA, [&] {
            need(d.frozen && d.machine_held, "failed callback entered before Frozen");
            ++callbacks; d.persona_b = 1; cycles(8);
            return callback_failure ? Error{ErrorCode::program_failed,
                "injected failure after applying RAM selection", "simulation"} : Error{};
        }, deadline());
        need(!result.ok() && pr.Poisoned(), "uncertain configuration was accepted");
        // A mismatched probe enters the shell's sticky Fault state; a failed
        // callback leaves Frozen. Both states isolate the selected persona.
        const auto status = access(true, false, reg::FesDe25PrControlStatusOffset);
        need(!d.running && d.machine_held && (d.frozen || (status & reg::FesDe25PrControlStatusFault)),
             "failed configuration did not isolate actual standard machine");
        need(callbacks == 1 && d.persona_b, "failure was not injected after real selection");
        const auto writes = d.pack_writes, commands = pr_writes, configured = callbacks;
        cycles(100);
        need(d.pack_writes == writes, "failed configuration allowed RAM writes");
        need(!pr.Reconfigure(reg::FesDe25PrControlPersonaA, [&] {
            ++callbacks; return Error{};
        }, deadline()).ok(), "poisoned controller retried configuration");
        need(pr_writes == commands && callbacks == configured,
             "poisoned controller issued commands or called configuration");
        std::cout << "STANDARD_PR_FAILURE case=" << (callback_failure ? "applied-failure" : "wrong-persona")
                  << " callbacks=" << callbacks << " retryCommands=0 postFrozenWrites=0\n";
    }
};
int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    Verilated::threadContextp()->threads(1);
    try {
        Bench b;
        b.hold(); b.select(false, false); b.cycles(100);
        need(b.d.running && b.d.exec_reset && b.d.machine_held,
             "PR resume bypassed client Quiesce");
        b.start(); b.memory(false); b.freeze();
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
        need(b.callbacks == 10 && !b.pr.Poisoned(), "configuration count or client state differs");
        Bench wrong_persona; wrong_persona.failed_configuration(false);
        Bench applied_failure; applied_failure.failed_configuration(true);
        std::cout << "PASS standard core, real FesGpCoreDriver and De25PrControl, empty/16K personas, reset/reselect and live write drain\n";
    } catch (const std::exception& error) {
        std::cerr << error.what() << '\n'; return 1;
    }
}
