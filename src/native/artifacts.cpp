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
