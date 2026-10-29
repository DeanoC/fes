#include "Vfes_link_sim.h"
#include "verilated.h"
#include <cstdint>
#include <deque>
#include <iostream>
#include <stdexcept>

static void require(bool ok, const char *why) {
    if (!ok) throw std::runtime_error(why);
}
int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vfes_link_sim dut;
    std::deque<std::pair<uint32_t, uint32_t>> expected;
    uint32_t count = 0, responses = 0, cycle = 0;
    auto tick = [&] {
        dut.clk = 0; dut.eval();
        if (dut.slot_reset) { expected.clear(); count = 0; }
        else {
            if (dut.req) {
                count += dut.persona_b ? 3 : 1;
                uint32_t word = dut.persona_b
                    ? 0x5a000000u ^ (uint32_t(dut.address) * 0x00110011u)
                    : 0xa5000000u ^ (uint32_t(dut.address) * 0x01010101u);
                expected.emplace_back(word ^ count ^ (dut.persona_b ? 0xc002b002u : 0xc001a001u), cycle+2);
            }
            if (dut.rsp_valid) {
                require(!expected.empty(), "unexpected response");
                require(expected.front().second == cycle, "response sampled at wrong clock edge");
                require(dut.rsp_data == expected.front().first, "persona ROM/counter/ID mismatch");
                expected.pop_front(); ++responses;
            }
            require(expected.empty() || expected.front().second > cycle, "missing due response");
        }
        if (dut.freeze_ack) {
            require(dut.slot_reset, "frozen slot not reset");
            require(dut.isolated_data == 0, "frozen output not clamped");
            require(!dut.req, "request while frozen");
        }
        dut.clk = 1; dut.eval(); ++cycle;
    };
    dut.reset_n = 0; dut.freeze_req = 1; dut.persona_b = 0;
    for (int n=0;n<4;++n) tick();
    dut.reset_n = 1;
    for (int n=0;n<5;++n) tick();
    require(dut.freeze_ack, "cold shell not frozen");
    for (int variant=0;variant<4;++variant) {
        dut.persona_b = variant & 1;
        auto before = dut.static_count;
        auto old_responses = responses;
        dut.freeze_req = 0;
        for (int n=0;n<91+variant;++n) tick();
        require(responses > old_responses+7, "persona made no progress");
        dut.freeze_req = 1;
        int wait = 0;
        while (!dut.freeze_ack && wait++<16) tick();
        require(dut.freeze_ack, "stop did not drain bounded pipeline");
        require(expected.empty(), "freeze acknowledged with pending response");
        auto retired = dut.retired;
        for (int n=0;n<8;++n) tick();
        require(dut.retired == retired, "retirement progressed while frozen");
        require(dut.static_count > before+90, "static state lost across persona switch");
    }
    std::cout << "PASS: A/B/A/B ROM+counter IDs, stop/drain/reset/clamp, static persistence; "
              << responses << " checked responses\n";
}
