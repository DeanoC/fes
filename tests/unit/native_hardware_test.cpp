// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "capture_log.hpp"
#include "linux/production_hardware.hpp"
#include "native/artifacts.hpp"
#include "native/core_loader.hpp"
#include "native/hardware.hpp"
#include "native/linux/spi.hpp"
#include "native/video.hpp"

#include <assert.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

#include <cstdint>
#include <memory>
#include <string>
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

class RecordingOpener final : public mister::native::ArtifactOpener {
public:
	explicit RecordingOpener(std::vector<std::string>& events)
		: events_(events), delegate(), calls(0), fail_call(0) {}
	mister::Error Open(const std::string& path, std::uint64_t maximum,
		mister::native::Artifact* artifact) override
	{
		++calls;
		events_.push_back("open:" + path);
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
		events_.push_back("program:" + artifact.path());
		deadlines.push_back(deadline);
		++calls;
		return result;
	}
	std::vector<std::string>& events_;
	mister::native::NativeResult result;
	std::vector<std::uint64_t> deadlines;
	int calls = 0;
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
		events_.push_back("video:" + expected_core);
		deadlines.push_back(deadline);
		++calls;
		return result;
	}
	std::vector<std::string>& events_;
	mister::native::VideoResult result;
	std::vector<std::uint64_t> deadlines;
	int calls = 0;
};

class RecordingSpi final : public mister::native::Spi {
public:
	explicit RecordingSpi(std::vector<std::string>& events) : events_(events) {}
	mister::Error SynchronizeCore(std::uint64_t deadline) override
	{
		deadlines.push_back(deadline);
		events_.push_back("sync");
		return sync_error;
	}
	mister::Error Exchange(std::uint8_t,
		const std::vector<std::uint16_t>& request,
		std::vector<std::uint16_t>* response, std::uint64_t deadline) override
	{
		deadlines.push_back(deadline);
		if (request.empty()) return {mister::ErrorCode::io_failed, "empty"};
		if (request[0] == 0x0014) {
			events_.push_back("probe");
			if (!probe_error.ok()) return probe_error;
			response->assign(request.size(), 0);
			std::size_t index = 1;
			for (unsigned char byte : observed_core) (*response)[index++] = byte;
			(*response)[index] = ';';
		} else if (request[0] == 0x001e) {
			events_.push_back(status_calls == 0 ?
				"reset.assert:" + std::to_string(request.at(1)) :
				"status.initial:" + std::to_string(request.at(1)));
			++status_calls;
		} else if (request[0] == 0x0055) {
			events_.push_back("media:" + std::to_string(request[1]));
		}
		return {};
	}
	std::vector<std::string>& events_;
	std::string observed_core = "TESTCART";
	mister::Error probe_error;
	std::vector<std::uint64_t> deadlines;
	mister::Error sync_error;
	std::size_t status_calls = 0;
};

struct Fixture {
	Fixture()
		: temporary(), events(), idle(temporary.File("idle.rbf", "idle")),
		  rbf(temporary.File("game.rbf", "game")),
		  media_two(temporary.File("two.bin", "22")),
		  media_zero(temporary.File("zero.bin", "0")), opener(events), fpga(events),
		  spi(events), core(spi), video(events), clock(100), log(),
		  hardware(opener, fpga, core, video, clock, log, idle,
			{30000, 10000, 10000}) {}
	mister::PreparedLaunch Launch() const
	{
		mister::PreparedLaunch launch;
		launch.system = "test_cart";
		launch.expected_core = "TESTCART";
		launch.rbf = rbf;
		launch.media.push_back({2, media_two});
		launch.media.push_back({0, media_zero});
		launch.core = {0x1111, 0x2222, 0x3333,
			mister::FileWireFormat::little_endian_byte_pairs};
		return launch;
	}
	TempDirectory temporary;
	std::vector<std::string> events;
	std::string idle;
	std::string rbf;
	std::string media_two;
	std::string media_zero;
	RecordingOpener opener;
	RecordingFpga fpga;
	RecordingSpi spi;
	mister::native::CoreLoader core;
	RecordingVideo video;
	FixedClock clock;
	mister_test::CaptureLog log;
	mister::native::NativeHardware hardware;
};

std::size_t Find(const std::vector<std::string>& events, const std::string& value)
{
	for (std::size_t index = 0; index < events.size(); ++index) {
		if (events[index] == value) return index;
	}
	return events.size();
}

void TestLaunchUsesExactCoreRecipeAndExplicitMediaFormatInOrder()
{
	Fixture fixture;
	const mister::HardwareResult result = fixture.hardware.Launch(fixture.Launch());
	assert(result.error.ok() && result.mutation_attempted);
	assert(result.observed_core == "TESTCART");
	assert(fixture.events.size() >= 8);
	assert(fixture.events[0] == "open:" + fixture.rbf);
	assert(fixture.events[1] == "open:" + fixture.media_two);
	assert(fixture.events[2] == "open:" + fixture.media_zero);
	assert(Find(fixture.events, "program:" + fixture.rbf) == 3);
	assert(Find(fixture.events, "reset.assert:4369") == 4);
	assert(Find(fixture.events, "probe") == 5);
	assert(Find(fixture.events, "status.initial:8738") == 6);
	assert(Find(fixture.events, "media:0") < Find(fixture.events, "media:2"));
}

void TestLaunchRejectsUnsupportedPreparedMediaFormatBeforeFileSelection()
{
	Fixture fixture;
	mister::PreparedLaunch launch = fixture.Launch();
	launch.core.file_wire = static_cast<mister::FileWireFormat>(99);
	const mister::HardwareResult result = fixture.hardware.Launch(launch);
	assert(result.error.code == mister::ErrorCode::io_failed);
	assert(result.error.message == "unsupported file wire format");
	assert(result.mutation_attempted);
	assert(Find(fixture.events, "reset.assert:4369") < fixture.events.size());
	assert(Find(fixture.events, "status.initial:8738") < fixture.events.size());
	assert(Find(fixture.events, "media:0") == fixture.events.size());
	assert(Find(fixture.events, "media:2") == fixture.events.size());
}

void TestIdleRequiresVideoAndPreservesDevelopmentBehavior()
{
	Fixture fixture;
	assert(fixture.hardware.LoadDevelopmentRBF(fixture.rbf).error.ok());
	assert(fixture.events.size() == 2);
	assert(fixture.events[0] == "open:" + fixture.rbf);
	assert(fixture.events[1] == "program:" + fixture.rbf);
	assert(fixture.video.calls == 0);
	fixture.events.clear();
	const mister::HardwareResult idle = fixture.hardware.LoadIdle();
	assert(idle.error.ok());
	assert(idle.mutation_attempted);
	assert(idle.observed_core == "MENU");
	assert(fixture.events == std::vector<std::string>({
		"open:" + fixture.idle,
		"program:" + fixture.idle,
		"video:MENU",
	}));
	assert(fixture.video.deadlines == std::vector<std::uint64_t>({10100}));
}

void TestIdlePreflightFailureCallsNeitherFpgaNorVideo()
{
	Fixture fixture;
	fixture.opener.fail_call = 1;
	const mister::HardwareResult result = fixture.hardware.LoadIdle();
	assert(result.error.code == mister::ErrorCode::io_failed);
	assert(!result.mutation_attempted);
	assert(fixture.fpga.calls == 0);
	assert(fixture.video.calls == 0);
}

void TestIdleFpgaFailureNeverCallsVideoAndPreservesMutationFlag()
{
	Fixture before;
	before.fpga.result = {{mister::ErrorCode::program_failed, "before"}, false};
	const mister::HardwareResult before_result = before.hardware.LoadIdle();
	assert(before_result.error.code == mister::ErrorCode::program_failed);
	assert(!before_result.mutation_attempted);
	assert(before.video.calls == 0);

	Fixture after;
	after.fpga.result = {{mister::ErrorCode::program_failed, "after"}, true};
	const mister::HardwareResult after_result = after.hardware.LoadIdle();
	assert(after_result.error.code == mister::ErrorCode::program_failed);
	assert(after_result.mutation_attempted);
	assert(after.video.calls == 0);
}

void TestIdleVideoFailureIsAttemptedIoFailureWithoutCleanup()
{
	Fixture fixture;
	fixture.video.result.error = {
		mister::ErrorCode::program_failed, "injected video failure"};
	fixture.video.result.observed_core = "MENU";
	const mister::HardwareResult result = fixture.hardware.LoadIdle();
	assert(result.error.code == mister::ErrorCode::io_failed);
	assert(result.error.message == "injected video failure");
	assert(result.mutation_attempted);
	assert(result.observed_core == "MENU");
	assert(fixture.fpga.calls == 1);
	assert(fixture.video.calls == 1);
	assert(fixture.events == std::vector<std::string>({
		"open:" + fixture.idle,
		"program:" + fixture.idle,
		"video:MENU",
	}));
}

void TestLaunchNeverCallsIdleVideo()
{
	Fixture fixture;
	assert(fixture.hardware.Launch(fixture.Launch()).error.ok());
	assert(fixture.video.calls == 0);
}

void TestOneAbsoluteDeadlinePerNativeStage()
{
	Fixture fixture;
	assert(fixture.hardware.Launch(fixture.Launch()).error.ok());
	assert(fixture.fpga.deadlines.size() == 1);
	assert(fixture.fpga.deadlines[0] == 30100);
	assert(!fixture.spi.deadlines.empty());
	for (std::uint64_t deadline : fixture.spi.deadlines) assert(deadline == 10100);
	assert(fixture.video.calls == 0);
}

void TestPreflightAndProgramFailuresUseExactMutationMapping()
{
	Fixture preflight;
	preflight.opener.fail_call = 2;
	const auto open_result = preflight.hardware.Launch(preflight.Launch());
	assert(open_result.error.code == mister::ErrorCode::io_failed);
	assert(!open_result.mutation_attempted);
	assert(preflight.fpga.calls == 0 && preflight.spi.deadlines.empty());
	Fixture before;
	before.fpga.result = {{mister::ErrorCode::program_failed, "before"}, false};
	const auto before_result = before.hardware.Launch(before.Launch());
	assert(before_result.error.code == mister::ErrorCode::program_failed);
	assert(!before_result.mutation_attempted);
	Fixture after;
	after.fpga.result = {{mister::ErrorCode::program_failed, "after"}, true};
	const auto after_result = after.hardware.Launch(after.Launch());
	assert(after_result.error.code == mister::ErrorCode::program_failed);
	assert(after_result.mutation_attempted);
}

void TestEveryConcretePreflightRejectionPerformsZeroHardwareWork()
{
	Fixture missing;
	mister::PreparedLaunch missing_launch = missing.Launch();
	missing_launch.rbf = missing.temporary.path + "/missing.rbf";
	assert(missing.hardware.Launch(missing_launch).error.code ==
		mister::ErrorCode::io_failed);
	assert(missing.fpga.calls == 0 && missing.spi.deadlines.empty());

	Fixture directory;
	mister::PreparedLaunch directory_launch = directory.Launch();
	directory_launch.rbf = directory.temporary.path;
	assert(directory.hardware.Launch(directory_launch).error.code ==
		mister::ErrorCode::io_failed);
	assert(directory.fpga.calls == 0 && directory.spi.deadlines.empty());

	Fixture zero;
	mister::PreparedLaunch zero_launch = zero.Launch();
	zero_launch.rbf = zero.temporary.File("empty.rbf", "");
	assert(zero.hardware.Launch(zero_launch).error.code ==
		mister::ErrorCode::io_failed);
	assert(zero.fpga.calls == 0 && zero.spi.deadlines.empty());

	Fixture oversize;
	mister::PreparedLaunch oversize_launch = oversize.Launch();
	oversize_launch.rbf = oversize.temporary.SparseFile("large.rbf",
		32u * 1024u * 1024u + 1u);
	assert(oversize.hardware.Launch(oversize_launch).error.code ==
		mister::ErrorCode::io_failed);
	assert(oversize.fpga.calls == 0 && oversize.spi.deadlines.empty());
}

void TestProbeMismatchAndIoRetainObservedCoreAndMutation()
{
	Fixture mismatch;
	mismatch.spi.observed_core = "OTHER";
	const auto mismatch_result = mismatch.hardware.Launch(mismatch.Launch());
	assert(mismatch_result.error.code == mister::ErrorCode::core_mismatch);
	assert(mismatch_result.mutation_attempted);
	assert(mismatch_result.observed_core == "OTHER");
	Fixture io;
	io.spi.probe_error = {mister::ErrorCode::io_failed, "probe"};
	const auto io_result = io.hardware.Launch(io.Launch());
	assert(io_result.error.code == mister::ErrorCode::io_failed);
	assert(io_result.mutation_attempted);
	assert(io_result.observed_core.empty());
}

void TestNativeLoggingNamesPhasesAndConfirmedCore()
{
	Fixture fixture;
	assert(fixture.hardware.Launch(fixture.Launch()).error.ok());
	const auto records = fixture.log.records();
	bool preflight = false;
	bool program = false;
	bool probe = false;
	bool configure = false;
	bool media = false;
	for (const auto& record : records) {
		preflight = preflight || record.phase == "preflight";
		program = program || record.phase == "program";
		probe = probe || (record.phase == "probe" && record.core == "TESTCART");
		configure = configure || record.phase == "configure";
		media = media || record.phase == "media";
	}
	assert(preflight && program && probe && configure && media);
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
	assert(hardware->Launch({}).error.code == mister::ErrorCode::io_failed);
	assert(hardware->LoadDevelopmentRBF("/x").error.code ==
		mister::ErrorCode::io_failed);
}

} // namespace

int main()
{
	TestLaunchUsesExactCoreRecipeAndExplicitMediaFormatInOrder();
	TestLaunchRejectsUnsupportedPreparedMediaFormatBeforeFileSelection();
	TestIdleRequiresVideoAndPreservesDevelopmentBehavior();
	TestIdlePreflightFailureCallsNeitherFpgaNorVideo();
	TestIdleFpgaFailureNeverCallsVideoAndPreservesMutationFlag();
	TestIdleVideoFailureIsAttemptedIoFailureWithoutCleanup();
	TestLaunchNeverCallsIdleVideo();
	TestOneAbsoluteDeadlinePerNativeStage();
	TestPreflightAndProgramFailuresUseExactMutationMapping();
	TestEveryConcretePreflightRejectionPerformsZeroHardwareWork();
	TestProbeMismatchAndIoRetainObservedCoreAndMutation();
	TestNativeLoggingNamesPhasesAndConfirmedCore();
	TestProductionConstructionOwnsRealIdleHardware();
	TestUnavailableHardwareRemainsFailureOnly();
	puts("native_hardware_test: 14 passed");
	return 0;
}
