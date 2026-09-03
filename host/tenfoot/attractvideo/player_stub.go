//go:build !darwin || !cgo

package attractvideo

// Available reports that this platform cannot decode attract video.
func Available() bool { return false }

// Open reports that this platform has no attract video decoder.
func Open(string) (Player, error) {
	return nil, ErrUnavailable
}
