//go:build !linux || !arm || !fpgadev

package main

import (
	"errors"

	"github.com/DeanoC/FogCast-POC/internal/fpgadev"
)

type supervisorDependencyOptions struct {
	ConfigPath         string
	MainExecutable     string
	MainArguments      []string
	MainEnvironment    []string
	MainProcessScanner fpgadev.ProcessScanner
	ManagerState       string
	AgentExecutable    string
	AgentEnvironment   []string
	ProcRoot           string
	Reset              any
}

// Non-Linux, non-ARM, and untagged builds have no target constructors
// (!linux || !arm || !fpgadev). Linux/amd64
// tagged builds use the guarded productionSupervisorDependencies in
// deps_linux.go, which returns this same zero composition before any runtime
// or reboot adapter can be reached.
func productionSupervisorDependencies(int) fpgadev.Dependencies { return fpgadev.Dependencies{} }

func newSupervisorDependencies(supervisorDependencyOptions, int) (fpgadev.Dependencies, error) {
	return fpgadev.Dependencies{}, errors.New("target supervisor dependencies are ARM-only")
}
