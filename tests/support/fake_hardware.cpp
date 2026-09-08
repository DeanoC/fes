// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "fake_hardware.hpp"

#include <chrono>
#include <utility>

namespace mister_test {

namespace {
class FakeAdmittedCore final : public mister::AdmittedCorePackage {
public:
	explicit FakeAdmittedCore(mister::CorePackageInfo info)
		: AdmittedCorePackage(std::move(info)) {}
};
}

FakeHardware::FakeHardware()
	: idle_result(), launch_result(), development_result(), idle_calls(0),
	  launch_calls(0), development_calls(0), fault_sink_sets(0),
	  idle_without_fault_sink(false), launches(), launch_generations(),
	  development_rbfs(), idle_threads(),
	  mutex_(), condition_(), block_launch_(false), launch_entered_(false),
	  release_launch_(false), fault_sink_(nullptr)
{
}

void FakeHardware::SetFaultSink(mister::HardwareFaultSink* sink)
{
	std::lock_guard<std::mutex> lock(mutex_);
	fault_sink_ = sink;
	++fault_sink_sets;
	condition_.notify_all();
}

mister::Error FakeHardware::AdmitCorePackage(const std::string& directory,
	const std::string& expected_id,
	std::unique_ptr<mister::AdmittedCorePackage>* output)
{
	std::lock_guard<std::mutex> lock(mutex_);
	++admission_calls;
	if (!admission_result.ok()) return admission_result;
	if (output == nullptr)
		return {mister::ErrorCode::invalid_request, "missing admitted package output"};
	mister::CorePackageInfo info = core_info;
	info.package_id = expected_id;
	output->reset(new FakeAdmittedCore(std::move(info)));
	events.push_back("admit:" + directory);
	return {};
}

mister::HardwareResult FakeHardware::LoadCore(
	std::unique_ptr<mister::AdmittedCorePackage> package,
	std::uint64_t generation)
{
	std::lock_guard<std::mutex> lock(mutex_);
	++core_calls;
	core_generations.push_back(generation);
	events.push_back("load_core:" + package->info().package_id);
	mister::HardwareResult result = core_result;
	if (result.error.ok()) {
		result.mutation_attempted = true;
		if (result.observed_core.empty())
			result.observed_core = package->info().declared_core;
	}
	return result;
}

mister::HardwareResult FakeHardware::LoadIdle()
{
	std::lock_guard<std::mutex> lock(mutex_);
	++idle_calls;
	idle_threads.push_back(std::this_thread::get_id());
	idle_without_fault_sink = idle_without_fault_sink || fault_sink_ == nullptr;
	condition_.notify_all();
	if (idle_result.error.ok()) idle_result.mutation_attempted = true;
	return idle_result;
}

mister::HardwareResult FakeHardware::Launch(
	const mister::PreparedLaunch& launch, std::uint64_t generation)
{
	std::unique_lock<std::mutex> lock(mutex_);
	++launch_calls;
	launches.push_back(launch);
	launch_generations.push_back(generation);
	launch_entered_ = true;
	condition_.notify_all();
	while (block_launch_ && !release_launch_) condition_.wait(lock);
	mister::HardwareResult result = launch_result;
	if (result.error.ok()) {
		result.mutation_attempted = true;
		if (result.observed_core.empty()) result.observed_core = launch.expected_core;
	}
	return result;
}

mister::HardwareResult FakeHardware::LoadDevelopmentRBF(
	const std::string& rbf)
{
	std::lock_guard<std::mutex> lock(mutex_);
	++development_calls;
	development_rbfs.push_back(rbf);
	mister::HardwareResult result = development_result;
	if (result.error.ok()) result.mutation_attempted = true;
	return result;
}

void FakeHardware::BlockLaunch()
{
	std::lock_guard<std::mutex> lock(mutex_);
	block_launch_ = true;
	launch_entered_ = false;
	release_launch_ = false;
}

void FakeHardware::WaitUntilLaunchEntered()
{
	std::unique_lock<std::mutex> lock(mutex_);
	while (!launch_entered_) condition_.wait(lock);
}

void FakeHardware::ReleaseLaunch()
{
	std::lock_guard<std::mutex> lock(mutex_);
	release_launch_ = true;
	condition_.notify_all();
}

void FakeHardware::ReportFault(std::uint64_t generation, mister::Error error)
{
	mister::HardwareFaultSink* sink = nullptr;
	{
		std::lock_guard<std::mutex> lock(mutex_);
		sink = fault_sink_;
	}
	if (sink != nullptr) sink->ReportHardwareFault({generation, std::move(error)});
}

bool FakeHardware::WaitForIdleCalls(int count)
{
	std::unique_lock<std::mutex> lock(mutex_);
	return condition_.wait_for(lock, std::chrono::seconds(2),
		[this, count]() { return idle_calls >= count; });
}

} // namespace mister_test
