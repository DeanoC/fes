// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once

#include "native/generated/fes_de25_computer.hpp"
#include "native/linux/mmio.hpp"

namespace mister { namespace native {

constexpr std::uint32_t De25ComputerCpuBase =
	0x20000000u + generated::FesDe25ComputerPeripheralOffset;

// The protocol client uses resource-relative offsets. LinuxMmio takes physical
// CPU addresses; only this endpoint's seven reads and atomic GPO write are legal.
class De25ComputerMmio final : public Mmio {
public:
	explicit De25ComputerMmio(Mmio& physical) : physical_(physical) {}
	Error Read32(std::uint32_t offset, std::uint32_t* value) override
	{
		if (!value || (offset & 3u) || offset > generated::FesDe25ComputerVideoStatusOffset)
			return {ErrorCode::io_failed, "outside DE25 computer read registers", "transport"};
		__sync_synchronize();
		Error error = physical_.Read32(De25ComputerCpuBase + offset, value);
		__sync_synchronize();
		return error;
	}
	Error Write32(std::uint32_t offset, std::uint32_t value) override
	{
		if (offset != generated::FesDe25ComputerGpoOffset)
			return {ErrorCode::io_failed, "outside DE25 computer command register", "transport"};
		__sync_synchronize();
		Error error = physical_.Write32(De25ComputerCpuBase + offset, value);
		__sync_synchronize();
		return error;
	}
private:
	Mmio& physical_;
};

} } // namespace mister::native
