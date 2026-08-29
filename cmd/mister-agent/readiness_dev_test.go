//go:build fpgadev

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/agentconfig"
	"github.com/DeanoC/FogCast-POC/internal/fpgadev"
)

const readinessHelperEnv = "FOGCAST_TAGGED_AGENT_READINESS_HELPER"

func TestTaggedAgentRegistersReadinessFlagAndRejectsMalformedArguments(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "negative fd", args: []string{"--readiness-fd", "-2"}},
		{name: "standard fd", args: []string{"--readiness-fd", "2"}},
		{name: "non numeric fd", args: []string{"--readiness-fd", "not-a-number"}},
		{name: "unexpected positional argument", args: []string{"--readiness-fd", "3", "extra"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			flags := flag.NewFlagSet("mister-agent", flag.ContinueOnError)
			flags.SetOutput(io.Discard)
			configPath := flags.String("config", "", "config")
			readinessFD := registerStartupFlag(flags)
			if err := flags.Parse(test.args); err != nil {
				if test.name == "unexpected positional argument" {
					t.Fatalf("flag parsing failed before positional-argument validation: %v", err)
				}
				return
			}
			if test.name == "unexpected positional argument" {
				if len(flags.Args()) == 0 {
					t.Fatal("unexpected positional argument was discarded")
				}
				return
			}
			if err := validateStartupFD(startupFDValue(readinessFD)); err == nil {
				t.Fatalf("malformed startup fd accepted: config=%q fd=%d", *configPath, startupFDValue(readinessFD))
			}
		})
	}
}

func TestTaggedAgentReadinessSubprocessEmitsCanonicalEOFReceipt(t *testing.T) {
	if os.Getenv(readinessHelperEnv) == "1" {
		t.Skip("helper mode is exercised by the parent subprocess")
	}
	configPath := writeDevelopmentCompositionConfig(t)
	initial, err := agentconfig.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdinReader, stdinWriter, err := os.Pipe()
	if err != nil {
		_ = readPipe.Close()
		_ = writePipe.Close()
		t.Fatal(err)
	}
	defer readPipe.Close()
	defer stdinWriter.Close()
	defer stdinReader.Close()

	cmd := exec.Command(os.Args[0], "-test.run", "^TestTaggedAgentReadinessSubprocessHelper$")
	cmd.Env = append(os.Environ(), readinessHelperEnv+"=1", "FOGCAST_AGENT_CONFIG="+configPath)
	cmd.ExtraFiles = []*os.File{writePipe}
	cmd.Stdin = stdinReader
	if err := cmd.Start(); err != nil {
		_ = writePipe.Close()
		t.Fatal(err)
	}
	_ = writePipe.Close()

	raw, readErr := io.ReadAll(readPipe)
	if readErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("readiness pipe read: %v", readErr)
	}
	receipt, err := fpgadev.ParseReadinessReceipt(raw)
	if err != nil {
		_ = stdinWriter.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("canonical receipt parse: %v; raw=%q", err, raw)
	}
	canonical, err := receipt.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, canonical) {
		t.Fatalf("receipt was not canonical: got=%q want=%q", raw, canonical)
	}
	if receipt.PID != uint64(cmd.Process.Pid) {
		t.Fatalf("receipt pid=%d, child pid=%d", receipt.PID, cmd.Process.Pid)
	}
	exePath, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", cmd.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	exeInfo, err := os.Stat(exePath)
	if err != nil {
		t.Fatal(err)
	}
	exeDevice, ok := syscallStatDevice(exeInfo)
	if !ok {
		t.Fatal("child executable device is unavailable")
	}
	if receipt.ExecutableDevice != exeDevice || receipt.ExecutableInode != uint64(exeInfo.Sys().(*syscallStat).Ino) {
		t.Fatalf("receipt executable identity=(%d,%d), actual=(%d,%d)", receipt.ExecutableDevice, receipt.ExecutableInode, exeDevice, uint64(exeInfo.Sys().(*syscallStat).Ino))
	}
	exeBytes, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatal(err)
	}
	exeHash := sha256.Sum256(exeBytes)
	if receipt.ExecutableSHA256 != hex.EncodeToString(exeHash[:]) {
		t.Fatalf("receipt executable hash=%q, actual=%q", receipt.ExecutableSHA256, hex.EncodeToString(exeHash[:]))
	}
	startTime, err := childStartTime(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.StartTime != startTime {
		t.Fatalf("receipt start time=%d, actual=%d", receipt.StartTime, startTime)
	}
	profileBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	profileHash := sha256.Sum256(profileBytes)
	if receipt.ProfileSHA256 != hex.EncodeToString(profileHash[:]) {
		t.Fatalf("receipt profile hash=%q, actual=%q", receipt.ProfileSHA256, hex.EncodeToString(profileHash[:]))
	}
	if got := receipt.Capabilities; !equalStrings(got, fpgadev.DevelopmentCapabilities()) {
		t.Fatalf("receipt capabilities=%#v, want=%#v", got, fpgadev.DevelopmentCapabilities())
	}
	if err := stdinWriter.Close(); err != nil {
		t.Fatal(err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("readiness helper exit: %v", err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("readiness helper did not exit after stdin EOF")
	}
	_ = initial
}

func TestTaggedAgentReadinessRejectsProtectedProfileSwap(t *testing.T) {
	configPath := writeDevelopmentCompositionConfig(t)
	initial, err := agentconfig.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), `token = "test-token"`, `token = "swapped-token"`, 1))
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readPipe.Close()
	if err := announceStartup(context.Background(), configPath, initial, int(writePipe.Fd())); err == nil {
		t.Fatal("protected profile swap was accepted")
	}
	_ = writePipe.Close()
	if raw, err := io.ReadAll(readPipe); err != nil {
		t.Fatal(err)
	} else if len(raw) != 0 {
		t.Fatalf("profile swap emitted receipt bytes: %q", raw)
	}
}

func TestTaggedAgentReadinessRejectsInvalidDescriptorAndClosesOnWriteFailure(t *testing.T) {
	configPath := writeDevelopmentCompositionConfig(t)
	initial, err := agentconfig.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := announceStartup(context.Background(), configPath, initial, 2); err == nil {
		t.Fatal("standard error descriptor was accepted")
	}
	if err := announceStartup(context.Background(), configPath, initial, 1<<21); err == nil {
		t.Fatal("out-of-range descriptor was accepted")
	}
}

func TestTaggedAgentReadinessSubprocessHelper(t *testing.T) {
	if os.Getenv(readinessHelperEnv) != "1" {
		return
	}
	configPath := os.Getenv("FOGCAST_AGENT_CONFIG")
	initial, err := agentconfig.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := announceStartup(context.Background(), configPath, initial, 3); err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(os.Stdin)
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func syscallStatDevice(info os.FileInfo) (uint64, bool) {
	stat, ok := info.Sys().(*syscallStat)
	if !ok {
		return 0, false
	}
	return uint64(stat.Dev), true
}

func childStartTime(pid int) (uint64, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	closeParen := strings.LastIndexByte(string(raw), ')')
	if closeParen < 0 {
		return 0, errors.New("child stat has no closing comm delimiter")
	}
	fields := strings.Fields(string(raw)[closeParen+1:])
	if len(fields) <= 19 {
		return 0, errors.New("child stat has no start-time field")
	}
	return strconv.ParseUint(fields[19], 10, 64)
}

// syscallStat keeps the fixture independent of the unexported Linux stat
// aliases used by fpgadev while retaining exact device/inode checks.
type syscallStat = syscall.Stat_t
