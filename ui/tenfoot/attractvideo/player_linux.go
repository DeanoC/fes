//go:build linux

package attractvideo

// Available reports that Linux can decode attract video when ffmpeg is on PATH.
func Available() bool { return ffmpegAvailable() }

// Open starts an ffmpeg CLI decoder against a local media file.
func Open(path string) (Player, error) {
	return openFFmpeg(path)
}
