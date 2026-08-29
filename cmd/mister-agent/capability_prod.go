//go:build !fpgadev

package main

import (
	"github.com/DeanoC/FogCast-POC/internal/agentconfig"
	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
)

// Production builds deliberately omit the development capability.
const (
	fpgadevCapability      = false
	expectedFPGACapability = false
)

func newNormalGate(agentconfig.Config, ...string) hardwareowner.NormalGate { return nil }
