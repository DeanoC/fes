//go:build !linux

package controller

import (
	"errors"

	"github.com/DeanoC/FogCast/host/tenfoot/inputmap"
	"github.com/DeanoC/FogCast/remoteinput"
)

type Device struct {
	Name  string
	Path  string
	remap *inputmap.Remapper
}

func (*Device) Close() error { return nil }
func (*Device) Poll() ([]remoteinput.Event, error) {
	return nil, errors.New("kit controller requires Linux")
}
func (d *Device) Info() (string, string) {
	if d == nil {
		return "", ""
	}
	return d.Path, d.Name
}
