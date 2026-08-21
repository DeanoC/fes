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
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache control = %q", got)
	}
}

func TestUIEscapesGameTextInBrowserTemplate(t *testing.T) {
	html := hostapi.UIHTMLForTest()
	for _, token := range []string{"textContent", "createElement", "replaceChildren"} {
		if !strings.Contains(html, token) {
			t.Fatalf("assembled UI is missing safe DOM token %q", token)
		}
	}
	for _, token := range []string{".innerHTML", ".outerHTML", "insertAdjacentHTML", "document.write", "eval("} {
		if strings.Contains(html, token) {
			t.Fatalf("assembled UI contains forbidden unsafe DOM token %q", token)
		}
	}
}

func TestUIIsSelfContainedAndHasLauncherStates(t *testing.T) {
	html := hostapi.UIHTMLForTest()
	if got := strings.Count(html, "<style>"); got != 1 {
		t.Fatalf("expected exactly one inline style, got %d", got)
	}
	for _, token := range []string{"<script>", "FogCastMetadata", "gamesPath", "launchRequest"} {
		if !strings.Contains(html, token) {
			t.Fatalf("assembled UI is missing expected inline asset token %q", token)
		}
	}
	for _, token := range []string{"<script src", "<link rel=\"stylesheet\"", "http://", "https://", "//fonts.", "@import", "url(", "onclick=", "oninput="} {
		if strings.Contains(html, token) {
			t.Fatalf("assembled UI contains forbidden external-resource token %q", token)
		}
	}
	for _, token := range []string{
		"loading", "empty", "no_matches", "catalog_error", "retry", "metadata_fallback",
		"launching", "launch_success", "launch_error",
	} {
		if !strings.Contains(html, token) {
			t.Fatalf("assembled UI is missing launcher state/label token %q", token)
		}
	}
}

func TestUIUsesOnlyExistingAPIEndpointFamilies(t *testing.T) {
	html := hostapi.UIHTMLForTest()
	for _, token := range []string{
		"/api/v1/games",
		"/api/v1/session/launch",
		"/api/v1/platforms",
		"/api/v1/library/favorites/",
		"/api/v1/library/collections",
		"/api/v1/library/attract",
		"/api/v1/library/settings",
		"/api/v1/presentation/media/",
	} {
		if !strings.Contains(html, token) {
			t.Fatalf("assembled UI is missing API endpoint family %q", token)
		}
	}
	if strings.Contains(html, "/api/v1/health") || strings.Contains(html, "/api/v1/status") {
		t.Fatal("assembled UI references an API endpoint outside the approved families")
	}
}

func TestUISemanticAccessibilityAndResponsiveShell(t *testing.T) {
	html := hostapi.UIHTMLForTest()
	for _, token := range []string{
		"viewport", "<main", "<h1", "aria-label", "aria-live=\"polite\"", "aria-busy",
		"<button", "<input", ":focus-visible", "@media",
		`id="keyboard-help"`, "Type to search", "data-keyboard-pane",
	} {
		if !strings.Contains(html, token) {
			t.Fatalf("assembled UI is missing accessibility/responsive token %q", token)
		}
	}
}

func TestUIFixRoundAccessibilityContracts(t *testing.T) {
	html := hostapi.UIHTMLForTest()
	for _, token := range []string{
		`id="launch-status"`, `aria-live="polite"`, `heading.id = 'detail-heading';`,
		`aria-labelledby="detail-heading"`, `aria-pressed`,
		`launching`, `launch_success`, `launch_error`,
	} {
		if !strings.Contains(html, token) {
			t.Fatalf("assembled UI is missing fix-round accessibility contract %q", token)
		}
	}
	if strings.Contains(html, `class="detail-empty"`) {
		t.Fatal("assembled UI retains a non-replaceable initial detail prompt")
	}
	for _, token := range []string{
		`nodes.launchStatus.textContent = presentation.text;`,
		`nodes.launchStatus.setAttribute('role', presentation.role);`,
		`const selected = isSelectedGame(state.selectedLiveGame, game);`,
		`card.setAttribute('aria-pressed', String(selected));`,
		`const heading = element('h2', '', detailHeading(game));`,
		`nodes.detailContent.appendChild(heading);`,
	} {
		if !strings.Contains(html, token) {
			t.Fatalf("assembled UI is missing exact fix-round source contract %q", token)
		}
	}
}
