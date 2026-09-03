// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
//
// ADV7513 main-map programming, named from the public Analog Devices datasheet
// and ADV7513 Programming Guide (register map, quick-start, video input,
// AVI InfoFrame, audio clock regeneration, DE generator packing).
//
// Registers that Analog Devices documents only as "must be set" keep the
// documented required value and are grouped under adi_required. Their
// internal meaning is not published.

#ifndef MISTER_ADV7513_HPP
#define MISTER_ADV7513_HPP

#include "native/video_recipe.hpp"

#include <cstdint>
#include <vector>

namespace mister {
namespace native {
namespace adv7513 {

// 7-bit main-map address. The ADV7513 Programming Guide quotes 8-bit
// addresses 0x72 / 0x7A (R/W bit included). Linux i2c-dev uses 7-bit, so
// those become 0x39 / 0x3D. DE10-Nano pulls PD/AD low, selecting 0x39.
constexpr std::uint8_t kMainMapAddress7Bit = 0x39;
constexpr std::uint8_t kMainMapAddress7BitPdHigh = 0x3D;

// ---------------------------------------------------------------------------
// Main-map register addresses (Programming Guide register map)
// ---------------------------------------------------------------------------
namespace reg {
constexpr std::uint8_t kAudioN19_16 = 0x01;
constexpr std::uint8_t kAudioN15_8 = 0x02;
constexpr std::uint8_t kAudioN7_0 = 0x03;
constexpr std::uint8_t kAudioCts19_16 = 0x07;
constexpr std::uint8_t kAudioCts15_8 = 0x08;
constexpr std::uint8_t kAudioCts7_0 = 0x09;
constexpr std::uint8_t kAudioSource = 0x0a;
constexpr std::uint8_t kAudioConfig = 0x0b;
constexpr std::uint8_t kI2sConfig = 0x0c;
constexpr std::uint8_t kI2sWidth = 0x0d;
constexpr std::uint8_t kAudioCfg3 = 0x14;
constexpr std::uint8_t kI2cFreqIdCfg = 0x15;
constexpr std::uint8_t kVideoInputCfg1 = 0x16;
constexpr std::uint8_t kVideoInputCfg2 = 0x17;
constexpr std::uint8_t kCscUpper0 = 0x18;
constexpr std::uint8_t kCscLower0 = 0x19;
constexpr std::uint8_t kDeGenerator = 0x35; // 0x35-0x3A
constexpr std::uint8_t kPixelRepetition = 0x3b;
constexpr std::uint8_t kVicManual = 0x3c;
constexpr std::uint8_t kPacketEnable0 = 0x40;
constexpr std::uint8_t kPower = 0x41;
constexpr std::uint8_t kStatus = 0x42;
constexpr std::uint8_t kPacketI2cAddr = 0x45;
constexpr std::uint8_t kVideoInputCfg3 = 0x48;
constexpr std::uint8_t kInfoframeUpdate = 0x4a;
constexpr std::uint8_t kAviInfoframe = 0x55; // 0x55-0x6f data bytes
constexpr std::uint8_t kAudioInfoframeCc = 0x73;
constexpr std::uint8_t kIntEnable0 = 0x94;
constexpr std::uint8_t kInt0 = 0x96;
constexpr std::uint8_t kInputClkDiv = 0x9d;
constexpr std::uint8_t kHdmiPower = 0xa1;
constexpr std::uint8_t kHdcpHdmiCfg = 0xaf;
constexpr std::uint8_t kClockDelay = 0xba;
constexpr std::uint8_t kBksv0 = 0xc0;
constexpr std::uint8_t kEdidSegment = 0xc4;
constexpr std::uint8_t kEdidReadCtrl = 0xc9;
constexpr std::uint8_t kPower2 = 0xd6;
constexpr std::uint8_t kTmdsClockInv = 0xde;
constexpr std::uint8_t kCecCtrl = 0xe2;
constexpr std::uint8_t kPhaseSearch = 0xfa;
} // namespace reg

// Analog Devices required writes (Programming Guide §3 quick-start / §4.2.9).
// 0x9A is 0x70 in the known-working Main table (FPGA hdmi_config.sv). The
// guide's quick-start lists 0x9A[7:5]=111 (0xE0). Keep Main's working value.
namespace adi_required {
constexpr std::uint8_t kReg98 = 0x98;
constexpr std::uint8_t kVal98 = 0x03;
constexpr std::uint8_t kReg9a = 0x9a;
constexpr std::uint8_t kVal9a = 0x70;
constexpr std::uint8_t kReg9c = 0x9c;
constexpr std::uint8_t kVal9c = 0x30;
constexpr std::uint8_t kRegA2 = 0xa2;
constexpr std::uint8_t kValA2 = 0xa4;
constexpr std::uint8_t kRegA3 = 0xa3;
constexpr std::uint8_t kValA3 = 0xa4;
constexpr std::uint8_t kRegE0 = 0xe0;
constexpr std::uint8_t kValE0 = 0xd0;
constexpr std::uint8_t kReg49 = 0x49;
constexpr std::uint8_t kVal49 = 0xa8;
constexpr std::uint8_t kReg4c = 0x4c;
constexpr std::uint8_t kVal4c = 0x00;
constexpr std::uint8_t kReg99 = 0x99;
constexpr std::uint8_t kVal99 = 0x02;
constexpr std::uint8_t kReg9b = 0x9b;
constexpr std::uint8_t kVal9b = 0x18;
constexpr std::uint8_t kReg9f = 0x9f;
constexpr std::uint8_t kVal9f = 0x00;
constexpr std::uint8_t kRegA4 = 0xa4;
constexpr std::uint8_t kValA4 = 0x08;
constexpr std::uint8_t kRegA5 = 0xa5;
constexpr std::uint8_t kValA5 = 0x04;
constexpr std::uint8_t kRegA6 = 0xa6;
constexpr std::uint8_t kValA6 = 0x00;
constexpr std::uint8_t kRegA7 = 0xa7;
constexpr std::uint8_t kValA7 = 0x00;
constexpr std::uint8_t kRegA8 = 0xa8;
constexpr std::uint8_t kValA8 = 0x00;
constexpr std::uint8_t kRegA9 = 0xa9;
constexpr std::uint8_t kValA9 = 0x00;
constexpr std::uint8_t kRegAa = 0xaa;
constexpr std::uint8_t kValAa = 0x00;
constexpr std::uint8_t kRegAb = 0xab;
constexpr std::uint8_t kValAb = 0x40;
constexpr std::uint8_t kRegB9 = 0xb9;
constexpr std::uint8_t kValB9 = 0x00;
constexpr std::uint8_t kRegBb = 0xbb;
constexpr std::uint8_t kValBb = 0x00;
constexpr std::uint8_t kValDe = 0x9c;
constexpr std::uint8_t kRegE4 = 0xe4;
constexpr std::uint8_t kValE4 = 0x60;
} // namespace adi_required

// 0x41 Power. Bit 6 = 1 power-down. Programming Guide power-up value 0x10.
constexpr std::uint8_t kPowerPowerDown = 1u << 6;
constexpr std::uint8_t kPowerUp = 0x10;

// 0x42 Status.
constexpr std::uint8_t kStatusHpd = 1u << 6;
constexpr std::uint8_t kStatusMonitorSense = 1u << 5;
constexpr std::uint8_t kStatusLinkReady = kStatusHpd | kStatusMonitorSense;

// 0xD6 Power2 HPD source [7:6]: 11 = force HPD high (always connected).
constexpr std::uint8_t kPower2HpdAlwaysHigh = 0xc0;

// 0x9D Input clock divide. [7:4] must be 0110, [1:0] must be 01,
// [3:2] = 00 => input clock not divided.
constexpr std::uint8_t kInputClkDivUndivided = 0x61;

// 0x16 Video Input Config 1.
constexpr std::uint8_t kOutputFormat422 = 1u << 7;
constexpr std::uint8_t kColorDepth8Bit = 3u << 4;   // [5:4] = 11
constexpr std::uint8_t kInputStyle1 = 2u << 2;     // [3:2] = 10
constexpr std::uint8_t kDdrInputRising = 1u << 1;
constexpr std::uint8_t kOutputYcbcr = 1u << 0;
constexpr std::uint8_t kRgb444_8bitStyle1 =
	kColorDepth8Bit | kInputStyle1; // 0x38

// 0x17 Video Input Config 2 / sync / DE.
constexpr std::uint8_t kVsyncPolarity = 1u << 6;
constexpr std::uint8_t kHsyncPolarity = 1u << 5;
constexpr std::uint8_t kAspect16x9 = 1u << 1;
constexpr std::uint8_t kDeGeneratorEnable = 1u << 0;
// Known-working Main mode-0 720p encoding: both polarity bits, 16:9, external DE.
constexpr std::uint8_t kSync720p16x9 =
	kVsyncPolarity | kHsyncPolarity | kAspect16x9; // 0x62

// 0x48: [4:3] = 01 right-justified (used for 4:2:2; ignored for 4:4:4).
constexpr std::uint8_t kDataRightJustified = 1u << 3; // 0x08

// 0x4A bit 7: update AVI InfoFrame from the buffered registers.
constexpr std::uint8_t kInfoframeUpdateEnable = 1u << 7;

// 0x3B Pixel repetition. [6:5] 00 auto, 10 manual. Init table uses bit 7 as
// well (Main's software path); the 720p mode write then selects manual x1.
constexpr std::uint8_t kPixelRepeatAuto = 0x80;
constexpr std::uint8_t kPixelRepeatManual = 2u << 5; // 0x40, x1 clock, x1 sent

// CEA-861 VIC 4 = 1280x720p60 16:9.
constexpr std::uint8_t kVic720p60 = 4;
constexpr std::uint8_t kVicNone = 0;

// HDMI/HDCP 0xAF: [3:2] must be 01, [1]=1 HDMI, [7]=0 HDCP off.
constexpr std::uint8_t kHdmiMode = 1u << 1;
constexpr std::uint8_t kHdcpHdmiFixed = 1u << 2;
constexpr std::uint8_t kHdmiNoHdcp = kHdcpHdmiFixed | kHdmiMode; // 0x06

// 0xBA [7:5] input clock delay: 011 = none.
constexpr std::uint8_t kClockDelayNone = 3u << 5; // 0x60

// 0xE2 CEC: bit 0 = CEC power-down.
constexpr std::uint8_t kCecPowerDown = 1u << 0;

// 0xFA: phase-search attempts. Programming Guide / AN-1270: 0x7D.
constexpr std::uint8_t kPhaseSearchAttempts = 0x7d;

// 0x96 interrupt flags. Bit 2 = EDID ready.
constexpr std::uint8_t kInt0EdidReady = 1u << 2;
constexpr std::uint8_t kInt0ClearAll = 0xff;

// EDID read control 0xC9: pulse bit 4 to request a DDC/EDID read.
constexpr std::uint8_t kEdidReadEnable = 0x03;
constexpr std::uint8_t kEdidReadTrigger = 0x13;

// AVI InfoFrame data bytes (HDMI 1.4 / CEA-861).
// 0x55 DB1: Y1Y0=00 RGB, A0=1 active format valid.
constexpr std::uint8_t kAviRgbActiveFormat = 1u << 4; // 0x10
// 0x56 DB2: R3..R0 = 1000 "same as picture aspect".
constexpr std::uint8_t kAviRSameAsPicture = 0x08;
// 0x57 DB3: Q1Q0=10 full-range RGB (HDMI quantization).
constexpr std::uint8_t kAviRgbQuantFull = 2u << 2; // 0x08

// Audio: I2S, I2S0 enable, 16-bit, N=6144 (48 kHz), CTS=74250 (74.25 MHz).
constexpr std::uint8_t kAudioSelectI2s = 0x00;
constexpr std::uint8_t kAudioConfigI2s = 0x0e;
constexpr std::uint8_t kI2s0EnableStandard = 1u << 2; // 0x04
constexpr std::uint8_t kI2sWordWidth = 0x10;
constexpr std::uint8_t kAudioWordLength16 = 0x02;
constexpr std::uint8_t kSampleRate48k = 2u << 4; // 0x15[7:4]
constexpr std::uint8_t kInputIdRgb444SeparateSyncs = 0; // 0x15[3:0]
constexpr std::uint8_t kI2cFreqId48kRgb444 =
	kSampleRate48k | kInputIdRgb444SeparateSyncs; // 0x20
constexpr std::uint32_t kAudioN48k = 6144;
constexpr std::uint32_t kAudioCts74250kHz = 74250;
constexpr std::uint8_t kAudioInfoframeTwoChannels = 0x01;

// CSC 0x18: bit7 enable, [6:5] scale. Identity 0x0800 on the diagonal is the
// known-working Main RGB table (not the FPGA hdmi_config.sv limited-range set).
constexpr std::uint8_t kCscEnable = 1u << 7;
constexpr std::uint8_t kCscScale2 = 1u << 5;
constexpr std::uint8_t kCscUpdate = 1u << 3;
constexpr std::uint8_t kCscRgbIdentityUpper = kCscEnable | kCscScale2 | kCscUpdate; // 0xA8
constexpr std::uint8_t kCscIdentityCoeffHi = 0x08;

// 0xC0-0xC3 BKSV placeholder used by Main when HDCP is off.
constexpr std::uint8_t kBksv0 = 0x00;
constexpr std::uint8_t kBksv1 = 0x00;
constexpr std::uint8_t kBksv2 = 0x0f;
constexpr std::uint8_t kBksv3 = 0xff;

struct DeTiming {
	std::uint16_t hsync_delay_pixels; // 10-bit, 0x35[7:0]|0x36[7:6]
	std::uint16_t vsync_delay_lines;   // 6-bit, 0x36[5:0]
	std::uint16_t active_width;        // 12-bit, 0x37[4:0]|0x38[7:1]
	std::uint16_t active_height;       // 12-bit, 0x39[7:0]|0x3A[7:4]
};

// Known-working Main 720p DE-generator fields (Programming Guide §4.3.6 packing).
constexpr DeTiming kMenu720pDe = {259, 25, 1280, 720};

inline void EncodeDeGenerator(const DeTiming& de, std::uint8_t out[6])
{
	out[0] = static_cast<std::uint8_t>(de.hsync_delay_pixels >> 2);
	out[1] = static_cast<std::uint8_t>(
		((de.hsync_delay_pixels & 0x03) << 6) | (de.vsync_delay_lines & 0x3f));
	out[2] = static_cast<std::uint8_t>((de.active_width >> 7) & 0x1f);
	out[3] = static_cast<std::uint8_t>((de.active_width & 0x7f) << 1);
	out[4] = static_cast<std::uint8_t>(de.active_height >> 4);
	out[5] = static_cast<std::uint8_t>((de.active_height & 0x0f) << 4);
}

inline DeTiming DecodeDeGenerator(const std::uint8_t in[6])
{
	DeTiming de;
	de.hsync_delay_pixels = static_cast<std::uint16_t>(
		(static_cast<std::uint16_t>(in[0]) << 2) | (in[1] >> 6));
	de.vsync_delay_lines = static_cast<std::uint16_t>(in[1] & 0x3f);
	de.active_width = static_cast<std::uint16_t>(
		(static_cast<std::uint16_t>(in[2] & 0x1f) << 7) | (in[3] >> 1));
	de.active_height = static_cast<std::uint16_t>(
		(static_cast<std::uint16_t>(in[4]) << 4) | (in[5] >> 4));
	return de;
}

inline void Encode20Bit(std::uint32_t value, std::uint8_t* hi, std::uint8_t* mid,
	std::uint8_t* lo)
{
	*hi = static_cast<std::uint8_t>((value >> 16) & 0x0f);
	*mid = static_cast<std::uint8_t>(value >> 8);
	*lo = static_cast<std::uint8_t>(value);
}

inline RegisterWrite Wr(std::uint8_t address, std::uint8_t value)
{
	return {address, value};
}

inline std::vector<RegisterWrite> Menu720p60Initialization()
{
	std::uint8_t de[6];
	EncodeDeGenerator(kMenu720pDe, de);
	std::uint8_t n_hi, n_mid, n_lo;
	Encode20Bit(kAudioN48k, &n_hi, &n_mid, &n_lo);
	std::uint8_t cts_hi, cts_mid, cts_lo;
	Encode20Bit(kAudioCts74250kHz, &cts_hi, &cts_mid, &cts_lo);

	return {
		Wr(adi_required::kReg98, adi_required::kVal98),
		Wr(reg::kPower2, kPower2HpdAlwaysHigh),
		Wr(reg::kPower, kPowerUp),
		Wr(adi_required::kReg9a, adi_required::kVal9a),
		Wr(adi_required::kReg9c, adi_required::kVal9c),
		Wr(reg::kInputClkDiv, kInputClkDivUndivided),
		Wr(adi_required::kRegA2, adi_required::kValA2),
		Wr(adi_required::kRegA3, adi_required::kValA3),
		Wr(adi_required::kRegE0, adi_required::kValE0),
		Wr(reg::kDeGenerator + 0, de[0]),
		Wr(reg::kDeGenerator + 1, de[1]),
		Wr(reg::kDeGenerator + 2, de[2]),
		Wr(reg::kDeGenerator + 3, de[3]),
		Wr(reg::kDeGenerator + 4, de[4]),
		Wr(reg::kDeGenerator + 5, de[5]),
		Wr(reg::kVideoInputCfg1, kRgb444_8bitStyle1),
		Wr(reg::kVideoInputCfg2, kSync720p16x9),
		Wr(reg::kPixelRepetition, kPixelRepeatAuto),
		Wr(reg::kVicManual, kVicNone),
		Wr(reg::kVideoInputCfg3, kDataRightJustified),
		Wr(adi_required::kReg49, adi_required::kVal49),
		Wr(reg::kPacketEnable0, 0x00),
		Wr(reg::kInfoframeUpdate, kInfoframeUpdateEnable),
		Wr(adi_required::kReg4c, adi_required::kVal4c),
		Wr(reg::kAviInfoframe + 0, kAviRgbActiveFormat),
		Wr(reg::kAviInfoframe + 1, kAviRSameAsPicture),
		Wr(reg::kAviInfoframe + 2, kAviRgbQuantFull),
		Wr(reg::kAviInfoframe + 4, 0x00),
		Wr(reg::kAudioInfoframeCc, kAudioInfoframeTwoChannels),
		Wr(reg::kInt0, kInt0ClearAll),
		Wr(reg::kIntEnable0, 0x00),
		Wr(reg::kEdidReadCtrl, 0x00),
		Wr(adi_required::kReg99, adi_required::kVal99),
		Wr(adi_required::kReg9b, adi_required::kVal9b),
		Wr(adi_required::kReg9f, adi_required::kVal9f),
		Wr(reg::kHdmiPower, 0x00),
		Wr(adi_required::kRegA4, adi_required::kValA4),
		Wr(adi_required::kRegA5, adi_required::kValA5),
		Wr(adi_required::kRegA6, adi_required::kValA6),
		Wr(adi_required::kRegA7, adi_required::kValA7),
		Wr(adi_required::kRegA8, adi_required::kValA8),
		Wr(adi_required::kRegA9, adi_required::kValA9),
		Wr(adi_required::kRegAa, adi_required::kValAa),
		Wr(adi_required::kRegAb, adi_required::kValAb),
		Wr(reg::kHdcpHdmiCfg, kHdmiNoHdcp),
		Wr(adi_required::kRegB9, adi_required::kValB9),
		Wr(reg::kClockDelay, kClockDelayNone),
		Wr(adi_required::kRegBb, adi_required::kValBb),
		Wr(reg::kTmdsClockInv, adi_required::kValDe),
		Wr(reg::kCecCtrl, kCecPowerDown),
		Wr(adi_required::kRegE4, adi_required::kValE4),
		Wr(reg::kPhaseSearch, kPhaseSearchAttempts),
		Wr(reg::kAudioSource, kAudioSelectI2s),
		Wr(reg::kAudioConfig, kAudioConfigI2s),
		Wr(reg::kI2sConfig, kI2s0EnableStandard),
		Wr(reg::kI2sWidth, kI2sWordWidth),
		Wr(reg::kAudioCfg3, kAudioWordLength16),
		Wr(reg::kI2cFreqIdCfg, kI2cFreqId48kRgb444),
		Wr(reg::kAudioN19_16, n_hi),
		Wr(reg::kAudioN15_8, n_mid),
		Wr(reg::kAudioN7_0, n_lo),
		Wr(reg::kAudioCts19_16, cts_hi),
		Wr(reg::kAudioCts15_8, cts_mid),
		Wr(reg::kAudioCts7_0, cts_lo),
		Wr(reg::kCscUpper0 + 0, kCscRgbIdentityUpper),
		Wr(reg::kCscUpper0 + 1, 0x00),
		Wr(reg::kCscUpper0 + 2, 0x00),
		Wr(reg::kCscUpper0 + 3, 0x00),
		Wr(reg::kCscUpper0 + 4, 0x00),
		Wr(reg::kCscUpper0 + 5, 0x00),
		Wr(reg::kCscUpper0 + 6, 0x00),
		Wr(reg::kCscUpper0 + 7, 0x00),
		Wr(reg::kCscUpper0 + 8, 0x00),
		Wr(reg::kCscUpper0 + 9, 0x00),
		Wr(reg::kCscUpper0 + 10, kCscIdentityCoeffHi),
		Wr(reg::kCscUpper0 + 11, 0x00),
		Wr(reg::kCscUpper0 + 12, 0x00),
		Wr(reg::kCscUpper0 + 13, 0x00),
		Wr(reg::kCscUpper0 + 14, 0x00),
		Wr(reg::kCscUpper0 + 15, 0x00),
		Wr(reg::kCscUpper0 + 16, 0x00),
		Wr(reg::kCscUpper0 + 17, 0x00),
		Wr(reg::kCscUpper0 + 18, 0x00),
		Wr(reg::kCscUpper0 + 19, 0x00),
		Wr(reg::kCscUpper0 + 20, kCscIdentityCoeffHi),
		Wr(reg::kCscUpper0 + 21, 0x00),
		Wr(reg::kCscUpper0 + 22, 0x00),
		Wr(reg::kCscUpper0 + 23, 0x00),
		Wr(reg::kBksv0 + 0, kBksv0),
		Wr(reg::kBksv0 + 1, kBksv1),
		Wr(reg::kBksv0 + 2, kBksv2),
		Wr(reg::kBksv0 + 3, kBksv3),
	};
}

inline std::vector<RegisterWrite> Menu720p60Mode()
{
	return {
		Wr(reg::kVideoInputCfg2, kSync720p16x9),
		Wr(reg::kPixelRepetition, kPixelRepeatManual),
		Wr(reg::kVicManual, kVic720p60),
	};
}

inline std::vector<RegisterWrite> HdmiWake()
{
	return {
		Wr(reg::kInt0, kInt0EdidReady),
		Wr(reg::kEdidSegment, 0x00),
		Wr(reg::kEdidReadCtrl, kEdidReadEnable),
		Wr(reg::kEdidReadCtrl, kEdidReadTrigger),
		Wr(reg::kEdidReadCtrl, kEdidReadEnable),
	};
}

} // namespace adv7513
} // namespace native
} // namespace mister

#endif
