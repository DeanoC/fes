// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "fake_hardware.hpp"

namespace mister_test {

FakeHardware::FakeHardware()
	: idle_result(), launch_result(), development_result(), idle_calls(0),
	  launch_calls(0), development_calls(0), launches(), development_rbfs(),
	  mutex_(), condition_(), block_launch_(false), launch_entered_(false),
	  release_launch_(false)
{
}

mister::HardwareResult FakeHardware::LoadIdle()
{
	std::lock_guard<std::mutex> lock(mutex_);
	++idle_calls;
	if (idle_result.error.ok()) idle_result.mutation_attempted = true;
	return idle_result;
}

mister::HardwareResult FakeHardware::Launch(
	const mister::PreparedLaunch& launch)
{
	std::unique_lock<std::mutex> lock(mutex_);
	++launch_calls;
	launches.push_back(launch);
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

} // namespace mister_test
