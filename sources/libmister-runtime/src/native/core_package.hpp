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

// Manifest-only optional slot bus of fes.computer 1.0 shells. It grants no
// capability bit; multi-slot composition admission is its only consumer.
constexpr const char* kApple2ExpansionBusID = "fes.expansion.apple2-bus";
constexpr const char* kApple2ExpansionMapID = "fes.apple2-bus.slots/1";
constexpr const char* kC64ExpansionBusID = "fes.expansion.c64-bus";
constexpr const char* kC64ExpansionMapID = "fes.c64-bus.sockets/1";
constexpr const char* kAtariStExpansionBusID = "fes.expansion.atari-st-bus";
constexpr const char* kAtariStExpansionMapID = "fes.atari-st-bus.socket/1";
constexpr const char* kSpectrumExpansionBusID = "fes.expansion.spectrum-bus";
constexpr const char* kSpectrumExpansionMapID = "fes.spectrum-bus.sockets/1";
// The shared plane behind fes.video.session-display 1.0 (FesDisplayPlaneMajor
// and Minor). A session-display package declares it required, so software
// without the premultiplied plane refuses the package before programming. It
// grants no capability bit.
constexpr const char* kDisplayPlaneInterfaceID = "fes.fabric.video.display-plane";

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
// Format 2 is the ROM-less shell. Format 3 is the factory 16 KiB cartridge.
inline bool ColecoVideoShell(const CoreDescriptor& descriptor)
{
	if (descriptor.core.id != "fes.coleco") return false;
	if (descriptor.format == 2) return true;
	return descriptor.format == 3 && descriptor.rom.role == "cartridge" &&
		descriptor.rom.source_size == 16384;
}
Error CorePersistenceLayout(const CoreDescriptor&, VersionedContract*);

} // namespace native
} // namespace mister
