// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
#include <memory>
namespace mister { namespace native {
constexpr std::size_t kAtariStDiskBytes = 737280;
constexpr std::size_t kMaximumMediaDataBytes = kAtariStDiskBytes + 256;
struct MediaDataIdentity {
    std::string core_id, game_id, base_media_id;
    std::uint8_t unit = 0;
};
struct MediaDiskRecord {
    MediaDataIdentity identity;
    std::vector<unsigned char> bytes;
    std::string revision = "absent";
};
bool ValidMediaDataIdentity(const MediaDataIdentity&);
std::string MediaDataNamespace(const MediaDataIdentity&);
Error EncodeMediaData(const MediaDiskRecord&, std::vector<unsigned char>*);
Error DecodeMediaData(const std::vector<unsigned char>&, const MediaDataIdentity&, MediaDiskRecord*);
// Separate immutable-source disk namespace; neither package versions nor
// video/firmware selection name it. Open takes the namespace lock and
// removes leftover private save and probe files. Read reopens the complete
// canonical file.
class MediaDataFile {
public:
    ~MediaDataFile();
    static Error Open(const std::string& root, const MediaDataIdentity&, std::unique_ptr<MediaDataFile>*);
    Error Read(MediaDiskRecord*) const;
    Error CheckWritable() const;
    Error Persist(const MediaDiskRecord&, const std::string& expected_revision, MediaDiskRecord*);
    MediaDataFile(const MediaDataFile&) = delete;
    MediaDataFile& operator=(const MediaDataFile&) = delete;
private:
    MediaDataFile(int directory, MediaDataIdentity identity);
    Error ReadLocked(MediaDiskRecord*) const;
    int directory_;
    MediaDataIdentity identity_;
};
} }
