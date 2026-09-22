// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "daemon/protocol.hpp"

#include <cstring>
#include <utility>

#include "daemon/json.hpp"
#include "native/generated/fes_simple_computer.hpp"
#include "native/generated/fes_application.hpp"

namespace mister {
namespace daemon {
namespace {

const std::size_t kMaximumResponseLineBytes = 65536;
const std::size_t kResponseTerminatorBytes = 1;
const std::size_t kMaximumResponsePayloadBytes =
	kMaximumResponseLineBytes - kResponseTerminatorBytes;

Error Invalid(const std::string& message)
{
	return {ErrorCode::invalid_request, message, "request"};
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

bool Path(const std::string& value)
{
	return !value.empty() && value.size() <= 4095 && value[0] == '/' &&
		value.find('\0') == std::string::npos;
}

bool PackageID(const std::string& value)
{
	if (value.size() != 64) return false;
	for (unsigned char character : value)
		if (!((character >= '0' && character <= '9') ||
			(character >= 'a' && character <= 'f'))) return false;
	return true;
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

void AppendContract(BoundedOutput* output, const VersionedContract& contract)
{
	output->Append("{\"id\":");
	AppendQuoted(output, contract.id);
	output->Append(",\"major\":");
	output->Append(std::to_string(contract.major));
	output->Append(",\"minor\":");
	output->Append(std::to_string(contract.minor));
	output->Append("}");
}

void AppendSupportedInterface(BoundedOutput* output,
	const SupportedInterface& interface)
{
	output->Append("{\"id\":");
	AppendQuoted(output, interface.id);
	output->Append(",\"major\":");
	output->Append(std::to_string(interface.major));
	output->Append(",\"minor\":");
	output->Append(std::to_string(interface.minor));
	output->Append("}");
}

void AppendDescriptor(BoundedOutput* output, const CoreDescriptor& descriptor)
{
	output->Append("{\"format\":");
	output->Append(std::to_string(descriptor.format));
	output->Append(",\"core\":{\"id\":");
	AppendQuoted(output, descriptor.core.id);
	output->Append(",\"name\":");
	AppendQuoted(output, descriptor.core.name);
	output->Append(",\"description\":");
	AppendQuoted(output, descriptor.core.description);
	output->Append(",\"version\":");
	AppendQuoted(output, descriptor.core.version);
	if (!descriptor.core.system.empty()) {
		output->Append(",\"system\":");
		AppendQuoted(output, descriptor.core.system);
	}
	output->Append("},\"target\":{\"platform\":");
	AppendQuoted(output, descriptor.target.platform);
	output->Append(",\"device\":");
	AppendQuoted(output, descriptor.target.device);
	output->Append(",\"programming_profile\":");
	AppendQuoted(output, descriptor.target.programming_profile);
	output->Append("},\"payload\":{\"file\":");
	AppendQuoted(output, descriptor.payload.file);
	output->Append(",\"size\":");
	output->Append(std::to_string(descriptor.payload.size));
	output->Append(",\"sha256\":");
	AppendQuoted(output, descriptor.payload.sha256);
	output->Append("},\"abi\":");
	AppendContract(output, descriptor.abi);
	output->Append(",\"interfaces\":[");
	for (std::size_t index = 0; index < descriptor.interfaces.size(); ++index) {
		if (index != 0) output->Append(',');
		const CoreInterface& interface = descriptor.interfaces[index];
		output->Append("{\"id\":");
		AppendQuoted(output, interface.id);
		output->Append(",\"major\":");
		output->Append(std::to_string(interface.major));
		output->Append(",\"minor\":");
		output->Append(std::to_string(interface.minor));
		output->Append(",\"required\":");
		output->Append(interface.required ? "true" : "false");
		output->Append("}");
	}
	output->Append("],\"build\":{\"id\":");
	AppendQuoted(output, descriptor.build.id);
	output->Append(",\"repository\":");
	AppendQuoted(output, descriptor.build.repository);
	output->Append(",\"revision\":");
	AppendQuoted(output, descriptor.build.revision);
	output->Append(",\"recipe_sha256\":");
	AppendQuoted(output, descriptor.build.recipe_sha256);
	output->Append(",\"toolchain\":");
	AppendQuoted(output, descriptor.build.toolchain);
	output->Append("}}");
}

void AppendError(BoundedOutput* output, const Error& error)
{
	if (error.ok()) {
		output->Append("null");
		return;
	}
	output->Append("{\"code\":");
	AppendQuoted(output, ErrorCodeName(error.code));
	output->Append(",\"message\":");
	AppendQuoted(output, error.message);
	{
		output->Append(",\"phase\":");
		AppendQuoted(output, error.phase.empty() ? "lifecycle" : error.phase);
		if (!error.expected.empty()) {
			output->Append(",\"expected\":");
			AppendQuoted(output, error.expected);
		}
		if (!error.observed.empty()) {
			output->Append(",\"observed\":");
			AppendQuoted(output, error.observed);
		}
	}
	output->Append("}");
}

bool TryEncodeV2Response(bool ok, const Status& status, const std::string& version,
	const CorePackageInspection* inspection, const CoreData* core_data, std::string* response)
{
	BoundedOutput output(kMaximumResponsePayloadBytes);
	output.Append("{\"protocol\":2,\"ok\":");
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
	AppendError(&output, status.error);
	output.Append(",\"version\":");
	AppendQuoted(&output, version);
	output.Append(",\"capabilities\":{\"programming_profiles\":[");
	for (std::size_t index = 0;
		index < status.capabilities.programming_profiles.size(); ++index) {
		if (index != 0) output.Append(',');
		AppendQuoted(&output, status.capabilities.programming_profiles[index]);
	}
	output.Append("],\"abis\":[");
	for (std::size_t index = 0; index < status.capabilities.abis.size(); ++index) {
		if (index != 0) output.Append(',');
		const SupportedABI& abi = status.capabilities.abis[index];
		output.Append("{\"id\":");
		AppendQuoted(&output, abi.id);
		output.Append(",\"major\":");
		output.Append(std::to_string(abi.major));
		output.Append(",\"minor\":");
		output.Append(std::to_string(abi.minor));
		output.Append(",\"interfaces\":[");
		for (std::size_t interface = 0; interface < abi.interfaces.size(); ++interface) {
			if (interface != 0) output.Append(',');
			AppendSupportedInterface(&output, abi.interfaces[interface]);
		}
		output.Append("]}");
	}
	output.Append("],\"active_interfaces\":[");
	for (std::size_t index = 0;
		index < status.capabilities.active_interfaces.size(); ++index) {
		if (index != 0) output.Append(',');
		AppendSupportedInterface(&output,
			status.capabilities.active_interfaces[index]);
	}
	output.Append("]");
	const auto& media = status.capabilities.media_stream;
	if (!media.interface.id.empty() && status.generation != 0 &&
		!status.active_package.package_id.empty()) {
		output.Append(",\"media_stream\":{\"interface\":");
		AppendSupportedInterface(&output, media.interface);
		output.Append(",\"min_bytes\":");
		output.Append(std::to_string(media.min_bytes));
		output.Append(",\"max_bytes\":");
		output.Append(std::to_string(media.max_bytes));
		output.Append(",\"chunk_bytes\":");
		output.Append(std::to_string(media.chunk_bytes));
		output.Append("}");
	}
	output.Append("},\"active_package\":");
	if (status.active_package.package_id.empty()) {
		output.Append("null");
	} else {
		output.Append("{\"package_id\":");
		AppendQuoted(&output, status.active_package.package_id);
		output.Append(",\"descriptor\":");
		AppendDescriptor(&output, status.active_package.descriptor);
		const auto& composition=status.active_package.composition;
		if (!composition.id.empty()) {
			output.Append(",\"composition\":");
			output.Append("{\"composition_id\":");AppendQuoted(&output,composition.id);
			output.Append(",\"package_id\":");AppendQuoted(&output,composition.package_id);
			output.Append(",\"expansion_id\":");AppendQuoted(&output,composition.expansion_id);
			output.Append(",\"shell_sha256\":");AppendQuoted(&output,composition.shell_sha256);
			output.Append(",\"payload_sha256\":");AppendQuoted(&output,composition.payload_sha256);
			output.Append(",\"payload_size\":");output.Append(std::to_string(composition.payload_size));
			output.Append("}");
		}
		output.Append(",\"persistence_mode\":");
		AppendQuoted(&output, status.core_data.mode);
		output.Append(",\"observed\":{\"abi\":");
		if (status.active_package.observed.abi.id.empty()) output.Append("null");
		else AppendContract(&output, status.active_package.observed.abi);
		output.Append(",\"build_id\":");
		AppendIdentity(&output, status.active_package.observed.build_id);
		output.Append("}}");
	}
	output.Append(",\"generation\":");
	if (status.generation == 0) output.Append("null");
	else output.Append(std::to_string(status.generation));
	output.Append(",\"inspected_package\":");
	if (inspection == nullptr) {
		output.Append("null");
	} else {
		output.Append("{\"package_id\":");
		AppendQuoted(&output, inspection->package_id);
		output.Append(",\"descriptor\":");
		AppendDescriptor(&output, inspection->descriptor);
		output.Append(",\"persistence_layout\":");
		if (inspection->persistence_layout.id.empty())
			output.Append("null");
		else
			AppendContract(&output, inspection->persistence_layout);
		output.Append(",\"compatible\":");
		output.Append(inspection->compatible ? "true" : "false");
		output.Append(",\"compatibility_error\":");
		AppendError(&output, inspection->compatibility_error);
		output.Append("}");
	}
	if (core_data != nullptr) {
		output.Append(",\"core_data\":{\"package_id\":");
		AppendQuoted(&output, core_data->package_id);
		output.Append(",\"core_id\":");
		AppendQuoted(&output, core_data->core_id);
		output.Append(",\"layout\":");
		if (core_data->layout.id.empty())
			output.Append("null");
		else
			AppendContract(&output, core_data->layout);
		output.Append(",\"mode\":");
		AppendQuoted(&output, core_data->mode);
		output.Append(",\"revision\":");
		AppendQuoted(&output, core_data->revision);
		output.Append(",\"paddle_speed\":");
		output.Append(std::to_string(core_data->paddle_speed));
		output.Append(",\"best_rally\":");
		output.Append(std::to_string(core_data->best_rally));
		output.Append("}");
	}
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
	request->protocol = protocol->integer_value;
	const json::Value* operation = Find(root, "operation");
	if (operation == nullptr || operation->type != json::Type::string)
		return Invalid("request requires operation");

	Request parsed;
	parsed.protocol = protocol->integer_value;
	Error error;
	if (parsed.protocol == 2) {
		if (operation->string_value == "status" || operation->string_value == "stop") {
			const char* const fields[] = {"protocol", "operation"};
			if (!HasOnly(root, fields, 2, &error)) return error;
			parsed.operation = operation->string_value == "status" ?
				Operation::status : Operation::stop;
		} else if (operation->string_value == "load_composed_core") {
			const char* const fields[]={"protocol","operation","package_path","package_id","expansion_path","payload_path","composition"};
			if (!HasOnly(root,fields,7,&error)) return error;
			const std::string *path=nullptr,*id=nullptr,*expansion=nullptr,*payload=nullptr;
			if (!StringMember(root,"package_path",&path,&error) || !StringMember(root,"package_id",&id,&error) ||
				!StringMember(root,"expansion_path",&expansion,&error) || !StringMember(root,"payload_path",&payload,&error)) return error;
			if (!Path(*path) || !Path(*expansion) || !Path(*payload) || !PackageID(*id)) return Invalid("invalid composition paths or package identity");
			const auto* tuple=Find(root,"composition");
			if (!tuple || tuple->type!=json::Type::object) return Invalid("composition must be an object");
			const char* const tuple_fields[]={"composition_id","package_id","expansion_id","shell_sha256","payload_sha256","payload_size"};
			if (!HasOnly(*tuple,tuple_fields,6,&error)) return error;
			auto& c=parsed.composition_request.composition;
			std::string* destinations[]={&c.id,&c.package_id,&c.expansion_id,&c.shell_sha256,&c.payload_sha256};
			for (unsigned i=0;i<5;++i) {
				const std::string* value=nullptr;
				if (!StringMember(*tuple,tuple_fields[i],&value,&error)) return error;
				if (!PackageID(*value)) return Invalid("composition identities must be lowercase SHA-256");
				*destinations[i]=*value;
			}
			const auto* size=Find(*tuple,"payload_size");
			if (!size || size->type!=json::Type::integer || size->integer_value<40408 || size->integer_value>32*1024*1024 || c.package_id!=*id)
				return Invalid("invalid composition size or package binding");
			c.payload_size=static_cast<std::uint64_t>(size->integer_value);
			parsed.operation=Operation::load_composed_core;parsed.package_path=*path;parsed.package_id=*id;
			parsed.composition_request.expansion_path=*expansion;parsed.composition_request.payload_path=*payload;
		} else if (operation->string_value == "load_initialized_core" ||
			operation->string_value == "load_initialized_library_core" ||
			operation->string_value == "load_initialized_composed_core") {
			const bool library = operation->string_value == "load_initialized_library_core";
			const bool composed = operation->string_value == "load_initialized_composed_core";
			const char* plain[] = {"protocol","operation","package_path","package_id","programmed_path","programmed_sha256"};
			const char* with_root[] = {"protocol","operation","package_path","package_id","data_root","programmed_path","programmed_sha256"};
			const char* with_cart[] = {"protocol","operation","package_path","package_id","expansion_path","payload_path","composition","programmed_path","programmed_sha256"};
			const char* const* fields = plain;
			unsigned count = 6;
			if (library) { fields = with_root; count = 7; }
			if (composed) { fields = with_cart; count = 9; }
			if (!HasOnly(root, fields, count, &error)) return error;
			const std::string *path=nullptr,*id=nullptr,*programmed=nullptr,*digest=nullptr;
			if (!StringMember(root,"package_path",&path,&error) || !StringMember(root,"package_id",&id,&error) ||
				!StringMember(root,"programmed_path",&programmed,&error) || !StringMember(root,"programmed_sha256",&digest,&error))
				return error;
			if (!Path(*path) || !Path(*programmed) || !PackageID(*id) || !PackageID(*digest))
				return Invalid("invalid initialized core paths or digest");
			parsed.package_path=*path; parsed.package_id=*id;
			parsed.programmed_path=*programmed; parsed.programmed_sha256=*digest;
			parsed.operation = Operation::load_initialized_core;
			if (library) {
				const std::string* root_path=nullptr;
				if (!StringMember(root,"data_root",&root_path,&error) || !Path(*root_path))
					return Invalid("invalid initialized core data root");
				parsed.data_root=*root_path;
				parsed.operation = Operation::load_initialized_library_core;
			}
			if (composed) {
				const std::string *expansion=nullptr,*payload=nullptr;
				if (!StringMember(root,"expansion_path",&expansion,&error) || !StringMember(root,"payload_path",&payload,&error) ||
					!Path(*expansion) || !Path(*payload))
					return Invalid("invalid initialized composition paths");
				const auto* tuple=Find(root,"composition");
				if (!tuple || tuple->type!=json::Type::object) return Invalid("composition must be an object");
				const char* const tuple_fields[]={"composition_id","package_id","expansion_id","shell_sha256","payload_sha256","payload_size"};
				if (!HasOnly(*tuple,tuple_fields,6,&error)) return error;
				auto& c=parsed.composition_request.composition;
				std::string* destinations[]={&c.id,&c.package_id,&c.expansion_id,&c.shell_sha256,&c.payload_sha256};
				for (unsigned i=0;i<5;++i) {
					const std::string* value=nullptr;
					if (!StringMember(*tuple,tuple_fields[i],&value,&error)) return error;
					if (!PackageID(*value)) return Invalid("composition identities must be lowercase SHA-256");
					*destinations[i]=*value;
				}
				const auto* size=Find(*tuple,"payload_size");
				if (!size || size->type!=json::Type::integer || size->integer_value<40408 || size->integer_value>32*1024*1024 || c.package_id!=*id)
					return Invalid("invalid composition size or package binding");
				c.payload_size=static_cast<std::uint64_t>(size->integer_value);
				parsed.composition_request.expansion_path=*expansion;
				parsed.composition_request.payload_path=*payload;
				parsed.operation = Operation::load_initialized_composed_core;
			}
		} else if (operation->string_value == "inspect_core" ||
			operation->string_value == "load_core") {
			const char* const fields[] = {
				"protocol", "operation", "package_path", "package_id"};
			if (!HasOnly(root, fields, 4, &error)) return error;
			const std::string* package_path = nullptr;
			const std::string* package_id = nullptr;
			if (!StringMember(root, "package_path", &package_path, &error) ||
				!StringMember(root, "package_id", &package_id, &error)) return error;
			if (!Path(*package_path))
				return Invalid("package_path must be an absolute path of at most 4095 bytes");
			if (!PackageID(*package_id))
				return Invalid("package_id must be exactly 64 lowercase hexadecimal characters");
			parsed.operation = operation->string_value == "inspect_core" ?
				Operation::inspect_core : Operation::load_core;
			parsed.package_path = *package_path;
			parsed.package_id = *package_id;
		} else if (operation->string_value == "load_library_core" ||
				   operation->string_value == "inspect_core_data" ||
				   operation->string_value == "update_core_settings") {
			const bool update = operation->string_value == "update_core_settings";
			const char* const fields[] = {"protocol", "operation", "package_path", "package_id",
				"data_root", "expected_revision", "paddle_speed"};
			if (!HasOnly(root, fields, update ? 7 : 5, &error))
				return error;
			const std::string *path = nullptr, *id = nullptr, *data_root = nullptr;
			if (!StringMember(root, "package_path", &path, &error) ||
				!StringMember(root, "package_id", &id, &error) ||
				!StringMember(root, "data_root", &data_root, &error))
				return error;
			if (!Path(*path) || !Path(*data_root) || !PackageID(*id))
				return Invalid("invalid core-data package or root");
			parsed.package_path = *path;
			parsed.package_id = *id;
			parsed.data_root = *data_root;
			parsed.operation = operation->string_value == "load_library_core"
								   ? Operation::load_library_core
								   : Operation::inspect_core_data;
			if (update) {
				const std::string* revision = nullptr;
				const auto* speed = Find(root, "paddle_speed");
				if (!StringMember(root, "expected_revision", &revision, &error))
					return error;
				if ((*revision != "absent" && !PackageID(*revision)) || !speed ||
					speed->type != json::Type::integer || speed->integer_value < 0 ||
					speed->integer_value > 2)
					return Invalid("invalid settings revision or paddle speed");
				parsed.operation = Operation::update_core_settings;
				parsed.expected_revision = *revision;
				parsed.paddle_speed = static_cast<std::uint16_t>(speed->integer_value);
			}
		} else if (operation->string_value == "set_controller") {
			const char* const fields[] = {"protocol", "operation", "package_id",
				"expected_generation", "port", "buttons", "keypad"};
			if (!HasOnly(root, fields, 7, &error)) return error;
			const std::string* package = nullptr;
			if (!StringMember(root, "package_id", &package, &error)) return error;
			const auto* generation = Find(root, "expected_generation");
			const auto* port = Find(root, "port");
			const auto* buttons = Find(root, "buttons");
			const auto* keypad = Find(root, "keypad");
			using namespace native::generated;
			if (!PackageID(*package) || !generation || generation->type != json::Type::integer ||
				generation->integer_value <= 0 || !port || port->type != json::Type::integer ||
				port->integer_value < 0 || port->integer_value >= FesApplicationControllerPortCount ||
				!buttons || buttons->type != json::Type::integer || buttons->integer_value < 0 ||
				buttons->integer_value > FesApplicationControllerButtonMask ||
				!keypad || keypad->type != json::Type::integer || keypad->integer_value < 0 ||
				keypad->integer_value > FesApplicationControllerKeypadMask)
				return Invalid("invalid controller snapshot");
			parsed.operation = Operation::set_controller;
			parsed.package_id = *package;
			parsed.expected_generation = static_cast<std::uint64_t>(generation->integer_value);
			parsed.controller_port = static_cast<std::uint8_t>(port->integer_value);
			parsed.controller_buttons = static_cast<std::uint16_t>(buttons->integer_value);
			parsed.controller_keypad = static_cast<std::uint16_t>(keypad->integer_value);
		} else if (operation->string_value == "set_keyboard") {
			const char* const fields[] = {"protocol", "operation", "matrix"};
			if (!HasOnly(root, fields, 3, &error)) return error;
			const json::Value* matrix = Find(root, "matrix");
			if (matrix == nullptr || matrix->type != json::Type::integer ||
				matrix->integer_value < 0 ||
				static_cast<std::uint64_t>(matrix->integer_value) > 0xffffffffffull)
				return Invalid("keyboard matrix must be a 40-bit integer");
			parsed.operation = Operation::set_keyboard;
			parsed.keyboard_matrix =
				static_cast<std::uint64_t>(matrix->integer_value);
		} else if (operation->string_value == "load_media_stream") {
			const char* const fields[] = {"protocol", "operation", "path",
				"expected_package_id", "expected_generation", "size"};
			if (!HasOnly(root, fields, 6, &error)) return error;
			const std::string* path = nullptr;
			const std::string* package = nullptr;
			if (!StringMember(root, "path", &path, &error) ||
				!StringMember(root, "expected_package_id", &package, &error)) return error;
			const auto* generation = Find(root, "expected_generation");
			const auto* size = Find(root, "size");
			if (!Path(*path) || !PackageID(*package) || !generation ||
				!((generation->type == json::Type::integer && generation->integer_value > 0) ||
					generation->type == json::Type::unsigned_integer) ||
				!size || size->type != json::Type::integer ||
				size->integer_value < native::generated::FesSimpleComputerMediaStreamMinBytes ||
				size->integer_value > native::generated::FesSimpleComputerMediaStreamMaxBytes)
				return Invalid("invalid media stream path, binding or size");
			parsed.operation = Operation::load_media_stream;
			parsed.media_path = *path;
			parsed.expected_package_id = *package;
			parsed.expected_generation = generation->type == json::Type::unsigned_integer ?
				generation->unsigned_value : static_cast<std::uint64_t>(generation->integer_value);
			parsed.media_size = static_cast<std::uint32_t>(size->integer_value);
		} else if (operation->string_value == "load_firmware") {
			const char* const fields[] = {"protocol", "operation", "path"};
			if (!HasOnly(root, fields, 3, &error)) return error;
			const std::string* path = nullptr;
			if (!StringMember(root, "path", &path, &error)) return error;
			if (!Path(*path))
				return Invalid("path must be an absolute path of at most 4095 bytes");
			parsed.operation = Operation::load_firmware;
			parsed.media_path = *path;
		} else if (operation->string_value == "load_media") {
			const char* const fields[] = {"protocol", "operation", "path"};
			if (!HasOnly(root, fields, 3, &error)) return error;
			const std::string* path = nullptr;
			if (!StringMember(root, "path", &path, &error)) return error;
			if (!Path(*path))
				return Invalid("path must be an absolute path of at most 4095 bytes");
			parsed.operation = Operation::load_media;
			parsed.media_path = *path;
		} else if (operation->string_value == "load_development_rbf") {
			const char* const fields[] = {
				"protocol", "operation", "rbf", "programming_profile"};
			if (!HasOnly(root, fields, 4, &error)) return error;
			const std::string* rbf = nullptr;
			const std::string* profile = nullptr;
			if (!StringMember(root, "rbf", &rbf, &error) ||
				!StringMember(root, "programming_profile", &profile, &error)) return error;
			if (!Path(*rbf))
				return Invalid("rbf must be an absolute path of at most 4095 bytes");
			if (*profile != "development-contained-v1")
				return Invalid("diagnostic programming_profile must be development-contained-v1");
			parsed.operation = Operation::load_development_rbf;
			parsed.rbf = *rbf;
			parsed.programming_profile = *profile;
		} else {
			return Invalid("unknown operation");
		}
		*request = parsed;
		return {};
	}
	return {ErrorCode::unsupported_protocol, "only protocol 2 is supported", "request"};
}

std::string EncodeResponse(std::int64_t, bool ok, const Status& status,
	const std::string& version, const CorePackageInspection* inspected_package,
	const CoreData* core_data)
{
	std::string response;
	if (TryEncodeV2Response(ok, status, version, inspected_package, core_data, &response))
		return response;
	Status fallback = status;
	fallback.system.clear();
	fallback.core.clear();
	fallback.active_package = {};
	fallback.capabilities.active_interfaces.clear();
	fallback.generation = 0;
	fallback.error = {ErrorCode::io_failed,
		"response exceeds 65536 bytes", "lifecycle"};
	if (TryEncodeV2Response(false, fallback, "-", nullptr, nullptr, &response))
		return response;
	return "{\"protocol\":2,\"ok\":false,\"state\":\"idle\","
		"\"execution\":\"none\",\"system\":null,\"core\":null,"
		"\"error\":{\"code\":\"io_failed\","
		"\"message\":\"response exceeds 65536 bytes\","
		"\"phase\":\"lifecycle\"},\"version\":\"-\","
		"\"capabilities\":{\"programming_profiles\":[],\"abis\":[],"
		"\"active_interfaces\":[]},\"active_package\":null,"
		"\"generation\":null,\"inspected_package\":null}";
}

} // namespace daemon
} // namespace mister
