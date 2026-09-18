// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "fake_mmio.hpp"
#include "native/core_package.hpp"
#include "native/fes_gp.hpp"
#include "native/generated/de10_nano.hpp"
#include "native/generated/fes_gp.hpp"
#include "native/generated/fes_simple_computer.hpp"
#include "native/hardware.hpp"
#include "native/artifacts.hpp"

#include <assert.h>
#include <stdio.h>
#include <unistd.h>
#include <stdlib.h>

#include <cstdint>
#include <fstream>
#include <regex>
#include <string>
#include <vector>

namespace {

using namespace mister::native::generated;

class TickClock final : public mister::native::Clock {
public:
	explicit TickClock(std::uint64_t now = 0) : now_(now) {}
	std::uint64_t NowMs() const override { return now_++; }
	mutable std::uint64_t now_;
};

class ScriptClock final : public mister::native::Clock {
public:
	explicit ScriptClock(std::vector<std::uint64_t> values) : values_(std::move(values))
	{
		assert(!values_.empty());
	}
	std::uint64_t NowMs() const override
	{
		const std::size_t selected = std::min(index_, values_.size() - 1);
		++index_;
		return values_[selected];
	}
	mutable std::size_t index_ = 0;
	std::vector<std::uint64_t> values_;
};

struct GoldenExchange {
	std::string name;
	std::uint32_t settled;
	std::uint32_t toggled;
	std::uint32_t gpi;
	std::uint16_t data;
};

std::string ReadFile(const char* path)
{
	std::ifstream input(path, std::ios::binary);
	assert(input.good());
	return std::string(std::istreambuf_iterator<char>(input),
		std::istreambuf_iterator<char>());
}

std::vector<GoldenExchange> ReadGolden(std::string* build_id)
{
	const std::string source = ReadFile("tests/fixtures/fes-gp-v1/exchanges.json");
	std::smatch match;
	assert(std::regex_search(source, match,
		std::regex("\\\"initial_request_toggle\\\"[[:space:]]*:[[:space:]]*false")));
	assert(std::regex_search(source, match,
		std::regex("\\\"build_id\\\"[[:space:]]*:[[:space:]]*\\\"([0-9a-f]{32})\\\"")));
	*build_id = match[1].str();
	const std::regex row(
		"\\{\\\"name\\\":\\\"([^\\\"]+)\\\",\\\"gpo\\\":\\[([0-9]+),([0-9]+)\\],"
		"\\\"gpi\\\":([0-9]+),\\\"data\\\":([0-9]+)\\}");
	std::vector<GoldenExchange> result;
	for (std::sregex_iterator it(source.begin(), source.end(), row), end;
		it != end; ++it) {
		result.push_back({(*it)[1].str(),
			static_cast<std::uint32_t>(std::stoul((*it)[2].str())),
			static_cast<std::uint32_t>(std::stoul((*it)[3].str())),
			static_cast<std::uint32_t>(std::stoul((*it)[4].str())),
			static_cast<std::uint16_t>(std::stoul((*it)[5].str()))});
	}
	assert(result.size() == 22);
	return result;
}

void PushCompleted(mister_test::FakeMmio* mmio, bool toggle,
	std::uint16_t response, bool failed = false)
{
	const std::uint32_t final = FesGpSignature |
		(toggle ? FesGpAckMask : 0u) |
		(failed ? FesGpErrorMask : 0u) | response;
	const std::uint32_t stale = (final & ~FesGpAckMask) |
		(toggle ? 0u : FesGpAckMask);
	mmio->PushRead(kSpiGpiAddress, stale);
	mmio->PushRead(kSpiGpiAddress, final);
	mmio->PushRead(kSpiGpiAddress, final);
}

mister::native::CoreDescriptor Descriptor(const std::string& build_id)
{
	mister::native::CoreDescriptor descriptor;
	descriptor.core.id = "fes.pong";
	descriptor.abi = {FesGpABIID, FesGpABIMajor, FesGpABIMinor};
	descriptor.interfaces.push_back({FesGpInterfaceGamepadID,
		FesGpInterfaceGamepadMajor, FesGpInterfaceGamepadMinor, true});
	descriptor.interfaces.push_back({FesGpInterfaceVideoFixed720p60ID,
		FesGpInterfaceVideoFixed720p60Major,
		FesGpInterfaceVideoFixed720p60Minor, true});
	descriptor.build.id = build_id;
	return descriptor;
}

std::vector<std::uint16_t> IdentityWords(const std::string& build_id)
{
	std::vector<std::uint16_t> words(FesGpIdentityWordCount);
	words[FesGpIdentityMagic0Index] = static_cast<std::uint16_t>(FesGpIdentityMagic0);
	words[FesGpIdentityMagic1Index] = static_cast<std::uint16_t>(FesGpIdentityMagic1);
	words[FesGpIdentityTransportMajorIndex] =
		static_cast<std::uint16_t>(FesGpTransportMajor);
	words[FesGpIdentityTransportMinorIndex] =
		static_cast<std::uint16_t>(FesGpTransportMinor);
	words[FesGpIdentityAbiTagIndex] = static_cast<std::uint16_t>(FesGpAbiTag);
	words[FesGpIdentityAbiMajorIndex] = static_cast<std::uint16_t>(FesGpAbiMajor);
	words[FesGpIdentityAbiMinorIndex] = static_cast<std::uint16_t>(FesGpAbiMinor);
	words[FesGpIdentityCapabilitiesIndex] = static_cast<std::uint16_t>(
		FesGpCapabilityGamepad | FesGpCapabilityVideoFixed720p60);
	std::size_t word = FesGpIdentityBuildIDStartIndex;
	for (std::size_t offset = 0; offset < build_id.size(); offset += 4) {
		const unsigned first = static_cast<unsigned>(std::stoul(
			build_id.substr(offset, 2), nullptr, 16));
		const unsigned second = static_cast<unsigned>(std::stoul(
			build_id.substr(offset + 2, 2), nullptr, 16));
		assert(word < words.size());
		words[word++] = static_cast<std::uint16_t>(first | (second << 8));
	}
	assert(word == FesGpIdentityWordCount);
	return words;
}

void ScriptIdentity(mister_test::FakeMmio* mmio,
	const std::vector<std::uint16_t>& words)
{
	bool toggle = false;
	for (std::uint16_t word : words) {
		toggle = !toggle;
		PushCompleted(mmio, toggle, word);
	}
}

void TestReplaysSharedGoldenExchangeSequence()
{
	std::string build_id;
	const std::vector<GoldenExchange> golden = ReadGolden(&build_id);
	assert(build_id == "00112233445566778899aabbccddeeff");
	mister_test::FakeMmio mmio;
	TickClock clock;
	mister::native::FesGp gp(mmio, clock);
	for (const GoldenExchange& exchange : golden) {
		const bool toggle = (exchange.gpi & FesGpAckMask) != 0;
		const std::uint32_t stale = (exchange.gpi & ~FesGpAckMask) |
			(toggle ? 0u : FesGpAckMask);
		mmio.PushRead(kSpiGpiAddress, stale);
		mmio.PushRead(kSpiGpiAddress, exchange.gpi);
		mmio.PushRead(kSpiGpiAddress, exchange.gpi);
		const std::uint8_t opcode = static_cast<std::uint8_t>(
			(exchange.toggled & FesGpOpcodeMask) >> 24);
		const std::uint8_t index = static_cast<std::uint8_t>(
			(exchange.toggled & FesGpIndexMask) >> 16);
		const std::uint16_t argument = static_cast<std::uint16_t>(
			exchange.toggled & FesGpArgumentMask);
		std::uint16_t response = 0xffff;
		const mister::Error error = gp.Exchange(opcode, index, argument,
			10000, &response);
		assert(error.ok() == ((exchange.gpi & FesGpErrorMask) == 0));
		assert(response == exchange.data);
	}
	assert(mmio.writes.size() == golden.size() * 2);
	for (std::size_t index = 0; index < golden.size(); ++index) {
		assert(mmio.writes[index * 2].offset == kFpgaGpoAddress);
		assert(mmio.writes[index * 2].value == golden[index].settled);
		assert(mmio.writes[index * 2 + 1].offset == kFpgaGpoAddress);
		assert(mmio.writes[index * 2 + 1].value == golden[index].toggled);
	}
}

void TestExchangeRejectsMalformedAndUnstableResponsesWithoutRetry()
{
	{
		mister_test::FakeMmio mmio;
		TickClock clock;
		mister::native::FesGp gp(mmio, clock);
		std::uint16_t response = 0;
		const std::uint32_t opcode_unit = FesGpOpcodeMask & (~FesGpOpcodeMask + 1u);
		const std::uint32_t invalid_opcode = FesGpOpcodeMask / opcode_unit + 1u;
		assert(invalid_opcode <= 0xffu);
		assert(gp.Exchange(static_cast<std::uint8_t>(invalid_opcode), 0, 0, 1000,
			&response).code ==
			mister::ErrorCode::io_failed);
		assert(mmio.writes.empty());
	}
	{
		mister_test::FakeMmio mmio;
		TickClock clock;
		mister::native::FesGp gp(mmio, clock);
		mmio.PushRead(kSpiGpiAddress, 0u);
		std::uint16_t response = 0;
		assert(gp.Exchange(FesGpOpcodeIdentity, 0, 0, 1000, &response).code ==
			mister::ErrorCode::io_failed);
		assert(mmio.writes.size() == 2);
	}
	{
		mister_test::FakeMmio mmio;
		TickClock clock;
		mister::native::FesGp gp(mmio, clock);
		const std::uint32_t first = FesGpSignature | FesGpAckMask | 1u;
		mmio.PushRead(kSpiGpiAddress, first);
		mmio.PushRead(kSpiGpiAddress, first + 1u);
		std::uint16_t response = 0;
		assert(gp.Exchange(FesGpOpcodeIdentity, 0, 0, 1000, &response).code ==
			mister::ErrorCode::io_failed);
		assert(mmio.writes.size() == 2);
	}
	{
		mister_test::FakeMmio mmio;
		TickClock clock;
		mister::native::FesGp gp(mmio, clock);
		mmio.values[kSpiGpiAddress] = FesGpSignature;
		std::uint16_t response = 0;
		assert(gp.Exchange(FesGpOpcodeIdentity, 0, 0, 1000, &response).code ==
			mister::ErrorCode::io_failed);
		assert(mmio.writes.size() == 2);
		assert(gp.Exchange(FesGpOpcodeIdentity, 0, 0, 1000, &response).code ==
			mister::ErrorCode::io_failed);
		assert(mmio.writes.size() == 2);
		gp.BeginSession();
		PushCompleted(&mmio, true, 0x4546u);
		assert(gp.Exchange(FesGpOpcodeIdentity, 0, 0, 1000, &response).ok());
		assert(response == 0x4546u);
		assert(mmio.writes.size() == 4);
	}
}

void TestExchangeAndIdentityUseExactDeadlineBoundaries()
{
	{
		mister_test::FakeMmio mmio;
		TickClock clock(50);
		mister::native::FesGp gp(mmio, clock);
		mmio.values[kSpiGpiAddress] = FesGpSignature;
		std::uint16_t response = 0;
		assert(gp.Exchange(FesGpOpcodeIdentity, 0, 0, 1000, &response).code ==
			mister::ErrorCode::io_failed);
		assert(clock.now_ == 151);
		assert(mmio.reads.size() == 99);
		assert(mmio.writes.size() == 2);
	}
	{
		mister_test::FakeMmio mmio;
		TickClock clock(50);
		mister::native::FesGp gp(mmio, clock);
		mmio.values[kSpiGpiAddress] = FesGpSignature;
		std::uint16_t response = 0;
		assert(gp.Exchange(FesGpOpcodeIdentity, 0, 0, 75, &response).code ==
			mister::ErrorCode::io_failed);
		assert(clock.now_ == 76);
		assert(mmio.reads.size() == 24);
		assert(mmio.writes.size() == 2);
	}
	{
		mister_test::FakeMmio mmio;
		ScriptClock clock({0, 1999, 1999, 2000});
		mister::native::FesGp gp(mmio, clock);
		mmio.values[kSpiGpiAddress] = FesGpSignature;
		const mister::Error error = gp.Identify(
			Descriptor("00112233445566778899aabbccddeeff"), 10000);
		assert(error.code == mister::ErrorCode::io_failed);
		assert(error.phase == "transport");
		assert(clock.index_ == 4);
		assert(mmio.reads.size() == 1);
		assert(mmio.writes.size() == 2);
	}
}

void TestIdentifyReadsAllWordsThenRejectsEveryIdentityOrBuildMismatch()
{
	const std::string build_id = "00112233445566778899aabbccddeeff";
	const mister::native::CoreDescriptor descriptor = Descriptor(build_id);
	const std::vector<std::uint16_t> expected = IdentityWords(build_id);
	for (std::size_t changed = 0; changed < expected.size(); ++changed) {
		mister_test::FakeMmio mmio;
		TickClock clock;
		mister::native::FesGp gp(mmio, clock);
		std::vector<std::uint16_t> words = expected;
		words[changed] ^= 1u;
		ScriptIdentity(&mmio, words);
		bool safe_to_quiesce = true;
		const mister::Error error = gp.Identify(descriptor, 10000,
			&safe_to_quiesce);
		assert(error.code == mister::ErrorCode::core_mismatch);
		assert(error.phase == "identity");
		assert(!error.expected.empty() && !error.observed.empty());
		assert(safe_to_quiesce ==
			(changed > FesGpIdentityCapabilitiesIndex));
		assert(mmio.writes.size() == FesGpIdentityWordCount * 2);
	}

	mister_test::FakeMmio mmio;
	TickClock clock;
	mister::native::FesGp gp(mmio, clock);
	std::vector<std::uint16_t> words = expected;
	words[FesGpIdentityCapabilitiesIndex] = static_cast<std::uint16_t>(
		words[FesGpIdentityCapabilitiesIndex] | 0x8000u);
	ScriptIdentity(&mmio, words);
	bool safe_to_quiesce = false;
	assert(gp.Identify(descriptor, 10000, &safe_to_quiesce).ok());
	assert(safe_to_quiesce);
}

void TestCoreDriverExposesOnlyVerifiedFesGpSessionsForCleanup()
{
	const std::string build_id = "00112233445566778899aabbccddeeff";
	const mister::native::CoreDescriptor descriptor = Descriptor(build_id);
	mister::native::CoreDriverContext context;
	context.descriptor = &descriptor;
	{
		mister_test::FakeMmio mmio;
		TickClock clock;
		mister::native::FesGp gp(mmio, clock);
		mister::native::FesGpCoreDriver driver(gp);
		std::vector<std::uint16_t> words = IdentityWords(build_id);
		words[FesGpIdentityMagic0Index] ^= 1u;
		ScriptIdentity(&mmio, words);
		const mister::native::CoreDriverResult result =
			driver.Identify(context, 10000);
		assert(result.error.code == mister::ErrorCode::core_mismatch);
		assert(!result.safe_to_quiesce);
	}
	{
		mister_test::FakeMmio mmio;
		TickClock clock;
		mister::native::FesGp gp(mmio, clock);
		mister::native::FesGpCoreDriver driver(gp);
		std::vector<std::uint16_t> words = IdentityWords(build_id);
		words[FesGpIdentityBuildIDStartIndex] ^= 1u;
		ScriptIdentity(&mmio, words);
		const mister::native::CoreDriverResult result =
			driver.Identify(context, 10000);
		assert(result.error.code == mister::ErrorCode::core_mismatch);
		assert(result.safe_to_quiesce);
	}
}

void TestIdentifyAcceptsSimpleComputerTagAndCapabilities()
{
	const std::string build_id = "00112233445566778899aabbccddeeff";
	mister::native::CoreDescriptor descriptor;
	descriptor.core.id = "fes.zx81";
	descriptor.abi = {FesSimpleComputerABIID, FesSimpleComputerABIMajor,
		FesSimpleComputerABIMinor};
	descriptor.interfaces = {
		{FesSimpleComputerInterfaceKeyboardID,
			FesSimpleComputerInterfaceKeyboardMajor,
			FesSimpleComputerInterfaceKeyboardMinor, true},
		{FesSimpleComputerInterfaceVideoFixed720p60ID,
			FesSimpleComputerInterfaceVideoFixed720p60Major,
			FesSimpleComputerInterfaceVideoFixed720p60Minor, true},
		{FesSimpleComputerInterfaceMediaBlobID,
			FesSimpleComputerInterfaceMediaBlobMajor,
			FesSimpleComputerInterfaceMediaBlobMinor, true},
	};
	descriptor.build.id = build_id;
	std::vector<std::uint16_t> words = IdentityWords(build_id);
	words[FesGpIdentityAbiTagIndex] =
		static_cast<std::uint16_t>(FesSimpleComputerAbiTag);
	words[FesGpIdentityCapabilitiesIndex] = static_cast<std::uint16_t>(
		FesSimpleComputerCapabilityKeyboard |
		FesSimpleComputerCapabilityVideoFixed720p60 |
		FesSimpleComputerCapabilityMediaBlob);
	mister_test::FakeMmio mmio;
	TickClock clock;
	mister::native::FesGp gp(mmio, clock);
	mister::native::FesGpCoreDriver driver(gp);
	mister::native::CoreDriverContext context;
	context.descriptor = &descriptor;
	ScriptIdentity(&mmio, words);
	const mister::native::CoreDriverResult result = driver.Identify(context, 10000);
	assert(result.error.ok());
	assert(result.observed_core == "fes.zx81");
	assert(result.safe_to_quiesce);
}

void TestComputerKeyboardMatrixAndMediaBlob()
{
	const std::string build_id = "00112233445566778899aabbccddeeff";
	mister::native::CoreDescriptor descriptor;
	descriptor.core.id = "fes.zx81";
	descriptor.abi = {FesSimpleComputerABIID, FesSimpleComputerABIMajor,
		FesSimpleComputerABIMinor};
	descriptor.interfaces = {
		{FesSimpleComputerInterfaceKeyboardID,
			FesSimpleComputerInterfaceKeyboardMajor,
			FesSimpleComputerInterfaceKeyboardMinor, true},
		{FesSimpleComputerInterfaceVideoFixed720p60ID,
			FesSimpleComputerInterfaceVideoFixed720p60Major,
			FesSimpleComputerInterfaceVideoFixed720p60Minor, true},
		{FesSimpleComputerInterfaceMediaBlobID,
			FesSimpleComputerInterfaceMediaBlobMajor,
			FesSimpleComputerInterfaceMediaBlobMinor, true},
	};
	descriptor.build.id = build_id;
	std::vector<std::uint16_t> words = IdentityWords(build_id);
	words[FesGpIdentityAbiTagIndex] =
		static_cast<std::uint16_t>(FesSimpleComputerAbiTag);
	words[FesGpIdentityCapabilitiesIndex] = static_cast<std::uint16_t>(
		FesSimpleComputerCapabilityKeyboard |
		FesSimpleComputerCapabilityVideoFixed720p60 |
		FesSimpleComputerCapabilityMediaBlob);
	mister_test::FakeMmio mmio;
	TickClock clock;
	mister::native::FesGp gp(mmio, clock);
	mister::native::FesGpCoreDriver driver(gp);
	mister::native::CoreDriverContext context;
	context.descriptor = &descriptor;
	ScriptIdentity(&mmio, words);
	assert(driver.Identify(context, 10000).error.ok());
	bool toggle = true;
	for (int i = 0; i < 8; ++i) {
		PushCompleted(&mmio, toggle, 0);
		toggle = !toggle;
	}
	assert(driver.SetKeyboardMatrix(0xffffffffffull, 10000).ok());
	const std::uint64_t j_key = 0xffffffffffull & ~(1ull << (6 * 5 + 3));
	for (int i = 0; i < 8; ++i) {
		PushCompleted(&mmio, toggle, 0);
		toggle = !toggle;
	}
	assert(driver.SetKeyboardMatrix(j_key, 10000).ok());
	const std::size_t media_start = mmio.writes.size();
	for (int i = 0; i < 6; ++i) {
		PushCompleted(&mmio, toggle, 0);
		toggle = !toggle;
	}
	assert(driver.LoadMedia(std::vector<std::uint8_t>{1, 2, 3}, 10000).ok());
	// Hold reset, begin, little-endian pair, odd tail, commit, release.
	const std::uint32_t expected[] = {
		0x02000000, 0x04000003, 0x05000201, 0x05010003, 0x06000000, 0x02000001};
	assert(mmio.writes.size() == media_start + 12);
	for (std::size_t i = 0; i < 6; ++i)
		assert((mmio.writes[media_start + i * 2].value & 0x7fffffff) == expected[i]);
	const std::size_t before_invalid = mmio.writes.size();
	assert(driver.LoadMedia({}, 10000).code == mister::ErrorCode::invalid_request);
	assert(driver.LoadMedia(std::vector<std::uint8_t>(16385), 10000).code ==
		mister::ErrorCode::invalid_request);
	assert(mmio.writes.size() == before_invalid);
	// A rejected commit must leave execution held, never boot partial media.
	for (int i = 0; i < 4; ++i) {
		PushCompleted(&mmio, toggle, i == 3 ? 1 : 0);
		toggle = !toggle;
	}
	assert(driver.LoadMedia(std::vector<std::uint8_t>{4, 5}, 10000).code ==
		mister::ErrorCode::io_failed);
	assert(mmio.writes.size() == before_invalid + 8);
	assert((mmio.writes.back().value & 0x7fffffff) == 0x06000000);
	// A failed commit leaves reset held, but a retry starts with a fresh hold
	// and may publish the media once the commit succeeds.
	const std::size_t retry_start = mmio.writes.size();
	for (int i = 0; i < 5; ++i) {
		PushCompleted(&mmio, toggle, 0);
		toggle = !toggle;
	}
	assert(driver.LoadMedia(std::vector<std::uint8_t>{4, 5}, 10000).ok());
	const std::uint32_t retry_expected[] = {
		0x02000000, 0x04000002, 0x05000504, 0x06000000, 0x02000001};
	assert(mmio.writes.size() == retry_start + 10);
	for (std::size_t i = 0; i < 5; ++i)
		assert((mmio.writes[retry_start + i * 2].value & 0x7fffffff) ==
			retry_expected[i]);

	// Rejected data must not commit or release reset.  The next successful
	// transfer's hold is the observable recovery contract.
	const std::size_t reject_start = mmio.writes.size();
	for (int i = 0; i < 3; ++i) {
		PushCompleted(&mmio, toggle, i == 2 ? 1 : 0);
		toggle = !toggle;
	}
	assert(driver.LoadMedia(std::vector<std::uint8_t>{6, 7}, 10000).code ==
		mister::ErrorCode::io_failed);
	assert(mmio.writes.size() == reject_start + 6);
	assert((mmio.writes[reject_start].value & 0x7fffffff) == 0x02000000);
	assert((mmio.writes[reject_start + 2].value & 0x7fffffff) == 0x04000002);
	assert((mmio.writes[reject_start + 4].value & 0x7fffffff) == 0x05000706);

	// Even-length media has no tail exchange and still commits/releases.
	const std::size_t even_start = mmio.writes.size();
	for (int i = 0; i < 6; ++i) {
		PushCompleted(&mmio, toggle, 0);
		toggle = !toggle;
	}
	assert(driver.LoadMedia(std::vector<std::uint8_t>{0x10, 0x11, 0x12, 0x13},
		10000).ok());
	const std::uint32_t even_expected[] = {
		0x02000000, 0x04000004, 0x05001110, 0x05001312, 0x06000000,
		0x02000001};
	assert(mmio.writes.size() == even_start + 12);
	for (std::size_t i = 0; i < 6; ++i)
		assert((mmio.writes[even_start + i * 2].value & 0x7fffffff) ==
			even_expected[i]);
	assert(driver.SetKeyboardMatrix(1ull << 40, 10000).code ==
		mister::ErrorCode::invalid_request);
}

void TestCoreDriverRoutesGeneratedControlsAndChecksResponses()
{
	mister_test::FakeMmio mmio;
	TickClock clock;
	mister::native::FesGp gp(mmio, clock);
	mister::native::FesGpCoreDriver driver(gp);
	mister::native::CoreDriverContext context;
	driver.BeginSession();
	PushCompleted(&mmio, true, FesGpButtonUp);
	assert(driver.SetButtons(context, FesGpButtonUp, 1000).error.ok());
	PushCompleted(&mmio, false, 0);
	assert(driver.Start(context, 1000).error.ok());
	PushCompleted(&mmio, true, 0);
	assert(driver.Quiesce(context, 1000).error.ok());
	assert(mmio.writes.size() == 6);
	assert(driver.SetButtons(context,
		static_cast<std::uint16_t>(FesGpButtonMask + 1u), 1000).error.code ==
		mister::ErrorCode::invalid_request);
	assert(mmio.writes.size() == 6);

	mister_test::FakeMmio rejected_mmio;
	TickClock rejected_clock;
	mister::native::FesGp rejected_gp(rejected_mmio, rejected_clock);
	mister::native::FesGpCoreDriver rejected(rejected_gp);
	PushCompleted(&rejected_mmio, true,
		static_cast<std::uint16_t>(FesGpButtonUp | FesGpButtonDown));
	assert(rejected.SetButtons(context, FesGpButtonUp, 1000).error.code ==
		mister::ErrorCode::io_failed);
}

void TestPersistenceTransfersAndPoisonedSnapshot()
{
	const std::string build = "00112233445566778899aabbccddeeff";
	auto descriptor = Descriptor(build);
	descriptor.interfaces.push_back({FesGpInterfacePersistenceWordsID, 1, 0, true});
	descriptor.interfaces.push_back({FesGpInterfacePongProgressID, 1, 0, true});
	auto words = IdentityWords(build);
	words[FesGpIdentityCapabilitiesIndex] |= 12;
	mister_test::FakeMmio mmio;
	TickClock clock;
	mister::native::FesGp gp(mmio, clock);
	mister::native::FesGpCoreDriver driver(gp);
	mister::native::CoreDriverContext context;
	context.descriptor = &descriptor;
	ScriptIdentity(&mmio, words);
	bool toggle = false;
	for (auto v : {2, 1, 1, 0}) {
		toggle = !toggle;
		PushCompleted(&mmio, toggle, v);
	}
	assert(driver.Identify(context, 10000).error.ok());
	for (unsigned i = 0; i < 4; ++i) {
		toggle = !toggle;
		PushCompleted(&mmio, toggle, 0);
	}
	assert(driver.RestoreData(context, {2, 17}, 10000).ok());
	toggle = !toggle;
	PushCompleted(&mmio, toggle, 0);
	assert(driver.Start(context, 10000).error.ok());
	for (auto v : {0, 2, 17}) {
		toggle = !toggle;
		PushCompleted(&mmio, toggle, v);
	}
	std::vector<std::uint16_t> snapshot;
	assert(driver.CaptureData(context, 10000, &snapshot).ok());
	assert(snapshot == std::vector<std::uint16_t>({2, 17}));
	toggle = !toggle;
	PushCompleted(&mmio, toggle, 0);
	assert(driver.ResumeData(context, 10000).ok());
	assert(!driver.RestoreData(context, {1, 0}, 10000).ok());
	// Successful freeze plus an ambiguous read cannot expose partial bytes or resume.
	toggle = !toggle;
	PushCompleted(&mmio, toggle, 0);
	snapshot.clear();
	assert(!driver.CaptureData(context, clock.now_ + 15, &snapshot).ok());
	assert(snapshot.empty());
	const auto before = mmio.writes.size();
	assert(!driver.ResumeData(context, 10000).ok());
	assert(mmio.writes.size() == before);
}

void TestSharedPersistenceWireFixtures()
{
	const auto source = ReadFile("tests/fixtures/core-persistence-v1/exchanges.json");
	const std::regex rows(R"rx(\{[^{}]*"opcode"[^{}]*\})rx");
	const std::regex request(R"rx("gpo"\s*:\s*\[\s*([0-9]+),\s*([0-9]+)\s*\])rx");
	const std::regex response(R"rx("gpi"\s*:\s*([0-9]+))rx");
	mister_test::FakeMmio mmio;
	TickClock clock;
	mister::native::FesGp gp(mmio, clock);
	unsigned count = 0;
	for (std::sregex_iterator row(source.begin(), source.end(), rows), end; row != end; ++row) {
		std::string text = row->str();
		std::smatch q, r;
		assert(std::regex_search(text, q, request));
		assert(std::regex_search(text, r, response));
		auto settled = static_cast<std::uint32_t>(std::stoul(q[1]));
		auto toggled = static_cast<std::uint32_t>(std::stoul(q[2]));
		auto gpi = static_cast<std::uint32_t>(std::stoul(r[1]));
		PushCompleted(&mmio, (gpi & FesGpAckMask) != 0, static_cast<std::uint16_t>(gpi),
			(gpi & FesGpErrorMask) != 0);
		std::uint16_t value = 0;
		auto error = gp.Exchange(static_cast<std::uint8_t>((toggled & FesGpOpcodeMask) >> 24),
			static_cast<std::uint8_t>((toggled & FesGpIndexMask) >> 16),
			static_cast<std::uint16_t>(toggled), 100000, &value);
		assert(error.ok() == ((gpi & FesGpErrorMask) == 0));
		assert(value == static_cast<std::uint16_t>(gpi));
		assert(
			mmio.writes[2 * count].value == settled && mmio.writes[2 * count + 1].value == toggled);
		++count;
	}
	assert(count > 40);
}
void TestPersistenceIdentityInfoAndPartialRestoreRejection()
{
	const std::string build = "00112233445566778899aabbccddeeff";
	auto descriptor = Descriptor(build);
	descriptor.interfaces.push_back({FesGpInterfacePersistenceWordsID, 1, 0, true});
	descriptor.interfaces.push_back({FesGpInterfacePongProgressID, 1, 0, true});
	auto words = IdentityWords(build);
	words[FesGpIdentityCapabilitiesIndex] |= 12;
	for (unsigned mismatch = 0; mismatch < 6; ++mismatch) {
		mister_test::FakeMmio mmio;
		TickClock clock;
		mister::native::FesGp gp(mmio, clock);
		mister::native::FesGpCoreDriver driver(gp);
		mister::native::CoreDriverContext context;
		context.descriptor = &descriptor;
		assert(!driver.RestoreData(context, {1, 0}, 10000).ok() && mmio.writes.empty());
		auto live = words;
		if (mismatch >= 4)
			live[FesGpIdentityCapabilitiesIndex] ^= mismatch == 4 ? 4 : 8;
		ScriptIdentity(&mmio, live);
		bool toggle = false;
		unsigned index = 0;
		if (mismatch < 4)
			for (auto value : {2, 1, 1, 0}) {
				toggle = !toggle;
				PushCompleted(&mmio, toggle,
					static_cast<std::uint16_t>(value + (index++ == mismatch ? 1 : 0)));
			}
		assert(!driver.Identify(context, 10000).error.ok());
		auto before = mmio.writes.size();
		assert(!driver.RestoreData(context, {1, 0}, 10000).ok());
		assert(mmio.writes.size() == before);
	}
	mister_test::FakeMmio mmio;
	TickClock clock;
	mister::native::FesGp gp(mmio, clock);
	mister::native::FesGpCoreDriver driver(gp);
	mister::native::CoreDriverContext context;
	context.descriptor = &descriptor;
	ScriptIdentity(&mmio, words);
	bool toggle = false;
	for (auto v : {2, 1, 1, 0}) {
		toggle = !toggle;
		PushCompleted(&mmio, toggle, v);
	}
	assert(driver.Identify(context, 10000).error.ok());
	PushCompleted(&mmio, true, 0);
	PushCompleted(&mmio, false, 0);
	PushCompleted(&mmio, true, FesGpErrorInvalidState, true);
	assert(!driver.RestoreData(context, {2, 17}, 10000).ok());
	assert(mmio.writes.size() == 46); // no commit after rejected second write
}

void TestSharedStreamWireFixtures()
{
	const auto source = ReadFile("tests/fixtures/fes-media-stream-v1/exchanges.json");
	const std::regex scenarios(R"rx("initial_request_toggle"\s*:\s*false)rx");
	const std::regex rows(R"rx(\{[^{}]*"opcode"[^{}]*\})rx");
	const std::regex request(R"rx("gpo"\s*:\s*\[\s*([0-9]+),\s*([0-9]+)\s*\])rx");
	const std::regex response(R"rx("gpi"\s*:\s*([0-9]+))rx");
	std::vector<std::size_t> starts;
	for (std::sregex_iterator it(source.begin(), source.end(), scenarios), end; it != end; ++it)
		starts.push_back(static_cast<std::size_t>(it->position()));
	assert(starts.size() == 7);
	starts.push_back(source.size());
	unsigned count = 0;
	for (std::size_t scenario = 0; scenario + 1 < starts.size(); ++scenario) {
		const auto text = source.substr(starts[scenario], starts[scenario + 1] - starts[scenario]);
		mister_test::FakeMmio mmio;
		TickClock clock;
		mister::native::FesGp gp(mmio, clock);
		for (std::sregex_iterator row(text.begin(), text.end(), rows), end; row != end; ++row) {
			const auto exchange = row->str();
			std::smatch q, r;
			assert(std::regex_search(exchange, q, request));
			assert(std::regex_search(exchange, r, response));
			const auto settled = static_cast<std::uint32_t>(std::stoul(q[1]));
			const auto toggled = static_cast<std::uint32_t>(std::stoul(q[2]));
			const auto gpi = static_cast<std::uint32_t>(std::stoul(r[1]));
			PushCompleted(&mmio, (gpi & FesGpAckMask) != 0,
				static_cast<std::uint16_t>(gpi), (gpi & FesGpErrorMask) != 0);
			const auto before = mmio.writes.size();
			std::uint16_t result = 0;
			const auto error = gp.Exchange(static_cast<std::uint8_t>((toggled >> 24) & 0x7f),
				static_cast<std::uint8_t>(toggled >> 16), static_cast<std::uint16_t>(toggled),
				100000, &result);
			assert(error.ok() == ((gpi & FesGpErrorMask) == 0));
			assert(result == static_cast<std::uint16_t>(gpi));
			assert(mmio.writes[before].value == settled);
			assert(mmio.writes[before + 1].value == toggled);
			++count;
		}
	}
	assert(count == 92);
}

struct StreamFixture {
	mister_test::FakeMmio mmio;
	TickClock clock;
	mister::native::FesGp gp{mmio, clock};
	mister::native::FesGpCoreDriver driver{gp};
	mister::native::CoreDescriptor descriptor = Descriptor("00112233445566778899aabbccddeeff");
	mister::native::CoreDriverContext context;
	bool toggle = false;
	StreamFixture()
	{
		descriptor.abi = {FesSimpleComputerABIID, 1, 0};
		descriptor.interfaces = {{FesSimpleComputerInterfaceMediaBlobID, 1, 0, true},
			{FesSimpleComputerInterfaceMediaBlobStreamID, 1, 0, true}};
		context.descriptor = &descriptor;
	}
	void Reply(std::uint16_t value = 0, bool failed = false)
	{
		toggle = !toggle;
		PushCompleted(&mmio, toggle, value, failed);
	}
	mister::Error Identify(std::uint32_t minimum = 1, std::uint32_t maximum = 32768,
		std::uint16_t chunk = 512, std::uint16_t caps = 15)
	{
		auto words = IdentityWords(descriptor.build.id);
		words[FesGpIdentityAbiTagIndex] = FesSimpleComputerAbiTag;
		words[FesGpIdentityCapabilitiesIndex] = caps;
		for (auto word : words) Reply(word);
		bool declared = false;
		for (const auto& interface : descriptor.interfaces)
			if (interface.id == FesSimpleComputerInterfaceMediaBlobStreamID &&
				interface.major == 1 && interface.minor == 0) declared = true;
		if (declared && (caps & FesSimpleComputerCapabilityMediaBlobStream))
			for (auto word : {static_cast<std::uint16_t>(minimum),
				static_cast<std::uint16_t>(minimum >> 16), static_cast<std::uint16_t>(maximum),
				static_cast<std::uint16_t>(maximum >> 16), chunk}) Reply(word);
		return driver.Identify(context, 1000000).error;
	}
};

struct StreamFile {
	std::string path;
	StreamFile(std::uint32_t size)
	{
		char pattern[] = "/tmp/libmister-stream-test-XXXXXX";
		const int fd = mkstemp(pattern);
		assert(fd >= 0);
		path = pattern;
		std::array<std::uint8_t, 512> data;
		for (std::size_t i = 0; i < data.size(); ++i) data[i] = static_cast<std::uint8_t>(i + 1);
		for (std::uint32_t n = 0; n < size;) {
			const auto length = std::min<std::uint32_t>(data.size(), size - n);
			assert(write(fd, data.data(), length) == static_cast<ssize_t>(length));
			n += length;
		}
		assert(close(fd) == 0);
	}
	~StreamFile() { assert(unlink(path.c_str()) == 0); }
};

void TestStreamIdentityRequiresObservedCapacityAndDeclaration()
{
	for (auto maximum : {0u, 32767u, 33554433u, 0xffffffffu}) {
		StreamFixture f;
		assert(!f.Identify(1, maximum).ok());
		mister::native::MediaStreamInfo info;
		assert(!f.driver.StreamInfo(&info).ok());
	}
	for (auto chunk : {0u, 511u, 513u}) {
		StreamFixture f;
		assert(!f.Identify(1, 32768, chunk).ok());
	}
	StreamFixture minimum;
	assert(!minimum.Identify(2).ok());
	StreamFixture missing;
	assert(!missing.Identify(1, 32768, 512, 7).ok());
	StreamFixture undeclared;
	undeclared.descriptor.interfaces.pop_back();
	assert(undeclared.Identify().ok());
	assert(undeclared.driver.observed_capabilities() == 15);
	assert(undeclared.mmio.writes.size() == 32); // identity only; no Info commands
	mister::native::MediaStreamInfo info;
	assert(!undeclared.driver.StreamInfo(&info).ok());
	StreamFixture optional;
	optional.descriptor.interfaces.back().required = false;
	assert(optional.Identify(1, 32768, 512, 7).ok());
	assert(!optional.driver.StreamInfo(&info).ok());
	assert(optional.mmio.writes.size() == 32);
	StreamFixture unknown;
	unknown.descriptor.interfaces.back().required = false;
	unknown.descriptor.interfaces.back().major = 2;
	assert(unknown.Identify().ok());
	assert(unknown.mmio.writes.size() == 32);
	assert(!unknown.driver.StreamInfo(&info).ok());
	StreamFixture valid;
	assert(valid.Identify(1, 33554432).ok());
	assert(valid.driver.StreamInfo(&info).ok());
	assert(info.maximum == 33554432 && info.minimum == 1 && info.chunk_bytes == 512);
	valid.driver.BeginSession();
	assert(valid.driver.observed_capabilities() == 0);
	assert(!valid.driver.StreamInfo(&info).ok());
}

void TestStreamStartKeepsResetAndLegacyStillReleases()
{
	// Only the verified declaration + observed capability changes startup.
	for (unsigned mode = 0; mode < 5; ++mode) {
		StreamFixture f;
		if (mode == 1 || mode == 2) f.descriptor.interfaces.pop_back();
		if (mode == 3) f.descriptor.interfaces.back().required = false;
		if (mode == 4) {
			f.descriptor.interfaces.back().required = false;
			f.descriptor.interfaces.back().major = 2;
		}
		assert(f.Identify(1, 32768, 512, mode == 1 || mode == 3 ? 7 : 15).ok());
		const auto before = f.mmio.writes.size();
		for (unsigned i = 0; i < 9; ++i) f.Reply();
		assert(f.driver.Start(f.context, 1000000).error.ok());
		assert(f.mmio.writes.size() == before + 18);
		for (unsigned row = 0; row < 8; ++row)
			assert((f.mmio.writes[before + row * 2 + 1].value & ~FesGpRequestMask) ==
				((FesSimpleComputerOpcodeKeyboard << 24) | (row << 16) | 0x1f));
		assert((f.mmio.writes.back().value & ~FesGpRequestMask) ==
			((FesSimpleComputerOpcodeExecution << 24) | (mode == 0 ? 0u : 1u)));
	}
	// Each neutral row and the final hold can fail; no later command or release follows.
	for (unsigned failure = 0; failure < 9; ++failure) {
		StreamFixture f;
		assert(f.Identify().ok());
		const auto before = f.mmio.writes.size();
		for (unsigned i = 0; i < failure; ++i) f.Reply();
		f.Reply(FesSimpleComputerErrorInvalidState, true);
		const auto result = f.driver.Start(f.context, 1000000);
		assert(result.error.code == mister::ErrorCode::io_failed && result.mutation_attempted);
		assert(result.error.phase == (failure < 8 ? "input" : "quiesce"));
		assert(f.mmio.writes.size() == before + (failure + 1) * 2);
		for (auto i = before; i < f.mmio.writes.size(); ++i)
			assert((f.mmio.writes[i].value & ~FesGpRequestMask) != 0x02000001u);
	}
}

void TestStreamTransferBoundariesAndCRC()
{
	for (auto size : {1u, 3u, 511u, 512u, 513u, 16385u, 32768u}) {
		StreamFixture f;
		assert(f.Identify().ok());
		StreamFile file(size);
		mister::native::ComputerMediaSnapshot snapshot;
		assert(snapshot.Prepare(file.path, 1, 32768, f.clock, 1000000).ok());
		const auto start = f.mmio.writes.size();
		f.Reply(); // hold reset
		for (unsigned i = 0; i < 4; ++i) f.Reply();
		for (unsigned offset = 0; offset < size; offset += 512) {
			for (unsigned i = 0; i < 3; ++i) f.Reply();
			for (unsigned i = 0; i < (std::min(512u, size - offset) + 1) / 2; ++i) f.Reply();
		}
		f.Reply(); // commit
		f.Reply(); // release only after commit
		assert(f.driver.LoadMediaStream(snapshot, f.clock, 1000000).ok());
		std::size_t cursor = start;
		auto expect = [&](unsigned op, unsigned index, unsigned argument) {
			const auto word = f.mmio.writes[cursor + 1].value & ~FesGpRequestMask;
			assert(word == ((op << 24) | (index << 16) | argument));
			cursor += 2;
		};
		expect(2, 0, 0);
		expect(8, 0, size & 65535);
		expect(8, 1, size >> 16);
		expect(8, 2, snapshot.crc32() & 65535);
		expect(8, 3, snapshot.crc32() >> 16);
		if (size == 3) assert(snapshot.crc32() == 0x55bc801d);
		for (unsigned offset = 0; offset < size; offset += 512) {
			const auto length = std::min(512u, size - offset);
			expect(9, 0, offset & 65535);
			expect(9, 1, offset >> 16);
			expect(9, 2, length);
			for (unsigned byte = 0; byte < length; byte += 2)
				expect(10, byte / 2, ((byte + 1) & 255) |
					(byte + 1 < length ? ((byte + 2) & 255) << 8 : 0));
		}
		expect(11, 0, 0);
		expect(2, 0, 1);
		assert(cursor == f.mmio.writes.size());
	}
	StreamFixture f;
	assert(f.Identify().ok());
	StreamFile file(32769);
	mister::native::ComputerMediaSnapshot snapshot;
	assert(snapshot.Prepare(file.path, 1, 33554432, f.clock, 1000000).ok());
	const auto before = f.mmio.writes.size();
	assert(!f.driver.LoadMediaStream(snapshot, f.clock, 1000000).ok());
	assert(f.mmio.writes.size() == before);
}

void TestStreamFailureAbortAndAmbiguousSession()
{
	{
		StreamFixture f;
		assert(f.Identify().ok());
		StreamFile file(3);
		mister::native::ComputerMediaSnapshot snapshot;
		assert(snapshot.Prepare(file.path, 1, 32768, f.clock, 1000000).ok());
		for (unsigned i = 0; i < 5; ++i) f.Reply(); // hold + Begin
		const auto before = f.mmio.writes.size();
		ScriptClock expired({1000000});
		const auto error = f.driver.LoadMediaStream(snapshot, expired, 1000000);
		assert(!error.ok() && error.message == "media snapshot read deadline exceeded");
		assert(f.mmio.writes.size() == before + 10); // no Chunk/Data/Commit/Release
		f.Reply();
		assert(f.driver.AbortMediaStream(1000000).ok());
	}
	// Hold, all Begin/Chunk/data words, commit and release are independent failure points.
	for (unsigned failure = 1; failure < 12; ++failure) {
		StreamFixture f;
		assert(f.Identify().ok());
		StreamFile file(3);
		mister::native::ComputerMediaSnapshot snapshot;
		assert(snapshot.Prepare(file.path, 1, 32768, f.clock, 1000000).ok());
		const auto before = f.mmio.writes.size();
		for (unsigned i = 0; i < failure; ++i) f.Reply();
		f.Reply(4, true);
		assert(!f.driver.LoadMediaStream(snapshot, f.clock, 1000000).ok());
		assert(f.mmio.writes.size() == before + 2 * (failure + 1));
		const auto stopped = f.mmio.writes.size();
		assert(!f.driver.LoadMediaStream(snapshot, f.clock, 1000000).ok());
		assert(!f.driver.LoadMedia({1}, 1000000).ok());
		assert(!f.driver.Start({}, 1000000).error.ok());
		assert(f.mmio.writes.size() == stopped);
		f.Reply();
		assert(f.driver.AbortMediaStream(1000000).ok());
		assert(f.driver.AbortMediaStream(1000000).ok());
		assert(f.mmio.writes.size() == stopped + 2);
	}
	StreamFixture f;
	assert(f.Identify().ok());
	StreamFile file(3);
	mister::native::ComputerMediaSnapshot snapshot;
	assert(snapshot.Prepare(file.path, 1, 32768, f.clock, 1000000).ok());
	f.Reply(); // hold; missing Begin ACK poisons the session
	assert(!f.driver.LoadMediaStream(snapshot, f.clock, 1000000).ok());
	const auto before = f.mmio.writes.size();
	assert(!f.driver.AbortMediaStream(1000000).ok());
	assert(!f.driver.LoadMediaStream(snapshot, f.clock, 1000000).ok());
	assert(f.mmio.writes.size() == before);
}

} // namespace

int main()
{
	TestSharedStreamWireFixtures();
	TestStreamIdentityRequiresObservedCapacityAndDeclaration();
	TestStreamTransferBoundariesAndCRC();
	TestStreamStartKeepsResetAndLegacyStillReleases();
	TestStreamFailureAbortAndAmbiguousSession();
	TestPersistenceTransfersAndPoisonedSnapshot();
	TestSharedPersistenceWireFixtures();
	TestPersistenceIdentityInfoAndPartialRestoreRejection();
	TestReplaysSharedGoldenExchangeSequence();
	TestExchangeRejectsMalformedAndUnstableResponsesWithoutRetry();
	TestExchangeAndIdentityUseExactDeadlineBoundaries();
	TestIdentifyReadsAllWordsThenRejectsEveryIdentityOrBuildMismatch();
	TestIdentifyAcceptsSimpleComputerTagAndCapabilities();
	TestComputerKeyboardMatrixAndMediaBlob();
	TestCoreDriverExposesOnlyVerifiedFesGpSessionsForCleanup();
	TestCoreDriverRoutesGeneratedControlsAndChecksResponses();
	puts("fes_gp_test: 16 groups passed");
	return 0;
}
