//go:build !fpgadev

package main

import (
	"flag"
	"io"
	"testing"
)

func TestUntaggedAgentDoesNotRegisterPrivateStartupFlag(t *testing.T) {
	flags := flag.NewFlagSet("mister-agent", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	if registerStartupFlag(flags) != nil {
		t.Fatal("untagged agent registered a private startup flag")
	}
	if err := flags.Parse([]string{"--readiness-fd", "3"}); err == nil {
		t.Fatal("untagged agent accepted the private startup flag")
	}
}
