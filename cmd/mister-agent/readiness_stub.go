//go:build !fpgadev

package main

import (
	"context"
	"flag"

	"github.com/DeanoC/FogCast-POC/internal/agentconfig"
)

func registerStartupFlag(*flag.FlagSet) *int { return nil }

func startupFDValue(*int) int { return -1 }

func validateStartupFD(int) error { return nil }

func announceStartup(context.Context, string, agentconfig.Config, int) error { return nil }
