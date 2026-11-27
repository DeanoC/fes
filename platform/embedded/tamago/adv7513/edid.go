// SPDX-License-Identifier: GPL-3.0-or-later

package adv7513

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidEDID indicates a malformed EDID header, block checksum, or CEA extension.
var ErrInvalidEDID = errors.New("adv7513: invalid EDID")

var edidMagicHeader = [8]byte{0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x00}

// DetailedTiming represents an 18-byte EDID detailed timing descriptor.
type DetailedTiming struct {
	PixelClockKHz uint32
	HActive       int
	HBlank        int
	VActive       int
	VBlank        int
	HSyncOffset   int
	HSyncWidth    int
	VSyncOffset   int
	VSyncWidth    int
	Interlaced    bool
}

// RefreshMHz returns the approximate vertical refresh rate in millihertz.
func (d DetailedTiming) RefreshMHz() uint32 {
	htotal := uint64(d.HActive + d.HBlank)
	vtotal := uint64(d.VActive + d.VBlank)
	if htotal == 0 || vtotal == 0 || d.PixelClockKHz == 0 {
		return 0
	}
	return uint32((uint64(d.PixelClockKHz) * 1_000_000) / (htotal * vtotal))
}

// ShortVideoDescriptor holds a CEA-861 VIC code and native flag.
type ShortVideoDescriptor struct {
	VIC    uint8
	Native bool
}

// ShortAudioDescriptor holds a CEA-861 audio format descriptor.
type ShortAudioDescriptor struct {
	Format      uint8
	MaxChannels uint8
	SampleRates uint8
	BitDepths   uint8
}

// EDID captures parsed base EDID 1.4 and optional CEA-861 extension fields.
type EDID struct {
	Manufacturer   string
	ProductCode    uint16
	Serial         uint32
	Week           uint8
	Year           uint16
	Version        uint8
	Revision       uint8
	DigitalInput   bool
	ExtensionCount uint8
	MonitorName    string
	Timings        []DetailedTiming
	IsHDMI         bool
	HDMIAddr       [4]uint8
	BasicAudio     bool
	YCbCr444       bool
	YCbCr422       bool
	VICs           []ShortVideoDescriptor
	Audio          []ShortAudioDescriptor
}

// PreferredTiming returns the first detailed timing descriptor if present.
func (e *EDID) PreferredTiming() (DetailedTiming, bool) {
	if e == nil || len(e.Timings) == 0 {
		return DetailedTiming{}, false
	}
	return e.Timings[0], true
}

// SupportsVIC reports whether the monitor advertises CEA-861 VIC vic.
func (e *EDID) SupportsVIC(vic uint8) bool {
	if e == nil || vic == 0 {
		return false
	}
	for _, svd := range e.VICs {
		if svd.VIC == vic {
			return true
		}
	}
	return false
}

// SupportsAudio48kLPCM reports whether BasicAudio or a 48 kHz LPCM descriptor is advertised.
func (e *EDID) SupportsAudio48kLPCM() bool {
	if e == nil {
		return false
	}
	if e.BasicAudio {
		return true
	}
	for _, a := range e.Audio {
		// CEA format 1 = LPCM; SampleRates bit 2 = 48 kHz.
		if a.Format == 1 && a.SampleRates&(1<<2) != 0 {
			return true
		}
	}
	return false
}

func blockChecksumOK(block []byte) bool {
	if len(block) != 128 {
		return false
	}
	var sum byte
	for _, b := range block {
		sum += b
	}
	return sum == 0
}

func decodeManufacturer(raw [2]byte) string {
	v := binary.BigEndian.Uint16(raw[:])
	chars := [3]byte{
		byte((v>>10)&0x1f) + 'A' - 1,
		byte((v>>5)&0x1f) + 'A' - 1,
		byte(v&0x1f) + 'A' - 1,
	}
	for i, c := range chars {
		if c < 'A' || c > 'Z' {
			chars[i] = '?'
		}
	}
	return string(chars[:])
}

func parseDTD(d []byte) DetailedTiming {
	pclk := uint32(binary.LittleEndian.Uint16(d[0:2])) * 10
	return DetailedTiming{
		PixelClockKHz: pclk,
		HActive:       int(d[2]) | (int(d[4]&0xf0) << 4),
		HBlank:        int(d[3]) | (int(d[4]&0x0f) << 8),
		VActive:       int(d[5]) | (int(d[7]&0xf0) << 4),
		VBlank:        int(d[6]) | (int(d[7]&0x0f) << 8),
		HSyncOffset:   int(d[8]) | (int((d[11]>>6)&0x03) << 8),
		HSyncWidth:    int(d[9]) | (int((d[11]>>4)&0x03) << 8),
		VSyncOffset:   int(d[10]>>4) | (int((d[11]>>2)&0x03) << 4),
		VSyncWidth:    int(d[10]&0x0f) | (int(d[11]&0x03) << 4),
		Interlaced:    d[17]&0x80 != 0,
	}
}

// ParseEDID validates and parses a 128-byte base EDID block and optional 128-byte CEA-861 extension.
func ParseEDID(raw []byte) (*EDID, error) {
	if len(raw) < 128 {
		return nil, fmt.Errorf("%w: length %d < 128", ErrInvalidEDID, len(raw))
	}
	base := raw[:128]
	if !bytes.Equal(base[:8], edidMagicHeader[:]) {
		return nil, fmt.Errorf("%w: bad magic header", ErrInvalidEDID)
	}
	if !blockChecksumOK(base) {
		return nil, fmt.Errorf("%w: base checksum mismatch", ErrInvalidEDID)
	}

	out := &EDID{
		Manufacturer:   decodeManufacturer([2]byte{base[8], base[9]}),
		ProductCode:    binary.LittleEndian.Uint16(base[10:12]),
		Serial:         binary.LittleEndian.Uint32(base[12:16]),
		Week:           base[16],
		Year:           1990 + uint16(base[17]),
		Version:        base[18],
		Revision:       base[19],
		DigitalInput:   base[20]&0x80 != 0,
		ExtensionCount: base[126],
	}

	for off := 54; off <= 108; off += 18 {
		d := base[off : off+18]
		if d[0] != 0 || d[1] != 0 {
			out.Timings = append(out.Timings, parseDTD(d))
		} else if d[3] == 0xfc && out.MonitorName == "" {
			out.MonitorName = strings.TrimRight(string(d[5:18]), "\n\r \x00")
		}
	}

	needLen := (1 + int(out.ExtensionCount)) * 128
	if len(raw) < needLen {
		return nil, fmt.Errorf("%w: truncated EDID (%d bytes for %d extensions)", ErrInvalidEDID, len(raw), out.ExtensionCount)
	}

	for blk := 1; blk <= int(out.ExtensionCount); blk++ {
		ext := raw[blk*128 : (blk+1)*128]
		if !blockChecksumOK(ext) {
			return nil, fmt.Errorf("%w: extension %d checksum mismatch", ErrInvalidEDID, blk)
		}
		if ext[0] == 0x02 { // CEA-861 extension
			dtdStart := int(ext[2])
			if dtdStart != 0 && (dtdStart < 4 || dtdStart > 127) {
				return nil, fmt.Errorf("%w: invalid CEA DTD offset %d", ErrInvalidEDID, dtdStart)
			}
			flags := ext[3]
			out.BasicAudio = out.BasicAudio || flags&0x40 != 0
			out.YCbCr444 = out.YCbCr444 || flags&0x20 != 0
			out.YCbCr422 = out.YCbCr422 || flags&0x10 != 0

			if dtdStart > 4 {
				for pos := 4; pos < dtdStart; {
					hdr := ext[pos]
					tag := hdr >> 5
					n := int(hdr & 0x1f)
					pos++
					if pos+n > dtdStart {
						return nil, fmt.Errorf("%w: truncated CEA data block", ErrInvalidEDID)
					}
					payload := ext[pos : pos+n]
					switch tag {
					case 1: // Audio Data Block
						for i := 0; i+3 <= len(payload); i += 3 {
							out.Audio = append(out.Audio, ShortAudioDescriptor{
								Format:      (payload[i] >> 3) & 0x0f,
								MaxChannels: (payload[i] & 0x07) + 1,
								SampleRates: payload[i+1],
								BitDepths:   payload[i+2],
							})
						}
					case 2: // Video Data Block
						for _, b := range payload {
							out.VICs = append(out.VICs, ShortVideoDescriptor{
								VIC:    b & 0x7f,
								Native: b&0x80 != 0,
							})
						}
					case 3: // Vendor-Specific Data Block (HDMI IEEE OUI 0x000C03)
						if len(payload) >= 5 && payload[0] == 0x03 && payload[1] == 0x0c && payload[2] == 0x00 {
							out.IsHDMI = true
							out.HDMIAddr = [4]uint8{
								(payload[3] >> 4) & 0x0f,
								payload[3] & 0x0f,
								(payload[4] >> 4) & 0x0f,
								payload[4] & 0x0f,
							}
						}
					}
					pos += n
				}
			}
			if dtdStart >= 4 {
				for pos := dtdStart; pos+18 <= 127; pos += 18 {
					d := ext[pos : pos+18]
					if d[0] == 0 && d[1] == 0 {
						break
					}
					out.Timings = append(out.Timings, parseDTD(d))
				}
			}
		}
	}

	return out, nil
}
