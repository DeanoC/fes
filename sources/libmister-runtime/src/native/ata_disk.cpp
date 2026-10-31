// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/ata_disk.hpp"
#include "native/sha256.hpp"
#include <algorithm>
#include <atomic>
#include <cerrno>
#include <cstring>
#include <dirent.h>
#include <fcntl.h>
#include <sys/file.h>
#include <sys/stat.h>
#include <unistd.h>
namespace mister { namespace native { namespace {
const std::string kLayout = "fes.ata-disk.delta";
constexpr std::uint32_t kChunk = generated::FesComputerAtaDiskChunkBytes;
// 174 + 36 entries for every chunk of the largest image.
constexpr std::size_t kMaximumManifestBytes = 174 + 36 * std::size_t(generated::FesComputerAtaDiskMaxChunks);
Error Bad(const char* text) { return {ErrorCode::corrupt_data, text, "media_data"}; }
Error Incompatible(const char* text) { return {ErrorCode::incompatible_data, text, "media_data"}; }
Error Io(const char* text) { return {ErrorCode::save_failed, text, "media_data"}; }
std::array<std::uint8_t,32> Hash(const void* bytes, std::size_t size) {
    Sha256 hash; hash.Update(bytes,size); return hash.Final();
}
void Put16(std::vector<unsigned char>* b, unsigned v) { b->push_back(v); b->push_back(v>>8); }
void Put32(std::vector<unsigned char>* b, std::uint32_t v) { Put16(b,v&0xffff); Put16(b,v>>16); }
unsigned Get16(const std::vector<unsigned char>& b, std::size_t at) { return unsigned(b[at]) | unsigned(b[at+1])<<8; }
std::uint32_t Get32(const std::vector<unsigned char>& b, std::size_t at) { return Get16(b,at) | std::uint32_t(Get16(b,at+2))<<16; }
bool Digest(const std::string& text) {
    if (text.size()!=64) return false;
    for (char c:text) if (!((c>='0'&&c<='9')||(c>='a'&&c<='f'))) return false;
    return true;
}
void PutDigest(std::vector<unsigned char>* b, const std::string& hex) {
    for (unsigned at=0;at<64;at+=2) b->push_back(static_cast<unsigned char>(std::stoul(hex.substr(at,2),nullptr,16)));
}
std::string HexAt(const std::vector<unsigned char>& b, std::size_t at) {
    static const char digits[] = "0123456789abcdef";
    std::string text;
    for (std::size_t i=0;i<32;++i) { text.push_back(digits[b[at+i]>>4]); text.push_back(digits[b[at+i]&15]); }
    return text;
}
class FD {
public:
    explicit FD(int value) : value(value) {}
    ~FD() { if (value >= 0) close(value); }
    int value;
};
class Lock {
public:
    explicit Lock(int fd, int mode) : fd_(fd), ok_(flock(fd, mode) == 0) {}
    ~Lock() { if (ok_) flock(fd_, LOCK_UN); }
    bool ok() const { return ok_; }
private:
    int fd_;
    bool ok_;
};
bool WriteAll(int fd, const unsigned char* bytes, std::size_t size) {
    std::size_t written = 0;
    while (written < size) {
        const ssize_t count = write(fd, bytes + written, size - written);
        if (count < 0 && errno == EINTR) continue;
        if (count <= 0) return false;
        written += static_cast<std::size_t>(count);
    }
    return true;
}
// Reads a regular, no-follow file of at most limit bytes. ENOENT is reported
// through missing.
Error ReadFile(int directory, const char* name, std::size_t limit, std::vector<unsigned char>* output, bool* missing) {
    *missing = false;
    FD file(openat(directory, name, O_RDONLY | O_CLOEXEC | O_NOFOLLOW | O_NONBLOCK));
    if (file.value < 0) {
        if (errno == ENOENT) { *missing = true; return {}; }
        return Io("cannot open ATA disk delta file");
    }
    struct stat st {};
    if (fstat(file.value, &st) != 0) return Io("cannot inspect ATA disk delta file");
    if (!S_ISREG(st.st_mode) || st.st_size < 0 || static_cast<std::uint64_t>(st.st_size) > limit)
        return Bad("invalid ATA disk delta file");
    std::vector<unsigned char> bytes(static_cast<std::size_t>(st.st_size));
    std::size_t done = 0;
    while (done < bytes.size()) {
        const ssize_t count = read(file.value, bytes.data() + done, bytes.size() - done);
        if (count < 0 && errno == EINTR) continue;
        if (count <= 0) return Bad("truncated ATA disk delta file");
        done += static_cast<std::size_t>(count);
    }
    unsigned char extra;
    ssize_t count;
    do count = read(file.value, &extra, 1); while (count < 0 && errno == EINTR);
    if (count != 0) return Bad("ATA disk delta file changed during read");
    *output = std::move(bytes);
    return {};
}
// Writes bytes to a private temporary and renames it over name.
Error Replace(int directory, const char* prefix, const std::string& name,
    const unsigned char* bytes, std::size_t size) {
    static std::atomic<unsigned long> sequence{0};
    std::string temporary;
    int descriptor = -1;
    for (unsigned attempt = 0; attempt < 32; ++attempt) {
        temporary = std::string(prefix) + std::to_string(getpid()) + "-" + std::to_string(++sequence);
        descriptor = openat(directory, temporary.c_str(), O_WRONLY | O_CREAT | O_EXCL | O_CLOEXEC | O_NOFOLLOW, 0600);
        if (descriptor >= 0 || errno != EEXIST) break;
    }
    if (descriptor < 0) return Io("cannot create ATA disk delta temporary");
    FD file(descriptor);
    Error error;
    if (!WriteAll(file.value, bytes, size)) error = Io("cannot write ATA disk delta file");
    if (error.ok() && fsync(file.value) != 0) error = Io("cannot sync ATA disk delta file");
    if (error.ok() && renameat(directory, temporary.c_str(), directory, name.c_str()) != 0)
        error = Io("cannot publish ATA disk delta file");
    if (!error.ok()) unlinkat(directory, temporary.c_str(), 0);
    return error;
}
bool ChunkName(const std::string& name) {
    return name.size() == 70 && name.compare(0, 6, "chunk-") == 0 && Digest(name.substr(6));
}
} // namespace

AtaBaseImage::~AtaBaseImage() { close(fd_); }
Error AtaBaseImage::Open(const std::string& path, std::uint32_t size, std::unique_ptr<AtaBaseImage>* output)
{
    if (!output || path.empty() || path[0] != '/' || !ValidAtaDiskSize(size))
        return {ErrorCode::invalid_request, "invalid ATA disk image path or size", "admission"};
    FD file(open(path.c_str(), O_RDONLY | O_CLOEXEC | O_NOFOLLOW | O_NONBLOCK));
    if (file.value < 0) return {ErrorCode::invalid_request, "cannot open the ATA disk image", "admission"};
    struct stat st {};
    if (fstat(file.value, &st) != 0 || !S_ISREG(st.st_mode) || std::uint64_t(st.st_size) != size)
        return {ErrorCode::invalid_request, "ATA disk image size differs from the request", "admission"};
    output->reset(new AtaBaseImage(file.value));
    file.value = -1;
    return {};
}
Error AtaBaseImage::Read(std::uint64_t offset, unsigned char* bytes, std::size_t size) const
{
    std::size_t done = 0;
    while (done < size) {
        const ssize_t count = pread(fd_, bytes + done, size - done, static_cast<off_t>(offset + done));
        if (count < 0 && errno == EINTR) continue;
        if (count <= 0) return {ErrorCode::io_failed, "cannot read the ATA disk image", "input"};
        done += static_cast<std::size_t>(count);
    }
    return {};
}
Error EncodeAtaDelta(const AtaDiskDelta& delta, std::vector<unsigned char>* output)
{
    if (!output || !ValidMediaDataIdentity(delta.identity) || !ValidAtaDiskSize(delta.base_size))
        return Bad("invalid ATA disk delta identity or size");
    std::int64_t previous = -1;
    for (const auto& chunk : delta.chunks)
        if (std::int64_t(chunk.index) <= previous || chunk.index >= AtaDiskChunkCount(delta.base_size) ||
            !Digest(chunk.sha256))
            return Bad("invalid ATA disk delta chunk list");
        else previous = chunk.index;
    std::vector<unsigned char> bytes = {'F','E','S','D','E','L','T','1'};
    auto core = Hash(delta.identity.core_id.data(), delta.identity.core_id.size());
    auto game = Hash(delta.identity.game_id.data(), delta.identity.game_id.size());
    bytes.insert(bytes.end(), core.begin(), core.end());
    bytes.insert(bytes.end(), game.begin(), game.end());
    PutDigest(&bytes, delta.identity.base_media_id);
    Put16(&bytes, delta.identity.unit);
    Put16(&bytes, 1);
    Put16(&bytes, 0);
    Put16(&bytes, static_cast<unsigned>(kLayout.size()));
    Put32(&bytes, delta.base_size);
    Put32(&bytes, kChunk);
    Put32(&bytes, static_cast<std::uint32_t>(delta.chunks.size()));
    bytes.insert(bytes.end(), kLayout.begin(), kLayout.end());
    for (const auto& chunk : delta.chunks) {
        Put32(&bytes, chunk.index);
        PutDigest(&bytes, chunk.sha256);
    }
    auto checksum = Hash(bytes.data(), bytes.size());
    bytes.insert(bytes.end(), checksum.begin(), checksum.end());
    *output = std::move(bytes);
    return {};
}
Error DecodeAtaDelta(const std::vector<unsigned char>& bytes, const MediaDataIdentity& identity,
    std::uint32_t base_size, AtaDiskDelta* output)
{
    if (!output || !ValidMediaDataIdentity(identity) || !ValidAtaDiskSize(base_size))
        return Bad("invalid ATA disk delta identity or size");
    if (bytes.size() < 174 || bytes.size() > kMaximumManifestBytes || std::memcmp(bytes.data(), "FESDELT1", 8) != 0)
        return Bad("invalid ATA disk delta envelope");
    auto checksum = Hash(bytes.data(), bytes.size() - 32);
    if (!std::equal(checksum.begin(), checksum.end(), bytes.end() - 32))
        return Bad("ATA disk delta checksum mismatch");
    const std::uint32_t count = Get32(bytes, 120);
    if (count > generated::FesComputerAtaDiskMaxChunks)
        return Bad("invalid ATA disk delta chunk count");
    const std::size_t chunk_count = static_cast<std::size_t>(count);
    if (bytes.size() != 174 + 36 * chunk_count || Get16(bytes, 110) != kLayout.size() ||
        std::memcmp(bytes.data() + 124, kLayout.data(), kLayout.size()) != 0)
        return Bad("invalid ATA disk delta size or layout");
    auto core = Hash(identity.core_id.data(), identity.core_id.size());
    auto game = Hash(identity.game_id.data(), identity.game_id.size());
    std::vector<unsigned char> base;
    PutDigest(&base, identity.base_media_id);
    if (!std::equal(core.begin(), core.end(), bytes.begin() + 8) ||
        !std::equal(game.begin(), game.end(), bytes.begin() + 40) ||
        !std::equal(base.begin(), base.end(), bytes.begin() + 72) || Get16(bytes, 104) != identity.unit)
        return Incompatible("ATA disk delta identity mismatch");
    if (Get16(bytes, 106) != 1 || Get16(bytes, 108) != 0 || Get32(bytes, 116) != kChunk)
        return Incompatible("unsupported ATA disk delta layout");
    if (Get32(bytes, 112) != base_size)
        return Incompatible("ATA disk delta base size differs from the image");
    AtaDiskDelta delta;
    delta.identity = identity;
    delta.base_size = base_size;
    std::int64_t previous = -1;
    for (std::size_t at = 142; at < 142 + 36 * chunk_count; at += 36) {
        const std::uint32_t index = Get32(bytes, at);
        if (std::int64_t(index) <= previous || index >= AtaDiskChunkCount(base_size))
            return Bad("invalid ATA disk delta chunk list");
        previous = index;
        delta.chunks.push_back({index, HexAt(bytes, at + 4)});
    }
    delta.revision = Sha256Hex(Hash(bytes.data(), bytes.size()));
    *output = std::move(delta);
    return {};
}
AtaDiskDelta MergeAtaDelta(const AtaDiskDelta& existing, const std::vector<AtaDeltaChunk>& changed)
{
    AtaDiskDelta merged = existing;
    for (const auto& chunk : changed) {
        auto at = std::lower_bound(merged.chunks.begin(), merged.chunks.end(), chunk.index,
            [](const AtaDeltaChunk& a, std::uint32_t index) { return a.index < index; });
        if (at != merged.chunks.end() && at->index == chunk.index) at->sha256 = chunk.sha256;
        else merged.chunks.insert(at, chunk);
    }
    return merged;
}

AtaDeltaFile::AtaDeltaFile(int directory, MediaDataIdentity identity, std::uint32_t base_size)
    : directory_(directory), identity_(std::move(identity)), base_size_(base_size) {}
AtaDeltaFile::~AtaDeltaFile() { close(directory_); }
Error AtaDeltaFile::Open(const std::string& root, const MediaDataIdentity& identity, std::uint32_t base_size,
    std::unique_ptr<AtaDeltaFile>* output)
{
    if (!output || !ValidAtaDiskSize(base_size)) return Bad("invalid ATA disk delta identity or size");
    int directory = -1;
    const Error error = OpenMediaDataNamespace(root, identity, &directory);
    if (error.ok()) output->reset(new AtaDeltaFile(directory, identity, base_size));
    return error;
}
Error AtaDeltaFile::ReadLocked(AtaDiskDelta* output) const
{
    std::vector<unsigned char> bytes;
    bool missing = false;
    Error error = ReadFile(directory_, "record.bin", kMaximumManifestBytes, &bytes, &missing);
    if (!error.ok()) return error;
    if (missing) {
        AtaDiskDelta empty;
        empty.identity = identity_;
        empty.base_size = base_size_;
        *output = std::move(empty);
        return {};
    }
    return DecodeAtaDelta(bytes, identity_, base_size_, output);
}
Error AtaDeltaFile::Read(AtaDiskDelta* output) const
{
    if (!output) return Bad("missing ATA disk delta output");
    Lock lock(directory_, LOCK_SH);
    if (!lock.ok()) return Io("cannot lock media-data namespace");
    return ReadLocked(output);
}
Error AtaDeltaFile::CheckWritable() const
{
    Lock lock(directory_, LOCK_EX);
    if (!lock.ok()) return Io("cannot lock media-data namespace");
    static std::atomic<unsigned long> sequence{0};
    const std::string probe_name = ".probe-" + std::to_string(getpid()) + "-" + std::to_string(++sequence);
    const unsigned char probe = 0;
    Error error = Replace(directory_, ".probe-", probe_name, &probe, 1);
    if (error.ok() && unlinkat(directory_, probe_name.c_str(), 0) != 0)
        error = Io("media-data write check cleanup failed");
    if (!error.ok()) (void)unlinkat(directory_, probe_name.c_str(), 0);
    if (fsync(directory_) != 0 && error.ok()) error = Io("media-data namespace sync check failed");
    return error.ok() ? error : Error{ErrorCode::save_failed, "media-data namespace is not writable", "media_data"};
}
Error AtaDeltaFile::ReadChunk(const AtaDeltaChunk& chunk, std::vector<unsigned char>* output) const
{
    if (!output || !Digest(chunk.sha256)) return Bad("invalid ATA disk delta chunk");
    Lock lock(directory_, LOCK_SH);
    if (!lock.ok()) return Io("cannot lock media-data namespace");
    std::vector<unsigned char> bytes;
    bool missing = false;
    Error error = ReadFile(directory_, ("chunk-" + chunk.sha256).c_str(), kChunk, &bytes, &missing);
    if (!error.ok()) return error;
    if (missing) return Bad("ATA disk delta chunk is missing");
    if (bytes.size() != AtaDiskChunkLength(base_size_, chunk.index) ||
        Sha256Hex(Hash(bytes.data(), bytes.size())) != chunk.sha256)
        return Bad("ATA disk delta chunk differs from its manifest");
    *output = std::move(bytes);
    return {};
}
Error AtaDeltaFile::WriteChunk(const unsigned char* bytes, std::size_t size, std::string* sha256)
{
    if (!bytes || !sha256 || !size || size > kChunk) return Bad("invalid ATA disk delta chunk");
    const std::string digest = Sha256Hex(Hash(bytes, size));
    Lock lock(directory_, LOCK_EX);
    if (!lock.ok()) return Io("cannot lock media-data namespace");
    const Error error = Replace(directory_, ".chunk-", "chunk-" + digest, bytes, size);
    if (error.ok()) *sha256 = digest;
    return error;
}
Error AtaDeltaFile::CollectLocked(const AtaDiskDelta& published)
{
    const int copy = openat(directory_, ".", O_RDONLY | O_DIRECTORY | O_CLOEXEC);
    if (copy < 0) return Io("cannot scan media-data namespace");
    DIR* dir = fdopendir(copy);
    if (!dir) { close(copy); return Io("cannot scan media-data namespace"); }
    std::vector<std::string> unnamed;
    while (const dirent* entry = readdir(dir)) {
        const std::string name(entry->d_name);
        if (!ChunkName(name)) continue;
        const std::string digest = name.substr(6);
        if (std::none_of(published.chunks.begin(), published.chunks.end(),
                [&](const AtaDeltaChunk& chunk) { return chunk.sha256 == digest; }))
            unnamed.push_back(name);
    }
    closedir(dir);
    for (const auto& name : unnamed)
        if (unlinkat(directory_, name.c_str(), 0) != 0 && errno != ENOENT)
            return Io("cannot remove an unnamed ATA disk delta chunk");
    if (!unnamed.empty() && fsync(directory_) != 0)
        return Io("cannot sync media-data namespace after removing chunks");
    return {};
}
Error AtaDeltaFile::Publish(const AtaDiskDelta& delta, const std::string& revision, AtaDiskDelta* output)
{
    if (delta.identity.core_id != identity_.core_id || delta.identity.game_id != identity_.game_id ||
        delta.identity.base_media_id != identity_.base_media_id || delta.identity.unit != identity_.unit ||
        delta.base_size != base_size_)
        return Incompatible("ATA disk delta identity mismatch");
    std::vector<unsigned char> bytes;
    Error error = EncodeAtaDelta(delta, &bytes);
    if (!error.ok()) return error;
    Lock lock(directory_, LOCK_EX);
    if (!lock.ok()) return Io("cannot lock media-data namespace");
    AtaDiskDelta current;
    error = ReadLocked(&current);
    if (!error.ok()) return error;
    if (current.revision != revision)
        return {ErrorCode::stale_revision, "media-data revision changed", "media_data"};
    for (const auto& chunk : delta.chunks) {
        const std::string name = "chunk-" + chunk.sha256;
        struct stat st {};
        if (fstatat(directory_, name.c_str(), &st, AT_SYMLINK_NOFOLLOW) != 0 ||
            !S_ISREG(st.st_mode) || std::uint64_t(st.st_size) != AtaDiskChunkLength(base_size_, chunk.index))
            return Bad("ATA disk delta names a chunk that is not stored");
        std::vector<unsigned char> stored;
        bool missing = false;
        error = ReadFile(directory_, name.c_str(), kChunk, &stored, &missing);
        if (!error.ok()) return error;
        if (missing || stored.size() != AtaDiskChunkLength(base_size_, chunk.index) ||
            Sha256Hex(Hash(stored.data(), stored.size())) != chunk.sha256)
            return Bad("ATA disk delta chunk differs from its manifest");
    }
    // Chunk renames reach the directory before the manifest that names them.
    if (fsync(directory_) != 0) return Io("cannot sync ATA disk delta chunks");
    error = Replace(directory_, ".record-", "record.bin", bytes.data(), bytes.size());
    if (!error.ok()) return error;
    if (fsync(directory_) != 0) return Io("media-data published; durability is uncertain; retry required");
    AtaDiskDelta published;
    error = DecodeAtaDelta(bytes, identity_, base_size_, &published);
    if (error.ok()) error = CollectLocked(published);
    if (error.ok() && output) *output = std::move(published);
    return error;
}
} }
