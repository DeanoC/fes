// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "capture_log.hpp"
#include "fake_hardware.hpp"
#include "libmister-runtime/runtime.h"
#include "test_profiles.hpp"

#include <assert.h>
#include <stdio.h>

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
	fixture.hardware.idle_result.error = {ErrorCode::program_failed, "idle program"};
	assert(fixture.runtime.Start().code == ErrorCode::idle_failed);
	assert(fixture.hardware.idle_calls == 1);
	assert(fixture.runtime.status().state == State::reboot_required);
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
	assert(fixture.runtime.LoadDevelopmentRBF("/cores/dev.rbf").ok());
	const mister::Status status = fixture.runtime.status();
	assert(status.state == State::running_development);
	assert(status.execution == Execution::development);
	assert(status.system.empty());
	assert(status.core.empty());
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

} // namespace

int main()
{
	TestStartLoadsIdleOnceAndPublishesIdle();
	TestFailedStartRequiresReboot();
	TestValidationPrecedesHardwareMutation();
	TestBlockedLaunchPublishesStarting();
	TestConcurrentMutationReturnsBusyWithoutQueueing();
	TestStatusRemainsReadableDuringMutation();
	TestSuccessfulGameRecordsIdentity();
	TestWrongObservedCoreCleansUpOnce();
	TestPreMutationFailureDoesNotCleanUp();
	TestPostMutationFailureCleansUpExactlyOnce();
	TestSuccessfulCleanupPreservesPrimaryError();
	TestFailedCleanupRequiresReboot();
	TestDevelopmentHasNoGameIdentity();
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
	puts("runtime_test: 23 passed");
	return 0;
}
