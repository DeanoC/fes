package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/kithost"
)

func TestOtherKitInvocationsFail(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := kitMain(context.Background(), []string{"--print-kit-ui"}, &stdout, &stderr, nil); code != 2 || stdout.Len() != 0 {
		t.Fatalf("code=%d stdout=%q", code, stdout.String())
	}
}

// fogcast-kit host is fogcast-api -kit -headless.
func TestKitHostRunsTheAPIInKitMode(t *testing.T) {
	// A set GOMEMLIMIT wins over kitmemory.SetLimit, so this test does not
	// lower the memory limit of the whole test binary.
	t.Setenv("GOMEMLIMIT", "off")
	var got kithost.Options
	stubKitHost(t, func(_ context.Context, options kithost.Options) (string, error) {
		got = options
		return "", context.Canceled
	})
	var stdout, stderr bytes.Buffer
	code := kitMain(context.Background(), []string{"host", "-kit-state", "/run/fogcast/fesdata3/k-test", "-listen", "127.0.0.1:0"}, &stdout, &stderr,
		func(context.Context, fogcast.Paths) (service, error) { return &scanCompositionService{}, nil })
	if code != 1 || got.StateRoot != "/run/fogcast/fesdata3/k-test" {
		t.Fatalf("code=%d options=%+v stderr=%q", code, got, stderr.String())
	}
}
