// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/video.hpp"

#include "native/core_loader.hpp"
#include "native/framebuffer.hpp"
#include "native/hardware.hpp"
#include "native/linux/i2c.hpp"
#include "native/linux/spi.hpp"
#include "native/adv7513.hpp"
#include "native/video_recipe.hpp"

#include <cstdint>
#include <string>
#include <vector>

namespace mister {
namespace native {
namespace {

const std::vector<std::uint16_t> kAssertedStatus = {
	0x001e, 0x0001, 0x0000, 0x0000, 0x0000,
	0x0000, 0x0000, 0x0000, 0x0000,
};

const std::vector<std::uint16_t> kReleasedStatus = {
	0x001e, 0x0000, 0x0000, 0x0000, 0x0000,
	0x0000, 0x0000, 0x0000, 0x0000,
};

// ADV7513's EDID request edge wakes the fixed-mode transmitter on a cold
// native start.  The request is deliberately not read back here: the native
// path has no need to select the EDID sub-map, and the transmitter's IRQ
// response is not part of fixed-mode bring-up.
const std::vector<RegisterWrite> kHdmiWake = adv7513::HdmiWake();

const std::vector<std::uint16_t> kNeutralButtons = {0x0001, 0x0000};

std::string HexByte(std::uint8_t value)
{
	static const char digits[] = "0123456789abcdef";
	std::string output = "0x00";
	output[2] = digits[(value >> 4) & 0x0f];
	output[3] = digits[value & 0x0f];
	return output;
}

void WritePhase(LogSink& log, const char* phase, const VideoResult& result,
	const Error& error)
{
	log.Write({"start", "", result.observed_core, phase, error});
}

void PhaseSuccess(const char* phase, VideoResult* result, LogSink& log)
{
	result->phase = phase;
	result->error = {};
	WritePhase(log, phase, *result, result->error);
}

Error InitializeAdv(I2c& i2c, const VideoRecipe& recipe,
	std::uint64_t deadline, VideoResult* result, bool custom = false)
{
	Error error = i2c.SelectFirst(adv7513::kMainMapAddress7Bit, adv7513::reg::kPower,
		deadline, &result->selected_bus, &result->power_before);
	if (!error.ok()) return error;
	for (const RegisterWrite& write : recipe.adv_initialization) {
		const auto value = custom && write.address == adv7513::reg::kPacketEnable1 ?
			adv7513::kPacketsVideoOnly : write.value;
		error = i2c.WriteByte(write.address, value, deadline);
		if (!error.ok()) return error;
	}
	return i2c.ReadByte(adv7513::reg::kPower, &result->power_after, deadline);
}

VideoQuiesceResult QuiesceAdv(I2c& i2c, Clock& clock,
	std::uint64_t deadline)
{
	if (clock.NowMs() >= deadline)
		return {{ErrorCode::io_failed, "deadline exceeded"}, false};
	std::string selected_bus;
	std::uint8_t power = 0;
	Error error = i2c.SelectFirst(adv7513::kMainMapAddress7Bit,
		adv7513::reg::kPower, deadline, &selected_bus, &power);
	if (!error.ok()) return {error, false};
	// Present a clean link loss before the FPGA changes its pixel stream. The
	// ADV7513 guide documents main power-down for this transition; preserve the
	// other power-register bits exactly as its read-modify-write rule requires.
	error = i2c.WriteByte(adv7513::reg::kPower,
		static_cast<std::uint8_t>(power | adv7513::kPowerPowerDown), deadline);
	return {error, true};
}

Error ApplyMode(Spi& spi, I2c& i2c, const VideoRecipe& recipe,
	std::uint64_t deadline)
{
	Error error = spi.Exchange(kUserIoTarget, recipe.timing_words, nullptr,
		deadline);
	if (!error.ok()) return error;
	for (const RegisterWrite& write : recipe.adv_mode) {
		error = i2c.WriteByte(write.address, write.value, deadline);
		if (!error.ok()) return error;
	}
	return {};
}

Error ApplyFixedMode(I2c& i2c, const VideoRecipe& recipe,
	std::uint64_t deadline)
{
	for (const RegisterWrite& write : recipe.adv_mode) {
		const Error error = i2c.WriteByte(write.address, write.value, deadline);
		if (!error.ok()) return error;
	}
	return {};
}

Error WakeAdv(I2c& i2c, std::uint64_t deadline)
{
	for (const RegisterWrite& write : kHdmiWake) {
		const Error error = i2c.WriteByte(write.address, write.value, deadline);
		if (!error.ok()) return error;
	}
	return {};
}

Error RequireLink(I2c& i2c, Clock& clock, std::uint64_t deadline,
	VideoResult* result)
{
	do {
		if (clock.NowMs() >= deadline)
			return {ErrorCode::io_failed, "deadline exceeded"};
		const Error error = i2c.ReadByte(adv7513::reg::kStatus,
			&result->link_status, deadline);
		if (!error.ok()) return error;
	} while ((result->link_status & adv7513::kStatusLinkReady) !=
		adv7513::kStatusLinkReady);
	return {};
}

void CompleteVideo(const VideoRecipe& recipe, VideoResult* result, LogSink& log)
{
	result->phase = "hdmi_verify";
	result->error = {ErrorCode::none,
		std::string("recipe=") + recipe.identity +
		" bus=" + result->selected_bus +
		" address=" + HexByte(adv7513::kMainMapAddress7Bit) +
		" power_before=" + HexByte(result->power_before) +
		" power_after=" + HexByte(result->power_after) +
		" link_status=" + HexByte(result->link_status)};
	WritePhase(log, "hdmi_verify", *result, result->error);
}

} // namespace

FixedVideoBringup::FixedVideoBringup(Spi& spi, I2c& i2c, Clock& clock,
	LogSink& log, const VideoRecipe& recipe)
	: spi_(spi), i2c_(i2c), clock_(clock), log_(log), recipe_(recipe) {}

VideoQuiesceResult FixedVideoBringup::Quiesce(std::uint64_t deadline)
{
	return QuiesceAdv(i2c_, clock_, deadline);
}

VideoResult FixedVideoBringup::PhaseFailure(const char* phase,
	const Error& cause, const VideoResult& partial) const
{
	VideoResult result = partial;
	result.phase = phase;
	result.error.code = ErrorCode::io_failed;
	result.error.message = cause.message.empty() ?
		std::string(phase) + " failed" : cause.message;
	WritePhase(log_, phase, result, result.error);
	return result;
}

VideoResult FixedVideoBringup::BringUp(std::uint64_t deadline)
{
	VideoResult result;
	if (clock_.NowMs() >= deadline)
		return PhaseFailure("hdmi_init",
			{ErrorCode::io_failed, "deadline exceeded"}, result);

	Error error = InitializeAdv(i2c_, recipe_, deadline, &result);
	if (!error.ok()) return PhaseFailure("hdmi_init", error, result);
	PhaseSuccess("hdmi_init", &result, log_);

	error = ApplyMode(spi_, i2c_, recipe_, deadline);
	if (!error.ok()) return PhaseFailure("video_timing", error, result);
	PhaseSuccess("video_timing", &result, log_);

	error = WakeAdv(i2c_, deadline);
	if (!error.ok()) return PhaseFailure("hdmi_wake", error, result);
	PhaseSuccess("hdmi_wake", &result, log_);

	error = spi_.Exchange(kUserIoTarget, kNeutralButtons, nullptr, deadline);
	if (!error.ok()) return PhaseFailure("core_input", error, result);
	PhaseSuccess("core_input", &result, log_);

	error = RequireLink(i2c_, clock_, deadline, &result);
	if (!error.ok()) return PhaseFailure("hdmi_verify", error, result);
	// MiSTer sys_top initializes attenuation to 0x1f (mute). Games use
	// zero attenuation; newly programmed menu cores retain their muted default.
	error = spi_.Exchange(kUserIoTarget, {0x0026, 0x0000}, nullptr, deadline);
	if (!error.ok()) return PhaseFailure("audio_volume", error, result);
	PhaseSuccess("audio_volume", &result, log_);

	CompleteVideo(recipe_, &result, log_);
	return result;
}

VideoResult FixedVideoBringup::BringUpCustom(std::uint64_t deadline, bool audio)
{
	VideoResult result;
	if (clock_.NowMs() >= deadline)
		return PhaseFailure("hdmi_init",
			{ErrorCode::io_failed, "deadline exceeded"}, result);
	Error error = InitializeAdv(i2c_, recipe_, deadline, &result, true);
	if (!error.ok()) return PhaseFailure("hdmi_init", error, result);
	PhaseSuccess("hdmi_init", &result, log_);
	if (audio) {
		for (const auto& write : adv7513::ApplicationAudio48k()) {
			error = i2c_.WriteByte(write.address, write.value, deadline);
			if (!error.ok()) return PhaseFailure("audio_setup", error, result);
		}
		PhaseSuccess("audio_setup", &result, log_);
	}
	error = ApplyFixedMode(i2c_, recipe_, deadline);
	if (!error.ok()) return PhaseFailure("video_timing", error, result);
	PhaseSuccess("video_timing", &result, log_);
	error = WakeAdv(i2c_, deadline);
	if (!error.ok()) return PhaseFailure("hdmi_wake", error, result);
	PhaseSuccess("hdmi_wake", &result, log_);
	error = RequireLink(i2c_, clock_, deadline, &result);
	if (!error.ok()) return PhaseFailure("hdmi_verify", error, result);
	if (audio) {
		// The identified core remains held and supplies zero samples until Start.
		error = i2c_.WriteByte(adv7513::reg::kPacketEnable1,
			adv7513::kPacketsVideoAudio, deadline);
		if (!error.ok()) return PhaseFailure("audio_enable", error, result);
		PhaseSuccess("audio_enable", &result, log_);
	}
	CompleteVideo(recipe_, &result, log_);
	return result;
}

MenuVideoBringup::MenuVideoBringup(CoreLoader& core, Spi& spi, I2c& i2c,
	Framebuffer& framebuffer, Clock& clock, LogSink& log, const VideoRecipe& recipe)
	: core_(core), framebuffer_(framebuffer), spi_(spi), i2c_(i2c), clock_(clock), log_(log),
	  recipe_(recipe) {}

VideoQuiesceResult MenuVideoBringup::Quiesce(std::uint64_t deadline)
{
	return QuiesceAdv(i2c_, clock_, deadline);
}

VideoResult MenuVideoBringup::PhaseFailure(const char* phase,
	const Error& cause, const VideoResult& partial) const
{
	VideoResult result = partial;
	result.phase = phase;
	result.error.code = ErrorCode::io_failed;
	result.error.message = cause.message.empty() ?
		std::string(phase) + " failed" : cause.message;
	WritePhase(log_, phase, result, result.error);
	return result;
}

VideoResult MenuVideoBringup::BringUp(const std::string& expected_core,
	std::uint64_t deadline)
{
	IdleRecipe idle = TransitionalMenuIdle();
	idle.expected_core = expected_core;
	return BringUp(idle, deadline);
}

VideoResult MenuVideoBringup::BringUp(const IdleRecipe& idle,
	std::uint64_t deadline)
{
	VideoResult result;
	const bool mister_user_io = IdleUsesMisterUserIo(idle);
	if (clock_.NowMs() >= deadline)
		return PhaseFailure(mister_user_io ? "core_reset" : "hdmi_init",
			{ErrorCode::io_failed, "deadline exceeded"}, result);

	Error error;
	if (mister_user_io) {
		error = spi_.SynchronizeCore(deadline);
		if (!error.ok()) return PhaseFailure("core_sync", error, result);
		PhaseSuccess("core_sync", &result, log_);

		error = spi_.Exchange(kUserIoTarget, kAssertedStatus, nullptr, deadline);
		if (!error.ok()) return PhaseFailure("core_reset", error, result);
		PhaseSuccess("core_reset", &result, log_);

		if (idle.probe_core) {
			error = core_.Probe(&result.observed_core, deadline);
			if (!error.ok()) return PhaseFailure("core_probe", error, result);
			if (!idle.expected_core.empty() &&
				result.observed_core != idle.expected_core)
				return PhaseFailure("core_probe",
					{ErrorCode::io_failed, "unexpected idle core"}, result);
			PhaseSuccess("core_probe", &result, log_);
		}
	}

	error = InitializeAdv(i2c_, recipe_, deadline, &result);
	if (!error.ok()) return PhaseFailure("hdmi_init", error, result);
	PhaseSuccess("hdmi_init", &result, log_);

	if (mister_user_io)
		error = ApplyMode(spi_, i2c_, recipe_, deadline);
	else
		error = ApplyFixedMode(i2c_, recipe_, deadline);
	if (!error.ok()) return PhaseFailure("video_timing", error, result);
	PhaseSuccess("video_timing", &result, log_);

	if (mister_user_io) {
		error = spi_.Exchange(kUserIoTarget, kReleasedStatus, nullptr, deadline);
		if (!error.ok()) return PhaseFailure("core_release", error, result);
		PhaseSuccess("core_release", &result, log_);
	}

	error = WakeAdv(i2c_, deadline);
	if (!error.ok()) return PhaseFailure("hdmi_wake", error, result);
	PhaseSuccess("hdmi_wake", &result, log_);

	if (mister_user_io) {
		error = spi_.Exchange(kUserIoTarget, kNeutralButtons, nullptr, deadline);
		if (!error.ok()) return PhaseFailure("core_input", error, result);
		PhaseSuccess("core_input", &result, log_);
	}

	if (mister_user_io && idle.enable_hps_framebuffer) {
		FramebufferMode mode;
		error = framebuffer_.Prepare(deadline, &mode);
		if (error.ok()) error = ValidateMenuFramebuffer(mode);
		if (!error.ok()) return PhaseFailure("framebuffer", error, result);
		// Main's Linux framebuffer is at reserved DDR + one metadata page. RxB
		// selects the driver's 32-bit little-endian RGB layout. Scale to fixed HDMI.
		std::vector<std::uint16_t> response;
		error = spi_.Exchange(kUserIoTarget, {0x002f, 0x8016,
			static_cast<std::uint16_t>(mode.address),
			static_cast<std::uint16_t>(mode.address >> 16),
			static_cast<std::uint16_t>(mode.width), static_cast<std::uint16_t>(mode.height),
			0, 1279, 0, 719, static_cast<std::uint16_t>(mode.stride)}, &response, deadline);
		if (!error.ok()) return PhaseFailure("framebuffer", error, result);
		if (response.empty() || response[0] == 0)
			return PhaseFailure("framebuffer", {ErrorCode::io_failed,
				"idle core does not support HPS framebuffer"}, result);
		// Menu remains in the already released status0. Main's status helper
		// shifts and masks its framebuffer argument, leaving bits[8:5] zero.
		PhaseSuccess("framebuffer", &result, log_);
	}

	error = RequireLink(i2c_, clock_, deadline, &result);
	if (!error.ok()) return PhaseFailure("hdmi_verify", error, result);
	CompleteVideo(recipe_, &result, log_);
	return result;
}

} // namespace native
} // namespace mister
