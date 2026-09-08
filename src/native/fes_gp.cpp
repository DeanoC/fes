// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/fes_gp.hpp"

#include "native/core_package.hpp"
#include "native/generated/de10_nano.hpp"
#include "native/generated/fes_gp.hpp"
#include "native/hardware.hpp"
#include "native/linux/mmio.hpp"

#include <algorithm>
#include <limits>
#include <string>
#include <vector>

namespace mister {
namespace native {
namespace {

using namespace generated;

constexpr std::uint64_t kExchangeTimeoutMs = 100u;
constexpr std::uint64_t kDiscoveryTimeoutMs = 2000u;
constexpr std::uint32_t kResponseVariableMask =
	FesGpAckMask | FesGpErrorMask | FesGpResponseMask;
constexpr std::uint32_t kResponseFixedMask = ~kResponseVariableMask;

constexpr std::uint32_t FieldUnit(std::uint32_t mask)
{
	return mask & (~mask + 1u);
}

constexpr std::uint32_t FieldMaximum(std::uint32_t mask)
{
	return mask / FieldUnit(mask);
}

constexpr std::uint32_t EncodeField(std::uint32_t value, std::uint32_t mask)
{
	return value * FieldUnit(mask);
}

static_assert((FesGpSignature & kResponseVariableMask) == 0,
	"FES GP response signature overlaps generated variable fields");
static_assert(FieldMaximum(FesGpOpcodeMask) < 0xffu,
	"FES GP opcode admission needs a wider API type");
static_assert(FieldMaximum(FesGpIndexMask) == 0xffu,
	"FES GP index mask does not match its API type");
static_assert(FieldMaximum(FesGpArgumentMask) == 0xffffu,
	"FES GP argument mask does not match its API type");
static_assert(FesGpIdentityBuildIDStartIndex + 8u <= FesGpIdentityWordCount,
	"FES GP generated identity layout cannot hold the build ID");

std::uint64_t AddDeadline(std::uint64_t now, std::uint64_t duration)
{
	const std::uint64_t maximum = std::numeric_limits<std::uint64_t>::max();
	return now > maximum - duration ? maximum : now + duration;
}

Error Io(const std::string& message)
{
	return {ErrorCode::io_failed, message};
}

Error Mismatch(const std::string& message)
{
	return {ErrorCode::core_mismatch, message};
}

bool HexNibble(char value, unsigned* output)
{
	if (value >= '0' && value <= '9') *output = static_cast<unsigned>(value - '0');
	else if (value >= 'a' && value <= 'f')
		*output = static_cast<unsigned>(value - 'a' + 10);
	else return false;
	return true;
}

bool BuildWords(const std::string& build_id, std::vector<std::uint16_t>* words)
{
	if (build_id.size() != 32 || words == nullptr) return false;
	words->clear();
	for (std::size_t offset = 0; offset < build_id.size(); offset += 4) {
		unsigned a = 0, b = 0, c = 0, d = 0;
		if (!HexNibble(build_id[offset], &a) || !HexNibble(build_id[offset + 1], &b) ||
			!HexNibble(build_id[offset + 2], &c) || !HexNibble(build_id[offset + 3], &d))
			return false;
		const std::uint16_t first = static_cast<std::uint16_t>((a << 4) | b);
		const std::uint16_t second = static_cast<std::uint16_t>((c << 4) | d);
		words->push_back(static_cast<std::uint16_t>(first | (second << 8)));
	}
	return true;
}

} // namespace

FesGp::FesGp(Mmio& mmio, Clock& clock) : mmio_(mmio), clock_(clock) {}

Error FesGp::Exchange(std::uint8_t opcode, std::uint8_t index,
	std::uint16_t argument, std::uint64_t deadline, std::uint16_t* response)
{
	if (response == nullptr) return Io("missing FES GP response output");
	if (opcode > FieldMaximum(FesGpOpcodeMask))
		return Io("FES GP opcode exceeds the wire field");
	if (poisoned_) return Io("FES GP exchange state is ambiguous");
	const std::uint64_t now = clock_.NowMs();
	if (now >= deadline) return Io("FES GP exchange deadline exceeded");
	const std::uint64_t exchange_deadline = std::min(deadline,
		AddDeadline(now, kExchangeTimeoutMs));
	const bool next_toggle = !request_toggle_;
	const std::uint32_t fields =
		EncodeField(opcode, FesGpOpcodeMask) |
		EncodeField(index, FesGpIndexMask) |
		EncodeField(argument, FesGpArgumentMask);
	const std::uint32_t settled = fields |
		(request_toggle_ ? FesGpRequestMask : 0u);
	Error error = mmio_.Write32(generated::kFpgaGpoAddress, settled);
	if (!error.ok()) return error;
	request_toggle_ = next_toggle;
	error = mmio_.Write32(generated::kFpgaGpoAddress,
		fields | (next_toggle ? FesGpRequestMask : 0u));
	if (!error.ok()) {
		poisoned_ = true;
		return error;
	}

	for (;;) {
		if (clock_.NowMs() >= exchange_deadline) {
			poisoned_ = true;
			return Io("FES GP exchange deadline exceeded");
		}
		std::uint32_t observed = 0;
		error = mmio_.Read32(generated::kSpiGpiAddress, &observed);
		if (!error.ok()) {
			poisoned_ = true;
			return error;
		}
		if ((observed & kResponseFixedMask) != FesGpSignature) {
			poisoned_ = true;
			return Io("invalid FES GP response signature or reserved bits");
		}
		const bool acknowledged = (observed & FesGpAckMask) != 0;
		if (acknowledged != next_toggle) continue;
		if (clock_.NowMs() >= exchange_deadline) {
			poisoned_ = true;
			return Io("FES GP response stability deadline exceeded");
		}
		std::uint32_t confirmed = 0;
		error = mmio_.Read32(generated::kSpiGpiAddress, &confirmed);
		if (!error.ok() || confirmed != observed) {
			poisoned_ = true;
			return error.ok() ? Io("unstable FES GP response") : error;
		}
		*response = static_cast<std::uint16_t>(confirmed & FesGpResponseMask);
		if ((confirmed & FesGpErrorMask) != 0)
			return Io("FES GP command rejected with response " +
				std::to_string(*response));
		return {};
	}
}

Error FesGp::Identify(const CoreDescriptor& descriptor, std::uint64_t deadline)
{
	const std::uint64_t now = clock_.NowMs();
	if (now >= deadline) return Io("FES GP discovery deadline exceeded");
	const std::uint64_t discovery_deadline = std::min(deadline,
		AddDeadline(now, kDiscoveryTimeoutMs));
	std::vector<std::uint16_t> observed;
	for (std::uint32_t index = 0; index < FesGpIdentityWordCount; ++index) {
		std::uint16_t word = 0;
		const Error error = Exchange(static_cast<std::uint8_t>(FesGpOpcodeIdentity),
			static_cast<std::uint8_t>(index), 0, discovery_deadline, &word);
		if (!error.ok()) return error;
		observed.push_back(word);
	}

	std::vector<std::uint16_t> expected(FesGpIdentityWordCount);
	expected[FesGpIdentityMagic0Index] = static_cast<std::uint16_t>(FesGpIdentityMagic0);
	expected[FesGpIdentityMagic1Index] = static_cast<std::uint16_t>(FesGpIdentityMagic1);
	expected[FesGpIdentityTransportMajorIndex] =
		static_cast<std::uint16_t>(FesGpTransportMajor);
	expected[FesGpIdentityTransportMinorIndex] =
		static_cast<std::uint16_t>(FesGpTransportMinor);
	expected[FesGpIdentityAbiTagIndex] = static_cast<std::uint16_t>(FesGpAbiTag);
	expected[FesGpIdentityAbiMajorIndex] = static_cast<std::uint16_t>(descriptor.abi.major);
	expected[FesGpIdentityAbiMinorIndex] = static_cast<std::uint16_t>(descriptor.abi.minor);
	std::uint16_t capabilities = 0;
	for (const CoreInterface& interface : descriptor.interfaces) {
		if (interface.id == FesGpInterfaceGamepadID)
			capabilities = static_cast<std::uint16_t>(capabilities |
				FesGpCapabilityGamepad);
		else if (interface.id == FesGpInterfaceVideoFixed720p60ID)
			capabilities = static_cast<std::uint16_t>(capabilities |
				FesGpCapabilityVideoFixed720p60);
	}
	expected[FesGpIdentityCapabilitiesIndex] = capabilities;
	std::vector<std::uint16_t> build;
	if (!BuildWords(descriptor.build.id, &build))
		return Mismatch("package build ID is invalid for FES GP discovery");
	for (std::size_t index = 0; index < build.size(); ++index)
		expected[FesGpIdentityBuildIDStartIndex + index] = build[index];
	for (std::size_t index = 0; index < expected.size(); ++index) {
		if (index == FesGpIdentityCapabilitiesIndex) {
			if ((observed[index] & expected[index]) != expected[index])
				return Mismatch("live FES GP capabilities do not match package interfaces");
		} else if (observed[index] != expected[index]) {
			return Mismatch("live FES GP identity or build ID does not match package");
		}
	}
	return {};
}

} // namespace native
} // namespace mister
