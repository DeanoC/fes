// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/de25_pr_control.hpp"

#include "native/generated/fes_de25_pr_control.hpp"
#include "native/hardware.hpp"
#include "native/linux/mmio.hpp"

#include <algorithm>
#include <array>
#include <exception>
#include <thread>

namespace mister {
namespace native {
namespace {
using namespace generated;
constexpr std::uint32_t kStatusMask = FesDe25PrControlStatusReady |
	FesDe25PrControlStatusBusy | FesDe25PrControlStatusFrozen |
	FesDe25PrControlStatusRunning | FesDe25PrControlStatusFault;
const std::array<std::uint32_t, 4> kBuildOffsets{{FesDe25PrControlBuildId0Offset,
	FesDe25PrControlBuildId1Offset, FesDe25PrControlBuildId2Offset,
	FesDe25PrControlBuildId3Offset}};

Error Invalid(const std::string& message)
{
	return {ErrorCode::invalid_request, message, "de25-pr-control"};
}
bool CanonicalBuild(const std::string& input, std::string* output)
{
	if (input.size() != 32) return false;
	output->clear();
	bool nonzero = false;
	for (char c : input) {
		if (c >= 'A' && c <= 'F') c = static_cast<char>(c - 'A' + 'a');
		if (!((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'))) return false;
		nonzero = nonzero || c != '0';
		output->push_back(c);
	}
	return nonzero;
}
void AppendWord(std::uint32_t word, std::string* output)
{
	static const char hex[] = "0123456789abcdef";
	for (int shift = 28; shift >= 0; shift -= 4)
		output->push_back(hex[(word >> shift) & 15]);
}
bool KnownPersona(std::uint32_t persona)
{
	return persona == FesDe25PrControlPersonaA || persona == FesDe25PrControlPersonaB;
}
} // namespace

De25PrControl::De25PrControl(Mmio& io, Clock& clock) : io_(io), clock_(clock) {}

bool De25PrControl::Poisoned() const
{
	std::lock_guard<std::mutex> lock(mutex_);
	return poisoned_;
}

Error De25PrControl::Fail(const std::string& message, ErrorCode code)
{
	poisoned_ = true;
	identified_ = false;
	return {code, message, "de25-pr-control"};
}

Error De25PrControl::RequireIdentity() const
{
	if (poisoned_)
		return {ErrorCode::io_failed, "discard the failed DE25 PR controller; no retry or resume is allowed", "de25-pr-control"};
	if (!identified_)
		return {ErrorCode::io_failed, "identify the selected DE25 PR build before control", "de25-pr-control"};
	return {};
}

Error De25PrControl::Begin(std::uint64_t deadline)
{
	if (poisoned_)
		return {ErrorCode::io_failed, "discard the failed DE25 PR controller", "de25-pr-control"};
	const auto now = clock_.NowMs();
	if (identified_ && now < last_time_) return Fail("DE25 PR clock moved backwards");
	last_time_ = now;
	if (now >= deadline) return Fail("DE25 PR operation deadline already expired");
	deadline_ = deadline;
	wall_deadline_ = std::chrono::steady_clock::now() +
		std::chrono::milliseconds(std::min<std::uint64_t>(deadline - now, 120000));
	return {};
}

Error De25PrControl::CheckDeadline()
{
	const auto now = clock_.NowMs();
	if (now < last_time_) return Fail("DE25 PR clock moved backwards; completion is uncertain");
	last_time_ = now;
	if (now >= deadline_ || std::chrono::steady_clock::now() >= wall_deadline_)
		return Fail("DE25 PR operation timed out; completion is uncertain");
	return {};
}

Error De25PrControl::Read(std::uint32_t offset, std::uint32_t* value)
{
	Error error = CheckDeadline();
	if (!error.ok()) return error;
	error = io_.Read32(offset, value);
	if (!error.ok()) return Fail("DE25 PR register read failed; completion is uncertain");
	return CheckDeadline();
}

Error De25PrControl::Write(std::uint32_t offset, std::uint32_t value)
{
	Error error = CheckDeadline();
	if (!error.ok()) return error;
	error = io_.Write32(offset, value);
	if (!error.ok()) return Fail("DE25 PR register write failed; completion is uncertain");
	return CheckDeadline();
}

Error De25PrControl::ReadSnapshot(Snapshot* result)
{
	for (;;) {
		std::uint32_t before = 0, after = 0, status = 0, fault = 0, persona = 0;
		Error error = Read(FesDe25PrControlCompletedSeqOffset, &before);
		if (!error.ok()) return error;
		error = Read(FesDe25PrControlStatusOffset, &status);
		if (!error.ok()) return error;
		error = Read(FesDe25PrControlFaultOffset, &fault);
		if (!error.ok()) return error;
		error = Read(FesDe25PrControlPersonaIdOffset, &persona);
		if (!error.ok()) return error;
		error = Read(FesDe25PrControlCompletedSeqOffset, &after);
		if (!error.ok()) return error;
		if (fault || (status & FesDe25PrControlStatusFault))
			return Fail("DE25 PR endpoint reports a permanent fault");
		const auto state = status & (FesDe25PrControlStatusBusy |
			FesDe25PrControlStatusFrozen | FesDe25PrControlStatusRunning);
		if ((status & ~kStatusMask) || !(status & FesDe25PrControlStatusReady) ||
			!state || (state & (state - 1)))
			return Fail("DE25 PR endpoint status is invalid or not ready");
		if ((before | after) & ~FesDe25PrControlSequenceMask)
			return Fail("DE25 PR endpoint sequence has reserved bits");
		if (before != after) {
			std::this_thread::yield();
			continue;
		}
		if (!(status & FesDe25PrControlStatusBusy)) {
			if (!(status & (FesDe25PrControlStatusFrozen | FesDe25PrControlStatusRunning)))
				return Fail("idle DE25 PR endpoint is neither frozen nor running");
			if ((status & FesDe25PrControlStatusRunning) && !KnownPersona(persona))
				return Fail("running DE25 PR endpoint has an unknown persona");
		}
		*result = {status, after, persona};
		return {};
	}
}

Error De25PrControl::VerifyIdentity(Snapshot* snapshot)
{
	for (unsigned sample = 0; sample < 2; ++sample) {
		std::uint32_t id = 0, version = 0, timeout = 0;
		Error error = Read(FesDe25PrControlIdOffset, &id);
		if (!error.ok()) return error;
		error = Read(FesDe25PrControlVersionOffset, &version);
		if (!error.ok()) return error;
		std::string observed;
		for (const auto offset : kBuildOffsets) {
			std::uint32_t word = 0;
			error = Read(offset, &word);
			if (!error.ok()) return error;
			AppendWord(word, &observed);
		}
		error = Read(FesDe25PrControlTimeoutCyclesOffset, &timeout);
		if (!error.ok()) return error;
		if (id != FesDe25PrControlIdentity || version != FesDe25PrControlVersion ||
			observed != expected_build_ || timeout != FesDe25PrControlTimeoutCycles)
			return Fail("DE25 PR build, version or control contract differs from selection", ErrorCode::core_mismatch);
	}
	return ReadSnapshot(snapshot);
}

Error De25PrControl::Identify(const std::string& expected, std::uint64_t deadline,
	De25PrIdentity* identity)
{
	std::lock_guard<std::mutex> lock(mutex_);
	std::string canonical;
	if (!identity || !CanonicalBuild(expected, &canonical))
		return Invalid("selected DE25 PR build must be a nonzero 32-digit hexadecimal identity");
	if (identified_ && canonical != expected_build_)
		return Invalid("an identified DE25 PR controller cannot change its selected build");
	*identity = {};
	Error error = Begin(deadline);
	if (!error.ok()) return error;
	const bool already_identified = identified_;
	identified_ = false;
	expected_build_ = canonical;
	Snapshot snapshot;
	error = VerifyIdentity(&snapshot);
	if (!error.ok()) return error;
	if (snapshot.status & FesDe25PrControlStatusBusy)
		return Fail("DE25 PR endpoint already owns a command");
	if (already_identified && snapshot.sequence != sequence_)
		return Fail("DE25 PR command ownership changed during identity revalidation");
	sequence_ = snapshot.sequence;
	identified_ = true;
	identity->build_id = expected_build_;
	identity->completed_sequence = sequence_;
	identity->persona_id = snapshot.persona;
	identity->frozen = (snapshot.status & FesDe25PrControlStatusFrozen) != 0;
	return {};
}

Error De25PrControl::Command(std::uint32_t opcode, std::uint32_t argument)
{
	Snapshot before;
	Error error = VerifyIdentity(&before);
	if (!error.ok()) return error;
	if ((before.status & FesDe25PrControlStatusBusy) || before.sequence != sequence_)
		return Fail("DE25 PR command ownership changed");
	if (opcode == FesDe25PrControlCommandResume && !(before.status & FesDe25PrControlStatusFrozen))
		return Fail("DE25 PR resume requires a frozen slot");
	if (sequence_ == FesDe25PrControlSequenceMask)
		return Fail("DE25 PR command sequence is exhausted; start a new configuration epoch");
	const auto next = sequence_ + 1;
	error = Write(FesDe25PrControlArgumentOffset, argument);
	if (!error.ok()) return error;
	Snapshot staged;
	error = ReadSnapshot(&staged);
	if (!error.ok()) return error;
	if ((staged.status & FesDe25PrControlStatusBusy) || staged.sequence != sequence_ ||
		staged.status != before.status)
		return Fail("DE25 PR endpoint changed while staging a command");
	error = Write(FesDe25PrControlCommandOffset,
		(opcode << FesDe25PrControlCommandShift) | next);
	if (!error.ok()) return error;
	for (;;) {
		Snapshot current;
		error = ReadSnapshot(&current);
		if (!error.ok()) return error;
		if (current.sequence != sequence_ && current.sequence != next)
			return Fail("DE25 PR acknowledged an unexpected command sequence");
		if (current.sequence == next) {
			if (current.status & FesDe25PrControlStatusBusy)
				return Fail("DE25 PR completed sequence remains busy");
			const auto required = opcode == FesDe25PrControlCommandFreeze
				? FesDe25PrControlStatusFrozen : FesDe25PrControlStatusRunning;
			if (!(current.status & required) ||
				(opcode == FesDe25PrControlCommandResume && current.persona != argument))
				return Fail("DE25 PR command completed with a different slot state or persona");
			sequence_ = next;
			return {};
		}
		std::this_thread::yield();
	}
}

Error De25PrControl::Freeze(std::uint64_t deadline)
{
	std::lock_guard<std::mutex> lock(mutex_);
	Error error = RequireIdentity();
	if (!error.ok()) return error;
	error = Begin(deadline);
	if (!error.ok()) return error;
	return Command(FesDe25PrControlCommandFreeze, 0);
}

Error De25PrControl::Reconfigure(std::uint32_t persona, const ConfigurePersona& configure,
	std::uint64_t deadline)
{
	std::lock_guard<std::mutex> lock(mutex_);
	if (!KnownPersona(persona) || !configure)
		return Invalid("select one known diagnostic persona and a configuration callback");
	Error error = RequireIdentity();
	if (!error.ok()) return error;
	error = Begin(deadline);
	if (!error.ok()) return error;
	error = Command(FesDe25PrControlCommandFreeze, 0);
	if (!error.ok()) return error;
	Snapshot frozen;
	error = VerifyIdentity(&frozen);
	if (!error.ok()) return error;
	if (frozen.status != (FesDe25PrControlStatusReady | FesDe25PrControlStatusFrozen) ||
		frozen.sequence != sequence_)
		return Fail("DE25 PR programming requires the selected idle frozen endpoint");
	error = CheckDeadline();
	if (!error.ok()) return error;
	try {
		error = configure();
	} catch (const std::exception&) {
		return Fail("DE25 persona configuration threw; resume was not attempted", ErrorCode::program_failed);
	} catch (...) {
		return Fail("DE25 persona configuration threw; resume was not attempted", ErrorCode::program_failed);
	}
	if (!error.ok()) return Fail("DE25 persona configuration failed; resume was not attempted", ErrorCode::program_failed);
	error = CheckDeadline();
	if (!error.ok()) return error;
	error = VerifyIdentity(&frozen);
	if (!error.ok()) return error;
	if (frozen.status != (FesDe25PrControlStatusReady | FesDe25PrControlStatusFrozen) ||
		frozen.sequence != sequence_)
		return Fail("DE25 PR root or frozen ownership changed during configuration");
	return Command(FesDe25PrControlCommandResume, persona);
}
} // namespace native
} // namespace mister
