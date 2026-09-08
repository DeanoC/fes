// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"

#include <cstdint>

namespace mister {
namespace native {

class Clock;
struct CoreDescriptor;
class Mmio;

class FesGp final {
public:
	FesGp(Mmio&, Clock&);
	Error Exchange(std::uint8_t opcode, std::uint8_t index,
		std::uint16_t argument, std::uint64_t absolute_deadline_ms,
		std::uint16_t* response);
	Error Identify(const CoreDescriptor&, std::uint64_t absolute_deadline_ms);

private:
	Mmio& mmio_;
	Clock& clock_;
	bool request_toggle_ = false;
	bool poisoned_ = false;
};

} // namespace native
} // namespace mister
