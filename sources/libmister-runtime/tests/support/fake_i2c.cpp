// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "fake_i2c.hpp"

#include <iomanip>
#include <sstream>

namespace mister_test {

namespace {

std::string Hex(std::uint8_t value)
{
	std::ostringstream output;
	output << "0x" << std::hex << std::setw(2) << std::setfill('0')
		<< static_cast<unsigned int>(value);
	return output.str();
}

} // namespace

FakeI2c::FakeI2c(std::vector<std::string>* events,
	std::vector<std::string>* ordered_calls)
	: events_(events), ordered_calls_(ordered_calls) {}

mister::Error FakeI2c::SelectFirst(std::uint8_t slave,
	std::uint8_t detection_register, std::uint64_t deadline,
	std::string* bus, std::uint8_t* detected)
{
	calls.push_back({CallType::select, slave, detection_register, deadline});
	if (events_ != nullptr)
		events_->push_back("i2c:select:" + selected_bus + ":" + Hex(slave) +
			":" + Hex(detection_register));
	if (ordered_calls_ != nullptr)
		ordered_calls_->push_back("i2c:select:" + selected_bus + ":" +
			Hex(slave) + ":" + Hex(detection_register));
	if (!select_error.ok()) return select_error;
	if (bus == nullptr || detected == nullptr)
		return {mister::ErrorCode::io_failed, "missing fake I2C selection output"};
	*bus = selected_bus;
	*detected = detection_value;
	return {};
}

mister::Error FakeI2c::ReadByte(std::uint8_t address, std::uint8_t* value,
	std::uint64_t deadline)
{
	calls.push_back({CallType::read, address, 0, deadline});
	if (events_ != nullptr) events_->push_back("i2c:read:" + Hex(address));
	if (ordered_calls_ != nullptr)
		ordered_calls_->push_back("i2c:read:" + Hex(address));
	if (value == nullptr)
		return {mister::ErrorCode::io_failed, "missing fake I2C read output"};
	if (address == 0x41) {
		if (!power_read_error.ok()) return power_read_error;
		*value = power_after;
		return {};
	}
	if (address != 0x42)
		return {mister::ErrorCode::io_failed, "unexpected fake I2C register"};
	const std::size_t read_index = link_read_count_++;
	if (read_index == fail_link_read_index || !link_read_error.ok())
		return link_read_error.ok()
			? mister::Error{mister::ErrorCode::io_failed,
				"scripted link read failure"}
			: link_read_error;
	if (!link_statuses.empty()) {
		last_link_status_ = link_statuses.front();
		link_statuses.pop_front();
	}
	*value = last_link_status_;
	return {};
}

mister::Error FakeI2c::WriteByte(std::uint8_t address, std::uint8_t value,
	std::uint64_t deadline)
{
	const std::size_t write_index = writes_.size();
	calls.push_back({CallType::write, address, value, deadline});
	writes_.push_back({address, value});
	if (ordered_calls_ != nullptr)
		ordered_calls_->push_back("i2c:write:" + Hex(address) + ":" + Hex(value));
	if (events_ != nullptr) {
		const bool before_timing = timing_write_index_ ==
			std::numeric_limits<std::size_t>::max();
		const bool after_release = release_write_index_ !=
			std::numeric_limits<std::size_t>::max();
		const char* event = before_timing ? "i2c:initialization" :
			after_release ? "i2c:hdmi_wake" : "i2c:mode";
		if (writes_.size() == 1 ||
			(!before_timing && !after_release &&
				writes_.size() == timing_write_index_ + 1) ||
			(after_release && writes_.size() == release_write_index_ + 1))
			events_->push_back(event);
	}
	if (write_index == fail_write_index) return write_error;
	return {};
}

void FakeI2c::MarkTimingEvent()
{
	timing_write_index_ = writes_.size();
}

void FakeI2c::MarkReleaseEvent()
{
	release_write_index_ = writes_.size();
}

std::vector<mister::native::RegisterWrite> FakeI2c::InitializationWrites() const
{
	const std::size_t end = timing_write_index_ ==
		std::numeric_limits<std::size_t>::max() ? writes_.size() :
		timing_write_index_;
	return std::vector<mister::native::RegisterWrite>(writes_.begin(),
		writes_.begin() + end);
}

std::vector<mister::native::RegisterWrite> FakeI2c::ModeWrites() const
{
	if (timing_write_index_ == std::numeric_limits<std::size_t>::max()) return {};
	const std::size_t end = release_write_index_ ==
		std::numeric_limits<std::size_t>::max() ? writes_.size() :
		release_write_index_;
	return std::vector<mister::native::RegisterWrite>(
		writes_.begin() + timing_write_index_, writes_.begin() + end);
}

std::vector<mister::native::RegisterWrite> FakeI2c::HdmiWakeWrites() const
{
	if (release_write_index_ == std::numeric_limits<std::size_t>::max()) return {};
	return std::vector<mister::native::RegisterWrite>(
		writes_.begin() + release_write_index_, writes_.end());
}

} // namespace mister_test
