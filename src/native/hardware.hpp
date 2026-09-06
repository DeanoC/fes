// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"

#include <cstdint>
#include <mutex>
#include <memory>
#include <string>

namespace mister {
namespace native {

class Artifact;
class SaveFile;
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
};

struct NativeResult {
	Error error;
	bool mutation_attempted = false;
};

class FpgaManager {
public:
	virtual ~FpgaManager() {}
	virtual NativeResult Program(const Artifact&,
		std::uint64_t absolute_deadline_ms) = 0;
};

class NativeHardware final : public Hardware {
public:
	NativeHardware(ArtifactOpener&, FpgaManager&, CoreLoader&, VideoBringup&,
		FixedVideoBringup&, InputSession&, const InputDeviceIdentity&, Clock&,
		LogSink&, std::string idle_rbf, NativeTimeouts);
	~NativeHardware();
	void SetFaultSink(HardwareFaultSink*) override;
	HardwareResult LoadIdle() override;
	Error FlushSave() override;
	HardwareResult Launch(const PreparedLaunch&, std::uint64_t generation) override;
	HardwareResult LoadDevelopmentRBF(const std::string&) override;

private:
	Error StopInput(std::uint64_t absolute_deadline_ms);
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
	std::mutex fault_sink_mutex_;
	HardwareFaultSink* fault_sink_;
	bool input_open_;
	std::unique_ptr<SaveFile> save_;
	std::vector<unsigned char> snapshot_;
	bool save_flushed_ = false;
};

} // namespace native
} // namespace mister
