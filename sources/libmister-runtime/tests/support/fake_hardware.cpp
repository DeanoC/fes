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
	mister::CoreComposition composition() const override { return composition_; }
	mister::CoreComposition composition_;
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
	supported.programming_profiles = {
		"development-contained-v1", "fes-gp-v1", "mister-v1"};
	supported.abis = {
		{"fes.simple-game", 1, 0,
			{{"fes.gamepad", 1, 0}, {"fes.video.fixed-720p60", 1, 0}}},
		{"mister", 1, 0, {}}};
	core_info.descriptor.format = 2;
	core_info.descriptor.core = {"custom-core", "Custom Core", "test", "1.0.0", ""};
	core_info.descriptor.target = {"de10_nano", "5CSEBA6U23I7", "fes-gp-v1"};
	core_info.descriptor.payload = {"core.rbf", 1, std::string(64, 'b')};
	core_info.descriptor.abi = {"fes.simple-game", 1, 0};
	core_info.descriptor.interfaces = {
		{"fes.gamepad", 1, 0, true},
		{"fes.video.fixed-720p60", 1, 0, true}};
	core_info.descriptor.build = {std::string(32, 'c'), "https://example.invalid/core",
		std::string(40, 'd'), std::string(64, 'e'), "test toolchain"};
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

mister::Error FakeHardware::AdmitCoreComposition(const std::string& directory,
	const std::string& id, const mister::CoreCompositionRequest& request,
	std::unique_ptr<mister::AdmittedCorePackage>* output)
{
	auto error = AdmitCorePackage(directory, id, output);
	if (error.ok()) static_cast<FakeAdmittedCore*>(output->get())->composition_ = request.composition;
	return error;
}

mister::Error FakeHardware::InspectCorePackage(const std::string& directory,
	const std::string& expected_id, mister::CorePackageInspection* output)
{
	std::lock_guard<std::mutex> lock(mutex_);
	++inspection_calls;
	if (!inspection_result.ok()) return inspection_result;
	if (output == nullptr)
		return {mister::ErrorCode::invalid_request, "missing inspection output"};
	output->package_id = expected_id;
	output->descriptor = core_info.descriptor;
	output->compatible = inspection_compatible;
	output->compatibility_error = compatibility_error;
	events.push_back("inspect:" + directory);
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
	std::unique_lock<std::mutex> lock(mutex_);
	++idle_calls;
	idle_threads.push_back(std::this_thread::get_id());
	idle_without_fault_sink = idle_without_fault_sink || fault_sink_ == nullptr;
	idle_entered_ = true;
	condition_.notify_all();
	while (block_idle_ && !release_idle_) condition_.wait(lock);
	block_idle_ = false;
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
	const std::string& rbf, std::uint64_t generation)
{
	std::lock_guard<std::mutex> lock(mutex_);
	++development_calls;
	development_rbfs.push_back(rbf);
	development_generations.push_back(generation);
	mister::HardwareResult result = development_result;
	if (result.error.ok()) result.mutation_attempted = true;
	return result;
}

mister::HardwareResult FakeHardware::LoadContainedDevelopmentRBF(
	const std::string& rbf, std::uint64_t generation)
{
	mister::HardwareResult result = LoadDevelopmentRBF(rbf, generation);
	std::lock_guard<std::mutex> lock(mutex_);
	++contained_development_calls;
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

void FakeHardware::BlockNextIdle()
{
	std::lock_guard<std::mutex> lock(mutex_);
	block_idle_ = true;
	idle_entered_ = false;
	release_idle_ = false;
}

void FakeHardware::WaitUntilIdleEntered()
{
	std::unique_lock<std::mutex> lock(mutex_);
	while (!idle_entered_) condition_.wait(lock);
}

void FakeHardware::ReleaseIdle()
{
	std::lock_guard<std::mutex> lock(mutex_);
	release_idle_ = true;
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
