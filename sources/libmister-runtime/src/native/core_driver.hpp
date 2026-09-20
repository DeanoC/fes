// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"

#include <cstdint>
#include <functional>
#include <string>

namespace mister {
namespace native {

class Clock;
class CoreLoader;
class Mmio;
enum class ProgrammingProfile {
	mister_v1,
	fes_gp_v1,
	development_contained_v1,
};

struct CoreDriverContext {
	const CoreDescriptor* descriptor = nullptr;
	const CoreRecipe* mister_recipe = nullptr;
	std::string expected_core;
	std::uint64_t generation = 0;
	std::uint16_t player_command = 0;
	std::function<void(std::uint64_t, Error)> report_fault;
};

struct CoreDriverResult {
	Error error;
	bool mutation_attempted = false;
	std::string observed_core;
	// Identify may authorize one bounded cleanup command only after the
	// destination protocol and ABI have been verified.
	bool safe_to_quiesce = false;
};

class CoreDriver {
public:
	virtual ~CoreDriver() {}
	virtual void BeginSession() {}
	virtual Error CaptureData(const CoreDriverContext&, std::uint64_t, std::vector<std::uint16_t>*)
	{
		return {ErrorCode::unsupported_interface, "persistence unavailable"};
	}
	virtual Error RestoreData(
		const CoreDriverContext&, const std::vector<std::uint16_t>&, std::uint64_t)
	{
		return {ErrorCode::unsupported_interface, "persistence unavailable"};
	}
	virtual Error ResumeData(const CoreDriverContext&, std::uint64_t)
	{
		return {ErrorCode::unsupported_interface, "persistence unavailable"};
	}
	virtual CoreDriverResult Quiesce(const CoreDriverContext&,
		std::uint64_t absolute_deadline_ms) = 0;
	virtual CoreDriverResult Identify(const CoreDriverContext&,
		std::uint64_t absolute_deadline_ms) = 0;
	virtual CoreDriverResult NeutralizeButtons(const CoreDriverContext&,
		std::uint64_t absolute_deadline_ms) = 0;
	virtual CoreDriverResult SetButtons(const CoreDriverContext&, std::uint16_t,
		std::uint64_t absolute_deadline_ms) = 0;
	virtual CoreDriverResult Start(const CoreDriverContext&,
		std::uint64_t absolute_deadline_ms) = 0;
};

class MisterCoreDriver final : public CoreDriver {
public:
	MisterCoreDriver(Mmio&, CoreLoader&, Clock&);
	CoreDriverResult Quiesce(const CoreDriverContext&, std::uint64_t) override;
	CoreDriverResult Identify(const CoreDriverContext&, std::uint64_t) override;
	CoreDriverResult NeutralizeButtons(const CoreDriverContext&, std::uint64_t) override;
	CoreDriverResult SetButtons(const CoreDriverContext&, std::uint16_t,
		std::uint64_t) override;
	CoreDriverResult Start(const CoreDriverContext&, std::uint64_t) override;

private:
	Mmio& mmio_;
	CoreLoader& core_;
	Clock& clock_;
};

class ContainedCoreDriver final : public CoreDriver {
public:
	CoreDriverResult Quiesce(const CoreDriverContext&, std::uint64_t) override;
	CoreDriverResult Identify(const CoreDriverContext&, std::uint64_t) override;
	CoreDriverResult NeutralizeButtons(const CoreDriverContext&, std::uint64_t) override;
	CoreDriverResult SetButtons(const CoreDriverContext&, std::uint16_t,
		std::uint64_t) override;
	CoreDriverResult Start(const CoreDriverContext&, std::uint64_t) override;
};

class CoreDriverRegistry {
public:
	CoreDriverRegistry(CoreDriver& mister, CoreDriver* fes_gp,
		CoreDriver& contained);
	Error Resolve(const CoreDescriptor&, ProgrammingProfile*, CoreDriver**) const;
	CoreDriver* Resolve(ProgrammingProfile) const;

private:
	CoreDriver& mister_;
	CoreDriver* fes_gp_;
	CoreDriver& contained_;
};

Error ParseProgrammingProfile(const std::string&, ProgrammingProfile*);

} // namespace native
} // namespace mister
