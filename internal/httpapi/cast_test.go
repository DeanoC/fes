package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/cast"
	"github.com/DeanoC/FogCast-POC/protocol"
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

func TestCastStartDistinguishesOmittedAndNullMedia(t *testing.T) {
	for name, body := range map[string]string{
		"omitted":             `{"session":"session","token":"token","generation":9}`,
		"null":                `{"session":"session","token":"token","generation":9,"media":null}`,
		"missing audio":       `{"session":"session","token":"token","generation":9,"media":{"version":1,"video":true}}`,
		"null audio":          `{"session":"session","token":"token","generation":9,"media":{"version":1,"video":true,"audio":null}}`,
		"unknown audio field": `{"session":"session","token":"token","generation":9,"media":{"version":1,"video":true,"audio":false,"private":"x"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			controller := &castHandlerTestController{}
			request := httptest.NewRequest(http.MethodPost, "/v1/cast/start", strings.NewReader(body))
			response := httptest.NewRecorder()
			castStartHandler(controller).ServeHTTP(response, request)
			if name == "omitted" && (response.Code != http.StatusOK || controller.starts != 1) {
				t.Fatalf("legacy response=%d starts=%d", response.Code, controller.starts)
			}
			if name != "omitted" && (response.Code != http.StatusBadRequest || controller.starts != 0) {
				t.Fatalf("null response=%d starts=%d", response.Code, controller.starts)
			}
		})
	}
}

func TestCastStartRejectsAudioWithoutCoordinatorSinkCapability(t *testing.T) {
	controller := &castHandlerTestController{}
	request := httptest.NewRequest(http.MethodPost, "/v1/cast/start", strings.NewReader(`{"session":"session","token":"token","generation":9,"media":{"version":1,"video":true,"audio":true}}`))
	response := httptest.NewRecorder()
	castStartHandler(controller).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || controller.starts != 0 {
		t.Fatalf("status = %d starts = %d body=%s", response.Code, controller.starts, response.Body.String())
	}
}

func TestCastStartAcknowledgesVideoOnlyMedia(t *testing.T) {
	controller := &mediaCapableCastHandlerController{}
	request := httptest.NewRequest(http.MethodPost, "/v1/cast/start", strings.NewReader(`{"session":"session","token":"token","generation":9,"media":{"version":1,"video":true,"audio":false}}`))
	response := httptest.NewRecorder()
	castStartHandler(controller).ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"media":{"version":1,"video":true,"audio":false,"ready":true,"capabilities":{"version":1,"video":true,"audio":true}}`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestCastStartRejectsVideoOnlyMediaWithoutAudioCapability(t *testing.T) {
	controller := &videoOnlyMediaCastHandlerController{}
	request := httptest.NewRequest(http.MethodPost, "/v1/cast/start", strings.NewReader(`{"session":"session","token":"token","generation":9,"media":{"version":1,"video":true,"audio":false}}`))
	response := httptest.NewRecorder()
	castStartHandler(controller).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || controller.starts != 0 {
		t.Fatalf("response=%d starts=%d", response.Code, controller.starts)
	}
}

type videoOnlyMediaCastHandlerController struct{ castHandlerTestController }

func (*videoOnlyMediaCastHandlerController) StartWithMedia(context.Context, string, string, uint64, protocol.CastMediaSet) error {
	return nil
}
func (*videoOnlyMediaCastHandlerController) CastMediaCapabilities(context.Context) protocol.CastMediaCapabilities {
	return protocol.CastMediaCapabilities{Version: protocol.CastMediaSetVersion, Video: true, Audio: false}
}

type mediaCapableCastHandlerController struct{ castHandlerTestController }

func (c *mediaCapableCastHandlerController) StartWithMedia(ctx context.Context, session, token string, generation uint64, media protocol.CastMediaSet) error {
	c.starts++
	return nil
}

func (*mediaCapableCastHandlerController) CastMediaCapabilities(context.Context) protocol.CastMediaCapabilities {
	return protocol.CastMediaCapabilities{Version: protocol.CastMediaSetVersion, Video: true, Audio: true}
}

func (*mediaCapableCastHandlerController) Status(context.Context) cast.Status {
	return cast.Status{State: cast.Active, Media: &protocol.CastStatusMedia{Version: protocol.CastMediaSetVersion, Video: true, Audio: false, Ready: true, Capabilities: protocol.CastMediaCapabilities{Version: protocol.CastMediaSetVersion, Video: true, Audio: true}}}
}
