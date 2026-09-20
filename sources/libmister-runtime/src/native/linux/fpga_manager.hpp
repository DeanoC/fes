// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "native/generated/de10_nano.hpp"
#include "native/hardware.hpp"
#include "native/linux/mmio.hpp"

#include <cstdint>

namespace mister {
namespace native {

using generated::kFpgaStatusAddress;
using generated::kFpgaControlAddress;
using generated::kFpgaDclkCountAddress;
using generated::kFpgaDclkStatusAddress;
using generated::kFpgaGpoAddress;
using generated::kFpgaMonitorEoiAddress;
using generated::kFpgaMonitorAddress;
using generated::kFpgaDataAddress;
using generated::kSystemManagerFpgaInterfaceAddress;
using generated::kSdrFpgaPortResetAddress;
using generated::kBridgeResetAddress;
using generated::kL3RemapAddress;

using generated::kFpgaModeMask;
using generated::kFpgaMselMask;
using generated::kFpgaMselShift;
using generated::kFpgaModeReset;
using generated::kFpgaModeConfiguration;
using generated::kFpgaModeInitialization;
using generated::kFpgaModeUser;

using generated::kFpgaControlConfigurationWidthMask;
using generated::kFpgaControlAxiConfigurationEnableMask;
using generated::kFpgaControlClockDataRatioMask;
using generated::kFpgaControlClockDataRatioShift;
using generated::kFpgaControlNconfigPullMask;
using generated::kFpgaControlNceMask;
using generated::kFpgaControlEnableMask;

using generated::kFpgaMonitorNstatusMask;
using generated::kFpgaMonitorConfDoneMask;
using generated::kFpgaMonitorInitDoneMask;
using generated::kFpgaMonitorClearAll;

using generated::kFpgaCoreStateMask;
using generated::kFpgaCoreReset;
using generated::kFpgaCoreNormal;
using generated::kSdrFpgaPortsDisabled;
using generated::kSdrFpgaPortsEnabled;
using generated::kBridgesInReset;
using generated::kBridgesReleased;
using generated::kL3RemapContained;
using generated::kL3RemapFpgaEnabled;

class LinuxFpgaManager final : public FpgaManager {
public:
	LinuxFpgaManager(Mmio&, Clock&);
	NativeResult Program(const Artifact&,
		ProgrammingProfile,
		std::uint64_t absolute_deadline_ms) override;

private:
	Mmio& mmio_;
	Clock& clock_;
};

} // namespace native
} // namespace mister
