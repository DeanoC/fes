// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"

#include <cstdint>
#include <string>
#include <vector>

namespace mister {
namespace native {

class Artifact;
class Spi;

class CoreLoader {
public:
	explicit CoreLoader(Spi&);
	Error Probe(std::string* observed_core,
		std::uint64_t absolute_deadline_ms);
	Error Configure(const std::vector<Setting>&,
		std::uint64_t absolute_deadline_ms);
	Error Attach(std::uint8_t index, const Artifact&,
		std::uint64_t absolute_deadline_ms);

private:
	Spi& spi_;
};

} // namespace native
} // namespace mister
