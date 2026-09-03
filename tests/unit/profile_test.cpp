// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "libmister-runtime/runtime.h"
#include "test_profiles.hpp"

#include <assert.h>
#include <stdio.h>

#include <limits>
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
	duplicate_role.media.push_back({"bios", 3, false, {".bin"},
		32u * 1024u * 1024u});
	mister::Profiles profiles;
	assert(profiles.Add(duplicate_role).code == ErrorCode::invalid_request);
	mister::Profile duplicate_index = mister_test::BiosProfile();
	duplicate_index.media.push_back({"disc", 2, false, {".bin"},
		32u * 1024u * 1024u});
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
	launch.rbf = "/cores/test.rbf";
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
	assert(prepared.rbf == "/cores/test.rbf");
	assert(prepared.media.size() == 1);
	assert(prepared.media[0].index == 1);
	assert(prepared.media[0].path == "/games/game.bin");
	assert(prepared.core.reset_assert_word == 0x0001);
	assert(prepared.core.initial_status_word == 0x0001);
	assert(prepared.core.reset_release_word == 0x0000);
	assert(prepared.core.file_wire ==
		mister::FileWireFormat::little_endian_byte_pairs);
	assert(prepared.input.player_count == 1);
	assert(prepared.input.player_command == 0x02);
	assert(prepared.input.up == 0x0008);
	assert(prepared.input.down == 0x0004);
	assert(prepared.input.left == 0x0002);
	assert(prepared.input.right == 0x0001);
	assert(prepared.input.a == 0x0010);
	assert(prepared.input.b == 0x0020);
	assert(prepared.input.c == 0x0040);
	assert(prepared.input.start == 0x0080);
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

void TestPrepareRejectsWrongRbfAndMediaExtension()
{
	mister::Profiles profiles;
	assert(profiles.Add(mister_test::CartProfile()).ok());
	mister::PreparedLaunch unchanged;
	unchanged.system = "sentinel";
	mister::Launch launch = ValidCartLaunch();
	launch.rbf = "/cores/other.rbf";
	assert(profiles.Prepare(launch, &unchanged).code == ErrorCode::invalid_request);
	assert(unchanged.system == "sentinel");
	launch = ValidCartLaunch();
	launch.media[0].path = "/games/game.md";
	assert(profiles.Prepare(launch, &unchanged).code == ErrorCode::invalid_request);
	assert(unchanged.system == "sentinel");
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
			static_cast<std::uint8_t>(index + 2), false, {".bin"},
			32u * 1024u * 1024u});
	}
	assert(profiles.Add(profile).code == ErrorCode::invalid_request);
	profile = mister_test::CartProfile();
	profile.settings[0].allowed_values[0] = std::string("\xc0\xaf", 2);
	assert(profiles.Add(profile).code == ErrorCode::invalid_request);
}

void TestMalformedRecipeAndMediaMetadataAreRejectedAtomically()
{
	mister::Profiles profiles;
	mister::Profile profile = mister_test::CartProfile();
	profile.media[0].extensions.clear();
	assert(profiles.Add(profile).code == ErrorCode::invalid_request);
	profile = mister_test::CartProfile();
	profile.media[0].maximum_size = 0;
	assert(profiles.Add(profile).code == ErrorCode::invalid_request);
	profile = mister_test::CartProfile();
	profile.media[0].maximum_size = std::numeric_limits<std::uint64_t>::max();
	assert(profiles.Add(profile).code == ErrorCode::invalid_request);
	profile = mister_test::CartProfile();
	profile.core.reset_assert_word = 0;
	assert(profiles.Add(profile).code == ErrorCode::invalid_request);
	profile = mister_test::CartProfile();
	profile.input.player_command = 0;
	assert(profiles.Add(profile).code == ErrorCode::invalid_request);
	profile = mister_test::CartProfile();
	profile.input.b = profile.input.a;
	assert(profiles.Add(profile).code == ErrorCode::invalid_request);
	assert(profiles.empty());
}

void TestMultiBitInputMaskIsRejectedAtomically()
{
	mister::Profiles profiles;
	mister::Profile profile = mister_test::CartProfile();
	profile.input.a = 0x0300;
	assert(profiles.Add(profile).code == ErrorCode::invalid_request);
	assert(profiles.empty());
}

void TestUnsupportedFileWireFormatIsRejectedAtomically()
{
	mister::Profiles profiles;
	mister::Profile profile = mister_test::CartProfile();
	profile.core.file_wire = static_cast<mister::FileWireFormat>(99);
	assert(profiles.Add(profile).code == ErrorCode::invalid_request);
	assert(profiles.empty());
}

void TestFreshRegistryStartsEmpty()
{
	const mister::Profiles profiles;
	assert(profiles.empty());
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
	TestFreshRegistryStartsEmpty();
	TestPrepareRejectsWrongRbfAndMediaExtension();
	TestMalformedRecipeAndMediaMetadataAreRejectedAtomically();
	TestUnsupportedFileWireFormatIsRejectedAtomically();
	TestMultiBitInputMaskIsRejectedAtomically();
	puts("profile_test: 14 passed");
	return 0;
}
