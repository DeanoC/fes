package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const CastMediaSetVersion uint32 = 1

// CastMediaSet is the versioned, public admission request for a cast session.
// It intentionally carries only user-visible media selection, never target
// implementation details.
type CastMediaSet struct {
	Version uint32 `json:"version"`
	Video   bool   `json:"video"`
	Audio   bool   `json:"audio"`
}

func (media *CastMediaSet) UnmarshalJSON(data []byte) error {
	if media == nil || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return fmt.Errorf("cast media must be an object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode cast media: %w", err)
	}
	if len(fields) != 3 {
		return fmt.Errorf("cast media fields are invalid")
	}
	for _, name := range []string{"version", "video", "audio"} {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("cast media %s is required", name)
		}
	}
	var decoded struct {
		Version uint32 `json:"version"`
		Video   bool   `json:"video"`
		Audio   bool   `json:"audio"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("decode cast media: %w", err)
	}
	*media = CastMediaSet(decoded)
	return ValidateCastMediaSet(*media)
}

func ValidateCastMediaSet(media CastMediaSet) error {
	if media.Version != CastMediaSetVersion {
		return fmt.Errorf("unsupported cast media version %d", media.Version)
	}
	if !media.Video {
		return fmt.Errorf("cast media requires video")
	}
	return nil
}

// CastMediaCapabilities is the target's public capability projection.
type CastMediaCapabilities struct {
	Version uint32 `json:"version"`
	Video   bool   `json:"video"`
	Audio   bool   `json:"audio"`
}

// CastStatusMedia echoes an acknowledged media set and its readiness/capability
// projection. A nil status media object represents a legacy cast session.
type CastStatusMedia struct {
	Version      uint32                `json:"version"`
	Video        bool                  `json:"video"`
	Audio        bool                  `json:"audio"`
	Ready        bool                  `json:"ready"`
	Capabilities CastMediaCapabilities `json:"capabilities"`
}

// ValidateCastMediaAcknowledgement verifies that the target acknowledged the
// exact requested public set and is ready with the required capability.
func ValidateCastMediaAcknowledgement(requested CastMediaSet, acknowledged *CastStatusMedia) error {
	if err := ValidateCastMediaSet(requested); err != nil {
		return err
	}
	if acknowledged == nil || !acknowledged.Ready || acknowledged.Version != requested.Version || acknowledged.Video != requested.Video || acknowledged.Audio != requested.Audio {
		return fmt.Errorf("cast media acknowledgement is invalid")
	}
	capabilities := acknowledged.Capabilities
	if capabilities.Version != CastMediaSetVersion || !capabilities.Video || !capabilities.Audio {
		return fmt.Errorf("cast media capabilities are insufficient")
	}
	return nil
}
