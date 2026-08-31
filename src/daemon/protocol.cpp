// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "daemon/protocol.hpp"

#include <cstring>

#include "daemon/json.hpp"

namespace mister {
namespace daemon {
namespace {

Error Invalid(const std::string& message)
{
	return {ErrorCode::invalid_request, message};
}

const json::Value* Find(const json::Value& object, const char* name)
{
	for (const auto& member : object.object) {
		if (member.first == name) return &member.second;
	}
	return nullptr;
}

bool HasOnly(const json::Value& object, const char* const* names, std::size_t count,
		Error* error)
{
	for (const auto& member : object.object) {
		bool known = false;
		for (std::size_t index = 0; index < count; ++index) {
			if (member.first == names[index]) {
				known = true;
				break;
			}
		}
		if (!known) {
			*error = Invalid("request contains an unknown field");
			return false;
		}
	}
	return true;
}

bool Identifier(const std::string& value)
{
	if (value.empty() || value.size() > 32) return false;
	for (unsigned char character : value) {
		if (!((character >= 'a' && character <= 'z') ||
				(character >= '0' && character <= '9') || character == '_' || character == '-'))
			return false;
	}
	return true;
}

bool Path(const std::string& value)
{
	return !value.empty() && value.size() <= 4095 && value[0] == '/';
}

bool StringMember(const json::Value& object, const char* name, const std::string** value,
		Error* error)
{
	const json::Value* member = Find(object, name);
	if (member == nullptr || member->type != json::Type::string) {
		*error = Invalid(std::string("request requires string field: ") + name);
		return false;
	}
	*value = &member->string_value;
	return true;
}

bool ObjectMember(const json::Value& object, const char* name, const json::Value** value,
		Error* error)
{
	const json::Value* member = Find(object, name);
	if (member == nullptr || member->type != json::Type::object) {
		*error = Invalid(std::string("request requires object field: ") + name);
		return false;
	}
	*value = member;
	return true;
}

bool ParseMedia(const json::Value& object, std::vector<Media>* media, Error* error)
{
	media->clear();
	for (const auto& member : object.object) {
		if (!Identifier(member.first) || member.second.type != json::Type::string ||
			!Path(member.second.string_value)) {
			*error = Invalid("media contains an invalid name or path");
			return false;
		}
		media->push_back({member.first, member.second.string_value});
	}
	return true;
}

bool ParseSettings(const json::Value& object, std::vector<Setting>* settings, Error* error)
{
	settings->clear();
	for (const auto& member : object.object) {
		if (!Identifier(member.first) || member.second.type != json::Type::string ||
			member.second.string_value.size() > 64) {
			*error = Invalid("settings contains an invalid name or value");
			return false;
		}
		settings->push_back({member.first, member.second.string_value});
	}
	return true;
}

void AppendQuoted(std::string* output, const std::string& value)
{
	output->push_back('"');
	static const char hex[] = "0123456789abcdef";
	for (unsigned char character : value) {
		switch (character) {
		case '"': *output += "\\\""; break;
		case '\\': *output += "\\\\"; break;
		case '\b': *output += "\\b"; break;
		case '\f': *output += "\\f"; break;
		case '\n': *output += "\\n"; break;
		case '\r': *output += "\\r"; break;
		case '\t': *output += "\\t"; break;
		default:
			if (character < 0x20) {
				*output += "\\u00";
				output->push_back(hex[(character >> 4) & 0x0f]);
				output->push_back(hex[character & 0x0f]);
			} else {
				output->push_back(static_cast<char>(character));
			}
		}
	}
	output->push_back('"');
}

void AppendIdentity(std::string* output, const std::string& value)
{
	if (value.empty()) *output += "null";
	else AppendQuoted(output, value);
}

} // namespace

Error ParseRequest(const std::string& line, Request* request)
{
	if (request == nullptr) return Invalid("missing output request");
	json::Value root;
	std::string message;
	if (!json::Parse(line, &root, &message)) return Invalid(message);
	if (root.type != json::Type::object) return Invalid("request must be an object");

	const json::Value* protocol = Find(root, "protocol");
	if (protocol == nullptr) return Invalid("request requires protocol");
	if (protocol->type != json::Type::integer) return Invalid("protocol must be an integer");
	if (protocol->integer_value != 1)
		return {ErrorCode::unsupported_protocol, "unsupported protocol"};
	const json::Value* operation = Find(root, "operation");
	if (operation == nullptr || operation->type != json::Type::string)
		return Invalid("request requires operation");

	Request parsed;
	Error error;
	if (operation->string_value == "status" || operation->string_value == "stop") {
		const char* const fields[] = {"protocol", "operation"};
		if (!HasOnly(root, fields, 2, &error)) return error;
		parsed.operation = operation->string_value == "status" ? Operation::status : Operation::stop;
	} else if (operation->string_value == "load_development_rbf") {
		const char* const fields[] = {"protocol", "operation", "rbf"};
		if (!HasOnly(root, fields, 3, &error)) return error;
		const std::string* rbf = nullptr;
		if (!StringMember(root, "rbf", &rbf, &error)) return error;
		if (!Path(*rbf)) return Invalid("rbf must be an absolute path of at most 4095 bytes");
		parsed.operation = Operation::load_development_rbf;
		parsed.rbf = *rbf;
	} else if (operation->string_value == "launch") {
		const char* const fields[] = {"protocol", "operation", "system", "rbf", "media", "settings"};
		if (!HasOnly(root, fields, 6, &error)) return error;
		const std::string* system = nullptr;
		const std::string* rbf = nullptr;
		const json::Value* media = nullptr;
		const json::Value* settings = nullptr;
		if (!StringMember(root, "system", &system, &error) ||
			!StringMember(root, "rbf", &rbf, &error) ||
			!ObjectMember(root, "media", &media, &error) ||
			!ObjectMember(root, "settings", &settings, &error)) return error;
		if (!Identifier(*system)) return Invalid("system must be an identifier");
		if (!Path(*rbf)) return Invalid("rbf must be an absolute path of at most 4095 bytes");
		parsed.operation = Operation::launch;
		parsed.launch.system = *system;
		parsed.launch.rbf = *rbf;
		if (!ParseMedia(*media, &parsed.launch.media, &error) ||
			!ParseSettings(*settings, &parsed.launch.settings, &error)) return error;
	} else {
		return Invalid("unknown operation");
	}

	*request = parsed;
	return {};
}

std::string EncodeResponse(bool ok, const Status& status, const std::string& version)
{
	std::string output = "{\"protocol\":1,\"ok\":";
	output += ok ? "true" : "false";
	output += ",\"state\":";
	AppendQuoted(&output, StateName(status.state));
	output += ",\"execution\":";
	AppendQuoted(&output, ExecutionName(status.execution));
	output += ",\"system\":";
	AppendIdentity(&output, status.system);
	output += ",\"core\":";
	AppendIdentity(&output, status.core);
	output += ",\"error\":";
	if (status.error.ok()) {
		output += "null";
	} else {
		output += "{\"code\":";
		AppendQuoted(&output, ErrorCodeName(status.error.code));
		output += ",\"message\":";
		AppendQuoted(&output, status.error.message);
		output += "}";
	}
	output += ",\"version\":";
	AppendQuoted(&output, version);
	output += "}";
	return output;
}

} // namespace daemon
} // namespace mister
