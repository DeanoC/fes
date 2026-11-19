// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
#include "native/generated/fes_computer.hpp"
#include "native/generated/fes_application.hpp"
#include "native/generated/de25_nano_programming.hpp"
#include "native/features.hpp"
#include <array>
namespace mister { namespace native {
// Register word zero is the least significant word of the selected BUILD_ID.
inline Error De25BuildWords(const std::string& text, std::array<std::uint32_t,4>* words)
{
	if (!words || text.size() != 32)
		return {ErrorCode::core_mismatch, "DE25 requires a selected 128-bit build identity", "compatibility"};
	std::array<std::uint32_t,4> value{};
	bool nonzero = false;
	for (std::size_t i = 0; i < text.size(); ++i) {
		const char c = text[i];
		const unsigned digit = c >= '0' && c <= '9' ? c - '0' :
			c >= 'a' && c <= 'f' ? c - 'a' + 10 : c >= 'A' && c <= 'F' ? c - 'A' + 10 : 16;
		if (digit == 16)
			return {ErrorCode::core_mismatch, "DE25 build identity is not hexadecimal", "compatibility"};
		value[3 - i / 8] = (value[3 - i / 8] << 4) | digit;
		nonzero = nonzero || digit;
	}
	if (!nonzero)
		return {ErrorCode::core_mismatch, "DE25 build identity must be nonzero", "compatibility"};
	*words = value;
	return {};
}
inline Error CheckDe25MenuContract(const CoreDescriptor& descriptor)
{
	using namespace generated;
	if (descriptor.target.platform != kDe25NanoProgrammingPlatform ||
		descriptor.target.device != kDe25NanoProgrammingDevice ||
		descriptor.target.programming_profile != "de25-linux-menu-v1")
		return {ErrorCode::unsupported_target, "DE25 MENU target/profile differs", "compatibility"};
	if (descriptor.format != 2 || descriptor.core.id != "fes.menu" || !descriptor.core.system.empty() ||
		descriptor.abi.id != FesApplicationABIID || descriptor.abi.major != 1 ||
		descriptor.abi.minor != 0 || descriptor.interfaces.size() != 3)
		return {ErrorCode::unsupported_abi, "DE25 MENU requires the allocated scanout application contract", "compatibility"};
	std::array<std::uint32_t,4> words{};
	Error error = De25BuildWords(descriptor.build.id, &words);
	if (!error.ok()) return error;
	unsigned seen = 0;
	for (const auto& interface : descriptor.interfaces) {
		const unsigned bit = interface.id == FesApplicationInterfaceVideoFixed720p60ID ? 1 :
			interface.id == FesApplicationInterfaceVideoMenuDisplayID ? 2 :
			interface.id == "fes.fabric.de25-scanout" ? 4 : 0;
		if (!bit || (seen & bit) || !interface.required || interface.major != 1 ||
			interface.minor != (bit == 4 ? 3u : 0u))
			return {ErrorCode::unsupported_interface, "DE25 MENU requires fixed video, menu display and scanout1.3", "compatibility"};
		seen |= bit;
	}
	return {};
}
// fes.fabric.firmware-upload 1.0 is a manifest-only required interface: it
// grants no capability bit, and its opcodes are sent only to a package that
// declares it.
constexpr const char* kFirmwareUploadInterfaceID = "fes.fabric.firmware-upload";
struct De25StandardInterface {
	const char* id;
	std::uint16_t major, minor;
};
// One machine the standard shell runs. Every listed interface is required at
// exactly this version, beside the shell's own. `firmware_bytes` are the
// image sizes its upload endpoint takes; a machine without one is format 2.
struct De25StandardCore {
	const char* id;
	std::array<De25StandardInterface, 5> interfaces;
	std::array<std::uint32_t, 2> firmware_bytes;
	// The static profile adds the optional expansion bus to this machine.
	const char* static_bus;
};
// The shell every standard machine shares: its video, input and audio, and
// the session plane on the allocated scanout.
constexpr std::array<De25StandardInterface, 7> kDe25StandardShell = {{
	{generated::FesComputerInterfaceVideoFixed720p60ID, 1, 0},
	{generated::FesComputerInterfaceKeyboardHidID, 1, 0},
	{generated::FesComputerInterfaceGamepadPortsID, 1, 0},
	{generated::FesComputerInterfaceAudioPcmS16Stereo48kID, 1, 0},
	{generated::FesComputerInterfaceVideoSessionDisplayID, 1, 0},
	{"fes.fabric.video.display-plane", 1, 0},
	{"fes.fabric.de25-scanout", 1, 3},
}};
constexpr std::array<De25StandardCore, 4> kDe25StandardCores = {{
	{"fes.zx81",
		{{{generated::FesComputerInterfaceMediaZx81TapeID, 1, 0},
		  {generated::FesComputerInterfaceMediaZx81TapeWriteID, 1, 0}}},
		{{0, 0}}, "fes.expansion.zx81-bus"},
	{"fes.spectrum",
		{{{generated::FesComputerInterfaceMediaSpectrumTapeID,
			generated::FesComputerInterfaceMediaSpectrumTapeMajor,
			generated::FesComputerInterfaceMediaSpectrumTapeMinor},
		  {kFirmwareUploadInterfaceID, 1, 0}}},
		{{16384, 32768}}, nullptr},
	{"fes.spectrum128",
		{{{generated::FesComputerInterfaceMediaSpectrumTapeID,
			generated::FesComputerInterfaceMediaSpectrumTapeMajor,
			generated::FesComputerInterfaceMediaSpectrumTapeMinor},
		  {kFirmwareUploadInterfaceID, 1, 0}}},
		{{32768, 0}}, nullptr},
	{"fes.atari-st",
		{{{generated::FesComputerInterfaceMouseRelativeID, 1, 0},
		  {generated::FesComputerInterfaceMediaAtariStFloppyID, 1, 0},
		  {generated::FesComputerInterfaceMediaAtariStFloppyWriteID, 1, 0},
		  {generated::FesComputerInterfaceMediaAtariStFloppyGeometryID, 1, 0},
		  {kFirmwareUploadInterfaceID, 1, 0}}},
		{{196608, 0}}, nullptr},
}};
inline const De25StandardCore* FindDe25StandardCore(const std::string& id)
{
	for (const auto& core : kDe25StandardCores)
		if (id == core.id) return &core;
	return nullptr;
}
// Whether a package's firmware arrives by launch-time upload.
inline bool DeclaresFirmwareUpload(const CoreDescriptor& descriptor)
{
	for (const auto& interface : descriptor.interfaces)
		if (interface.id == kFirmwareUploadInterfaceID) return true;
	return false;
}
// A standard machine uses the shared computer ABI and a kernel-owned session
// allocation. Its profile never grants the Cyclone V reserved DDR window. A
// machine with firmware is format 3: the payload is the unmodified bitstream
// and the sealed ROM requirement names the image its upload endpoint takes.
inline Error CheckDe25StandardComputerContract(const CoreDescriptor& descriptor)
{
	using namespace generated;
	const bool static_profile = descriptor.target.programming_profile == "de25-linux-static-computer-v1";
	if (descriptor.target.platform != kDe25NanoProgrammingPlatform ||
		descriptor.target.device != kDe25NanoProgrammingDevice ||
		(descriptor.target.programming_profile != "de25-linux-computer-v1" &&
		 !(static_profile && De25StaticEnabled())))
		return {ErrorCode::unsupported_target, "DE25 standard computer target/profile differs", "compatibility"};
	const De25StandardCore* const core = FindDe25StandardCore(descriptor.core.id);
	const bool firmware = core && core->firmware_bytes[0] != 0;
	if (!core || (static_profile && !core->static_bus) ||
		descriptor.format != (static_profile ? 5u : firmware ? 3u : 2u) ||
		!descriptor.core.system.empty() || descriptor.abi.id != FesComputerABIID ||
		descriptor.abi.major != 1 || descriptor.abi.minor != 0)
		return {ErrorCode::unsupported_abi, "DE25 standard profile requires a registered computer contract", "compatibility"};
	if (firmware && (descriptor.rom.role != "firmware" ||
		(descriptor.rom.source_size != core->firmware_bytes[0] &&
		 (core->firmware_bytes[1] == 0 || descriptor.rom.source_size != core->firmware_bytes[1]))))
		return {ErrorCode::unsupported_abi, "DE25 machine firmware differs from its upload endpoint", "compatibility"};
	std::array<std::uint32_t,4> words{};
	Error error = De25BuildWords(descriptor.build.id, &words);
	if (!error.ok()) return error;
	std::size_t expected = kDe25StandardShell.size();
	for (const auto& interface : core->interfaces) if (interface.id) ++expected;
	if (static_profile) ++expected;
	const auto differ = [] {
		return Error{ErrorCode::unsupported_interface, "DE25 standard computer interfaces differ", "compatibility"};
	};
	if (descriptor.interfaces.size() != expected) return differ();
	// An exact count of registered declarations, none repeated, is the exact set.
	for (const auto& interface : descriptor.interfaces) {
		if (static_profile && interface.id == core->static_bus) {
			if (interface.required || interface.major != 2 || interface.minor != 0) return differ();
			continue;
		}
		const De25StandardInterface* registered = nullptr;
		for (const auto& candidate : kDe25StandardShell)
			if (interface.id == candidate.id) registered = &candidate;
		for (const auto& candidate : core->interfaces)
			if (candidate.id && interface.id == candidate.id) registered = &candidate;
		if (!registered || !interface.required || interface.major != registered->major ||
			interface.minor != registered->minor) return differ();
	}
	for (std::size_t i = 0; i < descriptor.interfaces.size(); ++i)
		for (std::size_t j = 0; j < i; ++j)
			if (descriptor.interfaces[i].id == descriptor.interfaces[j].id) return differ();
	return {};
}
// The selected BRAM driver implements this exact contract. This is shared by
// package preflight and live identity admission, not a programming profile table.
inline Error CheckDe25ComputerContract(const CoreDescriptor& descriptor)
{
	using namespace generated;
	if (descriptor.format != 2 || descriptor.core.id.empty() || !descriptor.core.system.empty() ||
		descriptor.abi.id != FesComputerABIID || descriptor.abi.major != 1 ||
		descriptor.abi.minor != 0 || descriptor.interfaces.size() != 3)
		return {ErrorCode::unsupported_abi, "DE25 driver requires the exact BRAM computer contract", "compatibility"};
	std::array<std::uint32_t,4> words{};
	Error error = De25BuildWords(descriptor.build.id, &words);
	if (!error.ok()) return error;
	unsigned seen = 0;
	for (const auto& interface : descriptor.interfaces) {
		const unsigned bit = interface.id == FesComputerInterfaceVideoFixed720p60ID ? 1 :
			interface.id == FesComputerInterfaceKeyboardHidID ? 2 :
			interface.id == FesComputerInterfaceMediaZx81TapeID ? 4 : 0;
		if (!bit || (seen & bit) || !interface.required || interface.major != 1 || interface.minor != 0)
			return {ErrorCode::unsupported_interface, "DE25 descriptor declares an unsupported interface", "compatibility"};
		seen |= bit;
	}
	return {};
}
} }
