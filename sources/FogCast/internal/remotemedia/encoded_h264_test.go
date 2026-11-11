package remotemedia

import (
	"testing"
)

func TestAVCCConversionIncludesParameterSetsBeforeIDR(t *testing.T) {
	avcc := []byte{0, 0, 0, 2, 0x65, 0xaa, 0, 0, 0, 2, 0x06, 0xbb}
	unit := EncodedAccessUnit{
		AVCC:          avcc,
		SPS:           []byte{0x67, 0x42},
		PPS:           []byte{0x68, 0xce},
		NALLengthSize: 4,
		Keyframe:      true,
	}
	annexB, err := unit.AnnexB()
	if err != nil {
		t.Fatalf("convert AVCC: %v", err)
	}
	want := []byte{0, 0, 0, 1, 0x67, 0x42, 0, 0, 0, 1, 0x68, 0xce, 0, 0, 0, 1, 0x65, 0xaa, 0, 0, 0, 1, 0x06, 0xbb}
	if string(annexB) != string(want) {
		t.Fatalf("Annex-B = %x, want %x", annexB, want)
	}
}

func TestAVCCConversionRejectsTruncatedNAL(t *testing.T) {
	if _, err := AVCCToNALs([]byte{0, 0, 0, 4, 0x65}, 4); err == nil {
		t.Fatal("truncated AVCC NAL accepted")
	}
	if _, err := AVCCToNALs([]byte{0, 0, 0, 1, 0x65}, 3); err == nil {
		t.Fatal("unsupported AVCC length size accepted")
	}
}

func TestAVCCConversionRejectsKeyframeWithoutParameterSets(t *testing.T) {
	unit := EncodedAccessUnit{
		AVCC:          []byte{0, 0, 0, 1, 0x65},
		NALLengthSize: 4,
		Keyframe:      true,
	}
	if _, err := unit.AnnexB(); err == nil {
		t.Fatal("keyframe without SPS/PPS was accepted")
	}
}
