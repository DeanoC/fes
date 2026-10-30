package targetclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

func (c *Client) InspectCoreData(ctx context.Context, size int64, body io.Reader, id, gameID string) (protocol.CoreDataInspection, error) {
	return c.coreData(ctx, size, body, id, gameID, nil)
}
func (c *Client) UpdateCoreSettings(ctx context.Context, size int64, body io.Reader, u protocol.CoreSettingsUpdate) (protocol.CoreDataInspection, error) {
	if !u.Valid() {
		return protocol.CoreDataInspection{}, fmt.Errorf("core settings request is invalid")
	}
	return c.coreData(ctx, size, body, u.ExpectedPackageID, u.GameID, &u)
}
func (c *Client) coreData(ctx context.Context, size int64, body io.Reader, id, gameID string, u *protocol.CoreSettingsUpdate) (protocol.CoreDataInspection, error) {
	if size < 1 || size > corepackage.MaxArchiveSize || body == nil || !lowerHex(id, 64) {
		return protocol.CoreDataInspection{}, fmt.Errorf("core data request is invalid")
	}
	path := "/v1/library/core/data/inspect"
	if u != nil {
		path = "/v1/library/core/settings"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(path, nil).String(), readOnlyReader{Reader: body})
	if err != nil {
		return protocol.CoreDataInspection{}, err
	}
	request.ContentLength = size
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("X-FogCast-Package-ID", id)
	if gameID != "" {
		request.Header.Set("X-FogCast-Game-ID", gameID)
	}
	hadLease := c.kitLease.CurrentToken() != ""
	if u != nil {
		request.Header.Set("X-FogCast-Expected-Revision", u.ExpectedRevision)
		if u.Machine != nil {
			machine, _ := json.Marshal(u.Machine)
			request.Header.Set("X-FogCast-PC386-Machine", string(machine))
		} else {
			request.Header.Set("X-FogCast-Paddle-Speed", strconv.Itoa(int(u.PaddleSpeed)))
		}
		if err := c.authorizeMutation(request); err != nil {
			return protocol.CoreDataInspection{}, err
		}
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		return protocol.CoreDataInspection{}, newTransferError(err)
	}
	defer response.Body.Close()
	var result protocol.CoreDataInspection
	var raw json.RawMessage
	if err := decodeResponse(response, &raw); err != nil {
		return result, c.finishSettingsLease(ctx, u != nil && !hadLease, err)
	}
	if err := decodeCoreDataInspection(raw, &result); err != nil {
		return result, err
	}
	if !result.Valid() || result.PackageID != id || (u != nil && (result.Mode != "persistent" || result.Revision == "absent" ||
		(u.Machine != nil && (result.GameID != u.GameID || result.Machine == nil || *result.Machine != *u.Machine)) ||
		(u.Machine == nil && result.PaddleSpeed != u.PaddleSpeed))) ||
		(result.Machine != nil && result.GameID != gameID) {
		return protocol.CoreDataInspection{}, fmt.Errorf("core data response does not match admitted package")
	}
	if err := c.finishSettingsLease(ctx, u != nil && !hadLease, nil); err != nil {
		return protocol.CoreDataInspection{}, err
	}
	return result, nil
}
func (c *Client) LoadLibraryCore(ctx context.Context, size int64, body io.Reader, id, gameID string) (protocol.Status, error) {
	if !lowerHex(id, 64) {
		return protocol.Status{}, fmt.Errorf("library package identity is invalid")
	}
	return c.loadCoreWithGameID(ctx, size, body, id, gameID)
}

func decodeCoreDataInspection(raw json.RawMessage, result *protocol.CoreDataInspection) error {
	invalid := fmt.Errorf("core data response is incomplete or invalid")
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return invalid
	}
	for _, key := range []string{"package_id", "core_id", "mode", "revision", "descriptor"} {
		v, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return invalid
		}
	}
	layout, ok := fields["layout"]
	if !ok {
		return invalid
	}
	_, hasGameID := fields["game_id"]
	_, hasMachine := fields["machine"]
	if hasGameID || hasMachine {
		if !hasGameID || !hasMachine || len(fields) != 8 || fields["paddle_speed"] != nil || fields["best_rally"] != nil {
			return invalid
		}
		for _, key := range []string{"game_id", "machine"} {
			if value, ok := fields[key]; !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return invalid
			}
		}
		var machine map[string]json.RawMessage
		if json.Unmarshal(fields["machine"], &machine) != nil || len(machine) != 7 {
			return invalid
		}
		for _, key := range []string{"preset", "frequency", "code_profile", "turbo", "sb_base", "sb_irq", "sb_dma"} {
			if value, ok := machine[key]; !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return invalid
			}
		}
	} else {
		if len(fields) != 8 {
			return invalid
		}
		for _, key := range []string{"paddle_speed", "best_rally"} {
			if value, ok := fields[key]; !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return invalid
			}
		}
	}
	if !bytes.Equal(bytes.TrimSpace(layout), []byte("null")) {
		var contract map[string]json.RawMessage
		if json.Unmarshal(layout, &contract) != nil || len(contract) != 3 {
			return invalid
		}
		for _, key := range []string{"id", "major", "minor"} {
			value, ok := contract[key]
			if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return invalid
			}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(result) != nil {
		return invalid
	}
	return nil
}

// The service lifecycle serializes this call with launches using the shared lease.
// Definite admission/CAS rejection is safe to release; uncertainty retains it.
func (c *Client) finishSettingsLease(ctx context.Context, acquired bool, operationErr error) error {
	if !acquired || c.kitLease == nil {
		return operationErr
	}
	if operationErr != nil {
		var apiErr *protocol.APIError
		if !errors.As(operationErr, &apiErr) || apiErr.Phase == "recovery" {
			return operationErr
		}
		switch apiErr.Code {
		case protocol.CodeStaleRevision, protocol.CodeCorruptData, protocol.CodeIncompatibleData, protocol.CodeBadRequest, protocol.CodeInvalidArchive, protocol.CodeUnsupportedOperation:
		default:
			return operationErr
		}
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := c.kitLease.Release(cleanup); err != nil {
		return &protocol.APIError{Code: protocol.CodeInternal, Message: "core settings operation finished but target ownership cleanup failed", Phase: "recovery"}
	}
	return operationErr
}
