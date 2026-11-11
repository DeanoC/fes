package remotemedia

import (
	"errors"
	"fmt"
)

type EncodedAccessUnit struct {
	AVCC          []byte
	SPS           []byte
	PPS           []byte
	NALLengthSize int
	Keyframe      bool
}

func AVCCToNALs(avcc []byte, nalLengthSize int) ([][]byte, error) {
	if nalLengthSize != 1 && nalLengthSize != 2 && nalLengthSize != 4 {
		return nil, fmt.Errorf("unsupported AVCC NAL length size %d", nalLengthSize)
	}
	if len(avcc) == 0 {
		return nil, errors.New("AVCC block is empty")
	}
	nals := make([][]byte, 0, 4)
	for offset := 0; offset < len(avcc); {
		if len(avcc)-offset < nalLengthSize {
			return nil, errors.New("AVCC block has a truncated NAL length")
		}
		var length uint32
		for i := 0; i < nalLengthSize; i++ {
			length = length<<8 | uint32(avcc[offset+i])
		}
		offset += nalLengthSize
		if length == 0 {
			return nil, errors.New("AVCC block contains a zero-length NAL")
		}
		if uint64(length) > uint64(len(avcc)-offset) {
			return nil, errors.New("AVCC block has a truncated NAL payload")
		}
		nals = append(nals, append([]byte(nil), avcc[offset:offset+int(length)]...))
		offset += int(length)
	}
	return nals, nil
}

func (u EncodedAccessUnit) AnnexB() ([]byte, error) {
	nals, err := AVCCToNALs(u.AVCC, u.NALLengthSize)
	if err != nil {
		return nil, err
	}
	if u.Keyframe {
		nals = parameterSetsForKeyframe(nals, u.SPS, u.PPS)
		if !containsNALType(nals, 7) || !containsNALType(nals, 8) {
			return nil, errors.New("keyframe access unit is missing SPS or PPS")
		}
	}
	var out []byte
	for _, nal := range nals {
		if len(nal) == 0 {
			return nil, errors.New("encoded access unit contains an empty NAL")
		}
		out = append(out, 0, 0, 0, 1)
		out = append(out, nal...)
	}
	return out, nil
}

func parameterSetsForKeyframe(nals [][]byte, sps, pps []byte) [][]byte {
	result := make([][]byte, 0, len(nals)+2)
	if !containsNALType(nals, 7) && len(sps) > 0 {
		result = append(result, append([]byte(nil), sps...))
	}
	if !containsNALType(nals, 8) && len(pps) > 0 {
		result = append(result, append([]byte(nil), pps...))
	}
	return append(result, nals...)
}

func containsNALType(nals [][]byte, nalType byte) bool {
	for _, nal := range nals {
		if len(nal) > 0 && nal[0]&0x1f == nalType {
			return true
		}
	}
	return false
}
