// SPDX-License-Identifier: MIT
#pragma once
#include <array>
#include <cstdint>
#include <map>
#include <ostream>
#include <stdexcept>

// Read after each running RTL CPU posedge. The probe latches D's pre-edge
// inputs on that edge. "Sole" means D could issue this uop if its x87
// hold were cleared. This is a local upper bound, not a measured speedup.
struct PentiumX87Stalls {
    std::uint64_t sampled = 0, busy = 0, busy_integer_issue = 0, held = 0;
    std::uint64_t discarded = 0, co_blocked = 0, sole = 0, sole_fwait = 0;
    std::uint64_t register_raw = 0, no_register_raw = 0;
    // Index flags in order: board latch, previous operation, pending no-wait
    // control, store waiting for S_RDY, FNSAVE register-read port alignment.
    // A store behind an older ESC can assert both store_value and prior_op.
    std::array<std::uint64_t, 32> sole_masks{};

    // Count only clocks with the prior-operation hold term asserted. The
    // active bit distinguishes two different instructions from a stale opcode.
    std::map<unsigned, std::uint64_t> prior_pairs;
    std::uint64_t prior_pair_unknown = 0;

    void reset() { *this = {}; }

    template<class Root> void sample(const Root *root) {
        observe(root->fes_i386_cpu__DOT__p5_x87__DOT__sample_bits,
                root->fes_i386_cpu__DOT__p5_x87__DOT__pair_bits);
    }

    // These non-popping D8/DC register forms both read ST(0) and ST(i).
    // FCOM/FCOMP, memory forms and stack-manipulating groups are excluded.
    static bool stable_register_arithmetic(unsigned opcode) {
        const unsigned esc = opcode >> 8, group = (opcode >> 3) & 7u;
        return (esc == 0 || esc == 4) && (opcode & 0xc0u) == 0xc0u
               && group != 2 && group != 3;
    }

    void observe(unsigned sample_bits, unsigned pair_bits) {
        const bool issued = sample_bits & 1u;
        const bool otherwise_ready = sample_bits & 2u;
        const bool discard = sample_bits & 4u;
        const bool hold = sample_bits & 8u;
        const bool unit_busy = sample_bits & 16u;
        const bool esc_or_wait = sample_bits & 32u;
        const bool fwait = sample_bits & 64u;
        const unsigned reasons = (sample_bits >> 7) & 31u;
        if (issued != (otherwise_ready && !hold) || (hold && (!esc_or_wait || !reasons)))
            throw std::logic_error("x87 probe disagrees with the sampled D issue or hold");
        ++sampled;
        busy += unit_busy;
        busy_integer_issue += unit_busy && issued && !esc_or_wait && !discard;
        if (!hold) return;
        ++held;
        if (discard) {
            ++discarded;
        } else if (!otherwise_ready) {
            ++co_blocked;
        } else {
            ++sole;
            sole_fwait += fwait;
            ++sole_masks[reasons];
            const unsigned older = (pair_bits >> 11) & 0x7ffu;
            const unsigned head = pair_bits & 0x7ffu;
            if (reasons & 2u) {
                if (pair_bits & (1u << 22)) ++prior_pairs[(older << 11) | head];
                else ++prior_pair_unknown;
            }
            if ((pair_bits & (1u << 22)) && stable_register_arithmetic(older)
                                         && stable_register_arithmetic(head)) {
                const unsigned dest = (older >> 8) == 0 ? 0 : older & 7u;
                if (dest == 0 || dest == (head & 7u)) ++register_raw;
                else ++no_register_raw;
            }
        }
    }

    void write_json(std::ostream &out) const {
        out << "{\"sampled_cpu_clocks\":" << sampled << ",\"x87_busy_clocks\":" << busy
            << ",\"busy_with_non_esc_issue\":" << busy_integer_issue
            << ",\"x87_hold_asserted\":" << held << ",\"hold_redirect_or_flush\":" << discarded
            << ",\"hold_with_another_issue_blocker\":" << co_blocked
            << ",\"sole_x87_issue_blocker\":" << sole << ",\"sole_with_wait_instruction\":" << sole_fwait
            << ",\"sole_stable_stack_reg_arith_raw\":" << register_raw
            << ",\"sole_stable_stack_reg_arith_no_raw\":" << no_register_raw
            << ",\"sole_register_raw_unclassified\":" << sole - register_raw - no_register_raw
            << ",\"sole_by_hold_term_set\":{";
        static constexpr const char *names[] = {
            "board_latch", "prior_operation", "pending_control", "store_value", "save_read"
        };
        bool first = true;
        for (unsigned mask = 1; mask < sole_masks.size(); ++mask) {
            if (!sole_masks[mask]) continue;
            if (!first) out << ',';
            first = false;
            out << '"';
            bool first_term = true;
            for (unsigned i = 0; i < 5; ++i) {
                if (!(mask & (1u << i))) continue;
                if (!first_term) out << '+';
                out << names[i];
                first_term = false;
            }
            out << "\":" << sole_masks[mask];
        }
        out << "},\"prior_pair_unknown\":" << prior_pair_unknown
            << ",\"prior_opcode_pairs\":[";
        first = true;
        for (const auto& entry : prior_pairs) {
            if (!first) out << ',';
            first = false;
            out << '[' << (entry.first >> 11) << ',' << (entry.first & 0x7ffu)
                << ',' << entry.second << ']';
        }
        out << "]}";
    }
};
