package protocol

import (
	"encoding/json"
	"testing"
)

func TestValidateCastMediaSetAcceptsSupportedVideoAndAudio(t *testing.T) {
	if err := ValidateCastMediaSet(CastMediaSet{Version: CastMediaSetVersion, Video: true, Audio: true}); err != nil {
		t.Fatalf("ValidateCastMediaSet: %v", err)
	}
}

func TestCastMediaSetUnmarshalRequiresExactBooleanShape(t *testing.T) {
	for name, raw := range map[string]string{
		"missing audio": `{ "version": 1, "video": true }`,
		"null audio":    `{ "version": 1, "video": true, "audio": null }`,
		"string audio":  `{ "version": 1, "video": true, "audio": "true" }`,
		"unknown":       `{ "version": 1, "video": true, "audio": false, "sink": "private" }`,
		"empty":         `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			var media CastMediaSet
			if err := json.Unmarshal([]byte(raw), &media); err == nil {
				t.Fatal("invalid media accepted")
			}
		})
	}
}

func TestValidateCastMediaAcknowledgementRequiresExactReadyCapabilities(t *testing.T) {
	requested := CastMediaSet{Version: CastMediaSetVersion, Video: true, Audio: true}
	valid := &CastStatusMedia{Version: requested.Version, Video: requested.Video, Audio: requested.Audio, Ready: true, Capabilities: CastMediaCapabilities{Version: CastMediaSetVersion, Video: true, Audio: true}}
	if err := ValidateCastMediaAcknowledgement(requested, valid); err != nil {
		t.Fatalf("valid acknowledgement: %v", err)
	}
	for name, status := range map[string]*CastStatusMedia{
		"nil":                   nil,
		"wrong audio":           {Version: 1, Video: true, Ready: true, Capabilities: CastMediaCapabilities{Version: 1, Video: true, Audio: true}},
		"not ready":             {Version: requested.Version, Video: requested.Video, Audio: requested.Audio, Ready: false, Capabilities: CastMediaCapabilities{Version: 1, Video: true, Audio: true}},
		"no capability":         {Version: requested.Version, Video: requested.Video, Audio: requested.Audio, Ready: true, Capabilities: CastMediaCapabilities{Version: 1, Video: true}},
		"video only capability": {Version: 1, Video: true, Audio: false, Ready: true, Capabilities: CastMediaCapabilities{Version: 1, Video: true, Audio: false}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateCastMediaAcknowledgement(requested, status); err == nil {
				t.Fatal("invalid acknowledgement accepted")
			}
		})
	}
}

func TestValidateCastMediaSetRejectsUnsupportedShapes(t *testing.T) {
	for name, media := range map[string]CastMediaSet{
		"version zero":   {Video: true},
		"future version": {Version: 2, Video: true},
		"video disabled": {Version: CastMediaSetVersion, Video: false, Audio: true},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateCastMediaSet(media); err == nil {
				t.Fatal("invalid media set accepted")
			}
		})
	}
}
