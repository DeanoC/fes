// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "capture_log.hpp"
#include "fake_i2c.hpp"
#include "native/core_loader.hpp"
#include "native/linux/spi.hpp"
#include "native/video.hpp"
#include "native/video_recipe.hpp"

#include <assert.h>
#include <stdio.h>

#include <algorithm>
#include <cstddef>
#include <cstdint>
#include <deque>
#include <iomanip>
#include <limits>
#include <sstream>
#include <string>
#include <utility>
#include <vector>

namespace {

const std::uint64_t kDeadline = 100;
// Selection, 92 initialization writes, the power read, and three mode writes.
const std::size_t kLinkReadCallBegin = 97;
std::size_t scenarios = 0;

class SequenceClock final : public mister::native::Clock {
public:
	explicit SequenceClock(std::vector<std::uint64_t> values = {0, 1, 2, 3, 4})
		: values_(std::move(values)) {}
	std::uint64_t NowMs() const override
	{
		assert(!values_.empty());
		const std::size_t current = std::min(index_, values_.size() - 1);
		++index_;
		return values_[current];
	}
	mutable std::size_t index_ = 0;
	std::vector<std::uint64_t> values_;
};

class RecordingSpi final : public mister::native::Spi {
public:
	struct Call {
		std::uint8_t target;
		std::vector<std::uint16_t> request;
		std::uint64_t deadline;
	};

	RecordingSpi(std::vector<std::string>& events,
		std::vector<std::string>& ordered_calls, mister_test::FakeI2c& i2c)
		: events_(events), ordered_calls_(ordered_calls), i2c_(i2c) {}

	mister::Error Exchange(std::uint8_t target,
		const std::vector<std::uint16_t>& request,
		std::vector<std::uint16_t>* response, std::uint64_t deadline) override
	{
		const std::size_t call_index = calls.size();
		calls.push_back({target, request, deadline});
		std::string event;
		if (!request.empty() && request[0] == 0x0014) {
			event = "spi:probe";
		} else if (!request.empty() && request[0] == 0x0020) {
			event = "spi:timing";
			i2c_.MarkTimingEvent();
		} else if (request.size() == 9 && request[0] == 0x001e &&
			request[1] == 0x0001) {
			event = "spi:status_assert";
		} else if (request.size() == 9 && request[0] == 0x001e &&
			request[1] == 0x0000) {
			event = "spi:status_release";
		} else {
			event = "spi:unexpected";
		}
		events_.push_back(event);
		ordered_calls_.push_back(event);
		if (call_index == fail_call_index) return failure;
		if (response != nullptr) {
			response->assign(request.size(), terminate_identity ? 0 : 'X');
			if (!request.empty() && request[0] == 0x0014) {
				std::size_t index = 1;
				for (unsigned char byte : identity) {
					if (index >= response->size()) break;
					(*response)[index++] = byte;
				}
				if (invalid_identity && response->size() > 1) (*response)[1] = 0x01;
				if (terminate_identity && index < response->size())
					(*response)[index] = ';';
			}
		}
		return {};
	}

	const Call& TimingCall() const
	{
		for (const Call& call : calls)
			if (!call.request.empty() && call.request[0] == 0x0020) return call;
		assert(false);
		return calls.front();
	}

	std::vector<std::string>& events_;
	std::vector<std::string>& ordered_calls_;
	mister_test::FakeI2c& i2c_;
	std::string identity = "MENU";
	bool terminate_identity = true;
	bool invalid_identity = false;
	std::size_t fail_call_index = std::numeric_limits<std::size_t>::max();
	mister::Error failure = {mister::ErrorCode::io_failed,
		"scripted SPI failure"};
	std::vector<Call> calls;
};

struct Fixture {
	explicit Fixture(std::vector<std::uint64_t> clock_values = {0, 1, 2, 3, 4})
		: clock(std::move(clock_values)), i2c(&events, &ordered_calls),
		spi(events, ordered_calls, i2c),
		core(spi), video(core, spi, i2c, clock, log,
			mister::native::Menu720p60Recipe()) {}
	std::vector<std::string> events;
	std::vector<std::string> ordered_calls;
	SequenceClock clock;
	mister_test::FakeI2c i2c;
	RecordingSpi spi;
	mister::native::CoreLoader core;
	mister_test::CaptureLog log;
	mister::native::MenuVideoBringup video;
};

bool EqualWrites(const std::vector<mister::native::RegisterWrite>& actual,
	const std::vector<mister::native::RegisterWrite>& expected)
{
	if (actual.size() != expected.size()) return false;
	for (std::size_t index = 0; index < actual.size(); ++index)
		if (actual[index].address != expected[index].address ||
			actual[index].value != expected[index].value) return false;
	return true;
}

std::string HexByte(std::uint8_t value)
{
	std::ostringstream output;
	output << "0x" << std::hex << std::setw(2) << std::setfill('0')
		<< static_cast<unsigned int>(value);
	return output.str();
}

std::vector<std::string> ExpectedAfterProbe()
{
	return {"spi:status_assert", "spi:probe"};
}

std::vector<std::string> ExpectedAfterSelection()
{
	std::vector<std::string> expected = ExpectedAfterProbe();
	expected.push_back("i2c:select:/dev/i2c-1:0x39:0x41");
	return expected;
}

std::vector<std::string> ExpectedAfterInitializationWrites(std::size_t count)
{
	std::vector<std::string> expected = ExpectedAfterSelection();
	const auto& writes = mister::native::Menu720p60Recipe().adv_initialization;
	assert(count <= writes.size());
	for (std::size_t index = 0; index < count; ++index)
		expected.push_back("i2c:write:" + HexByte(writes[index].address) + ":" +
			HexByte(writes[index].value));
	return expected;
}

std::vector<std::string> ExpectedAfterPowerRead()
{
	std::vector<std::string> expected = ExpectedAfterInitializationWrites(
		mister::native::Menu720p60Recipe().adv_initialization.size());
	expected.push_back("i2c:read:0x41");
	return expected;
}

std::vector<std::string> ExpectedAfterTimingAttempt()
{
	std::vector<std::string> expected = ExpectedAfterPowerRead();
	expected.push_back("spi:timing");
	return expected;
}

std::vector<std::string> ExpectedAfterModeWrites(std::size_t count)
{
	std::vector<std::string> expected = ExpectedAfterTimingAttempt();
	const auto& writes = mister::native::Menu720p60Recipe().adv_mode;
	assert(count <= writes.size());
	for (std::size_t index = 0; index < count; ++index)
		expected.push_back("i2c:write:" + HexByte(writes[index].address) + ":" +
			HexByte(writes[index].value));
	return expected;
}

std::vector<std::string> ExpectedAfterReleaseAttempt()
{
	std::vector<std::string> expected = ExpectedAfterModeWrites(
		mister::native::Menu720p60Recipe().adv_mode.size());
	expected.push_back("spi:status_release");
	return expected;
}

std::vector<std::string> ExpectedAfterLinkReads(std::size_t count)
{
	std::vector<std::string> expected = ExpectedAfterReleaseAttempt();
	for (std::size_t index = 0; index < count; ++index)
		expected.push_back("i2c:read:0x42");
	return expected;
}

std::size_t CountEvent(const std::vector<std::string>& events,
	const std::string& event)
{
	return static_cast<std::size_t>(std::count(events.begin(), events.end(), event));
}

std::size_t CountI2cCalls(const mister_test::FakeI2c& i2c,
	mister_test::FakeI2c::CallType type, std::uint8_t address)
{
	std::size_t count = 0;
	for (const auto& call : i2c.calls)
		if (call.type == type && call.address == address) ++count;
	return count;
}

void ExpectFailure(const Fixture& fixture, const mister::native::VideoResult& result,
	const std::string& phase, const std::string& message = "")
{
	assert(result.error.code == mister::ErrorCode::io_failed);
	assert(result.phase == phase);
	if (!message.empty()) assert(result.error.message == message);
	const std::vector<mister::LogRecord> records = fixture.log.records();
	assert(!records.empty());
	assert(records.back().phase == phase);
	assert(records.back().error.code == mister::ErrorCode::io_failed);
	assert(records.back().error.message == result.error.message);
}

void TestSuccessUsesExactOrderWireRequestsDeadlineDiagnosticsAndLogs()
{
	Fixture fixture;
	const mister::native::VideoResult result = fixture.video.BringUp("MENU", kDeadline);
	const std::vector<std::string> expected = {
		"spi:status_assert", "spi:probe",
		"i2c:select:/dev/i2c-1:0x39:0x41", "i2c:initialization",
		"i2c:read:0x41", "spi:timing", "i2c:mode",
		"spi:status_release", "i2c:read:0x42",
	};
	assert(fixture.events == expected);
	assert(fixture.ordered_calls == ExpectedAfterLinkReads(1));
	const std::vector<std::uint16_t> asserted = {
		0x001e, 0x0001, 0x0000, 0x0000, 0x0000,
		0x0000, 0x0000, 0x0000, 0x0000,
	};
	const std::vector<std::uint16_t> released = {
		0x001e, 0x0000, 0x0000, 0x0000, 0x0000,
		0x0000, 0x0000, 0x0000, 0x0000,
	};
	assert(fixture.spi.calls.front().request == asserted);
	assert(fixture.spi.TimingCall().request ==
		mister::native::Menu720p60Recipe().timing_words);
	assert(fixture.spi.calls.back().request == released);
	assert(EqualWrites(fixture.i2c.InitializationWrites(),
		mister::native::Menu720p60Recipe().adv_initialization));
	assert(EqualWrites(fixture.i2c.ModeWrites(),
		mister::native::Menu720p60Recipe().adv_mode));
	assert(result.error.ok());
	assert(result.phase == "hdmi_verify");
	assert(result.observed_core == "MENU");
	assert(result.selected_bus == "/dev/i2c-1");
	assert(result.power_before == 0x40);
	assert(result.power_after == 0x10);
	assert(result.link_status == 0x60);
	for (const auto& call : fixture.spi.calls) {
		assert(call.target == mister::native::kUserIoTarget);
		assert(call.deadline == kDeadline);
	}
	for (const auto& call : fixture.i2c.calls) assert(call.deadline == kDeadline);
	const std::vector<mister::LogRecord> records = fixture.log.records();
	const std::vector<std::string> phases = {"core_reset", "core_probe",
		"hdmi_init", "video_timing", "core_release", "hdmi_verify"};
	assert(records.size() == phases.size());
	for (std::size_t index = 0; index < records.size(); ++index) {
		assert(records[index].operation == "start");
		assert(records[index].system.empty());
		assert(records[index].phase == phases[index]);
		assert(records[index].error.code == mister::ErrorCode::none);
		if (index == 0) assert(records[index].core.empty());
		else assert(records[index].core == "MENU");
	}
	assert(records.back().error.message ==
		"recipe=menu_720p60 bus=/dev/i2c-1 address=0x39 power_before=0x40 "
		"power_after=0x10 link_status=0x60");
	++scenarios;
}

void TestExpiredBeforeResetMakesNoHardwareCall()
{
	Fixture fixture({100});
	const auto result = fixture.video.BringUp("MENU", kDeadline);
	ExpectFailure(fixture, result, "core_reset", "deadline exceeded");
	assert(fixture.events.empty());
	assert(fixture.ordered_calls.empty());
	assert(fixture.spi.calls.empty());
	assert(fixture.i2c.calls.empty());
	++scenarios;
}

void TestResetAssertionFailureStopsAtAttempt()
{
	Fixture fixture;
	fixture.spi.fail_call_index = 0;
	fixture.spi.failure = {mister::ErrorCode::invalid_request,
		"scripted SPI failure"};
	const auto result = fixture.video.BringUp("MENU", kDeadline);
	ExpectFailure(fixture, result, "core_reset", "scripted SPI failure");
	assert((fixture.events == std::vector<std::string>{"spi:status_assert"}));
	assert((fixture.ordered_calls ==
		std::vector<std::string>{"spi:status_assert"}));
	++scenarios;
}

void TestProbeTransportAndMalformedFailuresStopAtProbe()
{
	{
		Fixture fixture;
		fixture.spi.fail_call_index = 1;
		const auto result = fixture.video.BringUp("MENU", kDeadline);
		ExpectFailure(fixture, result, "core_probe", "scripted SPI failure");
		assert((fixture.events == std::vector<std::string>{
			"spi:status_assert", "spi:probe"}));
		assert(fixture.ordered_calls == ExpectedAfterProbe());
		++scenarios;
	}
	{
		Fixture fixture;
		fixture.spi.invalid_identity = true;
		const auto result = fixture.video.BringUp("MENU", kDeadline);
		ExpectFailure(fixture, result, "core_probe", "invalid observed core name");
		assert(fixture.spi.calls.size() == 2);
		assert(fixture.i2c.calls.empty());
		assert(fixture.ordered_calls == ExpectedAfterProbe());
		++scenarios;
	}
}

void TestOnlyExactTerminatedUppercaseMenuIdentityIsAccepted()
{
	struct Case { const char* identity; bool terminated; };
	const Case cases[] = {{"OTHER", true}, {"Menu", true}, {"", true},
		{"MENU", false}};
	for (const Case& test : cases) {
		Fixture fixture;
		fixture.spi.identity = test.identity;
		fixture.spi.terminate_identity = test.terminated;
		const auto result = fixture.video.BringUp("MENU", kDeadline);
		ExpectFailure(fixture, result, "core_probe");
		assert(fixture.i2c.calls.empty());
		assert(CountEvent(fixture.events, "spi:status_release") == 0);
		assert(fixture.ordered_calls == ExpectedAfterProbe());
		++scenarios;
	}
	Fixture wrong_expectation;
	wrong_expectation.spi.identity = "OTHER";
	const auto result = wrong_expectation.video.BringUp("OTHER", kDeadline);
	assert(result.error.code == mister::ErrorCode::io_failed);
	ExpectFailure(wrong_expectation, result, "core_probe", "unexpected menu core");
	assert(wrong_expectation.i2c.calls.empty());
	assert(wrong_expectation.ordered_calls == ExpectedAfterProbe());
	++scenarios;
}

void TestNoAdvResponderStopsAtSelection()
{
	Fixture fixture;
	fixture.i2c.select_error = {mister::ErrorCode::io_failed,
		"no responding ADV7513 bus after /dev/i2c-2"};
	const auto result = fixture.video.BringUp("MENU", kDeadline);
	ExpectFailure(fixture, result, "hdmi_init",
		"no responding ADV7513 bus after /dev/i2c-2");
	assert(fixture.i2c.calls.size() == 1);
	assert(CountEvent(fixture.events, "spi:status_release") == 0);
	assert(fixture.ordered_calls == ExpectedAfterSelection());
	++scenarios;
}

void TestEveryInitializationWriteFailureStopsAtThatExactWrite()
{
	const auto& writes = mister::native::Menu720p60Recipe().adv_initialization;
	for (std::size_t index = 0; index < writes.size(); ++index) {
		Fixture fixture;
		fixture.i2c.fail_write_index = index;
		const auto result = fixture.video.BringUp("MENU", kDeadline);
		ExpectFailure(fixture, result, "hdmi_init", "scripted I2C write failure");
		const auto actual = fixture.i2c.InitializationWrites();
		assert(actual.size() == index + 1);
		assert(fixture.i2c.calls.size() == index + 2);
		assert(fixture.i2c.calls[0].type ==
			mister_test::FakeI2c::CallType::select);
		for (std::size_t written = 0; written <= index; ++written) {
			assert(fixture.i2c.calls[written + 1].type ==
				mister_test::FakeI2c::CallType::write);
			assert(actual[written].address == writes[written].address);
			assert(actual[written].value == writes[written].value);
		}
		assert(CountEvent(fixture.events, "spi:timing") == 0);
		assert(CountEvent(fixture.events, "spi:status_release") == 0);
		assert(fixture.ordered_calls ==
			ExpectedAfterInitializationWrites(index + 1));
		++scenarios;
	}
}

void TestPostInitializationPowerReadFailureStopsBeforeTiming()
{
	Fixture fixture;
	fixture.i2c.power_read_error = {mister::ErrorCode::io_failed,
		"power read failed"};
	const auto result = fixture.video.BringUp("MENU", kDeadline);
	ExpectFailure(fixture, result, "hdmi_init", "power read failed");
	assert(CountEvent(fixture.events, "i2c:read:0x41") == 1);
	assert(CountEvent(fixture.events, "spi:timing") == 0);
	assert(CountEvent(fixture.events, "spi:status_release") == 0);
	assert(fixture.ordered_calls == ExpectedAfterPowerRead());
	++scenarios;
}

void TestFullTimingExchangeFailureStopsBeforeModeWrites()
{
	Fixture fixture;
	fixture.spi.fail_call_index = 2;
	const auto result = fixture.video.BringUp("MENU", kDeadline);
	ExpectFailure(fixture, result, "video_timing", "scripted SPI failure");
	assert(fixture.spi.TimingCall().request ==
		mister::native::Menu720p60Recipe().timing_words);
	assert(fixture.i2c.ModeWrites().empty());
	assert(CountEvent(fixture.events, "spi:status_release") == 0);
	assert(fixture.ordered_calls == ExpectedAfterTimingAttempt());
	++scenarios;
}

void TestEveryModeWriteFailureStopsAtThatExactWrite()
{
	const auto& initialization =
		mister::native::Menu720p60Recipe().adv_initialization;
	const auto& mode = mister::native::Menu720p60Recipe().adv_mode;
	for (std::size_t index = 0; index < mode.size(); ++index) {
		Fixture fixture;
		fixture.i2c.fail_write_index = initialization.size() + index;
		const auto result = fixture.video.BringUp("MENU", kDeadline);
		ExpectFailure(fixture, result, "video_timing",
			"scripted I2C write failure");
		const auto actual = fixture.i2c.ModeWrites();
		assert(actual.size() == index + 1);
		for (std::size_t written = 0; written <= index; ++written) {
			assert(actual[written].address == mode[written].address);
			assert(actual[written].value == mode[written].value);
		}
		assert(CountEvent(fixture.events, "spi:status_release") == 0);
		assert(fixture.ordered_calls == ExpectedAfterModeWrites(index + 1));
		++scenarios;
	}
}

void TestSoftwareResetReleaseFailureStopsAtReleaseAttempt()
{
	Fixture fixture;
	fixture.spi.fail_call_index = 3;
	const auto result = fixture.video.BringUp("MENU", kDeadline);
	ExpectFailure(fixture, result, "core_release", "scripted SPI failure");
	assert(CountEvent(fixture.events, "spi:status_release") == 1);
	assert(CountEvent(fixture.events, "i2c:read:0x42") == 0);
	assert(fixture.ordered_calls == ExpectedAfterReleaseAttempt());
	++scenarios;
}

void TestLinkReadTransportFailureStopsAtRead()
{
	Fixture fixture;
	fixture.i2c.link_read_error = {mister::ErrorCode::io_failed,
		"link read failed"};
	const auto result = fixture.video.BringUp("MENU", kDeadline);
	ExpectFailure(fixture, result, "hdmi_verify", "link read failed");
	assert(CountEvent(fixture.events, "i2c:read:0x42") == 1);
	assert(CountEvent(fixture.events, "spi:status_release") == 1);
	assert(fixture.ordered_calls == ExpectedAfterLinkReads(1));
	++scenarios;
}

void TestLinkPollingRepeatsOnlyStatusReadUntilBothBitsAreSet()
{
	Fixture fixture({0, 1, 2, 3, 4});
	fixture.i2c.link_statuses = {0x00, 0x20, 0x40, 0x60};
	const auto result = fixture.video.BringUp("MENU", kDeadline);
	assert(result.error.ok());
	assert(result.link_status == 0x60);
	assert(CountEvent(fixture.events, "i2c:read:0x42") == 4);
	assert(CountEvent(fixture.events, "spi:status_assert") == 1);
	assert(CountEvent(fixture.events, "spi:probe") == 1);
	assert(CountEvent(fixture.events, "i2c:initialization") == 1);
	assert(CountEvent(fixture.events, "spi:timing") == 1);
	assert(CountEvent(fixture.events, "i2c:mode") == 1);
	assert(CountEvent(fixture.events, "spi:status_release") == 1);
	const auto& recipe = mister::native::Menu720p60Recipe();
	assert(fixture.i2c.calls.size() == kLinkReadCallBegin + 4);
	for (std::size_t index = kLinkReadCallBegin;
		index < fixture.i2c.calls.size(); ++index) {
		assert(fixture.i2c.calls[index].type ==
			mister_test::FakeI2c::CallType::read);
		assert(fixture.i2c.calls[index].address == 0x42);
		assert(fixture.i2c.calls[index].deadline == kDeadline);
	}
	assert(EqualWrites(fixture.i2c.InitializationWrites(),
		recipe.adv_initialization));
	assert(EqualWrites(fixture.i2c.ModeWrites(),
		recipe.adv_mode));
	assert(fixture.ordered_calls == ExpectedAfterLinkReads(4));
	++scenarios;
}

void TestEachIncompleteLinkPredicateExpiresAfterExactlyFourReads()
{
	const std::uint8_t statuses[] = {0x00, 0x20, 0x40};
	for (std::uint8_t status : statuses) {
		Fixture fixture({0, 1, 2, 3, 4, 5});
		fixture.i2c.link_statuses = {status};
		const auto result = fixture.video.BringUp("MENU", 5);
		assert(result.error.code == mister::ErrorCode::io_failed);
		ExpectFailure(fixture, result, "hdmi_verify", "deadline exceeded");
		assert(result.link_status == status);
		assert(CountI2cCalls(fixture.i2c,
			mister_test::FakeI2c::CallType::read, 0x42) == 4);
		assert(CountEvent(fixture.events, "spi:status_assert") == 1);
		assert(CountEvent(fixture.events, "i2c:initialization") == 1);
		assert(CountEvent(fixture.events, "spi:timing") == 1);
		assert(CountEvent(fixture.events, "i2c:mode") == 1);
		assert(CountEvent(fixture.events, "spi:status_release") == 1);
		assert(fixture.i2c.calls.size() == kLinkReadCallBegin + 4);
		for (std::size_t index = kLinkReadCallBegin;
			index < fixture.i2c.calls.size(); ++index) {
			assert(fixture.i2c.calls[index].type ==
				mister_test::FakeI2c::CallType::read);
			assert(fixture.i2c.calls[index].address == 0x42);
			assert(fixture.i2c.calls[index].deadline == 5);
		}
		assert(fixture.ordered_calls == ExpectedAfterLinkReads(4));
		++scenarios;
	}
}

void TestEveryFailurePhaseLogsItsDirectIoFailure()
{
	struct Case { const char* expected_phase; std::size_t spi_failure; };
	const Case spi_cases[] = {{"core_reset", 0}, {"core_probe", 1},
		{"video_timing", 2}, {"core_release", 3}};
	for (const Case& test : spi_cases) {
		Fixture fixture;
		fixture.spi.fail_call_index = test.spi_failure;
		const auto result = fixture.video.BringUp("MENU", kDeadline);
		ExpectFailure(fixture, result, test.expected_phase, "scripted SPI failure");
		if (test.spi_failure == 0)
			assert((fixture.ordered_calls ==
				std::vector<std::string>{"spi:status_assert"}));
		else if (test.spi_failure == 1)
			assert(fixture.ordered_calls == ExpectedAfterProbe());
		else if (test.spi_failure == 2)
			assert(fixture.ordered_calls == ExpectedAfterTimingAttempt());
		else
			assert(fixture.ordered_calls == ExpectedAfterReleaseAttempt());
		++scenarios;
	}
	{
		Fixture fixture;
		fixture.i2c.select_error = {mister::ErrorCode::io_failed,
			"scripted HDMI initialization failure"};
		const auto result = fixture.video.BringUp("MENU", kDeadline);
		ExpectFailure(fixture, result, "hdmi_init",
			"scripted HDMI initialization failure");
		assert(fixture.ordered_calls == ExpectedAfterSelection());
		++scenarios;
	}
	{
		Fixture fixture;
		fixture.i2c.link_read_error = {mister::ErrorCode::io_failed,
			"scripted HDMI verification failure"};
		const auto result = fixture.video.BringUp("MENU", kDeadline);
		ExpectFailure(fixture, result, "hdmi_verify",
			"scripted HDMI verification failure");
		assert(fixture.ordered_calls == ExpectedAfterLinkReads(1));
		++scenarios;
	}
}

} // namespace

int main()
{
	TestSuccessUsesExactOrderWireRequestsDeadlineDiagnosticsAndLogs();
	TestExpiredBeforeResetMakesNoHardwareCall();
	TestResetAssertionFailureStopsAtAttempt();
	TestProbeTransportAndMalformedFailuresStopAtProbe();
	TestOnlyExactTerminatedUppercaseMenuIdentityIsAccepted();
	TestNoAdvResponderStopsAtSelection();
	TestEveryInitializationWriteFailureStopsAtThatExactWrite();
	TestPostInitializationPowerReadFailureStopsBeforeTiming();
	TestFullTimingExchangeFailureStopsBeforeModeWrites();
	TestEveryModeWriteFailureStopsAtThatExactWrite();
	TestSoftwareResetReleaseFailureStopsAtReleaseAttempt();
	TestLinkReadTransportFailureStopsAtRead();
	TestLinkPollingRepeatsOnlyStatusReadUntilBothBitsAreSet();
	TestEachIncompleteLinkPredicateExpiresAfterExactlyFourReads();
	TestEveryFailurePhaseLogsItsDirectIoFailure();
	printf("video_test: %zu scenarios passed\n", scenarios);
	return 0;
}
