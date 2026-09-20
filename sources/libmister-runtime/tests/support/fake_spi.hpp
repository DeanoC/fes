// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "native/linux/spi.hpp"

#include <cstdint>
#include <deque>
#include <string>
#include <vector>

namespace mister_test {

class FakeSpi final : public mister::native::Spi {
public:
	struct Call {
		std::uint8_t target;
		std::vector<std::uint16_t> request;
		std::uint64_t deadline;
	};

	mister::Error SynchronizeCore(std::uint64_t) override;

	mister::Error Exchange(std::uint8_t,
		const std::vector<std::uint16_t>&,
		std::vector<std::uint16_t>*, std::uint64_t) override;

	std::string observed_core = "TESTCART";
	std::deque<mister::Error> errors;
	std::vector<Call> calls;
};

} // namespace mister_test
