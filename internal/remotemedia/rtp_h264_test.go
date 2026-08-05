package remotemedia

import (
	"encoding/binary"
	"testing"
)

func TestPacketizerUsesSingleNALAndMarkerOnAccessUnitEnd(t *testing.T) {
	p := NewRTPPacketizer(1200, 0x11223344, 0x1000)
	packets, err := p.Packetize(AccessUnit{
		NALs:      [][]byte{{0x67, 0x01, 0x02}, {0x65, 0xaa}},
		Timestamp: 9000,
		Keyframe:  true,
	})
	if err != nil {
		t.Fatalf("packetize: %v", err)
	}
	if len(packets) != 2 {
		t.Fatalf("packet count = %d, want 2", len(packets))
	}
	if packets[0].Marker || !packets[1].Marker {
		t.Fatalf("marker bits = %v, %v; want false, true", packets[0].Marker, packets[1].Marker)
	}
	if packets[0].Sequence != 0x1000 || packets[1].Sequence != 0x1001 {
		t.Fatalf("sequences = %04x, %04x", packets[0].Sequence, packets[1].Sequence)
	}
	if !packets[0].Keyframe || !packets[1].Keyframe {
		t.Fatal("keyframe signal was not carried on every packet")
	}
	wire := packets[1].Marshal()
	if len(wire) != 12+2 {
		t.Fatalf("wire length = %d, want 14", len(wire))
	}
	if wire[0] != 0x80 || wire[1] != 0xe0 || binary.BigEndian.Uint32(wire[4:8]) != 9000 {
		t.Fatalf("unexpected RTP header: %x", wire[:12])
	}
}

func TestPacketizerFragmentsNALWithinMTUAndOnlyFinalPacketHasMarker(t *testing.T) {
	p := NewRTPPacketizer(20, 7, 4)
	nal := make([]byte, 40)
	nal[0] = 0x41
	for i := 1; i < len(nal); i++ {
		nal[i] = byte(i)
	}
	packets, err := p.Packetize(AccessUnit{NALs: [][]byte{nal}, Timestamp: 12})
	if err != nil {
		t.Fatalf("packetize: %v", err)
	}
	if len(packets) < 3 {
		t.Fatalf("fragment count = %d, want at least 3", len(packets))
	}
	for i, packet := range packets {
		if len(packet.Marshal()) > 20 {
			t.Fatalf("packet %d exceeds MTU: %d", i, len(packet.Marshal()))
		}
		if packet.Marker != (i == len(packets)-1) {
			t.Fatalf("packet %d marker = %v", i, packet.Marker)
		}
		if len(packet.Payload) < 2 || packet.Payload[0]&0x1f != 28 {
			t.Fatalf("packet %d is not FU-A: %x", i, packet.Payload)
		}
		if packet.Payload[1]&0x80 != (func() byte {
			if i == 0 {
				return 0x80
			}
			return 0
		}()) {
			t.Fatalf("packet %d start bit = %#x", i, packet.Payload[1]&0x80)
		}
	}
	if packets[len(packets)-1].Payload[1]&0x40 == 0 {
		t.Fatal("final FU-A packet does not have end bit")
	}
}

func TestAnnexBParserRejectsMalformedAndExtractsNALs(t *testing.T) {
	nals, err := ParseAnnexBNALs([]byte{0, 0, 0, 1, 0x67, 1, 0, 0, 1, 0x65, 2, 3})
	if err != nil {
		t.Fatalf("parse Annex-B: %v", err)
	}
	if len(nals) != 2 || nals[0][0] != 0x67 || nals[1][0] != 0x65 {
		t.Fatalf("NALs = %#v", nals)
	}
	for _, input := range [][]byte{
		{}, {0, 0, 1}, {0x67, 1, 2}, {0, 0, 1, 0, 0, 1}, {1, 0, 0, 1, 0x65},
	} {
		if _, err := ParseAnnexBNALs(input); err == nil {
			t.Fatalf("malformed Annex-B accepted: %x", input)
		}
	}
}

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

func TestPacketizerPreservesNALHeaderReferenceBitsInFUA(t *testing.T) {
	p := NewRTPPacketizer(20, 7, 4)
	nal := make([]byte, 40)
	nal[0] = 0xA1
	packets, err := p.Packetize(AccessUnit{NALs: [][]byte{nal}})
	if err != nil {
		t.Fatalf("packetize: %v", err)
	}
	if packets[0].Payload[0] != 0xBC || packets[0].Payload[1]&0x1f != 1 {
		t.Fatalf("FU-A headers = %#x %#x", packets[0].Payload[0], packets[0].Payload[1])
	}
}
