// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "capture_diagnostic.hpp"
#include "capture_log.hpp"
#include "fake_hardware.hpp"
#include "libmister-runtime/runtime.h"

#include <assert.h>
#include <stdio.h>

#include <chrono>
#include <fcntl.h>
#include <condition_variable>
#include <mutex>
#include <thread>
#include <vector>

namespace {

using mister::ErrorCode;
using mister::Execution;
using mister::State;


struct Fixture {
 Fixture() : hardware(), log(), runtime(hardware, log) {}
 mister_test::FakeHardware hardware;
 mister_test::CaptureLog log;
 mister::Runtime runtime;
};

bool WaitForState(mister::Runtime&, State);

void TestSaveFailurePreservesSessionForStopRetry()
{
	Fixture f;
	assert(f.runtime.Start().ok());
	assert(f.runtime.LoadCore("/package", std::string(64, 'a')).ok());
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
	assert(f.runtime.LoadCore("/package", std::string(64, 'a')).code == ErrorCode::save_failed);
	f.hardware.flush_result = {};
	assert(f.runtime.Stop().ok());
	assert(f.hardware.flush_calls == 3 && f.hardware.idle_calls == 2);
	assert(f.runtime.status().state == State::idle);
	assert(f.runtime.Stop().ok());
	assert(f.hardware.flush_calls == 3);
}

void TestSaveFailureInputRestoreFailureRequiresRecovery()
{
	Fixture f;
	assert(f.runtime.Start().ok());
	assert(f.runtime.LoadCore("/package", std::string(64, 'a')).ok());
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
	assert(f.runtime.LoadCore("/package", std::string(64, 'a')).ok());
	const auto generation = f.hardware.core_generations.back();
	f.hardware.on_flush = [&] {
		f.hardware.ReportFault(generation, {ErrorCode::io_failed, "late input error"});
	};
	f.hardware.flush_result = {ErrorCode::save_failed, "disk full"};
	assert(f.runtime.Stop().code == ErrorCode::save_failed);
	assert(!f.hardware.WaitForIdleCalls(2));
	assert(f.runtime.status().state == State::running_development);
	f.hardware.on_flush = {};
	f.hardware.flush_result = {};
	assert(f.runtime.Stop().ok());
	assert(f.hardware.idle_calls == 2);
}

void TestInputFaultDuringRestoreIsOwnedByRestoredGeneration()
{
	Fixture f;
	assert(f.runtime.Start().ok());
	assert(f.runtime.LoadCore("/package", std::string(64, 'a')).ok());
	const std::uint64_t generation = f.hardware.core_generations.back();
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

void TestNonMenuIdlePublishesIdleWithoutReboot()
{
	Fixture fixture;
	fixture.hardware.idle_result.observed_core = "OTHER";
	assert(fixture.runtime.Start().ok());
	assert(fixture.hardware.idle_calls == 1);
	assert(fixture.runtime.status().state == State::idle);
	assert(fixture.runtime.status().error.ok());
	assert(fixture.runtime.Stop().ok());
	assert(fixture.runtime.status().state == State::idle);
}

void TestProbeLessIdlePublishesIdleWithoutReboot()
{
	mister_test::CaptureDiagnostic capture;
	mister::DiagnosticInstall install(&capture);
	Fixture fixture;
	fixture.hardware.idle_result.observed_core.clear();
	assert(fixture.runtime.Start().ok());
	assert(fixture.hardware.idle_calls == 1);
	assert(fixture.runtime.status().state == State::idle);
	assert(fixture.runtime.status().error.ok());
	assert(capture.Count("corename.change") == 0);
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


void TestControllerSnapshotBindingAndFaultCleanup()
{
	Fixture f;
	Start(f);
	const std::string id(64, 'a');
	assert(f.runtime.LoadCore("/packages/custom", id).ok());
	const auto generation = f.runtime.status().generation;
	assert(f.runtime.SetController(id, generation + 1, 0, 0, 0).code == ErrorCode::invalid_request);
	assert(f.runtime.SetController(std::string(64, 'b'), generation, 0, 0, 0).code == ErrorCode::invalid_request);
	assert(f.runtime.SetController(id, generation, 2, 0, 0).code == ErrorCode::invalid_request);
	assert(f.runtime.SetController(id, generation, 0, 256, 0).code == ErrorCode::invalid_request);
	assert(f.runtime.SetController(id, generation, 0, 0, 4096).code == ErrorCode::invalid_request);
	assert(f.hardware.controller_calls == 0);
	f.hardware.on_controller = [&] { assert(f.runtime.Stop().code == ErrorCode::busy); };
	assert(f.runtime.SetController(id, generation, 1, 255, 4095).ok());
	assert(f.hardware.controller_snapshot == std::vector<std::uint16_t>({1, 255, 4095}));
	f.hardware.controller_result = {ErrorCode::unsupported_interface, "unsupported"};
	assert(!f.runtime.SetController(id, generation, 0, 0, 0).ok());
	assert(f.runtime.status().state == State::running_development);
	f.hardware.controller_result = {ErrorCode::io_failed, "partial snapshot", "input"};
	assert(!f.runtime.SetController(id, generation, 0, 0, 0).ok());
	assert(f.hardware.WaitForIdleCalls(2));
	assert(WaitForState(f.runtime, State::idle));
	assert(f.runtime.status().error.message == "partial snapshot");
	assert(f.runtime.SetController(id, generation, 0, 0, 0).code == ErrorCode::busy);
}

void TestUnsupportedPackageLeavesRunningSessionExactlyUntouched()
{
	Fixture fixture;
	Start(fixture);
	assert(fixture.runtime.LoadCore("/package", std::string(64, 'a')).ok());
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
	assert(fixture.hardware.core_calls == 1);
}

void TestPackageSaveFailurePreventsProgrammingAndRetainsActiveGeneration()
{
	Fixture fixture;
	Start(fixture);
	assert(fixture.runtime.LoadCore("/package", std::string(64, 'a')).ok());
	const std::uint64_t generation = fixture.hardware.core_generations.back();
	fixture.hardware.flush_result = {ErrorCode::save_failed, "disk full"};
	assert(fixture.runtime.LoadCore("/packages/custom", std::string(64, 'a')).code ==
		ErrorCode::save_failed);
	assert(fixture.hardware.core_calls == 1);
	assert(fixture.hardware.restore_input_calls == 1);
	assert(fixture.hardware.restored_input_generations ==
		std::vector<std::uint64_t>({generation}));
	assert(fixture.runtime.status().state == State::running_development);
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
	assert(fixture.runtime.LoadContainedDevelopmentRBF(path).code == ErrorCode::invalid_request);
	assert(fixture.hardware.development_calls == 0);
}

void TestBlockedLaunchPublishesStarting()
{
	Fixture fixture;
	Start(fixture);
	fixture.hardware.BlockLaunch();
	std::thread launch([&fixture]() {
		assert(fixture.runtime.LoadCore("/package", std::string(64, 'a')).ok());
	});
	fixture.hardware.WaitUntilLaunchEntered();
	const mister::Status status = fixture.runtime.status();
	assert(status.state == State::starting);
	assert(status.execution == Execution::development);
	assert(status.system.empty());
	fixture.hardware.ReleaseLaunch();
	launch.join();
}


void TestConcurrentMutationReturnsBusyWithoutQueueing()
{
	Fixture fixture;
	Start(fixture);
	fixture.hardware.BlockLaunch();
	std::thread launch([&fixture]() {
		assert(fixture.runtime.LoadCore("/package", std::string(64, 'a')).ok());
	});
	fixture.hardware.WaitUntilLaunchEntered();
	assert(fixture.runtime.LoadContainedDevelopmentRBF("/cores/dev.rbf").code ==
		ErrorCode::busy);
	assert(fixture.hardware.development_calls == 0);
	fixture.hardware.ReleaseLaunch();
	launch.join();
}

void TestRejectedMutationLogCanReadStatusWithoutDeadlock()
{
	mister_test::FakeHardware hardware;
	StatusReentrantLog log;
	mister::Runtime runtime(hardware, log);
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
		assert(fixture.runtime.LoadCore("/package", std::string(64, 'a')).ok());
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
	assert(fixture.runtime.LoadCore("/package", std::string(64, 'a')).ok());
	const mister::Status status = fixture.runtime.status();
	assert(status.state == State::running_development);
	assert(status.execution == Execution::development);
	assert(status.system.empty());
	assert(status.core == "custom-core");
}


void TestPreMutationFailureDoesNotCleanUp()
{
	Fixture fixture;
	Start(fixture);
	fixture.hardware.core_result.error = {ErrorCode::program_failed, "before write"};
	fixture.hardware.core_result.mutation_attempted = false;
	assert(fixture.runtime.LoadCore("/package", std::string(64, 'a')).code ==
		ErrorCode::program_failed);
	assert(fixture.hardware.idle_calls == 1);
}

void TestPostMutationFailureCleansUpExactlyOnce()
{
	Fixture fixture;
	Start(fixture);
	fixture.hardware.core_result.error = {ErrorCode::io_failed, "after write"};
	fixture.hardware.core_result.mutation_attempted = true;
	assert(fixture.runtime.LoadCore("/package", std::string(64, 'a')).code == ErrorCode::io_failed);
	assert(fixture.hardware.idle_calls == 2);
}

void TestSuccessfulCleanupPreservesPrimaryError()
{
	Fixture fixture;
	Start(fixture);
	fixture.hardware.core_result.error = {ErrorCode::io_failed, "primary"};
	fixture.hardware.core_result.mutation_attempted = true;
	assert(fixture.runtime.LoadCore("/package", std::string(64, 'a')).code == ErrorCode::io_failed);
	const mister::Status status = fixture.runtime.status();
	assert(status.state == State::idle);
	assert(status.error.code == ErrorCode::io_failed);
}

void TestFailedCleanupRequiresReboot()
{
	Fixture fixture;
	Start(fixture);
	fixture.hardware.core_result.error = {ErrorCode::io_failed, "primary"};
	fixture.hardware.core_result.mutation_attempted = true;
	fixture.hardware.idle_result.error = {ErrorCode::program_failed, "cleanup idle"};
	assert(fixture.runtime.LoadCore("/package", std::string(64, 'a')).code ==
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
	assert(fixture.runtime.LoadContainedDevelopmentRBF("/cores/dev.rbf").ok());
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
	assert(preflight.runtime.LoadContainedDevelopmentRBF("/cores/dev.rbf").code ==
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
			fixture.runtime.LoadContainedDevelopmentRBF("/cores/dev.rbf");
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
		fixture.runtime.LoadContainedDevelopmentRBF("/cores/dev.rbf");
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
	assert(game.runtime.LoadCore("/package", std::string(64, 'a')).ok());
	assert(game.runtime.Stop().ok());
	assert(game.hardware.idle_calls == 2);
	Fixture development;
	Start(development);
	assert(development.runtime.LoadContainedDevelopmentRBF("/dev.rbf").ok());
	assert(development.runtime.Stop().ok());
	assert(development.hardware.idle_calls == 2);
	assert(development.runtime.LoadCore("/package", std::string(64, 'a')).ok());
	assert(development.runtime.status().state == State::running_development);
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

void TestRecoverIdleRetriesProgramFromRebootRequired()
{
	Fixture fixture;
	fixture.hardware.idle_result.error = {ErrorCode::program_failed, "failed"};
	assert(fixture.runtime.Start().code == ErrorCode::idle_failed);
	assert(fixture.hardware.idle_calls == 1);
	fixture.hardware.idle_result.error = {};
	assert(fixture.runtime.RecoverIdle().ok());
	assert(fixture.runtime.status().state == State::idle);
	assert(fixture.runtime.status().error.code == ErrorCode::none);
	assert(fixture.hardware.idle_calls == 2);
	assert(fixture.runtime.Stop().ok());
	assert(fixture.hardware.idle_calls == 2);
}

void TestRecoverIdleFailureStaysRebootRequired()
{
	Fixture fixture;
	fixture.hardware.idle_result.error = {ErrorCode::program_failed, "failed"};
	assert(fixture.runtime.Start().code == ErrorCode::idle_failed);
	assert(fixture.runtime.RecoverIdle().code == ErrorCode::idle_failed);
	assert(fixture.runtime.status().state == State::reboot_required);
	assert(fixture.hardware.idle_calls == 2);
	assert(fixture.runtime.Stop().code == ErrorCode::idle_failed);
	assert(fixture.hardware.idle_calls == 2);
}


void TestRebootRequiredRejectsBothLaunchKinds()
{
	Fixture fixture;
	fixture.hardware.idle_result.error = {ErrorCode::program_failed, "failed"};
	assert(fixture.runtime.Start().code == ErrorCode::idle_failed);
	assert(fixture.runtime.LoadCore("/package", std::string(64, 'a')).code == ErrorCode::busy);
	assert(fixture.runtime.LoadContainedDevelopmentRBF("/dev.rbf").code ==
		ErrorCode::busy);
	assert(fixture.hardware.core_calls == 0);
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
	assert(stop.runtime.LoadCore("/package", std::string(64, 'a')).ok());
	stop.hardware.idle_result.error = {ErrorCode::program_failed, "failed"};
	assert(stop.runtime.Stop().code == ErrorCode::idle_failed);
	assert(stop.hardware.idle_calls == 2);
}



void TestPostMutationFailureLogsCleanupAndPrimary()
{
	Fixture fixture;
	Start(fixture);
	fixture.log.Clear();
	fixture.hardware.core_result.error = {ErrorCode::io_failed, "primary"};
	fixture.hardware.core_result.mutation_attempted = true;
	assert(fixture.runtime.LoadCore("/package", std::string(64, 'a')).code == ErrorCode::io_failed);
	const std::vector<mister::LogRecord> records = fixture.log.records();
	assert(HasLog(records, "load_core", "failure", ErrorCode::io_failed));
	assert(HasLog(records, "load_core", "cleanup"));
	assert(fixture.runtime.status().error.code == ErrorCode::io_failed);
}

void TestFailedCleanupLogsBothFailuresAndDevelopmentInventsNoIdentity()
{
	Fixture failed;
	Start(failed);
	failed.log.Clear();
	failed.hardware.core_result.error = {ErrorCode::io_failed, "primary"};
	failed.hardware.core_result.mutation_attempted = true;
	failed.hardware.idle_result.error = {ErrorCode::program_failed, "idle"};
	assert(failed.runtime.LoadCore("/package", std::string(64, 'a')).code ==
		ErrorCode::idle_failed);
	const std::vector<mister::LogRecord> records = failed.log.records();
	assert(HasLog(records, "load_core", "failure", ErrorCode::io_failed));
	assert(HasLog(records, "load_core", "cleanup", ErrorCode::idle_failed));
	Fixture development;
	Start(development);
	development.log.Clear();
	assert(development.runtime.LoadContainedDevelopmentRBF("/dev.rbf").ok());
	for (const mister::LogRecord& record : development.log.records()) {
		assert(record.operation != "load_development_rbf" ||
			(record.system.empty() && record.core.empty()));
	}
}

void TestRuntimeOwnsFaultSinkBeforeStartupAndReleasesItOnDestruction()
{
	mister_test::FakeHardware hardware;
	mister_test::CaptureLog log;
	{
		mister::Runtime runtime(hardware, log);
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
	assert(fixture.runtime.LoadCore("/package", std::string(64, 'a')).ok());
	assert(fixture.runtime.Stop().ok());
	assert(fixture.runtime.LoadCore("/package", std::string(64, 'a')).ok());
	assert(fixture.hardware.core_generations ==
		std::vector<std::uint64_t>({1, 2}));
	assert(fixture.runtime.status().state == State::running_development);
}

void TestFaultQueuedDuringLaunchRunsOffReporterAndCleansActiveGenerationOnce()
{
	Fixture fixture;
	Start(fixture);
	fixture.hardware.BlockLaunch();
	mister::Error core_result;
	std::thread launch([&]() {
		core_result = fixture.runtime.LoadCore("/package", std::string(64, 'a'));
	});
	fixture.hardware.WaitUntilLaunchEntered();
	assert(fixture.hardware.core_generations ==
		std::vector<std::uint64_t>({1}));
	const std::thread::id reporter = std::this_thread::get_id();
	fixture.hardware.ReportFault(1,
		{ErrorCode::io_failed, "queued input read failed"});
	assert(fixture.hardware.idle_calls == 1);
	fixture.hardware.ReleaseLaunch();
	launch.join();
	assert(core_result.ok());
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
	assert(fixture.runtime.LoadCore("/package", std::string(64, 'a')).ok());
	assert(fixture.runtime.Stop().ok());
	assert(fixture.runtime.LoadCore("/package", std::string(64, 'a')).ok());
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
	assert(fixture.runtime.LoadCore("/package", std::string(64, 'a')).ok());
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
	mister_test::FakeHardware hardware;
	BlockingFaultIdleLog log;
	mister::Runtime runtime(hardware, log);
	assert(runtime.Start().ok());
	assert(runtime.LoadCore("/package", std::string(64, 'a')).ok());

	log.Arm();
	hardware.ReportFault(1,
		{ErrorCode::io_failed, "first input failure"});
	assert(log.WaitUntilBlocked());
	assert(runtime.status().state == State::idle);
	assert(runtime.LoadCore("/package", std::string(64, 'a')).ok());

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

void TestLiveMediaGenerationBindingAndBusy()
{
	Fixture f;
	const std::string id(64, 'a');
	f.hardware.core_info.descriptor.abi = {"fes.simple-computer", 1, 0};
	f.hardware.core_info.descriptor.interfaces = {
		{"fes.keyboard", 1, 0, true},
		{"fes.media.blob", 1, 0, true},
		{"fes.video.fixed-720p60", 1, 0, true}};
	assert(f.runtime.Start().ok());
	assert(f.runtime.LoadCore("/packages/zx81", id).ok());
	const auto generation = f.runtime.status().generation;
	assert(generation > 0);
	assert(f.runtime.status().active_package.descriptor.abi.id == "fes.simple-computer");

	assert(f.runtime.LoadComputerMedia("/tmp/a.p").ok());
	assert(f.hardware.media_calls == 1 && f.hardware.live_media_calls == 0);
	assert(f.hardware.media_path == "/tmp/a.p");

	assert(!f.runtime.ReplaceLiveComputerMedia("/tmp/b.p", id, 0).ok());
	assert(!f.runtime.ReplaceLiveComputerMedia("/tmp/b.p", std::string(64, 'b'), generation).ok());
	assert(!f.runtime.ReplaceLiveComputerMedia("relative.p", id, generation).ok());
	assert(f.hardware.live_media_calls == 0);

	f.hardware.on_live_media = [&] {
		assert(f.runtime.ReplaceLiveComputerMedia("/tmp/b.p", id, generation).code ==
			ErrorCode::busy);
		assert(f.runtime.ClearComputerMedia(id, generation).code == ErrorCode::busy);
		assert(f.runtime.Stop().code == ErrorCode::busy);
	};
	assert(f.runtime.ReplaceLiveComputerMedia("/tmp/b.p", id, generation).ok());
	assert(f.hardware.live_media_calls == 1);
	assert(f.hardware.live_media_path == "/tmp/b.p");
	assert(f.hardware.media_calls == 1);

	f.hardware.live_media_result = {ErrorCode::busy, "tape loader is busy", "input"};
	assert(f.runtime.ReplaceLiveComputerMedia("/tmp/c.p", id, generation).code ==
		ErrorCode::busy);
	f.hardware.live_media_result = {};

	assert(f.runtime.ClearComputerMedia(id, generation).ok());
	assert(f.hardware.clear_media_calls == 1);
	assert(!f.runtime.ClearComputerMedia(id, generation + 1).ok());
	assert(f.hardware.clear_media_calls == 1);

	assert(f.runtime.Stop().ok());
	assert(!f.runtime.ReplaceLiveComputerMedia("/tmp/b.p", id, generation).ok());
	assert(f.hardware.live_media_calls == 2);
}


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

void TestCompositionUsesExistingLifecycle()
{
	Fixture f;
	assert(f.runtime.Start().ok());
	const std::string id(64, 'a');
	mister::CoreCompositionRequest request;
	request.composition.id = std::string(64, 'e');
	request.composition.package_id = id;
	assert(f.runtime.LoadComposedCore("/base", id, request).ok());
	auto status = f.runtime.status();
	assert(status.active_package.package_id == id);
	assert(status.active_package.composition.id == request.composition.id);
	const auto generation = status.generation;
	const auto calls = f.hardware.core_calls;
	f.hardware.admission_result = {ErrorCode::invalid_package, "invalid composition"};
	assert(!f.runtime.LoadComposedCore("/bad", id, request).ok());
	assert(f.runtime.status().generation == generation);
	assert(f.runtime.status().active_package.composition.id == request.composition.id);
	assert(f.hardware.core_calls == calls);
	f.hardware.admission_result = {};
	assert(f.runtime.LoadCore("/plain", id).ok());
	assert(f.runtime.status().active_package.composition.id.empty());
	assert(f.runtime.LoadComposedCore("/base", id, request).ok());
	assert(f.runtime.Stop().ok());
	assert(f.runtime.status().active_package.composition.id.empty());
}

void UseComputerPackage(mister_test::FakeHardware& hardware)
{
	hardware.core_info.descriptor.core.id = "fes.apple2";
	hardware.core_info.declared_core = "fes.apple2";
	hardware.core_info.descriptor.abi = {"fes.computer", 1, 0};
	hardware.core_info.descriptor.interfaces = {{"fes.video.fixed-720p60", 1, 0, true},
		{"fes.keyboard.hid", 1, 0, true}, {"fes.gamepad.ports", 1, 0, true},
		{"fes.media.apple2-floppy", 1, 0, true}, {"fes.expansion.apple2-bus", 1, 0, false}};
	hardware.supported.abis.push_back({"fes.computer", 1, 0, {{"fes.audio.pcm-s16-stereo-48k", 1, 0},
		{"fes.expansion.apple2-bus", 1, 0}, {"fes.gamepad.ports", 1, 0}, {"fes.keyboard.hid", 1, 0},
		{"fes.media.apple2-floppy", 1, 0}, {"fes.video.fixed-720p60", 1, 0}}});
	mister::MediaUnitCapability unit;
	unit.unit = 0;
	unit.interface = {"fes.media.apple2-floppy", 1, 0};
	unit.min_bytes = unit.max_bytes = 143360;
	unit.chunk_bytes = 512;
	hardware.supported.media_units = {unit};
}

void TestComputerKeyboardBindingSerializationAndFaults()
{
	Fixture f;
	UseComputerPackage(f.hardware);
	Start(f);
	const std::string id(64, 'a');
	mister::KeyboardHidRows rows{};
	rows[0] = 0x0010;
	rows[8] = 0x0002;
	assert(f.runtime.SetKeyboardHid(id, 1, rows).code == ErrorCode::busy);
	assert(f.runtime.LoadCore("/packages/apple2", id).ok());
	const auto generation = f.runtime.status().generation;
	assert(f.runtime.status().active_package.observed.abi.id == "fes.computer");
	auto reserved = rows;
	reserved[0] |= 0x0001;
	auto modifier = rows;
	modifier[8] = 0x0100;
	assert(f.runtime.SetKeyboardHid(id, generation, reserved).code == ErrorCode::invalid_request);
	assert(f.runtime.SetKeyboardHid(id, generation, modifier).code == ErrorCode::invalid_request);
	assert(f.runtime.SetKeyboardHid(id, generation + 1, rows).code == ErrorCode::invalid_request);
	assert(f.runtime.SetKeyboardHid(std::string(64, 'b'), generation, rows).code == ErrorCode::invalid_request);
	assert(f.runtime.SetKeyboardHid(id, 0, rows).code == ErrorCode::invalid_request);
	assert(f.hardware.keyboard_hid_calls == 0);
	f.hardware.on_keyboard_hid = [&] {
		assert(f.runtime.Stop().code == ErrorCode::busy);
		assert(f.runtime.SetKeyboardHid(id, generation, rows).code == ErrorCode::busy);
		assert(f.runtime.EjectMedia(id, generation, 0).code == ErrorCode::busy);
	};
	assert(f.runtime.SetKeyboardHid(id, generation, rows).ok());
	assert(f.hardware.keyboard_hid_calls == 1 && f.hardware.keyboard_hid_rows == rows);
	f.hardware.on_keyboard_hid = {};
	f.hardware.keyboard_hid_result = {ErrorCode::unsupported_interface, "inactive", "input"};
	assert(f.runtime.SetKeyboardHid(id, generation, rows).code == ErrorCode::unsupported_interface);
	assert(f.runtime.status().state == State::running_development);
	// A failed row exchange retires the generation like a controller fault.
	f.hardware.keyboard_hid_result = {ErrorCode::io_failed, "partial HID snapshot", "input"};
	assert(!f.runtime.SetKeyboardHid(id, generation, rows).ok());
	assert(f.hardware.WaitForIdleCalls(2));
	assert(WaitForState(f.runtime, State::idle));
	assert(f.runtime.status().error.message == "partial HID snapshot");
	assert(f.runtime.status().capabilities.media_units.empty());
	assert(f.runtime.SetKeyboardHid(id, generation, rows).code == ErrorCode::busy);

	// Other ABIs, and computers without the keyboard, never reach hardware.
	Fixture game;
	Start(game);
	assert(game.runtime.LoadCore("/packages/pong", id).ok());
	const auto calls = game.hardware.keyboard_hid_calls;
	assert(game.runtime.SetKeyboardHid(id, game.runtime.status().generation, rows).code ==
		ErrorCode::unsupported_interface);
	Fixture silent;
	UseComputerPackage(silent.hardware);
	silent.hardware.core_info.descriptor.interfaces.erase(
		silent.hardware.core_info.descriptor.interfaces.begin() + 1);
	Start(silent);
	assert(silent.runtime.LoadCore("/packages/apple2", id).ok());
	assert(silent.runtime.SetKeyboardHid(id, silent.runtime.status().generation, rows).code ==
		ErrorCode::unsupported_interface);
	assert(game.hardware.keyboard_hid_calls == calls && silent.hardware.keyboard_hid_calls == 0);
}

void TestComputerMediaUnitsStayLiveAndReportUnitState()
{
	Fixture f;
	UseComputerPackage(f.hardware);
	Start(f);
	const std::string id(64, 'a');
	assert(f.runtime.InsertMedia("/media/dos33.dsk", id, 1, 0, 143360).code == ErrorCode::busy);
	assert(f.runtime.status().capabilities.media_units.empty());
	assert(f.runtime.LoadCore("/packages/apple2", id).ok());
	const auto generation = f.runtime.status().generation;
	auto units = f.runtime.status().capabilities.media_units;
	assert(units.size() == 1 && units[0].unit == 0 && units[0].state == mister::MediaUnitState::empty);
	for (const auto& invalid : std::vector<std::pair<std::string, std::uint32_t>>{
		{"relative.dsk", 143360}, {"/media/dos33.dsk", 0}, {"/media/dos33.dsk", 33554433}})
		assert(f.runtime.InsertMedia(invalid.first, id, generation, 0, invalid.second).code ==
			ErrorCode::invalid_request);
	assert(f.runtime.InsertMedia("/media/dos33.dsk", id, generation, 8, 143360).code ==
		ErrorCode::invalid_request);
	assert(f.runtime.InsertMedia("/media/dos33.dsk", id, generation, 0, 143359).code ==
		ErrorCode::invalid_request);
	assert(f.runtime.InsertMedia("/media/dos33.dsk", id, generation, 1, 143360).code ==
		ErrorCode::unsupported_interface);
	assert(f.runtime.InsertMedia("/media/dos33.dsk", id, generation + 1, 0, 143360).code ==
		ErrorCode::invalid_request);
	assert(f.runtime.InsertMedia("/media/dos33.dsk", std::string(64, 'b'), generation, 0, 143360).code ==
		ErrorCode::invalid_request);
	assert(f.runtime.EjectMedia(id, generation, 1).code == ErrorCode::unsupported_interface);
	assert(f.hardware.insert_media_calls == 0 && f.hardware.eject_media_calls == 0);
	f.hardware.on_insert_media = [&] {
		assert(f.runtime.Stop().code == ErrorCode::busy);
		assert(f.runtime.SetKeyboardHid(id, generation, mister::KeyboardHidRows{}).code == ErrorCode::busy);
		assert(f.runtime.InsertMedia("/media/dos33.dsk", id, generation, 0, 143360).code == ErrorCode::busy);
		f.hardware.supported.media_units[0].state = mister::MediaUnitState::ready;
	};
	assert(f.runtime.InsertMedia("/media/dos33.dsk", id, generation, 0, 143360).ok());
	assert(f.hardware.insert_media_calls == 1 && f.hardware.insert_media_unit == 0);
	assert(f.hardware.insert_media_path == "/media/dos33.dsk" && f.hardware.insert_media_size == 143360);
	assert(f.runtime.status().capabilities.media_units[0].state == mister::MediaUnitState::ready);
	// A failed transfer reports its error and leaves the generation running.
	f.hardware.on_insert_media = [&] {
		f.hardware.supported.media_units[0].state = mister::MediaUnitState::loading;
	};
	f.hardware.insert_media_result = {ErrorCode::io_failed,
		"transfer failed; media eject failed: unavailable", "input"};
	const auto failed = f.runtime.InsertMedia("/media/other.dsk", id, generation, 0, 143360);
	assert(failed.code == ErrorCode::io_failed && failed.phase == "input");
	assert(f.runtime.status().state == State::running_development);
	assert(f.runtime.status().generation == generation);
	assert(f.runtime.status().capabilities.media_units[0].state == mister::MediaUnitState::loading);
	assert(f.hardware.idle_calls == 1);
	f.hardware.on_eject_media = [&] {
		f.hardware.supported.media_units[0].state = mister::MediaUnitState::empty;
	};
	assert(f.runtime.EjectMedia(id, generation, 0).ok());
	assert(f.hardware.eject_media_calls == 1 && f.hardware.eject_media_unit == 0);
	assert(f.runtime.status().capabilities.media_units[0].state == mister::MediaUnitState::empty);
	f.hardware.eject_media_result = {ErrorCode::io_failed, "eject failed", "input"};
	assert(f.runtime.EjectMedia(id, generation, 0).code == ErrorCode::io_failed);
	assert(f.runtime.status().state == State::running_development);
	assert(f.runtime.Stop().ok());
	assert(f.runtime.status().capabilities.media_units.empty());
	assert(f.runtime.EjectMedia(id, generation, 0).code == ErrorCode::busy);

	// Media units belong only to fes.computer generations.
	Fixture game;
	game.hardware.supported.media_units = f.hardware.supported.media_units;
	Start(game);
	assert(game.runtime.LoadCore("/packages/pong", id).ok());
	assert(game.runtime.InsertMedia("/media/dos33.dsk", id, game.runtime.status().generation, 0,
		143360).code == ErrorCode::unsupported_interface);
	assert(game.hardware.insert_media_calls == 0);
}

void TestMenuGenerationAndPreparationFencing()
{
 Fixture f;const std::string id(64,'a');
 assert(f.runtime.Start().ok());assert(!f.runtime.status().menu_display.available);
 assert(f.runtime.ConfigureMenuPackage("/menu",id).ok());
 const auto menu=f.runtime.status().menu_display;
 assert(menu.available&&menu.generation!=0&&f.runtime.status().generation==0);
 std::unique_ptr<mister::MenuFrame> frame;
 assert(!f.runtime.BeginMenuFrame(0,&frame).ok());
 assert(f.runtime.BeginMenuFrame(menu.generation,&frame).ok());
 std::unique_ptr<mister::MenuFrame> second;
 assert(f.runtime.BeginMenuFrame(menu.generation,&second).code==ErrorCode::busy);
 mister::MenuDisplayInfo info;
 assert(!f.runtime.PresentMenuFrame(menu.generation,*frame,&info).ok());assert(f.hardware.menu_present_calls==0);
 assert(fcntl(frame->fd(),F_ADD_SEALS,F_SEAL_WRITE|F_SEAL_GROW|F_SEAL_SHRINK|F_SEAL_SEAL)==0);
 f.hardware.admission_result={ErrorCode::invalid_package,"bad package"};
 assert(!f.runtime.LoadCore("/game",id).ok());assert(f.runtime.status().menu_display.generation==menu.generation);
 assert(f.runtime.PresentMenuFrame(menu.generation,*frame,&info).ok());assert(f.hardware.menu_present_calls==1);
 frame.reset();assert(f.runtime.BeginMenuFrame(menu.generation,&frame).ok());
 f.hardware.admission_result={};assert(f.runtime.LoadCore("/game",id).ok());
 assert(!f.runtime.status().menu_display.available);
 assert(!f.runtime.PresentMenuFrame(menu.generation,*frame,&info).ok());assert(f.hardware.menu_present_calls==1);
 assert(f.runtime.Stop().ok());const auto fresh=f.runtime.status().menu_display;
 assert(fresh.available&&fresh.generation>menu.generation);
 assert(!f.runtime.PresentMenuFrame(fresh.generation,*frame,&info).ok());
 frame.reset();assert(f.runtime.BeginMenuFrame(fresh.generation,&frame).ok());
 assert(f.runtime.ConfigureMenuPackage("relative",id).code==ErrorCode::invalid_request);
}

void TestMenuIdleStopAndPreMutationFailurePreserveFences()
{
 Fixture f;const std::string id(64,'a');assert(f.runtime.Start().ok());assert(f.runtime.ConfigureMenuPackage("/menu",id).ok());
 const auto generation=f.runtime.status().menu_display.generation;
 std::unique_ptr<mister::MenuFrame> frame;assert(f.runtime.BeginMenuFrame(generation,&frame).ok());
 f.hardware.core_result={{ErrorCode::invalid_package,"recheck failed"},false,""};
 assert(!f.runtime.LoadCore("/game",id).ok());assert(f.runtime.status().menu_display.generation==generation);
 assert(f.runtime.Stop().ok());assert(f.runtime.status().menu_display.generation>generation);
 assert(fcntl(frame->fd(),F_ADD_SEALS,F_SEAL_WRITE|F_SEAL_GROW|F_SEAL_SHRINK|F_SEAL_SEAL)==0);
 mister::MenuDisplayInfo info;assert(!f.runtime.PresentMenuFrame(generation,*frame,&info).ok());frame.reset();
 const auto fresh=f.runtime.status().menu_display.generation;assert(f.runtime.BeginMenuFrame(fresh,&frame).ok());
 assert(fcntl(frame->fd(),F_ADD_SEALS,F_SEAL_WRITE|F_SEAL_GROW|F_SEAL_SHRINK|F_SEAL_SEAL)==0);
 f.hardware.on_menu_present=[&]{assert(f.runtime.Stop().code==ErrorCode::busy);assert(f.runtime.ConfigureMenuPackage("/menu",id).code==ErrorCode::busy);};
 assert(f.runtime.PresentMenuFrame(fresh,*frame,&info).ok());
}

void TestMenuPresentationFailureUsesIdleRecovery()
{
 Fixture f;const std::string id(64,'a');assert(f.runtime.Start().ok());
 assert(f.runtime.ConfigureMenuPackage("/menu",id).ok());
 auto generation=f.runtime.status().menu_display.generation;
 std::unique_ptr<mister::MenuFrame> frame;assert(f.runtime.BeginMenuFrame(generation,&frame).ok());
 assert(fcntl(frame->fd(),F_ADD_SEALS,F_SEAL_WRITE|F_SEAL_GROW|F_SEAL_SHRINK|F_SEAL_SEAL)==0);
 f.hardware.menu_present_result={ErrorCode::io_failed,"display timeout"};
 f.hardware.idle_result={{ErrorCode::io_failed,"containment failed"},true,""};
 mister::MenuDisplayInfo info;assert(!f.runtime.PresentMenuFrame(generation,*frame,&info).ok());
 assert(f.runtime.status().state==State::reboot_required);
 assert(!f.runtime.status().menu_display.available);
 assert(!f.runtime.BeginMenuFrame(generation,&frame).ok());
}

void TestMenuFrameYieldsToCoreDataAndLaunch()
{
 for (const bool inspect_data : {true, false}) {
  Fixture f; const std::string id(64, 'a');
  assert(f.runtime.Start().ok());
  assert(f.runtime.ConfigureMenuPackage("/menu", id).ok());
  const auto generation = f.runtime.status().menu_display.generation;
  std::unique_ptr<mister::MenuFrame> frame;
  assert(f.runtime.BeginMenuFrame(generation, &frame).ok());
  assert(fcntl(frame->fd(), F_ADD_SEALS,
   F_SEAL_WRITE | F_SEAL_GROW | F_SEAL_SHRINK | F_SEAL_SEAL) == 0);
  std::mutex mutex; std::condition_variable condition;
  bool presenting = false; bool release = false;
  bool mutation_started = false; bool mutation_finished = false;
  f.hardware.on_menu_present = [&] {
   std::unique_lock<std::mutex> lock(mutex);
   presenting = true; condition.notify_all();
   condition.wait(lock, [&] { return release; });
  };
  mister::MenuDisplayInfo info;
  std::thread frame_thread([&] {
   assert(f.runtime.PresentMenuFrame(generation, *frame, &info).ok());
  });
  {
   std::unique_lock<std::mutex> lock(mutex);
   assert(condition.wait_for(lock, std::chrono::seconds(2), [&] { return presenting; }));
  }
  mister::Error mutation_error;
  std::thread mutation_thread([&] {
   {
    std::lock_guard<std::mutex> lock(mutex);
    mutation_started = true;
   }
   condition.notify_all();
   if (inspect_data) {
    mister::CoreData data;
    mutation_error = f.runtime.InspectCoreData("/game", id, "/data", &data);
   } else {
    mutation_error = f.runtime.LoadCore("/game", id);
   }
   {
    std::lock_guard<std::mutex> lock(mutex);
    mutation_finished = true;
   }
   condition.notify_all();
  });
  {
   std::unique_lock<std::mutex> lock(mutex);
   assert(condition.wait_for(lock, std::chrono::seconds(2), [&] { return mutation_started; }));
  }
  std::this_thread::sleep_for(std::chrono::milliseconds(100));
  {
   std::lock_guard<std::mutex> lock(mutex);
   assert(!mutation_finished);
   release = true;
  }
  condition.notify_all();
  frame_thread.join(); mutation_thread.join();
  if (inspect_data) assert(mutation_error.code == ErrorCode::unsupported_interface);
  else assert(mutation_error.ok());
  assert(f.runtime.status().state == (inspect_data ? State::idle : State::running_development));
  assert(f.runtime.Stop().ok());
  assert(f.runtime.status().menu_display.available);
 }
}

void TestMenuFrameWaitIsBounded()
{
 Fixture f; const std::string id(64, 'a');
 assert(f.runtime.Start().ok());
 assert(f.runtime.ConfigureMenuPackage("/menu", id).ok());
 const auto generation = f.runtime.status().menu_display.generation;
 std::unique_ptr<mister::MenuFrame> frame;
 assert(f.runtime.BeginMenuFrame(generation, &frame).ok());
 assert(fcntl(frame->fd(), F_ADD_SEALS,
  F_SEAL_WRITE | F_SEAL_GROW | F_SEAL_SHRINK | F_SEAL_SEAL) == 0);
 f.hardware.on_menu_present = [&] {
  const auto start = std::chrono::steady_clock::now();
  const auto result = f.runtime.LoadCore("/game", id);
  const auto elapsed = std::chrono::steady_clock::now() - start;
  assert(result.code == ErrorCode::busy);
  assert(elapsed >= std::chrono::milliseconds(1900));
  assert(elapsed < std::chrono::seconds(4));
 };
 mister::MenuDisplayInfo info;
 assert(f.runtime.PresentMenuFrame(generation, *frame, &info).ok());
 assert(f.runtime.status().state == State::idle);
 assert(f.runtime.LoadCore("/game", id).ok());
 assert(f.runtime.Stop().ok());
}

void SealMenuFrame(mister::Runtime& runtime, std::uint64_t generation,
	std::unique_ptr<mister::MenuFrame>* frame)
{
	assert(runtime.BeginMenuFrame(generation, frame).ok());
	assert(fcntl((*frame)->fd(), F_ADD_SEALS,
		F_SEAL_WRITE | F_SEAL_GROW | F_SEAL_SHRINK | F_SEAL_SEAL) == 0);
}

void TestMenuFrameYieldsToLifecycleOps()
{
	const std::string id(64, 'a');
	enum class Op { stop, develop, recover, configure };
	for (const Op op : {Op::stop, Op::develop, Op::recover, Op::configure}) {
		Fixture f;
		assert(f.runtime.Start().ok());
		assert(f.runtime.ConfigureMenuPackage("/menu", id).ok());
		const auto generation = f.runtime.status().menu_display.generation;
		const auto idle_before = f.hardware.idle_calls;
		std::unique_ptr<mister::MenuFrame> frame;
		SealMenuFrame(f.runtime, generation, &frame);
		std::mutex mutex;
		std::condition_variable condition;
		bool presenting = false, release = false, mutation_started = false;
		bool mutation_finished = false, begin_rejected = false;
		f.hardware.on_menu_present = [&] {
			std::unique_lock<std::mutex> lock(mutex);
			presenting = true;
			condition.notify_all();
			assert(condition.wait_for(lock, std::chrono::seconds(2), [&] { return mutation_started; }));
			lock.unlock();
			std::unique_ptr<mister::MenuFrame> rejected;
			const bool rejected_begin = !f.runtime.BeginMenuFrame(generation, &rejected).ok();
			lock.lock();
			begin_rejected = rejected_begin;
			if (op == Op::stop) assert(f.hardware.idle_calls == idle_before);
			if (op == Op::develop) assert(f.hardware.development_calls == 0);
			condition.notify_all();
			condition.wait(lock, [&] { return release; });
		};
		mister::MenuDisplayInfo info;
		std::thread frame_thread([&] {
			assert(f.runtime.PresentMenuFrame(generation, *frame, &info).ok());
		});
		{
			std::unique_lock<std::mutex> lock(mutex);
			assert(condition.wait_for(lock, std::chrono::seconds(2), [&] { return presenting; }));
		}
		mister::Error mutation_error;
		const auto started = std::chrono::steady_clock::now();
		std::thread mutation_thread([&] {
			{
				std::lock_guard<std::mutex> lock(mutex);
				mutation_started = true;
			}
			condition.notify_all();
			if (op == Op::stop) mutation_error = f.runtime.Stop();
			else if (op == Op::develop) mutation_error = f.runtime.LoadContainedDevelopmentRBF("/cores/dev.rbf");
			else if (op == Op::recover) mutation_error = f.runtime.RecoverIdle();
			else mutation_error = f.runtime.ConfigureMenuPackage("/menu", id);
			{
				std::lock_guard<std::mutex> lock(mutex);
				mutation_finished = true;
			}
			condition.notify_all();
		});
		{
			std::unique_lock<std::mutex> lock(mutex);
			assert(condition.wait_for(lock, std::chrono::seconds(2), [&] { return begin_rejected; }));
			assert(!mutation_finished);
			release = true;
		}
		condition.notify_all();
		frame_thread.join();
		mutation_thread.join();
		const auto elapsed = std::chrono::steady_clock::now() - started;
		assert(mutation_error.ok());
		assert(begin_rejected);
		assert(elapsed < std::chrono::milliseconds(1900));
		if (op == Op::stop) {
			assert(f.runtime.status().menu_display.available);
			assert(f.runtime.status().menu_display.generation > generation);
			assert(f.hardware.idle_calls == idle_before + 1);
		} else if (op == Op::develop) {
			assert(f.runtime.status().state == State::running_development);
			assert(!f.runtime.status().menu_display.available);
			assert(f.hardware.development_calls == 1);
			assert(f.runtime.Stop().ok());
		} else if (op == Op::recover) {
			assert(f.runtime.status().state == State::idle);
			assert(f.runtime.status().menu_display.generation == generation);
			assert(f.hardware.idle_calls == idle_before);
		} else {
			assert(f.runtime.status().menu_display.available);
			assert(f.runtime.status().menu_display.generation > generation);
		}
	}
}

void TestMenuLifecycleWaitIsBounded()
{
	const std::string id(64, 'a');
	for (int kind = 0; kind < 4; ++kind) {
		Fixture f;
		assert(f.runtime.Start().ok());
		assert(f.runtime.ConfigureMenuPackage("/menu", id).ok());
		const auto generation = f.runtime.status().menu_display.generation;
		const auto idle_before = f.hardware.idle_calls;
		std::unique_ptr<mister::MenuFrame> frame;
		SealMenuFrame(f.runtime, generation, &frame);
		f.hardware.on_menu_present = [&] {
			const auto start = std::chrono::steady_clock::now();
			mister::Error result;
			if (kind == 0) result = f.runtime.Stop();
			else if (kind == 1) result = f.runtime.LoadContainedDevelopmentRBF("/cores/dev.rbf");
			else if (kind == 2) result = f.runtime.RecoverIdle();
			else result = f.runtime.ConfigureMenuPackage("/menu", id);
			const auto elapsed = std::chrono::steady_clock::now() - start;
			assert(result.code == ErrorCode::busy);
			assert(elapsed >= std::chrono::milliseconds(1900));
			assert(elapsed < std::chrono::seconds(4));
		};
		mister::MenuDisplayInfo info;
		assert(f.runtime.PresentMenuFrame(generation, *frame, &info).ok());
		assert(f.runtime.status().state == State::idle);
		assert(f.runtime.status().menu_display.generation == generation);
		assert(f.hardware.idle_calls == idle_before);
		assert(f.hardware.development_calls == 0);
	}
}

void TestStaleMenuErrorIsNotPromotedOntoIdleStatus()
{
	Fixture f;
	const std::string id(64, 'a');
	assert(f.runtime.Start().ok());
	assert(f.runtime.ConfigureMenuPackage("/menu", id).ok());
	assert(f.runtime.LoadCore("/game", id).ok());
	f.hardware.menu_status.error = {ErrorCode::io_failed, "stale menu map", "menu_memory"};
	f.hardware.menu_configured = true;
	assert(f.runtime.Stop().ok());
	assert(f.runtime.status().error.ok());
	assert(f.runtime.status().state == State::idle);
	assert(f.runtime.status().menu_display.available);
	assert(f.runtime.status().menu_display.error.code == ErrorCode::io_failed);
	assert(f.runtime.status().menu_display.error.phase == "menu_memory");
	assert(f.runtime.status().menu_display.error.message == "stale menu map");
}

void StartSessionDisplayFixture(Fixture& f,const std::string& id)
{
 f.hardware.core_info.descriptor.abi={"fes.simple-computer",1,0};
 f.hardware.core_info.descriptor.interfaces={
  {"fes.keyboard",1,0,true},{"fes.video.fixed-720p60",1,0,true},
  {"fes.media.blob",1,0,true},{"fes.memory.hps-ddr",1,0,true},
  {"fes.video.session-display",1,0,true}};
 f.hardware.supported.abis={{"fes.simple-computer",1,0,{
  {"fes.keyboard",1,0},{"fes.video.fixed-720p60",1,0},
  {"fes.media.blob",1,0},{"fes.memory.hps-ddr",1,0},{"fes.video.session-display",1,0}}}};
 assert(f.runtime.Start().ok());assert(f.runtime.LoadCore("/zx81",id).ok());
}

void TestSessionDisplayBindingsFocusAndFrameRevocation()
{
 Fixture f;const std::string id(64,'a');StartSessionDisplayFixture(f,id);
 const auto active=f.runtime.status();const auto core_generation=active.generation;
 assert(active.menu_display.session&&!active.menu_display.available);
 assert(active.menu_display.package_id==id&&active.menu_display.core_generation==core_generation);
 assert(active.menu_display.generation==0);
 assert(f.runtime.SetSessionDisplay(id,0,true).code==ErrorCode::invalid_request);
 assert(f.runtime.SetSessionDisplay(id,core_generation+1,true).code==ErrorCode::invalid_request);
 assert(f.runtime.SetSessionDisplay(std::string(64,'b'),core_generation,true).code==ErrorCode::invalid_request);
 assert(f.hardware.session_display_calls==0);
 assert(f.runtime.SetComputerKeyboard(0).ok()&&f.hardware.keyboard_calls==1);
 assert(f.runtime.SetSessionDisplay(id,core_generation,true).ok());
 const auto opened=f.runtime.status().menu_display;
 assert(opened.available&&opened.generation&&opened.core_generation==core_generation);
 assert(f.runtime.SetSessionDisplay(id,core_generation,true).ok());
 assert(f.runtime.status().menu_display.generation==opened.generation&&f.hardware.session_display_calls==1);
 assert(f.runtime.SetComputerKeyboard(0).ok()&&f.hardware.keyboard_calls==1);
 assert(f.runtime.SetComputerKeyboard((std::uint64_t(1)<<40)-1).ok()&&f.hardware.keyboard_calls==2);
 assert(f.runtime.SetComputerKeyboard(std::uint64_t(1)<<40).code==ErrorCode::invalid_request);
 std::unique_ptr<mister::MenuFrame> old;SealMenuFrame(f.runtime,opened.generation,&old);
 assert(f.runtime.SetSessionDisplay(id,core_generation,false).ok());
 assert(!f.runtime.status().menu_display.available&&f.runtime.status().menu_display.generation==0);
 assert(f.runtime.SetComputerKeyboard(0).ok()&&f.hardware.keyboard_calls==3);
 assert(f.runtime.SetSessionDisplay(id,core_generation,true).ok());
 const auto fresh=f.runtime.status().menu_display.generation;assert(fresh>opened.generation);
 std::unique_ptr<mister::MenuFrame> frame;SealMenuFrame(f.runtime,fresh,&frame);
 mister::MenuDisplayInfo info;
 assert(!f.runtime.PresentMenuFrame(opened.generation,*old,&info).ok());
 assert(!f.runtime.PresentMenuFrame(fresh,*old,&info).ok());
 assert(f.hardware.menu_present_calls==0);
 f.hardware.on_menu_present=[&] {
  assert(f.runtime.ReplaceLiveComputerMedia("/next.p",id,core_generation).code==ErrorCode::busy);
  assert(f.runtime.SetComputerKeyboard(0).code==ErrorCode::busy);
 };
 assert(f.runtime.PresentMenuFrame(fresh,*frame,&info).ok());
 assert(f.runtime.ReplaceLiveComputerMedia("/next.p",id,core_generation).ok());
 assert(f.runtime.ClearComputerMedia(id,core_generation).ok());
 assert(f.runtime.status().generation==core_generation&&f.hardware.core_calls==1&&f.hardware.idle_calls==1);
 frame.reset();SealMenuFrame(f.runtime,fresh,&frame);
 assert(f.runtime.LoadCore("/replacement",id).ok());
 assert(!f.runtime.PresentMenuFrame(fresh,*frame,&info).ok());
 const auto replacement=f.runtime.status().generation;assert(replacement>core_generation);
 assert(!f.runtime.SetSessionDisplay(id,core_generation,true).ok());
 assert(f.runtime.SetSessionDisplay(id,replacement,true).ok());
 std::unique_ptr<mister::MenuFrame> next;SealMenuFrame(f.runtime,f.runtime.status().menu_display.generation,&next);
 assert(f.runtime.Stop().ok());
 assert(!f.runtime.PresentMenuFrame(fresh,*next,&info).ok());
}

void TestSessionDisplayFailuresNeverReloadIdleOrRetireCore()
{
 Fixture f;const std::string id(64,'a');StartSessionDisplayFixture(f,id);
 const auto active=f.runtime.status();const auto generation=active.generation;
 assert(f.runtime.SetSessionDisplay(id,generation,true).ok());
 const auto display=f.runtime.status().menu_display.generation;
 std::unique_ptr<mister::MenuFrame> frame;SealMenuFrame(f.runtime,display,&frame);
 f.hardware.idle_result={{ErrorCode::io_failed,"idle must never be called"},true,""};
 f.hardware.menu_present_result={ErrorCode::io_failed,"session frame timed out","menu"};
 mister::MenuDisplayInfo info;assert(!f.runtime.PresentMenuFrame(display,*frame,&info).ok());
 const auto failed=f.runtime.status();
 assert(failed.state==active.state&&failed.generation==generation&&failed.active_package.package_id==id);
 assert(failed.menu_display.session&&!failed.menu_display.available&&failed.menu_display.generation==0);
 assert(failed.menu_display.error.message=="session frame timed out");
 assert(f.hardware.idle_calls==1&&f.hardware.core_calls==1&&f.hardware.flush_calls==0);
 assert(f.runtime.ClearComputerMedia(id,generation).ok());
 f.hardware.session_display_result={ErrorCode::io_failed,"drain not proved","menu"};
 assert(!f.runtime.SetSessionDisplay(id,generation,false).ok());
 assert(f.runtime.SetComputerKeyboard(0).ok()&&f.hardware.keyboard_calls==0);
 assert(f.runtime.status().generation==generation&&f.hardware.idle_calls==1);
 f.hardware.session_display_result={};assert(f.runtime.SetSessionDisplay(id,generation,false).ok());
 assert(f.runtime.SetComputerKeyboard(0).ok()&&f.hardware.keyboard_calls==1);
 f.hardware.session_display_result={ErrorCode::io_failed,"enable failed","menu"};
 assert(!f.runtime.SetSessionDisplay(id,generation,true).ok());
 assert(f.runtime.status().generation==generation&&f.hardware.idle_calls==1);
}

void TestSessionCloseAndStopWaitForInFlightFrame()
{
 for(bool stop:{false,true}) {
  Fixture f;const std::string id(64,'a');StartSessionDisplayFixture(f,id);
  const auto generation=f.runtime.status().generation;
  assert(f.runtime.SetSessionDisplay(id,generation,true).ok());
  const auto display=f.runtime.status().menu_display.generation;
  std::unique_ptr<mister::MenuFrame> frame;SealMenuFrame(f.runtime,display,&frame);
  std::mutex mutex;std::condition_variable ready;bool entered=false,release=false;
  f.hardware.on_menu_present=[&] {
   std::unique_lock<std::mutex> lock(mutex);entered=true;ready.notify_all();
   ready.wait(lock,[&]{return release;});
  };
  mister::MenuDisplayInfo info;
  std::thread present([&]{assert(f.runtime.PresentMenuFrame(display,*frame,&info).ok());});
  {std::unique_lock<std::mutex> lock(mutex);ready.wait(lock,[&]{return entered;});}
  mister::Error result;
  std::thread mutation([&]{result=stop?f.runtime.Stop():f.runtime.SetSessionDisplay(id,generation,false);});
  std::this_thread::sleep_for(std::chrono::milliseconds(25));
  assert(f.hardware.session_display_calls==1&&f.hardware.idle_calls==1);
  {std::lock_guard<std::mutex> lock(mutex);release=true;ready.notify_all();}
  present.join();mutation.join();assert(result.ok());
  assert(!f.runtime.status().menu_display.available);
  assert(f.runtime.status().state==(stop?State::idle:State::running_development));
 }
}

int main()
{
 TestSessionDisplayBindingsFocusAndFrameRevocation();
 TestSessionDisplayFailuresNeverReloadIdleOrRetireCore();
 TestSessionCloseAndStopWaitForInFlightFrame();
 TestMenuFrameYieldsToLifecycleOps();
 TestMenuLifecycleWaitIsBounded();
 TestStaleMenuErrorIsNotPromotedOntoIdleStatus();
 TestMenuFrameYieldsToCoreDataAndLaunch();
 TestMenuFrameWaitIsBounded();
 TestMenuGenerationAndPreparationFencing();
 TestMenuPresentationFailureUsesIdleRecovery();
 TestMenuIdleStopAndPreMutationFailurePreserveFences();
	TestComputerKeyboardBindingSerializationAndFaults();
	TestComputerMediaUnitsStayLiveAndReportUnitState();
	TestCompositionUsesExistingLifecycle();
	TestControllerSnapshotBindingAndFaultCleanup();
	TestStreamBindingCapacityOwnershipAndStop();
	TestLiveMediaGenerationBindingAndBusy();
	TestInputFaultDuringFailedSaveCannotDiscardSnapshot();
	TestInputFaultDuringRestoreIsOwnedByRestoredGeneration();
	TestSaveFailurePreservesSessionForStopRetry();
	TestSaveFailureInputRestoreFailureRequiresRecovery();
	TestStartLoadsIdleOnceAndPublishesIdle();
	TestNonMenuIdlePublishesIdleWithoutReboot();
	TestProbeLessIdlePublishesIdleWithoutReboot();
	TestFailedStartRequiresReboot();
	TestUnsupportedPackageLeavesRunningSessionExactlyUntouched();
	TestPackageSaveFailurePreventsProgrammingAndRetainsActiveGeneration();
	TestPackageGenerationsRejectOldFaultsAndAcceptTheActiveFault();
	TestDevelopmentPathRejectsEmbeddedNulBeforeHardwareMutation();
	TestBlockedLaunchPublishesStarting();
	TestConcurrentMutationReturnsBusyWithoutQueueing();
	TestRejectedMutationLogCanReadStatusWithoutDeadlock();
	TestStatusRemainsReadableDuringMutation();
	TestSuccessfulGameRecordsIdentity();
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
	TestRecoverIdleRetriesProgramFromRebootRequired();
	TestRecoverIdleFailureStaysRebootRequired();
	TestRebootRequiredRejectsBothLaunchKinds();
	TestFailedStartAndStopNeverPerformSecondCleanup();
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
	puts("runtime_test: 55 passed");
	return 0;
}
