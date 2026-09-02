// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"

#include <condition_variable>
#include <cstdint>
#include <mutex>
#include <string>
#include <thread>
#include <vector>

namespace mister_test {

class FakeHardware final : public mister::Hardware {
public:
	FakeHardware();
	void SetFaultSink(mister::HardwareFaultSink*) override;
	mister::HardwareResult LoadIdle() override;
	mister::HardwareResult Launch(const mister::PreparedLaunch&,
		std::uint64_t generation) override;
	mister::HardwareResult LoadDevelopmentRBF(const std::string&) override;
	void BlockLaunch();
	void WaitUntilLaunchEntered();
	void ReleaseLaunch();
	void ReportFault(std::uint64_t generation, mister::Error);
	bool WaitForIdleCalls(int count);

	mister::HardwareResult idle_result;
	mister::HardwareResult launch_result;
	mister::HardwareResult development_result;
	int idle_calls;
	int launch_calls;
	int development_calls;
	int fault_sink_sets;
	bool idle_without_fault_sink;
	std::vector<mister::PreparedLaunch> launches;
	std::vector<std::uint64_t> launch_generations;
	std::vector<std::string> development_rbfs;
	std::vector<std::thread::id> idle_threads;

private:
	std::mutex mutex_;
	std::condition_variable condition_;
	bool block_launch_;
	bool launch_entered_;
	bool release_launch_;
	mister::HardwareFaultSink* fault_sink_;
};

} // namespace mister_test
