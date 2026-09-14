//go:build !linux

package controller

import (
	"errors"

	"github.com/DeanoC/FogCast/ui/tenfoot/inputmap"
)

func Open() (*Hub, error) {
	return nil, errors.New("kit controller requires Linux")
}

func OpenWith(*inputmap.Remapper) (*Hub, error) {
	return nil, errors.New("kit controller requires Linux")
}
