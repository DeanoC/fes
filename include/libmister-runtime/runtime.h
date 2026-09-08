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
	save_failed,
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
	std::string save_path;
};

struct Status {
	State state = State::starting;
	Execution execution = Execution::none;
	std::string system;
	std::string core;
	Error error;
};

enum class MediaTransform { raw, snes_cartridge, nes_cartridge };

struct MediaRule {
	std::string role;
	std::uint8_t index = 0;
	bool required = false;
	std::vector<std::string> extensions;
	std::uint64_t maximum_size = 0;
	MediaTransform transform = MediaTransform::raw;
};

struct SettingRule {
	std::string name;
	std::vector<std::string> allowed_values;
};

// MiSTer's hps_io can expose downloaded bytes as either 16-bit words or
// individual 8-bit values carried in the low byte of each SPI word.
enum class FileWireFormat {
	little_endian_byte_pairs,
	little_endian_bytes
};

struct CoreRecipe {
	std::uint16_t reset_assert_word = 0;
	std::uint16_t initial_status_word = 0;
	std::uint16_t reset_release_word = 0;
	FileWireFormat file_wire = FileWireFormat::little_endian_byte_pairs;
};

struct InputRecipe {
	std::uint8_t player_count = 0;
	std::uint16_t player_command = 0;
	std::uint16_t up = 0, down = 0, left = 0, right = 0;
	std::uint16_t a = 0, b = 0, c = 0, start = 0;
	std::uint16_t x = 0, y = 0, l = 0, r = 0, select = 0;
};

struct Profile {
	std::string system;
	std::string expected_core;
	std::string rbf;
	std::vector<MediaRule> media;
	std::vector<SettingRule> settings;
	CoreRecipe core;
	InputRecipe input;
};

struct PreparedMedia {
	std::uint8_t index = 0;
	std::string path;
	std::uint64_t maximum_size = 0;
	MediaTransform transform = MediaTransform::raw;
};

struct PreparedLaunch {
	std::string save_path;
	std::string system;
	std::string expected_core;
	std::string rbf;
	std::vector<PreparedMedia> media;
	std::vector<Setting> settings;
	CoreRecipe core;
	InputRecipe input;
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

struct HardwareFault {
	std::uint64_t generation = 0;
	Error error;
};

class HardwareFaultSink {
public:
	virtual ~HardwareFaultSink() = default;
	virtual void ReportHardwareFault(HardwareFault fault) = 0;
};

class Hardware {
public:
	virtual ~Hardware() {}
	virtual void SetFaultSink(HardwareFaultSink*) = 0;
	virtual HardwareResult LoadIdle() = 0;
	virtual Error FlushSave() { return {}; }
	virtual HardwareResult Launch(const PreparedLaunch&,
		std::uint64_t generation) = 0;
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
