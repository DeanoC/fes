// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "fake_spi.hpp"
#include "snes_save_spi.hpp"
#include "native/artifacts.hpp"
#include "native/core_loader.hpp"

#include <assert.h>
#include <fcntl.h>
#include <stdio.h>
#include <unistd.h>

#include <string>
#include <array>
#include <algorithm>
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

std::vector<unsigned char> BasicSnes(bool hi, bool copier)
{
	const std::size_t offset = copier ? 512 : 0;
	std::vector<unsigned char> bytes(65536 + offset, 0);
	const std::size_t header = offset + (hi ? 0xffc0 : 0x7fc0);
	bytes[offset + (hi ? 0x8000 : 0)] = 0x78;
	bytes[header + 0x15] = hi ? 0x21 : 0x20;
	bytes[header + 0x17] = 6;
	bytes[header + 0x19] = 2;
	bytes[header + 0x1c] = 0xcb;
	bytes[header + 0x1d] = 0xed;
	bytes[header + 0x1e] = 0x34;
	bytes[header + 0x1f] = 0x12;
	bytes[header + 0x3d] = 0x80;
	return bytes;
}

void TestSaveEligibility()
{
	for (bool hi : {false, true}) for (unsigned type : {0u, 1u, 2u}) for (unsigned ram : {0u, 1u, 7u}) {
		if (!type && ram) continue;
		auto bytes = BasicSnes(hi, false);
		const unsigned header = hi ? 0xffc0 : 0x7fc0;
		bytes[header + 0x16] = type; bytes[header + 0x18] = ram;
		TempFile source(bytes);
		mister::native::Artifact artifact;
		mister::native::MediaContentPlan content;
		mister::native::PosixArtifactOpener opener;
		assert(opener.Open(source.path, 0, &artifact).ok());
		assert(mister::native::PrepareMediaContent(artifact, mister::MediaTransform::snes_cartridge, &content).ok());
		assert(content.battery_ram_size == (type == 2 && ram ? (1024u << ram) : 0));
	}
}

void TestBatterySizeAndSaveTransport()
{
	for (bool hi : {false, true}) for (unsigned exponent : {1u, 7u}) {
		auto rom = BasicSnes(hi, false);
		const unsigned header = hi ? 0xffc0 : 0x7fc0;
		rom[header + 0x16] = 2;
		rom[header + 0x18] = exponent;
		TempFile source(rom);
		mister::native::OpenedMedia media;
		media.index = 1;
		mister::native::PosixArtifactOpener opener;
		assert(opener.Open(source.path, 0, &media.artifact).ok());
		assert(mister::native::PrepareMediaContent(media.artifact, mister::MediaTransform::snes_cartridge, &media.content).ok());
		const unsigned size = 1024u << exponent;
		assert(media.content.battery_ram_size == size);
		std::vector<unsigned char> original(size);
		for (unsigned i = 0; i < size; ++i) original[i] = (i * 7) ^ (i >> 8);
		TempFile existing(original);
		mister::native::SaveFile save;
		assert(save.Prepare(existing.path, size).ok());
		mister_test::SnesSaveSpi spi;
		spi.ram.resize(size, 0xff);
		mister_test::SaveClock clock;
		mister::native::CoreLoader loader(spi);
		assert(loader.Attach(media, mister::FileWireFormat::little_endian_byte_pairs, 10000, &save).ok());
		assert(spi.mounted && !spi.downloading && spi.op == 1);
		assert(loader.RestoreSave(save, clock, 10000).ok());
		assert(spi.ram == original && spi.op == 0);
		spi.ram[1] ^= 0xff;
		std::vector<unsigned char> captured;
		assert(loader.CaptureSave(size, clock, 10000, &captured).ok());
		assert(captured == spi.ram && spi.snapshots == 1 && spi.status == 1);
		// A prior interrupted write transaction is drained before a new complete capture.
		spi.op = 2; spi.lba = 1;
		assert(loader.CaptureSave(size, clock, 10000, &captured).ok());
		assert(captured == spi.ram && spi.snapshots == 2);
		for (unsigned bad : {4u, 0x40u, 0x200u}) {
			spi.bad_status = bad;
			const auto before = captured;
			assert(!loader.CaptureSave(size, clock, 10000, &captured).ok());
			assert(captured == before);
		}
		spi.bad_status = 0; spi.bad_lba = 1;
		assert(!loader.CaptureSave(size, clock, 10000, &captured).ok());
		spi.bad_lba = 0; spi.short_response = true;
		assert(!loader.CaptureSave(size, clock, 10000, &captured).ok());
		spi.short_response = false; spi.stall = true;
		assert(!loader.CaptureSave(size, clock, clock.now + 10, &captured).ok());
	}
}

void TestSnesPrefixAndRetainedCartridgeStream()
{
	for (bool hi : {false, true}) for (bool copier : {false, true}) {
		const auto original = BasicSnes(hi, copier);
		TempFile source(original);
		mister::native::PosixArtifactOpener opener;
		mister::native::OpenedMedia media;
		media.index = 1;
		assert(opener.Open(source.path, 0, &media.artifact).ok());
		assert(mister::native::PrepareMediaContent(media.artifact,
			mister::MediaTransform::snes_cartridge, &media.content).ok());
		assert(media.content.source_offset == (copier ? 512u : 0u));
		assert(media.content.source_size == 65536 && media.content.prefix_size == 512);
		std::array<unsigned char, 512> expected = {};
		expected[0] = 6;
		expected[1] = hi ? 1 : 0;
		expected[3] = 1;
		expected[4] = 0xc0;
		expected[5] = hi ? 0xff : 0x7f;
		expected[10] = 1;
		assert(media.content.prefix == expected);
		mister_test::FakeSpi spi;
		mister::native::CoreLoader loader(spi);
		assert(loader.Attach(media, mister::FileWireFormat::little_endian_byte_pairs, 1234).ok());
		std::vector<unsigned char> wire;
		for (const auto& call : spi.calls) {
			assert(call.deadline == 1234);
			if (call.request[0] != 0x54) continue;
			for (std::size_t i = 1; i < call.request.size(); ++i) {
				wire.push_back(call.request[i] & 0xff);
				wire.push_back(call.request[i] >> 8);
			}
		}
		assert(wire.size() == 512 + 65536);
		assert(std::equal(expected.begin(), expected.end(), wire.begin()));
		assert(std::equal(original.begin() + (copier ? 512 : 0), original.end(), wire.begin() + 512));
	}
}

void TestSnesRejectsUnsupportedOrAmbiguousBeforeTransfer()
{
	for (int bad = 0; bad < 10; ++bad) {
		auto bytes = BasicSnes(false, false);
		if (bad == 0) bytes.resize(65535);
		if (bad == 1) bytes[0x7fd6] = 3; // Enhancement cartridge.
		if (bad == 2) bytes[0x7fd5] = 0x23; // SA-1 mapping.
		if (bad == 3) bytes[0x7ffd] = 0; // Invalid reset vector.
		if (bad == 4) bytes[0x7fd7] = 7; // Declared size mismatch.
		if (bad == 5) {
			auto hi = BasicSnes(true, false);
			std::copy(hi.begin() + 0xffc0, hi.end(), bytes.begin() + 0xffc0);
			bytes[0x8000] = 0x78;
		}
		if (bad == 6) bytes[0x7fd8] = 8; // RAM above admitted cap.
		if (bad == 7) bytes[0x7fdc] = 0; // Invalid checksum pair.
		if (bad == 8) { bytes = BasicSnes(true, false); bytes[0x8000] = 0xff; }
		if (bad == 9) bytes.resize(65536 + 513);
		TempFile file(bytes);
		mister::native::Artifact artifact;
		mister::native::PosixArtifactOpener opener;
		assert(opener.Open(file.path, 0, &artifact).ok());
		mister::native::MediaContentPlan plan;
		assert(mister::native::PrepareMediaContent(artifact,
			mister::MediaTransform::snes_cartridge, &plan).code == mister::ErrorCode::invalid_request);
	}
}

void TestSnesRetainedReadFailuresStopTransfer()
{
	TempFile source(BasicSnes(false, true));
	mister::native::PosixArtifactOpener opener;
	mister::native::OpenedMedia media;
	media.index = 1;
	assert(opener.Open(source.path, 0, &media.artifact).ok());
	assert(mister::native::PrepareMediaContent(media.artifact,
		mister::MediaTransform::snes_cartridge, &media.content).ok());
	assert(truncate(source.path.c_str(), 512) == 0);
	mister_test::FakeSpi spi;
	mister::native::CoreLoader loader(spi);
	assert(loader.Attach(media, mister::FileWireFormat::little_endian_byte_pairs, 123).code == mister::ErrorCode::io_failed);
	for (const auto& call : spi.calls) assert(call.request[0] != 0x54 && call.request[0] != 0x29);
	mister::native::MediaContentPlan plan;
	assert(mister::native::PrepareMediaContent(media.artifact,
		mister::MediaTransform::snes_cartridge, &plan).code == mister::ErrorCode::io_failed);
}

} // namespace

int main()
{
	TestSaveEligibility();
	TestBatterySizeAndSaveTransport();
	TestSnesRetainedReadFailuresStopTransfer();
	TestSnesPrefixAndRetainedCartridgeStream();
	TestSnesRejectsUnsupportedOrAmbiguousBeforeTransfer();
	TestProbeUsesCoreNameCommandAndParsesPrintableName();
	TestRecipeResetAndStatusPrimitivesUseExactWholeStatusWords();
	TestEachRecipePrimitiveReturnsItsDirectFailureWithoutLaterCalls();
	TestAttachKeepsExactFileCommandOrderingAndBoundedFrames();
	TestAttachPairsBytesLittleEndianAcrossThe4096ByteChunkBoundary();
	TestAttachRejectsUnsupportedWireFormatBeforeSelectingFileIo();
	TestAttachStopsAtEveryFailedExchangeAndShortRead();
	TestAttachStopsAfterFirstAndPerChunkArtifactReadFailures();
	TestDirectSpiFailureIsReturnedWithoutLaterCommands();
	puts("core_loader_test: 14 behaviors passed");
	return 0;
}
