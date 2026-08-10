package hostapi

import (
	_ "embed"
	"net/http"
	"strings"
)

//go:embed ui_shell.html
var uiShell string

//go:embed ui.css
var uiStyles string

//go:embed ui_metadata.js
var uiMetadata string

//go:embed ui_app.js
var uiApp string

var uiHTML = assembleUI()

func assembleUI() string {
	assets := map[string]string{
		"{{FOGCAST_STYLES}}":   "<style>" + uiStyles + "</style>",
		"{{FOGCAST_METADATA}}": "<script>" + uiMetadata + "</script>",
		"{{FOGCAST_APP}}":      "<script>" + uiApp + "</script>",
	}
	result := uiShell
	for placeholder, asset := range assets {
		if strings.Count(result, placeholder) != 1 {
			panic("hostapi: UI asset placeholder must occur exactly once: " + placeholder)
		}
		result = strings.Replace(result, placeholder, asset, 1)
	}
	return result
}

func UIHTMLForTest() string { return uiHTML }

func UIHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(uiHTML))
	})
}
