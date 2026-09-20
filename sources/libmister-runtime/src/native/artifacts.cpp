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

namespace {
Error SaveError(const char* action)
{
	return {ErrorCode::save_failed, std::string(action) + ": " + std::strerror(errno)};
}
}

SaveFile::~SaveFile() { if (directory_ >= 0) close(directory_); }

int SaveFile::Temporary(std::string* name)
{
	static std::atomic<unsigned long> sequence{0};
	for (unsigned attempt = 0; attempt < 64; ++attempt) {
		*name = ".mister-save-" + std::to_string(getpid()) + "-" + std::to_string(++sequence);
		const int fd = openat(directory_, name->c_str(), O_WRONLY | O_CREAT | O_EXCL | O_CLOEXEC, 0600);
		if (fd >= 0 || errno != EEXIST) return fd;
	}
	return -1;
}

Error SaveFile::Prepare(const std::string& path, std::size_t size)
{
	if (directory_ >= 0 || path.empty() || path[0] != '/' || path.find('\0') != std::string::npos ||
		size < 2048 || size > 131072 || (size & (size - 1)))
		return {ErrorCode::save_failed, "invalid save file admission"};
	const std::size_t slash = path.find_last_of('/');
	name_ = path.substr(slash + 1);
	if (name_.empty() || name_ == "." || name_ == "..")
		return {ErrorCode::save_failed, "invalid save filename"};
	directory_ = open((slash ? path.substr(0, slash) : "/").c_str(), O_RDONLY | O_DIRECTORY | O_CLOEXEC);
	if (directory_ < 0) return SaveError("open save directory");
	const int fd = openat(directory_, name_.c_str(), O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC);
	if (fd < 0 && errno != ENOENT) return SaveError("open save file");
	if (fd >= 0) {
		struct stat st = {};
		if (fstat(fd, &st) != 0 || !S_ISREG(st.st_mode) || st.st_size != static_cast<off_t>(size)) {
			close(fd);
			return {ErrorCode::save_failed, "save file must be regular and exactly match cartridge RAM size"};
		}
		std::vector<unsigned char> content(size);
		std::size_t offset = 0;
		while (offset < size) {
			const ssize_t count = pread(fd, content.data() + offset, size - offset, offset);
			if (count < 0 && errno == EINTR) continue;
			if (count <= 0) { close(fd); return {ErrorCode::save_failed, "incomplete save file read"}; }
			offset += static_cast<std::size_t>(count);
		}
		close(fd);
		bytes_ = std::move(content);
	}
	std::string temporary;
	const int probe = Temporary(&temporary);
	if (probe < 0) return SaveError("create save temporary");
	const int closed = close(probe);
	const int removed = unlinkat(directory_, temporary.c_str(), 0);
	if (closed != 0 || removed != 0) return SaveError("remove save temporary");
	size_ = size;
	return {};
}

Error SaveFile::Persist(const std::vector<unsigned char>& bytes)
{
	if (directory_ < 0 || size_ == 0 || bytes.size() != size_)
		return {ErrorCode::save_failed, "snapshot does not match admitted save"};
	std::string temporary;
	const int fd = Temporary(&temporary);
	if (fd < 0) return SaveError("create save temporary");
	Error error;
	std::size_t offset = 0;
	while (offset < bytes.size()) {
		const ssize_t count = write(fd, bytes.data() + offset, bytes.size() - offset);
		if (count < 0 && errno == EINTR) continue;
		if (count <= 0) { error = SaveError("write save temporary"); break; }
		offset += static_cast<std::size_t>(count);
	}
	if (error.ok() && fsync(fd) != 0) error = SaveError("sync save temporary");
	if (close(fd) != 0 && error.ok()) error = SaveError("close save temporary");
	if (error.ok() && renameat(directory_, temporary.c_str(), directory_, name_.c_str()) != 0)
		error = SaveError("replace save file");
	if (!error.ok()) unlinkat(directory_, temporary.c_str(), 0);
	else if (fsync(directory_) != 0) error = SaveError("sync save directory");
	return error;
}

// Metadata contract: SNES_MiSTer 93d359e6 uses a synthesized 512-byte prefix;
// NES_MiSTer 9a638211 receives an unchanged iNES/NES2 file. Basic cartridges
// only: bounded retained-file reads, no mirroring or special chips.
Error PrepareMediaContent(const Artifact& artifact, MediaTransform transform,
	MediaContentPlan* output)
{
	if (!output) return {ErrorCode::invalid_request, "missing content plan"};
	MediaContentPlan plan;
	plan.source_size = artifact.size();
	if (transform == MediaTransform::raw) { *output = plan; return {}; }
	if (transform == MediaTransform::nes_cartridge) {
		auto invalid = [] {
			return Error{ErrorCode::invalid_request,
				"unsupported or malformed NES cartridge"};
		};
		constexpr std::uint64_t kMaximumSize = 32u * 1024u * 1024u;
		if (plan.source_size < 16 || plan.source_size > kMaximumSize)
			return invalid();
		unsigned char header[16] = {};
		if (pread(artifact.fd(), header, sizeof(header), 0) !=
			static_cast<ssize_t>(sizeof(header)))
			return {ErrorCode::io_failed, "NES header read failed"};
		if (std::memcmp(header, "NES\x1a", 4) != 0 || (header[6] & 0x04u) != 0)
			return invalid();

		auto decode_exponent_size = [&](unsigned encoded, unsigned unit,
			std::uint64_t* size) {
			const unsigned exponent = encoded >> 2;
			const unsigned multiplier = ((encoded & 3u) * 2u) + 1u;
			if (exponent >= 63 || multiplier >
				std::numeric_limits<std::uint64_t>::max() >> exponent)
				return false;
			const std::uint64_t bytes = static_cast<std::uint64_t>(multiplier) << exponent;
			if (unit != 0 && bytes > std::numeric_limits<std::uint64_t>::max() / unit)
				return false;
			*size = bytes * unit;
			return true;
		};
		auto decode_linear_size = [](unsigned low, unsigned high, unsigned unit,
			std::uint64_t* size) {
			const std::uint64_t pages = static_cast<std::uint64_t>(low) |
				(static_cast<std::uint64_t>(high) << 8);
			if (unit != 0 && pages > std::numeric_limits<std::uint64_t>::max() / unit)
				return false;
			*size = pages * unit;
			return true;
		};

		std::uint64_t prg_size = 0;
		std::uint64_t chr_size = 0;
		const bool nes2 = (header[7] & 0x0cu) == 0x08u;
		if (nes2) {
			const unsigned prg_high = header[9] & 0x0fu;
			const unsigned chr_high = (header[9] >> 4) & 0x0fu;
			if ((prg_high == 0x0fu &&
				!decode_exponent_size(header[4], 1, &prg_size)) ||
				(prg_high != 0x0fu &&
				!decode_linear_size(header[4], prg_high, 16384, &prg_size)) ||
				(chr_high == 0x0fu &&
				!decode_exponent_size(header[5], 1, &chr_size)) ||
				(chr_high != 0x0fu &&
				!decode_linear_size(header[5], chr_high, 8192, &chr_size)))
				return invalid();
		} else {
			if (header[4] == 0 ||
				!decode_linear_size(header[4], 0, 16384, &prg_size) ||
				!decode_linear_size(header[5], 0, 8192, &chr_size))
				return invalid();
		}
		if (prg_size == 0 || prg_size > kMaximumSize || chr_size > kMaximumSize ||
			prg_size > kMaximumSize - chr_size ||
			16u > kMaximumSize - prg_size - chr_size)
			return invalid();
		const std::uint64_t declared = 16u + prg_size + chr_size;
		if (declared > plan.source_size)
			return invalid();
		*output = plan;
		return {};
	}
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
		plan.battery_ram_size = header[0x16] == 2 && header[0x18] ? (1024u << header[0x18]) : 0;
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
	if (!launch.save_path.empty()) {
		if (launch.system != "snes") return {ErrorCode::invalid_request, "save_path is SNES-only"};
		for (const OpenedMedia& media : candidate.media) {
			if (!media.content.battery_ram_size) continue;
			if (launch.save_path == media.artifact.path() || launch.save_path == launch.rbf)
				return {ErrorCode::save_failed, "save path must not replace launch media"};
			candidate.save.reset(new SaveFile);
			error = candidate.save->Prepare(launch.save_path, media.content.battery_ram_size);
			if (!error.ok()) return error;
		}
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
