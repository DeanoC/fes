// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "native/core_driver.hpp"
#include "native/de25_computer.hpp"
#include "native/video.hpp"
namespace mister { namespace native {
// HDMI configuration belongs to this FPGA image. Linux observes readiness;
// it never writes the DE10 I2C recipe. Core quiesce remains the driver's HOLD.
class De25VideoBringup final : public CoreVideoBringup, public VideoBringup {
public:
 bool SupportsPlatform(NativePlatform p) const override { return p == NativePlatform::de25_nano; }
 VideoResult BringUpCustom(std::uint64_t deadline, bool audio = false) override;
 VideoResult BringUpSelected(CoreDriver&, std::uint64_t, bool audio = false) override;
 VideoResult BringUp(const IdleRecipe&, std::uint64_t) override;
 VideoQuiesceResult Quiesce(std::uint64_t) override { return {}; }
};

// Internal driver for the known BRAM ZX81 contract. The production factory
// does not construct it yet. Its caller serializes lifecycle/control calls
// and owns the exclusive protocol client.
class De25ComputerCoreDriver final : public CoreDriver {
public:
	explicit De25ComputerCoreDriver(De25Computer& client) : client_(client) {}
	void BeginSession() override;
	Error WaitVideoReady(std::uint64_t deadline) override;
	CoreDriverResult Identify(const CoreDriverContext&, std::uint64_t) override;
	CoreDriverResult Quiesce(const CoreDriverContext&, std::uint64_t) override;
	CoreDriverResult NeutralizeButtons(const CoreDriverContext&, std::uint64_t) override;
	CoreDriverResult SetButtons(const CoreDriverContext&, std::uint16_t, std::uint64_t) override;
	CoreDriverResult Start(const CoreDriverContext&, std::uint64_t) override;
	Error SetKeyboardHid(const KeyboardHidRows&, std::uint64_t) override;
	Error LoadMedia(const std::vector<std::uint8_t>&, std::uint64_t) override;
	Error LoadMediaLive(const std::vector<std::uint8_t>&, std::uint64_t) override;
	Error ClearMedia(std::uint64_t) override;
	const std::vector<MediaUnitCapability>& media_units() const override { return media_; }
	Error InsertMedia(std::uint8_t, const ComputerMediaSnapshot&, Clock&,
		std::uint64_t, std::uint32_t) override;
	Error EjectMedia(std::uint8_t, std::uint64_t) override;
private:
	Error Ready() const;
	Error Context(const CoreDriverContext&) const;
	Error RecordTransfer(Error, MediaUnitState);
	De25Computer& client_;
	std::vector<MediaUnitCapability> media_;
	bool selected_ = false;
	std::string core_, build_;
};

class LinuxDe25Menu;
// The application has no guest CPU or GP mailbox. Identify binds the selected
// allocation; Configure performs the two observed black frames before Start.
class De25MenuCoreDriver final : public CoreDriver {
public:
	explicit De25MenuCoreDriver(LinuxDe25Menu& menu) : menu_(menu) {}
	void BeginSession() override;
	Error WaitVideoReady(std::uint64_t) override;
	CoreDriverResult Identify(const CoreDriverContext&, std::uint64_t) override;
	CoreDriverResult Quiesce(const CoreDriverContext&, std::uint64_t) override;
	CoreDriverResult NeutralizeButtons(const CoreDriverContext&, std::uint64_t) override;
	CoreDriverResult SetButtons(const CoreDriverContext&, std::uint16_t, std::uint64_t) override;
	CoreDriverResult Start(const CoreDriverContext&, std::uint64_t) override;
private:
	Error Context(const CoreDriverContext&) const;
	LinuxDe25Menu& menu_;
	bool selected_ = false;
	std::string build_;
};
} } // namespace mister::native
