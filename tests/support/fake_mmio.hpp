// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "native/linux/mmio.hpp"

#include <cstdint>
#include <deque>
#include <map>
#include <string>
#include <vector>

namespace mister_test {

class FakeMmio final : public mister::native::Mmio {
public:
	struct Write {
		std::uint32_t offset;
		std::uint32_t value;
	};

	mister::Error Read32(std::uint32_t, std::uint32_t*) override;
	mister::Error Write32(std::uint32_t, std::uint32_t) override;
	void PushRead(std::uint32_t, std::uint32_t);
	void PushReadError(std::uint32_t, const mister::Error&);
	void PushWriteError(std::uint32_t, const mister::Error&);

	mister::Error read_error;
	mister::Error write_error;
	std::map<std::uint32_t, std::uint32_t> values;
	std::map<std::uint32_t, std::deque<std::uint32_t>> scripted_reads;
	std::map<std::uint32_t, std::deque<mister::Error>> scripted_read_errors;
	std::map<std::uint32_t, std::deque<mister::Error>> scripted_write_errors;
	std::map<std::uint32_t, std::uint32_t> forced_values_after_write;
	std::vector<Write> expected_writes;
	bool enforce_expected_writes = false;
	std::string write_mismatch;
	std::vector<std::uint32_t> reads;
	std::vector<Write> writes;
};

} // namespace mister_test
