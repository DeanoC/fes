package corepackage

import (
	"errors"
	"regexp"
)

// InitialMediaBytes is the legacy ST drive-A image size.
const InitialMediaBytes = 737280

var initialMediaGameID = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// InitialMedia transports the immutable library base disk with a ROM launch.
// The runtime restores any durable revision before releasing the new CPU.
// It carries no generation: the runtime assigns the new launch generation.
type InitialMedia struct {
	GameID      string
	BaseMediaID string
	Unit        uint8
	Bytes       []byte
}

// StagedInitialMedia names a sealed source disk retained with the ROM input.
// It is private staging metadata, not a caller-selected runtime storage path.
type StagedInitialMedia struct {
	GameID      string
	BaseMediaID string
	Unit        uint8
	Path        string
	Size        int64
}

type initialMediaReceipt struct {
	GameID      string `json:"game_id"`
	BaseMediaID string `json:"base_media_id"`
	Unit        uint8  `json:"unit"`
	Size        int64  `json:"size"`
}

func inspectInitialMedia(d Descriptor, media *InitialMedia) (*initialMediaReceipt, error) {
	if media == nil {
		return nil, nil
	}
	if len(media.GameID) > 256 || !initialMediaGameID.MatchString(media.GameID) || media.Unit != 0 || !hex64RE.MatchString(media.BaseMediaID) || !ValidAtariStBase(media.Bytes, DeclaresAtariStGeometry(d)) || romDigest(media.Bytes) != media.BaseMediaID {
		return nil, errors.New("initial disk requires exact immutable ST base bytes and library identity")
	}
	if d.Format != 3 || d.ROM == nil || d.ROM.Role != "firmware" || d.Core.ID != "fes.atari-st" || d.ABI != (Contract{ID: "fes.computer", Major: 1}) {
		return nil, errors.New("initial disk requires a format-3 ST computer firmware package")
	}
	base, write := false, false
	for _, i := range d.Interfaces {
		if i.Required && i.Major == 1 && i.Minor == 0 {
			switch i.ID {
			case "fes.media.atari-st-floppy":
				base = true
			case "fes.media.atari-st-floppy-write":
				write = true
			}
		}
	}
	if !base || !write {
		return nil, errors.New("initial library disk requires the writable ST floppy contract")
	}
	return &initialMediaReceipt{GameID: media.GameID, BaseMediaID: media.BaseMediaID, Unit: media.Unit, Size: int64(len(media.Bytes))}, nil
}

func stagedInitialMedia(media *InitialMedia, path string) *StagedInitialMedia {
	if media == nil {
		return nil
	}
	return &StagedInitialMedia{GameID: media.GameID, BaseMediaID: media.BaseMediaID, Unit: media.Unit, Path: path, Size: int64(len(media.Bytes))}
}
