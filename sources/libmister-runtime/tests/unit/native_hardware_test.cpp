// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "capture_log.hpp"
#include "de25_menu_operations.hpp"
#include "native/de25_core_driver.hpp"
#include "daemon/protocol.hpp"
#include "fake_input.hpp"
#include "fake_mmio.hpp"
#include "fes_computer_endpoint.hpp"
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
#include "native/menu_admission.hpp"
#include "native/menu_display.hpp"
#include "native/menu_underflow.hpp"
#include "native/linux/menu_window.hpp"
#include "native/core_data.hpp"
#include "native/media_data.hpp"
#include "native/input.hpp"
#include "native/linux/fpga_manager.hpp"
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
#include <cstring>
#include <chrono>
#include <condition_variable>
#include <functional>
#include <fstream>
#include <limits>
#include <memory>
#include <mutex>
#include <sstream>
#include <string>
#include <thread>
#include <utility>
#include <vector>
#if defined(__linux__)
#include <sys/syscall.h>
#include <atomic>
#include <cerrno>
static std::atomic<bool> fail_media_directory_sync{false};
extern "C" int fsync(int descriptor)
{
    struct stat state {};
    if (fail_media_directory_sync.load() && fstat(descriptor,&state)==0 &&
        S_ISDIR(state.st_mode) && fail_media_directory_sync.exchange(false)) {
        errno=EIO;return -1;
    }
    return static_cast<int>(syscall(SYS_fsync,descriptor));
}
#endif

void AssertComputerABIVersions(const mister::Capabilities& capabilities);

namespace {

std::vector<std::string> live_computer_status_responses;

void RecordLiveComputerStatus(const mister::Status& status)
{
	AssertComputerABIVersions(status.capabilities);
	const std::string response = mister::daemon::EncodeResponse(2, true, status, "fixture");
	live_computer_status_responses.push_back(response);
}

mister::CoreSettingsUpdate PaddleSpeed(std::uint16_t speed)
{
	mister::CoreSettingsUpdate update;
	update.paddle_speed = speed;
	return update;
}

mister::Error CreateKnownProductionHardware(mister::LogSink& log,
	std::unique_ptr<mister::Hardware>* hardware)
{
	const char model[] = "Terasic DE10-nano";
	const char compatible[] = "altr,socfpga-cyclone5\0altr,socfpga";
	return mister::CreateProductionHardware(log, hardware, "armv7",
		std::string(model, sizeof(model)), std::string(compatible, sizeof(compatible)),
		[] { return mister::Error{}; });
}

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
	bool LocalTime(std::tm* output) const override
	{
		if (has_local) *output = local;
		return has_local;
	}
	std::uint64_t now_;
	bool has_local = false;
	std::tm local {};
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
	bool BootHpsDdrLayout() override { return boot_hps_ddr_layout; }
	mister::Error ReleaseHpsDdrPorts(std::uint64_t deadline) override
	{
		events_.push_back("fpga.hps_ddr");
		hps_ddr_deadlines.push_back(deadline);
		++hps_ddr_calls;
		if (on_hps_ddr) on_hps_ddr();
		return hps_ddr_result;
	}
	std::vector<std::string>& events_;
	mister::Error hps_ddr_result;
	bool boot_hps_ddr_layout = true;
	std::vector<std::uint64_t> hps_ddr_deadlines;
	std::function<void()> on_hps_ddr;
	int hps_ddr_calls = 0;
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
	void SetButtonDeliveryEnabled(bool) override {}
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
	void SetButtonDeliveryEnabled(bool enabled) override
	{
		native_.SetButtonDeliveryEnabled(enabled);
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
		std::lock_guard<std::mutex> lock(mutex_);
		faults_.push_back(std::move(fault));
		condition_.notify_all();
	}
	bool WaitForFaults(std::size_t count)
	{
		std::unique_lock<std::mutex> lock(mutex_);
		return condition_.wait_for(lock, std::chrono::seconds(2),
			[this, count] { return faults_.size() >= count; });
	}
	std::vector<mister::HardwareFault> Faults()
	{
		std::lock_guard<std::mutex> lock(mutex_);
		return faults_;
	}

private:
	std::mutex mutex_;
	std::condition_variable condition_;
	std::vector<mister::HardwareFault> faults_;
};

class RecordingDriver final : public mister::native::CoreDriver {
public:
 mister::Error WaitVideoReady(std::uint64_t) override {
  events_.push_back("driver.video.ready");return {};
 }

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
	mister::Error SetKeyboardHid(const mister::KeyboardHidRows& rows, std::uint64_t) override
	{ keyboard = rows; ++keyboard_calls; return control_error; }
	mister::Error LoadMedia(const std::vector<std::uint8_t>& bytes, std::uint64_t) override
	{ media = bytes; ++media_calls; return control_error; }
	mister::Error LoadMediaLive(const std::vector<std::uint8_t>& bytes, std::uint64_t) override
	{ media = bytes; ++live_media_calls; return control_error; }
	mister::Error ClearMedia(std::uint64_t) override
	{ ++clear_calls; return control_error; }
	const std::vector<mister::MediaUnitCapability>& media_units() const override { return units; }
	mister::Error InsertMedia(std::uint8_t unit, const mister::native::ComputerMediaSnapshot& snapshot,
		mister::native::Clock& clock, std::uint64_t deadline, std::uint32_t) override
	{
		assert(unit == 0); ++unit_insert_calls; media.resize(snapshot.size());
		auto error = snapshot.Read(0, media.data(), media.size(), clock, deadline);
		if (!error.ok()) return error;
		if (control_error.ok()) units[0].state = mister::MediaUnitState::ready;
		return control_error;
	}
	mister::Error EjectMedia(std::uint8_t unit, std::uint64_t) override
	{
		assert(unit == 0); ++unit_eject_calls;
		if (control_error.ok()) units[0].state = mister::MediaUnitState::empty;
		return control_error;
	}
	std::vector<mister::MediaUnitCapability> units;
	unsigned unit_insert_calls = 0, unit_eject_calls = 0;
	mister::KeyboardHidRows keyboard{};
	std::vector<std::uint8_t> media;
	unsigned keyboard_calls = 0, media_calls = 0, live_media_calls = 0, clear_calls = 0;
	mister::Error control_error;
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

// fes-window grants over a memory copy of the window from the ATA disk offset.
class ScriptedDiskWindow final : public mister::native::MenuWindowOperations {
public:
	mister::Error Grant(std::uint64_t offset, std::uint64_t bytes, int* fd) override
	{
		const std::uint64_t base = mister::native::generated::FesComputerAtaDiskWindowOffset;
		if (offset < base || offset - base + bytes > region.size() || offset % 4096 || bytes % 4096)
			return {mister::ErrorCode::io_failed, "grant outside the scripted disk region"};
		grants.push_back({offset - base, bytes});
		*fd = static_cast<int>(grants.size());
		return {};
	}
	mister::Error Map(int fd, std::size_t, void** mapping) override
	{
		*mapping = region.data() + grants[static_cast<std::size_t>(fd - 1)].first;
		return {};
	}
	void Unmap(void*, std::size_t) override {}
	mister::Error Clean(int, std::uint64_t, std::uint32_t, std::uint32_t, std::uint32_t) override { return {}; }
	mister::Error Revoke(int) override { ++revokes; return {}; }
	mister::Error Duplicate(int, int*) override { return {mister::ErrorCode::io_failed, "unused"}; }
	void Close(int) override { ++closes; }
	std::vector<std::uint8_t> region;
	std::vector<std::pair<std::uint64_t, std::uint64_t>> grants;
	unsigned revokes = 0, closes = 0;
};

struct Fixture {
	explicit Fixture(mister::native::CoreDriver* gp_driver = nullptr,
		mister::native::IdleRecipe idle_recipe = mister::native::SplashIdle(),
        mister::native::MenuMemory* menu_memory = nullptr)
		: temporary(), events(), idle(temporary.File("idle.rbf", "idle")),
		  rbf(temporary.File("megadrive.rbf", "game")),
		  media_two(temporary.File("two.bin", "22")),
		  media_zero(temporary.File("zero.bin", "0")),
		  rom(temporary.File("sonic2.bin", "sonic")), opener(events), mmio(), fpga(events),
		  i2c(events), idle_video(events), clock(100),
		  log(events), game_video(i2c, clock, log,
			mister::native::Menu720p60Recipe()), input(events, clock),
		  input_identity({"FogCast Virtual Gamepad", 0x0006, 0x0000, 0x0001,
			0x0001}), sink(), disk_operations(), disk_window(disk_operations),
			hardware(opener, fpga, idle_video, game_video, input,
			input_identity, clock, log, idle, {30000, 10000, 10000},
			gp_driver, {"/tmp"}, std::move(idle_recipe), nullptr, menu_memory, &disk_window)
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
	ScriptedDiskWindow disk_operations;
	mister::native::DiskWindow disk_window;
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
	// Reading the error precedes the worker's fault callback. Observe delivery
	// before replacement retires the generation and suppresses pending faults.
	assert(sink.WaitForFaults(1));

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
	const auto disconnect_faults = sink.Faults();
	assert(disconnect_faults.size() == 1 && disconnect_faults[0].generation == 1);

	const std::size_t replacement_writes = mmio.writes.size();
	assert(input.writers[0](FesGpButtonUp, 1000).ok());
	input.callbacks[0](1,
		{mister::ErrorCode::io_failed, "retained old-generation callback"});
	assert(mmio.writes.size() == replacement_writes);
	const auto retired_faults = sink.Faults();
	assert(retired_faults.size() == 2 && retired_faults[1].generation == 1);

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
	assert(capabilities.abis.size() == 5);
	assert(capabilities.abis[0].id == "fes.application");
	assert(capabilities.abis[0].interfaces.size() == 10);
	assert(capabilities.abis[0].interfaces[1].id == "fes.expansion.coleco-bus");
	assert(capabilities.abis[0].interfaces[1].major == 1);
	assert(capabilities.abis[0].interfaces[1].minor == 0);
	assert(capabilities.abis[0].interfaces[2].id == "fes.firmware.blob");
	assert(capabilities.abis[0].interfaces[8].id == "fes.memory.hps-ddr");
	assert(capabilities.abis[0].interfaces[8].major == 1);
	assert(capabilities.abis[0].interfaces[8].minor == 0);
	assert(capabilities.abis[0].interfaces[9].id == "fes.video.fixed-720p60");
	const auto& computer = capabilities.abis[1];
	assert(computer.id == "fes.computer" && computer.major == 1 && computer.minor == 0);
	std::vector<std::string> computer_interfaces;
	for (const auto& interface : computer.interfaces) {
		// Spectrum tape is a PZX stream; snapshot interfaces are host metadata.
		assert(interface.major == (interface.id == "fes.media.spectrum-tape" ? 2 : 1) && interface.minor == 0);
		computer_interfaces.push_back(interface.id);
	}
	assert(computer_interfaces == std::vector<std::string>({"fes.audio.pcm-s16-stereo-48k",
		"fes.expansion.apple2-bus", "fes.expansion.atari-st-bus", "fes.expansion.c64-bus", "fes.expansion.spectrum-bus",
		"fes.gamepad.ports", "fes.keyboard.hid", "fes.media.apple2-floppy",
		"fes.media.atari-st-floppy", "fes.media.atari-st-floppy-geometry", "fes.media.atari-st-floppy-write", "fes.media.c64-disk", "fes.media.spectrum-disk", "fes.media.spectrum-snapshot", "fes.media.spectrum-snapshot-128", "fes.media.spectrum-snapshot-plus3", "fes.media.spectrum-tape", "fes.media.zx81-tape", "fes.media.zx81-tape-write",
		"fes.memory.hps-ddr", "fes.mouse.relative", "fes.video.fixed-720p60"}));
	// 1.1 is its own entry: the 1.0 interfaces and identity word 16's.
	const auto& computer_11 = capabilities.abis[2];
	assert(computer_11.id == "fes.computer" && computer_11.major == 1 && computer_11.minor == 1);
	std::vector<std::string> computer_11_interfaces;
	for (const auto& interface : computer_11.interfaces) computer_11_interfaces.push_back(interface.id);
	std::vector<std::string> expected_11 = computer_interfaces;
	expected_11.insert(expected_11.end(), {"fes.media.ata-disk", "fes.media.pcxt-floppy", "fes.pc386.machine", "fes.persistence.words",
		"fes.rtc.wall-clock"});
	std::sort(expected_11.begin(), expected_11.end());
	assert(computer_11_interfaces == expected_11);
	assert(capabilities.abis[3].id == "fes.simple-computer");
	assert(capabilities.abis[3].interfaces[0].id == "fes.audio.pcm-s16-stereo-48k");
	assert(capabilities.abis[4].id == "fes.simple-game");
	assert(capabilities.media_units.empty());

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

void TestPcxtDosPackageInspectionAcceptsRuntimeFloppyContract()
{
	std::vector<std::string> gp_events;
	RecordingDriver gp_driver(gp_events);
	Fixture available(&gp_driver);
	TempDirectory package;
	std::string manifest = ReadText(
		"tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
	ReplaceAll(&manifest, "id = \"fes.pong\"", "id = \"fes.pcxt-dos\"");
	ReplaceAll(&manifest, "id = \"fes.simple-game\"", "id = \"fes.computer\"");
	ReplaceAll(&manifest, "[abi]\nid = \"fes.computer\"\nmajor = 1\nminor = 0",
		"[abi]\nid = \"fes.computer\"\nmajor = 1\nminor = 1");
	ReplaceAll(&manifest, "id = \"fes.gamepad\"", "id = \"fes.media.pcxt-floppy\"");
	package.File("manifest.toml", manifest);
	package.File("core.rbf", ReadText(
		"tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
	mister::native::OpenedCorePackage opened;
	assert(mister::native::OpenCorePackage(package.path, "", &opened).ok());
	assert(opened.descriptor.core.id == "fes.pcxt-dos");
	assert(opened.descriptor.abi.id == "fes.computer" &&
		opened.descriptor.abi.major == 1 && opened.descriptor.abi.minor == 1);
	assert(opened.descriptor.interfaces.size() == 2);
	mister::CorePackageInspection inspection;
	assert(available.hardware.InspectCorePackage(package.path, opened.package_id,
		&inspection).ok());
	assert(inspection.compatible && inspection.compatibility_error.ok());
	assert(inspection.persistence_layout.id.empty());
	assert(inspection.descriptor.core.id == "fes.pcxt-dos");
	assert(available.fpga.programmed.empty() && gp_events.empty());
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

void TestSimpleComputerAudioIsEnabledOnlyAfterIdentity()
{
	std::vector<std::string> driver_events;
	RecordingDriver driver(driver_events);
	Fixture fixture(&driver);
	auto load = [&](bool audio, std::uint64_t generation) {
		TempDirectory package;
		std::string manifest = ReadText("tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
		ReplaceAll(&manifest, "fes.simple-game", "fes.simple-computer");
		ReplaceAll(&manifest, "fes.gamepad", "fes.keyboard");
		manifest += "\n[[interfaces]]\nid = \"fes.media.blob\"\nmajor = 1\nminor = 0\nrequired = true\n";
		if (audio)
			manifest += "\n[[interfaces]]\nid = \"fes.audio.pcm-s16-stereo-48k\"\nmajor = 1\nminor = 0\nrequired = true\n";
		package.File("manifest.toml", manifest);
		package.File("core.rbf", ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
		mister::native::OpenedCorePackage opened;
		assert(mister::native::OpenCorePackage(package.path, "", &opened).ok());
		std::unique_ptr<mister::AdmittedCorePackage> admitted;
		assert(fixture.hardware.AdmitCorePackage(package.path, opened.package_id, &admitted).ok());
		return fixture.hardware.LoadCore(std::move(admitted), generation);
	};
	auto has_phase = [&](const char* phase) {
		for (const auto& entry : fixture.log.records())
			if (entry.phase == phase && entry.error.ok()) return true;
		return false;
	};
	assert(load(true, 1).error.ok());
	assert(has_phase("audio_setup") && has_phase("audio_enable"));
	fixture.log.Clear();
	assert(load(false, 2).error.ok());
	assert(!has_phase("audio_setup") && !has_phase("audio_enable"));
	fixture.log.Clear();
	driver.identify_result.error = {mister::ErrorCode::core_mismatch, "injected identity failure"};
	assert(!load(true, 3).error.ok());
	assert(!has_phase("audio_setup") && !has_phase("audio_enable"));
}

void TestComputerControlsDispatchThroughSelectedCoreDriver()
{
	std::vector<std::string> events; RecordingDriver driver(events); Fixture fixture(&driver);
	mister::KeyboardHidRows rows{}; rows[0] = 32;
	assert(!fixture.hardware.SetKeyboardHid(rows).ok() && driver.keyboard_calls == 0);
	TempDirectory package; PopulateFesGpPackage(&package);
	mister::native::OpenedCorePackage opened;
	assert(mister::native::OpenCorePackage(package.path, "", &opened).ok());
	std::unique_ptr<mister::AdmittedCorePackage> admitted;
	assert(fixture.hardware.AdmitCorePackage(package.path, opened.package_id, &admitted).ok());
	assert(fixture.hardware.LoadCore(std::move(admitted), 1).error.ok());
	assert(fixture.hardware.SetKeyboardHid(rows).ok());
	assert(driver.keyboard_calls == 1 && driver.keyboard == rows);
	assert(fixture.hardware.LoadComputerMedia(fixture.media_two).ok());
	assert(driver.media_calls == 1 && driver.media == std::vector<std::uint8_t>({'2','2'}));
	assert(fixture.hardware.LoadComputerMediaLive(fixture.media_zero).ok());
	assert(driver.live_media_calls == 1 && driver.media == std::vector<std::uint8_t>({'0'}));
	assert(fixture.hardware.ClearComputerMedia().ok() && driver.clear_calls == 1);
	mister::MediaUnitCapability unit; unit.unit = 0;
	unit.interface = {"fes.media.zx81-tape", 1, 0};
	unit.min_bytes = 1; unit.max_bytes = 16384; unit.chunk_bytes = 512; unit.saveable = false;
	driver.units.push_back(unit);
	assert(fixture.hardware.capabilities().media_units.size() == 1);
	assert(!fixture.hardware.InsertComputerMedia(1, fixture.media_two, 2).ok());
	assert(!fixture.hardware.InsertComputerMedia(0, fixture.media_two, 1).ok());
	assert(driver.unit_insert_calls == 0);
	assert(fixture.hardware.InsertComputerMedia(0, fixture.media_two, 2).ok());
	assert(driver.unit_insert_calls == 1 && driver.media == std::vector<std::uint8_t>({'2','2'}));
	assert(fixture.hardware.capabilities().media_units[0].state == mister::MediaUnitState::ready);
	assert(!fixture.hardware.capabilities().media_units[0].saveable);
	assert(fixture.hardware.EjectComputerMedia(0).ok() && driver.unit_eject_calls == 1);
	assert(fixture.hardware.capabilities().media_units[0].state == mister::MediaUnitState::empty);
	driver.control_error = {mister::ErrorCode::io_failed, "injected selected-driver error"};
	assert(fixture.hardware.SetKeyboardHid(rows).code == mister::ErrorCode::io_failed);
	assert(fixture.hardware.ClearComputerMedia().code == mister::ErrorCode::io_failed);
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

void TestComputerSnapshotInterfacesBecomeActiveThroughRuntime()
{
	std::vector<std::string> events;
	RecordingDriver driver(events);
	IntegratedFixture fixture(&driver);
	fixture.Start();
	TempDirectory package;
	std::string manifest = ReadText("tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
	ReplaceAll(&manifest, "fes.simple-game", "fes.computer");
	ReplaceAll(&manifest, "fes.gamepad", "fes.keyboard.hid");
	for (const char* id : {"fes.media.spectrum-snapshot", "fes.media.spectrum-snapshot-128",
		"fes.media.spectrum-snapshot-plus3"})
		manifest += std::string("\n[[interfaces]]\nid = \"") + id +
			"\"\nmajor = 1\nminor = 0\nrequired = false\n";
	manifest += "\n[[interfaces]]\nid = \"fes.media.spectrum-snapshot-unknown\"\nmajor = 1\nminor = 0\nrequired = false\n";
	package.File("manifest.toml", manifest);
	package.File("core.rbf", ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
	mister::native::OpenedCorePackage opened;
	assert(mister::native::OpenCorePackage(package.path, "", &opened).ok());
	assert(fixture.runtime.LoadCore(package.path, opened.package_id).ok());
	const auto status = fixture.runtime.status();
	for (const char* id : {"fes.media.spectrum-snapshot", "fes.media.spectrum-snapshot-128",
		"fes.media.spectrum-snapshot-plus3"}) {
		bool active = false;
		for (const auto& interface : status.capabilities.active_interfaces)
			if (interface.id == id && interface.major == 1 && interface.minor == 0)
				active = true;
		assert(active);
	}
	for (const auto& interface : status.capabilities.active_interfaces)
		assert(interface.id != "fes.media.spectrum-snapshot-unknown");
	assert(fixture.runtime.Stop().ok());
}

// A video-only application, optionally requiring fes.memory.hps-ddr 1.0.
void PopulateHpsDdrApplication(TempDirectory* package, bool hps_ddr,
	mister::native::OpenedCorePackage* opened)
{
	std::string manifest = ReadText("tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
	ReplaceAll(&manifest, "fes.simple-game", "fes.application");
	const auto first = manifest.find("[[interfaces]]");
	const auto second = manifest.find("[[interfaces]]", first + 1);
	assert(first != std::string::npos && second != std::string::npos);
	manifest.erase(first, second - first); // remove gamepad; retain fixed video
	if (hps_ddr)
		manifest += "\n[[interfaces]]\nid = \"fes.memory.hps-ddr\"\nmajor = 1\nminor = 0\nrequired = true\n";
	package->File("manifest.toml", manifest);
	package->File("core.rbf", ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
	assert(mister::native::OpenCorePackage(package->path, "", opened).ok());
}

// The successful MSEL 9 readback sequence that the FPGA manager test scripts.
void ScriptFpgaProgramming(mister_test::FakeMmio* mmio)
{
	using namespace mister::native::generated;
	const auto mode = [](std::uint32_t value) { return (9u << 3) | value; };
	mmio->values[kFpgaControlAddress] = 0xa5a500c2u;
	mmio->values[kFpgaMonitorAddress] = 7;
	for (const std::uint32_t value : {4u, 0u, 1u, 1u, 2u, 2u, 3u, 3u, 4u, 4u})
		mmio->PushRead(kFpgaStatusAddress, mode(value));
	for (const std::uint32_t value : {1u, 3u, 7u})
		mmio->PushRead(kFpgaMonitorAddress, value);
	for (const std::uint32_t value : {1u, 0u, 1u, 1u, 1u})
		mmio->PushRead(kFpgaDclkStatusAddress, value);
}

void SetHpsDdrMirrors(mister_test::FakeMmio* mmio, bool matching)
{
	using namespace mister::native::generated;
	const std::pair<std::uint32_t, std::uint32_t> mirrors[] = {
		{kSdrCportWidthAddress, FesApplicationHpsDdrCfgPortWidth},
		{kSdrCportWmapAddress, FesApplicationHpsDdrCfgCportWfifoMap},
		{kSdrCportRmapAddress, FesApplicationHpsDdrCfgCportRfifoMap},
		{kSdrRfifoCmapAddress, FesApplicationHpsDdrCfgRfifoCportMap},
		{kSdrWfifoCmapAddress, FesApplicationHpsDdrCfgWfifoCportMap},
		{kSdrCportRdwrAddress, FesApplicationHpsDdrCfgCportType},
		{kSdrPortCfgAddress, FesApplicationHpsDdrCfgAxiMmSelect}};
	// A core without the fpga2sdram cell leaves every cfg_* input reading one.
	for (const auto& mirror : mirrors)
		mmio->values[mirror.first] = matching ? mirror.second : 0xffffffffu;
}

// Programming, the FES GP mailbox and the SDR registers share one bus, as in
// production, so the fake records the complete activation write order.
class MenuOperations final : public mister::native::MenuWindowOperations {
public:
 std::vector<std::uint32_t> pixels=std::vector<std::uint32_t>(8388608/4,0xdeadbeef);
 bool driver=true,fail_revoke=false;unsigned grants=0,revokes=0,cleans=0;
 struct Cleaned {std::uint64_t offset;std::uint32_t width,height,stride;};
 std::vector<Cleaned> cleaned;
 std::vector<std::uint64_t> granted;
 mister::Error Grant(std::uint64_t offset,std::uint64_t bytes,int* fd) override {
  assert(offset==0&&(bytes==8388608||bytes==83886080));
  if(!driver)return {mister::ErrorCode::io_failed,"no fes-window driver"};
  granted.push_back(bytes);*fd=int(100+ ++grants);return {};
 }
 mister::Error Map(int,std::size_t bytes,void** out) override {assert(bytes==8388608);*out=pixels.data();return {};}
 void Unmap(void*,std::size_t) override {}
 mister::Error Clean(int,std::uint64_t offset,std::uint32_t width,std::uint32_t height,std::uint32_t stride) override {
  cleaned.push_back({offset,width,height,stride});++cleans;return {};
 }
 mister::Error Revoke(int) override {
  ++revokes;return fail_revoke?mister::Error{mister::ErrorCode::io_failed,"menu revoke failed"}:mister::Error{};
 }
 mister::Error Duplicate(int fd,int* copy) override {*copy=fd+1000;return {};}
 void Close(int) override {}
 bool Granted() const {return grants>revokes;}
};
void TestMenuRevocationFailureBlocksIdleAndReplacement()
{
 using namespace mister::native;
 MenuOperations operations;MenuWindow memory(operations);
 assert(memory.Activate(false,false).ok());
 operations.fail_revoke=true;
 Fixture fixture(nullptr,SplashIdle(),&memory);
 const auto idle=fixture.hardware.LoadIdle();
 assert(!idle.error.ok()&&idle.error.phase=="quiesce");
 assert(fixture.fpga.calls==0&&fixture.opener.calls==0&&fixture.idle_video.calls==0);
 // Even a later successful-looking device cannot erase a failed revoke.
 operations.fail_revoke=false;
 const auto replacement=fixture.hardware.LoadContainedDevelopmentRBF(fixture.rbf,17);
 assert(!replacement.error.ok()&&replacement.error.phase=="quiesce");
 assert(fixture.fpga.calls==0&&operations.revokes==1);
 assert(!memory.Activate(true,false).ok()&&operations.grants==1);
}
// The client draws into the slot the hardware names, then commits it.
mister::Error PresentBackSlot(mister::native::NativeHardware& hardware,mister::MenuDisplayInfo* info)
{
 std::uint8_t slot=0;mister::Error error=hardware.MenuBackSlot(&slot);
 return error.ok()?hardware.PresentMenuSlot(slot,{},info):error;
}
// The (index, argument) of each MenuBlit exchange written since begin. An
// exchange writes its fields twice, settled and then toggled.
std::vector<std::pair<unsigned,unsigned>> BlitRequests(const mister_test::FakeMmio& mmio,std::size_t begin)
{
 using namespace mister::native::generated;
 std::vector<std::pair<unsigned,unsigned>> requests;bool second=false;
 for(std::size_t i=begin;i<mmio.writes.size();++i) {
  const auto value=mmio.writes[i].value;
  if(mmio.writes[i].offset!=kSpiGpoAddress||((value&FesGpOpcodeMask)>>24)!=FesApplicationOpcodeMenuBlit)continue;
  if((second=!second))continue;
  requests.push_back({unsigned((value&FesGpIndexMask)>>16),unsigned(value&FesGpArgumentMask)});
 }
 return requests;
}
struct MenuGpScript {
 mister_test::FakeMmio* mmio;
 bool toggle=false;
 // A session plane reports premultiplied RGBA and its picture rectangle.
 bool session=false;
 void Reply(std::uint16_t value=0){toggle=!toggle;PushFesGpResponse(mmio,toggle,value);}
 void Info(std::uint16_t state,std::uint32_t sequence,std::uint32_t underflows=0){
  const std::uint16_t words[]={1280,720,5120,16384,56,0,64,std::uint16_t(session?2:1),2,state,
   static_cast<std::uint16_t>(sequence),static_cast<std::uint16_t>(sequence>>16),
   static_cast<std::uint16_t>(underflows),static_cast<std::uint16_t>(underflows>>16)};
  for(auto word:words)Reply(word);
  if(session)for(std::uint16_t word:{312,90,656,540})Reply(word);
 }
 void Identity(std::uint16_t capabilities){
  using namespace mister::native::generated;
  auto words=FesGpIdentityWords();
  words[FesGpIdentityAbiTagIndex]=FesApplicationAbiTag;
  words[FesGpIdentityCapabilitiesIndex]=capabilities;
  for(auto word:words)Reply(word);
 }
 // A blitter menu also proves its blitter idle with nothing completed.
 void BringUp(bool blitter=false,bool read_blitter=true){
  Identity(blitter?1794:770);Info(8,0);Reply();Reply();Reply();Info(3,0);
  if(blitter&&read_blitter){Reply(0);Reply(0);}
 }
 // ReadBlit reads the completed count, then the state.
 void BlitState(std::uint16_t state,std::uint16_t completed){Reply(completed);Reply(state);}
 // Three quiesce exchanges, then BeginSession clears the GP toggle.
 void QuiesceRunningMenu(){Reply(0);Reply(8);Reply(0);toggle=false;}
 void Present(std::uint32_t sequence,std::uint32_t baseline,std::uint32_t final_underflows,bool pending_poll=false){
  Info(3,sequence,baseline);Reply();Reply();Reply();
  if(pending_poll)Info(7,sequence,baseline);
  Info(3,sequence+1,final_underflows);
 }
};
void TestSessionDisplayPreservesMachineOnCloseAndPlaneFailure()
{
 using namespace mister::native;using namespace mister::native::generated;
 TempDirectory package;
 auto manifest=ReadText("tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
 ReplaceAll(&manifest,"fes.simple-game","fes.simple-computer");ReplaceAll(&manifest,"fes.gamepad","fes.keyboard");
 for(const char* id:{"fes.media.blob","fes.memory.hps-ddr","fes.video.session-display","fes.fabric.video.display-plane"})
  manifest+="\n[[interfaces]]\nid = \""+std::string(id)+"\"\nmajor = 1\nminor = 0\nrequired = true\n";
 package.File("manifest.toml",manifest);package.File("core.rbf",ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
 OpenedCorePackage opened;assert(OpenCorePackage(package.path,"",&opened).ok());
 std::vector<std::string> events;RecordingOpener opener(events);RecordingFpga fpga(events);
 mister_test::FakeMmio mmio;FixedClock clock(100);FesGp gp(mmio,clock);FesGpCoreDriver driver(gp);
 MenuDisplayDriver display(gp,clock);MenuOperations operations;MenuWindow memory(operations);
 RecordingI2c i2c(events);RecordingVideo idle_video(events);LedgerLog log(events);
 FixedVideoBringup video(i2c,clock,log,Menu720p60Recipe());RecordingInput input(events,clock);
 TempDirectory splash;const auto idle=splash.File("idle.rbf","idle");
 const InputDeviceIdentity identity={"test",0,0,0,0};
 NativeHardware hardware(opener,fpga,idle_video,video,input,identity,clock,log,
  idle,{30000,10000,10000},&driver,{"/tmp"},SplashIdle(),&display,&memory);
 std::unique_ptr<mister::AdmittedCorePackage> admitted;
 fpga.boot_hps_ddr_layout=false;
 assert(!hardware.AdmitCorePackage(package.path,opened.package_id,&admitted).ok());
 fpga.boot_hps_ddr_layout=true;
 // The shared plane is advertised with session display, so FogCast sees every
 // required interface of the package as supported.
 bool advertised=false,plane=false;for(const auto& abi:hardware.capabilities().abis)for(const auto& interface:abi.interfaces)
  if(abi.id==FesSimpleComputerABIID) {
   advertised|=interface.id==FesSimpleComputerInterfaceVideoSessionDisplayID;
   plane|=interface.id==std::string(kDisplayPlaneInterfaceID)&&interface.major==1&&interface.minor==0;
  }
 assert(advertised&&plane);
 assert(hardware.AdmitCorePackage(package.path,opened.package_id,&admitted).ok());
 MenuGpScript script{&mmio};script.session=true;auto words=FesGpIdentityWords();
 words[FesGpIdentityAbiTagIndex]=FesSimpleComputerAbiTag;
 words[FesGpIdentityCapabilitiesIndex]=775;
 for(auto word:words)script.Reply(word);
 script.Info(8,0);script.Reply(); // configure
 for(unsigned row=0;row<8;++row){script.Reply();}
 script.Reply(); // execution release
 script.Info(9,0); // disabled, configured, drained
 auto loaded=hardware.LoadCore(std::move(admitted),17);
 if(!loaded.error.ok())fprintf(stderr,"session display activation: %s\n",loaded.error.message.c_str());
 assert(loaded.error.ok()&&fpga.calls==1&&fpga.hps_ddr_calls==1);
 assert(!hardware.menu_display().available&&hardware.menu_display().session&&hardware.menu_display().core_generation==17);
 assert(hardware.menu_display().package_id==opened.package_id);
 const auto begin=mmio.writes.size();
 const auto open_plane=[&] {
  for(unsigned row=0;row<8;++row){script.Reply();}
  script.Reply();script.Info(3,hardware.menu_display().displayed_sequence);
  assert(hardware.SetSessionDisplay(true,true).ok()&&hardware.menu_display().available);
  assert(hardware.menu_display().launcher_focus);
 };
 open_plane();
 const auto focused=mmio.writes.size();assert(hardware.SetComputerKeyboard(0).ok()&&mmio.writes.size()==focused);
 script.Present(0,0,0);mister::MenuDisplayInfo info;assert(PresentBackSlot(hardware,&info).ok());
 script.Reply();script.Info(9,1);assert(hardware.SetSessionDisplay(false,false).ok());
 assert(!hardware.menu_display().available&&!hardware.menu_display().launcher_focus&&fpga.calls==1);
 // A closed plane never holds launcher focus.
 assert(hardware.SetSessionDisplay(false,true).code==mister::ErrorCode::invalid_request);
 // A surround shows the plane while the machine keeps input: no neutral post,
 // and machine keys still reach the core.
 script.Reply();script.Info(3,1);
 assert(hardware.SetSessionDisplay(true,false).ok()&&hardware.menu_display().available);
 assert(!hardware.menu_display().launcher_focus);
 auto keys=mmio.writes.size();for(unsigned row=0;row<8;++row){script.Reply();}
 assert(hardware.SetComputerKeyboard(0).ok()&&mmio.writes.size()>keys);
 // Taking focus on the shown plane neutralizes keys without touching the plane.
 for(unsigned row=0;row<8;++row){script.Reply();}
 assert(hardware.SetSessionDisplay(true,true).ok()&&hardware.menu_display().launcher_focus&&operations.Granted());
 keys=mmio.writes.size();assert(hardware.SetComputerKeyboard(0).ok()&&mmio.writes.size()==keys);
 // Returning focus is local, and the plane stays shown.
 assert(hardware.SetSessionDisplay(true,false).ok()&&mmio.writes.size()==keys);
 assert(hardware.menu_display().available&&!hardware.menu_display().launcher_focus&&operations.Granted());
 script.Reply();script.Info(9,1);assert(hardware.SetSessionDisplay(false,false).ok());
 assert(!hardware.menu_display().available&&!operations.Granted());
 open_plane();
 // Completion with a bad underflow delta invokes only plane quiesce.
 script.Present(1,0,kMenuUnderflowPresentCap+1);script.Reply();script.Info(25,2,kMenuUnderflowPresentCap+1);
 assert(!PresentBackSlot(hardware,&info).ok());
 assert(!hardware.menu_display().available&&hardware.menu_display().error.message=="menu underflow exceeded the per-present cap");
 assert(fpga.calls==1&&hardware.menu_display().core_generation==17&&!operations.Granted());
 for(std::size_t i=begin;i<mmio.writes.size();++i)
  if(mmio.writes[i].offset==kSpiGpoAddress)assert(((mmio.writes[i].value>>24)&127)!=FesSimpleComputerOpcodeExecution);
 // A session plane can prove drained while its sticky reader fault remains.
 script.Reply();script.Info(25,2);assert(hardware.SetSessionDisplay(false,false).ok());
 for(unsigned row=0;row<8;++row){script.Reply();}
 assert(hardware.SetComputerKeyboard(0).ok());
 // Stop still drains first, then uses the ordinary lifecycle execution hold.
 script.Reply();script.Reply(25);script.Reply();
 assert(hardware.LoadIdle().error.ok()&&fpga.calls==2&&fpga.programmed.back()=="idle.rbf");
 assert(!hardware.menu_display().session);
}

// An application machine with controller ports (the Coleco native shell)
// carries the same session plane. Launcher focus releases every port instead
// of a keyboard, and buttons pressed while the UI has focus never reach the
// machine.
void TestApplicationSessionDisplayReleasesControllers()
{
 using namespace mister::native;using namespace mister::native::generated;
 TempDirectory package;
 auto manifest=ReadText("tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
 ReplaceAll(&manifest,"fes.simple-game","fes.application");ReplaceAll(&manifest,"\"fes.gamepad\"","\"fes.gamepad.ports\"");
 for(const char* id:{"fes.memory.hps-ddr","fes.video.session-display","fes.fabric.video.display-plane"})
  manifest+="\n[[interfaces]]\nid = \""+std::string(id)+"\"\nmajor = 1\nminor = 0\nrequired = true\n";
 package.File("manifest.toml",manifest);package.File("core.rbf",ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
 OpenedCorePackage opened;assert(OpenCorePackage(package.path,"",&opened).ok());
 std::vector<std::string> events;RecordingOpener opener(events);RecordingFpga fpga(events);
 mister_test::FakeMmio mmio;FixedClock clock(100);FesGp gp(mmio,clock);FesGpCoreDriver driver(gp);
 MenuDisplayDriver display(gp,clock);MenuOperations operations;MenuWindow memory(operations);
 RecordingI2c i2c(events);RecordingVideo idle_video(events);LedgerLog log(events);
 FixedVideoBringup video(i2c,clock,log,Menu720p60Recipe());RecordingInput input(events,clock);
 TempDirectory splash;const auto idle=splash.File("idle.rbf","idle");
 const InputDeviceIdentity identity={"test",0,0,0,0};
 NativeHardware hardware(opener,fpga,idle_video,video,input,identity,clock,log,
  idle,{30000,10000,10000},&driver,{"/tmp"},SplashIdle(),&display,&memory);
 fpga.boot_hps_ddr_layout=true;
 bool advertised=false,plane=false;for(const auto& abi:hardware.capabilities().abis)for(const auto& interface:abi.interfaces)
  if(abi.id==FesApplicationABIID) {
   advertised|=interface.id==FesApplicationInterfaceVideoSessionDisplayID;
   plane|=interface.id==std::string(kDisplayPlaneInterfaceID)&&interface.major==1&&interface.minor==0;
  }
 assert(advertised&&plane);
 std::unique_ptr<mister::AdmittedCorePackage> admitted;
 assert(hardware.AdmitCorePackage(package.path,opened.package_id,&admitted).ok());
 MenuGpScript script{&mmio};script.session=true;
 script.Identity(FesApplicationCapabilityVideoFixed720p60|FesApplicationCapabilityGamepadPorts|
  FesApplicationCapabilityMemoryHpsDdr|FesApplicationCapabilityVideoSessionDisplay);
 script.Info(8,0);script.Reply(); // configure
 script.Reply();script.Reply(); // both controller ports released
 script.Reply(); // execution release
 script.Info(9,0); // disabled, configured, drained
 auto loaded=hardware.LoadCore(std::move(admitted),17);
 if(!loaded.error.ok())fprintf(stderr,"application session display activation: %s\n",loaded.error.message.c_str());
 assert(loaded.error.ok()&&hardware.menu_display().session&&!hardware.menu_display().available);
 // A surround leaves the controllers with the machine.
 script.Reply();script.Info(3,0);
 assert(hardware.SetSessionDisplay(true,false).ok()&&hardware.menu_display().available);
 script.Reply();auto writes=mmio.writes.size();
 assert(hardware.SetController(0,0x20,0).ok()&&mmio.writes.size()>writes);
 // Focus releases both ports, then pressed buttons go nowhere.
 script.Reply();script.Reply();writes=mmio.writes.size();
 assert(hardware.SetSessionDisplay(true,true).ok()&&hardware.menu_display().launcher_focus);
 std::vector<std::uint32_t> released;
 for(std::size_t i=writes;i<mmio.writes.size();++i)
  if(mmio.writes[i].offset==kSpiGpoAddress&&(mmio.writes[i].value>>31))released.push_back(mmio.writes[i].value);
 assert(released.size()==2);
 for(std::size_t port=0;port<2;++port)
  assert(((released[port]>>24)&127)==FesApplicationOpcodeControllerButtons&&
   ((released[port]>>16)&255)==port&&(released[port]&65535)==0);
 writes=mmio.writes.size();
 assert(hardware.SetController(1,0x20,0).ok()&&mmio.writes.size()==writes);
 // Returning focus is local; the machine's controllers work again.
 assert(hardware.SetSessionDisplay(true,false).ok()&&mmio.writes.size()==writes);
 script.Reply();assert(hardware.SetController(1,0x20,0).ok()&&mmio.writes.size()>writes);
}

void TestApplicationSessionDisplayMutesSingleGamepad()
{
 using namespace mister::native;using namespace mister::native::generated;
 TempDirectory package;
 auto manifest=ReadText("tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
 ReplaceAll(&manifest,"fes.simple-game","fes.application");
 for(const char* id:{"fes.memory.hps-ddr","fes.video.session-display","fes.fabric.video.display-plane"})
  manifest+="\n[[interfaces]]\nid = \""+std::string(id)+"\"\nmajor = 1\nminor = 0\nrequired = true\n";
 package.File("manifest.toml",manifest);package.File("core.rbf",ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
 OpenedCorePackage opened;assert(OpenCorePackage(package.path,"",&opened).ok());
 std::vector<std::string> events;RecordingOpener opener(events);RecordingFpga fpga(events);
 mister_test::FakeMmio mmio;FixedClock clock(100);FesGp gp(mmio,clock);FesGpCoreDriver driver(gp);
 MenuDisplayDriver display(gp,clock);MenuOperations operations;MenuWindow memory(operations);
 RecordingI2c i2c(events);RecordingVideo idle_video(events);LedgerLog log(events);
 FixedVideoBringup video(i2c,clock,log,Menu720p60Recipe());
 mister_test::FakeInputDevice device;RetainingNativeInput input(device,clock);
 TempDirectory splash;const auto idle=splash.File("idle.rbf","idle");
 const InputDeviceIdentity identity={"test",0,0,0,0};
 NativeHardware hardware(opener,fpga,idle_video,video,input,identity,clock,log,
  idle,{30000,10000,10000},&driver,{"/tmp"},SplashIdle(),&display,&memory);
 fpga.boot_hps_ddr_layout=true;
 std::unique_ptr<mister::AdmittedCorePackage> admitted;
 assert(hardware.AdmitCorePackage(package.path,opened.package_id,&admitted).ok());
 MenuGpScript script{&mmio};script.session=true;
 script.Identity(FesApplicationCapabilityVideoFixed720p60|FesApplicationCapabilityGamepad|
  FesApplicationCapabilityMemoryHpsDdr|FesApplicationCapabilityVideoSessionDisplay);
 script.Info(8,0);script.Reply(); // configure
 script.Reply(); // input neutralization
 script.Reply(); // execution release
 script.Info(9,0); // disabled, configured, drained
 auto loaded=hardware.LoadCore(std::move(admitted),17);
 if(!loaded.error.ok())fprintf(stderr,"single-gamepad session activation: %s\n",loaded.error.message.c_str());
 assert(loaded.error.ok()&&hardware.menu_display().session&&!hardware.menu_display().available);
 assert(device.WaitUntilReading());

 std::size_t input_reads=0;
 auto push_report=[&](InputControl control,std::int32_t value) {
  device.Push({control,value});
  device.Push({InputControl::synchronize,0});
  device.Push({InputControl::horizontal,0});
  input_reads+=3;
  assert(device.WaitForReads(input_reads));
  // The worker's next blocking read proves Apply finished this report.
  assert(device.WaitUntilReading());
 };

 const auto button_arguments=[&](std::size_t begin) {
  std::vector<std::uint16_t> arguments;
  for(std::size_t i=begin;i<mmio.writes.size();++i) {
   const auto& write=mmio.writes[i];
   if(write.offset!=kSpiGpoAddress||!(write.value&FesGpRequestMask)||
    ((write.value&FesGpOpcodeMask)>>24)!=FesApplicationOpcodeButtons)continue;
   arguments.push_back(static_cast<std::uint16_t>(write.value&FesGpArgumentMask));
  }
  return arguments;
 };
 script.Reply();script.Info(3,0);
 assert(hardware.SetSessionDisplay(true,false).ok());
 auto writes=mmio.writes.size();script.Reply();
 assert(hardware.SetSessionDisplay(true,true).ok()&&hardware.menu_display().launcher_focus);
 auto arguments=button_arguments(writes);
 assert(arguments.size()==1&&arguments[0]==0);
 push_report(InputControl::a,1);
 assert(button_arguments(writes).size()==1); // A is held only for the launcher.

 assert(hardware.SetSessionDisplay(true,false).ok()&&!hardware.menu_display().launcher_focus);
 script.Reply();push_report(InputControl::b,1);
 arguments=button_arguments(writes);
 assert(arguments.size()==2&&arguments[0]==0&&arguments[1]==0x20);
 push_report(InputControl::a,0);
 arguments=button_arguments(writes);
 assert(arguments.size()==2&&arguments[1]==0x20);
 // NativeHardware joins the worker on teardown and sends its final neutral.
 script.Reply();
}

void TestApplicationSessionDisplaySuppressesOppositeDirections()
{
 using namespace mister::native;using namespace mister::native::generated;
 TempDirectory package;
 auto manifest=ReadText("tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
 ReplaceAll(&manifest,"fes.simple-game","fes.application");
 for(const char* id:{"fes.memory.hps-ddr","fes.video.session-display","fes.fabric.video.display-plane"})
  manifest+="\n[[interfaces]]\nid = \""+std::string(id)+"\"\nmajor = 1\nminor = 0\nrequired = true\n";
 package.File("manifest.toml",manifest);package.File("core.rbf",ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
 OpenedCorePackage opened;assert(OpenCorePackage(package.path,"",&opened).ok());
 std::vector<std::string> events;RecordingOpener opener(events);RecordingFpga fpga(events);
 mister_test::FakeMmio mmio;FixedClock clock(100);FesGp gp(mmio,clock);FesGpCoreDriver driver(gp);
 MenuDisplayDriver display(gp,clock);MenuOperations operations;MenuWindow memory(operations);
 RecordingI2c i2c(events);RecordingVideo idle_video(events);LedgerLog log(events);
 FixedVideoBringup video(i2c,clock,log,Menu720p60Recipe());
 mister_test::FakeInputDevice device;RetainingNativeInput input(device,clock);
 TempDirectory splash;const auto idle=splash.File("idle.rbf","idle");
 const InputDeviceIdentity identity={"test",0,0,0,0};
 NativeHardware hardware(opener,fpga,idle_video,video,input,identity,clock,log,
  idle,{30000,10000,10000},&driver,{"/tmp"},SplashIdle(),&display,&memory);
 fpga.boot_hps_ddr_layout=true;
 std::unique_ptr<mister::AdmittedCorePackage> admitted;
 assert(hardware.AdmitCorePackage(package.path,opened.package_id,&admitted).ok());
 MenuGpScript script{&mmio};script.session=true;
 script.Identity(FesApplicationCapabilityVideoFixed720p60|FesApplicationCapabilityGamepad|
  FesApplicationCapabilityMemoryHpsDdr|FesApplicationCapabilityVideoSessionDisplay);
 script.Info(8,0);script.Reply(); // configure
 script.Reply(); // input neutralization
 script.Reply(); // execution release
 script.Info(9,0); // disabled, configured, drained
 auto loaded=hardware.LoadCore(std::move(admitted),17);
 if(!loaded.error.ok())fprintf(stderr,"opposite-direction session activation: %s\n",loaded.error.message.c_str());
 assert(loaded.error.ok()&&hardware.menu_display().session&&!hardware.menu_display().available);
 assert(device.WaitUntilReading());

 std::size_t input_reads=0;
 auto push_report=[&](InputControl control,std::int32_t value) {
  device.Push({control,value});
  device.Push({InputControl::synchronize,0});
  device.Push({InputControl::horizontal,0});
  input_reads+=3;
  assert(device.WaitForReads(input_reads));
  // The worker's next blocking read proves Apply finished this report.
  assert(device.WaitUntilReading());
 };
 const auto button_arguments=[&](std::size_t begin) {
  std::vector<std::uint16_t> arguments;
  for(std::size_t i=begin;i<mmio.writes.size();++i) {
   const auto& write=mmio.writes[i];
   if(write.offset!=kSpiGpoAddress||!(write.value&FesGpRequestMask)||
    ((write.value&FesGpOpcodeMask)>>24)!=FesApplicationOpcodeButtons)continue;
   arguments.push_back(static_cast<std::uint16_t>(write.value&FesGpArgumentMask));
  }
  return arguments;
 };
 script.Reply();script.Info(3,0);
 assert(hardware.SetSessionDisplay(true,false).ok());
 const auto writes=mmio.writes.size();script.Reply();
 assert(hardware.SetSessionDisplay(true,true).ok()&&hardware.menu_display().launcher_focus);
 auto arguments=button_arguments(writes);
 assert(arguments.size()==1&&arguments[0]==0);

 push_report(InputControl::left,1);
 push_report(InputControl::right,1);
 assert(button_arguments(writes).size()==1); // Opposite held directions stay muted.
 assert(hardware.SetSessionDisplay(true,false).ok()&&!hardware.menu_display().launcher_focus);
 script.Reply(); // Used by a leaked write on the failing implementation, or the fresh press below.
 push_report(InputControl::right,0);
 arguments=button_arguments(writes);
 assert(arguments.size()==1&&arguments[0]==0); // Releasing one side cannot restore the other.
 push_report(InputControl::left,0);
 assert(button_arguments(writes).size()==1);
 push_report(InputControl::left,1);
 arguments=button_arguments(writes);
 assert(arguments.size()==2&&arguments[1]==FesGpButtonLeft);
 script.Reply(); // NativeHardware joins the worker on teardown and sends its final neutral.
}

// A session package built before the shared plane (ZX81 1.5.0) declares no
// display plane, so inspection and admission refuse it before programming.
// A package that declares the plane but whose bitstream still reports the
// idle XRGB format fails closed after programming, so the runtime returns to idle.
void TestSessionDisplayRejectsPlaneWithoutBlend()
{
 using namespace mister::native;using namespace mister::native::generated;
 auto session_package=[](TempDirectory* package,bool plane,OpenedCorePackage* opened) {
  auto manifest=ReadText("tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
  ReplaceAll(&manifest,"fes.simple-game","fes.simple-computer");ReplaceAll(&manifest,"fes.gamepad","fes.keyboard");
  for(const char* id:{"fes.media.blob","fes.memory.hps-ddr","fes.video.session-display","fes.fabric.video.display-plane"})
   if(plane||std::string(id)!=kDisplayPlaneInterfaceID)
    manifest+="\n[[interfaces]]\nid = \""+std::string(id)+"\"\nmajor = 1\nminor = 0\nrequired = true\n";
  package->File("manifest.toml",manifest);package->File("core.rbf",ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
  assert(OpenCorePackage(package->path,"",opened).ok());
 };
 TempDirectory opaque,package;OpenedCorePackage opaque_opened,opened;
 session_package(&opaque,false,&opaque_opened);session_package(&package,true,&opened);
 std::vector<std::string> events;RecordingOpener opener(events);RecordingFpga fpga(events);
 mister_test::FakeMmio mmio;FixedClock clock(100);FesGp gp(mmio,clock);FesGpCoreDriver driver(gp);
 MenuDisplayDriver display(gp,clock);MenuOperations operations;MenuWindow memory(operations);
 RecordingI2c i2c(events);RecordingVideo idle_video(events);LedgerLog log(events);
 FixedVideoBringup video(i2c,clock,log,Menu720p60Recipe());RecordingInput input(events,clock);
 TempDirectory splash;const auto idle=splash.File("idle.rbf","idle");
 const InputDeviceIdentity identity={"test",0,0,0,0};
 NativeHardware hardware(opener,fpga,idle_video,video,input,identity,clock,log,
  idle,{30000,10000,10000},&driver,{"/tmp"},SplashIdle(),&display,&memory);
 std::unique_ptr<mister::AdmittedCorePackage> admitted;
 mister::CorePackageInspection inspection;
 assert(hardware.InspectCorePackage(opaque.path,opaque_opened.package_id,&inspection).ok());
 assert(!inspection.compatible&&inspection.compatibility_error.code==mister::ErrorCode::unsupported_interface);
 const auto refused=hardware.AdmitCorePackage(opaque.path,opaque_opened.package_id,&admitted);
 assert(refused.code==mister::ErrorCode::unsupported_interface&&refused.phase=="compatibility");
 assert(!admitted&&fpga.calls==0);
 assert(hardware.AdmitCorePackage(package.path,opened.package_id,&admitted).ok());
 MenuGpScript script{&mmio};auto words=FesGpIdentityWords();
 words[FesGpIdentityAbiTagIndex]=FesSimpleComputerAbiTag;
 words[FesGpIdentityCapabilitiesIndex]=775;
 for(auto word:words)script.Reply(word);
 script.Info(8,0);script.Reply(); // configure
 for(unsigned row=0;row<8;++row){script.Reply();}
 script.Reply(); // execution release
 script.Info(9,0); // drained, but pixel format 1
 const auto loaded=hardware.LoadCore(std::move(admitted),17);
 assert(loaded.error.code==mister::ErrorCode::io_failed&&loaded.error.phase=="menu");
 assert(loaded.error.message=="session display must blend premultiplied RGBA");
 assert(loaded.mutation_attempted&&fpga.calls==1&&!hardware.menu_display().session);
}

// Kit software packing publishes these records so an image can refuse a
// runtime that would reject its menu. The union covers both explicit board
// contracts. DE10 keeps its accepted older minors; DE25 requires scanout1.1.
void TestMenuInterfaceRecordsMatchMenuAdmission()
{
 using namespace mister::native;
 std::vector<CoreInterface> listed;
 for(const char* record=kMenuInterfaceRecords;*record!='\0';record+=std::strlen(record)+1){
  const std::string text(record);
  const auto equals=text.find('='),dot=text.rfind('.');
  assert(equals!=std::string::npos&&equals>0&&dot!=std::string::npos&&dot>equals+1&&dot+1<text.size());
  listed.push_back({text.substr(0,equals),static_cast<std::uint16_t>(std::stoul(text.substr(equals+1,dot-equals-1))),
   static_cast<std::uint16_t>(std::stoul(text.substr(dot+1))),true});
 }
 assert(!listed.empty());
 const auto all=listed;
 listed.erase(std::remove_if(listed.begin(),listed.end(),[](const CoreInterface& i){
  return i.id=="fes.fabric.de25-scanout";
 }),listed.end());
 CoreDescriptor menu;
 menu.format=2;menu.core.id="fes.menu";
 menu.target.platform="de10_nano";menu.target.device="5CSEBA6U23I7";menu.target.programming_profile="fes-gp-v1";
 menu.abi={generated::FesApplicationABIID,generated::FesApplicationABIMajor,generated::FesApplicationABIMinor};
 menu.interfaces=listed;
 assert(CheckMenu(menu).ok());
 for(std::size_t index=0;index<listed.size();++index){
  for(std::uint16_t minor=0;minor<listed[index].minor;++minor){
   auto older=menu;older.interfaces[index].minor=minor;
   assert(CheckMenu(older).ok());
  }
  auto newer=menu;++newer.interfaces[index].minor;
  assert(!CheckMenu(newer).ok());
 }
 // The list is complete: an application interface CheckMenu admits beside
 // the listed ones must itself be listed, or an image whose menu declares
 // it would hold back a runtime that accepts it.
 for(const char* id:{generated::FesApplicationInterfaceGamepadID,generated::FesApplicationInterfaceVideoFixed720p60ID,
  generated::FesApplicationInterfaceMediaBlobID,generated::FesApplicationInterfaceMediaBlobStreamID,
  generated::FesApplicationInterfaceAudioPcmS16Stereo48kID,generated::FesApplicationInterfaceGamepadPortsID,
  generated::FesApplicationInterfaceKeypadPortsID,generated::FesApplicationInterfaceFirmwareBlobID,
  generated::FesApplicationInterfaceMemoryHpsDdrID,generated::FesApplicationInterfaceVideoMenuDisplayID,
  generated::FesApplicationInterfaceVideoMenuBlitterID}){
  bool named=false;
  for(const auto& interface:listed)named=named||interface.id==id;
  if(named)continue;
  auto extra=menu;extra.interfaces.push_back({id,1,0,true});
  assert(!CheckMenu(extra).ok());
 }
 auto allocated=menu;
 allocated.target.platform="de25_nano";allocated.target.device="A5EB013BB23BE4SCS";
 allocated.target.programming_profile="de25-linux-menu-v1";
 allocated.build.id="0123456789abcdeffedcba9876543210";allocated.interfaces.clear();
 for(const auto& i:all)
  if(i.id=="fes.video.fixed-720p60"||i.id=="fes.video.menu-display"||i.id=="fes.fabric.de25-scanout")
   allocated.interfaces.push_back(i);
 assert(allocated.interfaces.size()==3&&CheckMenu(allocated,NativePlatform::de25_nano).ok());
 assert(!CheckMenu(allocated).ok());
 for(std::size_t i=0;i<allocated.interfaces.size();++i) {
  auto newer=allocated;++newer.interfaces[i].minor;
  assert(!CheckMenu(newer,NativePlatform::de25_nano).ok());
 }

}

void TestNativeMenuActivationAndCompletion()
{
 using namespace mister::native;using namespace mister::native::generated;
 TempDirectory package;OpenedCorePackage opened;PopulateHpsDdrApplication(&package,true,&opened);
 std::string manifest=ReadText(package.path+"/manifest.toml");ReplaceAll(&manifest,"fes.pong","fes.menu");
 manifest+="\n[[interfaces]]\nid = \"fes.video.menu-display\"\nmajor = 1\nminor = 0\nrequired = true\n";
 {std::ofstream output(package.path+"/manifest.toml",std::ios::trunc);output<<manifest;assert(output.good());}
 assert(OpenCorePackage(package.path,"",&opened).ok());
 std::vector<std::string> events;RecordingOpener opener(events);RecordingFpga fpga(events);
 mister_test::FakeMmio mmio;FixedClock clock(100);FesGp gp(mmio,clock);FesGpCoreDriver driver(gp);
 MenuDisplayDriver display(gp,clock);MenuOperations operations;MenuWindow memory(operations);
 RecordingI2c i2c(events);RecordingVideo idle_video(events);LedgerLog log(events);
 FixedVideoBringup video(i2c,clock,log,Menu720p60Recipe());RecordingInput input(events,clock);
 const InputDeviceIdentity identity={"test",0,0,0,0};
 TempDirectory splash;const auto idle=splash.File("idle.rbf","idle");
 NativeHardware hardware(opener,fpga,idle_video,video,input,identity,clock,log,
  idle,{30000,10000,10000},&driver,{"/tmp"},SplashIdle(),&display,&memory);
 TempDirectory wrong;OpenedCorePackage nonmenu;PopulateHpsDdrApplication(&wrong,true,&nonmenu);
 assert(hardware.ConfigureMenuPackage(wrong.path,nonmenu.package_id).error.code==mister::ErrorCode::invalid_package);
 assert(hardware.ConfigureMenuPackage(package.path,std::string(64,'0')).error.code==mister::ErrorCode::invalid_package);
 fpga.boot_hps_ddr_layout=false;
 assert(hardware.ConfigureMenuPackage(package.path,opened.package_id).error.code==mister::ErrorCode::unsupported_interface);
 assert(fpga.calls==0&&operations.cleans==0&&mmio.writes.empty());fpga.boot_hps_ddr_layout=true;
 bool advertised=false;for(const auto& abi:hardware.capabilities().abis)for(const auto& interface:abi.interfaces)
  if(interface.id==FesApplicationInterfaceVideoMenuDisplayID)advertised=true;
 assert(advertised);
 MenuGpScript gp_script{&mmio};
 gp_script.BringUp();
 auto result=hardware.ConfigureMenuPackage(package.path,opened.package_id);
 if(!result.error.ok())fprintf(stderr,"menu activation: %s\n",result.error.message.c_str());
 assert(result.error.ok());assert(hardware.menu_display().available&&fpga.hps_ddr_calls==1);
 // The slots were filled black through a grant revoked before the client's,
 // so activation cleaned nothing and left no slot line cached.
 assert(operations.Granted()&&operations.cleans==0&&!hardware.menu_display().blitter);
 assert((operations.granted==std::vector<std::uint64_t>{8388608,8388608})&&operations.revokes==1);
 assert(operations.pixels[0]==0&&operations.pixels[3686400/4]==0xdeadbeef);
 // The first frame goes to slot 1; slot 0 is what scanout shows black.
 // Committing the displayed slot is refused before any GP traffic.
 std::uint8_t back=9;assert(hardware.MenuBackSlot(&back).ok()&&back==1);
 mister::MenuDisplayInfo displayed;const auto before_wrong=mmio.writes.size();
 assert(hardware.PresentMenuSlot(0,{},&displayed).code==mister::ErrorCode::invalid_request);
 assert(mmio.writes.size()==before_wrong&&hardware.menu_display().available);
 int shared=-1;assert(memory.Share(&shared).ok()&&shared==1102);
 gp_script.Present(0,0,0,true);
 assert(PresentBackSlot(hardware,&displayed).ok());
 // The client drew the slot; the runtime copied nothing and only wrote back
 // that slot's frame for the fabric before submitting it.
 assert(displayed.displayed_sequence==1&&displayed.underflows==0&&operations.cleans==1);
 assert(operations.cleaned[0].offset==4194304&&operations.cleaned[0].width==3686400&&
  operations.cleaned[0].height==1&&operations.cleaned[0].stride==5120);
 // Without a blitter, a list frame is refused before any GP traffic.
 mister::MenuDraw listed;listed.blit=true;listed.count=1;const auto before_list=mmio.writes.size();
 assert(hardware.PresentMenuSlot(0,listed,&displayed).code==mister::ErrorCode::invalid_request);
 assert(mmio.writes.size()==before_list&&hardware.menu_display().available);
 assert(hardware.MenuBackSlot(&back).ok()&&back==0);
 // Generic game launch must reject the menu before touching the running display.
 std::unique_ptr<mister::AdmittedCorePackage> admitted;assert(hardware.AdmitCorePackage(package.path,opened.package_id,&admitted).ok());
 const auto programs=fpga.calls;assert(!hardware.LoadCore(std::move(admitted),1).error.ok());assert(fpga.calls==programs&&hardware.menu_display().available);
 // An uncertain live state does not copy. The package is kept so idle recovery
 // reactivates the menu instead of dropping straight to splash.
 gp_script.Info(7,1,0);
 assert(!PresentBackSlot(hardware,&displayed).ok());
 assert(!operations.Granted());
 gp_script.QuiesceRunningMenu();gp_script.BringUp();
 assert(hardware.LoadIdle().error.ok());
 assert(hardware.menu_display().available&&hardware.menu_display().error.ok());
 assert(fpga.programmed.back()=="core.rbf");
 assert(operations.Granted());

}

void TestNativeMenuBlitterPresent(std::uint16_t declared_minor)
{
 using namespace mister::native;using namespace mister::native::generated;
 TempDirectory package;OpenedCorePackage opened;PopulateHpsDdrApplication(&package,true,&opened);
 std::string manifest=ReadText(package.path+"/manifest.toml");ReplaceAll(&manifest,"fes.pong","fes.menu");
 manifest+="\n[[interfaces]]\nid = \"fes.video.menu-display\"\nmajor = 1\nminor = 0\nrequired = true\n";
 manifest+="\n[[interfaces]]\nid = \"fes.video.menu-blitter\"\nmajor = 1\nminor = "+
  std::to_string(declared_minor)+"\nrequired = true\n";
 {std::ofstream output(package.path+"/manifest.toml",std::ios::trunc);output<<manifest;assert(output.good());}
 assert(OpenCorePackage(package.path,"",&opened).ok());
 std::vector<std::string> events;RecordingOpener opener(events);RecordingFpga fpga(events);
 mister_test::FakeMmio mmio;FixedClock clock(100);FesGp gp(mmio,clock);FesGpCoreDriver driver(gp);
 MenuDisplayDriver display(gp,clock);MenuOperations operations;MenuWindow memory(operations);
 RecordingI2c i2c(events);RecordingVideo idle_video(events);LedgerLog log(events);
 FixedVideoBringup video(i2c,clock,log,Menu720p60Recipe());RecordingInput input(events,clock);
 const InputDeviceIdentity identity={"test",0,0,0,0};
 TempDirectory splash;const auto idle=splash.File("idle.rbf","idle");
 NativeHardware hardware(opener,fpga,idle_video,video,input,identity,clock,log,
  idle,{30000,10000,10000},&driver,{"/tmp"},SplashIdle(),&display,&memory);
 fpga.boot_hps_ddr_layout=true;
 bool advertised=false;for(const auto& abi:hardware.capabilities().abis)for(const auto& interface:abi.interfaces)
  if(abi.id==FesApplicationABIID&&interface.id==FesApplicationInterfaceVideoMenuBlitterID&&
   interface.minor==FesApplicationInterfaceVideoMenuBlitterMinor)advertised=true;
 assert(advertised);
 MenuGpScript gp_script{&mmio};
 gp_script.BringUp(true,declared_minor>=kMenuBlitterMinimumMinor);
 auto result=hardware.ConfigureMenuPackage(package.path,opened.package_id);
 if(!result.error.ok())fprintf(stderr,"blitter menu activation: %s\n",result.error.message.c_str());
 const bool has_blitter=declared_minor>=kMenuBlitterMinimumMinor;
 assert(result.error.ok()&&hardware.menu_display().available&&hardware.menu_display().blitter==has_blitter);
 assert(hardware.menu_display().blitter_minor==(has_blitter?declared_minor:0));
 assert((operations.granted==std::vector<std::uint64_t>{8388608,has_blitter?83886080u:8388608u})&&
  operations.Granted()&&operations.cleans==0);
 if(!has_blitter) {
  // Minor 0 remains a CPU-only menu grant. No peer can opt the shared window
  // into list-only commits, so CPU presentation remains available afterwards.
  mister::MenuDisplayInfo displayed;gp_script.Present(0,0,0);
  assert(PresentBackSlot(hardware,&displayed).ok()&&displayed.displayed_sequence==1);
  mister::MenuDraw list;list.blit=true;list.first=100;list.count=1;
  const auto before_rejected_list=mmio.writes.size();
  assert(hardware.PresentMenuSlot(0,list,&displayed).code==mister::ErrorCode::invalid_request);
  assert(mmio.writes.size()==before_rejected_list&&operations.cleans==1&&hardware.menu_display().available);
  gp_script.Present(1,1,0);
  assert(PresentBackSlot(hardware,&displayed).ok()&&displayed.displayed_sequence==2&&operations.cleans==2);
  return;
 }
 // The client's grant covers the slots, lists and heap, and names the
 // firmware's menu-blitter minor.
 assert(hardware.menu_display().blitter_minor==1);
 // Until its first list, a blitter grant takes CPU frames as tenfoot draws
 // them: the slot's frame is written back and submitted, and the blitter
 // never runs.
 mister::MenuDisplayInfo displayed;const auto before_cpu=mmio.writes.size();
 gp_script.Present(0,0,0);
 assert(PresentBackSlot(hardware,&displayed).ok()&&displayed.displayed_sequence==1);
 assert(BlitRequests(mmio,before_cpu).empty()&&!hardware.menu_display().blitted);
 assert(operations.cleans==1&&operations.cleaned[0].offset==4194304&&operations.cleaned[0].width==3686400&&
  operations.cleaned[0].height==1&&operations.cleaned[0].stride==5120);
 // The list and the uploads reach DDR, the blitter draws slot 0 from them,
 // and only once that run completes is the slot submitted.
 mister::MenuDraw draw;draw.blit=true;draw.first=100;draw.count=3;draw.uploaded={{16777216,8192},{83886076,4}};
 gp_script.Info(3,1,0);gp_script.BlitState(0,0);
 gp_script.Reply();gp_script.Reply();gp_script.Reply();
 // The run completes between the count and state reads: the first pair looks
 // like the old count with idle state, then the confirming sample sees it done.
 gp_script.BlitState(0,0);gp_script.BlitState(0,1);
 gp_script.Reply();gp_script.Reply();gp_script.Reply();gp_script.Info(3,2,0);
 const auto before_blit=mmio.writes.size();
 assert(hardware.PresentMenuSlot(0,draw,&displayed).ok()&&displayed.displayed_sequence==2);
 assert((BlitRequests(mmio,before_blit)==std::vector<std::pair<unsigned,unsigned>>{
  {4,0},{3,0},{0,100},{1,3},{2,0},{4,0},{3,0},{4,0},{3,0}}));
 assert(operations.cleans==4&&hardware.menu_display().blitted);
 assert(operations.cleaned[1].offset==8388608+100*32&&operations.cleaned[1].width==96&&operations.cleaned[1].height==1);
 assert(operations.cleaned[2].offset==16777216&&operations.cleaned[2].width==8192);
 assert(operations.cleaned[3].offset==83886076&&operations.cleaned[3].width==4);
 std::uint8_t back=9;assert(hardware.MenuBackSlot(&back).ok()&&back==1);
 // After a list only the blitter draws this grant's slots. A CPU frame is
 // refused before any GP traffic or write-back, and the menu keeps running.
 const auto before_refused=mmio.writes.size();
 assert(hardware.PresentMenuSlot(1,{},&displayed).code==mister::ErrorCode::invalid_request);
 assert(mmio.writes.size()==before_refused&&operations.cleans==4&&hardware.menu_display().available);
 // A fault is a failed present: the grant is revoked, the slot is never
 // submitted, and the package is kept for bounded reactivation.
 draw.uploaded.clear();
 gp_script.Info(3,2,0);gp_script.BlitState(0,1);
 gp_script.Reply();gp_script.Reply();gp_script.Reply();
 gp_script.BlitState(0x0302,1);
 const auto before_fault=mmio.writes.size();
 const auto faulted=hardware.PresentMenuSlot(1,draw,&displayed);
 assert(faulted.code==mister::ErrorCode::io_failed&&faulted.message=="menu blitter fault 3");
 for(std::size_t i=before_fault;i<mmio.writes.size();++i)
  if(mmio.writes[i].offset==kSpiGpoAddress)assert(((mmio.writes[i].value&FesGpOpcodeMask)>>24)!=FesApplicationOpcodeMenuSubmit);
 assert(!hardware.menu_display().available&&!operations.Granted()&&operations.cleans==5);
 // The reactivated menu's new grant takes CPU frames again.
 gp_script.QuiesceRunningMenu();gp_script.BringUp(true);
 assert(hardware.LoadIdle().error.ok()&&hardware.menu_display().available&&hardware.menu_display().blitter);
 assert(operations.Granted()&&operations.granted.back()==83886080&&!hardware.menu_display().blitted);
 gp_script.Present(0,0,0);
 assert(PresentBackSlot(hardware,&displayed).ok()&&displayed.displayed_sequence==1&&operations.cleans==6);
}

void TestMenuUnderflowPolicyReactivatesThenSplashes()
{
 using namespace mister::native;using namespace mister::native::generated;
 TempDirectory package;OpenedCorePackage opened;PopulateHpsDdrApplication(&package,true,&opened);
 std::string manifest=ReadText(package.path+"/manifest.toml");ReplaceAll(&manifest,"fes.pong","fes.menu");
 manifest+="\n[[interfaces]]\nid = \"fes.video.menu-display\"\nmajor = 1\nminor = 0\nrequired = true\n";
 {std::ofstream output(package.path+"/manifest.toml",std::ios::trunc);output<<manifest;assert(output.good());}
 assert(OpenCorePackage(package.path,"",&opened).ok());
 std::vector<std::string> events;RecordingOpener opener(events);RecordingFpga fpga(events);
 mister_test::FakeMmio mmio;FixedClock clock(100);FesGp gp(mmio,clock);FesGpCoreDriver driver(gp);
 MenuDisplayDriver display(gp,clock);MenuOperations operations;MenuWindow memory(operations);
 RecordingI2c i2c(events);RecordingVideo idle_video(events);LedgerLog log(events);
 FixedVideoBringup video(i2c,clock,log,Menu720p60Recipe());RecordingInput input(events,clock);
 const InputDeviceIdentity identity={"test",0,0,0,0};
 TempDirectory splash;const auto idle=splash.File("idle.rbf","idle");
 NativeHardware hardware(opener,fpga,idle_video,video,input,identity,clock,log,
  idle,{30000,10000,10000},&driver,{"/tmp"},SplashIdle(),&display,&memory);
 MenuGpScript script{&mmio};script.BringUp();
 auto configured=hardware.ConfigureMenuPackage(package.path,opened.package_id);
 if(!configured.error.ok())fprintf(stderr,"menu underflow setup: %s\n",configured.error.message.c_str());
 assert(configured.error.ok());
 mister::MenuDisplayInfo displayed;
 auto show=[&](std::uint32_t sequence,std::uint32_t baseline,std::uint32_t final_underflows){
  script.Present(sequence,baseline,final_underflows);
  return PresentBackSlot(hardware,&displayed);
 };
 auto reactivate=[&]{
  script.QuiesceRunningMenu();script.BringUp();
  const auto idle_result=hardware.LoadIdle();
  if(!idle_result.error.ok())fprintf(stderr,"menu reactivation: %s\n",idle_result.error.message.c_str());
  assert(idle_result.error.ok());
  assert(hardware.menu_display().available&&hardware.menu_display().error.ok());
  assert(fpga.programmed.back()=="core.rbf");
 };
 assert(show(0,0,100).ok());
 assert(displayed.underflows==100&&hardware.menu_display().underflows==100&&hardware.menu_display().available);
 assert(show(1,5000,5000).ok());
 assert(displayed.underflows==0);
 assert(show(2,0,kMenuUnderflowPresentCap).ok());
 assert(displayed.underflows==kMenuUnderflowPresentCap);
 assert(show(3,kMenuUnderflowPresentCap,kMenuUnderflowPresentCap).ok());
 assert(displayed.underflows==0);
 std::uint32_t sequence=4,baseline=10;
 for(unsigned i=0;i+1<kMenuUnderflowSustainPresents;++i,++sequence,++baseline){
  assert(show(sequence,baseline,baseline+1).ok());
  assert(displayed.underflows==1);
 }
 script.Present(sequence,baseline,baseline+1);
 assert(!PresentBackSlot(hardware,&displayed).ok());
 assert(!hardware.menu_display().available);
 assert(hardware.menu_display().error.message=="menu underflow persisted across presents");
 reactivate();
 script.Present(0,0,kMenuUnderflowPresentCap+1);
 assert(!PresentBackSlot(hardware,&displayed).ok());
 assert(hardware.menu_display().error.message=="menu underflow exceeded the per-present cap");
 reactivate();
 clock.now_+=kMenuReactivationWindowMs;
 script.Present(0,0,kMenuUnderflowPresentCap+1);
 assert(!PresentBackSlot(hardware,&displayed).ok());
 reactivate();
 script.Present(0,0,kMenuUnderflowPresentCap+1);
 assert(!PresentBackSlot(hardware,&displayed).ok());
 reactivate();
 script.Present(0,0,kMenuUnderflowPresentCap+1);
 assert(!PresentBackSlot(hardware,&displayed).ok());
 assert(!hardware.menu_display().available);
 assert(hardware.menu_display().error.message=="menu underflow exceeded the per-present cap");
 const auto programs=fpga.calls;
 assert(hardware.LoadIdle().error.ok());
 assert(!hardware.menu_display().available);
 assert(hardware.menu_display().error.message=="menu underflow exceeded the per-present cap");
 assert(fpga.programmed.back()=="idle.rbf"&&fpga.calls==programs+1);
 assert(!operations.Granted());
 // Revoked: no client can be handed the window until it is granted again.
 int shared=-1;assert(!memory.Share(&shared).ok()&&shared==-1);
 script.toggle=false;script.BringUp();
 configured=hardware.ConfigureMenuPackage(package.path,opened.package_id);
 if(!configured.error.ok())fprintf(stderr,"menu reconfigure: %s\n",configured.error.message.c_str());
 assert(configured.error.ok()&&hardware.menu_display().available&&operations.Granted());
 script.Present(0,0,kMenuUnderflowPresentCap+1);
 assert(!PresentBackSlot(hardware,&displayed).ok());
 reactivate();
 // A counter stuck at 0xffffffff is not a clean present: both samples
 // saturated used to subtract to a zero delta and reset the streak.
 script.Present(0,0xffffffffu,0xffffffffu);
 assert(!PresentBackSlot(hardware,&displayed).ok());
 assert(!hardware.menu_display().available);
 assert(hardware.menu_display().error.message=="menu underflow exceeded the per-present cap");
}

void TestHpsDdrPortsReleaseAfterIdentityBeforeExecution()
{
	using namespace mister::native;
	using namespace mister::native::generated;
	enum class Core { undeclared, matching, missing_cell };
	for (const Core core : {Core::undeclared, Core::matching, Core::missing_cell}) {
		const bool declared = core != Core::undeclared;
		TempDirectory temporary;
		TempDirectory package;
		OpenedCorePackage opened;
		PopulateHpsDdrApplication(&package, declared, &opened);
		std::vector<std::string> events;
		RecordingOpener opener(events);
		mister_test::FakeMmio mmio;
		FixedClock clock(100);
		LinuxFpgaManager fpga(mmio, clock, temporary.File("boot-hps-ddr", "latched\n"));
		RecordingI2c i2c(events);
		RecordingVideo idle_video(events);
		LedgerLog log(events);
		FixedVideoBringup game_video(i2c, clock, log, Menu720p60Recipe());
		RecordingInput input(events, clock);
		const InputDeviceIdentity identity = {
			"FogCast Virtual Gamepad", 0x0006, 0x0000, 0x0001, 0x0001};
		FesGp transport(mmio, clock);
		FesGpCoreDriver driver(transport);
		NativeHardware hardware(opener, fpga, idle_video, game_video, input, identity,
			clock, log, temporary.File("idle.rbf", "idle"), {30000, 10000, 10000},
			&driver, {"/tmp"});
		std::unique_ptr<mister::AdmittedCorePackage> admitted;
		assert(hardware.AdmitCorePackage(package.path, opened.package_id, &admitted).ok());
		ScriptFpgaProgramming(&mmio);
		SetHpsDdrMirrors(&mmio, core == Core::matching);
		auto words = FesGpIdentityWords();
		words[FesGpIdentityAbiTagIndex] = FesApplicationAbiTag;
		words[FesGpIdentityCapabilitiesIndex] = static_cast<std::uint16_t>(
			FesApplicationCapabilityVideoFixed720p60 |
			(declared ? FesApplicationCapabilityMemoryHpsDdr : 0u));
		ScriptFesGpIdentity(&mmio, words);
		PushFesGpResponse(&mmio, true, 0); // execution release
		const mister::HardwareResult loaded = hardware.LoadCore(std::move(admitted), 1);

		// Programming holds the ports in reset and releases only the bridge
		// reset and L3 remap; identity follows directly.
		std::vector<std::uint32_t> port_resets;
		std::size_t remap = mmio.writes.size();
		for (std::size_t index = 0; index < mmio.writes.size(); ++index) {
			const auto& write = mmio.writes[index];
			if (write.offset == kSdrFpgaPortResetAddress) port_resets.push_back(write.value);
			if (write.offset == kL3RemapAddress && write.value == kL3RemapFpgaEnabled)
				remap = index;
		}
		assert(remap < mmio.writes.size());
		assert(mmio.writes[remap - 1].offset == kBridgeResetAddress);
		assert(mmio.writes[remap - 1].value == kBridgesReleased);
		const std::size_t identity_end = remap + 1 + FesGpIdentityWordCount * 2;
		assert(mmio.writes.size() >= identity_end);
		for (std::size_t index = remap + 1; index < identity_end; ++index)
			assert(mmio.writes[index].offset == kFpgaGpoAddress);
		std::size_t mirror_reads = 0;
		for (const std::uint32_t read : mmio.reads)
			if (read == kSdrCportWidthAddress) ++mirror_reads;
		assert(mirror_reads == (declared ? 1u : 0u));
		if (core == Core::missing_cell) {
			assert(loaded.error.code == mister::ErrorCode::core_mismatch);
			assert(loaded.error.phase == "identity");
			assert(loaded.error.message ==
				"HPS DDR CPORTWIDTH mismatch: observed=0xfff expected=0x16");
			assert(loaded.mutation_attempted);
			// Neither the ports nor execution leave reset.
			assert(port_resets == std::vector<std::uint32_t>({kSdrFpgaPortsDisabled}));
			assert(mmio.writes.size() == identity_end);
			continue;
		}
		if (!loaded.error.ok()) fprintf(stderr, "HPS DDR activation: %s\n",
			loaded.error.message.c_str());
		assert(loaded.error.ok());
		std::size_t release = identity_end;
		if (core == Core::matching) {
			assert(mmio.writes[release].offset == kSdrFpgaPortResetAddress);
			assert(mmio.writes[release].value == kSdrFpgaPortsEnabled);
			++release;
			assert(port_resets == std::vector<std::uint32_t>({
				kSdrFpgaPortsDisabled, kSdrFpgaPortsEnabled}));
		} else {
			assert(port_resets == std::vector<std::uint32_t>({kSdrFpgaPortsDisabled}));
		}
		// The execution-release request is the only exchange after the ports.
		assert(mmio.writes.size() == release + 2);
		const std::uint32_t command = mmio.writes.back().value;
		assert(mmio.writes.back().offset == kFpgaGpoAddress);
		assert((command & FesGpOpcodeMask) ==
			FesGpOpcodeGameplay * (FesGpOpcodeMask & (~FesGpOpcodeMask + 1u)));
		assert((command & FesGpArgumentMask) == FesGpGameplayRelease);
	}
}

// A card whose U-Boot core did not latch the layout (an old splash kept by
// a network update) neither advertises nor admits fes.memory.hps-ddr.
void TestHpsDdrNeedsTheBootLayout()
{
	using namespace mister::native;
	for (const bool latched : {true, false}) {
		TempDirectory temporary;
		TempDirectory declaring;
		TempDirectory plain;
		OpenedCorePackage declared;
		OpenedCorePackage undeclared;
		PopulateHpsDdrApplication(&declaring, true, &declared);
		PopulateHpsDdrApplication(&plain, false, &undeclared);
		std::vector<std::string> events;
		RecordingOpener opener(events);
		mister_test::FakeMmio mmio;
		FixedClock clock(100);
		LinuxFpgaManager fpga(mmio, clock,
			temporary.File("boot-hps-ddr", latched ? "latched\n" : "absent\n"));
		RecordingI2c i2c(events);
		RecordingVideo idle_video(events);
		LedgerLog log(events);
		FixedVideoBringup game_video(i2c, clock, log, Menu720p60Recipe());
		RecordingInput input(events, clock);
		const InputDeviceIdentity identity = {
			"FogCast Virtual Gamepad", 0x0006, 0x0000, 0x0001, 0x0001};
		FesGp transport(mmio, clock);
		FesGpCoreDriver driver(transport);
		NativeHardware hardware(opener, fpga, idle_video, game_video, input, identity,
			clock, log, temporary.File("idle.rbf", "idle"), {30000, 10000, 10000},
			&driver, {"/tmp"});

		bool advertised = false;
		for (const auto& abi : hardware.capabilities().abis)
			for (const auto& interface : abi.interfaces)
				if (interface.id == "fes.memory.hps-ddr") advertised = true;
		assert(advertised == latched);

		std::unique_ptr<mister::AdmittedCorePackage> admitted;
		const mister::Error admission =
			hardware.AdmitCorePackage(declaring.path, declared.package_id, &admitted);
		mister::CorePackageInspection inspection;
		assert(hardware.InspectCorePackage(declaring.path, declared.package_id,
			&inspection).ok());
		if (latched) {
			assert(admission.ok());
			assert(inspection.compatible);
		} else {
			assert(admission.code == mister::ErrorCode::unsupported_interface);
			assert(!inspection.compatible);
			assert(inspection.compatibility_error.code == mister::ErrorCode::unsupported_interface);
		}
		// Cores that do not declare the interface are unaffected.
		std::unique_ptr<mister::AdmittedCorePackage> other;
		assert(hardware.AdmitCorePackage(plain.path, undeclared.package_id, &other).ok());
		// The verdict came from the record, not the bus.
		assert(mmio.reads.empty());
	}
}

void TestHpsDdrReleaseFailureRecoversBeforeExecution()
{
	struct Case {
		bool declared;
		mister::Error failure;
		const char* phase;
	};
	const Case cases[] = {
		{false, {}, ""},
		{true, {}, ""},
		{true, {mister::ErrorCode::core_mismatch,
			"HPS DDR PORTCFG mismatch: observed=0x1 expected=0x0", "",
			"PORTCFG=0x0", "PORTCFG=0x1"}, "identity"},
		{true, {mister::ErrorCode::program_failed,
			"HPS DDR CPORTRDWR read failed: injected"}, "programming"},
	};
	for (const Case& item : cases) {
		std::vector<std::string> driver_events;
		RecordingDriver gp(driver_events);
		IntegratedFixture fixture(&gp);
		fixture.Start();
		TempDirectory package;
		mister::native::OpenedCorePackage opened;
		PopulateHpsDdrApplication(&package, item.declared, &opened);
		RecordingFpga& fpga = fixture.native.fpga;
		fpga.hps_ddr_result = item.failure;
		fpga.on_hps_ddr = [&] {
			assert(driver_events == std::vector<std::string>({
				"driver.begin", "driver.identify"}));
		};
		gp.on_start = [&] { assert(fpga.hps_ddr_calls == (item.declared ? 1 : 0)); };
		const mister::Error error = fixture.runtime.LoadCore(package.path, opened.package_id);
		assert(fpga.hps_ddr_calls == (item.declared ? 1 : 0));
		const mister::Status status = fixture.runtime.status();
		if (item.failure.ok()) {
			assert(error.ok());
			assert(status.state == mister::State::running_development);
			if (item.declared)
				assert(fpga.hps_ddr_deadlines == std::vector<std::uint64_t>({10100}));
			bool active = false;
			for (const auto& contract : status.capabilities.active_interfaces)
				if (contract.id == "fes.memory.hps-ddr") active = true;
			assert(active == item.declared);
			assert(fixture.runtime.Stop().ok());
			continue;
		}
		assert(error.code == item.failure.code && error.phase == item.phase);
		assert(error.message == item.failure.message);
		assert(error.expected == item.failure.expected);
		assert(error.observed == item.failure.observed);
		assert(Count(driver_events, "driver.start") == 0);
		// The one defined-idle recovery holds the identified core and reprograms.
		assert(Count(driver_events, "driver.quiesce") == 1);
		assert(fpga.calls == 3);
		assert(status.state == mister::State::idle);
		assert(status.error.code == item.failure.code && status.error.phase == item.phase);
	}
}

void TestCompositionProgramsRetainedLinkedArtifactAndRechecksBeforeMutation()
{
	for (const unsigned layout : {0u, 1u, 2u}) for (const bool mutate : {false, true}) {
		const bool parts=layout!=0, native=layout==2;
		std::vector<std::string> driver_events;
		RecordingDriver driver(driver_events);
		Fixture fixture(&driver);
		TempDirectory package, expansion, composition;
		std::string manifest = ReadText("tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
		if(parts) {
			ReplaceAll(&manifest,"fes.simple-game","fes.application");ReplaceAll(&manifest,"fes.pong","fes.coleco");
			manifest += "\n[[interfaces]]\nid = \"fes.expansion.coleco-bus\"\nmajor = 2\nminor = 0\nrequired = false\n";
			manifest += "\n[[interfaces]]\nid = \"" + std::string(native ? "fes.fabric.video.native-pixels" : "fes.fabric.video.raster-rgb888") + "\"\nmajor = 1\nminor = 0\nrequired = false\n";
		} else {
		ReplaceAll(&manifest, "fes.simple-game", "fes.simple-computer");
		ReplaceAll(&manifest, "fes.gamepad", "fes.keyboard");
		manifest += "\n[[interfaces]]\nid = \"fes.expansion.zx81-bus\"\nmajor = 1\nminor = 0\nrequired = false\n";
		manifest += "\n[[interfaces]]\nid = \"fes.media.blob\"\nmajor = 1\nminor = 0\nrequired = true\n";
		}
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
		if(parts) {ReplaceAll(&manifest,"fes.zx81-bus.socket/1",native ? "fes.coleco-native-video.socket/1" : "fes.coleco-video.socket/1");ReplaceAll(&manifest,"fes.expansion.zx81-bus",native ? "fes.fabric.video.native-pixels" : "fes.fabric.video.raster-rgb888");}
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
		if(parts) {
			request.parts={{"video",request.expansion_path}};request.expansion_path.clear();info.layout=native ? "fes.coleco-native-video.parts/1" : "fes.coleco-video.parts/1";
			info.parts={{"video",info.expansion_id}};info.expansion_id.clear();
			info.id=hash("fes-parts-composition-v1"+std::string(1,'\0')+info.package_id+std::string(1,'\0')+info.layout+std::string(1,'\0')+"video:"+info.parts[0].part_id+std::string(1,'\0')+info.payload_sha256);
		}
		std::unique_ptr<mister::AdmittedCorePackage> admitted;
		const auto admission = fixture.hardware.AdmitCoreComposition(package.path, base.package_id, request, &admitted);
		if (!admission.ok()) fprintf(stderr, "composition admission: %s\n", admission.message.c_str());
		assert(admission.ok());
		if(parts)assert(admitted->composition().parts[0].part_id==info.parts[0].part_id);
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

void TestLibraryPartsChecksCoreNamespaceBeforeProgramming(bool native=false)
{
	std::vector<std::string> driver_events;
	RecordingDriver driver(driver_events);
	IntegratedFixture fixture(&driver);
	fixture.Start();
	TempDirectory package, expansion, composition, data;
	std::string manifest = ReadText("tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
	ReplaceAll(&manifest, "fes.simple-game", "fes.application");
	ReplaceAll(&manifest, "fes.pong", "fes.coleco");
	manifest += "\n[[interfaces]]\nid = \"fes.expansion.coleco-bus\"\nmajor = 2\nminor = 0\nrequired = false\n";
	manifest += "\n[[interfaces]]\nid = \"" + std::string(native ? "fes.fabric.video.native-pixels" : "fes.fabric.video.raster-rgb888") + "\"\nmajor = 1\nminor = 0\nrequired = false\n";
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
		"\"map\":\"" + std::string(native ? "fes.coleco-native-video.socket/1" : "fes.coleco-video.socket/1") + "\",\"recipe_sha256\":\"" + std::string(64,'c') + "\",\"revision\":\"" + std::string(40,'d') +
		"\",\"shell_build_id\":\"" + base.descriptor.build.id + "\",\"shell_package_id\":\"" + base.package_id +
		"\",\"shell_sha256\":\"" + base.descriptor.payload.sha256 + "\",\"slot\":\"" + std::string(native ? "fes.fabric.video.native-pixels" : "fes.fabric.video.raster-rgb888") + "\",\"slot_major\":1,\"slot_minor\":0}";
	expansion.File("manifest.json", manifest);
	mister::CoreCompositionRequest request;
	request.parts = {{"video", expansion.path}};
	request.payload_path = composition.File("linked.rbf", linked);
	auto& info = request.composition;
	info.package_id = base.package_id;
	info.shell_sha256 = base.descriptor.payload.sha256;
	info.payload_sha256 = hash(linked);
	info.payload_size = linked.size();
	info.layout = native ? "fes.coleco-native-video.parts/1" : "fes.coleco-video.parts/1";
	info.parts = {{"video", hash(std::string("fes-expansion-v1\0", 17) + manifest)}};
	info.id = hash("fes-parts-composition-v1" + std::string(1, '\0') + info.package_id +
		std::string(1, '\0') + info.layout + std::string(1, '\0') + "video:" +
		info.parts[0].part_id + std::string(1, '\0') + info.payload_sha256);
	const auto programs = fixture.native.fpga.calls;
	auto reject_plain_native = [&]() {
		if (!native) return;
		const auto before=fixture.runtime.status();
		const auto calls=fixture.native.fpga.calls;
		fixture.native.events.clear();driver_events.clear();
		for (const bool library : {false,true}) {
			const auto error=library ? fixture.runtime.LoadLibraryCore(package.path,base.package_id,data.path) :
				fixture.runtime.LoadCore(package.path,base.package_id);
			assert(error.code==mister::ErrorCode::unsupported_interface && error.phase=="admission");
			assert(error.message=="native video shell requires a video parts composition");
			const auto after=fixture.runtime.status();
			assert(after.state==before.state && after.generation==before.generation);
			assert(after.active_package.package_id==before.active_package.package_id &&
				after.active_package.composition.id==before.active_package.composition.id);
			assert(fixture.native.fpga.calls==calls && fixture.native.events.empty() && driver_events.empty());
		}
	};
	if (native) {
		mister::CorePackageInspection inspection;
		assert(fixture.runtime.InspectCore(package.path,base.package_id,&inspection).ok() && inspection.compatible);
		reject_plain_native();
	}
	// Normal library Play inspects the base namespace before loading its parts.
	// This metadata operation must not activate even a valid native shell.
	auto inspect_data = [&](const std::string& path, const std::string& id,
		mister::CoreData* output) {
		const auto before = fixture.runtime.status();
		const auto calls = fixture.native.fpga.calls;
		fixture.native.events.clear();
		driver_events.clear();
		const auto error = fixture.runtime.InspectCoreData(path, id, data.path, "", output);
		const auto after = fixture.runtime.status();
		assert(after.state == before.state && after.generation == before.generation);
		assert(after.active_package.package_id == before.active_package.package_id &&
			after.active_package.composition.id == before.active_package.composition.id);
		assert(fixture.native.fpga.calls == calls && fixture.native.events.empty() &&
			driver_events.empty());
		return error;
	};
	mister::CoreData preflight;
	assert(inspect_data(package.path, base.package_id, &preflight).ok());
	assert(preflight.package_id == base.package_id && preflight.core_id == "fes.coleco");
	assert(preflight.mode == "volatile" && preflight.layout.id.empty() &&
		preflight.revision == "absent");
	reject_plain_native();
	assert(inspect_data(package.path, std::string(64, '0'), &preflight).code ==
		mister::ErrorCode::invalid_package);
	TempDirectory malformed, incompatible;
	const auto shell_manifest = ReadText(package.path + "/manifest.toml");
	malformed.File("manifest.toml", shell_manifest);
	malformed.File("core.rbf", "invalid-data");
	assert(inspect_data(malformed.path, base.package_id, &preflight).code ==
		mister::ErrorCode::invalid_package);
	incompatible.File("manifest.toml", shell_manifest +
		"\n[[interfaces]]\nid = \"fes.unsupported\"\nmajor = 1\nminor = 0\nrequired = true\n");
	incompatible.File("core.rbf", ReadText(package.path + "/core.rbf"));
	mister::native::OpenedCorePackage incompatible_base;
	assert(mister::native::OpenCorePackage(incompatible.path, "", &incompatible_base).ok());
	const auto unsupported = inspect_data(incompatible.path, incompatible_base.package_id, &preflight);
	assert(unsupported.code == mister::ErrorCode::unsupported_interface);
	assert(fixture.runtime.LoadLibraryPartsCore(package.path, base.package_id, "", request).code == mister::ErrorCode::invalid_request);
	assert(fixture.native.fpga.calls == programs);
	std::unique_ptr<mister::native::CoreDataFile> file;
	assert(mister::native::CoreDataFile::Open(data.path, "fes.coleco", &file).ok());
	mister::CoreData saved;
	saved.core_id = "fes.coleco";
	saved.layout = {"fes.pong.progress", 1, 0};
	saved.paddle_speed = 1;
	assert(file->Persist(saved, "absent", &saved).ok());
	const std::string dir = data.path + "/" + mister::native::CoreDataNamespace("fes.coleco");
	const auto record = ReadText(dir + "/record.bin");
	const auto inspection_rejected = inspect_data(package.path, base.package_id, &preflight);
	assert(inspection_rejected.code == mister::ErrorCode::incompatible_data &&
		inspection_rejected.phase == "core_data");
	assert(ReadText(dir + "/record.bin") == record);
	fixture.native.events.clear();
	driver_events.clear();
	const auto rejected = fixture.runtime.LoadLibraryPartsCore(package.path, base.package_id, data.path, request);
	assert(rejected.code == mister::ErrorCode::incompatible_data && rejected.phase == "core_data");
	assert(fixture.native.fpga.calls == programs && fixture.native.events.empty() && driver_events.empty());
	assert(fixture.runtime.status().state == mister::State::idle);
	// Developer loads intentionally stay volatile, even with an existing namespace.
	assert(fixture.runtime.LoadComposedCore(package.path, base.package_id, request).ok());
	assert(fixture.runtime.status().core_data.mode == "volatile");
	reject_plain_native();
	assert(fixture.runtime.Stop().ok());
	file.reset();
	assert(unlink((dir + "/record.bin").c_str()) == 0);
	assert(fixture.runtime.LoadLibraryPartsCore(package.path, base.package_id, data.path, request).ok());
	const auto status = fixture.runtime.status();
	assert(status.active_package.package_id == base.package_id && status.active_package.composition.id == info.id);
	assert(status.active_package.composition.parts[0].part_id == info.parts[0].part_id);
	assert(status.core_data.mode == "volatile" && status.core_data.core_id == "fes.coleco");
	assert(fixture.runtime.Stop().ok());
	assert(rmdir(dir.c_str()) == 0);
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
		assert(CreateKnownProductionHardware(log, &hardware).ok());
		// Creation and data operations are lazy with respect to all physical devices.
		// Do not call Start/LoadIdle/LoadCore: this exercises only production admission/storage.
		mister::CoreData observed;
		auto result = hardware->InspectCoreData(package.path, id, data.path, "", &observed);
		inspected = result.ok() && observed.package_id == id && observed.mode == "persistent" &&
					observed.revision == "absent" && observed.paddle_speed == 1 &&
					observed.best_rally == 0;
		std::unique_ptr<mister::AdmittedCorePackage> admitted;
		assert(hardware->AdmitCorePackage(package.path, id, &admitted).ok());
		result = hardware->PrepareCoreData(admitted.get(), data.path, "", &observed);
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
			hardware->UpdateCoreSettings(package.path, id, data.path, "", saved.revision, PaddleSpeed(0), &observed);
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

void TestProductionFactoryForwardsProgrammedBitstreamWithoutHardwareMutation()
{
	const char* development_root = "/tmp/fogcast-development";
	const char* package_root = "/tmp/fogcast-development/core-packages";
	const bool created_development = mkdir(development_root, 0700) == 0;
	assert(created_development || errno == EEXIST);
	const bool created_packages = mkdir(package_root, 0700) == 0;
	assert(created_packages || errno == EEXIST);
	bool rejected = false;
	bool attached = false;
	{
		TempDirectory package(package_root), bits;
		const std::string id = PersistentPackage(&package);
		const std::string bytes = "rom-init-bitstream";
		const std::string bitstream = bits.File("programmed.rbf", bytes);
		mister::native::Sha256 hash;
		hash.Update(bytes.data(), bytes.size());
		const std::string digest = mister::native::Sha256Hex(hash.Final());
		mister_test::CaptureLog log;
		std::unique_ptr<mister::Hardware> hardware;
		assert(CreateKnownProductionHardware(log, &hardware).ok());
		std::unique_ptr<mister::AdmittedCorePackage> admitted;
		assert(hardware->AdmitCorePackage(package.path, id, &admitted).ok());
		const auto mismatch = hardware->AttachProgrammedBitstream(
			admitted.get(), bitstream, std::string(64, 'a'));
		rejected = mismatch.code == mister::ErrorCode::invalid_request &&
				   mismatch.phase == "admission" &&
				   mismatch.message == "programmed bitstream does not match its receipt";
		const auto match =
			hardware->AttachProgrammedBitstream(admitted.get(), bitstream, digest);
		attached = match.ok();
		hardware.reset();
		admitted.reset();
	}
	if (created_packages)
		assert(rmdir(package_root) == 0 || errno == ENOTEMPTY);
	if (created_development)
		assert(rmdir(development_root) == 0 || errno == ENOTEMPTY);
	assert(rejected && attached);
}

std::string Pc386Manifest()
{
	std::string manifest = ReadText("tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
	ReplaceAll(&manifest, "fes.pong", "fes.pc386");
	ReplaceAll(&manifest, "id = \"fes.simple-game\"\nmajor = 1\nminor = 0",
		"id = \"fes.computer\"\nmajor = 1\nminor = 1");
	ReplaceAll(&manifest, "id = \"fes.gamepad\"", "id = \"fes.keyboard.hid\"");
	for (const auto* id : {"fes.audio.pcm-s16-stereo-48k", "fes.mouse.relative",
		"fes.persistence.words", "fes.pc386.machine", "fes.rtc.wall-clock"})
		manifest += std::string("\n[[interfaces]]\nid = \"") + id +
			"\"\nmajor = 1\nminor = 0\nrequired = true\n";
	return manifest;
}

// A pc386 library launch restores its game's machine record and seeds the
// clock while held; Stop captures the record; each game has its own.
void TestPc386LibraryRecordAndWallClock()
{
	using namespace mister::native;
	using namespace mister::native::generated;
	const std::uint32_t caps = FesComputerCapabilityVideoFixed720p60 | FesComputerCapabilityKeyboardHid |
		FesComputerCapabilityAudioPcmS16Stereo48k | FesComputerCapabilityMouseRelative |
		FesComputerCapabilityPersistenceWords | FesComputerCapabilityPc386Machine |
		FesComputerCapabilityRtcWallClock;
	mister_test::ComputerEndpoint endpoint(caps, {}, "0123456789abcdef0123456789abcdef");
	endpoint.abi_minor = 1;
	FixedClock clock(100);
	FesGp transport(endpoint, clock);
	FesGpCoreDriver driver(transport);
	IntegratedFixture fixture(&driver);
	fixture.native.clock.has_local = true;
	fixture.native.clock.local.tm_year = 126;
	fixture.native.clock.local.tm_mon = 9;
	fixture.native.clock.local.tm_mday = 28;
	fixture.native.clock.local.tm_hour = 14;
	fixture.native.clock.local.tm_min = 5;
	fixture.native.clock.local.tm_sec = 37;
	fixture.native.clock.local.tm_wday = 3;
	fixture.Start();
	fixture.native.fpga.on_program = [&] { endpoint.Reset(); };
	TempDirectory package, data;
	package.File("manifest.toml", Pc386Manifest());
	package.File("core.rbf", ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
	OpenedCorePackage opened;
	assert(OpenCorePackage(package.path, "", &opened).ok());
	const std::string id = opened.package_id;
	// The record is per game: a pc386 package needs a game, Pong never takes one.
	assert(fixture.runtime.LoadLibraryCore(package.path, id, data.path).code ==
		mister::ErrorCode::invalid_request);
	mister::CoreData inspected;
	assert(fixture.runtime.InspectCoreData(package.path, id, data.path, "ultima-vi", &inspected).ok());
	assert(inspected.revision == "absent" && inspected.words == Pc386MachineDefaults() &&
		inspected.mode == "persistent" && inspected.game_id == "ultima-vi");
	mister::CoreSettingsUpdate settings;
	settings.pc386 = true;
	settings.machine.preset = 1;
	settings.machine.sb_base = 0x240;
	settings.machine.sb_irq = 5;
	assert(fixture.runtime.UpdateCoreSettings(package.path, id, data.path, "ultima-vi", "absent",
		PaddleSpeed(1), &inspected).code == mister::ErrorCode::invalid_request);
	auto invalid = settings;
	invalid.machine.sb_irq = 9;
	assert(fixture.runtime.UpdateCoreSettings(package.path, id, data.path, "ultima-vi", "absent",
		invalid, &inspected).code == mister::ErrorCode::invalid_request);
	assert(fixture.runtime.UpdateCoreSettings(package.path, id, data.path, "ultima-vi", "absent",
		settings, &inspected).ok());
	assert(inspected.words[0] == 1 && inspected.words[4] == 0x240 && inspected.words[5] == 5);

	assert(fixture.runtime.LoadLibraryCore(package.path, id, data.path, "ultima-vi").ok());
	assert(!endpoint.held && fixture.runtime.status().core_data.mode == "persistent");
	assert(endpoint.machine_words == inspected.words);
	assert(endpoint.clock == std::vector<unsigned>({2026, 10, 28, 14, 5, 37, 4}));
	// Restore and the clock both precede release.
	std::size_t commit = 0, clock_word = 0, release = 0;
	for (std::size_t at = 0; at < endpoint.requests.size(); ++at) {
		const auto& r = endpoint.requests[at];
		if (r.opcode == FesComputerOpcodeDataControl && r.argument == FesComputerDataCommit) commit = at;
		if (r.opcode == FesComputerOpcodeWallClock && r.index == 3) clock_word = at;
		if (r.opcode == FesComputerOpcodeExecution && r.argument == FesComputerExecutionRelease) release = at;
	}
	assert(commit && commit < release && clock_word < release && clock_word);
	assert(fixture.runtime.UpdateCoreSettings(package.path, id, data.path, "ultima-vi",
		inspected.revision, settings, &inspected).code == mister::ErrorCode::busy);
	assert(fixture.runtime.UpdateCoreSettings(package.path, id, data.path, "", "absent",
		settings, &inspected).code == mister::ErrorCode::invalid_request);
	endpoint.GuestCmosWrite(0x40, 0x5a);
	assert(fixture.runtime.Stop().ok());
	mister::CoreData saved;
	assert(fixture.runtime.InspectCoreData(package.path, id, data.path, "ultima-vi", &saved).ok());
	assert(saved.words[8 + 0x20] == 0x5a && saved.words[0] == 1 && saved.revision != inspected.revision);
	auto changed_settings = settings;
	changed_settings.machine.preset = 2;
	assert(fixture.runtime.UpdateCoreSettings(package.path, id, data.path, "ultima-vi",
		saved.revision, changed_settings, &inspected).ok());
	assert(inspected.words[0] == 2 && inspected.words[8 + 0x20] == 0x5a);
	// A Stop whose record cannot be published resumes the core, and the
	// package stays persistent through its record although it has no disk.
	assert(fixture.runtime.LoadLibraryCore(package.path, id, data.path, "ultima-vi").ok());
	const std::string first_record = data.path + "/games/" + GameDataNamespace("fes.pc386", "ultima-vi");
	assert(chmod(first_record.c_str(), 0500) == 0);
	assert(fixture.runtime.Stop().code == mister::ErrorCode::save_failed);
	assert(fixture.runtime.status().state == mister::State::running_development &&
		fixture.runtime.status().core_data.mode == "persistent");
	assert(chmod(first_record.c_str(), 0700) == 0);
	assert(fixture.runtime.Stop().ok());
	// A diskless 1.1 input fault still snapshots the durable machine record.
	assert(fixture.runtime.LoadLibraryCore(package.path, id, data.path, "ultima-vii").ok());
	assert(fixture.runtime.status().core_data.mode == "persistent" && fixture.runtime.status().capabilities.media_units.empty());
	endpoint.GuestCmosWrite(0x40, 0x6b);
	const auto fault_generation = fixture.runtime.status().generation;
	bool input_failed = false;
	endpoint.fail_after_request = [&](const mister_test::ComputerEndpoint::Request& request) {
		if (request.opcode == FesComputerOpcodeKeyboardHid && !input_failed) {
			input_failed = true;
			return true;
		}
		return false;
	};
	assert(!fixture.runtime.SetKeyboardHid(id, fault_generation, mister::KeyboardHidRows{}).ok());
	if (!WaitForState(fixture.runtime, mister::State::idle)) {
		const auto failed = fixture.runtime.status();
		std::fprintf(stderr, "pc386 input fault state=%u error=%s phase=%s\n",
			static_cast<unsigned>(failed.state), failed.error.message.c_str(), failed.error.phase.c_str());
		assert(false);
	}
	endpoint.fail_after_request = {};
	assert(input_failed);
	mister::CoreData fault_saved;
	assert(fixture.runtime.InspectCoreData(package.path, id, data.path, "ultima-vii", &fault_saved).ok());
	assert(fault_saved.words[8 + 0x20] == 0x6b && fault_saved.revision != "absent");
	// Another game of the same package starts from the defaults.
	assert(fixture.runtime.InspectCoreData(package.path, id, data.path, "ultima-vii", &inspected).ok());
	assert(inspected.revision == fault_saved.revision && inspected.words[8 + 0x20] == 0x6b);
	assert(fixture.runtime.InspectCoreData(package.path, id, data.path, "ultima-viii", &inspected).ok());
	assert(inspected.revision == "absent" && inspected.words == Pc386MachineDefaults());
	fixture.native.clock.has_local = false;
	assert(fixture.runtime.LoadLibraryCore(package.path, id, data.path, "ultima-vi").code ==
		mister::ErrorCode::io_failed);
	fixture.native.clock.has_local = true;
	const std::string games = data.path + "/games/";
	const std::string first = games + GameDataNamespace("fes.pc386", "ultima-vi");
	assert(unlink((first + "/record.bin").c_str()) == 0 && rmdir(first.c_str()) == 0);
	assert(unlink((games + GameDataNamespace("fes.pc386", "ultima-vii") + "/record.bin").c_str()) == 0);
	assert(rmdir((games + GameDataNamespace("fes.pc386", "ultima-vii")).c_str()) == 0);
	assert(rmdir((games + GameDataNamespace("fes.pc386", "ultima-viii")).c_str()) == 0);
	assert(rmdir(games.c_str()) == 0);
}

// A pc386 launch writes the base image with its game's delta into the
// window and commits it with its CRC; Stop stores the dirty chunks and
// publishes the merged manifest; the next launch restores them over the base.
void TestPc386AtaDiskLaunchSaveAndRestore()
{
	using namespace mister::native;
	using namespace mister::native::generated;
	const std::uint32_t caps = FesComputerCapabilityVideoFixed720p60 | FesComputerCapabilityKeyboardHid |
		FesComputerCapabilityAudioPcmS16Stereo48k | FesComputerCapabilityMouseRelative |
		FesComputerCapabilityMemoryHpsDdr | FesComputerCapabilityMediaAtaDisk |
		FesComputerCapabilityPersistenceWords | FesComputerCapabilityPc386Machine |
		FesComputerCapabilityRtcWallClock;
	mister_test::ComputerEndpoint endpoint(caps, {}, "0123456789abcdef0123456789abcdef");
	endpoint.abi_minor = 1;
	endpoint.verify_reads = 0;
	FixedClock clock(100);
	FesGp transport(endpoint, clock);
	FesGpCoreDriver driver(transport);
	IntegratedFixture fixture(&driver);
	fixture.native.clock.has_local = true;
	fixture.native.clock.local.tm_year = 126;
	fixture.native.clock.local.tm_mday = 1;
	fixture.native.clock.local.tm_wday = 4;
	fixture.native.disk_operations.region.assign(4u << 20, 0xee);
	endpoint.window = &fixture.native.disk_operations.region;
	fixture.Start();
	fixture.native.fpga.on_program = [&] { endpoint.Reset(); };
	TempDirectory package, data, media, images;
	auto manifest = Pc386Manifest();
	for (const auto* id : {"fes.memory.hps-ddr", "fes.media.ata-disk"})
		manifest += std::string("\n[[interfaces]]\nid = \"") + id + "\"\nmajor = 1\nminor = 0\nrequired = true\n";
	package.File("manifest.toml", manifest);
	package.File("core.rbf", ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
	OpenedCorePackage opened;
	assert(OpenCorePackage(package.path, "", &opened).ok());
	const std::uint32_t size = (2u << 20) + 1536;
	std::string base(size, '\0');
	for (std::uint32_t at = 0; at < size; ++at) base[at] = static_cast<char>(at * 7 + (at >> 11));
	Sha256 hash;
	hash.Update(base.data(), base.size());
	mister::AtaDiskImage disk{images.File("disk.img", base), size, Sha256Hex(hash.Final()), media.path, 4, 16, 63};
	const MediaDataIdentity identity{"fes.pc386", "ultima-vi", disk.base_media_id, 0};
	assert(fixture.runtime.LoadLibraryCore(package.path, opened.package_id, data.path, "ultima-vi").ok());
	const auto empty_status = fixture.runtime.status();
	AssertComputerABIVersions(empty_status.capabilities);
	RecordLiveComputerStatus(empty_status);
	assert(empty_status.core_data.mode == "persistent" && empty_status.capabilities.media_units.size() == 1);
	assert(empty_status.capabilities.media_units[0].interface.id == FesComputerInterfaceMediaAtaDiskID &&
		empty_status.capabilities.media_units[0].state == mister::MediaUnitState::empty &&
		!empty_status.capabilities.media_units[0].saveable);
	const auto empty_generation = empty_status.generation;
	assert(fixture.runtime.InsertMedia(disk.path, opened.package_id, empty_generation, 0, size).code ==
		mister::ErrorCode::unsupported_interface);
	assert(fixture.runtime.status().core_data.mode == "persistent");
	assert(fixture.runtime.EjectMedia(opened.package_id, empty_generation, 0).code ==
		mister::ErrorCode::unsupported_interface);
	assert(fixture.runtime.status().core_data.mode == "persistent");
	assert(fixture.runtime.Stop().ok());
	// The disk binds to the request's game and needs geometry that fits.
	auto bad = disk;
	bad.cylinders = 5;
	assert(fixture.runtime.LoadLibraryCore(package.path, opened.package_id, data.path, "ultima-vi", &bad).code ==
		mister::ErrorCode::invalid_request);
	assert(fixture.runtime.LoadLibraryCore(package.path, opened.package_id, data.path, "ultima-vi", &disk).ok());
	auto& region = fixture.native.disk_operations.region;
	assert(std::equal(base.begin(), base.end(), region.begin(),
		[](char a, std::uint8_t b) { return static_cast<std::uint8_t>(a) == b; }));
	assert(endpoint.disk_state == 3 && endpoint.disk_geometry == std::vector<unsigned>({size / 512, 4, 16, 63}));
	assert(fixture.native.disk_operations.revokes == fixture.native.disk_operations.grants.size());
	const auto status = fixture.runtime.status();
	AssertComputerABIVersions(status.capabilities);
	RecordLiveComputerStatus(status);
	assert(status.capabilities.media_units.size() == 1);
	const auto& unit = status.capabilities.media_units[0];
	assert(unit.interface.id == "fes.media.ata-disk" && unit.state == mister::MediaUnitState::ready &&
		unit.chunk_bytes == 65536 && !unit.saveable && unit.persistence_mode == "persistent" && unit.revision == "absent");
	// Released: the disk is fixed while the machine runs.
	assert(!endpoint.held && !fixture.runtime.InsertMedia(images.path + "/disk.img", opened.package_id,
		status.generation, 0, size).ok());
	const auto live_disk_request_count = endpoint.requests.size();
	assert(fixture.runtime.SaveMedia(opened.package_id, status.generation, 0).code ==
		mister::ErrorCode::unsupported_interface);
	assert(endpoint.requests.size() == live_disk_request_count && !endpoint.disk_frozen);
	assert(fixture.runtime.EjectMedia(opened.package_id, status.generation, 0).code ==
		mister::ErrorCode::unsupported_interface);
	assert(endpoint.requests.size() == live_disk_request_count && !endpoint.disk_frozen);
	endpoint.GuestSectorWrite(0, 0x11);
	endpoint.GuestSectorWrite(300, 0x22);
	endpoint.GuestSectorWrite(size / 512 - 1, 0x33);  // in the 1536-byte tail chunk
	const auto modified = region;
	const std::string record_directory = data.path + "/games/" +
		GameDataNamespace("fes.pc386", "ultima-vi");
	assert(chmod(record_directory.c_str(), 0500) == 0);
	const std::size_t failed_save_requests = endpoint.requests.size();
	const auto failed_save = fixture.runtime.Stop();
	assert(failed_save.code == mister::ErrorCode::save_failed);
	assert(fixture.runtime.status().state == mister::State::running_development);
	assert(!endpoint.disk_frozen && endpoint.data_snapshot.empty());
	bool ata_resumed = false, data_resumed = false;
	for (std::size_t at = failed_save_requests; at < endpoint.requests.size(); ++at) {
		const auto& request = endpoint.requests[at];
		ata_resumed = ata_resumed || (request.opcode == FesComputerOpcodeDiskControl &&
			request.argument == FesComputerDiskControlResume);
		data_resumed = data_resumed || (request.opcode == FesComputerOpcodeDataControl &&
			request.argument == FesComputerDataResume);
	}
	assert(ata_resumed && data_resumed);
	assert(chmod(record_directory.c_str(), 0700) == 0);
	// The resumed guest can change CMOS before retrying Stop. The retry must
	// capture this new state rather than reuse the failed save's frozen words.
	endpoint.machine_words[8 + 0x20] = 0x5a;
	assert(fixture.runtime.Stop().ok());
	mister::CoreData retried_machine;
	assert(fixture.runtime.InspectCoreData(package.path, opened.package_id, data.path,
		"ultima-vi", &retried_machine).ok());
	assert(retried_machine.words[8 + 0x20] == 0x5a);
	std::unique_ptr<AtaDeltaFile> file;
	assert(AtaDeltaFile::Open(media.path, identity, size, &file).ok());
	AtaDiskDelta saved;
	assert(file->Read(&saved).ok() && saved.revision != "absent" && saved.chunks.size() == 3);
	assert(saved.chunks[0].index == 0 && saved.chunks[1].index == 2 && saved.chunks[2].index == 32);
	std::vector<unsigned char> chunk;
	assert(file->ReadChunk(saved.chunks[2], &chunk).ok() && chunk.size() == 1536 && chunk.back() == 0x33);
	assert(endpoint.dirty_chunks.empty());
	// The next launch writes the base and the saved chunks over stale DDR.
	std::fill(region.begin(), region.end(), 0xee);
	assert(fixture.runtime.LoadLibraryCore(package.path, opened.package_id, data.path, "ultima-vi", &disk).ok());
	assert(std::equal(modified.begin(), modified.begin() + size, region.begin()));
	assert(fixture.runtime.status().capabilities.media_units[0].revision == saved.revision);
	// A Stop with no guest writes publishes nothing new.
	assert(fixture.runtime.Stop().ok());
	assert(file->Read(&saved).ok() && saved.chunks.size() == 3);
	// A completed execution-release rejection proves the guest never ran. It
	// must take idle cleanup without trying to capture a record while held.
	bool rejected_release = false;
	endpoint.reject_request = [&](const mister_test::ComputerEndpoint::Request& request) {
		if (!rejected_release && request.opcode == FesComputerOpcodeExecution &&
			request.argument == FesComputerExecutionRelease) {
			rejected_release = true;
			return 4u;
		}
		return 0u;
	};
	const std::size_t failed_start_requests = endpoint.requests.size();
	const auto failed_start = fixture.runtime.LoadLibraryCore(package.path, opened.package_id,
		data.path, "ultima-vi", &disk);
	assert(rejected_release && failed_start.code == mister::ErrorCode::io_failed);
	assert(fixture.runtime.status().state == mister::State::idle);
	for (std::size_t at = failed_start_requests; at < endpoint.requests.size(); ++at)
		assert(!(endpoint.requests[at].opcode == FesComputerOpcodeDiskControl &&
			endpoint.requests[at].argument == FesComputerDiskControlFreeze));
	endpoint.reject_request = {};
	// A base that differs from its identity, or a corrupt chunk, blocks the launch.
	std::string changed = base;
	changed[4096] ^= 1;
	{ std::ofstream rewrite(disk.path, std::ios::binary | std::ios::trunc); rewrite << changed; }
	const auto changed_base = fixture.runtime.LoadLibraryCore(package.path, opened.package_id, data.path, "ultima-vi", &disk);
	assert(changed_base.code == mister::ErrorCode::corrupt_data && changed_base.phase == "identity");
	{ std::ofstream rewrite(disk.path, std::ios::binary | std::ios::trunc); rewrite << base; }
	const std::string ns = media.path + "/" + MediaDataNamespace(identity);
	{ std::ofstream corrupt(ns + "/chunk-" + saved.chunks[1].sha256, std::ios::binary | std::ios::trunc); corrupt << "x"; }
	const auto corrupt_chunk = fixture.runtime.LoadLibraryCore(package.path, opened.package_id,
		data.path, "ultima-vi", &disk);
	assert(corrupt_chunk.code == mister::ErrorCode::corrupt_data &&
		corrupt_chunk.phase == "media_data");
	file.reset();
	assert(unlink((ns + "/record.bin").c_str()) == 0);
	for (const auto& entry : saved.chunks) unlink((ns + "/chunk-" + entry.sha256).c_str());
	assert(rmdir(ns.c_str()) == 0);
	const std::string games = data.path + "/games/";
	const std::string record = games + GameDataNamespace("fes.pc386", "ultima-vi");
	assert(unlink((record + "/record.bin").c_str()) == 0 && rmdir(record.c_str()) == 0 && rmdir(games.c_str()) == 0);
}

// An input fault, or an execution release lost at launch, leaves the
// computer mailbox poisoned. The fault save realigns it before freezing the
// ATA disk, stores the dirty chunks and the machine record, and reaches idle.
// A declared disk that was never loaded saves the record alone, and a failed
// activation saves the disk but never the record.
void TestPc386DiskFaultSaveRealignsMailbox()
{
	using namespace mister::native;
	using namespace mister::native::generated;
	for (const std::string kind : {"bound disk", "empty disk", "ambiguous release"}) {
		const std::uint32_t caps = FesComputerCapabilityVideoFixed720p60 | FesComputerCapabilityKeyboardHid |
			FesComputerCapabilityAudioPcmS16Stereo48k | FesComputerCapabilityMouseRelative |
			FesComputerCapabilityMemoryHpsDdr | FesComputerCapabilityMediaAtaDisk |
			FesComputerCapabilityPersistenceWords | FesComputerCapabilityPc386Machine |
			FesComputerCapabilityRtcWallClock;
		mister_test::ComputerEndpoint endpoint(caps, {}, "0123456789abcdef0123456789abcdef");
		endpoint.abi_minor = 1;
		endpoint.verify_reads = 0;
		FixedClock clock(100);
		FesGp transport(endpoint, clock);
		FesGpCoreDriver driver(transport);
		IntegratedFixture fixture(&driver);
		fixture.native.clock.has_local = true;
		fixture.native.clock.local.tm_year = 126;
		fixture.native.clock.local.tm_mday = 1;
		fixture.native.clock.local.tm_wday = 4;
		fixture.native.disk_operations.region.assign(4u << 20, 0xee);
		endpoint.window = &fixture.native.disk_operations.region;
		fixture.Start();
		fixture.native.fpga.on_program = [&] { endpoint.Reset(); };
		TempDirectory package, data, media, images;
		auto manifest = Pc386Manifest();
		for (const auto* id : {"fes.memory.hps-ddr", "fes.media.ata-disk"})
			manifest += std::string("\n[[interfaces]]\nid = \"") + id + "\"\nmajor = 1\nminor = 0\nrequired = true\n";
		package.File("manifest.toml", manifest);
		package.File("core.rbf", ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
		OpenedCorePackage opened;
		assert(OpenCorePackage(package.path, "", &opened).ok());
		const std::uint32_t size = (2u << 20) + 1536;
		std::string base(size, '\0');
		for (std::uint32_t at = 0; at < size; ++at) base[at] = static_cast<char>(at * 7 + (at >> 11));
		Sha256 hash;
		hash.Update(base.data(), base.size());
		mister::AtaDiskImage disk{images.File("disk.img", base), size, Sha256Hex(hash.Final()), media.path, 4, 16, 63};
		const MediaDataIdentity identity{"fes.pc386", "ultima-vi", disk.base_media_id, 0};
		std::size_t poisoned = 0;
		if (kind == "ambiguous release") {
			endpoint.fail_after_request = [&](const mister_test::ComputerEndpoint::Request& request) {
				if (poisoned || request.opcode != FesComputerOpcodeExecution || request.argument != FesComputerExecutionRelease)
					return false;
				poisoned = endpoint.requests.size();
				return true;
			};
			assert(!fixture.runtime.LoadLibraryCore(package.path, opened.package_id, data.path, "ultima-vi", &disk).ok());
		} else {
			assert(fixture.runtime.LoadLibraryCore(package.path, opened.package_id, data.path, "ultima-vi",
				kind == "bound disk" ? &disk : nullptr).ok());
			const auto generation = fixture.runtime.status().generation;
			if (kind == "bound disk") endpoint.GuestSectorWrite(0, 0x11);
			endpoint.GuestCmosWrite(0x40, 0x6b);
			endpoint.fail_after_request = [&](const mister_test::ComputerEndpoint::Request& request) {
				if (poisoned || request.opcode != FesComputerOpcodeKeyboardHid) return false;
				poisoned = endpoint.requests.size();
				return true;
			};
			assert(!fixture.runtime.SetKeyboardHid(opened.package_id, generation, mister::KeyboardHidRows{}).ok());
		}
		if (!WaitForState(fixture.runtime, mister::State::idle)) {
			const auto failed = fixture.runtime.status();
			std::fprintf(stderr, "pc386 %s fault state=%u error=%s\n", kind.c_str(),
				static_cast<unsigned>(failed.state), failed.error.message.c_str());
			assert(false);
		}
		endpoint.fail_after_request = {};
		assert(poisoned && !endpoint.disk_frozen);
		bool froze = false, saved_disk = false;
		for (std::size_t at = poisoned; at < endpoint.requests.size(); ++at) {
			const auto& request = endpoint.requests[at];
			froze = froze || (request.opcode == FesComputerOpcodeDiskControl && request.argument == FesComputerDiskControlFreeze);
			saved_disk = saved_disk || (request.opcode == FesComputerOpcodeDiskControl && request.argument == FesComputerDiskControlSaved);
		}
		assert(froze == (kind != "empty disk") && saved_disk == froze);
		std::unique_ptr<AtaDeltaFile> file;
		assert(AtaDeltaFile::Open(media.path, identity, size, &file).ok());
		AtaDiskDelta saved;
		assert(file->Read(&saved).ok());
		assert(saved.chunks.size() == (kind == "bound disk" ? 1u : 0u));
		mister::CoreData record;
		assert(fixture.runtime.InspectCoreData(package.path, opened.package_id, data.path, "ultima-vi", &record).ok());
		if (kind == "ambiguous release")
			assert(record.revision == "absent");
		else
			assert(record.revision != "absent" && record.words[8 + 0x20] == 0x6b);
		file.reset();
		const std::string ns = media.path + "/" + MediaDataNamespace(identity);
		unlink((ns + "/record.bin").c_str());
		for (const auto& entry : saved.chunks) assert(unlink((ns + "/chunk-" + entry.sha256).c_str()) == 0);
		rmdir(ns.c_str());
		const std::string games = data.path + "/games/";
		const std::string game = games + GameDataNamespace("fes.pc386", "ultima-vi");
		unlink((game + "/record.bin").c_str());
		assert(rmdir(game.c_str()) == 0 && rmdir(games.c_str()) == 0);
	}
}

// Pong progress keeps Stop as its only durability boundary: an input fault
// on a persistent library launch idles without capturing the progress.
void TestPongInputFaultDoesNotCapture()
{
	std::vector<std::string> events;
	RecordingDriver gp(events);
	IntegratedFixture fixture(&gp);
	fixture.Start();
	TempDirectory package, data;
	const auto id = PersistentPackage(&package);
	assert(fixture.runtime.LoadLibraryCore(package.path, id, data.path).ok());
	assert(fixture.runtime.status().core_data.mode == "persistent");
	const auto generation = fixture.runtime.status().generation;
	gp.live_data = {2, 42};
	events.clear();
	gp.Report(generation, {mister::ErrorCode::io_failed, "pong input fault", "input"});
	assert(WaitForState(fixture.runtime, mister::State::idle));
	assert(Find(events, "driver.capture") == events.size());
	mister::CoreData inspected;
	assert(fixture.runtime.InspectCoreData(package.path, id, data.path, "", &inspected).ok());
	assert(inspected.revision == "absent");
	assert(rmdir((data.path + "/" + mister::native::CoreDataNamespace("fes.pong")).c_str()) == 0);
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
	assert(fixture.runtime.InspectCoreData(a.path, aid, data.path, "", &preflight).ok());
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
	assert(fixture.runtime.InspectCoreData(b.path, bid, data.path, "", &inspected).ok());
	assert(inspected.paddle_speed == 2 && inspected.best_rally == 17);
	assert(fixture.runtime
			   .UpdateCoreSettings(b.path, bid, data.path, "", inspected.revision, PaddleSpeed(0), &inspected)
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
	assert(fixture.runtime.InspectCoreData(b.path, bid, data.path, "", &inspected).ok() &&
		   inspected.best_rally == 17);
	gp.live_data = {2, 25};
	assert(fixture.runtime.Stop().ok());
	assert(fixture.runtime.InspectCoreData(b.path, bid, data.path, "", &inspected).ok() &&
		   inspected.best_rally == 25);
	assert(
		fixture.runtime.UpdateCoreSettings(b.path, bid, data.path, "", "absent", PaddleSpeed(0), &inspected).code ==
		mister::ErrorCode::stale_revision);
	auto revision = inspected.revision;
	assert(
		fixture.runtime.UpdateCoreSettings(b.path, bid, data.path, "", revision, PaddleSpeed(0), &inspected).ok() &&
		inspected.best_rally == 25 && inspected.paddle_speed == 0);
	TempDirectory old;
	PopulateFesGpPackage(&old);
	assert(fixture.runtime
			   .InspectCoreData(old.path,
				   "b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0", data.path, "",
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
	assert(fixture.runtime.UpdateCoreSettings(package.path, id, data.path, "", "absent", PaddleSpeed(0), &inspected)
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
				   "b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0", data.path, "",
				   &inspected)
			   .code == mister::ErrorCode::incompatible_data);
	gp.live_data = {2, 17};
	assert(fixture.runtime.Stop().ok());
	assert(fixture.runtime.InspectCoreData(package.path, id, data.path, "", &inspected).ok());
	auto revision = inspected.revision;
	assert(fixture.runtime.LoadCore(package.path, id).ok());
	assert(fixture.runtime.status().core_data.mode == "volatile");
	gp.live_data = {0, 99};
	assert(fixture.runtime.Stop().ok());
	assert(fixture.runtime.InspectCoreData(package.path, id, data.path, "", &inspected).ok() &&
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

void TestFormat3InspectionAndLoadGateBeforeMutation()
{
	std::vector<std::string> driver_events;
	RecordingDriver driver(driver_events);
	IntegratedFixture fixture(&driver);
	fixture.Start();
	TempDirectory outgoing;
	PopulateFesGpPackage(&outgoing);
	mister::native::OpenedCorePackage base;
	assert(mister::native::OpenCorePackage(outgoing.path, "", &base).ok());
	assert(fixture.runtime.LoadCore(outgoing.path, base.package_id).ok());
	const auto before = fixture.runtime.status();
	fixture.native.events.clear();
	driver_events.clear();
	TempDirectory package;
	const std::string map = "{}\n";
	mister::native::Sha256 hash;
	hash.Update(map.data(), map.size());
	std::string manifest = base.manifest_bytes;
	ReplaceAll(&manifest, "format = 2", "format = 3");
	manifest += "\n[rom]\nid = \"bios.main\"\nrole = \"firmware\"\nsource_size = 8192\n"
		"file = \"rom-map.json\"\nsize = 3\nsha256 = \"" +
		mister::native::Sha256Hex(hash.Final()) + "\"\n";
	package.File("manifest.toml", manifest);
	package.File("core.rbf", ReadText(outgoing.path + "/core.rbf"));
	package.File("rom-map.json", map);
	mister::native::OpenedCorePackage opened;
	assert(mister::native::OpenCorePackage(package.path, "", &opened).ok());
	mister::CorePackageInspection inspection;
	assert(fixture.runtime.InspectCore(package.path, opened.package_id, &inspection).ok());
	assert(inspection.descriptor.format == 3 && inspection.compatible);
	assert(inspection.compatibility_error.ok());
	assert(fixture.runtime.LoadCore(package.path, opened.package_id).code == mister::ErrorCode::unsupported_abi);
	assert(fixture.runtime.LoadInitializedCore(package.path, opened.package_id,
		outgoing.path + "/core.rbf", base.descriptor.payload.sha256).code == mister::ErrorCode::unsupported_abi);
	assert(driver_events.empty());
	assert(std::all_of(fixture.native.events.begin(), fixture.native.events.end(),
		[](const std::string& event) { return event.find("artifact.open:") == 0; }));
	assert(fixture.runtime.status().generation == before.generation);
	assert(fixture.runtime.status().active_package.package_id == before.active_package.package_id);
	mister::CoreROMLink link;
	link.rom_id = opened.descriptor.rom.id;
	link.map_sha256 = opened.descriptor.rom.sha256;
	link.source_sha256 = std::string(64, 'a');
	link.source_size = opened.descriptor.rom.source_size;
	link.programmed_sha256 = base.descriptor.payload.sha256;
	link.programmed_size = base.descriptor.payload.size;
	const std::string programmed = outgoing.path + "/core.rbf";
	for (unsigned mismatch = 0; mismatch < 6; ++mismatch) {
		auto bad = link;
		if (mismatch == 0) bad.rom_id = "other";
		if (mismatch == 1) bad.map_sha256 = std::string(64, 'b');
		if (mismatch == 2) bad.source_size++;
		if (mismatch == 3) bad.source_sha256 = std::string(64, 'A');
		if (mismatch == 4) bad.programmed_sha256 = std::string(64, 'b');
		if (mismatch == 5) bad.programmed_size++;
		assert(!fixture.runtime.LoadROMCore(package.path, opened.package_id, programmed, bad).ok());
		assert(driver_events.empty());
		assert(std::all_of(fixture.native.events.begin(), fixture.native.events.end(),
			[](const std::string& event) { return event.find("artifact.open:") == 0; }));
		assert(fixture.runtime.status().generation == before.generation);
	}
	assert(!fixture.runtime.LoadROMCore(outgoing.path, base.package_id, programmed, link).ok());
	assert(driver_events.empty());
	assert(std::all_of(fixture.native.events.begin(), fixture.native.events.end(),
		[](const std::string& event) { return event.find("artifact.open:") == 0; }));
	std::unique_ptr<mister::AdmittedCorePackage> unbound;
	assert(fixture.native.hardware.AdmitCorePackage(package.path, opened.package_id, &unbound).ok());
	assert(fixture.native.hardware.AttachProgrammedBitstream(unbound.get(), programmed, link.programmed_sha256).ok());
	const auto rejected = fixture.native.hardware.LoadCore(std::move(unbound), 99);
	assert(!rejected.error.ok() && !rejected.mutation_attempted);
	assert(driver_events.empty());
	assert(std::all_of(fixture.native.events.begin(), fixture.native.events.end(),
		[](const std::string& event) { return event.find("artifact.open:") == 0; }));
	assert(fixture.runtime.LoadROMCore(package.path, opened.package_id, programmed, link).ok());
	assert(fixture.runtime.status().active_package.rom_link.source_sha256 == link.source_sha256);
	assert(fixture.runtime.status().capabilities.rom_linking == 1);
	fixture.native.events.clear(); driver_events.clear();
	for (bool append : {false, true}) {
		TempDirectory linked;
		const auto linked_path = linked.File("linked.rbf", ReadText(programmed));
		std::unique_ptr<mister::AdmittedCorePackage> admitted;
		assert(fixture.native.hardware.AdmitCorePackage(package.path, opened.package_id, &admitted).ok());
		assert(fixture.native.hardware.AttachROMBitstream(admitted.get(), linked_path, link).ok());
		fixture.native.events.clear();
		int fd = open(linked_path.c_str(), O_WRONLY);
		assert(fd >= 0);
		assert(pwrite(fd, "!", 1, append ? link.programmed_size : 0) == 1);
		assert(close(fd) == 0);
		const auto result = fixture.native.hardware.LoadCore(std::move(admitted), 99);
		assert(!result.error.ok() && !result.mutation_attempted);
		assert(driver_events.empty());
		assert(std::all_of(fixture.native.events.begin(), fixture.native.events.end(),
			[](const std::string& event) { return event.find("artifact.open:") == 0; }));
	}
	assert(fixture.runtime.Stop().ok());
	assert(fixture.runtime.status().active_package.rom_link.rom_id.empty());
}

void TestFormat4TwoSourceAdmissionBeforeMutation()
{
	std::vector<std::string> driver_events;
	RecordingDriver driver(driver_events);
	IntegratedFixture fixture(&driver);
	fixture.Start();
	TempDirectory base;
	PopulateFesGpPackage(&base);
	mister::native::OpenedCorePackage opened_base;
	assert(mister::native::OpenCorePackage(base.path, "", &opened_base).ok());
	const std::string map = "{}\n";
	mister::native::Sha256 map_hash;
	map_hash.Update(map.data(), map.size());
	std::string manifest = opened_base.manifest_bytes;
	ReplaceAll(&manifest, "format = 2", "format = 4");
	manifest += "\n[[roms]]\nid = \"coleco-bios\"\nrole = \"firmware\"\nsource_size = 1024\nsource_offset = 0\n"
		"\n[[roms]]\nid = \"coleco-cart\"\nrole = \"cartridge\"\nsource_size = 1024\nsource_offset = 1024\n"
		"\n[rom_map]\nfile = \"rom-map.json\"\nsize = 3\nsha256 = \"" +
		mister::native::Sha256Hex(map_hash.Final()) + "\"\n";
	TempDirectory package;
	package.File("manifest.toml", manifest);
	package.File("core.rbf", ReadText(base.path + "/core.rbf"));
	package.File("rom-map.json", map);
	mister::native::OpenedCorePackage opened;
	assert(mister::native::OpenCorePackage(package.path, "", &opened).ok());
	assert(opened.descriptor.format == 4 && opened.descriptor.roms.size() == 2);
	assert(fixture.runtime.LoadCore(package.path, opened.package_id).code == mister::ErrorCode::unsupported_abi);
	assert(fixture.runtime.LoadInitializedCore(package.path, opened.package_id,
		base.path + "/core.rbf", opened_base.descriptor.payload.sha256).code == mister::ErrorCode::unsupported_abi);
	assert(driver_events.empty());
	mister::CoreROMLinks links;
	links.map_sha256 = opened.descriptor.rom_map.sha256;
	links.programmed_sha256 = opened_base.descriptor.payload.sha256;
	links.programmed_size = opened_base.descriptor.payload.size;
	links.sources = {{"coleco-bios", "firmware", std::string(64, 'a'), 1024},
		{"coleco-cart", "cartridge", std::string(64, 'b'), 1024}};
	const std::string programmed = base.path + "/core.rbf";
	// Exercise the actual production adapter as well as the native test fixture.
	// Admission and attachment retain files without touching physical devices.
	mister_test::CaptureLog production_log;
	std::unique_ptr<mister::Hardware> production;
	assert(CreateKnownProductionHardware(production_log, &production).ok());
	std::unique_ptr<mister::AdmittedCorePackage> retained;
	assert(fixture.native.hardware.AdmitCorePackage(package.path, opened.package_id, &retained).ok());
	assert(production->AttachROMsBitstream(retained.get(), programmed, links).ok());
	assert(production->RecheckProgrammedBitstream(retained.get()).ok());
	for (unsigned mismatch = 0; mismatch < 6; ++mismatch) {
		auto bad = links;
		if (mismatch == 0) bad.sources.pop_back();
		if (mismatch == 1) bad.sources[0].id = "wrong";
		if (mismatch == 2) bad.sources[1].source_size++;
		if (mismatch == 3) bad.sources[0].source_sha256 = std::string(64, 'A');
		if (mismatch == 4) bad.map_sha256 = std::string(64, 'c');
		if (mismatch == 5) bad.programmed_sha256 = std::string(64, 'd');
		assert(!fixture.runtime.LoadROMsCore(package.path, opened.package_id, programmed, bad).ok());
		assert(driver_events.empty());
	}
	assert(fixture.runtime.LoadROMsCore(package.path, opened.package_id, programmed, links).ok());
	assert(fixture.runtime.status().active_package.rom_links.sources.size() == 2);
	assert(fixture.runtime.Stop().ok());
}

std::string ComputerManifest()
{
	std::string manifest = ReadText("tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
	ReplaceAll(&manifest, "fes.simple-game", "fes.computer");
	ReplaceAll(&manifest, "id = \"fes.gamepad\"", "id = \"fes.keyboard.hid\"");
	for (const auto* id : {"fes.gamepad.ports", "fes.audio.pcm-s16-stereo-48k", "fes.media.apple2-floppy"})
		manifest += std::string("\n[[interfaces]]\nid = \"") + id +
			"\"\nmajor = 1\nminor = 0\nrequired = true\n";
	manifest += "\n[[interfaces]]\nid = \"fes.expansion.apple2-bus\"\nmajor = 1\nminor = 0\nrequired = false\n";
	return manifest;
}

void AssertComputerABIVersions(const mister::Capabilities& capabilities)
{
	bool computer_10 = false;
	bool computer_11 = false;
	for (const auto& abi : capabilities.abis) {
		if (abi.id != "fes.computer" || abi.major != 1) continue;
		computer_10 = computer_10 || abi.minor == 0;
		computer_11 = computer_11 || abi.minor == 1;
	}
	assert(computer_10 && computer_11);
}

std::string ComputerDisk(std::size_t size)
{
	std::string bytes(size, '\0');
	for (std::size_t i = 0; i < size; ++i)
		bytes[i] = static_cast<char>((i * 13u + (i >> 9)) & 0xffu);
	return bytes;
}

void TestGeometryDiskBaseAdmissionAndRestoredLength()
{
    using namespace mister::native;using namespace mister::native::generated;
    for(bool extension:{false,true}) {
        const auto caps=31-16+FesComputerCapabilityMediaAtariStFloppy+FesComputerCapabilityMediaAtariStFloppyWrite+(extension?FesComputerCapabilityMediaAtariStFloppyGeometry:0);
        mister_test::ComputerEndpoint endpoint(caps,{{0,{extension?368640u:737280u,extension?839680u:737280u}}},"0123456789abcdef0123456789abcdef");
        FixedClock clock(100);FesGp transport(endpoint,clock);FesGpCoreDriver driver(transport);
        IntegratedFixture fixture(&driver);fixture.Start();fixture.native.fpga.on_program=[&]{endpoint.Reset();};
        TempDirectory package,media,storage;
        auto manifest=ComputerManifest();ReplaceAll(&manifest,"fes.pong","fes.atari-st");ReplaceAll(&manifest,"fes.media.apple2-floppy","fes.media.atari-st-floppy");ReplaceAll(&manifest,"fes.expansion.apple2-bus","fes.expansion.atari-st-bus");
        manifest+="\n[[interfaces]]\nid = \"fes.media.atari-st-floppy-write\"\nmajor = 1\nminor = 0\nrequired = true\n";
        if(extension) manifest+="\n[[interfaces]]\nid = \"fes.media.atari-st-floppy-geometry\"\nmajor = 1\nminor = 0\nrequired = true\n";
        package.File("manifest.toml",manifest);package.File("core.rbf",ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
        OpenedCorePackage opened;assert(OpenCorePackage(package.path,"",&opened).ok());
        assert(fixture.runtime.LoadCore(package.path,opened.package_id).ok());const auto generation=fixture.runtime.status().generation;
        AssertComputerABIVersions(fixture.runtime.status().capabilities);
        if (!extension) RecordLiveComputerStatus(fixture.runtime.status());
        auto bytes=ComputerDisk(409600);
        const auto word=[&](unsigned at,unsigned value){bytes[at]=value;bytes[at+1]=value>>8;};
        word(11,512);word(19,800);word(24,10);word(26,1);
        const auto path=media.File("disk.st",bytes);
        Sha256 hash;hash.Update(bytes.data(),bytes.size());
        mister::MediaDataBinding binding{"atari-st-demo",Sha256Hex(hash.Final()),0};
        MediaDataIdentity identity{"fes.atari-st",binding.game_id,binding.base_media_id,0};
        const auto before=endpoint.requests.size();
        auto error=fixture.runtime.InsertLibraryMedia(path,opened.package_id,generation,0,409600,storage.path,binding);
        assert(error.ok()==extension);
        if(!extension) {for(std::size_t i=before;i<endpoint.requests.size();++i) assert(endpoint.requests[i].opcode!=FesComputerOpcodeMediaBegin);continue;}
        assert(endpoint.unit(0).data.size()==409600);
        // Invalid immutable boot BPB fails without replacing/saving the guest disk.
        auto bad=bytes;bad[24]=9;const auto badpath=media.File("bad.st",bad);
        const auto stable=endpoint.requests.size();
        assert(!fixture.runtime.InsertMedia(badpath,opened.package_id,generation,0,409600).ok());
        assert(endpoint.requests.size()==stable);
        // Persist a changed BPB: physical geometry remains determined by byte length.
        endpoint.unit(0).data[24]=0;endpoint.dirty=true;
        assert(fixture.runtime.SaveMedia(opened.package_id,generation,0).ok());
        assert(fixture.runtime.InsertLibraryMedia(path,opened.package_id,generation,0,409600,storage.path,binding).ok());
        assert(endpoint.unit(0).data[24]==0);
        assert(fixture.runtime.EjectMedia(opened.package_id,generation,0).ok());
        std::unique_ptr<MediaDataFile> file;assert(MediaDataFile::Open(storage.path,identity,&file).ok());
        MediaDiskRecord record;assert(file->Read(&record).ok());record.bytes.resize(839680);
        assert(file->Persist(record,record.revision,nullptr).ok());
        const auto untouched=endpoint.requests.size();
        assert(fixture.runtime.InsertLibraryMedia(path,opened.package_id,generation,0,409600,storage.path,binding).code==mister::ErrorCode::incompatible_data);
        assert(endpoint.requests.size()==untouched);
        file.reset();const auto ns=storage.path+"/"+MediaDataNamespace(identity);
        assert(unlink((ns+"/record.bin").c_str())==0);assert(rmdir(ns.c_str())==0);
    }
}

void TestWritableLibraryDiskCaptureRestoreAndFailedSave()
{
    using namespace mister::native;using namespace mister::native::generated;
    mister_test::ComputerEndpoint endpoint(31-16+FesComputerCapabilityMediaAtariStFloppy+FesComputerCapabilityMediaAtariStFloppyWrite,
        {{0,{737280,737280}}},"0123456789abcdef0123456789abcdef");
    FixedClock clock(100);FesGp transport(endpoint,clock);FesGpCoreDriver driver(transport);
    IntegratedFixture fixture(&driver);fixture.Start();fixture.native.fpga.on_program=[&]{endpoint.Reset();};
    TempDirectory package,media,storage;
    std::string manifest=ComputerManifest();ReplaceAll(&manifest,"fes.pong","fes.atari-st");ReplaceAll(&manifest,"fes.media.apple2-floppy","fes.media.atari-st-floppy");ReplaceAll(&manifest,"fes.expansion.apple2-bus","fes.expansion.atari-st-bus");
    manifest+="\n[[interfaces]]\nid = \"fes.media.atari-st-floppy-write\"\nmajor = 1\nminor = 0\nrequired = true\n";
    package.File("manifest.toml",manifest);package.File("core.rbf",ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
    OpenedCorePackage opened;assert(OpenCorePackage(package.path,"",&opened).ok());
    const std::string original=ComputerDisk(737280),path=media.File("disk.st",original);Sha256 hash;hash.Update(original.data(),original.size());
    mister::MediaDataBinding binding;binding.game_id="atari-st-desktop";binding.base_media_id=Sha256Hex(hash.Final());binding.unit=0;
    MediaDataIdentity identity{"fes.atari-st",binding.game_id,binding.base_media_id,0};
    std::unique_ptr<MediaDataFile> record;assert(MediaDataFile::Open(storage.path,identity,&record).ok());
    assert(fixture.runtime.LoadCore(package.path,opened.package_id).ok());auto gen=fixture.runtime.status().generation;
    // Busy replacement is a completed rejection. The guest may finish its
    // write before any cleanup could run; never eject the unchanged raw disk.
    assert(fixture.runtime.InsertMedia(path,opened.package_id,gen,0,737280).ok());
    const auto old_bytes=endpoint.unit(0).data;
    auto replacement=original;replacement[1234]^=0x5a;
    const auto replacement_path=media.File("replacement.st",replacement);
    endpoint.write_busy=true;bool rejected_begin=false;
    endpoint.fail_after_request=[&](const mister_test::ComputerEndpoint::Request& request){
        if(request.opcode==FesComputerOpcodeMediaBegin&&request.index==0){
            rejected_begin=true;endpoint.write_busy=false;
        }
        return false;
    };
    const auto rejected_at=endpoint.requests.size();
    const auto rejected_insert=fixture.runtime.InsertMedia(replacement_path,opened.package_id,gen,0,737280);
    assert(rejected_insert.message=="FES GP command rejected with response 4");
    endpoint.fail_after_request={};
    assert(rejected_begin&&endpoint.requests.size()==rejected_at+6+1);
    assert(endpoint.requests.back().opcode==FesComputerOpcodeMediaBegin);
    assert(endpoint.unit(0).state==3&&endpoint.unit(0).data==old_bytes&&!endpoint.held);
    auto unchanged=fixture.runtime.status();
    assert(unchanged.generation==gen&&unchanged.active_package.package_id==opened.package_id);
    assert(unchanged.capabilities.media_units[0].state==mister::MediaUnitState::ready);
    assert(unchanged.capabilities.media_units[0].persistence_mode=="volatile");
    endpoint.write_busy=true;const auto eject_at=endpoint.requests.size();
    const auto rejected_eject=fixture.runtime.EjectMedia(opened.package_id,gen,0);
    assert(rejected_eject.code==mister::ErrorCode::busy&&rejected_eject.message=="media unit eject is busy");
    assert(endpoint.requests.size()==eject_at+1&&endpoint.unit(0).data==old_bytes&&endpoint.unit(0).state==3);
    assert(fixture.runtime.status().generation==gen&&fixture.runtime.status().capabilities.media_units[0].state==mister::MediaUnitState::ready);
    endpoint.write_busy=false;
    assert(fixture.runtime.InsertMedia(replacement_path,opened.package_id,gen,0,737280).ok());
    assert(std::string(endpoint.unit(0).data.begin(),endpoint.unit(0).data.end())==replacement);
    auto invalid=binding;invalid.base_media_id=std::string(64,'a');const auto before=endpoint.requests.size();
    assert(!fixture.runtime.InsertLibraryMedia(path,opened.package_id,gen,0,737280,storage.path,invalid).ok());assert(endpoint.requests.size()==before);
    assert(fixture.runtime.InsertLibraryMedia(path,opened.package_id,gen,0,737280,storage.path,binding).ok());
    assert(fixture.runtime.status().capabilities.media_units[0].persistence_mode=="persistent");
    // A clean first insertion is still frozen and captured before destruction.
    auto changed=endpoint.unit(0).data;changed[512]^=0x5a;changed[737279]^=0xe3;endpoint.unit(0).data=changed;endpoint.dirty=false;
    const auto capture_start=endpoint.requests.size();assert(fixture.runtime.SaveMedia(opened.package_id,gen,0).ok());
    assert(endpoint.requests[capture_start].opcode==FesComputerOpcodeMediaSnapshotControl&&endpoint.requests[capture_start].argument==FesComputerMediaSnapshotFreeze);
    assert(!endpoint.frozen&&!endpoint.held&&endpoint.unit(0).data==changed);
    MediaDiskRecord saved;assert(record->Read(&saved).ok()&&saved.bytes==changed&&saved.revision!="absent");
    const auto frozen_end=endpoint.requests.size();assert(fixture.runtime.EjectMedia(opened.package_id,gen,0).ok());
    assert(endpoint.unit(0).state==1&&fixture.runtime.status().core_data.mode=="volatile");
    for(std::size_t at=frozen_end;at<endpoint.requests.size();++at)
        assert(!(endpoint.requests[at].opcode==FesComputerOpcodeMediaSnapshotControl&&endpoint.requests[at].argument==FesComputerMediaSnapshotResume));
    assert(fixture.runtime.InsertLibraryMedia(path,opened.package_id,gen,0,737280,storage.path,binding).ok());assert(endpoint.unit(0).data==changed);

    // A completed Eject rejection changes no image. Resume and observe the
    // same bound disk; a response-lost accepted Eject instead retires it.
    bool rejected=false;
    endpoint.lose_request=[&](const mister_test::ComputerEndpoint::Request& request){
        if(request.opcode==FesComputerOpcodeMediaEject&&!rejected){endpoint.saved=false;rejected=true;}
        return false;
    };
    assert(fixture.runtime.EjectMedia(opened.package_id,gen,0).code==mister::ErrorCode::save_failed);
    endpoint.lose_request={};assert(rejected&&!endpoint.frozen&&endpoint.unit(0).data==changed);
    assert(fixture.runtime.status().core_data.mode=="persistent"&&fixture.runtime.status().generation==gen);
    bool lost=false;endpoint.fail_after_request=[&](const mister_test::ComputerEndpoint::Request& request){
        if(request.opcode==FesComputerOpcodeMediaEject&&!lost){lost=true;return true;}return false;
    };
    assert(fixture.runtime.EjectMedia(opened.package_id,gen,0).code==mister::ErrorCode::save_failed);
    endpoint.fail_after_request={};assert(lost&&endpoint.unit(0).state==1);
    assert(fixture.runtime.status().core_data.mode=="volatile"&&fixture.runtime.status().capabilities.media_units[0].persistence_mode=="volatile");
    assert(fixture.runtime.InsertLibraryMedia(path,opened.package_id,gen,0,737280,storage.path,binding).ok());

    // A lost read response before Begin never changed the image. Neither raw
    // nor library replacement may eject it or drop its binding; recovery
    // realigns the mailbox and resumes that same owned disk.
    for(bool library:{false,true}) {
        bool failed=false,unexpected_eject=false;
        endpoint.fail_after_request=[&](const mister_test::ComputerEndpoint::Request& request){
            if(request.opcode==FesComputerOpcodeMediaInfo&&!failed){failed=true;return true;}return false;
        };
        endpoint.lose_request=[&](const mister_test::ComputerEndpoint::Request& request){
            if(request.opcode==FesComputerOpcodeMediaEject){unexpected_eject=true;}
            return false;
        };
        const auto error=library?fixture.runtime.InsertLibraryMedia(path,opened.package_id,gen,0,737280,storage.path,binding):fixture.runtime.InsertMedia(path,opened.package_id,gen,0,737280);
        endpoint.fail_after_request={};endpoint.lose_request={};
        assert(error.code==mister::ErrorCode::save_failed&&failed&&!unexpected_eject);
        assert(fixture.runtime.status().state==mister::State::running_development&&fixture.runtime.status().generation==gen);
        assert(fixture.runtime.status().capabilities.media_units[0].game_id==binding.game_id&&!endpoint.frozen&&endpoint.unit(0).data==changed);
    }

    // Directory sync can fail after a complete rename. Retain the visible
    // revision without claiming durability, then admit new guest writes on
    // an explicit retry rather than calling our own revision concurrent.
#if defined(__linux__)
    endpoint.unit(0).data[3000]^=0x21;
    bool armed=false;endpoint.lose_request=[&](const mister_test::ComputerEndpoint::Request& request){
        if(request.opcode==FesComputerOpcodeMediaSnapshotControl&&request.argument==FesComputerMediaSnapshotFreeze&&!armed){fail_media_directory_sync=true;armed=true;}
        return false;
    };
    assert(fixture.runtime.SaveMedia(opened.package_id,gen,0).code==mister::ErrorCode::save_failed);
    endpoint.lose_request={};assert(armed&&!fail_media_directory_sync.load()&&!endpoint.frozen);
    assert(record->Read(&saved).ok()&&saved.bytes==endpoint.unit(0).data);
    assert(fixture.runtime.status().capabilities.media_units[0].revision==saved.revision);
    endpoint.unit(0).data[3001]^=0x42;
    assert(fixture.runtime.SaveMedia(opened.package_id,gen,0).ok());
    changed=endpoint.unit(0).data;assert(record->Read(&saved).ok()&&saved.bytes==changed);
#endif

    // A healthy incoming record can change after preflight while the outgoing
    // disk is captured. Failure before Begin must resume the old owned disk.
    auto other_id=identity;other_id.game_id="st-other";auto other_binding=binding;other_binding.game_id=other_id.game_id;
    std::unique_ptr<MediaDataFile> other_file;assert(MediaDataFile::Open(storage.path,other_id,&other_file).ok());
    MediaDiskRecord other;other.identity=other_id;other.bytes=changed;assert(other_file->Persist(other,"absent",&other).ok());
    const std::string other_ns=storage.path+"/"+MediaDataNamespace(other_id);
    bool corrupted=false;endpoint.lose_request=[&](const mister_test::ComputerEndpoint::Request& request){
        if(request.opcode==FesComputerOpcodeMediaSnapshotControl&&request.argument==FesComputerMediaSnapshotSaved&&!corrupted){
            std::ofstream bad(other_ns+"/record.bin",std::ios::binary|std::ios::trunc);bad<<"corrupt";bad.close();corrupted=true;
        }
        return false;
    };
    assert(fixture.runtime.InsertLibraryMedia(path,opened.package_id,gen,0,737280,storage.path,other_binding).code==mister::ErrorCode::save_failed);
    endpoint.lose_request={};assert(corrupted&&!endpoint.frozen&&!endpoint.held&&endpoint.unit(0).data==changed);
    assert(fixture.runtime.status().generation==gen&&fixture.runtime.status().capabilities.media_units[0].game_id==binding.game_id);
    other_file.reset();assert(unlink((other_ns+"/record.bin").c_str())==0);assert(rmdir(other_ns.c_str())==0);
    // Another writer changes the durable revision. Capture must refuse to
    // overwrite it, preserve the current image and keep this generation.
    auto concurrent=saved;concurrent.bytes[1024]^=0x2e;assert(record->Persist(concurrent,saved.revision,&concurrent).ok());
    endpoint.unit(0).data[2048]^=0x87;const auto retained=endpoint.unit(0).data;
    assert(fixture.runtime.Stop().code==mister::ErrorCode::save_failed);
    assert(fixture.runtime.status().generation==gen&&fixture.runtime.status().state==mister::State::running_development);
    assert(endpoint.unit(0).data==retained&&!endpoint.frozen&&!endpoint.held);
    assert(fixture.runtime.status().capabilities.media_units[0].game_id==binding.game_id);
    // Restore the expected durable record, then Stop publishes the retained
    // machine image before ExecutionHold and FPGA programming.
    assert(record->Persist(saved,concurrent.revision,&saved).ok());
    assert(fixture.runtime.Stop().ok());assert(record->Read(&saved).ok()&&saved.bytes==retained);
    assert(ReadText(path)==original);
    assert(fixture.runtime.LoadCore(package.path,opened.package_id).ok());gen=fixture.runtime.status().generation;
    assert(fixture.runtime.InsertLibraryMedia(path,opened.package_id,gen,0,737280,storage.path,binding).ok());assert(endpoint.unit(0).data==retained);
    // Raw replacement is explicitly volatile after the outgoing disk saves.
    assert(fixture.runtime.InsertMedia(path,opened.package_id,gen,0,737280).ok());assert(fixture.runtime.status().capabilities.media_units[0].persistence_mode=="volatile");
    assert(fixture.runtime.Stop().ok());
    // If a replacement Commit actually succeeded but both its response and
    // cleanup failed, Ready can describe the new image. Never attach the old
    // durable namespace to those different bytes: retain ownership in recovery.
    assert(fixture.runtime.LoadCore(package.path,opened.package_id).ok());gen=fixture.runtime.status().generation;
    assert(fixture.runtime.InsertLibraryMedia(path,opened.package_id,gen,0,737280,storage.path,binding).ok());
    bool commit_lost=false,cleanup_failed=false;
    endpoint.fail_after_request=[&](const mister_test::ComputerEndpoint::Request& request){
        if(request.opcode==FesComputerOpcodeMediaCommit&&!commit_lost){commit_lost=true;return true;}return false;
    };
    endpoint.lose_request=[&](const mister_test::ComputerEndpoint::Request& request){
        if(request.opcode==FesComputerOpcodeMediaEject&&!cleanup_failed){endpoint.frozen=true;endpoint.saved=false;cleanup_failed=true;}
        return false;
    };
    assert(!fixture.runtime.InsertMedia(path,opened.package_id,gen,0,737280).ok());
    endpoint.fail_after_request={};endpoint.lose_request={};
    assert(commit_lost&&cleanup_failed&&fixture.runtime.status().state==mister::State::reboot_required);
    assert(fixture.runtime.status().generation==gen&&fixture.runtime.status().capabilities.media_units[0].game_id==binding.game_id);
    assert(endpoint.frozen&&record->Read(&saved).ok()&&saved.bytes==retained);
    const std::string ns=storage.path+"/"+MediaDataNamespace(identity);record.reset();assert(unlink((ns+"/record.bin").c_str())==0);assert(rmdir(ns.c_str())==0);
}

void TestInitialSTDiskBeforeExecution(unsigned size = 737280)
{
    using namespace mister::native;
    using namespace mister::native::generated;
    mister_test::ComputerEndpoint endpoint(15 | FesComputerCapabilityMediaAtariStFloppy |
        FesComputerCapabilityMediaAtariStFloppyWrite | (size == 737280 ? 0 : FesComputerCapabilityMediaAtariStFloppyGeometry),
        {{0, {size == 737280 ? 737280u : 368640u, size == 737280 ? 737280u : 839680u}}},
        "0123456789abcdef0123456789abcdef");
    FixedClock clock(100);
    FesGp gp(endpoint, clock);
    FesGpCoreDriver driver(gp);
    IntegratedFixture fixture(&driver);
    fixture.Start();
    // Computer controller ports do not use the legacy gamepad input worker.
    // A poisoned worker startup must therefore not affect any ST launch.
    fixture.native.input.start_error = {mister::ErrorCode::io_failed, "legacy input start must not run"};

    fixture.native.fpga.on_program = [&] { endpoint.Reset(); };
    TempDirectory package, media, storage;
    auto manifest = ComputerManifest();
    ReplaceAll(&manifest, "format = 2", "format = 3");
    ReplaceAll(&manifest, "fes.pong", "fes.atari-st");
    ReplaceAll(&manifest, "fes.media.apple2-floppy", "fes.media.atari-st-floppy");
    ReplaceAll(&manifest, "fes.expansion.apple2-bus", "fes.expansion.atari-st-bus");
    const std::string map = "{}\n";
    Sha256 map_hash; map_hash.Update(map.data(), map.size());
    manifest += "\n[[interfaces]]\nid = \"fes.media.atari-st-floppy-write\"\nmajor = 1\nminor = 0\nrequired = true\n"
        "\n[rom]\nid = \"bios.main\"\nrole = \"firmware\"\nsource_size = 8192\n"
        "file = \"rom-map.json\"\nsize = 3\nsha256 = \"" + Sha256Hex(map_hash.Final()) + "\"\n";
    if (size != 737280) manifest += "\n[[interfaces]]\nid = \"fes.media.atari-st-floppy-geometry\"\nmajor = 1\nminor = 0\nrequired = true\n";
    package.File("manifest.toml", manifest);
    const auto programmed = package.File("core.rbf", ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
    package.File("rom-map.json", map);
    OpenedCorePackage opened;
    assert(OpenCorePackage(package.path, "", &opened).ok());
    mister::CoreROMLink link;
    link.rom_id = opened.descriptor.rom.id;
    link.map_sha256 = opened.descriptor.rom.sha256;
    link.source_sha256 = std::string(64, 'a');
    link.source_size = opened.descriptor.rom.source_size;
    link.programmed_sha256 = opened.descriptor.payload.sha256;
    link.programmed_size = opened.descriptor.payload.size;
    auto original = ComputerDisk(size);
    if (size != 737280) {
        AtariStGeometry geometry; assert(InferAtariStGeometry(size, &geometry));
        const auto word = [&](unsigned at, unsigned value) { original[at]=value;original[at+1]=value>>8; };
        word(11,512);word(19,size/512);word(24,geometry.sectors);word(26,geometry.heads);
    }
    mister::InitialComputerMedia initial;
    initial.path = media.File("disk.st", original);
    initial.size = size;
    initial.data_root = storage.path;
    Sha256 source; source.Update(original.data(), original.size());
    initial.binding = {"st-initial", Sha256Hex(source.Final()), 0};
    mister_test::CaptureLog production_log;
    std::unique_ptr<mister::Hardware> production;
    assert(CreateKnownProductionHardware(production_log, &production).ok());
    std::unique_ptr<mister::AdmittedCorePackage> prepared;
    assert(fixture.native.hardware.AdmitCorePackage(package.path, opened.package_id, &prepared).ok());
    assert(production->PrepareInitialComputerMedia(prepared.get(), initial).ok());
    assert(production->RefreshInitialComputerMedia(prepared.get()).ok());
    prepared.reset();
    std::vector<std::uint8_t> expected(original.begin(), original.end());
    unsigned releases = 0, held_transfers = 0;
    endpoint.lose_request = [&](const mister_test::ComputerEndpoint::Request& request) {
        if (request.opcode >= FesComputerOpcodeMediaBegin && request.opcode <= FesComputerOpcodeMediaCommit) {
            assert(endpoint.held); ++held_transfers;
        }
        if (request.opcode == FesComputerOpcodeExecution && request.argument == FesComputerExecutionRelease) {
            assert(endpoint.held && endpoint.unit(0).state == FesComputerMediaStateReady);
            assert(endpoint.unit(0).data == expected); ++releases;
        }
        return false;
    };
    auto load = [&] { return fixture.runtime.LoadROMCore(package.path, opened.package_id, programmed, link, &initial); };
    // The source path can be removed after preflight: activation uses retained bytes.
    fixture.native.fpga.on_program = [&] { endpoint.Reset(); assert(unlink(initial.path.c_str()) == 0); };
    assert(load().ok());
    assert(releases == 1 && held_transfers > size / 2);
    assert(fixture.native.input.start_calls == 0 && fixture.native.input.open_calls == 0);
    assert(fixture.runtime.status().core_data.mode == "persistent");
    auto unit = fixture.runtime.status().capabilities.media_units[0];
    assert(unit.state == mister::MediaUnitState::ready && unit.persistence_mode == "persistent");
    assert(unit.game_id == initial.binding.game_id && unit.base_media_id == initial.binding.base_media_id);
    { std::ofstream restored_source(initial.path, std::ios::binary);
      restored_source.write(original.data(), original.size()); assert(restored_source.good()); }
    fixture.native.fpga.on_program = [&] { endpoint.Reset(); };
    MediaDataIdentity identity{"fes.atari-st", initial.binding.game_id, initial.binding.base_media_id, 0};
    std::unique_ptr<MediaDataFile> record;
    assert(MediaDataFile::Open(storage.path, identity, &record).ok());
    // Same namespace must refresh the incoming absent record after outgoing save.
    endpoint.unit(0).data[4096] ^= 0x91;
    expected = endpoint.unit(0).data;
    assert(load().ok() && releases == 2 && endpoint.unit(0).data == expected);
    MediaDiskRecord saved;
    assert(record->Read(&saved).ok() && saved.bytes == expected);
    const auto generation = fixture.runtime.status().generation;
    const auto programs = fixture.native.fpga.calls;
    for (unsigned failure = 0; failure < 5; ++failure) {
        auto bad = initial;
        if (failure == 0) bad.binding.base_media_id = std::string(64, 'b');
        if (failure == 1) bad.size--;
        if (failure == 2) bad.binding.unit = 1;
        if (failure == 3) bad.data_root = storage.path + "/../bad";
        if (failure == 4) bad.path = media.path + "/missing.st";
        assert(!fixture.runtime.LoadROMCore(package.path, opened.package_id, programmed, link, &bad).ok());
        assert(fixture.native.fpga.calls == programs && fixture.runtime.status().generation == generation);
        assert(endpoint.unit(0).data == expected && !endpoint.held);
    }
    const auto record_path = storage.path + "/" + MediaDataNamespace(identity) + "/record.bin";
    const auto canonical = ReadText(record_path);
    { std::ofstream corrupt(record_path, std::ios::binary | std::ios::trunc); corrupt << "broken"; }
    assert(!load().ok() && fixture.native.fpga.calls == programs);
    assert(fixture.runtime.status().generation == generation && endpoint.unit(0).data == expected);
    { std::ofstream repaired(record_path, std::ios::binary | std::ios::trunc); repaired.write(canonical.data(), canonical.size()); }
    // A read-only shell cannot accept this explicitly persistent initial disk.
    TempDirectory readonly;
    auto readonly_manifest = manifest;
    ReplaceAll(&readonly_manifest, "\n[[interfaces]]\nid = \"fes.media.atari-st-floppy-write\"\nmajor = 1\nminor = 0\nrequired = true\n", "\n");
    readonly.File("manifest.toml", readonly_manifest);
    readonly.File("core.rbf", ReadText(programmed)); readonly.File("rom-map.json", map);
    OpenedCorePackage read_only; assert(OpenCorePackage(readonly.path, "", &read_only).ok());
    assert(!fixture.runtime.LoadROMCore(readonly.path, read_only.package_id, programmed, link, &initial).ok());
    assert(fixture.native.fpga.calls == programs && fixture.runtime.status().generation == generation);
    // Requiring the legacy interface cannot make input_.Start reachable on ST:
    // computer admission rejects it before programming or disk ownership changes.
    TempDirectory legacy_gamepad;
    legacy_gamepad.File("manifest.toml", manifest +
        "\n[[interfaces]]\nid = \"fes.gamepad\"\nmajor = 1\nminor = 0\nrequired = true\n");
    legacy_gamepad.File("core.rbf", ReadText(programmed));
    legacy_gamepad.File("rom-map.json", map);
    OpenedCorePackage legacy_package;
    assert(OpenCorePackage(legacy_gamepad.path, "", &legacy_package).ok());
    const auto legacy_rejected = fixture.runtime.LoadROMCore(legacy_gamepad.path,
        legacy_package.package_id, programmed, link, &initial);
    assert(legacy_rejected.code == mister::ErrorCode::unsupported_interface);
    assert(fixture.native.fpga.calls == programs && fixture.runtime.status().generation == generation);
    assert(endpoint.unit(0).data == expected && fixture.native.input.start_calls == 0);
    // Failure during outgoing capture preserves its ownership and forbids programming.
    bool capture_failed = false;
    endpoint.fail_after_request = [&](const mister_test::ComputerEndpoint::Request& request) {
        if (!capture_failed && request.opcode == FesComputerOpcodeMediaSnapshotData) { capture_failed = true; return true; }
        return false;
    };
    assert(load().code == mister::ErrorCode::save_failed && capture_failed);
    endpoint.fail_after_request = {};
    assert(fixture.native.fpga.calls == programs && fixture.runtime.status().generation == generation);
    assert(fixture.runtime.status().capabilities.media_units[0].game_id == initial.binding.game_id);
    assert(endpoint.unit(0).data == expected);
    assert(fixture.runtime.Stop().ok());
    assert(load().ok() && releases == 3 && endpoint.unit(0).data == expected);
    assert(fixture.runtime.Stop().ok());
    // A partially accepted initial upload fails without releasing execution.
    bool upload_failed = false;
    endpoint.fail_after_request = [&](const mister_test::ComputerEndpoint::Request& request) {
        if (!upload_failed && request.opcode == FesComputerOpcodeMediaData) { upload_failed = true; return true; }
        return false;
    };
    const auto failed_upload = load();
    assert(!failed_upload.ok() && failed_upload.phase == "input");
    assert(upload_failed && releases == 3 && endpoint.held);
    endpoint.fail_after_request = {};
    assert(record->Read(&saved).ok() && saved.bytes == expected && ReadText(initial.path) == original);
    record.reset();
    assert(unlink(record_path.c_str()) == 0);
    assert(rmdir((storage.path + "/" + MediaDataNamespace(identity)).c_str()) == 0);
}

void TestInitialSTDiskAmbiguousReleaseSavesOrRetainsOwner()
{
    for (unsigned failure = 0; failure < 3; ++failure) {
        using namespace mister::native;
        using namespace mister::native::generated;
        mister_test::ComputerEndpoint endpoint(15 | FesComputerCapabilityMediaAtariStFloppy |
            FesComputerCapabilityMediaAtariStFloppyWrite, {{0, {737280, 737280}}},
            "0123456789abcdef0123456789abcdef");
        FixedClock clock(100);
        FesGp gp(endpoint, clock);
        FesGpCoreDriver driver(gp);
        IntegratedFixture fixture(&driver);
        fixture.Start();
        bool saved_before_idle = false;
        fixture.native.fpga.on_program = [&] {
            if (fixture.native.fpga.calls > 2) saved_before_idle = endpoint.saved;
            endpoint.Reset();
        };
        TempDirectory package, media, storage;
        auto manifest = ComputerManifest();
        ReplaceAll(&manifest, "format = 2", "format = 3");
        ReplaceAll(&manifest, "fes.pong", "fes.atari-st");
        ReplaceAll(&manifest, "fes.media.apple2-floppy", "fes.media.atari-st-floppy");
        ReplaceAll(&manifest, "fes.expansion.apple2-bus", "fes.expansion.atari-st-bus");
        const std::string map = "{}\n";
        Sha256 map_hash; map_hash.Update(map.data(), map.size());
        manifest += "\n[[interfaces]]\nid = \"fes.media.atari-st-floppy-write\"\nmajor = 1\nminor = 0\nrequired = true\n"
            "\n[rom]\nid = \"bios.main\"\nrole = \"firmware\"\nsource_size = 8192\n"
            "file = \"rom-map.json\"\nsize = 3\nsha256 = \"" + Sha256Hex(map_hash.Final()) + "\"\n";
        package.File("manifest.toml", manifest);
        const auto programmed = package.File("core.rbf", ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
        package.File("rom-map.json", map);
        OpenedCorePackage opened;
        assert(OpenCorePackage(package.path, "", &opened).ok());
        mister::CoreROMLink link;
        link.rom_id = opened.descriptor.rom.id;
        link.map_sha256 = opened.descriptor.rom.sha256;
        link.source_sha256 = std::string(64, 'a');
        link.source_size = opened.descriptor.rom.source_size;
        link.programmed_sha256 = opened.descriptor.payload.sha256;
        link.programmed_size = opened.descriptor.payload.size;
        const auto original = ComputerDisk(737280);
        mister::InitialComputerMedia initial;
        initial.path = media.File("disk.st", original);
        initial.size = 737280;
        initial.data_root = storage.path;
        Sha256 source; source.Update(original.data(), original.size());
        initial.binding = {"st-initial", Sha256Hex(source.Final()), 0};
        MediaDataIdentity identity{"fes.atari-st", initial.binding.game_id, initial.binding.base_media_id, 0};
        const auto ns = storage.path + "/" + MediaDataNamespace(identity);
        std::unique_ptr<MediaDataFile> record;
        // Preflight creates this namespace; verify the actual record after activation.
        unsigned release_requests = 0;
        bool snapshot_failed = false;
        std::vector<std::uint8_t> changed;
        endpoint.fail_after_request = [&](const mister_test::ComputerEndpoint::Request& request) {
            if (request.opcode == FesComputerOpcodeExecution && request.argument == FesComputerExecutionRelease) {
                ++release_requests;
                assert(!endpoint.held && endpoint.unit(0).state == FesComputerMediaStateReady);
                endpoint.unit(0).data[4096] ^= 0x91;
                endpoint.dirty = true;
                changed = endpoint.unit(0).data;
                // An unlinked open namespace permits Read(absent), but durable
                // publication cannot create its temporary record through that fd.
                if (failure == 2) assert(rmdir(ns.c_str()) == 0);
                return true; // command accepted; its acknowledgement is unavailable
            }
            if (request.opcode == FesComputerOpcodeMediaSnapshotControl &&
                request.argument == FesComputerMediaSnapshotFreeze) {
                const auto saving = fixture.runtime.status();
                assert(saving.state == mister::State::starting && saving.generation == 0);
                assert(saving.active_package.package_id.empty() && saving.capabilities.media_units.empty());
            }
            if (failure == 1 && !snapshot_failed && request.opcode == FesComputerOpcodeMediaSnapshotData) {
                snapshot_failed = true;
                return true;
            }
            return false;
        };
        const auto programs_before = fixture.native.fpga.calls;
        const auto result = fixture.runtime.LoadROMCore(package.path, opened.package_id, programmed, link, &initial);
        assert(!result.ok() && release_requests == 1); // no replay or second launch
        const auto state = fixture.runtime.status();
        if (failure == 0) {
            assert(state.state == mister::State::idle && result.phase == "transport");
            assert(saved_before_idle && fixture.native.fpga.calls == programs_before + 2);
            assert(MediaDataFile::Open(storage.path, identity, &record).ok());
            MediaDiskRecord saved; assert(record->Read(&saved).ok() && saved.bytes == changed);
            assert(saved.revision != "absent" && endpoint.unit(0).state == FesComputerMediaStateEmpty);
            record.reset();
            assert(unlink((ns + "/record.bin").c_str()) == 0);
        } else {
            assert(state.state == mister::State::reboot_required && result.phase == "recovery");
            assert(fixture.native.fpga.calls == programs_before + 1 && endpoint.unit(0).data == changed);
            assert(state.generation != 0 && state.package_id == opened.package_id);
            assert(state.active_package.rom_link.source_sha256 == link.source_sha256);
            assert(state.core_data.mode == "persistent" && state.capabilities.media_units.size() == 1);
            assert(state.capabilities.media_units[0].game_id == initial.binding.game_id);
            assert(state.capabilities.media_units[0].base_media_id == initial.binding.base_media_id);
            assert(state.capabilities.media_units[0].revision == "absent");
            assert(!fixture.runtime.Stop().ok());
            assert(fixture.runtime.RecoverIdle().code == mister::ErrorCode::save_failed);
            assert(!fixture.runtime.LoadROMCore(package.path, opened.package_id, programmed, link, &initial).ok());
            assert(fixture.native.fpga.calls == programs_before + 1 && endpoint.unit(0).data == changed);
            assert(snapshot_failed == (failure == 1));
        }
        assert(ReadText(initial.path) == original);
        if (failure != 2) assert(rmdir(ns.c_str()) == 0);
    }
}

void TestDiskBindingRetiresAfterProgramming()
{
	using namespace mister::native;
	using namespace mister::native::generated;
	for (bool menu_idle : {false, true}) {
		mister_test::ComputerEndpoint computer(15 | FesComputerCapabilityMediaAtariStFloppy |
			FesComputerCapabilityMediaAtariStFloppyWrite, {{0, {737280, 737280}}},
			"0123456789abcdef0123456789abcdef");
		mister_test::FakeMmio menu;
		class SelectedMmio final : public Mmio {
		public:
			SelectedMmio(Mmio& computer, Mmio& menu) : computer_(computer), menu_(menu) {}
			mister::Error Read32(std::uint32_t address, std::uint32_t* value) override
			{ return (menu_active ? menu_ : computer_).Read32(address, value); }
			mister::Error Write32(std::uint32_t address, std::uint32_t value) override
			{ return (menu_active ? menu_ : computer_).Write32(address, value); }
			bool menu_active = false;
		private:
			Mmio& computer_;
			Mmio& menu_;
		} mmio(computer, menu);
		FixedClock clock(100);
		FesGp gp(mmio, clock);
		FesGpCoreDriver driver(gp);
		MenuDisplayDriver display(gp, clock);
		MenuOperations operations;
		MenuWindow memory(operations);
		std::vector<std::string> events;
		RecordingOpener opener(events);
		RecordingFpga fpga(events);
		RecordingI2c i2c(events);
		RecordingVideo idle_video(events);
		LedgerLog log(events);
		FixedVideoBringup video(i2c, clock, log, Menu720p60Recipe());
		RecordingInput input(events, clock);
		TempDirectory splash, package, menu_package, media, storage;
		const auto idle = splash.File("idle.rbf", "idle");
		NativeHardware hardware(opener, fpga, idle_video, video, input, {"test", 0, 0, 0, 0},
			clock, log, idle, {30000, 10000, 10000}, &driver, {"/tmp"}, SplashIdle(), &display, &memory);
		mister::Runtime runtime(hardware, log);
		assert(runtime.Start().ok());
		bool next_menu = false;
		fpga.on_program = [&] { computer.Reset(); mmio.menu_active = next_menu; };
		MenuGpScript script{&menu};
		if (menu_idle) {
			OpenedCorePackage opened_menu;
			PopulateHpsDdrApplication(&menu_package, true, &opened_menu);
			auto manifest = ReadText(menu_package.path + "/manifest.toml");
			ReplaceAll(&manifest, "fes.pong", "fes.menu");
			manifest += "\n[[interfaces]]\nid = \"fes.video.menu-display\"\nmajor = 1\nminor = 0\nrequired = true\n";
			{ std::ofstream output(menu_package.path + "/manifest.toml"); output << manifest; assert(output.good()); }
			assert(OpenCorePackage(menu_package.path, "", &opened_menu).ok());
			next_menu = true;
			script.BringUp();
			assert(runtime.ConfigureMenuPackage(menu_package.path, opened_menu.package_id).ok());
		}
		auto manifest = ComputerManifest();
		ReplaceAll(&manifest, "fes.pong", "fes.atari-st");
		ReplaceAll(&manifest, "fes.media.apple2-floppy", "fes.media.atari-st-floppy");
		ReplaceAll(&manifest, "fes.expansion.apple2-bus", "fes.expansion.atari-st-bus");
		manifest += "\n[[interfaces]]\nid = \"fes.media.atari-st-floppy-write\"\nmajor = 1\nminor = 0\nrequired = true\n";
		package.File("manifest.toml", manifest);
		package.File("core.rbf", ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
		OpenedCorePackage opened;
		assert(OpenCorePackage(package.path, "", &opened).ok());
		const auto original = ComputerDisk(737280);
		const auto path = media.File("disk.st", original);
		Sha256 hash;
		hash.Update(original.data(), original.size());
		mister::MediaDataBinding binding;
		binding.game_id = "st-relaunch";
		binding.base_media_id = Sha256Hex(hash.Final());
		MediaDataIdentity identity{"fes.atari-st", binding.game_id, binding.base_media_id, 0};
		std::unique_ptr<MediaDataFile> record;
		assert(MediaDataFile::Open(storage.path, identity, &record).ok());
		auto load_fresh = [&] {
			if (mmio.menu_active) script.QuiesceRunningMenu();
			next_menu = false;
			assert(runtime.LoadCore(package.path, opened.package_id).ok());
			const auto status = runtime.status();
			assert(status.core_data.mode == "volatile" && status.capabilities.media_units.size() == 1);
			for (const auto& unit : hardware.capabilities().media_units) {
				assert(unit.state == mister::MediaUnitState::empty && unit.persistence_mode == "volatile");
				assert(unit.game_id.empty() && unit.base_media_id.empty() && unit.revision == "absent");
			}
			assert(status.capabilities.media_units[0].persistence_mode == "volatile");
			return status.generation;
		};
		auto stop = [&] {
			next_menu = menu_idle;
			if (menu_idle) script.BringUp();
			assert(runtime.Stop().ok());
			assert(runtime.status().capabilities.media_units.empty());
			assert(runtime.status().menu_display.available == menu_idle);
		};
		auto generation = load_fresh();
		assert(runtime.InsertLibraryMedia(path, opened.package_id, generation, 0, 737280, storage.path, binding).ok());
		computer.unit(0).data[4096] ^= 0x91;
		const auto changed = computer.unit(0).data;
		stop();
		MediaDiskRecord saved;
		assert(record->Read(&saved).ok() && saved.bytes == changed);
		// A normal Stop/relaunch in the same daemon starts with no binding;
		// only explicit insertion may restore the retained record.
		generation = load_fresh();
		assert(runtime.InsertLibraryMedia(path, opened.package_id, generation, 0, 737280, storage.path, binding).ok());
		assert(computer.unit(0).data == changed);
		// Replacement also retires the old binding after its successful save.
		generation = load_fresh();
		assert(runtime.InsertLibraryMedia(path, opened.package_id, generation, 0, 737280, storage.path, binding).ok());
		assert(computer.unit(0).data == changed);
		stop();
		assert(record->Read(&saved).ok() && saved.bytes == changed && ReadText(path) == original);
		record.reset();
		const auto directory = storage.path + "/" + MediaDataNamespace(identity);
		assert(unlink((directory + "/record.bin").c_str()) == 0);
		assert(rmdir(directory.c_str()) == 0);
	}
}

void TestWritableDiskInputFaultSavesBeforeIdleOrRetainsRAM()
{
    using namespace mister::native;using namespace mister::native::generated;
    for(bool capture_fails:{false,true}) {
        mister_test::ComputerEndpoint endpoint(15+FesComputerCapabilityMediaAtariStFloppy+FesComputerCapabilityMediaAtariStFloppyWrite,
            {{0,{737280,737280}}},"0123456789abcdef0123456789abcdef");
        FixedClock clock(100);FesGp transport(endpoint,clock);FesGpCoreDriver driver(transport);
        IntegratedFixture fixture(&driver);fixture.Start();fixture.native.fpga.on_program=[&]{endpoint.Reset();};
        TempDirectory package,media,storage;
        std::string manifest=ComputerManifest();ReplaceAll(&manifest,"fes.pong","fes.atari-st");ReplaceAll(&manifest,"fes.media.apple2-floppy","fes.media.atari-st-floppy");ReplaceAll(&manifest,"fes.expansion.apple2-bus","fes.expansion.atari-st-bus");
        manifest+="\n[[interfaces]]\nid = \"fes.media.atari-st-floppy-write\"\nmajor = 1\nminor = 0\nrequired = true\n";
        package.File("manifest.toml",manifest);package.File("core.rbf",ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
        OpenedCorePackage opened;assert(OpenCorePackage(package.path,"",&opened).ok());
        const std::string original=ComputerDisk(737280),path=media.File("disk.st",original);Sha256 hash;hash.Update(original.data(),original.size());
        mister::MediaDataBinding binding;binding.game_id="st-fault";binding.base_media_id=Sha256Hex(hash.Final());binding.unit=0;
        MediaDataIdentity identity{"fes.atari-st",binding.game_id,binding.base_media_id,0};
        std::unique_ptr<MediaDataFile> record;assert(MediaDataFile::Open(storage.path,identity,&record).ok());
        assert(fixture.runtime.LoadCore(package.path,opened.package_id).ok());const auto gen=fixture.runtime.status().generation;
        assert(fixture.runtime.InsertLibraryMedia(path,opened.package_id,gen,0,737280,storage.path,binding).ok());
        endpoint.unit(0).data[4096]^=0x91;endpoint.dirty=true;const auto changed=endpoint.unit(0).data;
        const auto programs=fixture.native.fpga.calls;
        bool input_failed=false,snapshot_failed=false;
        endpoint.fail_after_request=[&](const mister_test::ComputerEndpoint::Request& request){
            if(request.opcode==FesComputerOpcodeKeyboardHid&&!input_failed){input_failed=true;return true;}
            if(capture_fails&&request.opcode==FesComputerOpcodeMediaSnapshotControl&&request.argument==FesComputerMediaSnapshotFreeze&&!snapshot_failed){snapshot_failed=true;return true;}
            return false;
        };
        assert(!fixture.runtime.SetKeyboardHid(opened.package_id,gen,mister::KeyboardHidRows{}).ok());
        assert(WaitForState(fixture.runtime,capture_fails?mister::State::reboot_required:mister::State::idle));
        assert(!fixture.native.input.HasActiveCallback());
        endpoint.fail_after_request={};assert(input_failed);
        MediaDiskRecord saved;assert(record->Read(&saved).ok());
        if(capture_fails) {
            assert(snapshot_failed&&fixture.native.fpga.calls==programs&&endpoint.unit(0).data==changed);
            assert(fixture.runtime.status().generation==gen&&fixture.runtime.status().capabilities.media_units[0].game_id==binding.game_id);
            assert(saved.revision=="absent"&&saved.bytes.empty());
            assert(!fixture.runtime.Stop().ok()&&fixture.native.fpga.calls==programs);
            assert(fixture.runtime.RecoverIdle().code==mister::ErrorCode::save_failed);
            assert(fixture.native.fpga.calls==programs&&endpoint.unit(0).data==changed);
        } else {
            assert(fixture.native.fpga.calls==programs+1&&saved.bytes==changed&&endpoint.unit(0).state==1);
        }
        assert(ReadText(path)==original);
        const std::string ns=storage.path+"/"+MediaDataNamespace(identity);record.reset();
        if(!capture_fails)assert(unlink((ns+"/record.bin").c_str())==0);
        assert(rmdir(ns.c_str())==0);
    }
}

void TestComputerLiveMediaLifecycleThroughRuntime()
{
	using namespace mister::native;
	using namespace mister::native::generated;
	mister_test::ComputerEndpoint endpoint(31, {{0, {143360, 143360}}},
		"0123456789abcdef0123456789abcdef");
	FixedClock clock(100);
	FesGp transport(endpoint, clock);
	FesGpCoreDriver driver(transport);
	IntegratedFixture fixture(&driver);
	fixture.Start();
	// Every FPGA program replaces the endpoint with a freshly configured one.
	fixture.native.fpga.on_program = [&] { endpoint.Reset(); };
	TempDirectory package;
	package.File("manifest.toml", ComputerManifest());
	package.File("core.rbf", ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
	OpenedCorePackage opened;
	assert(OpenCorePackage(package.path, "", &opened).ok());
	mister::CorePackageInspection inspection;
	assert(fixture.runtime.InspectCore(package.path, opened.package_id, &inspection).ok());
	assert(inspection.compatible && inspection.persistence_layout.id.empty());

	TempDirectory media;
	for (unsigned run = 0; run < 2; ++run) {
		const auto first = endpoint.requests.size();
		assert(fixture.runtime.LoadCore(package.path, opened.package_id).ok());
		auto status = fixture.runtime.status();
		const auto generation = status.generation;
		assert(status.active_package.observed.abi.id == "fes.computer");
		assert(status.capabilities.active_interfaces.size() == 6);
		assert(status.capabilities.media_units.size() == 1);
		const auto unit = status.capabilities.media_units[0];
		assert(unit.unit == 0 && unit.interface.id == "fes.media.apple2-floppy");
		assert(unit.min_bytes == 143360 && unit.max_bytes == 143360 && unit.chunk_bytes == 512);
		assert(unit.state == mister::MediaUnitState::empty);
		// Identity, unit discovery, then release: no media gate, no evdev worker.
		assert(endpoint.requests.size() - first == 16u + 6u + 1u);
		assert(endpoint.requests.back().opcode == FesComputerOpcodeExecution &&
			endpoint.requests.back().argument == FesComputerExecutionRelease);
		assert(!endpoint.held && fixture.native.input.open_calls == 0);

		mister::KeyboardHidRows rows{};
		rows[0] = 0x0010;
		rows[8] = 0x0002;
		auto before = endpoint.requests.size();
		assert(fixture.runtime.SetKeyboardHid(opened.package_id, generation, rows).ok());
		assert(endpoint.requests.size() == before + 9);
		rows[0] = 0;
		assert(fixture.runtime.SetKeyboardHid(opened.package_id, generation, rows).ok());
		assert(endpoint.requests.size() == before + 10);
		assert(endpoint.rows == std::vector<std::uint16_t>({0, 0, 0, 0, 0, 0, 0, 0, 2}));
		assert(fixture.runtime.SetController(opened.package_id, generation, 1, 0x28, 0).ok());
		assert(endpoint.ports == std::vector<std::uint16_t>({0, 0x28}));
		assert(fixture.runtime.SetController(opened.package_id, generation, 0, 0, 1).code ==
			mister::ErrorCode::unsupported_interface);
		assert(fixture.runtime.status().state == mister::State::running_development);

		const auto disk = media.File("disk" + std::to_string(run) + ".dsk", ComputerDisk(143360));
		const auto shorter = media.File("short" + std::to_string(run) + ".dsk", ComputerDisk(143359));
		before = endpoint.requests.size();
		assert(fixture.runtime.InsertMedia(disk, opened.package_id, generation, 0, 143360).ok());
		assert(endpoint.unit(0).state == 3 && endpoint.unit(0).data.size() == 143360);
		const std::string expected = ComputerDisk(143360);
		assert(std::equal(expected.begin(), expected.end(), endpoint.unit(0).data.begin(),
			[](char a, std::uint8_t b) { return static_cast<std::uint8_t>(a) == b; }));
		assert(endpoint.requests.size() == before + 6 + 4 + 280 * (3 + 256) + 1 + 1);
		for (std::size_t i = before; i < endpoint.requests.size(); ++i)
			assert(endpoint.requests[i].opcode != FesComputerOpcodeExecution);
		assert(!endpoint.held);
		assert(fixture.runtime.status().capabilities.media_units[0].state == mister::MediaUnitState::ready);
		// Outside the observed limits: rejected before hardware.
		before = endpoint.requests.size();
		assert(fixture.runtime.InsertMedia(shorter, opened.package_id, generation, 0, 143359).code ==
			mister::ErrorCode::invalid_request);
		// A file that does not match the request fails admission without an exchange.
		assert(fixture.runtime.InsertMedia(shorter, opened.package_id, generation, 0, 143360).code ==
			mister::ErrorCode::invalid_request);
		assert(endpoint.requests.size() == before && endpoint.unit(0).state == 3);
		assert(fixture.runtime.EjectMedia(opened.package_id, generation, 0).ok());
		assert(endpoint.requests.size() == before + 2 && endpoint.unit(0).state == 1);
		assert(fixture.runtime.status().capabilities.media_units[0].state == mister::MediaUnitState::empty);
		assert(fixture.runtime.InsertMedia(disk, opened.package_id, generation, 0, 143360).ok());

		// Stop holds execution before reprogramming; the hold itself neutralizes
		// keys and ports in the core, so no neutral writes follow it.
		before = endpoint.requests.size();
		bool held_before_program = false;
		fixture.native.fpga.on_program = [&] {
			held_before_program = endpoint.held && endpoint.rows == std::vector<std::uint16_t>(9, 0) &&
				endpoint.ports == std::vector<std::uint16_t>({0, 0});
			endpoint.Reset();
		};
		assert(fixture.runtime.Stop().ok());
		assert(held_before_program && endpoint.requests.size() == before + 1);
		assert(endpoint.requests.back().opcode == FesComputerOpcodeExecution &&
			endpoint.requests.back().argument == FesComputerExecutionHoldReset);
		assert(fixture.runtime.status().capabilities.media_units.empty());
		assert(fixture.native.hardware.capabilities().media_units.empty());
		assert(fixture.runtime.InsertMedia(disk, opened.package_id, generation, 0, 143360).code ==
			mister::ErrorCode::busy);
		fixture.native.fpga.on_program = [&] { endpoint.Reset(); };
	}
	// The production factory forwards the computer operations to native hardware.
	mister_test::CaptureLog production_log;
	std::unique_ptr<mister::Hardware> production;
	assert(CreateKnownProductionHardware(production_log, &production).ok());
	assert(production->SetKeyboardHid(mister::KeyboardHidRows{}).message == "FES computer is not active");
	assert(production->InsertComputerMedia(0, "/media/disk.dsk", 143360).message ==
		"FES computer media unit is not active");
	assert(production->EjectComputerMedia(0).message == "FES computer media unit is not active");
}

void TestPcxtFloppyLifecycleThroughRuntime()
{
	using namespace mister::native;
	using namespace mister::native::generated;
	const auto caps = 15u | FesComputerCapabilityMediaPcxtFloppy;
	mister_test::ComputerEndpoint endpoint(caps,
		{{0, {FesComputerPcxtFloppyBytes, FesComputerPcxtFloppyBytes}}},
		"0123456789abcdef0123456789abcdef", FesComputerExtendedAbiMinor);
	FixedClock clock(100);
	FesGp transport(endpoint, clock);
	FesGpCoreDriver driver(transport);
	IntegratedFixture fixture(&driver);
	fixture.Start();
	fixture.native.fpga.on_program = [&] { endpoint.Reset(); };
	std::string manifest = ComputerManifest();
	ReplaceAll(&manifest, "id = \"fes.computer\"\nmajor = 1\nminor = 0",
		"id = \"fes.computer\"\nmajor = 1\nminor = 1");
	ReplaceAll(&manifest, "fes.media.apple2-floppy", "fes.media.pcxt-floppy");
	ReplaceAll(&manifest, "fes.pong", "fes.pcxt");
	TempDirectory package;
	package.File("manifest.toml", manifest);
	package.File("core.rbf", ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
	OpenedCorePackage opened;
	assert(OpenCorePackage(package.path, "", &opened).ok());
	assert(fixture.runtime.LoadCore(package.path, opened.package_id).ok());
	const auto status = fixture.runtime.status();
	assert(status.capabilities.media_units.size() == 1);
	assert(status.capabilities.media_units[0].interface.id == "fes.media.pcxt-floppy");
	assert(status.capabilities.media_units[0].min_bytes == FesComputerPcxtFloppyBytes &&
		status.capabilities.media_units[0].max_bytes == FesComputerPcxtFloppyBytes);
	assert(driver.observed_capabilities() == caps && !endpoint.held);
	const auto generation = status.generation;
	TempDirectory media;
	const auto bytes = ComputerDisk(FesComputerPcxtFloppyBytes);
	const auto disk = media.File("disk.img", bytes);
	const auto start = endpoint.requests.size();
	assert(fixture.runtime.InsertMedia(disk, opened.package_id, generation, 0, bytes.size()).ok());
	assert(endpoint.unit(0).state == FesComputerMediaStateReady && endpoint.unit(0).data.size() == bytes.size());
	assert(std::equal(bytes.begin(), bytes.end(), endpoint.unit(0).data.begin(),
		[](char a, std::uint8_t b) { return static_cast<std::uint8_t>(a) == b; }));
	assert(fixture.runtime.status().capabilities.media_units[0].state == mister::MediaUnitState::ready);
	assert(fixture.runtime.EjectMedia(opened.package_id, generation, 0).ok());
	assert(fixture.runtime.status().capabilities.media_units[0].state == mister::MediaUnitState::empty);
	for (std::size_t i = start; i < endpoint.requests.size(); ++i)
		assert(endpoint.requests[i].opcode != FesComputerOpcodeExecution);
	assert(!endpoint.held && fixture.runtime.status().generation == generation);
	assert(fixture.runtime.Stop().ok());
}

void TestZx81CleanMachineStopsWithoutFreeze()
{
	using namespace mister::native;
	using namespace mister::native::generated;
	const std::uint16_t capabilities = FesComputerCapabilityVideoFixed720p60 |
		FesComputerCapabilityKeyboardHid | FesComputerCapabilityGamepadPorts |
		FesComputerCapabilityAudioPcmS16Stereo48k | FesComputerCapabilityMediaZx81Tape |
		FesComputerCapabilityMediaZx81TapeWrite;
	mister_test::ComputerEndpoint endpoint(capabilities,
		{{0, {1, FesComputerZx81TapeMaxBytes}}}, "0123456789abcdef0123456789abcdef");
	FixedClock clock(100);
	FesGp transport(endpoint, clock);
	FesGpCoreDriver driver(transport);
	IntegratedFixture fixture(&driver);
	fixture.Start();
	fixture.native.fpga.on_program = [&] { endpoint.Reset(); };

	TempDirectory package;
	std::string manifest = ComputerManifest();
	ReplaceAll(&manifest, "fes.pong", "fes.zx81");
	ReplaceAll(&manifest, "fes.media.apple2-floppy", "fes.media.zx81-tape");
	ReplaceAll(&manifest, "fes.expansion.apple2-bus", "fes.expansion.zx81-bus");
	manifest += "\n[[interfaces]]\nid = \"fes.media.zx81-tape-write\"\nmajor = 1\nminor = 0\nrequired = true\n";
	package.File("manifest.toml", manifest);
	package.File("core.rbf", ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
	OpenedCorePackage opened;
	assert(OpenCorePackage(package.path, "", &opened).ok());
	assert(fixture.runtime.LoadCore(package.path, opened.package_id).ok());
	assert(!endpoint.save_ready && !endpoint.dirty);

	const std::size_t before_stop = endpoint.requests.size();
	assert(fixture.runtime.Stop().ok());
	assert(fixture.runtime.status().state == mister::State::idle);
	for (std::size_t i = before_stop; i < endpoint.requests.size(); ++i)
		assert(!(endpoint.requests[i].opcode == FesComputerOpcodeMediaSnapshotControl &&
			endpoint.requests[i].argument == FesComputerMediaSnapshotFreeze));
}

void TestComputerSlotCompositionActivatesLinkedPayload()
{
	auto hash = [](const std::string& value) {
		mister::native::Sha256 h;
		h.Update(value.data(), value.size());
		return mister::native::Sha256Hex(h.Final());
	};
	for (const bool mutate : {false, true}) {
		std::vector<std::string> driver_events;
		RecordingDriver driver(driver_events);
		IntegratedFixture fixture(&driver);
		fixture.Start();
		TempDirectory package, slot4, slot6, composition;
		package.File("manifest.toml", ComputerManifest());
		package.File("core.rbf", ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
		mister::native::OpenedCorePackage base;
		assert(mister::native::OpenCorePackage(package.path, "", &base).ok());
		mister::CoreCompositionRequest request;
		for (auto* card : {&slot4, &slot6}) {
			const unsigned slot = card == &slot4 ? 4 : 6;
			const std::string cart(40408, static_cast<char>('0' + slot));
			card->File("cart.rbf", cart);
			const std::string manifest = "{\"cart_sha256\":\"" + hash(cart) +
				"\",\"cart_size\":40408,\"device\":\"5CSEBA6U23I7\",\"format\":1,"
				"\"map\":\"fes.apple2-bus.slots/1\",\"recipe_sha256\":\"" + std::string(64, 'c') +
				"\",\"revision\":\"" + std::string(40, 'd') + "\",\"shell_build_id\":\"" +
				base.descriptor.build.id + "\",\"shell_package_id\":\"" + base.package_id +
				"\",\"shell_sha256\":\"" + base.descriptor.payload.sha256 +
				"\",\"slot\":\"fes.expansion.apple2-bus\",\"slot_index\":" + std::to_string(slot) +
				",\"slot_major\":1,\"slot_minor\":0}";
			card->File("manifest.json", manifest);
			request.expansions.push_back({static_cast<std::uint8_t>(slot), card->path});
			request.composition.expansions.push_back({static_cast<std::uint8_t>(slot),
				hash(std::string("fes-expansion-v1\0", 17) + manifest)});
		}
		const std::string linked(40408, 'l');
		request.payload_path = composition.File("linked.rbf", linked);
		auto& info = request.composition;
		info.package_id = base.package_id;
		info.shell_sha256 = base.descriptor.payload.sha256;
		info.payload_sha256 = hash(linked);
		info.payload_size = linked.size();
		std::string canonical = std::string("fes-composition-v2\0", 19) + info.package_id + std::string(1, '\0');
		for (const auto& slot : info.expansions)
			canonical += std::to_string(slot.slot) + ":" + slot.expansion_id + std::string(1, '\0');
		info.id = hash(canonical + info.payload_sha256);
		if (mutate) {
			// Admission retains every card; the second is rechecked before programming.
			std::unique_ptr<mister::AdmittedCorePackage> admitted;
			assert(fixture.native.hardware.AdmitCoreComposition(package.path, base.package_id,
				request, &admitted).ok());
			const int fd = open((slot6.path + "/cart.rbf").c_str(), O_WRONLY);
			assert(fd >= 0 && pwrite(fd, "x", 1, 0) == 1 && close(fd) == 0);
			fixture.native.events.clear();
			const auto result = fixture.native.hardware.LoadCore(std::move(admitted), 1);
			assert(result.error.code == mister::ErrorCode::invalid_package && !result.mutation_attempted);
			assert(fixture.native.events.empty() && driver_events.empty());
			continue;
		}
		assert(fixture.runtime.LoadComposedCore(package.path, base.package_id, request).ok());
		assert(fixture.native.fpga.programmed.back() == "linked.rbf");
		assert(fixture.native.fpga.programmed_first_bytes.back() == 'l');
		const auto status = fixture.runtime.status();
		assert(status.active_package.composition.id == info.id);
		assert(status.active_package.composition.expansion_id.empty());
		assert(status.active_package.composition.expansions.size() == 2);
		assert(status.active_package.composition.expansions[1].slot == 6);
		assert(fixture.runtime.Stop().ok());
		assert(fixture.runtime.status().active_package.composition.id.empty());
	}
}

// The ZX81 interface table matches build_fes_zx81_oss._manifest. Computer DDR
// follows the boot layout alone. Session display also needs both presentation
// adapters, so a DDR-only computer is admitted without them.
void TestZx81ComputerDisplayPackageAdmittedWhenAdvertised()
{
	using namespace mister::native;
	using namespace mister::native::generated;
	auto open_computer = [](bool session_display, TempDirectory* package, OpenedCorePackage* opened) {
		std::string manifest = ReadText("tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
		ReplaceAll(&manifest, "fes.simple-game", "fes.computer");
		if (session_display) {
			ReplaceAll(&manifest, "fes.gamepad", "fes.keyboard.hid");
			manifest +=
				"\n[[interfaces]]\nid = \"fes.gamepad.ports\"\nmajor = 1\nminor = 0\nrequired = true\n"
				"\n[[interfaces]]\nid = \"fes.audio.pcm-s16-stereo-48k\"\nmajor = 1\nminor = 0\nrequired = true\n"
				"\n[[interfaces]]\nid = \"fes.media.zx81-tape\"\nmajor = 1\nminor = 0\nrequired = true\n"
				"\n[[interfaces]]\nid = \"fes.media.zx81-tape-write\"\nmajor = 1\nminor = 0\nrequired = true\n"
				"\n[[interfaces]]\nid = \"fes.memory.hps-ddr\"\nmajor = 1\nminor = 0\nrequired = true\n"
				"\n[[interfaces]]\nid = \"fes.video.session-display\"\nmajor = 1\nminor = 0\nrequired = true\n"
				"\n[[interfaces]]\nid = \"fes.fabric.video.display-plane\"\nmajor = 1\nminor = 0\nrequired = true\n"
				"\n[[interfaces]]\nid = \"fes.expansion.zx81-bus\"\nmajor = 2\nminor = 0\nrequired = false\n";
		} else {
			const auto first = manifest.find("[[interfaces]]");
			const auto second = manifest.find("[[interfaces]]", first + 1);
			assert(first != std::string::npos && second != std::string::npos);
			manifest.erase(first, second - first);
			manifest +=
				"\n[[interfaces]]\nid = \"fes.memory.hps-ddr\"\nmajor = 1\nminor = 0\nrequired = true\n";
		}
		package->File("manifest.toml", manifest);
		package->File("core.rbf", ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
		assert(OpenCorePackage(package->path, "", opened).ok());
		assert(opened->descriptor.abi.id == FesComputerABIID);
		assert(CheckCoreCompatibility(opened->descriptor).ok());
	};
	TempDirectory display_package;
	TempDirectory ddr_package;
	OpenedCorePackage display_opened;
	OpenedCorePackage ddr_opened;
	open_computer(true, &display_package, &display_opened);
	open_computer(false, &ddr_package, &ddr_opened);

	auto advertised = [](const NativeHardware& hardware, const char* id) {
		for (const auto& abi : hardware.capabilities().abis)
			if (abi.id == FesComputerABIID)
				for (const auto& interface : abi.interfaces)
					if (interface.id == id) return true;
		return false;
	};
	auto admitted = [](NativeHardware* hardware, const std::string& path, const std::string& id) {
		mister::CorePackageInspection inspection;
		assert(hardware->InspectCorePackage(path, id, &inspection).ok());
		std::unique_ptr<mister::AdmittedCorePackage> package;
		const mister::Error admission = hardware->AdmitCorePackage(path, id, &package);
		if (admission.ok()) {
			assert(inspection.compatible);
			return true;
		}
		assert(!inspection.compatible);
		assert(inspection.compatibility_error.code == mister::ErrorCode::unsupported_interface);
		assert(admission.code == mister::ErrorCode::unsupported_interface);
		return false;
	};
	struct Case {
		bool layout;
		bool adapters;
	};
	const Case cases[] = {{false, false}, {false, true}, {true, false}, {true, true}};
	for (const Case& item : cases) {
		std::vector<std::string> events;
		RecordingOpener opener(events);
		RecordingFpga fpga(events);
		fpga.boot_hps_ddr_layout = item.layout;
		mister_test::FakeMmio mmio;
		FixedClock clock(100);
		FesGp transport(mmio, clock);
		FesGpCoreDriver driver(transport);
		MenuDisplayDriver display(transport, clock);
		MenuOperations operations;
		MenuWindow memory(operations);
		RecordingI2c i2c(events);
		RecordingVideo idle_video(events);
		LedgerLog log(events);
		FixedVideoBringup video(i2c, clock, log, Menu720p60Recipe());
		RecordingInput input(events, clock);
		TempDirectory splash;
		const InputDeviceIdentity identity = {"test", 0, 0, 0, 0};
		NativeHardware hardware(opener, fpga, idle_video, video, input, identity, clock, log,
			splash.File("idle.rbf", "idle"), {30000, 10000, 10000}, &driver, {"/tmp"},
			SplashIdle(), item.adapters ? &display : nullptr, item.adapters ? &memory : nullptr);
		const bool plane = item.layout && item.adapters;
		assert(advertised(hardware, FesComputerInterfaceMemoryHpsDdrID) == item.layout);
		assert(advertised(hardware, FesComputerInterfaceVideoSessionDisplayID) == plane);
		assert(advertised(hardware, kDisplayPlaneInterfaceID) == plane);
		assert(admitted(&hardware, display_package.path, display_opened.package_id) == plane);
		assert(admitted(&hardware, ddr_package.path, ddr_opened.package_id) == item.layout);
	}
}

void TestLiveComputerStatusResponsesMatchRuntime()
{
	assert(live_computer_status_responses.size() == 3);
	std::istringstream expected(ReadText("tests/fixtures/protocol-v2-live-computer-statuses.jsonl"));
	std::string line;
	for (const std::string& response : live_computer_status_responses) {
		assert(std::getline(expected, line));
		assert(response == line);
	}
	assert(!std::getline(expected, line));
}

class RecordingCoreVideo final : public mister::native::CoreVideoBringup {
public:
 explicit RecordingCoreVideo(std::vector<std::string>& events) : events_(events) {}
 bool SupportsPlatform(mister::native::NativePlatform p) const override {
  return p == mister::native::NativePlatform::de25_nano;
 }
 mister::native::VideoResult BringUpCustom(std::uint64_t, bool audio) override {
  assert(!audio); events_.push_back("de25.video.ready");
  mister::native::VideoResult result; result.error = failure; return result;
 }
 mister::native::VideoQuiesceResult Quiesce(std::uint64_t) override {
  events_.push_back("de25.video.quiesce"); return {};
 }
 mister::Error failure;
 std::vector<std::string>& events_;
};

void TestDe25SelectedPlatformLifecycle()
{
 using namespace mister;
 using namespace mister::native;
 Fixture base;
 std::vector<std::string> events;
 RecordingDriver selected(events), wrong_driver(events);
 RecordingCoreVideo video(events);
 TempDirectory package;
 std::string manifest = ReadText("tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
 ReplaceAll(&manifest, "fes.pong", "zx81");
 ReplaceAll(&manifest, "de10_nano", "de25_nano");
 ReplaceAll(&manifest, "5CSEBA6U23I7", "A5EB013BB23BE4SCS");
 ReplaceAll(&manifest, "fes-gp-v1", "de25-hps-first-v1");
 ReplaceAll(&manifest, "fes.simple-game", "fes.computer");
 ReplaceAll(&manifest, "fes.gamepad", "fes.keyboard.hid");
 manifest += "\n[[interfaces]]\nid = \"fes.media.zx81-tape\"\nmajor = 1\nminor = 0\nrequired = true\n";
 package.File("manifest.toml", manifest);
 package.File("core.rbf", ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf"));
 OpenedCorePackage opened;
 assert(OpenCorePackage(package.path, "", &opened).ok());
 const auto make_hardware = [&](CoreVideoBringup& backend, CoreDriver* driver) {
  return std::unique_ptr<NativeHardware>(new NativeHardware(base.opener, base.fpga,
   base.idle_video, backend, base.input, base.input_identity, base.clock, base.log,
   base.idle, {}, &wrong_driver, {"/tmp"}, SplashIdle(), nullptr, nullptr, nullptr,
   NativePlatform::de25_nano, driver));
 };
 // Selecting DE25 cannot execute the DE10 transmitter recipe or its driver.
 for (unsigned kind = 0; kind < 2; ++kind) {
  auto refused = make_hardware(kind == 0 ? static_cast<CoreVideoBringup&>(base.game_video) : video,
   kind == 0 ? &selected : nullptr);
  std::unique_ptr<AdmittedCorePackage> admitted;
  assert(!refused->AdmitCorePackage(package.path, opened.package_id, &admitted).ok());
  assert(!admitted && base.fpga.calls == 0 && events.empty());
  assert(refused->capabilities().programming_profiles.empty());
 }
 auto hardware = make_hardware(video, &selected);
 assert(!hardware->LoadIdle().error.ok());
 assert(base.fpga.calls == 0 && base.i2c.write_calls == 0 && events.empty());
 const auto capabilities = hardware->capabilities();
 assert(capabilities.rom_linking == 0 && capabilities.programming_profiles ==
  std::vector<std::string>({"de25-hps-first-v1"}));
 assert(capabilities.abis.size() == 1 && capabilities.abis[0].id == "fes.computer" &&
  capabilities.abis[0].interfaces.size() == 3);
 for (unsigned generation = 1; generation <= 2; ++generation) {
  std::unique_ptr<AdmittedCorePackage> admitted;
  assert(hardware->AdmitCorePackage(package.path, opened.package_id, &admitted).ok());
  events.clear();
  auto loaded = hardware->LoadCore(std::move(admitted), generation);
  assert(loaded.error.ok() && loaded.observed_core == "zx81");
  assert(Find(events, "driver.identify") < Find(events, "de25.video.ready"));
  assert(Find(events, "de25.video.ready") < Find(events, "driver.start"));
  assert(base.fpga.profiles.back() == ProgrammingProfile::de25_hps_first_v1);
  assert(base.fpga.hps_ddr_calls == 0 && base.input.open_calls == 0);
 }
 video.failure = {ErrorCode::io_failed, "injected FPGA video not ready"};
 std::unique_ptr<AdmittedCorePackage> admitted;
 assert(hardware->AdmitCorePackage(package.path, opened.package_id, &admitted).ok());
 events.clear();
 assert(!hardware->LoadCore(std::move(admitted), 3).error.ok());
 assert(Find(events, "driver.start") == events.size());
 assert(wrong_driver.identify_generations.empty() && wrong_driver.start_generations.empty());
 assert(base.i2c.write_calls == 0 && base.idle_video.quiesce_calls == 0);
 // A foreign platform is refused before programming even with both drivers supplied.
 TempDirectory foreign; PopulateFesGpPackage(&foreign);
 OpenedCorePackage foreign_opened; assert(OpenCorePackage(foreign.path, "", &foreign_opened).ok());
 const int programmed = base.fpga.calls;
 assert(!hardware->AdmitCorePackage(foreign.path, foreign_opened.package_id, &admitted).ok());
 assert(base.fpga.calls == programmed);
}

void TestDe25AllocatedMenuLaunchStopAndPinnedPeer()
{
 using namespace mister;using namespace mister::native;
 namespace dm=mister_test::de25_menu;
 Fixture base;dm::Operations operations;
 LinuxDe25Menu presenter(operations.clock,operations,5);De25MenuCoreDriver menu_driver(presenter);
 RecordingDriver computer(base.events);De25VideoBringup video;
 NativeHardware hardware(base.opener,base.fpga,video,video,base.input,base.input_identity,
  operations.clock,base.log,base.idle,{},nullptr,{"/tmp"},SplashIdle(),&presenter,&presenter,nullptr,
  NativePlatform::de25_nano,&computer,&menu_driver);
 TempDirectory menu,game;
 auto manifest=ReadText("tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
 ReplaceAll(&manifest,"de10_nano","de25_nano");ReplaceAll(&manifest,"5CSEBA6U23I7","A5EB013BB23BE4SCS");
 auto menu_manifest=manifest;
 ReplaceAll(&menu_manifest,"fes.pong","fes.menu");ReplaceAll(&menu_manifest,"fes-gp-v1","de25-linux-menu-v1");
 ReplaceAll(&menu_manifest,"fes.simple-game","fes.application");ReplaceAll(&menu_manifest,"fes.gamepad","fes.video.menu-display");
 ReplaceAll(&menu_manifest,"0123456789abcdef0123456789abcdef","0123456789abcdeffedcba9876543210");
 menu_manifest+="\n[[interfaces]]\nid=\"fes.fabric.de25-scanout\"\nmajor=1\nminor=1\nrequired=true\n";
 auto game_manifest=manifest;
 ReplaceAll(&game_manifest,"fes.pong","zx81");ReplaceAll(&game_manifest,"fes-gp-v1","de25-hps-first-v1");
 ReplaceAll(&game_manifest,"fes.simple-game","fes.computer");ReplaceAll(&game_manifest,"fes.gamepad","fes.keyboard.hid");
 game_manifest+="\n[[interfaces]]\nid=\"fes.media.zx81-tape\"\nmajor=1\nminor=0\nrequired=true\n";
 menu.File("manifest.toml",menu_manifest);game.File("manifest.toml",game_manifest);
 const auto payload=ReadText("tests/fixtures/core-bundle-v2/payloads/fes-fixture.rbf");
 menu.File("core.rbf",payload);game.File("core.rbf",payload);
 OpenedCorePackage menu_package,game_package;
 assert(OpenCorePackage(menu.path,"",&menu_package).ok()&&OpenCorePackage(game.path,"",&game_package).ok());
 const auto capabilities=hardware.capabilities();assert(capabilities.abis.size()==2&&capabilities.programming_profiles.size()==2);
 Runtime runtime(hardware,base.log);
 assert(runtime.Start(menu.path,menu_package.package_id).ok());
 assert(runtime.status().menu_display.available&&runtime.status().menu_display.frame_mapping);
 const auto initial_generation=runtime.status().menu_display.generation;
 for(unsigned turn=0;turn<2;++turn) {
  assert(runtime.LoadCore(game.path,game_package.package_id).ok());
  assert(runtime.status().state==State::running_development&&!operations.allocated);
  assert(runtime.Stop().ok()&&runtime.status().menu_display.available);
 }
 assert(runtime.status().menu_display.generation>initial_generation);
 assert(operations.allocations==3&&operations.reaps==2&&operations.published.size()==6);
 assert(base.fpga.profiles==std::vector<ProgrammingProfile>({ProgrammingProfile::de25_linux_menu_v1,
  ProgrammingProfile::de25_hps_first_v1,ProgrammingProfile::de25_linux_menu_v1,
  ProgrammingProfile::de25_hps_first_v1,ProgrammingProfile::de25_linux_menu_v1}));
 assert(base.fpga.hps_ddr_calls==0&&base.i2c.write_calls==0&&base.input.open_calls==0);
 int peer=-1;assert(presenter.Share(&peer).ok());const int programs=base.fpga.calls;
 assert(!runtime.LoadCore(game.path,game_package.package_id).ok());
 assert(base.fpga.calls==programs&&runtime.status().state==State::reboot_required&&operations.peers==1);
 operations.Close(peer);assert(!runtime.Stop().ok()&&base.fpga.calls==programs);
}

int main()
{
 TestDe25AllocatedMenuLaunchStopAndPinnedPeer();
 TestDe25SelectedPlatformLifecycle();
	TestZx81ComputerDisplayPackageAdmittedWhenAdvertised();
	TestLibraryPartsChecksCoreNamespaceBeforeProgramming();
	TestLibraryPartsChecksCoreNamespaceBeforeProgramming(true);
 TestSessionDisplayPreservesMachineOnCloseAndPlaneFailure();
 TestSessionDisplayRejectsPlaneWithoutBlend();
 TestApplicationSessionDisplayReleasesControllers();
 TestApplicationSessionDisplayMutesSingleGamepad();
 TestApplicationSessionDisplaySuppressesOppositeDirections();
 TestMenuInterfaceRecordsMatchMenuAdmission();
 TestNativeMenuActivationAndCompletion();
 TestNativeMenuBlitterPresent(0);
 TestNativeMenuBlitterPresent(1);
 TestMenuUnderflowPolicyReactivatesThenSplashes();
	TestComputerLiveMediaLifecycleThroughRuntime();
	TestPcxtFloppyLifecycleThroughRuntime();
	TestPcxtDosPackageInspectionAcceptsRuntimeFloppyContract();
	TestZx81CleanMachineStopsWithoutFreeze();
	TestInitialSTDiskBeforeExecution();
    TestInitialSTDiskBeforeExecution(409600);
    TestInitialSTDiskBeforeExecution(839680);
	TestInitialSTDiskAmbiguousReleaseSavesOrRetainsOwner();
	TestDiskBindingRetiresAfterProgramming();
	TestWritableLibraryDiskCaptureRestoreAndFailedSave();
    TestGeometryDiskBaseAdmissionAndRestoredLength();
	TestWritableDiskInputFaultSavesBeforeIdleOrRetainsRAM();
	TestComputerSlotCompositionActivatesLinkedPayload();
	TestFormat4TwoSourceAdmissionBeforeMutation();
	TestFormat3InspectionAndLoadGateBeforeMutation();
	TestApplicationVideoOnlyLifecycleNeedsNoInput();
	TestSimpleComputerAudioIsEnabledOnlyAfterIdentity();
	TestComputerControlsDispatchThroughSelectedCoreDriver();
	TestApplicationFirmwareStatusAdvertisesOptionalSlot();
	TestComputerSnapshotInterfacesBecomeActiveThroughRuntime();
	TestHpsDdrPortsReleaseAfterIdentityBeforeExecution();
	TestHpsDdrReleaseFailureRecoversBeforeExecution();
	TestHpsDdrNeedsTheBootLayout();
	TestNativeStreamSnapshotSizeCleanupAndObservedCapabilities();
	TestProductionFactoryForwardsCoreDataWithoutHardwareMutation();
	TestProductionFactoryForwardsProgrammedBitstreamWithoutHardwareMutation();
	TestPc386AtaDiskLaunchSaveAndRestore();
	TestPc386DiskFaultSaveRealignsMailbox();
	TestPongInputFaultDoesNotCapture();
	TestPc386LibraryRecordAndWallClock();
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
	TestMenuRevocationFailureBlocksIdleAndReplacement();
	TestSplashIdleProgramsWithoutProbeOrFramebuffer();
	TestIdlePreflightFailureCallsNeitherFpgaNorVideo();
	TestIdleFpgaFailureAfterQuiesceReportsHardwareMutation();
	TestIdleVideoFailureIsAttemptedIoFailureWithoutCleanup();
	TestUnavailableHardwareRemainsFailureOnly();
	TestInspectionReportsActualDriverCompatibilityWithoutMutation();
	TestCompositionProgramsRetainedLinkedArtifactAndRechecksBeforeMutation();
	TestActivationRechecksRetainedPayloadIdentityBeforeMutation();
	TestLiveComputerStatusResponsesMatchRuntime();
	puts("native_hardware_test: 35 passed");
	return 0;
}
