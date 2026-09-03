// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "libmister-runtime/runtime.h"

#include <array>
#include <set>
#include <utility>

namespace mister {
namespace {

Error Invalid(const char* message)
{
	return {ErrorCode::invalid_request, message};
}

bool ValidIdentifier(const std::string& value)
{
	if (value.empty() || value.size() > 32) return false;
	for (unsigned char byte : value) {
		if (!((byte >= 'a' && byte <= 'z') ||
			(byte >= '0' && byte <= '9') || byte == '_' || byte == '-'))
			return false;
	}
	return true;
}

bool ValidCore(const std::string& value)
{
	if (value.empty() || value.size() > 64) return false;
	for (unsigned char byte : value) {
		if (byte < 0x20 || byte > 0x7e) return false;
	}
	return true;
}

bool ValidAbsolutePath(const std::string& value)
{
	return !value.empty() && value.size() <= 4095 && value[0] == '/' &&
		value.find('\0') == std::string::npos;
}

bool ValidExtension(const std::string& value)
{
	if (value.size() < 2 || value.size() > 16 || value[0] != '.') return false;
	for (std::size_t index = 1; index < value.size(); ++index) {
		const unsigned char byte = static_cast<unsigned char>(value[index]);
		if (!((byte >= 'a' && byte <= 'z') ||
			(byte >= '0' && byte <= '9')))
			return false;
	}
	return true;
}

bool HasExtension(const std::string& path, const std::string& extension)
{
	return path.size() > extension.size() &&
		path.compare(path.size() - extension.size(), extension.size(), extension) == 0;
}

bool ValidCoreRecipe(const CoreRecipe& recipe)
{
	return recipe.reset_assert_word != 0 && recipe.initial_status_word != 0 &&
		recipe.file_wire == FileWireFormat::little_endian_byte_pairs;
}

bool ValidInputRecipe(const InputRecipe& recipe)
{
	if (recipe.player_count != 1 || recipe.player_command == 0) return false;
	const std::array<std::uint16_t, 8> masks = {{recipe.up, recipe.down,
		recipe.left, recipe.right, recipe.a, recipe.b, recipe.c, recipe.start}};
	std::uint16_t seen = 0;
	for (const std::uint16_t mask : masks) {
		if (mask == 0 || (mask & (mask - 1u)) != 0 || (seen & mask) != 0)
			return false;
		seen = static_cast<std::uint16_t>(seen | mask);
	}
	return true;
}

bool ValidUtf8(const std::string& value)
{
	if (value.empty() || value.size() > 64) return false;
	for (std::size_t index = 0; index < value.size();) {
		const unsigned char first = static_cast<unsigned char>(value[index]);
		std::size_t continuation = 0;
		std::uint32_t codepoint = 0;
		if (first <= 0x7f) {
			++index;
			continue;
		} else if (first >= 0xc2 && first <= 0xdf) {
			continuation = 1;
			codepoint = first & 0x1fu;
		} else if (first >= 0xe0 && first <= 0xef) {
			continuation = 2;
			codepoint = first & 0x0fu;
		} else if (first >= 0xf0 && first <= 0xf4) {
			continuation = 3;
			codepoint = first & 0x07u;
		} else {
			return false;
		}
		if (index + continuation >= value.size()) return false;
		for (std::size_t offset = 1; offset <= continuation; ++offset) {
			const unsigned char byte =
				static_cast<unsigned char>(value[index + offset]);
			if ((byte & 0xc0u) != 0x80u) return false;
			codepoint = (codepoint << 6) | (byte & 0x3fu);
		}
		if ((continuation == 2 && codepoint < 0x800u) ||
			(continuation == 3 && codepoint < 0x10000u) ||
			(codepoint >= 0xd800u && codepoint <= 0xdfffu) ||
			codepoint > 0x10ffffu)
			return false;
		index += continuation + 1;
	}
	return true;
}

const MediaRule* FindMedia(const Profile& profile, const std::string& role)
{
	for (const MediaRule& rule : profile.media) {
		if (rule.role == role) return &rule;
	}
	return nullptr;
}

const SettingRule* FindSetting(const Profile& profile, const std::string& name)
{
	for (const SettingRule& rule : profile.settings) {
		if (rule.name == name) return &rule;
	}
	return nullptr;
}

} // namespace

Error Profiles::Add(Profile profile)
{
	if (!ValidIdentifier(profile.system)) return Invalid("invalid system identifier");
	if (!ValidCore(profile.expected_core)) return Invalid("invalid expected core");
	if (!ValidAbsolutePath(profile.rbf)) return Invalid("invalid profile RBF path");
	if (profile.media.size() > 8) return Invalid("too many media rules");
	if (profile.settings.size() > 16) return Invalid("too many setting rules");
	if (!ValidCoreRecipe(profile.core)) return Invalid("invalid core recipe");
	if (!ValidInputRecipe(profile.input)) return Invalid("invalid input recipe");
	for (const Profile& existing : profiles_) {
		if (existing.system == profile.system) return Invalid("duplicate system");
	}
	std::set<std::string> roles;
	std::set<std::uint8_t> indices;
	for (const MediaRule& rule : profile.media) {
		if (!ValidIdentifier(rule.role)) return Invalid("invalid media role");
		if (!roles.insert(rule.role).second) return Invalid("duplicate media role");
		if (!indices.insert(rule.index).second) return Invalid("duplicate media index");
		if (rule.extensions.empty()) return Invalid("missing media extensions");
		if (rule.maximum_size == 0 || rule.maximum_size > 32u * 1024u * 1024u)
			return Invalid("invalid media size limit");
		std::set<std::string> extensions;
		for (const std::string& extension : rule.extensions) {
			if (!ValidExtension(extension)) return Invalid("invalid media extension");
			if (!extensions.insert(extension).second)
				return Invalid("duplicate media extension");
		}
	}
	std::set<std::string> settings;
	for (const SettingRule& rule : profile.settings) {
		if (!ValidIdentifier(rule.name)) return Invalid("invalid setting name");
		if (!settings.insert(rule.name).second) return Invalid("duplicate setting name");
		std::set<std::string> values;
		for (const std::string& value : rule.allowed_values) {
			if (!ValidUtf8(value)) return Invalid("invalid setting value");
			if (!values.insert(value).second) return Invalid("duplicate allowed value");
		}
	}
	profiles_.push_back(std::move(profile));
	return {};
}

Error Profiles::Prepare(const Launch& launch, PreparedLaunch* output) const
{
	if (output == nullptr) return Invalid("missing prepared launch output");
	if (!ValidIdentifier(launch.system)) return Invalid("invalid system identifier");
	if (!ValidAbsolutePath(launch.rbf)) return Invalid("invalid RBF path");
	if (launch.media.size() > 8) return Invalid("too many media entries");
	if (launch.settings.size() > 16) return Invalid("too many settings");
	const Profile* profile = nullptr;
	for (const Profile& candidate : profiles_) {
		if (candidate.system == launch.system) {
			profile = &candidate;
			break;
		}
	}
	if (profile == nullptr) return {ErrorCode::unknown_system, "unknown system"};

	PreparedLaunch prepared;
	prepared.system = profile->system;
	prepared.expected_core = profile->expected_core;
	prepared.rbf = launch.rbf;
	prepared.core = profile->core;
	prepared.input = profile->input;
	if (launch.rbf != profile->rbf) return Invalid("RBF path is not profile-owned");
	std::set<std::string> supplied_media;
	for (const Media& media : launch.media) {
		if (!ValidIdentifier(media.role) || !ValidAbsolutePath(media.path))
			return Invalid("invalid media entry");
		if (!supplied_media.insert(media.role).second)
			return Invalid("duplicate media role");
		const MediaRule* rule = FindMedia(*profile, media.role);
		if (rule == nullptr) return Invalid("unknown media role");
		bool allowed_extension = false;
		for (const std::string& extension : rule->extensions) {
			if (HasExtension(media.path, extension)) {
				allowed_extension = true;
				break;
			}
		}
		if (!allowed_extension) return Invalid("media extension not allowed");
		prepared.media.push_back({rule->index, media.path, rule->maximum_size});
	}
	for (const MediaRule& rule : profile->media) {
		if (rule.required && supplied_media.count(rule.role) == 0)
			return {ErrorCode::missing_media, "missing required media"};
	}

	std::set<std::string> supplied_settings;
	for (const Setting& setting : launch.settings) {
		if (!ValidIdentifier(setting.name) || !ValidUtf8(setting.value))
			return Invalid("invalid setting");
		if (!supplied_settings.insert(setting.name).second)
			return Invalid("duplicate setting");
		const SettingRule* rule = FindSetting(*profile, setting.name);
		if (rule == nullptr) return Invalid("unknown setting");
		bool allowed = false;
		for (const std::string& value : rule->allowed_values) {
			if (value == setting.value) {
				allowed = true;
				break;
			}
		}
		if (!allowed) return Invalid("setting value not allowed");
		prepared.settings.push_back(setting);
	}
	*output = std::move(prepared);
	return {};
}

bool Profiles::empty() const
{
	return profiles_.empty();
}

} // namespace mister
