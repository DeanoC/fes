// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "linux/production_hardware.hpp"

#include "native/artifacts.hpp"
#include "native/core_loader.hpp"
#include "native/hardware.hpp"
#include "native/linux/fpga_manager.hpp"
#include "native/linux/mmio.hpp"
#include "native/linux/spi.hpp"

#include <time.h>
#include <utility>

#ifndef MISTER_RUNTIME_IDLE_RBF
#define MISTER_RUNTIME_IDLE_RBF "/usr/share/mister-runtime/idle.rbf"
#endif

namespace mister {
namespace {

class UnavailableHardware final : public Hardware {
public:
	explicit UnavailableHardware(Error reason) : reason_(std::move(reason))
	{
		if (reason_.code != ErrorCode::io_failed)
			reason_ = {ErrorCode::io_failed,
				reason_.message.empty() ? "production hardware unavailable" : reason_.message};
	}
	HardwareResult LoadIdle() override { return {reason_, false, ""}; }
	HardwareResult Launch(const PreparedLaunch&) override
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
		if (clock_gettime(CLOCK_MONOTONIC, &stamp) != 0) return 0;
		return static_cast<std::uint64_t>(stamp.tv_sec) * 1000u +
			static_cast<std::uint64_t>(stamp.tv_nsec) / 1000000u;
	}
};

class ProductionHardware final : public Hardware {
public:
	explicit ProductionHardware(LogSink& log)
		: opener_(), mmio_(), clock_(), fpga_(mmio_, clock_),
		  spi_(mmio_, clock_), core_(spi_),
		  hardware_(opener_, fpga_, core_, clock_, log,
			  MISTER_RUNTIME_IDLE_RBF, {}) {}

	HardwareResult LoadIdle() override { return hardware_.LoadIdle(); }
	HardwareResult Launch(const PreparedLaunch& launch) override
	{
		return hardware_.Launch(launch);
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
	native::NativeHardware hardware_;
};

} // namespace

const Profiles& ProductionProfiles()
{
	static const Profiles profiles;
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
