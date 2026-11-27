// SPDX-License-Identifier: MIT
#include "x87_stalls.h"
#include <cassert>
#include <sstream>

static constexpr unsigned issue = 1, ready = 2, flush = 4, hold = 8;
static constexpr unsigned busy = 16, esc = 32, wait_insn = 64;
static constexpr unsigned board = 128, older = 256, control = 512, store = 1024, save = 2048;

static unsigned pair(unsigned producer, unsigned consumer) {
    return (1u << 22) | (producer << 11) | consumer;
}

int main() {
    PentiumX87Stalls profile;
    // D8 C1: FADD ST(0),ST(1); DC C1: FADD ST(1),ST(0).
    const unsigned issue_hold = ready | hold | esc;
    profile.observe(issue_hold | older, pair(0x0c1, 0x4ca)); // ST(0) RAW
    profile.observe(issue_hold | older, pair(0x4c1, 0x0e1)); // ST(1) RAW
    profile.observe(issue_hold | older, pair(0x4c1, 0x4ca)); // ST(1) -> ST(0),ST(2)
    profile.observe(issue_hold | older | store, pair(0x4c1, 0x114)); // D9 mem
    profile.observe(issue_hold | older, pair(0x4c1, 0x6c1)); // popping DE arithmetic
    profile.observe(issue_hold | older, pair(0x4c1, 0x0d1)); // register FCOM
    profile.observe(issue_hold | older, pair(0x4c1, 0x0c1) & ~(1u << 22)); // stale cur
    profile.observe(issue_hold | control | board | wait_insn, 0);
    profile.observe(issue_hold | save, 0);
    profile.observe(hold | esc | older | busy, pair(0x4c1, 0x4ca)); // another D blocker
    profile.observe(flush | hold | esc | older, 0);
    profile.observe(issue | ready | busy, 0); // integer uop while x87 works
    profile.observe(issue | ready | busy | flush, 0);
    profile.observe(issue | ready | busy | esc, 0);
    profile.observe(0, 0);

    assert(profile.sampled == 15 && profile.busy == 4 && profile.busy_integer_issue == 1);
    assert(profile.held == 11 && profile.sole == 9 && profile.sole_fwait == 1);
    assert(profile.co_blocked == 1 && profile.discarded == 1);
    assert(profile.register_raw == 2 && profile.no_register_raw == 1);
    std::ostringstream json;
    profile.write_json(json);
    assert(json.str().find("\"prior_operation+store_value\":1") != std::string::npos);
    assert(json.str().find("\"board_latch+pending_control\":1") != std::string::npos);
    assert(json.str().find("\"sole_register_raw_unclassified\":6") != std::string::npos);
    for (unsigned invalid : {ready, issue | hold | ready, hold | esc, hold | older}) {
        bool caught = false;
        try { profile.observe(invalid, 0); } catch (const std::logic_error &) { caught = true; }
        assert(caught);
    }
    profile.reset();
    struct Root {
        unsigned fes_i386_cpu__DOT__p5_x87__DOT__sample_bits = issue_hold | older;
        unsigned fes_i386_cpu__DOT__p5_x87__DOT__pair_bits = pair(0x4c1, 0x4ca);
    } root;
    profile.sample(&root);
    assert(profile.sole == 1 && profile.no_register_raw == 1 && profile.register_raw == 0);
}
