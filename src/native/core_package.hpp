// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "native/artifacts.hpp"

#include <cstdint>
#include <string>
#include <vector>

namespace mister {
namespace native {

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

struct OpenedCorePackage {
	CoreDescriptor descriptor;
	std::string manifest_bytes;
	Artifact payload;
	std::string package_id;
};

Error OpenCorePackage(const std::string& directory,
	const std::string& expected_id, OpenedCorePackage* result);
Error CheckCoreCompatibility(const CoreDescriptor& descriptor);

} // namespace native
} // namespace mister
