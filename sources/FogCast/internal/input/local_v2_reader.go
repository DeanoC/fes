package input

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
)

// readLocalControlLine distinguishes a bounded newline JSON control header
// from the existing binary frame without consuming its first byte. A timeout
// after a partial line is fatal: it must not be resumed as a new record.
func readLocalControlLine(r *bufio.Reader) ([]byte, bool, error) {
	first, err := r.Peek(1)
	if err != nil {
		return nil, false, err
	}
	if first[0] != '{' {
		return nil, false, nil
	}
	var line [localhid.MaxRequestBytes + 1]byte
	n := 0
	for {
		b, err := r.ReadByte()
		if err != nil {
			if n > 0 {
				return nil, true, errors.New("partial local control record")
			}
			return nil, true, err
		}
		if n == len(line) {
			return nil, true, errors.New("oversized local control record")
		}
		line[n] = b
		n++
		if b == '\n' {
			return append([]byte(nil), line[:n]...), true, nil
		}
	}
}
func writeV2Reply(conn net.Conn, reply localhid.Reply) error {
	data, err := localhid.EncodeReply(reply)
	if err != nil {
		return err
	}
	if err := conn.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}
	defer conn.SetWriteDeadline(time.Time{})
	for len(data) > 0 {
		n, err := conn.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

// retireV2Source keeps the sole reader slot while neutral is uncertain. Only
// neutralization is retried; no input event is replayed. Shutdown/lease cleanup
// remains the manager's responsibility. The raw-keyboard guard remains latched
// even after this returns until a future acknowledged physical-state handoff.
func (c *TargetController) retireV2Source(ctx context.Context, source *localhid.SourceSession) {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for ctx.Err() == nil && !c.localIsClosed() {
		attempt, cancel := context.WithTimeout(ctx, 3*time.Second)
		status, _ := source.Abort(attempt)
		cancel()
		if status == "neutral-confirmed" || status == "old-generation-retired" {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// retireLocalNonHID retains the serialized reader slot until local pad,
// mouse and joystick-to-matrix contributions are confirmed neutral. Only
// idempotent source-neutral cleanup is retried; no input event is replayed.
func (c *TargetController) retireLocalNonHID(ctx context.Context) {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for ctx.Err() == nil && !c.localIsClosed() {
		attempt, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := c.ports.releaseSourceWithoutHIDContext(attempt, sourceLocal)
		cancel()
		if err == nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
