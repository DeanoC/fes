// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
#include "native/generated/fes_computer.hpp"
#include <ctime>
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
	if (frequency != 0 && (preset == FesComputerPc386PresetMaxPerformance ||
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
// The clock the record's CMOS bytes held at its last Stop, in the form its
// register B names (BCD or binary, 12- or 24-hour), with the century from
// 32h. False unless every field is a valid date and time.
inline bool Pc386RecordClock(const std::vector<std::uint16_t>& w, std::tm* out)
{
	if (w.size() != generated::FesComputerPc386MachineWordCount) return false;
	const auto cmos = [&](unsigned n) {
		const std::uint16_t word = w[generated::FesComputerPc386CmosStartIndex + n / 2];
		return static_cast<unsigned>(n & 1u ? word >> 8 : word & 0xffu);
	};
	const bool binary = (cmos(0x0b) & 0x04u) != 0, hour24 = (cmos(0x0b) & 0x02u) != 0;
	bool valid = true;
	const auto field = [&](unsigned value) {
		if (binary) return value;
		if ((value & 0x0fu) > 9 || (value >> 4) > 9) valid = false;
		return (value >> 4) * 10 + (value & 0x0fu);
	};
	const unsigned second = field(cmos(0x00)), minute = field(cmos(0x02));
	const unsigned hour_byte = cmos(0x04);
	unsigned hour = field(hour24 ? hour_byte : hour_byte & 0x7fu);
	const unsigned day = field(cmos(0x07)), month = field(cmos(0x08)), year = field(cmos(0x09));
	const unsigned century_byte = cmos(0x32);
	const unsigned century = (century_byte >> 4) * 10 + (century_byte & 0x0fu);
	if ((century_byte & 0x0fu) > 9 || (century_byte >> 4) > 9) valid = false;
	if (!hour24) {
		if (hour < 1 || hour > 12) valid = false;
		hour = hour % 12 + ((hour_byte & 0x80u) ? 12 : 0);
	}
	const unsigned full_year = century * 100 + year;
	static const unsigned days[] = {31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31};
	const bool leap = full_year % 4 == 0 && (full_year % 100 != 0 || full_year % 400 == 0);
	if (!valid || second > 59 || minute > 59 || hour > 23 || year > 99 || month < 1 || month > 12 ||
		day < 1 || day > days[month - 1] + (month == 2 && leap ? 1 : 0) ||
		full_year < generated::FesComputerWallClockMinYear ||
		full_year > generated::FesComputerWallClockMaxYear)
		return false;
	// The date's own weekday, 0 for Sunday as std::tm counts it.
	static const unsigned shift[] = {0, 3, 2, 5, 0, 3, 5, 1, 4, 6, 2, 4};
	const unsigned y = full_year - (month < 3 ? 1 : 0);
	std::tm time{};
	time.tm_year = static_cast<int>(full_year) - 1900;
	time.tm_mon = static_cast<int>(month) - 1;
	time.tm_mday = static_cast<int>(day);
	time.tm_hour = static_cast<int>(hour);
	time.tm_min = static_cast<int>(minute);
	time.tm_sec = static_cast<int>(second);
	time.tm_wday = static_cast<int>((y + y / 4 - y / 100 + y / 400 + shift[month - 1] + day) % 7);
	*out = time;
	return true;
}
} // namespace native
} // namespace mister
