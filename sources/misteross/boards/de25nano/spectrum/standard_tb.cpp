// SPDX-License-Identifier: GPL-2.0-or-later
// DE25 ZX Spectrum standard component through its real computer slave: the
// atomic transport, the firmware front end, the mailbox, the machine and the
// board SDRAM controller against a logical SDR SDRAM model. Execution cannot
// be released before a firmware image with a matching CRC is committed; the
// open diagnostic firmware is then uploaded, a PZX tape committed in two
// chunks lands in SDRAM (a byte pair straddling a halfword), the firmware's
// loader plays it back from SDRAM, and HID and controller state reach the
// keyboard matrix. On the +2A/+3 and the Pentagon (SPECTRUM_DISK_BETA or
// SPECTRUM_DISK_UPD765) a disk image committed to media unit 1 lands 16 MiB
// up without touching the tape, and the drive's header fetch reads it back
// through the store's fetch port. The scanout slave and its memory master
// stay idle.
// argv: firmware image, PZX tape.
#include "Vfes_de25_spectrum_standard.h"
#include "Vfes_de25_spectrum_standard___024root.h"
#include "verilated.h"
#include "../sim/sdram_model.h"
#ifdef SPECTRUM_NATIVE48
#include "nano_fabric_stop.h"
#include "nano_program.h"
#include "generated/fes_de25_ddr_probe.h"
#endif

#include <cstdint>
#include <fstream>
#include <iostream>
#include <iterator>
#include <sstream>
#include <string>
#include <vector>

namespace {

constexpr uint64_t kSysHalfPs = 9574;    // nominal 52.224 MHz, rounded to ps
constexpr uint64_t kBusHalfPs = 5000;    // 100 MHz peripheral clock
constexpr uint64_t kPixelHalfPs = 6734;  // nominal 74.25 MHz, rounded to ps
constexpr uint32_t kGpo = 0x0c, kGpi = 0x10, kFault = 0x14;
constexpr uint32_t kInfo = 29, kBegin = 30, kData = 31, kCommit = 32;
#ifdef SPECTRUM_NATIVE48
constexpr uint32_t kDisplayGrantBase = 0x95000000;
#else
constexpr uint32_t kDisplayGrantBase = 0x00100000;
#endif
constexpr uint32_t kErrorIndex = 2, kErrorArgument = 3, kErrorState = 4;

[[noreturn]] void fail(const std::string& message) {
    std::cerr << "de25 spectrum standard: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string& message) {
    if (!condition) fail(message);
}

uint32_t crc32(const std::vector<uint8_t>& bytes) {
    uint32_t c = 0xffffffffu;
    for (uint8_t byte : bytes) {
        c ^= byte;
        for (int bit = 0; bit < 8; ++bit) c = (c & 1u) ? (c >> 1) ^ 0xedb88320u : c >> 1;
    }
    return c ^ 0xffffffffu;
}

std::vector<uint8_t> read_file(const char* path) {
    std::ifstream file(path, std::ios::binary);
    return {std::istreambuf_iterator<char>(file), {}};
}

struct Board {
    Vfes_de25_spectrum_standard dut;
    Vfes_de25_spectrum_standard___024root& root;
    uint64_t next_sys = 0, next_pixel = 0, next_bus = 0;
    bool toggle = false;
    SdramModel sdram;
    unsigned display_pending = 0;
    bool display_reply = false;
    bool machine_clock_stopped = false;

    Board() : root(*dut.rootp) {
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
        if (!dut.machine_clk) sdram.falling(dut, require);
        return dut.machine_clk ? 1 : 0;
    }
    void run_clocks(int clocks) {
        for (int i = 0; i < clocks;) i += step() == 1;
    }
    void run_bus(int clocks) {
        for (int i = 0; i < clocks;) i += step() == 2;
    }
    uint32_t scanout(uint32_t address, bool write, uint32_t value = 0) {
        dut.scanout_address = address;
        dut.scanout_byteenable = 0xf;
        dut.scanout_read = !write;
        dut.scanout_write = write;
        dut.scanout_writedata = value;
        dut.eval();
        require(!dut.scanout_waitrequest, "scanout stalled a CSR access");
        // Move from either level to the next actual pixel rising edge.
        while (dut.pixel_clk) step();
        while (!dut.pixel_clk) step();
        require(dut.scanout_response == 0 &&
                (write ? dut.scanout_writeresponsevalid : dut.scanout_readdatavalid),
                "scanout CSR rejected");
        value = dut.scanout_readdata;
        dut.scanout_read = 0;
        dut.scanout_write = 0;
        dut.eval();
        return value;
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
    void refused(uint32_t opcode, uint32_t index, uint32_t argument, uint32_t error, const char* what) {
        const uint32_t word = request(opcode, index, argument);
        require((word & 0x400000u) && (word & 0xffff) == error, std::string(what) + " was not refused");
    }
    void firmware_begin(uint32_t bytes, uint32_t crc) {
        ok(kBegin, 0, bytes & 0xffff, "firmware total");
        ok(kBegin, 1, bytes >> 16, "firmware total high");
        ok(kBegin, 2, crc & 0xffff, "firmware crc");
    }
    void firmware_data(const std::vector<uint8_t>& image) {
        for (size_t i = 0; i < image.size(); i += 2)
            ok(kData, (i / 2) & 0xff, image[i] | (image[i + 1] << 8), "firmware data");
    }

};

void prime_display(Board& board) {
    board.scanout(0x10, true, kDisplayGrantBase);
    board.scanout(0x18, true, 0x800000);
    board.scanout(0x1c, true, 1);
    board.scanout(0x24, true, 1);
    board.scanout(0x28, true, 1);
    board.scanout(0x0c, true, 1);
    for (unsigned i = 0; i < 16000000 && !board.display_pending; ++i) board.step();
    require(board.display_pending != 0, "scanout did not issue a burst");
}

#ifdef SPECTRUM_NATIVE48
struct SecureStop {
    Board& board;
    bool withhold;
    unsigned stops = 0, snapshots = 0, probe_reads = 0;

    static int owns(void*) { return 1; }
    static int read(void* ptr, uint32_t offset, uint32_t* value) {
        *value = static_cast<SecureStop*>(ptr)->board.scanout(offset, false);
        return 0;
    }
    static int write(void* ptr, uint32_t offset, uint32_t value) {
        auto& self = *static_cast<SecureStop*>(ptr);
        require(offset == 0x0c && (value == 2 || value == 8), "secure stop wrote an unexpected CSR");
        if (value == 2) ++self.stops;
        else ++self.snapshots;
        self.board.scanout(offset, true, value);
        return 0;
    }
    static void wait(void* ptr) {
        auto& self = *static_cast<SecureStop*>(ptr);
        if (!self.withhold) self.board.display_reply = true;
        self.board.run_bus(32);
    }
    static int probe_read(void* ptr, uint32_t offset, uint32_t* value) {
        auto& self = *static_cast<SecureStop*>(ptr);
        ++self.probe_reads;
        // The existing probe bench exercises the real independent DDR
        // endpoint. Here it starts idle while scanout uses the real RTL.
        *value = offset == FES_DE25_DDR_PROBE_ID_OFFSET ? FES_DE25_DDR_PROBE_IDENTITY :
                 offset == FES_DE25_DDR_PROBE_VERSION_OFFSET ? FES_DE25_DDR_PROBE_VERSION :
                 offset == FES_DE25_DDR_PROBE_STATUS_OFFSET ? FES_DE25_DDR_PROBE_STATUS_IDLE : 0;
        return 0;
    }
    static int probe_write(void*, uint32_t, uint32_t) {
        fail("secure stop wrote an idle DDR probe");
    }

    void inject_dma_reject() {
        // Keep one real request in flight and corrupt the next offered command.
        board.dut.avm_waitrequest = true;
        auto& reader = board.root;
        for (unsigned i = 0; i < 10000 && !board.dut.avm_read; ++i) board.step();
        require(board.dut.avm_read, "missing next scanout DMA command");
        const auto pending = board.display_pending;
        reader.fes_de25_spectrum_standard__DOT__display__DOT__memory__DOT__video__DOT__reader__DOT__address =
            0x9580000;
        board.run_bus(32);
        require((board.scanout(0x08, false) & 0x108) == 0x100 && board.display_pending == pending,
                "DMA rejection was lost or an accepted request falsely drained");
        board.dut.avm_waitrequest = false;
    }

    void run(unsigned mode) {
        if (mode == 4) board.scanout(0x0c, true, 2);
        else if (mode != 0 && mode != 5 && mode != 6) prime_display(board);
        if (mode == 8) inject_dma_reject();
        const unsigned expected_stops = mode == 4 || mode == 5 ? 0 : 1;
        struct fes_nano_fabric_stop stop{};
        stop.io = {this, owns, read, write, wait};
        stop.probe.io = {this, owns, probe_read, probe_write, wait};
        stop.abi = FES_NANO_PROGRAM_SPECTRUM48_ABI;
        stop.build[0] = SPECTRUM_BUILD_0; stop.build[1] = SPECTRUM_BUILD_1;
        stop.build[2] = SPECTRUM_BUILD_2; stop.build[3] = SPECTRUM_BUILD_3;
        stop.bus_base = kDisplayGrantBase;
        stop.bytes = 8U << 20;
        stop.cookie = mode == 3 ? 2 : 1;
        stop.qualified = 1;
        if (mode == 6 || mode == 7) {
            int result = fes_nano_fabric_scanout_ready(&stop);
            require((mode == 6 ? result == 0 && !stop.failed : result != 0 && stop.failed) &&
                    !stop.attempted && !snapshots && !stops && !probe_reads,
                    "secure scanout admission did not require a fresh reader");
            return;
        }
        int result = fes_nano_fabric_stop(&stop);
        require(stops == expected_stops, "secure scanout STOP count differs");
        if (mode == 2 || mode == 3) {
            require(result && stop.failed && !stop.done && probe_reads == 0,
                    "uncertain display granted a stop receipt");
            const auto issued = snapshots;
            require(fes_nano_fabric_stop(&stop) == result && snapshots == issued && stops == 1,
                    "failed native stop replayed a command");
        } else {
            require(!result && stop.done && snapshots == 1 && probe_reads != 0 && board.display_pending == 0,
                    "native scanout and probe did not reach a joined stop");
            require(!fes_nano_fabric_stopped(&stop) && snapshots == 1 && stops == expected_stops,
                    "native stop receipt readback wrote a command");
        }
    }
};
#endif

// Test-only register transport for the Linux-free Go driver. Reads and writes
// use the same Avalon slave as the standalone and Linux-side benches.
int native_mailbox() {
    Board board;
    board.run_clocks(8000);
    std::string line;
    while (std::getline(std::cin, line)) {
        std::istringstream input(line);
        std::string operation;
        uint32_t address = 0, value = 0;
        input >> operation >> std::hex;
        if (operation == "read" && input >> address) {
            value = board.read(address);
        } else if (operation == "write" && input >> address >> value) {
            board.write(address, value);
            value = 0;
        } else if (operation == "advance" && input >> value) {
            require(value <= 100000, "native mailbox batch too large");
            board.run_clocks(value);
            value = 0;
        } else if (operation == "display-read" && input >> address) {
            value = board.scanout(address, false);
        } else if (operation == "display-write" && input >> address >> value) {
            board.scanout(address, true, value);
            value = 0;
        } else if (operation == "display-prime") {
            // A real reader submission with the first burst accepted but its
            // replies withheld until directed by the test's memory supplier.
            prime_display(board);
            value = board.display_pending;
        } else if (operation == "display-reply") {
            board.display_reply = true;
            value = board.display_pending;
#ifdef SPECTRUM_NATIVE48
        } else if (operation == "check-secure-display-stop" && input >> value) {
            require(value <= 8, "unknown secure display stop case");
            SecureStop stop{board, value == 2};
            stop.run(value);
            value = 0;
        } else if (operation == "check-display-grant" || operation == "check-display-retirement") {
            if (operation == "check-display-retirement") {
                board.scanout(0x0c, true, 2);
                require((board.scanout(0x08, false) & 0x80000008) == 0x80000008,
                        "empty reader did not retire on STOP");
                board.scanout(0x0c, true, 4);
                value = kDisplayGrantBase;
            } else {
                require(bool(input >> value), "display grant test missing base");
            }
            board.scanout(0x10, true, value);
            board.scanout(0x18, true, 0x800000);
            board.scanout(0x1c, true, 1);
            board.scanout(0x24, true, 1);
            board.scanout(0x28, true, 1);
            board.scanout(0x0c, true, 1);
            const auto status = board.scanout(0x08, false);
            require((status & 0x11) == 0x10 && board.display_pending == 0,
                    "unauthorized native DMA grant enabled scanout");
            value = board.display_pending;
        } else if (operation == "check-fetch-snapshots") {
            uint32_t previous_count = 0;
            for (unsigned i = 0; i < 32; ++i) {
                board.run_clocks(19 + i * 3);
                const auto prior_reply = board.root.fes_de25_spectrum_standard__DOT__transport__DOT__fetch_reply;
                const uint32_t count = board.read(0x18);
                const auto reply = board.root.fes_de25_spectrum_standard__DOT__transport__DOT__fetch_reply;
                const uint64_t source = board.root.fes_de25_spectrum_standard__DOT__transport__DOT__fetch_payload;
                require(reply != prior_reply, "fetch count reused a prior reply");
                require(count > previous_count && count == source >> 16, "fetch count is stale or differs from its source snapshot");
                board.run_clocks(53);
                require(board.read(0x1c) == (source & 0xffff), "fetch address differs from the paired count");
                previous_count = count;
            }
            value = previous_count;
#endif
        } else if (operation == "fetch-stall") {
            board.machine_clock_stopped = true;
            board.read(0x18, true);
            value = board.read(kFault);
        } else if (operation == "spectrum-border") {
            value = board.root.fes_de25_spectrum_standard__DOT__core__DOT__machine__DOT__border_q;
        } else if (operation == "spectrum-state" || operation == "tape-result") {
#if SPECTRUM_BANKS == 3
            const auto& ram = board.root.fes_de25_spectrum_standard__DOT__core__DOT__machine__DOT__ram__DOT__bank__BRA__1__KET____DOT__mem;
            if (operation == "tape-result") {
                value = 0;
                for (unsigned i = 0; i < 4; ++i)
                    value = (value << 8) | ram[0x100 + i];
                std::cout << "R 1 " << std::hex << value << std::endl;
                continue;
            }
            std::cout << "R 1 " << static_cast<unsigned>(board.dut.exec_reset) << ' ' << std::hex
                      << static_cast<unsigned>(ram[0]) << std::endl;
            continue;
#else
            fail("native mailbox proof is for Spectrum 48K");
#endif
        } else {
            fail("unknown native mailbox request");
        }
        std::cout << "R 1 " << std::hex << value << std::endl;
    }
    return 0;
}

}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    if (argc == 2 && std::string(argv[1]) == "--native-mailbox")
        return native_mailbox();
    require(argc == 3, "usage: firmware tape");
    const std::vector<uint8_t> firmware = read_file(argv[1]);
    const std::vector<uint8_t> tape = read_file(argv[2]);
    require(firmware.size() == 16384 || firmware.size() == 32768 || firmware.size() == 49152 ||
            firmware.size() == 65536, "no diagnostic firmware");
    require(tape.size() > 16 && tape.size() <= 512, "no diagnostic tape");

    Board board;
    auto& core = board.root;
    require(board.ok(1, 0, 0, "magic") == 0x4546, "identity magic");
    require(board.ok(1, 4, 0, "tag") == 4, "ABI tag");
    require(board.ok(1, 6, 0, "minor") == 0, "ABI minor");
    // video, keyboard, gamepad, audio, spectrum tape (bits 0-3 and 5), the
    // session display (bit 13) and, on a disk model, the spectrum disk (bit
    // 15). There is no HPS DDR media window (bit 12).
#if defined(SPECTRUM_DISK_BETA) || defined(SPECTRUM_DISK_UPD765)
    require(board.ok(1, 7, 0, "capabilities") == 0xa02f, "capability word");
#else
    require(board.ok(1, 7, 0, "capabilities") == 0x202f, "capability word");
#endif
    constexpr uint16_t first_build_word = (SPECTRUM_BUILD_3 >> 24) |
                                          ((SPECTRUM_BUILD_3 >> 8) & 0xff00);
    require(board.ok(1, 8, 0, "build") == first_build_word, "build identity");

    // fes.fabric.firmware-upload: exactly the model's ROM, and no release
    // without it.
    const uint32_t bytes = static_cast<uint32_t>(firmware.size());
    require(board.ok(kInfo, 0, 0, "bytes") == (bytes & 0xffff), "firmware size");
    require(board.ok(kInfo, 1, 0, "bytes high") == (bytes >> 16), "firmware size high");
    require(board.ok(kInfo, 2, 0, "state") == 0, "firmware starts empty");
    board.refused(kInfo, 5, 0, kErrorIndex, "firmware info index");
    board.refused(2, 0, 1, kErrorState, "release without firmware");
    board.refused(kData, 0, 0, kErrorState, "data without a header");
    board.refused(kBegin, 1, 0, kErrorIndex, "header out of order");
    const uint32_t crc = crc32(firmware);
    board.firmware_begin(bytes + 2, crc);
    board.refused(kBegin, 3, crc >> 16, kErrorArgument, "wrong firmware size");
    // A complete image with the wrong CRC never becomes ready.
    board.firmware_begin(bytes, crc ^ 1u);
    board.ok(kBegin, 3, crc >> 16, "firmware crc high");
    require(board.ok(kInfo, 2, 0, "state") == 1, "firmware is loading");
    board.refused(kCommit, 0, 0, kErrorState, "commit before the image");
    board.refused(kData, 1, 0, kErrorIndex, "data out of order");
    board.firmware_data(firmware);
    board.refused(kData, 0, 0, kErrorState, "data past the image");
    require(board.ok(kInfo, 3, 0, "received") == (bytes & 0xffff), "received count");
    board.refused(kCommit, 0, 0, kErrorArgument, "commit with a wrong CRC");
    board.refused(2, 0, 1, kErrorState, "release after a failed commit");
    board.firmware_begin(bytes, crc);
    board.ok(kBegin, 3, crc >> 16, "firmware crc high");
    board.firmware_data(firmware);
    board.ok(kCommit, 0, 0, "firmware commit");
    require(board.ok(kInfo, 2, 0, "state") == 2, "firmware did not become ready");
    for (size_t i = 0; i < firmware.size(); ++i) {
        const uint8_t stored = (i & 1)
            ? core.fes_de25_spectrum_standard__DOT__core__DOT__machine__DOT__rom__DOT__odd[i / 2]
            : core.fes_de25_spectrum_standard__DOT__core__DOT__machine__DOT__rom__DOT__even[i / 2];
        require(stored == firmware[i], "firmware byte " + std::to_string(i) + " differs");
    }

    // fes.media.spectrum-tape 2.0: a PZX image of 10 bytes to 16 MiB.
    require(board.ok(5, 0, 0, "min lo") == 10, "tape minimum");
    require(board.ok(5, 3, 0, "max hi") == 0x100, "tape maximum high");
    require(board.ok(5, 5, 0, "state") == 1, "tape starts empty");
    board.ok(2, 0, 1, "release");
    board.refused(kBegin, 0, 0, kErrorState, "firmware header while running");

    const uint32_t tape_crc = crc32(tape);
    board.ok(6, 0, tape.size(), "begin total");
    board.ok(6, 1, 0, "begin total high");
    board.ok(6, 2, tape_crc & 0xffff, "crc lo");
    board.ok(6, 3, tape_crc >> 16, "crc hi");
    // A 7-byte chunk leaves the next one at an odd offset, so its pair at
    // bytes 7 and 8 straddles two SDRAM halfwords.
    for (const auto [offset, length] : {std::pair<size_t, size_t>{0, 7}, {7, tape.size() - 7}}) {
        board.ok(7, 0, offset, "chunk offset");
        board.ok(7, 1, 0, "chunk offset high");
        board.ok(7, 2, length, "chunk length");
        for (size_t i = 0; i < length; i += 2) {
            const unsigned low = tape[offset + i];
            const unsigned high = i + 1 < length ? tape[offset + i + 1] : 0;
            board.ok(8, i / 2, low | (high << 8), "data");
        }
    }
    board.ok(9, 0, 0, "commit");
    require(board.ok(5, 5, 0, "ready") == 3, "tape did not become ready");
    for (size_t i = 0; i < tape.size(); ++i)
        require(board.sdram.byte(i) == tape[i], "SDRAM byte " + std::to_string(i) + " differs");

    // The diagnostic's loader starts the deck, which streams the image back
    // from SDRAM: three successive pilot edges 1200 T apart, then the four
    // bytes the loader decodes into $8100.
    int matched = 0;
    bool seen_edge = false, last_ear = false, last_tick = false;
    uint32_t since = 0;
    for (uint64_t clocks = 0; matched < 3; ++clocks) {
        board.run_clocks(1);
        const bool tick = core.fes_de25_spectrum_standard__DOT__core__DOT__tick;
        if (tick && !last_tick && seen_edge) ++since;
        last_tick = tick;
        const bool ear = core.fes_de25_spectrum_standard__DOT__core__DOT__ear;
        if (ear != last_ear) {
            matched = seen_edge && since == 1200 ? matched + 1 : seen_edge ? 0 : matched;
            seen_edge = true;
            since = 0;
            last_ear = ear;
        }
        require(clocks < 8000000, "no tape pilot from SDRAM (" + std::to_string(matched) + " edges)");
    }
    // Spectrum bank 2 ($8000) is stored bank 1 of three, or bank 2 of eight.
    const uint8_t loaded[4] = {0xa5, 0x3c, 0x81, 0x7e};
    auto ram = [&](unsigned offset) {
#if SPECTRUM_BANKS == 3
        return core.fes_de25_spectrum_standard__DOT__core__DOT__machine__DOT__ram__DOT__bank__BRA__1__KET____DOT__mem[offset];
#else
        return core.fes_de25_spectrum_standard__DOT__core__DOT__machine__DOT__ram__DOT__bank__BRA__2__KET____DOT__mem[offset];
#endif
    };
    for (uint64_t clocks = 0;; ++clocks) {
        bool done = true;
        for (unsigned i = 0; i < 4; ++i) done &= ram(0x100 + i) == loaded[i];
        if (done) break;
        require(clocks < 20000000, "the loader did not decode the tape from SDRAM");
        board.run_clocks(1);
    }

#if defined(SPECTRUM_DISK_BETA) || defined(SPECTRUM_DISK_UPD765)
    // fes.media.spectrum-disk 1.0, media unit 1: 8 KiB to 2,834,432 bytes.
    // The image is an 8 KiB header-only raw track disk: byte 8 is 14, which
    // the drive reads as 16 KiB slots, and byte 7 has no launch flag, so the
    // machine is not reset. Byte 8 of the tape is something else.
    require(board.ok(5, 8, 0, "disk min lo") == 0x2000, "disk minimum");
    require(board.ok(5, 11, 0, "disk max hi") == 0x2b, "disk maximum high");
    require(board.ok(5, 13, 0, "disk state") == 1, "disk starts empty");
    std::vector<uint8_t> disk(8192);
    for (size_t i = 0; i < disk.size(); ++i) disk[i] = static_cast<uint8_t>(i * 37 + (i >> 8));
    disk[7] = 0;
    disk[8] = 14;
    require(tape.size() > 8 && tape[8] != 14, "the tape's byte 8 must differ from the disk's");
#if defined(SPECTRUM_DISK_BETA)
    auto& big_slots = core.fes_de25_spectrum_standard__DOT__core__DOT__machine__DOT__beta__DOT__disk__DOT__drives__DOT__big_slots;
#else
    auto& big_slots = core.fes_de25_spectrum_standard__DOT__core__DOT__machine__DOT__upd765__DOT__disk__DOT__drives__DOT__big_slots;
#endif
    require(!big_slots, "the drive read a header before a disk was committed");
    const uint32_t disk_crc = crc32(disk);
    board.ok(6, 4, disk.size() & 0xffff, "disk begin total");
    board.ok(6, 5, disk.size() >> 16, "disk begin total high");
    board.ok(6, 6, disk_crc & 0xffff, "disk crc lo");
    board.ok(6, 7, disk_crc >> 16, "disk crc hi");
    for (size_t offset = 0; offset < disk.size(); offset += 512) {
        board.ok(7, 4, offset & 0xffff, "disk chunk offset");
        board.ok(7, 5, offset >> 16, "disk chunk offset high");
        board.ok(7, 6, 512, "disk chunk length");
        for (size_t i = 0; i < 512; i += 2)
            board.ok(8, i / 2, disk[offset + i] | (disk[offset + i + 1] << 8), "disk data");
    }
    board.ok(9, 1, 0, "disk commit");
    require(board.ok(5, 13, 0, "disk ready") == 3, "disk did not become ready");
    require(board.ok(5, 5, 0, "tape still ready") == 3, "the disk transfer dropped the tape");
    for (size_t i = 0; i < disk.size(); ++i)
        require(board.sdram.byte(0x1000000 + i) == disk[i], "disk SDRAM byte " + std::to_string(i) + " differs");
    for (size_t i = 0; i < tape.size(); ++i)
        require(board.sdram.byte(i) == tape[i], "the disk overwrote tape byte " + std::to_string(i));
    for (uint64_t clocks = 0; !big_slots; ++clocks) {
        require(clocks < 200000, "the drive did not read the disk header through the fetch port");
        board.run_clocks(1);
    }
#endif

    // HID usage 0x04 ('A') is bit 4 of keyboard row 0.
    board.ok(3, 0, 1u << 4, "key A");
    board.run_clocks(60000);
    const uint64_t matrix = core.fes_de25_spectrum_standard__DOT__core__DOT__matrix;
    require((matrix & 0xffffffffffULL) == (0xffffffffffULL & ~(1ULL << 5)),
            "HID A did not land on the Spectrum A key");
    board.ok(3, 0, 0, "keys up");
    // Port 1 Up/Fire: Sinclair joystick 1 is keys 9 and 0.
    board.ok(4, 1, 0x11, "pad 1");
    board.run_clocks(60000);
    const uint64_t joystick = core.fes_de25_spectrum_standard__DOT__core__DOT__matrix;
    require((joystick & 0xffffffffffULL) == (0xffffffffffULL & ~(1ULL << 21) & ~(1ULL << 20)),
            "Sinclair joystick 1 did not press 9 and 0");

    // The committed firmware survives a hold: a relaunch needs no new upload.
    board.ok(2, 0, 0, "hold");
    require(board.ok(kInfo, 2, 0, "state") == 2, "a hold dropped the firmware");
    board.ok(2, 0, 1, "second release");
    require(board.read(kFault) == 0, "transport fault");
    std::cout << "de25 spectrum standard: ok\n";
    return 0;
}
