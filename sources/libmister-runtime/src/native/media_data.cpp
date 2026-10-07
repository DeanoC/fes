// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/media_data.hpp"
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
const std::string kLayout = "fes.atari-st-floppy.image";
Error Bad(const char* text) { return {ErrorCode::corrupt_data, text, "media_data"}; }
Error Incompatible(const char* text) { return {ErrorCode::incompatible_data, text, "media_data"}; }
Error Io(const char* text) { return {ErrorCode::save_failed, text, "media_data"}; }
std::array<std::uint8_t,32> Hash(const void* bytes, std::size_t size) {
    Sha256 hash; hash.Update(bytes,size); return hash.Final();
}
std::array<std::uint8_t,32> Hash(const std::string& text) { return Hash(text.data(),text.size()); }
void Word(std::vector<unsigned char>* b, unsigned word) { b->push_back(word); b->push_back(word>>8); }
unsigned Word(const std::vector<unsigned char>& b, std::size_t at) { return unsigned(b[at]) | unsigned(b[at+1])<<8; }
bool SameIdentity(const MediaDataIdentity& a, const MediaDataIdentity& b) {
    return a.core_id == b.core_id && a.game_id == b.game_id && a.base_media_id == b.base_media_id && a.unit == b.unit;
}
bool Digest(const std::string& text) {
    if (text.size()!=64) return false;
    for (char c:text) if (!((c>='0'&&c<='9')||(c>='a'&&c<='f'))) return false;
    return true;
}
std::vector<unsigned char> DigestBytes(const std::string& text) {
    std::vector<unsigned char> bytes;
    for (unsigned at=0;at<64;at+=2) bytes.push_back(static_cast<unsigned char>(std::stoul(text.substr(at,2),nullptr,16)));
    return bytes;
}
class FD {
public:
	explicit FD(int value) : value(value) {}
	~FD()
	{
		if (value >= 0)
			close(value);
	}
	int value;
};
class Lock {
public:
	explicit Lock(int fd, int mode) : fd_(fd), ok_(flock(fd, mode) == 0) {}
	~Lock()
	{
		if (ok_)
			flock(fd_, LOCK_UN);
	}
	bool ok() const
	{
		return ok_;
	}

private:
	int fd_;
	bool ok_;
};
bool Decimal(const std::string& text, std::size_t begin, std::size_t end)
{
	if (begin >= end || end > text.size())
		return false;
	for (std::size_t at = begin; at < end; ++at)
		if (text[at] < '0' || text[at] > '9')
			return false;
	return true;
}
bool StaleMediaTemporary(const std::string& name)
{
	static const char* prefixes[] = {".record-", ".probe-"};
	for (const char* prefix : prefixes) {
		const std::size_t length = std::strlen(prefix);
		if (name.compare(0, length, prefix) != 0)
			continue;
		const std::size_t hyphen = name.find('-', length);
		return hyphen != std::string::npos && Decimal(name, length, hyphen) &&
			Decimal(name, hyphen + 1, name.size());
	}
	return false;
}
Error SweepStaleMediaTemps(int directory)
{
	Lock lock(directory, LOCK_EX);
	if (!lock.ok())
		return Io("cannot lock media-data namespace");
	const int copy = openat(directory, ".", O_RDONLY | O_DIRECTORY | O_CLOEXEC);
	if (copy < 0)
		return Io("cannot scan media-data namespace");
	DIR* dir = fdopendir(copy);
	if (!dir) {
		close(copy);
		return Io("cannot scan media-data namespace");
	}
	std::vector<std::string> stale;
	while (true) {
		errno = 0;
		const dirent* entry = readdir(dir);
		if (!entry) {
			if (errno != 0) {
				closedir(dir);
				return Io("cannot scan media-data namespace");
			}
			break;
		}
		const std::string name(entry->d_name);
		if (StaleMediaTemporary(name))
			stale.push_back(name);
	}
	if (closedir(dir) != 0)
		return Io("cannot scan media-data namespace");
	bool removed = false;
	Error error;
	for (const std::string& name : stale) {
		if (unlinkat(directory, name.c_str(), 0) != 0) {
			if (errno == ENOENT)
				continue;
			error = Io("cannot remove stale media-data temporary");
			break;
		}
		removed = true;
	}
	if (removed && fsync(directory) != 0 && error.ok())
		error = Io("cannot sync media-data namespace after removing temporaries");
	return error;
}
 } // namespace
bool ValidMediaDataIdentity(const MediaDataIdentity& id) {
    if (id.unit!=0 || id.core_id.empty() || id.core_id.size()>96 ||
        id.game_id.empty() || id.game_id.size()>256 || !Digest(id.base_media_id)) return false;
    if (id.core_id.front()<'a'||id.core_id.front()>'z') return false;
    for(char c:id.core_id) if(!((c>='a'&&c<='z')||(c>='0'&&c<='9')||c=='.'||c=='_'||c=='-')) return false;
    if (id.game_id.front()=='-'||id.game_id.back()=='-') return false;
    bool hyphen=false;
    for(char c:id.game_id) {
        if(!((c>='a'&&c<='z')||(c>='0'&&c<='9')||c=='-')) return false;
        if(c=='-'&&hyphen) return false;
        hyphen=c=='-';
    }
    return true;
}
std::string MediaDataNamespace(const MediaDataIdentity& id) {
    const std::string name=std::string("fes-media-data-v1")+'\0'+id.core_id+'\0'+id.game_id+'\0'+
        std::to_string(id.unit)+'\0'+id.base_media_id;
    return Sha256Hex(Hash(name));
}
Error EncodeMediaData(const MediaDiskRecord& record,std::vector<unsigned char>* output) {
    if(!output||!ValidMediaDataIdentity(record.identity)) return Bad("invalid media-data identity");
    if(record.bytes.size()>generated::FesComputerAtariStFloppyGeometryMaxBytes || !InferAtariStGeometry(record.bytes.size())) return Bad("invalid media-data payload size");
    std::vector<unsigned char> bytes={'F','E','S','D','I','S','K','1'};
    for(const auto& digest:{Hash(record.identity.core_id),Hash(record.identity.game_id)})
        bytes.insert(bytes.end(),digest.begin(),digest.end());
    const auto base=DigestBytes(record.identity.base_media_id);
    bytes.insert(bytes.end(),base.begin(),base.end());
    Word(&bytes,record.identity.unit); Word(&bytes,1); Word(&bytes,record.bytes.size()==kAtariStDiskBytes ? 0 : 1); Word(&bytes,kLayout.size());
    Word(&bytes,record.bytes.size()); Word(&bytes,record.bytes.size()>>16);
    bytes.insert(bytes.end(),kLayout.begin(),kLayout.end());
    bytes.insert(bytes.end(),record.bytes.begin(),record.bytes.end());
    const auto sum=Hash(bytes.data(),bytes.size()); bytes.insert(bytes.end(),sum.begin(),sum.end());
    *output=std::move(bytes); return {};
}
Error DecodeMediaData(const std::vector<unsigned char>& bytes,const MediaDataIdentity& id,MediaDiskRecord* output) {
    if(!output||!ValidMediaDataIdentity(id)) return Bad("invalid media-data identity");
    if(bytes.size()<148+kLayout.size() || bytes.size()>kMaximumMediaDataBytes || std::memcmp(bytes.data(),"FESDISK1",8))
        return Bad("invalid media-data envelope or size");
    const auto sum=Hash(bytes.data(),bytes.size()-32);
    if(!std::equal(sum.begin(),sum.end(),bytes.end()-32)) return Bad("media-data checksum mismatch");
    const auto core=Hash(id.core_id), game=Hash(id.game_id); const auto base=DigestBytes(id.base_media_id);
    if(!std::equal(core.begin(),core.end(),bytes.begin()+8)||!std::equal(game.begin(),game.end(),bytes.begin()+40)||
       !std::equal(base.begin(),base.end(),bytes.begin()+72)||Word(bytes,104)!=id.unit)
        return Incompatible("media-data identity mismatch");
    const auto size=Word(bytes,112)|(Word(bytes,114)<<16);
    const auto minor=size==kAtariStDiskBytes ? 0u : 1u;
    if(Word(bytes,106)!=1||Word(bytes,108)!=minor||Word(bytes,110)!=kLayout.size()||
       !InferAtariStGeometry(size) || bytes.size()!=148+kLayout.size()+size ||
       !std::equal(kLayout.begin(),kLayout.end(),bytes.begin()+116)) return Incompatible("media-data layout mismatch");
    MediaDiskRecord record; record.identity=id;
    record.bytes.assign(bytes.begin()+116+kLayout.size(),bytes.end()-32);
    record.revision=Sha256Hex(Hash(bytes.data(),bytes.size())); *output=std::move(record); return {};
}
MediaDataFile::MediaDataFile(int directory, MediaDataIdentity id)
	: directory_(directory), identity_(std::move(id))
{
}
MediaDataFile::~MediaDataFile()
{
	close(directory_);
}
Error MediaDataFile::Open(
	const std::string& root, const MediaDataIdentity& id, std::unique_ptr<MediaDataFile>* output)
{
	if (!output || !ValidMediaDataIdentity(id) || root.empty() || root[0] != '/' || root.size() > 4095 ||
		root.find('\0') != std::string::npos)
		return Bad("invalid media-data root or identity");
	FD parent(open("/", O_RDONLY | O_DIRECTORY | O_CLOEXEC));
	if (parent.value < 0)
		return Io("cannot open media-data root");
	std::size_t at = 1;
	while (at < root.size()) {
		auto end = root.find('/', at);
		if (end == std::string::npos)
			end = root.size();
		auto part = root.substr(at, end - at);
		if (part.empty() || part == "." || part == "..")
			return Bad("invalid media-data root component");
		int next =
			openat(parent.value, part.c_str(), O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
		if (next < 0)
			return Io("cannot open trusted media-data root");
		close(parent.value);
		parent.value = next;
		at = end + 1;
	}
	const std::string name = MediaDataNamespace(id);
	if (mkdirat(parent.value, name.c_str(), 0700) != 0 && errno != EEXIST)
		return Io("cannot create media-data namespace");
	int directory =
		openat(parent.value, name.c_str(), O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
	if (directory < 0)
		return Io("cannot open media-data namespace");
	FD opened(directory);
	if (fsync(parent.value) != 0)
		return Io("cannot sync media-data namespace parent");
	Error swept = SweepStaleMediaTemps(opened.value);
	if (!swept.ok())
		return swept;
	output->reset(new MediaDataFile(opened.value, id));
	opened.value = -1;
	return {};
}
Error MediaDataFile::ReadLocked(MediaDiskRecord* output) const
{
	if (!output)
		return Bad("missing media-data output");
	FD file(openat(directory_, "record.bin", O_RDONLY | O_CLOEXEC | O_NOFOLLOW | O_NONBLOCK));
	if (file.value < 0) {
		if (errno != ENOENT)
			return Io("cannot open media-data record");
		MediaDiskRecord data;
		data.identity = identity_;
		*output = std::move(data);
		return {};
	}
	struct stat st {};
	if (fstat(file.value, &st) != 0)
		return Io("cannot inspect media-data record");
	if (!S_ISREG(st.st_mode) || st.st_size < 0 ||
		static_cast<std::uint64_t>(st.st_size) > kMaximumMediaDataBytes)
		return Bad("invalid media-data record file");
	std::vector<unsigned char> bytes(static_cast<std::size_t>(st.st_size));
	std::size_t read_bytes = 0;
	while (read_bytes < bytes.size()) {
		ssize_t count = read(file.value, bytes.data() + read_bytes, bytes.size() - read_bytes);
		if (count < 0 && errno == EINTR)
			continue;
		if (count <= 0)
			return Bad("truncated media-data record");
		read_bytes += count;
	}
	unsigned char extra;
	ssize_t count;
	do {
		count = read(file.value, &extra, 1);
	} while (count < 0 && errno == EINTR);
	if (count != 0)
		return Bad("media-data record changed during read");
	return DecodeMediaData(bytes, identity_, output);
}
Error MediaDataFile::Read(MediaDiskRecord* output) const
{
	Lock lock(directory_, LOCK_SH);
	if (!lock.ok())
		return Io("cannot lock media-data namespace");
	return ReadLocked(output);
}
Error MediaDataFile::CheckWritable() const
{
	Lock lock(directory_, LOCK_EX);
	if (!lock.ok())
		return Io("cannot lock media-data namespace");
	static std::atomic<unsigned long> sequence{0};
	std::string temporary;
	int descriptor = -1;
	for (unsigned attempt = 0; attempt < 32; ++attempt) {
		temporary = ".probe-" + std::to_string(getpid()) + "-" + std::to_string(++sequence);
		descriptor = openat(directory_, temporary.c_str(),
			O_WRONLY | O_CREAT | O_EXCL | O_CLOEXEC | O_NOFOLLOW, 0600);
		if (descriptor >= 0 || errno != EEXIST)
			break;
	}
	if (descriptor < 0)
		return Io("media-data namespace is not writable");
	FD file(descriptor);
	unsigned char probe = 0;
	ssize_t count;
	do {
		count = write(file.value, &probe, 1);
	} while (count < 0 && errno == EINTR);
	Error error;
	if (count != 1 || fsync(file.value) != 0)
		error = Io("media-data namespace write check failed");
	if (unlinkat(directory_, temporary.c_str(), 0) != 0 && error.ok())
		error = Io("media-data write check cleanup failed");
	if (fsync(directory_) != 0 && error.ok())
		error = Io("media-data namespace sync check failed");
	return error;
}
Error MediaDataFile::Persist(const MediaDiskRecord& data, const std::string& revision, MediaDiskRecord* output)
{
	if (!SameIdentity(data.identity, identity_))
		return Incompatible("media-data identity mismatch");
	std::vector<unsigned char> bytes;
	Error error = EncodeMediaData(data, &bytes);
	if (!error.ok())
		return error;
	Lock lock(directory_, LOCK_EX);
	if (!lock.ok())
		return Io("cannot lock media-data namespace");
	MediaDiskRecord current;
	error = ReadLocked(&current);
	if (!error.ok())
		return error;
	if (current.revision != revision)
		return {ErrorCode::stale_revision, "media-data revision changed", "media_data"};
	static std::atomic<unsigned long> sequence{0};
	std::string temporary;
	int descriptor = -1;
	for (unsigned attempt = 0; attempt < 32; ++attempt) {
		temporary = ".record-" + std::to_string(getpid()) + "-" + std::to_string(++sequence);
		descriptor = openat(directory_, temporary.c_str(),
			O_WRONLY | O_CREAT | O_EXCL | O_CLOEXEC | O_NOFOLLOW, 0600);
		if (descriptor >= 0 || errno != EEXIST)
			break;
	}
	if (descriptor < 0)
		return Io("cannot create media-data temporary record");
	FD file(descriptor);
	std::size_t written = 0;
	while (written < bytes.size()) {
		ssize_t count = write(file.value, bytes.data() + written, bytes.size() - written);
		if (count < 0 && errno == EINTR)
			continue;
		if (count <= 0) {
			error = Io("cannot write media-data record");
			break;
		}
		written += count;
	}
	if (error.ok() && fsync(file.value) != 0)
		error = Io("cannot sync media-data record");
	if (error.ok() && renameat(directory_, temporary.c_str(), directory_, "record.bin") != 0)
		error = Io("cannot publish media-data record");
	if (!error.ok()) {
		unlinkat(directory_, temporary.c_str(), 0);
		return error;
	}
	if (fsync(directory_) != 0)
		return Io("media-data published; durability is uncertain; retry required");
	MediaDiskRecord published;
	error = DecodeMediaData(bytes, identity_, &published);
	if (error.ok() && output)
		*output = std::move(published);
	return error;
}
} // namespace native
} // namespace mister
