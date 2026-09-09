// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_gp.h"
#include "verilated.h"

#include <array>
#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <regex>
#include <sstream>
#include <string>
#include <vector>

namespace {

struct Exchange {
    std::string name;
    uint32_t fields;
    uint32_t request;
    uint32_t gpi;
};

[[noreturn]] void fail(const std::string &message) {
    std::cerr << "FES GP: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string &message) {
    if (!condition) fail(message);
}

std::string read_file(const char *path) {
    std::ifstream stream(path);
    require(bool(stream), std::string("cannot open fixture: ") + path);
    std::ostringstream contents;
    contents << stream.rdbuf();
    return contents.str();
}

std::vector<Exchange> read_fixture(const char *path, std::string &build_id,
                                   bool &initial_toggle) {
    const std::string json = read_file(path);
    std::smatch match;
    require(std::regex_search(json, match,
                              std::regex("\\\"initial_request_toggle\\\"\\s*:\\s*(true|false)")),
            "fixture lacks initial request state");
    initial_toggle = match[1] == "true";
    require(std::regex_search(json, match,
                              std::regex("\\\"build_id\\\"\\s*:\\s*\\\"([0-9a-f]{32})\\\"")),
            "fixture lacks a 128-bit lowercase build ID");
    build_id = match[1];

    const std::regex row(
        "\\{\\\"name\\\":\\\"([^\\\"]+)\\\",\\\"gpo\\\":\\[([0-9]+),([0-9]+)\\],"
        "\\\"gpi\\\":([0-9]+),\\\"data\\\":[0-9]+\\}");
    std::vector<Exchange> exchanges;
    for (auto it = std::sregex_iterator(json.begin(), json.end(), row);
         it != std::sregex_iterator(); ++it) {
        exchanges.push_back({(*it)[1], uint32_t(std::stoull((*it)[2])),
                             uint32_t(std::stoull((*it)[3])),
                             uint32_t(std::stoull((*it)[4]))});
    }
    require(exchanges.size() == 22, "fixture exchange count changed");
    return exchanges;
}

uint32_t hex_word(const std::string &text, size_t offset) {
    return uint32_t(std::stoul(text.substr(offset, 8), nullptr, 16));
}

struct Mailbox {
    Vfes_gp dut;

    Mailbox() { dut.clk = 0; dut.gpo = 0; dut.eval(); }

    void tick() {
        dut.clk = 1;
        dut.eval();
        dut.clk = 0;
        dut.eval();
    }

    void drive_after_edge(uint32_t value) {
        dut.clk = 1;
        dut.eval();
        dut.gpo = value;
        dut.eval();
        dut.clk = 0;
        dut.eval();
    }

    void wait_for_ack(bool toggle, uint32_t expected, const std::string &name) {
        for (unsigned cycle = 0; cycle != 8; ++cycle) {
            if (((uint32_t(dut.gpi) >> 23) & 1u) == unsigned(toggle)) {
                require(uint32_t(dut.gpi) == expected, name + ": response mismatch");
                return;
            }
            tick();
        }
        fail(name + ": acknowledgement timeout");
    }
};

uint32_t response(bool toggle, bool error, uint16_t data) {
    return 0xf5000000u | (toggle ? 0x00800000u : 0u) |
           (error ? 0x00400000u : 0u) | data;
}

uint32_t command(bool toggle, uint8_t opcode, uint8_t index, uint16_t argument) {
    return (toggle ? 0x80000000u : 0u) | (uint32_t(opcode) << 24) |
           (uint32_t(index) << 16) | argument;
}

void exchange(Mailbox &mailbox, bool &toggle, uint8_t opcode, uint8_t index,
              uint16_t argument, uint32_t expected, const std::string &name) {
    const uint32_t fields = command(toggle, opcode, index, argument);
    const uint32_t before = mailbox.dut.gpi;
    const uint8_t reset_before = mailbox.dut.game_reset;
    const uint8_t buttons_before = mailbox.dut.buttons;
    mailbox.dut.gpo = fields;
    mailbox.dut.eval();
    mailbox.tick();
    require(uint32_t(mailbox.dut.gpi) == before, name + ": fields executed before toggle");
    require(mailbox.dut.game_reset == reset_before && mailbox.dut.buttons == buttons_before,
            name + ": side effect before toggle");
    toggle = !toggle;
    mailbox.dut.gpo = command(toggle, opcode, index, argument);
    mailbox.dut.eval();
    mailbox.wait_for_ack(toggle, expected, name);
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    require(argc == 2, "usage: Vfes_gp EXCHANGES_JSON");
    std::string build_id;
    bool toggle = false;
    const auto exchanges = read_fixture(argv[1], build_id, toggle);
    Mailbox mailbox;
    mailbox.dut.build_id[3] = hex_word(build_id, 0);
    mailbox.dut.build_id[2] = hex_word(build_id, 8);
    mailbox.dut.build_id[1] = hex_word(build_id, 16);
    mailbox.dut.build_id[0] = hex_word(build_id, 24);
    mailbox.dut.eval();

    require(!toggle, "fixture must begin at request toggle zero");
    require(uint32_t(mailbox.dut.gpi) == 0xf5000000u,
            "initial signature/ACK/response");
    require(mailbox.dut.game_reset && mailbox.dut.buttons == 0,
            "initial gameplay reset and neutral buttons");

    for (size_t index = 0; index != exchanges.size(); ++index) {
        const auto &item = exchanges[index];
        require(bool((item.fields >> 31) & 1u) == toggle,
                item.name + ": field word does not retain previous toggle");
        require(item.request == (item.fields ^ 0x80000000u),
                item.name + ": request must change only the toggle");
        const uint32_t previous_gpi = mailbox.dut.gpi;
        const uint8_t previous_reset = mailbox.dut.game_reset;
        const uint8_t previous_buttons = mailbox.dut.buttons;
        mailbox.dut.gpo = item.fields;
        mailbox.dut.eval();
        for (unsigned settle = 0; settle != 1 + index % 4; ++settle) mailbox.tick();
        require(uint32_t(mailbox.dut.gpi) == previous_gpi,
                item.name + ": duplicate-toggle fields replayed a command");
        require(mailbox.dut.game_reset == previous_reset &&
                    mailbox.dut.buttons == previous_buttons,
                item.name + ": duplicate-toggle fields changed gameplay");

        toggle = !toggle;
        require(bool((item.request >> 31) & 1u) == toggle,
                item.name + ": contiguous request toggle mismatch");
        if (index % 2 == 0) {
            mailbox.dut.gpo = item.request;
            mailbox.dut.eval();
        } else {
            mailbox.drive_after_edge(item.request);
        }
        mailbox.wait_for_ack(toggle, item.gpi, item.name);
        require(bool((uint32_t(mailbox.dut.gpi) >> 23) & 1u) == toggle,
                item.name + ": ACK differs from request toggle");
    }

    require(mailbox.dut.game_reset && mailbox.dut.buttons == 0,
            "fixture must finish held in reset with neutral buttons");

    exchange(mailbox, toggle, 2, 0, 1, response(!toggle, false, 0),
             "gameplay release");
    require(!mailbox.dut.game_reset, "release did not clear gameplay reset");
    exchange(mailbox, toggle, 3, 0, 0x00a5, response(!toggle, false, 0x00a5),
             "set buttons");
    require(mailbox.dut.buttons == 0xa5, "accepted buttons not applied");

    mailbox.dut.gpo = command(toggle, 3, 0, 0x005a);
    for (unsigned cycle = 0; cycle != 6; ++cycle) mailbox.tick();
    require(mailbox.dut.buttons == 0xa5,
            "unchanged toggle replayed changed button fields");
    exchange(mailbox, toggle, 3, 0, 0x0100,
             response(!toggle, true, 3), "reject high button bits");
    require(mailbox.dut.buttons == 0xa5,
            "malformed buttons had a gameplay side effect");
    exchange(mailbox, toggle, 2, 0, 0,
             response(!toggle, false, 0), "hold gameplay reset");
    require(mailbox.dut.game_reset && mailbox.dut.buttons == 0,
            "hold reset did not clear gameplay input");
    mailbox.dut.gpo = command(toggle, 2, 0, 1);
    for (unsigned cycle = 0; cycle != 6; ++cycle) mailbox.tick();
    require(mailbox.dut.game_reset,
            "reset command changed mailbox semantics on duplicate toggle");

    std::cout << "FES GP: stateful golden fixture, CDC settling, one-toggle effects, errors and reset passed\n";
    return EXIT_SUCCESS;
}
