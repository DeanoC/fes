// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/hardware.hpp"

#include "native/artifacts.hpp"
#include "native/core_loader.hpp"
#include "native/input.hpp"
#include "native/video.hpp"

#include <algorithm>
#include <limits>
#include <utility>

namespace mister {
namespace native {
namespace {

std::uint64_t Deadline(Clock& clock, std::uint32_t duration)
{
	const std::uint64_t now = clock.NowMs();
	if (duration > std::numeric_limits<std::uint64_t>::max() - now)
		return std::numeric_limits<std::uint64_t>::max();
	return now + duration;
}

Error ProgramError(const Error& error)
{
	return {ErrorCode::program_failed,
		error.message.empty() ? "FPGA programming failed" : error.message};
}

Error CoreIoError(const Error& error)
{
	return {ErrorCode::io_failed,
		error.message.empty() ? "core I/O failed" : error.message};
}

} // namespace

NativeHardware::NativeHardware(ArtifactOpener& opener, FpgaManager& fpga,
	CoreLoader& core, VideoBringup& idle_video, FixedVideoBringup& game_video,
	InputSession& input, const InputDeviceIdentity& input_identity, Clock& clock,
	LogSink& log, std::string idle_rbf, NativeTimeouts timeouts)
	: opener_(opener), fpga_(fpga), core_(core), idle_video_(idle_video),
	  game_video_(game_video), input_(input), input_identity_(input_identity),
	  clock_(clock), log_(log), idle_rbf_(std::move(idle_rbf)),
	  timeouts_(timeouts), fault_sink_mutex_(), fault_sink_(nullptr),
	  input_open_(false) {}

NativeHardware::~NativeHardware()
{
	SetFaultSink(nullptr);
	if (input_open_) (void)StopInput(Deadline(clock_, timeouts_.core_io_ms));
}

void NativeHardware::SetFaultSink(HardwareFaultSink* sink)
{
	std::lock_guard<std::mutex> lock(fault_sink_mutex_);
	fault_sink_ = sink;
}

void NativeHardware::ForwardInputFault(std::uint64_t generation, Error error)
{
	std::lock_guard<std::mutex> lock(fault_sink_mutex_);
	if (fault_sink_ != nullptr)
		fault_sink_->ReportHardwareFault({generation, std::move(error)});
}

Error NativeHardware::StopInput(std::uint64_t deadline)
{
	if (!input_open_) return {};
	const Error error = input_.Stop(deadline);
	input_open_ = false;
	return error;
}

HardwareResult NativeHardware::LoadIdle()
{
	const Error input_error = StopInput(Deadline(clock_, timeouts_.core_io_ms));
	Artifact artifact;
	Error error = OpenRBFArtifact(idle_rbf_, opener_, &artifact);
	log_.Write({"start", "", "", "preflight", error});
	if (!error.ok()) return {input_error.ok() ? error : input_error, false, ""};
	const VideoQuiesceResult quiesced = idle_video_.Quiesce(
		Deadline(clock_, timeouts_.video_ms));
	error = quiesced.error;
	log_.Write({"start", "", "", "hdmi_quiesce", error});
	if (!error.ok()) return {input_error.ok() ? error : input_error,
		quiesced.mutation_attempted, ""};
	const NativeResult programmed = fpga_.Program(artifact,
		Deadline(clock_, timeouts_.program_ms));
	error = programmed.error.ok() ? Error{} : ProgramError(programmed.error);
	log_.Write({"start", "", "", "program", error});
	if (!error.ok()) return {input_error.ok() ? error : input_error,
		quiesced.mutation_attempted || programmed.mutation_attempted, ""};
	const VideoResult video = idle_video_.BringUp("MENU",
		Deadline(clock_, timeouts_.video_ms));
	if (!video.error.ok())
		return {input_error.ok() ? CoreIoError(video.error) : input_error,
			true, video.observed_core};
	return {input_error, true, video.observed_core};
}

HardwareResult NativeHardware::Launch(const PreparedLaunch& launch,
	std::uint64_t generation)
{
	const std::uint64_t input_deadline =
		Deadline(clock_, timeouts_.core_io_ms);
	Error error = input_.Open(input_identity_, launch.input, input_deadline);
	if (!error.ok()) {
		log_.Write({"launch", launch.system, launch.expected_core,
			"preflight", error});
		return {error, false, ""};
	}
	input_open_ = true;

	ArtifactSet artifacts;
	error = OpenLaunchArtifacts(launch, opener_, &artifacts);
	if (!error.ok()) {
		log_.Write({"launch", launch.system, launch.expected_core,
			"preflight", error});
		const Error stopped = StopInput(input_deadline);
		return {stopped.ok() ? error : stopped, false, ""};
	}
	std::sort(artifacts.media.begin(), artifacts.media.end(),
		[](const OpenedMedia& left, const OpenedMedia& right) {
			return left.index < right.index;
		});
	log_.Write({"launch", launch.system, launch.expected_core, "preflight", {}});
	const VideoQuiesceResult quiesced = game_video_.Quiesce(
		Deadline(clock_, timeouts_.video_ms));
	error = quiesced.error;
	log_.Write({"launch", launch.system, launch.expected_core,
		"hdmi_quiesce", error});
	if (!error.ok()) {
		const Error stopped = StopInput(input_deadline);
		return {stopped.ok() ? error : stopped,
			quiesced.mutation_attempted, ""};
	}
	const NativeResult programmed = fpga_.Program(artifacts.rbf,
		Deadline(clock_, timeouts_.program_ms));
	error = programmed.error.ok() ? Error{} : ProgramError(programmed.error);
	log_.Write({"launch", launch.system, launch.expected_core, "program", error});
	if (!error.ok()) {
		if (programmed.mutation_attempted)
			return {error, true, ""};
		const Error stopped = StopInput(
			Deadline(clock_, timeouts_.core_io_ms));
		return {stopped.ok() ? error : stopped,
			quiesced.mutation_attempted, ""};
	}

	const std::uint64_t core_deadline = Deadline(clock_, timeouts_.core_io_ms);
	error = core_.Synchronize(core_deadline);
	if (!error.ok()) error = CoreIoError(error);
	log_.Write({"launch", launch.system, launch.expected_core, "sync", error});
	if (!error.ok()) return {error, true, ""};

	error = core_.AssertReset(launch.core, core_deadline);
	if (!error.ok()) error = CoreIoError(error);
	log_.Write({"launch", launch.system, launch.expected_core, "reset", error});
	if (!error.ok()) return {error, true, ""};

	std::string observed;
	error = core_.Probe(&observed, core_deadline);
	if (!error.ok()) error = CoreIoError(error);
	log_.Write({"launch", launch.system,
		observed.empty() ? launch.expected_core : observed, "probe", error});
	if (!error.ok()) return {error, true, observed};
	if (observed != launch.expected_core) {
		error = {ErrorCode::core_mismatch, "observed core does not match profile"};
		log_.Write({"launch", launch.system, observed, "failure", error});
		return {error, true, observed};
	}

	error = core_.ApplyInitialStatus(launch.core, core_deadline);
	if (!error.ok()) error = CoreIoError(error);
	log_.Write({"launch", launch.system, observed, "configure", error});
	if (!error.ok()) return {error, true, observed};
	for (const OpenedMedia& media : artifacts.media) {
		error = core_.Attach(media.index, media.artifact,
			launch.core.file_wire, core_deadline);
		if (!error.ok()) error = CoreIoError(error);
		log_.Write({"launch", launch.system, observed, "media", error});
		if (!error.ok()) return {error, true, observed};
	}

	const VideoResult video = game_video_.BringUp(
		Deadline(clock_, timeouts_.video_ms));
	error = video.error.ok() ? Error{} : CoreIoError(video.error);
	log_.Write({"launch", launch.system, observed, "video", error});
	if (!error.ok()) return {error, true, observed};

	const std::uint64_t post_video_deadline =
		Deadline(clock_, timeouts_.core_io_ms);
	error = input_.Neutralize(post_video_deadline);
	if (!error.ok()) error = CoreIoError(error);
	log_.Write({"launch", launch.system, observed, "input-neutral", error});
	if (!error.ok()) return {error, true, observed};

	error = core_.ReleaseReset(launch.core, post_video_deadline);
	if (!error.ok()) error = CoreIoError(error);
	log_.Write({"launch", launch.system, observed, "release", error});
	if (!error.ok()) return {error, true, observed};

	error = input_.Start(generation,
		[this](std::uint64_t reported_generation, Error fault) {
			ForwardInputFault(reported_generation, std::move(fault));
		});
	if (!error.ok()) error = CoreIoError(error);
	log_.Write({"launch", launch.system, observed, "input", error});
	if (!error.ok()) return {error, true, observed};
	return {{}, true, observed};
}

HardwareResult NativeHardware::LoadDevelopmentRBF(const std::string& rbf)
{
	Artifact artifact;
	Error error = OpenRBFArtifact(rbf, opener_, &artifact);
	log_.Write({"load_development_rbf", "", "", "preflight", error});
	if (!error.ok()) return {error, false, ""};
	const NativeResult programmed = fpga_.Program(artifact,
		Deadline(clock_, timeouts_.program_ms));
	error = programmed.error.ok() ? Error{} : ProgramError(programmed.error);
	log_.Write({"load_development_rbf", "", "", "program", error});
	return {error, programmed.error.ok() ? true : programmed.mutation_attempted, ""};
}

} // namespace native
} // namespace mister
