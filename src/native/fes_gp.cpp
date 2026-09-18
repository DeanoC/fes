// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/fes_gp.hpp"
#include "native/artifacts.hpp"

#include "native/core_package.hpp"
#include "native/generated/de10_nano.hpp"
#include "native/generated/fes_gp.hpp"
#include "native/generated/fes_simple_computer.hpp"
#include "native/hardware.hpp"
#include "native/linux/mmio.hpp"

#include <algorithm>
#include <limits>
#include <string>
#include <utility>
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

Error WithPhase(Error error, const char* phase)
{
	if (!error.ok() && error.phase.empty()) error.phase = phase;
	return error;
}

Error Mismatch(const std::string& message, std::string expected = {},
	std::string observed = {})
{
	return {ErrorCode::core_mismatch, message, "identity",
		std::move(expected), std::move(observed)};
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

std::string BuildId(const std::vector<std::uint16_t>& words)
{
	static const char hex[] = "0123456789abcdef";
	std::string result;
	for (std::size_t index = FesGpIdentityBuildIDStartIndex;
		index < words.size(); ++index) {
		const std::uint16_t word = words[index];
		for (unsigned shift : {0u, 8u}) {
			const unsigned byte = (word >> shift) & 0xffu;
			result.push_back(hex[byte >> 4]);
			result.push_back(hex[byte & 0x0fu]);
		}
	}
	return result;
}

std::string WordEvidence(std::size_t index, std::uint16_t value)
{
	return "identity[" + std::to_string(index) + "]=" +
		std::to_string(value);
}

} // namespace

FesGp::FesGp(Mmio& mmio, Clock& clock) : mmio_(mmio), clock_(clock) {}

void FesGp::BeginSession()
{
	std::lock_guard<std::mutex> lock(mutex_);
	request_toggle_ = false;
	poisoned_ = false;
}

Error FesGp::Exchange(std::uint8_t opcode, std::uint8_t index,
	std::uint16_t argument, std::uint64_t deadline, std::uint16_t* response)
{
	std::lock_guard<std::mutex> lock(mutex_);
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

Error FesGp::Identify(const CoreDescriptor& descriptor, std::uint64_t deadline,
	bool* safe_to_quiesce, std::uint16_t* observed_capabilities)
{
	if (observed_capabilities != nullptr) *observed_capabilities = 0;
	if (safe_to_quiesce != nullptr) *safe_to_quiesce = false;
	const std::uint64_t now = clock_.NowMs();
	if (now >= deadline) return Io("FES GP discovery deadline exceeded");
	const std::uint64_t discovery_deadline = std::min(deadline,
		AddDeadline(now, kDiscoveryTimeoutMs));
	std::vector<std::uint16_t> observed;
	for (std::uint32_t index = 0; index < FesGpIdentityWordCount; ++index) {
		std::uint16_t word = 0;
		const Error error = Exchange(static_cast<std::uint8_t>(FesGpOpcodeIdentity),
			static_cast<std::uint8_t>(index), 0, discovery_deadline, &word);
		if (!error.ok()) return WithPhase(error, "transport");
		observed.push_back(word);
	}

	std::vector<std::uint16_t> expected(FesGpIdentityWordCount);
	expected[FesGpIdentityMagic0Index] = static_cast<std::uint16_t>(FesGpIdentityMagic0);
	expected[FesGpIdentityMagic1Index] = static_cast<std::uint16_t>(FesGpIdentityMagic1);
	expected[FesGpIdentityTransportMajorIndex] =
		static_cast<std::uint16_t>(FesGpTransportMajor);
	expected[FesGpIdentityTransportMinorIndex] =
		static_cast<std::uint16_t>(FesGpTransportMinor);
	const bool computer = descriptor.abi.id == FesSimpleComputerABIID;
	expected[FesGpIdentityAbiTagIndex] = static_cast<std::uint16_t>(
		computer ? FesSimpleComputerAbiTag : FesGpAbiTag);
	expected[FesGpIdentityAbiMajorIndex] = static_cast<std::uint16_t>(descriptor.abi.major);
	expected[FesGpIdentityAbiMinorIndex] = static_cast<std::uint16_t>(descriptor.abi.minor);
	std::uint16_t capabilities = 0;
	for (const CoreInterface& interface : descriptor.interfaces) {
		if (computer) {
			if (interface.id == FesSimpleComputerInterfaceKeyboardID)
				capabilities = static_cast<std::uint16_t>(capabilities |
					FesSimpleComputerCapabilityKeyboard);
			else if (interface.id == FesSimpleComputerInterfaceVideoFixed720p60ID)
				capabilities = static_cast<std::uint16_t>(capabilities |
					FesSimpleComputerCapabilityVideoFixed720p60);
			else if (interface.id == FesSimpleComputerInterfaceMediaBlobID)
				capabilities = static_cast<std::uint16_t>(capabilities |
					FesSimpleComputerCapabilityMediaBlob);
			else if (interface.id == FesSimpleComputerInterfaceMediaBlobStreamID &&
				interface.required &&
				interface.major == FesSimpleComputerInterfaceMediaBlobStreamMajor &&
				interface.minor == FesSimpleComputerInterfaceMediaBlobStreamMinor)
				capabilities = static_cast<std::uint16_t>(capabilities |
					FesSimpleComputerCapabilityMediaBlobStream);
			continue;
		}
		if (interface.id == FesGpInterfaceGamepadID)
			capabilities = static_cast<std::uint16_t>(capabilities |
				FesGpCapabilityGamepad);
		else if (interface.id == FesGpInterfacePersistenceWordsID)
			capabilities |= FesGpCapabilityPersistenceWords;
		else if (interface.id == FesGpInterfacePongProgressID)
			capabilities |= FesGpCapabilityPongProgress;
		else if (interface.id == FesGpInterfaceVideoFixed720p60ID)
			capabilities = static_cast<std::uint16_t>(capabilities |
				FesGpCapabilityVideoFixed720p60);
	}
	expected[FesGpIdentityCapabilitiesIndex] = capabilities;
	std::vector<std::uint16_t> build;
	if (!BuildWords(descriptor.build.id, &build))
		return Mismatch("package build ID is invalid for FES GP discovery",
			"32 lowercase hexadecimal characters", descriptor.build.id);
	for (std::size_t index = 0; index < build.size(); ++index)
		expected[FesGpIdentityBuildIDStartIndex + index] = build[index];
	for (std::size_t index = 0; index < expected.size(); ++index) {
		if (index == FesGpIdentityCapabilitiesIndex) {
			if ((observed[index] & expected[index]) != expected[index])
				return Mismatch("live FES GP capabilities do not match package interfaces",
					"capabilities=" + std::to_string(expected[index]),
					"capabilities=" + std::to_string(observed[index]));
		} else if (observed[index] != expected[index]) {
			if (index >= FesGpIdentityBuildIDStartIndex)
				return Mismatch("live FES GP build ID does not match package",
					descriptor.build.id, BuildId(observed));
			return Mismatch("live FES GP identity does not match package",
				WordEvidence(index, expected[index]),
				WordEvidence(index, observed[index]));
		}
		if (index == FesGpIdentityCapabilitiesIndex && safe_to_quiesce != nullptr)
			*safe_to_quiesce = true;
	}
	if (observed_capabilities != nullptr)
		*observed_capabilities = observed[FesGpIdentityCapabilitiesIndex];
	return {};
}

FesGpCoreDriver::FesGpCoreDriver(FesGp& gp) : gp_(gp) {}

void FesGpCoreDriver::BeginSession()
{
	gp_.BeginSession();
	persistence_verified_ = false;
	reset_held_ = true;
	freeze_attempted_ = false;
	computer_ = false;
	observed_capabilities_ = 0;
	stream_info_ = {};
	stream_verified_ = false;
	stream_pending_ = false;
}

CoreDriverResult FesGpCoreDriver::Quiesce(const CoreDriverContext&,
	std::uint64_t deadline)
{
	CoreDriverResult result = Gameplay(
		static_cast<std::uint16_t>(FesGpGameplayHoldReset), deadline);
	if (result.error.ok())
		reset_held_ = true;
	result.error = WithPhase(std::move(result.error), "quiesce");
	return result;
}

CoreDriverResult FesGpCoreDriver::Identify(const CoreDriverContext& context,
	std::uint64_t deadline)
{
	if (context.descriptor == nullptr)
		return {{ErrorCode::invalid_request, "missing FES GP descriptor",
			"request"}, false, ""};
	bool safe_to_quiesce = false;
	persistence_verified_ = false;
	stream_verified_ = false;
	stream_info_ = {};
	Error error = gp_.Identify(*context.descriptor, deadline, &safe_to_quiesce,
		&observed_capabilities_);
	computer_ = error.ok() && context.descriptor->abi.id == FesSimpleComputerABIID;
	bool stream_declared = false;
	for (const auto& interface : context.descriptor->interfaces)
		if (interface.id == FesSimpleComputerInterfaceMediaBlobStreamID &&
			interface.major == FesSimpleComputerInterfaceMediaBlobStreamMajor &&
			interface.minor == FesSimpleComputerInterfaceMediaBlobStreamMinor)
			stream_declared = true;
	if (computer_ && stream_declared &&
		(observed_capabilities_ & FesSimpleComputerCapabilityMediaBlobStream)) {
		// Query actual endpoint limits during discovery, separately from declarations.
		std::uint16_t words[5] = {};
		for (std::uint8_t index = 0; index < 5 && error.ok(); ++index)
			error = gp_.Exchange(FesSimpleComputerOpcodeMediaStreamInfo,
				index, 0, deadline, &words[index]);
		MediaStreamInfo info;
		info.minimum = words[0] | (static_cast<std::uint32_t>(words[1]) << 16);
		info.maximum = words[2] | (static_cast<std::uint32_t>(words[3]) << 16);
		info.chunk_bytes = words[4];
		if (error.ok() && (info.minimum != FesSimpleComputerMediaStreamMinBytes ||
			info.maximum < FesSimpleComputerMediaStreamGuaranteedMaxBytes ||
			info.maximum > FesSimpleComputerMediaStreamMaxBytes ||
			info.chunk_bytes != FesSimpleComputerMediaStreamChunkMaxBytes))
			error = Mismatch("invalid live media stream capacity");
		if (error.ok()) {
			stream_info_ = info;
			for (const auto& interface : context.descriptor->interfaces)
				if (interface.id == FesSimpleComputerInterfaceMediaBlobStreamID &&
					interface.major == FesSimpleComputerInterfaceMediaBlobStreamMajor &&
					interface.minor == FesSimpleComputerInterfaceMediaBlobStreamMinor)
					stream_verified_ = true;
		}
	}
	if (error.ok()) {
		bool words = false, pong = false;
		for (const auto& interface : context.descriptor->interfaces) {
			if (interface.id == FesGpInterfacePersistenceWordsID)
				words = interface.required &&
						interface.major == FesGpInterfacePersistenceWordsMajor &&
						interface.minor == FesGpInterfacePersistenceWordsMinor;
			if (interface.id == FesGpInterfacePongProgressID)
				pong = interface.required && interface.major == FesGpInterfacePongProgressMajor &&
					   interface.minor == FesGpInterfacePongProgressMinor;
		}
		if (words != pong)
			error = {ErrorCode::unsupported_interface,
				"persistence requires exactly one supported layout", "compatibility"};
		if (error.ok() && words) {
			const std::uint16_t expected[] = {FesGpPongProgressWordCount, FesGpPongProgressTag,
				FesGpInterfacePongProgressMajor, FesGpInterfacePongProgressMinor};
			for (std::uint8_t index = 0; index < 4 && error.ok(); ++index) {
				std::uint16_t value = 0;
				error = gp_.Exchange(FesGpOpcodeDataInfo, index, 0, deadline, &value);
				if (error.ok() && value != expected[index])
					error = Mismatch("live persistence layout differs from package");
			}
			persistence_verified_ = error.ok();
		}
	}
	return {error, false, error.ok() ? context.descriptor->core.id : "",
		safe_to_quiesce};
}

CoreDriverResult FesGpCoreDriver::NeutralizeButtons(
	const CoreDriverContext& context, std::uint64_t deadline)
{
	return SetButtons(context, 0, deadline);
}

CoreDriverResult FesGpCoreDriver::SetButtons(const CoreDriverContext&,
	std::uint16_t map, std::uint64_t deadline)
{
	if ((map & ~static_cast<std::uint16_t>(FesGpButtonMask)) != 0)
		return {{ErrorCode::invalid_request, "FES GP button mask is invalid"}, false, ""};
	std::uint16_t response = 0;
	const Error error = gp_.Exchange(static_cast<std::uint8_t>(FesGpOpcodeButtons),
		static_cast<std::uint8_t>(FesGpControlIndex), map, deadline, &response);
	if (!error.ok()) return {WithPhase(error, "input"), true, ""};
	if (response != map)
		return {{ErrorCode::io_failed, "FES GP accepted button mask is invalid",
			"input"}, true, ""};
	return {{}, true, ""};
}

CoreDriverResult FesGpCoreDriver::Gameplay(std::uint16_t argument,
	std::uint64_t deadline)
{
	std::uint16_t response = 0;
	const Error error = gp_.Exchange(static_cast<std::uint8_t>(FesGpOpcodeGameplay),
		static_cast<std::uint8_t>(FesGpControlIndex), argument, deadline, &response);
	if (!error.ok()) return {error, true, ""};
	if (response != 0)
		return {{ErrorCode::io_failed, "FES GP gameplay response is invalid"}, true, ""};
	return {{}, true, ""};
}

CoreDriverResult FesGpCoreDriver::NeutralizeKeyboard(std::uint64_t deadline)
{
	for (std::uint8_t row = 0; row < FesSimpleComputerKeyboardRowCount; ++row) {
		std::uint16_t response = 0;
		const Error error = gp_.Exchange(
			static_cast<std::uint8_t>(FesSimpleComputerOpcodeKeyboard), row,
			static_cast<std::uint16_t>(FesSimpleComputerKeyboardNeutralRow),
			deadline, &response);
		if (!error.ok())
			return {WithPhase(error, "input"), true, ""};
		if (response != 0)
			return {{ErrorCode::io_failed, "FES computer keyboard row is invalid",
				"input"}, true, ""};
	}
	return {{}, true, ""};
}

Error FesGpCoreDriver::SetKeyboardMatrix(std::uint64_t matrix, std::uint64_t deadline)
{
	if (!computer_)
		return {ErrorCode::unsupported_interface, "FES computer keyboard is inactive",
			"input"};
	if ((matrix & ~0xffffffffffull) != 0)
		return {ErrorCode::invalid_request, "FES computer keyboard matrix is invalid",
			"input"};
	for (std::uint8_t row = 0; row < FesSimpleComputerKeyboardRowCount; ++row) {
		const std::uint16_t mask = static_cast<std::uint16_t>(
			(matrix >> (row * 5u)) & FesSimpleComputerKeyboardRowMask);
		std::uint16_t response = 0;
		const Error error = gp_.Exchange(
			static_cast<std::uint8_t>(FesSimpleComputerOpcodeKeyboard), row, mask,
			deadline, &response);
		if (!error.ok()) return WithPhase(error, "input");
		if (response != 0)
			return {ErrorCode::io_failed, "FES computer keyboard row is invalid",
				"input"};
	}
	return {};
}

Error FesGpCoreDriver::LoadMedia(
	const std::vector<std::uint8_t>& bytes, std::uint64_t deadline)
{
	if (stream_pending_) return Io("media stream requires recovery before legacy media");
	if (!computer_)
		return {ErrorCode::unsupported_interface, "FES computer media is inactive",
			"input"};
	if (bytes.size() < FesSimpleComputerMediaMinBytes ||
		bytes.size() > FesSimpleComputerMediaMaxBytes)
		return {ErrorCode::invalid_request, "FES computer media size is invalid",
			"request"};
	// The computer consumes committed media while execution reset is held.
	// Never release after a partial transfer or an unacknowledged commit.
	CoreDriverResult held = Quiesce({}, deadline);
	if (!held.error.ok()) return held.error;
	std::uint16_t response = 0;
	Error error = gp_.Exchange(static_cast<std::uint8_t>(FesSimpleComputerOpcodeMediaBegin),
		static_cast<std::uint8_t>(FesSimpleComputerControlIndex),
		static_cast<std::uint16_t>(bytes.size()), deadline, &response);
	if (!error.ok()) return WithPhase(error, "input");
	if (response != 0)
		return {ErrorCode::io_failed, "FES computer media begin failed", "input"};
	for (std::size_t offset = 0; offset + 1 < bytes.size(); offset += 2) {
		const std::uint16_t pair = static_cast<std::uint16_t>(
			bytes[offset] | (static_cast<std::uint16_t>(bytes[offset + 1]) << 8));
		error = gp_.Exchange(static_cast<std::uint8_t>(FesSimpleComputerOpcodeMediaData),
			static_cast<std::uint8_t>(FesSimpleComputerMediaDataPairIndex), pair,
			deadline, &response);
		if (!error.ok()) return WithPhase(error, "input");
		if (response != 0)
			return {ErrorCode::io_failed, "FES computer media data failed", "input"};
	}
	if ((bytes.size() % 2) != 0) {
		error = gp_.Exchange(static_cast<std::uint8_t>(FesSimpleComputerOpcodeMediaData),
			static_cast<std::uint8_t>(FesSimpleComputerMediaDataTailIndex),
			bytes.back(), deadline, &response);
		if (!error.ok()) return WithPhase(error, "input");
		if (response != 0)
			return {ErrorCode::io_failed, "FES computer media tail failed", "input"};
	}
	error = gp_.Exchange(static_cast<std::uint8_t>(FesSimpleComputerOpcodeMediaCommit),
		static_cast<std::uint8_t>(FesSimpleComputerControlIndex), 0, deadline, &response);
	if (!error.ok()) return WithPhase(error, "input");
	if (response != 0)
		return {ErrorCode::io_failed, "FES computer media commit failed", "input"};
	CoreDriverResult released = Gameplay(
		static_cast<std::uint16_t>(FesGpGameplayRelease), deadline);
	if (released.error.ok()) reset_held_ = false;
	return WithPhase(std::move(released.error), "input");
}

Error FesGpCoreDriver::StreamInfo(MediaStreamInfo* output) const
{
	if (!stream_verified_ || output == nullptr)
		return {ErrorCode::unsupported_interface, "verified media stream is unavailable",
			"compatibility"};
	*output = stream_info_;
	return {};
}

Error FesGpCoreDriver::StreamCommand(std::uint8_t opcode, std::uint8_t index,
	std::uint16_t argument, std::uint64_t deadline)
{
	std::uint16_t response = 0;
	Error error = gp_.Exchange(opcode, index, argument, deadline, &response);
	if (error.ok() && response != 0) error = Io("invalid media stream response");
	return WithPhase(error, "input");
}

Error FesGpCoreDriver::AbortMediaStream(std::uint64_t deadline)
{
	if (!stream_pending_) return {};
	// Exchange refuses all writes after an ambiguous ACK; never replay a mutation.
	Error error = StreamCommand(FesSimpleComputerOpcodeMediaStreamAbort,
		FesSimpleComputerControlIndex, 0, deadline);
	if (error.ok()) stream_pending_ = false;
	return error;
}

Error FesGpCoreDriver::LoadMediaStream(const ComputerMediaSnapshot& media,
	Clock& clock, std::uint64_t deadline)
{
	MediaStreamInfo info;
	Error error = StreamInfo(&info);
	if (!error.ok()) return error;
	if (stream_pending_) return Io("media stream requires recovery");
	if (media.size() < info.minimum || media.size() > info.maximum)
		return {ErrorCode::invalid_request, "media exceeds live stream capacity", "request"};
	// Also retain ownership if hold-reset itself completes ambiguously.
	stream_pending_ = true;
	error = Quiesce({}, deadline).error;
	if (!error.ok()) return error;
	// Mark pending before the first Begin word: its acceptance invalidates readiness.
	stream_pending_ = true;
	const std::uint16_t header[] = {static_cast<std::uint16_t>(media.size()),
		static_cast<std::uint16_t>(media.size() >> 16),
		static_cast<std::uint16_t>(media.crc32()),
		static_cast<std::uint16_t>(media.crc32() >> 16)};
	for (std::uint8_t index = 0; index < 4 && error.ok(); ++index)
		error = StreamCommand(FesSimpleComputerOpcodeMediaStreamBegin,
			index, header[index], deadline);
	std::array<std::uint8_t, FesSimpleComputerMediaStreamChunkMaxBytes> bytes = {};
	for (std::uint32_t offset = 0; offset < media.size() && error.ok();) {
		const auto length = std::min<std::uint32_t>(bytes.size(), media.size() - offset);
		error = media.Read(offset, bytes.data(), length, clock, deadline);
		const std::uint16_t chunk[] = {static_cast<std::uint16_t>(offset),
			static_cast<std::uint16_t>(offset >> 16), static_cast<std::uint16_t>(length)};
		for (std::uint8_t index = 0; index < 3 && error.ok(); ++index)
			error = StreamCommand(FesSimpleComputerOpcodeMediaStreamChunk,
				index, chunk[index], deadline);
		for (std::uint32_t byte = 0; byte < length && error.ok(); byte += 2) {
			const std::uint16_t word = static_cast<std::uint16_t>(bytes[byte] |
				(byte + 1 < length ? static_cast<std::uint16_t>(bytes[byte + 1]) << 8 : 0));
			error = StreamCommand(FesSimpleComputerOpcodeMediaStreamData,
				static_cast<std::uint8_t>(byte / 2), word, deadline);
		}
		offset += length;
	}
	if (error.ok()) error = StreamCommand(FesSimpleComputerOpcodeMediaStreamCommit,
		FesSimpleComputerControlIndex, 0, deadline);
	if (!error.ok()) return error;
	error = Gameplay(FesGpGameplayRelease, deadline).error;
	if (error.ok()) {
		reset_held_ = false;
		stream_pending_ = false;
	}
	return WithPhase(error, "input");
}

CoreDriverResult FesGpCoreDriver::Start(const CoreDriverContext&,
	std::uint64_t deadline)
{
	if (stream_pending_) return {Io("incomplete media stream cannot start"), false, ""};
	if (computer_) {
		CoreDriverResult neutralized = NeutralizeKeyboard(deadline);
		if (!neutralized.error.ok()) return neutralized;
		// Fresh stream endpoints have no committed media and reject release.
		// Activation publishes the owned session; media commit releases it later.
		if (stream_verified_) return Quiesce({}, deadline);
	}
	CoreDriverResult result = Gameplay(
		static_cast<std::uint16_t>(FesGpGameplayRelease), deadline);
	if (result.error.ok())
		reset_held_ = false;
	result.error = WithPhase(std::move(result.error), "transport");
	return result;
}

Error FesGpCoreDriver::DataControl(std::uint16_t argument, std::uint64_t deadline)
{
	std::uint16_t response = 0;
	Error error =
		gp_.Exchange(FesGpOpcodeDataControl, FesGpControlIndex, argument, deadline, &response);
	if (error.ok() && response != 0)
		error = Io("invalid persistence control response");
	return WithPhase(error, "core_data");
}
Error FesGpCoreDriver::CaptureData(
	const CoreDriverContext&, std::uint64_t deadline, std::vector<std::uint16_t>* output)
{
	if (!persistence_verified_ || reset_held_ || !output)
		return Io("persistence snapshot is unavailable");
	freeze_attempted_ = true;
	Error error = DataControl(FesGpDataFreeze, deadline);
	std::vector<std::uint16_t> snapshot;
	for (std::uint16_t index = 0; index < FesGpPongProgressWordCount && error.ok(); ++index) {
		std::uint16_t value = 0;
		error = gp_.Exchange(
			FesGpOpcodeDataRead, static_cast<std::uint8_t>(index), 0, deadline, &value);
		if (error.ok())
			snapshot.push_back(value);
	}
	if (error.ok() && snapshot[FesGpPongPaddleSpeedIndex] > FesGpPongPaddleSpeedFast)
		error = Io("invalid persistence snapshot setting");
	if (error.ok())
		*output = std::move(snapshot);
	return WithPhase(error, "core_data");
}
Error FesGpCoreDriver::RestoreData(
	const CoreDriverContext&, const std::vector<std::uint16_t>& words, std::uint64_t deadline)
{
	if (!persistence_verified_ || !reset_held_ || words.size() != FesGpPongProgressWordCount ||
		words[FesGpPongPaddleSpeedIndex] > FesGpPongPaddleSpeedFast)
		return Io("persistence restore is unavailable or invalid");
	Error error = DataControl(FesGpDataBegin, deadline);
	for (std::size_t index = 0; index < words.size() && error.ok(); ++index) {
		std::uint16_t response = 0;
		error = gp_.Exchange(FesGpOpcodeDataWrite, static_cast<std::uint8_t>(index), words[index],
			deadline, &response);
		if (error.ok() && response != 0)
			error = Io("invalid persistence write response");
	}
	if (error.ok())
		error = DataControl(FesGpDataCommit, deadline);
	return WithPhase(error, "core_data");
}
Error FesGpCoreDriver::ResumeData(const CoreDriverContext&, std::uint64_t deadline)
{
	if (!persistence_verified_)
		return Io("persistence resume is unavailable");
	if (!freeze_attempted_)
		return {};
	Error error = DataControl(FesGpDataResume, deadline);
	if (error.ok())
		freeze_attempted_ = false;
	return error;
}

} // namespace native
} // namespace mister
