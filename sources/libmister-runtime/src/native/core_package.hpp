// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "native/artifacts.hpp"

#include <cstdint>
#include <string>
#include <vector>

namespace mister {
namespace native {

using CoreMetadata = mister::CoreMetadata;
using CoreTarget = mister::CoreTarget;
using CorePayload = mister::CorePayload;
using VersionedContract = mister::VersionedContract;
using CoreInterface = mister::CoreInterface;
using CoreBuild = mister::CoreBuild;
using CoreDescriptor = mister::CoreDescriptor;

struct OpenedCorePackage {
	CoreDescriptor descriptor;
	std::string manifest_bytes;
	Artifact payload;
	Artifact rom_map;
	std::string package_id;
};

Error OpenCorePackage(const std::string& directory,
	const std::string& expected_id, OpenedCorePackage* result);
Error OpenCorePackage(const std::vector<std::string>& trusted_roots,
	const std::string& directory, const std::string& expected_id,
	OpenedCorePackage* result);
// Hash a retained regular artifact after checking its current size.
Error HashOpenedArtifact(const Artifact&, std::string* digest);
Error RecheckCorePackage(const OpenedCorePackage& package);
Error CheckCoreCompatibility(const CoreDescriptor& descriptor);
Error CorePersistenceLayout(const CoreDescriptor&, VersionedContract*);

} // namespace native
} // namespace mister
