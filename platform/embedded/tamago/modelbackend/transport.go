// SPDX-License-Identifier: GPL-3.0-or-later
package modelbackend

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast/hostclient"
)

func response(request *http.Request, status int, body []byte) *http.Response {
	return &http.Response{Request: request, StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body))}
}
func (b *Backend) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	path := request.URL.Path
	var wire any
	status := 200
	switch {
	case request.Method == "GET" && path == "/api/v1/session":
		return response(request, 200, b.snapshot()), nil
	case request.Method == "POST" && (path == "/api/v1/session/launch" || path == "/api/v1/session/stop"):
		var body struct {
			Game        string           `json:"game_id"`
			Expected    *expectedSession `json:"expected_session"`
			Retain      bool             `json:"retain_lease"`
			ReleaseIdle bool             `json:"release_idle"`
		}
		var data []byte
		var err error
		if request.Body != nil {
			data, err = io.ReadAll(io.LimitReader(request.Body, 4097))
		}
		if request.Body != nil {
			request.Body.Close()
		}
		if err != nil {
			return nil, err
		}
		if len(data) > 4096 || (len(bytes.TrimSpace(data)) > 0 && json.Unmarshal(data, &body) != nil) {
			return response(request, 400, []byte(`{"error":{"code":"INVALID_REQUEST","message":"invalid model request"}}`)), nil
		}
		method := "stop"
		if path == "/api/v1/session/launch" {
			method = "launch"
		}
		if body.ReleaseIdle {
			return response(request, 200, b.snapshot()), nil
		}
		expected := expectedSession{}
		if body.Expected != nil {
			expected = *body.Expected
		}
		r, err := b.request(call{bound: body.Expected != nil, ctx: request.Context(), method: method, game: body.Game, expected: expected, flight: request.Header.Get("X-FogCast-Flight-Id")})
		if err != nil {
			return nil, err
		}
		return response(request, r.status, r.body), nil
	case request.Method == "GET" && path == "/api/v1/games":
		games := []hostclient.Game{}
		for _, entry := range b.entries {
			games = append(games, hostclient.Game{ID: entry.ID, Title: entry.Title, System: "model", Execution: "fpga_model", State: "available", RootOnline: true, Launchable: true})
		}
		wire = map[string]any{"games": games}
	case path == "/api/v1/platforms":
		wire = map[string]any{"platforms": []any{}}
	case path == "/api/v1/library/collections":
		wire = map[string]any{"collections": []any{}}
	case path == "/api/v1/library/edition-preferences":
		wire = map[string]any{"preferences": []any{}}
	case path == "/api/v1/library/facets":
		wire = map[string]any{"genres": []any{}, "years": []any{}}
	case request.Method == "POST" && path == "/api/v1/events":
		wire = map[string]any{}
	default:
		status = 404
		wire = map[string]any{"error": map[string]string{"code": "UNSUPPORTED", "message": "not implemented by software model"}}
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return nil, err
	}
	return response(request, status, body), nil
}
