// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "native/generated/de10_nano.hpp"
#include "native/hardware.hpp"
#include "native/linux/mmio.hpp"

#include <cstdint>
#include <vector>

namespace mister {
namespace native {

constexpr std::uint8_t kFileIoTarget = 0;
constexpr std::uint8_t kUserIoTarget = 1;
using generated::kSpiGpoAddress;
using generated::kSpiGpiAddress;
using generated::kSpiCoreIdStrobeMask;
using generated::kSpiStrobeMask;
using generated::kSpiFileSelectMask;
using generated::kSpiUserSelectMask;

class Spi {
public:
	virtual ~Spi() {}
	// Toggle the FPGA core-ID strobe and sample GPI after programming. Main
	// performs this edge before its first user-I/O transaction.
	virtual Error SynchronizeCore(std::uint64_t absolute_deadline_ms) = 0;
	virtual Error Exchange(std::uint8_t target,
		const std::vector<std::uint16_t>& request,
		std::vector<std::uint16_t>* response,
		std::uint64_t absolute_deadline_ms) = 0;
};

class LinuxSpi final : public Spi {
public:
	LinuxSpi(Mmio&, Clock&);
	Error SynchronizeCore(std::uint64_t) override;
	Error Exchange(std::uint8_t, const std::vector<std::uint16_t>&,
		std::vector<std::uint16_t>*, std::uint64_t) override;

private:
	Mmio& mmio_;
	Clock& clock_;
};

} // namespace native
} // namespace mister
