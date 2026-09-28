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
#include "native/generated/fes_computer.hpp"
#include "native/input.hpp"
#include "native/menu_display.hpp"
#include "native/menu_underflow.hpp"
#include "native/linux/menu_memory.hpp"
#include <thread>
#include <chrono>
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

bool HasMenu(const CoreDescriptor& descriptor) {
 for(const auto& interface:descriptor.interfaces)
  if(interface.id==generated::FesApplicationInterfaceVideoMenuDisplayID)return true;
 return false;
}
Error CheckMenu(const CoreDescriptor& descriptor) {
 if(descriptor.format!=2||descriptor.core.id!="fes.menu"||!descriptor.core.system.empty()||
  descriptor.abi.id!=generated::FesApplicationABIID||descriptor.interfaces.size()!=3||!HasMenu(descriptor))
  return {ErrorCode::invalid_package,"idle menu requires the described fes.menu video/DDR/menu package","admission"};
 return CheckCoreCompatibility(descriptor);
}

// Admission accepts this interface only as a required application declaration.
bool RequiresHpsDdr(const CoreDescriptor& descriptor)
{
	if (descriptor.abi.id != generated::FesApplicationABIID) return false;
	for (const CoreInterface& interface : descriptor.interfaces)
		if (interface.id == generated::FesApplicationInterfaceMemoryHpsDdrID &&
			interface.required &&
			interface.major == generated::FesApplicationInterfaceMemoryHpsDdrMajor &&
			interface.minor == generated::FesApplicationInterfaceMemoryHpsDdrMinor)
			return true;
	return false;
}

// The SDR controller keeps the port layout U-Boot latched from its boot
// core; under any other layout every DDR command is refused.
Error BootHpsDdrLayoutError()
{
	return {ErrorCode::unsupported_interface,
		"fes.memory.hps-ddr needs the boot core to latch its port layout; "
		"this card's U-Boot idle core does not"};
}

// A live layout mismatch is a core that does not implement its declared
// interface. MMIO failures stay with the FPGA manager's programming phase.
Error HpsDdrError(Error error)
{
	const char* const phase = error.code == ErrorCode::core_mismatch ?
		"identity" : "programming";
	return WithPhase(std::move(error), phase);
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
	CoreROMLinks rom_links_;
	CoreComposition composition() const override { return composition_ ? composition_->info : CoreComposition{}; }
};

} // namespace

NativeHardware::NativeHardware(ArtifactOpener& opener, FpgaManager& fpga,
	VideoBringup& idle_video, FixedVideoBringup& game_video,
	InputSession& input, const InputDeviceIdentity& input_identity, Clock& clock,
	LogSink& log, std::string idle_rbf, NativeTimeouts timeouts,
	CoreDriver* fes_gp_driver, std::vector<std::string> package_roots,
	IdleRecipe idle_recipe,MenuDisplayDriver* menu_display,MenuMemory* menu_memory)
	: menu_display_(menu_display),menu_memory_(menu_memory),opener_(opener), fpga_(fpga), idle_video_(idle_video),
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
	// The in-flight frame copy stops between rows once quiesce or programming begins.
	CancelMenuCopies();
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
 if(!driver.error.ok()&&menu_status_.available){menu_unsafe_=true;menu_status_.available=false;menu_directory_.clear();menu_package_id_.clear();}
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
	if (error.ok() && RequiresHpsDdr(opened.descriptor) && !fpga_.BootHpsDdrLayout())
		error = BootHpsDdrLayoutError();
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
	if (compatibility.ok() && RequiresHpsDdr(opened.descriptor) &&
		!fpga_.BootHpsDdrLayout())
		compatibility = BootHpsDdrLayoutError();
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
			{generated::FesSimpleComputerInterfaceAudioPcmS16Stereo48kID,
				generated::FesSimpleComputerInterfaceAudioPcmS16Stereo48kMajor,
				generated::FesSimpleComputerInterfaceAudioPcmS16Stereo48kMinor},
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
		// Advertised only when the boot core latched the port layout.
		if (fpga_.BootHpsDdrLayout())
			application.interfaces.push_back(
				{generated::FesApplicationInterfaceMemoryHpsDdrID, 1, 0});
		if(fpga_.BootHpsDdrLayout()&&menu_display_&&menu_memory_)
   application.interfaces.push_back({generated::FesApplicationInterfaceVideoMenuDisplayID,1,0});
		std::sort(application.interfaces.begin(), application.interfaces.end(),
			[](const SupportedInterface& a, const SupportedInterface& b) { return a.id < b.id; });
		result.abis.insert(result.abis.begin(), std::move(application));
		SupportedABI computer_io;
		computer_io.id = generated::FesComputerABIID;
		computer_io.major = generated::FesComputerABIMajor;
		computer_io.minor = generated::FesComputerABIMinor;
		computer_io.interfaces = {
			{kApple2ExpansionBusID, 1, 0},
			{generated::FesComputerInterfaceAudioPcmS16Stereo48kID,
				generated::FesComputerInterfaceAudioPcmS16Stereo48kMajor,
				generated::FesComputerInterfaceAudioPcmS16Stereo48kMinor},
			{generated::FesComputerInterfaceGamepadPortsID,
				generated::FesComputerInterfaceGamepadPortsMajor,
				generated::FesComputerInterfaceGamepadPortsMinor},
			{generated::FesComputerInterfaceKeyboardHidID,
				generated::FesComputerInterfaceKeyboardHidMajor,
				generated::FesComputerInterfaceKeyboardHidMinor},
			{generated::FesComputerInterfaceMediaApple2FloppyID,
				generated::FesComputerInterfaceMediaApple2FloppyMajor,
				generated::FesComputerInterfaceMediaApple2FloppyMinor},
			{generated::FesComputerInterfaceMediaSpectrumTapeID,
				generated::FesComputerInterfaceMediaSpectrumTapeMajor,
				generated::FesComputerInterfaceMediaSpectrumTapeMinor},
			{kSpectrumExpansionBusID, 1, 0},
			{generated::FesComputerInterfaceVideoFixed720p60ID,
				generated::FesComputerInterfaceVideoFixed720p60Major,
				generated::FesComputerInterfaceVideoFixed720p60Minor}};
		std::sort(computer_io.interfaces.begin(), computer_io.interfaces.end(),
			[](const SupportedInterface& a, const SupportedInterface& b) { return a.id < b.id; });
		result.abis.insert(result.abis.begin(), std::move(computer_io));
		std::sort(result.abis.begin(), result.abis.end(),
			[](const SupportedABI& a, const SupportedABI& b) { return a.id < b.id; });
		if (active_driver_ == fes_gp_driver_) {
			MediaStreamInfo info;
			const auto* driver = dynamic_cast<FesGpCoreDriver*>(fes_gp_driver_);
			if (driver != nullptr && driver->home_computer())
				result.media_units = driver->media_units();
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

Error NativeHardware::SetKeyboardHid(const KeyboardHidRows& rows)
{
	auto* driver = active_driver_ == fes_gp_driver_ ?
		dynamic_cast<FesGpCoreDriver*>(fes_gp_driver_) : nullptr;
	if (driver == nullptr)
		return {ErrorCode::unsupported_interface, "FES computer is not active", "input"};
	return driver->SetKeyboardHid(rows, Deadline(clock_, timeouts_.core_io_ms));
}

Error NativeHardware::InsertComputerMedia(std::uint8_t unit, const std::string& path,
	std::uint32_t size)
{
	auto* driver = active_driver_ == fes_gp_driver_ ?
		dynamic_cast<FesGpCoreDriver*>(fes_gp_driver_) : nullptr;
	const MediaUnitCapability* info = nullptr;
	if (driver != nullptr)
		for (const auto& candidate : driver->media_units())
			if (candidate.unit == unit) info = &candidate;
	if (info == nullptr)
		return {ErrorCode::unsupported_interface, "FES computer media unit is not active", "request"};
	if (size < info->min_bytes || size > info->max_bytes)
		return {ErrorCode::invalid_request, "size is outside the observed media unit limits", "request"};
	// One budget covers the snapshot, CRC and every transfer exchange. Execution
	// stays released throughout; cleanup has its own bounded deadline.
	const auto deadline = Deadline(clock_, timeouts_.media_io_ms);
	ComputerMediaSnapshot snapshot;
	Error error = snapshot.Prepare(path, info->min_bytes, info->max_bytes, clock_, deadline);
	if (!error.ok()) return WithPhase(error, "request");
	if (snapshot.size() != size)
		return {ErrorCode::invalid_request, "media size does not match request", "request"};
	return driver->InsertMedia(unit, snapshot, clock_, deadline, timeouts_.core_io_ms);
}

Error NativeHardware::EjectComputerMedia(std::uint8_t unit)
{
	auto* driver = active_driver_ == fes_gp_driver_ ?
		dynamic_cast<FesGpCoreDriver*>(fes_gp_driver_) : nullptr;
	if (driver == nullptr)
		return {ErrorCode::unsupported_interface, "FES computer media unit is not active", "request"};
	return driver->EjectMedia(unit, Deadline(clock_, timeouts_.core_io_ms));
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
	admitted->rom_links_ = {};
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

Error NativeHardware::AttachROMsBitstream(AdmittedCorePackage* package,
	const std::string& path, const CoreROMLinks& links)
{
	auto* admitted = dynamic_cast<NativeAdmittedCore*>(package);
	auto digest = [](const std::string& value) {
		return value.size() == 64 && std::all_of(value.begin(), value.end(),
			[](char c) { return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'); });
	};
	if (!admitted || admitted->opened_.descriptor.format != 4)
		return {ErrorCode::invalid_request, "two-source ROM link requires format 4", "admission"};
	const auto& descriptor = admitted->opened_.descriptor;
	if (links.sources.size() != 2 || descriptor.roms.size() != 2 ||
		links.map_sha256 != descriptor.rom_map.sha256 || !digest(links.map_sha256) ||
		!digest(links.programmed_sha256) || links.programmed_size == 0)
		return {ErrorCode::invalid_request, "two-source ROM link does not match package", "admission"};
	for (std::size_t i = 0; i < 2; ++i) {
		const auto& observed = links.sources[i];
		const auto& required = descriptor.roms[i];
		if (observed.id != required.id || observed.role != required.role ||
			observed.source_size != required.source_size || !digest(observed.source_sha256))
			return {ErrorCode::invalid_request, "ROM source identity does not match package", "admission"};
	}
	const Error attached = AttachProgrammedBitstream(package, path, links.programmed_sha256);
	if (!attached.ok()) return attached;
	if (admitted->programmed_.size() != links.programmed_size) {
		admitted->has_programmed_ = false;
		return {ErrorCode::invalid_request, "ROM programmed size does not match receipt", "admission"};
	}
	admitted->rom_links_ = links;
	return {};
}

Error NativeHardware::RecheckProgrammedBitstream(AdmittedCorePackage* package)
{
	auto* admitted = dynamic_cast<NativeAdmittedCore*>(package);
	if (!admitted) return {ErrorCode::invalid_request, "invalid admitted package", "admission"};
	if (admitted->opened_.descriptor.format == 3 &&
		(!admitted->has_programmed_ || admitted->rom_link_.rom_id.empty()))
		return {ErrorCode::unsupported_abi, "format-3 activation requires a bound ROM link", "admission"};
	if (admitted->opened_.descriptor.format == 4 &&
		(!admitted->has_programmed_ || admitted->rom_links_.sources.size() != 2))
		return {ErrorCode::unsupported_abi, "format-4 activation requires two bound ROM sources", "admission"};
	if (admitted->has_programmed_) {
		std::string digest;
		const Error error = HashOpenedArtifact(admitted->programmed_, &digest);
		if (!error.ok() || digest != admitted->programmed_sha256_)
			return {ErrorCode::invalid_request, "programmed bitstream changed after admission", "admission"};
	}
	return {};
}

HardwareResult NativeHardware::LoadCore(std::unique_ptr<AdmittedCorePackage> package,std::uint64_t generation)
{return LoadCoreInternal(std::move(package),generation,false);}

HardwareResult NativeHardware::LoadCoreInternal(
 std::unique_ptr<AdmittedCorePackage> package,std::uint64_t generation,bool allow_menu)
{
	NativeAdmittedCore* admitted = dynamic_cast<NativeAdmittedCore*>(package.get());
	if (admitted == nullptr || admitted->driver_ == nullptr)
		return {{ErrorCode::invalid_request,
			"invalid admitted core package", "request"}, false, ""};
 const bool menu=HasMenu(admitted->opened_.descriptor);
 if(menu&&!allow_menu)return {{ErrorCode::unsupported_interface,"menu packages require idle configuration","admission"},false,""};
 if(menu&&(!menu_display_||!menu_memory_))return {{ErrorCode::unsupported_interface,"menu presentation adapter unavailable","admission"},false,""};
 if(menu){const auto checked=CheckMenu(admitted->opened_.descriptor);if(!checked.ok())return {checked,false,""};}
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
 menu_status_.available=false;
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
 menu_unsafe_=false;
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
	if (RequiresHpsDdr(admitted->opened_.descriptor)) {
		// Identity proved capability bit 8. The ports leave reset only when the
		// live fpga2sdram inputs also match, and before execution is released.
		error = fpga_.ReleaseHpsDdrPorts(Deadline(clock_, timeouts_.core_io_ms));
		log_.Write({"load_core", admitted->opened_.descriptor.core.system,
			identified.observed_core, "hps_ddr", error});
		if (!error.ok()) {
			const Error stopped = StopInput(Deadline(clock_, timeouts_.core_io_ms));
			return {stopped.ok() ? HpsDdrError(std::move(error)) :
				WithPhase(stopped, "input"), true, identified.observed_core};
		}
	}
 if(menu) {
  error=menu_memory_->InitializeBlack();
  if(error.ok()){menu_memory_->AllowCopies();menu_underflow_streak_=0;}
  if(error.ok())error=menu_display_->Configure(Deadline(clock_,timeouts_.core_io_ms));
  if(!error.ok()){menu_unsafe_=true;return {error,true,identified.observed_core};}
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
		// Application and computer audio are the same interface and contract.
		const std::string& abi = admitted->opened_.descriptor.abi.id;
		const char* const audio_id = abi == generated::FesComputerABIID ?
			generated::FesComputerInterfaceAudioPcmS16Stereo48kID :
			abi == generated::FesSimpleComputerABIID ?
			generated::FesSimpleComputerInterfaceAudioPcmS16Stereo48kID :
			generated::FesApplicationInterfaceAudioPcmS16Stereo48kID;
		if (abi == generated::FesApplicationABIID || abi == generated::FesComputerABIID ||
			abi == generated::FesSimpleComputerABIID)
			for (const auto& interface : admitted->opened_.descriptor.interfaces)
				if (interface.id == audio_id &&
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
  if(menu) {
   error=menu_display_->Enable(Deadline(clock_,timeouts_.core_io_ms));
   MenuDisplayInfo info;if(error.ok())error=menu_display_->ReadInfo(Deadline(clock_,timeouts_.core_io_ms),&info);
   // Programming and port reset clear the underflow register (initial value 0;
   // fes_menu_video.v also clears it on rst). There is no GP clear. A nonzero
   // count here is a fault in this image, not history from the previous core.
   if(error.ok()&&(!info.enabled||!info.configured||info.quiesced||info.pending||info.faulted||info.displayed_sequence||info.underflows))
    error={ErrorCode::io_failed,"menu initial state is not safe black","menu"};
   if(!error.ok()){menu_unsafe_=true;return {error,true,identified.observed_core};}
   menu_status_={};menu_status_.available=true;menu_status_.package_id=admitted->opened_.package_id;
   menu_status_.geometry=info.geometry;menu_slot_=0;
  }
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

HardwareResult NativeHardware::ConfigureMenuPackage(const std::string& directory,const std::string& id)
{
 std::unique_ptr<AdmittedCorePackage> package;Error error=AdmitCorePackage(directory,id,&package);
 if(!error.ok())return {error,false,""};
 error=CheckMenu(package->info().descriptor);if(!error.ok())return {error,false,""};
 const auto result=LoadCoreInternal(std::move(package),0,true);
 if(result.error.ok()){
  menu_directory_=directory;menu_package_id_=id;
  menu_underflow_streak_=0;menu_reactivations_=0;menu_reactivation_window_start_=0;
 }
 else if(result.mutation_attempted){menu_directory_.clear();menu_package_id_.clear();menu_status_.available=false;menu_status_.error=result.error;}
 return result;
}
HardwareResult NativeHardware::LoadIdle()
{
 if(!menu_directory_.empty()) {
  std::unique_ptr<AdmittedCorePackage> package;Error error=AdmitCorePackage(menu_directory_,menu_package_id_,&package);
  HardwareResult result;
  if(error.ok())error=CheckMenu(package->info().descriptor);
  if(error.ok())result=LoadCoreInternal(std::move(package),0,true);else result={error,false,""};
  if(result.error.ok())return result;
  menu_directory_.clear();menu_package_id_.clear();
  const auto fallback=LoadSplashIdle();menu_status_.available=false;menu_status_.error=result.error;
  return fallback;
 }
 return LoadSplashIdle();
}
HardwareResult NativeHardware::LoadSplashIdle()
{
	CancelMenuCopies();
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
	if (active_driver_ != nullptr && !menu_unsafe_) {
		const CoreDriverResult driver = active_driver_->Quiesce(active_context_,
			Deadline(clock_, timeouts_.core_io_ms));
		quiesce_mutation = quiesce_mutation || driver.mutation_attempted;
		log_.Write({"start", "", "", "driver_quiesce", driver.error});
  if(!driver.error.ok()) {
   if(!menu_status_.available)return {input_error.ok()?driver.error:input_error,quiesce_mutation,driver.observed_core};
   menu_unsafe_=true;menu_status_.error=driver.error;
  }
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
 menu_unsafe_=false;menu_status_.available=false;
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


void NativeHardware::CancelMenuCopies()
{if(menu_memory_)menu_memory_->CancelCopies();}
bool NativeHardware::MenuReactivationAllowed()
{
 const auto now=clock_.NowMs();
 if(menu_reactivation_window_start_==0||now-menu_reactivation_window_start_>=kMenuReactivationWindowMs){
  menu_reactivation_window_start_=now;menu_reactivations_=0;
 }
 return menu_reactivations_<kMenuReactivationLimit;
}
Error NativeHardware::FailMenuPresent(const Error& error)
{
 // The runtime's present failure path calls LoadIdle. Keeping the package
 // selects menu reactivation; clearing it selects splash. The counter resets
 // on that program, so the streak does not carry into the new image.
 menu_underflow_streak_=0;menu_status_.available=false;menu_status_.error=error;
 if(!menu_directory_.empty()&&MenuReactivationAllowed()){++menu_reactivations_;return error;}
 menu_unsafe_=true;menu_directory_.clear();menu_package_id_.clear();return error;
}
Error NativeHardware::PresentMenuFrame(const MenuFrame& frame,MenuDisplayInfo* output)
{
 if(!output||!menu_status_.available||menu_unsafe_||!menu_display_||!menu_memory_)
  return {ErrorCode::busy,"menu presentation unavailable","menu"};
 const auto deadline=Deadline(clock_,timeouts_.core_io_ms);
 MenuDisplayInfo info;Error error=menu_display_->ReadInfo(deadline,&info);
 // History accumulated before this present is not a reason to reject it.
 const auto baseline=error.ok()?info.underflows:0;
 if(error.ok()&&(!info.enabled||!info.configured||info.pending||info.quiesced||info.faulted||
  info.displayed_sequence!=menu_status_.displayed_sequence))
  error={ErrorCode::io_failed,"menu slot ownership or scanout state changed","menu"};
 if(error.ok()&&info.displayed_sequence==std::numeric_limits<std::uint32_t>::max())
  error={ErrorCode::io_failed,"menu sequence exhausted","menu"};
 const auto slot=std::uint8_t(menu_slot_^1);
 const auto sequence=info.displayed_sequence+1;
 if(error.ok())error=menu_memory_->CopyRgba(slot,frame);
 if(error.ok())error=menu_display_->Submit(slot,sequence,deadline);
 while(error.ok()) {
  error=menu_display_->ReadInfo(deadline,&info);if(!error.ok())break;
  if(!info.enabled||!info.configured||info.quiesced||info.faulted||
   (info.displayed_sequence!=menu_status_.displayed_sequence&&info.displayed_sequence!=sequence)) {
   error={ErrorCode::io_failed,"menu completion or scanout fault","menu"};break;
  }
  if(info.displayed_sequence==sequence&&!info.pending){
   const auto delta=info.underflows>=baseline?info.underflows-baseline:info.underflows;
   if(delta>kMenuUnderflowPresentCap){error={ErrorCode::io_failed,"menu underflow exceeded the per-present cap","menu"};break;}
   if(delta>0){if(++menu_underflow_streak_>=kMenuUnderflowSustainPresents){error={ErrorCode::io_failed,"menu underflow persisted across presents","menu"};break;}}
   else menu_underflow_streak_=0;
   menu_slot_=slot;menu_status_.displayed_sequence=sequence;menu_status_.underflows=delta;info.underflows=delta;*output=info;return {};
  }
  if(clock_.NowMs()>=deadline){error={ErrorCode::io_failed,"menu display completion timed out","menu"};break;}
  std::this_thread::sleep_for(std::chrono::milliseconds(1));
 }
 return FailMenuPresent(error);
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
	menu_status_.available=false;
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
