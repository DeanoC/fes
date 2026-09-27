// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "native/core_package.hpp"

namespace mister { namespace native {
struct OpenedCoreExpansion {
	std::uint8_t slot = 0;
	Artifact manifest, cart;
	std::string manifest_bytes, cart_sha256;
};
// A single-socket composition retains one expansion (slot 0). A multi-slot
// composition retains one expansion per requested slot, ascending.
struct OpenedCoreComposition {
	CoreComposition info;
	std::vector<OpenedCoreExpansion> expansions;
	Artifact payload;
};
Error OpenCoreComposition(const std::vector<std::string>& roots,
	const OpenedCorePackage&, const CoreCompositionRequest&, OpenedCoreComposition*);
Error RecheckCoreComposition(const OpenedCoreComposition&);
} }
