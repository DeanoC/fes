// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include <mutex>
#include <string>

#include "libmister-runtime/runtime.h"
#include "native/diagnostic.hpp"

namespace mister {

class StderrLogSink final : public LogSink {
public:
	void Write(const LogRecord& record) override;

private:
	std::mutex mutex_;
};

class DiagnosticFileSink final : public DiagnosticSink {
public:
	DiagnosticFileSink(DiagnosticRing& ring, std::string path);
	void Append(DiagnosticEvent event) override;

private:
	void PublishLocked();

	DiagnosticRing& ring_;
	std::string path_;
	std::mutex mutex_;
};

} // namespace mister
