// SPDX-License-Identifier: GPL-2.0-or-later
// fes_computer_mailbox_core with a Spectrum cassette on unit 0 and the
// ENABLE_SPECTRUM_DISK disk on unit 1. Checks each unit's Info, a transfer to
// each with its write unit and committed size, that a transfer is bound to
// its unit (Begin words, Chunk and Commit naming the other unit are refused),
// that one transfer runs at a time, that ejecting one unit leaves the other
// unit's image and transfer alone, and the disk's capability bit. Constants
// are fes_computer.vh's.
#include "Vfes_computer_mailbox_core.h"
#include "verilated.h"

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <string>
#include <vector>

namespace {

[[noreturn]] void fail(const std::string& message) {
    std::fprintf(stderr, "fes.computer mailbox units: %s\n", message.c_str());
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string& message) {
    if (!condition) fail(message);
}

constexpr uint32_t REQUEST = 0x80000000u, ACK = 0x00800000u, ERROR = 0x00400000u;
constexpr uint32_t IDENTITY = 1, INFO = 5, BEGIN = 6, CHUNK = 7, DATA = 8, COMMIT = 9, EJECT = 10;
constexpr uint32_t E_INDEX = 2, E_ARGUMENT = 3, E_STATE = 4;
constexpr uint32_t EMPTY = 1, LOADING = 2, READY = 3;
constexpr uint32_t DISK_BIT = 0x8000u;
constexpr uint32_t DISK_MIN = 8192, DISK_MAX = 1417216, TAPE_MIN = 10, TAPE_MAX = 16777216;

uint32_t crc32(const std::vector<uint8_t>& bytes) {
    uint32_t c = 0xffffffffu;
    for (uint8_t b : bytes) {
        c ^= b;
        for (int i = 0; i < 8; ++i) c = (c & 1) ? (c >> 1) ^ 0xedb88320u : c >> 1;
    }
    return c ^ 0xffffffffu;
}

struct Mailbox {
    Vfes_computer_mailbox_core dut;
    bool toggle = false;
    // Bytes written per unit, by the write port's unit.
    std::vector<uint8_t> memory[2] = {std::vector<uint8_t>(1 << 21), std::vector<uint8_t>(1 << 21)};

    Mailbox() {
        dut.clk = 0;
        dut.gpo = 0;
        dut.mouse_ready = 1;
        dut.media_write_ready = 1;
        dut.media_write_busy = 0;
        dut.media_changed = 0;
        dut.media_read_ready = 0;
        dut.media_read_data = 0;
        dut.save_ready = 0;
        dut.save_busy = 0;
        dut.save_size = 0;
        dut.display_response_valid = 0;
        dut.display_response_error = 0;
        dut.display_response_data = 0;
        dut.display_quiesced = 1;
        dut.eval();
        for (int i = 0; i < 4; ++i) tick();
    }
    void tick() {
        if (dut.media_write_enable & 1) memory[dut.media_write_unit][dut.media_write_addr & 0x1fffff] = dut.media_write_data & 0xff;
        if (dut.media_write_enable & 2) memory[dut.media_write_unit][(dut.media_write_addr + 1) & 0x1fffff] = dut.media_write_data >> 8;
        dut.clk = 1;
        dut.eval();
        dut.clk = 0;
        dut.eval();
    }
    // One request; returns the response word, or ~0 with the error code in
    // error when it was refused.
    uint32_t exchange(uint32_t opcode, uint32_t index, uint32_t argument, uint32_t* error = nullptr) {
        toggle = !toggle;
        dut.gpo = (toggle ? REQUEST : 0) | opcode << 24 | index << 16 | argument;
        for (int i = 0; i < 12; ++i) {
            tick();
            if (bool(dut.gpi & ACK) == toggle) {
                for (int j = 0; j < 2; ++j) tick();
                if (dut.gpi & ERROR) {
                    if (!error) fail("request " + std::to_string(opcode) + "/" + std::to_string(index) +
                                     " refused with " + std::to_string(dut.gpi & 0xffff));
                    *error = dut.gpi & 0xffff;
                    return ~0u;
                }
                if (error) *error = 0;
                return dut.gpi & 0xffff;
            }
        }
        fail("no acknowledgement");
    }
    uint32_t refused(uint32_t opcode, uint32_t index, uint32_t argument) {
        uint32_t error = 0;
        exchange(opcode, index, argument, &error);
        require(error != 0, "request " + std::to_string(opcode) + "/" + std::to_string(index) + " accepted");
        return error;
    }
    uint32_t info(unsigned unit, unsigned field) { return exchange(INFO, unit * 8 + field, 0); }
    void begin(unsigned unit, const std::vector<uint8_t>& image) {
        const uint32_t size = image.size(), crc = crc32(image);
        const uint32_t words[4] = {size & 0xffff, size >> 16, crc & 0xffff, crc >> 16};
        for (unsigned w = 0; w < 4; ++w) exchange(BEGIN, unit * 4 + w, words[w]);
    }
    void chunks(unsigned unit, const std::vector<uint8_t>& image) {
        for (uint32_t offset = 0; offset < image.size(); offset += 512) {
            const uint32_t length = std::min<uint32_t>(512, image.size() - offset);
            exchange(CHUNK, unit * 4 + 0, offset & 0xffff);
            exchange(CHUNK, unit * 4 + 1, offset >> 16);
            exchange(CHUNK, unit * 4 + 2, length);
            for (uint32_t i = 0; i < length; i += 2)
                exchange(DATA, i / 2, image[offset + i] | (i + 1 < length ? image[offset + i + 1] << 8 : 0));
        }
    }
    void load(unsigned unit, const std::vector<uint8_t>& image) {
        begin(unit, image);
        chunks(unit, image);
        exchange(COMMIT, unit, 0);
    }
};

std::vector<uint8_t> pattern(size_t size, uint8_t seed) {
    std::vector<uint8_t> bytes(size);
    for (size_t i = 0; i < size; ++i) bytes[i] = uint8_t(i * 31 + seed + (i >> 9));
    return bytes;
}

}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    Mailbox m;
    require((m.exchange(IDENTITY, 7, 0) & DISK_BIT) != 0, "the disk's capability bit is set");
    for (unsigned unit = 0; unit < 2; ++unit) {
        const uint32_t min = unit ? DISK_MIN : TAPE_MIN, max = unit ? DISK_MAX : TAPE_MAX;
        require(m.info(unit, 0) == (min & 0xffff) && m.info(unit, 1) == min >> 16 &&
                    m.info(unit, 2) == (max & 0xffff) && m.info(unit, 3) == max >> 16 &&
                    m.info(unit, 4) == 512 && m.info(unit, 5) == EMPTY,
                "unit " + std::to_string(unit) + " info");
    }
    require(m.info(2, 5) == 0, "unit 2 is absent");
    require(m.refused(BEGIN, 2 * 4, 1) == E_INDEX, "unit 2 refuses Begin");

    // The disk's limits are its own: a tape-sized image is refused.
    {
        const auto small = pattern(100, 1);
        const uint32_t crc = crc32(small);
        m.exchange(BEGIN, 4, 100);
        m.exchange(BEGIN, 5, 0);
        m.exchange(BEGIN, 6, crc & 0xffff);
        require(m.refused(BEGIN, 7, crc >> 16) == E_ARGUMENT, "disk refuses a 100-byte image");
        m.exchange(EJECT, 1, 0);
    }

    // A disk transfer: its writes name unit 1 and its commit readies unit 1.
    const auto disk = pattern(DISK_MIN + 1001, 7);
    m.load(1, disk);
    require(m.dut.unit1_state == READY && m.dut.unit1_size == disk.size() && m.info(1, 5) == READY,
            "disk ready after commit");
    require(m.dut.unit0_state == EMPTY, "the cassette is untouched");
    require(std::equal(disk.begin(), disk.end(), m.memory[1].begin()), "disk bytes reached unit 1's store");

    // A tape transfer beside it.
    const auto tape = pattern(3001, 3);
    m.load(0, tape);
    require(m.dut.unit0_state == READY && m.dut.unit0_size == tape.size(), "tape ready after commit");
    require(m.dut.unit1_state == READY && m.dut.unit1_size == disk.size(), "the disk stays ready");
    require(std::equal(tape.begin(), tape.end(), m.memory[0].begin()), "tape bytes reached unit 0's store");

    // A transfer is bound to its unit, and one runs at a time.
    {
        const auto second = pattern(DISK_MIN, 9);
        const uint32_t size = second.size(), crc = crc32(second);
        m.exchange(BEGIN, 4, size & 0xffff);
        require(m.refused(BEGIN, 1, size >> 16) == E_INDEX, "Begin may not switch units");
        m.exchange(BEGIN, 5, size >> 16);
        m.exchange(BEGIN, 6, crc & 0xffff);
        m.exchange(BEGIN, 7, crc >> 16);
        require(m.dut.unit1_state == LOADING && m.dut.unit0_state == READY, "only the disk is loading");
        require(m.refused(BEGIN, 0, 1) == E_STATE, "a second transfer waits for the first");
        require(m.refused(CHUNK, 0, 0) == E_INDEX, "a chunk naming the tape is refused");
        require(m.refused(COMMIT, 0, 0) == E_STATE, "a commit naming the tape is refused");
        // Ejecting the tape leaves the disk's transfer running.
        m.exchange(EJECT, 0, 0);
        require(m.dut.unit0_state == EMPTY && m.dut.unit1_state == LOADING, "tape eject spares the disk");
        m.chunks(1, second);
        m.exchange(COMMIT, 1, 0);
        require(m.dut.unit1_state == READY && m.dut.unit1_size == second.size(), "second disk ready");
        require(std::equal(second.begin(), second.end(), m.memory[1].begin()), "second disk bytes");
    }

    // Ejecting the disk leaves a ready tape alone, and ends a disk transfer.
    m.load(0, tape);
    m.exchange(EJECT, 1, 0);
    require(m.dut.unit1_state == EMPTY && m.dut.unit1_size == 0 && m.dut.unit0_state == READY,
            "disk eject spares the tape");
    m.begin(1, disk);
    m.exchange(EJECT, 1, 0);
    require(m.dut.unit1_state == EMPTY && m.refused(CHUNK, 4, 0) == E_STATE, "disk eject ends its transfer");
    m.load(0, pattern(TAPE_MIN, 5));
    require(m.dut.unit0_state == READY && m.dut.unit0_size == TAPE_MIN, "a new transfer starts after it");

    std::printf("fes.computer mailbox: cassette on unit 0, disk on unit 1 ok\n");
    return 0;
}
