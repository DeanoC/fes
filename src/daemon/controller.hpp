// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include <string>

#include "libmister-runtime/runtime.h"

namespace mister {
namespace daemon {

class Controller {
public:
	Controller(Runtime& runtime, std::string version);
	std::string Handle(const std::string& line);
	std::string InvalidRequest(const std::string& message);

private:
	std::string Respond(std::int64_t protocol, const Error& result,
		const CorePackageInspection* inspection = nullptr, const CoreData* data = nullptr);

	Runtime& runtime_;
	const std::string version_;
};

} // namespace daemon
} // namespace mister
