// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once

#include "libmister-runtime/runtime.h"
#include <chrono>
#include <cstdint>
#include <mutex>
#include <string>
#include <vector>

namespace mister {
namespace native {
class Mmio;
class Clock;

struct De25ComputerIdentity {
	std::string build_id;
	std::uint16_t capabilities = 0;
};

// Pure protocol consumer. Mmio offsets are resource-relative; this class has
// no physical base, device opener, programming path or production constructor.
// The exclusive backend must perform ordered, bounded, atomic full-word I/O.
// Software deadlines cannot interrupt a backend that blocks in a bus access.
class De25Computer final {
public:
	De25Computer(Mmio&, Clock&);
	bool Poisoned() const;
	// A selected, nonzero 128-bit build identity is mandatory before control.
	Error Identify(const std::string& expected_build_id, std::uint64_t deadline_ms,
		De25ComputerIdentity*);
	Error Hold(std::uint64_t deadline_ms);
	Error Run(std::uint64_t deadline_ms);
	Error SetKeyboardHid(const KeyboardHidRows&, std::uint64_t deadline_ms);
	Error LoadTape(const std::vector<std::uint8_t>&, std::uint64_t deadline_ms,
		std::uint16_t chunk_bytes = 512);
	Error EjectTape(std::uint64_t deadline_ms);
	// An acknowledged known rejection is returned in rejection, without
	// poisoning the link. Transport uncertainty permanently poisons this client.
	Error Exchange(std::uint8_t opcode, std::uint8_t index, std::uint16_t argument,
		std::uint64_t deadline_ms, std::uint16_t* response, std::uint16_t* rejection);
private:
	Error Begin(std::uint64_t);
	Error CheckDeadline();
	Error Fail(const std::string&);
	Error Read(std::uint32_t, std::uint32_t*);
	Error Status(std::uint32_t*);
	Error Gpi(std::uint32_t*);
	Error ExchangeLocked(std::uint8_t, std::uint8_t, std::uint16_t,
		std::uint16_t*, std::uint16_t*);
	Error Mutate(std::uint8_t, std::uint8_t, std::uint16_t);
	Error MediaInfo(std::uint32_t*, std::uint32_t*, std::uint16_t*, std::uint16_t*);
	Error RequireIdentity() const;
	Mmio& io_;
	Clock& clock_;
	mutable std::mutex mutex_;
	bool poisoned_ = false, identified_ = false;
	std::uint64_t deadline_ = 0, last_time_ = 0;
	std::chrono::steady_clock::time_point wall_deadline_;
};
} // namespace native
} // namespace mister
