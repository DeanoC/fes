// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "daemon/controller.hpp"

#include <utility>

#include "daemon/protocol.hpp"
#include "native/diagnostic.hpp"

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
	CoreData data;
	const CoreData* core_data = nullptr;
	const CorePackageInspection* inspected = nullptr;
	switch (request.operation) {
	case Operation::status:
		break;
	case Operation::inspect_core:
		result = runtime_.InspectCore(request.package_path,
			request.package_id, &inspection);
		if (result.ok()) inspected = &inspection;
		break;
	case Operation::load_library_core:
		result =
			runtime_.LoadLibraryCore(request.package_path, request.package_id, request.data_root);
		EmitFifoConsume("load_library_core", result.ok());
		break;
	case Operation::inspect_core_data:
		result = runtime_.InspectCoreData(
			request.package_path, request.package_id, request.data_root, &data);
		if (result.ok())
			core_data = &data;
		break;
	case Operation::update_core_settings:
		result = runtime_.UpdateCoreSettings(request.package_path, request.package_id,
			request.data_root, request.expected_revision, request.paddle_speed, &data);
		EmitFifoConsume("update_core_settings", result.ok());
		if (result.ok())
			core_data = &data;
		break;
	case Operation::load_core:
		result = runtime_.LoadCore(request.package_path, request.package_id);
		EmitFifoConsume("load_core", result.ok());
		break;
	case Operation::load_composed_core:
		result = runtime_.LoadComposedCore(request.package_path, request.package_id,
			request.composition_request);
		EmitFifoConsume("load_composed_core", result.ok());
		break;
	case Operation::load_initialized_core:
		result = runtime_.LoadInitializedCore(request.package_path, request.package_id,
			request.programmed_path, request.programmed_sha256);
		EmitFifoConsume("load_initialized_core", result.ok());
		break;
	case Operation::load_initialized_library_core:
		result = runtime_.LoadInitializedLibraryCore(request.package_path, request.package_id,
			request.data_root, request.programmed_path, request.programmed_sha256);
		EmitFifoConsume("load_initialized_library_core", result.ok());
		break;
	case Operation::load_initialized_composed_core:
		result = runtime_.LoadInitializedComposedCore(request.package_path, request.package_id,
			request.composition_request, request.programmed_path, request.programmed_sha256);
		EmitFifoConsume("load_initialized_composed_core", result.ok());
		break;
	case Operation::set_keyboard:
		result = runtime_.SetComputerKeyboard(request.keyboard_matrix);
		break;
	case Operation::set_controller:
		result = runtime_.SetController(request.package_id, request.expected_generation,
			request.controller_port, request.controller_buttons, request.controller_keypad);
		break;
	case Operation::load_media:
		result = runtime_.LoadComputerMedia(request.media_path,
			request.expected_package_id, request.expected_generation);
		break;
	case Operation::replace_live_media:
		result = runtime_.ReplaceLiveComputerMedia(request.media_path,
			request.expected_package_id, request.expected_generation);
		break;
	case Operation::clear_media:
		result = runtime_.ClearComputerMedia(request.expected_package_id,
			request.expected_generation);
		break;
	case Operation::load_firmware:
		result = runtime_.LoadComputerFirmware(request.media_path);
		break;
	case Operation::load_media_stream:
		result = runtime_.LoadComputerMediaStream(request.media_path,
			request.expected_package_id, request.expected_generation, request.media_size);
		break;
	case Operation::load_development_rbf:
		result = runtime_.LoadContainedDevelopmentRBF(request.rbf);
		EmitFifoConsume("load_development_rbf", result.ok());
		break;
	case Operation::stop:
		result = runtime_.Stop();
		EmitFifoConsume("stop", result.ok());
		break;
	case Operation::recover_idle:
		result = runtime_.RecoverIdle();
		EmitFifoConsume("recover_idle", result.ok());
		break;
	}
	return Respond(request.protocol, result, inspected, core_data);
}

std::string Controller::InvalidRequest(const std::string& message)
{
	return Respond(1, {ErrorCode::invalid_request, message, "request"});
}

std::string Controller::Respond(std::int64_t protocol, const Error& result,
	const CorePackageInspection* inspection, const CoreData* data)
{
	Status observed = runtime_.status();
	if (!result.ok()) observed.error = result;
	return EncodeResponse(protocol, result.ok(), observed, version_, inspection, data);
}

} // namespace daemon
} // namespace mister
