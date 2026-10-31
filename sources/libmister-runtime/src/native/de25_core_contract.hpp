// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
#include "native/generated/fes_computer.hpp"
namespace mister { namespace native {
// The selected BRAM driver implements this exact contract. This is shared by
// package preflight and live identity admission, not a programming profile table.
inline Error CheckDe25ComputerContract(const CoreDescriptor& descriptor)
{
	using namespace generated;
	if (descriptor.format != 2 || descriptor.core.id.empty() || !descriptor.core.system.empty() ||
		descriptor.abi.id != FesComputerABIID || descriptor.abi.major != 1 ||
		descriptor.abi.minor != 0 || descriptor.interfaces.size() != 3)
		return {ErrorCode::unsupported_abi, "DE25 driver requires the exact BRAM computer contract", "compatibility"};
	bool nonzero = false;
	if (descriptor.build.id.size() != 32)
		return {ErrorCode::core_mismatch, "DE25 requires a selected 128-bit build identity", "compatibility"};
	for (char c : descriptor.build.id) {
		if (!((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')))
			return {ErrorCode::core_mismatch, "DE25 build identity is not hexadecimal", "compatibility"};
		nonzero = nonzero || c != '0';
	}
	if (!nonzero) return {ErrorCode::core_mismatch, "DE25 build identity must be nonzero", "compatibility"};
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
