// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
#include "native/generated/fes_computer.hpp"
#include "native/generated/fes_application.hpp"
#include "native/generated/de25_nano_programming.hpp"
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
			interface.minor != (bit == 4 ? 1u : 0u))
			return {ErrorCode::unsupported_interface, "DE25 MENU requires fixed video, menu display and scanout1.1", "compatibility"};
		seen |= bit;
	}
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
