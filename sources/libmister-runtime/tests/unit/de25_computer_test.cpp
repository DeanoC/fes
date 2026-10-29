// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/de25_computer.hpp"
#include "native/generated/fes_de25_computer.hpp"
#include "native/generated/fes_computer.hpp"
#include "native/hardware.hpp"
#include "native/linux/mmio.hpp"
#include <algorithm>
#include <array>
#include <atomic>
#include <thread>
#include <cassert>
#include <cstdint>
#include <cstdio>
#include <string>
#include <vector>

using namespace mister;
using namespace mister::native;
using namespace mister::native::generated;
namespace {
const std::string kBuild = "00112233445566778899aabbccddeeff";
class TestClock final : public Clock {
public:
	mutable std::uint64_t time = 1;
	bool frozen = false, backwards = false;
	std::uint64_t NowMs() const override {
		if (backwards && time > 1) return --time;
		return frozen ? time : time++;
	}
};
class Bridge final : public Mmio {
public:
	std::vector<std::uint32_t> writes;
	std::array<std::uint16_t, 16> identity{{0x4546, 0x3153, 1, 0, 4, 1, 0, 0x803,
		0x1100, 0x3322, 0x5544, 0x7766, 0x9988, 0xbbaa, 0xddcc, 0xffee}};
	std::uint32_t gpo = 0, gpi = 0xf5000000, fault = 0;
	std::uint32_t bridge_id = FesDe25ComputerIdentity, version = FesDe25ComputerVersion;
	std::uint32_t minimum = 1, maximum = 16384;
	std::uint16_t state = FesComputerMediaStateEmpty, max_chunk = 512;
	std::uint16_t reject_next = 0, success_data = 0;
	bool slow_ack = false;
	bool pending = false, no_ack = false, read_error = false, write_error = false;
	bool change_payload = false, fault_after_ack = false, bad_signature = false, reserved = false;
	bool busy_after_ack = false, busy_on_completion = false, ready = true, read_error_after_write = false;
	std::uint32_t status_extra = 0;
	bool hold_state = true, commit_not_ready = false;
	unsigned delay = 1, samples = 0, reads = 0;
	std::array<std::uint16_t, 9> rows{};
	std::array<std::uint16_t, 4> header{};
	std::vector<std::uint8_t> tape;
	std::vector<std::uint16_t> chunk_sizes, data_words;
	std::uint32_t offset = 0;
	std::uint16_t count = 0;

	Error Read32(std::uint32_t address, std::uint32_t* value) override {
		++reads;
		if (read_error) return {ErrorCode::io_failed, "injected read error"};
		switch (address) {
		case FesDe25ComputerIdOffset: *value = bridge_id; break;
		case FesDe25ComputerVersionOffset: *value = version; break;
		case FesDe25ComputerFaultOffset: *value = fault; break;
		case FesDe25ComputerGpoOffset: *value = gpo ^ (change_payload && !writes.empty() ? 1u : 0u); break;
		case FesDe25ComputerStatusOffset:
			*value = (ready ? FesDe25ComputerStatusReady : 0) | status_extra | (fault ? FesDe25ComputerStatusFault : 0) |
				(pending || busy_after_ack ? FesDe25ComputerStatusBusy : 0); break;
		case FesDe25ComputerGpiOffset: {
			const bool complete_now = pending && !no_ack && samples++ >= delay;
			if (complete_now) {
				Complete();
				if (slow_ack) std::this_thread::sleep_for(std::chrono::milliseconds(110));
			}
			*value = gpi ^ (bad_signature ? 0x01000000u : 0u) ^ (reserved ? 0x00010000u : 0u);
			if (complete_now && fault_after_ack) fault = FesDe25ComputerFaultReset;
			break;
		}
		default: assert(false && "out-of-contract register read");
		}
		return {};
	}
	Error Write32(std::uint32_t address, std::uint32_t word) override {
		assert(address == FesDe25ComputerGpoOffset);
		assert(!pending); // An old-toggle preload, replay or busy write is a test failure.
		assert(((word >> 31) & 1) != ((gpi >> 23) & 1));
		gpo = word; writes.push_back(word); pending = true; samples = 0;
		if (read_error_after_write) read_error = true;
		return write_error ? Error{ErrorCode::io_failed, "lost write completion"} : Error{};
	}
	void Complete() {
		const unsigned opcode = (gpo >> 24) & 127, index = (gpo >> 16) & 255;
		const auto argument = std::uint16_t(gpo);
		std::uint16_t data = success_data, reject = reject_next; reject_next = 0;
		if (!reject) switch (opcode) {
		case FesComputerOpcodeIdentity: assert(index < identity.size()); data = identity[index]; break;
		case FesComputerOpcodeMediaInfo: {
			assert(index < 6);
			const std::uint16_t info[] = {std::uint16_t(minimum), std::uint16_t(minimum >> 16),
				std::uint16_t(maximum), std::uint16_t(maximum >> 16), max_chunk, state};
			data = info[index]; break;
		}
		case FesComputerOpcodeExecution: hold_state = argument == 0; break;
		case FesComputerOpcodeKeyboardHid: assert(index < rows.size()); rows[index] = argument; break;
		case FesComputerOpcodeMediaBegin:
			assert(index < 4); header[index] = argument;
			if (index == 0) { state = FesComputerMediaStateLoading; tape.assign(argument, 0); }
			break;
		case FesComputerOpcodeMediaChunk:
			assert(index < 3);
			if (index == 0) offset = argument;
			if (index == 1) offset |= std::uint32_t(argument) << 16;
			if (index == 2) { count = argument; chunk_sizes.push_back(count); }
			break;
		case FesComputerOpcodeMediaData:
			assert(index * 2u < count && offset + count <= tape.size());
			tape[offset + index * 2u] = argument & 255;
			if (index * 2u + 1 < count) tape[offset + index * 2u + 1] = argument >> 8;
			else assert((argument >> 8) == 0); // Odd chunk padding must be zero.
			data_words.push_back(argument); break;
		case FesComputerOpcodeMediaCommit:
			state = commit_not_ready ? FesComputerMediaStateLoading : FesComputerMediaStateReady; break;
		case FesComputerOpcodeMediaEject: state = FesComputerMediaStateEmpty; break;
		default: reject = FesComputerErrorInvalidOpcode;
		}
		gpi = 0xf5000000u | ((gpo >> 31) ? 0x00800000u : 0u) |
			(reject ? 0x00400000u | reject : data);
		pending = false;
		if (busy_on_completion) busy_after_ack = true;
	}
};
struct Fixture {
	Bridge bridge; TestClock clock; De25Computer client{bridge, clock};
	void Probe() {
		De25ComputerIdentity id;
		assert(client.Identify(kBuild, 1000000, &id).ok());
		assert(id.build_id == kBuild && id.capabilities == 0x803);
	}
};
void TestAtomicIdentityAndControl() {
	Fixture f;
	assert(!f.client.Run(1000000).ok() && f.bridge.writes.empty());
	f.Probe(); assert(f.bridge.writes.size() == 22);
	assert(f.bridge.writes[0] == 0x81000000u && f.bridge.writes[1] == 0x01010000u);
	assert(f.client.Hold(1000000).ok() && f.bridge.hold_state);
	assert(f.bridge.writes.back() == 0x82000000u);
	assert(f.client.Run(1000000).ok() && !f.bridge.hold_state);
	assert(f.bridge.writes.back() == 0x02000001u && f.bridge.writes.size() == 24);
	KeyboardHidRows rows{}; rows[0] = 0x10; rows[3] = 0x4000; rows[8] = 0x82;
	assert(f.client.SetKeyboardHid(rows, 1000000).ok()); assert(f.bridge.rows == rows);
	assert(f.bridge.writes.size() == 33); // Every owned row, exactly once, no preload.
	assert(f.client.SetKeyboardHid({}, 1000000).ok()); assert((f.bridge.rows == KeyboardHidRows{}));
}
void TestSelectedIdentityRefusesMutations() {
	for (unsigned fault = 0; fault < 7; ++fault) {
		Fixture f; De25ComputerIdentity id;
		if (fault == 0) f.bridge.bridge_id ^= 1;
		if (fault == 1) f.bridge.version++;
		if (fault == 2) f.bridge.identity[4]++;
		if (fault == 3) f.bridge.identity[7] |= 0x2000; // Unexpected session display is not this artifact.
		if (fault == 4) f.bridge.identity[8] ^= 1;
		if (fault == 5) f.bridge.maximum = 65536;
		if (fault == 6) f.bridge.max_chunk = 256;
		assert(!f.client.Identify(kBuild, 1000000, &id).ok() && f.client.Poisoned());
		const auto writes = f.bridge.writes.size(); assert(!f.client.Hold(1000000).ok());
		assert(f.bridge.writes.size() == writes);
		for (auto word : f.bridge.writes) assert(((word >> 24) & 127) == 1 || ((word >> 24) & 127) == 5);
	}
}
void TestInputPreflightBeforeEffects() {
	Fixture f; De25ComputerIdentity id;
	for (const auto& text : {std::string{}, std::string(32, '0'), std::string(32, 'g')})
		assert(!f.client.Identify(text, 1000000, &id).ok());
	assert(!f.bridge.reads && f.bridge.writes.empty()); f.Probe();
	const auto writes = f.bridge.writes.size();
	KeyboardHidRows rows{}; rows[0] = 1; assert(!f.client.SetKeyboardHid(rows, 1000000).ok());
	rows = {}; rows[8] = 0x100; assert(!f.client.SetKeyboardHid(rows, 1000000).ok());
	assert(!f.client.LoadTape({}, 1000000).ok());
	assert(!f.client.LoadTape(std::vector<std::uint8_t>(16385), 1000000).ok());
	assert(!f.client.LoadTape({1}, 1000000, 0).ok());
	assert(!f.client.LoadTape({1}, 1000000, 513).ok());
	std::uint16_t aliased = 0;
	assert(!f.client.Exchange(1, 0, 0, 1000000, &aliased, &aliased).ok());
	assert(!f.client.Run(0).ok());
	assert(f.bridge.writes.size() == writes && !f.client.Poisoned());
}
void TestOddTapeAndCrc() {
	Fixture f; f.Probe(); const std::vector<std::uint8_t> payload{'1','2','3','4','5','6','7','8','9'};
	assert(f.client.LoadTape(payload, 1000000, 7).ok());
	assert(f.bridge.tape == payload && f.bridge.header[0] == 9 && f.bridge.header[1] == 0);
	assert(f.bridge.header[2] == 0x3926 && f.bridge.header[3] == 0xcbf4); // CRC32 check vector.
	assert((f.bridge.chunk_sizes == std::vector<std::uint16_t>{7,2}));
	assert(f.bridge.data_words[3] == 0x0037 && f.bridge.data_words[4] == 0x3938);
	assert(f.client.EjectTape(1000000).ok() && f.bridge.state == FesComputerMediaStateEmpty);
}
void TestMaximumTapeChunkOrdinals() {
	Fixture f; f.Probe(); std::vector<std::uint8_t> payload(16384);
	for (unsigned i = 0; i < payload.size(); ++i) payload[i] = (i * 37u + 19u) & 255;
	assert(f.client.LoadTape(payload, 1000000000).ok()); assert(f.bridge.tape == payload);
	assert(f.bridge.chunk_sizes.size() == 32);
	assert(std::all_of(f.bridge.chunk_sizes.begin(), f.bridge.chunk_sizes.end(), [](auto n) { return n == 512; }));
	assert(f.bridge.data_words.size() == 8192);
}
void TestHealthyRejectionsDoNotReplayOrPoison() {
	Fixture f; f.Probe(); auto count = f.bridge.writes.size();
	f.bridge.reject_next = FesComputerErrorInvalidState;
	assert(f.client.Hold(1000000).code == ErrorCode::busy);
	assert(!f.client.Poisoned() && f.bridge.writes.size() == count + 1);
	assert(f.client.Run(1000000).ok());
	std::uint16_t data = 0, rejected = 0;
	assert(f.client.Exchange(0, 0, 0, 1000000, &data, &rejected).ok());
	assert(rejected == FesComputerErrorInvalidOpcode && !f.client.Poisoned());
}
void TestUncertainCommandsNeverReplay() {
	for (unsigned mode = 0; mode < 10; ++mode) {
		Fixture f; f.Probe(); const auto before = f.bridge.writes.size();
		if (mode == 0) f.bridge.write_error = true;
		if (mode == 1) f.bridge.change_payload = true;
		if (mode == 2) f.bridge.fault_after_ack = true;
		if (mode == 3) f.bridge.bad_signature = true;
		if (mode == 4) f.bridge.reserved = true;
		if (mode == 5) f.bridge.reject_next = 17;
		if (mode == 6) f.bridge.success_data = 1;
		if (mode == 7) f.bridge.read_error = true;
		if (mode == 8) f.bridge.busy_on_completion = true;
		if (mode == 9) f.bridge.read_error_after_write = true;
		assert(!f.client.Hold(1000000).ok() && f.client.Poisoned());
		const auto after = f.bridge.writes.size(); assert(after <= before + 1);
		assert(!f.client.Hold(1000000).ok() && f.bridge.writes.size() == after);
		De25ComputerIdentity id; assert(!f.client.Identify(kBuild, 1000000, &id).ok());
		assert(f.bridge.writes.size() == after);
	}
}
void TestBusyAndIdleToggleMismatchRefuseBeforeWrite() {
	for (unsigned mode = 0; mode < 5; ++mode) {
		Fixture f; f.Probe(); const auto before = f.bridge.writes.size();
		if (mode == 0) f.bridge.pending = true;
		if (mode == 1) f.bridge.gpo ^= FesComputerRequestMask;
		if (mode == 2) f.bridge.fault = FesDe25ComputerFaultReset;
		if (mode == 3) f.bridge.status_extra = 8;
		if (mode == 4) f.bridge.ready = false;
		assert(!f.client.Hold(1000000).ok() && f.client.Poisoned());
		assert(f.bridge.writes.size() == before);
	}
}
void TestBoundedMissingAckAndStalledClock() {
	Fixture f; f.Probe(); f.bridge.no_ack = true; f.clock.frozen = true;
	const auto before = f.bridge.writes.size();
	assert(!f.client.Hold(f.clock.time + 3).ok() && f.client.Poisoned());
	assert(f.bridge.writes.size() == before + 1);
	assert(!f.client.Run(1000000).ok() && f.bridge.writes.size() == before + 1);
	Fixture backwards; backwards.Probe(); backwards.clock.backwards = true;
	assert(!backwards.client.Run(1000000).ok() && backwards.client.Poisoned());
}
void TestLateSuccessfulAckIsUncertain() {
	Fixture f; f.Probe(); f.bridge.slow_ack = true;
	const auto before = f.bridge.writes.size();
	assert(!f.client.Run(1000000).ok() && f.client.Poisoned());
	assert(!f.bridge.hold_state && !f.bridge.pending); // The hardware command did complete.
	assert(f.bridge.writes.size() == before + 1);
	assert(!f.client.Run(1000000).ok() && f.bridge.writes.size() == before + 1);
}
void TestOneSerializedClientOwnership() {
	Fixture f; f.Probe(); const auto before = f.bridge.writes.size();
	std::atomic<bool> passed{true};
	auto worker = [&] {
		for (unsigned i = 0; i < 20; ++i) {
			std::uint16_t response = 0, rejected = 0;
			if (!f.client.Exchange(1, 0, 0, 1000000, &response, &rejected).ok() || response != 0x4546 || rejected)
				passed = false;
		}
	};
	std::thread one(worker), two(worker), three(worker); one.join(); two.join(); three.join();
	assert(passed && !f.client.Poisoned() && f.bridge.writes.size() == before + 60);
}
void TestAcknowledgedCommitMustBecomeReady() {
	Fixture f; f.Probe(); f.bridge.commit_not_ready = true;
	assert(!f.client.LoadTape({1}, 1000000).ok() && f.client.Poisoned());
}
} // namespace
int main() {
	TestAtomicIdentityAndControl(); TestSelectedIdentityRefusesMutations(); TestInputPreflightBeforeEffects();
	TestOddTapeAndCrc(); TestMaximumTapeChunkOrdinals(); TestHealthyRejectionsDoNotReplayOrPoison();
	TestUncertainCommandsNeverReplay(); TestBusyAndIdleToggleMismatchRefuseBeforeWrite();
	TestBoundedMissingAckAndStalledClock(); TestAcknowledgedCommitMustBecomeReady(); TestOneSerializedClientOwnership(); TestLateSuccessfulAckIsUncertain();
	std::puts("de25_computer_test: 12 groups passed (scripted registers; no hardware admission)");
}
