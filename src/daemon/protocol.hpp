// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include <string>

#include "libmister-runtime/runtime.h"

namespace mister {
namespace daemon {

enum class Operation { status, launch, load_development_rbf, stop };

struct Request {
	Operation operation = Operation::status;
	Launch launch;
	std::string rbf;
};

Error ParseRequest(const std::string& line, Request* request);
// `ok` is the current request result.  Status::error is lifecycle evidence and
// is intentionally encoded even when a later status request itself succeeds.
std::string EncodeResponse(bool ok, const Status& status, const std::string& version);

} // namespace daemon
} // namespace mister
