// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/hardware.hpp"

#include "native/artifacts.hpp"
#include "native/core_driver.hpp"
#include "native/core_package.hpp"
#include "native/core_composition.hpp"
#include "native/core_data.hpp"
#include "native/diagnostic.hpp"
#include "native/fes_gp.hpp"
#include "native/generated/fes_gp.hpp"
#include "native/generated/fes_simple_computer.hpp"
#include "native/generated/fes_application.hpp"
#include "native/input.hpp"
#include "native/video.hpp"

#include <algorithm>
#include <array>
#include <atomic>
#include <cctype>
#include <cerrno>
#include <limits>
#include <memory>
#include <string>
#include <vector>
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

Error WithPhase(Error error, const char* phase)
{
	if (!error.ok() && error.phase.empty()) error.phase = phase;
	return error;
}

bool RequiresFesGamepad(const CoreDescriptor& descriptor)
{
	for (const CoreInterface& interface : descriptor.interfaces)
		if (interface.id == generated::FesGpInterfaceGamepadID && interface.required)
			return true;
	return false;
}

Error ProgramError(Error error)
{
	error.code = ErrorCode::program_failed;
	if (error.message.empty()) error.message = "FPGA programming failed";
	return WithPhase(std::move(error), "programming");
}

void ObserveIdentify(const std::string& expected, const CoreDriverResult& identified)
{
	if (identified.error.code == ErrorCode::core_mismatch) {
		std::vector<DiagnosticField> detail;
		detail.push_back(DiagnosticString("operation", "identity"));
		detail.push_back(DiagnosticBool("ok", false));
		if (!expected.empty())
			detail.push_back(DiagnosticString("expected", expected));
		if (!identified.observed_core.empty())
			detail.push_back(DiagnosticString("observed", identified.observed_core));
		else if (!identified.error.observed.empty())
			detail.push_back(DiagnosticString("observed", identified.error.observed));
		if (!identified.error.expected.empty() && expected.empty())
			detail.push_back(DiagnosticString("expected", identified.error.expected));
		EmitDiagnostic(kDiagnosticLayerRuntime, kDiagnosticKindFenceAbi, "error",
			detail);
		return;
	}
	if (identified.error.ok() && !identified.observed_core.empty())
		EmitCoreNameChange(expected, identified.observed_core, true);
}

void ObserveProgram(const char* operation, const NativeResult& programmed)
{
	EmitFence(kDiagnosticKindFenceProgram,
		programmed.error.ok() ? "ok" : "error", operation, programmed.error.ok());
}

void ObserveHandoff(const char* operation, const Error& error)
{
	EmitFence(kDiagnosticKindFenceHandoff, error.ok() ? "ok" : "error",
		operation, error.ok());
}

Error CoreIoError(Error error, const char* phase = "transport")
{
	error.code = ErrorCode::io_failed;
	if (error.message.empty()) error.message = "core I/O failed";
	return WithPhase(std::move(error), phase);
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
			opened.descriptor.core.system, opened.descriptor}), opened_(std::move(opened)),
		  profile_(profile), driver_(driver), context_(std::move(context)) {}

	OpenedCorePackage opened_;
	ProgrammingProfile profile_;
	CoreDriver* driver_;
	CoreDriverContext context_;
	std::unique_ptr<CoreDataFile> data_file_;
	CoreData data_;
	std::unique_ptr<OpenedCoreComposition> composition_;
	Artifact programmed_;
	std::string programmed_sha256_;
	bool has_programmed_ = false;
	CoreROMLink rom_link_;
	CoreComposition composition() const override { return composition_ ? composition_->info : CoreComposition{}; }
};

} // namespace

NativeHardware::NativeHardware(ArtifactOpener& opener, FpgaManager& fpga,
	VideoBringup& idle_video, FixedVideoBringup& game_video,
	InputSession& input, const InputDeviceIdentity& input_identity, Clock& clock,
	LogSink& log, std::string idle_rbf, NativeTimeouts timeouts,
	CoreDriver* fes_gp_driver, std::vector<std::string> package_roots,
	IdleRecipe idle_recipe)
	: opener_(opener), fpga_(fpga), idle_video_(idle_video),
	  game_video_(game_video), input_(input), input_identity_(input_identity),
	  clock_(clock), log_(log), idle_rbf_(std::move(idle_rbf)),
	  idle_recipe_(std::move(idle_recipe)),
	  timeouts_(timeouts),
	  fes_gp_driver_(fes_gp_driver), contained_driver_(),
	  driver_registry_(fes_gp_driver_, contained_driver_),
	  package_roots_(std::move(package_roots)),
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
	if (core_data_file_) {
		if (core_data_flushed_)
			return {};
		Error error = StopInput(Deadline(clock_, timeouts_.core_io_ms));
		if (error.ok() && core_snapshot_.empty())
			error = active_driver_->CaptureData(
				active_context_, Deadline(clock_, timeouts_.core_io_ms), &core_snapshot_);
		if (error.ok() && core_snapshot_.size() != generated::FesGpPongProgressWordCount)
			error = {ErrorCode::save_failed, "incomplete core-data snapshot", "core_data"};
		if (error.ok()) {
			CoreData snapshot = durable_data_;
			snapshot.paddle_speed = core_snapshot_[generated::FesGpPongPaddleSpeedIndex];
			snapshot.best_rally = core_snapshot_[generated::FesGpPongBestRallyIndex];
			// Reopen after uncertain publication; never mistake a cached revision for
			// the current complete record on retry.
			CoreData current;
			error = core_data_file_->Read(&current);
			if (error.ok())
				error = core_data_file_->Persist(snapshot, current.revision, &durable_data_);
		}
		if (!error.ok()) {
			error.code = ErrorCode::save_failed;
			return WithPhase(error, "core_data");
		}
		core_data_flushed_ = true;
		return {};
	}
	return {};
}

Error NativeHardware::RestoreInput(std::uint64_t generation)
{
	if (!core_data_file_)
		return {};
	if (!has_active_input_recipe_ || active_driver_ == nullptr || generation == 0)
		return {ErrorCode::io_failed,
			"active input cannot be restored", "input"};
	if (input_open_)
		return {ErrorCode::io_failed,
			"active input was not fully stopped", "input"};

	if (core_data_file_) {
		const Error resumed =
			active_driver_->ResumeData(active_context_, Deadline(clock_, timeouts_.core_io_ms));
		if (!resumed.ok())
			return resumed;
	}
	CoreDriver* const driver = active_driver_;
	const CoreDriverContext context = active_context_;
	const std::shared_ptr<std::atomic<bool>> delivery_enabled =
		std::make_shared<std::atomic<bool>>(false);
	Error error = input_.Open(input_identity_, active_input_recipe_,
		Deadline(clock_, timeouts_.core_io_ms),
		[driver, context, delivery_enabled](std::uint16_t map,
			std::uint64_t deadline) {
			if (!delivery_enabled->load()) return Error{};
			return driver->SetButtons(context, map, deadline).error;
		});
	if (!error.ok()) return WithPhase(std::move(error), "input");
	input_open_ = true;
	input_delivery_enabled_ = delivery_enabled;
	delivery_enabled->store(true);
	error = input_.Neutralize(Deadline(clock_, timeouts_.core_io_ms));
	if (error.ok()) {
		error = input_.Start(generation,
			[this](std::uint64_t reported_generation, Error fault) {
				ForwardInputFault(reported_generation, std::move(fault));
			});
	}
	if (!error.ok()) {
		const Error stopped = StopInput(Deadline(clock_, timeouts_.core_io_ms));
		return WithPhase(stopped.ok() ? std::move(error) : stopped, "input");
	}

	// The failed write's snapshot described an earlier instant. Once play can
	// continue, the next save attempt must capture the then-current RAM.
	core_snapshot_.clear();
	core_data_flushed_ = false;
	log_.Write({"restore_input", "", "", "input", {}});
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
	has_active_input_recipe_ = false;
}

HardwareResult NativeHardware::QuiesceForReplacement(const char* operation,
	const std::string& system, const std::string& core)
{
	const VideoQuiesceResult video = game_video_.Quiesce(
		Deadline(clock_, timeouts_.video_ms));
	log_.Write({operation, system, core, "hdmi_quiesce", video.error});
	if (!video.error.ok()) {
		ObserveHandoff(operation, video.error);
		return {WithPhase(video.error, "quiesce"),
			video.mutation_attempted, ""};
	}
	if (active_driver_ == nullptr) {
		ObserveHandoff(operation, {});
		return {{}, video.mutation_attempted, ""};
	}
	const CoreDriverResult driver = active_driver_->Quiesce(active_context_,
		Deadline(clock_, timeouts_.core_io_ms));
	log_.Write({operation, system, core, "driver_quiesce", driver.error});
	ObserveHandoff(operation, driver.error);
	if (!driver.error.ok() && driver.mutation_attempted) ForgetActiveCore();
	return {WithPhase(driver.error, "quiesce"),
		video.mutation_attempted || driver.mutation_attempted,
		driver.observed_core};
}

Error NativeHardware::AdmitCorePackage(const std::string& directory,
	const std::string& expected_id,
	std::unique_ptr<AdmittedCorePackage>* output)
{
	if (output == nullptr)
		return {ErrorCode::invalid_request,
			"missing admitted package output", "request"};
	if (expected_id.empty())
		return {ErrorCode::invalid_request,
			"package activation requires expected identity", "request"};
	OpenedCorePackage opened;
	Error error = native::OpenCorePackage(package_roots_, directory,
		expected_id, &opened);
	if (error.ok()) error = CheckCoreCompatibility(opened.descriptor);
	ProgrammingProfile profile = ProgrammingProfile::development_contained_v1;
	CoreDriver* driver = nullptr;
	if (error.ok()) error = driver_registry_.Resolve(opened.descriptor,
		&profile, &driver);
	CoreDriverContext context;
	context.report_fault = [this](std::uint64_t generation, Error fault) {
		ForwardInputFault(generation, std::move(fault));
	};
	if (error.ok()) context.expected_core = opened.descriptor.core.id;
	if (!error.ok()) return error;
	context.descriptor = &opened.descriptor;
	std::unique_ptr<NativeAdmittedCore> admitted(new NativeAdmittedCore(
		std::move(opened), profile, driver, context));
	// Rebind the context after moving the retained descriptor.
	admitted->context_.descriptor = &admitted->opened_.descriptor;
	*output = std::move(admitted);
	return {};
}

Error NativeHardware::AdmitCoreComposition(const std::string& directory,
	const std::string& id, const CoreCompositionRequest& request,
	std::unique_ptr<AdmittedCorePackage>* output)
{
	if (!output) return {ErrorCode::invalid_request, "missing admitted composition output", "request"};
	std::unique_ptr<AdmittedCorePackage> package;
	Error error=AdmitCorePackage(directory,id,&package);
	if (!error.ok()) return error;
	auto* native=dynamic_cast<NativeAdmittedCore*>(package.get());
	std::unique_ptr<OpenedCoreComposition> composition(new OpenedCoreComposition);
	error=OpenCoreComposition(package_roots_,native->opened_,request,composition.get());
	if (!error.ok()) return error;
	native->composition_=std::move(composition);
	*output=std::move(package);return {};
}

Error NativeHardware::InspectCorePackage(const std::string& directory,
	const std::string& expected_id, CorePackageInspection* output)
{
	if (output == nullptr)
		return {ErrorCode::invalid_request,
			"missing core package inspection output", "request"};
	OpenedCorePackage opened;
	Error error = native::OpenCorePackage(package_roots_, directory,
		expected_id, &opened);
	if (!error.ok()) return error;
	Error compatibility = CheckCoreCompatibility(opened.descriptor);
	ProgrammingProfile profile = ProgrammingProfile::development_contained_v1;
	CoreDriver* driver = nullptr;
	if (compatibility.ok())
		compatibility = driver_registry_.Resolve(opened.descriptor,
			&profile, &driver);
	CorePackageInspection inspection;
	inspection.package_id = opened.package_id;
	inspection.descriptor = std::move(opened.descriptor);
	if (compatibility.ok())
		compatibility =
			CorePersistenceLayout(inspection.descriptor, &inspection.persistence_layout);
	inspection.compatible = compatibility.ok();
	inspection.compatibility_error = std::move(compatibility);
	*output = std::move(inspection);
	return {};
}

Error NativeHardware::PrepareCoreData(
	AdmittedCorePackage* package, const std::string& root, CoreData* output)
{
	return PrepareCoreDataInternal(package, root, output, true);
}

Error NativeHardware::PrepareCoreDataInternal(
	AdmittedCorePackage* package, const std::string& root, CoreData* output, bool writable)
{
	auto admitted = dynamic_cast<NativeAdmittedCore*>(package);
	if (!admitted || !output)
		return {ErrorCode::invalid_request, "invalid admitted core-data request"};
	VersionedContract layout;
	Error error = CorePersistenceLayout(admitted->opened_.descriptor, &layout);
	if (!error.ok())
		return error;
	const std::string& id = admitted->opened_.descriptor.core.id;
	if (core_data_file_ && durable_data_.core_id == id && layout.id.empty())
		return {ErrorCode::incompatible_data, "active namespace requires persistence", "core_data"};
	std::unique_ptr<CoreDataFile> file;
	error = CoreDataFile::Open(root, id, &file);
	CoreData data;
	if (error.ok())
		error = file->Read(&data);
	if (!error.ok())
		return error;
	if (layout.id.empty() && data.revision != "absent")
		return {
			ErrorCode::incompatible_data, "existing namespace requires persistence", "core_data"};
	if (writable && !layout.id.empty()) {
		error = file->CheckWritable();
		if (!error.ok())
			return error;
	}
	data.package_id = admitted->info().package_id;
	data.layout = layout;
	data.mode = layout.id.empty() ? "volatile" : "persistent";
	admitted->data_ = data;
	// Volatile candidates still admit/inspect the namespace, but never retain a
	// writer or restore target-owned data into a development session.
	if (!layout.id.empty())
		admitted->data_file_ = std::move(file);
	*output = std::move(data);
	return {};
}
Error NativeHardware::RefreshCoreData(AdmittedCorePackage* package, CoreData* output)
{
	auto admitted = dynamic_cast<NativeAdmittedCore*>(package);
	if (!admitted || !output)
		return {ErrorCode::invalid_request, "invalid core-data refresh"};
	if (!admitted->data_file_) {
		*output = admitted->data_;
		return {};
	}
	CoreData data;
	Error error = admitted->data_file_->Read(&data);
	if (!error.ok())
		return error;
	data.package_id = admitted->info().package_id;
	admitted->data_ = data;
	*output = std::move(data);
	return {};
}
Error NativeHardware::InspectCoreData(
	const std::string& path, const std::string& id, const std::string& root, CoreData* output)
{
	std::unique_ptr<AdmittedCorePackage> admitted;
	Error error = AdmitCorePackage(path, id, &admitted);
	if (error.ok())
		error = PrepareCoreDataInternal(admitted.get(), root, output, false);
	return error;
}
Error NativeHardware::UpdateCoreSettings(const std::string& path, const std::string& id,
	const std::string& root, const std::string& revision, std::uint16_t speed, CoreData* output)
{
	if (speed > generated::FesGpPongPaddleSpeedFast)
		return {ErrorCode::invalid_request, "invalid paddle speed", "request"};
	std::unique_ptr<AdmittedCorePackage> package;
	Error error = AdmitCorePackage(path, id, &package);
	if (!error.ok())
		return error;
	if (active_package_ && active_package_->info().declared_core == package->info().declared_core)
		return {ErrorCode::busy, "core-data namespace is active", "core_data"};
	CoreData data;
	error = PrepareCoreData(package.get(), root, &data);
	if (!error.ok())
		return error;
	auto admitted = dynamic_cast<NativeAdmittedCore*>(package.get());
	if (!admitted->data_file_)
		return {
			ErrorCode::unsupported_interface, "package has no persistent settings", "core_data"};
	data.paddle_speed = speed;
	CoreData published;
	error = admitted->data_file_->Persist(data, revision, &published);
	if (error.ok()) {
		published.package_id = id;
		*output = std::move(published);
	}
	return error;
}

Capabilities NativeHardware::capabilities() const
{
	Capabilities result;
	result.rom_linking = 1;
	result.programming_profiles = {"development-contained-v1"};
	if (fes_gp_driver_ != nullptr) {
		result.programming_profiles.insert(result.programming_profiles.begin() + 1,
			"fes-gp-v1");
		SupportedABI fes;
		fes.id = generated::FesGpABIID;
		fes.major = generated::FesGpABIMajor;
		fes.minor = generated::FesGpABIMinor;
		fes.interfaces = {
			{generated::FesGpInterfaceGamepadID, generated::FesGpInterfaceGamepadMajor,
				generated::FesGpInterfaceGamepadMinor},
			{generated::FesGpInterfaceVideoFixed720p60ID,
				generated::FesGpInterfaceVideoFixed720p60Major,
				generated::FesGpInterfaceVideoFixed720p60Minor},
			{generated::FesGpInterfacePersistenceWordsID,
				generated::FesGpInterfacePersistenceWordsMajor,
				generated::FesGpInterfacePersistenceWordsMinor},
			{generated::FesGpInterfacePongProgressID, generated::FesGpInterfacePongProgressMajor,
				generated::FesGpInterfacePongProgressMinor}};
		std::sort(fes.interfaces.begin(), fes.interfaces.end(),
			[](const SupportedInterface& a, const SupportedInterface& b) { return a.id < b.id; });
		result.abis.insert(result.abis.begin(), std::move(fes));
		SupportedABI computer;
		computer.id = generated::FesSimpleComputerABIID;
		computer.major = generated::FesSimpleComputerABIMajor;
		computer.minor = generated::FesSimpleComputerABIMinor;
		computer.interfaces = {
			{"fes.expansion.zx81-bus", 1, 0},
			{generated::FesSimpleComputerInterfaceKeyboardID,
				generated::FesSimpleComputerInterfaceKeyboardMajor,
				generated::FesSimpleComputerInterfaceKeyboardMinor},
			{generated::FesSimpleComputerInterfaceVideoFixed720p60ID,
				generated::FesSimpleComputerInterfaceVideoFixed720p60Major,
				generated::FesSimpleComputerInterfaceVideoFixed720p60Minor},
			{generated::FesSimpleComputerInterfaceMediaBlobID,
				generated::FesSimpleComputerInterfaceMediaBlobMajor,
				generated::FesSimpleComputerInterfaceMediaBlobMinor},
			{generated::FesSimpleComputerInterfaceMediaBlobStreamID,
				generated::FesSimpleComputerInterfaceMediaBlobStreamMajor,
				generated::FesSimpleComputerInterfaceMediaBlobStreamMinor}};
		std::sort(computer.interfaces.begin(), computer.interfaces.end(),
			[](const SupportedInterface& a, const SupportedInterface& b) { return a.id < b.id; });
		result.abis.insert(result.abis.begin(), std::move(computer));
		SupportedABI application;
		application.id = generated::FesApplicationABIID;
		application.major = generated::FesApplicationABIMajor;
		application.minor = generated::FesApplicationABIMinor;
		application.interfaces = {
			{"fes.expansion.coleco-bus", 1, 0},
			{generated::FesApplicationInterfaceAudioPcmS16Stereo48kID, 1, 0},
			{generated::FesApplicationInterfaceFirmwareBlobID, 1, 0},
			{generated::FesApplicationInterfaceGamepadID, 1, 0},
			{generated::FesApplicationInterfaceGamepadPortsID, 1, 0},
			{generated::FesApplicationInterfaceKeypadPortsID, 1, 0},
			{generated::FesApplicationInterfaceMediaBlobID, 1, 0},
			{generated::FesApplicationInterfaceMediaBlobStreamID, 1, 0},
			{generated::FesApplicationInterfaceVideoFixed720p60ID, 1, 0}};
		std::sort(application.interfaces.begin(), application.interfaces.end(),
			[](const SupportedInterface& a, const SupportedInterface& b) { return a.id < b.id; });
		result.abis.insert(result.abis.begin(), std::move(application));
		std::sort(result.abis.begin(), result.abis.end(),
			[](const SupportedABI& a, const SupportedABI& b) { return a.id < b.id; });
		if (active_driver_ == fes_gp_driver_) {
			MediaStreamInfo info;
			const auto* driver = dynamic_cast<FesGpCoreDriver*>(fes_gp_driver_);
			if (driver != nullptr && driver->StreamInfo(&info).ok()) {
				result.media_stream.interface = {generated::FesSimpleComputerInterfaceMediaBlobStreamID,
					generated::FesSimpleComputerInterfaceMediaBlobStreamMajor,
					generated::FesSimpleComputerInterfaceMediaBlobStreamMinor};
				result.media_stream.min_bytes = info.minimum;
				result.media_stream.max_bytes = info.maximum;
				result.media_stream.chunk_bytes = info.chunk_bytes;
			}
		}
	}
	return result;
}

Error NativeHardware::SetController(std::uint8_t port, std::uint16_t buttons,
	std::uint16_t keypad)
{
	if (active_driver_ != fes_gp_driver_ || fes_gp_driver_ == nullptr)
		return {ErrorCode::unsupported_interface, "controller ports are inactive", "input"};
	return static_cast<FesGpCoreDriver*>(fes_gp_driver_)->SetController(
		port, buttons, keypad, Deadline(clock_, timeouts_.core_io_ms));
}

Error NativeHardware::SetComputerKeyboard(std::uint64_t matrix)
{
	if (active_driver_ != fes_gp_driver_ || fes_gp_driver_ == nullptr)
		return {ErrorCode::unsupported_interface,
			"FES computer is not active", "input"};
	return static_cast<FesGpCoreDriver*>(fes_gp_driver_)->SetKeyboardMatrix(
		matrix, Deadline(clock_, timeouts_.core_io_ms));
}

Error NativeHardware::LoadComputerMedia(const std::string& path)
{
	if (active_driver_ != fes_gp_driver_ || fes_gp_driver_ == nullptr)
		return {ErrorCode::unsupported_interface,
			"FES computer is not active", "request"};
	std::vector<std::uint8_t> bytes;
	const Error admitted = ReadComputerMedia(path, &bytes);
	if (!admitted.ok()) return WithPhase(admitted, "request");
	return static_cast<FesGpCoreDriver*>(fes_gp_driver_)->LoadMedia(
		bytes, Deadline(clock_, timeouts_.core_io_ms));
}

Error NativeHardware::LoadComputerMediaLive(const std::string& path)
{
	if (active_driver_ != fes_gp_driver_ || fes_gp_driver_ == nullptr)
		return {ErrorCode::unsupported_interface,
			"FES computer is not active", "request"};
	std::vector<std::uint8_t> bytes;
	const Error admitted = ReadComputerMedia(path, &bytes);
	if (!admitted.ok()) return WithPhase(admitted, "request");
	return static_cast<FesGpCoreDriver*>(fes_gp_driver_)->LoadMediaLive(
		bytes, Deadline(clock_, timeouts_.core_io_ms));
}

Error NativeHardware::ClearComputerMedia()
{
	if (active_driver_ != fes_gp_driver_ || fes_gp_driver_ == nullptr)
		return {ErrorCode::unsupported_interface,
			"FES computer is not active", "request"};
	return static_cast<FesGpCoreDriver*>(fes_gp_driver_)->ClearMedia(
		Deadline(clock_, timeouts_.core_io_ms));
}

Error NativeHardware::LoadComputerFirmware(const std::string& path)
{
	if (active_driver_ != fes_gp_driver_ || fes_gp_driver_ == nullptr)
		return {ErrorCode::unsupported_interface,
			"FES firmware slot is not active", "request"};
	std::vector<std::uint8_t> bytes;
	const Error admitted = ReadComputerMedia(path, &bytes);
	if (!admitted.ok()) return WithPhase(admitted, "request");
	if (bytes.size() != generated::FesApplicationFirmwareBytes)
		return {ErrorCode::invalid_request, "FES firmware size is invalid", "request"};
	return static_cast<FesGpCoreDriver*>(fes_gp_driver_)->LoadFirmware(
		bytes, Deadline(clock_, timeouts_.core_io_ms));
}

Error NativeHardware::LoadComputerMediaStream(const std::string& path, std::uint32_t size)
{
	if (active_driver_ != fes_gp_driver_ || fes_gp_driver_ == nullptr)
		return {ErrorCode::unsupported_interface, "FES computer stream is not active", "request"};
	auto& driver = *static_cast<FesGpCoreDriver*>(fes_gp_driver_);
	MediaStreamInfo info;
	Error error = driver.StreamInfo(&info);
	if (!error.ok()) return error;
	if (size < info.minimum || size > info.maximum)
		return {ErrorCode::invalid_request, "size exceeds observed media stream capacity", "request"};
	// One budget includes file snapshot/CRC and every transfer exchange.
	const auto deadline = Deadline(clock_, timeouts_.media_io_ms);
	ComputerMediaSnapshot snapshot;
	error = snapshot.Prepare(path, info.minimum, info.maximum, clock_, deadline);
	if (!error.ok()) return WithPhase(error, "request");
	if (snapshot.size() != size)
		return {ErrorCode::invalid_request, "media size does not match request", "request"};
	error = driver.LoadMediaStream(snapshot, clock_, deadline);
	if (!error.ok()) {
		// Cleanup is independently bounded; a poisoned mailbox performs no writes.
		const auto cleanup = driver.AbortMediaStream(Deadline(clock_, timeouts_.core_io_ms));
		if (!cleanup.ok()) {
			error.message += "; media stream cleanup failed: " + cleanup.message;
			error.phase = "recovery";
		}
	}
	return error;
}

Error NativeHardware::AttachProgrammedBitstream(AdmittedCorePackage* package,
	const std::string& path, const std::string& sha256)
{
	NativeAdmittedCore* admitted = dynamic_cast<NativeAdmittedCore*>(package);
	if (admitted == nullptr || sha256.size() != 64)
		return {ErrorCode::invalid_request, "programmed bitstream is invalid", "admission"};
	Artifact artifact;
	Error error = OpenRBFArtifact(path, opener_, &artifact);
	if (!error.ok()) return error;
	std::string digest;
	error = HashOpenedArtifact(artifact, &digest);
	if (!error.ok() || digest != sha256)
		return {ErrorCode::invalid_request, "programmed bitstream does not match its receipt", "admission"};
	admitted->rom_link_ = {};
	admitted->programmed_ = std::move(artifact);
	admitted->programmed_sha256_ = sha256;
	admitted->has_programmed_ = true;
	return {};
}

Error NativeHardware::AttachROMBitstream(AdmittedCorePackage* package,
	const std::string& path, const CoreROMLink& link)
{
	auto* admitted = dynamic_cast<NativeAdmittedCore*>(package);
	auto digest = [](const std::string& value) {
		return value.size() == 64 && std::all_of(value.begin(), value.end(),
			[](char c) { return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'); });
	};
	if (!admitted || admitted->opened_.descriptor.format != 3)
		return {ErrorCode::invalid_request, "ROM link requires format 3", "admission"};
	const auto& rom = admitted->opened_.descriptor.rom;
	if (link.rom_id != rom.id || link.map_sha256 != rom.sha256 ||
		link.source_size != rom.source_size || !digest(link.map_sha256) ||
		!digest(link.source_sha256) || !digest(link.programmed_sha256) || link.programmed_size == 0)
		return {ErrorCode::invalid_request, "ROM link does not match package", "admission"};
	const Error attached = AttachProgrammedBitstream(package, path, link.programmed_sha256);
	if (!attached.ok()) return attached;
	if (admitted->programmed_.size() != link.programmed_size) {
		admitted->has_programmed_ = false;
		return {ErrorCode::invalid_request, "ROM programmed size does not match receipt", "admission"};
	}
	admitted->rom_link_ = link;
	return {};
}

Error NativeHardware::RecheckProgrammedBitstream(AdmittedCorePackage* package)
{
	auto* admitted = dynamic_cast<NativeAdmittedCore*>(package);
	if (!admitted) return {ErrorCode::invalid_request, "invalid admitted package", "admission"};
	if (admitted->opened_.descriptor.format == 3 &&
		(!admitted->has_programmed_ || admitted->rom_link_.rom_id.empty()))
		return {ErrorCode::unsupported_abi, "format-3 activation requires a bound ROM link", "admission"};
	if (admitted->has_programmed_) {
		std::string digest;
		const Error error = HashOpenedArtifact(admitted->programmed_, &digest);
		if (!error.ok() || digest != admitted->programmed_sha256_)
			return {ErrorCode::invalid_request, "programmed bitstream changed after admission", "admission"};
	}
	return {};
}

HardwareResult NativeHardware::LoadCore(
	std::unique_ptr<AdmittedCorePackage> package, std::uint64_t generation)
{
	NativeAdmittedCore* admitted = dynamic_cast<NativeAdmittedCore*>(package.get());
	if (admitted == nullptr || admitted->driver_ == nullptr)
		return {{ErrorCode::invalid_request,
			"invalid admitted core package", "request"}, false, ""};
	Error error = RecheckCorePackage(admitted->opened_);
	if (error.ok() && admitted->composition_) error=RecheckCoreComposition(*admitted->composition_);
	if (error.ok()) error = CheckCoreCompatibility(admitted->opened_.descriptor);
	if (!error.ok()) return {error, false, ""};
	ProgrammingProfile checked_profile;
	CoreDriver* checked_driver = nullptr;
	error = driver_registry_.Resolve(admitted->opened_.descriptor,
		&checked_profile, &checked_driver);
	if (!error.ok() || checked_profile != admitted->profile_ ||
		checked_driver != admitted->driver_)
		return {{ErrorCode::unsupported_abi,
			"admitted core driver changed before activation", "compatibility"},
			false, ""};

	error = RecheckProgrammedBitstream(package.get());
	if (!error.ok()) return {error, false, ""};
	const bool fes_gp = admitted->profile_ == ProgrammingProfile::fes_gp_v1;
	const bool fes_gamepad = fes_gp &&
		RequiresFesGamepad(admitted->opened_.descriptor);
	error = StopInput(Deadline(clock_, timeouts_.core_io_ms));
	log_.Write({"load_core", admitted->opened_.descriptor.core.system,
		admitted->opened_.descriptor.core.id, "input_stop", error});
	if (!error.ok()) return {WithPhase(error, "input"), true, ""};
	std::shared_ptr<std::atomic<bool>> identity_verified;
	if (fes_gamepad) {
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
		if (!error.ok()) return {WithPhase(error, "input"), true, ""};
		input_open_ = true;
		input_delivery_enabled_ = identity_verified;
	}
	const HardwareResult quiesced = QuiesceForReplacement("load_core",
		admitted->opened_.descriptor.core.system,
		admitted->opened_.descriptor.core.id);
	if (!quiesced.error.ok()) {
		const Error stopped = StopInput(Deadline(clock_, timeouts_.core_io_ms));
		return {stopped.ok() ? quiesced.error : WithPhase(stopped, "input"),
			quiesced.mutation_attempted, quiesced.observed_core};
	}
	const Artifact* bitstream = &admitted->opened_.payload;
	if (admitted->has_programmed_) bitstream = &admitted->programmed_;
	else if (admitted->composition_) bitstream = &admitted->composition_->payload;
	const NativeResult programmed = fpga_.Program(*bitstream,
		admitted->profile_, Deadline(clock_, timeouts_.program_ms));
	ObserveProgram("load_core", programmed);
	if (!programmed.error.ok()) {
		const Error stopped = StopInput(Deadline(clock_, timeouts_.core_io_ms));
		if (programmed.mutation_attempted) ForgetActiveCore();
		return {stopped.ok() ? ProgramError(programmed.error) :
			WithPhase(stopped, "input"),
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
	CoreDriverResult identified = admitted->driver_->Identify(
		admitted->context_, Deadline(clock_, timeouts_.core_io_ms));
	ObserveIdentify(admitted->context_.expected_core, identified);
	if (!identified.error.ok()) {
		const Error stopped = StopInput(Deadline(clock_, timeouts_.core_io_ms));
		if (fes_gp && !identified.safe_to_quiesce)
			ForgetActiveCore();
		if (!stopped.ok()) identified.error = WithPhase(stopped, "input");
		else {
			const char* const phase =
				identified.error.code == ErrorCode::core_mismatch ?
				"identity" : "transport";
			identified.error = WithPhase(std::move(identified.error), phase);
		}
		return {identified.error, true, identified.observed_core};
	}
	if (admitted->data_file_) {
		error = admitted->driver_->RestoreData(admitted->context_,
			{admitted->data_.paddle_speed, admitted->data_.best_rally},
			Deadline(clock_, timeouts_.core_io_ms));
		if (!error.ok())
			return {WithPhase(error, "core_data"), true, identified.observed_core};
	}
	if (fes_gp) {
		if (identity_verified)
			identity_verified->store(true);
		bool audio = false;
		if (admitted->opened_.descriptor.abi.id == generated::FesApplicationABIID)
			for (const auto& interface : admitted->opened_.descriptor.interfaces)
				if (interface.id == generated::FesApplicationInterfaceAudioPcmS16Stereo48kID &&
					interface.required && interface.major == 1 && interface.minor == 0)
					audio = true;
		// Identity already proved the exact declared/live capability set.
		const VideoResult video = game_video_.BringUpCustom(
			Deadline(clock_, timeouts_.video_ms), audio);
		log_.Write({"load_core", admitted->opened_.descriptor.core.system,
			identified.observed_core, "video", video.error});
		if (!video.error.ok()) {
			const Error stopped = StopInput(Deadline(clock_, timeouts_.core_io_ms));
			return {stopped.ok() ? CoreIoError(video.error, "video") :
				WithPhase(stopped, "input"),
				true, identified.observed_core};
		}
		if (fes_gamepad) {
			error = input_.Neutralize(Deadline(clock_, timeouts_.core_io_ms));
			if (!error.ok()) return {CoreIoError(error, "input"), true,
				identified.observed_core};
		}
		CoreDriverResult started = admitted->driver_->Start(admitted->context_,
			Deadline(clock_, timeouts_.core_io_ms));
		if (!started.error.ok()) return {WithPhase(std::move(started.error),
			"transport"), true, identified.observed_core};
		if (fes_gamepad) {
			error = input_.Start(generation,
				[this](std::uint64_t reported_generation, Error fault) {
					ForwardInputFault(reported_generation, std::move(fault));
				});
			if (!error.ok()) return {CoreIoError(error, "input"), true,
				identified.observed_core};
		}
	}
	if (fes_gamepad) {
		active_input_recipe_ = FesGpInputRecipe();
		has_active_input_recipe_ = true;
	} else {
		has_active_input_recipe_ = false;
	}
	core_data_file_ = std::move(admitted->data_file_);
	durable_data_ = admitted->data_;
	core_snapshot_.clear();
	core_data_flushed_ = false;
	return {{}, true, identified.observed_core};
}

HardwareResult NativeHardware::LoadIdle()
{
	// Startup/fault/failed-launch cleanup deliberately has no save side effect.
	core_data_file_.reset();
	core_snapshot_.clear();
	core_data_flushed_ = false;
	durable_data_ = {};
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
		idle_recipe_.programming_profile,
		Deadline(clock_, timeouts_.program_ms));
	ObserveProgram("start", programmed);
	error = programmed.error.ok() ? Error{} : ProgramError(programmed.error);
	log_.Write({"start", "", "", "program", error});
	if (!error.ok()) {
		if (programmed.mutation_attempted) ForgetActiveCore();
		return {input_error.ok() ? error : input_error,
			quiesce_mutation || programmed.mutation_attempted, ""};
	}
	const VideoResult video = idle_video_.BringUp(idle_recipe_,
		Deadline(clock_, timeouts_.video_ms));
	if (!video.error.ok())
		return {input_error.ok() ? CoreIoError(video.error) : input_error,
			true, video.observed_core};
	active_driver_ = ResolveDriver(idle_recipe_.programming_profile);
	active_package_.reset();
	active_context_ = {};
	active_context_.expected_core = idle_recipe_.expected_core;
	has_active_input_recipe_ = false;
	return {input_error, true, video.observed_core};
}


HardwareResult NativeHardware::LoadContainedDevelopmentRBF(const std::string& rbf,
	std::uint64_t generation)
{
	const auto profile = ProgrammingProfile::development_contained_v1;
	Artifact artifact;
	Error error = OpenRBFArtifact(rbf, opener_, &artifact);
	log_.Write({"load_development_rbf", "", "", "preflight", error});
	if (!error.ok()) return {WithPhase(error, "admission"), false, ""};
	const HardwareResult quiesced = QuiesceForReplacement(
		"load_development_rbf", "", "");
	error = quiesced.error;
	if (!error.ok()) return {error, quiesced.mutation_attempted, ""};
	const NativeResult programmed = fpga_.Program(artifact,
		profile,
		Deadline(clock_, timeouts_.program_ms));
	ObserveProgram("load_development_rbf", programmed);
	error = programmed.error.ok() ? Error{} : ProgramError(programmed.error);
	log_.Write({"load_development_rbf", "", "", "program", error});
	if (!error.ok()) {
		if (programmed.mutation_attempted) ForgetActiveCore();
		return {error, quiesced.mutation_attempted ||
			programmed.mutation_attempted, ""};
	}

	CoreDriver* driver = ResolveDriver(profile);
	if (driver == nullptr)
		return {{ErrorCode::unsupported_abi,
			"core driver is unavailable", "compatibility"},
			true, ""};
	CoreDriverContext context;
	context.generation = generation;
	CoreDriverResult identified = driver->Identify(context,
		Deadline(clock_, timeouts_.core_io_ms));
	ObserveIdentify(context.expected_core, identified);
	error = identified.error;
	if (!error.ok() && error.code == ErrorCode::core_mismatch)
		error = WithPhase(std::move(error), "identity");
	else if (!error.ok()) error = CoreIoError(error, "transport");
	log_.Write({"load_development_rbf", "", identified.observed_core,
		"probe", error});
	if (error.ok()) {
		active_driver_ = driver;
		active_package_.reset();
		active_context_ = context;
		has_active_input_recipe_ = false;
	}
	return {error, true, identified.observed_core};
}

} // namespace native
} // namespace mister
