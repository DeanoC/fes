// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
#include "native/generated/fes_computer.hpp"
#include <vector>
namespace mister {
namespace native {
// fes.pcxt.machine 1.0: 4 words (video_card, mono_tint, reserved0, reserved1).
inline bool ValidPcxtMachine(const std::vector<std::uint16_t>& w)
{
	using namespace generated;
	return w.size() == FesComputerPcxtMachineWordCount &&
		w[FesComputerPcxtVideoCardIndex] <= FesComputerPcxtVideoCardMax &&
		w[FesComputerPcxtMonoTintIndex] <= FesComputerPcxtMonoTintMax &&
		w[FesComputerPcxtReserved0Index] == 0 &&
		w[FesComputerPcxtReserved1Index] == 0;
}
inline std::vector<std::uint16_t> PcxtMachineDefaults()
{
	return std::vector<std::uint16_t>(generated::FesComputerPcxtMachineWordCount, 0);
}
inline PcxtMachineSettings PcxtSettings(const std::vector<std::uint16_t>& w)
{
	PcxtMachineSettings s;
	if (w.size() < generated::FesComputerPcxtMachineWordCount) return s;
	s.video_card = w[generated::FesComputerPcxtVideoCardIndex];
	s.mono_tint = w[generated::FesComputerPcxtMonoTintIndex];
	return s;
}
} // namespace native
} // namespace mister
