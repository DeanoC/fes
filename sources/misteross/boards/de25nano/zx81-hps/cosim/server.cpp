// SPDX-License-Identifier: GPL-3.0-or-later
// Actual endpoint/machine RTL; only clocks and observations live in this server.
#include "../computer_sim.hpp"
#include "wire.hpp"
#include "basic_observer.hpp"
#include <chrono>
#include <cstring>
#include <sys/socket.h>
#include <sys/un.h>
#include <thread>

int main(int argc, char** argv) {
    try {
        wire::need(argc >= 2, "expected private socket path");
        Verilated::commandArgs(argc, argv);
        std::setvbuf(stdout, nullptr, _IOLBF, 0);
        sockaddr_un address{}; address.sun_family = AF_UNIX;
        wire::need(std::strlen(argv[1]) < sizeof(address.sun_path), "socket path too long");
        std::strcpy(address.sun_path, argv[1]);
        const int listener = ::socket(AF_UNIX, SOCK_STREAM, 0);
        wire::need(listener >= 0, "socket failed");
        wire::need(::bind(listener, reinterpret_cast<sockaddr*>(&address), sizeof(address)) == 0, "bind failed");
        wire::need(::listen(listener, 1) == 0, "listen failed");
        Computer b; auto& t = b.t;
        std::vector<std::uint8_t> captured;
        std::uint32_t injection = wire::None, previous_ack = 0;
        bool submitted = false, delayed = false;
        const int client = ::accept(listener, nullptr, nullptr);
        wire::need(client >= 0, "accept failed");
        std::array<unsigned char, 16> request{};
        while (wire::transfer(client, request.data(), request.size(), false)) {
            const auto op = wire::decode(request.data());
            const auto arg = wire::decode(request.data() + 4);
            const auto value = wire::decode(request.data() + 8);
            std::uint32_t status = 0, result = 0;
            if (op == wire::Read || op == wire::Write) {
                // Include the live video-readiness register, but not the
                // padding at the end of the endpoint's resource span.
                wire::need(arg <= regs::FesDe25ComputerVideoStatusOffset && arg % 4 == 0,
                           "invalid register offset");
                if (op == wire::Read) {
                    result = b.read(arg);
                    if (submitted && arg == regs::FesDe25ComputerGpiOffset) {
                        if (injection == wire::BadSignature) result ^= 0x01000000;
                        if (injection == wire::MissingAck)
                            result = (result & ~regs::FesComputerAckMask) | previous_ack;
                        if (injection == wire::LateAck && !delayed &&
                            (result & regs::FesComputerAckMask) != previous_ack) {
                            delayed = true;
                            std::this_thread::sleep_for(std::chrono::milliseconds(130));
                        }
                    }
                    if (submitted && injection == wire::BadHeldWord && arg == regs::FesDe25ComputerGpoOffset)
                        result ^= 1;
                } else {
                    wire::need(arg == regs::FesDe25ComputerGpoOffset, "unexpected client write");
                    b.write(arg, value); submitted = true;
                    if (injection == wire::AmbiguousWrite) status = 1;
                    if (injection == wire::ResetAfterWrite) {
                        b.expect_running = false;
                        t.reset_n = 0; b.cycles(4); t.reset_n = 1; b.cycles(4);
                    }
                }
            } else if (op >= basic::HasLine && op <= basic::WaitNewline) {
                result=basic::observe(b,op,arg,value,captured);
            } else if (op == wire::Advance) {
                wire::need(arg <= 800000000, "advance exceeds test budget"); b.cycles(arg);
            } else if (op == wire::State) {
                switch (arg) {
                case wire::MachineReset: result = SIGNAL(machine_reset); break;
                case wire::KeyboardLow: result = static_cast<std::uint32_t>(SIGNAL(keyboard)); break;
                case wire::KeyboardHigh: result = static_cast<std::uint32_t>(SIGNAL(keyboard) >> 32); break;
                case wire::TapeReady: result = SIGNAL(tape_ready); break;
                case wire::TapeBusy: result = SIGNAL(tape_busy); break;
                case wire::LoadedBytes: result = b.loaded_bytes; break;
                case wire::Frames: result = b.complete_frames; break;
                case wire::PictureFrames: result = b.picture_frames; break;
                case wire::ELine: result = b.word(0x4014); break;
                case wire::SourceInk: result = b.source_ink != 0; break;
                case wire::BridgeFault: result = SIGNAL(faults); break;
                default: throw std::runtime_error("unknown observation");
                }
            } else if (op == wire::PeekRam) {
                wire::need(arg >= 0x4000 && arg < 0x8000, "RAM observation outside machine RAM"); result = b.peek(arg);
            } else if (op == wire::PeekTape) {
                wire::need(arg < 16384, "tape observation outside store"); result = SIGNAL(tape__DOT__image)[arg];
            } else if (op == wire::PixelClock) {
                b.pixel.running = arg != 0;
            } else if (op == wire::Reset) {
                b.expect_running = false;
                t.reset_n = 0; b.cycles(4); t.reset_n = 1; b.cycles(4);
            } else if (op == wire::BeginLoad) {
                wire::need(!captured.empty(), "no captured ROM SAVE image");
                b.expected_load = captured; b.loaded_bytes = 0;
            } else if (op == wire::WaitEditor) {
                b.idle_seen = false;
                b.wait_idle(arg, "real ROM did not reach keyboard idle");
                std::printf("EDITOR: cycles=%llu E_LINE=%04x halt=%u source_ink=%llu\n",
                    static_cast<unsigned long long>(b.system_edges), b.word(0x4014), b.halt_seen,
                    static_cast<unsigned long long>(b.source_ink));
            } else if (op == wire::WaitToken) {
                const auto end = b.system_edges + 25000000;
                while (b.token_count(arg) < value && b.system_edges < end) b.cycles(4096);
                wire::need(b.token_count(arg) >= value, "real ROM did not receive HID token");
                result = b.token_count(arg);
            } else if (op == wire::WaitLoaded) {
                const auto end = b.system_edges + 500000000;
                while ((b.loaded_bytes != captured.size() || SIGNAL(tape_busy)) && b.system_edges < end)
                    b.cycles(512);
                wire::need(b.loaded_bytes == captured.size() && !SIGNAL(tape_busy), "BASIC LOAD did not commit all bytes");
                result = b.loaded_bytes;
            } else if (op == wire::WaitPicture) {
                b.inspect_picture = true;
                const auto target = b.picture_frames + arg, end = b.system_edges + 20000000;
                while (b.picture_frames < target && b.system_edges < end) b.step();
                wire::need(b.picture_frames >= target, "typed command not visible in complete frames");
                wire::need(b.halt_seen && b.source_ink, "real ULA/CPU did not produce a picture");
                result = b.picture_frames;
            } else if (op == wire::ExpectRunning) {
                b.expect_running = arg != 0;
            } else if (op == wire::Fault) {
                wire::need(arg <= wire::BadHeldWord, "unknown injection");
                injection = arg; submitted = false; delayed = false;
                previous_ack = SIGNAL(gpi) & regs::FesComputerAckMask;
            } else if (op != wire::Done) {
                throw std::runtime_error("unknown simulator operation");
            }
            std::array<unsigned char, 32> response{};
            const std::uint32_t words[] = {status, result, static_cast<std::uint32_t>(b.system_edges),
                static_cast<std::uint32_t>(b.system_edges >> 32), static_cast<std::uint32_t>(b.retired),
                static_cast<std::uint32_t>(b.retired >> 32), b.mmio_writes, b.mmio_reads};
            for (unsigned i = 0; i < 8; ++i) wire::encode(response.data() + i * 4, words[i]);
            wire::need(wire::transfer(client, response.data(), response.size(), true), "reply failed");
            if (op == wire::Done) break;
        }
        ::close(client); ::close(listener); ::unlink(argv[1]);
        std::puts("PASS: actual DE25 endpoint/machine RTL session complete");
        return 0;
    } catch (const std::exception& error) {
        std::fprintf(stderr, "RTL server: %s\n", error.what()); return 1;
    }
}
