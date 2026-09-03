// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "fake_spi.hpp"
#include "native/artifacts.hpp"
#include "native/core_loader.hpp"

#include <assert.h>
#include <fcntl.h>
#include <stdio.h>
#include <unistd.h>

#include <string>
#include <vector>

namespace {

struct TempFile {
	explicit TempFile(std::vector<unsigned char> bytes = {})
	{
		char pattern[] = "/tmp/libmister-core-loader.XXXXXX.bin";
		const int descriptor = mkstemps(pattern, 4);
		assert(descriptor >= 0);
		path = pattern;
		if (bytes.empty()) {
			bytes.resize(5000);
			for (std::size_t index = 0; index < bytes.size(); ++index)
				bytes[index] = static_cast<unsigned char>(index);
		}
		assert(write(descriptor, bytes.data(), bytes.size()) ==
			static_cast<ssize_t>(bytes.size()));
		assert(close(descriptor) == 0);
	}
	~TempFile() { assert(unlink(path.c_str()) == 0); }
	std::string path;
};

class ScriptedArtifactReader final : public mister::native::ArtifactReader {
public:
	explicit ScriptedArtifactReader(std::size_t fail_call)
		: fail_call_(fail_call) {}
	mister::Error Read(const mister::native::Artifact& artifact,
		std::uint64_t offset, unsigned char* bytes, std::size_t count) override
	{
		const std::size_t call = calls++;
		offsets.push_back(offset);
		if (call == fail_call_)
			return {mister::ErrorCode::io_failed,
				"scripted artifact read failure"};
		const ssize_t actual = pread(artifact.fd(), bytes, count,
			static_cast<off_t>(offset));
		if (actual != static_cast<ssize_t>(count))
			return {mister::ErrorCode::io_failed, "test artifact read failed"};
		return {};
	}
	std::size_t fail_call_;
	std::size_t calls = 0;
	std::vector<std::uint64_t> offsets;
};

void TestProbeUsesCoreNameCommandAndParsesPrintableName()
{
	mister_test::FakeSpi spi;
	spi.observed_core = "TEST CART";
	mister::native::CoreLoader loader(spi);
	std::string observed;
	assert(loader.Probe(&observed, 1234).ok());
	assert(observed == "TEST CART");
	assert(spi.calls.size() == 1);
	assert(spi.calls[0].target == mister::native::kUserIoTarget);
	assert(spi.calls[0].request.front() == 0x0014);
	assert(spi.calls[0].request.size() == 66);
	assert(spi.calls[0].deadline == 1234);
}

void TestRecipeResetAndStatusPrimitivesUseExactWholeStatusWords()
{
	mister_test::FakeSpi spi;
	mister::native::CoreLoader loader(spi);
	const mister::CoreRecipe recipe = {0x1111, 0x2222, 0x3333,
		mister::FileWireFormat::little_endian_byte_pairs};
	assert(loader.AssertReset(recipe, 71).ok());
	assert(loader.ApplyInitialStatus(recipe, 72).ok());
	assert(loader.ReleaseReset(recipe, 73).ok());
	assert(spi.calls.size() == 3);
	const std::vector<std::uint16_t> asserted = {
		0x001e, 0x1111, 0x0000, 0x0000, 0x0000,
		0x0000, 0x0000, 0x0000, 0x0000};
	const std::vector<std::uint16_t> initial = {
		0x001e, 0x2222, 0x0000, 0x0000, 0x0000,
		0x0000, 0x0000, 0x0000, 0x0000};
	const std::vector<std::uint16_t> released = {
		0x001e, 0x3333, 0x0000, 0x0000, 0x0000,
		0x0000, 0x0000, 0x0000, 0x0000};
	assert(spi.calls[0].target == mister::native::kUserIoTarget);
	assert(spi.calls[0].request == asserted);
	assert(spi.calls[0].deadline == 71);
	assert(spi.calls[1].target == mister::native::kUserIoTarget);
	assert(spi.calls[1].request == initial);
	assert(spi.calls[1].deadline == 72);
	assert(spi.calls[2].target == mister::native::kUserIoTarget);
	assert(spi.calls[2].request == released);
	assert(spi.calls[2].deadline == 73);
}

void TestEachRecipePrimitiveReturnsItsDirectFailureWithoutLaterCalls()
{
	const mister::CoreRecipe recipe = {0x0001, 0x0001, 0x0000,
		mister::FileWireFormat::little_endian_byte_pairs};
	for (int operation = 0; operation != 3; ++operation) {
		mister_test::FakeSpi spi;
		spi.errors.push_back({mister::ErrorCode::io_failed,
			"scripted status failure"});
		mister::native::CoreLoader loader(spi);
		mister::Error error;
		if (operation == 0) error = loader.AssertReset(recipe, 81);
		if (operation == 1) error = loader.ApplyInitialStatus(recipe, 82);
		if (operation == 2) error = loader.ReleaseReset(recipe, 83);
		assert(error.code == mister::ErrorCode::io_failed);
		assert(error.message == "scripted status failure");
		assert(spi.calls.size() == 1);
	}
}

void TestAttachKeepsExactFileCommandOrderingAndBoundedFrames()
{
	TempFile file;
	mister::native::PosixArtifactOpener opener;
	mister::native::Artifact artifact;
	assert(opener.Open(file.path, 0, &artifact).ok());
	mister_test::FakeSpi spi;
	mister::native::CoreLoader loader(spi);
	assert(loader.Attach(2, artifact,
		mister::FileWireFormat::little_endian_byte_pairs, 77).ok());
	assert(spi.calls.size() == 7);
	assert((spi.calls[0].request == std::vector<std::uint16_t>{0x0055, 2}));
	assert(spi.calls[1].request ==
		std::vector<std::uint16_t>({0x0056, 0x622e, 0x6e69}));
	assert((spi.calls[2].request == std::vector<std::uint16_t>{0x0053, 0x00ff}));
	assert(spi.calls[3].request.front() == 0x0054);
	assert(spi.calls[3].request.size() == 2049);
	assert(spi.calls[4].request.front() == 0x0054);
	assert(spi.calls[4].request.size() == 453);
	assert((spi.calls[5].request == std::vector<std::uint16_t>{0x0029}));
	assert((spi.calls[6].request == std::vector<std::uint16_t>{0x0053, 0}));
}

void TestAttachPairsBytesLittleEndianAcrossThe4096ByteChunkBoundary()
{
	std::vector<unsigned char> bytes(4098, 0);
	bytes[4094] = 0x11;
	bytes[4095] = 0x22;
	bytes[4096] = 0x33;
	bytes[4097] = 0x44;
	TempFile file(bytes);
	mister::native::PosixArtifactOpener opener;
	mister::native::Artifact artifact;
	assert(opener.Open(file.path, 0, &artifact).ok());
	mister_test::FakeSpi spi;
	mister::native::CoreLoader loader(spi);
	assert(loader.Attach(1, artifact,
		mister::FileWireFormat::little_endian_byte_pairs, 91).ok());
	assert(spi.calls.size() == 7);
	assert(spi.calls[3].request.size() == 2049);
	assert(spi.calls[3].request.back() == 0x2211);
	assert(spi.calls[4].request ==
		std::vector<std::uint16_t>({0x0054, 0x4433}));
}

void TestAttachRejectsUnsupportedWireFormatBeforeSelectingFileIo()
{
	TempFile file;
	mister::native::PosixArtifactOpener opener;
	mister::native::Artifact artifact;
	assert(opener.Open(file.path, 0, &artifact).ok());
	mister_test::FakeSpi spi;
	mister::native::CoreLoader loader(spi);
	const mister::Error error = loader.Attach(1, artifact,
		static_cast<mister::FileWireFormat>(99), 92);
	assert(error.code == mister::ErrorCode::invalid_request);
	assert(error.message == "unsupported file wire format");
	assert(spi.calls.empty());
}

void TestAttachStopsAtEveryFailedExchangeAndShortRead()
{
	for (std::size_t fail = 0; fail != 7; ++fail) {
		TempFile file;
		mister::native::PosixArtifactOpener opener;
		mister::native::Artifact artifact;
		assert(opener.Open(file.path, 0, &artifact).ok());
		mister_test::FakeSpi spi;
		for (std::size_t prior = 0; prior < fail; ++prior) spi.errors.push_back({});
		spi.errors.push_back({mister::ErrorCode::io_failed,
			"scripted attach failure"});
		mister::native::CoreLoader loader(spi);
		const mister::Error error = loader.Attach(1, artifact,
			mister::FileWireFormat::little_endian_byte_pairs, 93);
		assert(error.code == mister::ErrorCode::io_failed);
		assert(error.message == "scripted attach failure");
		assert(spi.calls.size() == fail + 1);
	}

	TempFile file;
	mister::native::PosixArtifactOpener opener;
	mister::native::Artifact artifact;
	assert(opener.Open(file.path, 0, &artifact).ok());
	assert(truncate(file.path.c_str(), 4096) == 0);
	mister_test::FakeSpi spi;
	mister::native::CoreLoader loader(spi);
	const mister::Error error = loader.Attach(1, artifact,
		mister::FileWireFormat::little_endian_byte_pairs, 94);
	assert(error.code == mister::ErrorCode::io_failed);
	assert(error.message == "media read failed");
	assert(spi.calls.size() == 4);
}

void TestAttachStopsAfterFirstAndPerChunkArtifactReadFailures()
{
	const std::size_t expected_spi_calls[] = {3, 4};
	for (std::size_t fail_read = 0; fail_read != 2; ++fail_read) {
		TempFile file;
		mister::native::PosixArtifactOpener opener;
		mister::native::Artifact artifact;
		assert(opener.Open(file.path, 0, &artifact).ok());
		mister_test::FakeSpi spi;
		ScriptedArtifactReader reader(fail_read);
		mister::native::CoreLoader loader(spi, reader);
		const mister::Error error = loader.Attach(1, artifact,
			mister::FileWireFormat::little_endian_byte_pairs, 95);
		assert(error.code == mister::ErrorCode::io_failed);
		assert(error.message == "scripted artifact read failure");
		assert(reader.calls == fail_read + 1);
		assert(reader.offsets.front() == 0);
		if (fail_read == 1) assert(reader.offsets[1] == 4096);
		assert(spi.calls.size() == expected_spi_calls[fail_read]);
		for (const auto& call : spi.calls) {
			assert(call.request != std::vector<std::uint16_t>({0x0029}));
			assert(call.request != std::vector<std::uint16_t>({0x0053, 0x0000}));
		}
	}
}

void TestDirectSpiFailureIsReturnedWithoutLaterCommands()
{
	mister_test::FakeSpi spi;
	spi.errors.push_back({mister::ErrorCode::io_failed, "deadline exceeded"});
	mister::native::CoreLoader loader(spi);
	std::string observed = "sentinel";
	const mister::Error error = loader.Probe(&observed, 10);
	assert(error.code == mister::ErrorCode::io_failed);
	assert(error.message == "deadline exceeded");
	assert(observed == "sentinel");
	assert(spi.calls.size() == 1);
}

} // namespace

int main()
{
	TestProbeUsesCoreNameCommandAndParsesPrintableName();
	TestRecipeResetAndStatusPrimitivesUseExactWholeStatusWords();
	TestEachRecipePrimitiveReturnsItsDirectFailureWithoutLaterCalls();
	TestAttachKeepsExactFileCommandOrderingAndBoundedFrames();
	TestAttachPairsBytesLittleEndianAcrossThe4096ByteChunkBoundary();
	TestAttachRejectsUnsupportedWireFormatBeforeSelectingFileIo();
	TestAttachStopsAtEveryFailedExchangeAndShortRead();
	TestAttachStopsAfterFirstAndPerChunkArtifactReadFailures();
	TestDirectSpiFailureIsReturnedWithoutLaterCommands();
	puts("core_loader_test: 9 behaviors passed");
	return 0;
}
