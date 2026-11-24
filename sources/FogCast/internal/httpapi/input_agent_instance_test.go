package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/input"
	"github.com/DeanoC/FogCast/protocol"
)

type inputAgentStatusController struct{}

func (inputAgentStatusController) Health(string) protocol.Health { return protocol.Health{} }
func (inputAgentStatusController) Status() protocol.Status {
	return protocol.Status{State: protocol.StateActive, InputAgentInstance: "stale"}
}
func (inputAgentStatusController) Stop(context.Context) (protocol.Status, *protocol.APIError) {
	return protocol.Status{}, nil
}

type inputAgentStatusInput struct {
	id  string
	err error
}

func (x inputAgentStatusInput) LocalAgentInstance() (string, error)    { return x.id, x.err }
func (inputAgentStatusInput) Attach(context.Context, input.Spec) error { return nil }
func (inputAgentStatusInput) Detach(context.Context, uint64) error     { return nil }
func (inputAgentStatusInput) OpenStream(context.Context, uint64) (net.Conn, error) {
	return nil, errors.New("unused")
}
func (inputAgentStatusInput) Close() error { return nil }

func TestInputAgentInstanceStatusIsFreshAndValidated(t *testing.T) {
	for _, tc := range []struct {
		id   string
		err  error
		want string
	}{
		{id: strings.Repeat("a", 32), want: strings.Repeat("a", 32)},
		{id: "invalid"},
		{id: strings.Repeat("b", 32), err: errors.New("unavailable")},
	} {
		h := New(inputAgentStatusController{}, "token", "test", nil, WithInput(inputAgentStatusInput{id: tc.id, err: tc.err}))
		r := httptest.NewRequest("GET", "/v1/status", nil)
		r.Header.Set("Authorization", "Bearer token")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var status protocol.Status
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &status) != nil || status.InputAgentInstance != tc.want {
			t.Fatalf("status=%d %+v", w.Code, status)
		}
	}
}
