// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "native/generated/de10_nano.hpp"
#include "native/hardware.hpp"
#include "native/linux/mmio.hpp"

#include <cstdint>
#include <string>

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

using generated::kSdrCportWidthAddress;
using generated::kSdrCportWmapAddress;
using generated::kSdrCportRmapAddress;
using generated::kSdrRfifoCmapAddress;
using generated::kSdrWfifoCmapAddress;
using generated::kSdrCportRdwrAddress;
using generated::kSdrPortCfgAddress;
using generated::kSdrCportWidthFpgaMask;
using generated::kSdrCportWmapFpgaMask;
using generated::kSdrCportRmapFpgaMask;
using generated::kSdrRfifoCmapFpgaMask;
using generated::kSdrWfifoCmapFpgaMask;
using generated::kSdrCportRdwrFpgaMask;
using generated::kSdrPortCfgFpgaMask;

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

// Per-boot record of the layout the SDR controller latched (/run is tmpfs).
constexpr char kBootHpsDdrRecordPath[] = "/run/mister-runtime-boot-hps-ddr";

class LinuxFpgaManager final : public FpgaManager {
public:
	LinuxFpgaManager(Mmio&, Clock&,
		std::string boot_record_path = kBootHpsDdrRecordPath);
	NativeResult Program(const Artifact&,
		ProgrammingProfile,
		std::uint64_t absolute_deadline_ms) override;
	Error ReleaseHpsDdrPorts(std::uint64_t absolute_deadline_ms) override;
	// Read once per boot, before this boot's first program: the FPGA then
	// still holds the core U-Boot latched the layout from. The verdict is
	// kept in the boot record, so a restarted runtime reuses it.
	bool BootHpsDdrLayout() override;

private:
	Mmio& mmio_;
	Clock& clock_;
	std::string boot_record_path_;
	bool boot_layout_known_ = false;
	bool boot_layout_ = false;
};

} // namespace native
} // namespace mister
