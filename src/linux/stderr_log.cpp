// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "linux/stderr_log.hpp"

#include <cstdio>
#include <string>

namespace mister {
namespace {

std::string Escape(const std::string& value)
{
	if (value.empty()) return "-";
	std::string escaped;
	static const char hex[] = "0123456789abcdef";
	for (unsigned char character : value) {
		switch (character) {
		case '\\': escaped += "\\\\"; break;
		case '\n': escaped += "\\n"; break;
		case '\r': escaped += "\\r"; break;
		case '\t': escaped += "\\t"; break;
		case '\b': escaped += "\\b"; break;
		case '\f': escaped += "\\f"; break;
		default:
			if (character < 0x20 || character == 0x7f) {
				escaped += "\\x";
				escaped.push_back(hex[(character >> 4) & 0x0f]);
				escaped.push_back(hex[character & 0x0f]);
			} else {
				escaped.push_back(static_cast<char>(character));
			}
		}
	}
	return escaped;
}

} // namespace

void StderrLogSink::Write(const LogRecord& record)
{
	const std::string line = "mister-runtime operation=" + Escape(record.operation) +
		" phase=" + Escape(record.phase) + " system=" + Escape(record.system) +
		" core=" + Escape(record.core) + " error=" +
		ErrorCodeName(record.error.code) + " message=" + Escape(record.error.message) + "\n";
	std::lock_guard<std::mutex> lock(mutex_);
	(void)std::fwrite(line.data(), 1, line.size(), stderr);
	(void)std::fflush(stderr);
}

} // namespace mister
