// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"
#include "native/core_driver.hpp"

#include <cstdint>
#include <mutex>
#include <vector>

namespace mister {
namespace native {

class Clock;
class Mmio;

class FesGp final {
public:
	FesGp(Mmio&, Clock&);
	void BeginSession();
	Error Exchange(std::uint8_t opcode, std::uint8_t index,
		std::uint16_t argument, std::uint64_t absolute_deadline_ms,
		std::uint16_t* response);
	Error Identify(const CoreDescriptor&, std::uint64_t absolute_deadline_ms,
		bool* safe_to_quiesce = nullptr);

private:
	Mmio& mmio_;
	Clock& clock_;
	std::mutex mutex_;
	bool request_toggle_ = false;
	bool poisoned_ = false;
};

class FesGpCoreDriver final : public CoreDriver {
public:
	explicit FesGpCoreDriver(FesGp&);
	void BeginSession() override;
	Error CaptureData(
		const CoreDriverContext&, std::uint64_t, std::vector<std::uint16_t>*) override;
	Error RestoreData(
		const CoreDriverContext&, const std::vector<std::uint16_t>&, std::uint64_t) override;
	Error ResumeData(const CoreDriverContext&, std::uint64_t) override;
	CoreDriverResult Quiesce(const CoreDriverContext&, std::uint64_t) override;
	CoreDriverResult Identify(const CoreDriverContext&, std::uint64_t) override;
	CoreDriverResult NeutralizeButtons(const CoreDriverContext&, std::uint64_t) override;
	CoreDriverResult SetButtons(const CoreDriverContext&, std::uint16_t,
		std::uint64_t) override;
	CoreDriverResult Start(const CoreDriverContext&, std::uint64_t) override;
	Error SetKeyboardMatrix(std::uint64_t matrix, std::uint64_t deadline);
	Error LoadMedia(const std::vector<std::uint8_t>& bytes, std::uint64_t deadline);

private:
	CoreDriverResult Gameplay(std::uint16_t, std::uint64_t);
	CoreDriverResult NeutralizeKeyboard(std::uint64_t deadline);
	Error DataControl(std::uint16_t, std::uint64_t);
	FesGp& gp_;
	bool persistence_verified_ = false;
	bool reset_held_ = true;
	bool freeze_attempted_ = false;
	bool computer_ = false;
};

} // namespace native
} // namespace mister
