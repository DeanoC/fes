// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/linux/window_driver.hpp"

#include <assert.h>
#include <errno.h>
#include <stdio.h>

#include <map>
#include <string>
#include <vector>

namespace {

const char kDirectory[] = "/run/fogcast/software/current/share/fes-window";
const char kParams[] = "window_base=805306368 window_size=268435456";

class Operations final : public mister::native::WindowDriverOperations {
public:
	bool ReadFile(const std::string& path, std::string* contents) override
	{
		const auto found = files.find(path);
		if (found == files.end())
			return false;
		*contents = found->second;
		return true;
	}
	int LoadModule(const std::string& path, const std::string& params) override
	{
		loads.push_back(path + " " + params);
		return load_result;
	}
	int UnloadModule(const std::string& name) override
	{
		unloads.push_back(name);
		return unload_result;
	}
	void Loaded(const std::string& version, const std::string& base, const std::string& size)
	{
		files["/sys/module/fes_window/initstate"] = "live\n";
		files["/sys/module/fes_window/version"] = version + "\n";
		files["/sys/module/fes_window/parameters/window_base"] = base + "\n";
		files["/sys/module/fes_window/parameters/window_size"] = size + "\n";
	}
	std::map<std::string, std::string> files = {
		{std::string(kDirectory) + "/version", "0123456789abcdef\n"}};
	std::vector<std::string> loads;
	std::vector<std::string> unloads;
	int load_result = 0;
	int unload_result = 0;
};

const std::string kLoad = std::string(kDirectory) + "/fes_window.ko " + kParams;

void TestLoadsWithContractWindow()
{
	Operations operations;
	assert(mister::native::LoadWindowDriver(kDirectory, operations).ok());
	assert(operations.unloads.empty());
	assert(operations.loads == std::vector<std::string>{kLoad});
}

void TestKeepsMatchingLoadedDriver()
{
	Operations operations;
	operations.Loaded("0123456789abcdef", "805306368", "268435456");
	assert(mister::native::LoadWindowDriver(kDirectory, operations).ok());
	assert(operations.unloads.empty() && operations.loads.empty());
}

void TestReplacesOtherVersionOrWindow()
{
	for (const auto& loaded : std::vector<std::vector<std::string>>{
			 {"fedcba9876543210", "805306368", "268435456"},
			 {"0123456789abcdef", "805306368", "4194304"},
			 {"", "805306368", "268435456"}}) {
		Operations operations;
		operations.Loaded(loaded[0], loaded[1], loaded[2]);
		if (loaded[0].empty())
			operations.files.erase("/sys/module/fes_window/version");
		assert(mister::native::LoadWindowDriver(kDirectory, operations).ok());
		assert(operations.unloads == std::vector<std::string>{"fes_window"});
		assert(operations.loads == std::vector<std::string>{kLoad});
	}
}

void TestInUseDriverIsNotReplaced()
{
	Operations operations;
	operations.Loaded("fedcba9876543210", "805306368", "268435456");
	operations.unload_result = EWOULDBLOCK;
	const mister::Error error = mister::native::LoadWindowDriver(kDirectory, operations);
	assert(error.code == mister::ErrorCode::busy);
	assert(error.message.find("fedcba9876543210") != std::string::npos);
	assert(operations.loads.empty());
}

void TestReportsLoadFailure()
{
	Operations operations;
	operations.load_result = ENOEXEC;
	const mister::Error error = mister::native::LoadWindowDriver(kDirectory, operations);
	assert(error.code == mister::ErrorCode::io_failed);
	assert(error.message.find("0123456789abcdef") != std::string::npos);
}

void TestMissingVersionLoadsNothing()
{
	Operations operations;
	operations.files.clear();
	assert(mister::native::LoadWindowDriver(kDirectory, operations).code ==
		mister::ErrorCode::io_failed);
	assert(mister::native::LoadWindowDriver("", operations).code ==
		mister::ErrorCode::io_failed);
	assert(operations.loads.empty() && operations.unloads.empty());
}

} // namespace

int main()
{
	TestLoadsWithContractWindow();
	TestKeepsMatchingLoadedDriver();
	TestReplacesOtherVersionOrWindow();
	TestInUseDriverIsNotReplaced();
	TestReportsLoadFailure();
	TestMissingVersionLoadsNothing();
	puts("window_driver_test passed");
	return 0;
}
