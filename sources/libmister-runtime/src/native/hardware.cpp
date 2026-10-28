// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/hardware.hpp"

#include "native/artifacts.hpp"
#include "native/sha256.hpp"
#include "native/core_driver.hpp"
#include "native/core_package.hpp"
#include "native/core_composition.hpp"
#include "native/ata_disk.hpp"
#include "native/core_data.hpp"
#include "native/media_data.hpp"
#include "native/diagnostic.hpp"
#include "native/fes_gp.hpp"
#include "native/generated/fes_gp.hpp"
#include "native/generated/fes_simple_computer.hpp"
#include "native/generated/fes_application.hpp"
#include "native/generated/fes_computer.hpp"
#include "native/generated/fes_display_plane.hpp"
#include "native/input.hpp"
#include "native/menu_admission.hpp"
#include "native/menu_display.hpp"
#include "native/menu_underflow.hpp"
#include "native/linux/menu_window.hpp"
#include <thread>
#include <fcntl.h>
#include <unistd.h>
#include <chrono>
#include "native/video.hpp"

#include <algorithm>
#include <cstring>
#include <array>
#include <atomic>
#include <cctype>
#include <cerrno>
#include <cstdlib>
#include <limits.h>
#include <limits>
#include <memory>
#include <string>
#include <sys/stat.h>
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

bool Zx81CaptureDirectory(const std::string& path, std::string* directory)
{
	if (path.empty() || path.size() > 4095 || path.find('\0') != std::string::npos)
		return false;
	const std::size_t slash = path.rfind('/');
	if (slash == std::string::npos || path.substr(slash + 1) != "program.p")
		return false;
	const std::string parent = path.substr(0, slash);
	const std::size_t parent_slash = parent.rfind('/');
	const std::string name = parent.substr(parent_slash == std::string::npos ? 0 : parent_slash + 1);
	const std::string prefix = "fogcast-zx81-tape-";
	if (name.compare(0, prefix.size(), prefix) != 0)
		return false;
	char resolved[PATH_MAX];
	if (realpath(parent.c_str(), resolved) == nullptr || parent != resolved)
		return false;
	struct stat info;
	if (stat(parent.c_str(), &info) != 0 || !S_ISDIR(info.st_mode) ||
		info.st_uid != geteuid() || (info.st_mode & 0777) != 0700)
		return false;
	*directory = parent;
	return true;
}

bool HasMenu(const CoreDescriptor& descriptor) {
 for(const auto& interface:descriptor.interfaces)
  if(interface.id==generated::FesApplicationInterfaceVideoMenuDisplayID)return true;
 return false;
}
bool HasMenuBlitter(const CoreDescriptor& descriptor) {
 for(const auto& interface:descriptor.interfaces)
  if(interface.id==generated::FesApplicationInterfaceVideoMenuBlitterID)return true;
 return false;
}
std::uint16_t MenuBlitterMinor(const CoreDescriptor& descriptor) {
 for(const auto& interface:descriptor.interfaces)
  if(interface.id==generated::FesApplicationInterfaceVideoMenuBlitterID)return std::uint16_t(interface.minor);
 return 0;
}
bool HasSessionDisplay(const CoreDescriptor& descriptor) {
 if(descriptor.abi.id!=generated::FesSimpleComputerABIID&&
  descriptor.abi.id!=generated::FesComputerABIID&&
  descriptor.abi.id!=generated::FesApplicationABIID)return false;
 for(const auto& interface:descriptor.interfaces)
  if((interface.id==generated::FesSimpleComputerInterfaceVideoSessionDisplayID||
   interface.id==generated::FesComputerInterfaceVideoSessionDisplayID||
   interface.id==generated::FesApplicationInterfaceVideoSessionDisplayID)&&
   interface.required&&interface.major==1&&interface.minor==0)return true;
 return false;
}
bool HasNativeVideoSocket(const CoreDescriptor& descriptor)
{
	for (const auto& interface : descriptor.interfaces)
		if (interface.id == "fes.fabric.video.native-pixels") return true;
	return false;
}
} // namespace

// Kit software packing reads the records from this section of the stripped binary.
#if defined(__ELF__)
#define MISTER_MENU_INTERFACE_SECTION __attribute__((section(".fes_menu_interfaces"), used))
#else
#define MISTER_MENU_INTERFACE_SECTION __attribute__((used))
#endif
#define MISTER_MENU_INTERFACE_RECORD(id, major, minor) id "=" #major "." #minor "\0"
const char kMenuInterfaceRecords[] MISTER_MENU_INTERFACE_SECTION =
	MISTER_MENU_INTERFACES(MISTER_MENU_INTERFACE_RECORD);

Error CheckMenu(const CoreDescriptor& descriptor) {
 if(descriptor.format!=2||descriptor.core.id!="fes.menu"||!descriptor.core.system.empty()||
  descriptor.abi.id!=generated::FesApplicationABIID||descriptor.interfaces.size()!=(HasMenuBlitter(descriptor)?4u:3u)||!HasMenu(descriptor))
  return {ErrorCode::invalid_package,"idle menu requires the described fes.menu video/DDR/menu package","admission"};
 return CheckCoreCompatibility(descriptor);
}

namespace {

// Applications and simple computers share the boot-latched DDR port layout.
bool RequiresHpsDdr(const CoreDescriptor& descriptor)
{
	if (descriptor.abi.id != generated::FesApplicationABIID &&
		descriptor.abi.id != generated::FesSimpleComputerABIID &&
		descriptor.abi.id != generated::FesComputerABIID) return false;
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

struct PreparedInitialMedia {
	ComputerMediaSnapshot base;
	std::unique_ptr<ComputerMediaSnapshot> restored;
	std::unique_ptr<MediaDataFile> file;
	MediaDiskRecord record;
};

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
	std::unique_ptr<PreparedInitialMedia> initial_media_;
	std::unique_ptr<PreparedAtaDisk> ata_disk_;
	CoreComposition composition() const override { return composition_ ? composition_->info : CoreComposition{}; }
};

} // namespace

NativeHardware::NativeHardware(ArtifactOpener& opener, FpgaManager& fpga,
	VideoBringup& idle_video, FixedVideoBringup& game_video,
	InputSession& input, const InputDeviceIdentity& input_identity, Clock& clock,
	LogSink& log, std::string idle_rbf, NativeTimeouts timeouts,
	CoreDriver* fes_gp_driver, std::vector<std::string> package_roots,
	IdleRecipe idle_recipe,MenuDisplayDriver* menu_display,MenuWindow* menu_window,
	DiskWindow* disk_window)
	: menu_display_(menu_display),menu_window_(menu_window),disk_window_(disk_window),opener_(opener), fpga_(fpga), idle_video_(idle_video),
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
		if (input_identity_verified_)
			input_identity_verified_->store(false);
		if (input_delivery_enabled_)
			input_delivery_enabled_->store(false);
		input_identity_verified_.reset();
		input_delivery_enabled_.reset();
		return {};
	}
	const Error error = input_.Stop(deadline);
	if (input_identity_verified_)
		input_identity_verified_->store(false);
	if (input_delivery_enabled_)
		input_delivery_enabled_->store(false);
	input_identity_verified_.reset();
	input_delivery_enabled_.reset();
	input_open_ = false;
	return error;
}

Error NativeHardware::FlushSave()
{
	auto* driver = active_driver_ == fes_gp_driver_ ?
		dynamic_cast<FesGpCoreDriver*>(fes_gp_driver_) : nullptr;
	if (driver != nullptr && driver->TapeWriteCapable()) {
		const Error tape = driver->EnsureZx81TapeSaved(Deadline(clock_, timeouts_.media_io_ms));
		if (!tape.ok()) return tape;
	}
    const Error media = FlushMediaSave();
    if (!media.ok()) return media;
    const Error disk = FlushAtaDiskSave();
    if (!disk.ok()) return disk;
	if (core_data_file_) {
		if (core_data_flushed_)
			return {};
		Error error = StopInput(Deadline(clock_, timeouts_.core_io_ms));
		if (error.ok() && core_snapshot_.empty())
			error = active_driver_->CaptureData(
				active_context_, Deadline(clock_, timeouts_.core_io_ms), &core_snapshot_);
		const std::size_t words = durable_data_.game_id.empty() ?
			generated::FesGpPongProgressWordCount : generated::FesComputerPc386MachineWordCount;
		if (error.ok() && core_snapshot_.size() != words)
			error = {ErrorCode::save_failed, "incomplete core-data snapshot", "core_data"};
		if (error.ok()) {
			CoreData snapshot = durable_data_;
			if (snapshot.game_id.empty()) {
				snapshot.paddle_speed = core_snapshot_[generated::FesGpPongPaddleSpeedIndex];
				snapshot.best_rally = core_snapshot_[generated::FesGpPongBestRallyIndex];
			} else {
				snapshot.words = core_snapshot_;
			}
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

Error NativeHardware::FlushFaultSave()
{
    // StopInput joins/closes delivery even when its final neutral command
    // fails on the poisoned mailbox. Capture then explicitly reidentifies
    // that mailbox; no stale asynchronous writer remains eligible.
    (void)StopInput(Deadline(clock_, timeouts_.core_io_ms));
    return FlushSave();
}

Error NativeHardware::RestoreInput(std::uint64_t generation)
{
    if (media_data_file_) {
        auto* driver = active_driver_ == fes_gp_driver_ ? dynamic_cast<FesGpCoreDriver*>(fes_gp_driver_) : nullptr;
        if (!driver || generation == 0) return {ErrorCode::io_failed,"active media cannot be resumed","media_data"};
        // A destructive command may have committed before its response was
        // lost. Read the state without repeating that command or allowing a
        // frozen old image to change before its ownership is confirmed.
        const Error observed = driver->RefreshMediaState(durable_media_.identity.unit, Deadline(clock_,timeouts_.core_io_ms));
        if (!observed.ok()) return observed;
        for (const auto& unit : driver->media_units()) if (unit.unit == durable_media_.identity.unit) {
            if (unit.state == MediaUnitState::empty) { ForgetMediaData(); return {}; }
            if (unit.state != MediaUnitState::ready)
                return {ErrorCode::io_failed,"retained media state is unresolved","media_data"};
        }
        if (media_image_uncertain_) {
            std::vector<unsigned char> observed_image;
            const Error captured = driver->CaptureMedia(durable_media_.identity.unit,clock_,
                Deadline(clock_,timeouts_.media_io_ms),&observed_image);
            if (!captured.ok()) return captured;
            if (observed_image != media_snapshot_)
                return {ErrorCode::io_failed,"retained media image differs after failed replacement","media_data"};
        }
        const Error resumed = driver->ResumeMedia(durable_media_.identity.unit, Deadline(clock_,timeouts_.core_io_ms));
        if (!resumed.ok()) return resumed;
        media_image_uncertain_ = false;
        media_snapshot_.clear(); media_data_flushed_ = false;
        return {};
    }
	if (ata_delta_file_) {
		auto* driver = active_driver_ == fes_gp_driver_ ? dynamic_cast<FesGpCoreDriver*>(fes_gp_driver_) : nullptr;
		if (!driver || generation == 0) return {ErrorCode::io_failed, "active ATA disk cannot be resumed", "media_data"};
		const Error resumed = driver->AtaDiskControl(generated::FesComputerDiskControlResume, Deadline(clock_, timeouts_.core_io_ms));
		if (!resumed.ok()) return resumed;
		ata_flushed_ = false;
	}
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
	const std::shared_ptr<std::atomic<bool>> identity_verified =
		std::make_shared<std::atomic<bool>>(false);
	const std::shared_ptr<std::atomic<bool>> delivery_enabled =
		std::make_shared<std::atomic<bool>>(!session_display_focused_);
	Error error = input_.Open(input_identity_, active_input_recipe_,
		Deadline(clock_, timeouts_.core_io_ms),
		[driver, context, identity_verified, delivery_enabled](std::uint16_t map,
			std::uint64_t deadline) {
			if (!identity_verified->load() ||
				(map != 0 && !delivery_enabled->load())) return Error{};
			return driver->SetButtons(context, map, deadline).error;
		});
	if (!error.ok()) return WithPhase(std::move(error), "input");
	input_open_ = true;
	input_identity_verified_ = identity_verified;
	input_delivery_enabled_ = delivery_enabled;
	identity_verified->store(true);
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
    ForgetMediaData();
    ForgetAtaDisk();
	session_display_focused_ = false;
	active_driver_ = nullptr;
	active_context_ = {};
	active_package_.reset();
	has_active_input_recipe_ = false;
}

HardwareResult NativeHardware::QuiesceForReplacement(const char* operation,
	const std::string& system, const std::string& core)
{
	// No process writes the menu window once quiesce or programming begins.
	RevokeMenuWindow();
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
	return AdmitCorePackageInternal(directory, expected_id, false, output);
}

Error NativeHardware::AdmitCorePackageInternal(const std::string& directory,
	const std::string& expected_id, bool for_composition,
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
	if (error.ok() && HasNativeVideoSocket(opened.descriptor) && !for_composition)
		error = {ErrorCode::unsupported_interface,
			"native video shell requires a video parts composition", "admission"};
	if (error.ok() && RequiresHpsDdr(opened.descriptor) && !fpga_.BootHpsDdrLayout())
		error = BootHpsDdrLayoutError();
	if (error.ok() && HasSessionDisplay(opened.descriptor) && (!menu_display_ || !menu_window_))
		error = {ErrorCode::unsupported_interface, "session presentation adapter unavailable", "admission"};
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
	Error error=AdmitCorePackageInternal(directory,id,true,&package);
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
	if (compatibility.ok() && HasSessionDisplay(opened.descriptor) && (!menu_display_ || !menu_window_))
		compatibility = {ErrorCode::unsupported_interface, "session presentation adapter unavailable", "admission"};
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

Error NativeHardware::PrepareCoreData(AdmittedCorePackage* package, const std::string& root,
	const std::string& game_id, CoreData* output)
{
	return PrepareCoreDataInternal(package, root, game_id, output, true);
}

Error NativeHardware::PrepareCoreDataInternal(AdmittedCorePackage* package,
	const std::string& root, const std::string& game_id, CoreData* output, bool writable)
{
	auto admitted = dynamic_cast<NativeAdmittedCore*>(package);
	if (!admitted || !output)
		return {ErrorCode::invalid_request, "invalid admitted core-data request"};
	VersionedContract layout;
	Error error = CorePersistenceLayout(admitted->opened_.descriptor, &layout);
	if (!error.ok())
		return error;
	const std::string& id = admitted->opened_.descriptor.core.id;
	// A fes.computer record belongs to one library game; Pong's to its core.
	const bool per_game = !layout.id.empty() &&
		admitted->opened_.descriptor.abi.id == generated::FesComputerABIID;
	if (per_game ? !ValidLibraryGameID(game_id) : !game_id.empty())
		return {ErrorCode::invalid_request, "core-data game identity does not fit the package", "core_data"};
	if (core_data_file_ && durable_data_.core_id == id && layout.id.empty())
		return {ErrorCode::incompatible_data, "active namespace requires persistence", "core_data"};
	std::unique_ptr<CoreDataFile> file;
	error = per_game ? CoreDataFile::OpenGame(root, id, game_id, &file) :
		CoreDataFile::Open(root, id, &file);
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
Error NativeHardware::InspectCoreData(const std::string& path, const std::string& id,
	const std::string& root, const std::string& game_id, CoreData* output)
{
	std::unique_ptr<AdmittedCorePackage> admitted;
	// Library parts preflight inspects a native shell's namespace without
	// activating it. Composition remains mandatory on every native load path.
	Error error = AdmitCorePackageInternal(path, id, true, &admitted);
	if (error.ok())
		error = PrepareCoreDataInternal(admitted.get(), root, game_id, output, false);
	return error;
}
Error NativeHardware::UpdateCoreSettings(const std::string& path, const std::string& id,
	const std::string& root, const std::string& game_id, const std::string& revision,
	const CoreSettingsUpdate& update, CoreData* output)
{
	if (!update.pc386 && update.paddle_speed > generated::FesGpPongPaddleSpeedFast)
		return {ErrorCode::invalid_request, "invalid paddle speed", "request"};
	std::unique_ptr<AdmittedCorePackage> package;
	Error error = AdmitCorePackage(path, id, &package);
	if (!error.ok())
		return error;
	if (active_package_ && active_package_->info().declared_core == package->info().declared_core &&
		(game_id.empty() || durable_data_.game_id == game_id))
		return {ErrorCode::busy, "core-data namespace is active", "core_data"};
	CoreData data;
	error = PrepareCoreData(package.get(), root, game_id, &data);
	if (!error.ok())
		return error;
	auto admitted = dynamic_cast<NativeAdmittedCore*>(package.get());
	if (!admitted->data_file_)
		return {
			ErrorCode::unsupported_interface, "package has no persistent settings", "core_data"};
	if (update.pc386 != !data.game_id.empty())
		return {ErrorCode::invalid_request, "settings do not match the package's layout", "request"};
	if (update.pc386) {
		// Settings for the next launch; the machine's CMOS words stay.
		const Pc386MachineSettings& m = update.machine;
		const std::uint16_t settings[] = {m.preset, m.frequency, m.code_profile, m.turbo,
			m.sb_base, m.sb_irq, m.sb_dma, 0};
		std::copy(std::begin(settings), std::end(settings), data.words.begin());
		if (!ValidPc386Machine(data.words))
			return {ErrorCode::invalid_request, "invalid pc386 machine settings", "request"};
	} else {
		data.paddle_speed = update.paddle_speed;
	}
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
		if (fpga_.BootHpsDdrLayout())
			computer.interfaces.push_back({generated::FesSimpleComputerInterfaceMemoryHpsDdrID,1,0});
		if (fpga_.BootHpsDdrLayout() && menu_display_ && menu_window_) {
			computer.interfaces.push_back({generated::FesSimpleComputerInterfaceVideoSessionDisplayID,1,0});
			computer.interfaces.push_back({kDisplayPlaneInterfaceID,
				generated::FesDisplayPlaneMajor, generated::FesDisplayPlaneMinor});
		}
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
		if(fpga_.BootHpsDdrLayout()&&menu_display_&&menu_window_) {
   application.interfaces.push_back({generated::FesApplicationInterfaceVideoMenuDisplayID,1,0});
   application.interfaces.push_back({generated::FesApplicationInterfaceVideoMenuBlitterID,1,generated::FesApplicationInterfaceVideoMenuBlitterMinor});
   application.interfaces.push_back({generated::FesApplicationInterfaceVideoSessionDisplayID,1,0});
   application.interfaces.push_back({kDisplayPlaneInterfaceID,
    generated::FesDisplayPlaneMajor, generated::FesDisplayPlaneMinor});
  }
		std::sort(application.interfaces.begin(), application.interfaces.end(),
			[](const SupportedInterface& a, const SupportedInterface& b) { return a.id < b.id; });
		result.abis.insert(result.abis.begin(), std::move(application));
		SupportedABI computer_io;
		computer_io.id = generated::FesComputerABIID;
		computer_io.major = generated::FesComputerABIMajor;
		computer_io.minor = 0;
		computer_io.interfaces = {
			{kApple2ExpansionBusID, 1, 0},
			{kAtariStExpansionBusID, 1, 0},
			{kC64ExpansionBusID, 1, 0},
			{generated::FesComputerInterfaceAudioPcmS16Stereo48kID,
				generated::FesComputerInterfaceAudioPcmS16Stereo48kMajor,
				generated::FesComputerInterfaceAudioPcmS16Stereo48kMinor},
			{generated::FesComputerInterfaceGamepadPortsID,
				generated::FesComputerInterfaceGamepadPortsMajor,
				generated::FesComputerInterfaceGamepadPortsMinor},
			{generated::FesComputerInterfaceMouseRelativeID,
				generated::FesComputerInterfaceMouseRelativeMajor,
				generated::FesComputerInterfaceMouseRelativeMinor},
			{generated::FesComputerInterfaceKeyboardHidID,
				generated::FesComputerInterfaceKeyboardHidMajor,
				generated::FesComputerInterfaceKeyboardHidMinor},
			{generated::FesComputerInterfaceMediaApple2FloppyID,
				generated::FesComputerInterfaceMediaApple2FloppyMajor,
				generated::FesComputerInterfaceMediaApple2FloppyMinor},
			{generated::FesComputerInterfaceMediaC64DiskID,
				generated::FesComputerInterfaceMediaC64DiskMajor,
				generated::FesComputerInterfaceMediaC64DiskMinor},
			{generated::FesComputerInterfaceMediaAtariStFloppyID,
				generated::FesComputerInterfaceMediaAtariStFloppyMajor,
				generated::FesComputerInterfaceMediaAtariStFloppyMinor},
			{generated::FesComputerInterfaceMediaAtariStFloppyGeometryID, 1, 0},
            {generated::FesComputerInterfaceMediaAtariStFloppyWriteID,
                generated::FesComputerInterfaceMediaAtariStFloppyWriteMajor,
                generated::FesComputerInterfaceMediaAtariStFloppyWriteMinor},
			{generated::FesComputerInterfaceMediaSpectrumTapeID,
				generated::FesComputerInterfaceMediaSpectrumTapeMajor,
				generated::FesComputerInterfaceMediaSpectrumTapeMinor},
			{generated::FesComputerInterfaceMediaSpectrumDiskID,
				generated::FesComputerInterfaceMediaSpectrumDiskMajor,
				generated::FesComputerInterfaceMediaSpectrumDiskMinor},
			{"fes.media.spectrum-snapshot", 1, 0},
			{"fes.media.spectrum-snapshot-128", 1, 0},
			{"fes.media.spectrum-snapshot-plus3", 1, 0},
			{generated::FesComputerInterfaceMediaZx81TapeID,
				generated::FesComputerInterfaceMediaZx81TapeMajor,
				generated::FesComputerInterfaceMediaZx81TapeMinor},
			{generated::FesComputerInterfaceMediaZx81TapeWriteID,
				generated::FesComputerInterfaceMediaZx81TapeWriteMajor,
				generated::FesComputerInterfaceMediaZx81TapeWriteMinor},
			{kSpectrumExpansionBusID, 1, 0},
			{generated::FesComputerInterfaceVideoFixed720p60ID,
				generated::FesComputerInterfaceVideoFixed720p60Major,
				generated::FesComputerInterfaceVideoFixed720p60Minor}};
		// DDR follows the latched boot layout. Session display also needs both
		// presentation adapters, matching admission of each interface. Its
		// shared plane is advertised with it.
		if (fpga_.BootHpsDdrLayout())
			computer_io.interfaces.push_back(
				{generated::FesComputerInterfaceMemoryHpsDdrID, 1, 0});
		if (fpga_.BootHpsDdrLayout() && menu_display_ && menu_window_) {
			computer_io.interfaces.push_back(
				{generated::FesComputerInterfaceVideoSessionDisplayID, 1, 0});
			computer_io.interfaces.push_back({kDisplayPlaneInterfaceID,
				generated::FesDisplayPlaneMajor, generated::FesDisplayPlaneMinor});
		}
		std::sort(computer_io.interfaces.begin(), computer_io.interfaces.end(),
			[](const SupportedInterface& a, const SupportedInterface& b) { return a.id < b.id; });
		// 1.1 adds identity word 16 and its interfaces. Each minor is its own
		// entry, so a 1.0 package keeps matching its exact ABI.
		SupportedABI computer_11 = computer_io;
		computer_11.minor = generated::FesComputerABIMinor;
		computer_11.interfaces.push_back({generated::FesComputerInterfacePersistenceWordsID, 1, 0});
		computer_11.interfaces.push_back({generated::FesComputerInterfacePc386MachineID, 1, 0});
		computer_11.interfaces.push_back({generated::FesComputerInterfaceRtcWallClockID, 1, 0});
		if (fpga_.BootHpsDdrLayout() && disk_window_)
			computer_11.interfaces.push_back({generated::FesComputerInterfaceMediaAtaDiskID, 1, 0});
		std::sort(computer_11.interfaces.begin(), computer_11.interfaces.end(),
			[](const SupportedInterface& a, const SupportedInterface& b) { return a.id < b.id; });
		result.abis.insert(result.abis.begin(), std::move(computer_11));
		result.abis.insert(result.abis.begin(), std::move(computer_io));
		std::sort(result.abis.begin(), result.abis.end(),
			[](const SupportedABI& a, const SupportedABI& b) {
				return a.id != b.id ? a.id < b.id : a.minor < b.minor; });
		if (active_driver_ == fes_gp_driver_) {
			MediaStreamInfo info;
			const auto* driver = dynamic_cast<FesGpCoreDriver*>(fes_gp_driver_);
			if (driver != nullptr && driver->home_computer())
				result.media_units = driver->media_units();
            if (driver != nullptr && driver->home_computer() && driver->AtaDiskCapable()) {
                MediaUnitCapability unit;
                unit.unit = generated::FesComputerAtaDiskUnit;
                unit.interface = {generated::FesComputerInterfaceMediaAtaDiskID, 1, 0};
                unit.min_bytes = generated::FesComputerAtaDiskMinBytes;
                unit.max_bytes = generated::FesComputerAtaDiskMaxBytes;
                unit.chunk_bytes = generated::FesComputerAtaDiskChunkBytes;
                unit.state = ata_ready_ ? MediaUnitState::ready : MediaUnitState::empty;
                if (ata_delta_file_) {
                    unit.persistence_mode = "persistent"; unit.game_id = ata_identity_.game_id;
                    unit.base_media_id = ata_identity_.base_media_id; unit.revision = ata_delta_.revision;
                }
                result.media_units.insert(result.media_units.begin(), unit);
            }
            if (media_data_file_) for (auto& unit : result.media_units) {
                if (unit.unit == durable_media_.identity.unit) {
                    unit.persistence_mode="persistent"; unit.game_id=durable_media_.identity.game_id;
                    unit.base_media_id=durable_media_.identity.base_media_id; unit.revision=durable_media_.revision;
                }
            }
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
	if (session_display_focused_ && (buttons != 0 || keypad != 0))
		return {}; // UI focus never sends navigation buttons to the running machine.
	if (active_driver_ != fes_gp_driver_ || fes_gp_driver_ == nullptr)
		return {ErrorCode::unsupported_interface, "controller ports are inactive", "input"};
	return static_cast<FesGpCoreDriver*>(fes_gp_driver_)->SetController(
		port, buttons, keypad, Deadline(clock_, timeouts_.core_io_ms));
}

// ReleaseControllers lets go of every controller port. A core without
// controller ports has nothing to release.
Error NativeHardware::ReleaseControllers()
{
	const std::uint8_t ports = active_package_->info().descriptor.abi.id == generated::FesComputerABIID ?
		std::uint8_t(generated::FesComputerControllerPortCount) :
		std::uint8_t(generated::FesApplicationControllerPortCount);
	for (std::uint8_t port = 0; port < ports; ++port) {
		const Error error = SetController(port, 0, 0);
		if (error.code == ErrorCode::unsupported_interface) return {};
		if (!error.ok()) return error;
	}
	return {};
}

Error NativeHardware::SetComputerKeyboard(std::uint64_t matrix)
{
	if (matrix >> 40) return {ErrorCode::invalid_request,"invalid computer keyboard matrix","input"};
	if (session_display_focused_ && matrix != ((std::uint64_t(1)<<40)-1))
		return {}; // UI focus never sends navigation keys to the running machine.
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

Error NativeHardware::SendMouseRelative(std::int16_t dx, std::int16_t dy, std::uint8_t buttons)
{
	auto* driver = active_driver_ == fes_gp_driver_ ? dynamic_cast<FesGpCoreDriver*>(fes_gp_driver_) : nullptr;
	if (!driver) return {ErrorCode::unsupported_interface, "FES computer is not active", "input"};
	return driver->SendMouseRelative(dx, dy, buttons, Deadline(clock_, timeouts_.core_io_ms));
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
	if (info->interface.id == generated::FesComputerInterfaceMediaAtariStFloppyID) {
        std::array<unsigned char, 512> boot{};
        error = snapshot.Read(0, boot.data(), boot.size(), clock_, deadline);
        if (!error.ok()) return WithPhase(error, "request");
        if (!AtariStBaseBpbValid(boot.data(), boot.size(), size))
            return {ErrorCode::invalid_request, "Atari ST base BPB disagrees with geometry", "request"};
    }
	error = FlushMediaSave();
    if (!error.ok()) return error;
    const Error inserted = driver->InsertMedia(unit,snapshot,clock_,
        Deadline(clock_,timeouts_.media_io_ms),timeouts_.core_io_ms);
    // Successful insert or confirmed failure-eject retires the old binding.
    if (inserted.ok()) ForgetMediaData();
    else for (const auto& state : driver->media_units())
        if (state.unit==unit && state.state==MediaUnitState::empty) ForgetMediaData();
    if (!inserted.ok() && media_data_file_) {
        media_image_uncertain_ = true;
        return {ErrorCode::save_failed,inserted.message,"media_data"};
    }
    return inserted;
}

Error NativeHardware::CaptureZx81Tape(const std::string& path)
{
	auto* driver = active_driver_ == fes_gp_driver_ ?
		dynamic_cast<FesGpCoreDriver*>(fes_gp_driver_) : nullptr;
	if (driver == nullptr || !driver->TapeWriteCapable())
		return {ErrorCode::unsupported_interface, "ZX81 tape save is inactive", "request"};
	std::string directory;
	if (!Zx81CaptureDirectory(path, &directory))
		return {ErrorCode::invalid_request, "invalid ZX81 tape capture destination", "request"};
	std::vector<unsigned char> bytes;
	Error error = driver->CaptureZx81Tape(clock_, Deadline(clock_, timeouts_.media_io_ms), &bytes);
	if (!error.ok()) return error;
	const int dir_fd = open(directory.c_str(), O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
	if (dir_fd < 0) return {ErrorCode::save_failed, "cannot open ZX81 tape capture directory", "media_data"};
	struct stat directory_info;
	if (fstat(dir_fd, &directory_info) != 0 || !S_ISDIR(directory_info.st_mode) ||
		directory_info.st_uid != geteuid() || (directory_info.st_mode & 0777) != 0700) {
		close(dir_fd);
		return {ErrorCode::invalid_request, "unsafe ZX81 tape capture directory", "request"};
	}
	const int fd = openat(dir_fd, "program.p",
		O_CREAT | O_WRONLY | O_EXCL | O_CLOEXEC | O_NOFOLLOW, 0600);
	if (fd < 0) {
		close(dir_fd);
		return {ErrorCode::save_failed, "cannot create ZX81 tape capture", "media_data"};
	}
	std::size_t at = 0;
	while (at < bytes.size()) {
		const ssize_t wrote = write(fd, bytes.data() + at, bytes.size() - at);
		if (wrote < 0 && errno == EINTR) continue;
		if (wrote <= 0) {
			close(fd);
			unlinkat(dir_fd, "program.p", 0);
			close(dir_fd);
			return {ErrorCode::save_failed, "cannot write ZX81 tape", "media_data"};
		}
		at += static_cast<std::size_t>(wrote);
	}
	const bool file_synced = fsync(fd) == 0;
	const bool file_closed = close(fd) == 0;
	if (!file_synced || !file_closed || fsync(dir_fd) != 0) {
		unlinkat(dir_fd, "program.p", 0);
		close(dir_fd);
		return {ErrorCode::save_failed, "cannot sync ZX81 tape", "media_data"};
	}
	close(dir_fd);
	return {};
}

Error NativeHardware::AcknowledgeZx81Tape()
{
	auto* driver = active_driver_ == fes_gp_driver_ ?
		dynamic_cast<FesGpCoreDriver*>(fes_gp_driver_) : nullptr;
	if (driver == nullptr || !driver->TapeWriteCapable())
		return {ErrorCode::unsupported_interface, "ZX81 tape save is inactive", "request"};
	return driver->AcknowledgeZx81Tape(Deadline(clock_, timeouts_.media_io_ms));
}

Error NativeHardware::EjectComputerMedia(std::uint8_t unit)
{
	auto* driver = active_driver_ == fes_gp_driver_ ?
		dynamic_cast<FesGpCoreDriver*>(fes_gp_driver_) : nullptr;
	if (driver == nullptr)
		return {ErrorCode::unsupported_interface, "FES computer media unit is not active", "request"};
	const Error saved = FlushMediaSave();
    if (!saved.ok()) return saved;
    Error ejected = driver->EjectMedia(unit,Deadline(clock_,timeouts_.core_io_ms));
    if (ejected.ok()) ForgetMediaData();
    else if (media_data_file_) {
        ejected.code = ErrorCode::save_failed;
        ejected.phase = "media_data";
    }
    return ejected;
}

Error NativeHardware::PrepareAtaDisk(AdmittedCorePackage* package, const std::string& game_id,
	const AtaDiskImage& image)
{
	auto admitted = dynamic_cast<NativeAdmittedCore*>(package);
	if (!admitted) return {ErrorCode::invalid_request, "invalid admitted ATA disk request", "admission"};
	const CoreDescriptor& descriptor = admitted->opened_.descriptor;
	bool declared = false;
	for (const auto& interface : descriptor.interfaces)
		declared |= interface.id == generated::FesComputerInterfaceMediaAtaDiskID && interface.required;
	if (!declared || descriptor.abi.id != generated::FesComputerABIID)
		return {ErrorCode::unsupported_interface, "package has no ATA disk", "admission"};
	if (!ValidAtaDiskGeometry(image.size, image.cylinders, image.heads, image.sectors_per_track))
		return {ErrorCode::invalid_request, "invalid ATA disk size or geometry", "admission"};
	auto prepared = std::unique_ptr<PreparedAtaDisk>(new PreparedAtaDisk);
	prepared->image = image;
	prepared->identity = {descriptor.core.id, game_id, image.base_media_id, 0};
	if (!ValidMediaDataIdentity(prepared->identity))
		return {ErrorCode::invalid_request, "invalid ATA disk binding", "admission"};
	Error error = AtaBaseImage::Open(image.path, image.size, &prepared->base);
	if (error.ok()) error = AtaDeltaFile::Open(image.data_root, prepared->identity, image.size, &prepared->file);
	AtaDiskDelta delta;
	if (error.ok()) error = prepared->file->Read(&delta);
	if (error.ok()) error = prepared->file->CheckWritable();
	if (!error.ok()) return error;
	admitted->ata_disk_ = std::move(prepared);
	return {};
}

// Held, after identity: Prepare; the base image with the delta's chunks
// over it, written through the window in turn while the base's SHA-256 and
// the image's CRC-32 are computed; DiskCommit with that CRC; ready.
Error NativeHardware::LoadAtaDisk(FesGpCoreDriver& driver, PreparedAtaDisk& disk)
{
	const std::uint64_t deadline = Deadline(clock_, timeouts_.media_io_ms);
	Error error = disk.file->Read(&ata_delta_);
	if (error.ok()) error = driver.AtaDiskControl(generated::FesComputerDiskControlPrepare, deadline);
	if (!error.ok()) return error;
	constexpr std::uint32_t kChunk = generated::FesComputerAtaDiskChunkBytes;
	constexpr std::uint32_t kGrant = 64 * kChunk;  // 4 MiB a grant
	Sha256 base_hash;
	// CRC-32/IEEE a byte at a time: up to 128 MiB per launch on the HPS.
	std::uint32_t crc_table[256];
	for (std::uint32_t n = 0; n < 256; ++n) {
		std::uint32_t c = n;
		for (unsigned bit = 0; bit < 8; ++bit) c = (c >> 1) ^ ((c & 1u) ? 0xedb88320u : 0u);
		crc_table[n] = c;
	}
	std::uint32_t crc = 0xffffffffu;
	std::vector<unsigned char> chunk(kChunk), replaced;
	std::size_t next = 0;
	for (std::uint32_t offset = 0; offset < disk.image.size && error.ok(); offset += kGrant) {
		const std::uint32_t span = std::min(kGrant, disk.image.size - offset);
		// Grants are page aligned; the image is whole sectors, so round up.
		const std::uint32_t mapped = (span + 4095u) & ~4095u;
		error = disk_window_->Access(generated::FesComputerAtaDiskWindowOffset + offset, mapped,
			[&](unsigned char* window) {
				for (std::uint32_t at = 0; at < span; at += kChunk) {
					const std::uint32_t index = (offset + at) / kChunk;
					const std::uint32_t length = AtaDiskChunkLength(disk.image.size, index);
					Error read = disk.base->Read(offset + at, chunk.data(), length);
					if (!read.ok()) return read;
					base_hash.Update(chunk.data(), length);
					const unsigned char* bytes = chunk.data();
					if (next < ata_delta_.chunks.size() && ata_delta_.chunks[next].index == index) {
						read = disk.file->ReadChunk(ata_delta_.chunks[next++], &replaced);
						if (!read.ok()) return read;
						bytes = replaced.data();
					}
					std::memcpy(window + at, bytes, length);
					for (std::uint32_t i = 0; i < length; ++i)
						crc = crc_table[(crc ^ bytes[i]) & 0xffu] ^ (crc >> 8);
				}
				return Error{};
			});
	}
	if (error.ok() && Sha256Hex(base_hash.Final()) != disk.identity.base_media_id)
		error = {ErrorCode::corrupt_data, "ATA disk image differs from its base identity", "input"};
	if (error.ok())
		error = driver.CommitAtaDisk(disk.image.size / generated::FesComputerAtaDiskSectorBytes,
			disk.image.cylinders, disk.image.heads, disk.image.sectors_per_track, crc ^ 0xffffffffu, deadline);
	return error;
}

// Freeze; the dirty map; the dirty chunks read from the window and stored;
// the merged manifest published; Saved. The disk stays frozen after a
// failure until RestoreInput resumes it.
Error NativeHardware::FlushAtaDiskSave()
{
	if (!ata_delta_file_ || ata_flushed_) return {};
	auto* driver = active_driver_ == fes_gp_driver_ ? dynamic_cast<FesGpCoreDriver*>(fes_gp_driver_) : nullptr;
	if (!driver || !driver->AtaDiskCapable() || !disk_window_)
		return {ErrorCode::save_failed, "active ATA disk cannot be saved", "media_data"};
	const std::uint64_t deadline = Deadline(clock_, timeouts_.media_io_ms);
	std::vector<std::uint32_t> dirty;
	Error error = driver->AtaDiskControl(generated::FesComputerDiskControlFreeze, deadline);
	if (error.ok()) error = driver->ReadAtaDirty(deadline, &dirty);
	constexpr std::uint32_t kChunk = generated::FesComputerAtaDiskChunkBytes;
	std::vector<AtaDeltaChunk> changed;
	for (std::uint32_t index : dirty) {
		if (!error.ok()) break;
		const std::uint32_t length = AtaDiskChunkLength(ata_size_, index);
		if (!length) { error = {ErrorCode::save_failed, "dirty chunk past the ATA disk image", "media_data"}; break; }
		std::string sha256;
		error = disk_window_->Access(generated::FesComputerAtaDiskWindowOffset + std::uint64_t(index) * kChunk,
			kChunk, [&](unsigned char* window) { return ata_delta_file_->WriteChunk(window, length, &sha256); });
		if (error.ok()) changed.push_back({index, sha256});
	}
	AtaDiskDelta current;
	if (error.ok()) error = ata_delta_file_->Read(&current);
	if (error.ok() && !changed.empty())
		error = ata_delta_file_->Publish(MergeAtaDelta(current, changed), current.revision, &ata_delta_);
	if (error.ok()) error = driver->AtaDiskControl(generated::FesComputerDiskControlSaved, deadline);
	if (!error.ok()) {
		error.code = ErrorCode::save_failed;
		return WithPhase(error, "media_data");
	}
	ata_flushed_ = true;
	return {};
}

void NativeHardware::ForgetAtaDisk()
{
	ata_delta_file_.reset();
	ata_delta_ = {};
	ata_identity_ = {};
	ata_size_ = 0;
	ata_ready_ = false;
	ata_flushed_ = false;
}

void NativeHardware::ForgetMediaData()
{
    media_data_file_.reset(); media_snapshot_.clear(); durable_media_={}; media_base_size_=0; media_data_flushed_=false;
    media_image_uncertain_=false;
}
Error NativeHardware::FlushMediaSave()
{
    if (!media_data_file_ || media_data_flushed_) return {};
    auto* driver=active_driver_==fes_gp_driver_ ? dynamic_cast<FesGpCoreDriver*>(fes_gp_driver_) : nullptr;
    if(!driver) return {ErrorCode::save_failed,"active media snapshot unavailable","media_data"};
    const auto deadline=Deadline(clock_,timeouts_.media_io_ms);
    Error error;
    if(media_snapshot_.empty()) error=driver->CaptureMedia(durable_media_.identity.unit,clock_,deadline,&media_snapshot_);
    if(error.ok() && media_snapshot_.size()!=media_base_size_)
        error={ErrorCode::save_failed,"incomplete media snapshot","media_data"};
    if(error.ok()) {
        MediaDiskRecord current; error=media_data_file_->Read(&current);
        if(error.ok() && current.revision!=durable_media_.revision && current.bytes!=media_snapshot_)
            error={ErrorCode::stale_revision,"media-data revision changed","media_data"};
        if(error.ok()) {
            MediaDiskRecord snapshot=durable_media_; snapshot.bytes=media_snapshot_;
            error=media_data_file_->Persist(snapshot,current.revision,&durable_media_);
            if(!error.ok() && error.code==ErrorCode::save_failed) {
                // A rename can succeed before directory sync fails. Retain
                // that exact visible revision as ours while still reporting
                // uncertain durability. After Resume, new guest writes can
                // checkpoint against it instead of appearing concurrent.
                MediaDiskRecord visible;
                if(media_data_file_->Read(&visible).ok() && visible.bytes==media_snapshot_)
                    durable_media_=std::move(visible);
            }
        }
    }
    if(error.ok()) error=driver->MarkMediaSaved(durable_media_.identity.unit,deadline);
    if(!error.ok()) { error.code=ErrorCode::save_failed; return WithPhase(error,"media_data"); }
    media_data_flushed_=true; return {};
}
Error NativeHardware::InsertLibraryComputerMedia(std::uint8_t unit,const std::string& path,
    std::uint32_t size,const std::string& root,const MediaDataBinding& binding)
{
    auto* driver=active_driver_==fes_gp_driver_ ? dynamic_cast<FesGpCoreDriver*>(fes_gp_driver_) : nullptr;
    if(!driver || !active_context_.descriptor || unit!=0 || binding.unit!=unit || !AtariStSizeAdmitted(size,driver && driver->MediaGeometryCapable(unit)))
        return {ErrorCode::unsupported_interface,"library disk unit is unavailable","request"};
    MediaDataIdentity identity{active_context_.descriptor->core.id,binding.game_id,binding.base_media_id,unit};
    if(!ValidMediaDataIdentity(identity)) return {ErrorCode::invalid_request,"invalid library disk binding","request"};
    const auto deadline=Deadline(clock_,timeouts_.media_io_ms);
    ComputerMediaSnapshot base; Error error=base.Prepare(path,size,size,clock_,deadline);
    if(!error.ok()) return WithPhase(error,"request");
    Sha256 source; std::array<std::uint8_t,512> bytes{};
    for(std::uint32_t offset=0;offset<size && error.ok();offset+=bytes.size()) {
        error=base.Read(offset,bytes.data(),bytes.size(),clock_,deadline);
        if(error.ok() && offset==0 && !AtariStBaseBpbValid(bytes.data(),bytes.size(),size))
            return {ErrorCode::invalid_request,"Atari ST base BPB disagrees with geometry","request"};
        if(error.ok()) source.Update(bytes.data(),bytes.size());
    }
    if(!error.ok()) return WithPhase(error,"request");
    if(Sha256Hex(source.Final())!=binding.base_media_id)
        return {ErrorCode::invalid_request,"library disk source digest mismatch","request"};
    std::unique_ptr<MediaDataFile> incoming;
    error=MediaDataFile::Open(root,identity,&incoming);
    MediaDiskRecord record;
    if(error.ok()) error=incoming->Read(&record);
    if(!error.ok()) return WithPhase(error,"request");
    if(!record.bytes.empty() && record.bytes.size()!=size)
        return {ErrorCode::incompatible_data,"saved disk geometry differs from immutable base","request"};
    if(!driver->MediaWriteCapable(unit)) {
        if(record.revision!="absent") return {ErrorCode::incompatible_data,"library disk requires writable media contract","media_data"};
        return InsertComputerMedia(unit,path,size);
    }
    error=incoming->CheckWritable();
    if(!error.ok()) return WithPhase(error,"request");
    error=FlushMediaSave();
    if(!error.ok()) return error;
    // Outgoing and incoming disk can be the same namespace. Restore the
    // freshly published bytes, never the earlier preflight record.
    error=incoming->Read(&record);
    // Capture and insertion are separately bounded media phases. The old
    // image stays Frozen/Saved until the next Begin actually changes it.
    const auto insert_deadline=Deadline(clock_,timeouts_.media_io_ms);
    ComputerMediaSnapshot restored;
    if(error.ok() && !record.bytes.empty() && record.bytes.size()!=size)
        error={ErrorCode::incompatible_data,"saved disk geometry differs from immutable base","request"};
    if(error.ok() && !record.bytes.empty()) error=restored.PrepareBytes(record.bytes,clock_,insert_deadline);
    if(!error.ok()) {
        if(media_data_file_) { error.code=ErrorCode::save_failed;return WithPhase(error,"media_data"); }
        return WithPhase(error,"request");
    }
    error=driver->InsertMedia(unit,record.bytes.empty()?base:restored,clock_,insert_deadline,timeouts_.core_io_ms);
    if(!error.ok()) {
        for(const auto& state:driver->media_units())
            if(state.unit==unit && state.state==MediaUnitState::empty) ForgetMediaData();
        if(media_data_file_) {
            media_image_uncertain_=true;
            return {ErrorCode::save_failed,error.message,"media_data"};
        }
        return error;
    }
    media_data_file_=std::move(incoming); durable_media_=std::move(record); media_base_size_=size;
    media_snapshot_.clear(); media_data_flushed_=false; media_image_uncertain_=false;
    return {};
}

Error NativeHardware::PrepareInitialComputerMedia(AdmittedCorePackage* package,
	const InitialComputerMedia& request)
{
	auto* admitted = dynamic_cast<NativeAdmittedCore*>(package);
	if (!admitted) return {ErrorCode::invalid_request, "invalid admitted package", "admission"};
	const auto& descriptor = admitted->opened_.descriptor;
	bool floppy = false, writable = false, geometry = false;
	for (const auto& interface : descriptor.interfaces) {
		if (!interface.required || interface.major != 1 || interface.minor != 0) continue;
		if (interface.id == generated::FesComputerInterfaceMediaAtariStFloppyID) floppy = true;
		if (interface.id == generated::FesComputerInterfaceMediaAtariStFloppyWriteID) writable = true;
        if (interface.id == generated::FesComputerInterfaceMediaAtariStFloppyGeometryID) geometry = true;
	}
	if (descriptor.format != 3 || descriptor.rom.role != "firmware" ||
		descriptor.core.id != "fes.atari-st" || descriptor.abi.id != generated::FesComputerABIID ||
		!floppy || !writable)
		return {ErrorCode::unsupported_interface, "initial disk requires ROM-linked writable Atari ST", "admission"};
	MediaDataIdentity identity{descriptor.core.id, request.binding.game_id, request.binding.base_media_id,
		request.binding.unit};
	if (!AtariStSizeAdmitted(request.size, geometry) || request.binding.unit != 0 || !ValidMediaDataIdentity(identity))
		return {ErrorCode::invalid_request, "invalid initial disk identity or geometry", "admission"};
	std::unique_ptr<PreparedInitialMedia> prepared(new PreparedInitialMedia);
	const auto deadline = Deadline(clock_, timeouts_.media_io_ms);
	Error error = prepared->base.Prepare(request.path, request.size, request.size, clock_, deadline);
	Sha256 hash;
	std::array<std::uint8_t, 512> bytes{};
	for (std::uint32_t offset = 0; offset < request.size && error.ok(); offset += bytes.size()) {
		error = prepared->base.Read(offset, bytes.data(), bytes.size(), clock_, deadline);
		if (error.ok() && offset == 0 && !AtariStBaseBpbValid(bytes.data(), bytes.size(), request.size))
            return {ErrorCode::invalid_request, "Atari ST base BPB disagrees with geometry", "admission"};
        if (error.ok()) hash.Update(bytes.data(), bytes.size());
	}
	if (!error.ok()) return WithPhase(error, "admission");
	if (Sha256Hex(hash.Final()) != request.binding.base_media_id)
		return {ErrorCode::invalid_request, "initial disk source digest mismatch", "admission"};
	error = MediaDataFile::Open(request.data_root, identity, &prepared->file);
	if (error.ok()) error = prepared->file->Read(&prepared->record);
	if (error.ok() && !prepared->record.bytes.empty() && prepared->record.bytes.size() != request.size)
        error = {ErrorCode::incompatible_data, "saved disk geometry differs from immutable base", "admission"};
	if (error.ok()) error = prepared->file->CheckWritable();
	if (error.ok() && !prepared->record.bytes.empty()) {
		prepared->restored.reset(new ComputerMediaSnapshot);
		error = prepared->restored->PrepareBytes(prepared->record.bytes, clock_, deadline);
	}
	if (!error.ok()) return WithPhase(error, "admission");
	admitted->initial_media_ = std::move(prepared);
	return {};
}

Error NativeHardware::RefreshInitialComputerMedia(AdmittedCorePackage* package)
{
	auto* admitted = dynamic_cast<NativeAdmittedCore*>(package);
	if (!admitted || !admitted->initial_media_)
		return {ErrorCode::invalid_request, "initial disk is not prepared", "admission"};
	auto& prepared = *admitted->initial_media_;
	Error error = prepared.file->Read(&prepared.record);
	if (error.ok() && !prepared.record.bytes.empty() && prepared.record.bytes.size() != prepared.base.size())
        error = {ErrorCode::incompatible_data, "saved disk geometry differs from immutable base", "admission"};
	if (error.ok()) {
		std::unique_ptr<ComputerMediaSnapshot> restored;
		if (!prepared.record.bytes.empty()) {
			restored.reset(new ComputerMediaSnapshot);
			error = restored->PrepareBytes(prepared.record.bytes, clock_, Deadline(clock_, timeouts_.media_io_ms));
		}
		if (error.ok()) prepared.restored = std::move(restored);
	}
	return WithPhase(error, "admission");
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
	if (HasNativeVideoSocket(admitted->opened_.descriptor) &&
		(!admitted->composition_ ||
		admitted->composition_->info.layout != "fes.coleco-native-video.parts/1"))
		return {{ErrorCode::unsupported_interface,
			"native video shell requires a video parts composition", "admission"}, false, ""};
 const bool menu=HasMenu(admitted->opened_.descriptor);
 const bool session_display=HasSessionDisplay(admitted->opened_.descriptor);
 const bool blitter=menu&&HasMenuBlitter(admitted->opened_.descriptor)&&
  MenuBlitterMinor(admitted->opened_.descriptor)>=kMenuBlitterMinimumMinor;
 if(menu&&!allow_menu)return {{ErrorCode::unsupported_interface,"menu packages require idle configuration","admission"},false,""};
 if(menu&&(!menu_display_||!menu_window_))return {{ErrorCode::unsupported_interface,"menu presentation adapter unavailable","admission"},false,""};
 if(session_display&&(!menu_display_||!menu_window_))return {{ErrorCode::unsupported_interface,"session presentation adapter unavailable","admission"},false,""};
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
	std::shared_ptr<std::atomic<bool>> input_delivery_enabled;
	if (fes_gamepad) {
		identity_verified = std::make_shared<std::atomic<bool>>(false);
		input_delivery_enabled = std::make_shared<std::atomic<bool>>(true);
		CoreDriver* const input_driver = admitted->driver_;
		CoreDriverContext input_context;
		input_context.generation = generation;
		error = input_.Open(input_identity_, FesGpInputRecipe(),
			Deadline(clock_, timeouts_.core_io_ms),
			[input_driver, input_context, identity_verified, input_delivery_enabled](std::uint16_t map,
				std::uint64_t deadline) {
				// Identity admission gates every write. Focus suppresses nonzero
				// snapshots but still permits neutralization.
				if (!identity_verified->load() ||
					(map != 0 && !input_delivery_enabled->load())) return Error{};
				return input_driver->SetButtons(input_context, map, deadline).error;
			});
		log_.Write({"load_core", admitted->opened_.descriptor.core.system,
			admitted->opened_.descriptor.core.id, "input_open", error});
		if (!error.ok()) return {WithPhase(error, "input"), true, ""};
		input_open_ = true;
		input_identity_verified_ = identity_verified;
		input_delivery_enabled_ = input_delivery_enabled;
	}
	const HardwareResult quiesced = QuiesceForReplacement("load_core",
		admitted->opened_.descriptor.core.system,
		admitted->opened_.descriptor.core.id);
	if (!quiesced.error.ok()) {
		const Error stopped = StopInput(Deadline(clock_, timeouts_.core_io_ms));
		return {stopped.ok() ? quiesced.error : WithPhase(stopped, "input"),
			quiesced.mutation_attempted, quiesced.observed_core};
	}
 menu_status_={};session_display_focused_=false;
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
	// Programming retired the former core's disk ownership. Its record remains,
	// but only explicit insertion may bind the new core to that namespace.
	ForgetMediaData();
	ForgetAtaDisk();
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
 if(menu||session_display) {
  error=menu_window_->Activate(true,blitter);
  if(error.ok())menu_underflow_streak_=0;
  if(error.ok())error=menu_display_->Configure(Deadline(clock_,timeouts_.core_io_ms));
  if(!error.ok()){menu_unsafe_=true;return {error,true,identified.observed_core};}
 }
	if (admitted->data_file_) {
		const std::vector<std::uint16_t> words = admitted->data_.game_id.empty() ?
			std::vector<std::uint16_t>{admitted->data_.paddle_speed, admitted->data_.best_rally} :
			admitted->data_.words;
		error = admitted->driver_->RestoreData(admitted->context_, words,
			Deadline(clock_, timeouts_.core_io_ms));
		if (!error.ok())
			return {WithPhase(error, "core_data"), true, identified.observed_core};
	}
	if (auto* driver = fes_gp ? dynamic_cast<FesGpCoreDriver*>(admitted->driver_) : nullptr) {
		// The machine's clock starts from the kit's local time at every launch.
		std::tm local {};
		if (driver->WallClockCapable()) {
			error = clock_.LocalTime(&local) ? driver->SetWallClock(local,
				Deadline(clock_, timeouts_.core_io_ms)) :
				Error{ErrorCode::io_failed, "kit local time is unavailable", "transport"};
			if (!error.ok()) return {error, true, identified.observed_core};
		}
	}
	if (admitted->ata_disk_) {
		auto* driver = dynamic_cast<FesGpCoreDriver*>(admitted->driver_);
		if (!driver || !driver->AtaDiskCapable() || !disk_window_)
			return {{ErrorCode::unsupported_interface, "ATA disk contract is unavailable", "input"},
				true, identified.observed_core};
		error = LoadAtaDisk(*driver, *admitted->ata_disk_);
		log_.Write({"load_core", admitted->opened_.descriptor.core.system,
			identified.observed_core, "ata_disk", error});
		if (!error.ok()) return {WithPhase(error, "input"), true, identified.observed_core};
		ata_delta_file_ = std::move(admitted->ata_disk_->file);
		ata_identity_ = admitted->ata_disk_->identity;
		ata_size_ = admitted->ata_disk_->image.size;
		ata_ready_ = true;
		admitted->ata_disk_.reset();
	}
	if (admitted->initial_media_) {
		auto* driver = dynamic_cast<FesGpCoreDriver*>(admitted->driver_);
		if (!driver || !driver->MediaWriteCapable(0))
			return {{ErrorCode::unsupported_interface, "initial disk live write contract is unavailable", "input"},
				true, identified.observed_core};
		auto& prepared = *admitted->initial_media_;
		error = driver->InsertMedia(0, prepared.restored ? *prepared.restored : prepared.base,
			clock_, Deadline(clock_, timeouts_.media_io_ms), timeouts_.core_io_ms);
		log_.Write({"load_core", admitted->opened_.descriptor.core.system,
			identified.observed_core, "initial_media", error});
		if (!error.ok()) return {WithPhase(error, "input"), true, identified.observed_core};
		// InsertMedia confirmed ready; execution is still held until Start below.
		media_data_file_ = std::move(prepared.file);
		durable_media_ = std::move(prepared.record);
        media_base_size_ = prepared.base.size();
		admitted->initial_media_.reset();
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
			"transport"), true, identified.observed_core,
			media_data_file_ != nullptr || ata_delta_file_ != nullptr};
  if(menu) {
   error=menu_display_->Enable(Deadline(clock_,timeouts_.core_io_ms));
   MenuDisplayInfo info;if(error.ok())error=menu_display_->ReadInfo(Deadline(clock_,timeouts_.core_io_ms),&info);
   // Programming and port reset clear the underflow register (initial value 0;
   // fes_menu_video.v also clears it on rst). There is no GP clear. A nonzero
   // count here is a fault in this image, not history from the previous core.
   if(error.ok()&&(!info.enabled||!info.configured||info.quiesced||info.pending||info.faulted||info.displayed_sequence||info.underflows))
    error={ErrorCode::io_failed,"menu initial state is not safe black","menu"};
   if(error.ok()&&info.pixel_format!=generated::FesApplicationMenuPixelFormat)
    error={ErrorCode::io_failed,"idle menu must scan the XRGB format","menu"};
   MenuBlitState blit;
   if(error.ok()&&blitter)error=menu_display_->ReadBlit(Deadline(clock_,timeouts_.core_io_ms),&blit);
   if(error.ok()&&(blit.busy||blit.faulted||blit.completed))
    error={ErrorCode::io_failed,"menu blitter initial state is not idle","menu"};
   if(!error.ok()){menu_unsafe_=true;return {error,true,identified.observed_core};}
   menu_status_={};menu_status_.available=true;menu_status_.package_id=admitted->opened_.package_id;
   menu_status_.geometry=info.geometry;menu_status_.blitter=blitter;menu_slot_=0;
   menu_status_.blitter_minor=blitter?MenuBlitterMinor(admitted->opened_.descriptor):0;
  } else if(session_display) {
   MenuDisplayInfo info;error=menu_display_->ReadInfo(Deadline(clock_,timeouts_.core_io_ms),&info);
   if(error.ok()&&(!info.configured||info.enabled||!info.quiesced||info.pending||info.faulted||info.displayed_sequence||info.underflows))
    error={ErrorCode::io_failed,"session display initial state is not drained","menu"};
   // Admission required the declared shared plane; the live bitstream must be it.
   if(error.ok()&&info.pixel_format!=generated::FesDisplayPlanePixelFormatPremultipliedRgba)
    error={ErrorCode::io_failed,"session display must blend premultiplied RGBA","menu"};
   if(!error.ok())return {error,true,identified.observed_core};
   menu_status_={};menu_status_.session=true;menu_status_.core_generation=generation;
   menu_status_.package_id=admitted->opened_.package_id;menu_status_.geometry=info.geometry;menu_slot_=0;
   menu_status_.picture=info.picture;
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

Error NativeHardware::SetSessionDisplay(bool visible,bool launcher_focus)
{
 if(!active_package_||!HasSessionDisplay(active_package_->info().descriptor)||
  !menu_status_.session||!menu_display_||!menu_window_)
  return {ErrorCode::unsupported_interface,"session display is inactive","menu"};
 if(!visible&&launcher_focus)
  return {ErrorCode::invalid_request,"a closed session display leaves input with the machine","menu"};
 const auto deadline=Deadline(clock_,timeouts_.core_io_ms);
 if(visible) {
  if(launcher_focus&&!session_display_focused_) {
   // Neutralize held machine keys and controllers before handing navigation
   // to the UI. There is no execution hold here: the machine keeps running.
   const std::string& abi=active_package_->info().descriptor.abi.id;
   Error error;
   const bool gamepad=RequiresFesGamepad(active_package_->info().descriptor);
   if(gamepad&&input_delivery_enabled_) {
    input_delivery_enabled_->store(false);
    input_.SetButtonDeliveryEnabled(false);
    error=input_.Neutralize(deadline);
   }
   if(error.ok()&&abi==generated::FesComputerABIID){
    error=SetKeyboardHid({});
    if(error.code==ErrorCode::unsupported_interface)error={};
   }else if(error.ok()&&abi==generated::FesSimpleComputerABIID){
    error=SetComputerKeyboard((std::uint64_t(1)<<40)-1);
   }
   if(error.ok())error=ReleaseControllers();
   // A shown plane stays up with the machine still holding input.
   if(!error.ok()) {
    if(gamepad&&input_delivery_enabled_) {
     const bool enabled=!session_display_focused_;
     input_delivery_enabled_->store(enabled);
     input_.SetButtonDeliveryEnabled(enabled);
    }
    return menu_status_.available?error:FailMenuPresent(error);
   }
  }
  // Moving focus on a shown plane keeps its grant and frames.
  if(menu_status_.available){SetSessionDisplayInputFocus(launcher_focus);return {};}
  // Hiding revoked the grant. The plane's slots keep their pixels.
  Error error=menu_window_->Activate(false,false);
  if(error.ok())error=menu_display_->Enable(deadline);
  if(!error.ok())return FailMenuPresent(error);
  SetSessionDisplayInputFocus(launcher_focus);
  MenuDisplayInfo info;error=menu_display_->ReadInfo(deadline,&info);
  if(error.ok()&&(!info.enabled||!info.configured||info.quiesced||info.pending||info.faulted||
   info.displayed_sequence!=menu_status_.displayed_sequence))
   error={ErrorCode::io_failed,"session display enable did not preserve slot ownership","menu"};
  if(!error.ok())return FailMenuPresent(error);
  menu_underflow_streak_=0;menu_status_.available=true;menu_status_.error={};
  return {};
 }
 RevokeMenuWindow();
 MenuDisplayInfo info;
 const Error error=menu_display_->Quiesce(deadline,&active_package_->info().descriptor,&info);
 menu_status_.available=false;
 if(!error.ok()){menu_status_.error=error;SetSessionDisplayInputFocus(true);return error;}
 menu_status_.displayed_sequence=info.displayed_sequence;
 SetSessionDisplayInputFocus(false);menu_underflow_streak_=0;
 return {};
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
	RevokeMenuWindow();
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
 ForgetMediaData();
 ForgetAtaDisk();
 menu_unsafe_=false;session_display_focused_=false;
 if(menu_status_.session)menu_status_={};else menu_status_.available=false;
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


void NativeHardware::RevokeMenuWindow()
{if(menu_window_)menu_window_->Revoke();}
void NativeHardware::SetSessionDisplayInputFocus(bool launcher_focus)
{
 session_display_focused_=launcher_focus;
 if(input_delivery_enabled_&&active_package_&&
  RequiresFesGamepad(active_package_->info().descriptor)) {
  const bool enabled=!launcher_focus;
  input_delivery_enabled_->store(enabled);
  input_.SetButtonDeliveryEnabled(enabled);
 }
}
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
 if(menu_status_.session) {
  RevokeMenuWindow();menu_underflow_streak_=0;menu_status_.available=false;menu_status_.error=error;
  // The only physical recovery is disabling this plane. Never execute hold,
  // program another core, or spend the idle-menu reactivation budget here.
  MenuDisplayInfo info;
  const Error drained=menu_display_->Quiesce(Deadline(clock_,timeouts_.core_io_ms),
   active_package_?&active_package_->info().descriptor:nullptr,&info);
  SetSessionDisplayInputFocus(!drained.ok());
  if(drained.ok())menu_status_.displayed_sequence=info.displayed_sequence;
  if(!drained.ok())menu_status_.error={error.code,error.message+"; session display quiesce failed: "+drained.message,"menu"};
  return menu_status_.error;
 }
 // The runtime's present failure path calls LoadIdle. Keeping the package
 // selects menu reactivation; clearing it selects splash. The counter resets
 // on that program, so the streak does not carry into the new image.
 RevokeMenuWindow();menu_underflow_streak_=0;menu_status_.available=false;menu_status_.error=error;
 if(!menu_directory_.empty()&&MenuReactivationAllowed()){++menu_reactivations_;return error;}
 menu_unsafe_=true;menu_directory_.clear();menu_package_id_.clear();return error;
}
Error NativeHardware::ShareMenuWindow(int* fd)
{
 if(!menu_status_.available||menu_unsafe_||!menu_window_)
  return {ErrorCode::busy,"menu presentation unavailable","menu"};
 return menu_window_->Share(fd);
}
Error NativeHardware::MenuBackSlot(std::uint8_t* slot) const
{
 if(!slot||!menu_status_.available||menu_unsafe_||!menu_display_||!menu_window_)
  return {ErrorCode::busy,"menu presentation unavailable","menu"};
 *slot=std::uint8_t(menu_slot_^1);return {};
}
Error NativeHardware::PresentMenuSlot(std::uint8_t slot,const MenuDraw& draw,MenuDisplayInfo* output)
{
 if(!output||!menu_status_.available||menu_unsafe_||!menu_display_||!menu_window_)
  return {ErrorCode::busy,"menu presentation unavailable","menu"};
 // Scanout is reading the other slot, so a wrong slot is a client error,
 // refused before it can reach the display.
 if(slot!=std::uint8_t(menu_slot_^1))return {ErrorCode::invalid_request,"menu commit names the displayed slot","menu"};
 // A list needs the blitter's grant. That grant takes CPU frames until a list
 // has run, then only lists: a CPU frame's write-back leaves its lines valid
 // in the PL310, and a CPU write over what the blitter drew could make such a
 // stale line dirty and evict it over the blitter's pixels.
 if(draw.blit?!menu_status_.blitter:menu_status_.blitted)
  return {ErrorCode::invalid_request,draw.blit?"menu grant has no blitter":"menu grant takes only lists after its first list","menu"};
 const auto deadline=Deadline(clock_,timeouts_.core_io_ms);
 MenuDisplayInfo info;Error error=menu_display_->ReadInfo(deadline,&info);
 // History accumulated before this present is not a reason to reject it.
 const auto baseline=error.ok()?info.underflows:0;
 if(error.ok()&&(!info.enabled||!info.configured||info.pending||info.quiesced||info.faulted||
  info.displayed_sequence!=menu_status_.displayed_sequence))
  error={ErrorCode::io_failed,"menu slot ownership or scanout state changed","menu"};
 if(error.ok()&&info.displayed_sequence==std::numeric_limits<std::uint32_t>::max())
  error={ErrorCode::io_failed,"menu sequence exhausted","menu"};
 const auto sequence=info.displayed_sequence+1;
 using Steady=std::chrono::steady_clock;
 const auto micros=[](Steady::time_point from,Steady::time_point to){
  return std::uint32_t(std::chrono::duration_cast<std::chrono::microseconds>(to-from).count());};
 MenuBlitState blit;
 if(error.ok()&&draw.blit) {
  error=menu_display_->ReadBlit(deadline,&blit);
  if(error.ok()&&(blit.busy||blit.faulted))error={ErrorCode::io_failed,"menu blitter is not idle","menu"};
 }
 // The client wrote through its cached mapping; fpga2sdram bypasses the
 // caches, so its lines reach DDR before the fabric reads them.
 const auto clean_start=Steady::now();
 if(error.ok())error=draw.blit?menu_window_->CleanBlit(draw.first,draw.count,draw.uploaded):menu_window_->Clean(slot,draw.written);
 const auto blit_start=Steady::now();
 if(error.ok()&&draw.blit) {
  const std::uint16_t completed=std::uint16_t(blit.completed+1);
  error=menu_display_->RunBlit(draw.first,draw.count,slot,deadline);
  while(error.ok()) {
   error=menu_display_->ReadBlit(deadline,&blit);if(!error.ok())break;
   if(blit.faulted){error={ErrorCode::io_failed,"menu blitter fault "+std::to_string(blit.fault),"menu"};break;}
   if(blit.completed==completed&&!blit.busy)break;
   if(blit.completed==std::uint16_t(completed-1)&&!blit.busy) {
    // Completion can land between the separate count and state requests.
    // Confirm this ambiguous old-count/idle pair before treating it as a fault.
    MenuBlitState confirmation;
    error=menu_display_->ReadBlit(deadline,&confirmation);if(!error.ok())break;
    if(confirmation.faulted){error={ErrorCode::io_failed,"menu blitter fault "+std::to_string(confirmation.fault),"menu"};break;}
    if(confirmation.completed==completed&&!confirmation.busy){blit=confirmation;break;}
    if(confirmation.completed==std::uint16_t(completed-1)&&!confirmation.busy) {
     error={ErrorCode::io_failed,"menu blitter completion does not match its run","menu"};break;
    }
    blit=confirmation;
   }
   if(blit.completed!=std::uint16_t(completed-1)||!blit.busy){error={ErrorCode::io_failed,"menu blitter completion does not match its run","menu"};break;}
   if(clock_.NowMs()>=deadline){error={ErrorCode::io_failed,"menu blitter completion timed out","menu"};break;}
   std::this_thread::sleep_for(std::chrono::milliseconds(1));
  }
 }
 const auto submit_start=Steady::now();
 if(error.ok())error=menu_display_->Submit(slot,sequence,deadline);
 const auto wait_start=Steady::now();
 while(error.ok()) {
  error=menu_display_->ReadInfo(deadline,&info);if(!error.ok())break;
  if(!info.enabled||!info.configured||info.quiesced||info.faulted||
   (info.displayed_sequence!=menu_status_.displayed_sequence&&info.displayed_sequence!=sequence)) {
   error={ErrorCode::io_failed,"menu completion or scanout fault","menu"};break;
  }
  if(info.displayed_sequence==sequence&&!info.pending){
   const auto delta=info.underflows>=baseline?info.underflows-baseline:info.underflows;
   // fes_menu_video saturates underflows at 0xffffffff instead of wrapping.
   // A saturated baseline and a saturated completion both subtract to zero,
   // which would reset the streak and accept a stuck scanout.
   const auto saturated=std::numeric_limits<std::uint32_t>::max();
   if(baseline==saturated||info.underflows==saturated||delta>kMenuUnderflowPresentCap){error={ErrorCode::io_failed,"menu underflow exceeded the per-present cap","menu"};break;}
   if(delta>0){if(++menu_underflow_streak_>=kMenuUnderflowSustainPresents){error={ErrorCode::io_failed,"menu underflow persisted across presents","menu"};break;}}
   else menu_underflow_streak_=0;
   menu_slot_=slot;menu_status_.displayed_sequence=sequence;menu_status_.underflows=delta;info.underflows=delta;
   if(draw.blit)menu_status_.blitted=true;
   info.clean_us=micros(clean_start,blit_start);info.blit_us=micros(blit_start,submit_start);
   info.submit_us=micros(submit_start,wait_start);
   info.scanout_wait_us=micros(wait_start,Steady::now());
   *output=info;return {};
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

	ForgetMediaData();
	ForgetAtaDisk();
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
