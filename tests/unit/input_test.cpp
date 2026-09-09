// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "fake_input.hpp"
#include "native/input.hpp"
#include "native/generated/fes_gp.hpp"
#include "native/linux/input.hpp"
#include "native/linux/spi.hpp"

#include <assert.h>
#include <cerrno>
#include <fcntl.h>
#include <stdio.h>

#if defined(__linux__)
#include <linux/input.h>
#endif

#include <chrono>
#include <condition_variable>
#include <cstdint>
#include <deque>
#include <functional>
#include <mutex>
#include <string>
#include <tuple>
#include <thread>
#include <vector>

namespace {

class FixedClock final : public mister::native::Clock {
public:
	explicit FixedClock(std::uint64_t now) : now_(now) {}
	std::uint64_t NowMs() const override { return now_; }
	std::uint64_t now_;
};

class RecordingSpi final : public mister::native::Spi {
public:
	struct Call {
		std::uint8_t target;
		std::vector<std::uint16_t> request;
		std::uint64_t deadline;
	};

	mister::Error SynchronizeCore(std::uint64_t) override { return {}; }
	mister::Error Exchange(std::uint8_t target,
		const std::vector<std::uint16_t>& request,
		std::vector<std::uint16_t>*, std::uint64_t deadline) override
	{
		std::lock_guard<std::mutex> lock(mutex_);
		calls_.push_back({target, request, deadline});
		mister::Error result;
		if (!errors_.empty()) {
			result = errors_.front();
			errors_.pop_front();
		}
		condition_.notify_all();
		return result;
	}

	void PushError(mister::Error error)
	{
		std::lock_guard<std::mutex> lock(mutex_);
		errors_.push_back(error);
	}
	bool WaitForCalls(std::size_t count)
	{
		std::unique_lock<std::mutex> lock(mutex_);
		return condition_.wait_for(lock, std::chrono::seconds(2),
			[this, count] { return calls_.size() >= count; });
	}
	std::vector<Call> Calls() const
	{
		std::lock_guard<std::mutex> lock(mutex_);
		return calls_;
	}

private:
	mutable std::mutex mutex_;
	std::condition_variable condition_;
	std::deque<mister::Error> errors_;
	std::vector<Call> calls_;
};

class Faults {
public:
	void Report(std::uint64_t generation, mister::Error error)
	{
		std::lock_guard<std::mutex> lock(mutex_);
		generations_.push_back(generation);
		errors_.push_back(std::move(error));
		condition_.notify_all();
	}
	bool WaitFor(std::size_t count)
	{
		std::unique_lock<std::mutex> lock(mutex_);
		return condition_.wait_for(lock, std::chrono::seconds(2),
			[this, count] { return errors_.size() >= count; });
	}
	std::vector<std::uint64_t> Generations() const
	{
		std::lock_guard<std::mutex> lock(mutex_);
		return generations_;
	}
	std::vector<mister::Error> Errors() const
	{
		std::lock_guard<std::mutex> lock(mutex_);
		return errors_;
	}

private:
	mutable std::mutex mutex_;
	std::condition_variable condition_;
	std::vector<std::uint64_t> generations_;
	std::vector<mister::Error> errors_;
};

mister::InputRecipe Recipe()
{
	return {1, 0x02, 0x0008, 0x0004, 0x0002, 0x0001,
		0x0010, 0x0020, 0x0040, 0x0080};
}

mister::native::InputDeviceIdentity Identity()
{
	return {"FogCast Virtual Gamepad", 0x0006, 0x0000, 0x0001, 0x0001};
}

void TestProductionIdentityAndEveryFieldSelectExactlyOneDevice()
{
#if defined(__linux__)
	FixedClock clock(10);
	mister_test::FakeLinuxInputOperations operations;
	const mister::native::InputDeviceIdentity exact =
		mister::native::FogCastGamepadIdentity();
	assert(exact.name == "FogCast Virtual Gamepad");
	assert(exact.bus == BUS_VIRTUAL);
	assert(exact.vendor == 0x0000);
	assert(exact.product == 0x0001);
	assert(exact.version == 0x0001);
	operations.Add({"/dev/input/event5", 15,
		{"Wrong Gamepad", 0x0006, 0x0000, 0x0001, 0x0001}});
	operations.Add({"/dev/input/event1", 11,
		{"FogCast Virtual Gamepad", 0x0005, 0x0000, 0x0001, 0x0001}});
	operations.Add({"/dev/input/event2", 12,
		{"FogCast Virtual Gamepad", 0x0006, 0x0002, 0x0001, 0x0001}});
	operations.Add({"/dev/input/event3", 13,
		{"FogCast Virtual Gamepad", 0x0006, 0x0000, 0x0002, 0x0001}});
	operations.Add({"/dev/input/event4", 14,
		{"FogCast Virtual Gamepad", 0x0006, 0x0000, 0x0001, 0x0002}});
	operations.Add({"/dev/input/event9", 19, exact});
	mister::native::LinuxInput input(clock, operations);
	assert(input.Open(exact, 100).ok());
	assert((operations.opened_paths == std::vector<std::string>{
		"/dev/input/event1", "/dev/input/event2", "/dev/input/event3",
		"/dev/input/event4", "/dev/input/event5", "/dev/input/event9"}));
	for (int flags : operations.open_flags)
		assert(flags == (O_RDONLY | O_CLOEXEC | O_NONBLOCK));
	assert((operations.closed_descriptors ==
		std::vector<int>{11, 12, 13, 14, 15}));
	assert(input.Close().ok());
	assert((operations.closed_descriptors ==
		std::vector<int>{11, 12, 13, 14, 15, 19, 900}));
#endif
}

void TestAbsentDuplicateAndDeadlineDiscoveryRejectWithoutLeakingDescriptors()
{
#if defined(__linux__)
	{
		FixedClock clock(10);
		mister_test::FakeLinuxInputOperations operations;
		operations.Add({"/dev/input/event0", 10,
			{"Other", 0x0006, 0x0000, 0x0001, 0x0001}});
		mister::native::LinuxInput input(clock, operations);
		const mister::Error error = input.Open(Identity(), 100);
		assert(error.code == mister::ErrorCode::io_failed);
		assert(error.message == "FogCast input device not found");
		assert((operations.closed_descriptors == std::vector<int>{10}));
	}
	{
		FixedClock clock(10);
		mister_test::FakeLinuxInputOperations operations;
		operations.Add({"/dev/input/event7", 17, Identity()});
		operations.Add({"/dev/input/event2", 12, Identity()});
		mister::native::LinuxInput input(clock, operations);
		const mister::Error error = input.Open(Identity(), 100);
		assert(error.code == mister::ErrorCode::io_failed);
		assert(error.message == "multiple FogCast input devices found");
		assert((operations.closed_descriptors == std::vector<int>{12, 17}));
		assert(operations.create_cancellation_count == 0);
	}
	{
		FixedClock clock(100);
		mister_test::FakeLinuxInputOperations operations;
		operations.Add({"/dev/input/event0", 10, Identity()});
		mister::native::LinuxInput input(clock, operations);
		const mister::Error error = input.Open(Identity(), 100);
		assert(error.code == mister::ErrorCode::io_failed);
		assert(error.message == "deadline exceeded");
		assert(operations.opened_paths.empty());
	}
#endif
}

void TestLinuxAdapterRequiresWholeRecordsMapsSupportedEventsAndCancelsPoll()
{
#if defined(__linux__)
	FixedClock clock(1);
	mister_test::FakeLinuxInputOperations operations;
	operations.Add({"/dev/input/event0", 10, Identity()});
	mister::native::LinuxInput input(clock, operations);
	assert(input.Open(Identity(), 100).ok());
	mister::native::InputEvent event;
	bool cancelled = true;
	const std::vector<std::pair<std::uint16_t, mister::native::InputControl>> keys = {
		{BTN_DPAD_UP, mister::native::InputControl::up},
		{BTN_DPAD_DOWN, mister::native::InputControl::down},
		{BTN_DPAD_LEFT, mister::native::InputControl::left},
		{BTN_DPAD_RIGHT, mister::native::InputControl::right},
		{BTN_A, mister::native::InputControl::a},
		{BTN_B, mister::native::InputControl::b},
		{BTN_C, mister::native::InputControl::c},
		{BTN_START, mister::native::InputControl::start},
		{BTN_X, mister::native::InputControl::x},
		{BTN_Y, mister::native::InputControl::y},
		{BTN_TL, mister::native::InputControl::l},
		{BTN_TR, mister::native::InputControl::r},
		{BTN_SELECT, mister::native::InputControl::select},

	};
	for (const auto& key : keys) {
		operations.QueueRecord(EV_KEY, key.first, 1);
		assert(input.Read(&event, &cancelled).ok());
		assert(!cancelled);
		assert(event.control == key.second);
		assert(event.value == 1);
	}
	const std::vector<std::tuple<std::uint16_t, std::int32_t,
		mister::native::InputControl>> axes = {
		{ABS_X, -16000, mister::native::InputControl::horizontal},
		{ABS_Y, 16000, mister::native::InputControl::vertical},
	};
	for (const auto& axis : axes) {
		operations.QueueRecord(EV_ABS, std::get<0>(axis), std::get<1>(axis));
		assert(input.Read(&event, &cancelled).ok());
		assert(event.control == std::get<2>(axis));
		assert(event.value == std::get<1>(axis));
	}
	operations.QueueRecord(EV_SYN, SYN_REPORT, 0);
	assert(input.Read(&event, &cancelled).ok());
	assert(event.control == mister::native::InputControl::synchronize);
	operations.QueueShortRecord();
	assert(input.Read(&event, &cancelled).message ==
		"incomplete input event record");
	assert(input.Cancel().ok());
	assert(input.Read(&event, &cancelled).ok());
	assert(cancelled);
	assert((operations.signalled_descriptors == std::vector<int>{900}));
	assert(input.Close().ok());
#endif
}

void TestLinuxAdapterRejectsUnsupportedMalformedEofAndReadFailure()
{
#if defined(__linux__)
	FixedClock clock(1);
	mister_test::FakeLinuxInputOperations operations;
	operations.Add({"/dev/input/event0", 10, Identity()});
	mister::native::LinuxInput input(clock, operations);
	assert(input.Open(Identity(), 100).ok());
	mister::native::InputEvent event;
	bool cancelled = false;
	operations.QueueRecord(EV_KEY, BTN_THUMBL, 1);
	assert(input.Read(&event, &cancelled).message == "unsupported input event");
	operations.QueueRecord(EV_KEY, BTN_A, 3);
	assert(input.Read(&event, &cancelled).message == "invalid key value");
	operations.QueueRecord(EV_ABS, ABS_X, 40000);
	assert(input.Read(&event, &cancelled).message == "invalid axis value");
	operations.QueueRecord(EV_SYN, SYN_DROPPED, 0);
	assert(input.Read(&event, &cancelled).message == "unsupported input event");
	operations.QueueEof();
	assert(input.Read(&event, &cancelled).message == "input device removed");
	operations.QueueReadError();
	assert(input.Read(&event, &cancelled).message == "input event read failed");
	assert(input.Close().ok());
#endif
}

void TestBatchesDigitalControlsAtSynAndSuppressesDuplicateMaps()
{
	FixedClock clock(40);
	mister_test::FakeInputDevice device;
	RecordingSpi spi;
	mister::native::NativeInputSession session(device, spi, clock, 25);
	assert(session.Open(Identity(), Recipe(), 100).ok());
	assert(session.Neutralize(101).ok());
	Faults faults;
	assert(session.Start(7, [&](std::uint64_t generation, mister::Error error) {
		faults.Report(generation, std::move(error));
	}).ok());
	for (auto extra : {mister::native::InputControl::x, mister::native::InputControl::y,
		mister::native::InputControl::l, mister::native::InputControl::r,
		mister::native::InputControl::select}) {
		device.Push({extra, 1});
	}
	device.Push({mister::native::InputControl::synchronize, 0});
	assert(device.WaitForReads(6));
	assert(spi.Calls().size() == 1); // Zero masks do not alias MD's C or Start.
	device.Push({mister::native::InputControl::up, 1});
	device.Push({mister::native::InputControl::a, 1});
	device.Push({mister::native::InputControl::c, 1});
	device.Push({mister::native::InputControl::synchronize, 0});
	assert(spi.WaitForCalls(2));
	device.Push({mister::native::InputControl::up, 1});
	device.Push({mister::native::InputControl::synchronize, 0});
	assert(device.WaitForReads(12));
	assert(spi.Calls().size() == 2);
	device.Push({mister::native::InputControl::up, 0});
	device.Push({mister::native::InputControl::down, 1});
	device.Push({mister::native::InputControl::left, 1});
	device.Push({mister::native::InputControl::right, 1});
	device.Push({mister::native::InputControl::b, 1});
	device.Push({mister::native::InputControl::start, 1});
	device.Push({mister::native::InputControl::synchronize, 0});
	assert(spi.WaitForCalls(3));
	const std::vector<RecordingSpi::Call> calls = spi.Calls();
	assert(calls[0].target == mister::native::kUserIoTarget);
	assert((calls[0].request == std::vector<std::uint16_t>{0x02, 0x0000}));
	assert(calls[0].deadline == 101);
	assert((calls[1].request == std::vector<std::uint16_t>{0x02, 0x0058}));
	assert(calls[1].deadline == 65);
	assert((calls[2].request == std::vector<std::uint16_t>{0x02, 0x00f4}));
	assert(session.Stop(102).ok());
	assert((spi.Calls().back().request ==
		std::vector<std::uint16_t>{0x02, 0x0000}));
	assert(spi.Calls().back().deadline == 102);
}

void TestAxisThresholdsCommitOnlyAtSynReport()
{
	FixedClock clock(10);
	mister_test::FakeInputDevice device;
	RecordingSpi spi;
	mister::native::NativeInputSession session(device, spi, clock, 20);
	assert(session.Open(Identity(), Recipe(), 100).ok());
	assert(session.Neutralize(100).ok());
	Faults faults;
	assert(session.Start(1, [&](std::uint64_t generation, mister::Error error) {
		faults.Report(generation, std::move(error));
	}).ok());
	device.Push({mister::native::InputControl::horizontal, -16000});
	device.Push({mister::native::InputControl::vertical, 16000});
	assert(device.WaitForReads(2));
	assert(spi.Calls().size() == 1);
	device.Push({mister::native::InputControl::synchronize, 0});
	assert(spi.WaitForCalls(2));
	assert((spi.Calls()[1].request ==
		std::vector<std::uint16_t>{0x02, 0x0006}));
	device.Push({mister::native::InputControl::horizontal, -15999});
	device.Push({mister::native::InputControl::vertical, 15999});
	device.Push({mister::native::InputControl::synchronize, 0});
	assert(spi.WaitForCalls(3));
	assert((spi.Calls()[2].request ==
		std::vector<std::uint16_t>{0x02, 0x0000}));
	assert(session.Stop(100).ok());
}

void TestStopCancelsJoinsNeutralizesAndNewGenerationStartsClean()
{
	FixedClock clock(10);
	mister_test::FakeInputDevice device;
	RecordingSpi spi;
	mister::native::NativeInputSession session(device, spi, clock, 20);
	assert(session.Open(Identity(), Recipe(), 100).ok());
	assert(session.Neutralize(100).ok());
	Faults faults;
	auto callback = [&](std::uint64_t generation, mister::Error error) {
		faults.Report(generation, std::move(error));
	};
	assert(session.Start(9, callback).ok());
	assert(device.WaitUntilReading());
	assert(session.Stop(200).ok());
	assert(device.WaitUntilCancelled());
	assert(device.close_count == 1);
	const std::size_t stopped_calls = spi.Calls().size();
	device.Push({mister::native::InputControl::a, 1});
	device.Push({mister::native::InputControl::synchronize, 0});
	assert(spi.Calls().size() == stopped_calls);
	assert(session.Open(Identity(), Recipe(), 300).ok());
	assert(session.Neutralize(300).ok());
	assert(session.Start(8, callback).code == mister::ErrorCode::invalid_request);
	assert(session.Start(9, callback).ok());
	device.Push({mister::native::InputControl::b, 1});
	device.Push({mister::native::InputControl::synchronize, 0});
	assert(spi.WaitForCalls(stopped_calls + 2));
	assert((spi.Calls().back().request ==
		std::vector<std::uint16_t>{0x02, 0x0020}));
	assert(session.Stop(400).ok());
	assert(session.Open(Identity(), Recipe(), 500).ok());
	assert(session.Neutralize(500).ok());
	assert(session.Start(10, callback).ok());
	assert(session.Stop(600).ok());
	assert(faults.Errors().empty());
}

void TestStartRequiresSuccessfulNeutralization()
{
	FixedClock clock(10);
	mister_test::FakeInputDevice device;
	RecordingSpi spi;
	mister::native::NativeInputSession session(device, spi, clock, 20);
	assert(session.Open(Identity(), Recipe(), 100).ok());
	Faults faults;
	auto callback = [&](std::uint64_t generation, mister::Error error) {
		faults.Report(generation, std::move(error));
	};
	const mister::Error before_neutral = session.Start(1, callback);
	assert(before_neutral.code == mister::ErrorCode::invalid_request);
	assert(before_neutral.message == "input session is not neutralized");
	assert(!device.IsReading());
	assert(spi.Calls().empty());
	assert(session.Neutralize(101).ok());
	assert(session.Start(1, callback).ok());
	assert(device.WaitUntilReading());
	assert(session.Stop(102).ok());
	assert((spi.Calls()[0].request ==
		std::vector<std::uint16_t>{0x02, 0x0000}));
	assert((spi.Calls()[1].request ==
		std::vector<std::uint16_t>{0x02, 0x0000}));
}

void TestCancellationErrorStillJoinsNeutralizesAndCloses()
{
	FixedClock clock(10);
	mister_test::FakeInputDevice device;
	device.cancel_error = {mister::ErrorCode::io_failed,
		"scripted cancellation failure"};
	RecordingSpi spi;
	mister::native::NativeInputSession session(device, spi, clock, 20);
	assert(session.Open(Identity(), Recipe(), 100).ok());
	assert(session.Neutralize(101).ok());
	Faults faults;
	assert(session.Start(1, [&](std::uint64_t generation, mister::Error error) {
		faults.Report(generation, std::move(error));
	}).ok());
	assert(device.WaitUntilReading());
	std::mutex stop_mutex;
	std::condition_variable stop_condition;
	bool stop_done = false;
	mister::Error stopped;
	std::thread stopper([&] {
		const mister::Error result = session.Stop(102);
		std::lock_guard<std::mutex> lock(stop_mutex);
		stopped = result;
		stop_done = true;
		stop_condition.notify_all();
	});
	{
		std::unique_lock<std::mutex> lock(stop_mutex);
		assert(stop_condition.wait_for(lock, std::chrono::seconds(2),
			[&] { return stop_done; }));
	}
	stopper.join();
	assert(stopped.code == mister::ErrorCode::io_failed);
	assert(stopped.message == "scripted cancellation failure");
	assert(device.WaitUntilCancelled());
	assert(device.close_count == 1);
	assert(faults.Errors().empty());
	const std::vector<RecordingSpi::Call> calls = spi.Calls();
	assert(calls.size() == 2);
	assert((calls[0].request == std::vector<std::uint16_t>{0x02, 0x0000}));
	assert((calls[1].request == std::vector<std::uint16_t>{0x02, 0x0000}));
	assert(calls[1].deadline == 102);
}

void TestLinuxCancellationSignalFailureStillWakesPendingAndFutureReads()
{
#if defined(__linux__)
	FixedClock clock(1);
	mister_test::FakeLinuxInputOperations operations;
	operations.Add({"/dev/input/event0", 10, Identity()});
	operations.block_wait = true;
	operations.QueueCancellationResult(-1, EBADF);
	mister::native::LinuxInput input(clock, operations);
	assert(input.Open(Identity(), 100).ok());
	mister::Error read_error;
	bool read_cancelled = false;
	std::thread reader([&] {
		mister::native::InputEvent event;
		read_error = input.Read(&event, &read_cancelled);
	});
	assert(operations.WaitUntilWaiting());
	const mister::Error cancel_error = input.Cancel();
	assert(cancel_error.code == mister::ErrorCode::io_failed);
	assert(cancel_error.message == "input cancellation failed");
	reader.join();
	assert(read_error.ok());
	assert(read_cancelled);
	mister::native::InputEvent event;
	bool future_cancelled = false;
	assert(input.Read(&event, &future_cancelled).ok());
	assert(future_cancelled);
	assert(input.Close().ok());
#endif
}

void TestLinuxCancellationRetriesEintrAndAcceptsReadableEventfd()
{
#if defined(__linux__)
	{
		FixedClock clock(1);
		mister_test::FakeLinuxInputOperations operations;
		operations.Add({"/dev/input/event0", 10, Identity()});
		operations.QueueCancellationResult(-1, EINTR);
		operations.QueueCancellationResult(0, 0);
		mister::native::LinuxInput input(clock, operations);
		assert(input.Open(Identity(), 100).ok());
		assert(input.Cancel().ok());
		assert((operations.signalled_descriptors == std::vector<int>{900, 900}));
		assert(input.Close().ok());
	}
	{
		FixedClock clock(1);
		mister_test::FakeLinuxInputOperations operations;
		operations.Add({"/dev/input/event0", 10, Identity()});
		operations.QueueCancellationResult(-1, EAGAIN);
		mister::native::LinuxInput input(clock, operations);
		assert(input.Open(Identity(), 100).ok());
		assert(input.Cancel().ok());
		assert((operations.signalled_descriptors == std::vector<int>{900}));
		assert(input.Close().ok());
	}
#endif
}

void TestReadUnsupportedAndSpiFaultsReportOnceWithTheirGeneration()
{
	const std::vector<mister::Error> source_errors = {
		{mister::ErrorCode::io_failed, "input device removed"},
		{mister::ErrorCode::io_failed, "input event read failed"},
		{mister::ErrorCode::io_failed, "incomplete input event record"},
	};
	std::uint64_t generation = 20;
	for (const mister::Error& source_error : source_errors) {
		FixedClock clock(10);
		mister_test::FakeInputDevice device;
		RecordingSpi spi;
		mister::native::NativeInputSession session(device, spi, clock, 20);
		assert(session.Open(Identity(), Recipe(), 100).ok());
		assert(session.Neutralize(100).ok());
		Faults faults;
		assert(session.Start(generation, [&](std::uint64_t observed,
			mister::Error error) { faults.Report(observed, std::move(error)); }).ok());
		device.PushError(source_error);
		device.PushError({mister::ErrorCode::io_failed, "second failure"});
		assert(faults.WaitFor(1));
		assert(session.Stop(200).ok());
		assert((faults.Generations() == std::vector<std::uint64_t>{generation}));
		assert(faults.Errors()[0].message == source_error.message);
		++generation;
	}
	{
		FixedClock clock(10);
		mister_test::FakeInputDevice device;
		RecordingSpi spi;
		mister::native::NativeInputSession session(device, spi, clock, 20);
		assert(session.Open(Identity(), Recipe(), 100).ok());
		assert(session.Neutralize(100).ok());
		Faults faults;
		assert(session.Start(30, [&](std::uint64_t observed, mister::Error error) {
			faults.Report(observed, std::move(error));
		}).ok());
		device.Push({static_cast<mister::native::InputControl>(99), 1});
		assert(faults.WaitFor(1));
		assert(faults.Errors()[0].message == "unsupported input event");
		assert(session.Stop(200).ok());
	}
	{
		FixedClock clock(10);
		mister_test::FakeInputDevice device;
		RecordingSpi spi;
		mister::native::NativeInputSession session(device, spi, clock, 20);
		assert(session.Open(Identity(), Recipe(), 100).ok());
		assert(session.Neutralize(100).ok());
		spi.PushError({mister::ErrorCode::io_failed, "scripted SPI failure"});
		Faults faults;
		assert(session.Start(31, [&](std::uint64_t observed, mister::Error error) {
			faults.Report(observed, std::move(error));
		}).ok());
		device.Push({mister::native::InputControl::c, 1});
		device.Push({mister::native::InputControl::synchronize, 0});
		assert(faults.WaitFor(1));
		assert((faults.Generations() == std::vector<std::uint64_t>{31}));
		assert(faults.Errors()[0].message == "scripted SPI failure");
		assert(session.Stop(200).ok());
	}
}

void TestInvalidRecipeStateAndDirectBoundaryFailuresAreContained()
{
	FixedClock clock(10);
	mister_test::FakeInputDevice device;
	RecordingSpi spi;
	mister::native::NativeInputSession session(device, spi, clock, 20);
	mister::InputRecipe invalid = Recipe();
	invalid.player_count = 2;
	assert(session.Open(Identity(), invalid, 100).code ==
		mister::ErrorCode::invalid_request);
	assert(device.opened_identities.empty());
	assert(session.Start(1, {}).code == mister::ErrorCode::invalid_request);
	device.open_error = {mister::ErrorCode::io_failed, "discovery failed"};
	assert(session.Open(Identity(), Recipe(), 100).message == "discovery failed");
	device.open_error = {};
	assert(session.Open(Identity(), Recipe(), 100).ok());
	spi.PushError({mister::ErrorCode::io_failed, "neutral failed"});
	assert(session.Neutralize(123).message == "neutral failed");
	assert(spi.Calls().back().deadline == 123);
	assert(session.Stop(124).ok());
	assert(spi.Calls().size() == 1);
}

void TestSnesButtonsDeliverIndependentMasksAndStopNeutral()
{
	FixedClock clock(10);
	mister_test::FakeInputDevice device;
	RecordingSpi spi;
	mister::native::NativeInputSession session(device, spi, clock, 25);
	auto recipe = Recipe();
	recipe.c = 0;
	recipe.start = 0x800;
	recipe.x = 0x40; recipe.y = 0x80; recipe.l = 0x100;
	recipe.r = 0x200; recipe.select = 0x400;
	assert(session.Open(Identity(), recipe, 100).ok());
	assert(session.Neutralize(101).ok());
	assert(session.Start(1, [](std::uint64_t, mister::Error) { assert(false); }).ok());
	using C = mister::native::InputControl;
	const std::vector<std::pair<C, std::uint16_t>> controls = {
		{C::right, 1}, {C::left, 2}, {C::down, 4}, {C::up, 8},
		{C::a, 0x10}, {C::b, 0x20}, {C::x, 0x40}, {C::y, 0x80},
		{C::l, 0x100}, {C::r, 0x200}, {C::select, 0x400}, {C::start, 0x800}};
	std::size_t calls = 1;
	for (const auto& control : controls) {
		device.Push({control.first, 1}); device.Push({C::synchronize, 0});
		assert(spi.WaitForCalls(++calls));
		assert(spi.Calls().back().request == std::vector<std::uint16_t>({2, control.second}));
		device.Push({control.first, 0}); device.Push({C::synchronize, 0});
		assert(spi.WaitForCalls(++calls));
		assert(spi.Calls().back().request == std::vector<std::uint16_t>({2, 0}));
	}
	assert(session.Stop(102).ok());
	assert(spi.Calls().back().request == std::vector<std::uint16_t>({2, 0}));
}

void TestFesSinkNeutralizesOppositeDirectionsAndRetiresGeneration()
{
	FixedClock clock(10);
	mister_test::FakeInputDevice device;
	RecordingSpi spi;
	mister::native::NativeInputSession session(device, spi, clock, 25);
	mister::InputRecipe recipe = {1,
		static_cast<std::uint8_t>(mister::native::generated::FesGpOpcodeButtons),
		mister::native::generated::FesGpButtonUp,
		mister::native::generated::FesGpButtonDown,
		mister::native::generated::FesGpButtonLeft,
		mister::native::generated::FesGpButtonRight,
		mister::native::generated::FesGpButtonA,
		mister::native::generated::FesGpButtonB, 0,
		mister::native::generated::FesGpButtonStart};
	std::vector<std::uint16_t> maps;
	auto sink = [&](std::uint16_t map, std::uint64_t) {
		maps.push_back(map);
		return mister::Error{};
	};
	assert(session.Open(Identity(), recipe, 100, sink).ok());
	assert(session.Neutralize(101).ok());
	unsigned faults = 0;
	assert(session.Start(11, [&](std::uint64_t generation, mister::Error error) {
		assert(generation == 11);
		assert(error.code == mister::ErrorCode::io_failed);
		++faults;
	}).ok());
	device.Push({mister::native::InputControl::up, 1});
	device.Push({mister::native::InputControl::synchronize, 0});
	assert(device.WaitForReads(2));
	device.Push({mister::native::InputControl::down, 1});
	device.Push({mister::native::InputControl::synchronize, 0});
	assert(device.WaitForReads(4));
	device.PushError({mister::ErrorCode::io_failed, "gamepad disconnected"});
	assert(device.WaitForReads(5));
	assert(session.Stop(102).ok());
	assert(faults == 1);
	assert((maps == std::vector<std::uint16_t>{0,
		mister::native::generated::FesGpButtonUp, 0, 0}));
	const std::size_t retired = maps.size();
	device.Push({mister::native::InputControl::a, 1});
	device.Push({mister::native::InputControl::synchronize, 0});
	assert(maps.size() == retired);
	assert(spi.Calls().empty());
}

} // namespace

int main()
{
	TestFesSinkNeutralizesOppositeDirectionsAndRetiresGeneration();
	TestSnesButtonsDeliverIndependentMasksAndStopNeutral();
	TestProductionIdentityAndEveryFieldSelectExactlyOneDevice();
	TestAbsentDuplicateAndDeadlineDiscoveryRejectWithoutLeakingDescriptors();
	TestLinuxAdapterRequiresWholeRecordsMapsSupportedEventsAndCancelsPoll();
	TestLinuxAdapterRejectsUnsupportedMalformedEofAndReadFailure();
	TestBatchesDigitalControlsAtSynAndSuppressesDuplicateMaps();
	TestAxisThresholdsCommitOnlyAtSynReport();
	TestStopCancelsJoinsNeutralizesAndNewGenerationStartsClean();
	TestStartRequiresSuccessfulNeutralization();
	TestCancellationErrorStillJoinsNeutralizesAndCloses();
	TestLinuxCancellationSignalFailureStillWakesPendingAndFutureReads();
	TestLinuxCancellationRetriesEintrAndAcceptsReadableEventfd();
	TestReadUnsupportedAndSpiFaultsReportOnceWithTheirGeneration();
	TestInvalidRecipeStateAndDirectBoundaryFailuresAreContained();
	puts("input_test: 14 passed");
	return 0;
}
