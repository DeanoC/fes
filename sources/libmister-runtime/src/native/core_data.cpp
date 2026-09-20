// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/core_data.hpp"
#include "native/sha256.hpp"
#include <algorithm>
#include <atomic>
#include <cerrno>
#include <cstring>
#include <fcntl.h>
#include <sys/file.h>
#include <sys/stat.h>
#include <unistd.h>
namespace mister {
namespace native {
namespace {
const std::string kLayout = "fes.pong.progress";
Error Bad(const char* text)
{
	return {ErrorCode::corrupt_data, text, "core_data"};
}
Error Incompatible(const char* text)
{
	return {ErrorCode::incompatible_data, text, "core_data"};
}
Error Io(const char* text)
{
	return {ErrorCode::save_failed, text, "core_data"};
}
std::array<std::uint8_t, 32> Hash(const void* bytes, std::size_t size)
{
	Sha256 hash;
	hash.Update(bytes, size);
	return hash.Final();
}
void Word(std::vector<unsigned char>* bytes, std::uint16_t word)
{
	bytes->push_back(static_cast<unsigned char>(word));
	bytes->push_back(static_cast<unsigned char>(word >> 8));
}
std::uint16_t Word(const std::vector<unsigned char>& bytes, std::size_t at)
{
	return static_cast<std::uint16_t>(bytes[at] | (static_cast<unsigned>(bytes[at + 1]) << 8));
}
bool ID(const std::string& id)
{
	if (id.empty() || id.size() > 96)
		return false;
	for (char c : id)
		if (!((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-'))
			return false;
	return id.front() >= 'a' && id.front() <= 'z';
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
} // namespace
std::string CoreDataNamespace(const std::string& id)
{
	return Sha256Hex(Hash(id.data(), id.size()));
}
Error EncodeCoreData(const CoreData& data, std::vector<unsigned char>* output)
{
	if (!output || !ID(data.core_id))
		return Bad("invalid core-data identity");
	if (data.layout.id != kLayout || data.layout.major != 1 || data.layout.minor != 0)
		return Incompatible("unsupported core-data layout");
	if (data.paddle_speed > 2)
		return Bad("invalid paddle-speed value");
	std::vector<unsigned char> bytes = {'F', 'E', 'S', 'D', 'A', 'T', 'A', '1'};
	auto identity = Hash(data.core_id.data(), data.core_id.size());
	bytes.insert(bytes.end(), identity.begin(), identity.end());
	Word(&bytes, static_cast<std::uint16_t>(kLayout.size()));
	Word(&bytes, 1);
	Word(&bytes, 0);
	Word(&bytes, 2);
	bytes.insert(bytes.end(), kLayout.begin(), kLayout.end());
	Word(&bytes, data.paddle_speed);
	Word(&bytes, data.best_rally);
	auto checksum = Hash(bytes.data(), bytes.size());
	bytes.insert(bytes.end(), checksum.begin(), checksum.end());
	*output = std::move(bytes);
	return {};
}
Error DecodeCoreData(
	const std::vector<unsigned char>& bytes, const std::string& id, CoreData* output)
{
	if (!output || !ID(id))
		return Bad("invalid core-data identity");
	if (bytes.size() < 82 || bytes.size() > kMaximumCoreDataBytes ||
		std::memcmp(bytes.data(), "FESDATA1", 8) != 0)
		return Bad("invalid core-data envelope");
	const std::size_t length = Word(bytes, 40), count = Word(bytes, 46);
	if (length == 0 || length > 96 || count == 0 || count > 256 ||
		bytes.size() != 80 + length + count * 2)
		return Bad("invalid core-data size or count");
	auto checksum = Hash(bytes.data(), bytes.size() - 32);
	if (!std::equal(checksum.begin(), checksum.end(), bytes.end() - 32))
		return Bad("core-data checksum mismatch");
	auto identity = Hash(id.data(), id.size());
	if (!std::equal(identity.begin(), identity.end(), bytes.begin() + 8))
		return Incompatible("core-data identity mismatch");
	const std::string layout(bytes.begin() + 48, bytes.begin() + 48 + length);
	if (!ID(layout))
		return Bad("invalid core-data layout identity");
	if (layout != kLayout || Word(bytes, 42) != 1 || Word(bytes, 44) != 0 || count != 2)
		return Incompatible("core-data layout mismatch");
	CoreData data;
	data.core_id = id;
	data.layout = {layout, 1, 0};
	data.mode = "persistent";
	data.paddle_speed = Word(bytes, 48 + length);
	data.best_rally = Word(bytes, 50 + length);
	if (data.paddle_speed > 2)
		return Bad("invalid paddle-speed value");
	data.revision = Sha256Hex(Hash(bytes.data(), bytes.size()));
	*output = std::move(data);
	return {};
}
CoreDataFile::CoreDataFile(int directory, std::string id)
	: directory_(directory), core_id_(std::move(id))
{
}
CoreDataFile::~CoreDataFile()
{
	close(directory_);
}
Error CoreDataFile::Open(
	const std::string& root, const std::string& id, std::unique_ptr<CoreDataFile>* output)
{
	if (!output || !ID(id) || root.empty() || root[0] != '/' || root.size() > 4095 ||
		root.find('\0') != std::string::npos)
		return Bad("invalid core-data root or identity");
	FD parent(open("/", O_RDONLY | O_DIRECTORY | O_CLOEXEC));
	if (parent.value < 0)
		return Io("cannot open core-data root");
	std::size_t at = 1;
	while (at < root.size()) {
		auto end = root.find('/', at);
		if (end == std::string::npos)
			end = root.size();
		auto part = root.substr(at, end - at);
		if (part.empty() || part == "." || part == "..")
			return Bad("invalid core-data root component");
		int next =
			openat(parent.value, part.c_str(), O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
		if (next < 0)
			return Io("cannot open trusted core-data root");
		close(parent.value);
		parent.value = next;
		at = end + 1;
	}
	const std::string name = CoreDataNamespace(id);
	if (mkdirat(parent.value, name.c_str(), 0700) != 0 && errno != EEXIST)
		return Io("cannot create core-data namespace");
	int directory =
		openat(parent.value, name.c_str(), O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
	if (directory < 0)
		return Io("cannot open core-data namespace");
	FD opened(directory);
	if (fsync(parent.value) != 0)
		return Io("cannot sync core-data namespace parent");
	output->reset(new CoreDataFile(opened.value, id));
	opened.value = -1;
	return {};
}
Error CoreDataFile::ReadLocked(CoreData* output) const
{
	if (!output)
		return Bad("missing core-data output");
	FD file(openat(directory_, "record.bin", O_RDONLY | O_CLOEXEC | O_NOFOLLOW | O_NONBLOCK));
	if (file.value < 0) {
		if (errno != ENOENT)
			return Io("cannot open core-data record");
		CoreData data;
		data.core_id = core_id_;
		data.layout = {kLayout, 1, 0};
		data.mode = "persistent";
		*output = std::move(data);
		return {};
	}
	struct stat st {};
	if (fstat(file.value, &st) != 0)
		return Io("cannot inspect core-data record");
	if (!S_ISREG(st.st_mode) || st.st_size < 0 ||
		static_cast<std::uint64_t>(st.st_size) > kMaximumCoreDataBytes)
		return Bad("invalid core-data record file");
	std::vector<unsigned char> bytes(static_cast<std::size_t>(st.st_size));
	std::size_t read_bytes = 0;
	while (read_bytes < bytes.size()) {
		ssize_t count = read(file.value, bytes.data() + read_bytes, bytes.size() - read_bytes);
		if (count < 0 && errno == EINTR)
			continue;
		if (count <= 0)
			return Bad("truncated core-data record");
		read_bytes += count;
	}
	unsigned char extra;
	ssize_t count;
	do {
		count = read(file.value, &extra, 1);
	} while (count < 0 && errno == EINTR);
	if (count != 0)
		return Bad("core-data record changed during read");
	return DecodeCoreData(bytes, core_id_, output);
}
Error CoreDataFile::Read(CoreData* output) const
{
	Lock lock(directory_, LOCK_SH);
	if (!lock.ok())
		return Io("cannot lock core-data namespace");
	return ReadLocked(output);
}
Error CoreDataFile::CheckWritable() const
{
	Lock lock(directory_, LOCK_EX);
	if (!lock.ok())
		return Io("cannot lock core-data namespace");
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
		return Io("core-data namespace is not writable");
	FD file(descriptor);
	unsigned char probe = 0;
	ssize_t count;
	do {
		count = write(file.value, &probe, 1);
	} while (count < 0 && errno == EINTR);
	Error error;
	if (count != 1 || fsync(file.value) != 0)
		error = Io("core-data namespace write check failed");
	if (unlinkat(directory_, temporary.c_str(), 0) != 0 && error.ok())
		error = Io("core-data write check cleanup failed");
	if (fsync(directory_) != 0 && error.ok())
		error = Io("core-data namespace sync check failed");
	return error;
}
Error CoreDataFile::Persist(const CoreData& data, const std::string& revision, CoreData* output)
{
	if (data.core_id != core_id_)
		return Incompatible("core-data identity mismatch");
	std::vector<unsigned char> bytes;
	Error error = EncodeCoreData(data, &bytes);
	if (!error.ok())
		return error;
	Lock lock(directory_, LOCK_EX);
	if (!lock.ok())
		return Io("cannot lock core-data namespace");
	CoreData current;
	error = ReadLocked(&current);
	if (!error.ok())
		return error;
	if (current.revision != revision)
		return {ErrorCode::stale_revision, "core-data revision changed", "core_data"};
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
		return Io("cannot create core-data temporary record");
	FD file(descriptor);
	std::size_t written = 0;
	while (written < bytes.size()) {
		ssize_t count = write(file.value, bytes.data() + written, bytes.size() - written);
		if (count < 0 && errno == EINTR)
			continue;
		if (count <= 0) {
			error = Io("cannot write core-data record");
			break;
		}
		written += count;
	}
	if (error.ok() && fsync(file.value) != 0)
		error = Io("cannot sync core-data record");
	if (error.ok() && renameat(directory_, temporary.c_str(), directory_, "record.bin") != 0)
		error = Io("cannot publish core-data record");
	if (!error.ok()) {
		unlinkat(directory_, temporary.c_str(), 0);
		return error;
	}
	if (fsync(directory_) != 0)
		return Io("core-data published; durability is uncertain; retry required");
	CoreData published;
	error = DecodeCoreData(bytes, core_id_, &published);
	if (error.ok() && output)
		*output = std::move(published);
	return error;
}
} // namespace native
} // namespace mister
