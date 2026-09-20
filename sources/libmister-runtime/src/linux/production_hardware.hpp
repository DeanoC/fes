// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"

#include <memory>

namespace mister {

const Profiles& ProductionProfiles();
Error CreateProductionHardware(LogSink& log,
	std::unique_ptr<Hardware>* hardware);
std::unique_ptr<Hardware> CreateUnavailableHardware(const Error& reason);

} // namespace mister
