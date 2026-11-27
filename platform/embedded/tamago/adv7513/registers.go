// SPDX-License-Identifier: GPL-3.0-or-later

// Package adv7513 programs the Analog Devices ADV7513 HDMI transmitter and
// decodes monitor EDID/CEA-861 blocks over an i2c.Bus, matching the register
// contracts in sources/libmister-runtime/src/native/adv7513.hpp.
package adv7513

// Default 7-bit I2C addresses for ADV7513 main register map and EDID memory map.
const (
	MainMapAddr7Bit       uint8 = 0x39
	MainMapAddr7BitPDHigh uint8 = 0x3d
	DefaultEDIDMapAddr    uint8 = 0x3f

	ExpectedChipRevision uint8 = 0x13
)

// ADV7513 main-map register addresses (Programming Guide register map).
const (
	RegChipRevision    uint8 = 0x00
	RegAudioN19_16     uint8 = 0x01
	RegAudioN15_8      uint8 = 0x02
	RegAudioN7_0       uint8 = 0x03
	RegAudioCts19_16   uint8 = 0x07
	RegAudioCts15_8    uint8 = 0x08
	RegAudioCts7_0     uint8 = 0x09
	RegAudioSource     uint8 = 0x0a
	RegAudioConfig     uint8 = 0x0b
	RegI2sConfig       uint8 = 0x0c
	RegI2sWidth        uint8 = 0x0d
	RegAudioCfg3       uint8 = 0x14
	RegI2cFreqIdCfg    uint8 = 0x15
	RegVideoInputCfg1  uint8 = 0x16
	RegVideoInputCfg2  uint8 = 0x17
	RegCscUpper0       uint8 = 0x18
	RegDeGenerator     uint8 = 0x35
	RegPixelRepetition uint8 = 0x3b
	RegVicManual       uint8 = 0x3c
	RegVicDetected     uint8 = 0x3e
	RegPacketEnable0   uint8 = 0x40
	RegPower           uint8 = 0x41
	RegStatus          uint8 = 0x42
	RegEDIDAddr        uint8 = 0x43
	RegPacketEnable1   uint8 = 0x44
	RegVideoInputCfg3  uint8 = 0x48
	RegInfoframeUpdate uint8 = 0x4a
	RegAviInfoframe    uint8 = 0x55
	RegAudioInfoframe  uint8 = 0x73
	RegIntEnable0      uint8 = 0x94
	RegInt0            uint8 = 0x96
	RegInputClkDiv     uint8 = 0x9d
	RegPllStatus       uint8 = 0x9e
	RegHdmiPower       uint8 = 0xa1
	RegHdcpHdmiCfg     uint8 = 0xaf
	RegClockDelay      uint8 = 0xba
	RegBksv0           uint8 = 0xc0
	RegEdidSegment     uint8 = 0xc4
	RegEdidReadCtrl    uint8 = 0xc9
	RegPower2          uint8 = 0xd6
	RegTmdsClockInv    uint8 = 0xde
	RegCecCtrl         uint8 = 0xe2
	RegPhaseSearch     uint8 = 0xfa
)

// Analog Devices required fixed register values (Programming Guide section 3 / 4.2.9).
const (
	adiReg98, adiVal98 uint8 = 0x98, 0x03
	adiReg9a, adiVal9a uint8 = 0x9a, 0x70
	adiReg9c, adiVal9c uint8 = 0x9c, 0x30
	adiRegA2, adiValA2 uint8 = 0xa2, 0xa4
	adiRegA3, adiValA3 uint8 = 0xa3, 0xa4
	adiRegE0, adiValE0 uint8 = 0xe0, 0xd0
	adiReg49, adiVal49 uint8 = 0x49, 0xa8
	adiReg4c, adiVal4c uint8 = 0x4c, 0x00
	adiReg99, adiVal99 uint8 = 0x99, 0x02
	adiReg9b, adiVal9b uint8 = 0x9b, 0x18
	adiReg9f, adiVal9f uint8 = 0x9f, 0x00
	adiRegA4, adiValA4 uint8 = 0xa4, 0x08
	adiRegA5, adiValA5 uint8 = 0xa5, 0x04
	adiRegA6, adiValA6 uint8 = 0xa6, 0x00
	adiRegA7, adiValA7 uint8 = 0xa7, 0x00
	adiRegA8, adiValA8 uint8 = 0xa8, 0x00
	adiRegA9, adiValA9 uint8 = 0xa9, 0x00
	adiRegAa, adiValAa uint8 = 0xaa, 0x00
	adiRegAb, adiValAb uint8 = 0xab, 0x40
	adiRegB9, adiValB9 uint8 = 0xb9, 0x00
	adiRegBb, adiValBb uint8 = 0xbb, 0x00
	adiValDe           uint8 = 0x9c
	adiRegE4, adiValE4 uint8 = 0xe4, 0x60
	adiRegF9, adiValF9 uint8 = 0xf9, 0x00
)

// Field and command constants matching adv7513.hpp.
const (
	PowerDown uint8 = 1 << 6
	PowerUp   uint8 = 0x10

	StatusHPD          uint8 = 1 << 6
	StatusMonitorSense uint8 = 1 << 5
	StatusLinkReady    uint8 = StatusHPD | StatusMonitorSense

	PllLocked uint8 = 1 << 4
	VicShift  uint8 = 2

	Power2HpdAlwaysHigh uint8 = 0xc0
	InputClkDivUndiv    uint8 = 0x61

	ColorDepth8Bit     uint8 = 3 << 4
	InputStyle1        uint8 = 2 << 2
	Rgb4448bitStyle1   uint8 = ColorDepth8Bit | InputStyle1 // 0x38
	VsyncPolarity      uint8 = 1 << 6
	HsyncPolarity      uint8 = 1 << 5
	Aspect16x9         uint8 = 1 << 1
	Sync720p16x9       uint8 = VsyncPolarity | HsyncPolarity | Aspect16x9 // 0x62
	DataRightJustified uint8 = 1 << 3                                     // 0x08

	InfoframeAutoChecksum uint8 = 1 << 7
	AudioInfoframeUpdate  uint8 = 1 << 5
	PacketsVideoOnly      uint8 = 0x11
	PacketsVideoAudio     uint8 = 0x79

	PixelRepeatAuto   uint8 = 0x80
	PixelRepeatManual uint8 = 2 << 5 // 0x40
	Vic720p60         uint8 = 4
	VicNone           uint8 = 0

	HdmiMode             uint8 = 1 << 1
	HdcpHdmiFixed        uint8 = 1 << 2
	HdmiNoHdcp           uint8 = HdcpHdmiFixed | HdmiMode // 0x06
	ClockDelayMinus0p8ns uint8 = 1 << 5                   // 0x20
	CecPowerDown         uint8 = 1 << 0
	PhaseSearchAttempts  uint8 = 0x7d

	Int0EDIDReady uint8 = 1 << 2
	Int0ClearAll  uint8 = 0xff

	EdidReadEnable  uint8 = 0x03
	EdidReadTrigger uint8 = 0x13

	AviRgbActiveFormat uint8 = 1 << 4 // 0x10
	AviRSameAsPicture  uint8 = 0x08
	AviRgbQuantFull    uint8 = 2 << 2 // 0x08

	AudioSelectI2s         uint8  = 0x00
	AudioConfigI2s         uint8  = 0x0e
	I2s0EnableStandard     uint8  = 1 << 2 // 0x04
	I2sWordWidth           uint8  = 0x10
	AudioWordLength16      uint8  = 0x02
	SampleRate48k          uint8  = 2 << 4
	I2cFreqId48kRgb444     uint8  = SampleRate48k
	AudioN48k              uint32 = 6144
	AudioCts74250kHz       uint32 = 74250
	AudioInfoframeTwoChans uint8  = 0x01

	CscEnable           uint8 = 1 << 7
	CscScale2           uint8 = 1 << 5
	CscUpdate           uint8 = 1 << 3
	CscRgbIdentityUpper uint8 = CscEnable | CscScale2 | CscUpdate // 0xa8
	CscIdentityCoeffHi  uint8 = 0x08

	bksv0, bksv1, bksv2, bksv3 uint8 = 0x00, 0x00, 0x0f, 0xff
)

// RegisterWrite represents one register write pair (address, value).
type RegisterWrite struct {
	Address uint8
	Value   uint8
}

// DETiming holds ADV7513 data-enable generator parameters (0x35..0x3a).
type DETiming struct {
	HSyncDelayPixels uint16 // 10-bit
	VSyncDelayLines  uint16 // 6-bit
	ActiveWidth      uint16 // 12-bit
	ActiveHeight     uint16 // 12-bit
}

// Menu720pDE matches kMenu720pDe in adv7513.hpp.
var Menu720pDE = DETiming{
	HSyncDelayPixels: 259,
	VSyncDelayLines:  25,
	ActiveWidth:      1280,
	ActiveHeight:     720,
}

// EncodeDEGenerator packs de into the 6 ADV7513 DE-generator registers (0x35..0x3a).
func EncodeDEGenerator(de DETiming) [6]byte {
	var out [6]byte
	out[0] = byte(de.HSyncDelayPixels >> 2)
	out[1] = byte(((de.HSyncDelayPixels & 0x03) << 6) | (de.VSyncDelayLines & 0x3f))
	out[2] = byte((de.ActiveWidth >> 7) & 0x1f)
	out[3] = byte((de.ActiveWidth & 0x7f) << 1)
	out[4] = byte(de.ActiveHeight >> 4)
	out[5] = byte((de.ActiveHeight & 0x0f) << 4)
	return out
}

// DecodeDEGenerator unpacks 6 DE-generator registers (0x35..0x3a).
func DecodeDEGenerator(in [6]byte) DETiming {
	return DETiming{
		HSyncDelayPixels: (uint16(in[0]) << 2) | uint16(in[1]>>6),
		VSyncDelayLines:  uint16(in[1] & 0x3f),
		ActiveWidth:      (uint16(in[2]&0x1f) << 7) | uint16(in[3]>>1),
		ActiveHeight:     (uint16(in[4]) << 4) | uint16(in[5]>>4),
	}
}

// Encode20Bit splits a 20-bit N or CTS counter into [19:16], [15:8], [7:0].
func Encode20Bit(value uint32) (hi, mid, lo uint8) {
	return uint8((value >> 16) & 0x0f), uint8(value >> 8), uint8(value)
}

func wr(addr, val uint8) RegisterWrite {
	return RegisterWrite{Address: addr, Value: val}
}

// Menu720p60Initialization returns the exact power-up and RGB 720p60 register sequence
// from Menu720p60Initialization() in adv7513.hpp.
func Menu720p60Initialization() []RegisterWrite {
	de := EncodeDEGenerator(Menu720pDE)
	nHi, nMid, nLo := Encode20Bit(AudioN48k)
	ctsHi, ctsMid, ctsLo := Encode20Bit(AudioCts74250kHz)

	return []RegisterWrite{
		wr(RegPower2, Power2HpdAlwaysHigh),
		wr(RegPower, PowerUp),
		wr(adiReg98, adiVal98),
		wr(RegPacketEnable1, PacketsVideoAudio),
		wr(adiReg9a, adiVal9a),
		wr(adiReg9c, adiVal9c),
		wr(RegInputClkDiv, InputClkDivUndiv),
		wr(adiRegA2, adiValA2),
		wr(adiRegA3, adiValA3),
		wr(adiRegE0, adiValE0),
		wr(RegDeGenerator+0, de[0]),
		wr(RegDeGenerator+1, de[1]),
		wr(RegDeGenerator+2, de[2]),
		wr(RegDeGenerator+3, de[3]),
		wr(RegDeGenerator+4, de[4]),
		wr(RegDeGenerator+5, de[5]),
		wr(RegVideoInputCfg1, Rgb4448bitStyle1),
		wr(RegVideoInputCfg2, Sync720p16x9),
		wr(RegPixelRepetition, PixelRepeatAuto),
		wr(RegVicManual, VicNone),
		wr(RegVideoInputCfg3, DataRightJustified),
		wr(adiReg49, adiVal49),
		wr(RegPacketEnable0, 0x00),
		wr(RegInfoframeUpdate, InfoframeAutoChecksum),
		wr(adiReg4c, adiVal4c),
		wr(RegAviInfoframe+0, AviRgbActiveFormat),
		wr(RegAviInfoframe+1, AviRSameAsPicture),
		wr(RegAviInfoframe+2, AviRgbQuantFull),
		wr(RegAviInfoframe+4, 0x00),
		wr(RegAudioInfoframe, AudioInfoframeTwoChans),
		wr(RegInt0, Int0ClearAll),
		wr(RegIntEnable0, 0x00),
		wr(RegEdidReadCtrl, 0x00),
		wr(adiReg99, adiVal99),
		wr(adiReg9b, adiVal9b),
		wr(adiReg9f, adiVal9f),
		wr(RegHdmiPower, 0x00),
		wr(adiRegA4, adiValA4),
		wr(adiRegA5, adiValA5),
		wr(adiRegA6, adiValA6),
		wr(adiRegA7, adiValA7),
		wr(adiRegA8, adiValA8),
		wr(adiRegA9, adiValA9),
		wr(adiRegAa, adiValAa),
		wr(adiRegAb, adiValAb),
		wr(RegHdcpHdmiCfg, HdmiNoHdcp),
		wr(adiRegB9, adiValB9),
		wr(RegClockDelay, ClockDelayMinus0p8ns),
		wr(adiRegBb, adiValBb),
		wr(RegTmdsClockInv, adiValDe),
		wr(RegCecCtrl, CecPowerDown),
		wr(adiRegE4, adiValE4),
		wr(adiRegF9, adiValF9),
		wr(RegPhaseSearch, PhaseSearchAttempts),
		wr(RegAudioSource, AudioSelectI2s),
		wr(RegAudioConfig, AudioConfigI2s),
		wr(RegI2sConfig, I2s0EnableStandard),
		wr(RegI2sWidth, I2sWordWidth),
		wr(RegAudioCfg3, AudioWordLength16),
		wr(RegI2cFreqIdCfg, I2cFreqId48kRgb444),
		wr(RegAudioN19_16, nHi),
		wr(RegAudioN15_8, nMid),
		wr(RegAudioN7_0, nLo),
		wr(RegAudioCts19_16, ctsHi),
		wr(RegAudioCts15_8, ctsMid),
		wr(RegAudioCts7_0, ctsLo),
		wr(RegCscUpper0+0, CscRgbIdentityUpper),
		wr(RegCscUpper0+1, 0x00),
		wr(RegCscUpper0+2, 0x00),
		wr(RegCscUpper0+3, 0x00),
		wr(RegCscUpper0+4, 0x00),
		wr(RegCscUpper0+5, 0x00),
		wr(RegCscUpper0+6, 0x00),
		wr(RegCscUpper0+7, 0x00),
		wr(RegCscUpper0+8, 0x00),
		wr(RegCscUpper0+9, 0x00),
		wr(RegCscUpper0+10, CscIdentityCoeffHi),
		wr(RegCscUpper0+11, 0x00),
		wr(RegCscUpper0+12, 0x00),
		wr(RegCscUpper0+13, 0x00),
		wr(RegCscUpper0+14, 0x00),
		wr(RegCscUpper0+15, 0x00),
		wr(RegCscUpper0+16, 0x00),
		wr(RegCscUpper0+17, 0x00),
		wr(RegCscUpper0+18, 0x00),
		wr(RegCscUpper0+19, 0x00),
		wr(RegCscUpper0+20, CscIdentityCoeffHi),
		wr(RegCscUpper0+21, 0x00),
		wr(RegCscUpper0+22, 0x00),
		wr(RegCscUpper0+23, 0x00),
		wr(RegBksv0+0, bksv0),
		wr(RegBksv0+1, bksv1),
		wr(RegBksv0+2, bksv2),
		wr(RegBksv0+3, bksv3),
	}
}

// Menu720p60Mode returns the mode-commit register writes selecting manual 720p60 VIC 4.
func Menu720p60Mode() []RegisterWrite {
	return []RegisterWrite{
		wr(RegVideoInputCfg2, Sync720p16x9),
		wr(RegPixelRepetition, PixelRepeatManual),
		wr(RegVicManual, Vic720p60),
	}
}

// HDMIWake returns the register sequence that clears 0x96[2] and triggers an EDID read.
func HDMIWake() []RegisterWrite {
	return []RegisterWrite{
		wr(RegInt0, Int0EDIDReady),
		wr(RegEdidSegment, 0x00),
		wr(RegEdidReadCtrl, EdidReadEnable),
		wr(RegEdidReadCtrl, EdidReadTrigger),
		wr(RegEdidReadCtrl, EdidReadEnable),
	}
}

// ApplicationAudio48k returns the 48 kHz 16-bit I2S PCM + Audio InfoFrame writes.
func ApplicationAudio48k() []RegisterWrite {
	return []RegisterWrite{
		wr(RegAudioSource, 0x01),
		wr(RegAudioConfig, 0x2e),
		wr(RegI2sConfig, 0x84),
		wr(0x0e, 0x01),
		wr(0x12, 0x00),
		wr(0x13, 0x00),
		wr(RegAudioCfg3, AudioWordLength16),
		wr(RegI2cFreqIdCfg, I2cFreqId48kRgb444),
		wr(RegAudioN19_16, 0x00),
		wr(RegAudioN15_8, 0x18),
		wr(RegAudioN7_0, 0x00),
		wr(0x47, 0x00),
		wr(RegInfoframeUpdate, InfoframeAutoChecksum|AudioInfoframeUpdate),
		wr(RegAudioInfoframe, AudioInfoframeTwoChans),
		wr(0x74, 0x00), wr(0x75, 0x00), wr(0x76, 0x00),
		wr(0x77, 0x00), wr(0x78, 0x00), wr(0x79, 0x00),
		wr(0x7a, 0x00), wr(0x7b, 0x00), wr(0x7c, 0x00),
		wr(RegInfoframeUpdate, InfoframeAutoChecksum),
	}
}
