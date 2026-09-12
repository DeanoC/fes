package misterruntime

import (
	"context"
	"strings"
	"testing"
)

func TestDevelopmentMediaWireShapeAndNoReplay(t *testing.T) {
	active := strings.ReplaceAll(strings.ReplaceAll(fixtureLines(t, "protocol-v2.jsonl")[5], "fes.simple-game", "fes.simple-computer"), "fes.gamepad", "fes.media.blob")
	for _, reply := range []string{active + "\n", "", "{}\n"} {
		fixture := newSequenceSocketFixture(t, []string{reply})
		response, err := NewClient(fixture.path).LoadMedia(context.Background(), "/tmp/private/media.bin")
		if reply == active+"\n" {
			if err != nil || !response.OK {
				t.Fatalf("response=%+v error=%v", response, err)
			}
		} else if err == nil {
			t.Fatal("lost or malformed reply accepted")
		}
		requests := fixture.wait(t)
		if len(requests) != 1 || requests[0] != `{"protocol":2,"operation":"load_media","path":"/tmp/private/media.bin"}` {
			t.Fatalf("wire requests: %v", requests)
		}
	}
}
