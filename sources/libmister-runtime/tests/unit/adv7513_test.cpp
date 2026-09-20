// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/adv7513.hpp"

#include <assert.h>
#include <stdio.h>

#include <cstdint>
#include <map>

namespace {

void TestDeGeneratorPacks720pFields()
{
	std::uint8_t encoded[6];
	mister::native::adv7513::EncodeDeGenerator(
		mister::native::adv7513::kMenu720pDe, encoded);
	assert(encoded[0] == 0x40);
	assert(encoded[1] == 0xd9);
	assert(encoded[2] == 0x0a);
	assert(encoded[3] == 0x00);
	assert(encoded[4] == 0x2d);
	assert(encoded[5] == 0x00);
	const mister::native::adv7513::DeTiming decoded =
		mister::native::adv7513::DecodeDeGenerator(encoded);
	assert(decoded.hsync_delay_pixels == 259);
	assert(decoded.vsync_delay_lines == 25);
	assert(decoded.active_width == 1280);
	assert(decoded.active_height == 720);
}

void TestNamedFieldsMatchProgrammingGuideBytes()
{
	using namespace mister::native::adv7513;
	assert(kMainMapAddress7Bit == 0x39);
	assert(kRgb444_8bitStyle1 == 0x38);
	assert(kSync720p16x9 == 0x62);
	assert(kHdmiNoHdcp == 0x06);
	assert(kPowerUp == 0x10);
	assert(kStatusLinkReady == 0x60);
	assert(kPixelRepeatManual == 0x40);
	assert(kVic720p60 == 4);
	assert(kI2cFreqId48kRgb444 == 0x20);
	std::uint8_t hi, mid, lo;
	Encode20Bit(kAudioN48k, &hi, &mid, &lo);
	assert(hi == 0x00 && mid == 0x18 && lo == 0x00);
	Encode20Bit(kAudioCts74250kHz, &hi, &mid, &lo);
	assert(hi == 0x01 && mid == 0x22 && lo == 0x0a);
	assert(kCscRgbIdentityUpper == 0xa8);
}

void TestApplicationAudioProgrammingGuideValues()
{
	const auto recipe = mister::native::adv7513::ApplicationAudio48k();
	std::map<unsigned, unsigned> registers;
	std::vector<unsigned> updates;
	for (const auto& write : recipe) {
		registers[write.address] = write.value;
		if (write.address == 0x4a) updates.push_back(write.value);
	}
	// Literal guide values independently pin the hardware contract rather than
	// comparing the recipe to constants used to construct it.
	assert(registers.at(0x0a) == 0x01); // automatic CTS, I2S, 256*Fs
	assert(registers.at(0x0b) == 0x2e); // external MCLK, sample rising SCLK
	assert(registers.at(0x0c) == 0x84); // I2C Fs, I2S0, standard one-bit delay
	assert(registers.at(0x0e) == 0x01); // left/right I2S0 mapping
	assert(registers.at(0x12) == 0x00); // consumer linear PCM
	assert(registers.at(0x14) == 0x02); // 16-bit word length
	assert(registers.at(0x15) == 0x20); // 48 kHz and RGB input
	assert(registers.at(0x01) == 0x00 && registers.at(0x02) == 0x18 &&
		registers.at(0x03) == 0x00); // N=6144
	assert(registers.at(0x47) == 0x00); // valid sample subpackets
	assert(registers.at(0x73) == 0x01); // two channels
	assert(registers.at(0x74) == 0x00 && registers.at(0x75) == 0x00);
	assert(registers.at(0x76) == 0x00 && registers.at(0x77) == 0x00); // FL/FR, 0dB
	assert(updates == std::vector<unsigned>({0xa0, 0x80}));
	assert(registers.count(0x44) == 0); // setup must not enable packets
}

} // namespace

int main()
{
	TestDeGeneratorPacks720pFields();
	TestNamedFieldsMatchProgrammingGuideBytes();
	TestApplicationAudioProgrammingGuideValues();
	puts("adv7513_test: named register packing passed");
	return 0;
}
