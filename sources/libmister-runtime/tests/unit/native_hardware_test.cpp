// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "capture_log.hpp"
#include "fake_input.hpp"
#include "fake_mmio.hpp"
#include "linux/production_hardware.hpp"
#include "native/artifacts.hpp"
#include "native/core_package.hpp"
#include "native/sha256.hpp"
#include "native/fes_gp.hpp"
#include "native/generated/fes_gp.hpp"
#include "native/generated/de10_nano.hpp"
#include "native/generated/fes_application.hpp"
#include "native/generated/fes_simple_computer.hpp"
#include "native/hardware.hpp"
#include "native/core_data.hpp"
#include "native/input.hpp"
#include "native/linux/i2c.hpp"
#include "native/linux/input.hpp"
#include "native/video.hpp"
#include "native/video_recipe.hpp"

#include <assert.h>
#include <algorithm>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <sys/stat.h>

#include <cstdint>
#include <chrono>
#include <functional>
#include <fstream>
#include <limits>
#include <memory>
#include <mutex>
#include <string>
#include <thread>
#include <utility>
#include <vector>

namespace {

struct TempDirectory {
	explicit TempDirectory(const std::string& parent = "/tmp")
	{
		const std::string name = parent + "/libmister-native-hardware.XXXXXX";
		std::vector<char> pattern(name.begin(), name.end());
		pattern.push_back(0);
		char* created = mkdtemp(pattern.data());
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


std::string ReadText(const std::string& path)
{
	std::ifstream input(path, std::ios::binary);
	assert(input.good());
	return std::string(std::istreambuf_iterator<char>(input),
		std::istreambuf_iterator<char>());
}

void PopulateFesGpPackage(TempDirectory* package)
{
	package->File("manifest.toml", ReadText(
		"tests/fixtures/core-bundle-v2/manifests/valid-basic.toml"));
	package->File("core.rbf", ReadText(
		"tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
}

void ReplaceAll(std::string* text, const std::string& from,
	const std::string& to)
{
	std::size_t position = 0;
	while ((position = text->find(from, position)) != std::string::npos) {
		text->replace(position, from.size(), to);
		position += to.size();
	}
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
		mister::native::ProgrammingProfile profile,
		std::uint64_t deadline) override
	{
		events_.push_back("fpga.program");
		profiles.push_back(profile);
		programmed.push_back(BaseName(artifact.path()));
		char first_byte = 0;
		assert(pread(artifact.fd(), &first_byte, 1, 0) == 1);
		programmed_first_bytes.push_back(first_byte);
		deadlines.push_back(deadline);
		++calls;
		if (on_program) on_program();
		if (calls == fail_call) return failure;
		return result;
	}
	std::vector<std::string>& events_;
	mister::native::NativeResult result;
	mister::native::NativeResult failure = {
		{mister::ErrorCode::program_failed, "injected program failure"}, true};
	std::vector<std::string> programmed;
	std::vector<char> programmed_first_bytes;
	std::vector<std::uint64_t> deadlines;
	std::vector<mister::native::ProgrammingProfile> profiles;
	std::function<void()> on_program;
	int calls = 0;
	int fail_call = 0;
};

class RecordingVideo final : public mister::native::VideoBringup {
public:
	explicit RecordingVideo(std::vector<std::string>& events) : events_(events)
	{
		result.phase = "hdmi_verify";
		result.observed_core = "MENU";
		quiesce_result.mutation_attempted = true;
	}
	mister::native::VideoQuiesceResult Quiesce(
		std::uint64_t deadline) override
	{
		events_.push_back("idle.video.quiesce");
		quiesce_deadlines.push_back(deadline);
		++quiesce_calls;
		return quiesce_result;
	}
	mister::native::VideoResult BringUp(const mister::native::IdleRecipe& idle,
		std::uint64_t deadline) override
	{
		last_idle = idle;
		events_.push_back("idle.video:" + idle.expected_core);
		deadlines.push_back(deadline);
		++calls;
		return result;
	}
	std::vector<std::string>& events_;
	mister::native::VideoResult result;
	mister::native::VideoQuiesceResult quiesce_result;
	mister::native::IdleRecipe last_idle;
	std::vector<std::uint64_t> quiesce_deadlines;
	std::vector<std::uint64_t> deadlines;
	int quiesce_calls = 0;
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
		selection_pending_ = true;
		if (!select_error.ok()) {
			const mister::Error error = select_error;
			select_error = {};
			return error;
		}
		*bus = "/dev/i2c-1";
		*value = 0x10;
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
	mister::Error WriteByte(std::uint8_t address, std::uint8_t value,
		std::uint64_t deadline) override
	{
		deadlines.push_back(deadline);
		++write_calls;
		if (selection_pending_) {
			const std::string selection_event =
				address == 0x41 && value == 0x50 ?
				"video.quiesce" : "video.adv.initialize";
			events_.push_back(selection_event);
			selection_pending_ = false;
			if (fail_event == selection_event) {
				fail_event.clear();
				return {mister::ErrorCode::io_failed,
					"injected game video failure"};
			}
			if (selection_event == "video.quiesce" && on_quiesce)
				on_quiesce();
		}
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
	std::function<void()> on_quiesce;
	std::function<void()> on_link_ready;

private:
	std::vector<std::string>& events_;
	bool timing_seen_ = false;
	bool mode_seen_ = false;
	bool selection_pending_ = false;
};


class RecordingInput final : public mister::native::InputSession {
public:
	RecordingInput(std::vector<std::string>& events,
		const mister::native::Clock& clock) : events_(events), clock_(clock) {}
	mister::Error Open(const mister::native::InputDeviceIdentity& identity,
		const mister::InputRecipe& recipe, std::uint64_t deadline,
		mister::native::ButtonWriter writer = {}) override
	{
		events_.push_back("input.resolve:" + identity.name);
		identities.push_back(identity);
		recipes.push_back(recipe);
		writer_ = std::move(writer);
		writers.push_back(writer_);
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
		if (!neutral_error.ok()) return neutral_error;
		if (writer_ && recipes.back().player_command ==
			mister::native::generated::FesGpOpcodeButtons)
			return writer_(0, deadline);
		return {};
	}
	mister::Error Stop(std::uint64_t deadline) override
	{
		assert(opened_);
		events_.push_back("input.stop");
		events_.push_back("input.final-neutral");
		stop_deadlines.push_back(deadline);
		++stop_calls;
		mister::Error result = stop_error;
		if (result.ok() && writer_ && recipes.back().player_command ==
			mister::native::generated::FesGpOpcodeButtons)
			result = writer_(0, deadline);
		opened_ = false;
		callback_ = {};
		writer_ = {};
		return result;
	}
	void Report(mister::Error error)
	{
		assert(static_cast<bool>(callback_));
		callback_(generations.back(), std::move(error));
	}
	mister::Error Deliver(std::uint16_t map, std::uint64_t deadline)
	{
		assert(static_cast<bool>(writer_));
		return writer_(map, deadline);
	}
	bool HasActiveCallback() const { return static_cast<bool>(callback_); }
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
	std::vector<mister::native::ButtonWriter> writers;
	bool reject_expired_deadline = false;

private:
	const mister::native::Clock& clock_;
	bool opened_ = false;
	std::function<void(std::uint64_t, mister::Error)> callback_;
	mister::native::ButtonWriter writer_;
};

class RetainingNativeInput final : public mister::native::InputSession {
public:
	RetainingNativeInput(mister::native::InputDevice& device,
		mister::native::Clock& clock)
		: native_(device, clock, 25) {}
	mister::Error Open(const mister::native::InputDeviceIdentity& identity,
		const mister::InputRecipe& recipe, std::uint64_t deadline,
		mister::native::ButtonWriter writer = {}) override
	{
		writers.push_back(writer);
		return native_.Open(identity, recipe, deadline, std::move(writer));
	}
	mister::Error Start(std::uint64_t generation,
		std::function<void(std::uint64_t, mister::Error)> callback) override
	{
		callbacks.push_back(callback);
		return native_.Start(generation, std::move(callback));
	}
	mister::Error Neutralize(std::uint64_t deadline) override
	{
		return native_.Neutralize(deadline);
	}
	mister::Error Stop(std::uint64_t deadline) override
	{
		return native_.Stop(deadline);
	}
	std::vector<mister::native::ButtonWriter> writers;
	std::vector<std::function<void(std::uint64_t, mister::Error)>> callbacks;

private:
	mister::native::NativeInputSession native_;
};

class RecordingFaultSink final : public mister::HardwareFaultSink {
public:
	void ReportHardwareFault(mister::HardwareFault fault) override
	{
		faults.push_back(std::move(fault));
	}
	std::vector<mister::HardwareFault> faults;
};

class RecordingDriver final : public mister::native::CoreDriver {
public:
	explicit RecordingDriver(std::vector<std::string>& events) : events_(events) {}
	void BeginSession() override { events_.push_back("driver.begin"); }
	mister::native::CoreDriverResult Quiesce(
		const mister::native::CoreDriverContext& context, std::uint64_t) override
	{
		events_.push_back("driver.quiesce");
		quiesce_generations.push_back(context.generation);
		return quiesce_result;
	}
	mister::native::CoreDriverResult Identify(
		const mister::native::CoreDriverContext& context, std::uint64_t) override
	{
		events_.push_back("driver.identify");
		identify_generations.push_back(context.generation);
		mister::native::CoreDriverResult result = identify_result;
		if (result.error.ok() && result.observed_core.empty() &&
			context.descriptor != nullptr)
			result.observed_core = context.descriptor->core.id;
		return result;
	}
	mister::native::CoreDriverResult NeutralizeButtons(
		const mister::native::CoreDriverContext&, std::uint64_t) override
	{
		events_.push_back("driver.buttons");
		return buttons_result;
	}
	mister::native::CoreDriverResult SetButtons(
		const mister::native::CoreDriverContext&, std::uint16_t map,
		std::uint64_t) override
	{
		events_.push_back("driver.buttons");
		button_maps.push_back(map);
		if (on_buttons) on_buttons();
		return buttons_result;
	}
	mister::native::CoreDriverResult Start(
		const mister::native::CoreDriverContext& context, std::uint64_t) override
	{
		events_.push_back("driver.start");
		if (on_start) on_start();
		start_generations.push_back(context.generation);
		fault_ = context.report_fault;
		return start_result;
	}
	void Report(std::uint64_t generation, mister::Error error)
	{
		assert(static_cast<bool>(fault_));
		fault_(generation, std::move(error));
	}

	mister::Error CaptureData(const mister::native::CoreDriverContext&, std::uint64_t,
		std::vector<std::uint16_t>* output) override
	{
		events_.push_back("driver.capture");
		*output = live_data;
		if (on_capture)
			on_capture();
		return {};
	}
	mister::Error RestoreData(const mister::native::CoreDriverContext&,
		const std::vector<std::uint16_t>& words, std::uint64_t) override
	{
		events_.push_back("driver.restore");
		live_data = words;
		restored_data.push_back(words);
		return {};
	}
	mister::Error ResumeData(const mister::native::CoreDriverContext&, std::uint64_t) override
	{
		events_.push_back("driver.resume");
		return resume_error;
	}
	std::vector<std::uint16_t> live_data{1, 0};
	std::vector<std::vector<std::uint16_t>> restored_data;
	std::function<void()> on_capture;
	mister::Error resume_error;
	std::vector<std::string>& events_;
	mister::native::CoreDriverResult quiesce_result;
	mister::native::CoreDriverResult identify_result;
	mister::native::CoreDriverResult buttons_result;
	mister::native::CoreDriverResult start_result;
	std::vector<std::uint64_t> quiesce_generations;
	std::vector<std::uint64_t> identify_generations;
	std::vector<std::uint64_t> start_generations;
	std::vector<std::uint16_t> button_maps;
	std::function<void(std::uint64_t, mister::Error)> fault_;
	std::function<void()> on_buttons;
	std::function<void()> on_start;
};

struct Fixture {
	explicit Fixture(mister::native::CoreDriver* gp_driver = nullptr,
		mister::native::IdleRecipe idle_recipe = mister::native::SplashIdle())
		: temporary(), events(), idle(temporary.File("idle.rbf", "idle")),
		  rbf(temporary.File("megadrive.rbf", "game")),
		  media_two(temporary.File("two.bin", "22")),
		  media_zero(temporary.File("zero.bin", "0")),
		  rom(temporary.File("sonic2.bin", "sonic")), opener(events), mmio(), fpga(events),
		  i2c(events), idle_video(events), clock(100),
		  log(events), game_video(i2c, clock, log,
			mister::native::Menu720p60Recipe()), input(events, clock),
		  input_identity({"FogCast Virtual Gamepad", 0x0006, 0x0000, 0x0001,
			0x0001}), sink(),
			hardware(opener, fpga, idle_video, game_video, input,
			input_identity, clock, log, idle, {30000, 10000, 10000},
			gp_driver, {"/tmp"}, std::move(idle_recipe))
	{
		hardware.SetFaultSink(&sink);
	}
	TempDirectory temporary;
	std::vector<std::string> events;
	std::string idle;
	std::string rbf;
	std::string media_two;
	std::string media_zero;
	std::string rom;
	RecordingOpener opener;
	mister_test::FakeMmio mmio;
	RecordingFpga fpga;
	RecordingI2c i2c;
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

void PushFesGpResponse(mister_test::FakeMmio* mmio, bool toggle,
	std::uint16_t response, bool failed = false)
{
	using namespace mister::native::generated;
	const std::uint32_t completed = FesGpSignature |
		(toggle ? FesGpAckMask : 0u) | (failed ? FesGpErrorMask : 0u) | response;
	mmio->PushRead(kSpiGpiAddress, completed ^ FesGpAckMask);
	mmio->PushRead(kSpiGpiAddress, completed);
	mmio->PushRead(kSpiGpiAddress, completed);
}

std::vector<std::uint16_t> FesGpIdentityWords()
{
	using namespace mister::native::generated;
	std::vector<std::uint16_t> words(FesGpIdentityWordCount);
	words[FesGpIdentityMagic0Index] = FesGpIdentityMagic0;
	words[FesGpIdentityMagic1Index] = FesGpIdentityMagic1;
	words[FesGpIdentityTransportMajorIndex] = FesGpTransportMajor;
	words[FesGpIdentityTransportMinorIndex] = FesGpTransportMinor;
	words[FesGpIdentityAbiTagIndex] = FesGpAbiTag;
	words[FesGpIdentityAbiMajorIndex] = FesGpAbiMajor;
	words[FesGpIdentityAbiMinorIndex] = FesGpAbiMinor;
	words[FesGpIdentityCapabilitiesIndex] =
		FesGpCapabilityGamepad | FesGpCapabilityVideoFixed720p60;
	const std::string build = "0123456789abcdef0123456789abcdef";
	std::size_t word = FesGpIdentityBuildIDStartIndex;
	for (std::size_t offset = 0; offset < build.size(); offset += 4) {
		const unsigned first = static_cast<unsigned>(std::stoul(
			build.substr(offset, 2), nullptr, 16));
		const unsigned second = static_cast<unsigned>(std::stoul(
			build.substr(offset + 2, 2), nullptr, 16));
		words[word++] = static_cast<std::uint16_t>(first | (second << 8));
	}
	return words;
}

void ScriptFesGpIdentity(mister_test::FakeMmio* mmio,
	const std::vector<std::uint16_t>& words)
{
	bool toggle = false;
	for (std::uint16_t value : words) {
		toggle = !toggle;
		PushFesGpResponse(mmio, toggle, value);
	}
}

void ScriptFesGpActivation(mister_test::FakeMmio* mmio)
{
	ScriptFesGpIdentity(mmio, FesGpIdentityWords());
	PushFesGpResponse(mmio, true, 0);  // initial neutral after 16 words
	PushFesGpResponse(mmio, false, 0); // gameplay release
}

bool WaitForState(mister::Runtime& runtime, mister::State state)
{
	const auto deadline = std::chrono::steady_clock::now() +
		std::chrono::seconds(2);
	do {
		if (runtime.status().state == state) return true;
		std::this_thread::yield();
	} while (std::chrono::steady_clock::now() < deadline);
	return runtime.status().state == state;
}

struct IntegratedFixture {
	explicit IntegratedFixture(mister::native::CoreDriver* gp_driver = nullptr,
		mister::native::IdleRecipe idle_recipe = mister::native::SplashIdle())
		: native(gp_driver, std::move(idle_recipe)),
		  runtime(native.hardware, native.log) {}
	void Start()
	{
		assert(runtime.Start().ok());
		native.events.clear();
		native.log.Clear();
	}
	Fixture native;
	mister::Runtime runtime;
};



void TestFesGpPackageUsesSelectedDriverAndReturnsToMenuThroughThatDriver()
{
	std::vector<std::string> driver_events;
	RecordingDriver gp(driver_events);
	IntegratedFixture fixture(&gp);
	fixture.Start();
	gp.on_buttons = [&] {
		assert(Find(fixture.native.events, "video.adv.initialize") <
			fixture.native.events.size());
		assert(Find(fixture.native.events, "video.timing:menu_720p60") ==
			fixture.native.events.size());
	};
	gp.on_start = [&] {
		assert(gp.button_maps == std::vector<std::uint16_t>({0}));
	};
	TempDirectory package;
	PopulateFesGpPackage(&package);
	bool outgoing_reset_seen = false;
	fixture.native.fpga.on_program = [&] {
		if (fixture.native.fpga.calls == 2)
			outgoing_reset_seen = fixture.native.mmio.writes.empty() &&
				driver_events.empty();
	};
	const std::string package_id =
		"b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0";
	assert(fixture.runtime.LoadCore(package.path, package_id).ok());
	assert(outgoing_reset_seen);
	assert(fixture.native.fpga.profiles.back() ==
		mister::native::ProgrammingProfile::fes_gp_v1);
	assert(driver_events == std::vector<std::string>({
		"driver.begin", "driver.identify", "driver.buttons", "driver.start"}));
	assert(fixture.native.input.open_calls == 1);
	assert(fixture.native.input.start_calls == 1);
	assert(fixture.native.input.HasActiveCallback());
	assert(gp.button_maps == std::vector<std::uint16_t>({0}));
	const mister::Status running = fixture.runtime.status();
	assert(running.state == mister::State::running_development);
	assert(running.package_id == package_id);
	assert(running.declared_core == "fes.pong");
	assert(running.core == "fes.pong");
	assert(gp.start_generations == std::vector<std::uint64_t>({1}));

	driver_events.clear();
	fixture.native.fpga.on_program = [&] {
		if (fixture.native.fpga.calls == 3)
			assert(driver_events == std::vector<std::string>({
				"driver.buttons", "driver.quiesce"}));
	};
	assert(fixture.runtime.Stop().ok());
	assert(fixture.native.fpga.profiles.back() ==
		mister::native::ProgrammingProfile::development_contained_v1);
}

void TestProductionFesInputDisconnectAndGenerationRetirement()
{
	using namespace mister::native;
	using namespace mister::native::generated;
	TempDirectory temporary;
	TempDirectory package;
	PopulateFesGpPackage(&package);
	std::vector<std::string> events;
	RecordingOpener opener(events);
	mister_test::FakeMmio mmio;
	RecordingFpga fpga(events);
	RecordingI2c i2c(events);


	RecordingVideo idle_video(events);
	FixedClock clock(100);

	LedgerLog log(events);
	FixedVideoBringup game_video(i2c, clock, log,
		Menu720p60Recipe());
	mister_test::FakeInputDevice device;
	RetainingNativeInput input(device, clock);
	const InputDeviceIdentity identity = {
		"FogCast Virtual Gamepad", 0x0006, 0x0000, 0x0001, 0x0001};
	FesGp transport(mmio, clock);
	FesGpCoreDriver gp_driver(transport);
	RecordingFaultSink sink;
	const std::string idle = temporary.File("idle.rbf", "idle");
	NativeHardware hardware(opener, fpga, idle_video, game_video,
		input, identity, clock, log, idle, {30000, 10000, 10000},
		&gp_driver, {"/tmp"});
	hardware.SetFaultSink(&sink);
	const std::string package_id =
		"b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0";

	std::unique_ptr<mister::AdmittedCorePackage> first;
	assert(hardware.AdmitCorePackage(package.path, package_id, &first).ok());
	ScriptFesGpActivation(&mmio);
	const mister::HardwareResult activated = hardware.LoadCore(std::move(first), 1);
	assert(activated.error.ok() && activated.observed_core == "fes.pong");
	assert(mmio.writes.size() == 36);

	PushFesGpResponse(&mmio, true, FesGpButtonUp);
	device.Push({InputControl::up, 1});
	device.Push({InputControl::synchronize, 0});
	assert(device.WaitForReads(2));
	device.PushError({mister::ErrorCode::io_failed, "gamepad disconnected"});
	assert(device.WaitForReads(3));

	// The replacement joins the failed worker, sends its final neutral, then
	// quiesces the outgoing fabric before resetting the new GP session.
	PushFesGpResponse(&mmio, false, 0);
	PushFesGpResponse(&mmio, true, FesGpGameplayHoldReset);
	ScriptFesGpActivation(&mmio);
	std::unique_ptr<mister::AdmittedCorePackage> replacement;
	assert(hardware.AdmitCorePackage(package.path, package_id, &replacement).ok());
	const mister::HardwareResult replaced =
		hardware.LoadCore(std::move(replacement), 2);
	assert(replaced.error.ok() && replaced.observed_core == "fes.pong");
	assert(input.writers.size() == 2 && input.callbacks.size() == 2);
	assert(mmio.writes.size() == 78);
	assert((mmio.writes[37].value & FesGpOpcodeMask) ==
		FesGpOpcodeButtons * (FesGpOpcodeMask & (~FesGpOpcodeMask + 1u)));
	assert((mmio.writes[37].value & FesGpArgumentMask) == FesGpButtonUp);
	assert((mmio.writes[39].value & FesGpArgumentMask) == 0);
	assert(sink.faults.size() == 1 && sink.faults[0].generation == 1);

	const std::size_t replacement_writes = mmio.writes.size();
	assert(input.writers[0](FesGpButtonUp, 1000).ok());
	input.callbacks[0](1,
		{mister::ErrorCode::io_failed, "retained old-generation callback"});
	assert(mmio.writes.size() == replacement_writes);
	assert(sink.faults.size() == 2 && sink.faults[1].generation == 1);

	// Satisfy destruction's final neutral without weakening the stale-writer
	// assertion above.
	PushFesGpResponse(&mmio, true, 0);
}

void TestOnlyVerifiedFesGpMismatchIsQuiescedDuringIdleRecovery()
{
	using namespace mister::native;
	using namespace mister::native::generated;
	auto run = [](std::vector<std::uint16_t> words, bool expect_quiesce) {
		TempDirectory temporary;
		TempDirectory package;
		PopulateFesGpPackage(&package);
		std::vector<std::string> events;
		RecordingOpener opener(events);
		mister_test::FakeMmio mmio;
		RecordingFpga fpga(events);
		RecordingI2c i2c(events);


		RecordingVideo idle_video(events);
		FixedClock clock(100);

		LedgerLog log(events);
		FixedVideoBringup game_video(i2c, clock, log,
			Menu720p60Recipe());
		RecordingInput input(events, clock);
		const InputDeviceIdentity identity = {
			"FogCast Virtual Gamepad", 0x0006, 0x0000, 0x0001, 0x0001};
		FesGp transport(mmio, clock);
		FesGpCoreDriver gp_driver(transport);
		NativeHardware hardware(opener, fpga, idle_video, game_video,
			input, identity, clock, log, temporary.File("idle.rbf", "idle"),
			{30000, 10000, 10000}, &gp_driver, {"/tmp"});
		const std::string package_id =
			"b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0";
		std::unique_ptr<mister::AdmittedCorePackage> admitted;
		assert(hardware.AdmitCorePackage(package.path, package_id, &admitted).ok());
		ScriptFesGpIdentity(&mmio, words);
		if (expect_quiesce) PushFesGpResponse(&mmio, true, 0);
		const mister::HardwareResult loaded = hardware.LoadCore(std::move(admitted), 1);
		assert(loaded.error.code == mister::ErrorCode::core_mismatch);
		assert(mmio.writes.size() == FesGpIdentityWordCount * 2);
		const std::size_t expected_writes = FesGpIdentityWordCount * 2 +
			(expect_quiesce ? 2 : 0);
		fpga.on_program = [&] {
			if (fpga.calls == 2) assert(mmio.writes.size() == expected_writes);
		};
		assert(hardware.LoadIdle().error.ok());
		assert(fpga.calls == 2);
		assert(mmio.writes.size() == expected_writes);
		if (expect_quiesce) {
			const auto& command = mmio.writes[expected_writes - 1].value;
			assert((command & FesGpOpcodeMask) ==
				FesGpOpcodeGameplay * (FesGpOpcodeMask & (~FesGpOpcodeMask + 1u)));
			assert((command & FesGpArgumentMask) == FesGpGameplayHoldReset);
		}
	};

	std::vector<std::uint16_t> wrong_magic = FesGpIdentityWords();
	wrong_magic[FesGpIdentityMagic0Index] ^= 1u;
	run(std::move(wrong_magic), false);
	std::vector<std::uint16_t> wrong_abi = FesGpIdentityWords();
	wrong_abi[FesGpIdentityAbiMajorIndex] ^= 1u;
	run(std::move(wrong_abi), false);
	std::vector<std::uint16_t> missing_capability = FesGpIdentityWords();
	missing_capability[FesGpIdentityCapabilitiesIndex] = 0;
	run(std::move(missing_capability), false);
	std::vector<std::uint16_t> wrong_build = FesGpIdentityWords();
	wrong_build[FesGpIdentityBuildIDStartIndex] ^= 1u;
	run(std::move(wrong_build), true);
}

void TestUnknownAndContainedFabricReceiveNoMisterQuiesceWords()
{
	Fixture startup;
	assert(startup.hardware.LoadIdle().error.ok());
	assert(startup.mmio.writes.empty());

	Fixture contained;
	assert(contained.hardware.LoadContainedDevelopmentRBF(contained.rbf, 1).error.ok());
	contained.mmio.writes.clear();
	assert(contained.hardware.LoadIdle().error.ok());
	assert(contained.mmio.writes.empty());
}


void TestDriverIdentifyStartAndQuiesceFailuresHaveOneRecoveryDecision()
{
	const std::string package_id =
		"b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0";
	{
		std::vector<std::string> events;
		RecordingDriver gp(events);
		gp.identify_result.error = {mister::ErrorCode::core_mismatch,
			"injected stable identity mismatch"};
		gp.identify_result.safe_to_quiesce = true;
		IntegratedFixture fixture(&gp);
		fixture.Start();
		TempDirectory package;
		PopulateFesGpPackage(&package);
		assert(fixture.runtime.LoadCore(package.path, package_id).code ==
			mister::ErrorCode::core_mismatch);
		assert(fixture.native.fpga.calls == 3);
		assert(fixture.runtime.status().state == mister::State::idle);
		assert(Count(events, "driver.identify") == 1);
		assert(Count(events, "driver.buttons") == 0);
		assert(Count(events, "driver.quiesce") == 1);
		assert(gp.quiesce_generations == std::vector<std::uint64_t>({1}));
	}
	{
		std::vector<std::string> events;
		RecordingDriver gp(events);
		gp.identify_result.error = {mister::ErrorCode::core_mismatch,
			"injected unverified identity mismatch"};
		IntegratedFixture fixture(&gp);
		fixture.Start();
		TempDirectory package;
		PopulateFesGpPackage(&package);
		assert(fixture.runtime.LoadCore(package.path, package_id).code ==
			mister::ErrorCode::core_mismatch);
		assert(fixture.native.fpga.calls == 3);
		assert(fixture.runtime.status().state == mister::State::idle);
		assert(Count(events, "driver.identify") == 1);
		assert(Count(events, "driver.quiesce") == 0);
	}
	{
		std::vector<std::string> events;
		RecordingDriver gp(events);
		gp.identify_result.error = {mister::ErrorCode::io_failed,
			"injected identify failure"};
		IntegratedFixture fixture(&gp);
		fixture.Start();
		TempDirectory package;
		PopulateFesGpPackage(&package);
		assert(fixture.runtime.LoadCore(package.path, package_id).code ==
			mister::ErrorCode::io_failed);
		assert(fixture.native.fpga.calls == 3);
		assert(fixture.runtime.status().state == mister::State::idle);
		assert(Count(events, "driver.identify") == 1);
		assert(Count(events, "driver.buttons") == 0);
		assert(Count(events, "driver.quiesce") == 0);
	}
	{
		std::vector<std::string> events;
		RecordingDriver gp(events);
		gp.start_result.error = {mister::ErrorCode::io_failed,
			"injected start failure"};
		IntegratedFixture fixture(&gp);
		fixture.Start();
		TempDirectory package;
		PopulateFesGpPackage(&package);
		assert(fixture.runtime.LoadCore(package.path, package_id).code ==
			mister::ErrorCode::io_failed);
		assert(fixture.native.fpga.calls == 3);
		assert(Count(events, "driver.start") == 1);
		assert(Count(events, "driver.quiesce") == 1);
	}
	{
		std::vector<std::string> events;
		RecordingDriver gp(events);
		IntegratedFixture fixture(&gp);
		fixture.Start();
		TempDirectory package;
		PopulateFesGpPackage(&package);
		assert(fixture.runtime.LoadCore(package.path, package_id).ok());
		gp.quiesce_result = {{mister::ErrorCode::io_failed,
			"injected quiesce failure"}, true, ""};
		assert(fixture.runtime.Stop().code == mister::ErrorCode::idle_failed);
		assert(fixture.runtime.status().state == mister::State::reboot_required);
		assert(fixture.native.fpga.calls == 2);
		assert(Count(events, "driver.quiesce") == 1);
	}
}



void TestPackageReplacementQuiesceFailureRespectsMutationBoundary()
{
	const std::string package_id =
		"b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0";
	{
		std::vector<std::string> events;
		RecordingDriver gp(events);
		IntegratedFixture fixture(&gp);
		fixture.Start();
		TempDirectory package;
		PopulateFesGpPackage(&package);
		assert(fixture.runtime.LoadCore(package.path, package_id).ok());
		events.clear();
		gp.quiesce_generations.clear();
		gp.quiesce_result = {{mister::ErrorCode::io_failed,
			"injected pre-mutation quiesce failure"}, false, ""};
		const mister::Error error = fixture.runtime.LoadCore(package.path, package_id);
		assert(error.code == mister::ErrorCode::idle_failed);
		assert(fixture.runtime.status().state == mister::State::reboot_required);
		assert(fixture.native.fpga.calls == 2);
		assert(Count(events, "driver.quiesce") == 2);
		assert(gp.quiesce_generations ==
			std::vector<std::uint64_t>({1, 1}));
		assert(gp.start_generations == std::vector<std::uint64_t>({1}));
	}
	{
		std::vector<std::string> events;
		RecordingDriver gp(events);
		IntegratedFixture fixture(&gp);
		fixture.Start();
		TempDirectory package;
		PopulateFesGpPackage(&package);
		assert(fixture.runtime.LoadCore(package.path, package_id).ok());
		events.clear();
		gp.quiesce_generations.clear();
		gp.quiesce_result = {{mister::ErrorCode::io_failed,
			"injected ambiguous quiesce failure"}, true, ""};
		const mister::Error error = fixture.runtime.LoadCore(package.path, package_id);
		assert(error.code == mister::ErrorCode::io_failed);
		assert(fixture.runtime.status().state == mister::State::idle);
		assert(fixture.native.fpga.calls == 3);
		assert(Count(events, "driver.quiesce") == 1);
		assert(gp.quiesce_generations == std::vector<std::uint64_t>({1}));
		assert(gp.start_generations == std::vector<std::uint64_t>({1}));
		assert(fixture.native.fpga.profiles.back() ==
			mister::native::ProgrammingProfile::development_contained_v1);
	}
}

void TestMutatingProgramFailureForgetsOutgoingDriverBeforeRecovery()
{
	std::vector<std::string> events;
	RecordingDriver gp(events);
	IntegratedFixture fixture(&gp);
	fixture.Start();
	fixture.native.fpga.fail_call = 2;
	TempDirectory package;
	PopulateFesGpPackage(&package);
	const mister::Error error = fixture.runtime.LoadCore(package.path,
		"b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0");
	assert(error.code == mister::ErrorCode::program_failed);
	assert(fixture.native.fpga.calls == 3);
	assert(fixture.native.mmio.writes.empty());
	assert(events.empty());
	assert(fixture.runtime.status().state == mister::State::idle);
}

void TestDriverFaultCallbackRejectsOldPackageGeneration()
{
	std::vector<std::string> events;
	RecordingDriver gp(events);
	IntegratedFixture fixture(&gp);
	fixture.Start();
	TempDirectory package;
	PopulateFesGpPackage(&package);
	const std::string package_id =
		"b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0";
	assert(fixture.runtime.LoadCore(package.path, package_id).ok());
	assert(fixture.runtime.Stop().ok());
	assert(fixture.runtime.LoadCore(package.path, package_id).ok());
	assert(gp.start_generations == std::vector<std::uint64_t>({1, 2}));
	gp.Report(1, {mister::ErrorCode::io_failed, "stale GP input fault"});
	assert(fixture.runtime.status().state == mister::State::running_development);
	gp.Report(2, {mister::ErrorCode::io_failed, "active GP input fault"});
	assert(WaitForState(fixture.runtime, mister::State::idle));
	assert(fixture.runtime.status().error.message == "active GP input fault");
	assert(fixture.native.fpga.calls == 5);
}















void TestIdleRequiresVideo()
{
	Fixture fixture;
	const mister::HardwareResult idle = fixture.hardware.LoadIdle();
	assert(idle.error.ok());
	assert(idle.mutation_attempted);
	assert(idle.observed_core == "MENU");
	assert(fixture.events == std::vector<std::string>({
		"artifact.open:idle.rbf",
		"idle.video.quiesce",
		"fpga.program",
		"idle.video:",
	}));
	assert(fixture.idle_video.deadlines == std::vector<std::uint64_t>({10100}));
	assert(fixture.fpga.profiles == std::vector<mister::native::ProgrammingProfile>({
		mister::native::ProgrammingProfile::development_contained_v1}));
	assert(fixture.idle_video.last_idle.expected_core.empty());
}



void TestSplashIdleProgramsWithoutProbeOrFramebuffer()
{
	Fixture fixture(nullptr, mister::native::SplashIdle());
	fixture.idle_video.result.observed_core.clear();
	const mister::HardwareResult idle = fixture.hardware.LoadIdle();
	assert(idle.error.ok());
	assert(idle.mutation_attempted);
	assert(idle.observed_core.empty());
	assert(fixture.fpga.profiles == std::vector<mister::native::ProgrammingProfile>({
		mister::native::ProgrammingProfile::development_contained_v1}));
	assert(fixture.idle_video.last_idle.expected_core.empty());
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

void TestIdleFpgaFailureAfterQuiesceReportsHardwareMutation()
{
	Fixture before;
	before.fpga.result = {{mister::ErrorCode::program_failed, "before"}, false};
	const mister::HardwareResult before_result = before.hardware.LoadIdle();
	assert(before_result.error.code == mister::ErrorCode::program_failed);
	assert(before_result.mutation_attempted);
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
		"idle.video.quiesce",
		"fpga.program",
		"idle.video:",
	}));
}














void TestUnavailableHardwareRemainsFailureOnly()
{
	const mister::Error reason = {
		mister::ErrorCode::io_failed, "injected construction failure"};
	std::unique_ptr<mister::Hardware> hardware =
		mister::CreateUnavailableHardware(reason);
	assert(hardware);
	assert(hardware->LoadIdle().error.code == mister::ErrorCode::io_failed);
	assert(hardware->LoadContainedDevelopmentRBF("/x", 1).error.code ==
		mister::ErrorCode::io_failed);
	assert(hardware->LoadContainedDevelopmentRBF("/x", 1).error.code ==
		mister::ErrorCode::io_failed);
	std::unique_ptr<mister::AdmittedCorePackage> package;
	assert(hardware->AdmitCorePackage("/x", std::string(64, 'a'), &package).code ==
		mister::ErrorCode::io_failed);
	mister::CorePackageInspection inspection;
	assert(hardware->InspectCorePackage("/x", std::string(64, 'a'),
		&inspection).code == mister::ErrorCode::io_failed);
}

void TestInspectionReportsActualDriverCompatibilityWithoutMutation()
{
	std::vector<std::string> gp_events;
	RecordingDriver gp_driver(gp_events);
	Fixture available(&gp_driver);
	TempDirectory package;
	PopulateFesGpPackage(&package);
	const std::string id =
		"b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0";
	mister::CorePackageInspection inspection;
	assert(available.hardware.InspectCorePackage(package.path, id,
		&inspection).ok());
	assert(inspection.compatible && inspection.compatibility_error.ok());
	assert(inspection.package_id == id);
	assert(available.fpga.programmed.empty() && gp_events.empty());
	const mister::Capabilities capabilities = available.hardware.capabilities();
	assert(capabilities.programming_profiles == std::vector<std::string>({
		"development-contained-v1", "fes-gp-v1"}));
	assert(capabilities.abis.size() == 3);
	assert(capabilities.abis[0].id == "fes.application");
	assert(capabilities.abis[0].interfaces.size() == 8);
	assert(capabilities.abis[0].interfaces[1].id == "fes.firmware.blob");
	assert(capabilities.abis[0].interfaces[1].major == 1);
	assert(capabilities.abis[0].interfaces[1].minor == 0);
	assert(capabilities.abis[1].id == "fes.simple-computer");
	assert(capabilities.abis[2].id == "fes.simple-game");

	Fixture unavailable;
	assert(unavailable.hardware.InspectCorePackage(package.path, id,
		&inspection).ok());
	assert(!inspection.compatible);
	assert(inspection.compatibility_error.code == mister::ErrorCode::unsupported_abi);
	assert(inspection.compatibility_error.phase == "compatibility");
	assert(unavailable.hardware.capabilities().programming_profiles ==
		std::vector<std::string>({"development-contained-v1"}));
	assert(unavailable.hardware.InspectCorePackage(package.path,
		std::string(64, '0'), &inspection).code ==
		mister::ErrorCode::invalid_package);
}

void TestApplicationVideoOnlyLifecycleNeedsNoInput()
{
	for (const bool ports : {false, true}) {
	for (const bool audio : {false, true}) {
	std::vector<std::string> events;
	RecordingDriver gp(events);
	IntegratedFixture fixture(&gp);
	fixture.Start();
	TempDirectory package;
	std::string manifest = ReadText("tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
	ReplaceAll(&manifest, "fes.simple-game", "fes.application");
	const auto first = manifest.find("[[interfaces]]");
	const auto second = manifest.find("[[interfaces]]", first + 1);
	assert(first != std::string::npos && second != std::string::npos);
	manifest.erase(first, second - first); // remove gamepad; retain fixed video
	if (ports) manifest += "\n[[interfaces]]\nid = \"fes.gamepad.ports\"\nmajor = 1\nminor = 0\nrequired = true\n";
	if (audio) manifest += "\n[[interfaces]]\nid = \"fes.audio.pcm-s16-stereo-48k\"\nmajor = 1\nminor = 0\nrequired = true\n";
	package.File("manifest.toml", manifest);
	package.File("core.rbf", ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
	mister::native::OpenedCorePackage opened;
	assert(mister::native::OpenCorePackage(package.path, "", &opened).ok());
	for (unsigned run = 0; run < 2; ++run) {
		assert(fixture.runtime.LoadCore(package.path, opened.package_id).ok());
		const auto status = fixture.runtime.status();
		assert(status.active_package.observed.abi.id == "fes.application");
		assert(status.active_package.observed.build_id == opened.descriptor.build.id);
		assert(status.capabilities.active_interfaces.size() == 1u + (audio ? 1u : 0u) + (ports ? 1u : 0u));
		assert(status.capabilities.active_interfaces.back().id == "fes.video.fixed-720p60");
		if (audio) assert(status.capabilities.active_interfaces[0].id == "fes.audio.pcm-s16-stereo-48k");
		assert(fixture.native.input.open_calls == 0);
		assert(fixture.runtime.Stop().ok());
		assert(fixture.runtime.status().state == mister::State::idle);
	}
	}
	}
}

void TestApplicationFirmwareStatusAdvertisesOptionalSlot()
{
	std::vector<std::string> events;
	RecordingDriver gp(events);
	IntegratedFixture fixture(&gp);
	fixture.Start();
	const auto idle = fixture.native.hardware.capabilities();
	assert(idle.abis[0].id == "fes.application");
	bool advertised = false;
	for (const auto& contract : idle.abis[0].interfaces)
		if (contract.id == "fes.firmware.blob" && contract.major == 1 && contract.minor == 0)
			advertised = true;
	assert(advertised);
	TempDirectory package;
	std::string manifest = ReadText("tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
	ReplaceAll(&manifest, "fes.simple-game", "fes.application");
	manifest += "\n[[interfaces]]\nid = \"fes.media.blob\"\nmajor = 1\nminor = 0\nrequired = true\n";
	manifest += "\n[[interfaces]]\nid = \"fes.firmware.blob\"\nmajor = 1\nminor = 0\nrequired = false\n";
	package.File("manifest.toml", manifest);
	package.File("core.rbf", ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
	mister::native::OpenedCorePackage opened;
	assert(mister::native::OpenCorePackage(package.path, "", &opened).ok());
	assert(fixture.runtime.LoadCore(package.path, opened.package_id).ok());
	const auto status = fixture.runtime.status();
	bool active = false;
	for (const auto& contract : status.capabilities.active_interfaces)
		if (contract.id == "fes.firmware.blob" && contract.major == 1 && contract.minor == 0)
			active = true;
	assert(active);
	assert(fixture.runtime.Stop().ok());
}

void TestCompositionProgramsRetainedLinkedArtifactAndRechecksBeforeMutation()
{
	for (const bool mutate : {false, true}) {
		std::vector<std::string> driver_events;
		RecordingDriver driver(driver_events);
		Fixture fixture(&driver);
		TempDirectory package, expansion, composition;
		std::string manifest = ReadText("tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
		ReplaceAll(&manifest, "fes.simple-game", "fes.simple-computer");
		ReplaceAll(&manifest, "fes.gamepad", "fes.keyboard");
		manifest += "\n[[interfaces]]\nid = \"fes.expansion.zx81-bus\"\nmajor = 1\nminor = 0\nrequired = false\n";
		manifest += "\n[[interfaces]]\nid = \"fes.media.blob\"\nmajor = 1\nminor = 0\nrequired = true\n";
		package.File("manifest.toml", manifest);
		package.File("core.rbf", ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
		mister::native::OpenedCorePackage base;
		assert(mister::native::OpenCorePackage(package.path, "", &base).ok());
		auto hash = [](const std::string& value) {
			mister::native::Sha256 h; h.Update(value.data(), value.size());
			return mister::native::Sha256Hex(h.Final());
		};
		const std::string cart(40408, 'c'), linked(40408, 'l');
		expansion.File("cart.rbf", cart);
		manifest = "{\"cart_sha256\":\"" + hash(cart) + "\",\"cart_size\":40408,\"device\":\"5CSEBA6U23I7\",\"format\":1,"
			"\"map\":\"fes.zx81-bus.socket/1\",\"recipe_sha256\":\"" + std::string(64,'c') + "\",\"revision\":\"" + std::string(40,'d') +
			"\",\"shell_build_id\":\"" + base.descriptor.build.id + "\",\"shell_package_id\":\"" + base.package_id +
			"\",\"shell_sha256\":\"" + base.descriptor.payload.sha256 + "\",\"slot\":\"fes.expansion.zx81-bus\",\"slot_major\":1,\"slot_minor\":0}";
		expansion.File("manifest.json", manifest);
		mister::CoreCompositionRequest request;
		request.expansion_path = expansion.path;
		request.payload_path = composition.File("linked.rbf", linked);
		auto& info = request.composition;
		info.package_id = base.package_id;
		info.shell_sha256 = base.descriptor.payload.sha256;
		info.expansion_id = hash(std::string("fes-expansion-v1\0",17) + manifest);
		info.payload_sha256 = hash(linked); info.payload_size = linked.size();
		info.id = hash(std::string("fes-composition-v1\0",19) + info.package_id + std::string(1,'\0') + info.expansion_id + std::string(1,'\0') + info.payload_sha256);
		std::unique_ptr<mister::AdmittedCorePackage> admitted;
		const auto admission = fixture.hardware.AdmitCoreComposition(package.path, base.package_id, request, &admitted);
		if (!admission.ok()) fprintf(stderr, "composition admission: %s\n", admission.message.c_str());
		assert(admission.ok());
		fixture.events.clear();
		if (mutate) {
			const int fd = open(request.payload_path.c_str(), O_WRONLY);
			assert(fd >= 0 && pwrite(fd, "x", 1, 0) == 1); assert(close(fd) == 0);
		} else {
			assert(rename(request.payload_path.c_str(), (composition.path + "/retained.rbf").c_str()) == 0);
			composition.files.back() = composition.path + "/retained.rbf";
			composition.File("linked.rbf", std::string(40408, 'x'));
		}
		const auto result = fixture.hardware.LoadCore(std::move(admitted), 1);
		if (mutate) {
			assert(result.error.code == mister::ErrorCode::invalid_package);
			assert(!result.mutation_attempted);
			assert(fixture.events.empty() && driver_events.empty());
		} else {
			assert(result.error.ok());
			assert(fixture.fpga.programmed == std::vector<std::string>{"linked.rbf"});
			assert(fixture.fpga.programmed_first_bytes == std::vector<char>{'l'});
		}
	}
}

void TestActivationRechecksRetainedPayloadIdentityBeforeMutation()
{
	std::vector<std::string> gp_events;
	RecordingDriver gp_driver(gp_events);
	Fixture fixture(&gp_driver);
	TempDirectory package;
	PopulateFesGpPackage(&package);
	const std::string id =
		"b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0";
	std::unique_ptr<mister::AdmittedCorePackage> admitted;
	assert(fixture.hardware.AdmitCorePackage(package.path, id, &admitted).ok());
	const int payload = open((package.path + "/core.rbf").c_str(), O_WRONLY);
	assert(payload >= 0);
	const char replacement[] = "changed-data";
	static_assert(sizeof(replacement) - 1 == 12, "fixture payload size");
	assert(write(payload, replacement, sizeof(replacement) - 1) == 12);
	assert(close(payload) == 0);
	const mister::HardwareResult result =
		fixture.hardware.LoadCore(std::move(admitted), 1);
	assert(result.error.code == mister::ErrorCode::invalid_package);
	assert(result.error.phase == "admission");
	assert(!result.mutation_attempted);
	assert(fixture.fpga.programmed.empty() && gp_events.empty());
}

std::string PersistentPackage(TempDirectory* package, const std::string& suffix = "")
{
	std::string manifest = ReadText("tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
	manifest +=
		"\n[[interfaces]]\nid = \"fes.persistence.words\"\nmajor = 1\nminor = 0\nrequired = "
		"true\n[[interfaces]]\nid = \"fes.pong.progress\"\nmajor = 1\nminor = 0\nrequired = true\n";
	manifest += suffix;
	package->File("manifest.toml", manifest);
	package->File("core.rbf", ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
	mister::native::OpenedCorePackage opened;
	assert(mister::native::OpenCorePackage(package->path, "", &opened).ok());
	return opened.package_id;
}
void TestProductionFactoryForwardsCoreDataWithoutHardwareMutation()
{
	const char* development_root = "/tmp/fogcast-development";
	const char* package_root = "/tmp/fogcast-development/core-packages";
	const bool created_development = mkdir(development_root, 0700) == 0;
	assert(created_development || errno == EEXIST);
	const bool created_packages = mkdir(package_root, 0700) == 0;
	assert(created_packages || errno == EEXIST);
	bool inspected = false, prepared = false, refreshed = false, updated = false;
	{
		TempDirectory package(package_root), data;
		const std::string id = PersistentPackage(&package);
		mister_test::CaptureLog log;
		std::unique_ptr<mister::Hardware> hardware;
		assert(mister::CreateProductionHardware(log, &hardware).ok());
		// Creation and data operations are lazy with respect to all physical devices.
		// Do not call Start/LoadIdle/LoadCore: this exercises only production admission/storage.
		mister::CoreData observed;
		auto result = hardware->InspectCoreData(package.path, id, data.path, &observed);
		inspected = result.ok() && observed.package_id == id && observed.mode == "persistent" &&
					observed.revision == "absent" && observed.paddle_speed == 1 &&
					observed.best_rally == 0;
		std::unique_ptr<mister::AdmittedCorePackage> admitted;
		assert(hardware->AdmitCorePackage(package.path, id, &admitted).ok());
		result = hardware->PrepareCoreData(admitted.get(), data.path, &observed);
		prepared = result.ok() && observed.package_id == id && observed.revision == "absent";
		std::unique_ptr<mister::native::CoreDataFile> file;
		assert(mister::native::CoreDataFile::Open(data.path, "fes.pong", &file).ok());
		mister::CoreData saved;
		saved.core_id = "fes.pong";
		saved.layout = {"fes.pong.progress", 1, 0};
		saved.paddle_speed = 2;
		saved.best_rally = 17;
		assert(file->Persist(saved, "absent", &saved).ok());
		observed = {};
		result = hardware->RefreshCoreData(admitted.get(), &observed);
		refreshed = result.ok() && observed.package_id == id &&
					observed.revision == saved.revision && observed.paddle_speed == 2 &&
					observed.best_rally == 17;
		result =
			hardware->UpdateCoreSettings(package.path, id, data.path, saved.revision, 0, &observed);
		mister::CoreData reread;
		assert(file->Read(&reread).ok());
		updated = result.ok() && observed.package_id == id && observed.revision != saved.revision &&
				  observed.revision == reread.revision && reread.paddle_speed == 0 &&
				  reread.best_rally == 17;
		const std::string directory =
			data.path + "/" + mister::native::CoreDataNamespace("fes.pong");
		hardware.reset();
		admitted.reset();
		file.reset();
		assert(unlink((directory + "/record.bin").c_str()) == 0);
		assert(rmdir(directory.c_str()) == 0);
	}
	if (created_packages)
		assert(rmdir(package_root) == 0 || errno == ENOTEMPTY);
	if (created_development)
		assert(rmdir(development_root) == 0 || errno == ENOTEMPTY);
	assert(inspected && prepared && refreshed && updated);
}

void TestPersistentReplacementRefreshAndSaveFailureResume()
{
	std::vector<std::string> events;
	RecordingDriver gp(events);
	IntegratedFixture fixture(&gp);
	fixture.Start();
	TempDirectory a, b, data;
	auto aid = PersistentPackage(&a);
	auto bid = PersistentPackage(&b, "# next compatible version\n");
	mister::CoreData preflight;
	assert(fixture.runtime.InspectCoreData(a.path, aid, data.path, &preflight).ok());
	const std::string namespace_path =
		data.path + "/" + mister::native::CoreDataNamespace("fes.pong");
	assert(chmod(namespace_path.c_str(), 0500) == 0);
	const auto before_program = fixture.native.fpga.calls;
	assert(fixture.runtime.LoadLibraryCore(a.path, aid, data.path).code ==
		   mister::ErrorCode::save_failed);
	assert(fixture.native.fpga.calls == before_program && fixture.native.input.open_calls == 0);
	assert(chmod(namespace_path.c_str(), 0700) == 0);
	assert(fixture.runtime.LoadLibraryCore(a.path, aid, data.path).ok());
	assert(gp.restored_data.back() == std::vector<std::uint16_t>({1, 0}));
	gp.live_data = {2, 17};
	assert(fixture.runtime.LoadLibraryCore(b.path, bid, data.path).ok());
	assert(gp.restored_data.back() == std::vector<std::uint16_t>({2, 17}));
	assert(fixture.runtime.status().core_data.mode == "persistent");
	mister::CoreData inspected;
	assert(fixture.runtime.InspectCoreData(b.path, bid, data.path, &inspected).ok());
	assert(inspected.paddle_speed == 2 && inspected.best_rally == 17);
	assert(fixture.runtime
			   .UpdateCoreSettings(b.path, bid, data.path, inspected.revision, 0, &inspected)
			   .code == mister::ErrorCode::busy);
	const std::string dir = data.path + "/" + mister::native::CoreDataNamespace("fes.pong");
	gp.on_capture = [&] { assert(chmod(dir.c_str(), 0500) == 0); };
	gp.live_data = {2, 19};
	const auto generation = fixture.runtime.status().generation;
	const auto programs = fixture.native.fpga.calls;
	events.clear();
	assert(fixture.runtime.Stop().code == mister::ErrorCode::save_failed);
	assert(fixture.runtime.status().generation == generation &&
		   fixture.native.input.HasActiveCallback());
	assert(fixture.native.fpga.calls == programs);
	assert(Find(events, "driver.capture") < Find(events, "driver.resume"));
	assert(events.back() == "driver.buttons");
	assert(Find(events, "driver.quiesce") == events.size());
	assert(chmod(dir.c_str(), 0700) == 0);
	gp.on_capture = {};
	assert(fixture.runtime.InspectCoreData(b.path, bid, data.path, &inspected).ok() &&
		   inspected.best_rally == 17);
	gp.live_data = {2, 25};
	assert(fixture.runtime.Stop().ok());
	assert(fixture.runtime.InspectCoreData(b.path, bid, data.path, &inspected).ok() &&
		   inspected.best_rally == 25);
	assert(
		fixture.runtime.UpdateCoreSettings(b.path, bid, data.path, "absent", 0, &inspected).code ==
		mister::ErrorCode::stale_revision);
	auto revision = inspected.revision;
	assert(
		fixture.runtime.UpdateCoreSettings(b.path, bid, data.path, revision, 0, &inspected).ok() &&
		inspected.best_rally == 25 && inspected.paddle_speed == 0);
	TempDirectory old;
	PopulateFesGpPackage(&old);
	assert(fixture.runtime
			   .InspectCoreData(old.path,
				   "b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0", data.path,
				   &inspected)
			   .code == mister::ErrorCode::incompatible_data);
	assert(unlink((dir + "/record.bin").c_str()) == 0);
	assert(rmdir(dir.c_str()) == 0);
}

void TestPersistenceUnsafeResumeRetainsRecoveryOwnership()
{
	std::vector<std::string> events;
	RecordingDriver gp(events);
	IntegratedFixture fixture(&gp);
	fixture.Start();
	TempDirectory package, data;
	auto id = PersistentPackage(&package);
	assert(fixture.runtime.LoadLibraryCore(package.path, id, data.path).ok());
	const auto generation = fixture.runtime.status().generation;
	const auto programs = fixture.native.fpga.calls;
	const std::string dir = data.path + "/" + mister::native::CoreDataNamespace("fes.pong");
	gp.live_data = {2, 17};
	gp.on_capture = [&] { assert(chmod(dir.c_str(), 0500) == 0); };
	gp.resume_error = {mister::ErrorCode::io_failed, "ambiguous resume", "core_data"};
	events.clear();
	auto error = fixture.runtime.Stop();
	assert(error.code == mister::ErrorCode::idle_failed && error.phase == "recovery");
	const auto status = fixture.runtime.status();
	assert(status.state == mister::State::reboot_required && status.generation == generation &&
		   status.package_id == id && status.core_data.mode == "persistent");
	assert(fixture.native.fpga.calls == programs && !fixture.native.input.HasActiveCallback());
	assert(Find(events, "driver.quiesce") == events.size());
	mister::CoreData inspected;
	assert(fixture.runtime.UpdateCoreSettings(package.path, id, data.path, "absent", 0, &inspected)
			   .code == mister::ErrorCode::busy);
	assert(chmod(dir.c_str(), 0700) == 0);
	assert(rmdir(dir.c_str()) == 0);
}
void TestPersistenceContractAdmissionAndVolatileIsolation()
{
	std::vector<std::string> events;
	RecordingDriver gp(events);
	IntegratedFixture fixture(&gp);
	fixture.Start();
	const auto capabilities = fixture.native.hardware.capabilities();
	assert(std::is_sorted(capabilities.abis[0].interfaces.begin(),
		capabilities.abis[0].interfaces.end(),
		[](const mister::SupportedInterface& a, const mister::SupportedInterface& b) {
			return a.id < b.id;
		}));
	TempDirectory package, data, old;
	auto id = PersistentPackage(&package);
	PopulateFesGpPackage(&old);
	mister::native::OpenedCorePackage opened;
	assert(mister::native::OpenCorePackage(package.path, id, &opened).ok());
	auto incomplete = opened.descriptor;
	incomplete.interfaces.pop_back();
	assert(!mister::native::CheckCoreCompatibility(incomplete).ok());
	auto multiple = opened.descriptor;
	multiple.interfaces.push_back(multiple.interfaces.back());
	assert(!mister::native::CheckCoreCompatibility(multiple).ok());
	assert(fixture.runtime.LoadLibraryCore(package.path, id, data.path).ok());
	mister::CoreData inspected;
	assert(fixture.runtime
			   .InspectCoreData(old.path,
				   "b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0", data.path,
				   &inspected)
			   .code == mister::ErrorCode::incompatible_data);
	gp.live_data = {2, 17};
	assert(fixture.runtime.Stop().ok());
	assert(fixture.runtime.InspectCoreData(package.path, id, data.path, &inspected).ok());
	auto revision = inspected.revision;
	assert(fixture.runtime.LoadCore(package.path, id).ok());
	assert(fixture.runtime.status().core_data.mode == "volatile");
	gp.live_data = {0, 99};
	assert(fixture.runtime.Stop().ok());
	assert(fixture.runtime.InspectCoreData(package.path, id, data.path, &inspected).ok() &&
		   inspected.revision == revision && inspected.best_rally == 17);
	const std::string dir = data.path + "/" + mister::native::CoreDataNamespace("fes.pong");
	assert(unlink((dir + "/record.bin").c_str()) == 0);
	assert(rmdir(dir.c_str()) == 0);
}

} // namespace

// Model the endpoint readiness gate, rather than acknowledging premature release.
// Other protocol responses remain scripted; this is not a second stream codec.
class MediaReadyGate final : public mister::native::Mmio {
public:
	mister_test::FakeMmio scripted;
	bool ready = false;
	bool reset_held = true;
	unsigned releases = 0;
	mister::Error Write32(std::uint32_t address, std::uint32_t value) override
	{
		request_ = value;
		return scripted.Write32(address, value);
	}
	mister::Error Read32(std::uint32_t address, std::uint32_t* value) override
	{
		using namespace mister::native::generated;
		auto error = scripted.Read32(address, value);
		if (!error.ok()) return error;
		const bool toggle = (request_ & FesGpRequestMask) != 0;
		if (((*value & FesGpAckMask) != 0) != toggle) return {};
		const auto opcode = (request_ & FesGpOpcodeMask) >> 24;
		const auto argument = request_ & FesGpArgumentMask;
		if (opcode == FesSimpleComputerOpcodeExecution &&
			argument == FesSimpleComputerExecutionRelease && !ready)
			*value = (*value & ~FesGpResponseMask) | FesGpErrorMask |
				FesSimpleComputerErrorInvalidState;
		if (toggle == applied_toggle_) return {};
		applied_toggle_ = toggle;
		if (*value & FesGpErrorMask) return {};
		if (opcode == FesSimpleComputerOpcodeMediaStreamBegin ||
			opcode == FesSimpleComputerOpcodeMediaStreamAbort) ready = false;
		if (opcode == FesSimpleComputerOpcodeMediaStreamCommit) ready = true;
		if (opcode == FesSimpleComputerOpcodeExecution) {
			reset_held = argument == FesSimpleComputerExecutionHoldReset;
			if (!reset_held) ++releases;
		}
		return {};
	}
private:
	std::uint32_t request_ = 0;
	bool applied_toggle_ = false;
};

void TestNativeStreamSnapshotSizeCleanupAndObservedCapabilities()
{
	using namespace mister::native;
	using namespace mister::native::generated;
	for (const bool controller_ports : {false, true}) {
	MediaReadyGate endpoint;
	auto& mmio = endpoint.scripted;
	FixedClock clock(100);
	FesGp transport(endpoint, clock);
	FesGpCoreDriver driver(transport);
	Fixture fixture(&driver);
	TempDirectory package;
	std::string manifest = ReadText("tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
	ReplaceAll(&manifest, "fes.simple-game", controller_ports ? "fes.application" : "fes.simple-computer");
	ReplaceAll(&manifest, "fes.gamepad", controller_ports ? "fes.gamepad.ports" : "fes.keyboard");
	if (controller_ports)
		manifest += "\n[[interfaces]]\nid = \"fes.keypad.ports\"\nmajor = 1\nminor = 0\nrequired = true\n";
	manifest += "\n[[interfaces]]\nid = \"fes.media.blob\"\nmajor = 1\nminor = 0\nrequired = true\n"
		"\n[[interfaces]]\nid = \"fes.media.blob-stream\"\nmajor = 1\nminor = 0\nrequired = true\n";
	package.File("manifest.toml", manifest);
	package.File("core.rbf", ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
	OpenedCorePackage opened;
	assert(OpenCorePackage(package.path, "", &opened).ok());
	std::unique_ptr<mister::AdmittedCorePackage> admitted;
	assert(fixture.hardware.AdmitCorePackage(package.path, opened.package_id, &admitted).ok());
	assert(fixture.hardware.capabilities().media_stream.interface.id.empty());
	auto words = FesGpIdentityWords();
	words[FesGpIdentityAbiTagIndex] = controller_ports ? FesApplicationAbiTag : FesSimpleComputerAbiTag;
	words[FesGpIdentityCapabilitiesIndex] = controller_ports ? 110 : 15;
	ScriptFesGpIdentity(&mmio, words);
	bool toggle = false;
	auto reply = [&](std::uint16_t value = 0, bool failed = false) {
		toggle = !toggle;
		PushFesGpResponse(&mmio, toggle, value, failed);
	};
	for (auto value : {1, 0, 32768, 0, 512}) reply(value);
	for (unsigned i = 0; i < 9u; ++i) reply(); // neutral inputs + hold
	const auto activated = fixture.hardware.LoadCore(std::move(admitted), 1);
	if (!activated.error.ok()) fprintf(stderr, "fresh stream activation: %s\n", activated.error.message.c_str());
	assert(activated.error.ok());
	assert(endpoint.reset_held && !endpoint.ready && endpoint.releases == 0);
	const auto capacity = fixture.hardware.capabilities().media_stream;
	assert(capacity.interface.id == "fes.media.blob-stream");
	assert(capacity.min_bytes == 1 && capacity.max_bytes == 32768 && capacity.chunk_bytes == 512);
	const auto media = package.File("media", "123");
	const auto before = mmio.writes.size();
	assert(!fixture.hardware.LoadComputerMediaStream(media, 4).ok());
	assert(!fixture.hardware.LoadComputerMediaStream(media, 32769).ok());
	for (const auto& path : {package.path + "/missing", package.path + "/missing/media", package.path}) {
		const auto admission = fixture.hardware.LoadComputerMediaStream(path, 3);
		assert(admission.code == mister::ErrorCode::io_failed);
		assert(admission.phase == "request");
		assert(mmio.writes.size() == before); // no hold, transfer or Abort on admission failure
	}
	assert(mmio.writes.size() == before);
	for (unsigned i = 0; i < (controller_ports ? 16u : 12u); ++i) reply();
	assert(fixture.hardware.LoadComputerMediaStream(media, 3).ok());
	assert(mmio.writes.size() == before + (controller_ports ? 32 : 24));
	assert(!endpoint.reset_held && endpoint.ready && endpoint.releases == 1);
	if (controller_ports) {
		// The real native path snapshots and sends all 64 chunks, including upper ROM.
		const auto full_media = package.File("full-media", std::string(32768, 'x'));
		for (unsigned i = 0; i < 11 + 64 * (3 + 256); ++i) reply();
		assert(fixture.hardware.LoadComputerMediaStream(full_media, 32768).ok());
		assert(!endpoint.reset_held && endpoint.ready && endpoint.releases == 2);
		reply(); reply();
		assert(driver.SetController(1, 0x20, 0x800, 10000).ok());
	}
	// A rejected Commit (including endpoint CRC failure) must never release media.
	const auto releases_before_rejection = endpoint.releases;
	for (unsigned i = 0; i < (controller_ports ? 14u : 10u); ++i) reply();
	reply(4, true);
	reply(); // bounded Abort
	assert(!fixture.hardware.LoadComputerMediaStream(media, 3).ok());
	assert(endpoint.reset_held && !endpoint.ready);
	assert(endpoint.releases == releases_before_rejection);
	// Explicit data rejection causes one independently acknowledged Abort, no release.
	for (unsigned i = 0; i < (controller_ports ? 12u : 8u); ++i) reply();
	reply(4, true);
	reply();
	auto error = fixture.hardware.LoadComputerMediaStream(media, 3);
	assert(!error.ok() && error.phase == "input");
	assert(((mmio.writes.back().value >> 24) & 127) == FesSimpleComputerOpcodeMediaStreamAbort);
	for (unsigned i = 0; i < (controller_ports ? 16u : 12u); ++i) reply();
	assert(fixture.hardware.LoadComputerMediaStream(media, 3).ok());
	// Abort failure retains pending ownership; retry cannot send Begin or legacy data.
	for (unsigned i = 0; i < (controller_ports ? 12u : 8u); ++i) reply();
	reply(4, true);
	reply(4, true);
	error = fixture.hardware.LoadComputerMediaStream(media, 3);
	assert(!error.ok() && error.phase == "recovery");
	const auto failed = mmio.writes.size();
	assert(!driver.LoadMedia({1}, 10000).ok());
	assert(mmio.writes.size() == failed);
	// Explicit Stop can establish a new programmed session outside the stream path.
	if (controller_ports) for (unsigned i = 0; i < 4; ++i) reply();
	reply(); // outgoing hold reset
	assert(fixture.hardware.LoadIdle().error.ok());
	assert(fixture.hardware.capabilities().media_stream.interface.id.empty());
	}
}

int main()
{
	TestApplicationVideoOnlyLifecycleNeedsNoInput();
	TestApplicationFirmwareStatusAdvertisesOptionalSlot();
	TestNativeStreamSnapshotSizeCleanupAndObservedCapabilities();
	TestProductionFactoryForwardsCoreDataWithoutHardwareMutation();
	TestPersistentReplacementRefreshAndSaveFailureResume();
	TestPersistenceUnsafeResumeRetainsRecoveryOwnership();
	TestPersistenceContractAdmissionAndVolatileIsolation();
	TestFesGpPackageUsesSelectedDriverAndReturnsToMenuThroughThatDriver();
	TestProductionFesInputDisconnectAndGenerationRetirement();
	TestOnlyVerifiedFesGpMismatchIsQuiescedDuringIdleRecovery();
	TestUnknownAndContainedFabricReceiveNoMisterQuiesceWords();
	TestDriverIdentifyStartAndQuiesceFailuresHaveOneRecoveryDecision();
	TestPackageReplacementQuiesceFailureRespectsMutationBoundary();
	TestMutatingProgramFailureForgetsOutgoingDriverBeforeRecovery();
	TestDriverFaultCallbackRejectsOldPackageGeneration();
	TestIdleRequiresVideo();
	TestSplashIdleProgramsWithoutProbeOrFramebuffer();
	TestIdlePreflightFailureCallsNeitherFpgaNorVideo();
	TestIdleFpgaFailureAfterQuiesceReportsHardwareMutation();
	TestIdleVideoFailureIsAttemptedIoFailureWithoutCleanup();
	TestUnavailableHardwareRemainsFailureOnly();
	TestInspectionReportsActualDriverCompatibilityWithoutMutation();
	TestCompositionProgramsRetainedLinkedArtifactAndRechecksBeforeMutation();
	TestActivationRechecksRetainedPayloadIdentityBeforeMutation();
	puts("native_hardware_test: 24 passed");
	return 0;
}
