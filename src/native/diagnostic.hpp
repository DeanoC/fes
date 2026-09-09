// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"

#include <cstddef>
#include <cstdint>
#include <initializer_list>
#include <mutex>
#include <string>
#include <vector>

namespace mister {

const std::size_t kDiagnosticRingCapacity = 10000;

const char kDiagnosticEventsPath[] = "/run/mister-runtime.events.json";

const char kDiagnosticLayerRuntime[] = "runtime";
const char kDiagnosticLayerFpga[] = "fpga";

const char kDiagnosticKindFifoDispatch[] = "fifo.dispatch";
const char kDiagnosticKindFifoConsume[] = "fifo.consume";
const char kDiagnosticKindCapFdOpen[] = "cap.fd.open";
const char kDiagnosticKindFpgaManager[] = "fpga_manager.state";
const char kDiagnosticKindCoreNameChange[] = "corename.change";
const char kDiagnosticKindMainStart[] = "main.start";
const char kDiagnosticKindMainExit[] = "main.exit";
const char kDiagnosticKindMainAppRestart[] = "main.app_restart";
const char kDiagnosticKindFenceOwnership[] = "fence.ownership";
const char kDiagnosticKindFenceHandoff[] = "fence.handoff";
const char kDiagnosticKindFenceProgram[] = "fence.program";
const char kDiagnosticKindFenceAbi[] = "fence.abi";
const char kDiagnosticKindFenceRecovery[] = "fence.recovery";

struct DiagnosticField {
	std::string key;
	enum class Type { string, boolean, integer } type = Type::string;
	std::string string_value;
	bool boolean_value = false;
	std::int64_t integer_value = 0;
};

inline DiagnosticField DiagnosticString(std::string key, std::string value)
{
	DiagnosticField field;
	field.key = std::move(key);
	field.type = DiagnosticField::Type::string;
	field.string_value = std::move(value);
	return field;
}

inline DiagnosticField DiagnosticBool(std::string key, bool value)
{
	DiagnosticField field;
	field.key = std::move(key);
	field.type = DiagnosticField::Type::boolean;
	field.boolean_value = value;
	return field;
}

inline DiagnosticField DiagnosticInt(std::string key, std::int64_t value)
{
	DiagnosticField field;
	field.key = std::move(key);
	field.type = DiagnosticField::Type::integer;
	field.integer_value = value;
	return field;
}

struct DiagnosticEvent {
	std::string ts_utc;
	std::int64_t mono_ms = 0;
	std::string flight_id;
	std::string lease_gen;
	std::string run_id;
	std::string layer;
	std::string kind;
	std::string severity;
	std::vector<DiagnosticField> detail;
};

class DiagnosticSink {
public:
	virtual ~DiagnosticSink() {}
	virtual void Append(DiagnosticEvent event) = 0;
};

class DiagnosticRing final : public DiagnosticSink {
public:
	explicit DiagnosticRing(std::size_t capacity = kDiagnosticRingCapacity);
	void Append(DiagnosticEvent event) override;
	std::vector<DiagnosticEvent> Snapshot(int limit = 0) const;
	std::size_t capacity() const;
	std::size_t size() const;

private:
	mutable std::mutex mutex_;
	std::size_t capacity_;
	std::vector<DiagnosticEvent> events_;
};

void InstallDiagnosticSink(DiagnosticSink* sink);
DiagnosticSink* InstalledDiagnosticSink();
void ResetDiagnosticState();
Error SetDiagnosticJoin(const std::string& flight_id, const std::string& lease_gen,
	const std::string& run_id);
void ClearDiagnosticJoin();
void EmitDiagnostic(const char* layer, const char* kind, const char* severity,
	const std::vector<DiagnosticField>& detail);
void EmitDiagnostic(const char* layer, const char* kind, const char* severity,
	std::initializer_list<DiagnosticField> detail);
void EmitCoreNameChange(const std::string& expected, const std::string& observed,
	bool ok);
void EmitCapFdOpen(bool ok, const std::string& path);
void EmitFifoConsume(const char* operation, bool ok);
void EmitFence(const char* kind, const char* severity, const char* operation,
	bool ok);
std::string DiagnosticHex32(std::uint32_t value);
std::string ReadDiagnosticFile(const char* path, std::size_t maximum_bytes = 256);
std::string EncodeDiagnosticEvent(const DiagnosticEvent& event);
std::string EncodeDiagnosticEvents(const std::vector<DiagnosticEvent>& events);

class DiagnosticInstall {
public:
	explicit DiagnosticInstall(DiagnosticSink* sink)
	{
		ResetDiagnosticState();
		InstallDiagnosticSink(sink);
	}
	~DiagnosticInstall()
	{
		InstallDiagnosticSink(nullptr);
		ResetDiagnosticState();
	}
	DiagnosticInstall(const DiagnosticInstall&) = delete;
	DiagnosticInstall& operator=(const DiagnosticInstall&) = delete;
};

} // namespace mister
