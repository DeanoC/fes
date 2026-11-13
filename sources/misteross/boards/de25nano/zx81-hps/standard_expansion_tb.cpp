// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_de25_standard_expansion_harness.h"
#include "zx81_firmware.hpp"
#include "fes_de25_pr_control.hpp"
#include "fes_de25_computer.hpp"
#include "native/de25_pr_control.hpp"
#include "native/fes_gp.hpp"
#include "native/hardware.hpp"
#include "native/artifacts.hpp"
#include "native/linux/mmio.hpp"
#include "native/generated/fes_computer.hpp"
#include "de25_standard_descriptor.hpp"
#include "cosim/basic.hpp"
#include "cosim/basic_observer.hpp"
#include "verilated.h"
#include <cstdint>
#include <cstring>
#include <fcntl.h>
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
    bool basic_mode, pixel_running = true, newline_seen = false, idle_seen = false;
    bool load_tags[2] = {}, expect_load = false, previous_vs = true, frame_started = false;
    std::uint64_t retired = 0, picture_frames = 0, frame_pixels = 0;
    unsigned ink = 0, paper = 0, active_pixels = 0, loaded_bytes = 0, uploads = 0;
    unsigned ramtop = 0, ramtop_writes = 0;
    std::string save_path;
    std::vector<unsigned char> captured;
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
    explicit Bench(bool basic = false) : basic_mode(basic), computer_io(*this, false), pr_io(*this, true),
              gp(computer_io, *this, FesGpTransport::de25_computer),
              driver(gp), pr(pr_io, *this) {
        if (basic_mode) {
            char directory[4096]; need(::getcwd(directory, sizeof(directory)) != nullptr, "cannot identify SAVE directory");
            save_path = std::string(directory) + "/standard-rom-save.p";
        }
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
        const bool previous_pixel = d.pixel_clk;
        pixel_phase += 297;
        if (pixel_phase >= 400) { pixel_phase -= 400; if (pixel_running) d.pixel_clk = !d.pixel_clk; }
        d.eval();
        need(!d.unexpected_dma, "unallocated display issued a read");
        need(!d.reset_mismatch, "exported bus reset differs from the actual machine reset");
        if (basic_mode && !previous_pixel && d.pixel_clk) picture();
    }
    void tick() {
        const bool ram_write = d.pack_write;
        const unsigned address = d.pack_write_address, data = d.pack_write_data;
        const bool loader = basic_mode && expect_load && load_tags[1];
        if (basic_mode) {
            load_tags[1] = load_tags[0]; load_tags[0] = d.loader_write;
            const auto request = d.bus_request;
            if (!d.machine_held && !(request & (1ull << 24)) && !(request & (1ull << 27))) {
                const unsigned a = request & 65535, v = (request >> 16) & 255;
                if (a == 0x4004) { ramtop = (ramtop & 0xff00) | v; ramtop_writes |= 1; }
                if (a == 0x4005) { ramtop = (ramtop & 255) | (v << 8); ramtop_writes |= 2; }
            }
        }
        half(false); half(true); ++edges;
        if (basic_mode) {
            retired += d.cpu_retired != 0;
            if (!d.machine_held && d.cpu_address == Zx81Newline) newline_seen = true;
            if (!d.machine_held && d.cpu_address == Zx81EditorPoll) idle_seen = true;
            if (loader) {
                need(ram_write && loaded_bytes < captured.size(), "standard LOAD tag did not reach persona RAM");
                need(address == 9 + loaded_bytes && data == captured[loaded_bytes], "standard LOAD address/data differs");
                ++loaded_bytes;
            }
        }
    }
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
    std::uint16_t word(unsigned address) {
        const auto low = peek(address); return low | (unsigned(peek(address + 1)) << 8);
    }
    void picture() {
        if (d.video_vs && !previous_vs) {
            if (frame_started) {
                need(frame_pixels == 1650u * 750u && active_pixels == 1280u * 720u, "standard BASIC raster changed");
                if (ink && paper > 1000) ++picture_frames;
            }
            frame_started = true; frame_pixels = active_pixels = ink = paper = 0;
        }
        previous_vs = d.video_vs;
        if (!frame_started) return;
        const unsigned h = frame_pixels % 1650, v = (725 + frame_pixels / 1650) % 750;
        active_pixels += d.video_de;
        if (h >= 312 && h < 968 && v >= 90 && v < 630) {
            ink += d.video_rgb == 0; paper += d.video_rgb == 0xffffff;
        }
        ++frame_pixels;
    }
    void editor(std::uint64_t budget, unsigned expected) {
        idle_seen = false;
        const auto end = edges + budget;
        while (edges < end) {
            if (ramtop_writes == 3 && ramtop == expected && idle_seen &&
                word(0x4004) == expected && word(0x4025) == 0xffff && peek(0x4027) == 0) {
                cycles(10000); return;
            }
            cycles(256);
        }
        throw std::runtime_error("standard ROM editor/RAMTOP deadline expired");
    }
    wire::Reply basic_command(unsigned op, unsigned arg, unsigned value) {
        wire::Reply reply;
        if (op == wire::Advance) cycles(arg);
        else if (op == wire::PixelClock) pixel_running = arg != 0;
        else if (op == wire::State) {
            need(arg == wire::MachineReset, "unexpected standard BASIC state observation"); reply.value = d.machine_held;
        } else if (op == wire::WaitEditor) editor(arg, 0x8000);
        else if (op == wire::WaitToken) {
            const auto count = [&] {
                unsigned at = word(0x4014), result = 0;
                for (unsigned n = 0; n < 32 && at < 0x8000; ++n, ++at) {
                    const unsigned byte = peek(at); if (byte == 0x76) break; result += byte == arg;
                }
                return result;
            };
            const auto end = edges + 25000000;
            while (count() < value && edges < end) cycles(4096);
            need(count() >= value, "standard ROM missed HID token");
        } else if (op == wire::BeginLoad) {
            need(!captured.empty(), "no actual standard ROM SAVE"); loaded_bytes = 0; expect_load = true;
        } else if (op == wire::WaitLoaded) {
            const auto end = edges + 160000000;
            while (loaded_bytes < captured.size() && edges < end) cycles(256);
            need(loaded_bytes == captured.size(), "standard LOAD did not consume every actual SAVE byte");
            expect_load = false; reply.value = loaded_bytes;
        } else if (op == wire::WaitPicture) {
            const auto wanted = picture_frames + arg, end = edges + 20000000;
            while (picture_frames < wanted && edges < end) cycles(256);
            need(picture_frames >= wanted, "standard BASIC picture deadline expired");
        } else if (op == basic::HasLine) reply.value = basic::line(*this, arg, value);
        else if (op == basic::WaitNewline) {
            newline_seen = false; const auto end = edges + 25000000;
            while (!newline_seen && edges < end) cycles(256);
            need(newline_seen, "standard ROM did not accept ENTER");
        } else if (op == basic::WaitPrint) {
            const auto end = edges + 160000000;
            while (!basic::printed(*this, arg) && edges < end) cycles(256);
            need(basic::printed(*this, arg), "standard BASIC RUN output differs"); reply.value = arg;
            std::cout << "STANDARD_BASIC RUN printed=" << arg << std::endl;
        } else if (op == basic::WaitSaved) {
            const auto end = edges + 200000000;
            std::uint16_t flags = 0;
            do {
                okay(gp.Exchange(reg::FesComputerOpcodeMediaSnapshotInfo, 0, 0, deadline(), &flags));
                if (flags & reg::FesComputerMediaSnapshotReadyMask) break;
                cycles(4096);
            } while (edges < end);
            need(flags & reg::FesComputerMediaSnapshotReadyMask, "standard ROM SAVE deadline expired");
        } else if (op == basic::CaptureSaved) {
            okay(driver.CaptureZx81Tape(*this, deadline(), &captured));
            need(captured.size() > 51 && captured.size() <= 16384, "invalid standard ROM SAVE size");
            // Store the actual public-API capture before acknowledging it.
            const int fd = ::open(save_path.c_str(), O_CREAT | O_EXCL | O_WRONLY, 0600);
            need(fd >= 0, "standard SAVE fixture already exists or cannot be created");
            need(wire::transfer(fd, captured.data(), captured.size(), true) && ::fsync(fd) == 0, "standard SAVE fixture write failed");
            need(::close(fd) == 0, "standard SAVE fixture close failed");
            okay(driver.AcknowledgeZx81Tape(deadline())); reply.value = captured.size();
            std::cout << "STANDARD_BASIC actual public SAVE bytes=" << captured.size() << std::endl;
        } else if (op == basic::PeekSaved) {
            need(arg < captured.size(), "standard SAVE observation out of range"); reply.value = captured[arg];
        } else throw std::runtime_error("unknown standard BASIC observation");
        reply.retired = retired; reply.cycles = edges; return reply;
    }
    void basic_roundtrip() {
        select(false); editor(770000000, 0x4400);
        std::cout << "STANDARD_BASIC empty RAMTOP=" << word(0x4004) << std::endl;
        freeze(); ramtop = ramtop_writes = 0;
        select(true); editor(770000000, 0x8000);
        std::cout << "STANDARD_BASIC 16K RAMTOP=" << word(0x4004) << std::endl;
        basic::Session session(
            [&](const KeyboardHidRows& rows, std::uint64_t d) { return driver.SetKeyboardHid(rows, d); },
            [&](const std::vector<unsigned char>& bytes, std::uint64_t d, unsigned) {
                need(bytes == captured, "upload differs from the retained public SAVE capture");
                ComputerMediaSnapshot snapshot;
                auto error = snapshot.Prepare(save_path, 1,
                    reg::FesComputerZx81TapeMaxBytes, *this, d);
                if (error.ok()) { ++uploads; error = driver.InsertMedia(0, snapshot, *this, d, 1000); }
                return error;
            },
            [&](unsigned op, unsigned arg, unsigned value) { return basic_command(op, arg, value); },
            [&] { return deadline(); });
        const auto result = session.roundtrip();
        need(uploads == 2 && result.loads == 2 && result.runs == 2 && result.printed_value == 7 && result.reload_removed_edit,
             "incomplete standard real-ROM BASIC roundtrip");
        freeze(); expect_load = false; ramtop = ramtop_writes = 0;
        select(false); editor(770000000, 0x4400); freeze();
        std::cout << "PASS STANDARD_BASIC " << result.json()
                  << " uploads=" << uploads << " callbacks=" << callbacks << " picture_frames=" << picture_frames << std::endl;
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
        const bool basic = argc > 1 && std::strcmp(argv[1], "--basic") == 0;
        Bench b(basic);
        if (basic) { b.basic_roundtrip(); return 0; }
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
