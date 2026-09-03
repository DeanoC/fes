// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "capture_log.hpp"
#include "linux/production_hardware.hpp"
#include "native/artifacts.hpp"
#include "native/core_loader.hpp"
#include "native/hardware.hpp"
#include "native/input.hpp"
#include "native/linux/i2c.hpp"
#include "native/linux/input.hpp"
#include "native/linux/spi.hpp"
#include "native/video.hpp"
#include "native/video_recipe.hpp"

#include <assert.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

#include <cstdint>
#include <functional>
#include <memory>
#include <mutex>
#include <string>
#include <utility>
#include <vector>

namespace {

struct TempDirectory {
	TempDirectory()
	{
		char pattern[] = "/tmp/libmister-native-hardware.XXXXXX";
		char* created = mkdtemp(pattern);
		assert(created != nullptr);
		path = created;
	}
	~TempDirectory()
	{
		for (const std::string& file : files) assert(unlink(file.c_str()) == 0);
		assert(rmdir(path.c_str()) == 0);
	}
	std::string File(const std::string& name, const std::string& contents)
	{
		const std::string file = path + "/" + name;
		const int descriptor = open(file.c_str(), O_WRONLY | O_CREAT | O_EXCL, 0600);
		assert(descriptor >= 0);
		assert(write(descriptor, contents.data(), contents.size()) ==
			static_cast<ssize_t>(contents.size()));
		assert(close(descriptor) == 0);
		files.push_back(file);
		return file;
	}
	std::string SparseFile(const std::string& name, std::uint64_t size)
	{
		const std::string file = path + "/" + name;
		const int descriptor = open(file.c_str(), O_WRONLY | O_CREAT | O_EXCL, 0600);
		assert(descriptor >= 0);
		assert(ftruncate(descriptor, static_cast<off_t>(size)) == 0);
		assert(close(descriptor) == 0);
		files.push_back(file);
		return file;
	}
	std::string path;
	std::vector<std::string> files;
};

class FixedClock final : public mister::native::Clock {
public:
	explicit FixedClock(std::uint64_t now) : now_(now) {}
	std::uint64_t NowMs() const override { return now_; }
	std::uint64_t now_;
};

std::string BaseName(const std::string& path)
{
	const std::size_t slash = path.find_last_of('/');
	return slash == std::string::npos ? path : path.substr(slash + 1);
}

class LedgerLog final : public mister::LogSink {
public:
	explicit LedgerLog(std::vector<std::string>& events) : events_(events) {}
	void Write(const mister::LogRecord& record) override
	{
		std::lock_guard<std::mutex> lock(mutex_);
		records_.push_back(record);
		if (record.operation == "launch" && record.phase == "validate" &&
			record.error.ok())
			events_.push_back("profile.validate:" + record.system);
		if (record.operation == "launch" && record.phase == "preflight" &&
			record.error.ok())
			events_.push_back("media.sort:1");
		if (record.operation == "launch" && record.phase == "running")
			events_.push_back("runtime.running_game");
		if (record.operation == "stop" && record.phase == "idle")
			events_.push_back("runtime.idle");
	}
	std::vector<mister::LogRecord> records() const
	{
		std::lock_guard<std::mutex> lock(mutex_);
		return records_;
	}
	void Clear()
	{
		std::lock_guard<std::mutex> lock(mutex_);
		records_.clear();
	}

private:
	std::vector<std::string>& events_;
	mutable std::mutex mutex_;
	std::vector<mister::LogRecord> records_;
};

class RecordingOpener final : public mister::native::ArtifactOpener {
public:
	explicit RecordingOpener(std::vector<std::string>& events)
		: events_(events), delegate(), calls(0), fail_call(0) {}
	mister::Error Open(const std::string& path, std::uint64_t maximum,
		mister::native::Artifact* artifact) override
	{
		++calls;
		events_.push_back("artifact.open:" + BaseName(path));
		if (calls == fail_call)
			return {mister::ErrorCode::io_failed, "injected preflight"};
		return delegate.Open(path, maximum, artifact);
	}
	std::vector<std::string>& events_;
	mister::native::PosixArtifactOpener delegate;
	int calls;
	int fail_call;
};

class RecordingFpga final : public mister::native::FpgaManager {
public:
	explicit RecordingFpga(std::vector<std::string>& events) : events_(events) {}
	mister::native::NativeResult Program(const mister::native::Artifact& artifact,
		std::uint64_t deadline) override
	{
		events_.push_back("fpga.program");
		programmed.push_back(BaseName(artifact.path()));
		deadlines.push_back(deadline);
		++calls;
		if (calls == fail_call) return failure;
		return result;
	}
	std::vector<std::string>& events_;
	mister::native::NativeResult result;
	mister::native::NativeResult failure = {
		{mister::ErrorCode::program_failed, "injected program failure"}, true};
	std::vector<std::string> programmed;
	std::vector<std::uint64_t> deadlines;
	int calls = 0;
	int fail_call = 0;
};

class RecordingVideo final : public mister::native::VideoBringup {
public:
	explicit RecordingVideo(std::vector<std::string>& events) : events_(events)
	{
		result.phase = "hdmi_verify";
		result.observed_core = "MENU";
	}
	mister::native::VideoResult BringUp(const std::string& expected_core,
		std::uint64_t deadline) override
	{
		events_.push_back("idle.video:" + expected_core);
		deadlines.push_back(deadline);
		++calls;
		return result;
	}
	std::vector<std::string>& events_;
	mister::native::VideoResult result;
	std::vector<std::uint64_t> deadlines;
	int calls = 0;
};

class RecordingI2c final : public mister::native::I2c {
public:
	explicit RecordingI2c(std::vector<std::string>& events) : events_(events) {}
	mister::Error SelectFirst(std::uint8_t slave, std::uint8_t detection,
		std::uint64_t deadline, std::string* bus, std::uint8_t* value) override
	{
		assert(slave == 0x39 && detection == 0x41);
		deadlines.push_back(deadline);
		timing_seen_ = false;
		mode_seen_ = false;
		events_.push_back("video.adv.initialize");
		if (fail_event == "video.adv.initialize") {
			fail_event.clear();
			return {mister::ErrorCode::io_failed, "injected game video failure"};
		}
		if (!select_error.ok()) {
			const mister::Error error = select_error;
			select_error = {};
			return error;
		}
		*bus = "/dev/i2c-1";
		*value = 0x40;
		return {};
	}
	mister::Error ReadByte(std::uint8_t address, std::uint8_t* value,
		std::uint64_t deadline) override
	{
		deadlines.push_back(deadline);
		if (address == 0x41) {
			*value = 0x10;
			return {};
		}
		assert(address == 0x42);
		events_.push_back("video.link.ready");
		if (on_link_ready) on_link_ready();
		if (fail_event == "video.link.ready") {
			fail_event.clear();
			return {mister::ErrorCode::io_failed, "injected game video failure"};
		}
		*value = 0x60;
		return {};
	}
	mister::Error WriteByte(std::uint8_t, std::uint8_t,
		std::uint64_t deadline) override
	{
		deadlines.push_back(deadline);
		++write_calls;
		if (timing_seen_ && !mode_seen_) {
			events_.push_back("video.adv.mode");
			mode_seen_ = true;
			if (fail_event == "video.adv.mode") {
				fail_event.clear();
				return {mister::ErrorCode::io_failed,
					"injected game video failure"};
			}
		}
		if (write_calls == fail_write_call)
			return {mister::ErrorCode::io_failed, "injected game video failure"};
		return {};
	}
	void MarkTiming() { timing_seen_ = true; }
	void ResetCycle()
	{
		timing_seen_ = false;
		mode_seen_ = false;
	}
	std::vector<std::uint64_t> deadlines;
	mister::Error select_error;
	std::string fail_event;
	std::size_t write_calls = 0;
	std::size_t fail_write_call = 0;
	std::function<void()> on_link_ready;

private:
	std::vector<std::string>& events_;
	bool timing_seen_ = false;
	bool mode_seen_ = false;
};

class RecordingSpi final : public mister::native::Spi {
public:
	RecordingSpi(std::vector<std::string>& events, RecordingI2c& i2c)
		: events_(events), i2c_(i2c) {}
	mister::Error SynchronizeCore(std::uint64_t deadline) override
	{
		deadlines.push_back(deadline);
		events_.push_back("core.sync");
		return sync_error;
	}
	mister::Error Exchange(std::uint8_t,
		const std::vector<std::uint16_t>& request,
		std::vector<std::uint16_t>* response, std::uint64_t deadline) override
	{
		deadlines.push_back(deadline);
		if (request.empty()) return {mister::ErrorCode::io_failed, "empty"};
		std::string event;
		if (request[0] == 0x0014) {
			event = "core.probe:" + observed_core;
			events_.push_back(event);
			if (!probe_error.ok()) {
				const mister::Error error = probe_error;
				probe_error = {};
				return error;
			}
			response->assign(request.size(), 0);
			std::size_t index = 1;
			for (unsigned char byte : observed_core) (*response)[index++] = byte;
			(*response)[index] = ';';
		} else if (request[0] == 0x001e) {
			if (status_calls % 3 == 0) event = "core.reset.assert";
			else if (status_calls % 3 == 1) event = "core.status.initial";
			else event = "core.reset.release";
			events_.push_back(event);
			++status_calls;
		} else if (request[0] == 0x0055) {
			event = "core.media.select:" + std::to_string(request.at(1));
			events_.push_back(event);
		} else if (request[0] == 0x0056) {
			event = "core.media.extension:.bin";
			events_.push_back(event);
		} else if (request[0] == 0x0053 && request.at(1) == 0x00ff) {
			event = "core.media.enable";
			events_.push_back(event);
		} else if (request[0] == 0x0054) {
			event = "core.media.data:all bytes once";
			events_.push_back(event);
		} else if (request[0] == 0x0029) {
			event = "core.media.complete";
			events_.push_back(event);
		} else if (request[0] == 0x0020) {
			event = "video.timing:menu_720p60";
			events_.push_back(event);
			i2c_.MarkTiming();
		} else if (request ==
			std::vector<std::uint16_t>({0x0001, 0x0000})) {
			event = "core.buttons.neutral";
			events_.push_back(event);
		}
		if (!fail_event.empty() && event == fail_event) {
			fail_event.clear();
			return {mister::ErrorCode::io_failed, "injected core failure"};
		}
		return {};
	}
	std::vector<std::string>& events_;
	RecordingI2c& i2c_;
	std::string observed_core = "MegaDrive";
	mister::Error probe_error;
	std::vector<std::uint64_t> deadlines;
	mister::Error sync_error;
	std::string fail_event;
	std::size_t status_calls = 0;
};

class RecordingInput final : public mister::native::InputSession {
public:
	RecordingInput(std::vector<std::string>& events,
		const mister::native::Clock& clock) : events_(events), clock_(clock) {}
	mister::Error Open(const mister::native::InputDeviceIdentity& identity,
		const mister::InputRecipe& recipe, std::uint64_t deadline) override
	{
		events_.push_back("input.resolve:" + identity.name);
		identities.push_back(identity);
		recipes.push_back(recipe);
		open_deadlines.push_back(deadline);
		++open_calls;
		if (!open_error.ok()) return open_error;
		opened_ = true;
		descriptors.push_back(open_calls);
		return {};
	}
	mister::Error Start(std::uint64_t generation,
		std::function<void(std::uint64_t, mister::Error)> callback) override
	{
		events_.push_back("input.start:" + std::to_string(generation));
		++start_calls;
		generations.push_back(generation);
		if (!start_error.ok()) return start_error;
		callback_ = std::move(callback);
		workers.push_back(start_calls);
		return {};
	}
	mister::Error Neutralize(std::uint64_t deadline) override
	{
		events_.push_back("input.neutral");
		neutral_deadlines.push_back(deadline);
		if (reject_expired_deadline && clock_.NowMs() >= deadline)
			return {mister::ErrorCode::io_failed, "input deadline expired"};
		return neutral_error;
	}
	mister::Error Stop(std::uint64_t deadline) override
	{
		assert(opened_);
		events_.push_back("input.stop");
		events_.push_back("input.final-neutral");
		stop_deadlines.push_back(deadline);
		++stop_calls;
		opened_ = false;
		callback_ = {};
		return stop_error;
	}
	void Report(mister::Error error)
	{
		assert(static_cast<bool>(callback_));
		callback_(generations.back(), std::move(error));
	}
	std::vector<std::string>& events_;
	mister::Error open_error;
	mister::Error neutral_error;
	mister::Error start_error;
	mister::Error stop_error;
	int open_calls = 0;
	int start_calls = 0;
	int stop_calls = 0;
	std::vector<int> descriptors;
	std::vector<int> workers;
	std::vector<mister::native::InputDeviceIdentity> identities;
	std::vector<mister::InputRecipe> recipes;
	std::vector<std::uint64_t> open_deadlines;
	std::vector<std::uint64_t> neutral_deadlines;
	std::vector<std::uint64_t> stop_deadlines;
	std::vector<std::uint64_t> generations;
	bool reject_expired_deadline = false;

private:
	const mister::native::Clock& clock_;
	bool opened_ = false;
	std::function<void(std::uint64_t, mister::Error)> callback_;
};

class RecordingFaultSink final : public mister::HardwareFaultSink {
public:
	void ReportHardwareFault(mister::HardwareFault fault) override
	{
		faults.push_back(std::move(fault));
	}
	std::vector<mister::HardwareFault> faults;
};

struct Fixture {
	Fixture()
		: temporary(), events(), idle(temporary.File("idle.rbf", "idle")),
		  rbf(temporary.File("megadrive.rbf", "game")),
		  media_two(temporary.File("two.bin", "22")),
		  media_zero(temporary.File("zero.bin", "0")),
		  rom(temporary.File("sonic2.bin", "sonic")), opener(events), fpga(events),
		  i2c(events), spi(events, i2c), core(spi), idle_video(events), clock(100),
		  log(events), game_video(spi, i2c, clock, log,
			mister::native::Menu720p60Recipe()), input(events, clock),
		  input_identity({"FogCast Virtual Gamepad", 0x0006, 0x0000, 0x0001,
			0x0001}), sink(),
		  hardware(opener, fpga, core, idle_video, game_video, input,
			input_identity, clock, log, idle, {30000, 10000, 10000})
	{
		hardware.SetFaultSink(&sink);
	}
	mister::PreparedLaunch Launch() const
	{
		mister::PreparedLaunch launch;
		launch.system = "megadrive";
		launch.expected_core = "MegaDrive";
		launch.rbf = rbf;
		launch.media.push_back({2, media_two});
		launch.media.push_back({0, media_zero});
		launch.core = {0x1111, 0x2222, 0x3333,
			mister::FileWireFormat::little_endian_byte_pairs};
		launch.input = {1, 0x02, 0x0008, 0x0004, 0x0002, 0x0001,
			0x0010, 0x0020, 0x0040, 0x0080};
		return launch;
	}
	mister::PreparedLaunch MegaDriveLaunch() const
	{
		mister::PreparedLaunch launch = Launch();
		launch.media.clear();
		launch.media.push_back({1, rom, 32u * 1024u * 1024u});
		return launch;
	}
	TempDirectory temporary;
	std::vector<std::string> events;
	std::string idle;
	std::string rbf;
	std::string media_two;
	std::string media_zero;
	std::string rom;
	RecordingOpener opener;
	RecordingFpga fpga;
	RecordingI2c i2c;
	RecordingSpi spi;
	mister::native::CoreLoader core;
	RecordingVideo idle_video;
	FixedClock clock;
	LedgerLog log;
	mister::native::FixedVideoBringup game_video;
	RecordingInput input;
	mister::native::InputDeviceIdentity input_identity;
	RecordingFaultSink sink;
	mister::native::NativeHardware hardware;
};

std::size_t Find(const std::vector<std::string>& events, const std::string& value)
{
	for (std::size_t index = 0; index < events.size(); ++index) {
		if (events[index] == value) return index;
	}
	return events.size();
}

std::size_t Count(const std::vector<std::string>& values,
	const std::string& wanted)
{
	std::size_t count = 0;
	for (const std::string& value : values) {
		if (value == wanted) ++count;
	}
	return count;
}

struct IntegratedFixture {
	IntegratedFixture()
		: native(), profiles(BuildProfiles(native)),
		  runtime(native.hardware, profiles, native.log) {}
	static mister::Profiles BuildProfiles(const Fixture& native)
	{
		mister::Profile profile;
		profile.system = "megadrive";
		profile.expected_core = "MegaDrive";
		profile.rbf = native.rbf;
		profile.media.push_back({"cartridge", 1, true, {".bin"},
			32u * 1024u * 1024u});
		profile.core = {0x1111, 0x2222, 0x3333,
			mister::FileWireFormat::little_endian_byte_pairs};
		profile.input = {1, 0x02, 0x0008, 0x0004, 0x0002, 0x0001,
			0x0010, 0x0020, 0x0040, 0x0080};
		mister::Profiles profiles;
		assert(profiles.Add(std::move(profile)).ok());
		return profiles;
	}
	mister::Launch Request() const
	{
		mister::Launch launch;
		launch.system = "megadrive";
		launch.rbf = native.rbf;
		launch.media.push_back({"cartridge", native.rom});
		return launch;
	}
	void Start()
	{
		assert(runtime.Start().ok());
		native.events.clear();
		native.log.Clear();
	}
	Fixture native;
	mister::Profiles profiles;
	mister::Runtime runtime;
};

const std::vector<std::string> kSuccessfulLaunch = {
	"profile.validate:megadrive",
	"input.resolve:FogCast Virtual Gamepad",
	"artifact.open:megadrive.rbf",
	"artifact.open:sonic2.bin",
	"media.sort:1",
	"fpga.program",
	"core.sync",
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
	"core.buttons.neutral",
	"video.link.ready",
	"input.neutral",
	"core.reset.release",
	"input.start:1",
	"runtime.running_game",
};

void TestLaunchUsesExactCoreRecipeAndExplicitMediaFormatInOrder()
{
	Fixture fixture;
	const mister::HardwareResult result = fixture.hardware.Launch(fixture.Launch(), 1);
	assert(result.error.ok() && result.mutation_attempted);
	assert(result.observed_core == "MegaDrive");
	assert(fixture.events[0] == "input.resolve:FogCast Virtual Gamepad");
	assert(fixture.events[1] == "artifact.open:megadrive.rbf");
	assert(fixture.events[2] == "artifact.open:two.bin");
	assert(fixture.events[3] == "artifact.open:zero.bin");
	assert(Find(fixture.events, "fpga.program") == 5);
	assert(Find(fixture.events, "core.sync") == 6);
	assert(Find(fixture.events, "core.reset.assert") == 7);
	assert(Find(fixture.events, "core.probe:MegaDrive") == 8);
	assert(Find(fixture.events, "core.status.initial") == 9);
	assert(Find(fixture.events, "core.media.select:0") <
		Find(fixture.events, "core.media.select:2"));
	assert(Find(fixture.events, "input.neutral") <
		Find(fixture.events, "core.reset.release"));
	assert(Find(fixture.events, "core.reset.release") <
		Find(fixture.events, "input.start:1"));
}

void TestLaunchSynchronizesTheProgrammedCoreBeforeAnyCoreIo()
{
	Fixture success;
	const mister::HardwareResult result = success.hardware.Launch(
		success.MegaDriveLaunch(), 1);
	assert(result.error.ok());
	assert(Count(success.events, "core.sync") == 1);
	assert(Find(success.events, "fpga.program") <
		Find(success.events, "core.sync"));
	assert(Find(success.events, "core.sync") <
		Find(success.events, "core.reset.assert"));

	Fixture failure;
	failure.spi.sync_error = {
		mister::ErrorCode::io_failed, "injected core synchronization failure"};
	const mister::HardwareResult failed = failure.hardware.Launch(
		failure.MegaDriveLaunch(), 1);
	assert(failed.error.code == mister::ErrorCode::io_failed);
	assert(failed.error.message == "injected core synchronization failure");
	assert(failed.mutation_attempted);
	assert(Count(failure.events, "core.sync") == 1);
	assert(Find(failure.events, "core.reset.assert") == failure.events.size());
	assert(Find(failure.events, "core.probe:MegaDrive") == failure.events.size());
	assert(Find(failure.events, "core.media.select:1") == failure.events.size());
	assert(Find(failure.events, "video.adv.initialize") == failure.events.size());
	assert(Find(failure.events, "core.reset.release") == failure.events.size());
	assert(Find(failure.events, "input.start:1") == failure.events.size());
}

void TestRuntimeLaunchUsesTheCompleteProductionOrderBeforePublishingRunning()
{
	IntegratedFixture fixture;
	fixture.Start();
	assert(fixture.runtime.LaunchGame(fixture.Request()).ok());
	assert(fixture.native.events == kSuccessfulLaunch);
	const mister::Status status = fixture.runtime.status();
	assert(status.state == mister::State::running_game);
	assert(status.execution == mister::Execution::game);
	assert(status.system == "megadrive");
	assert(status.core == "MegaDrive");
	assert(fixture.native.input.identities.size() == 1);
	const mister::native::InputDeviceIdentity& identity =
		fixture.native.input.identities[0];
	assert(identity.name == "FogCast Virtual Gamepad");
	assert(identity.bus == 0x0006 && identity.vendor == 0x0000);
	assert(identity.product == 0x0001 && identity.version == 0x0001);
	assert(fixture.native.input.open_deadlines ==
		std::vector<std::uint64_t>({10100}));
	assert(fixture.native.input.neutral_deadlines ==
		std::vector<std::uint64_t>({10100}));
}

void TestAllInputAndArtifactPreflightCompletesBeforeProgramming()
{
	IntegratedFixture missing_input;
	missing_input.Start();
	missing_input.native.input.open_error = {
		mister::ErrorCode::io_failed, "gamepad missing"};
	assert(missing_input.runtime.LaunchGame(missing_input.Request()).code ==
		mister::ErrorCode::io_failed);
	assert(missing_input.native.events == std::vector<std::string>({
		"profile.validate:megadrive",
		"input.resolve:FogCast Virtual Gamepad",
	}));
	assert(missing_input.native.fpga.calls == 1);
	assert(missing_input.native.input.stop_calls == 0);

	IntegratedFixture missing_rbf;
	missing_rbf.Start();
	missing_rbf.native.opener.fail_call = 2;
	assert(missing_rbf.runtime.LaunchGame(missing_rbf.Request()).code ==
		mister::ErrorCode::io_failed);
	assert(Find(missing_rbf.native.events, "fpga.program") ==
		missing_rbf.native.events.size());
	assert(missing_rbf.native.input.stop_calls == 1);

	IntegratedFixture missing_rom;
	missing_rom.Start();
	missing_rom.native.opener.fail_call = 3;
	assert(missing_rom.runtime.LaunchGame(missing_rom.Request()).code ==
		mister::ErrorCode::io_failed);
	assert(Find(missing_rom.native.events, "artifact.open:megadrive.rbf") <
		Find(missing_rom.native.events, "artifact.open:sonic2.bin"));
	assert(Find(missing_rom.native.events, "fpga.program") ==
		missing_rom.native.events.size());
	assert(missing_rom.native.input.stop_calls == 1);
}

void TestOversizedRomIsRejectedBeforeFpgaAndClosesPreflightInput()
{
	Fixture fixture;
	mister::PreparedLaunch launch = fixture.MegaDriveLaunch();
	launch.media[0].maximum_size = 4;
	const mister::HardwareResult result = fixture.hardware.Launch(launch, 1);
	assert(result.error.code == mister::ErrorCode::io_failed);
	assert(result.error.message.find("artifact exceeds size limit") !=
		std::string::npos);
	assert(!result.mutation_attempted);
	assert(fixture.fpga.calls == 0);
	assert(Find(fixture.events, "fpga.program") == fixture.events.size());
	assert(fixture.input.stop_calls == 1);
}

void TestEveryPostProgramPhaseFailureGetsOneIdleCleanup()
{
	const std::vector<std::string> phases = {
		"program", "core.sync", "core.reset.assert", "core.probe:MegaDrive",
		"core.status.initial", "core.media.select:1",
		"core.media.extension:.bin", "core.media.enable",
		"core.media.data:all bytes once", "core.media.complete",
		"video.adv.initialize", "video.timing:menu_720p60",
		"video.adv.mode", "core.buttons.neutral", "video.link.ready",
		"input.neutral",
		"core.reset.release", "input.start",
	};
	for (const std::string& phase : phases) {
		IntegratedFixture fixture;
		fixture.Start();
		if (phase == "program") {
			fixture.native.fpga.fail_call = fixture.native.fpga.calls + 1;
		} else if (phase == "core.sync") {
			fixture.native.spi.sync_error = {
				mister::ErrorCode::io_failed, "injected core failure"};
		} else if (phase == "core.probe:MegaDrive") {
			fixture.native.spi.probe_error = {
				mister::ErrorCode::io_failed, "injected core failure"};
		} else if (phase.find("video.") == 0 &&
			phase != "video.timing:menu_720p60") {
			fixture.native.i2c.fail_event = phase;
		} else if (phase == "input.neutral") {
			fixture.native.input.neutral_error = {
				mister::ErrorCode::io_failed, "injected input neutral failure"};
		} else if (phase == "input.start") {
			fixture.native.input.start_error = {
				mister::ErrorCode::io_failed, "injected input start failure"};
		} else {
			fixture.native.spi.fail_event = phase;
		}
		const mister::Error result = fixture.runtime.LaunchGame(fixture.Request());
		assert(!result.ok());
		assert(fixture.runtime.status().state == mister::State::idle);
		assert(Count(fixture.native.fpga.programmed, "idle.rbf") == 2);
		assert(Count(fixture.native.fpga.programmed, "megadrive.rbf") == 1);
		assert(fixture.native.input.stop_calls == 1);
		assert(Find(fixture.native.events, "runtime.running_game") ==
			fixture.native.events.size());
	}
}

void TestCleanupFailureAndDirectInputStopErrorRequireReboot()
{
	IntegratedFixture idle_failure;
	idle_failure.Start();
	idle_failure.native.spi.fail_event = "core.reset.assert";
	idle_failure.native.idle_video.result.error = {
		mister::ErrorCode::io_failed, "idle video cleanup failed"};
	assert(idle_failure.runtime.LaunchGame(idle_failure.Request()).code ==
		mister::ErrorCode::idle_failed);
	assert(idle_failure.runtime.status().state == mister::State::reboot_required);
	assert(Count(idle_failure.native.fpga.programmed, "idle.rbf") == 2);
	assert(idle_failure.native.input.stop_calls == 1);

	IntegratedFixture stop_failure;
	stop_failure.Start();
	stop_failure.native.spi.fail_event = "core.reset.assert";
	stop_failure.native.input.stop_error = {
		mister::ErrorCode::io_failed, "input cancellation failed"};
	assert(stop_failure.runtime.LaunchGame(stop_failure.Request()).code ==
		mister::ErrorCode::idle_failed);
	const mister::Status status = stop_failure.runtime.status();
	assert(status.state == mister::State::reboot_required);
	assert(status.error.message == "input cancellation failed");
	assert(Count(stop_failure.native.fpga.programmed, "idle.rbf") == 2);
}

void TestStopOrdersInputBeforeIdleAndImmediateRelaunchIsFresh()
{
	IntegratedFixture fixture;
	fixture.Start();
	assert(fixture.runtime.LaunchGame(fixture.Request()).ok());
	fixture.native.events.clear();
	assert(fixture.runtime.Stop().ok());
	assert(fixture.native.events == std::vector<std::string>({
		"input.stop",
		"input.final-neutral",
		"artifact.open:idle.rbf",
		"fpga.program",
		"idle.video:MENU",
		"runtime.idle",
	}));
	fixture.native.events.clear();
	std::vector<std::string> second = kSuccessfulLaunch;
	second[22] = "input.start:2";
	assert(fixture.runtime.LaunchGame(fixture.Request()).ok());
	assert(fixture.native.events == second);
	assert(fixture.native.input.descriptors == std::vector<int>({1, 2}));
	assert(fixture.native.input.workers == std::vector<int>({1, 2}));
	assert(fixture.native.input.generations ==
		std::vector<std::uint64_t>({1, 2}));
	assert(fixture.native.input.stop_calls == 1);
}

void TestManualStopPreservesInputCancellationErrorAfterAttemptingIdle()
{
	IntegratedFixture fixture;
	fixture.Start();
	assert(fixture.runtime.LaunchGame(fixture.Request()).ok());
	fixture.native.input.stop_error = {
		mister::ErrorCode::io_failed, "direct cancellation failure"};
	assert(fixture.runtime.Stop().code == mister::ErrorCode::idle_failed);
	const mister::Status status = fixture.runtime.status();
	assert(status.state == mister::State::reboot_required);
	assert(status.error.message == "direct cancellation failure");
	assert(Count(fixture.native.fpga.programmed, "idle.rbf") == 2);
}

void TestInputCallbackReportsTheGenerationThroughTheHardwareBoundary()
{
	Fixture fixture;
	assert(fixture.hardware.Launch(fixture.MegaDriveLaunch(), 41).error.ok());
	fixture.input.Report({mister::ErrorCode::io_failed, "input read failed"});
	assert(fixture.sink.faults.size() == 1);
	assert(fixture.sink.faults[0].generation == 41);
	assert(fixture.sink.faults[0].error.code == mister::ErrorCode::io_failed);
	assert(fixture.sink.faults[0].error.message == "input read failed");
}

void TestLaunchRejectsUnsupportedPreparedMediaFormatBeforeFileSelection()
{
	Fixture fixture;
	mister::PreparedLaunch launch = fixture.Launch();
	launch.core.file_wire = static_cast<mister::FileWireFormat>(99);
	const mister::HardwareResult result = fixture.hardware.Launch(launch, 1);
	assert(result.error.code == mister::ErrorCode::io_failed);
	assert(result.error.message == "unsupported file wire format");
	assert(result.mutation_attempted);
	assert(Find(fixture.events, "core.reset.assert") < fixture.events.size());
	assert(Find(fixture.events, "core.status.initial") < fixture.events.size());
	assert(Find(fixture.events, "core.media.select:0") == fixture.events.size());
	assert(Find(fixture.events, "core.media.select:2") == fixture.events.size());
}

void TestIdleRequiresVideoAndPreservesDevelopmentBehavior()
{
	Fixture fixture;
	assert(fixture.hardware.LoadDevelopmentRBF(fixture.rbf).error.ok());
	assert(fixture.events.size() == 2);
	assert(fixture.events[0] == "artifact.open:megadrive.rbf");
	assert(fixture.events[1] == "fpga.program");
	assert(fixture.idle_video.calls == 0);
	fixture.events.clear();
	const mister::HardwareResult idle = fixture.hardware.LoadIdle();
	assert(idle.error.ok());
	assert(idle.mutation_attempted);
	assert(idle.observed_core == "MENU");
	assert(fixture.events == std::vector<std::string>({
		"artifact.open:idle.rbf",
		"fpga.program",
		"idle.video:MENU",
	}));
	assert(fixture.idle_video.deadlines == std::vector<std::uint64_t>({10100}));
}

void TestIdlePreflightFailureCallsNeitherFpgaNorVideo()
{
	Fixture fixture;
	fixture.opener.fail_call = 1;
	const mister::HardwareResult result = fixture.hardware.LoadIdle();
	assert(result.error.code == mister::ErrorCode::io_failed);
	assert(!result.mutation_attempted);
	assert(fixture.fpga.calls == 0);
	assert(fixture.idle_video.calls == 0);
}

void TestIdleFpgaFailureNeverCallsVideoAndPreservesMutationFlag()
{
	Fixture before;
	before.fpga.result = {{mister::ErrorCode::program_failed, "before"}, false};
	const mister::HardwareResult before_result = before.hardware.LoadIdle();
	assert(before_result.error.code == mister::ErrorCode::program_failed);
	assert(!before_result.mutation_attempted);
	assert(before.idle_video.calls == 0);

	Fixture after;
	after.fpga.result = {{mister::ErrorCode::program_failed, "after"}, true};
	const mister::HardwareResult after_result = after.hardware.LoadIdle();
	assert(after_result.error.code == mister::ErrorCode::program_failed);
	assert(after_result.mutation_attempted);
	assert(after.idle_video.calls == 0);
}

void TestIdleVideoFailureIsAttemptedIoFailureWithoutCleanup()
{
	Fixture fixture;
	fixture.idle_video.result.error = {
		mister::ErrorCode::program_failed, "injected video failure"};
	fixture.idle_video.result.observed_core = "MENU";
	const mister::HardwareResult result = fixture.hardware.LoadIdle();
	assert(result.error.code == mister::ErrorCode::io_failed);
	assert(result.error.message == "injected video failure");
	assert(result.mutation_attempted);
	assert(result.observed_core == "MENU");
	assert(fixture.fpga.calls == 1);
	assert(fixture.idle_video.calls == 1);
	assert(fixture.events == std::vector<std::string>({
		"artifact.open:idle.rbf",
		"fpga.program",
		"idle.video:MENU",
	}));
}

void TestLaunchNeverCallsIdleVideo()
{
	Fixture fixture;
	assert(fixture.hardware.Launch(fixture.Launch(), 1).error.ok());
	assert(fixture.idle_video.calls == 0);
}

void TestOneAbsoluteDeadlinePerNativeStage()
{
	Fixture fixture;
	assert(fixture.hardware.Launch(fixture.Launch(), 1).error.ok());
	assert(fixture.fpga.deadlines.size() == 1);
	assert(fixture.fpga.deadlines[0] == 30100);
	assert(!fixture.spi.deadlines.empty());
	for (std::uint64_t deadline : fixture.spi.deadlines) assert(deadline == 10100);
	assert(fixture.idle_video.calls == 0);
}

void TestPostVideoCoreStageGetsFreshDeadline()
{
	Fixture fixture;
	fixture.input.reject_expired_deadline = true;
	fixture.i2c.on_link_ready = [&fixture] { fixture.clock.now_ = 10101; };
	const mister::HardwareResult result = fixture.hardware.Launch(
		fixture.Launch(), 1);
	assert(result.error.ok());
	assert(fixture.input.neutral_deadlines ==
		std::vector<std::uint64_t>({20101}));
	assert(!fixture.spi.deadlines.empty());
	assert(fixture.spi.deadlines.back() == 20101);
}

void TestPreflightAndProgramFailuresUseExactMutationMapping()
{
	Fixture preflight;
	preflight.opener.fail_call = 2;
	const auto open_result = preflight.hardware.Launch(preflight.Launch(), 1);
	assert(open_result.error.code == mister::ErrorCode::io_failed);
	assert(!open_result.mutation_attempted);
	assert(preflight.fpga.calls == 0 && preflight.spi.deadlines.empty());
	Fixture before;
	before.fpga.result = {{mister::ErrorCode::program_failed, "before"}, false};
	const auto before_result = before.hardware.Launch(before.Launch(), 1);
	assert(before_result.error.code == mister::ErrorCode::program_failed);
	assert(!before_result.mutation_attempted);
	Fixture after;
	after.fpga.result = {{mister::ErrorCode::program_failed, "after"}, true};
	const auto after_result = after.hardware.Launch(after.Launch(), 1);
	assert(after_result.error.code == mister::ErrorCode::program_failed);
	assert(after_result.mutation_attempted);
}

void TestEveryConcretePreflightRejectionPerformsZeroHardwareWork()
{
	Fixture missing;
	mister::PreparedLaunch missing_launch = missing.Launch();
	missing_launch.rbf = missing.temporary.path + "/missing.rbf";
	assert(missing.hardware.Launch(missing_launch, 1).error.code ==
		mister::ErrorCode::io_failed);
	assert(missing.fpga.calls == 0 && missing.spi.deadlines.empty());

	Fixture directory;
	mister::PreparedLaunch directory_launch = directory.Launch();
	directory_launch.rbf = directory.temporary.path;
	assert(directory.hardware.Launch(directory_launch, 1).error.code ==
		mister::ErrorCode::io_failed);
	assert(directory.fpga.calls == 0 && directory.spi.deadlines.empty());

	Fixture zero;
	mister::PreparedLaunch zero_launch = zero.Launch();
	zero_launch.rbf = zero.temporary.File("empty.rbf", "");
	assert(zero.hardware.Launch(zero_launch, 1).error.code ==
		mister::ErrorCode::io_failed);
	assert(zero.fpga.calls == 0 && zero.spi.deadlines.empty());

	Fixture oversize;
	mister::PreparedLaunch oversize_launch = oversize.Launch();
	oversize_launch.rbf = oversize.temporary.SparseFile("large.rbf",
		32u * 1024u * 1024u + 1u);
	assert(oversize.hardware.Launch(oversize_launch, 1).error.code ==
		mister::ErrorCode::io_failed);
	assert(oversize.fpga.calls == 0 && oversize.spi.deadlines.empty());
}

void TestProbeMismatchAndIoRetainObservedCoreAndMutation()
{
	Fixture mismatch;
	mismatch.spi.observed_core = "OTHER";
	const auto mismatch_result = mismatch.hardware.Launch(mismatch.Launch(), 1);
	assert(mismatch_result.error.code == mister::ErrorCode::core_mismatch);
	assert(mismatch_result.mutation_attempted);
	assert(mismatch_result.observed_core == "OTHER");
	Fixture io;
	io.spi.probe_error = {mister::ErrorCode::io_failed, "probe"};
	const auto io_result = io.hardware.Launch(io.Launch(), 1);
	assert(io_result.error.code == mister::ErrorCode::io_failed);
	assert(io_result.mutation_attempted);
	assert(io_result.observed_core.empty());
}

void TestNativeLoggingNamesPhasesAndConfirmedCore()
{
	Fixture fixture;
	assert(fixture.hardware.Launch(fixture.Launch(), 1).error.ok());
	const auto records = fixture.log.records();
	bool preflight = false;
	bool program = false;
	bool sync = false;
	bool probe = false;
	bool configure = false;
	bool media = false;
	for (const auto& record : records) {
		preflight = preflight || record.phase == "preflight";
		program = program || record.phase == "program";
		sync = sync || (record.phase == "sync" && record.core == "MegaDrive");
		probe = probe || (record.phase == "probe" && record.core == "MegaDrive");
		configure = configure || record.phase == "configure";
		media = media || record.phase == "media";
	}
	assert(preflight && program && sync && probe && configure && media);
}

void TestProductionConstructionOwnsRealIdleHardware()
{
	const mister::Profiles& profiles = mister::ProductionProfiles();
	mister::Launch launch;
	launch.system = "megadrive";
	launch.rbf = "/usr/share/mister-runtime/cores/megadrive.rbf";
	launch.media.push_back({"cartridge", "/media/fat/fogcast/cache/sonic2.bin"});
	mister::PreparedLaunch prepared;
	assert(profiles.Prepare(launch, &prepared).ok());
	assert(prepared.system == "megadrive");
	assert(prepared.expected_core == "MegaDrive");
	assert(prepared.media.size() == 1 && prepared.media[0].index == 1);
	assert(prepared.core.reset_assert_word == 0x0001);
	assert(prepared.core.initial_status_word == 0x0001);
	assert(prepared.core.reset_release_word == 0x0000);
	assert(prepared.core.file_wire ==
		mister::FileWireFormat::little_endian_byte_pairs);
	assert(prepared.input.player_count == 1);
	assert(prepared.input.player_command == 0x02);
	assert(prepared.input.up == 0x0008);
	assert(prepared.input.down == 0x0004);
	assert(prepared.input.left == 0x0002);
	assert(prepared.input.right == 0x0001);
	assert(prepared.input.a == 0x0010);
	assert(prepared.input.b == 0x0020);
	assert(prepared.input.c == 0x0040);
	assert(prepared.input.start == 0x0080);

	mister::PreparedLaunch unchanged;
	unchanged.system = "sentinel";
	launch.system = "snes";
	assert(profiles.Prepare(launch, &unchanged).code ==
		mister::ErrorCode::unknown_system);
	assert(unchanged.system == "sentinel");
	launch.system = "megadrive";
	launch.settings.push_back({"region", "pal"});
	assert(profiles.Prepare(launch, &unchanged).code ==
		mister::ErrorCode::invalid_request);
	assert(unchanged.system == "sentinel");
	launch.settings.clear();
	launch.media[0].path = "/media/fat/fogcast/cache/sonic2.zip";
	assert(profiles.Prepare(launch, &unchanged).code ==
		mister::ErrorCode::invalid_request);
	assert(unchanged.system == "sentinel");
	launch.media[0].path = "/media/fat/fogcast/cache/sonic2.bin";
	launch.media.push_back({"cartridge", "/media/fat/fogcast/cache/sonic2.md"});
	assert(profiles.Prepare(launch, &unchanged).code ==
		mister::ErrorCode::invalid_request);
	assert(unchanged.system == "sentinel");
	launch.media.pop_back();
	launch.media.push_back({"bios", "/media/fat/fogcast/cache/bios.bin"});
	assert(profiles.Prepare(launch, &unchanged).code ==
		mister::ErrorCode::invalid_request);
	assert(unchanged.system == "sentinel");
	launch.media.pop_back();
	launch.rbf = "/usr/share/mister-runtime/cores/other.rbf";
	assert(profiles.Prepare(launch, &unchanged).code ==
		mister::ErrorCode::invalid_request);
	assert(unchanged.system == "sentinel");
	mister_test::CaptureLog log;
	std::unique_ptr<mister::Hardware> hardware;
	const mister::Error created = mister::CreateProductionHardware(log, &hardware);
	assert(created.ok());
	assert(hardware);
	const mister::HardwareResult idle = hardware->LoadIdle();
	assert(idle.error.code == mister::ErrorCode::io_failed);
	assert(idle.error.message.find(
		"/definitely-missing/libmister-runtime/idle.rbf") != std::string::npos);
	assert(!idle.mutation_attempted);
}

void TestUnavailableHardwareRemainsFailureOnly()
{
	const mister::Error reason = {
		mister::ErrorCode::io_failed, "injected construction failure"};
	std::unique_ptr<mister::Hardware> hardware =
		mister::CreateUnavailableHardware(reason);
	assert(hardware);
	assert(hardware->LoadIdle().error.code == mister::ErrorCode::io_failed);
	assert(hardware->Launch({}, 1).error.code == mister::ErrorCode::io_failed);
	assert(hardware->LoadDevelopmentRBF("/x").error.code ==
		mister::ErrorCode::io_failed);
}

} // namespace

int main()
{
	TestLaunchUsesExactCoreRecipeAndExplicitMediaFormatInOrder();
	TestLaunchSynchronizesTheProgrammedCoreBeforeAnyCoreIo();
	TestRuntimeLaunchUsesTheCompleteProductionOrderBeforePublishingRunning();
	TestAllInputAndArtifactPreflightCompletesBeforeProgramming();
	TestOversizedRomIsRejectedBeforeFpgaAndClosesPreflightInput();
	TestEveryPostProgramPhaseFailureGetsOneIdleCleanup();
	TestCleanupFailureAndDirectInputStopErrorRequireReboot();
	TestStopOrdersInputBeforeIdleAndImmediateRelaunchIsFresh();
	TestManualStopPreservesInputCancellationErrorAfterAttemptingIdle();
	TestInputCallbackReportsTheGenerationThroughTheHardwareBoundary();
	TestLaunchRejectsUnsupportedPreparedMediaFormatBeforeFileSelection();
	TestIdleRequiresVideoAndPreservesDevelopmentBehavior();
	TestIdlePreflightFailureCallsNeitherFpgaNorVideo();
	TestIdleFpgaFailureNeverCallsVideoAndPreservesMutationFlag();
	TestIdleVideoFailureIsAttemptedIoFailureWithoutCleanup();
	TestLaunchNeverCallsIdleVideo();
	TestOneAbsoluteDeadlinePerNativeStage();
	TestPostVideoCoreStageGetsFreshDeadline();
	TestPreflightAndProgramFailuresUseExactMutationMapping();
	TestEveryConcretePreflightRejectionPerformsZeroHardwareWork();
	TestProbeMismatchAndIoRetainObservedCoreAndMutation();
	TestNativeLoggingNamesPhasesAndConfirmedCore();
	TestProductionConstructionOwnsRealIdleHardware();
	TestUnavailableHardwareRemainsFailureOnly();
	puts("native_hardware_test: 24 passed");
	return 0;
}
