package protocol_test

import (
	"encoding/json"
	"testing"

	"github.com/DeanoC/FogCast-POC/protocol"
)

const testDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestCacheProbeResponseJSON(t *testing.T) {
	t.Parallel()

	content := protocol.ContentIdentity{SHA256: testDigest, Size: 1024, Extension: "sfc"}
	system := protocol.SystemSNES
	tests := []struct {
		name  string
		value protocol.CacheProbeResponse
		want  string
	}{
		{name: "absent", value: protocol.CacheProbeResponse{Present: false}, want: `{"present":false}`},
		{name: "present", value: protocol.CacheProbeResponse{Present: true, System: &system, Content: &content}, want: `{"present":true,"system":"snes","content":{"sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","size":1024,"extension":"sfc"}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("JSON = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestCacheUploadResponseJSON(t *testing.T) {
	t.Parallel()

	for _, result := range []protocol.CacheUploadResult{protocol.CacheUploadPresent, protocol.CacheUploadCreated} {
		t.Run(string(result), func(t *testing.T) {
			got, err := json.Marshal(protocol.CacheUploadResponse{
				Result:  result,
				System:  protocol.SystemMegaDrive,
				Content: protocol.ContentIdentity{SHA256: testDigest, Size: 2048, Extension: "md"},
			})
			if err != nil {
				t.Fatal(err)
			}
			want := `{"result":"` + string(result) + `","system":"megadrive","content":{"sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","size":2048,"extension":"md"}}`
			if string(got) != want {
				t.Fatalf("JSON = %s, want %s", got, want)
			}
		})
	}
}

func TestCachedLaunchJSON(t *testing.T) {
	t.Parallel()

	content := protocol.ContentIdentity{SHA256: testDigest, Size: 1024, Extension: "sfc"}
	request, err := json.Marshal(protocol.CachedLaunchRequest{GameID: "snes-test", System: protocol.SystemSNES, Content: content})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"game_id":"snes-test","system":"snes","content":{"sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","size":1024,"extension":"sfc"}}`; string(request) != want {
		t.Fatalf("request JSON = %s, want %s", request, want)
	}

	response, err := json.Marshal(protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive}, Content: content})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"status":{"state":"active","game_id":null,"system":null,"expected_core":null,"observed_core":null,"last_error":null},"content":{"sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","size":1024,"extension":"sfc"}}`; string(response) != want {
		t.Fatalf("response JSON = %s, want %s", response, want)
	}
}

func TestContentIdentityKeyDropsSize(t *testing.T) {
	t.Parallel()

	identity := protocol.ContentIdentity{SHA256: testDigest, Size: 1234, Extension: "md"}
	if got, want := identity.Key(), (protocol.ContentKey{SHA256: testDigest, Extension: "md"}); got != want {
		t.Fatalf("Key() = %#v, want %#v", got, want)
	}
}
