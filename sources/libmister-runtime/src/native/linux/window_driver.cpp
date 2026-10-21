// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/linux/window_driver.hpp"

#include "native/generated/fes_application.hpp"

#include <cerrno>
#include <cstring>
#include <fcntl.h>
#include <fstream>
#include <sstream>
#include <unistd.h>

#if defined(__linux__)
#include <sys/syscall.h>
#endif

namespace mister {
namespace native {
namespace {

constexpr char kModuleName[] = "fes_window";
constexpr char kSysfs[] = "/sys/module/fes_window/";

std::string Trimmed(std::string value)
{
	while (!value.empty() && (value.back() == '\n' || value.back() == ' '))
		value.pop_back();
	return value;
}

class SystemOperations final : public WindowDriverOperations {
public:
	bool ReadFile(const std::string& path, std::string* contents) override
	{
		std::ifstream stream(path);
		if (!stream)
			return false;
		std::ostringstream text;
		text << stream.rdbuf();
		*contents = text.str();
		return !stream.bad();
	}
	int LoadModule(const std::string& path, const std::string& params) override
	{
#if defined(__linux__)
		const int descriptor = open(path.c_str(), O_RDONLY | O_CLOEXEC);
		if (descriptor < 0)
			return errno;
		const int result = syscall(SYS_finit_module, descriptor, params.c_str(), 0) == 0 ? 0 : errno;
		close(descriptor);
		return result;
#else
		(void)path;
		(void)params;
		return ENOSYS;
#endif
	}
	int UnloadModule(const std::string& name) override
	{
#if defined(__linux__)
		return syscall(SYS_delete_module, name.c_str(), O_NONBLOCK) == 0 ? 0 : errno;
#else
		(void)name;
		return ENOSYS;
#endif
	}
};

} // namespace

std::string WindowDriverDirectory()
{
	char executable[4096];
	const ssize_t length = readlink("/proc/self/exe", executable, sizeof(executable) - 1);
	if (length <= 0)
		return "";
	std::string release(executable, static_cast<std::size_t>(length));
	// <release>/bin/mister-runtime
	for (int level = 0; level < 2; ++level) {
		const std::size_t slash = release.rfind('/');
		if (slash == std::string::npos)
			return "";
		release.erase(slash);
	}
	return release + "/share/fes-window";
}

Error LoadWindowDriver(const std::string& directory)
{
	SystemOperations operations;
	return LoadWindowDriver(directory, operations);
}

Error LoadWindowDriver(const std::string& directory, WindowDriverOperations& operations)
{
	std::string expected;
	if (directory.empty() || !operations.ReadFile(directory + "/version", &expected) ||
		Trimmed(expected).empty())
		return {ErrorCode::io_failed, "fes-window driver version is missing from " + directory};
	expected = Trimmed(expected);
	const std::string base = std::to_string(generated::FesApplicationHpsDdrWindowBase);
	const std::string size = std::to_string(generated::FesApplicationHpsDdrWindowBytes);

	std::string state;
	if (operations.ReadFile(std::string(kSysfs) + "initstate", &state)) {
		std::string version, loaded_base, loaded_size;
		operations.ReadFile(std::string(kSysfs) + "version", &version);
		operations.ReadFile(std::string(kSysfs) + "parameters/window_base", &loaded_base);
		operations.ReadFile(std::string(kSysfs) + "parameters/window_size", &loaded_size);
		if (Trimmed(state) == "live" && Trimmed(version) == expected &&
			Trimmed(loaded_base) == base && Trimmed(loaded_size) == size)
			return {};
		const int unloaded = operations.UnloadModule(kModuleName);
		if (unloaded != 0)
			return {ErrorCode::busy, "loaded fes-window driver " +
				(Trimmed(version).empty() ? std::string("without a version") : Trimmed(version)) +
				" cannot be replaced: " + std::strerror(unloaded)};
	}
	const int loaded = operations.LoadModule(directory + "/fes_window.ko",
		"window_base=" + base + " window_size=" + size);
	if (loaded != 0)
		return {ErrorCode::io_failed, "loading fes-window driver " + expected + ": " +
			std::strerror(loaded)};
	return {};
}

} // namespace native
} // namespace mister
