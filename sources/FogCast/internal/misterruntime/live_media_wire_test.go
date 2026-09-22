package misterruntime

import (
	"context"
	"strings"
	"testing"
)

func TestReplaceLiveMediaWireShapeAndNoHoldResetLoadMedia(t *testing.T) {
	active := strings.ReplaceAll(strings.ReplaceAll(fixtureLines(t, "protocol-v2.jsonl")[5], "fes.simple-game", "fes.simple-computer"), "fes.gamepad", "fes.media.blob")
	for _, reply := range []string{active + "\n", "", "{}\n"} {
		fixture := newSequenceSocketFixture(t, []string{reply})
		response, err := NewClient(fixture.path).ReplaceLiveMedia(context.Background(), "/tmp/private/media.bin", strings.Repeat("a", 64), 9)
		if reply == active+"\n" {
			if err != nil || !response.OK {
				t.Fatalf("response=%+v error=%v", response, err)
			}
		} else if err == nil {
			t.Fatal("lost or malformed reply accepted")
		}
		requests := fixture.wait(t)
		want := `{"protocol":2,"operation":"replace_live_media","path":"/tmp/private/media.bin","expected_package_id":"` + strings.Repeat("a", 64) + `","expected_generation":9}`
		if len(requests) != 1 || requests[0] != want {
			t.Fatalf("wire requests: %v", requests)
		}
	}
}

func TestClearLiveMediaWireShape(t *testing.T) {
	active := strings.ReplaceAll(strings.ReplaceAll(fixtureLines(t, "protocol-v2.jsonl")[5], "fes.simple-game", "fes.simple-computer"), "fes.gamepad", "fes.media.blob")
	fixture := newSequenceSocketFixture(t, []string{active + "\n"})
	response, err := NewClient(fixture.path).ClearLiveMedia(context.Background(), strings.Repeat("a", 64), 9)
	if err != nil || !response.OK {
		t.Fatalf("response=%+v error=%v", response, err)
	}
	requests := fixture.wait(t)
	want := `{"protocol":2,"operation":"clear_media","expected_package_id":"` + strings.Repeat("a", 64) + `","expected_generation":9}`
	if len(requests) != 1 || requests[0] != want {
		t.Fatalf("wire requests: %v", requests)
	}
}
