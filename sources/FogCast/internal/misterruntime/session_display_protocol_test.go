package misterruntime

import (
	"strings"
	"testing"
)

func TestProtocol2SessionDisplayStatusIsStrictAndBackwardCompatible(t *testing.T) {
	base := `{"available":false,"package_id":"` + strings.Repeat("a", 64) + `","generation":0,"width":1280,"height":720,"stride":5120,"byte_count":3686400,"slot_bytes":4194304,"staging_format":"rgba8888","displayed_sequence":0,"underflows":0,"error":null`
	for _, suffix := range []string{`}`, `,"session":true,"core_generation":9}`} {
		if err := validateMenuDisplayShape([]byte(base + suffix)); err != nil {
			t.Fatalf("valid status rejected: %v", err)
		}
	}
	for _, suffix := range []string{`,"session":true}`, `,"core_generation":9}`, `,"session":false,"core_generation":9}`, `,"session":true,"core_generation":0}`, `,"session":true,"core_generation":"9"}`, `,"session":true,"core_generation":9,"visible":true}`} {
		if err := validateMenuDisplayShape([]byte(base + suffix)); err == nil {
			t.Fatalf("invalid status admitted: %s", suffix)
		}
	}
}
