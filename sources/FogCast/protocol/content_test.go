package protocol_test

import (
	"encoding/json"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
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

func TestCacheIndexJSON(t *testing.T) {
	t.Parallel()
	index := protocol.CacheIndex{
		UsedBytes: 1024,
		MaxBytes:  2048,
		FreeBytes: 1024,
		Entries: []protocol.CacheIndexEntry{{
			System: protocol.SystemSNES, SHA256: testDigest, Size: 1024, Extension: "sfc",
		}},
	}
	got, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"used_bytes":1024,"max_bytes":2048,"free_bytes":1024,"entries":[{"system":"snes","sha256":"` + testDigest + `","size":1024,"extension":"sfc"}]}`
	if string(got) != want {
		t.Fatalf("JSON = %s, want %s", got, want)
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

	response, err := json.Marshal(protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive}, Content: content})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"status":{"state":"active","game_id":null,"system":null,"expected_core":null,"observed_core":null,"last_error":null},"content":{"sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","size":1024,"extension":"sfc"}}`; string(response) != want {
		t.Fatalf("response JSON = %s, want %s", response, want)
	}
}

func TestCachedIdentityResponseJSON(t *testing.T) {
	t.Parallel()
	absent, err := json.Marshal(protocol.CachedIdentityResponse{Present: false})
	if err != nil {
		t.Fatal(err)
	}
	if string(absent) != `{"present":false}` {
		t.Fatalf("absent JSON = %s", absent)
	}
	system := protocol.SystemSNES
	content := protocol.ContentIdentity{SHA256: testDigest, Size: 1024, Extension: "sfc"}
	present, err := json.Marshal(protocol.CachedIdentityResponse{Present: true, GameID: "snes-test", System: &system, Content: &content})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"present":true,"game_id":"snes-test","system":"snes","content":{"sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","size":1024,"extension":"sfc"}}`
	if string(present) != want {
		t.Fatalf("present JSON = %s, want %s", present, want)
	}
}

func TestContentIdentityKeyDropsSize(t *testing.T) {
	t.Parallel()

	identity := protocol.ContentIdentity{SHA256: testDigest, Size: 1234, Extension: "md"}
	if got, want := identity.Key(), (protocol.ContentKey{SHA256: testDigest, Extension: "md"}); got != want {
		t.Fatalf("Key() = %#v, want %#v", got, want)
	}
}
