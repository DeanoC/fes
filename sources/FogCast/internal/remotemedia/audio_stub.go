//go:build !darwin || !cgo

package remotemedia

import "errors"

// ErrNativeAudioUnavailable is actionable without exposing any endpoint or
// display identity. TCC grants are per signed FogCast bundle, not transferable
// from Genki or another capture application.
var ErrNativeAudioUnavailable = errors.New("native FogCast audio capture requires a cgo-enabled macOS FogCast helper with Microphone or Screen Recording permission")

func OpenNativeAudioSource(config AudioSourceConfig) (AudioSource, error) {
	if err := validateNativeAudioSourceConfig(config); err != nil {
		return nil, err
	}
	return nil, ErrNativeAudioUnavailable
}
