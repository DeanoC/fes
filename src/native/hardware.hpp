// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"
#include "native/core_driver.hpp"

#include <atomic>
#include <cstdint>
#include <mutex>
#include <memory>
#include <string>

namespace mister {
namespace native {

class Artifact;
class SaveFile;
class CoreDataFile;
class ArtifactOpener;
class CoreLoader;
class FixedVideoBringup;
class InputSession;
struct InputDeviceIdentity;
class VideoBringup;

class Clock {
public:
	virtual ~Clock() {}
	virtual std::uint64_t NowMs() const = 0;
};

struct NativeTimeouts {
	std::uint32_t program_ms = 30000;
	std::uint32_t core_io_ms = 10000;
	std::uint32_t video_ms = 10000;
	// Cartridge transfers use one deadline per media item. The Linux HPS SPI
	// path performs a bounded MMIO handshake for every 16-bit word, so a
	// normal core-control timeout is too short for larger ROMs.
	std::uint32_t media_io_ms = 120000;
};

struct NativeResult {
	Error error;
	bool mutation_attempted = false;
};

class FpgaManager {
public:
	virtual ~FpgaManager() {}
	virtual NativeResult Program(const Artifact&,
		ProgrammingProfile,
		std::uint64_t absolute_deadline_ms) = 0;
};

class NativeHardware final : public Hardware {
public:
	NativeHardware(ArtifactOpener&, FpgaManager&, CoreLoader&, VideoBringup&,
		FixedVideoBringup&, InputSession&, const InputDeviceIdentity&, Clock&,
		LogSink&, std::string idle_rbf, NativeTimeouts, CoreDriver& mister_driver,
		CoreDriver* fes_gp_driver = nullptr, const Profiles* profiles = nullptr,
		std::vector<std::string> package_roots = {});
	~NativeHardware();
	void SetFaultSink(HardwareFaultSink*) override;
	HardwareResult LoadIdle() override;
	Error FlushSave() override;
	Error RestoreInput(std::uint64_t generation) override;
	Error AdmitCorePackage(const std::string&, const std::string&,
		std::unique_ptr<AdmittedCorePackage>*) override;
	Error InspectCorePackage(const std::string&, const std::string&,
		CorePackageInspection*) override;
	Error PrepareCoreData(AdmittedCorePackage*, const std::string&, CoreData*) override;
	Error RefreshCoreData(AdmittedCorePackage*, CoreData*) override;
	Error InspectCoreData(
		const std::string&, const std::string&, const std::string&, CoreData*) override;
	Error UpdateCoreSettings(const std::string&, const std::string&, const std::string&,
		const std::string&, std::uint16_t, CoreData*) override;
	Capabilities capabilities() const override;
	HardwareResult LoadCore(std::unique_ptr<AdmittedCorePackage>,
		std::uint64_t generation) override;
	HardwareResult Launch(const PreparedLaunch&, std::uint64_t generation) override;
	HardwareResult LoadDevelopmentRBF(const std::string&,
		std::uint64_t generation = 0) override;
	HardwareResult LoadContainedDevelopmentRBF(const std::string&,
		std::uint64_t generation) override;
	HardwareResult LoadDevelopmentRBF(const std::string&, ProgrammingProfile,
		std::uint64_t generation = 0);

private:
	Error PrepareCoreDataInternal(AdmittedCorePackage*, const std::string&, CoreData*, bool);
	Error StopInput(std::uint64_t absolute_deadline_ms);
	HardwareResult QuiesceForReplacement(const char* operation,
		const std::string& system, const std::string& core);
	CoreDriver* ResolveDriver(ProgrammingProfile) const;
	void ForgetActiveCore();
	void ForwardInputFault(std::uint64_t generation, Error);
	ArtifactOpener& opener_;
	FpgaManager& fpga_;
	CoreLoader& core_;
	VideoBringup& idle_video_;
	FixedVideoBringup& game_video_;
	InputSession& input_;
	const InputDeviceIdentity& input_identity_;
	Clock& clock_;
	LogSink& log_;
	std::string idle_rbf_;
	NativeTimeouts timeouts_;
	CoreDriver& mister_driver_;
	CoreDriver* fes_gp_driver_;
	ContainedCoreDriver contained_driver_;
	CoreDriverRegistry driver_registry_;
	const Profiles* profiles_;
	std::vector<std::string> package_roots_;
	CoreDriver* active_driver_;
	CoreDriverContext active_context_;
	std::unique_ptr<AdmittedCorePackage> active_package_;
	std::mutex fault_sink_mutex_;
	HardwareFaultSink* fault_sink_;
	bool input_open_;
	std::shared_ptr<std::atomic<bool>> input_delivery_enabled_;
	InputRecipe active_input_recipe_;
	bool has_active_input_recipe_ = false;
	std::unique_ptr<CoreDataFile> core_data_file_;
	CoreData durable_data_;
	std::vector<std::uint16_t> core_snapshot_;
	bool core_data_flushed_ = false;
	std::unique_ptr<SaveFile> save_;
	std::vector<unsigned char> snapshot_;
	bool save_flushed_ = false;
};

} // namespace native
} // namespace mister
