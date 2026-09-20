// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include <string>

#include "libmister-runtime/runtime.h"

namespace mister {
namespace daemon {

enum class Operation {
	status,
	launch,
	inspect_core,
	load_core,
	load_library_core,
	inspect_core_data,
	update_core_settings,
	set_keyboard,
	set_controller,
	load_media,
	load_firmware,
	load_media_stream,
	load_development_rbf,
	stop,
};

struct Request {
	std::int64_t protocol = 1;
	Operation operation = Operation::status;
	Launch launch;
	std::string rbf;
	std::string package_path;
	std::string package_id;
	std::string programming_profile;
	std::string data_root;
	std::string expected_revision;
	std::uint16_t paddle_speed = 1;
	std::uint64_t keyboard_matrix = 0;
	std::uint8_t controller_port = 0;
	std::uint16_t controller_buttons = 0;
	std::uint16_t controller_keypad = 0;
	std::string media_path;
	std::string expected_package_id;
	std::uint64_t expected_generation = 0;
	std::uint32_t media_size = 0;
};

Error ParseRequest(const std::string& line, Request* request);
// `ok` is the current request result.  Status::error is lifecycle evidence and
// is intentionally encoded even when a later status request itself succeeds.
std::string EncodeResponse(bool ok, const Status& status, const std::string& version);
std::string EncodeResponse(std::int64_t protocol, bool ok, const Status& status,
	const std::string& version, const CorePackageInspection* inspected_package = nullptr,
	const CoreData* core_data = nullptr);

} // namespace daemon
} // namespace mister
