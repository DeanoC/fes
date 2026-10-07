package misterruntime

import (
	"context"
	"testing"
)

func TestMouseRequestIdentityAndNoRetry(t *testing.T) {
	r := computerResponse(t)
	id := r.ActivePackage.PackageID
	fixture := newSequenceSocketFixture(t, []string{responseLine(t, r) + "\n"})
	if _, err := NewClient(fixture.path).SendMouseRelative(context.Background(), id, 7, -32768, 32767, 3); err != nil {
		t.Fatal(err)
	}
	req := fixture.wait(t)
	want := `{"protocol":2,"operation":"send_mouse_relative","package_id":"` + id + `","expected_generation":7,"dx":-32768,"dy":32767,"buttons":3}`
	if len(req) != 1 || req[0] != want {
		t.Fatalf("%q", req)
	}
	stale := newSequenceSocketFixture(t, []string{responseLine(t, r) + "\n"})
	if _, err := NewClient(stale.path).SendMouseRelative(context.Background(), id, 8, 1, 1, 0); err != errInvalidRuntimeResponse {
		t.Fatalf("stale %v", err)
	}
	if len(stale.wait(t)) != 1 {
		t.Fatal("replayed")
	}
	client := NewClient("/not/opened")
	for _, v := range []struct {
		id string
		g  uint64
		b  uint8
	}{{id, 0, 0}, {id, 7, 4}, {"bad", 7, 0}} {
		if _, err := client.SendMouseRelative(context.Background(), v.id, v.g, 0, 0, v.b); err != errInvalidRuntimeRequest {
			t.Fatal(err)
		}
	}
}
