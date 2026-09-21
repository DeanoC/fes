// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "native/core_package.hpp"

namespace mister { namespace native {
struct OpenedCoreComposition {
	CoreComposition info;
	Artifact manifest, cart, payload;
	std::string manifest_bytes, cart_sha256;
};
Error OpenCoreComposition(const std::vector<std::string>& roots,
	const OpenedCorePackage&, const CoreCompositionRequest&, OpenedCoreComposition*);
Error RecheckCoreComposition(const OpenedCoreComposition&);
} }
