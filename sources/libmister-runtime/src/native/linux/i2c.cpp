// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/linux/i2c.hpp"

#include <fcntl.h>

#if defined(__linux__)
#include <linux/i2c-dev.h>
#include <linux/i2c.h>
#include <sys/ioctl.h>
#include <unistd.h>
#endif

#include <utility>

namespace mister {
namespace native {
namespace {

class Operations {
public:
	virtual ~Operations() {}
	virtual int Open(const char* path, int flags) = 0;
	virtual int Close(int descriptor) = 0;
	virtual int SelectSlave(int descriptor, std::uint8_t address) = 0;
	virtual int ReadByteData(int descriptor, std::uint8_t address,
		std::uint8_t* value) = 0;
	virtual int WriteByteData(int descriptor, std::uint8_t address,
		std::uint8_t value) = 0;
};

class PosixOperations final : public Operations {
public:
	int Open(const char* path, int flags) override
	{
#if defined(__linux__)
		return open(path, flags);
#else
		(void)path;
		(void)flags;
		return -1;
#endif
	}
	int Close(int descriptor) override
	{
#if defined(__linux__)
		return close(descriptor);
#else
		(void)descriptor;
		return -1;
#endif
	}
	int SelectSlave(int descriptor, std::uint8_t address) override
	{
#if defined(__linux__)
		return ioctl(descriptor, I2C_SLAVE, static_cast<unsigned long>(address));
#else
		(void)descriptor;
		(void)address;
		return -1;
#endif
	}
	int ReadByteData(int descriptor, std::uint8_t address,
		std::uint8_t* value) override
	{
#if defined(__linux__)
		union i2c_smbus_data data;
		struct i2c_smbus_ioctl_data request = {};
		request.read_write = I2C_SMBUS_READ;
		request.command = address;
		request.size = I2C_SMBUS_BYTE_DATA;
		request.data = &data;
		if (ioctl(descriptor, I2C_SMBUS, &request) < 0) return -1;
		*value = data.byte;
		return 0;
#else
		(void)descriptor;
		(void)address;
		(void)value;
		return -1;
#endif
	}
	int WriteByteData(int descriptor, std::uint8_t address,
		std::uint8_t value) override
	{
#if defined(__linux__)
		union i2c_smbus_data data;
		struct i2c_smbus_ioctl_data request = {};
		data.byte = value;
		request.read_write = I2C_SMBUS_WRITE;
		request.command = address;
		request.size = I2C_SMBUS_BYTE_DATA;
		request.data = &data;
		return ioctl(descriptor, I2C_SMBUS, &request) < 0 ? -1 : 0;
#else
		(void)descriptor;
		(void)address;
		(void)value;
		return -1;
#endif
	}
};

#if defined(MISTER_RUNTIME_TESTING)
class TestOperations final : public Operations {
public:
	explicit TestOperations(LinuxI2cTestOperations& operations)
		: operations_(operations) {}
	int Open(const char* path, int flags) override
	{
		return operations_.Open(path, flags);
	}
	int Close(int descriptor) override { return operations_.Close(descriptor); }
	int SelectSlave(int descriptor, std::uint8_t address) override
	{
		return operations_.SelectSlave(descriptor, address);
	}
	int ReadByteData(int descriptor, std::uint8_t address,
		std::uint8_t* value) override
	{
		return operations_.ReadByteData(descriptor, address, value);
	}
	int WriteByteData(int descriptor, std::uint8_t address,
		std::uint8_t value) override
	{
		return operations_.WriteByteData(descriptor, address, value);
	}

private:
	LinuxI2cTestOperations& operations_;
};
#endif

Error Deadline()
{
	return {ErrorCode::io_failed, "deadline exceeded"};
}

} // namespace

class LinuxI2c::Impl {
public:
	Impl(Clock& clock, std::unique_ptr<Operations> owned)
		: clock_(clock), owned_(std::move(owned)), operations_(owned_.get()),
		  descriptor_(-1), slave_address_(0) {}
	~Impl()
	{
		if (descriptor_ >= 0) operations_->Close(descriptor_);
	}

	Error SelectFirst(std::uint8_t slave_address, std::uint8_t detection_register,
		std::uint64_t deadline, std::string* selected_bus,
		std::uint8_t* detected_value)
	{
		if (selected_bus == nullptr || detected_value == nullptr)
			return {ErrorCode::io_failed, "missing I2C selection output"};
		if (descriptor_ >= 0) {
			if (slave_address != slave_address_)
				return {ErrorCode::io_failed,
					"a different I2C device is already selected"};
			if (clock_.NowMs() >= deadline) return Deadline();
			std::uint8_t detected = 0;
			if (operations_->ReadByteData(descriptor_, detection_register,
				&detected) != 0)
				return {ErrorCode::io_failed, "I2C device detection read failed"};
			*selected_bus = selected_bus_;
			*detected_value = detected;
			return {};
		}
		const char* const buses[] = {
			"/dev/i2c-0", "/dev/i2c-1", "/dev/i2c-2",
		};
		for (const char* bus : buses) {
			if (clock_.NowMs() >= deadline) return Deadline();
			const int flags = O_RDWR | O_CLOEXEC;
			const int candidate = operations_->Open(bus, flags);
			if (candidate < 0) continue;
			if (clock_.NowMs() >= deadline) {
				operations_->Close(candidate);
				return Deadline();
			}
			if (operations_->SelectSlave(candidate, slave_address) != 0) {
				operations_->Close(candidate);
				continue;
			}
			if (clock_.NowMs() >= deadline) {
				operations_->Close(candidate);
				return Deadline();
			}
			std::uint8_t detected = 0;
			if (operations_->ReadByteData(candidate, detection_register, &detected) != 0) {
				operations_->Close(candidate);
				continue;
			}
			descriptor_ = candidate;
			slave_address_ = slave_address;
			selected_bus_ = bus;
			*selected_bus = bus;
			*detected_value = detected;
			return {};
		}
		return {ErrorCode::io_failed, "ADV7513 I2C device not found"};
	}

	Error ReadByte(std::uint8_t address, std::uint8_t* value,
		std::uint64_t deadline)
	{
		if (descriptor_ < 0) return {ErrorCode::io_failed, "I2C device is not selected"};
		if (value == nullptr) return {ErrorCode::io_failed, "missing I2C read output"};
		if (clock_.NowMs() >= deadline) return Deadline();
		std::uint8_t observed = 0;
		if (operations_->ReadByteData(descriptor_, address, &observed) != 0)
			return {ErrorCode::io_failed, "I2C byte read failed"};
		*value = observed;
		return {};
	}

	Error WriteByte(std::uint8_t address, std::uint8_t value,
		std::uint64_t deadline)
	{
		if (descriptor_ < 0) return {ErrorCode::io_failed, "I2C device is not selected"};
		if (clock_.NowMs() >= deadline) return Deadline();
		if (operations_->WriteByteData(descriptor_, address, value) != 0)
			return {ErrorCode::io_failed, "I2C byte write failed"};
		return {};
	}

private:
	Clock& clock_;
	std::unique_ptr<Operations> owned_;
	Operations* operations_;
	int descriptor_;
	std::uint8_t slave_address_;
	std::string selected_bus_;
};

LinuxI2c::LinuxI2c(Clock& clock)
	: impl_(new Impl(clock, std::unique_ptr<Operations>(new PosixOperations))) {}

#if defined(MISTER_RUNTIME_TESTING)
LinuxI2c::LinuxI2c(Clock& clock, LinuxI2cTestOperations& operations)
	: impl_(new Impl(clock, std::unique_ptr<Operations>(new TestOperations(operations)))) {}
#endif

LinuxI2c::~LinuxI2c() = default;

Error LinuxI2c::SelectFirst(std::uint8_t slave_address,
	std::uint8_t detection_register, std::uint64_t deadline,
	std::string* selected_bus, std::uint8_t* detected_value)
{
	return impl_->SelectFirst(slave_address, detection_register, deadline,
		selected_bus, detected_value);
}

Error LinuxI2c::ReadByte(std::uint8_t address, std::uint8_t* value,
	std::uint64_t deadline)
{
	return impl_->ReadByte(address, value, deadline);
}

Error LinuxI2c::WriteByte(std::uint8_t address, std::uint8_t value,
	std::uint64_t deadline)
{
	return impl_->WriteByte(address, value, deadline);
}

} // namespace native
} // namespace mister
