// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/linux/i2c.hpp"

#include <assert.h>
#include <fcntl.h>
#include <stdio.h>

#include <cstdint>
#include <map>
#include <set>
#include <string>
#include <tuple>
#include <utility>
#include <vector>

namespace {

class FixedClock final : public mister::native::Clock {
public:
	explicit FixedClock(std::uint64_t now) : now_(now) {}
	std::uint64_t NowMs() const override { return now_; }
	std::uint64_t now_;
};

class ScriptedClock final : public mister::native::Clock {
public:
	explicit ScriptedClock(std::vector<std::uint64_t> values)
		: values_(std::move(values)), next_(0) {}
	std::uint64_t NowMs() const override
	{
		assert(next_ < values_.size());
		return values_[next_++];
	}

private:
	std::vector<std::uint64_t> values_;
	mutable std::size_t next_;
};

class Operations final : public mister::native::LinuxI2cTestOperations {
public:
	int Open(const char* path, int flags) override
	{
		opened_paths.push_back(path);
		open_flags.push_back(flags);
		const auto found = open_results.find(path);
		return found == open_results.end() ? -1 : found->second;
	}
	int Close(int descriptor) override
	{
		closed_descriptors.push_back(descriptor);
		return close_error ? -1 : 0;
	}
	int SelectSlave(int descriptor, std::uint8_t address) override
	{
		selected_descriptors.push_back(descriptor);
		selected_addresses.push_back(address);
		return select_failures.count(descriptor) == 0 ? 0 : -1;
	}
	int ReadByteData(int descriptor, std::uint8_t address,
		std::uint8_t* value) override
	{
		read_requests.push_back({descriptor, address});
		const auto found = read_values.find({descriptor, address});
		if (found == read_values.end()) return -1;
		*value = found->second;
		return 0;
	}
	int WriteByteData(int descriptor, std::uint8_t address,
		std::uint8_t value) override
	{
		write_requests.push_back({descriptor, address, value});
		return write_failures.count({descriptor, address, value}) == 0 ? 0 : -1;
	}

	std::map<std::string, int> open_results;
	std::set<int> select_failures;
	std::map<std::pair<int, std::uint8_t>, std::uint8_t> read_values;
	std::set<std::tuple<int, std::uint8_t, std::uint8_t>> write_failures;
	bool close_error = false;
	std::vector<std::string> opened_paths;
	std::vector<int> open_flags;
	std::vector<int> closed_descriptors;
	std::vector<int> selected_descriptors;
	std::vector<std::uint8_t> selected_addresses;
	std::vector<std::pair<int, std::uint8_t>> read_requests;
	std::vector<std::tuple<int, std::uint8_t, std::uint8_t>> write_requests;
};

void TestSelectionRequiresDetectionReadAndKeepsOnlyItOpen()
{
	Operations operations;
	operations.open_results = {{"/dev/i2c-1", 11}, {"/dev/i2c-2", 12}};
	operations.select_failures.insert(11);
	operations.read_values[{12, 0x41}] = 0x40;
	FixedClock clock(1);
	std::string bus;
	std::uint8_t power = 0;
	{
		mister::native::LinuxI2c i2c(clock, operations);
		assert(i2c.SelectFirst(0x39, 0x41, 1100, &bus, &power).ok());
		assert(bus == "/dev/i2c-2");
		assert(power == 0x40);
		assert((operations.opened_paths == std::vector<std::string>{
			"/dev/i2c-0", "/dev/i2c-1", "/dev/i2c-2"}));
		assert((operations.selected_addresses == std::vector<std::uint8_t>{0x39, 0x39}));
		assert((operations.closed_descriptors == std::vector<int>{11}));
		assert((operations.open_flags == std::vector<int>{O_RDWR | O_CLOEXEC,
			O_RDWR | O_CLOEXEC, O_RDWR | O_CLOEXEC}));
	}
	assert((operations.closed_descriptors == std::vector<int>{11, 12}));
}

void TestFailedOpenAdvancesWithinBoundedBusList()
{
	Operations operations;
	operations.open_results = {{"/dev/i2c-1", 11}};
	operations.read_values[{11, 0x41}] = 0x40;
	FixedClock clock(1);
	mister::native::LinuxI2c i2c(clock, operations);
	std::string bus;
	std::uint8_t power = 0;
	assert(i2c.SelectFirst(0x39, 0x41, 100, &bus, &power).ok());
	assert(bus == "/dev/i2c-1");
	assert((operations.opened_paths == std::vector<std::string>{
		"/dev/i2c-0", "/dev/i2c-1"}));
}

void TestFailedDetectionReadClosesCandidateAndAdvances()
{
	Operations operations;
	operations.open_results = {{"/dev/i2c-0", 10}, {"/dev/i2c-1", 11}};
	operations.read_values[{11, 0x41}] = 0x40;
	FixedClock clock(1);
	mister::native::LinuxI2c i2c(clock, operations);
	std::string bus;
	std::uint8_t power = 0;
	assert(i2c.SelectFirst(0x39, 0x41, 100, &bus, &power).ok());
	assert((operations.closed_descriptors == std::vector<int>{10}));
	assert((operations.read_requests ==
		std::vector<std::pair<int, std::uint8_t>>{{10, 0x41}, {11, 0x41}}));
}

void TestNoResponderLeavesOutputsUnchanged()
{
	Operations operations;
	operations.open_results = {{"/dev/i2c-0", 10}};
	FixedClock clock(1);
	mister::native::LinuxI2c i2c(clock, operations);
	std::string bus = "unchanged";
	std::uint8_t power = 0x5a;
	const mister::Error error = i2c.SelectFirst(0x39, 0x41, 100, &bus, &power);
	assert(error.code == mister::ErrorCode::io_failed);
	assert(bus == "unchanged");
	assert(power == 0x5a);
	assert((operations.closed_descriptors == std::vector<int>{10}));
	assert((operations.opened_paths == std::vector<std::string>{
		"/dev/i2c-0", "/dev/i2c-1", "/dev/i2c-2"}));
}

void TestSelectionDeadlineBeforeFirstOpenDoesNotDispatch()
{
	Operations operations;
	ScriptedClock clock({100});
	mister::native::LinuxI2c i2c(clock, operations);
	std::string bus = "unchanged";
	std::uint8_t power = 0x5a;
	assert(i2c.SelectFirst(0x39, 0x41, 100, &bus, &power).code ==
		mister::ErrorCode::io_failed);
	assert(bus == "unchanged");
	assert(power == 0x5a);
	assert(operations.opened_paths.empty());
	assert(operations.open_flags.empty());
	assert(operations.selected_descriptors.empty());
	assert(operations.selected_addresses.empty());
	assert(operations.read_requests.empty());
	assert(operations.closed_descriptors.empty());
}

void TestSelectionDeadlineAfterOpenClosesCandidateBeforeSlaveSelect()
{
	Operations operations;
	operations.open_results = {{"/dev/i2c-0", 10}};
	ScriptedClock clock({1, 100});
	mister::native::LinuxI2c i2c(clock, operations);
	std::string bus = "unchanged";
	std::uint8_t power = 0x5a;
	assert(i2c.SelectFirst(0x39, 0x41, 100, &bus, &power).code ==
		mister::ErrorCode::io_failed);
	assert(bus == "unchanged");
	assert(power == 0x5a);
	assert((operations.opened_paths == std::vector<std::string>{"/dev/i2c-0"}));
	assert((operations.open_flags == std::vector<int>{O_RDWR | O_CLOEXEC}));
	assert(operations.selected_descriptors.empty());
	assert(operations.selected_addresses.empty());
	assert(operations.read_requests.empty());
	assert((operations.closed_descriptors == std::vector<int>{10}));
}

void TestSelectionDeadlineAfterSlaveSelectClosesCandidateBeforeDetectionRead()
{
	Operations operations;
	operations.open_results = {{"/dev/i2c-0", 10}};
	ScriptedClock clock({1, 1, 100});
	mister::native::LinuxI2c i2c(clock, operations);
	std::string bus = "unchanged";
	std::uint8_t power = 0x5a;
	assert(i2c.SelectFirst(0x39, 0x41, 100, &bus, &power).code ==
		mister::ErrorCode::io_failed);
	assert(bus == "unchanged");
	assert(power == 0x5a);
	assert((operations.opened_paths == std::vector<std::string>{"/dev/i2c-0"}));
	assert((operations.open_flags == std::vector<int>{O_RDWR | O_CLOEXEC}));
	assert((operations.selected_descriptors == std::vector<int>{10}));
	assert((operations.selected_addresses == std::vector<std::uint8_t>{0x39}));
	assert(operations.read_requests.empty());
	assert((operations.closed_descriptors == std::vector<int>{10}));
}

void TestInvalidAndUnselectedCallsDoNotDispatch()
{
	Operations operations;
	FixedClock clock(10);
	mister::native::LinuxI2c i2c(clock, operations);
	std::uint8_t value = 0;
	assert(i2c.SelectFirst(0x39, 0x41, 10, nullptr, &value).code ==
		mister::ErrorCode::io_failed);
	assert(i2c.ReadByte(0x41, &value, 100).code == mister::ErrorCode::io_failed);
	assert(i2c.WriteByte(0x41, 0x40, 100).code == mister::ErrorCode::io_failed);
	assert(operations.opened_paths.empty());
	assert(operations.read_requests.empty());
	assert(operations.write_requests.empty());
}

void TestSelectedTransfersForwardExactBytesAndRespectDeadline()
{
	Operations operations;
	operations.open_results = {{"/dev/i2c-0", 10}};
	operations.read_values[{10, 0x41}] = 0x40;
	operations.read_values[{10, 0x42}] = 0x60;
	FixedClock clock(1);
	mister::native::LinuxI2c i2c(clock, operations);
	std::string bus;
	std::uint8_t power = 0;
	assert(i2c.SelectFirst(0x39, 0x41, 100, &bus, &power).ok());
	std::uint8_t value = 0;
	assert(i2c.ReadByte(0x42, &value, 100).ok());
	assert(value == 0x60);
	assert(i2c.WriteByte(0x17, 0x62, 100).ok());
	assert((operations.read_requests ==
		std::vector<std::pair<int, std::uint8_t>>{{10, 0x41}, {10, 0x42}}));
	assert((operations.write_requests ==
		std::vector<std::tuple<int, std::uint8_t, std::uint8_t>>{{10, 0x17, 0x62}}));
	clock.now_ = 100;
	assert(i2c.ReadByte(0x42, &value, 100).code == mister::ErrorCode::io_failed);
	assert(i2c.WriteByte(0x17, 0x62, 100).code == mister::ErrorCode::io_failed);
	assert(operations.read_requests.size() == 2);
	assert(operations.write_requests.size() == 1);
}

void TestPersistentSelectionSupportsMenuGameMenuAndRelaunch()
{
	Operations operations;
	operations.open_results = {{"/dev/i2c-0", 10}};
	operations.read_values[{10, 0x41}] = 0x40;
	FixedClock clock(1);
	mister::native::LinuxI2c i2c(clock, operations);
	const char* const stages[] = {"menu", "game", "menu", "relaunch"};
	for (const char* stage : stages) {
		(void)stage;
		std::string bus = "unchanged";
		std::uint8_t power = 0;
		assert(i2c.SelectFirst(0x39, 0x41, 100, &bus, &power).ok());
		assert(bus == "/dev/i2c-0");
		assert(power == 0x40);
	}
	assert((operations.opened_paths ==
		std::vector<std::string>{"/dev/i2c-0"}));
	assert((operations.selected_descriptors == std::vector<int>{10}));
	assert((operations.selected_addresses == std::vector<std::uint8_t>{0x39}));
	assert((operations.read_requests ==
		std::vector<std::pair<int, std::uint8_t>>{
			{10, 0x41}, {10, 0x41}, {10, 0x41}, {10, 0x41}}));
}

void TestTransferFailuresAndDifferentSelectionDoNotCreateAnotherOwner()
{
	Operations operations;
	operations.open_results = {{"/dev/i2c-0", 10}, {"/dev/i2c-1", 11}};
	operations.read_values[{10, 0x41}] = 0x40;
	operations.write_failures.insert({10, 0x17, 0x62});
	operations.close_error = true;
	FixedClock clock(1);
	{
		mister::native::LinuxI2c i2c(clock, operations);
		std::string bus;
		std::uint8_t power = 0;
		assert(i2c.SelectFirst(0x39, 0x41, 100, &bus, &power).ok());
		std::uint8_t value = 0;
		assert(i2c.ReadByte(0x42, &value, 100).code == mister::ErrorCode::io_failed);
		assert(i2c.WriteByte(0x17, 0x62, 100).code == mister::ErrorCode::io_failed);
		assert(i2c.SelectFirst(0x3a, 0x41, 100, &bus, &power).code ==
			mister::ErrorCode::io_failed);
		assert((operations.opened_paths == std::vector<std::string>{"/dev/i2c-0"}));
	}
	assert((operations.closed_descriptors == std::vector<int>{10}));
}

} // namespace

int main()
{
	TestSelectionRequiresDetectionReadAndKeepsOnlyItOpen();
	TestFailedOpenAdvancesWithinBoundedBusList();
	TestFailedDetectionReadClosesCandidateAndAdvances();
	TestNoResponderLeavesOutputsUnchanged();
	TestSelectionDeadlineBeforeFirstOpenDoesNotDispatch();
	TestSelectionDeadlineAfterOpenClosesCandidateBeforeSlaveSelect();
	TestSelectionDeadlineAfterSlaveSelectClosesCandidateBeforeDetectionRead();
	TestInvalidAndUnselectedCallsDoNotDispatch();
	TestSelectedTransfersForwardExactBytesAndRespectDeadline();
	TestPersistentSelectionSupportsMenuGameMenuAndRelaunch();
	TestTransferFailuresAndDifferentSelectionDoNotCreateAnotherOwner();
	puts("i2c_test: 11 passed");
	return 0;
}
