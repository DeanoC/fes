// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
#include <memory>
#include "native/atari_st_geometry.hpp"
namespace mister { namespace native {
constexpr std::size_t kAtariStDiskBytes = 737280;
constexpr std::size_t kMaximumMediaDataBytes =
    (generated::FesComputerPcFloppyMaxBytes > generated::FesComputerAtariStFloppyGeometryMaxBytes ?
     generated::FesComputerPcFloppyMaxBytes : generated::FesComputerAtariStFloppyGeometryMaxBytes) + 256;
// fes.pc386-floppy.image: every pc386 floppy is keyed under unit 1, whichever
// drive holds it (mister-packages docs/media-data.md).
constexpr std::uint8_t kPc386FloppyNamespaceUnit = 1;
// A size is one of the PC formats fes.media.pc-floppy admits.
inline bool PcFloppySizeAdmitted(std::size_t bytes) {
    for (std::size_t size : {163840u, 184320u, 327680u, 368640u, 737280u, 1228800u, 1474560u, 1720320u, 2949120u})
        if (bytes == size) return true;
    return false;
}
struct MediaDataIdentity {
    std::string core_id, game_id, base_media_id;
    std::uint8_t unit = 0;
};
enum class MediaDataLayout { atari_st, pcxt_floppy, zx81_tape, pc386_floppy };
// The pc386 floppy record's identity: core fes.pc386, unit 1.
inline bool Pc386FloppyIdentity(const std::string& core_id, std::uint8_t unit) {
    return core_id == "fes.pc386" && unit == kPc386FloppyNamespaceUnit;
}
struct MediaDiskRecord {
    MediaDataIdentity identity;
    std::vector<unsigned char> bytes;
    std::string revision = "absent";
    MediaDataLayout layout = MediaDataLayout::atari_st;
};
// Lowercase letters, digits and single inner hyphens, at most 256 bytes.
inline bool ValidLibraryGameID(const std::string& game) {
    if (game.empty() || game.size()>256 || game.front()=='-' || game.back()=='-') return false;
    bool hyphen=false;
    for(char c:game) {
        if(!((c>='a'&&c<='z')||(c>='0'&&c<='9')||c=='-')) return false;
        if(c=='-'&&hyphen) return false;
        hyphen=c=='-';
    }
    return true;
}
bool ValidMediaDataIdentity(const MediaDataIdentity&);
std::string MediaDataNamespace(const MediaDataIdentity&);
Error EncodeMediaData(const MediaDiskRecord&, std::vector<unsigned char>*);
Error DecodeMediaData(const std::vector<unsigned char>&, const MediaDataIdentity&, MediaDiskRecord*);
// The no-follow namespace directory of an identity under root, created when
// absent, with leftover private save, probe and chunk temporaries removed
// under its lock. The caller owns the descriptor.
Error OpenMediaDataNamespace(const std::string& root, const MediaDataIdentity&, int* directory);
// The saved record of an identity, read without creating its namespace or
// binding it. state is absent, present or corrupt; revision is the digest of
// the record file as it stands, or absent.
Error InspectMediaData(const std::string& root, const MediaDataIdentity&, MediaDataInspection*);
// Removes an identity's saved record when its file still has
// expected_revision, then its namespace when that leaves it empty. The next
// start of that identity reads the immutable source again. The caller must
// not hold the identity bound: a bound disk would be saved straight back.
Error DiscardMediaData(const std::string& root, const MediaDataIdentity&,
	const std::string& expected_revision, MediaDataInspection*);
// Writes the image held in an identity's saved record to a new file at
// path, when the record still has expected_revision. The record is not
// changed. A corrupt record is refused; bytes reports the image's length.
Error ExportMediaData(const std::string& root, const MediaDataIdentity&,
	const std::string& expected_revision, const std::string& path, MediaDataInspection*);
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
