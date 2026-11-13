// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/de25_pr_control.hpp"
#include "de25_pr_endpoint.hpp"
#include "native/generated/fes_de25_pr_control.hpp"
#include "native/hardware.hpp"
#include "native/linux/mmio.hpp"

#include <cassert>
#include <cstdio>
#include <functional>
#include <map>
#include <stdexcept>
#include <string>
#include <vector>

namespace {
using namespace mister;
using namespace mister::native;
using namespace mister::native::generated;
using namespace mister_test::de25_pr;

class TestClock final : public Clock {
public:
	std::uint64_t NowMs() const override
	{
		++calls;
		const auto value = now;
		now += step;
		return value;
	}
	mutable std::uint64_t now = 100;
	mutable unsigned calls = 0;
	std::uint64_t step = 1;
};


void Identify(De25PrControl& controller, TestClock& clock)
{
	De25PrIdentity identity;
	const auto error = controller.Identify(kBuild, clock.now + 10000, &identity);
	if (!error.ok()) std::fprintf(stderr, "Identify failed: %s\n", error.message.c_str());
	assert(error.ok());
	assert(identity.build_id == kBuild);
}

void AssertPoisonedAndQuiet(De25PrControl& controller, Endpoint& endpoint, TestClock& clock)
{
	assert(controller.Poisoned());
	const auto count = endpoint.writes.size();
	assert(!controller.Freeze(clock.now + 10000).ok());
	assert(!controller.Reconfigure(FesDe25PrControlPersonaA, [] { return Error{}; }, clock.now + 10000).ok());
	De25PrIdentity identity;
	assert(!controller.Identify(kBuild, clock.now + 10000, &identity).ok());
	assert(endpoint.writes.size() == count);
}

void TestNormalSequence(unsigned latency)
{
	Endpoint endpoint;
	endpoint.latency = latency;
	TestClock clock;
	De25PrControl controller(endpoint, clock);
	De25PrIdentity identity;
	assert(controller.Identify("112233445566778899AABBCCDDEEFF00", 10000, &identity).ok());
	assert(identity.frozen && identity.completed_sequence == 0 && identity.persona_id == 0);
	for (const auto persona : {FesDe25PrControlPersonaA, FesDe25PrControlPersonaB, FesDe25PrControlPersonaA}) {
		unsigned calls = 0;
		const auto error = controller.Reconfigure(persona, [&] {
			assert(endpoint.registers[FesDe25PrControlStatusOffset] == kReadyFrozen);
			assert(!endpoint.pending);
			++calls;
			endpoint.loaded_persona = persona;
			return Error{};
		}, clock.now + 10000);
		assert(error.ok() && calls == 1 && !controller.Poisoned());
		assert(endpoint.registers[FesDe25PrControlStatusOffset] == kReadyRunning);
		assert(endpoint.registers[FesDe25PrControlPersonaIdOffset] == persona);
	}
	assert(endpoint.Commands(FesDe25PrControlCommandFreeze) == 3);
	assert(endpoint.Commands(FesDe25PrControlCommandResume) == 3);
	assert(endpoint.registers[FesDe25PrControlCompletedSeqOffset] == 6);
	assert(controller.Freeze(clock.now + 10000).ok());
	assert(endpoint.registers[FesDe25PrControlStatusOffset] == kReadyFrozen);
}

void TestIdentityAndArgumentGuards()
{
	Endpoint endpoint;
	TestClock clock;
	De25PrControl controller(endpoint, clock);
	assert(!controller.Reconfigure(FesDe25PrControlPersonaA, [] { return Error{}; }, 10000).ok());
	assert(!controller.Freeze(10000).ok() && endpoint.writes.empty());
	De25PrIdentity identity;
	for (const auto* bad : {"", "00000000000000000000000000000000", "112233445566778899aabbccddeeff0z"})
		assert(!controller.Identify(bad, 10000, &identity).ok());
	assert(!controller.Identify(kBuild, 10000, nullptr).ok());
	Identify(controller, clock);
	assert(!controller.Reconfigure(0, [] { return Error{}; }, 10000).ok());
	assert(!controller.Reconfigure(FesDe25PrControlPersonaA, {}, 10000).ok());
	assert(!controller.Poisoned() && endpoint.writes.empty());
	for (const auto offset : {FesDe25PrControlIdOffset, FesDe25PrControlVersionOffset,
		FesDe25PrControlBuildId0Offset, FesDe25PrControlBuildId1Offset,
		FesDe25PrControlBuildId2Offset, FesDe25PrControlBuildId3Offset,
		FesDe25PrControlTimeoutCyclesOffset}) {
		Endpoint wrong;
		TestClock now;
		De25PrControl bad(wrong, now);
		wrong.registers[offset] ^= 1;
		assert(!bad.Identify(kBuild, 10000, &identity).ok());
		AssertPoisonedAndQuiet(bad, wrong, now);
	}
}

void TestStatusAndOwnershipGuards()
{
	for (const auto status : {FesDe25PrControlStatusFrozen, kReadyFrozen | 0x80000000u,
		kReadyFrozen | FesDe25PrControlStatusRunning,
		kReadyFrozen | FesDe25PrControlStatusBusy, kReadyRunning | FesDe25PrControlStatusBusy,
		FesDe25PrControlStatusReady,
		FesDe25PrControlStatusReady | FesDe25PrControlStatusBusy,
		FesDe25PrControlStatusReady | FesDe25PrControlStatusFault}) {
		Endpoint endpoint;
		TestClock clock;
		De25PrControl controller(endpoint, clock);
		endpoint.registers[FesDe25PrControlStatusOffset] = status;
		De25PrIdentity identity;
		assert(!controller.Identify(kBuild, 10000, &identity).ok());
		AssertPoisonedAndQuiet(controller, endpoint, clock);
	}
	for (const auto mutation : {0, 1, 2, 3}) {
		Endpoint endpoint;
		TestClock clock;
		De25PrControl controller(endpoint, clock);
		Identify(controller, clock);
		if (mutation == 0) endpoint.registers[FesDe25PrControlCompletedSeqOffset] = 7;
		if (mutation == 1) endpoint.registers[FesDe25PrControlCompletedSeqOffset] = 0x1000000;
		if (mutation == 2) endpoint.registers[FesDe25PrControlFaultOffset] = FesDe25PrControlFaultReset;
		if (mutation == 3) endpoint.registers[FesDe25PrControlBuildId3Offset] ^= 1;
		assert(!controller.Freeze(clock.now + 10000).ok());
		assert(endpoint.writes.empty());
		AssertPoisonedAndQuiet(controller, endpoint, clock);
	}
	{
		Endpoint endpoint;
		TestClock clock;
		De25PrControl controller(endpoint, clock);
		Identify(controller, clock);
		endpoint.registers[FesDe25PrControlCompletedSeqOffset] = 2;
		De25PrIdentity identity;
		assert(!controller.Identify(kBuild, clock.now + 10000, &identity).ok());
		AssertPoisonedAndQuiet(controller, endpoint, clock);
	}
}

void TestCommandUncertainty()
{
	for (const auto mutation : {0, 1, 2, 3, 4, 5, 6}) {
		Endpoint endpoint;
		TestClock clock;
		De25PrControl controller(endpoint, clock);
		Identify(controller, clock);
		if (mutation == 0) endpoint.fail_write = FesDe25PrControlArgumentOffset;
		if (mutation == 1) endpoint.fail_write = FesDe25PrControlCommandOffset;
		if (mutation == 2) { endpoint.fail_write = FesDe25PrControlCommandOffset; endpoint.accept_failed_write = true; }
		if (mutation == 3) endpoint.wrong_sequence = true;
		if (mutation == 4) endpoint.busy_ack = true;
		if (mutation == 5) endpoint.fail_read = FesDe25PrControlStatusOffset;
		if (mutation == 6) endpoint.on_read = [&](std::uint32_t) {
			if (endpoint.staged) endpoint.registers[FesDe25PrControlFaultOffset] = FesDe25PrControlFaultPartial;
		};
		unsigned calls = 0;
		assert(!controller.Reconfigure(FesDe25PrControlPersonaA, [&] { ++calls; return Error{}; }, clock.now + 10000).ok());
		assert(calls == 0 && endpoint.Commands(FesDe25PrControlCommandResume) == 0);
		assert(endpoint.Commands(FesDe25PrControlCommandFreeze) <= 1);
		AssertPoisonedAndQuiet(controller, endpoint, clock);
	}
}

void TestConfigurationFailureAndIdentity()
{
	for (const auto mutation : {0, 1, 2, 3, 4, 5}) {
		Endpoint endpoint;
		TestClock clock;
		De25PrControl controller(endpoint, clock);
		Identify(controller, clock);
		unsigned calls = 0;
		const auto result = controller.Reconfigure(FesDe25PrControlPersonaB, [&]() -> Error {
			++calls;
			assert(endpoint.registers[FesDe25PrControlStatusOffset] == kReadyFrozen);
			if (mutation == 0) {
				endpoint.registers[FesDe25PrControlStatusOffset] = kReadyRunning;
				return {ErrorCode::program_failed, "failed after an unknown hardware transition"};
			}
			if (mutation == 1) throw std::runtime_error("configuration failure");
			if (mutation == 2) endpoint.registers[FesDe25PrControlBuildId0Offset] ^= 1;
			if (mutation == 3) endpoint.registers[FesDe25PrControlCompletedSeqOffset] += 1;
			if (mutation == 4) endpoint.registers[FesDe25PrControlStatusOffset] = kReadyRunning;
			if (mutation == 5) endpoint.registers[FesDe25PrControlFaultOffset] = FesDe25PrControlFaultReset;
			return {};
		}, clock.now + 10000);
		assert(!result.ok() && calls == 1);
		assert(endpoint.Commands(FesDe25PrControlCommandResume) == 0);
		AssertPoisonedAndQuiet(controller, endpoint, clock);
	}
	for (bool wrong_state : {false, true}) {
		Endpoint endpoint;
		TestClock clock;
		De25PrControl controller(endpoint, clock);
		Identify(controller, clock);
		endpoint.wrong_state = wrong_state;
		assert(!controller.Reconfigure(FesDe25PrControlPersonaB, [&] {
			if (wrong_state) endpoint.loaded_persona = FesDe25PrControlPersonaB;
			return Error{};
		}, clock.now + 10000).ok());
		assert(endpoint.Commands(FesDe25PrControlCommandResume) == 1);
		AssertPoisonedAndQuiet(controller, endpoint, clock);
	}
}

void TestSequenceExhaustion()
{
	Endpoint endpoint;
	TestClock clock;
	De25PrControl controller(endpoint, clock);
	endpoint.registers[FesDe25PrControlCompletedSeqOffset] = FesDe25PrControlSequenceMask;
	Identify(controller, clock);
	assert(!controller.Freeze(clock.now + 10000).ok() && endpoint.writes.empty());
	AssertPoisonedAndQuiet(controller, endpoint, clock);
}

void TestDeadlinesAndClockRegressions()
{
	for (const auto mutation : {0, 1, 2, 3}) {
		Endpoint endpoint;
		TestClock clock;
		De25PrControl controller(endpoint, clock);
		Identify(controller, clock);
		if (mutation == 0) clock.now = 0;
		if (mutation == 1 || mutation == 2) endpoint.stall = true;
		if (mutation == 2) clock.step = 0;
		const auto deadline = clock.now + (mutation == 2 ? 5 : 150);
		unsigned calls = 0;
		const auto result = controller.Reconfigure(FesDe25PrControlPersonaA, [&] {
			++calls;
			if (mutation == 3) clock.now = deadline;
			return Error{};
		}, deadline);
		assert(!result.ok());
		assert(calls == (mutation == 3 ? 1u : 0u));
		assert(endpoint.Commands(FesDe25PrControlCommandResume) == 0);
		AssertPoisonedAndQuiet(controller, endpoint, clock);
	}
}
} // namespace

int main()
{
	for (unsigned latency : {0u, 1u, 3u, 20u}) TestNormalSequence(latency);
	TestIdentityAndArgumentGuards();
	TestStatusAndOwnershipGuards();
	TestCommandUncertainty();
	TestConfigurationFailureAndIdentity();
	TestSequenceExhaustion();
	TestDeadlinesAndClockRegressions();
	std::puts("PASS: DE25 PR identity, freeze/configure/resume, sequencing and terminal-failure controls");
}
