// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/linux/input.hpp"

#include <algorithm>
#include <atomic>
#include <cerrno>
#include <cstring>
#include <fcntl.h>
#include <utility>

#if defined(__linux__)
#include <glob.h>
#include <linux/input.h>
#include <poll.h>
#include <sys/eventfd.h>
#include <sys/ioctl.h>
#include <unistd.h>
#endif

namespace mister {
namespace native {
namespace {

constexpr int kCancellationFallbackPollMs = 100;

class Operations {
public:
	virtual ~Operations() {}
	virtual Error Enumerate(std::vector<std::string>*) = 0;
	virtual int Open(const char*, int) = 0;
	virtual int Close(int) = 0;
	virtual int QueryName(int, std::string*) = 0;
	virtual int QueryIdentity(int, InputDeviceIdentity*) = 0;
	virtual int CreateCancellation() = 0;
	virtual int Wait(int, int, bool*, bool*) = 0;
	virtual long Read(int, void*, std::size_t) = 0;
	virtual int SignalCancellation(int) = 0;
};

class PosixOperations final : public Operations {
public:
	Error Enumerate(std::vector<std::string>* paths) override
	{
#if defined(__linux__)
		glob_t matches = {};
		const int result = glob("/dev/input/event*", GLOB_NOSORT, nullptr, &matches);
		if (result == GLOB_NOMATCH) {
			globfree(&matches);
			return {};
		}
		if (result != 0) {
			globfree(&matches);
			return {ErrorCode::io_failed, "input device enumeration failed"};
		}
		for (std::size_t index = 0; index < matches.gl_pathc; ++index)
			paths->push_back(matches.gl_pathv[index]);
		globfree(&matches);
		return {};
#else
		(void)paths;
		return {ErrorCode::io_failed, "evdev input is unavailable"};
#endif
	}

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

	int QueryName(int descriptor, std::string* name) override
	{
#if defined(__linux__)
		char value[256] = {};
		if (ioctl(descriptor, EVIOCGNAME(sizeof(value) - 1u), value) < 0) return -1;
		*name = value;
		return 0;
#else
		(void)descriptor;
		(void)name;
		return -1;
#endif
	}

	int QueryIdentity(int descriptor, InputDeviceIdentity* identity) override
	{
#if defined(__linux__)
		struct input_id id = {};
		if (ioctl(descriptor, EVIOCGID, &id) < 0) return -1;
		identity->bus = id.bustype;
		identity->vendor = id.vendor;
		identity->product = id.product;
		identity->version = id.version;
		return 0;
#else
		(void)descriptor;
		(void)identity;
		return -1;
#endif
	}

	int CreateCancellation() override
	{
#if defined(__linux__)
		return eventfd(0, EFD_CLOEXEC | EFD_NONBLOCK);
#else
		return -1;
#endif
	}

	int Wait(int input_descriptor, int cancellation_descriptor,
		bool* input_ready, bool* cancelled) override
	{
#if defined(__linux__)
		struct pollfd descriptors[2] = {};
		descriptors[0].fd = input_descriptor;
		descriptors[0].events = POLLIN;
		descriptors[1].fd = cancellation_descriptor;
		descriptors[1].events = POLLIN;
		const int result = poll(descriptors, 2, kCancellationFallbackPollMs);
		if (result < 0) return -1;
		*cancelled = (descriptors[1].revents & (POLLIN | POLLERR | POLLHUP | POLLNVAL)) != 0;
		*input_ready = (descriptors[0].revents &
			(POLLIN | POLLERR | POLLHUP | POLLNVAL)) != 0;
		return 0;
#else
		(void)input_descriptor;
		(void)cancellation_descriptor;
		(void)input_ready;
		(void)cancelled;
		return -1;
#endif
	}

	long Read(int descriptor, void* output, std::size_t size) override
	{
#if defined(__linux__)
		ssize_t result = 0;
		do {
			result = read(descriptor, output, size);
		} while (result < 0 && errno == EINTR);
		return static_cast<long>(result);
#else
		(void)descriptor;
		(void)output;
		(void)size;
		return -1;
#endif
	}

	int SignalCancellation(int descriptor) override
	{
#if defined(__linux__)
		const std::uint64_t signal = 1;
		const ssize_t result = write(descriptor, &signal, sizeof(signal));
		return result == static_cast<ssize_t>(sizeof(signal)) ? 0 : -1;
#else
		(void)descriptor;
		return -1;
#endif
	}
};

#if defined(MISTER_RUNTIME_TESTING)
class TestOperations final : public Operations {
public:
	explicit TestOperations(LinuxInputTestOperations& operations)
		: operations_(operations) {}
	Error Enumerate(std::vector<std::string>* paths) override
	{
		return operations_.Enumerate(paths);
	}
	int Open(const char* path, int flags) override
	{
		return operations_.Open(path, flags);
	}
	int Close(int descriptor) override { return operations_.Close(descriptor); }
	int QueryName(int descriptor, std::string* name) override
	{
		return operations_.QueryName(descriptor, name);
	}
	int QueryIdentity(int descriptor, InputDeviceIdentity* identity) override
	{
		return operations_.QueryIdentity(descriptor, identity);
	}
	int CreateCancellation() override
	{
		return operations_.CreateCancellation();
	}
	int Wait(int input, int cancellation, bool* ready, bool* cancelled) override
	{
		return operations_.Wait(input, cancellation, ready, cancelled);
	}
	long Read(int descriptor, void* output, std::size_t size) override
	{
		return operations_.Read(descriptor, output, size);
	}
	int SignalCancellation(int descriptor) override
	{
		return operations_.SignalCancellation(descriptor);
	}

private:
	LinuxInputTestOperations& operations_;
};
#endif

bool SameIdentity(const InputDeviceIdentity& left,
	const InputDeviceIdentity& right)
{
	return left.name == right.name && left.bus == right.bus &&
		left.vendor == right.vendor && left.product == right.product &&
		left.version == right.version;
}

Error Deadline()
{
	return {ErrorCode::io_failed, "deadline exceeded"};
}

} // namespace

const InputDeviceIdentity& FogCastGamepadIdentity()
{
#if defined(__linux__)
	static const InputDeviceIdentity identity = {
		"FogCast Virtual Gamepad", BUS_VIRTUAL, 0x0000, 0x0001, 0x0001};
#else
	static const InputDeviceIdentity identity = {
		"FogCast Virtual Gamepad", 0x0006, 0x0000, 0x0001, 0x0001};
#endif
	return identity;
}

class LinuxInput::Impl {
public:
	Impl(Clock& clock, std::unique_ptr<Operations> owned)
		: clock_(clock), owned_(std::move(owned)), operations_(owned_.get()) {}
	~Impl() { (void)Close(); }

	Error Open(const InputDeviceIdentity& wanted, std::uint64_t deadline)
	{
		if (descriptor_ >= 0) return {ErrorCode::invalid_request, "input device is already open"};
		std::vector<std::string> paths;
		Error error = operations_->Enumerate(&paths);
		if (!error.ok()) return error;
		std::sort(paths.begin(), paths.end());
		int match = -1;
		for (const std::string& path : paths) {
			if (clock_.NowMs() >= deadline) {
				if (match >= 0) operations_->Close(match);
				return Deadline();
			}
			const int candidate = operations_->Open(path.c_str(),
				O_RDONLY | O_CLOEXEC | O_NONBLOCK);
			if (candidate < 0) continue;
			std::string name;
			InputDeviceIdentity observed;
			if (operations_->QueryName(candidate, &name) != 0 ||
				operations_->QueryIdentity(candidate, &observed) != 0) {
				operations_->Close(candidate);
				continue;
			}
			observed.name = name;
			if (!SameIdentity(observed, wanted)) {
				operations_->Close(candidate);
				continue;
			}
			if (match >= 0) {
				operations_->Close(match);
				operations_->Close(candidate);
				return {ErrorCode::io_failed, "multiple FogCast input devices found"};
			}
			match = candidate;
		}
		if (match < 0) return {ErrorCode::io_failed, "FogCast input device not found"};
		if (clock_.NowMs() >= deadline) {
			operations_->Close(match);
			return Deadline();
		}
		const int cancellation = operations_->CreateCancellation();
		if (cancellation < 0) {
			operations_->Close(match);
			return {ErrorCode::io_failed, "input cancellation descriptor failed"};
		}
		descriptor_ = match;
		cancellation_ = cancellation;
		cancellation_requested_.store(false, std::memory_order_release);
		return {};
	}

	Error Read(InputEvent* output, bool* cancelled)
	{
		if (output == nullptr || cancelled == nullptr)
			return {ErrorCode::invalid_request, "missing input event output"};
		if (descriptor_ < 0 || cancellation_ < 0)
			return {ErrorCode::io_failed, "input device is not open"};
		for (;;) {
			if (cancellation_requested_.load(std::memory_order_acquire)) {
				*cancelled = true;
				return {};
			}
			bool ready = false;
			bool stopped = false;
			if (operations_->Wait(descriptor_, cancellation_, &ready, &stopped) != 0) {
				if (errno == EINTR) continue;
				return {ErrorCode::io_failed, "input poll failed"};
			}
			if (stopped || cancellation_requested_.load(std::memory_order_acquire)) {
				*cancelled = true;
				return {};
			}
			if (!ready) continue;
#if defined(__linux__)
			struct input_event event = {};
			const long count = operations_->Read(descriptor_, &event, sizeof(event));
			if (count == 0) return {ErrorCode::io_failed, "input device removed"};
			if (count < 0) return {ErrorCode::io_failed, "input event read failed"};
			if (count != static_cast<long>(sizeof(event)))
				return {ErrorCode::io_failed, "incomplete input event record"};
			Error error = Translate(event.type, event.code, event.value, output);
			if (!error.ok()) return error;
			*cancelled = false;
			return {};
#else
			(void)ready;
			return {ErrorCode::io_failed, "evdev input is unavailable"};
#endif
		}
	}

	Error Cancel()
	{
		if (cancellation_ < 0) return {};
		cancellation_requested_.store(true, std::memory_order_release);
		int result = 0;
		do {
			result = operations_->SignalCancellation(cancellation_);
		} while (result != 0 && errno == EINTR);
		if (result != 0 && errno != EAGAIN && errno != EWOULDBLOCK)
			return {ErrorCode::io_failed, "input cancellation failed"};
		return {};
	}

	Error Close()
	{
		Error error;
		if (descriptor_ >= 0 && operations_->Close(descriptor_) != 0)
			error = {ErrorCode::io_failed, "input device close failed"};
		if (cancellation_ >= 0 && operations_->Close(cancellation_) != 0 && error.ok())
			error = {ErrorCode::io_failed, "input cancellation close failed"};
		descriptor_ = -1;
		cancellation_ = -1;
		return error;
	}

private:
	Error Translate(std::uint16_t type, std::uint16_t code,
		std::int32_t value, InputEvent* output)
	{
#if defined(__linux__)
		if (type == EV_SYN && code == SYN_REPORT && value == 0) {
			*output = {InputControl::synchronize, 0};
			return {};
		}
		if (type == EV_KEY) {
			if (value != 0 && value != 1)
				return {ErrorCode::io_failed, "invalid key value"};
			InputControl control;
			switch (code) {
			case BTN_DPAD_UP: control = InputControl::up; break;
			case BTN_DPAD_DOWN: control = InputControl::down; break;
			case BTN_DPAD_LEFT: control = InputControl::left; break;
			case BTN_DPAD_RIGHT: control = InputControl::right; break;
			case BTN_A: control = InputControl::a; break;
			case BTN_B: control = InputControl::b; break;
			case BTN_C: control = InputControl::c; break;
			case BTN_START: control = InputControl::start; break;
			default: return {ErrorCode::io_failed, "unsupported input event"};
			}
			*output = {control, value};
			return {};
		}
		if (type == EV_ABS && (code == ABS_X || code == ABS_Y)) {
			if (value < -32768 || value > 32767)
				return {ErrorCode::io_failed, "invalid axis value"};
			*output = {code == ABS_X ? InputControl::horizontal : InputControl::vertical,
				value};
			return {};
		}
#else
		(void)type;
		(void)code;
		(void)value;
		(void)output;
#endif
		return {ErrorCode::io_failed, "unsupported input event"};
	}

	Clock& clock_;
	std::unique_ptr<Operations> owned_;
	Operations* operations_;
	int descriptor_ = -1;
	int cancellation_ = -1;
	std::atomic<bool> cancellation_requested_{false};
};

LinuxInput::LinuxInput(Clock& clock)
	: impl_(new Impl(clock, std::unique_ptr<Operations>(new PosixOperations))) {}

#if defined(MISTER_RUNTIME_TESTING)
LinuxInput::LinuxInput(Clock& clock, LinuxInputTestOperations& operations)
	: impl_(new Impl(clock, std::unique_ptr<Operations>(new TestOperations(operations)))) {}
#endif

LinuxInput::~LinuxInput() = default;

Error LinuxInput::Open(const InputDeviceIdentity& identity, std::uint64_t deadline)
{
	return impl_->Open(identity, deadline);
}

Error LinuxInput::Read(InputEvent* event, bool* cancelled)
{
	return impl_->Read(event, cancelled);
}

Error LinuxInput::Cancel()
{
	return impl_->Cancel();
}

Error LinuxInput::Close()
{
	return impl_->Close();
}

} // namespace native
} // namespace mister
