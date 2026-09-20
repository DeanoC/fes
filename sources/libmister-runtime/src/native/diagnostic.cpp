// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/diagnostic.hpp"

#include <fcntl.h>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>

#include <cstddef>
#include <cstdio>
#include <cstring>

namespace mister {
namespace {

struct DiagnosticContext {
	std::mutex mutex;
	DiagnosticSink* sink = nullptr;
	std::string flight_id;
	std::string lease_gen;
	std::string run_id;
	std::string last_core;
	struct timespec start = {};
};

DiagnosticContext& Context()
{
	static DiagnosticContext context;
	static const bool started = (clock_gettime(CLOCK_MONOTONIC, &context.start), true);
	(void)started;
	return context;
}

bool HexNibble(char value)
{
	return (value >= '0' && value <= '9') || (value >= 'a' && value <= 'f');
}

bool ValidFlightID(const std::string& value)
{
	if (value.size() != 36) return false;
	static const int dashes[] = {8, 13, 18, 23};
	for (int dash : dashes) {
		if (value[static_cast<std::size_t>(dash)] != '-') return false;
	}
	for (std::size_t index = 0; index < value.size(); ++index) {
		if (index == 8 || index == 13 || index == 18 || index == 23) continue;
		if (!HexNibble(value[index])) return false;
	}
	if (value[14] != '4') return false;
	const char variant = value[19];
	return variant == '8' || variant == '9' || variant == 'a' || variant == 'b';
}

bool ValidJoinComponent(const std::string& value)
{
	if (value.empty() || value.size() > 256) return false;
	if (value.find_first_not_of(' ') != 0) return false;
	if (value.find_last_not_of(' ') != value.size() - 1) return false;
	for (unsigned char character : value) {
		if ((character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '.' || character == '_' || character == '-')
			continue;
		return false;
	}
	return true;
}

std::string FormatUtcNs()
{
	struct timespec now = {};
	clock_gettime(CLOCK_REALTIME, &now);
	struct tm utc = {};
	gmtime_r(&now.tv_sec, &utc);
	char stamp[96] = {};
	std::snprintf(stamp, sizeof(stamp),
		"%04d-%02d-%02dT%02d:%02d:%02d.%09ldZ",
		utc.tm_year + 1900, utc.tm_mon + 1, utc.tm_mday,
		utc.tm_hour, utc.tm_min, utc.tm_sec,
		static_cast<long>(now.tv_nsec));
	return stamp;
}

std::int64_t MonoMs(const struct timespec& start)
{
	struct timespec now = {};
	clock_gettime(CLOCK_MONOTONIC, &now);
	const std::int64_t start_ms =
		static_cast<std::int64_t>(start.tv_sec) * 1000 +
		static_cast<std::int64_t>(start.tv_nsec) / 1000000;
	const std::int64_t now_ms =
		static_cast<std::int64_t>(now.tv_sec) * 1000 +
		static_cast<std::int64_t>(now.tv_nsec) / 1000000;
	return now_ms - start_ms;
}

void AppendQuoted(std::string* output, const std::string& value)
{
	output->push_back('"');
	static const char hex[] = "0123456789abcdef";
	for (unsigned char character : value) {
		switch (character) {
		case '"': output->append("\\\""); break;
		case '\\': output->append("\\\\"); break;
		case '\b': output->append("\\b"); break;
		case '\f': output->append("\\f"); break;
		case '\n': output->append("\\n"); break;
		case '\r': output->append("\\r"); break;
		case '\t': output->append("\\t"); break;
		default:
			if (character < 0x20) {
				output->append("\\u00");
				output->push_back(hex[(character >> 4) & 0x0f]);
				output->push_back(hex[character & 0x0f]);
			} else {
				output->push_back(static_cast<char>(character));
			}
		}
	}
	output->push_back('"');
}

void AppendDetail(std::string* output, const std::vector<DiagnosticField>& detail)
{
	output->push_back('{');
	for (std::size_t index = 0; index < detail.size(); ++index) {
		if (index != 0) output->push_back(',');
		AppendQuoted(output, detail[index].key);
		output->push_back(':');
		switch (detail[index].type) {
		case DiagnosticField::Type::boolean:
			output->append(detail[index].boolean_value ? "true" : "false");
			break;
		case DiagnosticField::Type::integer:
			output->append(std::to_string(detail[index].integer_value));
			break;
		case DiagnosticField::Type::string:
			AppendQuoted(output, detail[index].string_value);
			break;
		}
	}
	output->push_back('}');
}

void PrepareEvent(DiagnosticEvent* event)
{
	DiagnosticContext& context = Context();
	std::lock_guard<std::mutex> lock(context.mutex);
	if (event->ts_utc.empty()) event->ts_utc = FormatUtcNs();
	if (event->mono_ms == 0) event->mono_ms = MonoMs(context.start);
	if (event->flight_id.empty()) event->flight_id = context.flight_id;
	if (event->lease_gen.empty()) event->lease_gen = context.lease_gen;
	if (event->run_id.empty()) event->run_id = context.run_id;
}

} // namespace

DiagnosticRing::DiagnosticRing(std::size_t capacity)
	: capacity_(capacity == 0 ? kDiagnosticRingCapacity : capacity) {}

void DiagnosticRing::Append(DiagnosticEvent event)
{
	PrepareEvent(&event);
	std::lock_guard<std::mutex> lock(mutex_);
	events_.push_back(std::move(event));
	if (events_.size() > capacity_) {
		events_.erase(events_.begin(),
			events_.begin() + static_cast<std::ptrdiff_t>(events_.size() - capacity_));
	}
}

std::vector<DiagnosticEvent> DiagnosticRing::Snapshot(int limit) const
{
	std::lock_guard<std::mutex> lock(mutex_);
	std::size_t start = 0;
	if (limit > 0 && static_cast<std::size_t>(limit) < events_.size())
		start = events_.size() - static_cast<std::size_t>(limit);
	return std::vector<DiagnosticEvent>(events_.begin() +
		static_cast<std::ptrdiff_t>(start), events_.end());
}

std::size_t DiagnosticRing::capacity() const
{
	std::lock_guard<std::mutex> lock(mutex_);
	return capacity_;
}

std::size_t DiagnosticRing::size() const
{
	std::lock_guard<std::mutex> lock(mutex_);
	return events_.size();
}

void InstallDiagnosticSink(DiagnosticSink* sink)
{
	DiagnosticContext& context = Context();
	std::lock_guard<std::mutex> lock(context.mutex);
	context.sink = sink;
}

DiagnosticSink* InstalledDiagnosticSink()
{
	DiagnosticContext& context = Context();
	std::lock_guard<std::mutex> lock(context.mutex);
	return context.sink;
}

void ResetDiagnosticState()
{
	DiagnosticContext& context = Context();
	std::lock_guard<std::mutex> lock(context.mutex);
	context.sink = nullptr;
	context.flight_id.clear();
	context.lease_gen.clear();
	context.run_id.clear();
	context.last_core.clear();
	clock_gettime(CLOCK_MONOTONIC, &context.start);
}

Error SetDiagnosticJoin(const std::string& flight_id, const std::string& lease_gen,
	const std::string& run_id)
{
	if (!flight_id.empty() && !ValidFlightID(flight_id))
		return {ErrorCode::invalid_request,
			"flight_id must be the canonical host UUID v4"};
	if (!lease_gen.empty() && !ValidJoinComponent(lease_gen))
		return {ErrorCode::invalid_request,
			"lease_gen is not a safe diagnostic identifier"};
	if (!run_id.empty() && !ValidJoinComponent(run_id))
		return {ErrorCode::invalid_request,
			"run_id is not a safe diagnostic identifier"};
	DiagnosticContext& context = Context();
	std::lock_guard<std::mutex> lock(context.mutex);
	context.flight_id = flight_id;
	context.lease_gen = lease_gen;
	context.run_id = run_id;
	return {};
}

void ClearDiagnosticJoin()
{
	DiagnosticContext& context = Context();
	std::lock_guard<std::mutex> lock(context.mutex);
	context.flight_id.clear();
	context.lease_gen.clear();
	context.run_id.clear();
}

void EmitDiagnostic(const char* layer, const char* kind, const char* severity,
	const std::vector<DiagnosticField>& detail)
{
	DiagnosticSink* sink = nullptr;
	{
		DiagnosticContext& context = Context();
		std::lock_guard<std::mutex> lock(context.mutex);
		sink = context.sink;
	}
	if (sink == nullptr) return;
	DiagnosticEvent event;
	event.layer = layer == nullptr ? "" : layer;
	event.kind = kind == nullptr ? "" : kind;
	event.severity = severity == nullptr ? "" : severity;
	event.detail = detail;
	PrepareEvent(&event);
	sink->Append(std::move(event));
}

void EmitDiagnostic(const char* layer, const char* kind, const char* severity,
	std::initializer_list<DiagnosticField> detail)
{
	EmitDiagnostic(layer, kind, severity,
		std::vector<DiagnosticField>(detail.begin(), detail.end()));
}

void EmitCoreNameChange(const std::string& expected, const std::string& observed,
	bool ok)
{
	std::string previous;
	bool left_menu = false;
	{
		DiagnosticContext& context = Context();
		std::lock_guard<std::mutex> lock(context.mutex);
		previous = context.last_core;
		if (ok && !observed.empty()) {
			left_menu = previous == "MENU" && observed != "MENU";
			if (observed != context.last_core) context.last_core = observed;
			else if (previous == observed && !previous.empty()) {
				// Duplicate confirmation of the same core is not a change.
				if (expected.empty() || expected == observed) return;
			}
		}
	}
	std::vector<DiagnosticField> detail;
	if (!expected.empty())
		detail.push_back(DiagnosticString("expected", expected));
	if (!observed.empty())
		detail.push_back(DiagnosticString("observed", observed));
	if (!previous.empty())
		detail.push_back(DiagnosticString("previous", previous));
	detail.push_back(DiagnosticBool("left_menu", left_menu));
	detail.push_back(DiagnosticBool("ok", ok));
	const std::string file_observed = ReadDiagnosticFile("/tmp/CORENAME");
	if (!file_observed.empty())
		detail.push_back(DiagnosticString("file_observed", file_observed));
	EmitDiagnostic(kDiagnosticLayerRuntime, kDiagnosticKindCoreNameChange,
		ok ? "ok" : "error", detail);
}

void EmitCapFdOpen(bool ok, const std::string& path)
{
	std::vector<DiagnosticField> detail;
	detail.push_back(DiagnosticBool("ok", ok));
	if (!path.empty()) detail.push_back(DiagnosticString("path", path));
	EmitDiagnostic(kDiagnosticLayerRuntime, kDiagnosticKindCapFdOpen,
		ok ? "ok" : "error", detail);
}

void EmitFifoConsume(const char* operation, bool ok)
{
	std::vector<DiagnosticField> detail;
	if (operation != nullptr)
		detail.push_back(DiagnosticString("operation", operation));
	detail.push_back(DiagnosticBool("ok", ok));
	EmitDiagnostic(kDiagnosticLayerRuntime, kDiagnosticKindFifoConsume,
		ok ? "ok" : "error", detail);
}

void EmitFence(const char* kind, const char* severity, const char* operation,
	bool ok)
{
	std::vector<DiagnosticField> detail;
	if (operation != nullptr)
		detail.push_back(DiagnosticString("operation", operation));
	detail.push_back(DiagnosticBool("ok", ok));
	EmitDiagnostic(kDiagnosticLayerRuntime, kind, severity, detail);
}

std::string DiagnosticHex32(std::uint32_t value)
{
	char output[16] = {};
	std::snprintf(output, sizeof(output), "0x%x", value);
	return output;
}

std::string ReadDiagnosticFile(const char* path, std::size_t maximum_bytes)
{
	if (path == nullptr || maximum_bytes == 0) return {};
	const int descriptor = open(path,
		O_RDONLY | O_CLOEXEC | O_NOFOLLOW | O_NONBLOCK);
	if (descriptor < 0) return {};
	struct stat metadata = {};
	if (fstat(descriptor, &metadata) != 0 || !S_ISREG(metadata.st_mode)) {
		close(descriptor);
		return {};
	}
	std::string bytes(maximum_bytes, '\0');
	const ssize_t count = read(descriptor, &bytes[0], maximum_bytes);
	close(descriptor);
	if (count <= 0) return {};
	bytes.resize(static_cast<std::size_t>(count));
	while (!bytes.empty() &&
		(bytes.back() == '\n' || bytes.back() == '\r' || bytes.back() == ' ' ||
			bytes.back() == '\t'))
		bytes.pop_back();
	return bytes;
}

std::string EncodeDiagnosticEvent(const DiagnosticEvent& event)
{
	std::string output = "{\"ts_utc\":";
	AppendQuoted(&output, event.ts_utc);
	output.append(",\"mono_ms\":");
	output.append(std::to_string(event.mono_ms));
	if (!event.flight_id.empty()) {
		output.append(",\"flight_id\":");
		AppendQuoted(&output, event.flight_id);
	}
	if (!event.lease_gen.empty()) {
		output.append(",\"lease_gen\":");
		AppendQuoted(&output, event.lease_gen);
	}
	if (!event.run_id.empty()) {
		output.append(",\"run_id\":");
		AppendQuoted(&output, event.run_id);
	}
	output.append(",\"layer\":");
	AppendQuoted(&output, event.layer);
	output.append(",\"kind\":");
	AppendQuoted(&output, event.kind);
	output.append(",\"severity\":");
	AppendQuoted(&output, event.severity);
	output.append(",\"detail\":");
	AppendDetail(&output, event.detail);
	output.push_back('}');
	return output;
}

std::string EncodeDiagnosticEvents(const std::vector<DiagnosticEvent>& events)
{
	std::string output = "{\"events\":[";
	for (std::size_t index = 0; index < events.size(); ++index) {
		if (index != 0) output.push_back(',');
		output.append(EncodeDiagnosticEvent(events[index]));
	}
	output.append("],\"count\":");
	output.append(std::to_string(events.size()));
	output.push_back('}');
	return output;
}

} // namespace mister
