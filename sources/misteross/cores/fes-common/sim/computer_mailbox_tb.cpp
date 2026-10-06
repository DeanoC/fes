// SPDX-License-Identifier: GPL-2.0-or-later
// Replays one scenario of the fes.computer 1.0 golden exchanges against
// fes_computer_mailbox. The model is compiled once per scenario because
// capabilities and unit limits are endpoint parameters.
#include "Vfes_computer_mailbox.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <iostream>
#include <regex>
#include <sstream>
#include <string>
#include <vector>

namespace {

[[noreturn]] void fail(const std::string& message) {
    std::cerr << "fes.computer mailbox: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string& message) {
    if (!condition) fail(message);
}

struct Exchange {
    std::string name;
    uint32_t gpo0, gpo1, gpi, data;
    uint32_t opcode, index, argument, error;
};

uint32_t crc32(const std::vector<uint8_t>& bytes) {
    uint32_t c = 0xffffffffu;
    for (uint8_t b : bytes) {
        c ^= b;
        for (int i = 0; i < 8; ++i) c = (c & 1) ? (c >> 1) ^ 0xedb88320u : c >> 1;
    }
    return c ^ 0xffffffffu;
}

}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    require(argc >= 3, "usage: fixture.json scenario-index");
    std::ifstream stream(argv[1]);
    require(bool(stream), "cannot open fixture");
    std::ostringstream text;
    text << stream.rdbuf();
    const std::string json = text.str();
    const int scenario = std::atoi(argv[2]);

    // Split the document at each scenario's exchange array.
    std::vector<size_t> starts;
    for (size_t at = json.find("\"exchanges\""); at != std::string::npos;
         at = json.find("\"exchanges\"", at + 1))
        starts.push_back(at);
    require(starts.size() >= 2 && scenario >= 0 && scenario < int(starts.size()), "invalid fixture scenario");
    const size_t begin = starts[scenario];
    const size_t end = scenario + 1 < int(starts.size()) ? starts[scenario + 1] : json.size();
    const std::string body = json.substr(begin, end - begin);

    const std::regex row(
        R"re(\{\s*"argument":\s*(\d+),\s*"data":\s*(\d+),\s*"error":\s*(\d+),\s*"gpi":\s*(\d+),\s*"gpo":\s*\[\s*(\d+),\s*(\d+)\s*\],\s*"index":\s*(\d+),\s*"name":\s*"([^"]+)",\s*"opcode":\s*(\d+)\s*\})re");
    std::vector<Exchange> exchanges;
    for (auto it = std::sregex_iterator(body.begin(), body.end(), row); it != std::sregex_iterator(); ++it) {
        const auto& m = *it;
        exchanges.push_back({m[8], uint32_t(std::stoul(m[5])), uint32_t(std::stoul(m[6])),
                             uint32_t(std::stoul(m[4])), uint32_t(std::stoul(m[2])),
                             uint32_t(std::stoul(m[9])), uint32_t(std::stoul(m[7])),
                             uint32_t(std::stoul(m[1])), uint32_t(std::stoul(m[3]))});
    }
    require(!exchanges.empty(), "empty fixture scenario");

    std::smatch match;
    require(std::regex_search(json, match, std::regex("\"build_id\":\\s*\"([0-9a-f]{32})\"")),
            "fixture lacks build id");
    const std::string build = match[1];

    Vfes_computer_mailbox dut;
    for (int word = 0; word < 4; ++word)
        dut.build_id[3 - word] = uint32_t(std::stoul(build.substr(word * 8, 8), nullptr, 16));
    dut.clk = 0;
    dut.gpo = 0;
    dut.mouse_ready=1; dut.media_write_ready=1; dut.media_write_busy=0; dut.media_changed=0; dut.media_read_ready=0; dut.media_read_data=0;
    dut.eval();
    std::vector<uint8_t> memory(1 << 18, 0);
    auto tick = [&]() {
        dut.clk = 1;
        dut.eval();
        dut.clk = 0;
        dut.eval();
    };
    auto sample_writes = [&]() {
        // Writes are combinational for the acknowledging clock; sample first.
        if (dut.media_write_enable & 1) memory[dut.media_write_addr] = dut.media_write_data & 0xff;
        if (dut.media_write_enable & 2) memory[(dut.media_write_addr + 1) & 0x3ffff] = dut.media_write_data >> 8;
    };
    for (int i = 0; i < 4; ++i) tick();
    for (const Exchange& x : exchanges) {
        dut.gpo = x.gpo0;
        for (int i = 0; i < 3; ++i) { sample_writes(); tick(); }
        dut.gpo = x.gpo1;
        bool acknowledged = false;
        for (int i = 0; i < 8 && !acknowledged; ++i) {
            sample_writes();
            tick();
            acknowledged = ((dut.gpi >> 23) & 1) == ((x.gpo1 >> 31) & 1);
        }
        require(acknowledged, x.name + ": no acknowledgement");
        if (dut.gpi != x.gpi) {
            std::ostringstream message;
            message << x.name << ": gpi 0x" << std::hex << dut.gpi << " want 0x" << x.gpi;
            fail(message.str());
        }
    }

    // Final state from the fixture's reference model.
    if (scenario == 0) {
        require(dut.exec_reset == 0, "input scenario ends released");
        uint32_t rows[9] = {0, 0, 512, 0, 0, 0, 0, 0, 0};
        for (int r = 0; r < 9; ++r) {
            uint32_t got = (dut.keyboard_rows[(r * 16) / 32] >> ((r * 16) % 32)) & 0xffff;
            require(got == rows[r], "keyboard row " + std::to_string(r));
        }
        require(dut.controller_buttons == 0, "hold must neutralise both ports");
    } else if (scenario == 1) {
        require(dut.unit0_state == 3 && dut.unit0_size == 3, "unit 0 ends ready with three bytes");
        std::vector<uint8_t> image(memory.begin(), memory.begin() + dut.unit0_size);
        require(crc32(image) == 1471701714u, "committed unit 0 image differs from the fixture");
    }
    std::cout << "fes.computer mailbox scenario " << scenario << ": " << exchanges.size()
              << " exchanges match\n";
    return 0;
}
