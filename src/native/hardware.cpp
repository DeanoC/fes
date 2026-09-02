// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/hardware.hpp"

#include "native/artifacts.hpp"
#include "native/core_loader.hpp"
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
	CoreLoader& core, VideoBringup& video, Clock& clock, LogSink& log,
	std::string idle_rbf, NativeTimeouts timeouts)
	: opener_(opener), fpga_(fpga), core_(core), video_(video), clock_(clock),
	  log_(log), idle_rbf_(std::move(idle_rbf)), timeouts_(timeouts) {}

HardwareResult NativeHardware::LoadIdle()
{
	Artifact artifact;
	Error error = OpenRBFArtifact(idle_rbf_, opener_, &artifact);
	log_.Write({"start", "", "", "preflight", error});
	if (!error.ok()) return {error, false, ""};
	const NativeResult programmed = fpga_.Program(artifact,
		Deadline(clock_, timeouts_.program_ms));
	error = programmed.error.ok() ? Error{} : ProgramError(programmed.error);
	log_.Write({"start", "", "", "program", error});
	if (!error.ok()) return {error, programmed.mutation_attempted, ""};
	const VideoResult video = video_.BringUp("MENU",
		Deadline(clock_, timeouts_.video_ms));
	if (!video.error.ok())
		return {CoreIoError(video.error), true, video.observed_core};
	return {{}, true, video.observed_core};
}

HardwareResult NativeHardware::Launch(const PreparedLaunch& launch)
{
	ArtifactSet artifacts;
	Error error = OpenLaunchArtifacts(launch, opener_, &artifacts);
	log_.Write({"launch", launch.system, launch.expected_core, "preflight", error});
	if (!error.ok()) return {error, false, ""};
	std::sort(artifacts.media.begin(), artifacts.media.end(),
		[](const OpenedMedia& left, const OpenedMedia& right) {
			return left.index < right.index;
		});
	const NativeResult programmed = fpga_.Program(artifacts.rbf,
		Deadline(clock_, timeouts_.program_ms));
	error = programmed.error.ok() ? Error{} : ProgramError(programmed.error);
	log_.Write({"launch", launch.system, launch.expected_core, "program", error});
	if (!error.ok()) return {error, programmed.mutation_attempted, ""};

	const std::uint64_t core_deadline = Deadline(clock_, timeouts_.core_io_ms);
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
