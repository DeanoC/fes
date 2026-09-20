// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"

#include <mutex>
#include <vector>

namespace mister_test {

class CaptureLog final : public mister::LogSink {
public:
	void Write(const mister::LogRecord&) override;
	std::vector<mister::LogRecord> records() const;
	void Clear();

private:
	mutable std::mutex mutex_;
	std::vector<mister::LogRecord> records_;
};

} // namespace mister_test
