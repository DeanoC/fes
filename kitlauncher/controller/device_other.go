//go:build !linux

package controller

import (
	"errors"
	"github.com/DeanoC/FogCast/remoteinput"
)

type Device struct{ Name string }

func Open() (*Device, error) { return nil, errors.New("kit controller requires Linux") }
func (*Device) Close() error { return nil }
func (*Device) Poll() ([]remoteinput.Event, error) {
	return nil, errors.New("kit controller requires Linux")
}
