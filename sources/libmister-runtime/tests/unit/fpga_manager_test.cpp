// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "capture_diagnostic.hpp"
#include "fake_mmio.hpp"
#include "native/artifacts.hpp"
#include "native/linux/fpga_manager.hpp"

#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

#include <cstdint>
#include <initializer_list>
#include <string>
#include <vector>

namespace {

constexpr std::uint32_t kStatus = 0xff706000u;
constexpr std::uint32_t kControl = 0xff706004u;
constexpr std::uint32_t kDclkCount = 0xff706008u;
constexpr std::uint32_t kDclkStatus = 0xff70600cu;
constexpr std::uint32_t kGpo = 0xff706010u;
constexpr std::uint32_t kMonitorEoi = 0xff70684cu;
constexpr std::uint32_t kMonitor = 0xff706850u;
constexpr std::uint32_t kData = 0xffb90000u;
constexpr std::uint32_t kInterface = 0xffd08028u;
constexpr std::uint32_t kSdr = 0xffc25080u;
constexpr std::uint32_t kBridgeReset = 0xffd0501cu;
constexpr std::uint32_t kRemap = 0xff800000u;

constexpr std::uint32_t kControlSeed = 0xa5a500c2u;
constexpr std::uint32_t kGpoSeed = 0x12345678u;
constexpr std::uint32_t kGpoReset = 0x52345678u;
constexpr std::uint32_t kGpoNormal = 0x92345678u;

const char* current_test = nullptr;

void Expect(bool condition, const char* expression, int line)
{
	if (condition) return;
	fprintf(stderr, "%s:%d: expectation failed: %s\n", current_test, line,
		expression);
	abort();
}

#define EXPECT(expression) Expect((expression), #expression, __LINE__)

class FixedClock final : public mister::native::Clock {
public:
	explicit FixedClock(std::uint64_t now) : now_(now) {}
	std::uint64_t NowMs() const override { return now_; }
	std::uint64_t now_;
};

class AdvancingClock final : public mister::native::Clock {
public:
	explicit AdvancingClock(std::uint64_t now = 0) : now_(now) {}
	std::uint64_t NowMs() const override { return now_++; }
private:
	mutable std::uint64_t now_;
};

class StreamDeadlineClock final : public mister::native::Clock {
public:
	explicit StreamDeadlineClock(const mister_test::FakeMmio& mmio)
		: mmio_(mmio) {}
	std::uint64_t NowMs() const override
	{
		for (const auto& write : mmio_.writes)
			if (write.offset == kData) return 100;
		return 0;
	}
private:
	const mister_test::FakeMmio& mmio_;
};

class PreWriteDeadlineClock final : public mister::native::Clock {
public:
	std::uint64_t NowMs() const override
	{
		return calls_++ < 3 ? 0 : 100;
	}
private:
	mutable std::size_t calls_ = 0;
};

struct TempArtifact {
	explicit TempArtifact(std::size_t size)
	{
		char pattern[] = "/tmp/libmister-fpga.XXXXXX";
		const int descriptor = mkstemp(pattern);
		EXPECT(descriptor >= 0);
		path = pattern;
		std::vector<unsigned char> bytes(size);
		for (std::size_t index = 0; index < size; ++index)
			bytes[index] = static_cast<unsigned char>(index);
		EXPECT(write(descriptor, bytes.data(), bytes.size()) ==
			static_cast<ssize_t>(bytes.size()));
		EXPECT(close(descriptor) == 0);
		mister::native::PosixArtifactOpener opener;
		EXPECT(opener.Open(path, 32u * 1024u * 1024u, &artifact).ok());
	}
	~TempArtifact() { EXPECT(unlink(path.c_str()) == 0); }
	void Truncate() { EXPECT(truncate(path.c_str(), 0) == 0); }
	std::string path;
	mister::native::Artifact artifact;
};

std::uint32_t Mode(std::uint32_t msel, std::uint32_t mode)
{
	return (msel << 3) | mode;
}

void PushReads(mister_test::FakeMmio& mmio, std::uint32_t address,
	std::initializer_list<std::uint32_t> values)
{
	for (std::uint32_t value : values) mmio.PushRead(address, value);
}

void ConfigurePreflight(mister_test::FakeMmio& mmio, std::uint32_t msel)
{
	mmio.values[kGpo] = kGpoSeed;
	mmio.values[kStatus] = Mode(msel, 4);
	mmio.values[kControl] = kControlSeed;
	mmio.values[kDclkStatus] = 0;
	mmio.forced_values_after_write[kDclkStatus] = 0;
	mmio.values[kMonitor] = 7;
	mmio.values[kSdr] = 0;
	mmio.values[kBridgeReset] = 7;
	mmio.read_as_zero.insert(kRemap);
}

void ConfigureSuccess(mister_test::FakeMmio& mmio, std::uint32_t msel = 9)
{
	ConfigurePreflight(mmio, msel);
	PushReads(mmio, kStatus, {
		Mode(msel, 4),
		Mode(msel, 0), Mode(msel, 1),
		Mode(msel, 1), Mode(msel, 2),
		Mode(msel, 2), Mode(msel, 3),
		Mode(msel, 3), Mode(msel, 4),
		Mode(msel, 4),
	});
	PushReads(mmio, kMonitor, {1, 3, 7});
	PushReads(mmio, kDclkStatus, {1, 0, 1, 1, 1});
}

std::vector<mister_test::FakeMmio::Write> SuccessfulWrites()
{
	return {
		{kInterface, 0}, {kSdr, 0}, {kBridgeReset, 7}, {kRemap, 1},
		{kControl, 0xa5a502c2u},
		{kControl, 0xa5a50282u},
		{kControl, 0xa5a50280u},
		{kControl, 0xa5a50281u},
		{kControl, 0xa5a50285u},
		{kGpo, 0},
		{kControl, 0xa5a50281u},
		{kMonitorEoi, 0xfffu},
		{kControl, 0xa5a50381u},
		{kData, 0x03020100u}, {kData, 0x07060504u},
		{kControl, 0xa5a50281u},
		{kDclkStatus, 1}, {kDclkCount, 4}, {kDclkStatus, 1},
		{kDclkStatus, 1}, {kDclkCount, 0x5000}, {kDclkStatus, 1},
		{kControl, 0xa5a50280u},
	};
}

bool HasWrite(const mister_test::FakeMmio& mmio, std::uint32_t address,
	std::uint32_t value)
{
	for (const auto& write : mmio.writes)
		if (write.offset == address && write.value == value) return true;
	return false;
}


void ExpectStillContained(const mister_test::FakeMmio& mmio)
{
	EXPECT(!HasWrite(mmio, kSdr, 0x3fffu));
	EXPECT(!HasWrite(mmio, kBridgeReset, 0));
	EXPECT(!HasWrite(mmio, kRemap, 0x19u));
	EXPECT(!HasWrite(mmio, kGpo, kGpoNormal));
}

std::string Hex(std::uint32_t value)
{
	char output[24] = {};
	snprintf(output, sizeof(output), "0x%x", value);
	return output;
}

void ExpectProgramFailure(const mister::native::NativeResult& result,
	bool attempted, const char* phase, std::uint32_t last)
{
	EXPECT(result.error.code == mister::ErrorCode::program_failed);
	EXPECT(result.mutation_attempted == attempted);
	EXPECT(result.error.message.find(phase) != std::string::npos);
	EXPECT(result.error.message.find(Hex(last)) != std::string::npos);
	EXPECT(result.error.message.size() < 256);
}

void PushReadFailureAfter(mister_test::FakeMmio& mmio,
	std::uint32_t address, std::size_t successful_reads)
{
	for (std::size_t index = 0; index < successful_reads; ++index)
		mmio.PushReadError(address, {});
	mmio.PushReadError(address,
		{mister::ErrorCode::io_failed, "scripted readback failure"});
}

void TestProgramEmitsFpgaManagerStateAndFailure()
{
	mister_test::CaptureDiagnostic capture;
	mister::DiagnosticInstall install(&capture);
	TempArtifact input(8);
	mister_test::FakeMmio mmio;
	ConfigureSuccess(mmio);
	FixedClock clock(1);
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 100);
	EXPECT(result.error.ok());
	EXPECT(capture.Count("fpga_manager.state") >= 5);
	bool saw_reset = false;
	bool saw_user = false;
	for (const auto& event : capture.events()) {
		if (event.kind != "fpga_manager.state") continue;
		EXPECT(event.layer == "fpga");
		if (mister_test::HasString(event, "mode", "reset") &&
			mister_test::HasBool(event, "ok", true))
			saw_reset = true;
		if (mister_test::HasString(event, "mode", "user") &&
			mister_test::HasBool(event, "ok", true))
			saw_user = true;
	}
	EXPECT(saw_reset);
	EXPECT(saw_user);

	capture.Clear();
	mister_test::FakeMmio failing;
	ConfigurePreflight(failing, 9);
	failing.PushReadError(kStatus,
		{mister::ErrorCode::io_failed, "scripted preflight failure"});
	mister::native::LinuxFpgaManager failing_manager(failing, clock);
	const auto failed = failing_manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 100);
	EXPECT(!failed.error.ok());
	EXPECT(capture.Count("fpga_manager.state") >= 1);
	bool saw_failed = false;
	for (const auto& event : capture.events()) {
		if (event.kind == "fpga_manager.state" &&
			mister_test::HasString(event, "mode", "failed") &&
			mister_test::HasBool(event, "ok", false))
			saw_failed = true;
	}
	EXPECT(saw_failed);
}

void TestProgramsWithExactContainmentConfigurationAndReleaseOrder()
{
	TempArtifact input(8);
	mister_test::FakeMmio mmio;
	ConfigureSuccess(mmio);
	mmio.expected_writes = SuccessfulWrites();
	mmio.enforce_expected_writes = true;
	FixedClock clock(1);
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 100);
	if (!result.error.ok()) fprintf(stderr, "program error: %s\n",
		result.error.message.c_str());
	if (!mmio.write_mismatch.empty()) fprintf(stderr, "%s\n",
		mmio.write_mismatch.c_str());
	EXPECT(result.error.ok());
	EXPECT(result.mutation_attempted);
	EXPECT(mmio.writes.size() == mmio.expected_writes.size());
}

void TestFesGpInitializationWaitsForContainmentAndConfigurationReset()
{
	TempArtifact input(4);
	mister_test::FakeMmio mmio;
	ConfigureSuccess(mmio);
	FixedClock clock(1);
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 100);
	EXPECT(result.error.ok());
	std::size_t interface = mmio.writes.size();
	std::size_t nconfig = mmio.writes.size();
	std::size_t gpo = mmio.writes.size();
	for (std::size_t index = 0; index < mmio.writes.size(); ++index) {
		if (mmio.writes[index].offset == kInterface &&
			mmio.writes[index].value == 0) interface = index;
		if (mmio.writes[index].offset == kControl &&
			(mmio.writes[index].value & 4u) != 0) nconfig = index;
		if (mmio.writes[index].offset == kGpo) gpo = index;
	}
	EXPECT(interface < nconfig);
	EXPECT(nconfig < gpo);
	EXPECT(mmio.writes[gpo].value == 0);
	EXPECT(!HasWrite(mmio, kSdr, 0x3fffu));
	EXPECT(!HasWrite(mmio, kBridgeReset, 0));
	EXPECT(!HasWrite(mmio, kRemap, 0x19u));
}


void TestMselMappingAndControlRmwPreserveUnrelatedBits()
{
	struct Case {
		std::uint32_t msel;
		std::uint32_t after_width;
		std::uint32_t after_ratio;
	};
	const Case cases[] = {
		{0, 0xa5a500c2u, 0xa5a50002u},
		{1, 0xa5a500c2u, 0xa5a50042u},
		{2, 0xa5a500c2u, 0xa5a50082u},
		{8, 0xa5a502c2u, 0xa5a50202u},
		{9, 0xa5a502c2u, 0xa5a50282u},
		{10, 0xa5a502c2u, 0xa5a502c2u},
	};
	for (const Case& item : cases) {
		TempArtifact input(4);
		mister_test::FakeMmio mmio;
		ConfigureSuccess(mmio, item.msel);
		FixedClock clock(1);
		mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 100);
		EXPECT(result.error.ok());
		std::vector<std::uint32_t> control_writes;
		for (const auto& write : mmio.writes)
			if (write.offset == kControl) control_writes.push_back(write.value);
		EXPECT(control_writes.size() == 9);
		EXPECT(control_writes[0] == item.after_width);
		EXPECT(control_writes[1] == item.after_ratio);
		for (std::uint32_t value : control_writes)
			EXPECT((value & 0xffff0000u) == 0xa5a50000u);
		EXPECT(mmio.values[kGpo] == 0);
	}
}

void TestStreamsAcrossFourKiBReadBoundaryInWordOrder()
{
	TempArtifact input(5000);
	mister_test::FakeMmio mmio;
	ConfigureSuccess(mmio);
	FixedClock clock(1);
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 100);
	EXPECT(result.error.ok());
	std::vector<std::uint32_t> data;
	for (const auto& write : mmio.writes)
		if (write.offset == kData) data.push_back(write.value);
	EXPECT(data.size() == 1250);
	EXPECT(data[0] == 0x03020100u);
	EXPECT(data[1023] == 0xfffefdfcu);
	EXPECT(data[1024] == 0x03020100u);
	EXPECT(data[1249] == 0x87868584u);
}

void TestUnsupportedMselFailsBeforeMutation()
{
	TempArtifact input(4);
	mister_test::FakeMmio mmio;
	ConfigurePreflight(mmio, 3);
	FixedClock clock(1);
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 100);
	ExpectProgramFailure(result, false, "MSEL", 3);
	EXPECT(mmio.writes.empty());
}

void TestEveryPreflightReadFailureIsNotAttempted()
{
	const std::uint32_t addresses[] = {kStatus, kControl};
	for (std::uint32_t address : addresses) {
		TempArtifact input(4);
		mister_test::FakeMmio mmio;
		ConfigurePreflight(mmio, 9);
		mmio.PushReadError(address,
			{mister::ErrorCode::io_failed, "scripted preflight read"});
		FixedClock clock(1);
		mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 100);
		ExpectProgramFailure(result, false, "preflight", 0);
		EXPECT(mmio.writes.empty());
	}
}



void TestDeadlineAfterPreflightBeforeFirstWriteIsNotAttempted()
{
	TempArtifact input(4);
	mister_test::FakeMmio mmio;
	ConfigureSuccess(mmio);
	PreWriteDeadlineClock clock;
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 100);
	ExpectProgramFailure(result, false, "bridge containment interface", 0);
	EXPECT(mmio.writes.empty());
}

void TestResetPhaseTimeoutRemainsContained()
{
	TempArtifact input(4);
	mister_test::FakeMmio mmio;
	ConfigurePreflight(mmio, 9);
	mmio.PushRead(kStatus, Mode(9, 4));
	AdvancingClock clock;
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 1000);
	ExpectProgramFailure(result, true, "reset phase", Mode(9, 4));
	ExpectStillContained(mmio);
}

void TestConfigurationPhaseTimeoutRemainsContained()
{
	TempArtifact input(4);
	mister_test::FakeMmio mmio;
	ConfigurePreflight(mmio, 9);
	PushReads(mmio, kStatus, {Mode(9, 4), Mode(9, 1)});
	mmio.values[kStatus] = Mode(9, 1);
	AdvancingClock clock;
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 1000);
	ExpectProgramFailure(result, true, "configuration phase", Mode(9, 1));
	ExpectStillContained(mmio);
}

void TestNstatusDropFailsImmediatelyAndRemainsContained()
{
	TempArtifact input(4);
	mister_test::FakeMmio mmio;
	ConfigureSuccess(mmio);
	mmio.scripted_reads[kMonitor].clear();
	mmio.PushRead(kMonitor, 0);
	FixedClock clock(1);
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 100);
	ExpectProgramFailure(result, true, "nSTATUS", 0);
	ExpectStillContained(mmio);
}

void TestConfDoneTimeoutRemainsContained()
{
	TempArtifact input(4);
	mister_test::FakeMmio mmio;
	ConfigureSuccess(mmio);
	mmio.scripted_reads[kMonitor].clear();
	mmio.values[kMonitor] = 1;
	AdvancingClock clock;
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 1000);
	ExpectProgramFailure(result, true, "CONF_DONE", 1);
	ExpectStillContained(mmio);
}

void TestDclkFourTimeoutRemainsContained()
{
	TempArtifact input(4);
	mister_test::FakeMmio mmio;
	ConfigureSuccess(mmio);
	mmio.scripted_reads[kDclkStatus].clear();
	mmio.values[kDclkStatus] = 0;
	AdvancingClock clock;
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 1000);
	ExpectProgramFailure(result, true, "DCLK 0x4", 0);
	ExpectStillContained(mmio);
}

void TestInitializationPhaseTimeoutRemainsContained()
{
	TempArtifact input(4);
	mister_test::FakeMmio mmio;
	ConfigureSuccess(mmio);
	mmio.scripted_reads[kStatus].clear();
	PushReads(mmio, kStatus, {
		Mode(9, 4), Mode(9, 1), Mode(9, 2), Mode(9, 2),
	});
	mmio.values[kStatus] = Mode(9, 2);
	mmio.scripted_reads[kDclkStatus].clear();
	PushReads(mmio, kDclkStatus, {0, 1});
	AdvancingClock clock;
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 1000);
	ExpectProgramFailure(result, true, "initialization phase", Mode(9, 2));
	ExpectStillContained(mmio);
}

void TestDclkFiveThousandTimeoutRemainsContained()
{
	TempArtifact input(4);
	mister_test::FakeMmio mmio;
	ConfigureSuccess(mmio);
	mmio.scripted_reads[kStatus].clear();
	PushReads(mmio, kStatus, {
		Mode(9, 4), Mode(9, 1), Mode(9, 2), Mode(9, 3),
	});
	mmio.values[kStatus] = Mode(9, 3);
	mmio.scripted_reads[kDclkStatus].clear();
	PushReads(mmio, kDclkStatus, {0, 1, 0});
	mmio.values[kDclkStatus] = 0;
	AdvancingClock clock;
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 1000);
	ExpectProgramFailure(result, true, "DCLK 0x5000", 0);
	ExpectStillContained(mmio);
}

void TestUserModeTimeoutRemainsContained()
{
	TempArtifact input(4);
	mister_test::FakeMmio mmio;
	ConfigureSuccess(mmio);
	mmio.scripted_reads[kStatus].clear();
	PushReads(mmio, kStatus, {
		Mode(9, 4), Mode(9, 1), Mode(9, 2), Mode(9, 3), Mode(9, 3),
	});
	mmio.values[kStatus] = Mode(9, 3);
	mmio.scripted_reads[kDclkStatus].clear();
	PushReads(mmio, kDclkStatus, {0, 1, 0, 1});
	AdvancingClock clock;
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 1000);
	ExpectProgramFailure(result, true, "user mode", Mode(9, 3));
	ExpectStillContained(mmio);
}

void TestStreamDeadlineRemainsContained()
{
	TempArtifact input(8);
	mister_test::FakeMmio mmio;
	ConfigureSuccess(mmio);
	StreamDeadlineClock clock(mmio);
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 100);
	ExpectProgramFailure(result, true, "stream", 4);
	EXPECT(HasWrite(mmio, kData, 0x03020100u));
	ExpectStillContained(mmio);
}

void TestShortReadRemainsContained()
{
	TempArtifact input(4);
	input.Truncate();
	mister_test::FakeMmio mmio;
	ConfigureSuccess(mmio);
	FixedClock clock(1);
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 100);
	ExpectProgramFailure(result, true, "stream read", 0);
	ExpectStillContained(mmio);
}

void TestDataWriteFailureRemainsContained()
{
	TempArtifact input(4);
	mister_test::FakeMmio mmio;
	ConfigureSuccess(mmio);
	mmio.PushWriteError(kData,
		{mister::ErrorCode::io_failed, "scripted data write"});
	FixedClock clock(1);
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 100);
	ExpectProgramFailure(result, true, "stream write", 0x03020100u);
	ExpectStillContained(mmio);
}


void TestManagerReadbackErrorsRemainContained()
{
	struct Case {
		std::uint32_t address;
		std::size_t successful_reads;
		const char* phase;
		std::uint32_t last;
	};
	const Case cases[] = {
		{kStatus, 9, "manager STAT readback", Mode(9, 4)},
		{kMonitor, 2, "manager monitor readback", 3},
		{kControl, 1, "manager CTRL readback", 0xa5a50280u},
	};
	for (const Case& item : cases) {
		TempArtifact input(4);
		mister_test::FakeMmio mmio;
		ConfigureSuccess(mmio);
		PushReadFailureAfter(mmio, item.address, item.successful_reads);
		FixedClock clock(1);
		mister::native::LinuxFpgaManager manager(mmio, clock);
		const auto result = manager.Program(input.artifact,
			mister::native::ProgrammingProfile::fes_gp_v1, 100);
		ExpectProgramFailure(result, true, item.phase, item.last);
		EXPECT(!HasWrite(mmio, kSdr, 0x3fffu));
		EXPECT(!HasWrite(mmio, kGpo, kGpoNormal));
	}
}

void TestManagerReadbackMismatchRemainsContained()
{
	TempArtifact input(4);
	mister_test::FakeMmio mmio;
	ConfigureSuccess(mmio);
	mmio.PushRead(kControl, kControlSeed);
	mmio.PushRead(kControl, 0xa5a50281u);
	FixedClock clock(1);
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 100);
	ExpectProgramFailure(result, true, "manager CTRL readback", 0xa5a50281u);
	EXPECT(!HasWrite(mmio, kSdr, 0x3fffu));
	EXPECT(!HasWrite(mmio, kGpo, kGpoNormal));
}






void TestInitialDeadlineIsNotAttempted()
{
	TempArtifact input(4);
	mister_test::FakeMmio mmio;
	FixedClock clock(10);
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact,
		mister::native::ProgrammingProfile::fes_gp_v1, 10);
	EXPECT(result.error.code == mister::ErrorCode::program_failed);
	EXPECT(result.error.message == "deadline exceeded");
	EXPECT(!result.mutation_attempted);
}

template <typename Function>
void Run(const char* name, Function function, int* count)
{
	current_test = name;
	function();
	++*count;
}

} // namespace

int main()
{
	int count = 0;
	Run("fpga_manager diagnostic state",
		TestProgramEmitsFpgaManagerStateAndFailure, &count);
	Run("exact containment/configuration/release order",
		TestProgramsWithExactContainmentConfigurationAndReleaseOrder, &count);
	Run("FES GP initialization after reset",
		TestFesGpInitializationWaitsForContainmentAndConfigurationReset, &count);
	Run("MSEL mapping and CTRL/GPO RMW preservation",
		TestMselMappingAndControlRmwPreserveUnrelatedBits, &count);
	Run("4 KiB stream boundary order",
		TestStreamsAcrossFourKiBReadBoundaryInWordOrder, &count);
	Run("unsupported MSEL preflight", TestUnsupportedMselFailsBeforeMutation, &count);
	Run("preflight read classification", TestEveryPreflightReadFailureIsNotAttempted,
		&count);
	Run("pre-write deadline classification",
		TestDeadlineAfterPreflightBeforeFirstWriteIsNotAttempted, &count);
	Run("reset-phase timeout containment", TestResetPhaseTimeoutRemainsContained,
		&count);
	Run("configuration-phase timeout containment",
		TestConfigurationPhaseTimeoutRemainsContained, &count);
	Run("nSTATUS drop containment", TestNstatusDropFailsImmediatelyAndRemainsContained,
		&count);
	Run("CONF_DONE timeout containment", TestConfDoneTimeoutRemainsContained, &count);
	Run("DCLK 0x4 timeout containment", TestDclkFourTimeoutRemainsContained, &count);
	Run("initialization-phase timeout containment",
		TestInitializationPhaseTimeoutRemainsContained, &count);
	Run("DCLK 0x5000 timeout containment",
		TestDclkFiveThousandTimeoutRemainsContained, &count);
	Run("user-mode timeout containment", TestUserModeTimeoutRemainsContained, &count);
	Run("stream deadline containment", TestStreamDeadlineRemainsContained, &count);
	Run("short stream read containment", TestShortReadRemainsContained, &count);
	Run("stream MMIO failure containment", TestDataWriteFailureRemainsContained, &count);
	Run("manager readback error containment",
		TestManagerReadbackErrorsRemainContained, &count);
	Run("manager readback mismatch containment",
		TestManagerReadbackMismatchRemainsContained, &count);
	Run("initial deadline classification", TestInitialDeadlineIsNotAttempted, &count);
	printf("fpga_manager_test: %d passed\n", count);
	return 0;
}
