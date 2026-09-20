//go:build !linux

package main

import "errors"

type nativeFramebuffer struct{}

func openNativeFramebuffer(string) (*nativeFramebuffer, error) {
	return nil, errors.New("framebuffer unavailable")
}
func (*nativeFramebuffer) Width() int         { return 0 }
func (*nativeFramebuffer) Height() int        { return 0 }
func (*nativeFramebuffer) Stride() int        { return 0 }
func (*nativeFramebuffer) Write([]byte) error { return errors.New("framebuffer unavailable") }
func (*nativeFramebuffer) Close() error       { return nil }
