// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "linux/stderr_log.hpp"

#include <cerrno>
#include <cstdio>
#include <fcntl.h>
#include <string>
#include <sys/stat.h>
#include <unistd.h>

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

DiagnosticFileSink::DiagnosticFileSink(DiagnosticRing& ring, std::string path)
	: ring_(ring), path_(std::move(path)) {}

void DiagnosticFileSink::Append(DiagnosticEvent event)
{
	std::lock_guard<std::mutex> lock(mutex_);
	ring_.Append(std::move(event));
	PublishLocked();
}

void DiagnosticFileSink::PublishLocked()
{
	if (path_.empty()) return;
	const std::string body = EncodeDiagnosticEvents(ring_.Snapshot(0));
	const std::string temporary = path_ + ".tmp";
	const int descriptor = open(temporary.c_str(),
		O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC | O_NOFOLLOW | O_NONBLOCK, 0600);
	if (descriptor < 0) return;
	struct stat metadata = {};
	if (fstat(descriptor, &metadata) != 0 || !S_ISREG(metadata.st_mode)) {
		close(descriptor);
		(void)unlink(temporary.c_str());
		return;
	}
	const char* cursor = body.data();
	std::size_t remaining = body.size();
	bool wrote = true;
	while (remaining > 0) {
		const ssize_t count = write(descriptor, cursor, remaining);
		if (count < 0 && errno == EINTR) continue;
		if (count <= 0) {
			wrote = false;
			break;
		}
		cursor += count;
		remaining -= static_cast<std::size_t>(count);
	}
	if (close(descriptor) != 0) wrote = false;
	if (!wrote) {
		(void)unlink(temporary.c_str());
		return;
	}
	if (rename(temporary.c_str(), path_.c_str()) != 0)
		(void)unlink(temporary.c_str());
}

} // namespace mister
