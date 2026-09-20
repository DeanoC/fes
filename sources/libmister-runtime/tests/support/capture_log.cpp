// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "capture_log.hpp"

namespace mister_test {

void CaptureLog::Write(const mister::LogRecord& record)
{
	std::lock_guard<std::mutex> lock(mutex_);
	records_.push_back(record);
}

std::vector<mister::LogRecord> CaptureLog::records() const
{
	std::lock_guard<std::mutex> lock(mutex_);
	return records_;
}

void CaptureLog::Clear()
{
	std::lock_guard<std::mutex> lock(mutex_);
	records_.clear();
}

} // namespace mister_test
