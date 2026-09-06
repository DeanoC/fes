// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/artifacts.hpp"

#include <fcntl.h>
#include <sys/stat.h>
#include <unistd.h>

#include <cerrno>
#include <cstring>
#include <limits>
#include <utility>

namespace mister {
namespace native {
namespace {

Error IoError(const char* action, const std::string& path)
{
	std::string message(action);
	message += ": ";
	message += path;
	if (errno != 0) {
		message += ": ";
		message += std::strerror(errno);
	}
	return {ErrorCode::io_failed, message};
}

} // namespace

Artifact::Artifact() = default;

Artifact::~Artifact()
{
	if (fd_ >= 0) close(fd_);
}

Artifact::Artifact(Artifact&& other) noexcept
	: fd_(other.fd_), size_(other.size_), path_(std::move(other.path_))
{
	other.fd_ = -1;
	other.size_ = 0;
}

Artifact& Artifact::operator=(Artifact&& other) noexcept
{
	if (this == &other) return *this;
	if (fd_ >= 0) close(fd_);
	fd_ = other.fd_;
	size_ = other.size_;
	path_ = std::move(other.path_);
	other.fd_ = -1;
	other.size_ = 0;
	return *this;
}

int Artifact::fd() const { return fd_; }
std::uint64_t Artifact::size() const { return size_; }
const std::string& Artifact::path() const { return path_; }

Error PosixArtifactOpener::Open(const std::string& path,
	std::uint64_t maximum_size, Artifact* output)
{
	if (output == nullptr) return {ErrorCode::io_failed, "missing artifact output"};
	if (path.find('\0') != std::string::npos)
		return {ErrorCode::io_failed, "artifact path contains a NUL byte"};
	errno = 0;
	const int descriptor = open(path.c_str(), O_RDONLY | O_CLOEXEC);
	if (descriptor < 0) return IoError("open failed", path);
	struct stat metadata = {};
	if (fstat(descriptor, &metadata) != 0) {
		const Error error = IoError("stat failed", path);
		close(descriptor);
		return error;
	}
	if (!S_ISREG(metadata.st_mode) || metadata.st_size <= 0) {
		close(descriptor);
		return {ErrorCode::io_failed, "artifact is not a non-empty regular file: " + path};
	}
	if (static_cast<std::uintmax_t>(metadata.st_size) >
		static_cast<std::uintmax_t>(std::numeric_limits<std::uint64_t>::max())) {
		close(descriptor);
		return {ErrorCode::io_failed, "artifact size is not representable: " + path};
	}
	const std::uint64_t size = static_cast<std::uint64_t>(metadata.st_size);
	if (maximum_size != 0 && size > maximum_size) {
		close(descriptor);
		return {ErrorCode::io_failed, "artifact exceeds size limit: " + path};
	}
	unsigned char probe = 0;
	if (pread(descriptor, &probe, 1, 0) != 1) {
		const Error error = IoError("artifact is not readable", path);
		close(descriptor);
		return error;
	}
	Artifact candidate;
	candidate.fd_ = descriptor;
	candidate.size_ = size;
	candidate.path_ = path;
	*output = std::move(candidate);
	return {};
}

// Metadata contract: Main_MiSTer 915ca339 support/snes/snes.cpp and
// SNES_MiSTer 93d359e6 SNES.sv (512-byte prefix, cartridge index 1).
// Basic cartridges only: bounded retained-file reads, no mirroring or special chips.
Error PrepareMediaContent(const Artifact& artifact, MediaTransform transform,
	MediaContentPlan* output)
{
	if (!output) return {ErrorCode::invalid_request, "missing content plan"};
	MediaContentPlan plan;
	plan.source_size = artifact.size();
	if (transform == MediaTransform::raw) { *output = plan; return {}; }
	if (transform != MediaTransform::snes_cartridge)
		return {ErrorCode::invalid_request, "unknown media transform"};
	auto invalid = [] { return Error{ErrorCode::invalid_request, "unsupported or malformed SNES cartridge"}; };
	auto size_ok = [](std::uint64_t size) {
		return size >= 32768 && size <= 4u * 1024u * 1024u && (size & (size - 1)) == 0;
	};
	if (!size_ok(plan.source_size)) {
		if (plan.source_size < 512 || !size_ok(plan.source_size - 512)) return invalid();
		plan.source_offset = 512;
		plan.source_size -= 512;
	}
	auto read = [&](std::uint64_t offset, unsigned char* data, std::size_t count) {
		return offset <= plan.source_size && count <= plan.source_size - offset &&
			pread(artifact.fd(), data, count, plan.source_offset + offset) == static_cast<ssize_t>(count);
	};
	unsigned char first[16] = {};
	if (!read(0, first, sizeof(first))) return {ErrorCode::io_failed, "SNES header read failed"};
	if (std::memcmp(first, "BANDAI SFC-ADX", 14) == 0) return invalid();
	unsigned exponent = 0;
	for (std::uint64_t size = plan.source_size / 1024; size > 1; size >>= 1) ++exponent;
	unsigned candidates = 0;
	for (std::uint32_t address : {0x7fc0u, 0xffc0u}) {
		if (address + 64 > plan.source_size) continue;
		unsigned char bytes[80] = {};
		if (!read(address - 16, bytes, sizeof(bytes))) return {ErrorCode::io_failed, "SNES header read failed"};
		const unsigned char* header = bytes + 16;
		const bool hi = address == 0xffc0;
		const unsigned mapper = header[0x15];
		const unsigned reset = header[0x3c] | (header[0x3d] << 8);
		const unsigned check = header[0x1e] | (header[0x1f] << 8);
		const unsigned complement = header[0x1c] | (header[0x1d] << 8);
		if (mapper != (hi ? 0x21u : 0x20u) && mapper != (hi ? 0x31u : 0x30u)) continue;
		if (reset < 0x8000 || check == 0 || complement == 0 || (check ^ complement) != 0xffff) continue;
		// Do not silently reinterpret a valid-looking unsupported header as another mapping.
		if (header[0x16] > 2 || header[0x17] != exponent || header[0x18] > 7 ||
			(header[0x16] == 0 && header[0x18] != 0) || header[0x19] > 0x14 ||
			std::memcmp(header, "Satellaview BS-X", 15) == 0 ||
			(bytes[2] == 'Z' && bytes[5] == 'J' &&
			 ((bytes[3] >= 'A' && bytes[3] <= 'Z') || (bytes[3] >= '0' && bytes[3] <= '9')) &&
			 (header[0x1a] == 0x33 || (bytes[6] == 0 && bytes[12] == 0)))) return invalid();
		unsigned char opcode = 0;
		if (!read((address & ~0x7fffu) | (reset & 0x7fffu), &opcode, 1))
			return {ErrorCode::io_failed, "SNES reset opcode read failed"};
		if (opcode == 0 || opcode == 2 || opcode == 0x42 || opcode == 0xdb || opcode == 0xff) continue;
		++candidates;
		plan.prefix[0] = static_cast<unsigned char>((header[0x18] << 4) | exponent);
		plan.prefix[1] = hi ? 1 : 0;
		plan.prefix[3] = ((header[0x19] >= 2 && header[0x19] <= 12) || header[0x19] == 0x11) ? 1 : 0;
		for (unsigned byte = 0; byte < 4; ++byte) {
			plan.prefix[4 + byte] = static_cast<unsigned char>(address >> (8 * byte));
			plan.prefix[8 + byte] = static_cast<unsigned char>(plan.source_size >> (8 * byte));
		}
	}
	if (candidates != 1) return invalid();
	plan.prefix_size = 512;
	*output = plan;
	return {};
}

Error OpenLaunchArtifacts(const PreparedLaunch& launch, ArtifactOpener& opener,
	ArtifactSet* output)
{
	if (output == nullptr) return {ErrorCode::io_failed, "missing artifact-set output"};
	ArtifactSet candidate;
	Error error = OpenRBFArtifact(launch.rbf, opener, &candidate.rbf);
	if (!error.ok()) return error;
	for (const PreparedMedia& media : launch.media) {
		OpenedMedia opened;
		opened.index = media.index;
		error = opener.Open(media.path, media.maximum_size, &opened.artifact);
		if (!error.ok()) return error;
		error = PrepareMediaContent(opened.artifact, media.transform, &opened.content);
		if (!error.ok()) return error;
		candidate.media.push_back(std::move(opened));
	}
	*output = std::move(candidate);
	return {};
}

Error OpenRBFArtifact(const std::string& path, ArtifactOpener& opener,
	Artifact* output)
{
	return opener.Open(path, 32u * 1024u * 1024u, output);
}

} // namespace native
} // namespace mister
