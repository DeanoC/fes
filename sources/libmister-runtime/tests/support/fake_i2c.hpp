// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "native/linux/i2c.hpp"
#include "native/video_recipe.hpp"

#include <cstddef>
#include <cstdint>
#include <deque>
#include <limits>
#include <string>
#include <vector>

namespace mister_test {

class FakeI2c final : public mister::native::I2c {
public:
	enum class CallType { select, read, write };
	struct Call {
		CallType type;
		std::uint8_t address;
		std::uint8_t value;
		std::uint64_t deadline;
	};

	explicit FakeI2c(std::vector<std::string>* events = nullptr,
		std::vector<std::string>* ordered_calls = nullptr);
	mister::Error SelectFirst(std::uint8_t, std::uint8_t, std::uint64_t,
		std::string*, std::uint8_t*) override;
	mister::Error ReadByte(std::uint8_t, std::uint8_t*,
		std::uint64_t) override;
	mister::Error WriteByte(std::uint8_t, std::uint8_t,
		std::uint64_t) override;

	void MarkTimingEvent();
	void MarkReleaseEvent();
	std::vector<mister::native::RegisterWrite> InitializationWrites() const;
	std::vector<mister::native::RegisterWrite> ModeWrites() const;
	std::vector<mister::native::RegisterWrite> HdmiWakeWrites() const;

	std::string selected_bus = "/dev/i2c-1";
	std::uint8_t detection_value = 0x40;
	std::uint8_t power_after = 0x10;
	std::deque<std::uint8_t> link_statuses = {0x60};
	mister::Error select_error;
	mister::Error power_read_error;
	mister::Error link_read_error;
	std::size_t fail_link_read_index = std::numeric_limits<std::size_t>::max();
	std::size_t fail_write_index = std::numeric_limits<std::size_t>::max();
	mister::Error write_error = {mister::ErrorCode::io_failed,
		"scripted I2C write failure"};
	std::vector<Call> calls;

private:
	std::vector<std::string>* events_;
	std::vector<std::string>* ordered_calls_;
	std::vector<mister::native::RegisterWrite> writes_;
	std::size_t timing_write_index_ = std::numeric_limits<std::size_t>::max();
	std::size_t release_write_index_ = std::numeric_limits<std::size_t>::max();
	std::size_t link_read_count_ = 0;
	std::uint8_t last_link_status_ = 0;
};

} // namespace mister_test
