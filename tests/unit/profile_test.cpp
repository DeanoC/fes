// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "libmister-runtime/runtime.h"
#include "test_profiles.hpp"

#include <assert.h>
#include <stdio.h>

#include <string>

namespace {

using mister::ErrorCode;

void TestValidProfilesAndDuplicateSystems()
{
	mister::Profiles profiles;
	assert(profiles.empty());
	assert(profiles.Add(mister_test::CartProfile()).ok());
	assert(!profiles.empty());
	assert(profiles.Add(mister_test::CartProfile()).code ==
		ErrorCode::invalid_request);
}

void TestDuplicateRolesAndIndicesAreRejected()
{
	mister::Profile duplicate_role = mister_test::BiosProfile();
	duplicate_role.media.push_back({"bios", 3, false});
	mister::Profiles profiles;
	assert(profiles.Add(duplicate_role).code == ErrorCode::invalid_request);
	mister::Profile duplicate_index = mister_test::BiosProfile();
	duplicate_index.media.push_back({"disc", 2, false});
	assert(profiles.Add(duplicate_index).code == ErrorCode::invalid_request);
}

void TestInvalidProfileShapeIsRejectedAtomically()
{
	mister::Profiles profiles;
	mister::Profile profile = mister_test::CartProfile();
	profile.expected_core.clear();
	assert(profiles.Add(profile).code == ErrorCode::invalid_request);
	profile = mister_test::CartProfile();
	profile.system = "Upper";
	assert(profiles.Add(profile).code == ErrorCode::invalid_request);
	profile = mister_test::CartProfile();
	profile.settings[0].allowed_values.push_back("pal");
	assert(profiles.Add(profile).code == ErrorCode::invalid_request);
	assert(profiles.empty());
}

mister::Launch ValidCartLaunch()
{
	mister::Launch launch;
	launch.system = "test_cart";
	launch.rbf = "/cores/test_cart.rbf";
	launch.media.push_back({"cartridge", "/games/game.bin"});
	launch.settings.push_back({"region", "pal"});
	return launch;
}

void TestPrepareMapsSemanticRolesToOwnedIndices()
{
	mister::Profiles profiles;
	assert(profiles.Add(mister_test::CartProfile()).ok());
	mister::PreparedLaunch prepared;
	assert(profiles.Prepare(ValidCartLaunch(), &prepared).ok());
	assert(prepared.system == "test_cart");
	assert(prepared.expected_core == "TESTCART");
	assert(prepared.rbf == "/cores/test_cart.rbf");
	assert(prepared.media.size() == 1);
	assert(prepared.media[0].index == 1);
	assert(prepared.media[0].path == "/games/game.bin");
}

void TestMissingRequiredMediaIsDirect()
{
	mister::Profiles profiles;
	assert(profiles.Add(mister_test::CartProfile()).ok());
	mister::Launch launch = ValidCartLaunch();
	launch.media.clear();
	mister::PreparedLaunch unchanged;
	unchanged.system = "sentinel";
	assert(profiles.Prepare(launch, &unchanged).code == ErrorCode::missing_media);
	assert(unchanged.system == "sentinel");
}

void TestUnknownSystemRoleSettingAndValueAreRejected()
{
	mister::Profiles profiles;
	assert(profiles.Add(mister_test::CartProfile()).ok());
	mister::PreparedLaunch prepared;
	mister::Launch launch = ValidCartLaunch();
	launch.system = "unknown";
	assert(profiles.Prepare(launch, &prepared).code == ErrorCode::unknown_system);
	launch = ValidCartLaunch();
	launch.media[0].role = "disc";
	assert(profiles.Prepare(launch, &prepared).code == ErrorCode::invalid_request);
	launch = ValidCartLaunch();
	launch.settings[0].name = "difficulty";
	assert(profiles.Prepare(launch, &prepared).code == ErrorCode::invalid_request);
	launch = ValidCartLaunch();
	launch.settings[0].value = "secam";
	assert(profiles.Prepare(launch, &prepared).code == ErrorCode::invalid_request);
}

void TestPathsMustBeAbsoluteAndBounded()
{
	mister::Profiles profiles;
	assert(profiles.Add(mister_test::CartProfile()).ok());
	mister::PreparedLaunch prepared;
	mister::Launch launch = ValidCartLaunch();
	launch.rbf = "relative.rbf";
	assert(profiles.Prepare(launch, &prepared).code == ErrorCode::invalid_request);
	launch = ValidCartLaunch();
	launch.media[0].path = "relative.bin";
	assert(profiles.Prepare(launch, &prepared).code == ErrorCode::invalid_request);
	launch = ValidCartLaunch();
	launch.rbf = "/" + std::string(4095, 'a');
	assert(profiles.Prepare(launch, &prepared).code == ErrorCode::invalid_request);
}

void TestEmbeddedNulPathsAreRejected()
{
	mister::Profiles profiles;
	assert(profiles.Add(mister_test::CartProfile()).ok());
	mister::PreparedLaunch prepared;
	mister::Launch launch = ValidCartLaunch();
	launch.rbf = std::string("/cores/real.rbf\0ignored.rbf", 27);
	assert(profiles.Prepare(launch, &prepared).code == ErrorCode::invalid_request);
	launch = ValidCartLaunch();
	launch.media[0].path = std::string("/games/real.bin\0ignored.bin", 27);
	assert(profiles.Prepare(launch, &prepared).code == ErrorCode::invalid_request);
}

void TestIdentifierSettingAndCountBounds()
{
	mister::Profiles profiles;
	mister::Profile profile = mister_test::CartProfile();
	profile.system = std::string(33, 'a');
	assert(profiles.Add(profile).code == ErrorCode::invalid_request);
	profile = mister_test::CartProfile();
	for (int index = 0; index != 8; ++index) {
		profile.media.push_back({"role" + std::to_string(index),
			static_cast<std::uint8_t>(index + 2), false});
	}
	assert(profiles.Add(profile).code == ErrorCode::invalid_request);
	profile = mister_test::CartProfile();
	profile.settings[0].allowed_values[0] = std::string("\xc0\xaf", 2);
	assert(profiles.Add(profile).code == ErrorCode::invalid_request);
}

void TestProductionRegistryStartsEmpty()
{
	const mister::Profiles production_profiles;
	assert(production_profiles.empty());
}

} // namespace

int main()
{
	TestValidProfilesAndDuplicateSystems();
	TestDuplicateRolesAndIndicesAreRejected();
	TestInvalidProfileShapeIsRejectedAtomically();
	TestPrepareMapsSemanticRolesToOwnedIndices();
	TestMissingRequiredMediaIsDirect();
	TestUnknownSystemRoleSettingAndValueAreRejected();
	TestPathsMustBeAbsoluteAndBounded();
	TestEmbeddedNulPathsAreRejected();
	TestIdentifierSettingAndCountBounds();
	TestProductionRegistryStartsEmpty();
	puts("profile_test: 10 passed");
	return 0;
}
