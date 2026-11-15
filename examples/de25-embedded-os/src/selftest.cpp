// SPDX-License-Identifier: GPL-3.0-or-later
#include "probe.hpp"

namespace fes_probe {
namespace {
enum class Fault { none, late_ack, timeout, reject, bad_signature, revert_ack,
                   unstable, read_error, toggle_write_error };

Identity fixture_identity()
{
    Identity identity = {};
    identity.word[gp::FesGpIdentityMagic0Index] = gp::FesGpIdentityMagic0;
    identity.word[gp::FesGpIdentityMagic1Index] = gp::FesGpIdentityMagic1;
    identity.word[gp::FesGpIdentityTransportMajorIndex] = gp::FesGpTransportMajor;
    identity.word[gp::FesGpIdentityTransportMinorIndex] = gp::FesGpTransportMinor;
    identity.word[gp::FesGpIdentityAbiTagIndex] = gp::FesGpAbiTag;
    identity.word[gp::FesGpIdentityAbiMajorIndex] = gp::FesGpAbiMajor;
    identity.word[gp::FesGpIdentityAbiMinorIndex] = gp::FesGpAbiMinor;
    identity.word[gp::FesGpIdentityCapabilitiesIndex] =
        gp::FesGpCapabilityGamepad | gp::FesGpCapabilityVideoFixed720p60;
    // Synthetic expected build identity, not an admitted package receipt.
    for (unsigned i = 0; i < 8; ++i)
        identity.word[gp::FesGpIdentityBuildIDStartIndex + i] = 0x1000u + i;
    return identity;
}

// This emulates only the GP register responder. It is not a DE25 FPGA model.
struct Model {
    Fault fault = Fault::none;
    Identity identity = fixture_identity();
    std::uint64_t virtual_us = 0;
    std::uint64_t (*clock)() = nullptr;
    void (*pause)() = nullptr;
    std::uint32_t response = gp::FesGpSignature;
    std::uint32_t first_writes[2] = {};
    unsigned writes = 0, commands = 0, reads = 0;
    bool ack = false, running = false;
    std::uint16_t buttons = 0;
    std::uint64_t first_ack_us = 0, confirmed_us = 0;
    unsigned ack_samples = 0;
    bool freeze_before_write = false, freeze_after_write = false;
    bool backwards_before_write = false, backwards_after_write = false;
    std::uint64_t frozen_us = 0;

    static bool read(void* context, std::uint32_t* value)
    {
        Model& m = *static_cast<Model*>(context);
        ++m.reads;
        if (m.commands && m.fault == Fault::read_error) return false;
        *value = m.response;
        if (m.commands) {
            if (m.fault == Fault::bad_signature) *value ^= 0x01000000u;
            if (m.fault == Fault::timeout ||
                (m.fault == Fault::late_ack && m.reads <= 3) ||
                (m.fault == Fault::revert_ack && m.reads > 1))
                *value ^= gp::FesGpAckMask;
            if (m.fault == Fault::unstable) *value ^= (m.reads & 1u);
        }
        if (((*value & gp::FesGpAckMask) != 0) == m.ack) {
            if (m.ack_samples == 0) m.first_ack_us = now(context);
            else if (m.ack_samples == 1) m.confirmed_us = now(context);
            ++m.ack_samples;
        }
        return true;
    }
    static bool write(void* context, std::uint32_t value)
    {
        Model& m = *static_cast<Model*>(context);
        if (m.writes < 2) m.first_writes[m.writes] = value;
        ++m.writes;
        if (m.fault == Fault::toggle_write_error && m.writes == 2) return false;
        const bool requested = (value & gp::FesGpRequestMask) != 0;
        if (requested == m.ack) return true;
        m.ack = requested;
        if (m.freeze_after_write || m.backwards_after_write) m.frozen_us = now(&m);
        ++m.commands;
        m.reads = 0;
        m.ack_samples = 0;
        const unsigned opcode = (value & gp::FesGpOpcodeMask) >> 24;
        const unsigned index = (value & gp::FesGpIndexMask) >> 16;
        const std::uint16_t argument = value & gp::FesGpArgumentMask;
        std::uint16_t payload = 0;
        bool error = m.fault == Fault::reject;
        if (opcode == gp::FesGpOpcodeIdentity && index < gp::FesGpIdentityWordCount)
            payload = m.identity.word[index];
        else if (opcode == gp::FesGpOpcodeGameplay)
            m.running = argument == gp::FesGpGameplayRelease;
        else if (opcode == gp::FesGpOpcodeButtons)
            m.buttons = argument;
        else error = true;
        if (error) payload = gp::FesGpErrorInvalidOpcode;
        m.response = gp::FesGpSignature | (m.ack ? gp::FesGpAckMask : 0u) |
            (error ? gp::FesGpErrorMask : 0u) | payload;
        return true;
    }
    static std::uint64_t now(void* context)
    {
        Model& m = *static_cast<Model*>(context);
        if (m.freeze_before_write || (m.commands && m.freeze_after_write)) return m.frozen_us;
        if (m.backwards_before_write || (m.commands && m.backwards_after_write)) return m.frozen_us--;
        return m.clock ? m.clock() : m.virtual_us;
    }
    static void idle(void* context)
    {
        Model& m = *static_cast<Model*>(context);
        if (m.pause) m.pause();
        else ++m.virtual_us;
    }
    Port port() { return {this, read, write, now, idle}; }
    std::uint64_t deadline() { return now(this) + 20000u; }
};
}

unsigned selftest(Report report, bool* passed, std::uint64_t (*clock_us)(),
                  void (*idle)())
{
    unsigned count = 0;
    *passed = true;
    auto check = [&](const char* name, bool ok) {
        ++count;
        *passed = *passed && ok;
        report(name, ok);
    };
    auto model = [&]() {
        Model m;
        m.clock = clock_us;
        m.pause = idle;
        return m;
    };
    const Identity expected = fixture_identity();
    {
        Model m = model();
        Link link(m.port());
        check("identity-before-release", link.execution(true, m.deadline()) ==
            Result::not_identified && m.writes == 0);
        bool ok = link.identify(expected, m.deadline()) == Result::ok &&
            m.commands == 16;
        ok = ok && link.execution(true, m.deadline()) == Result::ok && m.running;
        ok = ok && link.buttons(gp::FesGpButtonA, m.deadline()) == Result::ok &&
            m.buttons == gp::FesGpButtonA;
        ok = ok && link.buttons(0, m.deadline()) == Result::ok && m.buttons == 0;
        ok = ok && link.execution(false, m.deadline()) == Result::ok && !m.running;
        check("identity-release-input-neutralize-hold", ok && m.commands == 20);
    }
    {
        Model m = model();
        Link link(m.port());
        std::uint16_t response = 0;
        const Result result = link.exchange(gp::FesGpOpcodeIdentity, 8, 0,
                                           m.deadline(), &response);
        check("settled-fields-then-toggle-wire-vector", result == Result::ok &&
            response == 0x1000u && m.first_writes[0] == 0x01080000u &&
            m.first_writes[1] == 0x81080000u && m.writes == 2 && m.reads >= 3 &&
            m.confirmed_us - m.first_ack_us >= 1u);
    }
    {
        Model m = model();
        m.identity.word[gp::FesGpIdentityBuildIDStartIndex + 7] ^= 1u;
        Link link(m.port());
        check("build-identity-mismatch-blocks-release", link.identify(expected,
            m.deadline()) == Result::identity_mismatch &&
            link.execution(true, m.deadline()) == Result::not_identified &&
            m.commands == 16 && !m.running);
    }
    {
        bool ok = true;
        for (unsigned word = 0; word < gp::FesGpIdentityWordCount; ++word) {
            Model m = model();
            m.identity.word[word] ^= 1u;
            Link link(m.port());
            ok = ok && link.identify(expected, m.deadline()) == Result::identity_mismatch &&
                link.execution(true, m.deadline()) == Result::not_identified && !m.running;
        }
        check("every-identity-word-is-checked", ok);
    }
    const Fault faults[] = {Fault::late_ack, Fault::timeout, Fault::reject,
        Fault::bad_signature, Fault::revert_ack, Fault::unstable,
        Fault::read_error, Fault::toggle_write_error};
    const Result results[] = {Result::ok, Result::timeout, Result::rejected,
        Result::protocol, Result::protocol, Result::timeout,
        Result::io, Result::io};
    const char* names[] = {"late-ack-no-replay", "timeout-poisons-no-replay",
        "completed-rejection-preserves-toggle", "invalid-signature-poisons",
        "reverted-ack-poisons", "unstable-payload-times-out",
        "read-failure-poisons", "toggle-write-failure-poisons"};
    for (unsigned i = 0; i < sizeof(faults) / sizeof(faults[0]); ++i) {
        Model m = model();
        m.fault = faults[i];
        Link link(m.port());
        std::uint16_t response = 0;
        const Result result = link.exchange(gp::FesGpOpcodeIdentity, 0, 0,
                                           m.deadline(), &response);
        const unsigned writes = m.writes;
        const bool ambiguous = results[i] != Result::ok && results[i] != Result::rejected;
        bool ok = result == results[i] && link.poisoned() == ambiguous && writes == 2;
        m.fault = Fault::none;
        const Result next = link.exchange(gp::FesGpOpcodeIdentity, 1, 0,
                                         m.deadline(), &response);
        ok = ok && (ambiguous ? next == Result::poisoned && m.writes == writes :
                               next == Result::ok && m.writes == writes + 2);
        check(names[i], ok);
    }
    {
        Model m = model();
        Link link(m.port());
        std::uint16_t response = 0;
        check("expired-deadline-does-not-write", link.exchange(1, 0, 0,
            Model::now(&m), &response) == Result::timeout && m.writes == 0);
    }
    {
        Model m = model();
        Link link(m.port());
        std::uint16_t response = 0;
        check("invalid-request-does-not-write", link.exchange(128, 0, 0,
            m.deadline(), &response) == Result::invalid &&
            link.exchange(1, 0, 0, m.deadline(), nullptr) == Result::invalid && m.writes == 0);
    }
    {
        Model m = model();
        m.fault = Fault::timeout;
        Link link(m.port());
        const std::uint64_t before = Model::now(&m);
        std::uint16_t response = 0;
        const Result result = link.exchange(1, 0, 0, before + 1000000u, &response);
        const std::uint64_t elapsed = Model::now(&m) - before;
        check("long-deadline-is-capped-at-100ms", result == Result::timeout &&
            elapsed >= 100000u && elapsed < 200000u && m.writes == 2);
    }
    {
        Model m = model();
        m.ack = true;
        m.response |= gp::FesGpAckMask;
        Link link(m.port());
        std::uint16_t response = 0;
        check("initial-ack-one-rejected-without-writes", link.exchange(1, 0, 0,
            m.deadline(), &response) == Result::protocol && m.writes == 0 && m.commands == 0);
    }
    {
        Model m = model();
        m.response ^= 0x01000000u;
        Link link(m.port());
        std::uint16_t response = 0;
        check("invalid-initial-response-rejected-without-writes", link.exchange(1, 0, 0,
            m.deadline(), &response) == Result::protocol && m.writes == 0);
    }
    for (unsigned kind = 0; kind < 4; ++kind) {
        Model m = model();
        m.frozen_us = 100000u;
        m.freeze_before_write = kind == 0;
        m.backwards_before_write = kind == 1;
        m.freeze_after_write = kind == 2;
        m.backwards_after_write = kind == 3;
        Link link(m.port());
        std::uint16_t response = 0;
        const Result result = link.exchange(1, 0, 0, 2000000u, &response);
        const unsigned writes = m.writes;
        const char* names[] = {"flat-initial-clock-no-write", "backwards-initial-clock-no-write",
            "flat-ack-guard-poisons", "backwards-ack-guard-poisons"};
        check(names[kind], result == Result::timeout && link.poisoned() &&
            writes == (kind < 2 ? 0u : 2u) &&
            link.exchange(1, 0, 0, 2000000u, &response) == Result::poisoned && m.writes == writes);
    }
    return count;
}
} // namespace fes_probe
