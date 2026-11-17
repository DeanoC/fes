// SPDX-License-Identifier: GPL-2.0-or-later
// The kit runtime's own fes.computer driver against a DE25 standard machine's
// RTL. driver_proof.py generates the translation unit that includes the
// machine's bench (for its Board: the clocks, the SDRAM model and the
// computer slave's read and write) and then this file, with:
//
//   FES_PROOF_DESCRIPTOR(bytes)  the package descriptor the runtime admits
//   FES_PROOF_MACHINE            its name in the report
//
// Nothing here knows an opcode. libmister-runtime's FesGp and FesGpCoreDriver
// issue every register access through the board's slave, exactly as
// LinuxMmio would on the kit: identity, the refusal to release without
// firmware, FirmwareInfo, Begin, the data, Commit, and the release.
// argv: firmware image.
#include "native/artifacts.hpp"
#include "native/de25_core_contract.hpp"
#include "native/fes_gp.hpp"
#include "native/generated/fes_computer.hpp"
#include "native/generated/fes_de25_computer.hpp"
#include "native/generated/fes_firmware_upload.hpp"
#include "native/hardware.hpp"
#include "native/linux/de25_computer_mmio.hpp"

#include "de25_standard_descriptor.hpp"

#include <chrono>
#include <cstdio>
#include <fstream>
#include <iterator>
#include <unistd.h>

namespace {

using namespace mister::native::generated;

// The physical register window LinuxMmio maps on the kit.
struct SlaveRegisters final : mister::native::Mmio {
    explicit SlaveRegisters(Board& board) : board(board) {}
    mister::Error Read32(std::uint32_t address, std::uint32_t* value) override {
        *value = board.read(address - mister::native::De25ComputerCpuBase);
        ++reads;
        return {};
    }
    mister::Error Write32(std::uint32_t address, std::uint32_t value) override {
        board.write(address - mister::native::De25ComputerCpuBase, value);
        ++writes;
        const std::uint32_t opcode = (value >> 24) & 127u;
        if (opcode == FesFirmwareUploadOpcodeBegin) ++begins;
        if (opcode == FesFirmwareUploadOpcodeData) ++data;
        if (opcode == FesFirmwareUploadOpcodeCommit) ++commits;
        if (opcode == FesComputerOpcodeExecution && (value & 0xffffu) == FesComputerExecutionRelease) ++releases;
        return {};
    }
    Board& board;
    unsigned long reads = 0, writes = 0, begins = 0, data = 0, commits = 0, releases = 0;
};

// The transport bounds one exchange by wall time, so the clock is the wall's.
struct WallClock final : mister::native::Clock {
    std::uint64_t NowMs() const override {
        return static_cast<std::uint64_t>(std::chrono::duration_cast<std::chrono::milliseconds>(
            std::chrono::steady_clock::now().time_since_epoch()).count());
    }
};

struct TemporaryFile {
    std::string path;
    explicit TemporaryFile(const std::vector<uint8_t>& bytes) {
        char name[] = "/tmp/fes-de25-driver-proof-XXXXXX";
        const int fd = mkstemp(name);
        require(fd >= 0 && write(fd, bytes.data(), bytes.size()) == static_cast<ssize_t>(bytes.size()) &&
                close(fd) == 0, "temporary firmware file");
        path = name;
    }
    ~TemporaryFile() { unlink(path.c_str()); }
};

std::vector<uint8_t> proof_image(const char* path) {
    std::ifstream file(path, std::ios::binary);
    return {std::istreambuf_iterator<char>(file), {}};
}

void pass(const mister::Error& error, const char* what) {
    if (!error.ok()) fail(std::string(what) + ": " + error.message + " (" + error.phase + ")");
}

}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    require(argc == 2, "usage: firmware");
    const std::vector<uint8_t> firmware = proof_image(argv[1]);
    require(!firmware.empty(), "no firmware image");

    Board board;
    // A machine that stores its firmware in SDRAM holds the first
    // FirmwareData until that memory has initialized. On the kit that is
    // over long before the runtime's first request; here it must be simulated
    // first, because the transport bounds each exchange by wall time.
    board.run_clocks(8000);
    SlaveRegisters slave(board);
    mister::native::De25ComputerMmio registers(slave);
    WallClock clock;
    const std::uint64_t deadline = clock.NowMs() + 4ull * 60 * 60 * 1000;
    mister::native::FesGp gp(registers, clock, mister::native::FesGpTransport::de25_computer);
    mister::native::FesGpCoreDriver driver(gp);

    // The descriptor the runtime's admission table accepts, with the build
    // identity this RTL was elaborated with.
    mister::CoreDescriptor descriptor = FES_PROOF_DESCRIPTOR(firmware.size());
    descriptor.build.id = "00112233445566778899aabbccddeeff";
    pass(mister::native::CheckDe25StandardComputerContract(descriptor), "package admission");
    mister::native::CoreDriverContext context;
    context.descriptor = &descriptor;

    // Identity: the live capability word, build and media limits must equal
    // what the admitted descriptor declares.
    driver.BeginSession();
    pass(driver.Identify(context, deadline).error, "identity");
    require(driver.FirmwareUploadDeclared(), "driver did not select firmware upload");
    require(board.dut.exec_reset, "machine is not held after configuration");

    // Release before upload. The driver refuses without touching the core;
    // a raw release reaches the RTL, which refuses it itself.
    unsigned long writes = slave.writes;
    require(!driver.Start(context, deadline).error.ok() && slave.writes == writes,
            "driver released a machine without firmware");
    std::uint16_t response = 0;
    const mister::Error raw = gp.Exchange(FesComputerOpcodeExecution, 0, FesComputerExecutionRelease,
                                          deadline, &response);
    require(!raw.ok() && response == FesComputerErrorInvalidState && board.dut.exec_reset,
            "RTL released a machine without firmware");

    // Another image size is refused from FirmwareInfo, before any Begin.
    {
        std::vector<uint8_t> larger(firmware.size() + 1024, 0x5a);
        TemporaryFile file(larger);
        mister::native::ComputerMediaSnapshot snapshot;
        pass(snapshot.Prepare(file.path, larger.size(), larger.size(), clock, deadline), "snapshot");
        const mister::Error refused = driver.UploadFirmware(snapshot, clock, deadline);
        require(refused.code == mister::ErrorCode::core_mismatch && slave.begins == 0,
                "a wrong-size image was begun");
    }

    // The upload: the image's exact bytes, once each, then ready.
    mister::native::ComputerMediaSnapshot image;
    pass(image.Prepare(argv[1], firmware.size(), firmware.size(), clock, deadline), "firmware snapshot");
    bool uploaded = false;
    pass(driver.UploadFirmware(image, clock, deadline, &uploaded), "firmware upload");
    require(uploaded && slave.begins == 4 && slave.data == firmware.size() / 2 && slave.commits == 1,
            "upload request counts differ");
    require(board.dut.exec_reset && slave.releases == 1, "machine left hold during upload");
    pass(gp.Exchange(FesFirmwareUploadOpcodeInfo, FesFirmwareUploadInfoState, 0, deadline, &response),
         "firmware state");
    // Ready means the RTL's own CRC-32 over every stored byte matched.
    require(response == FesFirmwareUploadStateReady, "RTL firmware image is not ready");

    // A second session over the loaded core reads FirmwareInfo and skips.
    mister::native::FesGp again_gp(registers, clock, mister::native::FesGpTransport::de25_computer);
    mister::native::FesGpCoreDriver again(again_gp);
    again.BeginSession();
    pass(again.Identify(context, deadline).error, "second identity");
    uploaded = true;
    pass(again.UploadFirmware(image, clock, deadline, &uploaded), "second upload");
    require(!uploaded && slave.begins == 4 && slave.data == firmware.size() / 2 && slave.commits == 1,
            "a ready image was uploaded again");

    // The one release, accepted by the RTL only now.
    pass(again.Start(context, deadline).error, "release");
    board.run_clocks(64);
    require(!board.dut.exec_reset && slave.releases == 2, "machine did not leave hold");
    // A running machine's firmware cannot change.
    writes = slave.writes;
    require(again.UploadFirmware(image, clock, deadline).code == mister::ErrorCode::busy &&
            slave.writes == writes, "upload attempted while running");

    std::printf("{\"machine\":\"%s\",\"result\":\"passed\",\"firmware_bytes\":%zu,"
                "\"begin_words\":%lu,\"data_words\":%lu,\"commits\":%lu,\"releases_accepted\":1,"
                "\"register_reads\":%lu,\"register_writes\":%lu}\n",
                FES_PROOF_MACHINE, firmware.size(), slave.begins, slave.data, slave.commits,
                slave.reads, slave.writes);
    return 0;
}
