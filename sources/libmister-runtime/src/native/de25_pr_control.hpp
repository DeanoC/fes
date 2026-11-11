// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once

#include "libmister-runtime/runtime.h"

#include <chrono>
#include <cstdint>
#include <functional>
#include <mutex>
#include <string>

namespace mister {
namespace native {
class Mmio;
class Clock;

struct De25PrIdentity {
	std::string build_id;
	std::uint32_t completed_sequence = 0;
	std::uint32_t persona_id = 0;
	bool frozen = false;
};

// Protocol consumer for an exclusively owned DE25 PR-control resource. The
// caller supplies ordered, bounded, full-word MMIO and an already validated
// persona configuration operation. No physical opener, RBF parser, daemon
// operation or production constructor is provided here. A software deadline
// cannot interrupt a blocked MMIO access or callback. Callbacks must not reenter
// this controller; its mutex covers the complete freeze/configure/resume cycle.
// Keep one instance for the exclusively owned configuration epoch. After an
// uncertain operation, a new instance requires a separately established new
// FPGA configuration epoch; construction itself is not a recovery operation.
class De25PrControl final {
public:
	using ConfigurePersona = std::function<Error()>;
	De25PrControl(Mmio&, Clock&);
	bool Poisoned() const;
	Error Identify(const std::string& expected_build_id,
		std::uint64_t absolute_deadline_ms, De25PrIdentity*);
	Error Freeze(std::uint64_t absolute_deadline_ms);
	Error Reconfigure(std::uint32_t expected_persona_id,
		const ConfigurePersona&, std::uint64_t absolute_deadline_ms);

private:
	struct Snapshot {
		std::uint32_t status = 0;
		std::uint32_t sequence = 0;
		std::uint32_t persona = 0;
	};
	Error Begin(std::uint64_t);
	Error CheckDeadline();
	Error Fail(const std::string&, ErrorCode = ErrorCode::io_failed);
	Error RequireIdentity() const;
	Error Read(std::uint32_t, std::uint32_t*);
	Error Write(std::uint32_t, std::uint32_t);
	Error ReadSnapshot(Snapshot*);
	Error VerifyIdentity(Snapshot*);
	Error Command(std::uint32_t opcode, std::uint32_t argument);
	Mmio& io_;
	Clock& clock_;
	mutable std::mutex mutex_;
	bool poisoned_ = false;
	bool identified_ = false;
	std::string expected_build_;
	std::uint32_t sequence_ = 0;
	std::uint64_t deadline_ = 0;
	std::uint64_t last_time_ = 0;
	std::chrono::steady_clock::time_point wall_deadline_;
};
} // namespace native
} // namespace mister
