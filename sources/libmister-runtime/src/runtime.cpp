// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "libmister-runtime/runtime.h"
#include "native/diagnostic.hpp"
#include "native/generated/fes_simple_computer.hpp"
#include "native/generated/fes_application.hpp"

#include <algorithm>
#include <condition_variable>
#include <deque>
#include <mutex>
#include <thread>
#include <utility>

namespace mister {
namespace {

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

} // namespace

class Runtime::Impl final : public HardwareFaultSink {
public:
	Impl(Hardware& hardware, const Profiles& profiles, LogSink& log)
		: mutex_(), condition_(), hardware_(hardware), profiles_(profiles),
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
			EmitCoreNameChange("MENU", result.observed_core, true);
		return {};
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

	Error LaunchGame(const mister::Launch& launch)
	{
		LogRecord rejection;
		bool rejected = false;
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (busy_ || !started_ || status_.state != State::idle) {
				rejection = {"launch", launch.system, "", "validate",
					Busy("runtime is not idle")};
				rejected = true;
			} else {
				busy_ = true;
			}
		}
		if (rejected) {
			log_.Write(rejection);
			EmitBusyFence("launch");
			return rejection.error;
		}

		PreparedLaunch prepared;
		const Error validation = profiles_.Prepare(launch, &prepared);
		if (!validation.ok()) {
			{
				std::lock_guard<std::mutex> lock(mutex_);
				status_.error = validation;
				busy_ = false;
			}
			Log("launch", launch.system, "", "validate", validation);
			return validation;
		}
		Log("launch", prepared.system, prepared.expected_core, "validate");
		std::uint64_t generation = 0;
		{
			std::lock_guard<std::mutex> lock(mutex_);
			generation = ++next_generation_;
			active_generation_ = generation;
			status_.state = State::starting;
			status_.execution = Execution::game;
			status_.system = prepared.system;
			status_.core = prepared.expected_core;
			status_.error = {};
		}
		Log("launch", prepared.system, prepared.expected_core, "starting");

		HardwareResult result = hardware_.Launch(prepared, generation);
		if (result.error.ok() && result.observed_core != prepared.expected_core) {
			result.error = {ErrorCode::core_mismatch,
				"observed core does not match profile", "identity",
				prepared.expected_core, result.observed_core};
			result.mutation_attempted = true;
			EmitDiagnostic(kDiagnosticLayerRuntime, kDiagnosticKindFenceAbi, "error",
				{
					DiagnosticString("operation", "launch"),
					DiagnosticBool("ok", false),
					DiagnosticString("expected", prepared.expected_core),
					DiagnosticString("observed", result.observed_core),
				});
		}
		if (result.error.ok()) {
			{
				std::lock_guard<std::mutex> lock(mutex_);
				status_.state = State::running_game;
				status_.execution = Execution::game;
				status_.system = prepared.system;
				status_.core = result.observed_core;
				status_.generation = generation;
				status_.error = {};
			}
			Log("launch", prepared.system, result.observed_core, "running");
			{
				std::lock_guard<std::mutex> lock(mutex_);
				busy_ = false;
			}
			condition_.notify_all();
			return {};
		}
		return FinishLaunchFailure("launch", prepared.system,
			result.observed_core.empty() ? prepared.expected_core : result.observed_core,
			result);
	}

	Error LoadCore(const std::string& directory, const std::string& expected_package_id,
		const std::string& data_root = "")
	{
		LogRecord rejection;
		bool rejected = false;
		bool replacing = false;
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (busy_ || !started_ || status_.state == State::starting ||
				status_.state == State::reboot_required) {
				rejection = {"load_core", "", "", "validate",
					Busy("runtime mutation is busy")};
				rejected = true;
			} else {
				busy_ = true;
				replacing = status_.state == State::running_game ||
					status_.state == State::running_development;
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
		const Error admitted = hardware_.AdmitCorePackage(directory,
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
				result);
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
			if (info.descriptor.abi.id == "fes.simple-game" ||
				info.descriptor.abi.id == "fes.simple-computer" ||
				info.descriptor.abi.id == "fes.application") {
				status_.active_package.observed.abi = info.descriptor.abi;
				status_.active_package.observed.build_id = info.descriptor.build.id;
			}
			status_.capabilities.active_interfaces = ActiveInterfaces(
				info.descriptor, status_.capabilities);
			status_.capabilities.media_stream = hardware_.capabilities().media_stream;
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
			std::lock_guard<std::mutex> lock(mutex_);
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

	Error LoadDevelopmentRBF(const std::string& rbf, bool contained = false)
	{
		LogRecord rejection;
		bool rejected = false;
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (busy_ || !started_ || status_.state != State::idle) {
				rejection = {"load_development_rbf", "", "", "validate",
					Busy("runtime is not idle")};
				rejected = true;
			} else {
				busy_ = true;
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
		const HardwareResult result = contained ?
			hardware_.LoadContainedDevelopmentRBF(rbf, generation) :
			hardware_.LoadDevelopmentRBF(rbf, generation);
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
		return FinishLaunchFailure("load_development_rbf", "", "", result);
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
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (busy_ || !started_ || status_.state != State::running_development)
				return Busy("FES computer is not running");
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

	Error LoadComputerMedia(const std::string& path)
	{
		if (!ValidAbsolutePath(path))
			return Invalid("invalid computer media path");
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (busy_ || !started_ || status_.state != State::running_development)
				return Busy("FES computer is not running");
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

	Error Stop()
	{
		LogRecord immediate;
		bool return_immediately = false;
		std::uint64_t retired_generation = 0;
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (busy_ || pending_fault_generation_ != 0 || !started_) {
				immediate = {"stop", status_.system, status_.core, "validate",
					Busy("runtime mutation is busy")};
				return_immediately = true;
			} else if (status_.state == State::reboot_required) {
				immediate = {"stop", "", "", "validate",
					status_.error};
				return_immediately = true;
			} else if (status_.state == State::idle) {
				status_.error = {};
				immediate = {"stop", "", "", "idle", {}};
				return_immediately = true;
			} else if (status_.state != State::running_game &&
				status_.state != State::running_development) {
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

	private:
	Error RestoreAfterSaveFailure(const std::string& operation,
		const std::string& system, const std::string& core,
		std::uint64_t generation, const Error& cause)
	{
		const Error save_error = SaveFailure(cause);
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
				busy_ = false;
			}
			condition_.notify_all();
			Log(operation, system, core, "save", save_error);
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

	Status FreshStatus(State state) const
	{
		Status result;
		result.state = state;
		result.capabilities = hardware_.capabilities();
		result.capabilities.media_stream = {};
		return result;
	}

	Error FinishLaunchFailure(const std::string& operation,
		const std::string& system, const std::string& core,
		const HardwareResult& result)
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
			status_ = FreshStatus(State::idle);
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
					(status_.state != State::running_game &&
					 status_.state != State::running_development))
					continue;
				busy_ = true;
				active_generation_ = 0;
				pending_fault_generation_ = 0;
				system = status_.system;
				core = status_.core;
				status_ = FreshStatus(State::starting);
				status_.execution = Execution::none;
			}

			Log("input_fault", system, core, "failure", fault.error);
			Log("input_fault", system, core, "cleanup");
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
	const Profiles& profiles_;
	LogSink& log_;
	Status status_;
	std::deque<HardwareFault> faults_;
	bool busy_;
	bool started_;
	bool stopping_;
	std::uint64_t next_generation_;
	std::uint64_t active_generation_;
	std::uint64_t pending_fault_generation_;
	std::thread fault_thread_;
};

Runtime::Runtime(Hardware& hardware, const Profiles& profiles, LogSink& log)
	: impl_(new Impl(hardware, profiles, log)) {}

Runtime::~Runtime() = default;

Error Runtime::Start() { return impl_->Start(); }
Status Runtime::status() const { return impl_->status(); }
Error Runtime::LaunchGame(const Launch& launch) { return impl_->LaunchGame(launch); }
Error Runtime::LoadCore(const std::string& directory,
	const std::string& expected_package_id)
{
	return impl_->LoadCore(directory, expected_package_id);
}
Error Runtime::LoadLibraryCore(
	const std::string& directory, const std::string& id, const std::string& root)
{
	if (!ValidAbsolutePath(root))
		return Invalid("invalid core-data root");
	return impl_->LoadCore(directory, id, root);
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
Error Runtime::LoadDevelopmentRBF(const std::string& rbf)
{
	return impl_->LoadDevelopmentRBF(rbf);
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

Error Runtime::LoadComputerMediaStream(const std::string& path,
	const std::string& package_id, std::uint64_t generation, std::uint32_t size)
{
	return impl_->LoadComputerMediaStream(path, package_id, generation, size);
}
Error Runtime::LoadContainedDevelopmentRBF(const std::string& rbf)
{
	return impl_->LoadDevelopmentRBF(rbf, true);
}
Error Runtime::Stop() { return impl_->Stop(); }

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
	case State::running_game: return "running_game";
	case State::running_development: return "running_development";
	case State::reboot_required: return "reboot_required";
	}
	return "invalid";
}

const char* ExecutionName(Execution execution)
{
	switch (execution) {
	case Execution::none: return "none";
	case Execution::game: return "game";
	case Execution::development: return "development";
	}
	return "invalid";
}

} // namespace mister
