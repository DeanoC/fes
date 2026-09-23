package expansion

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestParseROMMapClosedAndBound(t *testing.T) {
	_, m := mistralFixture(t)
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseROMMap(context.Background(), raw, m.BaseSHA256, m.SourceSize); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"duplicate":      bytes.Replace(raw, []byte(`"format":1`), []byte(`"format":1,"format":1`), 1),
		"unknown":        bytes.Replace(raw, []byte(`"format":1`), []byte(`"format":1,"extra":1`), 1),
		"missing offset": bytes.Replace(raw, []byte(`"source_offset":0,`), nil, 1),
		"null offset":    bytes.Replace(raw, []byte(`"source_offset":0`), []byte(`"source_offset":null`), 1),
		"trailing":       append(bytes.Clone(raw), '0'),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseROMMap(context.Background(), data, m.BaseSHA256, m.SourceSize); err == nil {
				t.Fatal("invalid map accepted")
			}
		})
	}
	if _, err := ParseROMMap(context.Background(), raw, digest([]byte("wrong")), m.SourceSize); err == nil {
		t.Fatal("unbound map accepted")
	}
}
