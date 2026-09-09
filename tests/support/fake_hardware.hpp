// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"

#include <condition_variable>
#include <functional>
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
	mister::Error FlushSave() override { ++flush_calls; if (on_flush) on_flush(); return flush_result; }
	mister::Error RestoreInput(std::uint64_t generation) override
	{
		++restore_input_calls;
		restored_input_generations.push_back(generation);
		if (on_restore_input) on_restore_input();
		return restore_input_result;
	}
	mister::Error AdmitCorePackage(const std::string&, const std::string&,
		std::unique_ptr<mister::AdmittedCorePackage>*) override;
	mister::Error InspectCorePackage(const std::string&, const std::string&,
		mister::CorePackageInspection*) override;
	mister::Capabilities capabilities() const override { return supported; }
	mister::HardwareResult LoadCore(
		std::unique_ptr<mister::AdmittedCorePackage>, std::uint64_t) override;
	int flush_calls = 0;
	std::function<void()> on_flush;
	mister::Error flush_result;
	int restore_input_calls = 0;
	std::function<void()> on_restore_input;
	mister::Error restore_input_result;
	std::vector<std::uint64_t> restored_input_generations;
	mister::HardwareResult Launch(const mister::PreparedLaunch&,
		std::uint64_t generation) override;
	mister::HardwareResult LoadDevelopmentRBF(const std::string&,
		std::uint64_t generation) override;
	mister::HardwareResult LoadContainedDevelopmentRBF(const std::string&,
		std::uint64_t generation) override;
	void BlockLaunch();
	void WaitUntilLaunchEntered();
	void ReleaseLaunch();
	void BlockNextIdle();
	void WaitUntilIdleEntered();
	void ReleaseIdle();
	void ReportFault(std::uint64_t generation, mister::Error);
	bool WaitForIdleCalls(int count);

	mister::HardwareResult idle_result;
	mister::HardwareResult launch_result;
	mister::HardwareResult development_result;
	mister::Error admission_result;
	mister::HardwareResult core_result;
	mister::Error inspection_result;
	bool inspection_compatible = true;
	mister::Error compatibility_error;
	mister::Capabilities supported;
	mister::CorePackageInfo core_info = {
		std::string(64, 'a'), "custom-core", "", {}};
	int idle_calls;
	int launch_calls;
	int development_calls;
	int admission_calls = 0;
	int core_calls = 0;
	int inspection_calls = 0;
	int contained_development_calls = 0;
	int fault_sink_sets;
	bool idle_without_fault_sink;
	std::vector<mister::PreparedLaunch> launches;
	std::vector<std::uint64_t> launch_generations;
	std::vector<std::string> development_rbfs;
	std::vector<std::uint64_t> development_generations;
	std::vector<std::thread::id> idle_threads;
	std::vector<std::string> events;
	std::vector<std::uint64_t> core_generations;

private:
	std::mutex mutex_;
	std::condition_variable condition_;
	bool block_launch_;
	bool launch_entered_;
	bool release_launch_;
	bool block_idle_ = false;
	bool idle_entered_ = false;
	bool release_idle_ = false;
	mister::HardwareFaultSink* fault_sink_;
};

} // namespace mister_test
