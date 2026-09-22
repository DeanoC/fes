// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include <cstdint>
#include <memory>
#include <string>
#include <utility>
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
	invalid_package,
	unsupported_target,
	unsupported_programming_profile,
	unsupported_abi,
	unsupported_interface,
	corrupt_data,
	incompatible_data,
	stale_revision,
};

enum class State {
	idle,
	starting,
	running_development,
	reboot_required,
};

enum class Execution { none, development };

struct Error {
	Error() = default;
	Error(ErrorCode code_value, std::string message_value,
		std::string phase_value = {}, std::string expected_value = {},
		std::string observed_value = {})
		: code(code_value), message(std::move(message_value)),
		  phase(std::move(phase_value)), expected(std::move(expected_value)),
		  observed(std::move(observed_value)) {}
	ErrorCode code = ErrorCode::none;
	std::string message;
	std::string phase;
	std::string expected;
	std::string observed;
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

struct CoreMetadata {
	std::string id;
	std::string name;
	std::string description;
	std::string version;
	std::string system;
};

struct CoreTarget {
	std::string platform;
	std::string device;
	std::string programming_profile;
};

struct CorePayload {
	std::string file;
	std::uint64_t size = 0;
	std::string sha256;
};

struct VersionedContract {
	std::string id;
	std::uint16_t major = 0;
	std::uint16_t minor = 0;
};

struct CoreInterface {
	std::string id;
	std::uint16_t major = 0;
	std::uint16_t minor = 0;
	bool required = false;
};

struct CoreBuild {
	std::string id;
	std::string repository;
	std::string revision;
	std::string recipe_sha256;
	std::string toolchain;
};

struct CoreDescriptor {
	std::uint16_t format = 0;
	CoreMetadata core;
	CoreTarget target;
	CorePayload payload;
	VersionedContract abi;
	std::vector<CoreInterface> interfaces;
	CoreBuild build;
};

struct SupportedInterface {
	std::string id;
	std::uint16_t major = 0;
	std::uint16_t minor = 0;
};

struct SupportedABI {
	std::string id;
	std::uint16_t major = 0;
	std::uint16_t minor = 0;
	std::vector<SupportedInterface> interfaces;
};

struct MediaStreamCapability {
	SupportedInterface interface;
	std::uint32_t min_bytes = 0;
	std::uint32_t max_bytes = 0;
	std::uint32_t chunk_bytes = 0;
};

struct Capabilities {
	std::vector<std::string> programming_profiles;
	std::vector<SupportedABI> abis;
	std::vector<SupportedInterface> active_interfaces;
	// Empty interface ID means absent. This is observed active-session data,
	// not the compiled driver declaration registry above.
	MediaStreamCapability media_stream;
};

struct ObservedIdentity {
	VersionedContract abi;
	std::string build_id;
};

struct CoreComposition {
	std::string id, package_id, expansion_id, shell_sha256, payload_sha256;
	std::uint64_t payload_size = 0;
};

struct CoreCompositionRequest {
	std::string expansion_path, payload_path;
	CoreComposition composition;
};

struct ActiveCorePackage {
	std::string package_id;
	CoreDescriptor descriptor;
	ObservedIdentity observed;
	CoreComposition composition;
};

struct CoreData {
	std::string package_id;
	std::string core_id;
	VersionedContract layout;
	std::string mode = "volatile";
	std::string revision = "absent";
	std::uint16_t paddle_speed = 1;
	std::uint16_t best_rally = 0;
};

struct CorePackageInspection {
	std::string package_id;
	CoreDescriptor descriptor;
	VersionedContract persistence_layout;
	bool compatible = false;
	Error compatibility_error;
};

struct Status {
	State state = State::starting;
	Execution execution = Execution::none;
	std::string system;
	std::string core;
	std::string package_id;
	std::string declared_core;
	Capabilities capabilities;
	ActiveCorePackage active_package;
	CoreData core_data;
	std::uint64_t generation = 0;
	Error error;
};

struct InputRecipe {
	std::uint8_t player_count = 0;
	std::uint16_t player_command = 0;
	std::uint16_t up = 0, down = 0, left = 0, right = 0;
	std::uint16_t a = 0, b = 0, c = 0, start = 0;
	std::uint16_t x = 0, y = 0, l = 0, r = 0, select = 0;
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

struct CorePackageInfo {
	std::string package_id;
	std::string declared_core;
	std::string system;
	CoreDescriptor descriptor;
};

class AdmittedCorePackage {
public:
	virtual ~AdmittedCorePackage() {}
	const CorePackageInfo& info() const { return info_; }
	virtual CoreComposition composition() const { return {}; }

protected:
	explicit AdmittedCorePackage(CorePackageInfo info)
		: info_(std::move(info)) {}

private:
	CorePackageInfo info_;
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
	// FlushSave may stop input before a persistence failure. RestoreInput
	// re-establishes the still-active generation after that proven pre-mutation
	// failure.
	virtual Error RestoreInput(std::uint64_t) { return {}; }
	virtual Error AdmitCoreComposition(const std::string&, const std::string&,
		const CoreCompositionRequest&, std::unique_ptr<AdmittedCorePackage>*)
	{
		return {ErrorCode::unsupported_protocol, "core composition loading is unavailable"};
	}
	virtual Error AdmitCorePackage(const std::string&, const std::string&,
		std::unique_ptr<AdmittedCorePackage>*)
	{
		return {ErrorCode::unsupported_protocol,
			"core package loading is unavailable"};
	}
	virtual Error InspectCorePackage(const std::string&, const std::string&,
		CorePackageInspection*)
	{
		return {ErrorCode::unsupported_protocol,
			"core package inspection is unavailable"};
	}
	virtual Error PrepareCoreData(AdmittedCorePackage*, const std::string&, CoreData*)
	{
		return {ErrorCode::unsupported_interface, "core data unavailable"};
	}
	virtual Error RefreshCoreData(AdmittedCorePackage*, CoreData*)
	{
		return {};
	}
	virtual Error AttachProgrammedBitstream(AdmittedCorePackage*, const std::string&,
		const std::string&)
	{
		return {ErrorCode::unsupported_protocol, "programmed bitstream is unavailable"};
	}
	virtual Error InspectCoreData(
		const std::string&, const std::string&, const std::string&, CoreData*)
	{
		return {ErrorCode::unsupported_interface, "core data unavailable"};
	}
	virtual Error UpdateCoreSettings(const std::string&, const std::string&, const std::string&,
		const std::string&, std::uint16_t, CoreData*)
	{
		return {ErrorCode::unsupported_interface, "core data unavailable"};
	}
	virtual Capabilities capabilities() const { return {}; }
	virtual HardwareResult LoadCore(std::unique_ptr<AdmittedCorePackage>,
		std::uint64_t)
	{
		return {{ErrorCode::unsupported_protocol,
			"core package loading is unavailable"}, false, ""};
	}
	virtual HardwareResult LoadContainedDevelopmentRBF(const std::string&,
		std::uint64_t)
	{
		return {{ErrorCode::unsupported_programming_profile,
			"contained development loading is unavailable", "compatibility"},
			false, ""};
	}
	virtual Error SetController(std::uint8_t, std::uint16_t, std::uint16_t)
	{
		return {ErrorCode::unsupported_interface, "controller ports are unavailable", "input"};
	}
	virtual Error SetComputerKeyboard(std::uint64_t)
	{
		return {ErrorCode::unsupported_interface,
			"computer keyboard is unavailable", "input"};
	}
	virtual Error LoadComputerMedia(const std::string&)
	{
		return {ErrorCode::unsupported_interface,
			"computer media is unavailable", "request"};
	}
	virtual Error LoadComputerMediaLive(const std::string&)
	{
		return {ErrorCode::unsupported_interface,
			"live computer media is unavailable", "request"};
	}
	virtual Error ClearComputerMedia()
	{
		return {ErrorCode::unsupported_interface,
			"clear computer media is unavailable", "request"};
	}
	virtual Error LoadComputerFirmware(const std::string&)
	{
		return {ErrorCode::unsupported_interface,
			"firmware slot is unavailable", "request"};
	}
	virtual Error LoadComputerMediaStream(const std::string&, std::uint32_t)
	{
		return {ErrorCode::unsupported_interface, "computer media stream is unavailable", "request"};
	}
};

class Runtime {
public:
	Runtime(Hardware&, LogSink&);
	~Runtime();
	Runtime(const Runtime&) = delete;
	Runtime& operator=(const Runtime&) = delete;
	Error Start();
	Status status() const;
	Error LoadCore(const std::string& directory,
		const std::string& expected_package_id);
	Error LoadLibraryCore(const std::string&, const std::string&, const std::string&);
	Error LoadComposedCore(const std::string&, const std::string&, const CoreCompositionRequest&);
	Error LoadInitializedCore(const std::string&, const std::string&, const std::string&, const std::string&);
	Error LoadInitializedLibraryCore(const std::string&, const std::string&, const std::string&,
		const std::string&, const std::string&);
	Error LoadInitializedComposedCore(const std::string&, const std::string&, const CoreCompositionRequest&,
		const std::string&, const std::string&);
	Error InspectCoreData(const std::string&, const std::string&, const std::string&, CoreData*);
	Error UpdateCoreSettings(const std::string&, const std::string&, const std::string&,
		const std::string&, std::uint16_t, CoreData*);
	Error InspectCore(const std::string& directory,
		const std::string& expected_package_id, CorePackageInspection*);
	Error LoadContainedDevelopmentRBF(const std::string&);
	Error SetComputerKeyboard(std::uint64_t matrix);
	Error SetController(const std::string& package_id, std::uint64_t generation,
		std::uint8_t port, std::uint16_t buttons, std::uint16_t keypad);
	// Launch-time hold-reset primary bind (optional package/generation when unbound).
	Error LoadComputerMedia(const std::string& path);
	Error LoadComputerMedia(const std::string& path, const std::string& expected_package_id,
		std::uint64_t expected_generation);
	// Mid-session mailbox fill without hold-reset soft-reboot.
	Error ReplaceLiveComputerMedia(const std::string& path,
		const std::string& expected_package_id, std::uint64_t expected_generation);
	Error ClearComputerMedia(const std::string& expected_package_id,
		std::uint64_t expected_generation);
	Error LoadComputerFirmware(const std::string& path);
	Error LoadComputerMediaStream(const std::string& path,
		const std::string& expected_package_id, std::uint64_t expected_generation,
		std::uint32_t size);
	// RecoverIdle retries protocol-2 idle restore from reboot_required.
	// It does not reboot the board. Idle is already recovered. Any other
	// state is rejected so a running session still uses Stop.
	Error RecoverIdle();
	Error Stop();

private:
	class Impl;
	std::unique_ptr<Impl> impl_;
};

const char* ErrorCodeName(ErrorCode);
const char* StateName(State);
const char* ExecutionName(Execution);

} // namespace mister
