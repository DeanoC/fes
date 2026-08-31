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
constexpr std::uint32_t kFpgaMonitorAddress = 0xff706850u;
constexpr std::uint32_t kFpgaDataAddress = 0xffb90000u;

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
