// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include <mutex>

#include "libmister-runtime/runtime.h"

namespace mister {

class StderrLogSink final : public LogSink {
public:
	void Write(const LogRecord& record) override;

private:
	std::mutex mutex_;
};

} // namespace mister
