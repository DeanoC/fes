package kitsoftware

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type nativeStatusError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Phase   string `json:"phase"`
}
type nativeStatus struct {
	Protocol      int                `json:"protocol"`
	OK            bool               `json:"ok"`
	State         string             `json:"state"`
	Execution     string             `json:"execution"`
	Version       string             `json:"version"`
	Error         *nativeStatusError `json:"error"`
	System        *string            `json:"system,omitempty"`
	Core          *string            `json:"core,omitempty"`
	ActivePackage json.RawMessage    `json:"active_package,omitempty"`
	Generation    *uint64            `json:"generation,omitempty"`
	Capabilities  struct {
		ActiveInterfaces []json.RawMessage `json:"active_interfaces,omitempty"`
	} `json:"capabilities"`
}

// readNativeStatus is status-only. It authenticates the Unix peer to the exact
// verified runtime process, not just a socket filename or a public agent cache.
// Dial errno is preserved so startup may wait for a missing/refused socket.
func readNativeStatus(ctx context.Context, socket string, pid int, start, executable string) (nativeStatus, error) {
	return readNativeStatusPeer(ctx, socket, pid, 0, start, executable)
}
func readNativeStatusPeer(ctx context.Context, socket string, pid int, uid uint32, start, executable string) (nativeStatus, error) {
	var status nativeStatus
	if pid <= 1 || start == "" || executable == "" {
		return status, errors.New("runtime process identity is incomplete")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
	if err != nil {
		return status, err
	}
	defer connection.Close()
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		return status, errors.New("runtime socket is not Unix")
	}
	raw, err := unixConnection.SyscallConn()
	if err != nil {
		return status, err
	}
	var credentials *unix.Ucred
	var credentialErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, credentialErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return status, err
	}
	if credentialErr != nil {
		return status, credentialErr
	}
	if credentials == nil || credentials.Uid != uid || int(credentials.Pid) != pid {
		return status, errors.New("runtime socket peer does not match verified runtime process")
	}
	if err := sameExecutable(pid, executable); err != nil {
		return status, err
	}
	identity, err := processStartTime(pid)
	if err != nil {
		return status, err
	}
	if identity != start {
		return status, errors.New("runtime process identity changed")
	}
	deadline, _ := ctx.Deadline()
	if err := connection.SetDeadline(deadline); err != nil {
		return status, err
	}
	stop := context.AfterFunc(ctx, func() { connection.Close() })
	defer stop()
	if _, err := io.WriteString(connection, "{\"protocol\":2,\"operation\":\"status\"}\n"); err != nil {
		return status, err
	}
	line, err := bufio.NewReader(io.LimitReader(connection, 65537)).ReadBytes('\n')
	if err != nil {
		return status, err
	}
	if len(line) > 65536 {
		return status, errors.New("runtime status exceeds limit")
	}
	if err := json.Unmarshal(line, &status); err != nil {
		return status, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(line, &fields); err != nil {
		return status, err
	}
	for _, field := range []string{"protocol", "ok", "state", "execution", "version"} {
		if len(fields[field]) == 0 || string(fields[field]) == "null" {
			return status, errors.New("runtime status identity is incomplete")
		}
	}
	if status.Protocol != 2 || strings.TrimSpace(status.Version) == "" || status.State == "" || status.Execution == "" {
		return status, errors.New("runtime status does not implement protocol2")
	}
	if (status.State == "idle" || (status.State == "reboot_required" && status.Execution == "none")) && (status.Execution != "none" || status.System != nil || status.Core != nil || status.Generation != nil || (len(status.ActivePackage) != 0 && string(status.ActivePackage) != "null") || len(status.Capabilities.ActiveInterfaces) != 0) {
		return status, errors.New("idle runtime status contains active execution")
	}
	if err := ctx.Err(); err != nil {
		return status, err
	}
	identity, err = processStartTime(pid)
	if err != nil {
		return status, err
	}
	if identity != start {
		return status, errors.New("runtime changed while reading status")
	}
	if err := sameExecutable(pid, executable); err != nil {
		return status, err
	}
	return status, nil
}
