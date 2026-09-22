// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/fes_gp.hpp"
#include "native/artifacts.hpp"

#include "native/core_package.hpp"
#include "native/generated/de10_nano.hpp"
#include "native/generated/fes_gp.hpp"
#include "native/generated/fes_simple_computer.hpp"
#include "native/generated/fes_application.hpp"
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
// The $0347 patch copies at most one byte per ce_cpu_p, and ce_cpu_p is one
// pulse per 16 cycles of the 52 MHz system clock. A full 16384-byte blob is
// 16384 * 16 / 52e6 seconds (5.034 ms). Cores sealed before media_busy cannot
// report that the copy is running; a legal media begin is what clears
// readiness, so it has to wait until a copy already in progress has finished.
constexpr std::uint64_t kLegacyLoaderCopyBoundMs = 8u;
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

// Application media deliberately reuses the computer codec, but never its
// keyboard opcode or ABI identity. Detect contract drift before building a driver.
static_assert(FesApplicationOpcodeIdentity == FesGpOpcodeIdentity &&
	FesApplicationOpcodeExecution == FesGpOpcodeGameplay &&
	FesApplicationOpcodeButtons == FesGpOpcodeButtons &&
	FesApplicationButtonMask == FesGpButtonMask &&
	FesApplicationOpcodeMediaBegin == FesSimpleComputerOpcodeMediaBegin &&
	FesApplicationOpcodeMediaData == FesSimpleComputerOpcodeMediaData &&
	FesApplicationOpcodeMediaCommit == FesSimpleComputerOpcodeMediaCommit &&
	FesApplicationOpcodeMediaStreamInfo == FesSimpleComputerOpcodeMediaStreamInfo &&
	FesApplicationOpcodeMediaStreamBegin == FesSimpleComputerOpcodeMediaStreamBegin &&
	FesApplicationOpcodeMediaStreamChunk == FesSimpleComputerOpcodeMediaStreamChunk &&
	FesApplicationOpcodeMediaStreamData == FesSimpleComputerOpcodeMediaStreamData &&
	FesApplicationOpcodeMediaStreamCommit == FesSimpleComputerOpcodeMediaStreamCommit &&
	FesApplicationOpcodeMediaStreamAbort == FesSimpleComputerOpcodeMediaStreamAbort,
	"application codec requires matching shared control and media opcodes");

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

bool ExchangeTransportGlitch(const Error& error)
{
	if (error.code != ErrorCode::io_failed) return false;
	const std::string& message = error.message;
	return message.find("FES GP exchange state is ambiguous") != std::string::npos ||
		message.find("FES GP exchange deadline exceeded") != std::string::npos ||
		message.find("FES GP response stability deadline exceeded") != std::string::npos ||
		message.find("unstable FES GP response") != std::string::npos ||
		message.find("invalid FES GP response signature") != std::string::npos;
}

bool CommandRejected(const Error& error, std::uint32_t code)
{
	return error.code == ErrorCode::io_failed &&
		error.message == "FES GP command rejected with response " + std::to_string(code);
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

bool FesGp::Poisoned()
{
	std::lock_guard<std::mutex> lock(mutex_);
	return poisoned_;
}

Error FesGp::Realign(std::uint64_t deadline)
{
	std::lock_guard<std::mutex> lock(mutex_);
	if (!poisoned_) return {};
	if (clock_.NowMs() >= deadline)
		return Io("FES GP exchange deadline exceeded");
	std::uint32_t observed = 0;
	Error error = mmio_.Read32(generated::kSpiGpiAddress, &observed);
	if (!error.ok()) return error;
	if ((observed & kResponseFixedMask) != FesGpSignature)
		return Io("invalid FES GP response signature or reserved bits");
	if (clock_.NowMs() >= deadline)
		return Io("FES GP response stability deadline exceeded");
	std::uint32_t confirmed = 0;
	error = mmio_.Read32(generated::kSpiGpiAddress, &confirmed);
	if (!error.ok()) return error;
	if (confirmed != observed) return Io("unstable FES GP response");
	request_toggle_ = (confirmed & FesGpAckMask) != 0;
	poisoned_ = false;
	return {};
}

std::uint64_t FesGp::NowMs() const
{
	return clock_.NowMs();
}

Error FesGp::WaitUntilMs(std::uint64_t absolute_ms, std::uint64_t deadline)
{
	std::uint64_t now = clock_.NowMs();
	while (now < absolute_ms) {
		if (now >= deadline)
			return Io("FES GP exchange deadline exceeded");
		const std::uint64_t next = clock_.NowMs();
		// A clock that does not advance cannot prove the loader copy ended.
		if (next <= now)
			return Io("FES GP exchange deadline exceeded");
		now = next;
	}
	if (now >= deadline)
		return Io("FES GP exchange deadline exceeded");
	return {};
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

	bool response_seen = false;
	bool last_ack = false;
	for (;;) {
		if (clock_.NowMs() >= exchange_deadline) {
			poisoned_ = true;
			// Identify the stalled handshake without logging media arguments or
			// response data. Preserve the existing deadline and no-retry rule.
			return Io("FES GP exchange deadline exceeded: opcode=" +
				std::to_string(opcode) + " index=" + std::to_string(index) +
				" request=" + std::to_string(next_toggle) + " ack=" +
				(response_seen ? std::to_string(last_ack) : "unobserved"));
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
		response_seen = true;
		last_ack = acknowledged;
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
	const bool application = descriptor.abi.id == FesApplicationABIID;
	expected[FesGpIdentityAbiTagIndex] = static_cast<std::uint16_t>(
		application ? FesApplicationAbiTag : computer ? FesSimpleComputerAbiTag : FesGpAbiTag);
	expected[FesGpIdentityAbiMajorIndex] = static_cast<std::uint16_t>(descriptor.abi.major);
	expected[FesGpIdentityAbiMinorIndex] = static_cast<std::uint16_t>(descriptor.abi.minor);
	std::uint16_t capabilities = 0;
	for (const CoreInterface& interface : descriptor.interfaces) {
		if (application) {
			if (!interface.required || interface.major != 1 || interface.minor != 0) continue;
			if (interface.id == FesApplicationInterfaceGamepadID)
				capabilities |= FesApplicationCapabilityGamepad;
			else if (interface.id == FesApplicationInterfaceVideoFixed720p60ID)
				capabilities |= FesApplicationCapabilityVideoFixed720p60;
			else if (interface.id == FesApplicationInterfaceMediaBlobID)
				capabilities |= FesApplicationCapabilityMediaBlob;
			else if (interface.id == FesApplicationInterfaceMediaBlobStreamID)
				capabilities |= FesApplicationCapabilityMediaBlobStream;
			else if (interface.id == FesApplicationInterfaceAudioPcmS16Stereo48kID)
				capabilities |= FesApplicationCapabilityAudioPcmS16Stereo48k;
			else if (interface.id == FesApplicationInterfaceGamepadPortsID)
				capabilities |= FesApplicationCapabilityGamepadPorts;
			else if (interface.id == FesApplicationInterfaceKeypadPortsID)
				capabilities |= FesApplicationCapabilityKeypadPorts;
			continue;
		}
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
			const std::uint16_t application_mask = FesApplicationCapabilityGamepad |
				FesApplicationCapabilityVideoFixed720p60 | FesApplicationCapabilityMediaBlob |
				FesApplicationCapabilityMediaBlobStream | FesApplicationCapabilityAudioPcmS16Stereo48k |
				FesApplicationCapabilityGamepadPorts | FesApplicationCapabilityKeypadPorts;
			if ((application && (observed[index] & application_mask) != expected[index]) ||
				(!application && (observed[index] & expected[index]) != expected[index]))
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
	application_ = false;
	media_ = false;
	firmware_ = false;
	gamepad_ = false;
	controller_ports_ = false;
	keypad_ports_ = false;
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
	if (result.error.ok()) {
		reset_held_ = true;
		result.error = NeutralizeControllers(deadline);
	}
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
	application_ = error.ok() && context.descriptor->abi.id == FesApplicationABIID;
	media_ = computer_;
	gamepad_ = error.ok() && context.descriptor->abi.id == FesGpABIID;
	if (application_) {
		for (const auto& interface : context.descriptor->interfaces) {
			if (interface.major != 1 || interface.minor != 0) continue;
			if (interface.id == FesApplicationInterfaceMediaBlobID)
				media_ = (observed_capabilities_ & FesApplicationCapabilityMediaBlob) != 0;
			if (interface.id == FesApplicationInterfaceFirmwareBlobID)
				firmware_ = (observed_capabilities_ & FesApplicationCapabilityFirmwareBlob) != 0;
			if (interface.id == FesApplicationInterfaceGamepadID)
				gamepad_ = (observed_capabilities_ & FesApplicationCapabilityGamepad) != 0;
			if (interface.id == FesApplicationInterfaceGamepadPortsID)
				controller_ports_ = (observed_capabilities_ & FesApplicationCapabilityGamepadPorts) != 0;
			if (interface.id == FesApplicationInterfaceKeypadPortsID)
				keypad_ports_ = (observed_capabilities_ & FesApplicationCapabilityKeypadPorts) != 0;
		}
	}
	bool stream_declared = false;
	for (const auto& interface : context.descriptor->interfaces)
		if (interface.id == FesSimpleComputerInterfaceMediaBlobStreamID &&
			interface.major == FesSimpleComputerInterfaceMediaBlobStreamMajor &&
			interface.minor == FesSimpleComputerInterfaceMediaBlobStreamMinor)
			stream_declared = true;
	if (media_ && stream_declared &&
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
	if (error.ok()) {
		identified_ = *context.descriptor;
		have_identity_ = true;
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
	if (application_ && !gamepad_)
		return {{ErrorCode::unsupported_interface, "application gamepad is inactive", "input"}, false, ""};
	if ((map & ~static_cast<std::uint16_t>(FesGpButtonMask)) != 0)
		return {{ErrorCode::invalid_request, "FES GP button mask is invalid"}, false, ""};
	std::uint16_t response = 0;
	const Error error = gp_.Exchange(static_cast<std::uint8_t>(FesGpOpcodeButtons),
		static_cast<std::uint8_t>(FesGpControlIndex), map, deadline, &response);
	if (!error.ok()) return {WithPhase(error, "input"), true, ""};
	if (response != (application_ ? 0 : map))
		return {{ErrorCode::io_failed, "FES GP accepted button mask is invalid",
			"input"}, true, ""};
	return {{}, true, ""};
}

Error FesGpCoreDriver::SetController(std::uint8_t port, std::uint16_t buttons,
	std::uint16_t keypad, std::uint64_t deadline)
{
	if (!application_ || !controller_ports_)
		return {ErrorCode::unsupported_interface, "controller ports are inactive", "input"};
	if (port >= FesApplicationControllerPortCount ||
		(buttons & ~FesApplicationControllerButtonMask) != 0 ||
		(keypad & ~FesApplicationControllerKeypadMask) != 0)
		return {ErrorCode::invalid_request, "invalid controller snapshot", "input"};
	if (keypad != 0 && !keypad_ports_)
		return {ErrorCode::unsupported_interface, "keypad ports are inactive", "input"};
	std::uint16_t response = 0;
	Error error = gp_.Exchange(FesApplicationOpcodeControllerButtons, port,
		buttons, deadline, &response);
	if (error.ok() && response != 0)
		error = {ErrorCode::io_failed, "invalid controller acknowledgement", "input"};
	if (error.ok() && keypad_ports_) {
		error = gp_.Exchange(FesApplicationOpcodeControllerKeypad, port,
			keypad, deadline, &response);
		if (error.ok() && response != 0)
			error = {ErrorCode::io_failed, "invalid keypad acknowledgement", "input"};
	}
	return WithPhase(error, "input");
}

Error FesGpCoreDriver::NeutralizeControllers(std::uint64_t deadline)
{
	if (!controller_ports_) return {};
	for (std::uint8_t port = 0; port < FesApplicationControllerPortCount; ++port) {
		const Error error = SetController(port, 0, 0, deadline);
		if (!error.ok()) return error;
	}
	return {};
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

Error FesGpCoreDriver::MediaBusyOrIo(const Error& error) const
{
	// Endpoint invalid-state (error 4) while the tape-loader is copying maps to
	// busy so hosts can retry without tearing down the session.
	if (error.code == ErrorCode::io_failed &&
		error.message.find("response " +
			std::to_string(FesSimpleComputerErrorInvalidState)) != std::string::npos)
		return {ErrorCode::busy, "tape loader is busy", "input"};
	return error;
}

Error FesGpCoreDriver::TransferMediaBlob(
	const std::vector<std::uint8_t>& bytes, std::uint64_t deadline, bool hold_reset)
{
	if (stream_pending_) return Io("media stream requires recovery before legacy media");
	if (!media_)
		return {ErrorCode::unsupported_interface, "FES computer media is inactive",
			"input"};
	if (bytes.size() < FesSimpleComputerMediaMinBytes ||
		bytes.size() > FesSimpleComputerMediaMaxBytes)
		return {ErrorCode::invalid_request, "FES computer media size is invalid",
			"request"};
	if (hold_reset) {
		// Launch bind: the computer consumes committed media while execution
		// reset is held. Never release after a partial transfer or an
		// unacknowledged commit.
		CoreDriverResult held = Quiesce({}, deadline);
		if (!held.error.ok()) return held.error;
	} else if (reset_held_) {
		return {ErrorCode::busy, "execution reset is held; use launch media bind",
			"input"};
	}
	std::uint16_t response = 0;
	Error error = gp_.Exchange(static_cast<std::uint8_t>(FesSimpleComputerOpcodeMediaBegin),
		static_cast<std::uint8_t>(FesSimpleComputerControlIndex),
		static_cast<std::uint16_t>(bytes.size()), deadline, &response);
	if (!error.ok()) return MediaBusyOrIo(WithPhase(error, "input"));
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
	if (!hold_reset) return {};
	CoreDriverResult released = Gameplay(
		static_cast<std::uint16_t>(FesGpGameplayRelease), deadline);
	if (released.error.ok()) reset_held_ = false;
	return WithPhase(std::move(released.error), "input");
}

Error FesGpCoreDriver::LoadMedia(
	const std::vector<std::uint8_t>& bytes, std::uint64_t deadline)
{
	return TransferMediaBlob(bytes, deadline, true);
}

Error FesGpCoreDriver::LoadMediaLive(
	const std::vector<std::uint8_t>& bytes, std::uint64_t deadline)
{
	return TransferMediaBlob(bytes, deadline, false);
}

Error FesGpCoreDriver::RecoverPoisonedMediaLink(std::uint64_t deadline)
{
	// A keyboard exchange can poison the toggle without taking the core down.
	// Realign from the live ACK, then re-identify, before eject mutates media.
	const Error realigned = gp_.Realign(deadline);
	if (!realigned.ok() || !have_identity_)
		return {ErrorCode::busy, "tape loader is busy", "input"};
	const Error identified = gp_.Identify(identified_, deadline);
	if (!identified.ok())
		return {ErrorCode::busy, "tape loader is busy", "input"};
	return {};
}

Error FesGpCoreDriver::ClearMedia(std::uint64_t deadline)
{
	if (stream_pending_) return Io("media stream requires recovery before clear media");
	if (!media_)
		return {ErrorCode::unsupported_interface, "FES computer media is inactive",
			"input"};
	if (reset_held_)
		return {ErrorCode::busy, "execution reset is held; use launch media bind",
			"input"};
	if (gp_.Poisoned()) {
		const Error recovered = RecoverPoisonedMediaLink(deadline);
		if (!recovered.ok()) return recovered;
	}
	// A completed rejection is not a poisoned toggle, so re-identify does not
	// run. Classify the acknowledgement the core actually returned.
	const auto classify = [&](Error exchange, std::uint16_t response) -> Error {
		if (!exchange.ok()) {
			exchange = WithPhase(std::move(exchange), "input");
			// Deadline, unstable ACK, or a poisoned toggle after HID traffic is
			// retryable. A hard MMIO failure stays io_failed so the host can still
			// tell a dead link from a busy loader.
			if (ExchangeTransportGlitch(exchange)) {
				(void)gp_.Realign(deadline);
				return {ErrorCode::busy, "tape loader is busy", "input"};
			}
			return MediaBusyOrIo(exchange);
		}
		if (response != 0)
			return {ErrorCode::io_failed, "FES computer media clear failed", "input"};
		return {};
	};
	std::uint16_t response = 0;
	Error error = gp_.Exchange(static_cast<std::uint8_t>(FesSimpleComputerOpcodeMediaBegin),
		static_cast<std::uint8_t>(FesSimpleComputerMediaEjectIndex), 0, deadline, &response);
	// Response 2 is invalid index, not invalid state. Cores sealed before
	// MediaEjectIndex check the index first, so they answer 2 even while
	// media_busy is high and never reach the invalid-state (4) reject.
	if (!CommandRejected(error, FesSimpleComputerErrorInvalidIndex))
		return classify(error, response);

	// Cores that added eject before MediaEjectIndex use control-index begin
	// with argument 0. Invalid state on that command is still a busy loader.
	error = gp_.Exchange(static_cast<std::uint8_t>(FesSimpleComputerOpcodeMediaBegin),
		static_cast<std::uint8_t>(FesSimpleComputerControlIndex), 0, deadline, &response);
	// Response 3 is invalid argument, not busy (busy is response 4). The
	// sealed golden mailbox rejects argument 0 because it is below
	// MediaMinBytes; media-begin-zero stays that reject, and those bitstreams
	// have no media_busy input. The busy guard arrived in the same change as
	// argument-0 eject, so this core never answers a later begin with error 4.
	// Any legal begin drops media_ready and media_size immediately. A copy
	// already inside $0347 finishes within kLegacyLoaderCopyBoundMs while
	// readiness is left alone; only then is the minimum begin issued.
	// Do not commit afterwards: commit would mark the minimum blob ready.
	if (CommandRejected(error, FesSimpleComputerErrorInvalidArgument)) {
		const std::uint64_t now = gp_.NowMs();
		const std::uint64_t idle_at = now > std::numeric_limits<std::uint64_t>::max() -
			kLegacyLoaderCopyBoundMs ? std::numeric_limits<std::uint64_t>::max() :
			now + kLegacyLoaderCopyBoundMs;
		if (idle_at >= deadline)
			return {ErrorCode::busy, "tape loader is busy", "input"};
		const Error waited = gp_.WaitUntilMs(idle_at, deadline);
		if (!waited.ok())
			return {ErrorCode::busy, "tape loader is busy", "input"};
		error = gp_.Exchange(static_cast<std::uint8_t>(FesSimpleComputerOpcodeMediaBegin),
			static_cast<std::uint8_t>(FesSimpleComputerControlIndex),
			static_cast<std::uint16_t>(FesSimpleComputerMediaMinBytes),
			deadline, &response);
		const Error classified = classify(error, response);
		if (classified.ok() || classified.code == ErrorCode::busy) return classified;
		return {ErrorCode::io_failed,
			"FES computer media clear failed after sealed media begin: " +
				classified.message,
			"input"};
	}
	const Error classified = classify(error, response);
	if (classified.ok() || classified.code == ErrorCode::busy) return classified;
	return {ErrorCode::io_failed,
		"FES computer media clear failed after invalid eject index: " +
			classified.message,
		"input"};
}

Error FesGpCoreDriver::LoadFirmware(
	const std::vector<std::uint8_t>& bytes, std::uint64_t deadline)
{
	if (stream_pending_) return Io("media stream requires recovery before firmware");
	if (!firmware_)
		return {ErrorCode::unsupported_interface, "FES firmware slot is inactive",
			"input"};
	if (bytes.size() != FesApplicationFirmwareBytes)
		return {ErrorCode::invalid_request, "FES firmware size is invalid",
			"request"};
	// Hold reset through firmware commit. LoadMedia still owns release.
	CoreDriverResult held = Quiesce({}, deadline);
	if (!held.error.ok()) return held.error;
	std::uint16_t response = 0;
	Error error = gp_.Exchange(static_cast<std::uint8_t>(FesApplicationOpcodeFirmwareBegin),
		static_cast<std::uint8_t>(FesApplicationControlIndex),
		static_cast<std::uint16_t>(FesApplicationFirmwareBytes), deadline, &response);
	if (!error.ok()) return WithPhase(error, "input");
	if (response != 0)
		return {ErrorCode::io_failed, "FES firmware begin failed", "input"};
	for (std::size_t offset = 0; offset + 1 < bytes.size(); offset += 2) {
		const std::uint16_t pair = static_cast<std::uint16_t>(
			bytes[offset] | (static_cast<std::uint16_t>(bytes[offset + 1]) << 8));
		error = gp_.Exchange(static_cast<std::uint8_t>(FesApplicationOpcodeFirmwareData),
			static_cast<std::uint8_t>(FesApplicationFirmwareDataPairIndex), pair,
			deadline, &response);
		if (!error.ok()) return WithPhase(error, "input");
		if (response != 0)
			return {ErrorCode::io_failed, "FES firmware data failed", "input"};
	}
	error = gp_.Exchange(static_cast<std::uint8_t>(FesApplicationOpcodeFirmwareCommit),
		static_cast<std::uint8_t>(FesApplicationControlIndex), 0, deadline, &response);
	if (!error.ok()) return WithPhase(error, "input");
	if (response != 0)
		return {ErrorCode::io_failed, "FES firmware commit failed", "input"};
	return {};
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
	const Error controller_neutral = NeutralizeControllers(deadline);
	if (!controller_neutral.ok()) return {controller_neutral, true, ""};
	if (computer_) {
		CoreDriverResult neutralized = NeutralizeKeyboard(deadline);
		if (!neutralized.error.ok()) return neutralized;
	}
	// Fresh stream endpoints and media-bearing applications reject release
	// until media is committed. Activation publishes the reset-held session.
	if (stream_verified_ || (application_ && media_)) return Quiesce({}, deadline);
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
