// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"

#include <cstdint>
#include <string>

namespace mister {
namespace native {

class Artifact;
class ArtifactOpener;
class CoreLoader;

class Clock {
public:
	virtual ~Clock() {}
	virtual std::uint64_t NowMs() const = 0;
};

struct NativeTimeouts {
	std::uint32_t program_ms = 30000;
	std::uint32_t core_io_ms = 10000;
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
	NativeHardware(ArtifactOpener&, FpgaManager&, CoreLoader&, Clock&,
		LogSink&, std::string idle_rbf, NativeTimeouts);
	HardwareResult LoadIdle() override;
	HardwareResult Launch(const PreparedLaunch&) override;
	HardwareResult LoadDevelopmentRBF(const std::string&) override;

private:
	ArtifactOpener& opener_;
	FpgaManager& fpga_;
	CoreLoader& core_;
	Clock& clock_;
	LogSink& log_;
	std::string idle_rbf_;
	NativeTimeouts timeouts_;
};

} // namespace native
} // namespace mister
