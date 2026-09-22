// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "capture_diagnostic.hpp"
#include "native/artifacts.hpp"
#include "native/hardware.hpp"
#include "native/generated/fes_simple_computer.hpp"

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/stat.h>
#include <unistd.h>

#include <string>
#include <vector>
#include <functional>

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

void TestArtifactOpenEmitsCapFdEvents()
{
	mister_test::CaptureDiagnostic capture;
	mister::DiagnosticInstall install(&capture);
	TempDirectory temporary;
	const std::string path = temporary.File("core.rbf", 4);
	mister::native::PosixArtifactOpener opener;
	mister::native::Artifact artifact;
	assert(opener.Open("/definitely/missing/libmister", 0, &artifact).code ==
		mister::ErrorCode::io_failed);
	assert(opener.Open(path, 4, &artifact).ok());
	assert(capture.Count("cap.fd.open") == 2);
	const std::vector<mister::DiagnosticEvent> events = capture.events();
	assert(mister_test::HasBool(events[0], "ok", false));
	assert(mister_test::HasBool(events[1], "ok", true));
	assert(mister_test::HasString(events[1], "path", path));
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




} // namespace

void TestComputerMediaIsBoundedOpaqueRegularFile()
{
	TempDirectory d;
	std::vector<std::uint8_t> bytes;
	for (const auto& item : std::vector<std::pair<std::string, std::size_t>>{
		{"zx81.p", 1}, {"coleco.bin", 3}, {"cartridge", 16384}}) {
		const std::string path = d.File(item.first, item.second);
		const int fd = open(path.c_str(), O_WRONLY);
		const unsigned char first = 0xc3;
		assert(write(fd, &first, 1) == 1);
		assert(close(fd) == 0);
		assert(mister::native::ReadComputerMedia(path, &bytes).ok());
		assert(bytes.size() == item.second && bytes[0] == 0xc3);
	}
	for (const std::size_t size : {0u, 16385u}) {
		const std::string path = d.File("invalid" + std::to_string(size), size);
		assert(!mister::native::ReadComputerMedia(path, &bytes).ok());
	}
	const std::string fifo = d.path + "/fifo";
	assert(mkfifo(fifo.c_str(), 0600) == 0);
	d.files.push_back(fifo);
	assert(!mister::native::ReadComputerMedia(fifo, &bytes).ok());
	const std::string link = d.path + "/link";
	assert(symlink(d.files[0].c_str(), link.c_str()) == 0);
	d.files.push_back(link);
	assert(!mister::native::ReadComputerMedia(link, &bytes).ok());
	assert(!mister::native::ReadComputerMedia(d.path, &bytes).ok());
	assert(!mister::native::ReadComputerMedia(d.files[0] + std::string("\0x", 2), &bytes).ok());
}

class SnapshotClock final : public mister::native::Clock {
public:
	std::uint64_t NowMs() const override
	{
		if (hook) hook(calls);
		return calls++;
	}
	mutable std::uint64_t calls = 0;
	std::function<void(std::uint64_t)> hook;
};

void TestStreamSnapshotBoundsCRCAndPrivateCopy()
{
	using namespace mister::native::generated;
	TempDirectory d;
	const std::string path = d.File("crc", 9);
	const int fd = open(path.c_str(), O_WRONLY);
	assert(write(fd, "123456789", 9) == 9);
	assert(close(fd) == 0);
	SnapshotClock clock;
	mister::native::ComputerMediaSnapshot snapshot;
	assert(snapshot.Prepare(path, 1, 32768, clock, 100).ok());
	assert(snapshot.size() == 9 && snapshot.crc32() == 0xcbf43926u);
	assert(truncate(path.c_str(), 0) == 0);
	unsigned char result[9] = {};
	assert(snapshot.Read(0, result, 9, clock, 100).ok());
	assert(!snapshot.Read(0, result, 9, clock, 0).ok());
	assert(std::string(reinterpret_cast<char*>(result), 9) == "123456789");
	assert(!snapshot.Read(1, result, 9, clock, 100).ok());
	assert(!snapshot.Prepare(path, 1, 32768, clock, 100).ok());
	for (std::uint32_t size : {0u, 1u, 511u, 512u, 513u, 16385u, 32768u,
			32769u, FesSimpleComputerMediaStreamMaxBytes, FesSimpleComputerMediaStreamMaxBytes + 1}) {
		const std::string file = d.File("size" + std::to_string(size), size);
		SnapshotClock steady;
		mister::native::ComputerMediaSnapshot candidate;
		assert(candidate.Prepare(file, 1, FesSimpleComputerMediaStreamMaxBytes,
			steady, 1000000).ok() == (size > 0 && size <= FesSimpleComputerMediaStreamMaxBytes));
		if (size > 32768) {
			mister::native::ComputerMediaSnapshot small;
			assert(!small.Prepare(file, 1, 32768, steady, 1000000).ok());
		}
	}
}

void TestStreamSnapshotChangedLengthAndDeadline()
{
	TempDirectory d;
	for (const auto size : {1u, 1025u}) {
		const auto path = d.File("changing" + std::to_string(size), 1024);
		SnapshotClock clock;
		// Change after the first 512-byte read, before the next read / EOF check.
		clock.hook = [&](std::uint64_t call) {
			if (call == 2) assert(truncate(path.c_str(), size) == 0);
		};
		mister::native::ComputerMediaSnapshot candidate;
		assert(!candidate.Prepare(path, 1, 32768, clock, 100).ok());
		assert(candidate.size() == 0);
	}
	const auto path = d.File("deadline", 1024);
	for (const auto deadline : {0u, 1u, 2u, 3u, 4u, 5u}) {
		SnapshotClock clock;
		mister::native::ComputerMediaSnapshot candidate;
		assert(!candidate.Prepare(path, 1, 32768, clock, deadline).ok());
		assert(candidate.size() == 0);
	}
}

int main()
{
	TestStreamSnapshotBoundsCRCAndPrivateCopy();
	TestStreamSnapshotChangedLengthAndDeadline();
	TestComputerMediaIsBoundedOpaqueRegularFile();
	TestMissingDirectoryAndZeroLengthAreRejected();
	TestOversizeRbfIsRejected();
	TestEmbeddedNulCannotSelectATruncatedPosixPath();
	TestArtifactOpenEmitsCapFdEvents();
	TestAbsoluteAndRelativeOpenUseTheSameFileAdmission();
	puts("artifacts_test: 11 passed");
	return 0;
}
