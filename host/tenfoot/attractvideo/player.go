// Package attractvideo decodes attract-mode clips for the native tenfoot UI.
//
// Darwin uses AVFoundation (AVAssetReader) to pull RGBA frames. Other
// platforms return an error so attract can fall back to stills without
// downloading video bytes.
package attractvideo

import (
	"errors"
	"image"
)

// ErrUnavailable is returned when this platform cannot decode attract video.
var ErrUnavailable = errors.New("attract video decode is unavailable on this platform")

// Player is a pull-based video decoder. Frame must not block for long.
// A nil image means keep the previous frame; do not treat it as a new upload.
type Player interface {
	Frame() (img *image.RGBA, ended bool, err error)
	Close()
}
