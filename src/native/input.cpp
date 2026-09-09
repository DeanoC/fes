// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/input.hpp"

#include "native/linux/spi.hpp"

#include <array>
#include <limits>
#include <mutex>
#include <system_error>
#include <thread>
#include <utility>
#include <vector>

namespace mister {
namespace native {
namespace {

constexpr std::int32_t kAxisThreshold = 16000;

Error Invalid(const char* message)
{
	return {ErrorCode::invalid_request, message};
}

bool ValidRecipe(const InputRecipe& recipe)
{
	if (recipe.player_count != 1 || recipe.player_command == 0) return false;
	const std::array<std::uint16_t, 13> masks = {{recipe.up, recipe.down,
		recipe.left, recipe.right, recipe.a, recipe.b, recipe.c, recipe.start,
		recipe.x, recipe.y, recipe.l, recipe.r, recipe.select}};
	if (!recipe.up || !recipe.down || !recipe.left || !recipe.right ||
		!recipe.a || !recipe.b || !recipe.start) return false;
	std::uint16_t seen = 0;
	for (std::uint16_t mask : masks) {
		if (mask == 0) continue;
		if ((mask & (mask - 1u)) != 0 || (seen & mask) != 0)
			return false;
		seen = static_cast<std::uint16_t>(seen | mask);
	}
	return true;
}

std::uint64_t AddDeadline(std::uint64_t now, std::uint32_t duration)
{
	const std::uint64_t maximum = std::numeric_limits<std::uint64_t>::max();
	return now > maximum - duration ? maximum : now + duration;
}

} // namespace

class NativeInputSession::Impl {
public:
	Impl(InputDevice& device, Spi& spi, Clock& clock,
		std::uint32_t delivery_timeout_ms)
		: device_(device), spi_(spi), clock_(clock),
		  delivery_timeout_ms_(delivery_timeout_ms) {}

	~Impl()
	{
		ShutdownWithoutNeutral();
	}

	Error Open(const InputDeviceIdentity& identity, const InputRecipe& recipe,
		std::uint64_t deadline, ButtonWriter writer)
	{
		std::lock_guard<std::mutex> lock(mutex_);
		if (opened_ || worker_.joinable()) return Invalid("input session is already open");
		if (identity.name.empty() || identity.name.find('\0') != std::string::npos)
			return Invalid("invalid input identity");
		if (!ValidRecipe(recipe)) return Invalid("invalid input recipe");
		Error error = device_.Open(identity, deadline);
		if (!error.ok()) return error;
		recipe_ = recipe;
		writer_ = std::move(writer);
		opened_ = true;
		accepting_ = false;
		core_addressable_ = false;
		digital_map_ = 0;
		committed_map_ = 0;
		horizontal_ = 0;
		vertical_ = 0;
		return {};
	}

	Error Start(std::uint64_t generation,
		std::function<void(std::uint64_t, Error)> on_fault)
	{
		std::lock_guard<std::mutex> lock(mutex_);
		if (!opened_ || worker_.joinable()) return Invalid("input session is not startable");
		if (!core_addressable_) return Invalid("input session is not neutralized");
		if (generation == 0 || generation < last_generation_)
			return Invalid("input generation is stale");
		if (!on_fault) return Invalid("missing input fault callback");
		active_generation_ = generation;
		on_fault_ = std::move(on_fault);
		fault_sent_ = false;
		accepting_ = true;
		try {
			worker_ = std::thread([this, generation] { Run(generation); });
		} catch (const std::system_error&) {
			accepting_ = false;
			active_generation_ = 0;
			on_fault_ = {};
			return {ErrorCode::io_failed, "input worker start failed"};
		}
		last_generation_ = generation;
		return {};
	}

	Error Neutralize(std::uint64_t deadline)
	{
		std::lock_guard<std::mutex> lock(mutex_);
		if (!opened_) return Invalid("input session is not open");
		const Error error = Send(0, deadline);
		if (!error.ok()) return error;
		digital_map_ = 0;
		committed_map_ = 0;
		horizontal_ = 0;
		vertical_ = 0;
		core_addressable_ = true;
		return {};
	}

	Error Stop(std::uint64_t deadline)
	{
		bool join = false;
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (!opened_ && !worker_.joinable()) return {};
			accepting_ = false;
			active_generation_ = 0;
			join = worker_.joinable();
		}

		Error first_error;
		if (join) {
			first_error = device_.Cancel();
			worker_.join();
		}

		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (core_addressable_) {
				const Error neutral = Send(0, deadline);
				if (first_error.ok() && !neutral.ok()) first_error = neutral;
			}
			const Error close = device_.Close();
			if (first_error.ok() && !close.ok()) first_error = close;
			opened_ = false;
			core_addressable_ = false;
			accepting_ = false;
			active_generation_ = 0;
			on_fault_ = {};
			writer_ = {};
			digital_map_ = 0;
			committed_map_ = 0;
			horizontal_ = 0;
			vertical_ = 0;
		}
		return first_error;
	}

private:
	void Run(std::uint64_t generation)
	{
		for (;;) {
			InputEvent event;
			bool cancelled = false;
			const Error read = device_.Read(&event, &cancelled);
			if (cancelled) return;
			if (!read.ok()) {
				Fault(generation, read);
				return;
			}
			const Error applied = Apply(generation, event);
			if (!applied.ok()) {
				Fault(generation, applied);
				return;
			}
		}
	}

	Error Apply(std::uint64_t generation, const InputEvent& event)
	{
		std::lock_guard<std::mutex> lock(mutex_);
		if (!accepting_ || generation != active_generation_) return {};
		switch (event.control) {
		case InputControl::synchronize: {
			if (event.value != 0) return {ErrorCode::io_failed, "invalid input event"};
			const std::uint16_t next = CurrentMap();
			if (next == committed_map_) return {};
			const Error error = Send(next,
				AddDeadline(clock_.NowMs(), delivery_timeout_ms_));
			if (!error.ok()) return error;
			committed_map_ = next;
			return {};
		}
		case InputControl::horizontal:
		case InputControl::vertical:
			if (event.value < -32768 || event.value > 32767)
				return {ErrorCode::io_failed, "invalid axis value"};
			if (event.control == InputControl::horizontal) horizontal_ = event.value;
			else vertical_ = event.value;
			return {};
		case InputControl::up:
			return SetDigital(recipe_.up, event.value);
		case InputControl::down:
			return SetDigital(recipe_.down, event.value);
		case InputControl::left:
			return SetDigital(recipe_.left, event.value);
		case InputControl::right:
			return SetDigital(recipe_.right, event.value);
		case InputControl::a:
			return SetDigital(recipe_.a, event.value);
		case InputControl::b:
			return SetDigital(recipe_.b, event.value);
		case InputControl::c:
			return SetDigital(recipe_.c, event.value);
		case InputControl::x:
			return SetDigital(recipe_.x, event.value);
		case InputControl::y:
			return SetDigital(recipe_.y, event.value);
		case InputControl::l:
			return SetDigital(recipe_.l, event.value);
		case InputControl::r:
			return SetDigital(recipe_.r, event.value);
		case InputControl::select:
			return SetDigital(recipe_.select, event.value);
		case InputControl::start:
			return SetDigital(recipe_.start, event.value);
		}
		return {ErrorCode::io_failed, "unsupported input event"};
	}

	Error SetDigital(std::uint16_t mask, std::int32_t value)
	{
		if (value != 0 && value != 1)
			return {ErrorCode::io_failed, "invalid key value"};
		if (value == 1) digital_map_ = static_cast<std::uint16_t>(digital_map_ | mask);
		else digital_map_ = static_cast<std::uint16_t>(digital_map_ & ~mask);
		return {};
	}

	std::uint16_t CurrentMap() const
	{
		std::uint16_t map = digital_map_;
		if (horizontal_ <= -kAxisThreshold) map = static_cast<std::uint16_t>(map | recipe_.left);
		if (horizontal_ >= kAxisThreshold) map = static_cast<std::uint16_t>(map | recipe_.right);
		if (vertical_ <= -kAxisThreshold) map = static_cast<std::uint16_t>(map | recipe_.up);
		if (vertical_ >= kAxisThreshold) map = static_cast<std::uint16_t>(map | recipe_.down);
		if ((map & recipe_.left) != 0 && (map & recipe_.right) != 0)
			map = static_cast<std::uint16_t>(map & ~(recipe_.left | recipe_.right));
		if ((map & recipe_.up) != 0 && (map & recipe_.down) != 0)
			map = static_cast<std::uint16_t>(map & ~(recipe_.up | recipe_.down));
		return map;
	}

	Error Send(std::uint16_t map, std::uint64_t deadline)
	{
		if (writer_) return writer_(map, deadline);
		return spi_.Exchange(kUserIoTarget, {recipe_.player_command, map},
			nullptr, deadline);
	}

	void Fault(std::uint64_t generation, Error error)
	{
		std::function<void(std::uint64_t, Error)> callback;
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (!accepting_ || fault_sent_ || generation != active_generation_) return;
			fault_sent_ = true;
			accepting_ = false;
			callback = on_fault_;
		}
		callback(generation, std::move(error));
	}

	void ShutdownWithoutNeutral()
	{
		bool join = false;
		{
			std::lock_guard<std::mutex> lock(mutex_);
			accepting_ = false;
			active_generation_ = 0;
			join = worker_.joinable();
		}
		if (join) {
			(void)device_.Cancel();
			worker_.join();
		}
		std::lock_guard<std::mutex> lock(mutex_);
		if (opened_) (void)device_.Close();
		opened_ = false;
		writer_ = {};
	}

	InputDevice& device_;
	Spi& spi_;
	Clock& clock_;
	std::uint32_t delivery_timeout_ms_;
	std::mutex mutex_;
	std::thread worker_;
	InputRecipe recipe_;
	ButtonWriter writer_;
	std::function<void(std::uint64_t, Error)> on_fault_;
	bool opened_ = false;
	bool accepting_ = false;
	bool core_addressable_ = false;
	bool fault_sent_ = false;
	std::uint64_t active_generation_ = 0;
	std::uint64_t last_generation_ = 0;
	std::uint16_t digital_map_ = 0;
	std::uint16_t committed_map_ = 0;
	std::int32_t horizontal_ = 0;
	std::int32_t vertical_ = 0;
};

NativeInputSession::NativeInputSession(InputDevice& device, Spi& spi,
	Clock& clock, std::uint32_t delivery_timeout_ms)
	: impl_(new Impl(device, spi, clock, delivery_timeout_ms)) {}

NativeInputSession::~NativeInputSession() = default;

Error NativeInputSession::Open(const InputDeviceIdentity& identity,
	const InputRecipe& recipe, std::uint64_t deadline, ButtonWriter writer)
{
	return impl_->Open(identity, recipe, deadline, std::move(writer));
}

Error NativeInputSession::Start(std::uint64_t generation,
	std::function<void(std::uint64_t, Error)> on_fault)
{
	return impl_->Start(generation, std::move(on_fault));
}

Error NativeInputSession::Neutralize(std::uint64_t deadline)
{
	return impl_->Neutralize(deadline);
}

Error NativeInputSession::Stop(std::uint64_t deadline)
{
	return impl_->Stop(deadline);
}

} // namespace native
} // namespace mister
