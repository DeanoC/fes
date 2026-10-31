// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include <string>

#include "libmister-runtime/runtime.h"

namespace mister {
namespace daemon {

enum class Operation {
 configure_menu,menu_frame_grant,menu_frame_begin,menu_frame_commit,session_display,
	status,
	inspect_core,
	load_core,
	load_composed_core,
	load_parts_core,
	load_parts_library_core,
	inspect_parts_core,
	load_rom_core,
	load_rom_library_core,
	load_rom_composed_core,
	load_initialized_core,
	load_initialized_library_core,
	load_initialized_composed_core,
	load_library_core,
	inspect_core_data,
	update_core_settings,
	set_keyboard,
	set_controller,
	load_media,
	replace_live_media,
	clear_media,
	load_firmware,
	load_media_stream,
	set_keyboard_hid,
	send_mouse_relative,
	insert_media,
	insert_library_media,
	save_media,
	capture_zx81_tape,
	acknowledge_zx81_tape,
	eject_media,
	load_development_rbf,
	stop,
	recover_idle,
};

struct Request {
	std::int64_t protocol = 2;
	Operation operation = Operation::status;
	std::string rbf;
	std::string package_path;
	std::string package_id;
	std::string programming_profile;
	std::string data_root;
	std::string expected_revision;
	std::string game_id;
	CoreSettingsUpdate settings;
	bool has_ata_disk = false;
	AtaDiskImage ata_disk;
	std::uint64_t keyboard_matrix = 0;
	std::uint8_t controller_port = 0;
	std::uint16_t controller_buttons = 0;
	std::uint16_t controller_keypad = 0;
	std::string media_path;
	std::string expected_package_id;
	std::uint64_t expected_generation = 0;
	std::uint32_t media_size = 0;
 std::uint8_t menu_slot=0;
 // What a menu commit drew.
 MenuDraw menu_draw;
 bool visible=false,launcher_focus=false;
	std::uint8_t media_unit = 0;
	MediaDataBinding media_binding;
	KeyboardHidRows keyboard_rows{};
	std::int16_t mouse_dx = 0, mouse_dy = 0;
	std::uint8_t mouse_buttons = 0;
	CoreCompositionRequest composition_request;
	std::string programmed_path;
	std::string programmed_sha256;
	CoreROMLink rom_link;
	CoreROMLinks rom_links;
	bool has_initial_media = false;
	InitialComputerMedia initial_media;
};

struct MenuFrameReply {
 // A grant carries the window descriptor; a preparation names the slot to
 // draw; a presentation reports completion.
 enum class Kind {granted,prepared,presented} kind=Kind::presented;
 std::uint64_t generation=0;
 std::uint8_t slot=0;
 // A preparation's count on its generation.
 std::uint64_t preparation=0;
 // A grant that also covers the blitter's lists and heap, and the
 // menu-blitter minor its firmware declares.
 bool blitter=false;
 std::uint16_t blitter_minor=0;
 std::uint32_t displayed_sequence=0,underflows=0;
 // Present timing, encoded as menu_frame.timing on a commit reply.
 std::uint32_t clean_us=0,blit_us=0,submit_us=0,scanout_wait_us=0;
};
Error ParseRequest(const std::string& line, Request* request);
// `ok` is the current request result.  Status::error is lifecycle evidence and
// is intentionally encoded even when a later status request itself succeeds.
std::string EncodeResponse(std::int64_t protocol, bool ok, const Status& status,
	const std::string& version, const CorePackageInspection* inspected_package = nullptr,
	const CoreData* core_data = nullptr,const MenuFrameReply* menu_frame = nullptr);

} // namespace daemon
} // namespace mister
