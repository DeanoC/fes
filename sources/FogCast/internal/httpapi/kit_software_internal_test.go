package httpapi

import (
	"strings"
	"testing"
)

func TestDecodeSoftwareStatusRejectsOversizedValidDocument(t *testing.T) {
	id := strings.Repeat("a", 64)
	body := `{"version":1,"generation":0,"selected":"` + id + `","receipts":{},"active_bundle":"` + id + `","base_identity":"` + id + `"}`
	if _, ok := decodeSoftwareStatus([]byte(body)); !ok {
		t.Fatal("valid status rejected")
	}
	body += strings.Repeat(" ", maxSoftwareControlResponse)
	if status, ok := decodeSoftwareStatus([]byte(body)); ok || status.Selected != "" {
		t.Fatal("oversized status published selection")
	}
}
