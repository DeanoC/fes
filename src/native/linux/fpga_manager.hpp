// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "native/hardware.hpp"
#include "native/linux/mmio.hpp"

#include <cstdint>

namespace mister {
namespace native {

constexpr std::uint32_t kFpgaStatusAddress = 0xff706000u;
constexpr std::uint32_t kFpgaControlAddress = 0xff706004u;
constexpr std::uint32_t kFpgaDclkCountAddress = 0xff706008u;
constexpr std::uint32_t kFpgaDclkStatusAddress = 0xff70600cu;
constexpr std::uint32_t kFpgaGpoAddress = 0xff706010u;
constexpr std::uint32_t kFpgaMonitorEoiAddress = 0xff70684cu;
constexpr std::uint32_t kFpgaMonitorAddress = 0xff706850u;
constexpr std::uint32_t kFpgaDataAddress = 0xffb90000u;
constexpr std::uint32_t kSystemManagerFpgaInterfaceAddress = 0xffd08028u;
constexpr std::uint32_t kSdrFpgaPortResetAddress = 0xffc25080u;
constexpr std::uint32_t kBridgeResetAddress = 0xffd0501cu;
constexpr std::uint32_t kL3RemapAddress = 0xff800000u;

constexpr std::uint32_t kFpgaModeMask = 0x7u;
constexpr std::uint32_t kFpgaMselMask = 0xf8u;
constexpr std::uint32_t kFpgaMselShift = 3u;
constexpr std::uint32_t kFpgaModeReset = 0x1u;
constexpr std::uint32_t kFpgaModeConfiguration = 0x2u;
constexpr std::uint32_t kFpgaModeInitialization = 0x3u;
constexpr std::uint32_t kFpgaModeUser = 0x4u;

constexpr std::uint32_t kFpgaControlConfigurationWidthMask = 0x200u;
constexpr std::uint32_t kFpgaControlAxiConfigurationEnableMask = 0x100u;
constexpr std::uint32_t kFpgaControlClockDataRatioMask = 0xc0u;
constexpr std::uint32_t kFpgaControlClockDataRatioShift = 6u;
constexpr std::uint32_t kFpgaControlNconfigPullMask = 0x4u;
constexpr std::uint32_t kFpgaControlNceMask = 0x2u;
constexpr std::uint32_t kFpgaControlEnableMask = 0x1u;

constexpr std::uint32_t kFpgaMonitorNstatusMask = 0x1u;
constexpr std::uint32_t kFpgaMonitorConfDoneMask = 0x2u;
constexpr std::uint32_t kFpgaMonitorInitDoneMask = 0x4u;
constexpr std::uint32_t kFpgaMonitorClearAll = 0xfffu;

constexpr std::uint32_t kFpgaCoreStateMask = 0xc0000000u;
constexpr std::uint32_t kFpgaCoreReset = 0x40000000u;
constexpr std::uint32_t kFpgaCoreNormal = 0x80000000u;
constexpr std::uint32_t kSdrFpgaPortsDisabled = 0u;
constexpr std::uint32_t kSdrFpgaPortsEnabled = 0x3fffu;
constexpr std::uint32_t kBridgesInReset = 7u;
constexpr std::uint32_t kBridgesReleased = 0u;
constexpr std::uint32_t kL3RemapContained = 1u;
constexpr std::uint32_t kL3RemapFpgaEnabled = 0x19u;

class LinuxFpgaManager final : public FpgaManager {
public:
	LinuxFpgaManager(Mmio&, Clock&);
	NativeResult Program(const Artifact&,
		std::uint64_t absolute_deadline_ms) override;

private:
	Mmio& mmio_;
	Clock& clock_;
};

} // namespace native
} // namespace mister
