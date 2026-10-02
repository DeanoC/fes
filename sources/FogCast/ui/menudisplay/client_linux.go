//go:build linux

// Package menudisplay presents immutable RGBA frames through the local runtime
// socket. The runtime alone maps DDR or controls FPGA hardware.
package menudisplay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"golang.org/x/sys/unix"
)

const (
	Width      = 1280
	Height     = 720
	Stride     = Width * 4
	FrameBytes = Stride * Height
	SlotBytes  = 4 << 20
	maxReply   = 64 << 10
)

type Client struct{ Path string }
type Status struct {
	Available         bool   `json:"available"`
	Session           bool   `json:"session"`
	PackageID         string `json:"package_id"`
	CoreGeneration    uint64 `json:"core_generation"`
	Generation        uint64 `json:"generation"`
	Width             int    `json:"width"`
	Height            int    `json:"height"`
	Stride            int    `json:"stride"`
	ByteCount         int    `json:"byte_count"`
	SlotBytes         int    `json:"slot_bytes"`
	DisplayedSequence uint64 `json:"displayed_sequence"`
	Underflows        uint64 `json:"underflows"`
}
type Result struct {
	Generation        uint64 `json:"generation"`
	DisplayedSequence uint64 `json:"displayed_sequence"`
	Underflows        uint64 `json:"underflows"`
}
type reply struct {
	OK          bool    `json:"ok"`
	MenuDisplay *Status `json:"menu_display"`
	MenuFrame   *struct {
		Generation        uint64  `json:"generation"`
		ByteCount         int     `json:"byte_count"`
		StagingFormat     string  `json:"staging_format"`
		DisplayedSequence *uint64 `json:"displayed_sequence"`
		Underflows        uint64  `json:"underflows"`
	} `json:"menu_frame"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func New(path string) *Client { return &Client{Path: path} }

func (c *Client) dial(ctx context.Context) (*net.UnixConn, error) {
	if c == nil || c.Path == "" {
		return nil, errors.New("menu runtime socket unavailable")
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", c.Path)
	if err != nil {
		return nil, err
	}
	u, ok := conn.(*net.UnixConn)
	if !ok {
		conn.Close()
		return nil, errors.New("menu runtime is not a Unix socket")
	}
	deadline := time.Now().Add(4 * time.Second)
	if user, ok := ctx.Deadline(); ok && user.Before(deadline) {
		deadline = user
	}
	if err := u.SetDeadline(deadline); err != nil {
		u.Close()
		return nil, err
	}
	return u, nil
}

func send(conn *net.UnixConn, message any, fd int) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	var rights []byte
	if fd >= 0 {
		rights = unix.UnixRights(fd)
	}
	n, _, err := conn.WriteMsgUnix(data, rights, nil)
	if err != nil {
		return err
	}
	if n != len(data) {
		return errors.New("short menu request")
	}
	return nil
}

func receive(conn *net.UnixConn) (reply, []int, error) {
	var line []byte
	var fds []int
	bad := func(err error) (reply, []int, error) {
		for _, fd := range fds {
			unix.Close(fd)
		}
		return reply{}, nil, err
	}
	for {
		var buf [4096]byte
		var oob [128]byte
		n, oobn, flags, _, err := conn.ReadMsgUnix(buf[:], oob[:])
		if err != nil {
			return bad(err)
		}
		if n == 0 {
			return bad(errors.New("menu response ended before newline"))
		}
		if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
			return bad(errors.New("truncated menu response"))
		}
		if oobn > 0 {
			messages, err := unix.ParseSocketControlMessage(oob[:oobn])
			if err != nil {
				return bad(err)
			}
			for _, control := range messages {
				if control.Header.Level != unix.SOL_SOCKET || control.Header.Type != unix.SCM_RIGHTS {
					return bad(errors.New("invalid menu descriptor"))
				}
				got, err := unix.ParseUnixRights(&control)
				if err != nil {
					return bad(err)
				}
				for _, fd := range got {
					unix.CloseOnExec(fd)
				}
				fds = append(fds, got...)
			}
			if len(fds) > 1 {
				return bad(errors.New("too many menu descriptors"))
			}
		}
		line = append(line, buf[:n]...)
		if len(line) > maxReply {
			return bad(errors.New("menu response too large"))
		}
		if end := bytes.IndexByte(line, '\n'); end >= 0 {
			if end != len(line)-1 {
				return bad(errors.New("extra menu response bytes"))
			}
			var result reply
			if err := json.Unmarshal(line[:end], &result); err != nil {
				return bad(err)
			}
			if !result.OK {
				return bad(fmt.Errorf("menu request rejected: %s %s", result.Error.Code, result.Error.Message))
			}
			return result, fds, nil
		}
	}
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return Status{}, err
	}
	defer conn.Close()
	if err := send(conn, map[string]any{"protocol": 2, "operation": "status"}, -1); err != nil {
		return Status{}, err
	}
	result, fds, err := receive(conn)
	for _, fd := range fds {
		unix.Close(fd)
	}
	if err != nil {
		return Status{}, err
	}
	if len(fds) != 0 {
		return Status{}, errors.New("status returned a descriptor")
	}
	if result.MenuDisplay == nil {
		return Status{}, nil
	}
	s := *result.MenuDisplay
	if !s.Available {
		return s, nil
	}
	if s.Generation == 0 || s.Width != Width || s.Height != Height || s.Stride != Stride || s.ByteCount != FrameBytes || s.SlotBytes != SlotBytes {
		return Status{}, errors.New("unsupported menu geometry")
	}
	return s, nil
}

func (c *Client) Present(ctx context.Context, generation uint64, pix []byte) (Result, error) {
	if generation == 0 || len(pix) != FrameBytes {
		return Result{}, errors.New("invalid menu frame")
	}
	conn, err := c.dial(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Close()
	if err := send(conn, map[string]any{"protocol": 2, "operation": "menu_frame_begin", "expected_generation": generation, "byte_count": FrameBytes}, -1); err != nil {
		return Result{}, err
	}
	prepared, fds, err := receive(conn)
	if err != nil {
		return Result{}, err
	}
	if len(fds) != 1 {
		return Result{}, errors.New("menu begin did not return one descriptor")
	}
	fd := fds[0]
	defer unix.Close(fd)
	if prepared.MenuFrame == nil || prepared.MenuFrame.Generation != generation || prepared.MenuFrame.ByteCount != FrameBytes || prepared.MenuFrame.StagingFormat != "rgba8888" || prepared.MenuFrame.DisplayedSequence != nil {
		return Result{}, errors.New("invalid menu staging response")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return Result{}, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Size != FrameBytes {
		return Result{}, errors.New("invalid menu staging file")
	}
	mapped, err := unix.Mmap(fd, 0, FrameBytes, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return Result{}, err
	}
	copy(mapped, pix)
	if err := unix.Munmap(mapped); err != nil {
		return Result{}, err
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); err != nil {
		return Result{}, err
	}
	if err := send(conn, map[string]any{"protocol": 2, "operation": "menu_frame_commit", "generation": generation, "byte_count": FrameBytes}, fd); err != nil {
		return Result{}, err
	}
	completed, extra, err := receive(conn)
	for _, other := range extra {
		unix.Close(other)
	}
	if err != nil {
		return Result{}, err
	}
	if len(extra) != 0 || completed.MenuFrame == nil || completed.MenuFrame.Generation != generation || completed.MenuFrame.DisplayedSequence == nil || completed.MenuFrame.Underflows > TransientUnderflowCap {
		return Result{}, errors.New("invalid menu frame completion")
	}
	return Result{Generation: generation, DisplayedSequence: *completed.MenuFrame.DisplayedSequence, Underflows: completed.MenuFrame.Underflows}, nil
}
