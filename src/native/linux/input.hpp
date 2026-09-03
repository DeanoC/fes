// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "native/input.hpp"

#include <cstddef>
#include <cstdint>
#include <memory>
#include <string>
#include <vector>

namespace mister {
namespace native {

const InputDeviceIdentity& FogCastGamepadIdentity();

#if defined(MISTER_RUNTIME_TESTING)
class LinuxInputTestOperations {
public:
	virtual ~LinuxInputTestOperations() {}
	virtual Error Enumerate(std::vector<std::string>*) = 0;
	virtual int Open(const char*, int flags) = 0;
	virtual int Close(int descriptor) = 0;
	virtual int QueryName(int descriptor, std::string*) = 0;
	virtual int QueryIdentity(int descriptor, InputDeviceIdentity*) = 0;
	virtual int CreateCancellation() = 0;
	virtual int Wait(int input_descriptor, int cancellation_descriptor,
		bool* input_ready, bool* cancelled) = 0;
	virtual long Read(int descriptor, void*, std::size_t) = 0;
	virtual int SignalCancellation(int descriptor) = 0;
};
#endif

class LinuxInput final : public InputDevice {
public:
	explicit LinuxInput(Clock&);
#if defined(MISTER_RUNTIME_TESTING)
	LinuxInput(Clock&, LinuxInputTestOperations&);
#endif
	~LinuxInput();
	LinuxInput(const LinuxInput&) = delete;
	LinuxInput& operator=(const LinuxInput&) = delete;

	Error Open(const InputDeviceIdentity&, std::uint64_t) override;
	Error Read(InputEvent*, bool*) override;
	Error Cancel() override;
	Error Close() override;

private:
	class Impl;
	std::unique_ptr<Impl> impl_;
};

} // namespace native
} // namespace mister
