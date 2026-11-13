// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "native/linux/mmio.hpp"
#include "native/generated/fes_de25_pr_control.hpp"
#include <cassert>
#include <functional>
#include <map>
#include <vector>

namespace mister_test { namespace de25_pr {
using namespace mister;
using namespace mister::native;
using namespace mister::native::generated;
constexpr const char* kBuild = "112233445566778899aabbccddeeff00";
constexpr std::uint32_t kReadyFrozen = FesDe25PrControlStatusReady | FesDe25PrControlStatusFrozen;
constexpr std::uint32_t kReadyRunning = FesDe25PrControlStatusReady | FesDe25PrControlStatusRunning;

// A small CSR endpoint model with independent command completion. It validates
// staging, sequence and state instead of returning a list of canned reads.
// This models the public protocol, not PR configuration hardware or timing.
class Endpoint final : public Mmio {
public:
	Endpoint()
	{
		registers = {{FesDe25PrControlIdOffset, FesDe25PrControlIdentity},
			{FesDe25PrControlVersionOffset, FesDe25PrControlVersion},
			{FesDe25PrControlStatusOffset, kReadyFrozen},
			{FesDe25PrControlFaultOffset, 0}, {FesDe25PrControlCompletedSeqOffset, 0},
			{FesDe25PrControlPersonaIdOffset, 0},
			{FesDe25PrControlTimeoutCyclesOffset, FesDe25PrControlTimeoutCycles},
			{FesDe25PrControlBuildId0Offset, 0x11223344},
			{FesDe25PrControlBuildId1Offset, 0x55667788},
			{FesDe25PrControlBuildId2Offset, 0x99aabbcc},
			{FesDe25PrControlBuildId3Offset, 0xddeeff00}};
	}
	Error Read32(std::uint32_t offset, std::uint32_t* value) override
	{
		++reads;
		if (on_read) on_read(offset);
		if (fail_read == offset) return {ErrorCode::io_failed, "injected read failure"};
		if (pending && !stall && remaining && --remaining == 0) Complete();
		const auto it = registers.find(offset);
		assert(it != registers.end());
		*value = it->second;
		return {};
	}
	Error Write32(std::uint32_t offset, std::uint32_t value) override
	{
		writes.push_back({offset, value});
		if (offset == fail_write && !accept_failed_write)
			return {ErrorCode::io_failed, "injected rejected write"};
		if (offset == FesDe25PrControlArgumentOffset) {
			assert(!pending);
			staged = true;
			argument = value;
		} else {
			assert(offset == FesDe25PrControlCommandOffset && staged && !pending);
			staged = false;
			const auto opcode = value >> FesDe25PrControlCommandShift;
			const auto sequence = value & FesDe25PrControlSequenceMask;
			assert(sequence == registers[FesDe25PrControlCompletedSeqOffset] + 1);
			assert(opcode == FesDe25PrControlCommandFreeze || opcode == FesDe25PrControlCommandResume);
			if (opcode == FesDe25PrControlCommandFreeze) assert(argument == 0);
			if (opcode == FesDe25PrControlCommandResume)
				assert(registers[FesDe25PrControlStatusOffset] == kReadyFrozen);
			pending = true;
			command = value;
			remaining = latency;
			registers[FesDe25PrControlStatusOffset] = FesDe25PrControlStatusReady | FesDe25PrControlStatusBusy;
			if (!remaining && !stall) Complete();
		}
		if (offset == fail_write) return {ErrorCode::io_failed, "injected ambiguous accepted write"};
		return {};
	}
	void Complete()
	{
		pending = false;
		const auto opcode = command >> FesDe25PrControlCommandShift;
		const auto sequence = command & FesDe25PrControlSequenceMask;
		registers[FesDe25PrControlCompletedSeqOffset] = wrong_sequence ? sequence + 1 : sequence;
		if (opcode == FesDe25PrControlCommandFreeze) {
			registers[FesDe25PrControlStatusOffset] = kReadyFrozen;
		} else if (loaded_persona != argument) {
			registers[FesDe25PrControlStatusOffset] = FesDe25PrControlStatusReady | FesDe25PrControlStatusFault;
			registers[FesDe25PrControlFaultOffset] = FesDe25PrControlFaultPersona;
		} else {
			registers[FesDe25PrControlStatusOffset] = wrong_state ? kReadyFrozen : kReadyRunning;
			registers[FesDe25PrControlPersonaIdOffset] = loaded_persona;
		}
		if (busy_ack) registers[FesDe25PrControlStatusOffset] |= FesDe25PrControlStatusBusy;
	}
	std::size_t Commands(std::uint32_t opcode) const
	{
		std::size_t count = 0;
		for (const auto& write : writes)
			if (write.first == FesDe25PrControlCommandOffset &&
				(write.second >> FesDe25PrControlCommandShift) == opcode) ++count;
		return count;
	}
	std::map<std::uint32_t, std::uint32_t> registers;
	std::vector<std::pair<std::uint32_t, std::uint32_t>> writes;
	std::function<void(std::uint32_t)> on_read;
	std::uint32_t loaded_persona = FesDe25PrControlPersonaA;
	std::uint32_t fail_read = 0xffffffffu, fail_write = 0xffffffffu;
	std::uint32_t argument = 0, command = 0;
	unsigned latency = 3, remaining = 0, reads = 0;
	bool staged = false, pending = false, stall = false;
	bool wrong_sequence = false, wrong_state = false, busy_ack = false, accept_failed_write = false;
};

} }
