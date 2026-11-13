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

// Structural admission only. This does not select a programming profile or
// perform a transition. The base package must outlive this selection.
struct OpenedCoreStaticComposition {
	CoreStaticComposition info;
	OpenedCoreExpansion expansion;
	const Artifact& persona(const OpenedCorePackage& base) const {
		return info.expansion_id.empty() ? base.persona : expansion.cart;
	}
};
Error OpenCoreStaticComposition(const std::vector<std::string>& roots,
	const OpenedCorePackage&, const CoreStaticCompositionRequest&, OpenedCoreStaticComposition*);
Error RecheckCoreStaticComposition(const OpenedCorePackage&, const OpenedCoreStaticComposition&);
} }
