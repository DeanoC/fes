// SPDX-License-Identifier: GPL-2.0-or-later
// Same host vectors against fes_computer_mailbox and fes_computer_mailbox_direct.
// The golden exchanges must still match, and the Atari ST parameter set must
// agree on acknowledgement, opcodes, keyboard, mouse, and media.
#include "Vmailbox_direct_compare_top.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <regex>
#include <sstream>
#include <string>
#include <vector>

namespace {

[[noreturn]] void fail(const std::string& message) {
    std::cerr << "mailbox direct compare: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string& message) {
    if (!condition) fail(message);
}

struct Exchange {
    std::string name;
    uint32_t gpo0, gpo1, gpi;
};

uint32_t crc32(const std::vector<uint8_t>& bytes) {
    uint32_t c = 0xffffffffu;
    for (uint8_t byte : bytes) {
        c ^= byte;
        for (int i = 0; i < 8; ++i) c = (c & 1u) ? (c >> 1) ^ 0xedb88320u : c >> 1;
    }
    return c ^ 0xffffffffu;
}

std::vector<Exchange> load_scenario(const std::string& json, int scenario) {
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
        const auto& match = *it;
        exchanges.push_back({match[8], uint32_t(std::stoul(match[5])), uint32_t(std::stoul(match[6])),
                             uint32_t(std::stoul(match[4]))});
    }
    require(!exchanges.empty(), "empty fixture scenario");
    return exchanges;
}

}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    require(argc >= 2, "usage: computer-exchanges.json");
    std::ifstream stream(argv[1]);
    require(bool(stream), "cannot open fixture");
    std::ostringstream text;
    text << stream.rdbuf();
    const std::string json = text.str();
    std::smatch build_match;
    require(std::regex_search(json, build_match, std::regex("\"build_id\":\\s*\"([0-9a-f]{32})\"")),
            "fixture lacks build id");
    const std::string build = build_match[1];

    Vmailbox_direct_compare_top dut;
    for (int word = 0; word < 4; ++word)
        dut.build_id[3 - word] = uint32_t(std::stoul(build.substr(word * 8, 8), nullptr, 16));
    dut.clk = 0;
    dut.gpo = 0;
    dut.mouse_ready = 1;
    dut.media_write_ready = 1;
    dut.media_write_busy = 0;
    dut.media_changed = 0;
    dut.media_read_ready = 0;
    dut.media_read_data = 0;
    dut.eval();

    auto tick = [&](const std::string& where) {
        dut.clk = 1;
        dut.eval();
        dut.clk = 0;
        dut.eval();
        if (dut.diverge != 0) {
            std::ostringstream message;
            message << where << ": endpoints diverged (" << int(dut.diverge) << ")";
            fail(message.str());
        }
    };
    auto sample_media = [&](std::vector<uint8_t>& memory) {
        if (dut.media_write_enable_media & 1)
            memory[dut.media_write_addr_media] = dut.media_write_data_media & 0xff;
        if (dut.media_write_enable_media & 2)
            memory[(dut.media_write_addr_media + 1) & 0x3ffff] = dut.media_write_data_media >> 8;
    };

    for (int i = 0; i < 4; ++i) tick("reset");
    std::vector<uint8_t> media_memory(1 << 18, 0);
    const auto scenario0 = load_scenario(json, 0);
    for (const Exchange& exchange : scenario0) {
        dut.gpo = exchange.gpo0;
        for (int i = 0; i < 3; ++i) tick(exchange.name);
        dut.gpo = exchange.gpo1;
        bool acknowledged = false;
        for (int i = 0; i < 8 && !acknowledged; ++i) {
            tick(exchange.name);
            acknowledged = ((dut.gpi_input >> 23) & 1) == ((exchange.gpo1 >> 31) & 1);
        }
        require(acknowledged, exchange.name + ": shared input mailbox did not acknowledge");
        require(dut.gpi_input == exchange.gpi, exchange.name + ": input gpi drifted from the fixture");
    }
    require(dut.exec_reset_input == 0, "input scenario ends released");
    const uint32_t rows[9] = {0, 0, 512, 0, 0, 0, 0, 0, 0};
    for (int row = 0; row < 9; ++row) {
        const uint32_t got = (dut.keyboard_rows_input[(row * 16) / 32] >> ((row * 16) % 32)) & 0xffff;
        require(got == rows[row], "keyboard row " + std::to_string(row));
    }
    require(dut.controller_input == 0, "hold must neutralise both ports");

    const auto scenario1 = load_scenario(json, 1);
    for (const Exchange& exchange : scenario1) {
        dut.gpo = exchange.gpo0;
        for (int i = 0; i < 3; ++i) {
            sample_media(media_memory);
            tick(exchange.name);
        }
        dut.gpo = exchange.gpo1;
        bool acknowledged = false;
        for (int i = 0; i < 8 && !acknowledged; ++i) {
            sample_media(media_memory);
            tick(exchange.name);
            acknowledged = ((dut.gpi_media >> 23) & 1) == ((exchange.gpo1 >> 31) & 1);
        }
        require(acknowledged, exchange.name + ": shared media mailbox did not acknowledge");
        require(dut.gpi_media == exchange.gpi, exchange.name + ": media gpi drifted from the fixture");
    }
    require(dut.unit0_state_media == 3 && dut.unit0_size_media == 3, "unit 0 ends ready with three bytes");
    const std::vector<uint8_t> image(media_memory.begin(), media_memory.begin() + dut.unit0_size_media);
    require(crc32(image) == 1471701714u, "committed unit 0 image differs from the fixture");

    auto ack_bit = [&]() { return (dut.gpi_st >> 23) & 1u; };
    auto issue = [&](const std::string& name, uint32_t opcode, uint32_t index, uint32_t argument,
                     bool expect_error, uint32_t data, bool check_data) {
        const uint32_t toggle = ack_bit() ^ 1u;
        dut.gpo = (toggle << 31) | (opcode << 24) | (index << 16) | (argument & 0xffffu);
        bool acknowledged = false;
        bool saw_mouse = false;
        int16_t dx = 0, dy = 0;
        for (int i = 0; i < 8 && !acknowledged; ++i) {
            tick(name);
            if (dut.mouse_valid_st) {
                saw_mouse = true;
                dx = int16_t(dut.mouse_dx_st);
                dy = int16_t(dut.mouse_dy_st);
            }
            acknowledged = ack_bit() == toggle;
        }
        require(acknowledged, name + ": no acknowledgement");
        const uint32_t gpi = dut.gpi_st;
        if (((gpi >> 22) & 1u) != (expect_error ? 1u : 0u)) {
            std::ostringstream message;
            message << name << ": error bit, gpi 0x" << std::hex << gpi;
            fail(message.str());
        }
        if (check_data && (gpi & 0xffffu) != data) {
            std::ostringstream message;
            message << name << ": response 0x" << std::hex << (gpi & 0xffffu) << " want 0x" << data;
            fail(message.str());
        }
        require((gpi & 0xff000000u) == 0xf5000000u, name + ": signature");
        return saw_mouse ? std::pair<int16_t, int16_t>(dx, dy) : std::pair<int16_t, int16_t>(0, 0);
    };

    issue("eject stale begin", 0x0a, 0, 0, false, 0, false);
    require(dut.unit0_state_st == 1 && dut.unit0_size_st == 0, "eject returns empty");
    issue("identity magic", 0x01, 0, 0, false, 0x4546, true);
    issue("identity past end", 0x01, 16, 0, true, 2, true);
    issue("media info minimum low", 0x05, 0, 0, false, 0xa000, true);
    issue("media info minimum high", 0x05, 1, 0, false, 0x0005, true);
    issue("media info maximum low", 0x05, 2, 0, false, 0xd000, true);
    issue("media info maximum high", 0x05, 3, 0, false, 0x000c, true);
    issue("execution hold", 0x02, 0, 0, false, 0, true);
    require(dut.exec_reset_st == 1, "hold asserts reset");
    require(dut.keyboard_rows_st[0] == 0 && dut.keyboard_rows_st[1] == 0 && dut.keyboard_rows_st[2] == 0
            && dut.keyboard_rows_st[3] == 0 && dut.keyboard_rows_st[4] == 0, "hold clears keys");
    require(dut.controller_st == 0, "hold clears controllers");
    issue("execution release", 0x02, 0, 1, false, 0, true);
    require(dut.exec_reset_st == 0, "release clears reset");
    issue("keyboard row", 0x03, 2, 0x0200, false, 0, true);
    const uint32_t row2 = (dut.keyboard_rows_st[(2 * 16) / 32] >> ((2 * 16) % 32)) & 0xffffu;
    require(row2 == 0x0200u, "keyboard row 2");
    issue("controller", 0x04, 0, 0x0005, false, 0, true);
    require((dut.controller_st & 0xff) == 0x0005, "controller port 0");
    const auto motion = issue("mouse", 0x0b, 0, 0x01ff, false, 0, true);
    require(motion.first == -1 && motion.second == 1, "mouse motion");

    dut.media_write_busy = 1;
    issue("begin while busy", 0x06, 0, 0xa000, true, 4, true);
    dut.media_write_busy = 0;
    require(dut.unit0_state_st == 1, "busy begin does not load");
    issue("short begin 0", 0x06, 0, 3, false, 0, true);
    issue("short begin 1", 0x06, 1, 0, false, 0, true);
    issue("short begin 2", 0x06, 2, 0, false, 0, true);
    issue("short begin rejected", 0x06, 3, 0, true, 3, true);
    issue("eject rejected begin", 0x0a, 0, 0, false, 0, true);
    require(dut.unit0_state_st == 1, "eject after rejected begin");

    issue("geometry begin 0", 0x06, 0, 0xa000, false, 0, true);
    require(dut.unit0_state_st == 2, "begin word 0 loads");
    issue("geometry begin 1", 0x06, 1, 0x0005, false, 0, true);
    issue("geometry begin 2", 0x06, 2, 0, false, 0, true);
    issue("geometry begin 3", 0x06, 3, 0, false, 0, true);
    issue("chunk offset 0", 0x07, 0, 0, false, 0, true);
    issue("chunk offset 1", 0x07, 1, 0, false, 0, true);
    issue("chunk length", 0x07, 2, 2, false, 0, true);
    dut.media_write_ready = 0;
    const uint32_t held = ack_bit();
    dut.gpo = ((held ^ 1u) << 31) | (0x08u << 24) | 0x3412u;
    for (int i = 0; i < 8; ++i) tick("backpressure hold");
    require(ack_bit() == held, "data acknowledged while the writer was stalled");
    dut.media_write_ready = 1;
    bool wrote = false;
    for (int i = 0; i < 8 && ack_bit() == held; ++i) {
        if (dut.media_write_enable_st == 3 && dut.media_write_data_st == 0x3412
            && dut.media_write_addr_st == 0)
            wrote = true;
        tick("backpressure release");
    }
    require(ack_bit() != held, "data did not acknowledge after the writer was ready");
    require(wrote, "accepted data word was not written");
    issue("menu opcode", 0x12, 0, 0, true, 1, true);
    issue("snapshot before ready", 0x0d, 0, 0, true, 4, true);
    issue("eject partial image", 0x0a, 0, 0, false, 0, true);
    require(dut.unit0_state_st == 1 && dut.unit0_size_st == 0, "final eject");

    std::cout << "mailbox direct compare: golden exchanges and Atari ST commands match\n";
    return 0;
}
