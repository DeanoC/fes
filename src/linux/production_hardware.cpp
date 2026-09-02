// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "linux/production_hardware.hpp"

#include "native/artifacts.hpp"
#include "native/core_loader.hpp"
#include "native/hardware.hpp"
#include "native/input.hpp"
#include "native/linux/fpga_manager.hpp"
#include "native/linux/i2c.hpp"
#include "native/linux/input.hpp"
#include "native/linux/mmio.hpp"
#include "native/linux/spi.hpp"
#include "native/video.hpp"
#include "native/video_recipe.hpp"

#include <cstdlib>
#include <time.h>
#include <utility>

#ifndef MISTER_RUNTIME_IDLE_RBF
#define MISTER_RUNTIME_IDLE_RBF "/usr/share/mister-runtime/idle.rbf"
#endif

namespace mister {
namespace {

Profiles BuildProductionProfiles()
{
	Profile profile;
	profile.system = "megadrive";
	profile.expected_core = "MegaDrive";
	profile.rbf = "/usr/share/mister-runtime/cores/megadrive.rbf";
	profile.media.push_back({"cartridge", 1, true, {".md", ".gen", ".bin"},
		32u * 1024u * 1024u});
	profile.core = {0x0001, 0x0001, 0x0000,
		FileWireFormat::little_endian_byte_pairs};
	profile.input = {1, 0x02, 0x0008, 0x0004, 0x0002, 0x0001,
		0x0010, 0x0020, 0x0040, 0x0080};
	Profiles profiles;
	if (!profiles.Add(std::move(profile)).ok()) std::abort();
	return profiles;
}

class UnavailableHardware final : public Hardware {
public:
	explicit UnavailableHardware(Error reason) : reason_(std::move(reason))
	{
		if (reason_.code != ErrorCode::io_failed)
			reason_ = {ErrorCode::io_failed,
				reason_.message.empty() ? "production hardware unavailable" : reason_.message};
	}
	void SetFaultSink(HardwareFaultSink*) override {}
	HardwareResult LoadIdle() override { return {reason_, false, ""}; }
	HardwareResult Launch(const PreparedLaunch&, std::uint64_t) override
	{
		return {reason_, false, ""};
	}
	HardwareResult LoadDevelopmentRBF(const std::string&) override
	{
		return {reason_, false, ""};
	}

private:
	Error reason_;
};

class SteadyClock final : public native::Clock {
public:
	std::uint64_t NowMs() const override
	{
		struct timespec stamp = {};
		if (clock_gettime(CLOCK_MONOTONIC, &stamp) != 0) std::abort();
		return static_cast<std::uint64_t>(stamp.tv_sec) * 1000u +
			static_cast<std::uint64_t>(stamp.tv_nsec) / 1000000u;
	}
};

class ProductionHardware final : public Hardware {
public:
	explicit ProductionHardware(LogSink& log)
		: opener_(), mmio_(), clock_(), fpga_(mmio_, clock_),
		  spi_(mmio_, clock_), core_(spi_), i2c_(clock_),
		  idle_video_(core_, spi_, i2c_, clock_, log,
			  native::Menu720p60Recipe()),
		  game_video_(spi_, i2c_, clock_, log, native::Menu720p60Recipe()),
		  input_device_(clock_), timeouts_(),
		  input_session_(input_device_, spi_, clock_, timeouts_.core_io_ms),
		  hardware_(opener_, fpga_, core_, idle_video_, game_video_,
			  input_session_, native::FogCastGamepadIdentity(), clock_, log,
			  MISTER_RUNTIME_IDLE_RBF, timeouts_) {}

	void SetFaultSink(HardwareFaultSink* sink) override
	{
		hardware_.SetFaultSink(sink);
	}
	HardwareResult LoadIdle() override { return hardware_.LoadIdle(); }
	HardwareResult Launch(const PreparedLaunch& launch,
		std::uint64_t generation) override
	{
		return hardware_.Launch(launch, generation);
	}
	HardwareResult LoadDevelopmentRBF(const std::string& path) override
	{
		return hardware_.LoadDevelopmentRBF(path);
	}

private:
	native::PosixArtifactOpener opener_;
	native::LinuxMmio mmio_;
	SteadyClock clock_;
	native::LinuxFpgaManager fpga_;
	native::LinuxSpi spi_;
	native::CoreLoader core_;
	native::LinuxI2c i2c_;
	native::MenuVideoBringup idle_video_;
	native::FixedVideoBringup game_video_;
	native::LinuxInput input_device_;
	native::NativeTimeouts timeouts_;
	native::NativeInputSession input_session_;
	native::NativeHardware hardware_;
};

} // namespace

const Profiles& ProductionProfiles()
{
	static const Profiles profiles = BuildProductionProfiles();
	return profiles;
}

Error CreateProductionHardware(LogSink& log,
	std::unique_ptr<Hardware>* hardware)
{
	if (hardware == nullptr)
		return {ErrorCode::invalid_request, "missing production hardware output"};
	hardware->reset(new ProductionHardware(log));
	return {};
}

std::unique_ptr<Hardware> CreateUnavailableHardware(const Error& reason)
{
	return std::make_unique<UnavailableHardware>(reason);
}

} // namespace mister
