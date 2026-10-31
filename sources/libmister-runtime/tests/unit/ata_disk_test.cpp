// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/ata_disk.hpp"
#include "daemon/json.hpp"
#include "native/sha256.hpp"
#include <algorithm>
#include <cassert>
#include <cerrno>
#include <cstdio>
#include <dirent.h>
#include <fstream>
#include <map>
#include <signal.h>
#include <set>
#include <string>
#include <sys/stat.h>
#include <sys/wait.h>
#include <unistd.h>
using namespace mister;
using namespace mister::native;
namespace {
std::vector<unsigned char> Unhex(const std::string& hex)
{
	std::vector<unsigned char> bytes;
	for (std::size_t i = 0; i < hex.size(); i += 2)
		bytes.push_back(static_cast<unsigned char>(std::stoul(hex.substr(i, 2), nullptr, 16)));
	return bytes;
}
std::string Digest(const std::vector<unsigned char>& bytes)
{
	Sha256 hash;
	hash.Update(bytes.data(), bytes.size());
	return Sha256Hex(hash.Final());
}
std::set<std::string> Names(const std::string& directory)
{
	std::set<std::string> names;
	DIR* dir = opendir(directory.c_str());
	assert(dir);
	while (const dirent* entry = readdir(dir))
		if (std::string(entry->d_name) != "." && std::string(entry->d_name) != "..") names.insert(entry->d_name);
	closedir(dir);
	return names;
}
std::vector<unsigned char> Chunk(std::uint32_t index, std::uint32_t length)
{
	std::vector<unsigned char> bytes(length);
	for (std::uint32_t at = 0; at < length; ++at)
		bytes[at] = static_cast<unsigned char>(index * 31 + at * 13 + (at >> 8));
	return bytes;
}

std::uint32_t Crc32(const std::vector<unsigned char>& bytes)
{
	std::uint32_t crc = 0xffffffffu;
	for (unsigned char byte : bytes) {
		crc ^= byte;
		for (unsigned bit = 0; bit < 8; ++bit)
			crc = (crc >> 1) ^ ((crc & 1) ? 0xedb88320u : 0u);
	}
	return ~crc;
}

// The shared golden manifest: decode, re-encode, and reject every
// invalid vector.
void GoldenDelta()
{
	std::ifstream fixture("tests/fixtures/media-data-v1/ata-disk-delta.json");
	assert(fixture.good());
	const std::string text((std::istreambuf_iterator<char>(fixture)), std::istreambuf_iterator<char>());
	daemon::json::Value golden;
	std::string message;
	assert(daemon::json::ParseResponse(text, &golden, &message));
	std::map<std::string, daemon::json::Value> fields;
	for (const auto& pair : golden.object) fields[pair.first] = pair.second;
	const MediaDataIdentity id{fields["core_id"].string_value, fields["game_id"].string_value,
		fields["base_media_id"].string_value, 0};
	const auto base_size = static_cast<std::uint32_t>(fields["base_size"].integer_value);
	const auto record = Unhex(fields["record_hex"].string_value);
	AtaDiskDelta delta;
	assert(DecodeAtaDelta(record, id, base_size, &delta).ok());
	assert(delta.revision == fields["revision"].string_value && delta.revision == Digest(record));
	assert(MediaDataNamespace(id) == fields["namespace"].string_value);
	assert(delta.chunks.size() == fields["chunks"].array.size());
	std::vector<unsigned char> restored(base_size);
	for (std::uint32_t at = 0; at < base_size; ++at)
		restored[at] = static_cast<unsigned char>(at * 7 + (at >> 11));
	assert(Digest(restored) == fields["base_media_id"].string_value);
	for (std::size_t i = 0; i < delta.chunks.size(); ++i) {
		std::map<std::string, daemon::json::Value> chunk;
		for (const auto& pair : fields["chunks"].array[i].object) chunk[pair.first] = pair.second;
		assert(delta.chunks[i].index == chunk["index"].integer_value);
		assert(delta.chunks[i].sha256 == chunk["sha256"].string_value);
		assert(AtaDiskChunkLength(base_size, delta.chunks[i].index) == chunk["bytes"].integer_value);
		const auto bytes = Chunk(delta.chunks[i].index, AtaDiskChunkLength(base_size, delta.chunks[i].index));
		assert(Digest(bytes) == delta.chunks[i].sha256);
		std::copy(bytes.begin(), bytes.end(), restored.begin() +
			std::size_t(delta.chunks[i].index) * generated::FesComputerAtaDiskChunkBytes);
	}
	assert(Digest(restored) == fields["restored_sha256"].string_value);
	assert(Crc32(restored) == static_cast<std::uint32_t>(fields["restored_crc32"].integer_value));
	std::vector<unsigned char> encoded;
	assert(EncodeAtaDelta(delta, &encoded).ok() && encoded == record);
	for (const auto& row : fields["invalid"].array) {
		std::string bytes;
		for (const auto& pair : row.object) if (pair.first == "hex") bytes = pair.second.string_value;
		AtaDiskDelta rejected;
		assert(!DecodeAtaDelta(Unhex(bytes), id, base_size, &rejected).ok());
	}
	// Another game or base never takes the record.
	auto other = id;
	other.game_id = "ultima-vii";
	assert(DecodeAtaDelta(record, other, base_size, &delta).code == ErrorCode::incompatible_data);
	assert(DecodeAtaDelta(record, id, base_size + 512, &delta).code == ErrorCode::incompatible_data);
}

void WrappedChunkCount()
{
	const MediaDataIdentity identity{"fes.pc386", "ultima-vi", std::string(64, 'a'), 0};
	AtaDiskDelta delta;
	delta.identity = identity;
	delta.base_size = 1u << 20;
	delta.chunks = {{0, std::string(64, 'b')}};
	std::vector<unsigned char> record;
	assert(EncodeAtaDelta(delta, &record).ok());
	// On 32-bit ARM this count wraps 36 * count to 32, making a 206-byte
	// manifest appear to contain one chunk unless count is bounded first.
	record.resize(206);
	const std::uint32_t count = 119304648u;
	for (unsigned byte = 0; byte < 4; ++byte)
		record[120 + byte] = static_cast<unsigned char>(count >> (byte * 8));
	std::vector<unsigned char> prefix(record.begin(), record.end() - 32);
	const std::vector<unsigned char> checksum = Unhex(Digest(prefix));
	std::copy(checksum.begin(), checksum.end(), record.end() - 32);
	AtaDiskDelta rejected;
	assert(DecodeAtaDelta(record, identity, 1u << 20, &rejected).code == ErrorCode::corrupt_data);
}

void SizesAndMerge()
{
	assert(ValidAtaDiskSize(1u << 20) && ValidAtaDiskSize(128u << 20) && !ValidAtaDiskSize((1u << 20) - 512) &&
		!ValidAtaDiskSize((128u << 20) + 512) && !ValidAtaDiskSize((2u << 20) + 1));
	assert(AtaDiskChunkCount((2u << 20) + 1536) == 33 && AtaDiskChunkLength((2u << 20) + 1536, 32) == 1536 &&
		AtaDiskChunkLength((2u << 20) + 1536, 33) == 0);
	assert(ValidAtaDiskGeometry(2u << 20, 4, 16, 63) && !ValidAtaDiskGeometry(2u << 20, 5, 16, 63) &&
		!ValidAtaDiskGeometry(2u << 20, 4, 17, 63) && !ValidAtaDiskGeometry(2u << 20, 0, 16, 63));
	AtaDiskDelta existing;
	existing.chunks = {{1, std::string(64, 'a')}, {5, std::string(64, 'b')}};
	const auto merged = MergeAtaDelta(existing, {{0, std::string(64, 'c')}, {5, std::string(64, 'd')},
		{9, std::string(64, 'e')}});
	assert(merged.chunks.size() == 4 && merged.chunks[0].index == 0 && merged.chunks[1].sha256 == std::string(64, 'a') &&
		merged.chunks[2].sha256 == std::string(64, 'd') && merged.chunks[3].index == 9);
}

// Chunks are content-addressed and synced; publication checks the
// revision, then removes chunk files the manifest does not name.
void Store()
{
	char tmp[] = "/tmp/runtime-ata-delta-XXXXXX";
	assert(mkdtemp(tmp));
	const std::uint32_t size = (2u << 20) + 1536;
	const MediaDataIdentity id{"fes.pc386", "ultima-vi", std::string(64, 'a'), 0};
	std::unique_ptr<AtaDeltaFile> file;
	assert(!AtaDeltaFile::Open(tmp, id, size + 1, &file).ok());
	assert(AtaDeltaFile::Open(tmp, id, size, &file).ok());
	const std::string dir = std::string(tmp) + "/" + MediaDataNamespace(id);
	AtaDiskDelta delta;
	assert(file->Read(&delta).ok() && delta.revision == "absent" && delta.chunks.empty());
	assert(file->CheckWritable().ok());
	for (const auto& name : Names(dir)) assert(name.compare(0, 7, ".probe-") != 0 && name != ".probe-check");
	std::vector<AtaDeltaChunk> changed;
	for (std::uint32_t index : {0u, 5u, 32u}) {
		const auto bytes = Chunk(index, AtaDiskChunkLength(size, index));
		std::string sha;
		assert(file->WriteChunk(bytes.data(), bytes.size(), &sha).ok() && sha == Digest(bytes));
		changed.push_back({index, sha});
	}
	// A manifest naming an unstored chunk is refused.
	auto missing = MergeAtaDelta(delta, {{7, std::string(64, 'f')}});
	assert(file->Publish(missing, "absent", nullptr).code == ErrorCode::corrupt_data);
	AtaDiskDelta published;
	assert(file->Publish(MergeAtaDelta(delta, changed), "absent", &published).ok());
	assert(published.chunks.size() == 3 && published.revision != "absent");
	assert(file->Publish(MergeAtaDelta(delta, changed), "absent", nullptr).code == ErrorCode::stale_revision);
	std::vector<unsigned char> read;
	assert(file->ReadChunk(published.chunks[2], &read).ok() && read == Chunk(32, 1536));
	// The next save replaces chunk 5 and keeps 0 and 32; the old 5 is collected.
	const auto replaced = Chunk(55, 65536);
	std::string sha;
	assert(file->WriteChunk(replaced.data(), replaced.size(), &sha).ok());
	const std::string old_five = published.chunks[1].sha256;
	AtaDiskDelta second;
	assert(file->Publish(MergeAtaDelta(published, {{5, sha}}), published.revision, &second).ok());
	auto names = Names(dir);
	assert(names.count("record.bin") && names.count("chunk-" + sha) && !names.count("chunk-" + old_five) &&
		names.size() == 4);
	// A same-size replacement must not be published under the old digest.
	auto same_size_corrupt = replaced;
	same_size_corrupt[0] ^= 1;
	{ std::ofstream corrupt(dir + "/chunk-" + sha, std::ios::binary | std::ios::trunc);
		corrupt.write(reinterpret_cast<const char*>(same_size_corrupt.data()), same_size_corrupt.size()); }
	assert(file->Publish(second, second.revision, nullptr).code == ErrorCode::corrupt_data);
	// A corrupt chunk file is corruption, never silently replaced.
	{ std::ofstream corrupt(dir + "/chunk-" + sha, std::ios::binary | std::ios::trunc); corrupt << "bad"; }
	assert(file->ReadChunk(second.chunks[1], &read).code == ErrorCode::corrupt_data);
	assert(unlink((dir + "/chunk-" + sha).c_str()) == 0);
	assert(file->ReadChunk(second.chunks[1], &read).code == ErrorCode::corrupt_data);
	// Reopening removes a leftover private chunk temporary.
	{ std::ofstream leftover(dir + "/.chunk-123-4", std::ios::binary); leftover << "x"; }
	file.reset();
	assert(AtaDeltaFile::Open(tmp, id, size, &file).ok());
	assert(!Names(dir).count(".chunk-123-4"));
	assert(file->Read(&delta).ok() && delta.revision == second.revision);
	file.reset();
	for (const auto& name : Names(dir)) assert(unlink((dir + "/" + name).c_str()) == 0);
	assert(rmdir(dir.c_str()) == 0 && rmdir(tmp) == 0);
}

void BaseImage()
{
	char tmp[] = "/tmp/runtime-ata-base-XXXXXX";
	assert(mkdtemp(tmp));
	const std::string path = std::string(tmp) + "/disk.img";
	std::vector<unsigned char> bytes(1u << 20);
	for (std::size_t at = 0; at < bytes.size(); ++at) bytes[at] = static_cast<unsigned char>(at * 7);
	{ std::ofstream out(path, std::ios::binary); out.write(reinterpret_cast<const char*>(bytes.data()), bytes.size()); }
	std::unique_ptr<AtaBaseImage> image;
	assert(!AtaBaseImage::Open(path, (1u << 20) + 512, &image).ok());
	assert(AtaBaseImage::Open(path, 1u << 20, &image).ok());
	unsigned char window[512];
	assert(image->Read(4096, window, sizeof window).ok() && window[0] == bytes[4096] && window[511] == bytes[4607]);
	const std::string link = std::string(tmp) + "/link.img";
	assert(symlink(path.c_str(), link.c_str()) == 0);
	assert(!AtaBaseImage::Open(link, 1u << 20, &image).ok());
	const std::string fifo = std::string(tmp) + "/disk.fifo";
	assert(mkfifo(fifo.c_str(), 0600) == 0);
	const pid_t child = fork();
	assert(child >= 0);
	if (child == 0) {
		std::unique_ptr<AtaBaseImage> rejected;
		_exit(AtaBaseImage::Open(fifo, 1u << 20, &rejected).ok() ? 1 : 0);
	}
	int status = 0;
	bool finished = false;
	for (unsigned attempt = 0; attempt < 100; ++attempt) {
		const pid_t waited = waitpid(child, &status, WNOHANG);
		if (waited == child) {
			finished = true;
			break;
		}
		assert(waited == 0 || (waited < 0 && errno == EINTR));
		usleep(10000);
	}
	if (!finished) {
		assert(kill(child, SIGKILL) == 0);
		assert(waitpid(child, &status, 0) == child);
	}
	assert(finished && WIFEXITED(status) && WEXITSTATUS(status) == 0);
	image.reset();
	assert(unlink(fifo.c_str()) == 0 && unlink(link.c_str()) == 0 && unlink(path.c_str()) == 0 && rmdir(tmp) == 0);
}
} // namespace

int main()
{
	GoldenDelta();
	WrappedChunkCount();
	SizesAndMerge();
	Store();
	BaseImage();
	std::puts("ata_disk_test: golden delta, sizes, merge, chunk store, collection and base image passed");
}
