package tenfoot

import (
	"context"
	"image"
	"os"

	"github.com/DeanoC/FogCast/ui/tenfoot/attractvideo"
)

// attractPlayer is a pull-based attract clip. Frame must not block for long.
type attractPlayer interface {
	Frame() (img *image.RGBA, ended bool, err error)
	Close()
}

type fileAttractPlayer struct {
	inner attractPlayer
	path  string
	keep  bool
}

func (p *fileAttractPlayer) Frame() (*image.RGBA, bool, error) {
	return p.inner.Frame()
}

func (p *fileAttractPlayer) Close() {
	if p.inner != nil {
		p.inner.Close()
		p.inner = nil
	}
	if !p.keep && p.path != "" {
		_ = os.Remove(p.path)
		p.path = ""
	}
}

func defaultOpenAttractVideo(ctx context.Context, client *Client, handle string) (attractPlayer, error) {
	return openFetchedAttractVideo(ctx, client, handle, attractvideo.Available(), attractvideo.Open)
}

func openFetchedAttractVideo(ctx context.Context, client *Client, handle string, available bool, open func(string) (attractvideo.Player, error)) (attractPlayer, error) {
	if !available {
		return nil, errAttractVideoUnavailable
	}
	if client == nil {
		return nil, errAttractVideoUnavailable
	}
	path, err := client.FetchVideoFile(ctx, handle)
	if err != nil {
		return nil, err
	}
	inner, err := open(path)
	if err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return &fileAttractPlayer{inner: inner, path: path}, nil
}

func openAttractVideoPath(path string) (attractPlayer, error) {
	if path == "" {
		return nil, errAttractVideoUnavailable
	}
	inner, err := attractvideo.Open(path)
	if err != nil {
		return nil, err
	}
	return &fileAttractPlayer{inner: inner, path: path, keep: true}, nil
}

func takeAttractVideoFile(p attractPlayer) string {
	f, ok := p.(*fileAttractPlayer)
	if !ok {
		return ""
	}
	f.keep = true
	return f.path
}

var errAttractVideoUnavailable = attractvideo.ErrUnavailable
