// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "fake_mmio.hpp"

#include <cstdio>

namespace mister_test {

mister::Error FakeMmio::Read32(std::uint32_t offset, std::uint32_t* value)
{
	reads.push_back(offset);
	if (!read_error.ok()) return read_error;
	if (value == nullptr) return {mister::ErrorCode::io_failed, "missing read output"};
	auto scripted_error = scripted_read_errors.find(offset);
	if (scripted_error != scripted_read_errors.end() &&
		!scripted_error->second.empty()) {
		const mister::Error error = scripted_error->second.front();
		scripted_error->second.pop_front();
		if (!error.ok()) return error;
	}
	if (read_as_zero.count(offset) != 0) {
		*value = 0;
		return {};
	}
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
	if (enforce_expected_writes) {
		const std::size_t index = writes.size() - 1;
		if (index >= expected_writes.size() ||
			expected_writes[index].offset != offset ||
			expected_writes[index].value != value) {
			char detail[192] = {};
			if (index >= expected_writes.size()) {
				std::snprintf(detail, sizeof(detail),
					"unexpected MMIO write %zu: got 0x%08x=0x%08x",
					index, offset, value);
			} else {
				std::snprintf(detail, sizeof(detail),
					"unexpected MMIO write %zu: expected 0x%08x=0x%08x, got 0x%08x=0x%08x",
					index, expected_writes[index].offset,
					expected_writes[index].value, offset, value);
			}
			write_mismatch = detail;
			return {mister::ErrorCode::io_failed, write_mismatch};
		}
	}
	auto scripted_error = scripted_write_errors.find(offset);
	if (scripted_error != scripted_write_errors.end() &&
		!scripted_error->second.empty()) {
		const mister::Error error = scripted_error->second.front();
		scripted_error->second.pop_front();
		if (!error.ok()) return error;
	}
	auto forced = forced_values_after_write.find(offset);
	values[offset] = forced == forced_values_after_write.end() ?
		value : forced->second;
	return {};
}

void FakeMmio::PushRead(std::uint32_t offset, std::uint32_t value)
{
	scripted_reads[offset].push_back(value);
}

void FakeMmio::PushReadError(std::uint32_t offset, const mister::Error& error)
{
	scripted_read_errors[offset].push_back(error);
}

void FakeMmio::PushWriteError(std::uint32_t offset, const mister::Error& error)
{
	scripted_write_errors[offset].push_back(error);
}

} // namespace mister_test
