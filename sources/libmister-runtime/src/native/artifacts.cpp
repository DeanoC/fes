// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/artifacts.hpp"
#include "native/diagnostic.hpp"
#include "native/hardware.hpp"
#include "native/generated/fes_simple_computer.hpp"

#include <fcntl.h>
#include <sys/stat.h>
#include <unistd.h>

#include <cerrno>
#include <algorithm>
#include <cstdlib>
#include <atomic>
#include <cstdio>
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

ComputerMediaSnapshot::~ComputerMediaSnapshot()
{
	if (fd_ >= 0) close(fd_);
}

Error ComputerMediaSnapshot::Prepare(const std::string& path,
	std::uint32_t minimum, std::uint32_t maximum, Clock& clock, std::uint64_t deadline)
{
	if (fd_ >= 0 || minimum != generated::FesSimpleComputerMediaStreamMinBytes ||
		maximum < generated::FesSimpleComputerMediaStreamGuaranteedMaxBytes ||
		maximum > generated::FesSimpleComputerMediaStreamMaxBytes ||
		path.empty() || path[0] != '/' || path.find('\0') != std::string::npos)
		return {ErrorCode::invalid_request, "invalid stream media admission", "request"};
	const auto slash = path.find_last_of('/');
	const std::string parent = slash == 0 ? "/" : path.substr(0, slash);
	const int directory = open(parent.c_str(), O_RDONLY | O_DIRECTORY | O_CLOEXEC);
	if (directory < 0) return IoError("open media directory failed", parent);
	Artifact source;
	PosixArtifactOpener opener;
	Error error = opener.OpenRelative(directory, parent, path.substr(slash + 1), maximum, &source);
	close(directory);
	if (!error.ok()) return error;
	if (source.size() < minimum)
		return {ErrorCode::invalid_request, "stream media is below endpoint minimum", "request"};
	char name[] = "/tmp/mister-runtime-media-XXXXXX";
	int snapshot = mkstemp(name);
	if (snapshot < 0) return IoError("create media snapshot failed", "");
	if (unlink(name) != 0) {
		error = IoError("unlink media snapshot failed", "");
		close(snapshot);
		return error;
	}
	auto fail = [&](Error cause) {
		close(snapshot);
		return cause;
	};
	if (fcntl(snapshot, F_SETFD, FD_CLOEXEC) < 0)
		return fail(IoError("set media snapshot close-on-exec failed", ""));
	std::array<std::uint8_t, generated::FesSimpleComputerMediaStreamChunkMaxBytes> buffer = {};
	std::uint32_t total = 0, crc = generated::FesSimpleComputerMediaStreamCRC32Initial;
	while (true) {
		if (clock.NowMs() >= deadline)
			return fail({ErrorCode::io_failed, "media snapshot deadline exceeded", "request"});
		const std::size_t wanted = static_cast<std::size_t>(
			std::min<std::uint64_t>(buffer.size(), source.size() - total + 1));
		ssize_t count = read(source.fd(), buffer.data(), wanted);
		if (count < 0) {
			if (errno == EINTR) continue;
			return fail(IoError("read stream media failed", path));
		}
		if (count == 0) break;
		if (static_cast<std::uint64_t>(total) + count > source.size())
			return fail({ErrorCode::invalid_request, "stream media grew while reading", "request"});
		for (ssize_t i = 0; i < count; ++i) {
			crc ^= buffer[static_cast<std::size_t>(i)];
			for (unsigned bit = 0; bit < 8; ++bit)
				crc = (crc >> 1) ^ ((crc & 1u) ? generated::FesSimpleComputerMediaStreamCRC32Polynomial : 0u);
		}
		std::size_t written = 0;
		while (written < static_cast<std::size_t>(count)) {
			if (clock.NowMs() >= deadline)
				return fail({ErrorCode::io_failed, "media snapshot deadline exceeded", "request"});
			ssize_t n = write(snapshot, buffer.data() + written,
				static_cast<std::size_t>(count) - written);
			if (n < 0 && errno == EINTR) continue;
			if (n <= 0) return fail(IoError("write media snapshot failed", ""));
			written += static_cast<std::size_t>(n);
		}
		total += static_cast<std::uint32_t>(count);
	}
	if (total != source.size())
		return fail({ErrorCode::invalid_request, "stream media truncated while reading", "request"});
	if (clock.NowMs() >= deadline)
		return fail({ErrorCode::io_failed, "media snapshot deadline exceeded", "request"});
	fd_ = snapshot;
	size_ = total;
	crc32_ = crc ^ generated::FesSimpleComputerMediaStreamCRC32FinalXor;
	return {};
}

Error ComputerMediaSnapshot::Read(std::uint32_t offset, std::uint8_t* data,
	std::size_t length, Clock& clock, std::uint64_t deadline) const
{
	if (fd_ < 0 || data == nullptr || offset > size_ || length > size_ - offset)
		return {ErrorCode::invalid_request, "invalid media snapshot window", "request"};
	std::size_t done = 0;
	while (done < length) {
		if (clock.NowMs() >= deadline)
			return {ErrorCode::io_failed, "media snapshot read deadline exceeded", "input"};
		const ssize_t n = pread(fd_, data + done, length - done,
			static_cast<off_t>(offset) + static_cast<off_t>(done));
		if (n < 0 && errno == EINTR) continue;
		if (n <= 0) return {ErrorCode::io_failed, "read media snapshot failed", "input"};
		done += static_cast<std::size_t>(n);
	}
	if (clock.NowMs() >= deadline)
		return {ErrorCode::io_failed, "media snapshot read deadline exceeded", "input"};
	return {};
}

Error ReadComputerMedia(const std::string& path, std::vector<std::uint8_t>* output)
{
	if (!output || path.empty() || path[0] != '/' ||
		path.find('\0') != std::string::npos)
		return {ErrorCode::invalid_request, "computer media path is invalid", "request"};
	const auto slash = path.find_last_of('/');
	const std::string parent = slash == 0 ? "/" : path.substr(0, slash);
	const int directory = open(parent.c_str(), O_RDONLY | O_DIRECTORY | O_CLOEXEC);
	if (directory < 0) return IoError("open media directory failed", parent);
	Artifact artifact;
	PosixArtifactOpener opener;
	const Error admitted = opener.OpenRelative(directory, parent, path.substr(slash + 1),
		generated::FesSimpleComputerMediaMaxBytes, &artifact);
	close(directory);
	if (!admitted.ok()) return admitted;
	// Read at most max+1, even if a concurrent writer grows the retained file.
	std::vector<std::uint8_t> bytes(generated::FesSimpleComputerMediaMaxBytes + 1);
	std::size_t offset = 0;
	while (offset < bytes.size()) {
		const ssize_t count = read(artifact.fd(), bytes.data() + offset, bytes.size() - offset);
		if (count < 0) {
			if (errno == EINTR) continue;
			return IoError("read computer media failed", path);
		}
		if (count == 0) break;
		offset += static_cast<std::size_t>(count);
	}
	if (offset != artifact.size())
		return {ErrorCode::invalid_request, "computer media size changed while reading", "request"};
	bytes.resize(offset);
	*output = std::move(bytes);
	return {};
}

Error PosixArtifactOpener::Open(const std::string& path,
	std::uint64_t maximum_size, Artifact* output)
{
	if (output == nullptr) return {ErrorCode::io_failed, "missing artifact output"};
	if (path.find('\0') != std::string::npos)
		return {ErrorCode::io_failed, "artifact path contains a NUL byte"};
	errno = 0;
	const int descriptor = open(path.c_str(), O_RDONLY | O_CLOEXEC);
	if (descriptor < 0) {
		EmitCapFdOpen(false, path);
		return IoError("open failed", path);
	}
	const Error error = ValidateAndAdopt(descriptor, path, maximum_size, output);
	EmitCapFdOpen(error.ok(), path);
	return error;
}

Error PosixArtifactOpener::OpenRelative(int directory_fd,
	const std::string& directory_path, const std::string& name,
	std::uint64_t maximum_size, Artifact* output)
{
	if (output == nullptr) return {ErrorCode::io_failed, "missing artifact output"};
	if (directory_fd < 0 || name.empty() || name == "." || name == ".." ||
		name.find('/') != std::string::npos || name.find('\0') != std::string::npos)
		return {ErrorCode::io_failed, "invalid directory-relative artifact name"};
	errno = 0;
	const int descriptor = openat(directory_fd, name.c_str(),
		O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC);
	const std::string path = directory_path + "/" + name;
	if (descriptor < 0) {
		EmitCapFdOpen(false, path);
		return IoError("open failed", path);
	}
	const Error error = ValidateAndAdopt(descriptor, path, maximum_size, output);
	EmitCapFdOpen(error.ok(), path);
	return error;
}

Error PosixArtifactOpener::ValidateAndAdopt(int descriptor,
	const std::string& path, std::uint64_t maximum_size, Artifact* output)
{
	struct stat metadata = {};
	if (fstat(descriptor, &metadata) != 0) {
		const Error error = IoError("stat failed", path);
		close(descriptor);
		return error;
	}
	if (!S_ISREG(metadata.st_mode) || metadata.st_size <= 0) {
		close(descriptor);
		return {ErrorCode::io_failed,
			"artifact is not a non-empty regular file: " + path};
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

Error OpenRBFArtifact(const std::string& path, ArtifactOpener& opener,
	Artifact* output)
{
	return opener.Open(path, 32u * 1024u * 1024u, output);
}

} // namespace native
} // namespace mister
