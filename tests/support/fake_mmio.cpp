// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "fake_mmio.hpp"

namespace mister_test {

mister::Error FakeMmio::Read32(std::uint32_t offset, std::uint32_t* value)
{
	reads.push_back(offset);
	if (!read_error.ok()) return read_error;
	if (value == nullptr) return {mister::ErrorCode::io_failed, "missing read output"};
	auto script = scripted_reads.find(offset);
	if (script != scripted_reads.end() && !script->second.empty()) {
		*value = script->second.front();
		script->second.pop_front();
	} else {
		*value = values[offset];
	}
	return {};
}

mister::Error FakeMmio::Write32(std::uint32_t offset, std::uint32_t value)
{
	writes.push_back({offset, value});
	if (!write_error.ok()) return write_error;
	values[offset] = value;
	return {};
}

void FakeMmio::PushRead(std::uint32_t offset, std::uint32_t value)
{
	scripted_reads[offset].push_back(value);
}

} // namespace mister_test
