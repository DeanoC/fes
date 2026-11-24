package tenfoot

import "testing"

func TestManagedKitHostSelectsKeyboardAuthority(t *testing.T) {
	tests := []struct {
		name          string
		kit, explicit bool
		managed       string
		want          bool
	}{
		{"managed kit automatic", true, false, "1", true},
		{"managed kit explicit false cannot opt out", true, false, "1", true},
		{"unmanaged kit unchanged", true, false, "", false},
		{"unmanaged explicit opt in", true, true, "", true},
		{"noncanonical managed marker", true, false, "true", false},
		{"paired host managed environment unchanged", false, false, "1", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := managedNativeKeyboardOptions(Options{KitHost: tc.kit, NativeKeyboardV2: tc.explicit}, tc.managed)
			if got.NativeKeyboardV2 != tc.want || got.KitHost != tc.kit {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestNativeKeyboardServiceDisabledForSmokeAndNoInput(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
		want bool
	}{
		{"enabled", Options{NativeKeyboardV2: true, Input: "auto"}, true},
		{"smoke", Options{NativeKeyboardV2: true, Smoke: true, Input: "auto"}, false},
		{"none", Options{NativeKeyboardV2: true, Input: " none "}, false},
		{"disabled", Options{Input: "auto"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := nativeKeyboardInputEnabled(tc.opts); got != tc.want {
				t.Fatalf("got %v", got)
			}
		})
	}
}
