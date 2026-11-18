//go:build linux

package localcores

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/DeanoC/FogCast/internal/discovery"
	"golang.org/x/sys/unix"
)

// Identity makes a token-free local request only after authenticating the
// Unix peer as the launcher's effective UID. It rechecks process incarnation
// after the response, so a replaced agent cannot answer the same request.
func (c *Client) Identity(ctx context.Context) (AgentIdentity, error) {
	if c == nil || c.path == "" || ctx == nil {
		return AgentIdentity{}, ErrUnavailable
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, clientTimeout)
		defer cancel()
	}
	raw, err := (&net.Dialer{Timeout: clientDialTimeout}).DialContext(ctx, "unix", c.path)
	if err != nil {
		return AgentIdentity{}, err
	}
	defer raw.Close()
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		if err := raw.SetDeadline(deadline); err != nil {
			return AgentIdentity{}, err
		}
	}
	conn, ok := raw.(*net.UnixConn)
	if !ok {
		return AgentIdentity{}, ErrUnavailable
	}
	pid, start, err := identityPeer(conn)
	if err != nil {
		return AgentIdentity{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://local-control/v1/local/identity", nil)
	if err != nil {
		return AgentIdentity{}, err
	}
	req.Close = true
	if err = req.Write(conn); err != nil {
		return AgentIdentity{}, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		return AgentIdentity{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return AgentIdentity{}, ErrUnavailable
	}
	var payload struct {
		TargetID string `json:"target_id"`
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if err != nil || len(data) > 4096 {
		return AgentIdentity{}, ErrUnavailable
	}
	if json.Unmarshal(data, &payload) != nil || !discovery.ValidID(payload.TargetID) {
		return AgentIdentity{}, ErrUnavailable
	}
	afterPID, afterStart, err := identityPeer(conn)
	if err != nil || pid != afterPID || start != afterStart {
		return AgentIdentity{}, errors.New("local agent changed during identity request")
	}
	return AgentIdentity{TargetID: payload.TargetID, PeerPID: pid, PeerStart: start}, nil
}

func identityPeer(conn *net.UnixConn) (int, string, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, "", err
	}
	var cred *unix.Ucred
	var peerErr error
	if err := raw.Control(func(fd uintptr) { cred, peerErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); err != nil {
		return 0, "", err
	}
	if peerErr != nil || cred == nil || cred.Pid <= 0 || cred.Uid != uint32(os.Geteuid()) {
		return 0, "", errors.New("local agent peer identity unavailable")
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", cred.Pid))
	if err != nil {
		return 0, "", err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return 0, "", errors.New("invalid local agent process stat")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 || fields[0] == "Z" || fields[0] == "X" {
		return 0, "", errors.New("local agent process is not live")
	}
	return int(cred.Pid), fields[19], nil
}

// The identity endpoint is available only to a local same-UID peer. The
// managed agent and launcher both run as root, which owns this hardware.
type identityConnKey struct{}

func identityConnContext(ctx context.Context, conn net.Conn) context.Context {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return context.WithValue(ctx, identityConnKey{}, false)
	}
	pid, _, err := identityPeer(unixConn)
	return context.WithValue(ctx, identityConnKey{}, err == nil && pid > 0)
}

func identityRequestAllowed(r *http.Request) bool {
	allowed, _ := r.Context().Value(identityConnKey{}).(bool)
	return allowed
}
