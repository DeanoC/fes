package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/cast"
)

type castHandlerTestController struct{ starts int }

func (c *castHandlerTestController) Start(context.Context, string, string, uint64) error {
	c.starts++
	return nil
}
func (*castHandlerTestController) Stop(context.Context, string, uint64) error { return nil }
func (*castHandlerTestController) Status(context.Context) cast.Status {
	return cast.Status{State: cast.Idle}
}

func TestCastStartRejectsInvalidIdentityBeforeController(t *testing.T) {
	for name, body := range map[string]string{
		"empty session":   `{"session":"","token":"token","generation":9}`,
		"empty token":     `{"session":"session","token":"","generation":9}`,
		"zero generation": `{"session":"session","token":"token","generation":0}`,
	} {
		t.Run(name, func(t *testing.T) {
			controller := &castHandlerTestController{}
			request := httptest.NewRequest(http.MethodPost, "/v1/cast/start", strings.NewReader(body))
			response := httptest.NewRecorder()
			castStartHandler(controller).ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
			if controller.starts != 0 {
				t.Fatalf("controller starts = %d, want 0", controller.starts)
			}
		})
	}
}
