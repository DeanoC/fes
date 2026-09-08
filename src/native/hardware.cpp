// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/hardware.hpp"

#include "native/artifacts.hpp"
#include "native/core_driver.hpp"
#include "native/core_package.hpp"
#include "native/core_loader.hpp"
#include "native/generated/fes_gp.hpp"
#include "native/input.hpp"
#include "native/video.hpp"

#include <algorithm>
#include <atomic>
#include <limits>
#include <memory>
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

InputRecipe FesGpInputRecipe()
{
	InputRecipe recipe;
	recipe.player_count = 1;
	recipe.player_command = static_cast<std::uint16_t>(
		generated::FesGpOpcodeButtons);
	recipe.up = generated::FesGpButtonUp;
	recipe.down = generated::FesGpButtonDown;
	recipe.left = generated::FesGpButtonLeft;
	recipe.right = generated::FesGpButtonRight;
	recipe.a = generated::FesGpButtonA;
	recipe.b = generated::FesGpButtonB;
	recipe.select = generated::FesGpButtonSelect;
	recipe.start = generated::FesGpButtonStart;
	return recipe;
}

class NativeAdmittedCore final : public AdmittedCorePackage {
public:
	NativeAdmittedCore(OpenedCorePackage opened, ProgrammingProfile profile,
		CoreDriver* driver, CoreDriverContext context)
		: AdmittedCorePackage({opened.package_id, opened.descriptor.core.id,
			opened.descriptor.core.system}), opened_(std::move(opened)),
		  profile_(profile), driver_(driver), context_(std::move(context)) {}

	OpenedCorePackage opened_;
	ProgrammingProfile profile_;
	CoreDriver* driver_;
	CoreDriverContext context_;
	CoreRecipe recipe_;
};

} // namespace

NativeHardware::NativeHardware(ArtifactOpener& opener, FpgaManager& fpga,
	CoreLoader& core, VideoBringup& idle_video, FixedVideoBringup& game_video,
	InputSession& input, const InputDeviceIdentity& input_identity, Clock& clock,
	LogSink& log, std::string idle_rbf, NativeTimeouts timeouts,
	CoreDriver& mister_driver, CoreDriver* fes_gp_driver,
	const Profiles* profiles)
	: opener_(opener), fpga_(fpga), core_(core), idle_video_(idle_video),
	  game_video_(game_video), input_(input), input_identity_(input_identity),
	  clock_(clock), log_(log), idle_rbf_(std::move(idle_rbf)),
	  timeouts_(timeouts), mister_driver_(mister_driver),
	  fes_gp_driver_(fes_gp_driver), contained_driver_(),
	  driver_registry_(mister_driver_, fes_gp_driver_, contained_driver_),
	  profiles_(profiles),
	  active_driver_(nullptr), active_context_(), active_package_(),
	  fault_sink_mutex_(), fault_sink_(nullptr),
	  input_open_(false), input_delivery_enabled_() {}

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
	if (!input_open_) {
		if (input_delivery_enabled_)
			input_delivery_enabled_->store(false);
		input_delivery_enabled_.reset();
		return {};
	}
	const Error error = input_.Stop(deadline);
	if (input_delivery_enabled_)
		input_delivery_enabled_->store(false);
	input_delivery_enabled_.reset();
	input_open_ = false;
	return error;
}

Error NativeHardware::FlushSave()
{
	if (!save_ || save_flushed_) return {};
	Error error = StopInput(Deadline(clock_, timeouts_.core_io_ms));
	if (error.ok() && snapshot_.empty())
		error = core_.CaptureSave(save_->size(), clock_, Deadline(clock_, timeouts_.core_io_ms), &snapshot_);
	if (error.ok()) error = save_->Persist(snapshot_);
	if (!error.ok()) {
		error.code = ErrorCode::save_failed;
		log_.Write({"stop", "snes", "SNES", "save", error});
		return error;
	}
	save_flushed_ = true;
	log_.Write({"stop", "snes", "SNES", "save", {}});
	return {};
}

CoreDriver* NativeHardware::ResolveDriver(ProgrammingProfile profile) const
{
	return driver_registry_.Resolve(profile);
}

void NativeHardware::ForgetActiveCore()
{
	active_driver_ = nullptr;
	active_context_ = {};
	active_package_.reset();
}

HardwareResult NativeHardware::QuiesceForReplacement(const char* operation,
	const std::string& system, const std::string& core)
{
	const VideoQuiesceResult video = game_video_.Quiesce(
		Deadline(clock_, timeouts_.video_ms));
	log_.Write({operation, system, core, "hdmi_quiesce", video.error});
	if (!video.error.ok())
		return {video.error, video.mutation_attempted, ""};
	if (active_driver_ == nullptr) return {{}, video.mutation_attempted, ""};
	const CoreDriverResult driver = active_driver_->Quiesce(active_context_,
		Deadline(clock_, timeouts_.core_io_ms));
	log_.Write({operation, system, core, "driver_quiesce", driver.error});
	if (!driver.error.ok() && driver.mutation_attempted) ForgetActiveCore();
	return {driver.error,
		video.mutation_attempted || driver.mutation_attempted,
		driver.observed_core};
}

Error NativeHardware::AdmitCorePackage(const std::string& directory,
	const std::string& expected_id,
	std::unique_ptr<AdmittedCorePackage>* output)
{
	if (output == nullptr)
		return {ErrorCode::invalid_request, "missing admitted package output"};
	if (expected_id.empty())
		return {ErrorCode::invalid_request, "package activation requires expected identity"};
	OpenedCorePackage opened;
	Error error = native::OpenCorePackage(directory, expected_id, &opened);
	if (error.ok()) error = CheckCoreCompatibility(opened.descriptor);
	ProgrammingProfile profile = ProgrammingProfile::development_contained_v1;
	CoreDriver* driver = nullptr;
	if (error.ok()) error = driver_registry_.Resolve(opened.descriptor,
		&profile, &driver);
	CoreDriverContext context;
	context.report_fault = [this](std::uint64_t generation, Error fault) {
		ForwardInputFault(generation, std::move(fault));
	};
	Profile system_profile;
	if (error.ok() && profile == ProgrammingProfile::mister_v1 &&
		!opened.descriptor.core.system.empty()) {
		if (profiles_ == nullptr)
			error = {ErrorCode::unknown_system, "MiSTer package system is unavailable"};
		else error = profiles_->Describe(opened.descriptor.core.system,
			&system_profile);
		if (error.ok()) {
			context.mister_recipe = &system_profile.core;
			context.expected_core = system_profile.expected_core;
		}
	} else if (error.ok() && profile == ProgrammingProfile::fes_gp_v1) {
		context.expected_core = opened.descriptor.core.id;
	}
	if (!error.ok()) return error;
	context.descriptor = &opened.descriptor;
	std::unique_ptr<NativeAdmittedCore> admitted(new NativeAdmittedCore(
		std::move(opened), profile, driver, context));
	// Rebind pointers after moving the retained descriptor and optional recipe.
	admitted->context_.descriptor = &admitted->opened_.descriptor;
	if (profile == ProgrammingProfile::mister_v1 &&
		!admitted->opened_.descriptor.core.system.empty()) {
		admitted->context_.mister_recipe = nullptr;
		Profile checked;
		const Error described = profiles_->Describe(
			admitted->opened_.descriptor.core.system, &checked);
		if (!described.ok()) return described;
		// The recipe copy is retained in the context through the package wrapper.
		admitted->recipe_ = checked.core;
		admitted->context_.mister_recipe = &admitted->recipe_;
	}
	*output = std::move(admitted);
	return {};
}

HardwareResult NativeHardware::LoadCore(
	std::unique_ptr<AdmittedCorePackage> package, std::uint64_t generation)
{
	NativeAdmittedCore* admitted = dynamic_cast<NativeAdmittedCore*>(package.get());
	if (admitted == nullptr || admitted->driver_ == nullptr)
		return {{ErrorCode::invalid_request, "invalid admitted core package"}, false, ""};
	Error error = CheckCoreCompatibility(admitted->opened_.descriptor);
	if (!error.ok()) return {error, false, ""};
	ProgrammingProfile checked_profile;
	CoreDriver* checked_driver = nullptr;
	error = driver_registry_.Resolve(admitted->opened_.descriptor,
		&checked_profile, &checked_driver);
	if (!error.ok() || checked_profile != admitted->profile_ ||
		checked_driver != admitted->driver_)
		return {{ErrorCode::unsupported_protocol,
			"admitted core driver changed before activation"}, false, ""};

	const bool fes_gp = admitted->profile_ == ProgrammingProfile::fes_gp_v1;
	error = StopInput(Deadline(clock_, timeouts_.core_io_ms));
	log_.Write({"load_core", admitted->opened_.descriptor.core.system,
		admitted->opened_.descriptor.core.id, "input_stop", error});
	if (!error.ok()) return {error, true, ""};
	std::shared_ptr<std::atomic<bool>> identity_verified;
	if (fes_gp) {
		identity_verified = std::make_shared<std::atomic<bool>>(false);
		CoreDriver* const input_driver = admitted->driver_;
		CoreDriverContext input_context;
		input_context.generation = generation;
		error = input_.Open(input_identity_, FesGpInputRecipe(),
			Deadline(clock_, timeouts_.core_io_ms),
			[input_driver, input_context, identity_verified](std::uint16_t map,
				std::uint64_t deadline) {
				if (!identity_verified->load()) return Error{};
				return input_driver->SetButtons(input_context, map, deadline).error;
			});
		log_.Write({"load_core", admitted->opened_.descriptor.core.system,
			admitted->opened_.descriptor.core.id, "input_open", error});
		if (!error.ok()) return {error, true, ""};
		input_open_ = true;
		input_delivery_enabled_ = identity_verified;
	}
	const HardwareResult quiesced = QuiesceForReplacement("load_core",
		admitted->opened_.descriptor.core.system,
		admitted->opened_.descriptor.core.id);
	if (!quiesced.error.ok()) {
		const Error stopped = StopInput(Deadline(clock_, timeouts_.core_io_ms));
		return {stopped.ok() ? quiesced.error : stopped,
			quiesced.mutation_attempted, quiesced.observed_core};
	}
	const NativeResult programmed = fpga_.Program(admitted->opened_.payload,
		admitted->profile_, Deadline(clock_, timeouts_.program_ms));
	if (!programmed.error.ok()) {
		const Error stopped = StopInput(Deadline(clock_, timeouts_.core_io_ms));
		if (programmed.mutation_attempted) ForgetActiveCore();
		return {stopped.ok() ? ProgramError(programmed.error) : stopped,
			quiesced.mutation_attempted || programmed.mutation_attempted, ""};
	}
	admitted->driver_->BeginSession();
	admitted->context_.generation = generation;
	active_driver_ = admitted->driver_;
	active_package_ = std::move(package);
	NativeAdmittedCore* retained =
		dynamic_cast<NativeAdmittedCore*>(active_package_.get());
	active_context_ = retained->context_;
	admitted = retained;
	if (admitted->profile_ == ProgrammingProfile::mister_v1) {
		error = core_.Synchronize(Deadline(clock_, timeouts_.core_io_ms));
		if (!error.ok()) return {CoreIoError(error), true, ""};
	}
	CoreDriverResult identified = admitted->driver_->Identify(
		admitted->context_, Deadline(clock_, timeouts_.core_io_ms));
	if (!identified.error.ok()) {
		const Error stopped = StopInput(Deadline(clock_, timeouts_.core_io_ms));
		if (fes_gp) ForgetActiveCore();
		if (!stopped.ok()) identified.error = stopped;
		return {identified.error, true, identified.observed_core};
	}
	if (fes_gp) {
		identity_verified->store(true);
		const VideoResult video = game_video_.BringUpCustom(
			Deadline(clock_, timeouts_.video_ms));
		log_.Write({"load_core", admitted->opened_.descriptor.core.system,
			identified.observed_core, "video", video.error});
		if (!video.error.ok()) {
			const Error stopped = StopInput(Deadline(clock_, timeouts_.core_io_ms));
			return {stopped.ok() ? CoreIoError(video.error) : stopped,
				true, identified.observed_core};
		}
		error = input_.Neutralize(Deadline(clock_, timeouts_.core_io_ms));
		if (!error.ok()) return {CoreIoError(error), true, identified.observed_core};
		CoreDriverResult started = admitted->driver_->Start(admitted->context_,
			Deadline(clock_, timeouts_.core_io_ms));
		if (!started.error.ok()) return {started.error, true, identified.observed_core};
		error = input_.Start(generation,
			[this](std::uint64_t reported_generation, Error fault) {
				ForwardInputFault(reported_generation, std::move(fault));
			});
		if (!error.ok()) return {CoreIoError(error), true, identified.observed_core};
	}
	return {{}, true, identified.observed_core};
}

HardwareResult NativeHardware::LoadIdle()
{
	// Startup/fault/failed-launch cleanup deliberately has no save side effect.
	save_.reset();
	snapshot_.clear();
	save_flushed_ = false;
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
	bool quiesce_mutation = quiesced.mutation_attempted;
	if (active_driver_ != nullptr) {
		const CoreDriverResult driver = active_driver_->Quiesce(active_context_,
			Deadline(clock_, timeouts_.core_io_ms));
		quiesce_mutation = quiesce_mutation || driver.mutation_attempted;
		log_.Write({"start", "", "", "driver_quiesce", driver.error});
		if (!driver.error.ok())
			return {input_error.ok() ? driver.error : input_error,
				quiesce_mutation, driver.observed_core};
	}
	const NativeResult programmed = fpga_.Program(artifact,
		ProgrammingProfile::mister_v1,
		Deadline(clock_, timeouts_.program_ms));
	error = programmed.error.ok() ? Error{} : ProgramError(programmed.error);
	log_.Write({"start", "", "", "program", error});
	if (!error.ok()) {
		if (programmed.mutation_attempted) ForgetActiveCore();
		return {input_error.ok() ? error : input_error,
			quiesce_mutation || programmed.mutation_attempted, ""};
	}
	const VideoResult video = idle_video_.BringUp("MENU",
		Deadline(clock_, timeouts_.video_ms));
	if (!video.error.ok())
		return {input_error.ok() ? CoreIoError(video.error) : input_error,
			true, video.observed_core};
	active_driver_ = &mister_driver_;
	active_package_.reset();
	active_context_ = {};
	active_context_.expected_core = "MENU";
	return {input_error, true, video.observed_core};
}

HardwareResult NativeHardware::Launch(const PreparedLaunch& launch,
	std::uint64_t generation)
{
	CoreDriverContext driver_context;
	driver_context.mister_recipe = &launch.core;
	driver_context.expected_core = launch.expected_core;
	driver_context.generation = generation;
	driver_context.player_command = launch.input.player_command;
	const std::uint64_t input_deadline =
		Deadline(clock_, timeouts_.core_io_ms);
	const std::shared_ptr<std::atomic<bool>> input_delivery_enabled =
		std::make_shared<std::atomic<bool>>(false);
	Error error = input_.Open(input_identity_, launch.input, input_deadline,
		[this, driver_context, input_delivery_enabled](std::uint16_t map,
			std::uint64_t deadline) {
			if (!input_delivery_enabled->load()) return Error{};
			return mister_driver_.SetButtons(driver_context, map, deadline).error;
		});
	if (!error.ok()) {
		log_.Write({"launch", launch.system, launch.expected_core,
			"preflight", error});
		return {error, false, ""};
	}
	input_open_ = true;
	input_delivery_enabled_ = input_delivery_enabled;

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
	const HardwareResult quiesced = QuiesceForReplacement("launch",
		launch.system, launch.expected_core);
	error = quiesced.error;
	if (!error.ok()) {
		const Error stopped = StopInput(input_deadline);
		return {stopped.ok() ? error : stopped,
			quiesced.mutation_attempted, ""};
	}
	const NativeResult programmed = fpga_.Program(artifacts.rbf,
		ProgrammingProfile::mister_v1,
		Deadline(clock_, timeouts_.program_ms));
	error = programmed.error.ok() ? Error{} : ProgramError(programmed.error);
	log_.Write({"launch", launch.system, launch.expected_core, "program", error});
	if (!error.ok()) {
		if (programmed.mutation_attempted) {
			ForgetActiveCore();
			return {error, true, ""};
		}
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

	CoreDriverResult identified = mister_driver_.Identify(driver_context,
		core_deadline);
	error = identified.error;
	if (!error.ok() && error.code != ErrorCode::core_mismatch)
		error = CoreIoError(error);
	if (!error.ok()) return {error, true, identified.observed_core};

	std::string observed = identified.observed_core;
	log_.Write({"launch", launch.system,
		observed.empty() ? launch.expected_core : observed, "probe", error});

	error = core_.ApplyInitialStatus(launch.core, core_deadline);
	if (!error.ok()) error = CoreIoError(error);
	log_.Write({"launch", launch.system, observed, "configure", error});
	if (!error.ok()) return {error, true, observed};
	for (const OpenedMedia& media : artifacts.media) {
		const std::uint64_t media_deadline =
			Deadline(clock_, timeouts_.media_io_ms);
		error = core_.Attach(media,
			launch.core.file_wire, media_deadline, artifacts.save.get());
		if (!error.ok()) error = CoreIoError(error);
		log_.Write({"launch", launch.system, observed, "media", error});
		if (!error.ok()) return {error, true, observed};
	}

	if (artifacts.save) {
		error = core_.RestoreSave(*artifacts.save, clock_, Deadline(clock_, timeouts_.core_io_ms));
		log_.Write({"launch", launch.system, observed, "save_restore", error});
		if (!error.ok()) return {error, true, observed};
	}

	const VideoResult video = game_video_.BringUp(
		Deadline(clock_, timeouts_.video_ms));
	error = video.error.ok() ? Error{} : CoreIoError(video.error);
	log_.Write({"launch", launch.system, observed, "video", error});
	if (!error.ok()) return {error, true, observed};

	const std::uint64_t post_video_deadline =
		Deadline(clock_, timeouts_.core_io_ms);
	input_delivery_enabled->store(true);
	error = input_.Neutralize(post_video_deadline);
	if (!error.ok()) error = CoreIoError(error);
	log_.Write({"launch", launch.system, observed, "input-neutral", error});
	if (!error.ok()) return {error, true, observed};

	const CoreDriverResult started = mister_driver_.Start(driver_context,
		post_video_deadline);
	error = started.error;
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
	save_ = std::move(artifacts.save);
	snapshot_.clear();
	save_flushed_ = false;
	active_driver_ = &mister_driver_;
	active_package_.reset();
	active_context_ = driver_context;
	active_context_.mister_recipe = nullptr;
	return {{}, true, observed};
}

HardwareResult NativeHardware::LoadDevelopmentRBF(const std::string& rbf)
{
	return LoadDevelopmentRBF(rbf, ProgrammingProfile::mister_v1);
}

HardwareResult NativeHardware::LoadDevelopmentRBF(const std::string& rbf,
	ProgrammingProfile profile)
{
	if (profile == ProgrammingProfile::fes_gp_v1)
		return {{ErrorCode::unsupported_protocol,
			"raw FES GP loading requires a described package"}, false, ""};
	Artifact artifact;
	Error error = OpenRBFArtifact(rbf, opener_, &artifact);
	log_.Write({"load_development_rbf", "", "", "preflight", error});
	if (!error.ok()) return {error, false, ""};
	const HardwareResult quiesced = QuiesceForReplacement(
		"load_development_rbf", "", "");
	error = quiesced.error;
	if (!error.ok()) return {error, quiesced.mutation_attempted, ""};
	const NativeResult programmed = fpga_.Program(artifact,
		profile,
		Deadline(clock_, timeouts_.program_ms));
	error = programmed.error.ok() ? Error{} : ProgramError(programmed.error);
	log_.Write({"load_development_rbf", "", "", "program", error});
	if (!error.ok()) {
		if (programmed.mutation_attempted) ForgetActiveCore();
		return {error, quiesced.mutation_attempted ||
			programmed.mutation_attempted, ""};
	}

	CoreDriver* driver = ResolveDriver(profile);
	if (driver == nullptr)
		return {{ErrorCode::unsupported_protocol, "core driver is unavailable"},
			true, ""};
	CoreDriverContext context;
	if (profile == ProgrammingProfile::mister_v1) {
		error = core_.Synchronize(Deadline(clock_, timeouts_.core_io_ms));
		if (!error.ok()) return {CoreIoError(error), true, ""};
	}
	CoreDriverResult identified = driver->Identify(context,
		Deadline(clock_, timeouts_.core_io_ms));
	error = identified.error;
	if (!error.ok()) error = CoreIoError(error);
	log_.Write({"load_development_rbf", "", identified.observed_core,
		"probe", error});
	if (error.ok()) {
		active_driver_ = driver;
		active_package_.reset();
		active_context_ = context;
	}
	return {error, true, identified.observed_core};
}

} // namespace native
} // namespace mister
