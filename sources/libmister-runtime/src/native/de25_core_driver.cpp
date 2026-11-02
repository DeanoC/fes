// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/de25_core_driver.hpp"
#include "native/de25_core_contract.hpp"
#include "native/artifacts.hpp"
#include "native/generated/fes_computer.hpp"
#include "native/generated/fes_de25_computer.hpp"
#include "native/linux/de25_menu.hpp"
namespace mister { namespace native {
using namespace generated;
VideoResult De25VideoBringup::BringUpCustom(std::uint64_t, bool)
{
 VideoResult result;
 result.error = {ErrorCode::unsupported_interface, "DE25 video needs the selected core driver", "video"};
 return result;
}
VideoResult De25VideoBringup::BringUp(const IdleRecipe&, std::uint64_t deadline)
{
 return BringUpCustom(deadline);
}
VideoResult De25VideoBringup::BringUpSelected(CoreDriver& driver, std::uint64_t deadline, bool audio)
{
 VideoResult result;
 result.phase = "fpga-video";
 if (audio) result.error = {ErrorCode::unsupported_interface, "selected DE25 profiles have no audio", "video"};
 else result.error = driver.WaitVideoReady(deadline);
 return result;
}
void De25ComputerCoreDriver::BeginSession()
{
	selected_ = false; core_.clear(); build_.clear(); media_.clear();
	// A lifecycle boundary never clears the protocol client's sticky fault.
}
Error De25ComputerCoreDriver::WaitVideoReady(std::uint64_t deadline)
{
	Error error = Ready();
	return error.ok() ? client_.WaitVideoReady(deadline) : error;
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
	if (!descriptor || context.expected_core != descriptor->core.id)
		return {{ErrorCode::core_mismatch, "DE25 selected core context differs", "identity"}, false, ""};
	Error contract = CheckDe25ComputerContract(*descriptor);
	if (!contract.ok()) { contract.phase = "identity"; return {contract, false, ""}; }
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
	error = client_.WaitVideoReady(deadline);
	if (!error.ok()) return {error, false, core_};
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

void De25MenuCoreDriver::BeginSession()
{
	selected_ = false; build_.clear();
	// The shared presenter retains any uncertain grant until guarded retirement.
}
Error De25MenuCoreDriver::Context(const CoreDriverContext& context) const
{
	if (!selected_ || !context.descriptor || context.expected_core != "fes.menu" ||
		context.descriptor->build.id != build_)
		return {ErrorCode::core_mismatch, "DE25 MENU selected context differs", "identity"};
	return CheckDe25MenuContract(*context.descriptor);
}
CoreDriverResult De25MenuCoreDriver::Identify(const CoreDriverContext& context, std::uint64_t deadline)
{
	BeginSession();
	if (!context.descriptor || context.expected_core != "fes.menu")
		return {{ErrorCode::core_mismatch, "DE25 MENU selected context differs", "identity"}, false, ""};
	Error error = CheckDe25MenuContract(*context.descriptor);
	std::array<std::uint32_t,4> words{};
	if (error.ok()) error = De25BuildWords(context.descriptor->build.id, &words);
	if (!error.ok()) return {error, false, ""};
	error = menu_.Bind(words, deadline);
	CoreDriverResult result{error, true, error.ok() ? "fes.menu" : ""};
	if (error.ok()) {
		selected_ = true; build_ = context.descriptor->build.id;
		result.safe_to_quiesce = true;
	}
	return result;
}
Error De25MenuCoreDriver::WaitVideoReady(std::uint64_t deadline)
{
	if (!selected_)
		return {ErrorCode::core_mismatch, "DE25 MENU has no selected identity", "video"};
	MenuDisplayInfo info;
	Error error = menu_.ReadInfo(deadline, &info);
	if (error.ok() && (!info.enabled || !info.configured || info.quiesced || info.pending || info.faulted || info.underflows))
		error = {ErrorCode::io_failed, "DE25 MENU has no completed ready frame", "video"};
	return error;
}
CoreDriverResult De25MenuCoreDriver::Quiesce(const CoreDriverContext& context, std::uint64_t deadline)
{
	Error error = Context(context);
	return {error.ok() ? menu_.Quiesce(deadline) : error, true, "fes.menu"};
}
CoreDriverResult De25MenuCoreDriver::NeutralizeButtons(const CoreDriverContext& context, std::uint64_t)
{
	return {Context(context), false, "fes.menu"};
}
CoreDriverResult De25MenuCoreDriver::SetButtons(const CoreDriverContext& context, std::uint16_t buttons,
	std::uint64_t deadline)
{
	if (buttons) return {{ErrorCode::unsupported_interface, "DE25 MENU has no gamepad interface", "input"}, false, ""};
	return NeutralizeButtons(context, deadline);
}
CoreDriverResult De25MenuCoreDriver::Start(const CoreDriverContext& context, std::uint64_t deadline)
{
	Error error = Context(context);
	return {error.ok() ? WaitVideoReady(deadline) : error, false, "fes.menu"};
}
} } // namespace mister::native
