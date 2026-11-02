// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/core_driver.hpp"

#include "native/core_package.hpp"
#include "native/generated/de10_nano.hpp"
#include "native/hardware.hpp"
#include "native/linux/mmio.hpp"

#include <utility>

namespace mister {
namespace native {
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

CoreDriverRegistry::CoreDriverRegistry(CoreDriver* fes_gp, CoreDriver& contained,
	CoreDriver* de25, NativePlatform platform, CoreDriver* de25_menu)
	: platform_(platform), de25_(de25), de25_menu_(de25_menu), fes_gp_(fes_gp), contained_(contained) {}

CoreDriver* CoreDriverRegistry::Resolve(ProgrammingProfile profile) const
{
	if (platform_ == NativePlatform::de25_nano) {
		if (profile == ProgrammingProfile::de25_hps_first_v1) return de25_;
		if (profile == ProgrammingProfile::de25_linux_menu_v1) return de25_menu_;
		return nullptr;
	}
	if (platform_ != NativePlatform::de10_nano) return nullptr;
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
	*driver = nullptr;
	Error error = CheckCoreCompatibility(descriptor, platform_);
	if (!error.ok()) return error;
	error = ParseProgrammingProfile(descriptor.target.programming_profile,
		profile);
	if (!error.ok()) return error;
	*driver = Resolve(*profile);
	if (*driver == nullptr)
		return {ErrorCode::unsupported_abi,
			"registered core driver is unavailable", "compatibility"};
	return {};
}

Error ParseProgrammingProfile(const std::string& value,
	ProgrammingProfile* output)
{
	if (output == nullptr)
		return {ErrorCode::invalid_request, "missing programming-profile output"};
	if (value == "fes-gp-v1") *output = ProgrammingProfile::fes_gp_v1;
	else if (value == "development-contained-v1")
		*output = ProgrammingProfile::development_contained_v1;
	else if (value == "de25-hps-first-v1") *output = ProgrammingProfile::de25_hps_first_v1;
	else if (value == "de25-linux-menu-v1") *output = ProgrammingProfile::de25_linux_menu_v1;
	else return {ErrorCode::unsupported_programming_profile,
		"unsupported programming profile", "compatibility"};
	return {};
}

} // namespace native
} // namespace mister
