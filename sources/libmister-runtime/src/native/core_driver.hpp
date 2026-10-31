// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"
#include "native/platform.hpp"

#include <cstdint>
#include <functional>
#include <string>

namespace mister {
namespace native {

class ComputerMediaSnapshot;
class Clock;
class Mmio;
enum class ProgrammingProfile {
	fes_gp_v1,
	development_contained_v1,
	// Selected by the DE25-only shared programming-profile row.
	de25_hps_first_v1,
};

struct CoreDriverContext {
	const CoreDescriptor* descriptor = nullptr;
	std::string expected_core;
	std::uint64_t generation = 0;
	std::function<void(std::uint64_t, Error)> report_fault;
};

struct CoreDriverResult {
	Error error;
	bool mutation_attempted = false;
	std::string observed_core;
	// Identify may authorize one bounded cleanup command only after the
	// destination protocol and ABI have been verified.
	bool safe_to_quiesce = false;
	// A failed Start can be a completed rejection or an ambiguous release. Only
	// the latter may have let the guest run and require fault-save capture.
	bool execution_may_have_run = false;
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
	// Optional computer controls belong to the selected driver. Drivers that
	// do not implement them reject before any hardware access.
	virtual Error SetKeyboardHid(const KeyboardHidRows&, std::uint64_t)
	{ return {ErrorCode::unsupported_interface, "keyboard HID unavailable", "input"}; }
	virtual Error LoadMedia(const std::vector<std::uint8_t>&, std::uint64_t)
	{ return {ErrorCode::unsupported_interface, "computer media unavailable", "request"}; }
	virtual Error LoadMediaLive(const std::vector<std::uint8_t>&, std::uint64_t)
	{ return {ErrorCode::unsupported_interface, "live computer media unavailable", "request"}; }
	virtual Error ClearMedia(std::uint64_t)
	{ return {ErrorCode::unsupported_interface, "computer media unavailable", "request"}; }
	virtual const std::vector<MediaUnitCapability>& media_units() const
	{ static const std::vector<MediaUnitCapability> empty; return empty; }
	virtual Error InsertMedia(std::uint8_t, const ComputerMediaSnapshot&, Clock&,
		std::uint64_t, std::uint32_t)
	{ return {ErrorCode::unsupported_interface, "computer media unit unavailable", "request"}; }
	virtual Error EjectMedia(std::uint8_t, std::uint64_t)
	{ return {ErrorCode::unsupported_interface, "computer media unit unavailable", "request"}; }
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
	CoreDriverRegistry(CoreDriver* fes_gp,
		CoreDriver& contained, CoreDriver* de25 = nullptr,
		NativePlatform platform = NativePlatform::de10_nano);
	Error Resolve(const CoreDescriptor&, ProgrammingProfile*, CoreDriver**) const;
	CoreDriver* Resolve(ProgrammingProfile) const;

private:
	NativePlatform platform_;
	CoreDriver* de25_;
	CoreDriver* fes_gp_;
	CoreDriver& contained_;
};

Error ParseProgrammingProfile(const std::string&, ProgrammingProfile*);

} // namespace native
} // namespace mister
