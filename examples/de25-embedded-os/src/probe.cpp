// SPDX-License-Identifier: GPL-3.0-or-later
#include "probe.hpp"

namespace fes_probe {
namespace {
constexpr std::uint32_t fixed_mask =
    ~(gp::FesGpAckMask | gp::FesGpErrorMask | gp::FesGpResponseMask);
// Finite limits also terminate a frozen timer. The normal deadline wins when
// time progresses; backwards time is invalid. Callbacks themselves must return.
struct PollBudget {
    std::uint64_t last;
    std::uint64_t deadline;
    unsigned polls = 0;
    unsigned stalled = 0;
    bool step(const Port& port, std::uint64_t* now)
    {
        *now = port.now_us(port.context);
        if (*now < last || *now >= deadline || ++polls > 1000000u) return false;
        stalled = *now == last ? stalled + 1u : 0u;
        last = *now;
        return stalled <= 4096u;
    }
};

std::uint32_t field(std::uint32_t value, std::uint32_t mask)
{
    return value * (mask & (~mask + 1u));
}
}

Result Link::ambiguous(Result result)
{
    poisoned_ = true;
    identified_ = false;
    return result;
}

Result Link::exchange(std::uint8_t opcode, std::uint8_t index,
                      std::uint16_t argument, std::uint64_t deadline,
                      std::uint16_t* response)
{
    if (response == nullptr || opcode > 127u || !port_.read || !port_.write ||
        !port_.now_us || !port_.idle) return Result::invalid;
    if (poisoned_) return Result::poisoned;
    const std::uint64_t started = port_.now_us(port_.context);
    if (started >= deadline) return Result::timeout;
    // Preserve production's maximum 100 ms exchange. Subtraction avoids
    // overflow near the end of the clock's range.
    if (deadline - started > 100000u) deadline = started + 100000u;
    PollBudget budget{started, deadline};
    if (!initialized_) {
        // Reset-only probe: validate ACK=0 read-only before even the settled
        // write. Otherwise that write could itself become a command edge.
        bool have_sample = false;
        std::uint32_t previous = 0;
        std::uint64_t guard_start = started;
        for (;;) {
            std::uint64_t now;
            if (!budget.step(port_, &now)) return ambiguous(Result::timeout);
            std::uint32_t observed;
            if (!port_.read(port_.context, &observed)) return ambiguous(Result::io);
            if ((observed & fixed_mask) != gp::FesGpSignature ||
                (observed & gp::FesGpAckMask) != 0) return ambiguous(Result::protocol);
            if (have_sample && observed == previous && now - guard_start >= 1u) {
                if (!budget.step(port_, &now)) return ambiguous(Result::timeout);
                initialized_ = true;
                break;
            }
            if (!have_sample || observed != previous) guard_start = now;
            have_sample = true;
            previous = observed;
            port_.idle(port_.context);
        }
    }
    const std::uint32_t fields = field(opcode, gp::FesGpOpcodeMask) |
        field(index, gp::FesGpIndexMask) | field(argument, gp::FesGpArgumentMask);
    if (!port_.write(port_.context, fields | (toggle_ ? gp::FesGpRequestMask : 0u)))
        return Result::io;
    toggle_ = !toggle_;
    if (!port_.write(port_.context, fields | (toggle_ ? gp::FesGpRequestMask : 0u)))
        return ambiguous(Result::io);

    bool ack_seen = false;
    bool have_sample = false;
    std::uint32_t previous = 0;
    std::uint64_t guard_start = 0;
    for (;;) {
        std::uint64_t now;
        if (!budget.step(port_, &now)) return ambiguous(Result::timeout);
        if (ack_seen && now - guard_start < 1u) {
            port_.idle(port_.context);
            continue;
        }
        std::uint32_t observed = 0;
        if (!port_.read(port_.context, &observed)) return ambiguous(Result::io);
        if ((observed & fixed_mask) != gp::FesGpSignature)
            return ambiguous(Result::protocol);
        const bool ack = (observed & gp::FesGpAckMask) != 0;
        if (ack != toggle_) {
            if (ack_seen) return ambiguous(Result::protocol);
        } else if (!ack_seen) {
            ack_seen = true;
            if (!budget.step(port_, &guard_start)) return ambiguous(Result::timeout);
        } else {
            if (!budget.step(port_, &now)) return ambiguous(Result::timeout);
            if (have_sample && observed == previous) {
                *response = static_cast<std::uint16_t>(observed & gp::FesGpResponseMask);
                return (observed & gp::FesGpErrorMask) ? Result::rejected : Result::ok;
            }
            have_sample = true;
            previous = observed;
        }
        port_.idle(port_.context);
    }
}

Result Link::identify(const Identity& expected, std::uint64_t deadline)
{
    identified_ = false;
    Identity observed = {};
    for (unsigned i = 0; i < gp::FesGpIdentityWordCount; ++i) {
        const Result result = exchange(gp::FesGpOpcodeIdentity,
            static_cast<std::uint8_t>(i), 0, deadline, &observed.word[i]);
        if (result != Result::ok) return result;
    }
    for (unsigned i = 0; i < gp::FesGpIdentityWordCount; ++i)
        if (observed.word[i] != expected.word[i]) return Result::identity_mismatch;
    identified_ = true;
    return Result::ok;
}

Result Link::execution(bool release, std::uint64_t deadline)
{
    if (!identified_) return Result::not_identified;
    std::uint16_t response = 0;
    return exchange(gp::FesGpOpcodeGameplay, gp::FesGpControlIndex,
        release ? gp::FesGpGameplayRelease : gp::FesGpGameplayHoldReset,
        deadline, &response);
}

Result Link::buttons(std::uint16_t value, std::uint64_t deadline)
{
    if (!identified_) return Result::not_identified;
    if ((value & ~gp::FesGpButtonMask) != 0) return Result::invalid;
    std::uint16_t response = 0;
    return exchange(gp::FesGpOpcodeButtons, gp::FesGpControlIndex,
                    value, deadline, &response);
}
} // namespace fes_probe
