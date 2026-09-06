// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"

#include <cstdint>
#include <array>
#include <string>
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

struct MediaContentPlan {
	std::uint64_t source_offset = 0, source_size = 0;
	std::size_t prefix_size = 0;
	std::array<unsigned char, 512> prefix = {};
};

Error PrepareMediaContent(const Artifact&, MediaTransform, MediaContentPlan*);

struct OpenedMedia {
	std::uint8_t index = 0;
	Artifact artifact;
	MediaContentPlan content;
};

struct ArtifactSet {
	Artifact rbf;
	std::vector<OpenedMedia> media;
};

class ArtifactOpener {
public:
	virtual ~ArtifactOpener() {}
	virtual Error Open(const std::string& path, std::uint64_t maximum_size,
		Artifact* artifact) = 0;
};

class PosixArtifactOpener final : public ArtifactOpener {
public:
	Error Open(const std::string&, std::uint64_t, Artifact*) override;
};

Error OpenLaunchArtifacts(const PreparedLaunch&, ArtifactOpener&, ArtifactSet*);
Error OpenRBFArtifact(const std::string&, ArtifactOpener&, Artifact*);

} // namespace native
} // namespace mister
