// SPDX-License-Identifier: GPL-3.0-or-later
// Host-only bus/observation adapter. The selected computer RTL is unmodified.
#include "Vfes_de25_computer.h"
#include "Vfes_de25_computer___024root.h"
#include "verilated.h"
#include "nano_tape.h"
#include <cstdint>
#include <cstring>
#include <iostream>
#include <sstream>
#include <stdexcept>
#include <string>
#include <vector>

static Vfes_de25_computer top;
static uint64_t transitions;
static uint16_t previous_address;
static void tick(unsigned count = 1) {
    while (count--) {
        top.clk = 0; top.pixel_clk = 0; top.eval();
        top.clk = 1; top.pixel_clk = 1; top.eval();
        if (top.rootp->fes_de25_computer__DOT__cpu_addr != previous_address) ++transitions;
        previous_address = top.rootp->fes_de25_computer__DOT__cpu_addr;
    }
}

static bool csr(uint32_t address, bool write, uint32_t value, uint32_t *result) {
    if (address > 31 || address % 4) return false;
    top.address = address; top.writedata = value;
    top.read = !write; top.write = write;
    tick();
    bool valid = !top.waitrequest && (write ? top.writeresponsevalid : top.readdatavalid);
    *result = write ? 0 : top.readdata;
    uint32_t response = top.response;
    top.read = top.write = 0; tick();
    return valid && response == 0;
}

// Host-only secure supplier. Execution/reset and protocol are real RTL;
// firmware ownership, exclusive execution and the monotonic timer are injected.
struct secure_context {
    fes_nano_tape engine{};
    std::vector<uint8_t> image;
    uint64_t ticks = 0;
    unsigned mode = 0, active = 0, begins = 0, aborts = 0;
    unsigned phase = 0;
    uint32_t offset = 0;
};
static secure_context secure;
static int secure_owns(void *p, uint64_t owner, uint64_t generation) {
    auto *s = static_cast<secure_context *>(p);
    return s->active && owner == 17 && generation == 9 && top.reset_n &&
        top.rootp->fes_de25_computer__DOT__exec_reset && !top.expansion_running &&
        !(s->mode == 1 && s->engine.writes >= 48);
}
static int secure_begin(void *p, uint64_t owner, uint64_t generation, unsigned phase, uint32_t offset) {
    auto *s = static_cast<secure_context *>(p);
    if (s->active || owner != 17 || generation != 9) return -1;
    s->active = 1; s->phase = phase; s->offset = offset; s->begins++;
    if (!secure_owns(p, owner, generation)) {s->active = 0; return -1;}
    return 0;
}
static int secure_end(void *p, uint64_t owner, uint64_t generation, unsigned phase, uint32_t offset, unsigned success) {
    auto *s = static_cast<secure_context *>(p);
    bool valid = s->active && s->phase == phase && s->offset == offset;
    if (success) valid = valid && secure_owns(p, owner, generation);
    else s->aborts++;
    s->active = 0;
    return valid ? 0 : -1;
}
static int secure_read(void *, uint32_t offset, uint32_t *value) {
    return csr(offset, false, 0, value) ? 0 : -1;
}
static int secure_write(void *p, uint32_t offset, uint32_t word) {
    auto *s = static_cast<secure_context *>(p);
    if (offset != FES_DE25_COMPUTER_GPO_OFFSET || ((word >> 24) & 127) == FES_COMPUTER_OPCODE_EXECUTION) return -1;
    uint32_t ignored;
    bool issued = csr(offset, true, word, &ignored);
    return !issued || (s->mode == 4 && s->engine.writes == 48) ? -1 : 0;
}
static int secure_image(void *p, uint32_t offset, uint8_t *out, uint32_t bytes) {
    auto *s = static_cast<secure_context *>(p);
    if (!s->active || offset > s->image.size() || bytes > s->image.size() - offset) return -1;
    std::memcpy(out, s->image.data() + offset, bytes);
    if (s->mode == 2 && s->phase == FES_TAPE_CHUNK) out[0] ^= 1;
    return 0;
}
static uint64_t secure_now(void *p) {
    auto *s = static_cast<secure_context *>(p);
    return s->mode == 5 && s->engine.writes >= 48 ? 1000000001ULL : ++s->ticks;
}
static void secure_barrier(void *) {}

static void secure_load(uint32_t bytes, uint32_t gpo, uint32_t capabilities, const std::string &build, uint32_t crc, unsigned mode) {
    if (!bytes || bytes > FES_DE25_COMPUTER_ZX81_TAPE_MAX_BYTES || build.size() != 32 || mode > 5 || capabilities > 65535) throw std::runtime_error("invalid secure fixture");
    if (!secure.engine.initialized) {
        secure.mode = mode; secure.image.resize(bytes);
        for (unsigned i = 0; i < bytes; i++) secure.image[i] = uint8_t(i * 37 + 11);
        uint16_t words[16] = {FES_COMPUTER_IDENTITY_MAGIC0, FES_COMPUTER_IDENTITY_MAGIC1,
            FES_COMPUTER_TRANSPORT_MAJOR, FES_COMPUTER_TRANSPORT_MINOR, FES_COMPUTER_ABI_TAG,
            FES_COMPUTER_ABI_MAJOR, 0, uint16_t(capabilities)};
        for (unsigned i = 0; i < 8; i++) {
            unsigned low = std::stoul(build.substr(4 * i, 2), nullptr, 16);
            unsigned high = std::stoul(build.substr(4 * i + 2, 2), nullptr, 16);
            words[8 + i] = uint16_t(low | high << 8);
        }
        if (mode == 3) words[15] ^= 1;
        fes_nano_tape_ops ops{&secure, secure_owns, secure_begin, secure_end, secure_read,
            secure_write, secure_image, secure_now, secure_barrier};
        if (fes_nano_tape_init(&secure.engine, ops, 17, 9, words, 1000000000)) throw std::runtime_error("secure engine init");
    }
    fes_nano_tape_receipt receipt{};
    int status = fes_nano_tape_load(&secure.engine, 17, 9, gpo, bytes, crc, &receipt);
    std::cout << "R 1 " << std::hex << uint32_t(status) << ' ' << secure.engine.complete << ' '
        << receipt.gpo << ' ' << receipt.bytes << ' ' << receipt.crc << ' '
        << secure.engine.reads << ' ' << secure.engine.writes << ' ' << secure.begins << ' '
        << receipt.fetches << ' ' << secure.aborts << std::endl;
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    top.reset_n = 0;
    top.read = top.write = top.address = 0;
    top.byteenable = 15;
    top.expansion_running = 0;
    top.diagnostic_address = 0;
    top.expansion_response = 0;
    tick(4); top.reset_n = 1; tick(10);
    for (std::string line; std::getline(std::cin, line);) {
        std::istringstream in(line);
        std::string command;
        uint32_t address = 0, value = 0;
        in >> command >> std::hex;
        if (command == "read" || command == "write") {
            in >> address;
            if (command == "write") in >> value;
            if (!in || address > 31 || address % 4) return 2;
            uint32_t result;
            bool valid = csr(address, command == "write", value, &result);
            std::cout << "R " << (valid ? "1 " : "0 ") << std::hex << result << std::endl;
        } else if (command == "secure") {
            uint32_t gpo, capabilities, crc, mode;
            std::string build;
            in >> value >> gpo >> capabilities >> build >> crc >> mode;
            if (!in) return 2;
            secure_load(value, gpo, capabilities, build, crc, mode);
        } else if (command == "advance") {
            in >> value;
            if (!in || value > 3000000) return 2;
            tick(value);
            std::cout << "R 1 0" << std::endl;
        } else if (command == "reset") {
            top.reset_n = 0; tick(2); top.reset_n = 1; tick(10);
            std::cout << "R 1 0" << std::endl;
        } else if (command == "inspect") {
            auto *r = top.rootp;
            std::cout << "R 1 " << std::hex
                << unsigned(r->fes_de25_computer__DOT__exec_reset) << ' '
                << transitions << ' '
                << (r->fes_de25_computer__DOT__keyboard_rows[0] & 65535) << ' '
                << (r->fes_de25_computer__DOT__keyboard_rows[4] & 65535) << ' '
                << unsigned(r->fes_de25_computer__DOT__unit0_state) << std::endl;
        } else if (command == "byte") {
            in >> address;
            if (!in || address >= 16384) return 2;
            std::cout << "R 1 " << std::hex << unsigned(top.rootp->fes_de25_computer__DOT__tape__DOT__image[address]) << std::endl;
        } else {
            return 2;
        }
        if (Verilated::gotFinish()) return 3;
    }
    top.final();
}
