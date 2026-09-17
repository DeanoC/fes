package misterruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
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

func TestMediaStreamWireShapeAndObservation(t *testing.T) {
	active := strings.ReplaceAll(strings.ReplaceAll(fixtureLines(t, "protocol-v2.jsonl")[5], "fes.simple-game", "fes.simple-computer"), "fes.gamepad", "fes.media.blob")
	active = strings.ReplaceAll(active, `{"id":"fes.media.blob","major":1,"minor":0}`, `{"id":"fes.media.blob","major":1,"minor":0},{"id":"fes.media.blob-stream","major":1,"minor":0}`)
	active = strings.ReplaceAll(active, `{"id":"fes.media.blob","major":1,"minor":0,"required":true}`, `{"id":"fes.media.blob","major":1,"minor":0,"required":true},{"id":"fes.media.blob-stream","major":1,"minor":0,"required":true}`)
	capability, _ := json.Marshal(protocol.MediaStreamCapability{Interface: protocol.MediaStreamInterface(), MinBytes: 1, MaxBytes: 32768, ChunkBytes: 512})
	active = strings.Replace(active, `"capabilities":{`, `"capabilities":{"media_stream":`+string(capability)+`,`, 1)
	for _, reply := range []string{active + "\n", "", "{}\n", strings.Replace(active, `"chunk_bytes":512`, `"chunk_bytes":256`, 1) + "\n", strings.Replace(active, string(capability), "null", 1) + "\n"} {
		fixture := newSequenceSocketFixture(t, []string{reply})
		response, err := NewClient(fixture.path).LoadMediaStream(context.Background(), "/tmp/private/media.bin", strings.Repeat("a", 64), 9, 32768)
		if reply == active+"\n" {
			if err != nil || !response.Capabilities.MediaStream.Valid() {
				t.Fatalf("response=%+v error=%v", response, err)
			}
		} else if err == nil {
			t.Fatal("invalid/lost response accepted")
		}
		requests := fixture.wait(t)
		want := `{"protocol":2,"operation":"load_media_stream","path":"/tmp/private/media.bin","expected_package_id":"` + strings.Repeat("a", 64) + `","expected_generation":9,"size":32768}`
		if len(requests) != 1 || requests[0] != want {
			t.Fatalf("requests=%v", requests)
		}
	}
}

func TestMediaStreamDecoderConsumesRuntimeSerializerFixtures(t *testing.T) {
	lines := fixtureLines(t, "protocol-v2-media-stream-responses.jsonl")
	if len(lines) != 3 {
		t.Fatalf("expected active stream, legacy and idle serializer fixtures; got %d", len(lines))
	}
	for i, line := range lines {
		response, err := decodeProtocol2Response([]byte(line))
		if err != nil {
			t.Fatalf("runtime media fixture line %d: %v", i+1, err)
		}
		switch i {
		case 0:
			capability := response.Capabilities.MediaStream
			if !capability.Valid() || capability.MinBytes != 1 || capability.MaxBytes != 32768 || capability.ChunkBytes != 512 || response.ActivePackage == nil || response.Generation == nil || *response.Generation != 7 {
				t.Fatalf("stream observation lost: %+v", response)
			}
			activation := activationFromProtocol2(response.ActivePackage.PackageID, response.ActivePackage.Descriptor, response)
			if !protocol.MediaStreamCapable(corePackageStatus(activation)) {
				t.Fatal("runtime serializer observation lost in target status projection")
			}
		case 1:
			if response.Capabilities.MediaStream != nil || response.ActivePackage == nil || response.Generation == nil || *response.Generation != 8 {
				t.Fatalf("legacy observation changed: %+v", response)
			}
		case 2:
			if response.Capabilities.MediaStream != nil || response.ActivePackage != nil || response.Generation != nil || response.State != "idle" {
				t.Fatalf("idle observation changed: %+v", response)
			}
		}
	}
}
