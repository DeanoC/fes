package kitlauncher

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
)

func TestKeyboardCapabilityMatchesSharedDecoder(t *testing.T) {
	cases := []struct {
		name string
		core string
		want bool
	}{
		{"null-package", "null", false},
		{"missing-interfaces", `{"generation":7}`, false},
		{"empty-interfaces", `{"generation":7,"active_interfaces":[]}`, false},
		{"null-interfaces", `{"generation":7,"active_interfaces":null}`, false},
		{"supported", `{"generation":7,"active_interfaces":[{"id":"fes.keyboard","major":1,"minor":0}]}`, true},
		{"other-interface", `{"generation":7,"active_interfaces":[{"id":"fes.gamepad","major":1,"minor":0}]}`, false},
		{"old-major", `{"generation":7,"active_interfaces":[{"id":"fes.keyboard","major":0,"minor":0}]}`, false},
		{"future-major", `{"generation":7,"active_interfaces":[{"id":"fes.keyboard","major":2,"minor":0}]}`, false},
		{"future-minor", `{"generation":7,"active_interfaces":[{"id":"fes.keyboard","major":1,"minor":1}]}`, false},
		{"exact-id", `{"generation":7,"active_interfaces":[{"id":" fes.keyboard","major":1,"minor":0}]}`, false},
		{"case-sensitive-id", `{"generation":7,"active_interfaces":[{"id":"FES.KEYBOARD","major":1,"minor":0}]}`, false},
		{"missing-version", `{"generation":7,"active_interfaces":[{"id":"fes.keyboard"}]}`, false},
		{"omitted-zero-minor", `{"generation":7,"active_interfaces":[{"id":"fes.keyboard","major":1}]}`, true},
		{"mixed", `{"generation":7,"active_interfaces":[{"id":"fes.keyboard","major":2,"minor":0},{"id":"fes.gamepad","major":1,"minor":0},{"id":"fes.keyboard","major":1,"minor":0}]}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"state":"active","execution":"fpga_development","input":{"state":"attached","ready":true,"session_id":"keyboard-test"},"core_package":` + tc.core + `}`)
			decoded, err := hostclient.DecodeSession(http.StatusOK, body)
			if err != nil {
				t.Fatal(err)
			}
			if decoded.CoreKeyboard != tc.want {
				t.Fatalf("decoded keyboard = %v, want %v", decoded.CoreKeyboard, tc.want)
			}
			var direct Session
			if err := json.Unmarshal(body, &direct); err != nil {
				t.Fatal(err)
			}
			for name, session := range map[string]Session{"adapted": adaptSession(decoded), "direct": direct} {
				if got := session.CorePackage.HasKeyboard(); got != tc.want {
					t.Errorf("%s keyboard = %v, want %v", name, got, tc.want)
				}
			}
		})
	}
}
