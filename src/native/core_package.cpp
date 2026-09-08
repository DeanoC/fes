// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/core_package.hpp"

#include "native/generated/de10_nano_programming.hpp"
#include "native/generated/fes_gp.hpp"
#include "native/sha256.hpp"

#include <toml.hpp>

#include <arpa/inet.h>
#include <dirent.h>
#include <fcntl.h>
#include <sys/stat.h>
#include <unistd.h>

#include <algorithm>
#include <cerrno>
#include <cstring>
#include <initializer_list>
#include <limits>
#include <set>
#include <sstream>
#include <utility>

namespace mister {
namespace native {
namespace {

constexpr std::uint64_t kMaximumManifestSize = 65536;
constexpr std::uint64_t kMaximumPayloadSize = 32u * 1024u * 1024u;

Error Invalid(const std::string& message)
{
	return {ErrorCode::invalid_request, message};
}

class DirectoryDescriptor {
public:
	explicit DirectoryDescriptor(int descriptor) : descriptor_(descriptor) {}
	~DirectoryDescriptor() { if (descriptor_ >= 0) close(descriptor_); }
	DirectoryDescriptor(const DirectoryDescriptor&) = delete;
	DirectoryDescriptor& operator=(const DirectoryDescriptor&) = delete;
	int get() const { return descriptor_; }
private:
	int descriptor_;
};

Error CheckDirectoryEntries(int directory)
{
	const int scan_descriptor = openat(directory, ".",
		O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC);
	if (scan_descriptor < 0) return Invalid("cannot inspect package directory");
	DIR* scan = fdopendir(scan_descriptor);
	if (scan == nullptr) {
		close(scan_descriptor);
		return Invalid("cannot inspect package directory");
	}
	std::set<std::string> names;
	errno = 0;
	while (dirent* entry = readdir(scan)) {
		const std::string name(entry->d_name);
		if (name != "." && name != "..") names.insert(name);
		errno = 0;
	}
	const int scan_error = errno;
	closedir(scan);
	if (scan_error != 0) return Invalid("cannot inspect package directory");
	const std::set<std::string> expected = {"core.rbf", "manifest.toml"};
	if (names != expected)
		return Invalid("package directory must contain exactly manifest.toml and core.rbf");
	return {};
}

Error ReadArtifact(const Artifact& artifact, std::string* bytes)
{
	if (artifact.size() > std::numeric_limits<std::size_t>::max())
		return Invalid("package member is too large for this runtime");
	std::string content(static_cast<std::size_t>(artifact.size()), '\0');
	std::size_t offset = 0;
	while (offset < content.size()) {
		const ssize_t count = pread(artifact.fd(), &content[offset],
			content.size() - offset, static_cast<off_t>(offset));
		if (count < 0 && errno == EINTR) continue;
		if (count <= 0) return Invalid("incomplete package member read");
		offset += static_cast<std::size_t>(count);
	}
	*bytes = std::move(content);
	return {};
}

Error HashArtifact(const Artifact& artifact, Sha256* hash)
{
	std::array<unsigned char, 16384> buffer = {};
	std::uint64_t offset = 0;
	while (offset < artifact.size()) {
		const std::size_t wanted = static_cast<std::size_t>(
			std::min<std::uint64_t>(buffer.size(), artifact.size() - offset));
		const ssize_t count = pread(artifact.fd(), buffer.data(), wanted,
			static_cast<off_t>(offset));
		if (count < 0 && errno == EINTR) continue;
		if (count <= 0) return Invalid("incomplete payload read");
		hash->Update(buffer.data(), static_cast<std::size_t>(count));
		offset += static_cast<std::size_t>(count);
	}
	return {};
}

void HashLittleEndian64(Sha256* hash, std::uint64_t value)
{
	std::uint8_t bytes[8] = {};
	for (unsigned index = 0; index < 8; ++index)
		bytes[index] = static_cast<std::uint8_t>(value >> (index * 8));
	hash->Update(bytes, sizeof(bytes));
}

bool ValidUtf8Text(const std::string& value, std::size_t maximum,
	bool allow_empty)
{
	if ((!allow_empty && value.empty()) || value.size() > maximum) return false;
	for (std::size_t index = 0; index < value.size();) {
		const unsigned char first = static_cast<unsigned char>(value[index]);
		std::size_t continuation = 0;
		std::uint32_t codepoint = 0;
		if (first <= 0x7f) {
			codepoint = first;
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
			const unsigned char byte = static_cast<unsigned char>(value[index + offset]);
			if ((byte & 0xc0u) != 0x80u) return false;
			codepoint = (codepoint << 6) | (byte & 0x3fu);
		}
		if ((continuation == 1 && codepoint < 0x80u) ||
			(continuation == 2 && codepoint < 0x800u) ||
			(continuation == 3 && codepoint < 0x10000u) ||
			(codepoint >= 0xd800u && codepoint <= 0xdfffu) ||
			codepoint > 0x10ffffu || codepoint <= 0x1fu ||
			(codepoint >= 0x7fu && codepoint <= 0x9fu))
			return false;
		index += continuation + 1;
	}
	return true;
}

bool ValidIdentifier(const std::string& value)
{
	if (value.empty() || value.size() > 96 || value[0] < 'a' || value[0] > 'z')
		return false;
	for (const unsigned char byte : value) {
		if (!((byte >= 'a' && byte <= 'z') || (byte >= '0' && byte <= '9') ||
			byte == '_' || byte == '.' || byte == '-')) return false;
	}
	return true;
}

bool ValidHex(const std::string& value, std::size_t length, bool lowercase_only)
{
	if (value.size() != length) return false;
	for (const unsigned char byte : value) {
		if (byte >= '0' && byte <= '9') continue;
		if (byte >= 'a' && byte <= 'f') continue;
		if (!lowercase_only && byte >= 'A' && byte <= 'F') continue;
		return false;
	}
	return true;
}

bool ValidSemverIdentifier(const std::string& value, bool prerelease)
{
	if (value.empty()) return false;
	bool numeric = true;
	for (const unsigned char byte : value) {
		if (!((byte >= '0' && byte <= '9') || (byte >= 'A' && byte <= 'Z') ||
			(byte >= 'a' && byte <= 'z') || byte == '-')) return false;
		if (byte < '0' || byte > '9') numeric = false;
	}
	return !(prerelease && numeric && value.size() > 1 && value[0] == '0');
}

bool ValidSemver(const std::string& value)
{
	std::size_t build = value.find('+');
	if (build != std::string::npos && value.find('+', build + 1) != std::string::npos)
		return false;
	const std::string without_build = value.substr(0, build);
	const std::string build_text = build == std::string::npos ? "" : value.substr(build + 1);
	std::size_t prerelease = without_build.find('-');
	const std::string release = without_build.substr(0, prerelease);
	const std::string prerelease_text = prerelease == std::string::npos ? "" :
		without_build.substr(prerelease + 1);
	std::vector<std::string> release_parts;
	std::size_t start = 0;
	while (start <= release.size()) {
		const std::size_t end = release.find('.', start);
		release_parts.push_back(release.substr(start, end - start));
		if (end == std::string::npos) break;
		start = end + 1;
	}
	if (release_parts.size() != 3) return false;
	for (const std::string& part : release_parts) {
		if (part.empty() || (part.size() > 1 && part[0] == '0')) return false;
		for (const unsigned char byte : part)
			if (byte < '0' || byte > '9') return false;
	}
	auto valid_list = [](const std::string& text, bool pre) {
		if (text.empty()) return false;
		std::size_t begin = 0;
		while (begin <= text.size()) {
			const std::size_t end = text.find('.', begin);
			if (!ValidSemverIdentifier(text.substr(begin, end - begin), pre)) return false;
			if (end == std::string::npos) break;
			begin = end + 1;
		}
		return true;
	};
	if (prerelease != std::string::npos && !valid_list(prerelease_text, true)) return false;
	if (build != std::string::npos && !valid_list(build_text, false)) return false;
	return true;
}

bool UriUnreserved(unsigned char byte)
{
	return (byte >= 'a' && byte <= 'z') || (byte >= 'A' && byte <= 'Z') ||
		(byte >= '0' && byte <= '9') || std::strchr("-._~", byte);
}

bool UriSubDelimiter(unsigned char byte)
{
	return std::strchr("!$&'()*+,;=", byte);
}

bool ValidUriComponent(const std::string& value, bool allow_colon_at,
	bool allow_slash_question)
{
	for (std::size_t index = 0; index < value.size(); ++index) {
		const unsigned char byte = static_cast<unsigned char>(value[index]);
		if (UriUnreserved(byte) || UriSubDelimiter(byte) ||
			(allow_colon_at && (byte == ':' || byte == '@')) ||
			(allow_slash_question && (byte == '/' || byte == '?'))) continue;
		if (byte != '%' || index + 2 >= value.size() ||
			!ValidHex(value.substr(index + 1, 2), 2, false)) return false;
		index += 2;
	}
	return true;
}

bool ValidAuthority(const std::string& authority)
{
	if (authority.empty() || authority.find('@') != std::string::npos) return false;
	auto valid_port = [](const std::string& port) {
		for (const unsigned char byte : port)
			if (byte < '0' || byte > '9') return false;
		return true;
	};
	if (authority[0] == '[') {
		const std::size_t right = authority.find(']');
		if (right == std::string::npos || right <= 1 ||
			authority.find('[', 1) != std::string::npos ||
			authority.find(']', right + 1) != std::string::npos ||
			(right + 1 != authority.size() && authority[right + 1] != ':')) return false;
		const std::string address = authority.substr(1, right - 1);
		unsigned char binary[16] = {};
		bool valid_address = inet_pton(AF_INET6, address.c_str(), binary) == 1;
		if (!valid_address && address.size() > 3 && address[0] == 'v') {
			const std::size_t dot = address.find('.');
			valid_address = dot > 1 && dot + 1 < address.size() &&
				ValidHex(address.substr(1, dot - 1), dot - 1, false);
			for (std::size_t index = dot + 1; valid_address && index < address.size(); ++index) {
				const unsigned char byte = static_cast<unsigned char>(address[index]);
				valid_address = UriUnreserved(byte) || UriSubDelimiter(byte) || byte == ':';
			}
		}
		return valid_address && (right + 1 == authority.size() ||
			valid_port(authority.substr(right + 2)));
	}
	if (authority.find('[') != std::string::npos ||
		authority.find(']') != std::string::npos) return false;
	const std::size_t colon = authority.rfind(':');
	if (colon != std::string::npos && authority.find(':') != colon) return false;
	const std::string host = authority.substr(0, colon);
	return ValidUriComponent(host, false, false) &&
		(colon == std::string::npos || valid_port(authority.substr(colon + 1)));
}

bool ValidRepository(const std::string& value)
{
	const std::string scheme = "https://";
	if (value.compare(0, scheme.size(), scheme) != 0) return false;
	const std::size_t authority_end = value.find_first_of("/?#", scheme.size());
	const std::size_t end = authority_end == std::string::npos ? value.size() : authority_end;
	if (!ValidAuthority(value.substr(scheme.size(), end - scheme.size()))) return false;

	const std::size_t fragment = value.find('#', end);
	if (fragment != std::string::npos && value.find('#', fragment + 1) != std::string::npos)
		return false;
	std::size_t query = value.find('?', end);
	if (fragment != std::string::npos && query > fragment) query = std::string::npos;
	const std::size_t path_end = std::min(query == std::string::npos ? value.size() : query,
		fragment == std::string::npos ? value.size() : fragment);
	const std::string path = value.substr(end, path_end - end);
	if ((!path.empty() && path[0] != '/') || !ValidUriComponent(path, true, true))
		return false;
	if (query != std::string::npos) {
		const std::size_t query_end = fragment == std::string::npos ? value.size() : fragment;
		if (!ValidUriComponent(value.substr(query + 1, query_end - query - 1), true, true))
			return false;
	}
	return fragment == std::string::npos ||
		ValidUriComponent(value.substr(fragment + 1), true, true);
}

Error CheckKeys(const toml::table& table,
	std::initializer_list<const char*> required,
	std::initializer_list<const char*> optional = {})
{
	std::set<std::string> allowed;
	for (const char* key : required) allowed.insert(key);
	for (const char* key : optional) allowed.insert(key);
	for (const auto& item : table)
		if (allowed.count(item.first) == 0) return Invalid("unknown manifest field: " + item.first);
	for (const char* key : required)
		if (table.count(key) == 0) return Invalid(std::string("missing manifest field: ") + key);
	return {};
}

const toml::value* Field(const toml::table& table, const char* key)
{
	const auto found = table.find(key);
	return found == table.end() ? nullptr : &found->second;
}

Error TableField(const toml::table& parent, const char* key, const toml::table** output)
{
	const toml::value* value = Field(parent, key);
	if (value == nullptr || !value->is_table())
		return Invalid(std::string("manifest field must be a table: ") + key);
	*output = &value->as_table(std::nothrow);
	return {};
}

Error StringField(const toml::table& table, const char* key, std::string* output)
{
	const toml::value* value = Field(table, key);
	if (value == nullptr || !value->is_string())
		return Invalid(std::string("manifest field must be a string: ") + key);
	*output = value->as_string(std::nothrow).str;
	return {};
}

Error IntegerField(const toml::table& table, const char* key, std::int64_t* output)
{
	const toml::value* value = Field(table, key);
	if (value == nullptr || !value->is_integer())
		return Invalid(std::string("manifest field must be an integer: ") + key);
	*output = value->as_integer(std::nothrow);
	return {};
}

Error ContractFields(const toml::table& table, VersionedContract* contract)
{
	Error error = CheckKeys(table, {"id", "major", "minor"});
	std::int64_t major = 0, minor = 0;
	if (error.ok()) error = StringField(table, "id", &contract->id);
	if (error.ok()) error = IntegerField(table, "major", &major);
	if (error.ok()) error = IntegerField(table, "minor", &minor);
	if (!error.ok()) return error;
	if (!ValidIdentifier(contract->id) || major < 1 || major > 65535 ||
		minor < 0 || minor > 65535) return Invalid("invalid versioned contract");
	contract->major = static_cast<std::uint16_t>(major);
	contract->minor = static_cast<std::uint16_t>(minor);
	return {};
}

Error ParseManifest(const std::string& bytes, CoreDescriptor* descriptor)
{
	try {
		std::istringstream input(bytes);
		const toml::value root_value = toml::parse(input, "manifest.toml");
		if (!root_value.is_table()) return Invalid("manifest root must be a table");
		const toml::table& root = root_value.as_table(std::nothrow);
		Error error = CheckKeys(root,
			{"format", "core", "target", "payload", "abi", "interfaces", "build"});
		std::int64_t format = 0;
		if (error.ok()) error = IntegerField(root, "format", &format);
		if (!error.ok()) return error;
		if (format != 2) return Invalid("unsupported core manifest format");

		CoreDescriptor candidate;
		candidate.format = 2;
		const toml::table *core = nullptr, *target = nullptr, *payload = nullptr;
		const toml::table *abi = nullptr, *build = nullptr;
		if ((error = TableField(root, "core", &core)).ok()) error =
			CheckKeys(*core, {"id", "name", "description", "version"}, {"system"});
		if (error.ok()) error = StringField(*core, "id", &candidate.core.id);
		if (error.ok()) error = StringField(*core, "name", &candidate.core.name);
		if (error.ok()) error = StringField(*core, "description", &candidate.core.description);
		if (error.ok()) error = StringField(*core, "version", &candidate.core.version);
		const bool core_system_present = error.ok() && Field(*core, "system") != nullptr;
		if (error.ok() && core_system_present)
			error = StringField(*core, "system", &candidate.core.system);
		if (!error.ok()) return error;
		if (!ValidIdentifier(candidate.core.id) ||
			!ValidUtf8Text(candidate.core.name, 128, false) ||
			!ValidUtf8Text(candidate.core.description, 2048, true) ||
			!ValidSemver(candidate.core.version) ||
			(core_system_present && !ValidIdentifier(candidate.core.system)))
			return Invalid("invalid core metadata");

		if ((error = TableField(root, "target", &target)).ok()) error =
			CheckKeys(*target, {"platform", "device", "programming_profile"});
		if (error.ok()) error = StringField(*target, "platform", &candidate.target.platform);
		if (error.ok()) error = StringField(*target, "device", &candidate.target.device);
		if (error.ok()) error = StringField(*target, "programming_profile",
			&candidate.target.programming_profile);
		if (!error.ok()) return error;
		if (!ValidIdentifier(candidate.target.platform) ||
			!ValidUtf8Text(candidate.target.device, kMaximumManifestSize, false) ||
			!ValidIdentifier(candidate.target.programming_profile))
			return Invalid("invalid target metadata");

		if ((error = TableField(root, "payload", &payload)).ok()) error =
			CheckKeys(*payload, {"file", "size", "sha256"});
		std::int64_t payload_size = 0;
		if (error.ok()) error = StringField(*payload, "file", &candidate.payload.file);
		if (error.ok()) error = IntegerField(*payload, "size", &payload_size);
		if (error.ok()) error = StringField(*payload, "sha256", &candidate.payload.sha256);
		if (!error.ok()) return error;
		if (candidate.payload.file != "core.rbf" || payload_size < 1 ||
			payload_size > static_cast<std::int64_t>(kMaximumPayloadSize) ||
			!ValidHex(candidate.payload.sha256, 64, true))
			return Invalid("invalid payload metadata");
		candidate.payload.size = static_cast<std::uint64_t>(payload_size);

		if ((error = TableField(root, "abi", &abi)).ok())
			error = ContractFields(*abi, &candidate.abi);
		if (!error.ok()) return error;

		const toml::value* interfaces = Field(root, "interfaces");
		if (interfaces == nullptr || !interfaces->is_array())
			return Invalid("manifest interfaces must be an array");
		std::set<std::string> interface_ids;
		for (const toml::value& value : interfaces->as_array(std::nothrow)) {
			if (!value.is_table()) return Invalid("manifest interface must be a table");
			const toml::table& table = value.as_table(std::nothrow);
			error = CheckKeys(table, {"id", "major", "minor", "required"});
			std::int64_t major = 0, minor = 0;
			CoreInterface interface;
			if (error.ok()) error = StringField(table, "id", &interface.id);
			if (error.ok()) error = IntegerField(table, "major", &major);
			if (error.ok()) error = IntegerField(table, "minor", &minor);
			const toml::value* required = Field(table, "required");
			if (error.ok() && (required == nullptr || !required->is_boolean()))
				error = Invalid("manifest interface required must be a Boolean");
			if (!error.ok()) return error;
			if (!ValidIdentifier(interface.id) || major < 1 || major > 65535 ||
				minor < 0 || minor > 65535 || !interface_ids.insert(interface.id).second)
				return Invalid("invalid or duplicate interface contract");
			interface.major = static_cast<std::uint16_t>(major);
			interface.minor = static_cast<std::uint16_t>(minor);
			interface.required = required->as_boolean(std::nothrow);
			candidate.interfaces.push_back(std::move(interface));
		}

		if ((error = TableField(root, "build", &build)).ok()) error = CheckKeys(*build,
			{"id", "repository", "revision", "recipe_sha256", "toolchain"});
		if (error.ok()) error = StringField(*build, "id", &candidate.build.id);
		if (error.ok()) error = StringField(*build, "repository", &candidate.build.repository);
		if (error.ok()) error = StringField(*build, "revision", &candidate.build.revision);
		if (error.ok()) error = StringField(*build, "recipe_sha256", &candidate.build.recipe_sha256);
		if (error.ok()) error = StringField(*build, "toolchain", &candidate.build.toolchain);
		if (!error.ok()) return error;
		if (!ValidHex(candidate.build.id, 32, true) ||
			!ValidRepository(candidate.build.repository) ||
			!ValidHex(candidate.build.revision, 40, false) ||
			!ValidHex(candidate.build.recipe_sha256, 64, true) ||
			!ValidUtf8Text(candidate.build.toolchain, 1024, false))
			return Invalid("invalid build provenance");
		*descriptor = std::move(candidate);
		return {};
	} catch (const std::exception&) {
		return Invalid("manifest parse failed");
	} catch (...) {
		return Invalid("manifest parse failed");
	}
}

Error CompatibilityError(const char* message)
{
	return Invalid(std::string("incompatible core package: ") + message);
}

} // namespace

Error OpenCorePackage(const std::string& directory,
	const std::string& expected_id, OpenedCorePackage* result)
{
	if (result == nullptr) return Invalid("missing core package output");
	if (directory.empty() || directory.find('\0') != std::string::npos)
		return Invalid("invalid core package directory");
	if (!expected_id.empty() && !ValidHex(expected_id, 64, true))
		return Invalid("invalid expected package identity");
	const int raw_directory = open(directory.c_str(),
		O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC);
	if (raw_directory < 0) return Invalid("cannot open core package directory");
	DirectoryDescriptor directory_descriptor(raw_directory);
	Error error = CheckDirectoryEntries(directory_descriptor.get());
	if (!error.ok()) return error;

	PosixArtifactOpener opener;
	Artifact manifest;
	Artifact payload;
	error = opener.OpenRelative(directory_descriptor.get(), directory,
		"manifest.toml", kMaximumManifestSize, &manifest);
	if (!error.ok()) return Invalid("invalid package manifest file");
	error = opener.OpenRelative(directory_descriptor.get(), directory,
		"core.rbf", kMaximumPayloadSize, &payload);
	if (!error.ok()) return Invalid("invalid package payload file");
	std::string manifest_bytes;
	error = ReadArtifact(manifest, &manifest_bytes);
	if (!error.ok()) return error;
	CoreDescriptor descriptor;
	error = ParseManifest(manifest_bytes, &descriptor);
	if (!error.ok()) return error;
	if (descriptor.payload.size != payload.size())
		return Invalid("payload size does not match manifest");
	Sha256 payload_hash;
	error = HashArtifact(payload, &payload_hash);
	if (!error.ok()) return error;
	const std::string payload_digest = Sha256Hex(payload_hash.Final());
	if (payload_digest != descriptor.payload.sha256)
		return Invalid("payload digest does not match manifest");

	Sha256 package_hash;
	static constexpr char domain[] = "FES-CORE-PACKAGE-2\n";
	package_hash.Update(domain, sizeof(domain) - 1);
	HashLittleEndian64(&package_hash, manifest_bytes.size());
	package_hash.Update(manifest_bytes.data(), manifest_bytes.size());
	HashLittleEndian64(&package_hash, payload.size());
	error = HashArtifact(payload, &package_hash);
	if (!error.ok()) return error;
	const std::string package_id = Sha256Hex(package_hash.Final());
	if (!expected_id.empty() && package_id != expected_id)
		return Invalid("package identity does not match expected identity");
	error = CheckDirectoryEntries(directory_descriptor.get());
	if (!error.ok()) return error;

	OpenedCorePackage opened;
	opened.descriptor = std::move(descriptor);
	opened.manifest_bytes = std::move(manifest_bytes);
	opened.payload = std::move(payload);
	opened.package_id = package_id;
	*result = std::move(opened);
	return {};
}

Error CheckCoreCompatibility(const CoreDescriptor& descriptor)
{
	using namespace generated;
	if (descriptor.target.platform != kDe10NanoProgrammingPlatform)
		return CompatibilityError("unsupported target platform");
	if (descriptor.target.device != kDe10NanoProgrammingDevice)
		return CompatibilityError("unsupported target device");
	bool paired = false;
	for (std::size_t index = 0; index < kDe10NanoProgrammingProfilePairCount; ++index) {
		const GeneratedProgrammingProfilePair& row = kDe10NanoProgrammingProfilePairs[index];
		if (!row.diagnostic_only && descriptor.target.programming_profile == row.profile &&
			row.abi != nullptr && descriptor.abi.id == row.abi &&
			descriptor.abi.major == row.major) {
			paired = true;
			break;
		}
	}
	if (!paired) return CompatibilityError("unsupported profile and ABI pairing");
	if (descriptor.abi.id == "mister" && descriptor.abi.major == 1) {
		if (descriptor.abi.minor != 0)
			return CompatibilityError("MiSTer ABI minor is newer than the tested driver");
		for (const CoreInterface& interface : descriptor.interfaces)
			if (interface.required)
				return CompatibilityError("required MiSTer interface is unsupported");
		return {};
	}
	if (descriptor.abi.id != FesGpABIID || descriptor.abi.major != FesGpABIMajor)
		return CompatibilityError("ABI driver is unavailable");
	if (descriptor.abi.minor > FesGpABIMinor)
		return CompatibilityError("ABI minor is newer than the tested driver");
	if (!descriptor.core.system.empty())
		return CompatibilityError("FES GP packages must omit core.system");
	bool gamepad = false, video = false;
	for (const CoreInterface& interface : descriptor.interfaces) {
		if (interface.id == FesGpInterfaceGamepadID) {
			const bool supported = interface.major == FesGpInterfaceGamepadMajor &&
				interface.minor <= FesGpInterfaceGamepadMinor;
			if (!supported && interface.required)
				return CompatibilityError("required gamepad interface is unsupported");
			gamepad = supported && interface.required;
		} else if (interface.id == FesGpInterfaceVideoFixed720p60ID) {
			const bool supported = interface.major == FesGpInterfaceVideoFixed720p60Major &&
				interface.minor <= FesGpInterfaceVideoFixed720p60Minor;
			if (!supported && interface.required)
				return CompatibilityError("required fixed-video interface is unsupported");
			video = supported && interface.required;
		} else if (interface.required) {
			return CompatibilityError("required interface is unsupported");
		}
	}
	if (!gamepad || !video)
		return CompatibilityError("required FES GP interfaces are missing");
	return {};
}

} // namespace native
} // namespace mister
