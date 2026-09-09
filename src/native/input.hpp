// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "native/hardware.hpp"

#include <cstdint>
#include <functional>
#include <memory>
#include <string>

namespace mister {
namespace native {

class Spi;

using ButtonWriter = std::function<Error(std::uint16_t,
	std::uint64_t absolute_deadline_ms)>;

struct InputDeviceIdentity {
	std::string name;
	std::uint16_t bus = 0;
	std::uint16_t vendor = 0;
	std::uint16_t product = 0;
	std::uint16_t version = 0;
};

enum class InputControl {
	synchronize,
	up,
	down,
	left,
	right,
	a,
	b,
	c,
	x,
	y,
	l,
	r,
	select,
	start,
	horizontal,
	vertical,
};

struct InputEvent {
	InputControl control = InputControl::synchronize;
	std::int32_t value = 0;
};

class InputDevice {
public:
	virtual ~InputDevice() {}
	virtual Error Open(const InputDeviceIdentity&,
		std::uint64_t absolute_deadline_ms) = 0;
	virtual Error Read(InputEvent*, bool* cancelled) = 0;
	// Once called, every pending or future Read must return with cancelled=true,
	// even when Cancel reports that its prompt wake signal failed.
	virtual Error Cancel() = 0;
	virtual Error Close() = 0;
};

class InputSession {
public:
	virtual ~InputSession() {}
	virtual Error Open(const InputDeviceIdentity&, const InputRecipe&,
		std::uint64_t absolute_deadline_ms, ButtonWriter = {}) = 0;
	virtual Error Start(std::uint64_t generation,
		std::function<void(std::uint64_t, Error)> on_fault) = 0;
	virtual Error Neutralize(std::uint64_t absolute_deadline_ms) = 0;
	virtual Error Stop(std::uint64_t absolute_deadline_ms) = 0;
};

class NativeInputSession final : public InputSession {
public:
	NativeInputSession(InputDevice&, Spi&, Clock&,
		std::uint32_t delivery_timeout_ms);
	~NativeInputSession();
	NativeInputSession(const NativeInputSession&) = delete;
	NativeInputSession& operator=(const NativeInputSession&) = delete;

	Error Open(const InputDeviceIdentity&, const InputRecipe&,
		std::uint64_t, ButtonWriter = {}) override;
	Error Start(std::uint64_t,
		std::function<void(std::uint64_t, Error)>) override;
	Error Neutralize(std::uint64_t) override;
	Error Stop(std::uint64_t) override;

private:
	class Impl;
	std::unique_ptr<Impl> impl_;
};

} // namespace native
} // namespace mister
