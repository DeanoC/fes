package misterruntime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

const DefaultSocketPath = "/run/mister-runtime.sock"
const MaximumLineBytes = 65536
const maximumRuntimePathBytes = 4095
const megaDriveRBFPath = "/usr/share/mister-runtime/cores/megadrive.rbf"

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

type RemoteError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Response struct {
	Protocol  int          `json:"protocol"`
	OK        bool         `json:"ok"`
	State     string       `json:"state"`
	Execution string       `json:"execution"`
	System    *string      `json:"system"`
	Core      *string      `json:"core"`
	Error     *RemoteError `json:"error"`
	Version   string       `json:"version"`
}

type LaunchRequest struct {
	SavePath string            `json:"save_path,omitempty"`
	System   string            `json:"system"`
	RBF      string            `json:"rbf"`
	Media    map[string]string `json:"media"`
	Settings map[string]string `json:"settings"`
}

type Control interface {
	Status(context.Context) (Response, error)
	Launch(context.Context, LaunchRequest) (Response, error)
	LoadDevelopmentRBF(context.Context, string) (Response, error)
	Stop(context.Context) (Response, error)
}

type Client struct {
	socketPath string
}

func NewClient(socketPath string) *Client {
	return &Client{socketPath: socketPath}
}

func (client *Client) Status(ctx context.Context) (Response, error) {
	return client.call(ctx, struct {
		Protocol  int    `json:"protocol"`
		Operation string `json:"operation"`
	}{Protocol: 1, Operation: "status"})
}

func (client *Client) Launch(ctx context.Context, request LaunchRequest) (Response, error) {
	if !validLaunchRequest(request) {
		return Response{}, errInvalidRuntimeRequest
	}
	return client.call(ctx, struct {
		Protocol  int               `json:"protocol"`
		Operation string            `json:"operation"`
		System    string            `json:"system"`
		RBF       string            `json:"rbf"`
		Media     map[string]string `json:"media"`
		Settings  map[string]string `json:"settings"`
		SavePath  string            `json:"save_path,omitempty"`
	}{
		Protocol: 1, Operation: "launch", System: request.System,
		RBF: request.RBF, Media: request.Media, Settings: request.Settings, SavePath: request.SavePath,
	})
}

func (client *Client) LoadDevelopmentRBF(ctx context.Context, rbf string) (Response, error) {
	if !validRuntimePath(rbf) {
		return Response{}, errInvalidRuntimeRequest
	}
	return client.call(ctx, struct {
		Protocol  int    `json:"protocol"`
		Operation string `json:"operation"`
		RBF       string `json:"rbf"`
	}{Protocol: 1, Operation: "load_development_rbf", RBF: rbf})
}

func (client *Client) Stop(ctx context.Context) (Response, error) {
	return client.call(ctx, struct {
		Protocol  int    `json:"protocol"`
		Operation string `json:"operation"`
	}{Protocol: 1, Operation: "stop"})
}

func (client *Client) call(ctx context.Context, requestBody any) (Response, error) {
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return Response{}, errInvalidRuntimeResponse
	}

	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", client.socketPath)
	if err != nil {
		return Response{}, contextOr(ctx, errRuntimeConnection)
	}
	defer connection.Close()

	if deadline, ok := ctx.Deadline(); ok {
		if err := connection.SetDeadline(deadline); err != nil {
			return Response{}, contextOr(ctx, errRuntimeDeadline)
		}
	}
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	stopCancellationRelay := relayContextCancellation(ctx, connection)
	defer stopCancellationRelay()

	request := append(payload, '\n')
	if err := writePayload(connection, request); err != nil {
		return Response{}, contextOr(ctx, errRuntimeRequestWrite)
	}

	line, err := readResponseLine(connection)
	if err != nil {
		return Response{}, contextOr(ctx, err)
	}
	response, err := decodeResponse(line)
	if err != nil {
		return Response{}, err
	}
	return response, nil
}

func nativeRBFPath(system protocol.System) string {
	switch system {
	case protocol.SystemMegaDrive:
		return megaDriveRBFPath
	case protocol.SystemSNES:
		return "/usr/share/mister-runtime/cores/snes.rbf"
	case protocol.SystemPong:
		return "/usr/share/mister-runtime/cores/pong.rbf"
	default:
		return ""
	}
}

func validLaunchRequest(request LaunchRequest) bool {
	if request.SavePath != "" && (request.System != "snes" || !validRuntimePath(request.SavePath)) {
		return false
	}
	expected := nativeRBFPath(protocol.System(request.System))
	if expected == "" || request.RBF != expected || request.Settings == nil || len(request.Settings) != 0 || request.Media == nil {
		return false
	}
	if request.System == string(protocol.SystemPong) {
		return len(request.Media) == 0
	}
	if len(request.Media) != 1 {
		return false
	}
	cartridge, ok := request.Media["cartridge"]
	return ok && filepath.IsAbs(cartridge) && filepath.Clean(cartridge) == cartridge && strings.IndexByte(cartridge, 0) < 0
}

func validRuntimePath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path &&
		len(path) <= maximumRuntimePathBytes && strings.IndexByte(path, 0) < 0
}

func writePayload(connection net.Conn, payload []byte) error {
	for len(payload) > 0 {
		count, err := connection.Write(payload)
		payload = payload[count:]
		if err != nil {
			return err
		}
		if count == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
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

func decodeResponse(line []byte) (Response, error) {
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()

	var response Response
	if err := decoder.Decode(&response); err != nil {
		return Response{}, errInvalidRuntimeResponse
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Response{}, errInvalidRuntimeResponse
	}
	if err := validateResponseFields(line); err != nil {
		return Response{}, err
	}
	if err := validateResponse(response); err != nil {
		return Response{}, err
	}
	return response, nil
}

func validateResponseFields(line []byte) error {
	var fields struct {
		Protocol  *int            `json:"protocol"`
		OK        *bool           `json:"ok"`
		State     *string         `json:"state"`
		Execution *string         `json:"execution"`
		System    json.RawMessage `json:"system"`
		Core      json.RawMessage `json:"core"`
		Error     json.RawMessage `json:"error"`
		Version   *string         `json:"version"`
	}
	if err := json.Unmarshal(line, &fields); err != nil {
		return errInvalidRuntimeResponse
	}
	if fields.Protocol == nil || fields.OK == nil || fields.State == nil ||
		fields.Execution == nil || len(fields.System) == 0 || len(fields.Core) == 0 ||
		len(fields.Error) == 0 || fields.Version == nil {
		return errInvalidRuntimeResponse
	}

	if bytes.Equal(fields.Error, []byte("null")) {
		return nil
	}
	var remoteError struct {
		Code    *string `json:"code"`
		Message *string `json:"message"`
	}
	if err := json.Unmarshal(fields.Error, &remoteError); err != nil ||
		remoteError.Code == nil || remoteError.Message == nil {
		return errInvalidRuntimeResponse
	}
	return nil
}

func validateResponse(response Response) error {
	if response.Protocol != 1 || response.Version == "" {
		return errInvalidRuntimeResponse
	}
	if response.Error == nil {
		if !response.OK {
			return errInvalidRuntimeResponse
		}
	} else if !validErrorCode(response.Error.Code) {
		return errInvalidRuntimeResponse
	}

	identity := responseIdentity(response)
	switch response.State {
	case "idle":
		if response.Execution != "none" || identity != identityNone {
			return errInvalidRuntimeResponse
		}
	case "starting":
		switch response.Execution {
		case "none":
			if identity != identityNone && identity != identityPair {
				return errInvalidRuntimeResponse
			}
		case "game":
			if identity != identityPair {
				return errInvalidRuntimeResponse
			}
		case "development":
			if identity != identityNone {
				return errInvalidRuntimeResponse
			}
		default:
			return errInvalidRuntimeResponse
		}
	case "running_game":
		if response.Execution != "game" || identity != identityPair {
			return errInvalidRuntimeResponse
		}
	case "running_development":
		if response.Execution != "development" ||
			(identity != identityNone && identity != identityCore) {
			return errInvalidRuntimeResponse
		}
	case "reboot_required":
		if response.Execution != "none" || identity != identityNone || response.Error == nil ||
			response.Error.Code != "idle_failed" {
			return errInvalidRuntimeResponse
		}
	default:
		return errInvalidRuntimeResponse
	}
	return nil
}

type identityShape uint8

const (
	identityNone identityShape = iota
	identityPair
	identityCore
	identityInvalid
)

func responseIdentity(response Response) identityShape {
	if response.System == nil && response.Core == nil {
		return identityNone
	}
	if response.System == nil && response.Core != nil && *response.Core != "" {
		return identityCore
	}
	if response.System == nil || response.Core == nil || *response.System == "" || *response.Core == "" {
		return identityInvalid
	}
	return identityPair
}

func validErrorCode(code string) bool {
	switch code {
	case "invalid_request", "unsupported_protocol", "unknown_system", "missing_media", "busy",
		"program_failed", "core_mismatch", "io_failed", "idle_failed", "save_failed":
		return true
	default:
		return false
	}
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
