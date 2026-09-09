// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "daemon/controller.hpp"

#include <utility>

#include "daemon/protocol.hpp"

namespace mister {
namespace daemon {

Controller::Controller(Runtime& runtime, std::string version)
	: runtime_(runtime), version_(std::move(version)) {}

std::string Controller::Handle(const std::string& line)
{
	Request request;
	const Error parsed = ParseRequest(line, &request);
	if (!parsed.ok()) return Respond(request.protocol, parsed);

	Error result;
	CorePackageInspection inspection;
	const CorePackageInspection* inspected = nullptr;
	switch (request.operation) {
	case Operation::status:
		break;
	case Operation::launch:
		result = runtime_.LaunchGame(request.launch);
		break;
	case Operation::inspect_core:
		result = runtime_.InspectCore(request.package_path,
			request.package_id, &inspection);
		if (result.ok()) inspected = &inspection;
		break;
	case Operation::load_core:
		result = runtime_.LoadCore(request.package_path, request.package_id);
		break;
	case Operation::load_development_rbf:
		result = request.protocol == 2 ?
			runtime_.LoadContainedDevelopmentRBF(request.rbf) :
			runtime_.LoadDevelopmentRBF(request.rbf);
		break;
	case Operation::stop:
		result = runtime_.Stop();
		break;
	}
	return Respond(request.protocol, result, inspected);
}

std::string Controller::InvalidRequest(const std::string& message)
{
	return Respond(1, {ErrorCode::invalid_request, message, "request"});
}

std::string Controller::Respond(std::int64_t protocol, const Error& result,
	const CorePackageInspection* inspection)
{
	Status observed = runtime_.status();
	if (!result.ok()) observed.error = result;
	return EncodeResponse(protocol, result.ok(), observed, version_, inspection);
}

} // namespace daemon
} // namespace mister
