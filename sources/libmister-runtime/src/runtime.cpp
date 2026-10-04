// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "libmister-runtime/runtime.h"
#include "native/diagnostic.hpp"
#include "native/generated/fes_simple_computer.hpp"
#include "native/generated/fes_application.hpp"
#include "native/generated/fes_computer.hpp"

#include <algorithm>
#include <chrono>
#include <condition_variable>
#include <deque>
#include <mutex>
#include <limits>
#include <thread>
#include <utility>

namespace mister {
namespace {

// Shared by launch, core-data, Stop, development load, idle recovery and
// menu configure. Only an in-flight menu frame is waited out; every other
// busy reason still rejects immediately after the wait returns.
constexpr std::chrono::milliseconds kMenuFrameMutationWait(2000);

Error Busy(const char* message)
{
	return {ErrorCode::busy, message, "lifecycle"};
}

Error Invalid(const char* message)
{
	return {ErrorCode::invalid_request, message, "request"};
}

void EmitBusyFence(const char* operation)
{
	EmitFence(kDiagnosticKindFenceOwnership, "warn", operation, false);
}

Error IdleFailure(const Error& cause)
{
	return {ErrorCode::idle_failed,
		cause.message.empty() ? "idle load failed" : cause.message,
		"recovery", cause.expected, cause.observed};
}

bool ValidLibraryGameId(const std::string& id) {
 if(id.empty()||id.size()>256||id.front()=='-'||id.back()=='-')return false;
 bool hyphen=false;
 for(char c:id){if(!((c>='a'&&c<='z')||(c>='0'&&c<='9')||c=='-')||(c=='-'&&hyphen))return false;hyphen=c=='-';}
 return true;
}

Error SaveFailure(const Error& cause)
{
	return {ErrorCode::save_failed,
		cause.message.empty() ? "save persistence failed" : cause.message,
		"save", cause.expected, cause.observed};
}

bool ValidAbsolutePath(const std::string& path)
{
	return !path.empty() && path.size() <= 4095 && path[0] == '/' &&
		path.find('\0') == std::string::npos;
}

bool ValidPackageId(const std::string& value)
{
	if (value.size() != 64) return false;
	for (const unsigned char byte : value)
		if (!((byte >= '0' && byte <= '9') ||
			(byte >= 'a' && byte <= 'f'))) return false;
	return true;
}

std::vector<SupportedInterface> ActiveInterfaces(
	const CoreDescriptor& descriptor, const Capabilities& capabilities)
{
	std::vector<SupportedInterface> result;
	for (const SupportedABI& abi : capabilities.abis) {
		if (abi.id != descriptor.abi.id || abi.major != descriptor.abi.major ||
			abi.minor != descriptor.abi.minor) continue;
		for (const CoreInterface& declared : descriptor.interfaces) {
			for (const SupportedInterface& supported : abi.interfaces) {
				if (declared.id == supported.id &&
					declared.major == supported.major &&
					declared.minor == supported.minor)
					result.push_back(supported);
			}
		}
		break;
	}
	std::sort(result.begin(), result.end(),
		[](const SupportedInterface& left, const SupportedInterface& right) {
			return left.id < right.id;
		});
	return result;
}

bool DeclaresComputerInterface(const CoreDescriptor& descriptor, const char* id)
{
	if (descriptor.abi.id != native::generated::FesComputerABIID) return false;
	for (const CoreInterface& interface : descriptor.interfaces)
		if (interface.id == id && interface.required && interface.major == 1 &&
			interface.minor == 0)
			return true;
	return false;
}

} // namespace

class Runtime::Impl final : public HardwareFaultSink {
public:
	Impl(Hardware& hardware, LogSink& log)
		: mutex_(), condition_(), hardware_(hardware),
		  log_(log), status_(), faults_(), busy_(false), started_(false),
		  stopping_(false), next_generation_(0), active_generation_(0),
		  pending_fault_generation_(0), fault_thread_(&Impl::DrainFaults, this)
	{
		hardware_.SetFaultSink(this);
		status_.capabilities = hardware_.capabilities();
	}

	~Impl()
	{
		hardware_.SetFaultSink(nullptr);
		{
			std::lock_guard<std::mutex> lock(mutex_);
			stopping_ = true;
			faults_.clear();
		}
		condition_.notify_all();
		if (fault_thread_.joinable()) fault_thread_.join();
	}

	void ReportHardwareFault(HardwareFault fault) override
	{
		if (!fault.error.ok() && fault.error.phase.empty())
			fault.error.phase = "input";
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (stopping_) return;
			if (fault.generation != 0 &&
				fault.generation == active_generation_)
				pending_fault_generation_ = fault.generation;
			faults_.push_back(std::move(fault));
		}
		condition_.notify_all();
	}

	void Log(const std::string& operation, const std::string& system,
		const std::string& core, const std::string& phase, const Error& error = {})
	{
		log_.Write({operation, system, core, phase, error});
	}

	Error Start()
	{
		LogRecord rejection;
		bool rejected = false;
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (busy_ || started_) {
				rejection = {"start", "", "", "validate",
					Busy("runtime already started")};
				rejected = true;
			} else {
				busy_ = true;
				started_ = true;
				status_ = FreshStatus(State::starting);
			}
		}
		if (rejected) {
			log_.Write(rejection);
			EmitBusyFence("start");
			return rejection.error;
		}
		Log("start", "", "", "validate");
		Log("start", "", "", "starting");
		const HardwareResult result = hardware_.LoadIdle();
		if (!result.error.ok()) {
			const Error error = IdleFailure(result.error);
			{
				std::lock_guard<std::mutex> lock(mutex_);
				status_.state = State::reboot_required;
				status_.error = error;
				busy_ = false;
			}
			Log("start", "", "", "failure", error);
			EmitFence(kDiagnosticKindFenceRecovery, "error", "start", false);
			return error;
		}
		{
			std::lock_guard<std::mutex> lock(mutex_);
			status_ = FreshStatus(State::idle);
			busy_ = false;
		}
		condition_.notify_all();
		Log("start", "", "", "idle");
		if (!result.observed_core.empty())
			EmitCoreNameChange(result.observed_core, result.observed_core, true);
		return {};
	}

 Error ConfigureMenuPackage(const std::string& directory,const std::string& id)
 {
  if(!ValidAbsolutePath(directory)||!ValidPackageId(id))return Invalid("invalid menu package request");
  {
   std::unique_lock<std::mutex> lock(mutex_);
   WaitForMenuFrame(lock);
   if(busy_||!started_||status_.state!=State::idle)return Busy("menu configuration requires idle runtime");
   if(next_menu_generation_==std::numeric_limits<std::uint64_t>::max())return Invalid("menu generation exhausted");
   busy_=true;
  }
  const HardwareResult result=hardware_.ConfigureMenuPackage(directory,id);
  if(!result.error.ok()) {
   if(result.mutation_attempted)return FinishLaunchFailure("configure_menu","","fes.menu",result);
   std::lock_guard<std::mutex> lock(mutex_);busy_=false;condition_.notify_all();return result.error;
  }
  {
   std::lock_guard<std::mutex> lock(mutex_);status_=FreshStatus(State::idle);busy_=false;
  }
  condition_.notify_all();return {};
 }
 Error BeginMenuFrame(std::uint64_t generation,std::unique_ptr<MenuFrame>* output)
 {
  if(!output||*output)return Invalid("menu preparation requires an empty frame output");
  std::lock_guard<std::mutex> lock(mutex_);
  Error error=AdmitMenuGeneration(generation);if(!error.ok())return error;
  if(!preparation_.expired())return Busy("one menu preparation is already outstanding");
  std::unique_ptr<MenuFrame> frame;error=MenuFrame::Create(&frame);if(!error.ok())return error;
  frame->preparation_=std::make_shared<int>(0);frame->generation_=generation;
  preparation_=frame->preparation_;*output=std::move(frame);return {};
 }
 Error PresentMenuFrame(std::uint64_t generation,MenuFrame& frame,MenuDisplayInfo* output)
 {
  if(!output)return Invalid("missing menu completion output");
  bool session=false;
  {
   std::lock_guard<std::mutex> lock(mutex_);
   Error error=AdmitMenuGeneration(generation);if(!error.ok())return error;
   if(frame.generation_!=generation||!frame.preparation_||preparation_.lock()!=frame.preparation_)
    return Invalid("menu preparation does not belong to this generation");
   error=frame.ValidateImmutable(frame.fd());if(!error.ok())return error;
   busy_=true;
   menu_frame_busy_=true;
   session=status_.menu_display.session;
  }
  MenuDisplayInfo info;const Error error=hardware_.PresentMenuFrame(frame,&info);
  frame.preparation_.reset();
  if(!error.ok()) {
   if(session) {
    // Display ownership is revoked without retiring the running generation.
    // Hardware only disables the plane; this branch can never program idle.
    {
     std::lock_guard<std::mutex> lock(mutex_);
     status_.menu_display=hardware_.menu_display();
     status_.menu_display.available=false;status_.menu_display.generation=0;
     status_.menu_display.error=error;preparation_.reset();busy_=false;menu_frame_busy_=false;
    }
    condition_.notify_all();return error;
   }
   // Hardware keeps the menu package while a bounded reactivation is allowed
   // and clears it once that limit is exhausted. LoadIdle then reprograms the
   // package or the splash. The copy has already finished or been cancelled.
   const Error failure=FinishLaunchFailure("menu_frame_commit","","fes.menu",{error,true,"fes.menu"});
   { std::lock_guard<std::mutex> lock(mutex_);menu_frame_busy_=false; }
   condition_.notify_all();return failure;
  }
  {
   std::lock_guard<std::mutex> lock(mutex_);
   status_.menu_display.displayed_sequence=info.displayed_sequence;
   status_.menu_display.underflows=info.underflows;busy_=false;menu_frame_busy_=false;*output=info;
  }
  condition_.notify_all();return {};
 }

 Error SetSessionDisplay(const std::string& package_id,std::uint64_t generation,bool visible)
 {
  if(!ValidPackageId(package_id)||!generation)return Invalid("invalid session display request");
  {
   std::unique_lock<std::mutex> lock(mutex_);
   WaitForMenuFrame(lock);
   if(busy_||!started_||pending_fault_generation_||status_.state!=State::running_development)
    return Busy("session display is unavailable");
   if(generation!=active_generation_||generation!=status_.generation||package_id!=status_.active_package.package_id)
    return Invalid("session display package or generation changed");
   bool capable=false;
   for(const auto& interface:status_.capabilities.active_interfaces)
    if(interface.id==native::generated::FesSimpleComputerInterfaceVideoSessionDisplayID&&interface.major==1&&interface.minor==0)
     capable=true;
   if(!capable)return {ErrorCode::unsupported_interface,"session display is unavailable","menu"};
   if(visible&&status_.menu_display.available)return {};
   if(visible&&next_menu_generation_==std::numeric_limits<std::uint64_t>::max())return Invalid("menu generation exhausted");
   busy_=true;preparation_.reset();
  }
  const Error error=hardware_.SetSessionDisplay(visible);
  {
   std::lock_guard<std::mutex> lock(mutex_);
   status_.menu_display=hardware_.menu_display();
   status_.menu_display.session=true;status_.menu_display.core_generation=generation;
   status_.menu_display.package_id=package_id;status_.menu_display.generation=0;
   if(error.ok()&&visible) {
    status_.menu_display.generation=++next_menu_generation_;session_display_focused_=true;
   } else if(error.ok())session_display_focused_=false;
   else {
    status_.menu_display.available=false;status_.menu_display.error=error;
    // A failed close does not prove the UI plane has left HDMI. Require a
    // successful close before machine input becomes eligible again.
    session_display_focused_=true;
   }
   busy_=false;
  }
  condition_.notify_all();return error;
 }

	Status status() const
	{
		std::lock_guard<std::mutex> lock(mutex_);
		return status_;
	}

	Error InspectCore(const std::string& directory,
		const std::string& expected_package_id, CorePackageInspection* output)
	{
		if (output == nullptr || !ValidAbsolutePath(directory) ||
			!ValidPackageId(expected_package_id))
			return Invalid("invalid core package inspection request");
		CorePackageInspection inspection;
		const Error error = hardware_.InspectCorePackage(directory,
			expected_package_id, &inspection);
		Log("inspect_core", "", "", error.ok() ? "admission" :
			(error.phase.empty() ? "admission" : error.phase), error);
		if (!error.ok()) return error;
		*output = std::move(inspection);
		return {};
	}

	Error InspectPartsCore(const std::string& directory, const std::string& id,
		const CoreCompositionRequest& request, CorePackageInspection* output)
	{
		if (!output || !ValidAbsolutePath(directory) || !ValidPackageId(id) || request.parts.empty())
			return Invalid("invalid developer parts inspection request");
		std::unique_ptr<AdmittedCorePackage> admitted;
		Error error = hardware_.AdmitCoreComposition(directory, id, request, &admitted);
		if (error.ok()) error = hardware_.InspectCorePackage(directory, id, output);
		Log("inspect_parts_core", "", "", error.ok() ? "admission" : error.phase, error);
		return error;
	}

	Error LoadCore(const std::string& directory, const std::string& expected_package_id,
		const std::string& data_root = "", const CoreCompositionRequest* composition = nullptr,
		const std::string& programmed_path = "", const std::string& programmed_sha256 = "", const CoreROMLink* rom_link = nullptr,
		const CoreROMLinks* rom_links = nullptr)
	{
		LogRecord rejection;
		bool rejected = false;
		bool replacing = false;
 Status previous_status;
		{
			std::unique_lock<std::mutex> lock(mutex_);
			WaitForMenuFrame(lock);
			if (busy_ || !started_ || status_.state == State::starting ||
				status_.state == State::reboot_required) {
				rejection = {"load_core", "", "", "validate",
					Busy("runtime mutation is busy")};
				rejected = true;
			} else {
				busy_ = true;
 previous_status=status_;
				replacing = status_.state == State::running_development;
			}
		}
		if (rejected) {
			log_.Write(rejection);
			EmitBusyFence("load_core");
			return rejection.error;
		}
		if (!ValidAbsolutePath(directory) || !ValidPackageId(expected_package_id)) {
			const Error error = Invalid("invalid core package request");
			{
				std::lock_guard<std::mutex> lock(mutex_);
				busy_ = false;
			}
			Log("load_core", "", "", "validate", error);
			return error;
		}

		std::unique_ptr<AdmittedCorePackage> package;
		const Error admitted = composition ? hardware_.AdmitCoreComposition(directory,
			expected_package_id, *composition, &package) : hardware_.AdmitCorePackage(directory,
			expected_package_id, &package);
		if (!admitted.ok() || !package) {
			const Error error = admitted.ok() ?
				Error{ErrorCode::invalid_request, "hardware returned no admitted package"} :
				admitted;
			{
				std::lock_guard<std::mutex> lock(mutex_);
				busy_ = false;
			}
			condition_.notify_all();
			Log("load_core", "", "", "validate", error);
			return error;
		}
		if ((package->info().descriptor.format == 3) != (rom_link != nullptr) ||
			(package->info().descriptor.format == 4) != (rom_links != nullptr)) {
			std::lock_guard<std::mutex> lock(mutex_);
			busy_ = false;
			condition_.notify_all();
			return {ErrorCode::unsupported_abi, "ROM activation requires its format-specific source identity", "admission"};
		}
		CoreData data;
		data.package_id = package->info().package_id;
		data.core_id = package->info().declared_core;
		if (!data_root.empty()) {
			const Error prepared = hardware_.PrepareCoreData(package.get(), data_root, &data);
			if (!prepared.ok()) {
				{
					std::lock_guard<std::mutex> lock(mutex_);
					busy_ = false;
				}
				condition_.notify_all();
				return prepared;
			}
		}
		const CorePackageInfo info = package->info();
		const CoreComposition admitted_composition = package->composition();
		if (!programmed_path.empty() || rom_link || rom_links) {
			const Error attached = rom_links ? hardware_.AttachROMsBitstream(package.get(), programmed_path, *rom_links) :
				rom_link ? hardware_.AttachROMBitstream(package.get(), programmed_path, *rom_link) :
				hardware_.AttachProgrammedBitstream(package.get(), programmed_path, programmed_sha256);
			if (!attached.ok()) {
				{
					std::lock_guard<std::mutex> lock(mutex_);
					busy_ = false;
				}
				condition_.notify_all();
				Log("load_core", info.system, info.declared_core, "validate", attached);
				return attached;
			}
		}
		const Error rechecked = hardware_.RecheckProgrammedBitstream(package.get());
		if (!rechecked.ok()) {
			std::lock_guard<std::mutex> lock(mutex_);
			busy_ = false;
			condition_.notify_all();
			return rechecked;
		}
		Log("load_core", info.system, info.declared_core, "validate");

		std::uint64_t retired_generation = 0;
		if (replacing) {
			{
				std::lock_guard<std::mutex> lock(mutex_);
				retired_generation = active_generation_;
				active_generation_ = 0;
			}
			const Error saved = hardware_.FlushSave();
			if (!saved.ok()) {
				return RestoreAfterSaveFailure("load_core", info.system,
					info.declared_core, retired_generation, saved);
			}
		}

		if (!data_root.empty()) {
			const Error refreshed = hardware_.RefreshCoreData(package.get(), &data);
			if (!refreshed.ok()) {
				if (replacing)
					return RestoreAfterSaveFailure("load_library_core", info.system,
						info.declared_core, retired_generation, refreshed);
				{
					std::lock_guard<std::mutex> lock(mutex_);
					busy_ = false;
				}
				condition_.notify_all();
				return refreshed;
			}
		}

		std::uint64_t generation = 0;
		{
			std::lock_guard<std::mutex> lock(mutex_);
			generation = ++next_generation_;
			active_generation_ = generation;
			pending_fault_generation_ = 0;
			status_ = FreshStatus(State::starting);
			status_.execution = Execution::development;
			status_.declared_core = info.declared_core;
			status_.package_id = info.package_id;
		}
		Log("load_core", info.system, info.declared_core, "starting");
		HardwareResult result = hardware_.LoadCore(std::move(package), generation);
		if (!result.error.ok() && replacing && !result.mutation_attempted)
			result.mutation_attempted = true;
		if (!result.error.ok())
			return FinishLaunchFailure("load_core", info.system,
				result.observed_core.empty() ? info.declared_core : result.observed_core,
				result,&previous_status);
		{
			std::lock_guard<std::mutex> lock(mutex_);
			status_.state = State::running_development;
			status_.execution = Execution::development;
			status_.core = result.observed_core;
			status_.declared_core = info.declared_core;
			status_.package_id = info.package_id;
			status_.generation = generation;
			status_.core_data = data;
			status_.active_package.package_id = info.package_id;
			status_.active_package.descriptor = info.descriptor;
			status_.active_package.composition = admitted_composition;
			if (rom_link) status_.active_package.rom_link = *rom_link;
			if (rom_links) status_.active_package.rom_links = *rom_links;
			if (info.descriptor.abi.id == "fes.simple-game" ||
				info.descriptor.abi.id == "fes.simple-computer" ||
				info.descriptor.abi.id == "fes.application" ||
				info.descriptor.abi.id == native::generated::FesComputerABIID) {
				status_.active_package.observed.abi = info.descriptor.abi;
				status_.active_package.observed.build_id = info.descriptor.build.id;
			}
			status_.capabilities.active_interfaces = ActiveInterfaces(
				info.descriptor, status_.capabilities);
			const Capabilities observed = hardware_.capabilities();
			status_.capabilities.media_stream = observed.media_stream;
			status_.capabilities.media_units = observed.media_units;
   status_.menu_display=hardware_.menu_display();
			if (status_.capabilities.media_stream.interface.id.empty()) {
				auto& interfaces = status_.capabilities.active_interfaces;
				interfaces.erase(std::remove_if(interfaces.begin(), interfaces.end(),
					[](const SupportedInterface& item) {
						return item.id == native::generated::FesSimpleComputerInterfaceMediaBlobStreamID;
					}), interfaces.end());
			}
			status_.error = {};
			busy_ = false;
		}
		condition_.notify_all();
		Log("load_core", info.system, result.observed_core, "running");
		return {};
	}

	Error AccessCoreData(const std::string& directory, const std::string& package_id,
		const std::string& data_root, const std::string& revision, std::uint16_t speed, bool update,
		CoreData* output)
	{
		if (!output || !ValidAbsolutePath(directory) || !ValidAbsolutePath(data_root) ||
			!ValidPackageId(package_id) ||
			(update && (speed > 2 || (revision != "absent" && !ValidPackageId(revision)))))
			return Invalid("invalid core-data request");
		{
			std::unique_lock<std::mutex> lock(mutex_);
			WaitForMenuFrame(lock);
			if (busy_ || !started_ || pending_fault_generation_ != 0 ||
				status_.state == State::starting || status_.state == State::reboot_required)
				return Busy("runtime core-data access is busy");
			busy_ = true;
		}
		CoreData data;
		Error error = update ? hardware_.UpdateCoreSettings(
								   directory, package_id, data_root, revision, speed, &data)
							 : hardware_.InspectCoreData(directory, package_id, data_root, &data);
		{
			std::lock_guard<std::mutex> lock(mutex_);
			busy_ = false;
		}
		condition_.notify_all();
		if (error.ok())
			*output = std::move(data);
		return error;
	}

	Error LoadDevelopmentRBF(const std::string& rbf)
	{
		LogRecord rejection;
		bool rejected = false;
 Status previous_status;
		{
			std::unique_lock<std::mutex> lock(mutex_);
			WaitForMenuFrame(lock);
			if (busy_ || !started_ || status_.state != State::idle) {
				rejection = {"load_development_rbf", "", "", "validate",
					Busy("runtime is not idle")};
				rejected = true;
			} else {
				busy_ = true;previous_status=status_;
			}
		}
		if (rejected) {
			log_.Write(rejection);
			EmitBusyFence("load_development_rbf");
			return rejection.error;
		}
		if (!ValidAbsolutePath(rbf)) {
			const Error error = Invalid("invalid RBF path");
			{
				std::lock_guard<std::mutex> lock(mutex_);
				status_.error = error;
				busy_ = false;
			}
			Log("load_development_rbf", "", "", "validate", error);
			return error;
		}
		Log("load_development_rbf", "", "", "validate");
		std::uint64_t generation = 0;
		{
			std::lock_guard<std::mutex> lock(mutex_);
			generation = ++next_generation_;
			active_generation_ = generation;
			pending_fault_generation_ = 0;
			status_ = FreshStatus(State::starting);
			status_.execution = Execution::development;
		}
		Log("load_development_rbf", "", "", "starting");
		const HardwareResult result = hardware_.LoadContainedDevelopmentRBF(rbf, generation);
		if (result.error.ok()) {
			{
				std::lock_guard<std::mutex> lock(mutex_);
				status_ = FreshStatus(State::running_development);
				status_.execution = Execution::development;
				status_.core = result.observed_core;
				status_.generation = generation;
				busy_ = false;
			}
			Log("load_development_rbf", "", "", "running");
			return {};
		}
		return FinishLaunchFailure("load_development_rbf", "", "", result,&previous_status);
	}

	Error SetController(const std::string& package_id, std::uint64_t generation,
		std::uint8_t port, std::uint16_t buttons, std::uint16_t keypad)
	{
		using namespace native::generated;
		if (!ValidPackageId(package_id) || generation == 0 ||
			port >= FesApplicationControllerPortCount ||
			(buttons & ~FesApplicationControllerButtonMask) != 0 ||
			(keypad & ~FesApplicationControllerKeypadMask) != 0)
			return Invalid("invalid controller snapshot");
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (busy_ || !started_ || pending_fault_generation_ != 0 ||
				status_.state != State::running_development)
				return Busy("controller session is not available");
			if (generation != active_generation_ || generation != status_.generation ||
				package_id != status_.active_package.package_id)
				return Invalid("controller package or generation changed");
			busy_ = true;
		}
		const Error error = hardware_.SetController(port, buttons, keypad);
		// Invalid snapshots and absent interfaces cannot mutate the fabric. A
		// failed exchange may have applied half a snapshot: retire this generation
		// through the same one-shot input fault cleanup used by evdev delivery.
		if (!error.ok() && error.code != ErrorCode::invalid_request &&
			error.code != ErrorCode::unsupported_interface)
			ReportHardwareFault({generation, error});
		{
			std::lock_guard<std::mutex> lock(mutex_);
			busy_ = false;
		}
		condition_.notify_all();
		return error;
	}

	Error SetComputerKeyboard(std::uint64_t matrix)
	{
		if (matrix >> 40) return Invalid("invalid computer keyboard matrix");
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (busy_ || !started_ || status_.state != State::running_development)
				return Busy("FES computer is not running");
   if(session_display_focused_&&matrix!=((std::uint64_t(1)<<40)-1))return {};
			busy_ = true;
		}
		const Error error = hardware_.SetComputerKeyboard(matrix);
		{
			std::lock_guard<std::mutex> lock(mutex_);
			busy_ = false;
		}
		condition_.notify_all();
		Log("set_keyboard", status_.system, status_.core,
			error.ok() ? "running" : "input", error);
		return error;
	}

	Error LoadComputerMedia(const std::string& path,
		const std::string& package_id = {}, std::uint64_t generation = 0)
	{
		if (!ValidAbsolutePath(path))
			return Invalid("invalid computer media path");
		if ((!package_id.empty() || generation != 0) &&
			(!ValidPackageId(package_id) || generation == 0))
			return Invalid("invalid computer media binding");
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (busy_ || !started_ || pending_fault_generation_ != 0 ||
				status_.state != State::running_development)
				return Busy("FES computer is not running");
			if (!package_id.empty()) {
				if (generation != active_generation_ || generation != status_.generation ||
					package_id != status_.active_package.package_id)
					return Invalid("media package or generation changed");
			}
			busy_ = true;
		}
		const Error error = hardware_.LoadComputerMedia(path);
		{
			std::lock_guard<std::mutex> lock(mutex_);
			busy_ = false;
		}
		condition_.notify_all();
		Log("load_media", status_.system, status_.core,
			error.ok() ? "running" : "request", error);
		return error;
	}

	Error ReplaceLiveComputerMedia(const std::string& path,
		const std::string& package_id, std::uint64_t generation)
	{
		if (!ValidAbsolutePath(path) || !ValidPackageId(package_id) || generation == 0)
			return Invalid("invalid live media request");
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (busy_ || !started_ || pending_fault_generation_ != 0 ||
				status_.state != State::running_development)
				return Busy("FES computer is not available");
			if (generation != active_generation_ || generation != status_.generation ||
				package_id != status_.active_package.package_id)
				return Invalid("media package or generation changed");
			if (status_.active_package.descriptor.abi.id !=
				native::generated::FesSimpleComputerABIID)
				return {ErrorCode::unsupported_interface,
					"live media requires fes.simple-computer", "compatibility"};
			busy_ = true;
		}
		const Error error = hardware_.LoadComputerMediaLive(path);
		{
			std::lock_guard<std::mutex> lock(mutex_);
			busy_ = false;
		}
		condition_.notify_all();
		Log("replace_live_media", status_.system, status_.core,
			error.ok() ? "running" : "request", error);
		return error;
	}

	Error ClearComputerMedia(const std::string& package_id, std::uint64_t generation)
	{
		if (!ValidPackageId(package_id) || generation == 0)
			return Invalid("invalid clear media request");
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (busy_ || !started_ || pending_fault_generation_ != 0 ||
				status_.state != State::running_development)
				return Busy("FES computer is not available");
			if (generation != active_generation_ || generation != status_.generation ||
				package_id != status_.active_package.package_id)
				return Invalid("media package or generation changed");
			if (status_.active_package.descriptor.abi.id !=
				native::generated::FesSimpleComputerABIID)
				return {ErrorCode::unsupported_interface,
					"clear media requires fes.simple-computer", "compatibility"};
			busy_ = true;
		}
		const Error error = hardware_.ClearComputerMedia();
		{
			std::lock_guard<std::mutex> lock(mutex_);
			busy_ = false;
		}
		condition_.notify_all();
		Log("clear_media", status_.system, status_.core,
			error.ok() ? "running" : "request", error);
		return error;
	}

	Error LoadComputerFirmware(const std::string& path)
	{
		if (!ValidAbsolutePath(path))
			return Invalid("invalid computer firmware path");
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (busy_ || !started_ || status_.state != State::running_development)
				return Busy("FES computer is not running");
			busy_ = true;
		}
		const Error error = hardware_.LoadComputerFirmware(path);
		{
			std::lock_guard<std::mutex> lock(mutex_);
			busy_ = false;
		}
		condition_.notify_all();
		Log("load_firmware", status_.system, status_.core,
			error.ok() ? "running" : "request", error);
		return error;
	}

	Error LoadComputerMediaStream(const std::string& path, const std::string& package_id,
		std::uint64_t generation, std::uint32_t size)
	{
		if (!ValidAbsolutePath(path) || !ValidPackageId(package_id) || generation == 0 ||
			size < native::generated::FesSimpleComputerMediaStreamMinBytes ||
			size > native::generated::FesSimpleComputerMediaStreamMaxBytes)
			return Invalid("invalid media stream request");
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (busy_ || !started_ || pending_fault_generation_ != 0 ||
				status_.state != State::running_development)
				return Busy("FES computer is not available");
			if (generation != active_generation_ || generation != status_.generation ||
				package_id != status_.active_package.package_id)
				return Invalid("media stream package or generation changed");
			const auto& media = status_.capabilities.media_stream;
			if (media.interface.id.empty())
				return {ErrorCode::unsupported_interface, "observed media stream is unavailable", "compatibility"};
			if (size < media.min_bytes || size > media.max_bytes)
				return Invalid("media exceeds observed endpoint capacity");
			busy_ = true;
		}
		const Error error = hardware_.LoadComputerMediaStream(path, size);
		{
			std::lock_guard<std::mutex> lock(mutex_);
			status_.error = error;
			// Preserve ownership/package/generation if cleanup could not synchronize.
			if (error.phase == "recovery") {
				status_.state = State::reboot_required;
				// The terminal recovery error owns reconciliation now. A fault queued
				// while busy must not leave Stop permanently blocked by its marker.
				active_generation_ = 0;
				pending_fault_generation_ = 0;
			}
			busy_ = false;
		}
		condition_.notify_all();
		return error;
	}

	Error SendMouseRelative(const std::string& package_id, std::uint64_t generation,
		std::int16_t dx, std::int16_t dy, std::uint8_t buttons)
	{
		using namespace native::generated;
		if (!ValidPackageId(package_id) || generation == 0 || buttons > 3)
			return Invalid("invalid relative mouse input");
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (busy_ || !started_ || pending_fault_generation_ != 0 || status_.state != State::running_development)
				return Busy("mouse session is not available");
			if (generation != active_generation_ || generation != status_.generation || package_id != status_.active_package.package_id)
				return Invalid("mouse package or generation changed");
			if (!DeclaresComputerInterface(status_.active_package.descriptor, FesComputerInterfaceMouseRelativeID))
				return {ErrorCode::unsupported_interface, "mouse requires fes.computer with fes.mouse.relative", "compatibility"};
			busy_ = true;
		}
		const Error error = hardware_.SendMouseRelative(dx, dy, buttons);
		if (!error.ok() && error.code != ErrorCode::invalid_request && error.code != ErrorCode::unsupported_interface)
			ReportHardwareFault({generation, error});
		{
			std::lock_guard<std::mutex> lock(mutex_);
			busy_ = false;
		}
		condition_.notify_all();
		return error;
	}

	Error SetKeyboardHid(const std::string& package_id, std::uint64_t generation,
		const KeyboardHidRows& rows)
	{
		using namespace native::generated;
		if (!ValidPackageId(package_id) || generation == 0 ||
			(rows[0] & FesComputerKeyboardReservedRow0Mask) != 0 ||
			(rows[FesComputerKeyboardModifierRow] & ~FesComputerKeyboardModifierMask) != 0)
			return Invalid("invalid keyboard HID snapshot");
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (busy_ || !started_ || pending_fault_generation_ != 0 ||
				status_.state != State::running_development)
				return Busy("keyboard session is not available");
			if (generation != active_generation_ || generation != status_.generation ||
				package_id != status_.active_package.package_id)
				return Invalid("keyboard package or generation changed");
			if (!DeclaresComputerInterface(status_.active_package.descriptor,
				FesComputerInterfaceKeyboardHidID))
				return {ErrorCode::unsupported_interface,
					"keyboard HID requires fes.computer with fes.keyboard.hid", "compatibility"};
			busy_ = true;
		}
		const Error error = hardware_.SetKeyboardHid(rows);
		// A failed row exchange may have applied part of the snapshot. Retire this
		// generation through the one-shot input fault cleanup, as for controllers.
		if (!error.ok() && error.code != ErrorCode::invalid_request &&
			error.code != ErrorCode::unsupported_interface)
			ReportHardwareFault({generation, error});
		{
			std::lock_guard<std::mutex> lock(mutex_);
			busy_ = false;
		}
		condition_.notify_all();
		return error;
	}

	Error InsertMedia(const std::string& path, const std::string& package_id,
		std::uint64_t generation, std::uint8_t unit, std::uint32_t size)
	{
		using namespace native::generated;
		if (!ValidAbsolutePath(path) || !ValidPackageId(package_id) || generation == 0 ||
			unit >= FesComputerMediaUnitCount || size < FesComputerMediaMinBytes ||
			size > FesComputerMediaMaxBytes)
			return Invalid("invalid media unit request");
		std::string system, core;
		{
			std::lock_guard<std::mutex> lock(mutex_);
			const Error rejected = AdmitMediaUnit(package_id, generation, unit);
			if (!rejected.ok()) return rejected;
			for (const MediaUnitCapability& observed : status_.capabilities.media_units)
				if (observed.unit == unit && (size < observed.min_bytes || size > observed.max_bytes))
					return Invalid("media size is outside the observed unit limits");
			busy_ = true;
			system = status_.system;
			core = status_.core;
		}
		// Execution stays released. Cleanup ejects only a transfer that accepted
		// or ambiguously issued Begin; a rejected replacement retains the disk.
		const Error error = hardware_.InsertComputerMedia(unit, path, size);
		if (error.code == ErrorCode::save_failed)
			return RestoreAfterSaveFailure("insert_media", system, core, generation, error);
		FinishMediaUnit();
		Log("insert_media", system, core, error.ok() ? "running" : "request", error);
		return error;
	}

	Error InsertLibraryMedia(const std::string& path, const std::string& package_id,
		std::uint64_t generation, std::uint8_t unit, std::uint32_t size,
		const std::string& root, const MediaDataBinding& binding)
	{
		if (!ValidAbsolutePath(path) || !ValidAbsolutePath(root) || !ValidPackageId(package_id) ||
			!ValidPackageId(binding.base_media_id) || generation == 0 || unit != 0 || binding.unit != unit ||
			size != native::generated::FesComputerAtariStFloppyBytes ||
			!ValidLibraryGameId(binding.game_id))
			return Invalid("invalid library media request");
		std::string system, core;
		{
			std::lock_guard<std::mutex> lock(mutex_);
			const Error rejected = AdmitMediaUnit(package_id, generation, unit);
			if (!rejected.ok()) return rejected;
			busy_ = true; system = status_.system; core = status_.core;
		}
		const Error error = hardware_.InsertLibraryComputerMedia(unit, path, size, root, binding);
		if (error.code == ErrorCode::save_failed && error.phase != "request")
			return RestoreAfterSaveFailure("insert_library_media", system, core, generation, error);
		FinishMediaUnit();
		Log("insert_library_media", system, core, error.ok() ? "running" : "request", error);
		return error;
	}

	Error SaveMedia(const std::string& package_id, std::uint64_t generation, std::uint8_t unit)
	{
		if (!ValidPackageId(package_id) || generation == 0 || unit != 0)
			return Invalid("invalid media save binding");
		std::string system, core;
		{
			std::lock_guard<std::mutex> lock(mutex_);
			const Error rejected = AdmitMediaUnit(package_id, generation, unit);
			if (!rejected.ok()) return rejected;
			bool persistent = false;
			for (const auto& media : status_.capabilities.media_units)
				if (media.unit == unit && media.persistence_mode == "persistent") persistent = true;
			if (!persistent) return {ErrorCode::unsupported_interface,
				"media has no library data binding", "compatibility"};
			busy_ = true; system = status_.system; core = status_.core;
		}
		return RestoreAfterSaveFailure("save_media", system, core, generation, hardware_.FlushSave());
	}

	Error EjectMedia(const std::string& package_id, std::uint64_t generation,
		std::uint8_t unit)
	{
		if (!ValidPackageId(package_id) || generation == 0 ||
			unit >= native::generated::FesComputerMediaUnitCount)
			return Invalid("invalid media unit request");
		std::string system, core;
		{
			std::lock_guard<std::mutex> lock(mutex_);
			const Error rejected = AdmitMediaUnit(package_id, generation, unit);
			if (!rejected.ok()) return rejected;
			busy_ = true;
			system = status_.system;
			core = status_.core;
		}
		const Error error = hardware_.EjectComputerMedia(unit);
		if (error.code == ErrorCode::save_failed)
			return RestoreAfterSaveFailure("eject_media", system, core, generation, error);
		FinishMediaUnit();
		Log("eject_media", system, core, error.ok() ? "running" : "request", error);
		return error;
	}

	Error Stop()
	{
		LogRecord immediate;
		bool return_immediately = false;
		std::uint64_t retired_generation = 0;
		{
			std::unique_lock<std::mutex> lock(mutex_);
			WaitForMenuFrame(lock);
			if (busy_ || pending_fault_generation_ != 0 || !started_) {
				immediate = {"stop", status_.system, status_.core, "validate",
					Busy("runtime mutation is busy")};
				return_immediately = true;
			} else if (status_.state == State::reboot_required) {
				immediate = {"stop", "", "", "validate",
					status_.error};
				return_immediately = true;
			} else if (status_.state == State::idle && status_.menu_display.available) {
 busy_=true;
			} else if (status_.state == State::idle) {
				status_.error = {};
				immediate = {"stop", "", "", "idle", {}};
				return_immediately = true;
			} else if (status_.state != State::running_development) {
				immediate = {"stop", status_.system, status_.core, "validate",
					Busy("runtime is not stoppable")};
				return_immediately = true;
			} else {
				busy_ = true;
				// Input is being retired even if persistence needs a later retry.
				retired_generation = active_generation_;
				active_generation_ = 0;
			}
		}
		if (return_immediately) {
			log_.Write(immediate);
			if (immediate.error.code == ErrorCode::busy)
				EmitBusyFence("stop");
			return immediate.error;
		}
		const Error saved = hardware_.FlushSave();
		if (!saved.ok()) {
			return RestoreAfterSaveFailure("stop", "", "",
				retired_generation, saved);
		}
		{
			std::lock_guard<std::mutex> lock(mutex_);
			status_ = FreshStatus(State::starting);
			status_.execution = Execution::none;
		}
		Log("stop", "", "", "starting");
		const HardwareResult result = hardware_.LoadIdle();
		if (!result.error.ok()) {
			const Error error = IdleFailure(result.error);
			{
				std::lock_guard<std::mutex> lock(mutex_);
				status_ = FreshStatus(State::reboot_required);
				status_.error = error;
				busy_ = false;
			}
			Log("stop", "", "", "failure", error);
			EmitFence(kDiagnosticKindFenceRecovery, "error", "stop", false);
			return error;
		}
		{
			std::lock_guard<std::mutex> lock(mutex_);
			status_ = FreshStatus(State::idle);
			busy_ = false;
		}
		condition_.notify_all();
		Log("stop", "", "", "idle");
		return {};
	}

	Error RecoverIdle()
	{
		bool retry_idle = false;
		{
			std::unique_lock<std::mutex> lock(mutex_);
			WaitForMenuFrame(lock);
			if (busy_ || pending_fault_generation_ != 0 || !started_) {
				const Error error = Busy("runtime mutation is busy");
				Log("recover_idle", status_.system, status_.core, "validate", error);
				return error;
			}
			if (status_.state == State::running_development) {
				// Stop owns save and the first idle program.
			} else if (status_.state == State::idle) {
				status_.error = {};
				Log("recover_idle", "", "", "idle");
				return {};
			} else if (status_.state != State::reboot_required) {
				const Error error = Busy("runtime is not recoverable");
				Log("recover_idle", "", "", "validate", error);
				return error;
			} else {
				for (const auto& media : status_.capabilities.media_units)
					if (media.persistence_mode == "persistent") {
						const Error error{ErrorCode::save_failed,
							"retained library disk requires recovery before idle programming", "recovery"};
						Log("recover_idle", status_.system, status_.core, "recovery", error);
						return error;
					}
				busy_ = true;
				retry_idle = true;
			}
		}
		if (!retry_idle) return Stop();
		Log("recover_idle", "", "", "starting");
		const HardwareResult result = hardware_.LoadIdle();
		if (!result.error.ok()) {
			const Error error = IdleFailure(result.error);
			{
				std::lock_guard<std::mutex> lock(mutex_);
				status_ = FreshStatus(State::reboot_required);
				status_.error = error;
				busy_ = false;
			}
			Log("recover_idle", "", "", "failure", error);
			EmitFence(kDiagnosticKindFenceRecovery, "error", "recover_idle", false);
			return error;
		}
		{
			std::lock_guard<std::mutex> lock(mutex_);
			status_ = FreshStatus(State::idle);
			busy_ = false;
		}
		condition_.notify_all();
		Log("recover_idle", "", "", "idle");
		return {};
	}

	private:
 // Give one lifecycle operation a bounded chance to claim the runtime after
 // the current display transfer. New frames cannot enter while it waits.
 // Launch, core-data, Stop, development load, idle recovery and menu
 // configure share kMenuFrameMutationWait. The caller proceeds only after
 // the frame fence drops, then applies its existing busy checks.
 void WaitForMenuFrame(std::unique_lock<std::mutex>& lock) {
  if(!menu_frame_busy_)return;
  ++menu_mutation_waiters_;
  condition_.wait_for(lock,kMenuFrameMutationWait,[this]{return !menu_frame_busy_;});
  --menu_mutation_waiters_;
 }
 Error AdmitMenuGeneration(std::uint64_t generation) const {
  if(busy_||menu_mutation_waiters_||!started_||pending_fault_generation_||!status_.menu_display.available)
   return Busy("menu display is unavailable");
  if(status_.menu_display.session) {
   if(status_.state!=State::running_development||!active_generation_||
    status_.menu_display.core_generation!=active_generation_||status_.generation!=active_generation_||
    status_.menu_display.package_id!=status_.active_package.package_id)
    return Invalid("session display binding changed");
  } else if(status_.state!=State::idle)return Busy("idle menu display is unavailable");
  if(!generation||generation!=status_.menu_display.generation)return Invalid("menu generation changed");
  return {};
 }
	// Caller holds mutex_. Media units bind the exact active fes.computer
	// generation and one of its declared, observed units.
	Error AdmitMediaUnit(const std::string& package_id, std::uint64_t generation,
		std::uint8_t unit) const
	{
		if (busy_ || !started_ || pending_fault_generation_ != 0 ||
			status_.state != State::running_development)
			return Busy("FES computer is not available");
		if (generation != active_generation_ || generation != status_.generation ||
			package_id != status_.active_package.package_id)
			return Invalid("media package or generation changed");
		if (status_.active_package.descriptor.abi.id != native::generated::FesComputerABIID)
			return {ErrorCode::unsupported_interface, "media units require fes.computer",
				"compatibility"};
		for (const MediaUnitCapability& observed : status_.capabilities.media_units)
			if (observed.unit == unit) return {};
		return {ErrorCode::unsupported_interface,
			"media unit is not declared by the active computer", "compatibility"};
	}

	void FinishMediaUnit()
	{
		{
			std::lock_guard<std::mutex> lock(mutex_);
			// Publish the driver's unit state from its last live exchange.
			status_.capabilities.media_units = hardware_.capabilities().media_units;
			if (status_.active_package.descriptor.abi.id == native::generated::FesComputerABIID) {
				status_.core_data.mode = "volatile";
				for (const auto& media : status_.capabilities.media_units)
					if (media.persistence_mode == "persistent") status_.core_data.mode = "persistent";
			}
			busy_ = false;
		}
		condition_.notify_all();
	}

	Error RestoreAfterSaveFailure(const std::string& operation,
		const std::string& system, const std::string& core,
		std::uint64_t generation, const Error& cause)
	{
		const Error save_error = cause.ok() ? Error{} : SaveFailure(cause);
		{
			std::lock_guard<std::mutex> lock(mutex_);
			// Publish ownership before input becomes eligible so an immediate
			// worker fault is attributed to the restored generation.
			active_generation_ = generation;
			pending_fault_generation_ = 0;
		}
		const Error restored = hardware_.RestoreInput(generation);
		if (restored.ok()) {
			{
				std::lock_guard<std::mutex> lock(mutex_);
				status_.error = save_error;
				status_.capabilities.media_units = hardware_.capabilities().media_units;
				if (status_.active_package.descriptor.abi.id == native::generated::FesComputerABIID) {
					status_.core_data.mode = "volatile";
					for (const auto& media : status_.capabilities.media_units)
						if (media.persistence_mode == "persistent") status_.core_data.mode = "persistent";
				}
				busy_ = false;
			}
			condition_.notify_all();
			Log(operation, system, core, cause.ok() ? "running" : "save", save_error);
			return save_error;
		}

		const Error recovery = IdleFailure(restored);
		{
			std::lock_guard<std::mutex> lock(mutex_);
			active_generation_ = 0;
			pending_fault_generation_ = 0;
			if (status_.core_data.mode == "persistent")
				status_.state = State::reboot_required;
			else
				status_ = FreshStatus(State::reboot_required);
			status_.error = recovery;
			busy_ = false;
		}
		condition_.notify_all();
		Log(operation, system, core, "recovery", recovery);
		EmitFence(kDiagnosticKindFenceRecovery, "error", operation.c_str(), false);
		return recovery;
	}

	Status FreshStatus(State state)
	{
  if(state!=State::idle){preparation_.reset();session_display_focused_=false;}
		Status result;
		result.state = state;
		result.capabilities = hardware_.capabilities();
		result.capabilities.media_stream = {};
		result.capabilities.media_units.clear();
  if(state==State::idle) {
   result.menu_display=hardware_.menu_display();
   if(result.menu_display.available) {
    if(next_menu_generation_==std::numeric_limits<std::uint64_t>::max()) {
     result.menu_display.available=false;result.menu_display.error=Invalid("menu generation exhausted");
     result.error=result.menu_display.error;
    } else result.menu_display.generation=++next_menu_generation_;
   }
  }
  return result;
	}

	Error FinishLaunchFailure(const std::string& operation,
		const std::string& system, const std::string& core,
		const HardwareResult& result,const Status* previous_status=nullptr)
	{
		const Error primary = result.error;
		{
			std::lock_guard<std::mutex> lock(mutex_);
			active_generation_ = 0;
			pending_fault_generation_ = 0;
		}
		Log(operation, system, core, "failure", primary);
		if (!result.mutation_attempted) {
			std::lock_guard<std::mutex> lock(mutex_);
   if(previous_status&&previous_status->menu_display.available)status_=*previous_status;
   else status_=FreshStatus(State::idle);
			status_.error = primary;
			busy_ = false;
			condition_.notify_all();
			return primary;
		}
		Log(operation, system, core, "cleanup");
		const HardwareResult cleanup = hardware_.LoadIdle();
		if (cleanup.error.ok()) {
			{
				std::lock_guard<std::mutex> lock(mutex_);
				status_ = FreshStatus(State::idle);
				status_.error = primary;
				busy_ = false;
			}
			condition_.notify_all();
			EmitFence(kDiagnosticKindFenceRecovery, "ok", operation.c_str(), true);
			return primary;
		}
		const Error idle_error = IdleFailure(cleanup.error);
		{
			std::lock_guard<std::mutex> lock(mutex_);
			status_ = FreshStatus(State::reboot_required);
			status_.error = idle_error;
			busy_ = false;
		}
		condition_.notify_all();
		Log(operation, system, core, "cleanup", idle_error);
		EmitFence(kDiagnosticKindFenceRecovery, "error", operation.c_str(), false);
		return idle_error;
	}

	void DrainFaults()
	{
		for (;;) {
			HardwareFault fault;
			std::string system;
			std::string core;
			Status retained;
			bool bound_disk = false;
			{
				std::unique_lock<std::mutex> lock(mutex_);
				condition_.wait(lock, [this]() {
					return stopping_ || (!busy_ && !faults_.empty());
				});
				if (stopping_) return;
				fault = std::move(faults_.front());
				faults_.pop_front();
				if (fault.generation == 0 ||
					fault.generation != active_generation_ ||
					fault.generation != pending_fault_generation_ ||
					status_.state != State::running_development)
					continue;
				busy_ = true;
				active_generation_ = 0;
				pending_fault_generation_ = 0;
				system = status_.system;
				core = status_.core;
				for (const auto& media : status_.capabilities.media_units)
					if (media.persistence_mode == "persistent") bound_disk = true;
				if (bound_disk) retained = status_;
				status_ = FreshStatus(State::starting);
				status_.execution = Execution::none;
			}

			Log("input_fault", system, core, "failure", fault.error);
			Log("input_fault", system, core, "cleanup");
			if (bound_disk) {
				const Error saved = hardware_.FlushFaultSave();
				if (!saved.ok()) {
					const Error recovery = IdleFailure(saved);
					{
						std::lock_guard<std::mutex> lock(mutex_);
						status_ = std::move(retained);
						status_.state = State::reboot_required;
						status_.error = recovery;
						busy_ = false;
					}
					condition_.notify_all();
					Log("input_fault", system, core, "recovery", recovery);
					EmitFence(kDiagnosticKindFenceRecovery, "error", "input_fault", false);
					continue;
				}
			}
			const HardwareResult cleanup = hardware_.LoadIdle();
			if (cleanup.error.ok()) {
				{
					std::lock_guard<std::mutex> lock(mutex_);
					status_ = FreshStatus(State::idle);
					status_.error = fault.error;
					busy_ = false;
				}
				condition_.notify_all();
				Log("input_fault", system, core, "idle", fault.error);
				EmitFence(kDiagnosticKindFenceRecovery, "ok", "input_fault", true);
				continue;
			}

			const Error idle_error = IdleFailure(cleanup.error);
			{
				std::lock_guard<std::mutex> lock(mutex_);
				status_ = FreshStatus(State::reboot_required);
				status_.error = idle_error;
				busy_ = false;
			}
			condition_.notify_all();
			Log("input_fault", system, core, "cleanup", idle_error);
			EmitFence(kDiagnosticKindFenceRecovery, "error", "input_fault", false);
		}
	}

	mutable std::mutex mutex_;
	std::condition_variable condition_;
	Hardware& hardware_;
	LogSink& log_;
	Status status_;
	std::deque<HardwareFault> faults_;
	bool busy_;
	bool menu_frame_busy_=false;
 bool session_display_focused_=false;
	unsigned menu_mutation_waiters_=0;
	bool started_;
	bool stopping_;
	std::uint64_t next_generation_;
 std::uint64_t next_menu_generation_=0;
 std::weak_ptr<void> preparation_;
	std::uint64_t active_generation_;
	std::uint64_t pending_fault_generation_;
	std::thread fault_thread_;
};

Runtime::Runtime(Hardware& hardware, LogSink& log)
	: impl_(new Impl(hardware, log)) {}

Runtime::~Runtime() = default;

Error Runtime::Start() { return impl_->Start(); }
Error Runtime::ConfigureMenuPackage(const std::string& directory,const std::string& id) {return impl_->ConfigureMenuPackage(directory,id);}
Error Runtime::BeginMenuFrame(std::uint64_t generation,std::unique_ptr<MenuFrame>* output) {return impl_->BeginMenuFrame(generation,output);}
Error Runtime::SetSessionDisplay(const std::string& package_id,std::uint64_t generation,bool visible) {return impl_->SetSessionDisplay(package_id,generation,visible);}
Error Runtime::PresentMenuFrame(std::uint64_t generation,MenuFrame& frame,MenuDisplayInfo* output) {return impl_->PresentMenuFrame(generation,frame,output);}

Status Runtime::status() const { return impl_->status(); }
Error Runtime::LoadCore(const std::string& directory,
	const std::string& expected_package_id)
{
	return impl_->LoadCore(directory, expected_package_id);
}

Error Runtime::LoadComposedCore(const std::string& directory, const std::string& id,
	const CoreCompositionRequest& request)
{
	return impl_->LoadCore(directory, id, "", &request);
}
Error Runtime::LoadLibraryCore(
	const std::string& directory, const std::string& id, const std::string& root)
{
	if (!ValidAbsolutePath(root))
		return Invalid("invalid core-data root");
	return impl_->LoadCore(directory, id, root);
}
Error Runtime::LoadLibraryPartsCore(const std::string& directory, const std::string& id,
	const std::string& root, const CoreCompositionRequest& request)
{
	if (!ValidAbsolutePath(root) || request.parts.empty() || request.composition.parts.empty())
		return Invalid("library parts require a core-data root and parts composition");
	return impl_->LoadCore(directory, id, root, &request);
}
Error Runtime::LoadROMCore(const std::string& directory, const std::string& id,
	const std::string& path, const CoreROMLink& link)
{ return impl_->LoadCore(directory, id, "", nullptr, path, "", &link); }
Error Runtime::LoadROMLibraryCore(const std::string& directory, const std::string& id,
	const std::string& root, const std::string& path, const CoreROMLink& link)
{
	if (!ValidAbsolutePath(root)) return Invalid("invalid core-data root");
	return impl_->LoadCore(directory, id, root, nullptr, path, "", &link);
}
Error Runtime::LoadROMComposedCore(const std::string& directory, const std::string& id,
	const CoreCompositionRequest& request, const std::string& path, const CoreROMLink& link)
{ return impl_->LoadCore(directory, id, "", &request, path, "", &link); }

Error Runtime::LoadROMsCore(const std::string& directory, const std::string& id,
	const std::string& path, const CoreROMLinks& links)
{ return impl_->LoadCore(directory, id, "", nullptr, path, "", nullptr, &links); }
Error Runtime::LoadROMsLibraryCore(const std::string& directory, const std::string& id,
	const std::string& root, const std::string& path, const CoreROMLinks& links)
{
	if (!ValidAbsolutePath(root)) return Invalid("invalid core-data root");
	return impl_->LoadCore(directory, id, root, nullptr, path, "", nullptr, &links);
}
Error Runtime::LoadROMsComposedCore(const std::string& directory, const std::string& id,
	const CoreCompositionRequest& request, const std::string& path, const CoreROMLinks& links)
{ return impl_->LoadCore(directory, id, "", &request, path, "", nullptr, &links); }

Error Runtime::LoadInitializedCore(const std::string& directory, const std::string& id,
	const std::string& programmed_path, const std::string& programmed_sha256)
{
	return impl_->LoadCore(directory, id, "", nullptr, programmed_path, programmed_sha256);
}
Error Runtime::LoadInitializedLibraryCore(const std::string& directory, const std::string& id,
	const std::string& root, const std::string& programmed_path, const std::string& programmed_sha256)
{
	if (!ValidAbsolutePath(root))
		return Invalid("invalid core-data root");
	return impl_->LoadCore(directory, id, root, nullptr, programmed_path, programmed_sha256);
}
Error Runtime::LoadInitializedComposedCore(const std::string& directory, const std::string& id,
	const CoreCompositionRequest& request, const std::string& programmed_path,
	const std::string& programmed_sha256)
{
	return impl_->LoadCore(directory, id, "", &request, programmed_path, programmed_sha256);
}
Error Runtime::InspectCoreData(
	const std::string& directory, const std::string& id, const std::string& root, CoreData* output)
{
	return impl_->AccessCoreData(directory, id, root, "", 0, false, output);
}
Error Runtime::UpdateCoreSettings(const std::string& directory, const std::string& id,
	const std::string& root, const std::string& revision, std::uint16_t speed, CoreData* output)
{
	return impl_->AccessCoreData(directory, id, root, revision, speed, true, output);
}

Error Runtime::InspectCore(const std::string& directory,
	const std::string& expected_package_id, CorePackageInspection* output)
{
	return impl_->InspectCore(directory, expected_package_id, output);
}
Error Runtime::InspectPartsCore(const std::string& directory, const std::string& id,
	const CoreCompositionRequest& request, CorePackageInspection* output)
{
	return impl_->InspectPartsCore(directory, id, request, output);
}
Error Runtime::SetController(const std::string& package_id, std::uint64_t generation,
	std::uint8_t port, std::uint16_t buttons, std::uint16_t keypad)
{
	return impl_->SetController(package_id, generation, port, buttons, keypad);
}

Error Runtime::SetComputerKeyboard(std::uint64_t matrix)
{
	return impl_->SetComputerKeyboard(matrix);
}
Error Runtime::LoadComputerMedia(const std::string& path)
{
	return impl_->LoadComputerMedia(path);
}

Error Runtime::LoadComputerMedia(const std::string& path,
	const std::string& expected_package_id, std::uint64_t expected_generation)
{
	return impl_->LoadComputerMedia(path, expected_package_id, expected_generation);
}

Error Runtime::ReplaceLiveComputerMedia(const std::string& path,
	const std::string& expected_package_id, std::uint64_t expected_generation)
{
	return impl_->ReplaceLiveComputerMedia(path, expected_package_id, expected_generation);
}

Error Runtime::ClearComputerMedia(const std::string& expected_package_id,
	std::uint64_t expected_generation)
{
	return impl_->ClearComputerMedia(expected_package_id, expected_generation);
}

Error Runtime::LoadComputerFirmware(const std::string& path)
{
	return impl_->LoadComputerFirmware(path);
}

Error Runtime::LoadComputerMediaStream(const std::string& path,
	const std::string& package_id, std::uint64_t generation, std::uint32_t size)
{
	return impl_->LoadComputerMediaStream(path, package_id, generation, size);
}
Error Runtime::SendMouseRelative(const std::string& package_id, std::uint64_t generation,
	std::int16_t dx, std::int16_t dy, std::uint8_t buttons)
{
	return impl_->SendMouseRelative(package_id, generation, dx, dy, buttons);
}

Error Runtime::SetKeyboardHid(const std::string& package_id, std::uint64_t generation,
	const KeyboardHidRows& rows)
{
	return impl_->SetKeyboardHid(package_id, generation, rows);
}
Error Runtime::InsertMedia(const std::string& path, const std::string& package_id,
	std::uint64_t generation, std::uint8_t unit, std::uint32_t size)
{
	return impl_->InsertMedia(path, package_id, generation, unit, size);
}
Error Runtime::EjectMedia(const std::string& package_id, std::uint64_t generation,
	std::uint8_t unit)
{
	return impl_->EjectMedia(package_id, generation, unit);
}
Error Runtime::InsertLibraryMedia(const std::string& path, const std::string& package_id,
	std::uint64_t generation, std::uint8_t unit, std::uint32_t size,
	const std::string& root, const MediaDataBinding& binding)
{
	return impl_->InsertLibraryMedia(path, package_id, generation, unit, size, root, binding);
}
Error Runtime::SaveMedia(const std::string& package_id, std::uint64_t generation, std::uint8_t unit)
{
	return impl_->SaveMedia(package_id, generation, unit);
}
Error Runtime::LoadContainedDevelopmentRBF(const std::string& rbf)
{
	return impl_->LoadDevelopmentRBF(rbf);
}
Error Runtime::Stop() { return impl_->Stop(); }
Error Runtime::RecoverIdle() { return impl_->RecoverIdle(); }

const char* ErrorCodeName(ErrorCode code)
{
	switch (code) {
	case ErrorCode::none: return "none";
	case ErrorCode::invalid_request: return "invalid_request";
	case ErrorCode::unsupported_protocol: return "unsupported_protocol";
	case ErrorCode::unknown_system: return "unknown_system";
	case ErrorCode::missing_media: return "missing_media";
	case ErrorCode::busy: return "busy";
	case ErrorCode::program_failed: return "program_failed";
	case ErrorCode::core_mismatch: return "core_mismatch";
	case ErrorCode::io_failed: return "io_failed";
	case ErrorCode::idle_failed: return "idle_failed";
	case ErrorCode::save_failed: return "save_failed";
	case ErrorCode::invalid_package: return "invalid_package";
	case ErrorCode::unsupported_target: return "unsupported_target";
	case ErrorCode::unsupported_programming_profile: return "unsupported_programming_profile";
	case ErrorCode::unsupported_abi: return "unsupported_abi";
	case ErrorCode::unsupported_interface: return "unsupported_interface";
	case ErrorCode::corrupt_data:
		return "corrupt_data";
	case ErrorCode::incompatible_data:
		return "incompatible_data";
	case ErrorCode::stale_revision:
		return "stale_revision";
	}
	return "invalid";
}

const char* StateName(State state)
{
	switch (state) {
	case State::idle: return "idle";
	case State::starting: return "starting";
	case State::running_development: return "running_development";
	case State::reboot_required: return "reboot_required";
	}
	return "invalid";
}

const char* ExecutionName(Execution execution)
{
	switch (execution) {
	case Execution::none: return "none";
	case Execution::development: return "development";
	}
	return "invalid";
}

const char* MediaUnitStateName(MediaUnitState state)
{
	switch (state) {
	case MediaUnitState::empty: return "empty";
	case MediaUnitState::loading: return "loading";
	case MediaUnitState::ready: return "ready";
	}
	return "invalid";
}

} // namespace mister
