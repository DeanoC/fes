// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"

#include <condition_variable>
#include <mutex>
#include <string>
#include <vector>

namespace mister_test {

class FakeHardware final : public mister::Hardware {
public:
	FakeHardware();
	mister::HardwareResult LoadIdle() override;
	mister::HardwareResult Launch(const mister::PreparedLaunch&) override;
	mister::HardwareResult LoadDevelopmentRBF(const std::string&) override;
	void BlockLaunch();
	void WaitUntilLaunchEntered();
	void ReleaseLaunch();

	mister::HardwareResult idle_result;
	mister::HardwareResult launch_result;
	mister::HardwareResult development_result;
	int idle_calls;
	int launch_calls;
	int development_calls;
	std::vector<mister::PreparedLaunch> launches;
	std::vector<std::string> development_rbfs;

private:
	std::mutex mutex_;
	std::condition_variable condition_;
	bool block_launch_;
	bool launch_entered_;
	bool release_launch_;
};

} // namespace mister_test
