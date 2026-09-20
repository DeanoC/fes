package protocol

import (
	"encoding/json"
	"github.com/DeanoC/FogCast/corepackage"
	"os"
	"testing"
)

func TestMediaStreamCapabilityFrozenBounds(t *testing.T) {
	for _, tc := range []struct {
		name            string
		min, max, chunk uint32
		valid           bool
	}{
		{"guaranteed", 1, 32768, 512, true},
		{"ceiling", 1, 33554432, 512, true},
		{"min two", 2, 32768, 512, false},
		{"legacy max", 1, 16384, 512, false},
		{"above ceiling", 1, 33554433, 512, false},
		{"small chunk", 1, 32768, 256, false},
		{"large chunk", 1, 32768, 513, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &MediaStreamCapability{Interface: MediaStreamInterface(), MinBytes: tc.min, MaxBytes: tc.max, ChunkBytes: tc.chunk}
			if c.Valid() != tc.valid {
				t.Fatalf("Valid(%+v) = %v", c, c.Valid())
			}
		})
	}
	var absent *MediaStreamCapability
	if absent.Valid() {
		t.Fatal("missing capability accepted")
	}
	c := &MediaStreamCapability{Interface: MediaStreamInterface(), MinBytes: 1, MaxBytes: 32768, ChunkBytes: 512}
	c.Interface.Minor++
	if c.Valid() {
		t.Fatal("unknown version accepted")
	}
}

func TestDeclaredStreamRequiresBothExactRequiredInterfaces(t *testing.T) {
	for _, tc := range []struct {
		name           string
		legacy, stream bool
		major          uint16
		want           int64
	}{
		{"required pair", true, true, 1, 32768},
		{"optional legacy", false, true, 1, 16384},
		{"optional stream", true, false, 1, 16384},
		{"unknown stream", true, true, 2, 16384},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := corepackage.Descriptor{Core: corepackage.Core{ID: "example.not-sms"}, ABI: corepackage.Contract{ID: "fes.simple-computer", Major: 1}, Interfaces: []corepackage.Interface{
				{ID: "fes.media.blob", Major: 1, Required: tc.legacy},
					{ID: MediaStreamInterface().ID, Major: int64(tc.major), Required: tc.stream},
			}}
			got := DeclaredCoreMediaCapabilities(d)
			if len(got) != 1 || got[0].MaxBytes != tc.want {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestSharedStreamSizeVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/fes-media-stream-v1/exchanges.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Endpoint struct {
			Min   uint32
			Max   uint32
			Chunk uint32 `json:"chunk_max"`
		}
		Sizes []struct {
			Total  int64
			Accept bool `json:"sms_accept"`
		} `json:"size_vectors"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	c := &MediaStreamCapability{Interface: MediaStreamInterface(), MinBytes: fixture.Endpoint.Min, MaxBytes: fixture.Endpoint.Max, ChunkBytes: fixture.Endpoint.Chunk}
	if !c.Valid() {
		t.Fatal("canonical endpoint rejected")
	}
	if len(fixture.Sizes) == 0 {
		t.Fatal("no shared size vectors")
	}
	for _, v := range fixture.Sizes {
		accepted := v.Total >= int64(c.MinBytes) && v.Total <= MaxDeclaredMediaStreamBytes
		if accepted != v.Accept {
			t.Fatalf("size %d accepted=%v want=%v", v.Total, accepted, v.Accept)
		}
	}
}
