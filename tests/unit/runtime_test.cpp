// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "capture_diagnostic.hpp"
#include "capture_log.hpp"
#include "fake_hardware.hpp"
#include "libmister-runtime/runtime.h"
#include "test_profiles.hpp"

#include <assert.h>
#include <stdio.h>

#include <chrono>
#include <condition_variable>
#include <mutex>
#include <thread>
#include <vector>

namespace {

using mister::ErrorCode;
using mister::Execution;
using mister::State;

mister::Launch CartLaunch()
{
	mister::Launch launch;
	launch.system = "test_cart";
	launch.rbf = "/cores/test.rbf";
	launch.media.push_back({"cartridge", "/games/test.bin"});
	launch.settings.push_back({"region", "auto"});
	return launch;
}

struct Fixture {
	Fixture() : profiles(BuildProfiles()), hardware(), log(),
		runtime(hardware, profiles, log) {}
	static mister::Profiles BuildProfiles()
	{
		mister::Profiles profiles;
		assert(profiles.Add(mister_test::CartProfile()).ok());
		assert(profiles.Add(mister_test::BiosProfile()).ok());
		return profiles;
	}

	mister::Profiles profiles;
	mister_test::FakeHardware hardware;
	mister_test::CaptureLog log;
	mister::Runtime runtime;
};

bool WaitForState(mister::Runtime&, State);

void TestSaveFailurePreservesSessionForStopRetry()
{
	Fixture f;
	assert(f.runtime.Start().ok());
	assert(f.runtime.LaunchGame(CartLaunch()).ok());
	const auto before = f.runtime.status();
	f.hardware.flush_result = {ErrorCode::save_failed, "disk full"};
	assert(f.runtime.Stop().code == ErrorCode::save_failed);
	const auto failed = f.runtime.status();
	assert(failed.state == before.state && failed.execution == before.execution);
	assert(failed.system == before.system && failed.core == before.core);
	assert(failed.error.code == ErrorCode::save_failed);
	assert(failed.error.phase == "save");
	assert(f.hardware.restore_input_calls == 1);
	assert(f.hardware.restored_input_generations ==
		std::vector<std::uint64_t>({before.generation}));
	assert(f.hardware.idle_calls == 1);
	assert(f.hardware.flush_calls == 1);
	assert(f.runtime.LaunchGame(CartLaunch()).code == ErrorCode::busy);
	f.hardware.flush_result = {};
	assert(f.runtime.Stop().ok());
	assert(f.hardware.flush_calls == 2 && f.hardware.idle_calls == 2);
	assert(f.runtime.status().state == State::idle);
	assert(f.runtime.Stop().ok());
	assert(f.hardware.flush_calls == 2);
}

void TestSaveFailureInputRestoreFailureRequiresRecovery()
{
	Fixture f;
	assert(f.runtime.Start().ok());
	assert(f.runtime.LaunchGame(CartLaunch()).ok());
	f.hardware.flush_result = {ErrorCode::save_failed, "disk full"};
	f.hardware.restore_input_result = {ErrorCode::io_failed,
		"input reopen failed"};
	const mister::Error error = f.runtime.Stop();
	assert(error.code == ErrorCode::idle_failed);
	assert(error.phase == "recovery");
	const mister::Status failed = f.runtime.status();
	assert(failed.state == State::reboot_required);
	assert(failed.generation == 0);
	assert(failed.error.code == ErrorCode::idle_failed);
	assert(f.runtime.Stop().code == ErrorCode::idle_failed);
}

void TestInputFaultDuringFailedSaveCannotDiscardSnapshot()
{
	Fixture f;
	assert(f.runtime.Start().ok());
	assert(f.runtime.LaunchGame(CartLaunch()).ok());
	const auto generation = f.hardware.launch_generations.back();
	f.hardware.on_flush = [&] {
		f.hardware.ReportFault(generation, {ErrorCode::io_failed, "late input error"});
	};
	f.hardware.flush_result = {ErrorCode::save_failed, "disk full"};
	assert(f.runtime.Stop().code == ErrorCode::save_failed);
	assert(!f.hardware.WaitForIdleCalls(2));
	assert(f.runtime.status().state == State::running_game);
	f.hardware.on_flush = {};
	f.hardware.flush_result = {};
	assert(f.runtime.Stop().ok());
	assert(f.hardware.idle_calls == 2);
}

void TestInputFaultDuringRestoreIsOwnedByRestoredGeneration()
{
	Fixture f;
	assert(f.runtime.Start().ok());
	assert(f.runtime.LaunchGame(CartLaunch()).ok());
	const std::uint64_t generation = f.hardware.launch_generations.back();
	f.hardware.flush_result = {ErrorCode::save_failed, "disk full"};
	f.hardware.on_restore_input = [&] {
		f.hardware.ReportFault(generation,
			{ErrorCode::io_failed, "restored input failed immediately"});
	};
	assert(f.runtime.Stop().code == ErrorCode::save_failed);
	assert(f.hardware.WaitForIdleCalls(2));
	assert(WaitForState(f.runtime, State::idle));
	assert(f.runtime.status().error.message ==
		"restored input failed immediately");
}

void Start(Fixture& fixture)
{
	assert(fixture.runtime.Start().ok());
}

bool HasLog(const std::vector<mister::LogRecord>& records,
	const char* operation, const char* phase, ErrorCode code = ErrorCode::none)
{
	for (const mister::LogRecord& record : records) {
		if (record.operation == operation && record.phase == phase &&
			record.error.code == code) return true;
	}
	return false;
}

bool WaitForState(mister::Runtime& runtime, State state)
{
	const auto deadline = std::chrono::steady_clock::now() +
		std::chrono::seconds(2);
	do {
		if (runtime.status().state == state) return true;
		std::this_thread::yield();
	} while (std::chrono::steady_clock::now() < deadline);
	return runtime.status().state == state;
}

class StatusReentrantLog final : public mister::LogSink {
public:
	void Enable(mister::Runtime& runtime)
	{
		std::lock_guard<std::mutex> lock(mutex_);
		runtime_ = &runtime;
		enabled_ = true;
	}

	void Write(const mister::LogRecord&) override
	{
		mister::Runtime* runtime = nullptr;
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (enabled_) runtime = runtime_;
		}
		if (runtime == nullptr) return;
		const mister::Status status = runtime->status();
		{
			std::lock_guard<std::mutex> lock(mutex_);
			observed_state_ = status.state;
			reentered_ = true;
		}
		condition_.notify_all();
	}

	bool WaitUntilReentered()
	{
		std::unique_lock<std::mutex> lock(mutex_);
		return condition_.wait_for(lock, std::chrono::seconds(2),
			[this]() { return reentered_; });
	}

	mister::State observed_state() const
	{
		std::lock_guard<std::mutex> lock(mutex_);
		return observed_state_;
	}

private:
	mutable std::mutex mutex_;
	std::condition_variable condition_;
	mister::Runtime* runtime_ = nullptr;
	bool enabled_ = false;
	bool reentered_ = false;
	mister::State observed_state_ = mister::State::starting;
};

class BlockingFaultIdleLog final : public mister::LogSink {
public:
	void Write(const mister::LogRecord& record) override
	{
		bool block = false;
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (armed_ && record.operation == "input_fault" &&
				record.phase == "idle") {
				armed_ = false;
				blocked_ = true;
				block = true;
			}
		}
		condition_.notify_all();
		if (!block) return;
		std::unique_lock<std::mutex> lock(mutex_);
		condition_.wait(lock, [this]() { return released_; });
	}

	void Arm()
	{
		std::lock_guard<std::mutex> lock(mutex_);
		armed_ = true;
		blocked_ = false;
		released_ = false;
	}

	bool WaitUntilBlocked()
	{
		std::unique_lock<std::mutex> lock(mutex_);
		return condition_.wait_for(lock, std::chrono::seconds(2),
			[this]() { return blocked_; });
	}

	void Release()
	{
		{
			std::lock_guard<std::mutex> lock(mutex_);
			released_ = true;
		}
		condition_.notify_all();
	}

private:
	std::mutex mutex_;
	std::condition_variable condition_;
	bool armed_ = false;
	bool blocked_ = false;
	bool released_ = false;
};

void TestStartLoadsIdleOnceAndPublishesIdle()
{
	Fixture fixture;
	assert(fixture.runtime.Start().ok());
	assert(fixture.hardware.idle_calls == 1);
	assert(fixture.runtime.status().state == State::idle);
}

void TestFailedStartRequiresReboot()
{
	Fixture fixture;
	fixture.hardware.idle_result.error = {ErrorCode::program_failed,
		"idle program", "programming", "MENU", "OTHER"};
	assert(fixture.runtime.Start().code == ErrorCode::idle_failed);
	assert(fixture.hardware.idle_calls == 1);
	assert(fixture.runtime.status().state == State::reboot_required);
	assert(fixture.runtime.status().error.phase == "recovery");
	const mister::Error stopped = fixture.runtime.Stop();
	assert(stopped.phase == "recovery");
	assert(stopped.expected == "MENU" && stopped.observed == "OTHER");
}

void TestValidationPrecedesHardwareMutation()
{
	Fixture fixture;
	Start(fixture);
	mister::Launch invalid = CartLaunch();
	invalid.system = "not_known";
	assert(fixture.runtime.LaunchGame(invalid).code == ErrorCode::unknown_system);
	assert(fixture.hardware.launch_calls == 0);
}

void TestUnsupportedPackageLeavesRunningSessionExactlyUntouched()
{
	Fixture fixture;
	Start(fixture);
	assert(fixture.runtime.LaunchGame(CartLaunch()).ok());
	const mister::Status before = fixture.runtime.status();
	const std::vector<std::string> events = fixture.hardware.events;
	fixture.hardware.admission_result = {
		ErrorCode::unsupported_protocol, "package driver unavailable"};
	const mister::Error error = fixture.runtime.LoadCore(
		"/packages/custom", std::string(64, 'a'));
	assert(error.code == ErrorCode::unsupported_protocol);
	const mister::Status after = fixture.runtime.status();
	assert(after.state == before.state && after.execution == before.execution);
	assert(after.system == before.system && after.core == before.core);
	assert(after.error.code == before.error.code &&
		after.error.message == before.error.message);
	assert(fixture.hardware.events == events);
	assert(fixture.hardware.flush_calls == 0);
	assert(fixture.hardware.core_calls == 0);
}

void TestPackageSaveFailurePreventsProgrammingAndRetainsActiveGeneration()
{
	Fixture fixture;
	Start(fixture);
	assert(fixture.runtime.LaunchGame(CartLaunch()).ok());
	const std::uint64_t generation = fixture.hardware.launch_generations.back();
	fixture.hardware.flush_result = {ErrorCode::save_failed, "disk full"};
	assert(fixture.runtime.LoadCore("/packages/custom", std::string(64, 'a')).code ==
		ErrorCode::save_failed);
	assert(fixture.hardware.core_calls == 0);
	assert(fixture.hardware.restore_input_calls == 1);
	assert(fixture.hardware.restored_input_generations ==
		std::vector<std::uint64_t>({generation}));
	assert(fixture.runtime.status().state == State::running_game);
	fixture.hardware.ReportFault(generation,
		{ErrorCode::io_failed, "still active after failed save"});
	assert(fixture.hardware.WaitForIdleCalls(2));
	assert(WaitForState(fixture.runtime, State::idle));
}

void TestPackageGenerationsRejectOldFaultsAndAcceptTheActiveFault()
{
	Fixture fixture;
	Start(fixture);
	const std::string id(64, 'a');
	assert(fixture.runtime.LoadCore("/packages/custom", id).ok());
	assert(fixture.runtime.Stop().ok());
	assert(fixture.runtime.LoadCore("/packages/custom", id).ok());
	assert(fixture.hardware.core_generations ==
		std::vector<std::uint64_t>({1, 2}));
	fixture.hardware.ReportFault(1,
		{ErrorCode::io_failed, "stale package input fault"});
	fixture.hardware.ReportFault(2,
		{ErrorCode::io_failed, "active package input fault"});
	assert(fixture.hardware.WaitForIdleCalls(3));
	assert(WaitForState(fixture.runtime, State::idle));
	assert(fixture.runtime.status().error.message == "active package input fault");
}

void TestDevelopmentPathRejectsEmbeddedNulBeforeHardwareMutation()
{
	Fixture fixture;
	Start(fixture);
	const std::string path("/cores/real.rbf\0ignored.rbf", 27);
	assert(fixture.runtime.LoadDevelopmentRBF(path).code == ErrorCode::invalid_request);
	assert(fixture.hardware.development_calls == 0);
}

void TestBlockedLaunchPublishesStarting()
{
	Fixture fixture;
	Start(fixture);
	fixture.hardware.BlockLaunch();
	std::thread launch([&fixture]() {
		assert(fixture.runtime.LaunchGame(CartLaunch()).ok());
	});
	fixture.hardware.WaitUntilLaunchEntered();
	const mister::Status status = fixture.runtime.status();
	assert(status.state == State::starting);
	assert(status.execution == Execution::game);
	assert(status.system == "test_cart");
	fixture.hardware.ReleaseLaunch();
	launch.join();
}

void TestBusyAndMismatchEmitTypedFences()
{
	mister_test::CaptureDiagnostic capture;
	mister::DiagnosticInstall install(&capture);
	Fixture fixture;
	Start(fixture);
	assert(fixture.runtime.Start().code == ErrorCode::busy);
	assert(capture.Count("fence.ownership") >= 1);
	capture.Clear();
	fixture.hardware.launch_result.observed_core = "WRONG";
	fixture.hardware.launch_result.mutation_attempted = true;
	assert(fixture.runtime.LaunchGame(CartLaunch()).code == ErrorCode::core_mismatch);
	assert(capture.Count("fence.abi") >= 1);
	assert(capture.Count("fence.recovery") >= 1);
}

void TestConcurrentMutationReturnsBusyWithoutQueueing()
{
	Fixture fixture;
	Start(fixture);
	fixture.hardware.BlockLaunch();
	std::thread launch([&fixture]() {
		assert(fixture.runtime.LaunchGame(CartLaunch()).ok());
	});
	fixture.hardware.WaitUntilLaunchEntered();
	assert(fixture.runtime.LoadDevelopmentRBF("/cores/dev.rbf").code ==
		ErrorCode::busy);
	assert(fixture.hardware.development_calls == 0);
	fixture.hardware.ReleaseLaunch();
	launch.join();
}

void TestRejectedMutationLogCanReadStatusWithoutDeadlock()
{
	mister::Profiles profiles = Fixture::BuildProfiles();
	mister_test::FakeHardware hardware;
	StatusReentrantLog log;
	mister::Runtime runtime(hardware, profiles, log);
	assert(runtime.Start().ok());
	log.Enable(runtime);
	mister::Error second_start;
	std::thread rejected([&runtime, &second_start]() {
		second_start = runtime.Start();
	});
	if (!log.WaitUntilReentered()) {
		fputs("reentrant log could not read status\n", stderr);
		abort();
	}
	rejected.join();
	assert(second_start.code == ErrorCode::busy);
	assert(log.observed_state() == State::idle);
}

void TestStatusRemainsReadableDuringMutation()
{
	Fixture fixture;
	Start(fixture);
	fixture.hardware.BlockLaunch();
	std::thread launch([&fixture]() {
		assert(fixture.runtime.LaunchGame(CartLaunch()).ok());
	});
	fixture.hardware.WaitUntilLaunchEntered();
	assert(fixture.runtime.status().state == State::starting);
	fixture.hardware.ReleaseLaunch();
	launch.join();
}

void TestSuccessfulGameRecordsIdentity()
{
	Fixture fixture;
	Start(fixture);
	assert(fixture.runtime.LaunchGame(CartLaunch()).ok());
	const mister::Status status = fixture.runtime.status();
	assert(status.state == State::running_game);
	assert(status.execution == Execution::game);
	assert(status.system == "test_cart");
	assert(status.core == "TESTCART");
}

void TestWrongObservedCoreCleansUpOnce()
{
	Fixture fixture;
	Start(fixture);
	fixture.hardware.launch_result.observed_core = "WRONG";
	fixture.hardware.launch_result.mutation_attempted = true;
	assert(fixture.runtime.LaunchGame(CartLaunch()).code ==
		ErrorCode::core_mismatch);
	assert(fixture.hardware.idle_calls == 2);
	assert(fixture.runtime.status().state == State::idle);
}

void TestPreMutationFailureDoesNotCleanUp()
{
	Fixture fixture;
	Start(fixture);
	fixture.hardware.launch_result.error = {ErrorCode::program_failed, "before write"};
	fixture.hardware.launch_result.mutation_attempted = false;
	assert(fixture.runtime.LaunchGame(CartLaunch()).code ==
		ErrorCode::program_failed);
	assert(fixture.hardware.idle_calls == 1);
}

void TestPostMutationFailureCleansUpExactlyOnce()
{
	Fixture fixture;
	Start(fixture);
	fixture.hardware.launch_result.error = {ErrorCode::io_failed, "after write"};
	fixture.hardware.launch_result.mutation_attempted = true;
	assert(fixture.runtime.LaunchGame(CartLaunch()).code == ErrorCode::io_failed);
	assert(fixture.hardware.idle_calls == 2);
}

void TestSuccessfulCleanupPreservesPrimaryError()
{
	Fixture fixture;
	Start(fixture);
	fixture.hardware.launch_result.error = {ErrorCode::io_failed, "primary"};
	fixture.hardware.launch_result.mutation_attempted = true;
	assert(fixture.runtime.LaunchGame(CartLaunch()).code == ErrorCode::io_failed);
	const mister::Status status = fixture.runtime.status();
	assert(status.state == State::idle);
	assert(status.error.code == ErrorCode::io_failed);
}

void TestFailedCleanupRequiresReboot()
{
	Fixture fixture;
	Start(fixture);
	fixture.hardware.launch_result.error = {ErrorCode::io_failed, "primary"};
	fixture.hardware.launch_result.mutation_attempted = true;
	fixture.hardware.idle_result.error = {ErrorCode::program_failed, "cleanup idle"};
	assert(fixture.runtime.LaunchGame(CartLaunch()).code ==
		ErrorCode::idle_failed);
	const mister::Status status = fixture.runtime.status();
	assert(status.state == State::reboot_required);
	assert(status.error.code == ErrorCode::idle_failed);
}

void TestDevelopmentHasNoGameIdentity()
{
	Fixture fixture;
	Start(fixture);
	fixture.hardware.development_result.observed_core = "MegaDrive";
	assert(fixture.runtime.LoadDevelopmentRBF("/cores/dev.rbf").ok());
	const mister::Status status = fixture.runtime.status();
	assert(status.state == State::running_development);
	assert(status.execution == Execution::development);
	assert(status.system.empty());
	assert(status.core == "MegaDrive");
}

void TestEveryDevelopmentFailureUsesItsMutationBoundary()
{
	Fixture preflight;
	Start(preflight);
	preflight.hardware.development_result = {
		{ErrorCode::io_failed, "preflight"}, false, ""};
	assert(preflight.runtime.LoadDevelopmentRBF("/cores/dev.rbf").code ==
		ErrorCode::io_failed);
	assert(preflight.hardware.idle_calls == 1);
	assert(preflight.runtime.status().state == State::idle);
	assert(preflight.runtime.status().error.message == "preflight");

	const std::vector<const char*> post_mutation_failures = {
		"HDMI power-down write", "FPGA programming", "core synchronization",
		"core observation",
	};
	for (const char* failure : post_mutation_failures) {
		Fixture fixture;
		Start(fixture);
		fixture.hardware.development_result = {
			{ErrorCode::io_failed, failure}, true, ""};
		const mister::Error error =
			fixture.runtime.LoadDevelopmentRBF("/cores/dev.rbf");
		assert(error.code == ErrorCode::io_failed);
		assert(error.message == failure);
		assert(fixture.hardware.idle_calls == 2);
		const mister::Status status = fixture.runtime.status();
		assert(status.state == State::idle);
		assert(status.error.code == ErrorCode::io_failed);
		assert(status.error.message == failure);
	}
}

void TestDevelopmentCleanupFailureRequiresReboot()
{
	Fixture fixture;
	Start(fixture);
	fixture.hardware.development_result = {
		{ErrorCode::io_failed, "core observation"}, true, ""};
	fixture.hardware.idle_result.error = {
		ErrorCode::program_failed, "development cleanup idle failed"};
	const mister::Error error =
		fixture.runtime.LoadDevelopmentRBF("/cores/dev.rbf");
	assert(error.code == ErrorCode::idle_failed);
	assert(error.message == "development cleanup idle failed");
	assert(fixture.hardware.idle_calls == 2);
	const mister::Status status = fixture.runtime.status();
	assert(status.state == State::reboot_required);
	assert(status.error.code == ErrorCode::idle_failed);
	assert(status.error.message == "development cleanup idle failed");
}

void TestStopFromBothRunningStatesLoadsIdleOnce()
{
	Fixture game;
	Start(game);
	assert(game.runtime.LaunchGame(CartLaunch()).ok());
	assert(game.runtime.Stop().ok());
	assert(game.hardware.idle_calls == 2);
	Fixture development;
	Start(development);
	assert(development.runtime.LoadDevelopmentRBF("/dev.rbf").ok());
	assert(development.runtime.Stop().ok());
	assert(development.hardware.idle_calls == 2);
	assert(development.runtime.LaunchGame(CartLaunch()).ok());
	assert(development.runtime.status().state == State::running_game);
}

void TestStopFromIdleIsIdempotent()
{
	Fixture fixture;
	Start(fixture);
	assert(fixture.runtime.Stop().ok());
	assert(fixture.runtime.Stop().ok());
	assert(fixture.hardware.idle_calls == 1);
}

void TestStopFromRebootRequiredDoesNotCallHardware()
{
	Fixture fixture;
	fixture.hardware.idle_result.error = {ErrorCode::program_failed, "failed"};
	assert(fixture.runtime.Start().code == ErrorCode::idle_failed);
	assert(fixture.runtime.Stop().code == ErrorCode::idle_failed);
	assert(fixture.hardware.idle_calls == 1);
}

void TestRunningStateRejectsBothLaunchKinds()
{
	Fixture fixture;
	Start(fixture);
	assert(fixture.runtime.LaunchGame(CartLaunch()).ok());
	assert(fixture.runtime.LaunchGame(CartLaunch()).code == ErrorCode::busy);
	assert(fixture.runtime.LoadDevelopmentRBF("/dev.rbf").code ==
		ErrorCode::busy);
	assert(fixture.hardware.launch_calls == 1);
	assert(fixture.hardware.development_calls == 0);
}

void TestRebootRequiredRejectsBothLaunchKinds()
{
	Fixture fixture;
	fixture.hardware.idle_result.error = {ErrorCode::program_failed, "failed"};
	assert(fixture.runtime.Start().code == ErrorCode::idle_failed);
	assert(fixture.runtime.LaunchGame(CartLaunch()).code == ErrorCode::busy);
	assert(fixture.runtime.LoadDevelopmentRBF("/dev.rbf").code ==
		ErrorCode::busy);
	assert(fixture.hardware.launch_calls == 0);
	assert(fixture.hardware.development_calls == 0);
}

void TestFailedStartAndStopNeverPerformSecondCleanup()
{
	Fixture start;
	start.hardware.idle_result.error = {ErrorCode::program_failed, "failed"};
	assert(start.runtime.Start().code == ErrorCode::idle_failed);
	assert(start.hardware.idle_calls == 1);
	Fixture stop;
	Start(stop);
	assert(stop.runtime.LaunchGame(CartLaunch()).ok());
	stop.hardware.idle_result.error = {ErrorCode::program_failed, "failed"};
	assert(stop.runtime.Stop().code == ErrorCode::idle_failed);
	assert(stop.hardware.idle_calls == 2);
}

void TestSuccessfulLaunchLogsExpectedAndConfirmedCore()
{
	Fixture fixture;
	Start(fixture);
	fixture.log.Clear();
	assert(fixture.runtime.LaunchGame(CartLaunch()).ok());
	const std::vector<mister::LogRecord> records = fixture.log.records();
	assert(HasLog(records, "launch", "validate"));
	assert(HasLog(records, "launch", "starting"));
	assert(HasLog(records, "launch", "running"));
	bool expected = false;
	bool confirmed = false;
	for (const mister::LogRecord& record : records) {
		expected = expected || (record.system == "test_cart" &&
			record.core == "TESTCART" && record.phase == "starting");
		confirmed = confirmed || (record.system == "test_cart" &&
			record.core == "TESTCART" && record.phase == "running");
	}
	assert(expected && confirmed);
}

void TestValidationFailureLogsDirectErrorWithoutHardware()
{
	Fixture fixture;
	Start(fixture);
	fixture.log.Clear();
	mister::Launch launch = CartLaunch();
	launch.rbf = "relative";
	assert(fixture.runtime.LaunchGame(launch).code ==
		ErrorCode::invalid_request);
	assert(fixture.hardware.launch_calls == 0);
	assert(HasLog(fixture.log.records(), "launch", "validate",
		ErrorCode::invalid_request));
}

void TestPostMutationFailureLogsCleanupAndPrimary()
{
	Fixture fixture;
	Start(fixture);
	fixture.log.Clear();
	fixture.hardware.launch_result.error = {ErrorCode::io_failed, "primary"};
	fixture.hardware.launch_result.mutation_attempted = true;
	assert(fixture.runtime.LaunchGame(CartLaunch()).code == ErrorCode::io_failed);
	const std::vector<mister::LogRecord> records = fixture.log.records();
	assert(HasLog(records, "launch", "failure", ErrorCode::io_failed));
	assert(HasLog(records, "launch", "cleanup"));
	assert(fixture.runtime.status().error.code == ErrorCode::io_failed);
}

void TestFailedCleanupLogsBothFailuresAndDevelopmentInventsNoIdentity()
{
	Fixture failed;
	Start(failed);
	failed.log.Clear();
	failed.hardware.launch_result.error = {ErrorCode::io_failed, "primary"};
	failed.hardware.launch_result.mutation_attempted = true;
	failed.hardware.idle_result.error = {ErrorCode::program_failed, "idle"};
	assert(failed.runtime.LaunchGame(CartLaunch()).code ==
		ErrorCode::idle_failed);
	const std::vector<mister::LogRecord> records = failed.log.records();
	assert(HasLog(records, "launch", "failure", ErrorCode::io_failed));
	assert(HasLog(records, "launch", "cleanup", ErrorCode::idle_failed));
	Fixture development;
	Start(development);
	development.log.Clear();
	assert(development.runtime.LoadDevelopmentRBF("/dev.rbf").ok());
	for (const mister::LogRecord& record : development.log.records()) {
		assert(record.operation != "load_development_rbf" ||
			(record.system.empty() && record.core.empty()));
	}
}

void TestRuntimeOwnsFaultSinkBeforeStartupAndReleasesItOnDestruction()
{
	mister::Profiles profiles = Fixture::BuildProfiles();
	mister_test::FakeHardware hardware;
	mister_test::CaptureLog log;
	{
		mister::Runtime runtime(hardware, profiles, log);
		assert(hardware.fault_sink_sets == 1);
		assert(runtime.Start().ok());
		assert(!hardware.idle_without_fault_sink);
	}
	assert(hardware.fault_sink_sets == 2);
}

void TestStopAndImmediateRelaunchUseStrictlyNewGenerations()
{
	Fixture fixture;
	Start(fixture);
	assert(fixture.runtime.LaunchGame(CartLaunch()).ok());
	assert(fixture.runtime.Stop().ok());
	assert(fixture.runtime.LaunchGame(CartLaunch()).ok());
	assert(fixture.hardware.launch_generations ==
		std::vector<std::uint64_t>({1, 2}));
	assert(fixture.runtime.status().state == State::running_game);
}

void TestFaultQueuedDuringLaunchRunsOffReporterAndCleansActiveGenerationOnce()
{
	Fixture fixture;
	Start(fixture);
	fixture.hardware.BlockLaunch();
	mister::Error launch_result;
	std::thread launch([&]() {
		launch_result = fixture.runtime.LaunchGame(CartLaunch());
	});
	fixture.hardware.WaitUntilLaunchEntered();
	assert(fixture.hardware.launch_generations ==
		std::vector<std::uint64_t>({1}));
	const std::thread::id reporter = std::this_thread::get_id();
	fixture.hardware.ReportFault(1,
		{ErrorCode::io_failed, "queued input read failed"});
	assert(fixture.hardware.idle_calls == 1);
	fixture.hardware.ReleaseLaunch();
	launch.join();
	assert(launch_result.ok());
	assert(fixture.hardware.WaitForIdleCalls(2));
	assert(WaitForState(fixture.runtime, State::idle));
	const mister::Status status = fixture.runtime.status();
	assert(status.error.code == ErrorCode::io_failed);
	assert(status.error.message == "queued input read failed");
	assert(fixture.hardware.idle_calls == 2);
	assert(fixture.hardware.idle_threads.size() == 2);
	assert(fixture.hardware.idle_threads[1] != reporter);
}

void TestStaleFaultCannotCleanOrOverwriteANewerGeneration()
{
	Fixture fixture;
	Start(fixture);
	assert(fixture.runtime.LaunchGame(CartLaunch()).ok());
	assert(fixture.runtime.Stop().ok());
	assert(fixture.runtime.LaunchGame(CartLaunch()).ok());
	fixture.hardware.ReportFault(1,
		{ErrorCode::io_failed, "stale input failure"});
	fixture.hardware.ReportFault(2,
		{ErrorCode::io_failed, "active input failure"});
	assert(fixture.hardware.WaitForIdleCalls(3));
	assert(WaitForState(fixture.runtime, State::idle));
	const mister::Status status = fixture.runtime.status();
	assert(status.error.code == ErrorCode::io_failed);
	assert(status.error.message == "active input failure");
	assert(fixture.hardware.idle_calls == 3);
}

void TestActiveInputFaultCleanupFailureRequiresReboot()
{
	Fixture fixture;
	Start(fixture);
	assert(fixture.runtime.LaunchGame(CartLaunch()).ok());
	fixture.hardware.idle_result.error = {
		ErrorCode::program_failed, "fault cleanup idle failed"};
	fixture.hardware.ReportFault(1,
		{ErrorCode::io_failed, "input device removed"});
	assert(fixture.hardware.WaitForIdleCalls(2));
	assert(WaitForState(fixture.runtime, State::reboot_required));
	const mister::Status status = fixture.runtime.status();
	assert(status.error.code == ErrorCode::idle_failed);
	assert(status.error.message == "fault cleanup idle failed");
	assert(fixture.runtime.Stop().code == ErrorCode::idle_failed);
	assert(fixture.hardware.idle_calls == 2);
}

void TestActiveFaultRetiresPublishedIdentityBeforeBlockedRecovery()
{
	Fixture fixture;
	Start(fixture);
	const std::string id(64, 'a');
	assert(fixture.runtime.LoadCore("/packages/custom", id).ok());
	fixture.hardware.BlockNextIdle();
	fixture.hardware.ReportFault(1,
		{ErrorCode::io_failed, "active package input failure"});
	fixture.hardware.WaitUntilIdleEntered();
	const mister::Status recovering = fixture.runtime.status();
	assert(recovering.state == State::starting);
	assert(recovering.execution == Execution::none);
	assert(recovering.system.empty() && recovering.core.empty());
	assert(recovering.generation == 0);
	assert(recovering.active_package.package_id.empty());
	assert(recovering.capabilities.active_interfaces.empty());
	fixture.hardware.ReleaseIdle();
	assert(fixture.hardware.WaitForIdleCalls(2));
	assert(WaitForState(fixture.runtime, State::idle));
	assert(fixture.runtime.status().error.phase == "input");
}

void TestQueuedActiveFaultReservesCleanupBeforeStopAndPreservesError()
{
	mister::Profiles profiles = Fixture::BuildProfiles();
	mister_test::FakeHardware hardware;
	BlockingFaultIdleLog log;
	mister::Runtime runtime(hardware, profiles, log);
	assert(runtime.Start().ok());
	assert(runtime.LaunchGame(CartLaunch()).ok());

	log.Arm();
	hardware.ReportFault(1,
		{ErrorCode::io_failed, "first input failure"});
	assert(log.WaitUntilBlocked());
	assert(runtime.status().state == State::idle);
	assert(runtime.LaunchGame(CartLaunch()).ok());

	hardware.ReportFault(2,
		{ErrorCode::io_failed, "reserved input failure"});
	const mister::Error stop = runtime.Stop();
	assert(stop.code == ErrorCode::busy);
	assert(hardware.idle_calls == 2);

	log.Release();
	assert(hardware.WaitForIdleCalls(3));
	assert(WaitForState(runtime, State::idle));
	const mister::Status status = runtime.status();
	assert(status.error.code == ErrorCode::io_failed);
	assert(status.error.message == "reserved input failure");
	assert(hardware.idle_calls == 3);
}

void TestInspectionAndProtocol2IdentityShareTheLifecycleGeneration()
{
	Fixture fixture;
	Start(fixture);
	fixture.hardware.core_info.system = "pong";
	fixture.hardware.core_info.descriptor.core.system = "pong";
	fixture.hardware.core_info.descriptor.interfaces = {
		{"fes.video.fixed-720p60", 1, 0, true},
		{"vendor.optional", 1, 0, false},
		{"fes.gamepad", 1, 0, true}};
	const std::string id(64, 'a');
	mister::CorePackageInspection inspection;
	assert(fixture.runtime.InspectCore("/tmp/package", id, &inspection).ok());
	assert(inspection.package_id == id && inspection.compatible);
	assert(fixture.hardware.inspection_calls == 1);
	assert(fixture.runtime.status().generation == 0);
	assert(fixture.hardware.core_calls == 0);

	assert(fixture.runtime.LoadCore("/tmp/package", id).ok());
	mister::Status active = fixture.runtime.status();
	assert(active.generation == 1);
	assert(active.active_package.package_id == id);
	assert(active.active_package.descriptor.build.id == std::string(32, 'c'));
	assert(active.active_package.observed.abi.id == "fes.simple-game");
	assert(active.active_package.observed.build_id == std::string(32, 'c'));
	assert(active.system.empty());
	assert(active.active_package.descriptor.core.system == "pong");
	assert(active.capabilities.active_interfaces.size() == 2);
	assert(active.capabilities.active_interfaces[0].id == "fes.gamepad");
	assert(active.capabilities.active_interfaces[1].id ==
		"fes.video.fixed-720p60");
	assert(fixture.runtime.InspectCore("/tmp/package", id, &inspection).ok());
	assert(fixture.runtime.status().generation == 1);

	assert(fixture.runtime.Stop().ok());
	assert(fixture.runtime.status().generation == 0);
	assert(fixture.runtime.status().active_package.package_id.empty());
	assert(fixture.runtime.LoadContainedDevelopmentRBF("/tmp/core.rbf").ok());
	active = fixture.runtime.status();
	assert(active.generation == 2);
	assert(active.active_package.package_id.empty());
	assert(active.capabilities.active_interfaces.empty());
	assert(fixture.hardware.development_generations.back() == 2);
	assert(fixture.runtime.Stop().ok());
}

} // namespace

void TestStreamBindingCapacityOwnershipAndStop()
{
	Fixture f;
	const std::string id(64, 'a');
	f.hardware.supported.media_stream = {{"fes.media.blob-stream", 1, 0}, 1, 32768, 512};
	assert(f.runtime.Start().ok());
	assert(f.runtime.status().capabilities.media_stream.interface.id.empty());
	assert(f.runtime.LoadCore("/packages/sms", id).ok());
	const auto generation = f.runtime.status().generation;
	assert(generation > 0);
	assert(f.runtime.status().capabilities.media_stream.max_bytes == 32768);
	for (auto size : {0u, 32769u, 33554433u, 0xffffffffu})
		assert(!f.runtime.LoadComputerMediaStream("/media", id, generation, size).ok());
	assert(!f.runtime.LoadComputerMediaStream("/media", id, 0, 1).ok());
	assert(!f.runtime.LoadComputerMediaStream("/media", id, generation + 1, 1).ok());
	assert(!f.runtime.LoadComputerMediaStream("/media", std::string(64, 'b'), generation, 1).ok());
	assert(!f.runtime.LoadComputerMediaStream("relative", id, generation, 1).ok());
	assert(f.hardware.media_stream_calls == 0);
	f.hardware.on_media_stream = [&] {
		assert(f.runtime.Stop().code == ErrorCode::busy);
		assert(f.runtime.LoadCore("/packages/sms", id).code == ErrorCode::busy);
		assert(f.runtime.LoadComputerMediaStream("/media", id, generation, 3).code == ErrorCode::busy);
	};
	assert(f.runtime.LoadComputerMediaStream("/media", id, generation, 32768).ok());
	assert(f.hardware.media_stream_calls == 1 && f.hardware.media_stream_size == 32768);
	assert(f.hardware.media_stream_path == "/media");
	f.hardware.media_stream_result = {ErrorCode::io_failed, "transfer failed", "input"};
	assert(!f.runtime.LoadComputerMediaStream("/media", id, generation, 3).ok());
	assert(f.runtime.status().generation == generation);
	assert(f.runtime.status().active_package.package_id == id);
	assert(f.runtime.Stop().ok());
	assert(f.runtime.status().capabilities.media_stream.interface.id.empty());
	f.hardware.on_media_stream = {};
	f.hardware.media_stream_result = {};
	assert(f.runtime.LoadCore("/packages/sms", id).ok());
	assert(f.runtime.status().generation != generation);
	assert(!f.runtime.LoadComputerMediaStream("/media", id, generation, 3).ok());
	assert(f.runtime.LoadComputerMediaStream("/media", id, f.runtime.status().generation, 3).ok());
	const auto current = f.runtime.status().generation;
	f.hardware.on_media_stream = [&] {
		f.hardware.ReportFault(current, {ErrorCode::io_failed, "same-generation input fault", "input"});
	};
	f.hardware.media_stream_result = {ErrorCode::io_failed, "ambiguous cleanup", "recovery"};
	assert(!f.runtime.LoadComputerMediaStream("/media", id, current, 3).ok());
	assert(f.runtime.status().state == State::reboot_required);
	assert(f.runtime.status().generation == current);
	assert(f.runtime.status().active_package.package_id == id);
	const auto calls = f.hardware.media_stream_calls;
	assert(!f.runtime.LoadComputerMediaStream("/media", id, current, 3).ok());
	const auto idle_calls = f.hardware.idle_calls;
	const auto stopped = f.runtime.Stop();
	assert(stopped.code == ErrorCode::io_failed && stopped.phase == "recovery");
	assert(stopped.message == "ambiguous cleanup");
	f.hardware.ReportFault(current, {ErrorCode::io_failed, "late retired-generation fault", "input"});
	assert(f.runtime.Stop().message == "ambiguous cleanup");
	assert(f.runtime.status().error.message == "ambiguous cleanup");
	assert(f.hardware.idle_calls == idle_calls); // no replay or background cleanup
	assert(f.hardware.media_stream_calls == calls);
}

int main()
{
	TestStreamBindingCapacityOwnershipAndStop();
	TestInputFaultDuringFailedSaveCannotDiscardSnapshot();
	TestInputFaultDuringRestoreIsOwnedByRestoredGeneration();
	TestSaveFailurePreservesSessionForStopRetry();
	TestSaveFailureInputRestoreFailureRequiresRecovery();
	TestStartLoadsIdleOnceAndPublishesIdle();
	TestFailedStartRequiresReboot();
	TestValidationPrecedesHardwareMutation();
	TestUnsupportedPackageLeavesRunningSessionExactlyUntouched();
	TestPackageSaveFailurePreventsProgrammingAndRetainsActiveGeneration();
	TestPackageGenerationsRejectOldFaultsAndAcceptTheActiveFault();
	TestDevelopmentPathRejectsEmbeddedNulBeforeHardwareMutation();
	TestBlockedLaunchPublishesStarting();
	TestBusyAndMismatchEmitTypedFences();
	TestConcurrentMutationReturnsBusyWithoutQueueing();
	TestRejectedMutationLogCanReadStatusWithoutDeadlock();
	TestStatusRemainsReadableDuringMutation();
	TestSuccessfulGameRecordsIdentity();
	TestWrongObservedCoreCleansUpOnce();
	TestPreMutationFailureDoesNotCleanUp();
	TestPostMutationFailureCleansUpExactlyOnce();
	TestSuccessfulCleanupPreservesPrimaryError();
	TestFailedCleanupRequiresReboot();
	TestDevelopmentHasNoGameIdentity();
	TestEveryDevelopmentFailureUsesItsMutationBoundary();
	TestDevelopmentCleanupFailureRequiresReboot();
	TestStopFromBothRunningStatesLoadsIdleOnce();
	TestStopFromIdleIsIdempotent();
	TestStopFromRebootRequiredDoesNotCallHardware();
	TestRunningStateRejectsBothLaunchKinds();
	TestRebootRequiredRejectsBothLaunchKinds();
	TestFailedStartAndStopNeverPerformSecondCleanup();
	TestSuccessfulLaunchLogsExpectedAndConfirmedCore();
	TestValidationFailureLogsDirectErrorWithoutHardware();
	TestPostMutationFailureLogsCleanupAndPrimary();
	TestFailedCleanupLogsBothFailuresAndDevelopmentInventsNoIdentity();
	TestRuntimeOwnsFaultSinkBeforeStartupAndReleasesItOnDestruction();
	TestStopAndImmediateRelaunchUseStrictlyNewGenerations();
	TestFaultQueuedDuringLaunchRunsOffReporterAndCleansActiveGenerationOnce();
	TestStaleFaultCannotCleanOrOverwriteANewerGeneration();
	TestActiveInputFaultCleanupFailureRequiresReboot();
	TestActiveFaultRetiresPublishedIdentityBeforeBlockedRecovery();
	TestQueuedActiveFaultReservesCleanupBeforeStopAndPreservesError();
	TestInspectionAndProtocol2IdentityShareTheLifecycleGeneration();
	puts("runtime_test: 42 passed");
	return 0;
}
