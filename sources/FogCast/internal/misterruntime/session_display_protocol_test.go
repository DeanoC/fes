package misterruntime

import (
	"strings"
	"testing"
)

func TestProtocol2SessionDisplayStatusIsStrictAndBackwardCompatible(t *testing.T) {
	base := `{"available":false,"package_id":"` + strings.Repeat("a", 64) + `","generation":0,"width":1280,"height":720,"stride":5120,"byte_count":3686400,"slot_bytes":4194304,"slots":2,"format":"bgrx8888","displayed_sequence":0,"underflows":0,"error":null`
	for _, suffix := range []string{`}`, `,"session":true,"core_generation":9,"focus":"machine"}`, `,"session":true,"core_generation":9,"focus":"launcher"}`,
		`,"session":true,"core_generation":9,"focus":"machine","picture":[312,90,656,540]}`, `,"session":true,"core_generation":9,"focus":"launcher","picture":[0,0,1280,720]}`} {
		if err := validateMenuDisplayShape([]byte(base + suffix)); err != nil {
			t.Fatalf("valid status rejected: %s: %v", suffix, err)
		}
	}
	const session = `,"session":true,"core_generation":9,"focus":"machine"`
	for _, suffix := range []string{`,"session":true}`, `,"core_generation":9}`, `,"session":false,"core_generation":9,"focus":"machine"}`, `,"session":true,"core_generation":0,"focus":"machine"}`,
		`,"session":true,"core_generation":"9","focus":"machine"}`, session + `,"visible":true}`, `,"damage":true}`,
		`,"session":true,"core_generation":9}`, `,"session":true,"core_generation":9,"focus":"menu"}`, `,"session":true,"core_generation":9,"focus":null}`,
		`,"session":true,"core_generation":9,"focus":true}`, `,"focus":"machine"}`,
		`,"picture":[312,90,656,540]}`, session + `,"picture":[312,90,656]}`,
		session + `,"picture":[312,90,656,540,999]}`, session + `,"picture":[312,90,656,540,0]}`,
		session + `,"picture":[]}`,
		session + `,"picture":[312,90,0,540]}`, session + `,"picture":[1000,90,656,540]}`,
		session + `,"picture":[0,200,1280,540]}`, session + `,"picture":null}`} {
		if err := validateMenuDisplayShape([]byte(base + suffix)); err == nil {
			t.Fatalf("invalid status admitted: %s", suffix)
		}
	}
}
