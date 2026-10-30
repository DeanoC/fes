// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/de25_computer.hpp"
#include "native/generated/fes_de25_computer.hpp"
#include "native/generated/fes_computer.hpp"
#include "native/hardware.hpp"
#include "native/linux/mmio.hpp"
#include <algorithm>
#include <array>
#include <thread>

namespace mister {
namespace native {
namespace {
using namespace generated;
constexpr std::uint32_t kResponseFixedMask =
	~(FesComputerAckMask | FesComputerErrorMask | FesComputerResponseMask);
constexpr std::uint16_t kCapabilities = FesComputerCapabilityVideoFixed720p60 |
	FesComputerCapabilityKeyboardHid | FesComputerCapabilityMediaZx81Tape;
static_assert(FesComputerIdentityBuildIDStartIndex + 8 == FesComputerIdentityWordCount,
	"DE25 identity must contain exactly 128 build bits");
static_assert(FesComputerKeyboardRowCount == 9 && FesComputerKeyboardModifierRow == 8,
	"DE25 HID rows must match the native row type");
static_assert(FesComputerMediaChunkMaxBytes == 512,
	"DE25 default chunk must match the generated contract");

Error Io(const std::string& message) { return {ErrorCode::io_failed, message, "de25-transport"}; }
Error Invalid(const std::string& message) { return {ErrorCode::invalid_request, message, "de25-control"}; }
Error Rejected(std::uint8_t opcode, std::uint16_t code)
{
	return {code == FesComputerErrorInvalidState ? ErrorCode::busy : ErrorCode::io_failed,
		"DE25 command " + std::to_string(opcode) + " rejected with response " + std::to_string(code), "de25-command"};
}
bool BuildId(const std::string& text, std::string* canonical)
{
	if (text.size() != 32) return false;
	canonical->clear(); bool nonzero = false;
	for (char c : text) {
		if (c >= 'A' && c <= 'F') c = static_cast<char>(c - 'A' + 'a');
		if (!((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'))) return false;
		nonzero = nonzero || c != '0'; canonical->push_back(c);
	}
	return nonzero;
}
std::string BuildId(const std::array<std::uint16_t, FesComputerIdentityWordCount>& words)
{
	static const char hex[] = "0123456789abcdef";
	std::string result;
	for (unsigned i = FesComputerIdentityBuildIDStartIndex; i < words.size(); ++i)
		for (unsigned shift : {0u, 8u}) {
			const auto byte = (words[i] >> shift) & 255;
			result.push_back(hex[byte >> 4]); result.push_back(hex[byte & 15]);
		}
	return result;
}
} // namespace

De25Computer::De25Computer(Mmio& io, Clock& clock) : io_(io), clock_(clock) {}
bool De25Computer::Poisoned() const { std::lock_guard<std::mutex> lock(mutex_); return poisoned_; }
Error De25Computer::Fail(const std::string& message)
{
	poisoned_ = true; identified_ = false; return Io(message);
}
Error De25Computer::RequireIdentity() const
{
	if (poisoned_) return Io("discard the failed DE25 client; no further commands are allowed");
	return identified_ ? Error{} : Io("validate the selected DE25 build before issuing controls");
}
Error De25Computer::Begin(std::uint64_t deadline)
{
	if (poisoned_) return Io("discard the failed DE25 client; no further commands are allowed");
	last_time_ = clock_.NowMs();
	if (last_time_ >= deadline) return Io("DE25 operation deadline already expired");
	deadline_ = deadline;
	wall_deadline_ = std::chrono::steady_clock::now() +
		std::chrono::milliseconds(std::min<std::uint64_t>(deadline - last_time_, 120000));
	return {};
}
Error De25Computer::CheckDeadline()
{
	const auto now = clock_.NowMs();
	if (now < last_time_) return Fail("DE25 clock moved backwards; completion is uncertain");
	last_time_ = now;
	if (now >= deadline_ || std::chrono::steady_clock::now() >= wall_deadline_)
		return Fail("DE25 operation deadline expired; completion is uncertain");
	return {};
}
Error De25Computer::Read(std::uint32_t offset, std::uint32_t* value)
{
	Error e = CheckDeadline(); if (!e.ok()) return e;
	e = io_.Read32(offset, value);
	if (!e.ok()) return Fail("DE25 register read failed; completion is uncertain");
	return CheckDeadline();
}
Error De25Computer::Status(std::uint32_t* status)
{
	std::uint32_t fault = 0;
	Error e = Read(FesDe25ComputerStatusOffset, status); if (!e.ok()) return e;
	e = Read(FesDe25ComputerFaultOffset, &fault); if (!e.ok()) return e;
	if (fault || (*status & FesDe25ComputerStatusFault)) return Fail("DE25 bridge has a permanent transport fault");
	if ((*status & ~(FesDe25ComputerStatusReady | FesDe25ComputerStatusBusy | FesDe25ComputerStatusFault)) ||
		!(*status & FesDe25ComputerStatusReady)) return Fail("DE25 bridge is not ready or has unknown status bits");
	return {};
}
Error De25Computer::Gpi(std::uint32_t* value)
{
	Error e = Read(FesDe25ComputerGpiOffset, value); if (!e.ok()) return e;
	if ((*value & kResponseFixedMask) != FesComputerSignature)
		return Fail("DE25 mailbox response signature or reserved bits changed");
	return {};
}
Error De25Computer::ExchangeLocked(std::uint8_t opcode, std::uint8_t index,
	std::uint16_t argument, std::uint16_t* response, std::uint16_t* rejection)
{
	*response = 0; *rejection = 0;
	std::uint32_t status = 0, previous = 0, held = 0;
	Error e = Status(&status); if (!e.ok()) return e;
	if (status & FesDe25ComputerStatusBusy) return Fail("DE25 bridge already owns a pending request");
	e = Gpi(&previous); if (!e.ok()) return e;
	e = Read(FesDe25ComputerGpoOffset, &held); if (!e.ok()) return e;
	const bool old_ack = (previous & FesComputerAckMask) != 0;
	if (((held & FesComputerRequestMask) != 0) != old_ack)
		return Fail("DE25 idle request/acknowledgement state is inconsistent");
	const bool toggle = !old_ack;
	const std::uint32_t word = (std::uint32_t(opcode) << 24) | (std::uint32_t(index) << 16) |
		argument | (toggle ? FesComputerRequestMask : 0);
	const auto exchange_end = std::min(wall_deadline_, std::chrono::steady_clock::now() + std::chrono::milliseconds(100));
	e = CheckDeadline(); if (!e.ok()) return e;
	// Exactly one complete write. The Cyclone V old-toggle preload is invalid here.
	e = io_.Write32(FesDe25ComputerGpoOffset, word);
	if (!e.ok()) return Fail("DE25 register write failed; completion is uncertain");
	e = Status(&status); if (!e.ok()) return e;
	e = Read(FesDe25ComputerGpoOffset, &held); if (!e.ok()) return e;
	if (held != word) return Fail("DE25 bridge did not retain the submitted word");
	for (;;) {
		if (std::chrono::steady_clock::now() >= exchange_end)
			return Fail("DE25 acknowledgement deadline expired; completion is uncertain");
		e = Status(&status); if (!e.ok()) return e;
		std::uint32_t value = 0;
		e = Gpi(&value); if (!e.ok()) return e;
		if (std::chrono::steady_clock::now() >= exchange_end)
			return Fail("DE25 acknowledgement arrived after its deadline; completion is uncertain");
		if (((value & FesComputerAckMask) != 0) == toggle) {
			e = Status(&status); if (!e.ok()) return e;
			if (status & FesDe25ComputerStatusBusy) return Fail("DE25 acknowledged request remains busy");
			e = Read(FesDe25ComputerGpoOffset, &held); if (!e.ok()) return e;
			if (held != word) return Fail("DE25 held request changed before completion");
			e = Status(&status); if (!e.ok()) return e;
			if (std::chrono::steady_clock::now() >= exchange_end)
				return Fail("DE25 completion validation exceeded its deadline; completion is uncertain");
			*response = static_cast<std::uint16_t>(value & FesComputerResponseMask);
			if (value & FesComputerErrorMask) {
				if (*response < FesComputerErrorInvalidOpcode || *response > FesComputerErrorInvalidState)
					return Fail("DE25 mailbox returned an unknown rejection code");
				*rejection = *response;
			}
			return {};
		}
		std::this_thread::yield();
	}
}
Error De25Computer::Exchange(std::uint8_t opcode, std::uint8_t index, std::uint16_t argument,
	std::uint64_t deadline, std::uint16_t* response, std::uint16_t* rejection)
{
	std::lock_guard<std::mutex> lock(mutex_);
	if (!response || !rejection || response == rejection || opcode > 127) return Invalid("invalid DE25 exchange fields or output");
	Error e = RequireIdentity(); if (!e.ok()) return e;
	e = Begin(deadline); if (!e.ok()) return e;
	return ExchangeLocked(opcode, index, argument, response, rejection);
}
Error De25Computer::Mutate(std::uint8_t opcode, std::uint8_t index, std::uint16_t argument)
{
	std::uint16_t response = 0, rejection = 0;
	Error e = ExchangeLocked(opcode, index, argument, &response, &rejection); if (!e.ok()) return e;
	if (rejection) return Rejected(opcode, rejection);
	return response ? Fail("DE25 mutating command returned nonzero success data") : Error{};
}
Error De25Computer::MediaInfo(std::uint32_t* minimum, std::uint32_t* maximum,
	std::uint16_t* chunk, std::uint16_t* state)
{
	std::array<std::uint16_t, FesComputerMediaInfoStateField + 1> words{};
	for (unsigned field = 0; field < words.size(); ++field) {
		std::uint16_t rejected = 0;
		Error e = ExchangeLocked(FesComputerOpcodeMediaInfo,
			FesComputerZx81TapeUnit * FesComputerMediaInfoStride + field, 0, &words[field], &rejected);
		if (!e.ok()) return e;
		if (rejected) return Rejected(FesComputerOpcodeMediaInfo, rejected);
	}
	*minimum = words[0] | (std::uint32_t(words[1]) << 16);
	*maximum = words[2] | (std::uint32_t(words[3]) << 16); *chunk = words[4]; *state = words[5];
	if (*state != FesComputerMediaStateEmpty && *state != FesComputerMediaStateLoading && *state != FesComputerMediaStateReady)
		return Fail("DE25 tape unit returned an invalid state");
	return {};
}
Error De25Computer::Identify(const std::string& expected_build, std::uint64_t deadline, De25ComputerIdentity* identity)
{
	std::lock_guard<std::mutex> lock(mutex_);
	std::string expected;
	if (!identity || !BuildId(expected_build, &expected)) return Invalid("selected DE25 build must be a nonzero 32-digit hexadecimal identity");
	*identity = {}; identified_ = false;
	Error e = Begin(deadline); if (!e.ok()) return e;
	std::uint32_t id = 0, version = 0;
	e = Read(FesDe25ComputerIdOffset, &id); if (!e.ok()) return e;
	e = Read(FesDe25ComputerVersionOffset, &version); if (!e.ok()) return e;
	if (id != FesDe25ComputerIdentity || version != FesDe25ComputerVersion)
		return Fail("DE25 register identity/version differs from selected transport");
	std::array<std::uint16_t, FesComputerIdentityWordCount> words{};
	for (unsigned i = 0; i < words.size(); ++i) {
		std::uint16_t rejected = 0;
		e = ExchangeLocked(FesComputerOpcodeIdentity, i, 0, &words[i], &rejected);
		if (!e.ok()) return e;
		if (rejected) return Fail("DE25 identity probe was rejected");
	}
	const std::array<std::uint16_t, 8> prefix{{FesComputerIdentityMagic0, FesComputerIdentityMagic1,
		FesComputerTransportMajor, FesComputerTransportMinor, FesComputerABITag,
		FesComputerABIMajor, FesComputerABIMinor, kCapabilities}};
	if (!std::equal(prefix.begin(), prefix.end(), words.begin()) || BuildId(words) != expected)
		return Fail("DE25 mailbox identity/capabilities/build differ from selected artifact");
	std::uint32_t minimum = 0, maximum = 0; std::uint16_t chunk = 0, state = 0;
	e = MediaInfo(&minimum, &maximum, &chunk, &state); if (!e.ok()) return Fail("DE25 tape discovery failed: " + e.message);
	if (minimum != FesComputerZx81TapeMinBytes ||
		maximum != FesDe25ComputerZx81TapeMaxBytes || chunk != FesComputerMediaChunkMaxBytes)
		return Fail("DE25 tape limits differ from selected diagnostic contract");
	identified_ = true; identity->build_id = expected; identity->capabilities = kCapabilities;
	return {};
}
Error De25Computer::Hold(std::uint64_t deadline)
{
	std::lock_guard<std::mutex> lock(mutex_); Error e = RequireIdentity(); if (!e.ok()) return e;
	e = Begin(deadline); return e.ok() ? Mutate(FesComputerOpcodeExecution, FesComputerControlIndex, FesComputerExecutionHoldReset) : e;
}
Error De25Computer::Run(std::uint64_t deadline)
{
	std::lock_guard<std::mutex> lock(mutex_); Error e = RequireIdentity(); if (!e.ok()) return e;
	e = Begin(deadline); return e.ok() ? Mutate(FesComputerOpcodeExecution, FesComputerControlIndex, FesComputerExecutionRelease) : e;
}
Error De25Computer::SetKeyboardHid(const KeyboardHidRows& rows, std::uint64_t deadline)
{
	std::lock_guard<std::mutex> lock(mutex_);
	if ((rows[0] & FesComputerKeyboardReservedRow0Mask) || (rows[FesComputerKeyboardModifierRow] & ~FesComputerKeyboardModifierMask))
		return Invalid("DE25 keyboard rows include reserved usages/modifier bits");
	Error e = RequireIdentity(); if (!e.ok()) return e;
	e = Begin(deadline); if (!e.ok()) return e;
	for (unsigned row = 0; row < rows.size(); ++row) {
		e = Mutate(FesComputerOpcodeKeyboardHid, row, rows[row]); if (!e.ok()) return e;
	}
	// Match the diagnostic keyboard mapper's nominal one-millisecond settling
	// interval before a caller can immediately replace this complete snapshot.
	std::this_thread::sleep_until(std::min(wall_deadline_,
		std::chrono::steady_clock::now() + std::chrono::milliseconds(2)));
	// Every row is already acknowledged. Settling is only a pacing delay; an
	// expired command deadline here cannot make the completed snapshot uncertain.
	return {};
}
Error De25Computer::LoadTape(const std::vector<std::uint8_t>& bytes, std::uint64_t deadline, std::uint16_t chunk_bytes)
{
	std::lock_guard<std::mutex> lock(mutex_);
	if (bytes.size() < FesComputerZx81TapeMinBytes ||
		bytes.size() > FesDe25ComputerZx81TapeMaxBytes ||
		!chunk_bytes || chunk_bytes > FesComputerMediaChunkMaxBytes) return Invalid("invalid DE25 tape/chunk length");
	Error e = RequireIdentity(); if (!e.ok()) return e;
	e = Begin(deadline); if (!e.ok()) return e;
	std::uint32_t crc = FesComputerMediaCRC32Initial;
	for (const auto byte : bytes) {
		crc ^= byte;
		for (unsigned bit = 0; bit < 8; ++bit) crc = (crc >> 1) ^ ((crc & 1) ? FesComputerMediaCRC32Polynomial : 0);
	}
	crc ^= FesComputerMediaCRC32FinalXor;
	const std::uint32_t size = bytes.size();
	const std::uint16_t header[] = {std::uint16_t(size), std::uint16_t(size >> 16), std::uint16_t(crc), std::uint16_t(crc >> 16)};
	const auto base = FesComputerZx81TapeUnit * FesComputerMediaHeaderStride;
	for (unsigned word = 0; word < 4; ++word) {
		e = Mutate(FesComputerOpcodeMediaBegin, base + word, header[word]); if (!e.ok()) return e;
	}
	for (std::size_t offset = 0; offset < bytes.size(); offset += chunk_bytes) {
		const auto count = std::min<std::size_t>(chunk_bytes, bytes.size() - offset);
		const std::uint16_t chunk[] = {std::uint16_t(offset), std::uint16_t(offset >> 16), std::uint16_t(count)};
		for (unsigned word = 0; word < 3; ++word) {
			e = Mutate(FesComputerOpcodeMediaChunk, base + word, chunk[word]); if (!e.ok()) return e;
		}
		for (std::size_t start = 0; start < count; start += 2) {
			const auto value = std::uint16_t(bytes[offset + start]) |
				(start + 1 < count ? std::uint16_t(bytes[offset + start + 1]) << 8 : 0);
			e = Mutate(FesComputerOpcodeMediaData, start / 2, value); if (!e.ok()) return e;
		}
	}
	e = Mutate(FesComputerOpcodeMediaCommit, FesComputerZx81TapeUnit, 0); if (!e.ok()) return e;
	std::uint32_t minimum = 0, maximum = 0; std::uint16_t chunk = 0, state = 0;
	e = MediaInfo(&minimum, &maximum, &chunk, &state); if (!e.ok()) return e;
	return state == FesComputerMediaStateReady ? Error{} : Fail("DE25 acknowledged tape commit did not become ready");
}
Error De25Computer::EjectTape(std::uint64_t deadline)
{
	std::lock_guard<std::mutex> lock(mutex_); Error e = RequireIdentity(); if (!e.ok()) return e;
	e = Begin(deadline); if (!e.ok()) return e;
	e = Mutate(FesComputerOpcodeMediaEject, FesComputerZx81TapeUnit, 0); if (!e.ok()) return e;
	std::uint32_t minimum = 0, maximum = 0; std::uint16_t chunk = 0, state = 0;
	e = MediaInfo(&minimum, &maximum, &chunk, &state); if (!e.ok()) return e;
	return state == FesComputerMediaStateEmpty ? Error{} : Fail("DE25 acknowledged tape eject did not become empty");
}
} // namespace native
} // namespace mister
