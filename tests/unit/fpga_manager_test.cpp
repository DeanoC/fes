// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "fake_mmio.hpp"
#include "native/artifacts.hpp"
#include "native/linux/fpga_manager.hpp"

#include <assert.h>
#include <fcntl.h>
#include <stdio.h>
#include <unistd.h>

#include <cstdint>
#include <string>
#include <vector>

namespace {

class FixedClock final : public mister::native::Clock {
public:
	explicit FixedClock(std::uint64_t now) : now_(now) {}
	std::uint64_t NowMs() const override { return now_; }
	std::uint64_t now_;
};

struct TempArtifact {
	explicit TempArtifact(std::size_t size)
	{
		char pattern[] = "/tmp/libmister-fpga.XXXXXX";
		const int descriptor = mkstemp(pattern);
		assert(descriptor >= 0);
		path = pattern;
		std::vector<unsigned char> bytes(size);
		for (std::size_t index = 0; index < size; ++index)
			bytes[index] = static_cast<unsigned char>(index);
		assert(write(descriptor, bytes.data(), bytes.size()) ==
			static_cast<ssize_t>(bytes.size()));
		assert(close(descriptor) == 0);
		mister::native::PosixArtifactOpener opener;
		assert(opener.Open(path, 32u * 1024u * 1024u, &artifact).ok());
	}
	~TempArtifact() { assert(unlink(path.c_str()) == 0); }
	std::string path;
	mister::native::Artifact artifact;
};

void SetSuccessfulFinish(mister_test::FakeMmio& mmio)
{
	mmio.values[mister::native::kFpgaMonitorAddress] = 3;
	mmio.values[mister::native::kFpgaStatusAddress] = 4;
}

void TestProgramsInFourKiBChunksAndChecksFinalState()
{
	TempArtifact input(5000);
	mister_test::FakeMmio mmio;
	SetSuccessfulFinish(mmio);
	FixedClock clock(1);
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const mister::native::NativeResult result = manager.Program(input.artifact, 100);
	assert(result.error.ok() && result.mutation_attempted);
	std::size_t data_writes = 0;
	for (const auto& write : mmio.writes) {
		if (write.offset == mister::native::kFpgaDataAddress) ++data_writes;
	}
	assert(data_writes == 1250);
	assert(mmio.reads.size() >= 3);
}

void TestPreWriteAndFirstWriteFailuresAreClassified()
{
	TempArtifact input(4);
	FixedClock clock(1);
	mister_test::FakeMmio before;
	before.read_error = {mister::ErrorCode::io_failed, "read"};
	mister::native::LinuxFpgaManager before_manager(before, clock);
	const auto before_result = before_manager.Program(input.artifact, 100);
	assert(before_result.error.code == mister::ErrorCode::program_failed);
	assert(!before_result.mutation_attempted);
	mister_test::FakeMmio after;
	after.write_error = {mister::ErrorCode::io_failed, "write"};
	mister::native::LinuxFpgaManager after_manager(after, clock);
	const auto after_result = after_manager.Program(input.artifact, 100);
	assert(after_result.error.code == mister::ErrorCode::program_failed);
	assert(after_result.mutation_attempted);
}

void TestDeadlineUsesProgramFailureWithExactMessage()
{
	TempArtifact input(4);
	mister_test::FakeMmio mmio;
	FixedClock clock(10);
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact, 10);
	assert(result.error.code == mister::ErrorCode::program_failed);
	assert(result.error.message == "deadline exceeded");
	assert(!result.mutation_attempted);
}

void TestMissingFinalEvidenceFailsAfterMutation()
{
	TempArtifact input(4);
	mister_test::FakeMmio mmio;
	mmio.values[mister::native::kFpgaMonitorAddress] = 1;
	mmio.values[mister::native::kFpgaStatusAddress] = 4;
	FixedClock clock(1);
	mister::native::LinuxFpgaManager manager(mmio, clock);
	const auto result = manager.Program(input.artifact, 100);
	assert(result.error.code == mister::ErrorCode::program_failed);
	assert(result.mutation_attempted);
}

} // namespace

int main()
{
	TestProgramsInFourKiBChunksAndChecksFinalState();
	TestPreWriteAndFirstWriteFailuresAreClassified();
	TestDeadlineUsesProgramFailureWithExactMessage();
	TestMissingFinalEvidenceFailsAfterMutation();
	puts("fpga_manager_test: 4 passed");
	return 0;
}
