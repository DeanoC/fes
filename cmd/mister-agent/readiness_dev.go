//go:build fpgadev

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"

	"github.com/DeanoC/FogCast-POC/internal/agentconfig"
	"github.com/DeanoC/FogCast-POC/internal/fpgadev"
	"golang.org/x/sys/unix"
)

func registerStartupFlag(flags *flag.FlagSet) *int {
	if flags == nil {
		return nil
	}
	return flags.Int("readiness-fd", -1, "write the development startup receipt to inherited fd 3")
}

func startupFDValue(value *int) int {
	if value == nil {
		return -1
	}
	return *value
}

func validateStartupFD(fd int) error {
	if fd != 3 {
		return fmt.Errorf("startup receipt fd must be 3")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("startup receipt fd is unavailable: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFIFO {
		return errors.New("startup receipt fd is not a pipe")
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return fmt.Errorf("startup receipt fd flags are unavailable: %w", err)
	}
	if flags&unix.O_ACCMODE != unix.O_WRONLY {
		return errors.New("startup receipt fd is not write-only")
	}
	return nil
}

// announceStartup is called after the profile-specific controllers/routes and
// HTTP server have been composed, immediately before serving. It reloads the
// protected profile through its metadata-bound descriptor so the receipt
// cannot attest a different configuration from the one used to compose the
// process. The descriptor is one-use and is always closed by this function.
func announceStartup(ctx context.Context, configPath string, initial agentconfig.Config, fd int) error {
	if err := validateStartupFD(fd); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	current, profileHash, err := fpgadev.LoadProtectedAgentConfig(configPath)
	if err != nil {
		return err
	}
	if !current.IsDevelopmentProfile() {
		return errors.New("startup receipt requires the development profile")
	}
	if err := current.ValidateProfile(true); err != nil {
		return err
	}
	if !reflect.DeepEqual(initial, current) {
		return errors.New("protected profile changed before startup receipt")
	}
	identity, err := fpgadev.CurrentProcessAttestation()
	if err != nil {
		return err
	}
	if identity.PID != uint64(os.Getpid()) || identity.StartTime == 0 || identity.Device == 0 || identity.Inode == 0 || identity.SHA256 == "" {
		return errors.New("current process identity is incomplete")
	}
	receipt := fpgadev.ReadinessReceipt{
		Schema:           1,
		PID:              identity.PID,
		StartTime:        identity.StartTime,
		ExecutableDevice: identity.Device,
		ExecutableInode:  identity.Inode,
		ExecutableSHA256: identity.SHA256,
		ProfileSHA256:    profileHash,
		Capabilities:     fpgadev.DevelopmentCapabilities(),
	}
	payload, err := receipt.MarshalCanonical()
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "startup-receipt")
	if file == nil {
		return errors.New("startup receipt fd is unavailable")
	}
	if err := validateStartupFD(fd); err != nil {
		_ = file.Close()
		return err
	}
	writeErr := writeStartupReceipt(file, payload)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return ctx.Err()
}

func writeStartupReceipt(file *os.File, payload []byte) error {
	if file == nil {
		return errors.New("startup receipt writer is nil")
	}
	for len(payload) != 0 {
		written, err := file.Write(payload)
		if written > 0 {
			payload = payload[written:]
		}
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
