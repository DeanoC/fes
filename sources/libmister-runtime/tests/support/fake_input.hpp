// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "native/input.hpp"
#include "native/linux/input.hpp"

#include <condition_variable>
#include <cstddef>
#include <cstdint>
#include <deque>
#include <map>
#include <mutex>
#include <string>
#include <utility>
#include <vector>

namespace mister_test {

class FakeInputDevice final : public mister::native::InputDevice {
public:
	mister::Error Open(const mister::native::InputDeviceIdentity&,
		std::uint64_t) override;
	mister::Error Read(mister::native::InputEvent*, bool*) override;
	mister::Error Cancel() override;
	mister::Error Close() override;

	void Push(mister::native::InputEvent);
	void PushError(mister::Error);
	bool WaitUntilReading();
	bool WaitUntilCancelled();
	bool WaitForReads(std::size_t);
	bool IsReading();

	mister::Error open_error;
	mister::Error cancel_error;
	mister::Error close_error;
	std::vector<mister::native::InputDeviceIdentity> opened_identities;
	std::vector<std::uint64_t> open_deadlines;
	unsigned close_count = 0;

private:
	struct Result {
		mister::native::InputEvent event;
		mister::Error error;
	};
	std::mutex mutex_;
	std::condition_variable condition_;
	std::deque<Result> results_;
	bool opened_ = false;
	bool cancelled_ = false;
	bool reading_ = false;
	std::size_t read_count_ = 0;
};

#if defined(MISTER_RUNTIME_TESTING)
class FakeLinuxInputOperations final
	: public mister::native::LinuxInputTestOperations {
public:
	struct Candidate {
		std::string path;
		int descriptor;
		mister::native::InputDeviceIdentity identity;
		bool open_ok = true;
		bool name_ok = true;
		bool id_ok = true;
	};
	struct ReadResult {
		std::vector<unsigned char> bytes;
		long result = 0;
	};

	mister::Error Enumerate(std::vector<std::string>*) override;
	int Open(const char*, int) override;
	int Close(int) override;
	int QueryName(int, std::string*) override;
	int QueryIdentity(int, mister::native::InputDeviceIdentity*) override;
	int CreateCancellation() override;
	int Wait(int, int, bool*, bool*) override;
	long Read(int, void*, std::size_t) override;
	int SignalCancellation(int) override;

	void Add(Candidate);
	void QueueRecord(std::uint16_t type, std::uint16_t code,
		std::int32_t value);
	void QueueShortRecord();
	void QueueReadError();
	void QueueEof();
	void QueueCancellationResult(int result, int error_number);
	bool WaitUntilWaiting();

	mister::Error enumerate_error;
	int cancellation_descriptor = 900;
	bool create_cancellation_ok = true;
	bool wait_ok = true;
	bool signal_ok = true;
	bool block_wait = false;
	unsigned create_cancellation_count = 0;
	std::vector<Candidate> candidates;
	std::vector<std::string> opened_paths;
	std::vector<int> open_flags;
	std::vector<int> closed_descriptors;
	std::vector<int> queried_names;
	std::vector<int> queried_identities;
	std::vector<int> signalled_descriptors;

private:
	const Candidate* Find(int) const;
	std::deque<ReadResult> reads_;
	std::deque<std::pair<int, int>> cancellation_results_;
	std::mutex wait_mutex_;
	std::condition_variable wait_condition_;
	bool cancelled_ = false;
	bool waiting_ = false;
};
#endif

} // namespace mister_test
