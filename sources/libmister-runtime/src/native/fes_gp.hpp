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
class ComputerMediaSnapshot;

struct MediaStreamInfo {
	std::uint32_t minimum = 0;
	std::uint32_t maximum = 0;
	std::uint16_t chunk_bytes = 0;
};

class FesGp final {
public:
	FesGp(Mmio&, Clock&);
	void BeginSession();
	Error Exchange(std::uint8_t opcode, std::uint8_t index,
		std::uint16_t argument, std::uint64_t absolute_deadline_ms,
		std::uint16_t* response);
	Error Identify(const CoreDescriptor&, std::uint64_t absolute_deadline_ms,
		bool* safe_to_quiesce = nullptr, std::uint16_t* observed_capabilities = nullptr);

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
	Error SetController(std::uint8_t port, std::uint16_t buttons,
		std::uint16_t keypad, std::uint64_t deadline);
	// Launch-time primary bind: Quiesce (hold reset) → begin/data/commit → release.
	Error LoadMedia(const std::vector<std::uint8_t>& bytes, std::uint64_t deadline);
	// Mid-session replace: begin/data/commit while execution stays released.
	Error LoadMediaLive(const std::vector<std::uint8_t>& bytes, std::uint64_t deadline);
	// Mid-session eject: media begin with eject index and argument 0.
	Error ClearMedia(std::uint64_t deadline);
	Error LoadFirmware(const std::vector<std::uint8_t>& bytes, std::uint64_t deadline);
	Error StreamInfo(MediaStreamInfo*) const;
	Error LoadMediaStream(const ComputerMediaSnapshot&, Clock&, std::uint64_t deadline);
	Error AbortMediaStream(std::uint64_t deadline);
	std::uint16_t observed_capabilities() const { return observed_capabilities_; }

private:
	CoreDriverResult Gameplay(std::uint16_t, std::uint64_t);
	CoreDriverResult NeutralizeKeyboard(std::uint64_t deadline);
	Error NeutralizeControllers(std::uint64_t deadline);
	Error DataControl(std::uint16_t, std::uint64_t);
	Error StreamCommand(std::uint8_t opcode, std::uint8_t index,
		std::uint16_t argument, std::uint64_t deadline);
	Error TransferMediaBlob(const std::vector<std::uint8_t>& bytes,
		std::uint64_t deadline, bool hold_reset);
	Error MediaBusyOrIo(const Error& error) const;
	FesGp& gp_;
	bool persistence_verified_ = false;
	bool reset_held_ = true;
	bool freeze_attempted_ = false;
	bool computer_ = false;
	bool media_ = false;
	bool firmware_ = false;
	bool application_ = false;
	bool gamepad_ = false;
	bool controller_ports_ = false;
	bool keypad_ports_ = false;
	std::uint16_t observed_capabilities_ = 0;
	MediaStreamInfo stream_info_;
	bool stream_verified_ = false;
	bool stream_pending_ = false;
};

} // namespace native
} // namespace mister
