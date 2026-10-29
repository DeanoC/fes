// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"

#include <memory>
#if defined(MISTER_RUNTIME_TESTING)
#include <functional>
#endif

namespace mister {

Error CreateProductionHardware(LogSink& log,
	std::unique_ptr<Hardware>* hardware);
std::unique_ptr<Hardware> CreateUnavailableHardware(const Error& reason);

#if defined(MISTER_RUNTIME_TESTING)
// Exercise the same guarded construction path without reading host identity or
// loading a kernel module. This overload is absent from production artifacts.
Error CreateProductionHardware(LogSink& log, std::unique_ptr<Hardware>* hardware,
	const std::string& architecture, const std::string& model,
	const std::string& compatible, const std::function<Error()>& load_window_driver);
#endif

} // namespace mister
