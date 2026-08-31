// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "daemon/protocol.hpp"

#include <cstring>
#include <utility>

#include "daemon/json.hpp"

namespace mister {
namespace daemon {
namespace {

const std::size_t kMaximumResponseLineBytes = 65536;
const std::size_t kResponseTerminatorBytes = 1;
const std::size_t kMaximumResponsePayloadBytes =
	kMaximumResponseLineBytes - kResponseTerminatorBytes;

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

class BoundedOutput {
public:
	explicit BoundedOutput(std::size_t maximum)
		: maximum_(maximum), overflow_(false), output_() {}

	void Append(const char* value)
	{
		Append(value, std::strlen(value));
	}

	void Append(const std::string& value)
	{
		Append(value.data(), value.size());
	}

	void Append(char value) { Append(&value, 1); }
	bool ok() const { return !overflow_; }
	std::string Take() { return std::move(output_); }

private:
	void Append(const char* value, std::size_t size)
	{
		if (overflow_) return;
		if (size > maximum_ - output_.size()) {
			overflow_ = true;
			return;
		}
		output_.append(value, size);
	}

	const std::size_t maximum_;
	bool overflow_;
	std::string output_;
};

void AppendQuoted(BoundedOutput* output, const std::string& value)
{
	output->Append('"');
	static const char hex[] = "0123456789abcdef";
	for (unsigned char character : value) {
		if (!output->ok()) return;
		switch (character) {
		case '"': output->Append("\\\""); break;
		case '\\': output->Append("\\\\"); break;
		case '\b': output->Append("\\b"); break;
		case '\f': output->Append("\\f"); break;
		case '\n': output->Append("\\n"); break;
		case '\r': output->Append("\\r"); break;
		case '\t': output->Append("\\t"); break;
		default:
			if (character < 0x20) {
				output->Append("\\u00");
				output->Append(hex[(character >> 4) & 0x0f]);
				output->Append(hex[character & 0x0f]);
			} else {
				output->Append(static_cast<char>(character));
			}
		}
	}
	output->Append('"');
}

void AppendIdentity(BoundedOutput* output, const std::string& value)
{
	if (value.empty()) output->Append("null");
	else AppendQuoted(output, value);
}

bool TryEncodeResponse(bool ok, const Status& status, const std::string& version,
	std::string* response)
{
	BoundedOutput output(kMaximumResponsePayloadBytes);
	output.Append("{\"protocol\":1,\"ok\":");
	output.Append(ok ? "true" : "false");
	output.Append(",\"state\":");
	AppendQuoted(&output, StateName(status.state));
	output.Append(",\"execution\":");
	AppendQuoted(&output, ExecutionName(status.execution));
	output.Append(",\"system\":");
	AppendIdentity(&output, status.system);
	output.Append(",\"core\":");
	AppendIdentity(&output, status.core);
	output.Append(",\"error\":");
	if (status.error.ok()) {
		output.Append("null");
	} else {
		output.Append("{\"code\":");
		AppendQuoted(&output, ErrorCodeName(status.error.code));
		output.Append(",\"message\":");
		AppendQuoted(&output, status.error.message);
		output.Append("}");
	}
	output.Append(",\"version\":");
	AppendQuoted(&output, version);
	output.Append("}");
	if (!output.ok()) return false;
	*response = output.Take();
	return true;
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
	const bool unsupported_protocol = protocol->integer_value != 1;
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

	if (unsupported_protocol)
		return {ErrorCode::unsupported_protocol, "unsupported protocol"};
	*request = parsed;
	return {};
}

std::string EncodeResponse(bool ok, const Status& status, const std::string& version)
{
	std::string response;
	if (TryEncodeResponse(ok, status, version, &response)) return response;
	Status fallback = status;
	fallback.system.clear();
	fallback.core.clear();
	fallback.error = {ErrorCode::io_failed, "response exceeds 65536 bytes"};
	if (TryEncodeResponse(false, fallback, "-", &response)) return response;
	return "{\"protocol\":1,\"ok\":false,\"state\":\"idle\","
		"\"execution\":\"none\",\"system\":null,\"core\":null,"
		"\"error\":{\"code\":\"io_failed\","
		"\"message\":\"response exceeds 65536 bytes\"},\"version\":\"-\"}";
}

} // namespace daemon
} // namespace mister
