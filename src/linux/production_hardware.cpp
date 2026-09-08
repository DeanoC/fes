// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "linux/production_hardware.hpp"

#include "native/artifacts.hpp"
#include "native/core_loader.hpp"
#include "native/generated/megadrive.hpp"
#include "native/generated/nes.hpp"
#include "native/generated/pong.hpp"
#include "native/generated/snes.hpp"
#include "native/hardware.hpp"
#include "native/input.hpp"
#include "native/linux/fpga_manager.hpp"
#include "native/linux/framebuffer.hpp"
#include "native/linux/i2c.hpp"
#include "native/linux/input.hpp"
#include "native/linux/mmio.hpp"
#include "native/linux/spi.hpp"
#include "native/video.hpp"
#include "native/video_recipe.hpp"

#include <cstddef>
#include <cstring>
#include <cstdlib>
#include <string>
#include <time.h>
#include <utility>

#ifndef MISTER_RUNTIME_IDLE_RBF
#define MISTER_RUNTIME_IDLE_RBF "/usr/share/mister-runtime/idle.rbf"
#endif

#ifndef MISTER_RUNTIME_CORES_DIR
#define MISTER_RUNTIME_CORES_DIR "/usr/share/mister-runtime/cores"
#endif

namespace mister {
namespace {

Profile ProfileFromGenerated(const native::generated::GeneratedSystem& sys)
{
	FileWireFormat file_wire;
	if (std::strcmp(sys.core.file_wire, "little_endian_byte_pairs") == 0)
		file_wire = FileWireFormat::little_endian_byte_pairs;
	else if (std::strcmp(sys.core.file_wire, "little_endian_bytes") == 0)
		file_wire = FileWireFormat::little_endian_bytes;
	else
		std::abort();
	Profile profile;
	profile.system = sys.system;
	profile.expected_core = sys.expected_core;
	profile.rbf = std::string(MISTER_RUNTIME_CORES_DIR) + "/" + sys.rbf_artifact;
	for (std::size_t i = 0; i < sys.media_count; ++i) {
		const native::generated::GeneratedMediaRule& rule = sys.media[i];
		MediaRule media;
		media.role = rule.role;
		media.index = rule.index;
		media.required = rule.required;
		media.maximum_size = rule.maximum_size;
		if (std::strcmp(rule.transform, "raw") == 0) media.transform = MediaTransform::raw;
		else if (std::strcmp(rule.transform, "snes_cartridge") == 0) media.transform = MediaTransform::snes_cartridge;
		else if (std::strcmp(rule.transform, "nes_cartridge") == 0) media.transform = MediaTransform::nes_cartridge;
		else std::abort();
		for (std::size_t j = 0; j < rule.extension_count; ++j)
			media.extensions.push_back(rule.extensions[j]);
		profile.media.push_back(media);
	}
	profile.core.reset_assert_word = sys.core.reset_assert_word;
	profile.core.initial_status_word = sys.core.initial_status_word;
	profile.core.reset_release_word = sys.core.reset_release_word;
	profile.core.file_wire = file_wire;
	profile.input.player_count = sys.input.player_count;
	profile.input.player_command = sys.input.player_command;
	profile.input.up = sys.input.up;
	profile.input.down = sys.input.down;
	profile.input.left = sys.input.left;
	profile.input.right = sys.input.right;
	profile.input.a = sys.input.a;
	profile.input.b = sys.input.b;
	profile.input.c = sys.input.c;
	profile.input.start = sys.input.start;
	profile.input.x = sys.input.x;
	profile.input.y = sys.input.y;
	profile.input.l = sys.input.l;
	profile.input.r = sys.input.r;
	profile.input.select = sys.input.select;

	return profile;
}

Profiles BuildProductionProfiles()
{
	Profiles profiles;
	if (!profiles.Add(ProfileFromGenerated(native::generated::kMegaDrive)).ok())
		std::abort();
	if (!profiles.Add(ProfileFromGenerated(native::generated::kPong)).ok())
		std::abort();
	if (!profiles.Add(ProfileFromGenerated(native::generated::kSNES)).ok())
		std::abort();
	if (!profiles.Add(ProfileFromGenerated(native::generated::kNES)).ok())
		std::abort();
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
		  spi_(mmio_, clock_), core_(spi_), i2c_(clock_), framebuffer_(clock_),
		  idle_video_(core_, spi_, i2c_, framebuffer_, clock_, log,
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
	Error FlushSave() override { return hardware_.FlushSave(); }
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
	native::LinuxFramebuffer framebuffer_;
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
