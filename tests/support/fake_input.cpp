// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "fake_input.hpp"

#if defined(__linux__)
#include <linux/input.h>
#endif

#include <algorithm>
#include <cerrno>
#include <chrono>
#include <cstring>

namespace mister_test {

mister::Error FakeInputDevice::Open(
	const mister::native::InputDeviceIdentity& identity, std::uint64_t deadline)
{
	std::lock_guard<std::mutex> lock(mutex_);
	opened_identities.push_back(identity);
	open_deadlines.push_back(deadline);
	if (!open_error.ok()) return open_error;
	opened_ = true;
	cancelled_ = false;
	reading_ = false;
	read_count_ = 0;
	results_.clear();
	return {};
}

mister::Error FakeInputDevice::Read(mister::native::InputEvent* event,
	bool* cancelled)
{
	std::unique_lock<std::mutex> lock(mutex_);
	reading_ = true;
	condition_.notify_all();
	condition_.wait(lock, [this] { return cancelled_ || !results_.empty(); });
	reading_ = false;
	if (cancelled_) {
		*cancelled = true;
		return {};
	}
	const Result result = results_.front();
	results_.pop_front();
	++read_count_;
	condition_.notify_all();
	if (!result.error.ok()) return result.error;
	*event = result.event;
	*cancelled = false;
	return {};
}

mister::Error FakeInputDevice::Cancel()
{
	std::lock_guard<std::mutex> lock(mutex_);
	cancelled_ = true;
	condition_.notify_all();
	return cancel_error;
}

mister::Error FakeInputDevice::Close()
{
	std::lock_guard<std::mutex> lock(mutex_);
	++close_count;
	opened_ = false;
	cancelled_ = true;
	condition_.notify_all();
	return close_error;
}

void FakeInputDevice::Push(mister::native::InputEvent event)
{
	std::lock_guard<std::mutex> lock(mutex_);
	results_.push_back({event, {}});
	condition_.notify_all();
}

void FakeInputDevice::PushError(mister::Error error)
{
	std::lock_guard<std::mutex> lock(mutex_);
	results_.push_back({{}, error});
	condition_.notify_all();
}

bool FakeInputDevice::WaitUntilReading()
{
	std::unique_lock<std::mutex> lock(mutex_);
	return condition_.wait_for(lock, std::chrono::seconds(2),
		[this] { return reading_; });
}

bool FakeInputDevice::WaitUntilCancelled()
{
	std::unique_lock<std::mutex> lock(mutex_);
	return condition_.wait_for(lock, std::chrono::seconds(2),
		[this] { return cancelled_; });
}

bool FakeInputDevice::WaitForReads(std::size_t count)
{
	std::unique_lock<std::mutex> lock(mutex_);
	return condition_.wait_for(lock, std::chrono::seconds(2),
		[this, count] { return read_count_ >= count; });
}

bool FakeInputDevice::IsReading()
{
	std::lock_guard<std::mutex> lock(mutex_);
	return reading_;
}

#if defined(MISTER_RUNTIME_TESTING)
mister::Error FakeLinuxInputOperations::Enumerate(
	std::vector<std::string>* paths)
{
	if (!enumerate_error.ok()) return enumerate_error;
	paths->clear();
	for (const Candidate& candidate : candidates) paths->push_back(candidate.path);
	return {};
}

int FakeLinuxInputOperations::Open(const char* path, int flags)
{
	opened_paths.push_back(path);
	open_flags.push_back(flags);
	for (const Candidate& candidate : candidates) {
		if (candidate.path == path)
			return candidate.open_ok ? candidate.descriptor : -1;
	}
	return -1;
}

int FakeLinuxInputOperations::Close(int descriptor)
{
	closed_descriptors.push_back(descriptor);
	return 0;
}

int FakeLinuxInputOperations::QueryName(int descriptor, std::string* name)
{
	queried_names.push_back(descriptor);
	const Candidate* candidate = Find(descriptor);
	if (candidate == nullptr || !candidate->name_ok) return -1;
	*name = candidate->identity.name;
	return 0;
}

int FakeLinuxInputOperations::QueryIdentity(int descriptor,
	mister::native::InputDeviceIdentity* identity)
{
	queried_identities.push_back(descriptor);
	const Candidate* candidate = Find(descriptor);
	if (candidate == nullptr || !candidate->id_ok) return -1;
	*identity = candidate->identity;
	identity->name.clear();
	return 0;
}

int FakeLinuxInputOperations::CreateCancellation()
{
	++create_cancellation_count;
	return create_cancellation_ok ? cancellation_descriptor : -1;
}

int FakeLinuxInputOperations::Wait(int, int, bool* input_ready,
	bool* cancelled)
{
	if (!wait_ok) {
		errno = EIO;
		return -1;
	}
	std::unique_lock<std::mutex> lock(wait_mutex_);
	if (block_wait && !cancelled_) {
		waiting_ = true;
		wait_condition_.notify_all();
		wait_condition_.wait_for(lock, std::chrono::milliseconds(20),
			[this] { return cancelled_; });
		waiting_ = false;
	}
	*cancelled = cancelled_;
	*input_ready = !block_wait && !cancelled_;
	return 0;
}

long FakeLinuxInputOperations::Read(int, void* output, std::size_t size)
{
	if (reads_.empty()) return -1;
	ReadResult result = reads_.front();
	reads_.pop_front();
	if (result.result > 0) {
		const std::size_t count = std::min(size,
			static_cast<std::size_t>(result.result));
		std::memcpy(output, result.bytes.data(), count);
	}
	return result.result;
}

int FakeLinuxInputOperations::SignalCancellation(int descriptor)
{
	std::lock_guard<std::mutex> lock(wait_mutex_);
	signalled_descriptors.push_back(descriptor);
	if (!signal_ok) return -1;
	if (!cancellation_results_.empty()) {
		const std::pair<int, int> result = cancellation_results_.front();
		cancellation_results_.pop_front();
		errno = result.second;
		if (result.first == 0 || result.second == EAGAIN) {
			cancelled_ = true;
			wait_condition_.notify_all();
		}
		return result.first;
	}
	cancelled_ = true;
	wait_condition_.notify_all();
	return 0;
}

void FakeLinuxInputOperations::Add(Candidate candidate)
{
	candidates.push_back(std::move(candidate));
}

void FakeLinuxInputOperations::QueueRecord(std::uint16_t type,
	std::uint16_t code, std::int32_t value)
{
#if defined(__linux__)
	struct input_event event = {};
	event.type = type;
	event.code = code;
	event.value = value;
	ReadResult result;
	result.bytes.resize(sizeof(event));
	std::memcpy(result.bytes.data(), &event, sizeof(event));
	result.result = static_cast<long>(sizeof(event));
	reads_.push_back(std::move(result));
#else
	(void)type;
	(void)code;
	(void)value;
#endif
}

void FakeLinuxInputOperations::QueueShortRecord()
{
#if defined(__linux__)
	ReadResult result;
	result.bytes.resize(sizeof(struct input_event) - 1u);
	result.result = static_cast<long>(result.bytes.size());
	reads_.push_back(std::move(result));
#endif
}

void FakeLinuxInputOperations::QueueReadError()
{
	reads_.push_back({{}, -1});
}

void FakeLinuxInputOperations::QueueEof()
{
	reads_.push_back({{}, 0});
}

void FakeLinuxInputOperations::QueueCancellationResult(int result,
	int error_number)
{
	std::lock_guard<std::mutex> lock(wait_mutex_);
	cancellation_results_.push_back({result, error_number});
}

bool FakeLinuxInputOperations::WaitUntilWaiting()
{
	std::unique_lock<std::mutex> lock(wait_mutex_);
	return wait_condition_.wait_for(lock, std::chrono::seconds(2),
		[this] { return waiting_; });
}

const FakeLinuxInputOperations::Candidate* FakeLinuxInputOperations::Find(
	int descriptor) const
{
	for (const Candidate& candidate : candidates) {
		if (candidate.descriptor == descriptor) return &candidate;
	}
	return nullptr;
}
#endif

} // namespace mister_test
