// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "fake_mmio.hpp"
#include "native/core_package.hpp"
#include "native/fes_gp.hpp"
#include "native/generated/de10_nano.hpp"
#include "native/generated/fes_gp.hpp"
#include "native/hardware.hpp"

#include <assert.h>
#include <stdio.h>

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
		const mister::Error error = gp.Identify(descriptor, 10000);
		assert(error.code == mister::ErrorCode::core_mismatch);
		assert(error.phase == "identity");
		assert(!error.expected.empty() && !error.observed.empty());
		assert(mmio.writes.size() == FesGpIdentityWordCount * 2);
	}

	mister_test::FakeMmio mmio;
	TickClock clock;
	mister::native::FesGp gp(mmio, clock);
	std::vector<std::uint16_t> words = expected;
	words[FesGpIdentityCapabilitiesIndex] = static_cast<std::uint16_t>(
		words[FesGpIdentityCapabilitiesIndex] | 0x8000u);
	ScriptIdentity(&mmio, words);
	assert(gp.Identify(descriptor, 10000).ok());
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

} // namespace

int main()
{
	TestReplaysSharedGoldenExchangeSequence();
	TestExchangeRejectsMalformedAndUnstableResponsesWithoutRetry();
	TestExchangeAndIdentityUseExactDeadlineBoundaries();
	TestIdentifyReadsAllWordsThenRejectsEveryIdentityOrBuildMismatch();
	TestCoreDriverRoutesGeneratedControlsAndChecksResponses();
	puts("fes_gp_test: 5 groups passed");
	return 0;
}
