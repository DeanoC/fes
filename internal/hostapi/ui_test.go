package hostapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/hostapi"
)

func TestUIHandlerReturnsSelfContainedHTML(t *testing.T) {
	response := httptest.NewRecorder()
	hostapi.UIHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "FogCast") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
}

func TestUIEscapesGameTextInBrowserTemplate(t *testing.T) {
	html := hostapi.UIHTMLForTest()
	if !strings.Contains(html, "textContent") && !strings.Contains(html, "esc(") {
		t.Fatal("UI does not contain an explicit text escaping path")
	}
}
