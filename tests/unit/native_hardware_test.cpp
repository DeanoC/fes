// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "capture_log.hpp"
#include "linux/production_hardware.hpp"
#include "native/artifacts.hpp"
#include "native/core_loader.hpp"
#include "native/hardware.hpp"
#include "native/linux/spi.hpp"

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

class RecordingSpi final : public mister::native::Spi {
public:
	explicit RecordingSpi(std::vector<std::string>& events) : events_(events) {}
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
			events_.push_back("configure");
		} else if (request[0] == 0x0055) {
			events_.push_back("media:" + std::to_string(request[1]));
		}
		return {};
	}
	std::vector<std::string>& events_;
	std::string observed_core = "TESTCART";
	mister::Error probe_error;
	std::vector<std::uint64_t> deadlines;
};

struct Fixture {
	Fixture()
		: temporary(), events(), idle(temporary.File("idle.rbf", "idle")),
		  rbf(temporary.File("game.rbf", "game")),
		  media_two(temporary.File("two.bin", "22")),
		  media_zero(temporary.File("zero.bin", "0")), opener(events), fpga(events),
		  spi(events), core(spi), clock(100), log(), hardware(opener, fpga, core,
			clock, log, idle, {30000, 10000}) {}
	mister::PreparedLaunch Launch() const
	{
		mister::PreparedLaunch launch;
		launch.system = "test_cart";
		launch.expected_core = "TESTCART";
		launch.rbf = rbf;
		launch.media.push_back({2, media_two});
		launch.media.push_back({0, media_zero});
		launch.settings.push_back({"region", "pal"});
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

void TestLaunchPreflightsAllArtifactsThenProgramsAndConfiguresInOrder()
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
	assert(Find(fixture.events, "probe") == 4);
	assert(Find(fixture.events, "configure") == 5);
	assert(Find(fixture.events, "media:0") < Find(fixture.events, "media:2"));
}

void TestDevelopmentAndIdleOnlyPreflightAndProgramTheirRbf()
{
	Fixture fixture;
	assert(fixture.hardware.LoadDevelopmentRBF(fixture.rbf).error.ok());
	assert(fixture.events.size() == 2);
	assert(fixture.events[0] == "open:" + fixture.rbf);
	assert(fixture.events[1] == "program:" + fixture.rbf);
	fixture.events.clear();
	assert(fixture.hardware.LoadIdle().error.ok());
	assert(fixture.events.size() == 2);
	assert(fixture.events[0] == "open:" + fixture.idle);
	assert(fixture.events[1] == "program:" + fixture.idle);
}

void TestOneAbsoluteDeadlinePerNativeStage()
{
	Fixture fixture;
	assert(fixture.hardware.Launch(fixture.Launch()).error.ok());
	assert(fixture.fpga.deadlines.size() == 1);
	assert(fixture.fpga.deadlines[0] == 30100);
	assert(!fixture.spi.deadlines.empty());
	for (std::uint64_t deadline : fixture.spi.deadlines) assert(deadline == 10100);
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
	assert(mister::ProductionProfiles().empty());
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
	TestLaunchPreflightsAllArtifactsThenProgramsAndConfiguresInOrder();
	TestDevelopmentAndIdleOnlyPreflightAndProgramTheirRbf();
	TestOneAbsoluteDeadlinePerNativeStage();
	TestPreflightAndProgramFailuresUseExactMutationMapping();
	TestEveryConcretePreflightRejectionPerformsZeroHardwareWork();
	TestProbeMismatchAndIoRetainObservedCoreAndMutation();
	TestNativeLoggingNamesPhasesAndConfirmedCore();
	TestProductionConstructionOwnsRealIdleHardware();
	TestUnavailableHardwareRemainsFailureOnly();
	puts("native_hardware_test: 9 passed");
	return 0;
}
