// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "native/diagnostic.hpp"

#include <mutex>
#include <string>
#include <vector>

namespace mister_test {

class CaptureDiagnostic final : public mister::DiagnosticSink {
public:
	void Append(mister::DiagnosticEvent event) override
	{
		std::lock_guard<std::mutex> lock(mutex_);
		events_.push_back(std::move(event));
	}

	std::vector<mister::DiagnosticEvent> events() const
	{
		std::lock_guard<std::mutex> lock(mutex_);
		return events_;
	}

	void Clear()
	{
		std::lock_guard<std::mutex> lock(mutex_);
		events_.clear();
	}

	const mister::DiagnosticEvent* Find(const char* kind) const
	{
		std::lock_guard<std::mutex> lock(mutex_);
		for (const mister::DiagnosticEvent& event : events_) {
			if (event.kind == kind) return &event;
		}
		return nullptr;
	}

	std::size_t Count(const char* kind) const
	{
		std::lock_guard<std::mutex> lock(mutex_);
		std::size_t count = 0;
		for (const mister::DiagnosticEvent& event : events_) {
			if (event.kind == kind) ++count;
		}
		return count;
	}

private:
	mutable std::mutex mutex_;
	std::vector<mister::DiagnosticEvent> events_;
};

inline const mister::DiagnosticField* Field(const mister::DiagnosticEvent& event,
	const char* key)
{
	for (const mister::DiagnosticField& field : event.detail) {
		if (field.key == key) return &field;
	}
	return nullptr;
}

inline bool HasBool(const mister::DiagnosticEvent& event, const char* key, bool value)
{
	const mister::DiagnosticField* field = Field(event, key);
	return field != nullptr && field->type == mister::DiagnosticField::Type::boolean &&
		field->boolean_value == value;
}

inline bool HasString(const mister::DiagnosticEvent& event, const char* key,
	const std::string& value)
{
	const mister::DiagnosticField* field = Field(event, key);
	return field != nullptr && field->type == mister::DiagnosticField::Type::string &&
		field->string_value == value;
}

} // namespace mister_test
