// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"

#include <cstdint>
#include <array>
#include <string>
#include <memory>
#include <vector>

namespace mister {
namespace native {

class Artifact {
public:
	Artifact();
	~Artifact();
	Artifact(Artifact&&) noexcept;
	Artifact& operator=(Artifact&&) noexcept;
	Artifact(const Artifact&) = delete;
	Artifact& operator=(const Artifact&) = delete;
	int fd() const;
	std::uint64_t size() const;
	const std::string& path() const;

private:
	friend class PosixArtifactOpener;
	int fd_ = -1;
	std::uint64_t size_ = 0;
	std::string path_;
};

class Clock;

// Private unlinked file: a stable, bounded-memory snapshot for mailbox upload.
class ComputerMediaSnapshot {
public:
	ComputerMediaSnapshot() = default;
	~ComputerMediaSnapshot();
	ComputerMediaSnapshot(const ComputerMediaSnapshot&) = delete;
	ComputerMediaSnapshot& operator=(const ComputerMediaSnapshot&) = delete;
	Error Prepare(const std::string& path, std::uint32_t minimum,
		std::uint32_t maximum, Clock&, std::uint64_t deadline);
	Error Read(std::uint32_t offset, std::uint8_t* data, std::size_t length,
		Clock&, std::uint64_t deadline) const;
	std::uint32_t size() const { return size_; }
	std::uint32_t crc32() const { return crc32_; }
private:
	int fd_ = -1;
	std::uint32_t size_ = 0, crc32_ = 0;
};

// A retained directory and original snapshot; final bytes are replaced atomically.

class ArtifactOpener {
public:
	virtual ~ArtifactOpener() {}
	virtual Error Open(const std::string& path, std::uint64_t maximum_size,
		Artifact* artifact) = 0;
};

class PosixArtifactOpener final : public ArtifactOpener {
public:
	Error Open(const std::string&, std::uint64_t, Artifact*) override;
	Error OpenRelative(int directory_fd, const std::string& directory_path,
		const std::string& name, std::uint64_t maximum_size, Artifact*);
private:
	Error ValidateAndAdopt(int descriptor, const std::string& path,
		std::uint64_t maximum_size, Artifact*);
};

Error OpenRBFArtifact(const std::string&, ArtifactOpener&, Artifact*);
Error ReadComputerMedia(const std::string&, std::vector<std::uint8_t>*);

} // namespace native
} // namespace mister
