// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
#include "native/generated/fes_computer.hpp"
#include <vector>
namespace mister {
namespace native {
// fes.pc386.machine 1.0: settings words 0..7 in their sets, then CMOS.
inline bool ValidPc386Machine(const std::vector<std::uint16_t>& w)
{
	using namespace generated;
	if (w.size() != FesComputerPc386MachineWordCount) return false;
	const std::uint16_t preset = w[FesComputerPc386PresetIndex];
	const std::uint16_t frequency = w[FesComputerPc386FrequencyIndex];
	const std::uint16_t irq = w[FesComputerPc386SbIrqIndex];
	if (preset >= FesComputerPc386PresetCount ||
		w[FesComputerPc386CodeProfileIndex] > FesComputerPc386CodeProfileQueue16 ||
		w[FesComputerPc386TurboIndex] > FesComputerPc386TurboMax ||
		w[FesComputerPc386ReservedIndex] != 0 || w[FesComputerPc386SbDmaIndex] != FesComputerPc386SbDma)
		return false;
	if (frequency != 0 && (preset == FesComputerPc386PresetMax ||
		frequency < FesComputerPc386FrequencyMin || frequency > FesComputerPc386FrequencyMax))
		return false;
	const std::uint16_t base = w[FesComputerPc386SbBaseIndex];
	return (base == FesComputerPc386SbBase220 || base == FesComputerPc386SbBase240) &&
		irq < 16 && ((FesComputerPc386SbIrqMask >> irq) & 1u) != 0;
}
inline std::vector<std::uint16_t> Pc386MachineDefaults()
{
	std::vector<std::uint16_t> words(generated::FesComputerPc386MachineWordCount, 0);
	const Pc386MachineSettings defaults;
	words[generated::FesComputerPc386SbBaseIndex] = defaults.sb_base;
	words[generated::FesComputerPc386SbIrqIndex] = defaults.sb_irq;
	words[generated::FesComputerPc386SbDmaIndex] = defaults.sb_dma;
	return words;
}
inline Pc386MachineSettings Pc386Settings(const std::vector<std::uint16_t>& w)
{
	Pc386MachineSettings s;
	if (w.size() < generated::FesComputerPc386CmosStartIndex) return s;
	s.preset = w[generated::FesComputerPc386PresetIndex];
	s.frequency = w[generated::FesComputerPc386FrequencyIndex];
	s.code_profile = w[generated::FesComputerPc386CodeProfileIndex];
	s.turbo = w[generated::FesComputerPc386TurboIndex];
	s.sb_base = w[generated::FesComputerPc386SbBaseIndex];
	s.sb_irq = w[generated::FesComputerPc386SbIrqIndex];
	s.sb_dma = w[generated::FesComputerPc386SbDmaIndex];
	return s;
}
} // namespace native
} // namespace mister
