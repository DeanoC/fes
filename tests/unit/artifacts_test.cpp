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

} // namespace

int main()
{
	TestMissingDirectoryAndZeroLengthAreRejected();
	TestOversizeRbfIsRejected();
	TestCompleteSetFailureDoesNotAssignOutput();
	TestMultiFilePreflightRetainsAndClosesDescriptors();
	puts("artifacts_test: 4 passed");
	return 0;
}
