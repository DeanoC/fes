// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/artifacts.hpp"

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/stat.h>
#include <unistd.h>

#include <string>
#include <vector>

namespace {

struct TempDirectory {
	TempDirectory()
	{
		char pattern[] = "/tmp/libmister-artifacts.XXXXXX";
		char* created = mkdtemp(pattern);
		assert(created != nullptr);
		path = created;
	}
	~TempDirectory()
	{
		for (const std::string& file : files) assert(unlink(file.c_str()) == 0);
		assert(rmdir(path.c_str()) == 0);
	}
	std::string File(const std::string& name, std::size_t size)
	{
		const std::string file = path + "/" + name;
		const int fd = open(file.c_str(), O_WRONLY | O_CREAT | O_EXCL, 0600);
		assert(fd >= 0);
		assert(ftruncate(fd, static_cast<off_t>(size)) == 0);
		assert(close(fd) == 0);
		files.push_back(file);
		return file;
	}
	std::string path;
	std::vector<std::string> files;
};

void TestMissingDirectoryAndZeroLengthAreRejected()
{
	mister::native::PosixArtifactOpener opener;
	mister::native::Artifact artifact;
	assert(opener.Open("/definitely/missing/libmister", 0, &artifact).code ==
		mister::ErrorCode::io_failed);
	TempDirectory temporary;
	assert(opener.Open(temporary.path, 0, &artifact).code ==
		mister::ErrorCode::io_failed);
	const std::string zero = temporary.File("zero", 0);
	assert(opener.Open(zero, 0, &artifact).code == mister::ErrorCode::io_failed);
}

void TestOversizeRbfIsRejected()
{
	TempDirectory temporary;
	const std::string path = temporary.File("oversize.rbf", 32u * 1024u * 1024u + 1u);
	mister::native::PosixArtifactOpener opener;
	mister::native::Artifact artifact;
	assert(opener.Open(path, 32u * 1024u * 1024u, &artifact).code ==
		mister::ErrorCode::io_failed);
}

void TestEmbeddedNulCannotSelectATruncatedPosixPath()
{
	TempDirectory temporary;
	const std::string existing = temporary.File("existing.rbf", 4);
	const std::string deceptive = existing + std::string("\0missing.rbf", 12);
	mister::native::PosixArtifactOpener opener;
	mister::native::Artifact artifact;
	assert(opener.Open(deceptive, 0, &artifact).code == mister::ErrorCode::io_failed);
	assert(artifact.fd() == -1);
}

void TestAbsoluteAndRelativeOpenUseTheSameFileAdmission()
{
	TempDirectory temporary;
	const std::string path = temporary.File("member.rbf", 4);
	const int directory = open(temporary.path.c_str(), O_RDONLY | O_DIRECTORY | O_CLOEXEC);
	assert(directory >= 0);
	mister::native::PosixArtifactOpener opener;
	mister::native::Artifact absolute;
	mister::native::Artifact relative;
	assert(opener.Open(path, 3, &absolute).code == mister::ErrorCode::io_failed);
	assert(opener.OpenRelative(directory, temporary.path, "member.rbf", 3,
		&relative).code == mister::ErrorCode::io_failed);
	assert(opener.Open(path, 4, &absolute).ok());
	assert(opener.OpenRelative(directory, temporary.path, "member.rbf", 4,
		&relative).ok());
	assert(absolute.size() == 4 && relative.size() == 4);
	assert(relative.path() == path);
	assert(close(directory) == 0);
}

class FailingOpener final : public mister::native::ArtifactOpener {
public:
	explicit FailingOpener(int fail_call) : fail_call_(fail_call), calls(0), delegate() {}
	mister::Error Open(const std::string& path, std::uint64_t maximum,
		mister::native::Artifact* artifact) override
	{
		++calls;
		if (calls == fail_call_) return {mister::ErrorCode::io_failed, "injected open"};
		return delegate.Open(path, maximum, artifact);
	}
	int fail_call_;
	int calls;
	mister::native::PosixArtifactOpener delegate;
};

void TestCompleteSetFailureDoesNotAssignOutput()
{
	TempDirectory temporary;
	mister::PreparedLaunch launch;
	launch.rbf = temporary.File("core.rbf", 4);
	launch.media.push_back({1, temporary.File("game.bin", 3)});
	FailingOpener opener(2);
	mister::native::ArtifactSet output;
	assert(mister::native::OpenLaunchArtifacts(launch, opener, &output).code ==
		mister::ErrorCode::io_failed);
	assert(output.rbf.fd() == -1);
	assert(output.media.empty());
}

void TestMultiFilePreflightRetainsAndClosesDescriptors()
{
	TempDirectory temporary;
	mister::PreparedLaunch launch;
	launch.rbf = temporary.File("core.rbf", 4);
	launch.media.push_back({2, temporary.File("two.bin", 2)});
	launch.media.push_back({0, temporary.File("zero.bin", 1)});
	mister::native::PosixArtifactOpener opener;
	int descriptors[3] = {-1, -1, -1};
	{
		mister::native::ArtifactSet output;
		assert(mister::native::OpenLaunchArtifacts(launch, opener, &output).ok());
		assert(output.rbf.size() == 4 && output.media.size() == 2);
		descriptors[0] = output.rbf.fd();
		descriptors[1] = output.media[0].artifact.fd();
		descriptors[2] = output.media[1].artifact.fd();
		for (int descriptor : descriptors) assert(fcntl(descriptor, F_GETFD) >= 0);
	}
	for (int descriptor : descriptors) {
		errno = 0;
		assert(fcntl(descriptor, F_GETFD) == -1 && errno == EBADF);
	}
}

void TestSaveFileAdmissionAndAtomicRetry()
{
	TempDirectory d;
	const std::string path = d.path + "/game.srm";
	mister::native::SaveFile save;
	assert(save.Prepare(path, 2048).ok());
	assert(save.bytes().empty());
	assert(access(path.c_str(), F_OK) != 0);
	std::vector<unsigned char> data(2048, 0x5a);
	assert(save.Persist(data).ok());
	d.files.push_back(path);
	mister::native::SaveFile loaded;
	assert(loaded.Prepare(path, 2048).ok());
	assert(loaded.bytes() == data);
	mister::native::SaveFile wrong;
	assert(!wrong.Prepare(path, 4096).ok());
	mister::native::SaveFile short_save;
	assert(!short_save.Prepare(path, 1024).ok());
	const std::string link = d.path + "/link.srm";
	assert(symlink(path.c_str(), link.c_str()) == 0);
	d.files.push_back(link);
	mister::native::SaveFile symlink_save;
	assert(!symlink_save.Prepare(link, 2048).ok());
	assert(!wrong.Prepare(d.path + "/missing/game.srm", 2048).ok());
	assert(!save.Persist(std::vector<unsigned char>(1024)).ok());
	// Replacement failure leaves the captured bytes available for a later retry.
	const std::string old = path + ".old";
	assert(rename(path.c_str(), old.c_str()) == 0);
	assert(mkdir(path.c_str(), 0700) == 0);
	data[0] = 0xa5;
	assert(!save.Persist(data).ok());
	mister::native::SaveFile intact;
	assert(intact.Prepare(old, 2048).ok());
	assert(intact.bytes()[0] == 0x5a);
	assert(rmdir(path.c_str()) == 0);
	assert(rename(old.c_str(), path.c_str()) == 0);
	assert(save.Persist(data).ok());
	mister::native::SaveFile final;
	assert(final.Prepare(path, 2048).ok());
	assert(final.bytes() == data);
}

} // namespace

int main()
{
	TestSaveFileAdmissionAndAtomicRetry();
	TestMissingDirectoryAndZeroLengthAreRejected();
	TestOversizeRbfIsRejected();
	TestEmbeddedNulCannotSelectATruncatedPosixPath();
	TestAbsoluteAndRelativeOpenUseTheSameFileAdmission();
	TestCompleteSetFailureDoesNotAssignOutput();
	TestMultiFilePreflightRetainsAndClosesDescriptors();
	puts("artifacts_test: 7 passed");
	return 0;
}
