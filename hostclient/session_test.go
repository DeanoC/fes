package hostclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadResponseBodyRejectsOversize(t *testing.T) {
	response := &http.Response{
		Body:          io.NopCloser(strings.NewReader("12345")),
		ContentLength: 5,
	}
	_, err := ReadResponseBody(response, 4)
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("error = %v, want ErrResponseTooLarge", err)
	}
}

func TestReadResponseBodyRejectsTruncatedBody(t *testing.T) {
	response := &http.Response{
		Body:          io.NopCloser(strings.NewReader("123")),
		ContentLength: 5,
	}
	_, err := ReadResponseBody(response, 8)
	if !errors.Is(err, ErrResponseTruncated) {
		t.Fatalf("error = %v, want ErrResponseTruncated", err)
	}
}

func TestContentTypeMainCanonicalizesMediaTypes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "parameters", input: " Image/PNG; charset=utf-8 ", want: "image/png"},
		{name: "empty", input: "", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ContentTypeMain(tc.input); got != tc.want {
				t.Fatalf("ContentTypeMain() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAPIStatusErrorPreservesStructuredAndPlainErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "structured", status: http.StatusNotFound, body: `{"error":{"code":"NOT_FOUND","message":"missing"}}`, want: "host API 404 NOT_FOUND: missing"},
		{name: "plain", status: http.StatusBadGateway, body: "not-json", want: "host API status 502"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := APIStatusError(tc.status, []byte(tc.body)).Error(); got != tc.want {
				t.Fatalf("APIStatusError() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeHandleCanonicalizesAndRejectsHandles(t *testing.T) {
	t.Parallel()
	valid := strings.Repeat("aB", 32)
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "canonical", input: strings.Repeat("ab", 32), want: strings.Repeat("ab", 32)},
		{name: "trim-and-lower", input: " \t" + valid + "\n", want: strings.Repeat("ab", 32)},
		{name: "empty", input: "", want: ""},
		{name: "short", input: strings.Repeat("a", 63), want: ""},
		{name: "non-hex", input: strings.Repeat("g", 64), want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeHandle(tc.input); got != tc.want {
				t.Fatalf("NormalizeHandle() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDecodeSessionPreservesKitFieldsAndUint64Precision(t *testing.T) {
	body := []byte(`{"state":"active","game_id":"pong","execution":"fpga_development","input":{"state":"attached","ready":true,"session_id":"kit-session"},"core_package":{"generation":18446744073709551614,"gamepad":true,"active_interfaces":[{"id":"fes.keyboard","major":1,"minor":0},{"id":"fes.gamepad","major":1,"minor":0}]}}`)

	result, err := DecodeSession(http.StatusOK, body)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "active" || result.GameID != "pong" || result.Execution != "fpga_development" {
		t.Fatalf("session = %+v", result)
	}
	if result.Input == nil || result.Input.SessionID != "kit-session" {
		t.Fatalf("input = %+v", result.Input)
	}
	if result.CorePackage == nil {
		t.Fatal("core package missing")
	}
	if result.CorePackage.Generation != ^uint64(0)-1 {
		t.Fatalf("generation = %d, want %d", result.CorePackage.Generation, ^uint64(0)-1)
	}
	if !result.CorePackage.Gamepad || len(result.CorePackage.ActiveInterfaces) != 2 {
		t.Fatalf("core package = %+v", result.CorePackage)
	}
	if !result.CoreKeyboard {
		t.Fatal("keyboard capability not detected")
	}
}

func TestDecodeSessionPreservesStructuredError(t *testing.T) {
	result, err := DecodeSession(http.StatusConflict, []byte(`{"error":{"code":"KIT_LEASE_BLOCKED","message":"another session owns the kit"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.HTTPStatus != http.StatusConflict || result.ErrorCode != "KIT_LEASE_BLOCKED" || result.ErrorMessage != "another session owns the kit" {
		t.Fatalf("result = %+v", result)
	}
}

func TestGetSessionUsesCallerClientAndRejectsOversize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("accept = %q", r.Header.Get("Accept"))
		}
		_, _ = io.WriteString(w, `{"state":"idle"}`+strings.Repeat("x", 32))
	}))
	defer server.Close()

	result, err := GetSession(context.Background(), server.Client(), server.URL, 16)
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("result = %+v, error = %v, want ErrResponseTooLarge", result, err)
	}
}
