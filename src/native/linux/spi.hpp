// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "native/hardware.hpp"
#include "native/linux/mmio.hpp"

#include <cstdint>
#include <vector>

namespace mister {
namespace native {

constexpr std::uint8_t kFileIoTarget = 0;
constexpr std::uint8_t kUserIoTarget = 1;
constexpr std::uint32_t kSpiGpoAddress = 0xff706010u;
constexpr std::uint32_t kSpiGpiAddress = 0xff706014u;
constexpr std::uint32_t kSpiStrobeMask = 0x00020000u;
constexpr std::uint32_t kSpiFileSelectMask = 0x00040000u;
constexpr std::uint32_t kSpiUserSelectMask = 0x00100000u;

class Spi {
public:
	virtual ~Spi() {}
	virtual Error Exchange(std::uint8_t target,
		const std::vector<std::uint16_t>& request,
		std::vector<std::uint16_t>* response,
		std::uint64_t absolute_deadline_ms) = 0;
};

class LinuxSpi final : public Spi {
public:
	LinuxSpi(Mmio&, Clock&);
	Error Exchange(std::uint8_t, const std::vector<std::uint16_t>&,
		std::vector<std::uint16_t>*, std::uint64_t) override;

private:
	Mmio& mmio_;
	Clock& clock_;
};

} // namespace native
} // namespace mister
