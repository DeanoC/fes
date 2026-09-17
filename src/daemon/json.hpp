// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include <cstdint>
#include <string>
#include <utility>
#include <vector>

namespace mister {
namespace daemon {
namespace json {

enum class Type { object, string, integer, unsigned_integer, boolean, null_value };

struct Value {
	Type type = Type::null_value;
	std::string string_value;
	std::int64_t integer_value = 0;
	std::uint64_t unsigned_value = 0;
	bool boolean_value = false;
	std::vector<std::pair<std::string, Value> > object;
};

// Parses the deliberately small JSON subset used by the local daemon.
// `message` receives a human-readable syntax or shape failure when supplied.
bool Parse(const std::string& input, Value* value, std::string* message);

} // namespace json
} // namespace daemon
} // namespace mister
