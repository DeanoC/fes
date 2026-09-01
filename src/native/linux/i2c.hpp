// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "native/hardware.hpp"

#include <cstdint>
#include <memory>
#include <string>

namespace mister {
namespace native {

class I2c {
public:
	virtual ~I2c() {}
	virtual Error SelectFirst(std::uint8_t slave_address,
		std::uint8_t detection_register, std::uint64_t absolute_deadline_ms,
		std::string* selected_bus, std::uint8_t* detected_value) = 0;
	virtual Error ReadByte(std::uint8_t register_address, std::uint8_t* value,
		std::uint64_t absolute_deadline_ms) = 0;
	virtual Error WriteByte(std::uint8_t register_address, std::uint8_t value,
		std::uint64_t absolute_deadline_ms) = 0;
};

#if defined(MISTER_RUNTIME_TESTING)
class LinuxI2cTestOperations {
public:
	virtual ~LinuxI2cTestOperations() {}
	virtual int Open(const char* path, int flags) = 0;
	virtual int Close(int descriptor) = 0;
	virtual int SelectSlave(int descriptor, std::uint8_t address) = 0;
	virtual int ReadByteData(int descriptor, std::uint8_t address,
		std::uint8_t* value) = 0;
	virtual int WriteByteData(int descriptor, std::uint8_t address,
		std::uint8_t value) = 0;
};
#endif

class LinuxI2c final : public I2c {
public:
	explicit LinuxI2c(Clock&);
#if defined(MISTER_RUNTIME_TESTING)
	LinuxI2c(Clock&, LinuxI2cTestOperations&);
#endif
	~LinuxI2c();
	LinuxI2c(const LinuxI2c&) = delete;
	LinuxI2c& operator=(const LinuxI2c&) = delete;
	Error SelectFirst(std::uint8_t, std::uint8_t, std::uint64_t,
		std::string*, std::uint8_t*) override;
	Error ReadByte(std::uint8_t, std::uint8_t*, std::uint64_t) override;
	Error WriteByte(std::uint8_t, std::uint8_t, std::uint64_t) override;

private:
	class Impl;
	std::unique_ptr<Impl> impl_;
};

} // namespace native
} // namespace mister
