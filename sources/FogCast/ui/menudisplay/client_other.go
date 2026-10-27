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
	Slots      = 2
	Format     = "bgrx8888"
)

type Client struct{ Path string }
type Status struct {
	Available                                   bool
	Session                                     bool
	PackageID                                   string
	CoreGeneration                              uint64
	Generation                                  uint64
	Width, Height, Stride, ByteCount, SlotBytes int
	Slots                                       int
	Format                                      string
	DisplayedSequence, Underflows               uint64
	Picture                                     []int
}
type Result struct {
	Generation, DisplayedSequence, Underflows uint64
	Timing                                    Timing
}

func New(path string) *Client { return &Client{Path: path} }
func (*Client) Status(context.Context) (Status, error) {
	return Status{}, errors.New("native menu requires Linux")
}
func (*Client) Present(context.Context, uint64, []byte) (Result, error) {
	return Result{}, errors.New("native menu requires Linux")
}
func (*Client) PresentDamage(context.Context, uint64, []byte, Damage) (Result, error) {
	return Result{}, errors.New("native menu requires Linux")
}
func (*Client) Grant(context.Context, uint64) (Grant, error) {
	return Grant{}, errors.New("native menu requires Linux")
}
func (*Client) PresentBlit(context.Context, uint64, BlitFrame) (Result, error) {
	return Result{}, errors.New("native menu requires Linux")
}
func (*Client) Close() error { return nil }
