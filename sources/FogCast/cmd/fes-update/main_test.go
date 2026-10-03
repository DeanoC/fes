package main

import (
	"strings"
	"testing"
	"time"
)

func TestChooseDeadline(t *testing.T) {
	cases := []struct {
		action         string
		explicit       bool
		supplied, want time.Duration
		size           int64
	}{
		{action: "update", size: 128 << 20, want: 21 * time.Minute},
		{action: "update", explicit: true, supplied: 2 * time.Minute, size: 128 << 20, want: 2 * time.Minute},
		{action: "rollback", want: 10 * time.Minute},
		{action: "status", want: time.Minute},
		{action: "confirm", want: 15 * time.Minute},
	}
	for _, tc := range cases {
		got, _ := chooseDeadline(tc.action, tc.explicit, tc.supplied, tc.size)
		if got != tc.want {
			t.Errorf("%s explicit=%t: got %s, want %s", tc.action, tc.explicit, got, tc.want)
		}
	}
}

func TestValidateConfirmFlags(t *testing.T) {
	hash := strings.Repeat("a", 64)
	cases := []struct {
		release, hash string
		ok            bool
	}{
		{ok: false},
		{release: "dir", ok: true},
		{hash: hash, ok: true},
		{release: "dir", hash: hash, ok: false},
		{hash: strings.Repeat("A", 64), ok: false},
		{hash: strings.Repeat("a", 63), ok: false},
	}
	for _, tc := range cases {
		err := validateActionFlags("confirm", tc.release, tc.hash)
		if (err == nil) != tc.ok {
			t.Errorf("release=%q hash=%q: err=%v", tc.release, tc.hash, err)
		}
	}
}
