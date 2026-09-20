// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "capture_log.hpp"
#include "fake_i2c.hpp"
#include "native/artifacts.hpp"
#include "native/core_loader.hpp"
#include "native/linux/spi.hpp"
#include "native/video.hpp"
#include "native/framebuffer.hpp"
#include "native/video_recipe.hpp"
#include "native/adv7513.hpp"

#include <assert.h>
#include <fcntl.h>
#include <stdio.h>
#include <unistd.h>

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
// Selection, 93 initialization writes, the power read, three mode writes, and
// five ADV EDID wake writes.
const std::size_t kLinkReadCallBegin = 103;
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

	mister::Error SynchronizeCore(std::uint64_t deadline) override
	{
		sync_deadline = deadline;
		events_.push_back("spi:core_sync");
		ordered_calls_.push_back("spi:core_sync");
		return sync_error;
	}

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
		} else if (request.size() == 2 && request[0] == 0x0001) {
			event = "spi:buttons";
		} else if (request.size() == 9 && request[0] == 0x001e &&
			request[1] == 0x0001) {
			event = "spi:status_assert";
		} else if (request.size() == 9 && request[0] == 0x001e &&
			request[1] == 0x0000) {
			event = "spi:status_release";
		} else {
			event = request[0] == 0x002f ? "spi:framebuffer" : "spi:unexpected";
  }
		if (request.size() == 9 && request[0] == 0x001e &&
			request[1] == 0x0000)
			i2c_.MarkReleaseEvent();
		events_.push_back(event);
		ordered_calls_.push_back(event);
		if (call_index == fail_call_index) return failure;
		if (response != nullptr) {
			response->assign(request.size(), terminate_identity ? 0 : 'X');
   if (request[0] == 0x002f) (*response)[0] = framebuffer_supported ? 1 : 0;
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
 bool framebuffer_supported = true;
	bool invalid_identity = false;
	std::size_t fail_call_index = std::numeric_limits<std::size_t>::max();
	mister::Error failure = {mister::ErrorCode::io_failed,
		"scripted SPI failure"};
	mister::Error sync_error;
	std::uint64_t sync_deadline = 0;
	std::vector<Call> calls;
};

class FakeFramebuffer final : public mister::native::Framebuffer {
public:
 mister::Error Prepare(std::uint64_t deadline, mister::native::FramebufferMode* out) override {
  ++calls; last_deadline=deadline; *out=mode; return error;
 }
 mister::native::FramebufferMode mode={0x22001000,640*480*4,640,480,2560};
 mister::Error error;
 int calls=0;
 std::uint64_t last_deadline=0;
};

struct Fixture {
	explicit Fixture(std::vector<std::uint64_t> clock_values = {0, 1, 2, 3, 4})
		: clock(std::move(clock_values)), i2c(&events, &ordered_calls),
		spi(events, ordered_calls, i2c),
		core(spi), video(core, spi, i2c, framebuffer, clock, log,
			mister::native::Menu720p60Recipe()) {}
	std::vector<std::string> events;
	std::vector<std::string> ordered_calls;
	SequenceClock clock;
	mister_test::FakeI2c i2c;
	RecordingSpi spi;
	mister::native::CoreLoader core;
	mister_test::CaptureLog log;
	FakeFramebuffer framebuffer;
	mister::native::MenuVideoBringup video;
};

struct TempMedia {
	TempMedia()
	{
		char pattern[] = "/tmp/libmister-video-sequence.XXXXXX.bin";
		const int descriptor = mkstemps(pattern, 4);
		assert(descriptor >= 0);
		path = pattern;
		const unsigned char bytes[] = {0x10, 0x32, 0x54, 0x76};
		assert(write(descriptor, bytes, sizeof(bytes)) ==
			static_cast<ssize_t>(sizeof(bytes)));
		assert(close(descriptor) == 0);
	}
	~TempMedia() { assert(unlink(path.c_str()) == 0); }
	std::string path;
};

struct AttemptGate {
	mister::Error Call(const std::string& name)
	{
		attempts.push_back(name);
		const std::size_t current = next++;
		if (current == fail_at)
			return {mister::ErrorCode::io_failed, "scripted component failure"};
		return {};
	}
	std::size_t fail_at = std::numeric_limits<std::size_t>::max();
	std::size_t next = 0;
	std::vector<std::string> attempts;
};

class ChronologySpi final : public mister::native::Spi {
public:
	ChronologySpi(AttemptGate& gate, std::vector<std::string>& ledger)
		: gate_(gate), ledger_(ledger) {}

	mister::Error SynchronizeCore(std::uint64_t) override
	{
		return gate_.Call("spi:sync");
	}

	mister::Error Exchange(std::uint8_t target,
		const std::vector<std::uint16_t>& request,
		std::vector<std::uint16_t>* response, std::uint64_t) override
	{
		assert(!request.empty());
		std::string attempt = "spi:unexpected";
		if (target == mister::native::kUserIoTarget &&
			request.size() == 9 && request[0] == 0x001e) {
			attempt = status_count_ == 0 ? "spi:reset.assert" :
				status_count_ == 1 ? "spi:status.initial" : "spi:reset.release";
			ledger_.push_back(status_count_ == 0 ? "core.reset.assert" :
				status_count_ == 1 ? "core.status.initial" :
				"core.reset.release");
			++status_count_;
		} else if (target == mister::native::kUserIoTarget &&
			request[0] == 0x0014) {
			attempt = "spi:probe";
			ledger_.push_back("core.probe:MegaDrive");
		} else if (target == mister::native::kFileIoTarget &&
			request[0] == 0x0055) {
			attempt = "spi:media.select";
			ledger_.push_back("core.media.select:" +
				std::to_string(request.at(1)));
		} else if (target == mister::native::kFileIoTarget &&
			request[0] == 0x0056) {
			assert(request ==
				std::vector<std::uint16_t>({0x0056, 0x622e, 0x6e69}));
			attempt = "spi:media.extension";
			ledger_.push_back("core.media.extension:.bin");
		} else if (target == mister::native::kFileIoTarget &&
			request == std::vector<std::uint16_t>({0x0053, 0x00ff})) {
			attempt = "spi:media.enable";
			ledger_.push_back("core.media.enable");
		} else if (target == mister::native::kFileIoTarget &&
			request[0] == 0x0054) {
			assert(request ==
				std::vector<std::uint16_t>({0x0054, 0x3210, 0x7654}));
			attempt = "spi:media.data";
			ledger_.push_back("core.media.data:all bytes once");
		} else if (target == mister::native::kUserIoTarget &&
			request[0] == 0x0029) {
			attempt = "spi:media.index.clear";
		} else if (target == mister::native::kFileIoTarget &&
			request == std::vector<std::uint16_t>({0x0053, 0x0000})) {
			attempt = "spi:media.complete";
			ledger_.push_back("core.media.complete");
		} else if (target == mister::native::kUserIoTarget &&
			request[0] == 0x0020) {
			attempt = "spi:video.timing";
			ledger_.push_back("video.timing:menu_720p60");
		} else if (target == mister::native::kUserIoTarget &&
			request == std::vector<std::uint16_t>({0x0001, 0x0000})) {
			attempt = "spi:buttons.neutral";
			ledger_.push_back("core.buttons.neutral");
		} else if (target == mister::native::kUserIoTarget &&
			request == std::vector<std::uint16_t>({0x0026, 0x0000})) {
			attempt = "spi:audio.volume";
			ledger_.push_back("audio.volume:0");
		}
		const mister::Error error = gate_.Call(attempt);
		if (!error.ok()) return error;
		if (response != nullptr && request[0] == 0x0014) {
			response->assign(request.size(), 0);
			const std::string identity = "MegaDrive";
			std::size_t index = 1;
			for (unsigned char byte : identity) (*response)[index++] = byte;
			(*response)[index] = ';';
		}
		return {};
	}

private:
	AttemptGate& gate_;
	std::vector<std::string>& ledger_;
	std::size_t status_count_ = 0;
};

class ChronologyI2c final : public mister::native::I2c {
public:
	ChronologyI2c(AttemptGate& gate, std::vector<std::string>& ledger)
		: gate_(gate), ledger_(ledger) {}

	mister::Error SelectFirst(std::uint8_t slave, std::uint8_t detection,
		std::uint64_t, std::string* bus, std::uint8_t* value) override
	{
		assert(slave == 0x39);
		assert(detection == 0x41);
		ledger_.push_back("video.adv.initialize");
		const mister::Error error = gate_.Call("i2c:select");
		if (!error.ok()) return error;
		*bus = "/dev/i2c-1";
		*value = 0x40;
		return {};
	}

	mister::Error ReadByte(std::uint8_t address, std::uint8_t* value,
		std::uint64_t) override
	{
		const mister::Error error = gate_.Call(
			address == 0x41 ? "i2c:power.read" : "i2c:link.read");
		if (!error.ok()) return error;
		if (address == 0x41) {
			*value = 0x10;
		} else {
			assert(address == 0x42);
			*value = 0x60;
			ledger_.push_back("video.link.ready");
		}
		return {};
	}

	mister::Error WriteByte(std::uint8_t address, std::uint8_t value,
		std::uint64_t) override
	{
		writes.push_back({address, value});
		const std::size_t initialization =
			mister::native::Menu720p60Recipe().adv_initialization.size();
		const std::size_t mode =
			mister::native::Menu720p60Recipe().adv_mode.size();
		if (write_count_ == initialization)
			ledger_.push_back("video.adv.mode");
		if (write_count_ == initialization + mode)
			ledger_.push_back("video.adv.wake");
		const char* phase = write_count_ < initialization ? "i2c:init.write" :
			write_count_ < initialization + mode ? "i2c:mode.write" :
			"i2c:wake.write";
		++write_count_;
		return gate_.Call(phase);
	}
	std::vector<mister::native::RegisterWrite> writes;

private:
	AttemptGate& gate_;
	std::vector<std::string>& ledger_;
	std::size_t write_count_ = 0;
};

struct ChronologyFixture {
	explicit ChronologyFixture(
		std::size_t fail_at = std::numeric_limits<std::size_t>::max())
		: spi(gate, ledger), i2c(gate, ledger), core(spi),
		video(spi, i2c, clock, log, mister::native::Menu720p60Recipe())
	{
		gate.fail_at = fail_at;
		assert(opener.Open(media.path, 0, &artifact).ok());
	}
	TempMedia media;
	mister::native::PosixArtifactOpener opener;
	mister::native::Artifact artifact;
	AttemptGate gate;
	std::vector<std::string> ledger;
	ChronologySpi spi;
	ChronologyI2c i2c;
	SequenceClock clock;
	mister_test::CaptureLog log;
	mister::native::CoreLoader core;
	mister::native::FixedVideoBringup video;
};

class GenericResetSpi final : public mister::native::Spi {
public:
	mister::Error SynchronizeCore(std::uint64_t) override { return {}; }

	mister::Error Exchange(std::uint8_t target,
		const std::vector<std::uint16_t>& request,
		std::vector<std::uint16_t>* response, std::uint64_t) override
	{
		assert(!request.empty());
		if (target == mister::native::kUserIoTarget &&
			request.size() == 9 && request[0] == 0x001e) {
			++status_count;
			if (status_count == 3 && request[1] == 0 && generic_reset_asserted)
				return {mister::ErrorCode::io_failed,
					"generic button reset remained asserted"};
		} else if (target == mister::native::kUserIoTarget &&
			request[0] == 0x0014 && response != nullptr) {
			response->assign(request.size(), 0);
			const std::string identity = "MegaDrive";
			std::size_t index = 1;
			for (unsigned char byte : identity) (*response)[index++] = byte;
			(*response)[index] = ';';
		} else if (target == mister::native::kUserIoTarget &&
			request == std::vector<std::uint16_t>({0x0001, 0x0000})) {
			generic_reset_asserted = false;
			++neutral_button_count;
		}
		return {};
	}

	bool generic_reset_asserted = true;
	std::size_t neutral_button_count = 0;
	std::size_t status_count = 0;
};

void TestFixedGameVideoNeutralizesGenericResetBeforeStatusRelease()
{
	TempMedia media;
	mister::native::PosixArtifactOpener opener;
	mister::native::Artifact artifact;
	assert(opener.Open(media.path, 0, &artifact).ok());
	GenericResetSpi spi;
	std::vector<std::string> events;
	std::vector<std::string> ordered_calls;
	mister_test::FakeI2c i2c(&events, &ordered_calls);
	SequenceClock clock;
	mister_test::CaptureLog log;
	mister::native::CoreLoader core(spi);
	mister::native::FixedVideoBringup video(spi, i2c, clock, log,
		mister::native::Menu720p60Recipe());
	const mister::CoreRecipe recipe = {0x0001, 0x0001, 0x0000,
		mister::FileWireFormat::little_endian_byte_pairs};
	assert(core.AssertReset(recipe, kDeadline).ok());
	std::string observed;
	assert(core.Probe(&observed, kDeadline).ok());
	assert(observed == "MegaDrive");
	assert(core.ApplyInitialStatus(recipe, kDeadline).ok());
	assert(core.Attach(1, artifact, recipe.file_wire, kDeadline).ok());
	assert(video.BringUp(kDeadline).error.ok());
	assert(core.ReleaseReset(recipe, kDeadline).ok());
	assert(spi.neutral_button_count == 1);
	assert(!spi.generic_reset_asserted);
	++scenarios;
}

void TestQuiesceUsesOneReadModifyWriteWithTheCallerDeadline()
{
	Fixture menu;
	menu.i2c.detection_value = 0x15;
	const mister::native::VideoQuiesceResult menu_result =
		menu.video.Quiesce(kDeadline);
	assert(menu_result.error.ok());
	assert(menu_result.mutation_attempted);
	assert(menu.spi.calls.empty());
	assert(menu.i2c.calls.size() == 2);
	assert(menu.i2c.calls[0].type == mister_test::FakeI2c::CallType::select);
	assert(menu.i2c.calls[0].address == 0x39);
	assert(menu.i2c.calls[0].value == 0x41);
	assert(menu.i2c.calls[1].type == mister_test::FakeI2c::CallType::write);
	assert(menu.i2c.calls[1].address == 0x41);
	assert(menu.i2c.calls[1].value == 0x55);
	for (const auto& call : menu.i2c.calls) assert(call.deadline == kDeadline);

	Fixture game_fixture;
	game_fixture.i2c.detection_value = 0x10;
	mister::native::FixedVideoBringup game(game_fixture.spi, game_fixture.i2c,
		game_fixture.clock, game_fixture.log,
		mister::native::Menu720p60Recipe());
	const mister::native::VideoQuiesceResult game_result = game.Quiesce(kDeadline);
	assert(game_result.error.ok());
	assert(game_result.mutation_attempted);
	assert(game_fixture.i2c.calls.size() == 2);
	assert(game_fixture.i2c.calls[1].address == 0x41);
	assert(game_fixture.i2c.calls[1].value == 0x50);
	++scenarios;
}

void TestQuiesceFailureStopsAtTheExactBoundedAttempt()
{
	Fixture expired({100});
	const mister::native::VideoQuiesceResult expired_result =
		expired.video.Quiesce(kDeadline);
	assert(expired_result.error.code ==
		mister::ErrorCode::io_failed);
	assert(!expired_result.mutation_attempted);
	assert(expired.i2c.calls.empty());

	Fixture selection;
	selection.i2c.select_error = {
		mister::ErrorCode::io_failed, "scripted quiesce selection failure"};
	const mister::native::VideoQuiesceResult select_result =
		selection.video.Quiesce(kDeadline);
	assert(select_result.error.code == mister::ErrorCode::io_failed);
	assert(select_result.error.message == "scripted quiesce selection failure");
	assert(!select_result.mutation_attempted);
	assert(selection.i2c.calls.size() == 1);

	Fixture write;
	write.i2c.fail_write_index = 0;
	const mister::native::VideoQuiesceResult write_result =
		write.video.Quiesce(kDeadline);
	assert(write_result.error.code == mister::ErrorCode::io_failed);
	assert(write_result.error.message == "scripted I2C write failure");
	assert(write_result.mutation_attempted);
	assert(write.i2c.calls.size() == 2);
	assert(write.i2c.calls[1].address == 0x41);
	assert(write.i2c.calls[1].value == 0x40);
	++scenarios;
}

mister::Error RunPostProgramComponents(ChronologyFixture& fixture)
{
	const mister::CoreRecipe recipe = {0x0001, 0x0001, 0x0000,
		mister::FileWireFormat::little_endian_byte_pairs};
	mister::Error error = fixture.core.AssertReset(recipe, kDeadline);
	if (!error.ok()) return error;
	std::string observed;
	error = fixture.core.Probe(&observed, kDeadline);
	if (!error.ok()) return error;
	if (observed != "MegaDrive")
		return {mister::ErrorCode::core_mismatch, "unexpected test core"};
	error = fixture.core.ApplyInitialStatus(recipe, kDeadline);
	if (!error.ok()) return error;
	error = fixture.core.Attach(1, fixture.artifact, recipe.file_wire, kDeadline);
	if (!error.ok()) return error;
	return fixture.video.BringUp(kDeadline).error;
}

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
	return {"spi:core_sync", "spi:status_assert", "spi:probe"};
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

std::vector<mister::native::RegisterWrite> ExpectedHdmiWakeWrites()
{
	return {{0x96, 0x04}, {0xc4, 0x00}, {0xc9, 0x03},
		{0xc9, 0x13}, {0xc9, 0x03}};
}

std::vector<std::string> ExpectedAfterHdmiWake()
{
	std::vector<std::string> expected = ExpectedAfterReleaseAttempt();
	for (const auto& write : ExpectedHdmiWakeWrites())
		expected.push_back("i2c:write:" + HexByte(write.address) + ":" +
			HexByte(write.value));
	return expected;
}

std::vector<std::string> ExpectedAfterHdmiWakeWrites(std::size_t count)
{
	std::vector<std::string> expected = ExpectedAfterReleaseAttempt();
	const auto wake = ExpectedHdmiWakeWrites();
	assert(count <= wake.size());
	for (std::size_t index = 0; index < count; ++index)
		expected.push_back("i2c:write:" + HexByte(wake[index].address) + ":" +
			HexByte(wake[index].value));
	return expected;
}

std::vector<std::string> ExpectedAfterCoreInput()
{
	std::vector<std::string> expected = ExpectedAfterHdmiWake();
	expected.push_back("spi:buttons");
	return expected;
}

std::vector<std::string> ExpectedAfterLinkReads(std::size_t count)
{
	std::vector<std::string> expected = ExpectedAfterCoreInput();
 expected.push_back("spi:framebuffer");
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

void TestFramebufferFailurePreventsIdleVerification()
{
 for(int index=0;index<4;++index) {
  Fixture fixture;
  if(index==0) fixture.framebuffer.error={mister::ErrorCode::io_failed,"framebuffer unavailable"};
  if(index==1) fixture.framebuffer.mode.stride=2564;
  if(index==2) fixture.spi.fail_call_index=5;
  if(index==3) fixture.spi.framebuffer_supported=false;
  const auto result=fixture.video.BringUp("MENU",kDeadline);
  ExpectFailure(fixture,result,"framebuffer");
  assert(CountEvent(fixture.events,"i2c:read:0x42")==0);
  assert(fixture.framebuffer.last_deadline==kDeadline);
 }
 ++scenarios;
}

void TestIdleEnablesFramebufferOnEveryBringup()
{
 Fixture fixture;
 for (int iteration=0; iteration<2; ++iteration) {
  const auto before=fixture.spi.calls.size();
  assert(fixture.video.BringUp("MENU", kDeadline).error.ok());
  bool enabled=false;
  for (std::size_t i=before; i<fixture.spi.calls.size(); ++i) {
   if(fixture.spi.calls[i].request[0]==0x002f) {
    assert(fixture.spi.calls[i].request == std::vector<std::uint16_t>({0x002f,0x8016,0x1000,0x2200,640,480,0,1279,0,719,2560}));
    enabled=true;
   }
  }
  assert(enabled);
 }
 ++scenarios;
}

void TestSuccessUsesExactOrderWireRequestsDeadlineDiagnosticsAndLogs()
{
	Fixture fixture;
	const mister::native::VideoResult result = fixture.video.BringUp("MENU", kDeadline);
	const std::vector<std::string> expected = {
		"spi:core_sync", "spi:status_assert", "spi:probe",
		"i2c:select:/dev/i2c-1:0x39:0x41", "i2c:initialization",
		"i2c:read:0x41", "spi:timing", "i2c:mode",
		"spi:status_release", "i2c:hdmi_wake", "spi:buttons",
  "spi:framebuffer",
		"i2c:read:0x42",
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
	assert(fixture.spi.calls[3].request == released);
	assert(EqualWrites(fixture.i2c.InitializationWrites(),
		mister::native::Menu720p60Recipe().adv_initialization));
	assert(EqualWrites(fixture.i2c.ModeWrites(),
		mister::native::Menu720p60Recipe().adv_mode));
	assert(EqualWrites(fixture.i2c.HdmiWakeWrites(), ExpectedHdmiWakeWrites()));
	assert(fixture.spi.calls[4].request ==
		std::vector<std::uint16_t>({0x0001, 0x0000}));
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
	const std::vector<std::string> phases = {"core_sync", "core_reset", "core_probe",
		"hdmi_init", "video_timing", "core_release", "hdmi_wake",
		"core_input", "framebuffer", "hdmi_verify"};
	assert(records.size() == phases.size());
	for (std::size_t index = 0; index < records.size(); ++index) {
		assert(records[index].operation == "start");
		assert(records[index].system.empty());
		assert(records[index].phase == phases[index]);
		assert(records[index].error.code == mister::ErrorCode::none);
		if (index <= 1) assert(records[index].core.empty());
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

void TestCoreSynchronizationFailureStopsBeforeReset()
{
	Fixture fixture;
	fixture.spi.sync_error = {mister::ErrorCode::io_failed,
		"scripted core synchronization failure"};
	const auto result = fixture.video.BringUp("MENU", kDeadline);
	ExpectFailure(fixture, result, "core_sync",
		"scripted core synchronization failure");
	assert((fixture.events == std::vector<std::string>{"spi:core_sync"}));
	assert(fixture.ordered_calls == fixture.events);
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
	assert((fixture.events == std::vector<std::string>{
		"spi:core_sync", "spi:status_assert"}));
	assert((fixture.ordered_calls ==
		std::vector<std::string>{"spi:core_sync", "spi:status_assert"}));
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
			"spi:core_sync", "spi:status_assert", "spi:probe"}));
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

void TestEveryEdidWakeWriteFailureStopsBeforeCoreInput()
{
	const auto wake = ExpectedHdmiWakeWrites();
	const std::size_t mode_count =
		mister::native::Menu720p60Recipe().adv_mode.size();
	const std::size_t init_count =
		mister::native::Menu720p60Recipe().adv_initialization.size();
	for (std::size_t index = 0; index < wake.size(); ++index) {
		Fixture fixture;
		fixture.i2c.fail_write_index = init_count + mode_count + index;
		const auto result = fixture.video.BringUp("MENU", kDeadline);
		ExpectFailure(fixture, result, "hdmi_wake",
			"scripted I2C write failure");
		const auto actual = fixture.i2c.HdmiWakeWrites();
		assert(actual.size() == index + 1);
		for (std::size_t written = 0; written <= index; ++written) {
			assert(actual[written].address == wake[written].address);
			assert(actual[written].value == wake[written].value);
		}
		assert(CountEvent(fixture.events, "spi:buttons") == 0);
		assert(CountEvent(fixture.events, "i2c:read:0x42") == 0);
		assert(fixture.ordered_calls == ExpectedAfterHdmiWakeWrites(index + 1));
		++scenarios;
	}
}

void TestNeutralButtonFailureStopsBeforeLinkVerification()
{
	Fixture fixture;
	fixture.spi.fail_call_index = 4;
	const auto result = fixture.video.BringUp("MENU", kDeadline);
	ExpectFailure(fixture, result, "core_input", "scripted SPI failure");
	assert(CountEvent(fixture.events, "spi:buttons") == 1);
	assert(CountEvent(fixture.events, "i2c:read:0x42") == 0);
	assert(fixture.ordered_calls == ExpectedAfterCoreInput());
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
				std::vector<std::string>{"spi:core_sync", "spi:status_assert"}));
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
	{
		Fixture fixture;
		fixture.i2c.fail_write_index =
			mister::native::Menu720p60Recipe().adv_initialization.size() +
			mister::native::Menu720p60Recipe().adv_mode.size();
		const auto result = fixture.video.BringUp("MENU", kDeadline);
		ExpectFailure(fixture, result, "hdmi_wake",
			"scripted I2C write failure");
		assert(fixture.ordered_calls == ExpectedAfterHdmiWakeWrites(1));
		++scenarios;
	}
	{
		Fixture fixture;
		fixture.spi.fail_call_index = 4;
		const auto result = fixture.video.BringUp("MENU", kDeadline);
		ExpectFailure(fixture, result, "core_input", "scripted SPI failure");
		assert(fixture.ordered_calls == ExpectedAfterCoreInput());
		++scenarios;
	}
}

void TestPostProgramComponentsUseExactMegaDriveChronologyWithoutRelease()
{
	ChronologyFixture fixture;
	const mister::Error error = RunPostProgramComponents(fixture);
	assert(error.ok());
	const std::vector<std::string> expected = {
		"core.reset.assert",
		"core.probe:MegaDrive",
		"core.status.initial",
		"core.media.select:1",
		"core.media.extension:.bin",
		"core.media.enable",
		"core.media.data:all bytes once",
		"core.media.complete",
		"video.adv.initialize",
		"video.timing:menu_720p60",
		"video.adv.mode",
		"video.adv.wake",
		"core.buttons.neutral",
		"video.link.ready",
		"audio.volume:0",
	};
	assert(fixture.ledger == expected);
	assert(fixture.i2c.writes.size() == 101);
	const std::vector<mister::native::RegisterWrite> expected_wake = {
		{0x96, 0x04}, {0xc4, 0x00}, {0xc9, 0x03},
		{0xc9, 0x13}, {0xc9, 0x03}};
	for (std::size_t index = 0; index < expected_wake.size(); ++index) {
		const auto& actual = fixture.i2c.writes[96 + index];
		assert(actual.address == expected_wake[index].address);
		assert(actual.value == expected_wake[index].value);
	}
	assert(std::find(fixture.ledger.begin(), fixture.ledger.end(),
		"input.neutral") == fixture.ledger.end());
	assert(std::find(fixture.ledger.begin(), fixture.ledger.end(),
		"core.reset.release") == fixture.ledger.end());
	++scenarios;
}

void TestEveryPostProgramSpiI2cAndReadFailureStopsChronologyAtThatAttempt()
{
	ChronologyFixture success;
	assert(RunPostProgramComponents(success).ok());
	const std::vector<std::string> expected_attempts = success.gate.attempts;
	assert(!expected_attempts.empty());
	for (std::size_t fail = 0; fail < expected_attempts.size(); ++fail) {
		ChronologyFixture fixture(fail);
		const mister::Error error = RunPostProgramComponents(fixture);
		assert(error.code == mister::ErrorCode::io_failed);
		assert(error.message == "scripted component failure");
		assert(fixture.gate.attempts.size() == fail + 1);
		assert(std::equal(fixture.gate.attempts.begin(),
			fixture.gate.attempts.end(), expected_attempts.begin()));
		++scenarios;
	}
}

void TestGameAudioVolumeIsBoundedAndMenuStaysMuted()
{
	Fixture fixture;
	mister::native::FixedVideoBringup video(fixture.spi, fixture.i2c,
		fixture.clock, fixture.log, mister::native::Menu720p60Recipe());
	assert(video.BringUp(kDeadline).error.ok());
	const auto& audio = fixture.spi.calls.back();
	assert(audio.target == mister::native::kUserIoTarget);
	assert(audio.request == std::vector<std::uint16_t>({0x0026, 0x0000}));
	assert(audio.deadline == kDeadline);

	Fixture failed;
	failed.spi.fail_call_index = fixture.spi.calls.size() - 1;
	mister::native::FixedVideoBringup failing(failed.spi, failed.i2c,
		failed.clock, failed.log, mister::native::Menu720p60Recipe());
	const auto result = failing.BringUp(kDeadline);
	assert(result.error.code == mister::ErrorCode::io_failed);
	assert(result.phase == "audio_volume");
	assert(failed.spi.calls.size() == fixture.spi.calls.size());

	Fixture menu;
	assert(menu.video.BringUp("MENU", kDeadline).error.ok());
	for (const auto& call : menu.spi.calls) assert(call.request[0] != 0x0026);
	++scenarios;
}

void TestFixedVideoRequiresBothHpdAndMonitorSenseBeforeReady()
{
	Fixture fixture({0, 1, 2, 3, 4, 5});
	fixture.i2c.link_statuses = {0x20};
	mister::native::FixedVideoBringup video(fixture.spi, fixture.i2c,
		fixture.clock, fixture.log, mister::native::Menu720p60Recipe());
	const mister::native::VideoResult result = video.BringUp(5);
	ExpectFailure(fixture, result, "hdmi_verify", "deadline exceeded");
	assert(result.link_status == 0x20);
	assert(CountI2cCalls(fixture.i2c,
		mister_test::FakeI2c::CallType::read, 0x42) == 4);
	++scenarios;
}

void TestApplicationAudioPolicyAndFailureOrdering()
{
	using namespace mister::native;
	using Type = mister_test::FakeI2c::CallType;
	Fixture fixture;
	FixedVideoBringup video(fixture.spi, fixture.i2c, fixture.clock,
		fixture.log, Menu720p60Recipe());
	assert(video.BringUpCustom(kDeadline, true).error.ok());
	const auto& calls = fixture.i2c.calls;
	assert(calls[1].address == 0x44 && calls[1].value == 0x11);
	assert(calls[calls.size()-2].type == Type::read &&
		calls[calls.size()-2].address == 0x42);
	assert(calls.back().address == 0x44 && calls.back().value == 0x79);
	assert(fixture.spi.calls.empty());
	const std::size_t enabled_calls = calls.size();
	assert(video.Quiesce(kDeadline).error.ok());
	assert(calls.back().address == 0x41 && (calls.back().value & 0x40));
	assert(video.BringUpCustom(kDeadline).error.ok());
	for (std::size_t i = enabled_calls; i < calls.size(); ++i)
		if (calls[i].type == Type::write && calls[i].address == 0x44)
			assert(calls[i].value == 0x11);
	assert(video.BringUpCustom(kDeadline, true).error.ok());
	assert(calls.back().address == 0x44 && calls.back().value == 0x79);
	// Legacy launch explicitly restores packets after a silent application.
	assert(video.BringUpCustom(kDeadline).error.ok());
	const auto legacy_begin = calls.size();
	assert(video.BringUp(kDeadline).error.ok());
	assert(calls[legacy_begin+1].address == 0x44 && calls[legacy_begin+1].value == 0x79);
	// Direct audio-to-legacy transition must also restore its clock source.
	assert(video.BringUpCustom(kDeadline, true).error.ok());
	const auto clock_restore_begin = calls.size();
	assert(video.BringUp(kDeadline).error.ok());
	unsigned restored = 0;
	for (std::size_t i = clock_restore_begin; i < calls.size(); ++i) {
		if (calls[i].type != Type::write) continue;
		if (calls[i].address == 0x0a) { assert(calls[i].value == 0x00); restored |= 1; }
		if (calls[i].address == 0x0b) { assert(calls[i].value == 0x0e); restored |= 2; }
		if (calls[i].address == 0x0c) { assert(calls[i].value == 0x04); restored |= 4; }
	}
	assert(restored == 7);
	// Every setup write failure is bounded; none enables sample packets.
	const auto init_count = Menu720p60Recipe().adv_initialization.size();
	for (std::size_t i = 0; i < adv7513::ApplicationAudio48k().size(); ++i) {
		Fixture failed;
		failed.i2c.fail_write_index = init_count + i;
		FixedVideoBringup attempt(failed.spi, failed.i2c, failed.clock,
			failed.log, Menu720p60Recipe());
		const auto result = attempt.BringUpCustom(kDeadline, true);
		assert(!result.error.ok() && result.phase == "audio_setup");
		for (const auto& call : failed.i2c.calls)
			if (call.type == Type::write && call.address == 0x44)
				assert(call.value == 0x11);
	}
	Fixture failed_link;
	failed_link.i2c.link_read_error = {mister::ErrorCode::io_failed, "link failed"};
	FixedVideoBringup attempt(failed_link.spi, failed_link.i2c, failed_link.clock,
		failed_link.log, Menu720p60Recipe());
	assert(attempt.BringUpCustom(kDeadline, true).phase == "hdmi_verify");
	for (const auto& call : failed_link.i2c.calls)
		if (call.type == Type::write && call.address == 0x44) assert(call.value == 0x11);
	Fixture failed_enable;
	failed_enable.i2c.fail_write_index = init_count + adv7513::ApplicationAudio48k().size() +
		Menu720p60Recipe().adv_mode.size() + adv7513::HdmiWake().size();
	FixedVideoBringup enable(failed_enable.spi, failed_enable.i2c, failed_enable.clock,
		failed_enable.log, Menu720p60Recipe());
	const auto enable_result = enable.BringUpCustom(kDeadline, true);
	assert(!enable_result.error.ok() && enable_result.phase == "audio_enable");
	assert(failed_enable.i2c.calls.back().address == 0x44);
	// An ambiguous enable failure still permits the ordinary bounded power-down.
	assert(enable.Quiesce(kDeadline).error.ok());
	assert(failed_enable.i2c.calls.back().address == 0x41);
	++scenarios;
}

void TestCustomFixedVideoUsesOnlyAdvI2c()
{
	Fixture fixture;
	fixture.spi.fail_call_index = 0;
	mister::native::FixedVideoBringup video(fixture.spi, fixture.i2c,
		fixture.clock, fixture.log, mister::native::Menu720p60Recipe());
	assert(video.BringUpCustom(kDeadline).error.ok());
	assert(fixture.spi.calls.empty());
	++scenarios;
}

} // namespace

int main()
{
	TestCustomFixedVideoUsesOnlyAdvI2c();
	TestApplicationAudioPolicyAndFailureOrdering();
 TestIdleEnablesFramebufferOnEveryBringup();
 TestFramebufferFailurePreventsIdleVerification();
	TestQuiesceUsesOneReadModifyWriteWithTheCallerDeadline();
	TestQuiesceFailureStopsAtTheExactBoundedAttempt();
	TestFixedGameVideoNeutralizesGenericResetBeforeStatusRelease();
	TestSuccessUsesExactOrderWireRequestsDeadlineDiagnosticsAndLogs();
	TestExpiredBeforeResetMakesNoHardwareCall();
	TestCoreSynchronizationFailureStopsBeforeReset();
	TestResetAssertionFailureStopsAtAttempt();
	TestProbeTransportAndMalformedFailuresStopAtProbe();
	TestOnlyExactTerminatedUppercaseMenuIdentityIsAccepted();
	TestNoAdvResponderStopsAtSelection();
	TestEveryInitializationWriteFailureStopsAtThatExactWrite();
	TestPostInitializationPowerReadFailureStopsBeforeTiming();
	TestFullTimingExchangeFailureStopsBeforeModeWrites();
	TestEveryModeWriteFailureStopsAtThatExactWrite();
	TestSoftwareResetReleaseFailureStopsAtReleaseAttempt();
	TestEveryEdidWakeWriteFailureStopsBeforeCoreInput();
	TestNeutralButtonFailureStopsBeforeLinkVerification();
	TestLinkReadTransportFailureStopsAtRead();
	TestLinkPollingRepeatsOnlyStatusReadUntilBothBitsAreSet();
	TestEachIncompleteLinkPredicateExpiresAfterExactlyFourReads();
	TestEveryFailurePhaseLogsItsDirectIoFailure();
	TestPostProgramComponentsUseExactMegaDriveChronologyWithoutRelease();
	TestEveryPostProgramSpiI2cAndReadFailureStopsChronologyAtThatAttempt();
	TestGameAudioVolumeIsBoundedAndMenuStaysMuted();
	TestFixedVideoRequiresBothHpdAndMonitorSenseBeforeReady();
	printf("video_test: %zu scenarios passed\n", scenarios);
	return 0;
}
