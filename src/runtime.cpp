// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "libmister-runtime/runtime.h"

#include <mutex>
#include <utility>

namespace mister {
namespace {

Error Busy(const char* message)
{
	return {ErrorCode::busy, message};
}

Error Invalid(const char* message)
{
	return {ErrorCode::invalid_request, message};
}

Error IdleFailure(const Error& cause)
{
	return {ErrorCode::idle_failed,
		cause.message.empty() ? "idle load failed" : cause.message};
}

bool ValidAbsolutePath(const std::string& path)
{
	return !path.empty() && path.size() <= 4095 && path[0] == '/' &&
		path.find('\0') == std::string::npos;
}

} // namespace

class Runtime::Impl {
public:
	Impl(Hardware& hardware, const Profiles& profiles, LogSink& log)
		: mutex_(), hardware_(hardware), profiles_(profiles), log_(log), status_(),
		  busy_(false), started_(false) {}

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
				status_ = {};
				status_.state = State::starting;
			}
		}
		if (rejected) {
			log_.Write(rejection);
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
			return error;
		}
		{
			std::lock_guard<std::mutex> lock(mutex_);
			status_ = {};
			status_.state = State::idle;
			busy_ = false;
		}
		Log("start", "", "", "idle");
		return {};
	}

	Status status() const
	{
		std::lock_guard<std::mutex> lock(mutex_);
		return status_;
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
		{
			std::lock_guard<std::mutex> lock(mutex_);
			status_.state = State::starting;
			status_.execution = Execution::game;
			status_.system = prepared.system;
			status_.core = prepared.expected_core;
			status_.error = {};
		}
		Log("launch", prepared.system, prepared.expected_core, "starting");

		HardwareResult result = hardware_.Launch(prepared);
		if (result.error.ok() && result.observed_core != prepared.expected_core) {
			result.error = {ErrorCode::core_mismatch,
				"observed core does not match profile"};
			result.mutation_attempted = true;
		}
		if (result.error.ok()) {
			{
				std::lock_guard<std::mutex> lock(mutex_);
				status_.state = State::running_game;
				status_.execution = Execution::game;
				status_.system = prepared.system;
				status_.core = result.observed_core;
				status_.error = {};
				busy_ = false;
			}
			Log("launch", prepared.system, result.observed_core, "running");
			return {};
		}
		return FinishLaunchFailure("launch", prepared.system,
			result.observed_core.empty() ? prepared.expected_core : result.observed_core,
			result);
	}

	Error LoadDevelopmentRBF(const std::string& rbf)
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
		{
			std::lock_guard<std::mutex> lock(mutex_);
			status_ = {};
			status_.state = State::starting;
			status_.execution = Execution::development;
		}
		Log("load_development_rbf", "", "", "starting");
		const HardwareResult result = hardware_.LoadDevelopmentRBF(rbf);
		if (result.error.ok()) {
			{
				std::lock_guard<std::mutex> lock(mutex_);
				status_ = {};
				status_.state = State::running_development;
				status_.execution = Execution::development;
				busy_ = false;
			}
			Log("load_development_rbf", "", "", "running");
			return {};
		}
		return FinishLaunchFailure("load_development_rbf", "", "", result);
	}

	Error Stop()
	{
		LogRecord immediate;
		bool return_immediately = false;
		{
			std::lock_guard<std::mutex> lock(mutex_);
			if (busy_ || !started_) {
				immediate = {"stop", status_.system, status_.core, "validate",
					Busy("runtime mutation is busy")};
				return_immediately = true;
			} else if (status_.state == State::reboot_required) {
				immediate = {"stop", "", "", "validate",
					{ErrorCode::idle_failed, status_.error.message.empty() ?
						"reboot required" : status_.error.message}};
				return_immediately = true;
			} else if (status_.state == State::idle) {
				immediate = {"stop", "", "", "idle", {}};
				return_immediately = true;
			} else if (status_.state != State::running_game &&
				status_.state != State::running_development) {
				immediate = {"stop", status_.system, status_.core, "validate",
					Busy("runtime is not stoppable")};
				return_immediately = true;
			} else {
				busy_ = true;
				status_.state = State::starting;
				status_.execution = Execution::none;
			}
		}
		if (return_immediately) {
			log_.Write(immediate);
			return immediate.error;
		}
		Log("stop", "", "", "starting");
		const HardwareResult result = hardware_.LoadIdle();
		if (!result.error.ok()) {
			const Error error = IdleFailure(result.error);
			{
				std::lock_guard<std::mutex> lock(mutex_);
				status_ = {};
				status_.state = State::reboot_required;
				status_.error = error;
				busy_ = false;
			}
			Log("stop", "", "", "failure", error);
			return error;
		}
		{
			std::lock_guard<std::mutex> lock(mutex_);
			status_ = {};
			status_.state = State::idle;
			busy_ = false;
		}
		Log("stop", "", "", "idle");
		return {};
	}

private:
	Error FinishLaunchFailure(const std::string& operation,
		const std::string& system, const std::string& core,
		const HardwareResult& result)
	{
		const Error primary = result.error;
		Log(operation, system, core, "failure", primary);
		if (!result.mutation_attempted) {
			std::lock_guard<std::mutex> lock(mutex_);
			status_ = {};
			status_.state = State::idle;
			status_.error = primary;
			busy_ = false;
			return primary;
		}
		Log(operation, system, core, "cleanup");
		const HardwareResult cleanup = hardware_.LoadIdle();
		if (cleanup.error.ok()) {
			{
				std::lock_guard<std::mutex> lock(mutex_);
				status_ = {};
				status_.state = State::idle;
				status_.error = primary;
				busy_ = false;
			}
			return primary;
		}
		const Error idle_error = IdleFailure(cleanup.error);
		{
			std::lock_guard<std::mutex> lock(mutex_);
			status_ = {};
			status_.state = State::reboot_required;
			status_.error = idle_error;
			busy_ = false;
		}
		Log(operation, system, core, "cleanup", idle_error);
		return idle_error;
	}

	mutable std::mutex mutex_;
	Hardware& hardware_;
	const Profiles& profiles_;
	LogSink& log_;
	Status status_;
	bool busy_;
	bool started_;
};

Runtime::Runtime(Hardware& hardware, const Profiles& profiles, LogSink& log)
	: impl_(new Impl(hardware, profiles, log)) {}

Runtime::~Runtime() = default;

Error Runtime::Start() { return impl_->Start(); }
Status Runtime::status() const { return impl_->status(); }
Error Runtime::LaunchGame(const Launch& launch) { return impl_->LaunchGame(launch); }
Error Runtime::LoadDevelopmentRBF(const std::string& rbf)
{
	return impl_->LoadDevelopmentRBF(rbf);
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
