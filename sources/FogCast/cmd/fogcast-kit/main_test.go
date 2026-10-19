package main

import (
	"bytes"
	"testing"
)

func TestPrintKitUIAlwaysSelectsTenfoot(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--config", "/media/fat/fogcast/launcher.json", "--print-kit-ui"}, &stdout, &stderr)
	if code != 0 || stdout.String() != "tenfoot\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestOtherInvocationsFail(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--menu-display"}, &stdout, &stderr); code == 0 || stdout.Len() != 0 {
		t.Fatalf("code=%d stdout=%q", code, stdout.String())
	}
}
