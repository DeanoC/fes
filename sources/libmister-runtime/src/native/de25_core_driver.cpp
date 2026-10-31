// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/de25_core_driver.hpp"
#include "native/artifacts.hpp"
#include "native/generated/fes_computer.hpp"
#include "native/generated/fes_de25_computer.hpp"
namespace mister { namespace native {
using namespace generated;
void De25ComputerCoreDriver::BeginSession()
{
	selected_ = false; core_.clear(); build_.clear(); media_.clear();
	// A lifecycle boundary never clears the protocol client's sticky fault.
}
Error De25ComputerCoreDriver::Ready() const
{
	if (!selected_ || client_.Poisoned())
		return {ErrorCode::io_failed, "DE25 driver requires a healthy selected identity", "de25-control"};
	return {};
}
Error De25ComputerCoreDriver::Context(const CoreDriverContext& context) const
{
	Error error = Ready(); if (!error.ok()) return error;
	if (!context.descriptor || context.descriptor->core.id != core_ ||
		context.expected_core != core_ || context.descriptor->build.id != build_)
		return {ErrorCode::core_mismatch, "DE25 driver context changed", "identity"};
	return {};
}
CoreDriverResult De25ComputerCoreDriver::Identify(const CoreDriverContext& context,
	std::uint64_t deadline)
{
	BeginSession();
	const auto* descriptor = context.descriptor;
	if (!descriptor || descriptor->core.id.empty() ||
		context.expected_core != descriptor->core.id ||
		descriptor->abi.id != FesComputerABIID || descriptor->abi.major != 1 ||
		descriptor->abi.minor != 0 || descriptor->interfaces.size() != 3)
		return {{ErrorCode::unsupported_abi, "DE25 driver requires the exact BRAM computer contract", "identity"}, false, ""};
	unsigned seen = 0;
	for (const auto& interface : descriptor->interfaces) {
		unsigned bit = interface.id == FesComputerInterfaceVideoFixed720p60ID ? 1 :
			interface.id == FesComputerInterfaceKeyboardHidID ? 2 :
			interface.id == FesComputerInterfaceMediaZx81TapeID ? 4 : 0;
		if (!bit || (seen & bit) || !interface.required || interface.major != 1 || interface.minor != 0)
			return {{ErrorCode::unsupported_interface, "DE25 descriptor declares an unsupported interface", "identity"}, false, ""};
		seen |= bit;
	}
	De25ComputerIdentity identity; De25TapeInfo tape;
	Error error = client_.Identify(descriptor->build.id, deadline, &identity, &tape);
	if (!error.ok()) return {error, false, ""};
	core_ = descriptor->core.id; build_ = descriptor->build.id; selected_ = true;
	MediaUnitCapability unit; unit.unit = FesComputerZx81TapeUnit;
	unit.interface = {FesComputerInterfaceMediaZx81TapeID, 1, 0};
	unit.min_bytes = tape.minimum; unit.max_bytes = tape.maximum;
	unit.chunk_bytes = tape.chunk_bytes; unit.saveable = false;
	unit.state = tape.state == FesComputerMediaStateReady ? MediaUnitState::ready :
		tape.state == FesComputerMediaStateLoading ? MediaUnitState::loading : MediaUnitState::empty;
	media_.push_back(unit);
	CoreDriverResult result;
	result.observed_core = core_; result.safe_to_quiesce = true;
	return result;
}
CoreDriverResult De25ComputerCoreDriver::Quiesce(const CoreDriverContext& context,
	std::uint64_t deadline)
{
	Error error = Context(context); if (!error.ok()) return {error, false, ""};
	return {client_.Hold(deadline), true, core_};
}
CoreDriverResult De25ComputerCoreDriver::NeutralizeButtons(const CoreDriverContext& context,
	std::uint64_t deadline)
{
	Error error = Context(context); if (!error.ok()) return {error, false, ""};
	return {client_.SetKeyboardHid({}, deadline), true, core_};
}
CoreDriverResult De25ComputerCoreDriver::SetButtons(const CoreDriverContext& context,
	std::uint16_t buttons, std::uint64_t deadline)
{
	if (buttons) return {{ErrorCode::unsupported_interface, "DE25 ZX81 has no gamepad interface", "input"}, false, ""};
	return NeutralizeButtons(context, deadline);
}
CoreDriverResult De25ComputerCoreDriver::Start(const CoreDriverContext& context,
	std::uint64_t deadline)
{
	Error error = Context(context); if (!error.ok()) return {error, false, ""};
	error = client_.Run(deadline);
	CoreDriverResult result{error, true, core_};
	result.execution_may_have_run = error.ok() || client_.Poisoned();
	return result;
}
Error De25ComputerCoreDriver::SetKeyboardHid(const KeyboardHidRows& rows,
	std::uint64_t deadline)
{
	Error error = Ready(); return error.ok() ? client_.SetKeyboardHid(rows, deadline) : error;
}
Error De25ComputerCoreDriver::LoadMedia(const std::vector<std::uint8_t>& bytes,
	std::uint64_t deadline)
{
	Error error = Ready(); if (!error.ok()) return error;
	if (bytes.empty() || bytes.size() > FesDe25ComputerZx81TapeMaxBytes)
		return {ErrorCode::invalid_request, "DE25 tape is outside the selected media limits", "request"};
	// Launch binding stays held; Start is the only release operation.
	error = client_.Hold(deadline);
	return error.ok() ? RecordTransfer(client_.LoadTape(bytes, deadline), MediaUnitState::ready) : error;
}
Error De25ComputerCoreDriver::LoadMediaLive(const std::vector<std::uint8_t>& bytes,
	std::uint64_t deadline)
{
	Error error = Ready(); return error.ok() ? RecordTransfer(client_.LoadTape(bytes, deadline), MediaUnitState::ready) : error;
}
Error De25ComputerCoreDriver::ClearMedia(std::uint64_t deadline)
{
	Error error = Ready(); return error.ok() ? RecordTransfer(client_.EjectTape(deadline), MediaUnitState::empty) : error;
}
Error De25ComputerCoreDriver::RecordTransfer(Error error, MediaUnitState success)
{
	if (!media_.empty()) {
		if (error.ok()) media_[0].state = success;
		else if (client_.Poisoned()) media_[0].state = MediaUnitState::loading;
	}
	return error;
}
Error De25ComputerCoreDriver::InsertMedia(std::uint8_t unit,
	const ComputerMediaSnapshot& snapshot, Clock& clock, std::uint64_t deadline,
	std::uint32_t cleanup_ms)
{
	(void)cleanup_ms; // An uncertain DE25 command must never trigger a cleanup command.
	Error error = Ready(); if (!error.ok()) return error;
	if (unit != FesComputerZx81TapeUnit)
		return {ErrorCode::unsupported_interface, "DE25 tape unit is inactive", "request"};
	if (snapshot.size() < media_[0].min_bytes || snapshot.size() > media_[0].max_bytes)
		return {ErrorCode::invalid_request, "DE25 snapshot exceeds live tape limits", "request"};
	std::vector<std::uint8_t> bytes(snapshot.size());
	error = snapshot.Read(0, bytes.data(), bytes.size(), clock, deadline);
	if (!error.ok()) return error;
	return LoadMediaLive(bytes, deadline);
}
Error De25ComputerCoreDriver::EjectMedia(std::uint8_t unit, std::uint64_t deadline)
{
	if (unit != FesComputerZx81TapeUnit)
		return {ErrorCode::unsupported_interface, "DE25 tape unit is inactive", "request"};
	return ClearMedia(deadline);
}
} } // namespace mister::native
