//go:build linux

package remotemedia

import (
	"bytes"
	"strings"
	"testing"
)

func TestV4L2AnnexBParserEmitsAccessUnitsOnAUD(t *testing.T) {
	stream := bytes.Join([][]byte{
		[]byte{0, 0, 0, 1, 0x09, 0xf0},
		[]byte{0, 0, 1, 0x67, 0x42, 0x01},
		[]byte{0, 0, 0, 1, 0x68, 0xce, 0x06},
		[]byte{0, 0, 1, 0x65, 0x88, 0x84},
		[]byte{0, 0, 0, 1, 0x09, 0xf0},
		[]byte{0, 0, 1, 0x41, 0x9a, 0x22},
	}, nil)
	parser := newV4L2AccessUnitParser(640, 480, FrameRate{Numerator: 30, Denominator: 1})
	var samples []EncodedSample
	for offset := 0; offset < len(stream); {
		end := offset + 3
		if end > len(stream) {
			end = len(stream)
		}
		samples = append(samples, parser.feed(stream[offset:end])...)
		offset = end
	}
	samples = append(samples, parser.flush()...)
	if len(samples) != 2 {
		t.Fatalf("samples = %d, want 2", len(samples))
	}
	firstNals, err := AVCCToNALs(samples[0].AVCC, samples[0].NALLengthSize)
	if err != nil {
		t.Fatalf("decode first AVCC: %v", err)
	}
	if !containsNALType(firstNals, 5) || !samples[0].Keyframe {
		t.Fatalf("first sample = %#v, nals=%v", samples[0], nalTypes(firstNals))
	}
	if string(samples[0].SPS) != string([]byte{0x67, 0x42, 0x01}) || string(samples[0].PPS) != string([]byte{0x68, 0xce, 0x06}) {
		t.Fatalf("parameter sets = %x/%x", samples[0].SPS, samples[0].PPS)
	}
	if samples[0].Width != 640 || samples[0].Height != 480 || samples[0].NALLengthSize != 4 {
		t.Fatalf("format = %dx%d nal=%d", samples[0].Width, samples[0].Height, samples[0].NALLengthSize)
	}
	secondNals, err := AVCCToNALs(samples[1].AVCC, samples[1].NALLengthSize)
	if err != nil {
		t.Fatalf("decode second AVCC: %v", err)
	}
	if samples[1].Keyframe || !containsNALType(secondNals, 1) {
		t.Fatalf("second sample = %#v, nals=%v", samples[1], nalTypes(secondNals))
	}
}

func TestV4L2CaptureRequiresAbsoluteDevicePath(t *testing.T) {
	_, err := OpenNativeCapture(CaptureConfig{Device: "ShadowCast 3"})
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("error = %v, want an absolute device path error", err)
	}
}

func nalTypes(nals [][]byte) []byte {
	types := make([]byte, 0, len(nals))
	for _, nal := range nals {
		if len(nal) > 0 {
			types = append(types, nal[0]&0x1f)
		}
	}
	return types
}
