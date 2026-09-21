package misterruntime

import (
	"bufio"

	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"time"
)

const DefaultSocketPath = "/run/mister-runtime.sock"
const MaximumLineBytes = 65536
const maximumRuntimePathBytes = 4095

var (
	errRuntimeConnection      = errors.New("runtime connection failed")
	errRuntimeDeadline        = errors.New("runtime deadline setup failed")
	errRuntimeRequestWrite    = errors.New("runtime request write failed")
	errRuntimeResponseRead    = errors.New("runtime response read failed")
	errRuntimeResponseTooLong = errors.New("runtime response exceeds 65536 bytes")
	errRuntimeMissingNewline  = errors.New("runtime response is missing newline")
	errInvalidRuntimeResponse = errors.New("invalid runtime response")
	errInvalidRuntimeRequest  = errors.New("invalid runtime request")
)

type Control interface {
	Protocol2Status(context.Context) (Protocol2Response, error)
	Protocol2Stop(context.Context) (Protocol2Response, error)
	Protocol2LoadDevelopmentRBF(context.Context, string) (Protocol2Response, error)
}

type Client struct {
	socketPath string
}

func NewClient(socketPath string) *Client {
	return &Client{socketPath: socketPath}
}

func (client *Client) callRaw(ctx context.Context, requestBody any) ([]byte, error) {
	line, _, err := client.callRawTracked(ctx, requestBody)
	return line, err
}

// callRawTracked reports whether any request byte crossed the transport write
// boundary. Once that happens, an error is sent-or-ambiguous to callers.
func (client *Client) callRawTracked(ctx context.Context, requestBody any) ([]byte, bool, error) {
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return nil, false, errInvalidRuntimeResponse
	}

	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", client.socketPath)
	if err != nil {
		return nil, false, contextOr(ctx, errRuntimeConnection)
	}
	defer connection.Close()

	if deadline, ok := ctx.Deadline(); ok {
		if err := connection.SetDeadline(deadline); err != nil {
			return nil, false, contextOr(ctx, errRuntimeDeadline)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	stopCancellationRelay := relayContextCancellation(ctx, connection)
	defer stopCancellationRelay()

	request := append(payload, '\n')
	written, err := writePayloadCount(connection, request)
	if err != nil {
		return nil, protocol2FrameDispatched(written, len(request)), contextOr(ctx, errRuntimeRequestWrite)
	}

	line, err := readResponseLine(connection)
	if err != nil {
		return nil, true, contextOr(ctx, err)
	}
	return line, true, nil
}

func protocol2FrameDispatched(written, frameSize int) bool {
	return frameSize > 0 && written == frameSize
}

func validRuntimePath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path &&
		len(path) <= maximumRuntimePathBytes && strings.IndexByte(path, 0) < 0
}

func writePayload(connection net.Conn, payload []byte) error {
	_, err := writePayloadCount(connection, payload)
	return err
}

func writePayloadCount(connection net.Conn, payload []byte) (int, error) {
	written := 0
	for len(payload) > 0 {
		count, err := connection.Write(payload)
		payload = payload[count:]
		written += count
		if err != nil {
			return written, err
		}
		if count == 0 {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

func readResponseLine(connection net.Conn) ([]byte, error) {
	reader := bufio.NewReader(io.LimitReader(connection, MaximumLineBytes+1))
	line, err := reader.ReadBytes('\n')
	if len(line) > MaximumLineBytes {
		return nil, errRuntimeResponseTooLong
	}
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errRuntimeMissingNewline
		}
		return nil, errRuntimeResponseRead
	}
	return line[:len(line)-1], nil
}

func contextOr(ctx context.Context, fallback error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return fallback
}

func relayContextCancellation(ctx context.Context, connection net.Conn) func() {
	done := ctx.Done()
	if done == nil {
		return func() {}
	}

	stopped := make(chan struct{})
	go func() {
		select {
		case <-done:
			_ = connection.SetDeadline(time.Now())
		case <-stopped:
		}
	}()
	return func() { close(stopped) }
}
