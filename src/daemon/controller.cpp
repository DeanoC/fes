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
	if (!parsed.ok()) return Respond(parsed);

	Error result;
	switch (request.operation) {
	case Operation::status:
		break;
	case Operation::launch:
		result = runtime_.LaunchGame(request.launch);
		break;
	case Operation::load_development_rbf:
		result = runtime_.LoadDevelopmentRBF(request.rbf);
		break;
	case Operation::stop:
		result = runtime_.Stop();
		break;
	}
	return Respond(result);
}

std::string Controller::InvalidRequest(const std::string& message)
{
	return Respond({ErrorCode::invalid_request, message});
}

std::string Controller::Respond(const Error& result)
{
	Status observed = runtime_.status();
	if (!result.ok()) observed.error = result;
	return EncodeResponse(result.ok(), observed, version_);
}

} // namespace daemon
} // namespace mister
