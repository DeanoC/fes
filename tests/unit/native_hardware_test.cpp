// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "capture_log.hpp"
#include "fake_input.hpp"
#include "fake_mmio.hpp"
#include "snes_save_spi.hpp"
#include "linux/production_hardware.hpp"
#include "native/artifacts.hpp"
#include "native/core_loader.hpp"
#include "native/core_package.hpp"
#include "native/fes_gp.hpp"
#include "native/generated/fes_gp.hpp"
#include "native/hardware.hpp"
#include "native/core_data.hpp"
#include "native/input.hpp"
#include "native/linux/i2c.hpp"
#include "native/linux/input.hpp"
#include "native/linux/spi.hpp"
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

std::string BasicNesRom()
{
	std::string bytes(16u + 2u * 16384u + 1u * 8192u, '\0');
	bytes[0] = 'N'; bytes[1] = 'E'; bytes[2] = 'S'; bytes[3] = 0x1a;
	bytes[4] = 2; bytes[5] = 1;
	return bytes;
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

void PopulateMisterPackage(TempDirectory* package, bool with_system)
{
	std::string manifest = ReadText(
		"tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
	ReplaceAll(&manifest, "fes-gp-v1", "mister-v1");
	ReplaceAll(&manifest, "fes.simple-game", "mister");
	ReplaceAll(&manifest, "required = true", "required = false");
	if (with_system) {
		const std::string version = "version = \"0.1.0\"";
		const std::size_t position = manifest.find(version);
		assert(position != std::string::npos);
		manifest.insert(position + version.size(), "\nsystem = \"megadrive\"");
	}
	package->File("manifest.toml", manifest);
	package->File("core.rbf", ReadText(
		"tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
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
	mister::native::VideoQuiesceResult quiesce_result;
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
	mister::Error Exchange(std::uint8_t target,
		const std::vector<std::uint16_t>& request,
		std::vector<std::uint16_t>* response, std::uint64_t deadline) override
	{
		deadlines.push_back(deadline);
		requests.push_back(request);
		if (save_spi && !request.empty() && (request[0] == 0x53 || request[0] == 0x1e ||
		    request[0] == 0x1c || request[0] == 0x1d || request[0] == 0x16 || request[0] == 0x17 || request[0] == 0x18)) {
			if (request[0] == 0x18) events_.push_back("save.snapshot");
			if (request[0] == 0x17) events_.push_back("save.restore");
			auto error = save_spi->Exchange(target, request, response, deadline);
			if (!error.ok()) return error;
		}
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
		} else if (request == std::vector<std::uint16_t>({0x0026, 0x0000})) {
			event = "audio.volume:0";
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
	std::vector<std::vector<std::uint16_t>> requests;
	mister::Error sync_error;
	std::string fail_event;
	std::size_t status_calls = 0;
	mister_test::SnesSaveSpi* save_spi = nullptr;
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
		mister::native::Spi& spi, mister::native::Clock& clock)
		: native_(device, spi, clock, 25) {}
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
		const mister::Profiles* profiles = nullptr)
		: temporary(), events(), idle(temporary.File("idle.rbf", "idle")),
		  rbf(temporary.File("megadrive.rbf", "game")),
		  media_two(temporary.File("two.bin", "22")),
		  media_zero(temporary.File("zero.bin", "0")),
		  rom(temporary.File("sonic2.bin", "sonic")), opener(events), mmio(), fpga(events),
		  i2c(events), spi(events, i2c), core(spi), idle_video(events), clock(100),
		  driver(mmio, core, clock),
		  log(events), game_video(spi, i2c, clock, log,
			mister::native::Menu720p60Recipe()), input(events, clock),
		  input_identity({"FogCast Virtual Gamepad", 0x0006, 0x0000, 0x0001,
			0x0001}), sink(),
			hardware(opener, fpga, core, idle_video, game_video, input,
			input_identity, clock, log, idle, {30000, 10000, 10000}, driver,
			gp_driver, profiles, {"/tmp"})
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
	mister_test::FakeMmio mmio;
	RecordingFpga fpga;
	RecordingI2c i2c;
	RecordingSpi spi;
	mister::native::CoreLoader core;
	RecordingVideo idle_video;
	FixedClock clock;
	mister::native::MisterCoreDriver driver;
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
	std::uint16_t response)
{
	using namespace mister::native::generated;
	const std::uint32_t completed = FesGpSignature |
		(toggle ? FesGpAckMask : 0u) | response;
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
	explicit IntegratedFixture(mister::native::CoreDriver* gp_driver = nullptr)
		: native(gp_driver), profiles(BuildProfiles(native)),
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
	"video.quiesce",
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
	"audio.volume:0",
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
	assert(Find(fixture.events, "video.quiesce") == 5);
	assert(Find(fixture.events, "fpga.program") == 6);
	assert(Find(fixture.events, "core.sync") == 7);
	assert(Find(fixture.events, "core.reset.assert") == 8);
	assert(Find(fixture.events, "core.probe:MegaDrive") == 9);
	assert(Find(fixture.events, "core.status.initial") == 10);
	assert(Find(fixture.events, "core.media.select:0") <
		Find(fixture.events, "core.media.select:2"));
	assert(Find(fixture.events, "input.neutral") <
		Find(fixture.events, "core.reset.release"));
	assert(Find(fixture.events, "core.reset.release") <
		Find(fixture.events, "input.start:1"));
}

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
			outgoing_reset_seen = !fixture.native.mmio.writes.empty() &&
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
		mister::native::ProgrammingProfile::mister_v1);
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
	RecordingSpi spi(events, i2c);
	CoreLoader core(spi);
	RecordingVideo idle_video(events);
	FixedClock clock(100);
	MisterCoreDriver mister_driver(mmio, core, clock);
	LedgerLog log(events);
	FixedVideoBringup game_video(spi, i2c, clock, log,
		Menu720p60Recipe());
	mister_test::FakeInputDevice device;
	RetainingNativeInput input(device, spi, clock);
	const InputDeviceIdentity identity = {
		"FogCast Virtual Gamepad", 0x0006, 0x0000, 0x0001, 0x0001};
	FesGp transport(mmio, clock);
	FesGpCoreDriver gp_driver(transport);
	RecordingFaultSink sink;
	const std::string idle = temporary.File("idle.rbf", "idle");
	NativeHardware hardware(opener, fpga, core, idle_video, game_video,
		input, identity, clock, log, idle, {30000, 10000, 10000},
		mister_driver, &gp_driver, nullptr, {"/tmp"});
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
		RecordingSpi spi(events, i2c);
		CoreLoader core(spi);
		RecordingVideo idle_video(events);
		FixedClock clock(100);
		MisterCoreDriver mister_driver(mmio, core, clock);
		LedgerLog log(events);
		FixedVideoBringup game_video(spi, i2c, clock, log,
			Menu720p60Recipe());
		RecordingInput input(events, clock);
		const InputDeviceIdentity identity = {
			"FogCast Virtual Gamepad", 0x0006, 0x0000, 0x0001, 0x0001};
		FesGp transport(mmio, clock);
		FesGpCoreDriver gp_driver(transport);
		NativeHardware hardware(opener, fpga, core, idle_video, game_video,
			input, identity, clock, log, temporary.File("idle.rbf", "idle"),
			{30000, 10000, 10000}, mister_driver, &gp_driver, nullptr, {"/tmp"});
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
	assert(contained.hardware.LoadDevelopmentRBF(contained.rbf,
		mister::native::ProgrammingProfile::development_contained_v1).error.ok());
	contained.mmio.writes.clear();
	assert(contained.hardware.LoadIdle().error.ok());
	assert(contained.mmio.writes.empty());
}

void TestPackagedMisterIsExplicitDevelopmentAndValidatesDeclaredSystem()
{
	mister::Profile profile;
	profile.system = "megadrive";
	profile.expected_core = "MegaDrive";
	profile.rbf = "/cores/megadrive.rbf";
	profile.core = {0x1111, 0x2222, 0x3333,
		mister::FileWireFormat::little_endian_byte_pairs};
	profile.input = {1, 0x02, 0x0008, 0x0004, 0x0002, 0x0001,
		0x0010, 0x0020, 0x0040, 0x0080};
	mister::Profiles profiles;
	assert(profiles.Add(profile).ok());
	Fixture fixture(nullptr, &profiles);
	mister::Runtime runtime(fixture.hardware, profiles, fixture.log);
	assert(runtime.Start().ok());
	TempDirectory package;
	PopulateMisterPackage(&package, true);
	mister::native::OpenedCorePackage inspected;
	assert(mister::native::OpenCorePackage(package.path, "", &inspected).ok());
	assert(runtime.LoadCore(package.path, inspected.package_id).ok());
	const mister::Status status = runtime.status();
	assert(status.state == mister::State::running_development);
	assert(status.execution == mister::Execution::development);
	assert(status.system.empty());
	assert(status.active_package.descriptor.core.system == "megadrive");
	assert(status.declared_core == "fes.pong");
	assert(status.core == "MegaDrive");
	assert(fixture.input.open_calls == 0);
	assert(fixture.fpga.profiles.back() ==
		mister::native::ProgrammingProfile::mister_v1);

	TempDirectory unknown_package;
	PopulateMisterPackage(&unknown_package, true);
	std::string manifest = ReadText(unknown_package.path + "/manifest.toml");
	ReplaceAll(&manifest, "system = \"megadrive\"", "system = \"unknown\"");
	assert(unlink((unknown_package.path + "/manifest.toml").c_str()) == 0);
	unknown_package.files.erase(unknown_package.files.begin());
	unknown_package.File("manifest.toml", manifest);
	mister::native::OpenedCorePackage unknown_inspection;
	assert(mister::native::OpenCorePackage(unknown_package.path, "",
		&unknown_inspection).ok());
	std::unique_ptr<mister::AdmittedCorePackage> admitted;
	assert(fixture.hardware.AdmitCorePackage(unknown_package.path,
		unknown_inspection.package_id, &admitted).code ==
		mister::ErrorCode::unknown_system);
	assert(!admitted);
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

void TestRunningGameInputIsRetiredBeforePackageProgramming()
{
	std::vector<std::string> driver_events;
	RecordingDriver gp(driver_events);
	IntegratedFixture fixture(&gp);
	fixture.Start();
	assert(fixture.runtime.LaunchGame(fixture.Request()).ok());
	assert(fixture.native.input.HasActiveCallback());
	fixture.native.events.clear();
	TempDirectory package;
	PopulateFesGpPackage(&package);
	fixture.native.fpga.on_program = [&] {
		if (fixture.native.fpga.calls != 3) return;
		assert(Find(fixture.native.events, "input.stop") <
			Find(fixture.native.events, "video.quiesce"));
		assert(Find(fixture.native.events, "input.final-neutral") <
			Find(fixture.native.events, "video.quiesce"));
		assert(!fixture.native.input.HasActiveCallback());
	};
	const std::string package_id =
		"b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0";
	assert(fixture.runtime.LoadCore(package.path, package_id).ok());
	assert(fixture.native.input.stop_calls == 1);
	assert(fixture.native.input.HasActiveCallback());
	assert(fixture.runtime.status().state == mister::State::running_development);

	fixture.native.fpga.on_program = {};
	assert(fixture.runtime.Stop().ok());
	assert(fixture.runtime.LaunchGame(fixture.Request()).ok());
	assert(fixture.native.input.open_calls == 3);
	assert(fixture.native.input.start_calls == 3);
	assert(fixture.native.input.generations ==
		std::vector<std::uint64_t>({1, 2, 3}));
	assert(fixture.native.input.HasActiveCallback());
}

void TestPackageReplacementInputStopFailureUsesOneMenuRecovery()
{
	std::vector<std::string> driver_events;
	RecordingDriver gp(driver_events);
	IntegratedFixture fixture(&gp);
	fixture.Start();
	assert(fixture.runtime.LaunchGame(fixture.Request()).ok());
	fixture.native.input.stop_error = {
		mister::ErrorCode::io_failed, "injected replacement input stop failure"};
	TempDirectory package;
	PopulateFesGpPackage(&package);
	const mister::Error error = fixture.runtime.LoadCore(package.path,
		"b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0");
	assert(error.code == mister::ErrorCode::io_failed);
	assert(error.message == "injected replacement input stop failure");
	assert(fixture.runtime.status().state == mister::State::idle);
	assert(fixture.native.input.stop_calls == 1);
	assert(!fixture.native.input.HasActiveCallback());
	assert(fixture.native.fpga.calls == 3);
	assert(driver_events.empty());
	assert(fixture.native.fpga.profiles.back() ==
		mister::native::ProgrammingProfile::mister_v1);
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
			mister::native::ProgrammingProfile::mister_v1);
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
	assert(fixture.native.mmio.writes.size() == 1);
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

void TestEveryCoreTransitionQuiescesHdmiBeforeFpgaProgramming()
{
	Fixture launch;
	assert(launch.hardware.Launch(launch.MegaDriveLaunch(), 1).error.ok());
	assert(Count(launch.events, "video.quiesce") == 1);
	assert(Find(launch.events, "video.quiesce") <
		Find(launch.events, "fpga.program"));
	assert(launch.i2c.deadlines[0] == 10100);
	assert(launch.i2c.deadlines[1] == 10100);

	Fixture idle;
	assert(idle.hardware.LoadIdle().error.ok());
	assert(Count(idle.events, "idle.video.quiesce") == 1);
	assert(Find(idle.events, "idle.video.quiesce") <
		Find(idle.events, "fpga.program"));
	assert(idle.idle_video.quiesce_deadlines ==
		std::vector<std::uint64_t>({10100}));
}

void TestQuiesceFailureStopsBeforeFpgaMutationAndClosesLaunchInput()
{
	Fixture launch;
	launch.i2c.select_error = {
		mister::ErrorCode::io_failed, "injected HDMI quiesce failure"};
	const mister::HardwareResult launch_result = launch.hardware.Launch(
		launch.MegaDriveLaunch(), 1);
	assert(launch_result.error.code == mister::ErrorCode::io_failed);
	assert(launch_result.error.message == "injected HDMI quiesce failure");
	assert(!launch_result.mutation_attempted);
	assert(launch.fpga.calls == 0);
	assert(launch.input.stop_calls == 1);

	Fixture write_failure;
	write_failure.i2c.fail_event = "video.quiesce";
	const mister::HardwareResult write_result = write_failure.hardware.Launch(
		write_failure.MegaDriveLaunch(), 1);
	assert(write_result.error.code == mister::ErrorCode::io_failed);
	assert(write_result.mutation_attempted);
	assert(write_failure.fpga.calls == 0);
	assert(write_failure.input.stop_calls == 1);

	Fixture idle;
	idle.idle_video.quiesce_result.error = {
		mister::ErrorCode::io_failed, "injected idle HDMI quiesce failure"};
	idle.idle_video.quiesce_result.mutation_attempted = false;
	const mister::HardwareResult idle_result = idle.hardware.LoadIdle();
	assert(idle_result.error.code == mister::ErrorCode::io_failed);
	assert(idle_result.error.message == "injected idle HDMI quiesce failure");
	assert(!idle_result.mutation_attempted);
	assert(idle.fpga.calls == 0);
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
		"video.quiesce", "program", "core.sync", "core.reset.assert",
		"core.probe:MegaDrive",
		"core.status.initial", "core.media.select:1",
		"core.media.extension:.bin", "core.media.enable",
		"core.media.data:all bytes once", "core.media.complete",
		"video.adv.initialize", "video.timing:menu_720p60",
		"video.adv.mode", "core.buttons.neutral", "video.link.ready", "audio.volume:0",
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
		const std::string expected_phase =
			phase == "video.quiesce" ? "quiesce" :
			phase == "program" ? "programming" :
			phase.find("video.") == 0 || phase == "core.buttons.neutral" ||
				phase == "audio.volume:0" ? "video" :
			phase.find("input.") == 0 ? "input" : "transport";
		assert(result.phase == expected_phase);
		assert(fixture.runtime.status().state == mister::State::idle);
		assert(Count(fixture.native.fpga.programmed, "idle.rbf") == 2);
		assert(Count(fixture.native.fpga.programmed, "megadrive.rbf") ==
			(phase == "video.quiesce" ? 0 : 1));
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
		"idle.video.quiesce",
		"fpga.program",
		"idle.video:MENU",
		"runtime.idle",
	}));
	fixture.native.events.clear();
	std::vector<std::string> second = kSuccessfulLaunch;
	second[Find(second, "input.start:1")] = "input.start:2";
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

void TestDevelopmentLoadsMiSterRbfInExactOrderAndLeavesHdmiDown()
{
	Fixture fixture;
	fixture.i2c.on_quiesce = [&fixture] { fixture.clock.now_ = 200; };
	fixture.fpga.on_program = [&fixture] { fixture.clock.now_ = 300; };
	const mister::HardwareResult result =
		fixture.hardware.LoadDevelopmentRBF(fixture.rbf);
	assert(result.error.ok());
	assert(result.mutation_attempted);
	assert(result.observed_core == "MegaDrive");
	assert(fixture.events == std::vector<std::string>({
		"artifact.open:megadrive.rbf",
		"video.quiesce",
		"fpga.program",
		"core.sync",
		"core.probe:MegaDrive",
	}));
	assert(fixture.i2c.deadlines ==
		std::vector<std::uint64_t>({10100, 10100}));
	assert(fixture.fpga.deadlines ==
		std::vector<std::uint64_t>({30200}));
	assert(fixture.spi.deadlines ==
		std::vector<std::uint64_t>({10300, 10300}));
	assert(fixture.idle_video.calls == 0);
	assert(fixture.input.open_calls == 0);
	assert(fixture.input.neutral_deadlines.empty());
	assert(fixture.input.start_calls == 0);
	assert(fixture.input.stop_calls == 0);
}

void TestDevelopmentFailuresReportExactMutationBoundary()
{
	Fixture preflight;
	preflight.opener.fail_call = 1;
	const mister::HardwareResult preflight_result =
		preflight.hardware.LoadDevelopmentRBF(preflight.rbf);
	assert(preflight_result.error.code == mister::ErrorCode::io_failed);
	assert(!preflight_result.mutation_attempted);
	assert(preflight.i2c.deadlines.empty());
	assert(preflight.fpga.calls == 0);
	assert(preflight.spi.deadlines.empty());

	Fixture deadline;
	deadline.clock.now_ = std::numeric_limits<std::uint64_t>::max();
	const mister::HardwareResult deadline_result =
		deadline.hardware.LoadDevelopmentRBF(deadline.rbf);
	assert(deadline_result.error.code == mister::ErrorCode::io_failed);
	assert(deadline_result.error.message == "deadline exceeded");
	assert(!deadline_result.mutation_attempted);
	assert(deadline.i2c.deadlines.empty());
	assert(deadline.fpga.calls == 0);
	assert(deadline.spi.deadlines.empty());

	Fixture select;
	select.i2c.select_error = {
		mister::ErrorCode::io_failed, "injected HDMI select failure"};
	const mister::HardwareResult select_result =
		select.hardware.LoadDevelopmentRBF(select.rbf);
	assert(select_result.error.code == mister::ErrorCode::io_failed);
	assert(select_result.error.message == "injected HDMI select failure");
	assert(!select_result.mutation_attempted);
	assert(select.fpga.calls == 0);
	assert(select.spi.deadlines.empty());

	Fixture write;
	write.i2c.fail_event = "video.quiesce";
	const mister::HardwareResult write_result =
		write.hardware.LoadDevelopmentRBF(write.rbf);
	assert(write_result.error.code == mister::ErrorCode::io_failed);
	assert(write_result.mutation_attempted);
	assert(write.fpga.calls == 0);
	assert(write.spi.deadlines.empty());

	Fixture program;
	program.fpga.result = {
		{mister::ErrorCode::program_failed, "injected program failure"}, false};
	const mister::HardwareResult program_result =
		program.hardware.LoadDevelopmentRBF(program.rbf);
	assert(program_result.error.code == mister::ErrorCode::program_failed);
	assert(program_result.mutation_attempted);
	assert(program.spi.deadlines.empty());

	Fixture synchronize;
	synchronize.spi.sync_error = {
		mister::ErrorCode::io_failed, "injected synchronization failure"};
	const mister::HardwareResult synchronize_result =
		synchronize.hardware.LoadDevelopmentRBF(synchronize.rbf);
	assert(synchronize_result.error.code == mister::ErrorCode::io_failed);
	assert(synchronize_result.error.message == "injected synchronization failure");
	assert(synchronize_result.mutation_attempted);
	assert(synchronize.events.back() == "core.sync");

	Fixture probe;
	probe.spi.probe_error = {
		mister::ErrorCode::io_failed, "injected observation failure"};
	const mister::HardwareResult probe_result =
		probe.hardware.LoadDevelopmentRBF(probe.rbf);
	assert(probe_result.error.code == mister::ErrorCode::io_failed);
	assert(probe_result.error.message == "injected observation failure");
	assert(probe_result.mutation_attempted);
	assert(probe_result.observed_core.empty());
	assert(probe.events.back() == "core.probe:MegaDrive");
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
	for (std::uint64_t deadline : fixture.spi.deadlines)
		assert(deadline == 10100 || deadline == 120100);
	assert(fixture.idle_video.calls == 0);
}

void TestMediaTransferGetsDedicatedDeadline()
{
	Fixture fixture;
	assert(fixture.hardware.Launch(fixture.MegaDriveLaunch(), 1).error.ok());
	assert(fixture.spi.deadlines.size() > 4);
	for (std::size_t index = 0; index < 4; ++index)
		assert(fixture.spi.deadlines[index] == 10100);
	std::uint64_t largest = 0;
	for (std::uint64_t deadline : fixture.spi.deadlines)
		largest = std::max(largest, deadline);
	assert(largest == 120100);
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

void TestPreflightAndProgramFailuresIncludeQuiesceMutation()
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
	assert(before_result.mutation_attempted);
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
	assert(mismatch_result.error.phase == "identity");
	assert(mismatch_result.error.expected == "MegaDrive");
	assert(mismatch_result.error.observed == "OTHER");
	assert(mismatch_result.mutation_attempted);
	assert(mismatch_result.observed_core == "OTHER");
	Fixture io;
	io.spi.probe_error = {mister::ErrorCode::io_failed, "probe"};
	const auto io_result = io.hardware.Launch(io.Launch(), 1);
	assert(io_result.error.code == mister::ErrorCode::io_failed);
	assert(io_result.error.phase == "transport");
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

void TestSnesProductionTransformPreflightAndLifecycle()
{
	mister::Launch request;
	request.system = "snes";
	request.rbf = "/usr/share/mister-runtime/cores/snes.rbf";
	request.media.push_back({"cartridge", "/tmp/game.sfc"});
	mister::PreparedLaunch prepared;
	const auto& production = mister::ProductionProfiles();
	assert(production.Prepare(request, &prepared).ok());
	assert(prepared.expected_core == "SNES" && prepared.media.size() == 1);
	assert(prepared.media[0].index == 1 && prepared.media[0].transform == mister::MediaTransform::snes_cartridge);
	assert(prepared.input.c == 0 && prepared.input.x == 0x40 && prepared.input.start == 0x800);
	request.save_path = "/saves/test.srm";
	assert(production.Prepare(request, &prepared).ok() && prepared.save_path == request.save_path);
	request.save_path = "relative.srm";
	assert(!production.Prepare(request, &prepared).ok());
	request.save_path.clear();

	Fixture fixture;
	std::string bytes(32768, 0);
	bytes[0] = 0x78;
	bytes[0x7fd5] = 0x20; bytes[0x7fd7] = 5;
	bytes[0x7fdc] = static_cast<char>(0xcb); bytes[0x7fdd] = static_cast<char>(0xed);
	bytes[0x7fde] = 0x34; bytes[0x7fdf] = 0x12; bytes[0x7ffd] = static_cast<char>(0x80);
	const std::string rom = fixture.temporary.File("snes.bin", bytes);
	const std::string rbf = fixture.temporary.File("snes.rbf", "fixture");
	mister::Profile profile;
	profile.system = prepared.system; profile.expected_core = prepared.expected_core;
	profile.rbf = rbf; profile.core = prepared.core; profile.input = prepared.input;
	profile.media.push_back({"cartridge", 1, true, {".bin"}, 0x400200, mister::MediaTransform::snes_cartridge});
	mister::Profiles profiles;
	assert(profiles.Add(profile).ok());
	mister::Runtime runtime(fixture.hardware, profiles, fixture.log);
	assert(runtime.Start().ok());
	fixture.spi.observed_core = "SNES";
	request.rbf = rbf;
	request.media[0].path = fixture.rom; // Too short, before programming.
	const int calls = fixture.fpga.calls;
	assert(runtime.LaunchGame(request).code == mister::ErrorCode::invalid_request);
	assert(fixture.fpga.calls == calls);
	assert(runtime.status().state == mister::State::idle);
	request.media[0].path = rom;
	for (int cycle = 0; cycle < 2; ++cycle) {
		fixture.events.clear();
		assert(runtime.LaunchGame(request).ok());
		assert(runtime.status().system == "snes" && runtime.status().core == "SNES");
		assert(Find(fixture.events, "core.media.select:1") < Find(fixture.events, "audio.volume:0"));
		assert(Count(fixture.events, "core.media.data:all bytes once") == 9);
		assert(runtime.Stop().ok());
		assert(runtime.status().state == mister::State::idle);
	}
	fixture.spi.fail_event = "core.media.data:all bytes once";
	assert(runtime.LaunchGame(request).code == mister::ErrorCode::io_failed);
	assert(runtime.status().state == mister::State::idle);
}

void TestNativeSaveStopAndWriteRetry()
{
	Fixture f;
	mister_test::SnesSaveSpi backup;
	backup.ram.assign(2048, 0xff);
	f.spi.save_spi = &backup;
	f.spi.observed_core = "SNES";
	std::string bytes(32768, 0);
	bytes[0] = 0x78; bytes[0x7fd5] = 0x20; bytes[0x7fd6] = 2;
	bytes[0x7fd7] = 5; bytes[0x7fd8] = 1;
	bytes[0x7fdc] = char(0xcb); bytes[0x7fdd] = char(0xed);
	bytes[0x7fde] = 0x34; bytes[0x7fdf] = 0x12; bytes[0x7ffd] = char(0x80);
	auto launch = f.MegaDriveLaunch();
	launch.system = "snes"; launch.expected_core = "SNES";
	launch.core = {1, 1, 0, mister::FileWireFormat::little_endian_byte_pairs};
	launch.media = {{1, f.temporary.File("battery.sfc", bytes), 0x400200, mister::MediaTransform::snes_cartridge}};
	launch.save_path = f.temporary.path + "/battery.srm";
	const std::string rejected = f.temporary.File("wrong.srm", "truncated");
	const auto destination = launch.save_path;
	launch.save_path = rejected;
	const int before = f.fpga.calls;
	assert(!f.hardware.Launch(launch, 1).error.ok());
	assert(f.fpga.calls == before);
	launch.save_path = destination;
	assert(f.hardware.Launch(launch, 1).error.ok());
	assert(backup.mounted && backup.image_size == 0);
	assert(access(launch.save_path.c_str(), F_OK) != 0);
	backup.ram[0] = 0x42;
	// Force atomic rename failure after the snapshot. Resuming play must discard
	// those stale bytes so the retry captures current RAM.
	assert(mkdir(launch.save_path.c_str(), 0700) == 0);
	assert(f.hardware.FlushSave().code == mister::ErrorCode::save_failed);
	assert(backup.snapshots == 1);
	assert(Find(f.events, "input.stop") < Find(f.events, "save.snapshot"));
	const int programs = f.fpga.calls;
	const int input_opens = f.input.open_calls;
	const int input_starts = f.input.start_calls;
	const mister::native::ButtonWriter retired_writer = f.input.writers.back();
	backup.ram[0] = 0x99;
	assert(f.hardware.RestoreInput(1).ok());
	assert(f.input.open_calls == input_opens + 1 &&
		f.input.start_calls == input_starts + 1);
	assert(f.input.generations.back() == 1);
	const std::size_t requests_before_retired = f.spi.requests.size();
	assert(retired_writer(0x10, 998).ok());
	assert(f.spi.requests.size() == requests_before_retired);
	assert(f.input.Deliver(0x20, 999).ok());
	assert(f.spi.requests.size() == requests_before_retired + 1);
	assert((f.spi.requests.back() ==
		std::vector<std::uint16_t>{0x0002, 0x0020}));
	assert(rmdir(launch.save_path.c_str()) == 0);
	assert(f.hardware.FlushSave().ok());
	f.temporary.files.push_back(launch.save_path);
	assert(backup.snapshots == 2 && f.fpga.calls == programs);
	mister::native::SaveFile saved;
	assert(saved.Prepare(launch.save_path, 2048).ok() && saved.bytes()[0] == 0x99);
	assert(f.hardware.LoadIdle().error.ok());
	assert(f.hardware.FlushSave().ok() && backup.snapshots == 2);
	backup = mister_test::SnesSaveSpi{};
	backup.ram.assign(2048, 0xff);
	f.events.clear();
	assert(f.hardware.Launch(launch, 2).error.ok());
	assert(backup.ram[0] == 0x99);
	assert(Find(f.events, "save.restore") < Find(f.events, "input.start:2"));
	// Generic fault/failed-launch cleanup never publishes SRAM.
	backup.ram[0] = 0x77;
	assert(f.hardware.LoadIdle().error.ok());
	assert(backup.snapshots == 0);
	mister::native::SaveFile intact;
	assert(intact.Prepare(launch.save_path, 2048).ok() && intact.bytes()[0] == 0x99);
	backup = mister_test::SnesSaveSpi{};
	backup.ram.assign(2048, 0xff);
	f.input.start_error = {mister::ErrorCode::io_failed, "input start failed"};
	assert(!f.hardware.Launch(launch, 3).error.ok());
	assert(f.hardware.LoadIdle().error.ok());
	assert(f.hardware.FlushSave().ok() && backup.snapshots == 0);
}

void TestPongProductionProfileAndRomlessLifecycle()
{
	mister::Launch request;
	request.system = "pong";
	request.rbf = "/usr/share/mister-runtime/cores/pong.rbf";
	mister::PreparedLaunch prepared;
	const mister::Profiles& production = mister::ProductionProfiles();
	assert(production.Prepare(request, &prepared).ok());
	assert(prepared.expected_core == "Pong" && prepared.media.empty());
	assert(prepared.core.reset_assert_word == 1 && prepared.core.initial_status_word == 1);
	assert(prepared.core.reset_release_word == 0);
	assert(prepared.input.player_command == 2 && prepared.input.up == 8);
	assert(prepared.input.down == 4 && prepared.input.start == 0x80);
	request.media.push_back({"cartridge", "/tmp/unused.bin"});
	assert(production.Prepare(request, &prepared).code == mister::ErrorCode::invalid_request);
	request.media.clear();
	request.rbf = "/tmp/unowned.rbf";
	assert(production.Prepare(request, &prepared).code == mister::ErrorCode::invalid_request);
	request.rbf = "/usr/share/mister-runtime/cores/pong.rbf";
	assert(production.Prepare(request, &prepared).ok());

	Fixture fixture;
	const std::string pong = fixture.temporary.File("pong.rbf", "software-fixture");
	mister::Profile profile;
	profile.system = prepared.system;
	profile.expected_core = prepared.expected_core;
	profile.rbf = pong; // Only the artifact location differs from production.
	profile.core = prepared.core;
	profile.input = prepared.input;
	mister::Profiles profiles;
	assert(profiles.Add(profile).ok());
	mister::Runtime runtime(fixture.hardware, profiles, fixture.log);
	assert(runtime.Start().ok());
	fixture.spi.observed_core = "Pong";
	request.rbf = pong;
	const int before = fixture.fpga.calls;
	request.media.push_back({"cartridge", fixture.rom});
	assert(runtime.LaunchGame(request).code == mister::ErrorCode::invalid_request);
	assert(fixture.fpga.calls == before);
	request.media.clear();
	request.rbf = fixture.rbf;
	assert(runtime.LaunchGame(request).code == mister::ErrorCode::invalid_request);
	assert(fixture.fpga.calls == before);
	request.rbf = pong;
	for (int cycle = 0; cycle < 2; ++cycle) {
		fixture.events.clear();
		assert(runtime.LaunchGame(request).ok());
		assert(runtime.status().state == mister::State::running_game);
		assert(runtime.status().system == "pong" && runtime.status().core == "Pong");
		for (const std::string& event : fixture.events)
			assert(event.find("core.media.") != 0);
		const std::vector<std::string> ordered = {"artifact.open:pong.rbf", "video.quiesce",
			"fpga.program", "core.sync", "core.reset.assert", "core.probe:Pong",
			"core.status.initial", "video.adv.initialize", "audio.volume:0", "input.neutral",
			"core.reset.release", "input.start:" + std::to_string(cycle + 1), "runtime.running_game"};
		std::size_t previous = 0;
		for (const std::string& event : ordered) {
			const std::size_t index = Find(fixture.events, event);
			assert(index < fixture.events.size() && index >= previous);
			previous = index;
		}
		assert(runtime.Stop().ok());
		assert(runtime.status().state == mister::State::idle);
	}
}

void TestNesProductionProfileAndPreflight()
{
	mister::Launch request;
	request.system = "nes";
	request.rbf = "/usr/share/mister-runtime/cores/nes.rbf";
	request.media.push_back({"cartridge", "/tmp/game.nes"});
	mister::PreparedLaunch prepared;
	const mister::Profiles& production = mister::ProductionProfiles();
	assert(production.Prepare(request, &prepared).ok());
	assert(prepared.expected_core == "NES" && prepared.media.size() == 1);
	assert(prepared.media[0].index == 0x40 &&
		prepared.media[0].transform == mister::MediaTransform::nes_cartridge);
	assert(prepared.core.file_wire ==
		mister::FileWireFormat::little_endian_bytes);
	assert(prepared.input.a == 0x10 && prepared.input.b == 0x20 &&
		prepared.input.select == 0x400 && prepared.input.start == 0x800);

	Fixture fixture;
	const std::string nes_rbf = fixture.temporary.File("nes.rbf", "software-fixture");
	const std::string malformed = fixture.temporary.File("game.nes", "bad");
	const std::string valid = fixture.temporary.File("valid.nes", BasicNesRom());
	mister::Profile profile;
	profile.system = prepared.system;
	profile.expected_core = prepared.expected_core;
	profile.rbf = nes_rbf;
	profile.core = prepared.core;
	profile.input = prepared.input;
	profile.media.push_back({"cartridge", 0x40, true, {".nes"},
		32u * 1024u * 1024u, mister::MediaTransform::nes_cartridge});
	mister::Profiles profiles;
	assert(profiles.Add(profile).ok());
	mister::Runtime runtime(fixture.hardware, profiles, fixture.log);
	assert(runtime.Start().ok());
	fixture.spi.observed_core = "NES";
	request.rbf = nes_rbf;
	request.media[0].path = malformed;
	const int before = fixture.fpga.calls;
	assert(runtime.LaunchGame(request).code == mister::ErrorCode::invalid_request);
	assert(fixture.fpga.calls == before);
	request.media[0].path = valid;
	assert(runtime.LaunchGame(request).ok());
	assert(runtime.status().system == "nes" && runtime.status().core == "NES");
	assert(runtime.Stop().ok());
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
	launch.system = "sms";
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
	TempDirectory package;
	PopulateFesGpPackage(&package);
	std::unique_ptr<mister::AdmittedCorePackage> admitted;
	assert(hardware->AdmitCorePackage(package.path,
		"b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0",
		&admitted).code == mister::ErrorCode::invalid_package);
	assert(!admitted);
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
		"development-contained-v1", "fes-gp-v1", "mister-v1"}));
	assert(capabilities.abis.size() == 3);
	assert(capabilities.abis[0].id == "fes.simple-game");
	assert(capabilities.abis[1].id == "fes.simple-computer");

	Fixture unavailable;
	assert(unavailable.hardware.InspectCorePackage(package.path, id,
		&inspection).ok());
	assert(!inspection.compatible);
	assert(inspection.compatibility_error.code == mister::ErrorCode::unsupported_abi);
	assert(inspection.compatibility_error.phase == "compatibility");
	assert(unavailable.hardware.capabilities().programming_profiles ==
		std::vector<std::string>({"development-contained-v1", "mister-v1"}));
	assert(unavailable.hardware.InspectCorePackage(package.path,
		std::string(64, '0'), &inspection).code ==
		mister::ErrorCode::invalid_package);
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

int main()
{
	TestProductionFactoryForwardsCoreDataWithoutHardwareMutation();
	TestPersistentReplacementRefreshAndSaveFailureResume();
	TestPersistenceUnsafeResumeRetainsRecoveryOwnership();
	TestPersistenceContractAdmissionAndVolatileIsolation();
	TestNativeSaveStopAndWriteRetry();
	TestEveryCoreTransitionQuiescesHdmiBeforeFpgaProgramming();
	TestQuiesceFailureStopsBeforeFpgaMutationAndClosesLaunchInput();
	TestLaunchUsesExactCoreRecipeAndExplicitMediaFormatInOrder();
	TestFesGpPackageUsesSelectedDriverAndReturnsToMenuThroughThatDriver();
	TestProductionFesInputDisconnectAndGenerationRetirement();
	TestOnlyVerifiedFesGpMismatchIsQuiescedDuringIdleRecovery();
	TestUnknownAndContainedFabricReceiveNoMisterQuiesceWords();
	TestPackagedMisterIsExplicitDevelopmentAndValidatesDeclaredSystem();
	TestDriverIdentifyStartAndQuiesceFailuresHaveOneRecoveryDecision();
	TestPackageReplacementQuiesceFailureRespectsMutationBoundary();
	TestRunningGameInputIsRetiredBeforePackageProgramming();
	TestPackageReplacementInputStopFailureUsesOneMenuRecovery();
	TestMutatingProgramFailureForgetsOutgoingDriverBeforeRecovery();
	TestDriverFaultCallbackRejectsOldPackageGeneration();
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
	TestDevelopmentLoadsMiSterRbfInExactOrderAndLeavesHdmiDown();
	TestDevelopmentFailuresReportExactMutationBoundary();
	TestIdleRequiresVideo();
	TestIdlePreflightFailureCallsNeitherFpgaNorVideo();
	TestIdleFpgaFailureAfterQuiesceReportsHardwareMutation();
	TestIdleVideoFailureIsAttemptedIoFailureWithoutCleanup();
	TestLaunchNeverCallsIdleVideo();
	TestOneAbsoluteDeadlinePerNativeStage();
	TestMediaTransferGetsDedicatedDeadline();
	TestPostVideoCoreStageGetsFreshDeadline();
	TestPreflightAndProgramFailuresIncludeQuiesceMutation();
	TestEveryConcretePreflightRejectionPerformsZeroHardwareWork();
	TestProbeMismatchAndIoRetainObservedCoreAndMutation();
	TestNativeLoggingNamesPhasesAndConfirmedCore();
	TestSnesProductionTransformPreflightAndLifecycle();
	TestPongProductionProfileAndRomlessLifecycle();
	TestNesProductionProfileAndPreflight();
	TestProductionConstructionOwnsRealIdleHardware();
	TestUnavailableHardwareRemainsFailureOnly();
	TestInspectionReportsActualDriverCompatibilityWithoutMutation();
	TestActivationRechecksRetainedPayloadIdentityBeforeMutation();
	puts("native_hardware_test: 48 passed");
	return 0;
}
