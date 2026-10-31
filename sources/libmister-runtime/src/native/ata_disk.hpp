// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
#include "native/media_data.hpp"
#include <memory>
#include <string>
#include <vector>
namespace mister { namespace native {
// fes.ata-disk.delta 1.0 (mister-packages docs/media-data.md, "ATA disk
// delta"): the 64 KiB chunks a guest changed over an immutable base image.
struct AtaDeltaChunk {
    std::uint32_t index = 0;
    std::string sha256;
};
struct AtaDiskDelta {
    MediaDataIdentity identity;
    std::uint32_t base_size = 0;
    std::vector<AtaDeltaChunk> chunks;  // strictly ascending index
    std::string revision = "absent";
};
// 1 MiB to 128 MiB in whole 512-byte sectors.
inline bool ValidAtaDiskSize(std::uint64_t bytes)
{
    return bytes % generated::FesComputerAtaDiskSectorBytes == 0 &&
        bytes >= generated::FesComputerAtaDiskMinBytes && bytes <= generated::FesComputerAtaDiskMaxBytes;
}
inline std::uint32_t AtaDiskChunkCount(std::uint32_t base_size)
{
    return static_cast<std::uint32_t>((std::uint64_t(base_size) + generated::FesComputerAtaDiskChunkBytes - 1) /
        generated::FesComputerAtaDiskChunkBytes);
}
// The last chunk of an image that is not a whole number of chunks is short.
inline std::uint32_t AtaDiskChunkLength(std::uint32_t base_size, std::uint32_t index)
{
    const std::uint64_t start = std::uint64_t(index) * generated::FesComputerAtaDiskChunkBytes;
    if (start >= base_size) return 0;
    const std::uint64_t left = base_size - start;
    return static_cast<std::uint32_t>(left < generated::FesComputerAtaDiskChunkBytes ? left :
        generated::FesComputerAtaDiskChunkBytes);
}
// A geometry DiskCommit accepts for an image of size bytes.
inline bool ValidAtaDiskGeometry(std::uint64_t size, std::uint32_t cylinders, std::uint32_t heads,
    std::uint32_t sectors)
{
    return ValidAtaDiskSize(size) && cylinders >= 1 && cylinders <= generated::FesComputerAtaDiskMaxCylinders &&
        heads >= 1 && heads <= generated::FesComputerAtaDiskMaxHeads && sectors >= 1 &&
        sectors <= generated::FesComputerAtaDiskMaxSectorsPerTrack &&
        std::uint64_t(cylinders) * heads * sectors <= size / generated::FesComputerAtaDiskSectorBytes;
}
// The staged base image, read with no-follow open and pread.
class AtaBaseImage {
public:
    ~AtaBaseImage();
    static Error Open(const std::string& path, std::uint32_t size, std::unique_ptr<AtaBaseImage>*);
    Error Read(std::uint64_t offset, unsigned char* bytes, std::size_t size) const;
    AtaBaseImage(const AtaBaseImage&) = delete;
    AtaBaseImage& operator=(const AtaBaseImage&) = delete;
private:
    explicit AtaBaseImage(int fd) : fd_(fd) {}
    int fd_;
};
Error EncodeAtaDelta(const AtaDiskDelta&, std::vector<unsigned char>*);
Error DecodeAtaDelta(const std::vector<unsigned char>&, const MediaDataIdentity&,
    std::uint32_t base_size, AtaDiskDelta*);
// The existing manifest with each changed chunk added or replaced.
AtaDiskDelta MergeAtaDelta(const AtaDiskDelta& existing, const std::vector<AtaDeltaChunk>& changed);

// record.bin and chunk-<sha256> files in the media-data namespace. Open
// takes the namespace lock and removes leftover private temporaries. A
// missing record reads as an empty manifest with revision "absent".
class AtaDeltaFile {
public:
    ~AtaDeltaFile();
    static Error Open(const std::string& root, const MediaDataIdentity&, std::uint32_t base_size,
        std::unique_ptr<AtaDeltaFile>*);
    Error Read(AtaDiskDelta*) const;
    Error CheckWritable() const;
    // A named chunk's bytes; a missing file or a length or hash that differs
    // from the manifest is corruption.
    Error ReadChunk(const AtaDeltaChunk&, std::vector<unsigned char>*) const;
    // Writes and syncs one chunk file under its content name.
    Error WriteChunk(const unsigned char* bytes, std::size_t size, std::string* sha256);
    // Under the lock: compares the revision, syncs the directory so every
    // chunk file is durable, publishes the manifest by rename, syncs again,
    // then deletes chunk files the published manifest does not name.
    Error Publish(const AtaDiskDelta&, const std::string& expected_revision, AtaDiskDelta*);
    AtaDeltaFile(const AtaDeltaFile&) = delete;
    AtaDeltaFile& operator=(const AtaDeltaFile&) = delete;
private:
    AtaDeltaFile(int directory, MediaDataIdentity, std::uint32_t base_size);
    Error ReadLocked(AtaDiskDelta*) const;
    Error CollectLocked(const AtaDiskDelta&);
    int directory_;
    MediaDataIdentity identity_;
    std::uint32_t base_size_;
};
// A disk bound to a launch, between preparation and load.
struct PreparedAtaDisk {
    AtaDiskImage image;
    MediaDataIdentity identity;
    std::unique_ptr<AtaBaseImage> base;
    std::unique_ptr<AtaDeltaFile> file;
};
} }
