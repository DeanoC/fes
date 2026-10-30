// SPDX-License-Identifier: GPL-3.0-or-later
// The real runtime client controls RTL through test-only, resource-relative IO.
#include "wire.hpp"
#include "native/de25_computer.hpp"
#include "native/generated/fes_de25_computer.hpp"
#include "native/generated/fes_computer.hpp"
#include "native/hardware.hpp"
#include "native/linux/mmio.hpp"
#include <cstring>
#include <iostream>
#include <sys/socket.h>
#include <sys/un.h>

using namespace mister;
using namespace mister::native;
using namespace mister::native::generated;

class Transport final : public Mmio, public Clock {
public:
    explicit Transport(const char* path) {
        sockaddr_un address{}; address.sun_family = AF_UNIX;
        wire::need(std::strlen(path) < sizeof(address.sun_path), "socket path too long");
        std::strcpy(address.sun_path, path);
        fd_ = ::socket(AF_UNIX, SOCK_STREAM, 0); wire::need(fd_ >= 0, "socket failed");
        wire::need(::connect(fd_, reinterpret_cast<sockaddr*>(&address), sizeof(address)) == 0, "connect failed");
    }
    ~Transport() override { ::close(fd_); }
    wire::Reply command(std::uint32_t op, std::uint32_t arg = 0, std::uint32_t value = 0) {
        // Test observations can advance minutes of emulated time. Register
        // transactions have a separate finite host IPC timeout, not real MMIO.
        timeval timeout{}; timeout.tv_sec = op == wire::Read || op == wire::Write ? 2 : 600;
        wire::need(::setsockopt(fd_, SOL_SOCKET, SO_RCVTIMEO, &timeout, sizeof(timeout)) == 0, "socket timeout failed");
        wire::need(::setsockopt(fd_, SOL_SOCKET, SO_SNDTIMEO, &timeout, sizeof(timeout)) == 0, "socket timeout failed");
        std::array<unsigned char, 16> request{};
        wire::encode(request.data(), op); wire::encode(request.data() + 4, arg); wire::encode(request.data() + 8, value);
        wire::need(wire::transfer(fd_, request.data(), request.size(), true), "send failed");
        std::array<unsigned char, 32> response{};
        wire::need(wire::transfer(fd_, response.data(), response.size(), false), "receive failed");
        last_.status = wire::decode(response.data()); last_.value = wire::decode(response.data() + 4);
        last_.cycles = wire::decode(response.data() + 8) | (std::uint64_t(wire::decode(response.data() + 12)) << 32);
        last_.retired = wire::decode(response.data() + 16) | (std::uint64_t(wire::decode(response.data() + 20)) << 32);
        last_.writes = wire::decode(response.data() + 24); last_.reads = wire::decode(response.data() + 28);
        if (op != wire::Read && op != wire::Write) wire::need(last_.status == 0, "observation failed");
        return last_;
    }
    Error Read32(std::uint32_t offset, std::uint32_t* out) override {
        const auto reply = command(wire::Read, offset);
        *out = reply.value;
        return reply.status ? Error{ErrorCode::io_failed, "injected register read failure", "cosim"} : Error{};
    }
    Error Write32(std::uint32_t offset, std::uint32_t value) override {
        ++attempted_writes_;
        const auto reply = command(wire::Write, offset, value);
        return reply.status ? Error{ErrorCode::io_failed, "injected ambiguous write", "cosim"} : Error{};
    }
    std::uint64_t NowMs() const override { return last_.cycles / 100000; }
    std::uint32_t state(std::uint32_t field) { return command(wire::State, field).value; }
    std::uint32_t writes() { return command(wire::State, wire::MachineReset).writes; }
    std::uint64_t retired() { return command(wire::State, wire::MachineReset).retired; }
    std::uint32_t attempted_writes() const { return attempted_writes_; }
private:
    int fd_ = -1;
    wire::Reply last_;
    std::uint32_t attempted_writes_ = 0;
};

static void okay(const Error& error, const char* operation) {
    if (!error.ok()) throw std::runtime_error(std::string(operation) + ": " + error.message);
}
static std::uint64_t deadline(const Transport& io) { return io.NowMs() + 60000; }
static void poisoned(De25Computer& computer, Transport& io) {
    wire::need(computer.Poisoned(), "uncertainty did not poison client");
    const auto before = io.writes();
    wire::need(!computer.Run(deadline(io)).ok(), "poisoned client ran");
    wire::need(!computer.Hold(deadline(io)).ok(), "poisoned client held");
    wire::need(!computer.LoadTape({1, 2, 3}, deadline(io)).ok(), "poisoned client uploaded");
    wire::need(io.writes() == before, "poisoned client issued another write");
}
static void keyboard(De25Computer& computer, Transport& io, const KeyboardHidRows& rows, std::uint64_t matrix) {
    okay(computer.SetKeyboardHid(rows, deadline(io)), "HID snapshot");
    // The simulated clock runs explicitly, including the mapper's 1 ms settling
    // interval; wall-clock sleeping is not represented as FPGA progress.
    io.command(wire::Advance, 100100);
    const auto actual = std::uint64_t(io.state(wire::KeyboardLow)) |
        (std::uint64_t(io.state(wire::KeyboardHigh)) << 32);
    wire::need(actual == matrix, "HID matrix differs from requested keys");
}
static void type(De25Computer& computer, Transport& io, const KeyboardHidRows& rows,
    std::uint64_t matrix, std::uint8_t token, unsigned count) {
    keyboard(computer, io, rows, matrix);
    io.command(wire::WaitToken, token, count);
    keyboard(computer, io, {}, 0xffffffffffull);
    io.command(wire::WaitEditor, 160000000);
}
static void check_tape(Transport& io, const std::vector<std::uint8_t>& image) {
    wire::need(io.state(wire::TapeReady), "tape not ready after upload");
    for (unsigned i = 0; i < image.size(); ++i)
        wire::need(io.command(wire::PeekTape, i).value == image[i], "actual tape store byte mismatch");
}

int main(int argc, char** argv) {
    try {
        wire::need(argc == 4, "expected socket, scenario, selected build ID");
        Transport io(argv[1]); De25Computer computer(io, io); De25ComputerIdentity identity;
        const std::string scenario = argv[2]; std::string selected = argv[3];
        if (scenario == "wrong-build") selected[0] = selected[0] == '0' ? '1' : '0';
        const auto identified = computer.Identify(selected, deadline(io), &identity);
        std::uint32_t image_bytes = 0, loaded_bytes = 0, complete_frames = 0;
        if (scenario == "wrong-build") {
            wire::need(!identified.ok(), "wrong build accepted"); poisoned(computer, io);
        } else {
            okay(identified, "identify");
            wire::need(identity.build_id == selected && io.state(wire::MachineReset), "initial identity/held state differs");
            const auto start_writes = io.writes();
            if (scenario == "session") {
                std::uint16_t response = 0, rejection = 0;
                okay(computer.Exchange(FesComputerOpcodeMenuInfo, 0, 0, deadline(io), &response, &rejection), "known rejection");
                wire::need(rejection == FesComputerErrorInvalidOpcode && !computer.Poisoned(), "known rejection poisoned link");
                okay(computer.Run(deadline(io)), "run"); io.command(wire::ExpectRunning, 1);
                io.command(wire::WaitEditor, 770000000);
                image_bytes = io.command(wire::CaptureNew).value;
                std::vector<std::uint8_t> image;
                for (unsigned at = 0; at < image_bytes; ++at)
                    image.push_back(io.command(wire::PeekRam, 0x4009 + at).value);
                io.command(wire::PixelClock, 0);
                const auto before_stall = io.retired();
                okay(computer.LoadTape(image, deadline(io), 127), "CRC/chunked tape upload"); check_tape(io, image);
                io.command(wire::Advance, 500000);
                wire::need(io.retired() > before_stall && !io.state(wire::MachineReset), "pixel stall stopped CPU");
                io.command(wire::PixelClock, 1);
                KeyboardHidRows j{}; j[0] = 1u << 13;
                KeyboardHidRows quote{}; quote[1] = 1u << 3; quote[8] = 2;
                type(computer, io, j, 0xffffffffffull & ~(1ull << 33), 0xef, 1);
                type(computer, io, quote, 0xffffffffffull & ~(1ull << 0) & ~(1ull << 25), 0x0b, 1);
                type(computer, io, quote, 0xffffffffffull & ~(1ull << 0) & ~(1ull << 25), 0x0b, 2);
                io.command(wire::WaitPicture, 2);
                io.command(wire::BeginLoad);
                KeyboardHidRows enter{}; enter[2] = 1u << 8;
                keyboard(computer, io, enter, 0xffffffffffull & ~(1ull << 30));
                loaded_bytes = io.command(wire::WaitLoaded).value;
                wire::need(loaded_bytes == image.size(), "incomplete BASIC LOAD");
                keyboard(computer, io, {}, 0xffffffffffull);
                okay(computer.EjectTape(deadline(io)), "live eject");
                wire::need(!io.state(wire::TapeReady) && !io.state(wire::MachineReset), "eject changed execution");
                keyboard(computer, io, j, 0xffffffffffull & ~(1ull << 33));
                io.command(wire::ExpectRunning, 0); okay(computer.Hold(deadline(io)), "hold");
                const auto held_retired = io.retired(); io.command(wire::Advance, 2048);
                wire::need(io.retired() == held_retired && io.state(wire::MachineReset), "HOLD did not stop CPU");
                wire::need(io.state(wire::KeyboardLow) == 0xffffffffu && io.state(wire::KeyboardHigh) == 0xffu, "HOLD did not neutralize keys");
                okay(computer.Run(deadline(io)), "run after hold"); io.command(wire::Advance, 4096);
                wire::need(!io.state(wire::MachineReset) && io.retired() > held_retired, "RUN did not restart CPU");
                okay(computer.Hold(deadline(io)), "final hold");
                complete_frames = io.state(wire::Frames);
                wire::need(io.state(wire::PictureFrames) >= 2 && io.state(wire::BridgeFault) == 0, "session lacked valid frames or had bridge fault");
            } else if (scenario == "tape-bounds") {
                for (const unsigned length : {517u, unsigned(FesDe25ComputerZx81TapeMaxBytes)}) {
                    std::vector<std::uint8_t> image(length);
                    for (unsigned i = 0; i < length; ++i) image[i] = (i * 29u + (i >> 8)) & 255;
                    okay(computer.LoadTape(image, deadline(io), 127), "odd/max upload"); check_tape(io, image);
                    okay(computer.EjectTape(deadline(io)), "eject upload");
                    wire::need(!io.state(wire::TapeReady), "eject left tape ready");
                }
                // Exercise a real known CRC rejection through the public exchange API.
                std::uint16_t response = 0, rejected = 0;
                for (unsigned field = 0; field < 4; ++field) {
                    okay(computer.Exchange(FesComputerOpcodeMediaBegin, field, field == 0 ? 1 : 0,
                        deadline(io), &response, &rejected), "bad CRC header");
                    wire::need(!rejected, "header unexpectedly rejected");
                }
                const std::uint16_t fields[] = {0, 0, 1};
                for (unsigned field = 0; field < 3; ++field)
                    okay(computer.Exchange(FesComputerOpcodeMediaChunk, field, fields[field], deadline(io), &response, &rejected), "bad CRC chunk");
                okay(computer.Exchange(FesComputerOpcodeMediaData, 0, 42, deadline(io), &response, &rejected), "bad CRC byte");
                okay(computer.Exchange(FesComputerOpcodeMediaCommit, 0, 0, deadline(io), &response, &rejected), "bad CRC commit");
                wire::need(rejected == FesComputerErrorInvalidArgument && !computer.Poisoned() && !io.state(wire::TapeReady), "CRC failure exposed data or poisoned link");
                okay(computer.EjectTape(deadline(io)), "eject failed CRC");
            } else {
                if (scenario == "late-reset") io.command(wire::Reset);
                else {
                    const std::pair<const char*, wire::Injection> cases[] = {
                        {"bad-signature", wire::BadSignature}, {"missing-ack", wire::MissingAck},
                        {"late-ack", wire::LateAck}, {"post-write-reset", wire::ResetAfterWrite},
                        {"ambiguous-write", wire::AmbiguousWrite}, {"held-word-change", wire::BadHeldWord}};
                    bool found = false;
                    for (const auto& item : cases) if (scenario == item.first) { io.command(wire::Fault, item.second); found = true; }
                    wire::need(found, "unknown scenario");
                }
                wire::need(!computer.Run(deadline(io)).ok(), "injected uncertainty accepted"); poisoned(computer, io);
                wire::need(io.writes() - start_writes == (scenario == "late-reset" ? 0u : 1u), "uncertain request replayed");
                if (scenario == "late-reset" || scenario == "post-write-reset")
                    wire::need(io.state(wire::BridgeFault) & FesDe25ComputerFaultReset, "actual reset fault absent");
            }
        }
        const auto end = io.command(wire::Done);
        wire::need(end.writes == io.attempted_writes(), "RTL write count differs from client attempts");
        std::cout << "{\"scenario\":\"" << scenario << "\",\"result\":\"passed\",\"writes\":" << end.writes
                  << ",\"image_bytes\":" << image_bytes << ",\"loaded_bytes\":" << loaded_bytes
                  << ",\"complete_frames\":" << complete_frames << "}\n";
        return 0;
    } catch (const std::exception& error) { std::cerr << "native client: " << error.what() << "\n"; return 1; }
}
