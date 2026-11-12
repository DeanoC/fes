package kitpriority

import "testing"

func TestManagedOnlyUnderKitSupervisor(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"1", true},
		{"", false},
		{"0", false},
		{"true", false},
	} {
		getenv := func(key string) string {
			if key == managedEnv {
				return tc.value
			}
			return ""
		}
		if got := managed(getenv); got != tc.want {
			t.Errorf("managed with %s=%q = %v, want %v", managedEnv, tc.value, got, tc.want)
		}
	}
}
