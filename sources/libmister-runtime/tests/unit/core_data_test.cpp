// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/core_data.hpp"
#include <regex>
#include <cassert>
#include <fstream>
#include <unistd.h>
#include <sys/stat.h>
#if defined(__linux__)
#include <sys/syscall.h>
#include <cerrno>
// Fault injection stays in the test executable. Production uses ordinary
// filesystem calls and exports no testing hook.
static int fail_sync_kind = 0;
extern "C" int fsync(int descriptor)
{
	struct stat state {};
	if (fail_sync_kind && fstat(descriptor, &state) == 0 &&
		((fail_sync_kind == 1 && S_ISREG(state.st_mode)) ||
			(fail_sync_kind == 2 && S_ISDIR(state.st_mode)))) {
		fail_sync_kind = 0;
		errno = EIO;
		return -1;
	}
	return static_cast<int>(syscall(SYS_fsync, descriptor));
}
#endif
using namespace mister;
using namespace mister::native;
std::vector<unsigned char> Unhex(const std::string& hex)
{
	std::vector<unsigned char> bytes;
	for (std::size_t i = 0; i < hex.size(); i += 2)
		bytes.push_back(static_cast<unsigned char>(std::stoul(hex.substr(i, 2), nullptr, 16)));
	return bytes;
}
void SharedRecords()
{
	std::ifstream fixture("tests/fixtures/core-persistence-v1/records.json");
	assert(fixture.good());
	std::string json((std::istreambuf_iterator<char>(fixture)), std::istreambuf_iterator<char>());
	const std::regex rows(R"rx(\{[^{}]*"hex"[^{}]*\})rx");
	const std::regex hex(R"rx("hex"\s*:\s*"([0-9a-f]*)")rx");
	const std::regex revision(R"rx("revision"\s*:\s*"([0-9a-f]{64})")rx");
	const std::regex words(R"rx("words"\s*:\s*\[\s*([0-9]+),\s*([0-9]+)\s*\])rx");
	std::size_t valid = 0, invalid = 0;
	for (std::sregex_iterator row(json.begin(), json.end(), rows), end; row != end; ++row) {
		const std::string text = row->str();
		std::smatch h, r, w;
		assert(std::regex_search(text, h, hex));
		auto bytes = Unhex(h[1].str());
		CoreData data;
		if (std::regex_search(text, r, revision)) {
			++valid;
			assert(DecodeCoreData(bytes, "fes.pong", &data).ok());
			assert(data.revision == r[1]);
			assert(std::regex_search(text, w, words));
			assert(data.paddle_speed == std::stoul(w[1]));
			assert(data.best_rally == std::stoul(w[2]));
			std::vector<unsigned char> encoded;
			assert(EncodeCoreData(data, &encoded).ok());
			assert(encoded == bytes);
		} else {
			++invalid;
			assert(!DecodeCoreData(bytes, "fes.pong", &data).ok());
		}
	}
	assert(valid == 3 && invalid >= 8);
}
int main()
{
	SharedRecords();
	const std::string id = "org.fes.pong";
	CoreData data;
	data.core_id = id;
	data.layout = {"fes.pong.progress", 1, 0};
	std::vector<unsigned char> bytes;
	assert(EncodeCoreData(data, &bytes).ok());
	assert(bytes.size() == 101);
	CoreData decoded;
	assert(DecodeCoreData(bytes, id, &decoded).ok());
	assert(decoded.paddle_speed == 1 && decoded.best_rally == 0);
	assert(decoded.revision.size() == 64);
	assert(DecodeCoreData(bytes, "other.core", &decoded).code == ErrorCode::incompatible_data);
	auto bad = bytes;
	bad.back() ^= 1;
	assert(DecodeCoreData(bad, id, &decoded).code == ErrorCode::corrupt_data);
	bad = bytes;
	bad.push_back(0);
	assert(!DecodeCoreData(bad, id, &decoded).ok());
	data.paddle_speed = 3;
	assert(!EncodeCoreData(data, &bad).ok());
	data.paddle_speed = 2;
	data.best_rally = 17;
	char tmp[] = "/tmp/runtime-core-data-XXXXXX";
	assert(mkdtemp(tmp));
	std::unique_ptr<CoreDataFile> file;
	assert(CoreDataFile::Open(tmp, id, &file).ok());
	assert(file->Read(&decoded).ok() && decoded.revision == "absent");
	assert(file->Persist(data, "absent", &decoded).ok());
	assert(decoded.paddle_speed == 2 && decoded.best_rally == 17);
	assert(file->Persist(data, "absent", nullptr).code == ErrorCode::stale_revision);
	auto revision = decoded.revision;
	data.paddle_speed = 0;
	assert(file->Persist(data, revision, &decoded).ok());
	assert(decoded.best_rally == 17 && decoded.revision != revision);
	std::unique_ptr<CoreDataFile> reopened;
	assert(CoreDataFile::Open(tmp, id, &reopened).ok());
	assert(reopened->Read(&decoded).ok() && decoded.paddle_speed == 0 && decoded.best_rally == 17);

#if defined(__linux__)
	// File-sync failure leaves old bytes; directory-sync failure after rename
	// leaves a complete new record with an explicitly reported uncertain result.
	revision = decoded.revision;
	data.paddle_speed = 1;
	fail_sync_kind = 1;
	assert(file->Persist(data, revision, nullptr).code == ErrorCode::save_failed);
	assert(
		reopened->Read(&decoded).ok() && decoded.revision == revision && decoded.paddle_speed == 0);
	fail_sync_kind = 2;
	auto uncertain = file->Persist(data, revision, nullptr);
	assert(uncertain.code == ErrorCode::save_failed &&
		   uncertain.message.find("uncertain") != std::string::npos);
	assert(reopened->Read(&decoded).ok() && decoded.paddle_speed == 1 && decoded.best_rally == 17 &&
		   decoded.revision != revision);
	assert(file->Persist(data, decoded.revision, &decoded).ok());
#endif
	const std::string dir = std::string(tmp) + "/" + CoreDataNamespace(id);
	const std::string root_link = std::string(tmp) + "/root-link";
	assert(symlink(tmp, root_link.c_str()) == 0);
	std::unique_ptr<CoreDataFile> rejected;
	assert(!CoreDataFile::Open(root_link, id, &rejected).ok());
	assert(unlink(root_link.c_str()) == 0);
	assert(rename(dir.c_str(), (dir + "-retained").c_str()) == 0);
	assert(symlink(".", dir.c_str()) == 0);
	assert(!CoreDataFile::Open(tmp, id, &rejected).ok());
	assert(file->Read(&decoded).ok());
	assert(file->Persist(data, decoded.revision, &decoded).ok());
	assert(unlink(dir.c_str()) == 0);
	assert(rename((dir + "-retained").c_str(), dir.c_str()) == 0);
	assert(rename((dir + "/record.bin").c_str(), (dir + "/old.bin").c_str()) == 0);
	assert(symlink("old.bin", (dir + "/record.bin").c_str()) == 0);
	assert(!file->Read(&decoded).ok());
	assert(!file->Persist(data, "absent", nullptr).ok());
	assert(unlink((dir + "/record.bin").c_str()) == 0);
	assert(mkdir((dir + "/record.bin").c_str(), 0700) == 0);
	assert(!file->Persist(data, "absent", nullptr).ok());
	assert(rmdir((dir + "/record.bin").c_str()) == 0);
	assert(rename((dir + "/old.bin").c_str(), (dir + "/record.bin").c_str()) == 0);
	assert(file->Read(&decoded).ok() && decoded.best_rally == 17);
	std::ofstream corrupt(dir + "/record.bin", std::ios::binary | std::ios::trunc);
	corrupt << "bad";
	corrupt.close();
	assert(file->Read(&decoded).code == ErrorCode::corrupt_data);
	assert(file->Persist(data, "absent", nullptr).code == ErrorCode::corrupt_data);
	assert(unlink((dir + "/record.bin").c_str()) == 0);
	file.reset();
	reopened.reset();
	assert(rmdir(dir.c_str()) == 0);
	assert(rmdir(tmp) == 0);
	puts("core_data_test: codec, identity, corruption, CAS, retained storage and no-follow passed");
}
