//go:build !linux

package main

import (
	"os"
	"testing"
)

func settleLinuxCtime(*testing.T, string, os.FileInfo) {}
