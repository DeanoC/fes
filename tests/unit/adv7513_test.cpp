// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/adv7513.hpp"

#include <assert.h>
#include <stdio.h>

#include <cstdint>

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

} // namespace

int main()
{
	TestDeGeneratorPacks720pFields();
	TestNamedFieldsMatchProgrammingGuideBytes();
	puts("adv7513_test: named register packing passed");
	return 0;
}
