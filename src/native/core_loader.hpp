// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"

#include <cstddef>
#include <cstdint>
#include <string>

namespace mister {
namespace native {

class Artifact;
struct OpenedMedia;
struct MediaContentPlan;
class Spi;

class ArtifactReader {
public:
	virtual ~ArtifactReader() {}
	virtual Error Read(const Artifact&, std::uint64_t offset,
		unsigned char* bytes, std::size_t count) = 0;
};

class CoreLoader {
public:
	explicit CoreLoader(Spi&);
	CoreLoader(Spi&, ArtifactReader&);
	Error Synchronize(std::uint64_t absolute_deadline_ms);
	Error AssertReset(const CoreRecipe&, std::uint64_t absolute_deadline_ms);
	Error Probe(std::string* observed_core,
		std::uint64_t absolute_deadline_ms);
	Error ApplyInitialStatus(const CoreRecipe&,
		std::uint64_t absolute_deadline_ms);
	Error Attach(const OpenedMedia&, FileWireFormat, std::uint64_t absolute_deadline_ms);
	Error Attach(std::uint8_t index, const Artifact&, FileWireFormat,
		std::uint64_t absolute_deadline_ms);
	Error ReleaseReset(const CoreRecipe&, std::uint64_t absolute_deadline_ms);

private:
	Error AttachContent(std::uint8_t, const Artifact&, FileWireFormat, const MediaContentPlan&, std::uint64_t);
	Spi& spi_;
	ArtifactReader* reader_;
};

} // namespace native
} // namespace mister
