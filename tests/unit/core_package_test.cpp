// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/core_package.hpp"
#include "native/sha256.hpp"

#include <assert.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/stat.h>
#include <unistd.h>

#include <array>
#include <cerrno>
#include <cstdint>
#include <fstream>
#include <iterator>
#include <string>
#include <utility>
#include <vector>

namespace {

const char* const kFixtures = "tests/fixtures/core-bundle-v2";

std::string ReadFile(const std::string& path)
{
	std::ifstream input(path, std::ios::binary);
	assert(input.good());
	return std::string(std::istreambuf_iterator<char>(input),
		std::istreambuf_iterator<char>());
}

void WriteFile(const std::string& path, const std::string& bytes)
{
	const int descriptor = open(path.c_str(), O_WRONLY | O_CREAT | O_EXCL, 0600);
	assert(descriptor >= 0);
	std::size_t offset = 0;
	while (offset < bytes.size()) {
		const ssize_t count = write(descriptor, bytes.data() + offset,
			bytes.size() - offset);
		assert(count > 0);
		offset += static_cast<std::size_t>(count);
	}
	assert(close(descriptor) == 0);
}

struct TempDirectory {
	TempDirectory()
	{
		char pattern[] = "/tmp/libmister-core-package.XXXXXX";
		char* created = mkdtemp(pattern);
		assert(created != nullptr);
		path = created;
	}
	TempDirectory(const TempDirectory&) = delete;
	TempDirectory& operator=(const TempDirectory&) = delete;
	TempDirectory(TempDirectory&& other) noexcept
		: path(std::move(other.path)), entries(std::move(other.entries))
	{
		other.path.clear();
	}
	~TempDirectory()
	{
		if (path.empty()) return;
		for (const std::string& name : entries) {
			const std::string entry = path + "/" + name;
			struct stat metadata = {};
			if (lstat(entry.c_str(), &metadata) != 0) continue;
			if (S_ISDIR(metadata.st_mode)) assert(rmdir(entry.c_str()) == 0);
			else assert(unlink(entry.c_str()) == 0);
		}
		assert(rmdir(path.c_str()) == 0);
	}
	void Add(const std::string& name, const std::string& bytes)
	{
		WriteFile(path + "/" + name, bytes);
		entries.push_back(name);
	}
	std::string path;
	std::vector<std::string> entries;
};

TempDirectory PackageFromFixture(const std::string& manifest)
{
	TempDirectory package;
	package.Add("manifest.toml", ReadFile(std::string(kFixtures) +
		"/manifests/" + manifest + ".toml"));
	package.Add("core.rbf", ReadFile(std::string(kFixtures) +
		"/payloads/" + (manifest == "invalid-empty-payload" ?
		"empty.rbf" : "fes-fixture.rbf")));
	return package;
}

std::string Digest(const std::string& input, const std::vector<std::size_t>& chunks)
{
	mister::native::Sha256 hash;
	std::size_t offset = 0;
	for (const std::size_t chunk : chunks) {
		assert(chunk <= input.size() - offset);
		hash.Update(input.data() + offset, chunk);
		offset += chunk;
	}
	assert(offset == input.size());
	return mister::native::Sha256Hex(hash.Final());
}

void TestSha256StandardVectorsAndStreaming()
{
	struct Vector { std::string input, expected; };
	const std::vector<Vector> vectors = {
		{"", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{"abc", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
		{std::string(55, 'a'), "9f4390f8d30c2dd92ec9f095b65e2b9ae9b0a925a5258e241c9f1e910f734318"},
		{std::string(56, 'a'), "b35439a4ac6f0948b6d6f9e3c6af0f5f590ce20f1bde7090ef7970686ec6738a"},
		{std::string(64, 'a'), "ffe054fe7ae0cb6dc65c3af9b61d5209f439851db43d0ba5997337df154668eb"},
		{std::string(65, 'a'), "635361c48bb9eab14198e76ea8ab7f1a41685d6ad62aa9146d301d4f17eb0ae0"},
	};
	for (const Vector& vector : vectors) {
		assert(Digest(vector.input, {vector.input.size()}) == vector.expected);
		std::vector<std::size_t> chunks(vector.input.size(), 1);
		assert(Digest(vector.input, chunks) == vector.expected);
	}
}

void TestAllSharedFixturesAndExactIdentity()
{
	struct Valid { const char* name; const char* id; };
	const std::vector<Valid> valid = {
		{"valid-basic", "b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0"},
		{"valid-semver-prerelease-build", "03e7a0f933040eb392f73e14e37c4be9fa710249e47a24e49d67bc764a7e20a9"},
		{"valid-unknown-abi", "033fd02cab5ca1533d617e9adecbb2e975182672aebf669b9b9b7485f2a5d4ae"},
		{"valid-multibyte-bounds", "4e96d5f93d7c3477f8107e1a708e64c0daf607e8f6b628b9edfd15b784779d34"},
		{"valid-literal-strings", "5b3ec8ce5c12e347f1cce73ea858090c5204777a666d864a5b30672a34cbf2ab"},
		{"valid-dotted-keys", "ee3cedac57cfa50585540b8a8909aedb636a09c36dd9240b82a48305ae8b1a83"},
		{"valid-inline-tables", "ab48a770796990d08f3e8ba0404335af2f99cd06c4f6b1790f4e03db62f6b299"},
	};
	for (const Valid& fixture : valid) {
		TempDirectory package = PackageFromFixture(fixture.name);
		mister::native::OpenedCorePackage opened;
		const mister::Error error = mister::native::OpenCorePackage(
			package.path, fixture.id, &opened);
		if (!error.ok()) fprintf(stderr, "%s: %s\n", fixture.name, error.message.c_str());
		assert(error.ok());
		assert(opened.package_id == fixture.id);
		assert(opened.payload.fd() >= 0 && opened.payload.size() == 12);
		assert(opened.manifest_bytes == ReadFile(package.path + "/manifest.toml"));
	}

	const std::vector<std::string> invalid = {
		"invalid-missing-field", "invalid-wrong-type", "invalid-unknown-field",
		"invalid-duplicate-key", "invalid-duplicate-interface", "invalid-utf8",
		"invalid-empty-name", "invalid-oversized-name-utf8",
		"invalid-oversized-description-utf8", "invalid-boolean-size",
		"invalid-boolean-version", "invalid-float-size", "invalid-float-version",
		"invalid-payload-file", "invalid-payload-digest", "invalid-payload-size",
		"invalid-control-character", "invalid-malformed-repository",
		"invalid-malformed-repository-uri", "invalid-malformed-revision",
		"invalid-oversized-manifest", "invalid-empty-payload",
	};
	for (const std::string& fixture : invalid) {
		TempDirectory package = PackageFromFixture(fixture);
		mister::native::OpenedCorePackage opened;
		const mister::Error error = mister::native::OpenCorePackage(
			package.path, "", &opened);
		if (error.code != mister::ErrorCode::invalid_request)
			fprintf(stderr, "%s unexpectedly admitted: %s\n", fixture.c_str(), error.message.c_str());
		assert(error.code == mister::ErrorCode::invalid_request);
		assert(opened.payload.fd() == -1);
	}
}

void TestDescriptorFieldsAndCompatibilityAreSeparate()
{
	TempDirectory package = PackageFromFixture("valid-basic");
	mister::native::OpenedCorePackage opened;
	assert(mister::native::OpenCorePackage(package.path,
		"b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0",
		&opened).ok());
	const mister::native::CoreDescriptor& descriptor = opened.descriptor;
	assert(descriptor.format == 2 && descriptor.core.id == "fes.pong");
	assert(descriptor.core.name == "FES Pong" && descriptor.core.system.empty());
	assert(descriptor.target.platform == "de10_nano");
	assert(descriptor.target.device == "5CSEBA6U23I7");
	assert(descriptor.target.programming_profile == "fes-gp-v1");
	assert(descriptor.payload.file == "core.rbf" && descriptor.payload.size == 12);
	assert(descriptor.abi.id == "fes.simple-game");
	assert(descriptor.abi.major == 1 && descriptor.abi.minor == 0);
	assert(descriptor.interfaces.size() == 2);
	assert(descriptor.build.id == "0123456789abcdef0123456789abcdef");
	assert(mister::native::CheckCoreCompatibility(descriptor).ok());

	TempDirectory future = PackageFromFixture("valid-unknown-abi");
	mister::native::OpenedCorePackage unknown;
	assert(mister::native::OpenCorePackage(future.path, "", &unknown).ok());
	assert(mister::native::CheckCoreCompatibility(unknown.descriptor).code ==
		mister::ErrorCode::unsupported_abi);

	auto incompatible = descriptor;
	incompatible.abi.minor = 1;
	assert(!mister::native::CheckCoreCompatibility(incompatible).ok());
	incompatible = descriptor;
	incompatible.target.platform = "vendor.board";
	assert(!mister::native::CheckCoreCompatibility(incompatible).ok());
	incompatible = descriptor;
	incompatible.target.programming_profile = "development-contained-v1";
	assert(mister::native::CheckCoreCompatibility(incompatible).code ==
		mister::ErrorCode::unsupported_programming_profile);
	incompatible = descriptor;
	incompatible.target.programming_profile = "mister-v1";
	incompatible.abi = {"mister", 1, 0};
	incompatible.interfaces.clear();
	assert(mister::native::CheckCoreCompatibility(incompatible).ok());
	incompatible.abi.minor = 1;
	assert(!mister::native::CheckCoreCompatibility(incompatible).ok());
	incompatible = descriptor;
	incompatible.interfaces[0].major = 2;
	assert(!mister::native::CheckCoreCompatibility(incompatible).ok());
	incompatible = descriptor;
	incompatible.interfaces[0].minor = 1;
	assert(!mister::native::CheckCoreCompatibility(incompatible).ok());
	incompatible = descriptor;
	incompatible.interfaces.push_back({"vendor.optional", 7, 9, false});
	assert(mister::native::CheckCoreCompatibility(incompatible).ok());
	incompatible = descriptor;
	incompatible.interfaces.erase(incompatible.interfaces.begin());
	assert(!mister::native::CheckCoreCompatibility(incompatible).ok());
}

void TestSimpleComputerCompatibilityRequiresKeyboardVideoAndMedia()
{
	mister::native::CoreDescriptor descriptor;
	descriptor.target.platform = "de10_nano";
	descriptor.target.device = "5CSEBA6U23I7";
	descriptor.target.programming_profile = "fes-gp-v1";
	descriptor.abi = {"fes.simple-computer", 1, 0};
	descriptor.interfaces = {
		{"fes.keyboard", 1, 0, true},
		{"fes.video.fixed-720p60", 1, 0, true},
		{"fes.media.blob", 1, 0, true},
	};
	assert(mister::native::CheckCoreCompatibility(descriptor).ok());
	auto missing = descriptor;
	missing.interfaces.pop_back();
	assert(!mister::native::CheckCoreCompatibility(missing).ok());
	auto gamepad = descriptor;
	gamepad.interfaces.push_back({"fes.gamepad", 1, 0, true});
	assert(!mister::native::CheckCoreCompatibility(gamepad).ok());
}

void TestDirectoryAdmissionAndRetainedPayload()
{
	TempDirectory rooted = PackageFromFixture("valid-basic");
	mister::native::OpenedCorePackage rooted_opened;
	assert(mister::native::OpenCorePackage({"/tmp"}, rooted.path, "",
		&rooted_opened).ok());
	assert(mister::native::OpenCorePackage({"/usr/share/mister-runtime/core-packages"},
		rooted.path, "", &rooted_opened).code ==
		mister::ErrorCode::invalid_package);
	assert(mister::native::OpenCorePackage({rooted.path}, rooted.path,
		"", &rooted_opened).code == mister::ErrorCode::invalid_package);
	assert(mister::native::OpenCorePackage({"/tmp"},
		"/tmp/../tmp/" + rooted.path.substr(5), "", &rooted_opened).code ==
		mister::ErrorCode::invalid_package);
	assert(mister::native::OpenCorePackage({"/tmp"}, rooted.path + "/",
		"", &rooted_opened).code == mister::ErrorCode::invalid_package);
	TempDirectory symlink_root;
	assert(symlink(rooted.path.c_str(),
		(symlink_root.path + "/linked").c_str()) == 0);
	symlink_root.entries.push_back("linked");
	assert(mister::native::OpenCorePackage({symlink_root.path},
		symlink_root.path + "/linked", "", &rooted_opened).code ==
		mister::ErrorCode::invalid_package);

	TempDirectory extra = PackageFromFixture("valid-basic");
	extra.Add("unexpected", "x");
	mister::native::OpenedCorePackage opened;
	assert(mister::native::OpenCorePackage(extra.path, "", &opened).code ==
		mister::ErrorCode::invalid_request);

	TempDirectory symlink_package;
	assert(symlink((std::string(kFixtures) + "/manifests/valid-basic.toml").c_str(),
		(symlink_package.path + "/manifest.toml").c_str()) == 0);
	symlink_package.entries.push_back("manifest.toml");
	symlink_package.Add("core.rbf", "fes-fixture\n");
	assert(mister::native::OpenCorePackage(symlink_package.path, "", &opened).code ==
		mister::ErrorCode::invalid_request);

	TempDirectory fifo_package;
	fifo_package.Add("manifest.toml", ReadFile(std::string(kFixtures) +
		"/manifests/valid-basic.toml"));
	assert(mkfifo((fifo_package.path + "/core.rbf").c_str(), 0600) == 0);
	fifo_package.entries.push_back("core.rbf");
	assert(mister::native::OpenCorePackage(fifo_package.path, "", &opened).code ==
		mister::ErrorCode::invalid_request);

	TempDirectory retained = PackageFromFixture("valid-basic");
	assert(mister::native::OpenCorePackage(retained.path, "", &opened).ok());
	const int retained_fd = opened.payload.fd();
	assert(rename((retained.path + "/core.rbf").c_str(),
		(retained.path + "/original.rbf").c_str()) == 0);
	retained.entries[1] = "original.rbf";
	retained.Add("core.rbf", "replacement\n");
	char bytes[12] = {};
	assert(pread(retained_fd, bytes, sizeof(bytes), 0) == 12);
	assert(std::string(bytes, sizeof(bytes)) == "fes-fixture\n");
}

void TestExpectedIdentityAndParserExceptions()
{
	TempDirectory package = PackageFromFixture("valid-basic");
	mister::native::OpenedCorePackage opened;
	assert(mister::native::OpenCorePackage(package.path,
		std::string(64, '0'), &opened).code == mister::ErrorCode::invalid_request);
	assert(opened.payload.fd() == -1);

	std::string manifest = ReadFile(package.path + "/manifest.toml");
	const std::string marker = "name = \"FES Pong\"";
	const std::size_t position = manifest.find(marker);
	assert(position != std::string::npos);
	std::string nested = "name = ";
	for (int depth = 0; depth < 70; ++depth) nested += "[";
	nested += "\"FES Pong\"";
	for (int depth = 0; depth < 70; ++depth) nested += "]";
	manifest.replace(position, marker.size(), nested);
	assert(unlink((package.path + "/manifest.toml").c_str()) == 0);
	package.entries.erase(package.entries.begin());
	package.Add("manifest.toml", manifest);
	const mister::Error error = mister::native::OpenCorePackage(package.path, "", &opened);
	assert(error.code == mister::ErrorCode::invalid_request);
	assert(error.message.find("parse") != std::string::npos);
}

void TestRepositoryMatchesSharedRfc3986Contract()
{
	const std::string original = "https://example.invalid/fes-pong";
	struct Case { const char* repository; bool valid; };
	const std::vector<Case> cases = {
		{"https://example.invalid/repo@rev", true},
		{"https://example.invalid/a?x=@ok#f/@", true},
		{"https://[::1]/repo", true},
		{"https://[v1.alpha]/repo", true},
		{"https://:80/path", true},
		{"https://example.invalid:/path", true},
		{"https://", false},
		{"https:///path", false},
		{"https://?q", false},
		{"https://#fragment", false},
		{"https://example.invalid/path[bad]", false},
		{"https://example.invalid/a#b#c", false},
		{"https://example.invalid:port/path", false},
		{"https://example.invalid/%zz", false},
		{"https://[", false},
		{"https://user@example.invalid/repo", false},
	};
	for (const Case& item : cases) {
		TempDirectory package = PackageFromFixture("valid-basic");
		std::string manifest = ReadFile(package.path + "/manifest.toml");
		const std::size_t position = manifest.find(original);
		assert(position != std::string::npos);
		manifest.replace(position, original.size(), item.repository);
		assert(unlink((package.path + "/manifest.toml").c_str()) == 0);
		package.entries.erase(package.entries.begin());
		package.Add("manifest.toml", manifest);
		mister::native::OpenedCorePackage opened;
		const mister::Error error = mister::native::OpenCorePackage(package.path, "", &opened);
		if (error.ok() != item.valid)
			fprintf(stderr, "repository parity mismatch for %s: %s\n",
				item.repository, error.message.c_str());
		assert(error.ok() == item.valid);
	}
}

void TestCoreSystemPresenceIsValidatedAndFesGpRequiresOmission()
{
	const std::string marker = "version = \"0.1.0\"";
	{
		TempDirectory package = PackageFromFixture("valid-basic");
		std::string manifest = ReadFile(package.path + "/manifest.toml");
		const std::size_t position = manifest.find(marker);
		assert(position != std::string::npos);
		manifest.insert(position + marker.size(), "\nsystem = \"\"");
		assert(unlink((package.path + "/manifest.toml").c_str()) == 0);
		package.entries.erase(package.entries.begin());
		package.Add("manifest.toml", manifest);
		mister::native::OpenedCorePackage opened;
		assert(mister::native::OpenCorePackage(package.path, "", &opened).code ==
			mister::ErrorCode::invalid_request);
	}
	{
		TempDirectory package = PackageFromFixture("valid-basic");
		std::string manifest = ReadFile(package.path + "/manifest.toml");
		const std::size_t position = manifest.find(marker);
		assert(position != std::string::npos);
		manifest.insert(position + marker.size(), "\nsystem = \"pong\"");
		assert(unlink((package.path + "/manifest.toml").c_str()) == 0);
		package.entries.erase(package.entries.begin());
		package.Add("manifest.toml", manifest);
		mister::native::OpenedCorePackage opened;
		assert(mister::native::OpenCorePackage(package.path, "", &opened).ok());
		assert(opened.descriptor.core.system == "pong");
		assert(mister::native::CheckCoreCompatibility(opened.descriptor).code ==
			mister::ErrorCode::unsupported_abi);
	}
}

void TestMissingOrNonTableCoreIsRejectedWithoutChangingResult()
{
	auto expect_invalid = [](const std::string& manifest) {
		TempDirectory package = PackageFromFixture("valid-basic");
		assert(unlink((package.path + "/manifest.toml").c_str()) == 0);
		package.entries.erase(package.entries.begin());
		package.Add("manifest.toml", manifest);
		mister::native::OpenedCorePackage opened;
		opened.descriptor.core.id = "unchanged-core";
		opened.manifest_bytes = "unchanged-manifest";
		opened.package_id = "unchanged-package";
		const mister::Error error =
			mister::native::OpenCorePackage(package.path, "", &opened);
		assert(error.code == mister::ErrorCode::invalid_request);
		assert(opened.descriptor.core.id == "unchanged-core");
		assert(opened.manifest_bytes == "unchanged-manifest");
		assert(opened.payload.fd() == -1);
		assert(opened.package_id == "unchanged-package");
	};

	const std::string original = ReadFile(std::string(kFixtures) +
		"/manifests/valid-basic.toml");
	const std::size_t core_begin = original.find("[core]\n");
	const std::size_t core_end = original.find("[target]\n", core_begin);
	assert(core_begin != std::string::npos && core_end != std::string::npos);
	std::string missing = original;
	missing.erase(core_begin, core_end - core_begin);
	expect_invalid(missing);
	std::string non_table = original;
	non_table.replace(core_begin, core_end - core_begin, "core = \"not-a-table\"\n\n");
	expect_invalid(non_table);
}

} // namespace

int main()
{
	TestSha256StandardVectorsAndStreaming();
	TestAllSharedFixturesAndExactIdentity();
	TestDescriptorFieldsAndCompatibilityAreSeparate();
	TestSimpleComputerCompatibilityRequiresKeyboardVideoAndMedia();
	TestDirectoryAdmissionAndRetainedPayload();
	TestExpectedIdentityAndParserExceptions();
	TestRepositoryMatchesSharedRfc3986Contract();
	TestCoreSystemPresenceIsValidatedAndFesGpRequiresOmission();
	TestMissingOrNonTableCoreIsRejectedWithoutChangingResult();
	puts("core_package_test: 9 groups passed");
	return 0;
}
