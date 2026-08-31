// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include <cstdint>
#include <memory>
#include <string>
#include <vector>

namespace mister {

enum class ErrorCode {
	none,
	invalid_request,
	unsupported_protocol,
	unknown_system,
	missing_media,
	busy,
	program_failed,
	core_mismatch,
	io_failed,
	idle_failed,
};

enum class State {
	idle,
	starting,
	running_game,
	running_development,
	reboot_required,
};

enum class Execution { none, game, development };

struct Error {
	ErrorCode code = ErrorCode::none;
	std::string message;
	bool ok() const { return code == ErrorCode::none; }
};

struct LogRecord {
	std::string operation;
	std::string system;
	std::string core;
	std::string phase;
	Error error;
};

class LogSink {
public:
	virtual ~LogSink() {}
	virtual void Write(const LogRecord&) = 0;
};

struct Media {
	std::string role;
	std::string path;
};

struct Setting {
	std::string name;
	std::string value;
};

struct Launch {
	std::string system;
	std::string rbf;
	std::vector<Media> media;
	std::vector<Setting> settings;
};

struct Status {
	State state = State::starting;
	Execution execution = Execution::none;
	std::string system;
	std::string core;
	Error error;
};

struct MediaRule {
	std::string role;
	std::uint8_t index = 0;
	bool required = false;
};

struct SettingRule {
	std::string name;
	std::vector<std::string> allowed_values;
};

struct Profile {
	std::string system;
	std::string expected_core;
	std::vector<MediaRule> media;
	std::vector<SettingRule> settings;
};

struct PreparedMedia {
	std::uint8_t index = 0;
	std::string path;
};

struct PreparedLaunch {
	std::string system;
	std::string expected_core;
	std::string rbf;
	std::vector<PreparedMedia> media;
	std::vector<Setting> settings;
};

class Profiles {
public:
	Error Add(Profile);
	Error Prepare(const Launch&, PreparedLaunch*) const;
	bool empty() const;

private:
	std::vector<Profile> profiles_;
};

struct HardwareResult {
	Error error;
	bool mutation_attempted = false;
	std::string observed_core;
};

class Hardware {
public:
	virtual ~Hardware() {}
	virtual HardwareResult LoadIdle() = 0;
	virtual HardwareResult Launch(const PreparedLaunch&) = 0;
	virtual HardwareResult LoadDevelopmentRBF(const std::string&) = 0;
};

class Runtime {
public:
	Runtime(Hardware&, const Profiles&, LogSink&);
	~Runtime();
	Runtime(const Runtime&) = delete;
	Runtime& operator=(const Runtime&) = delete;
	Error Start();
	Status status() const;
	Error LaunchGame(const Launch&);
	Error LoadDevelopmentRBF(const std::string&);
	Error Stop();

private:
	class Impl;
	std::unique_ptr<Impl> impl_;
};

const char* ErrorCodeName(ErrorCode);
const char* StateName(State);
const char* ExecutionName(Execution);

} // namespace mister
