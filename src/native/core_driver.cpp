// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/core_driver.hpp"

#include "native/core_loader.hpp"
#include "native/core_package.hpp"
#include "native/generated/de10_nano.hpp"
#include "native/hardware.hpp"
#include "native/linux/mmio.hpp"

#include <utility>

namespace mister {
namespace native {
namespace {

CoreDriverResult DriverFailure(Error error, bool attempted)
{
	if (error.code == ErrorCode::none)
		error = {ErrorCode::io_failed, "core driver operation failed"};
	return {std::move(error), attempted, ""};
}

} // namespace

MisterCoreDriver::MisterCoreDriver(Mmio& mmio, CoreLoader& core, Clock& clock)
	: mmio_(mmio), core_(core), clock_(clock) {}

CoreDriverResult MisterCoreDriver::Quiesce(const CoreDriverContext&,
	std::uint64_t deadline)
{
	if (clock_.NowMs() >= deadline)
		return DriverFailure({ErrorCode::io_failed, "MiSTer quiesce deadline exceeded"}, false);
	std::uint32_t gpo = 0;
	Error error = mmio_.Read32(generated::kFpgaGpoAddress, &gpo);
	if (!error.ok()) return DriverFailure(error, false);
	const std::uint32_t reset = (gpo & ~generated::kFpgaCoreStateMask) |
		generated::kFpgaCoreReset;
	error = mmio_.Write32(generated::kFpgaGpoAddress, reset);
	if (!error.ok()) return DriverFailure(error, true);
	return {{}, true, ""};
}

CoreDriverResult MisterCoreDriver::Identify(const CoreDriverContext& context,
	std::uint64_t deadline)
{
	std::string observed;
	Error error = core_.Probe(&observed, deadline);
	if (!error.ok()) return DriverFailure(error, false);
	if (!context.expected_core.empty() && observed != context.expected_core)
		return {{ErrorCode::core_mismatch,
			"observed core does not match package or profile"}, false, observed};
	return {{}, false, observed};
}

CoreDriverResult MisterCoreDriver::NeutralizeButtons(const CoreDriverContext&,
	std::uint64_t deadline)
{
	const Error error = core_.NeutralizeButtons(deadline);
	return error.ok() ? CoreDriverResult{{}, true, ""} :
		DriverFailure(error, true);
}

CoreDriverResult MisterCoreDriver::SetButtons(const CoreDriverContext& context,
	std::uint16_t map, std::uint64_t deadline)
{
	if (context.player_command == 0)
		return DriverFailure({ErrorCode::invalid_request,
			"MiSTer input command is unavailable"}, false);
	const Error error = core_.SetButtons(context.player_command, map, deadline);
	return error.ok() ? CoreDriverResult{{}, true, ""} :
		DriverFailure(error, true);
}

CoreDriverResult MisterCoreDriver::Start(const CoreDriverContext& context,
	std::uint64_t deadline)
{
	if (context.mister_recipe == nullptr) return {};
	const Error error = core_.ReleaseReset(*context.mister_recipe, deadline);
	return error.ok() ? CoreDriverResult{{}, true, ""} :
		DriverFailure(error, true);
}

CoreDriverResult ContainedCoreDriver::Quiesce(const CoreDriverContext&,
	std::uint64_t) { return {}; }
CoreDriverResult ContainedCoreDriver::Identify(const CoreDriverContext&,
	std::uint64_t) { return {}; }
CoreDriverResult ContainedCoreDriver::NeutralizeButtons(const CoreDriverContext&,
	std::uint64_t) { return {}; }
CoreDriverResult ContainedCoreDriver::SetButtons(const CoreDriverContext&,
	std::uint16_t, std::uint64_t) { return {}; }
CoreDriverResult ContainedCoreDriver::Start(const CoreDriverContext&,
	std::uint64_t) { return {}; }

CoreDriverRegistry::CoreDriverRegistry(CoreDriver& mister,
	CoreDriver* fes_gp, CoreDriver& contained)
	: mister_(mister), fes_gp_(fes_gp), contained_(contained) {}

CoreDriver* CoreDriverRegistry::Resolve(ProgrammingProfile profile) const
{
	if (profile == ProgrammingProfile::mister_v1) return &mister_;
	if (profile == ProgrammingProfile::fes_gp_v1) return fes_gp_;
	if (profile == ProgrammingProfile::development_contained_v1)
		return &contained_;
	return nullptr;
}

Error CoreDriverRegistry::Resolve(const CoreDescriptor& descriptor,
	ProgrammingProfile* profile, CoreDriver** driver) const
{
	if (profile == nullptr || driver == nullptr)
		return {ErrorCode::invalid_request, "missing core-driver registry output"};
	Error error = ParseProgrammingProfile(descriptor.target.programming_profile,
		profile);
	if (!error.ok()) return error;
	*driver = Resolve(*profile);
	if (*driver == nullptr)
		return {ErrorCode::unsupported_protocol,
			"registered core driver is unavailable"};
	return {};
}

Error ParseProgrammingProfile(const std::string& value,
	ProgrammingProfile* output)
{
	if (output == nullptr)
		return {ErrorCode::invalid_request, "missing programming-profile output"};
	if (value == "mister-v1") *output = ProgrammingProfile::mister_v1;
	else if (value == "fes-gp-v1") *output = ProgrammingProfile::fes_gp_v1;
	else if (value == "development-contained-v1")
		*output = ProgrammingProfile::development_contained_v1;
	else return {ErrorCode::unsupported_protocol, "unsupported programming profile"};
	return {};
}

} // namespace native
} // namespace mister
