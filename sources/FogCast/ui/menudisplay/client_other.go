//go:build !linux

package menudisplay

import (
	"context"
	"errors"
)

const (
	Width      = 1280
	Height     = 720
	Stride     = Width * 4
	FrameBytes = Stride * Height
	SlotBytes  = 4 << 20
)

type Client struct{ Path string }
type Status struct {
	Available                                   bool
	Generation                                  uint64
	Width, Height, Stride, ByteCount, SlotBytes int
	DisplayedSequence, Underflows               uint64
}
type Result struct{ Generation, DisplayedSequence, Underflows uint64 }

func New(path string) *Client { return &Client{Path: path} }
func (*Client) Status(context.Context) (Status, error) {
	return Status{}, errors.New("native menu requires Linux")
}
func (*Client) Present(context.Context, uint64, []byte) (Result, error) {
	return Result{}, errors.New("native menu requires Linux")
}
