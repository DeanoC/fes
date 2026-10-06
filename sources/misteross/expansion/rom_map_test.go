package expansion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
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

func TestParseROMMapStreamingBounds(t *testing.T) {
	_, m := mistralFixture(t)
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	word, err := json.Marshal(m.Blocks[0].WordBits[0])
	if err != nil {
		t.Fatal(err)
	}
	for name, replacement := range map[string][]byte{
		"short word":       []byte(strings.TrimSuffix(string(word), "]")[:strings.LastIndex(string(word), ",")] + "]"),
		"long word":        append(bytes.Clone(word[:len(word)-1]), []byte(",1]")...),
		"null word":        []byte("null"),
		"nested word":      bytes.Replace(word, []byte("["), []byte("[["), 1),
		"null destination": bytes.Replace(word, word[1:bytes.IndexByte(word, ',')], []byte("null"), 1),
	} {
		t.Run(name, func(t *testing.T) {
			data := bytes.Replace(raw, word, replacement, 1)
			if _, err := ParseROMMap(context.Background(), data, m.BaseSHA256, m.SourceSize); err == nil {
				t.Fatal("invalid word accepted")
			}
		})
	}
	for name, data := range map[string][]byte{
		"null blocks":           bytes.Replace(raw, []byte(`"blocks":[`), []byte(`"blocks":null,"unused":[`), 1),
		"duplicate block field": bytes.Replace(raw, []byte(`"source_offset":0`), []byte(`"source_offset":0,"source_offset":0`), 1),
		"unknown block field":   bytes.Replace(raw, []byte(`"source_offset":0`), []byte(`"source_offset":0,"extra":0`), 1),
		"null root":             []byte("null"),
		"extra root":            append(bytes.Clone(raw), []byte(" {}")...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseROMMap(context.Background(), data, m.BaseSHA256, m.SourceSize); err == nil {
				t.Fatal("invalid shape accepted")
			}
		})
	}

	// Refuse the 257th word/block while decoding, before allocating a larger
	// collection or discovering duplicate destinations during validation.
	tooManyWords := m
	tooManyWords.Blocks = append([]ROMBlock(nil), m.Blocks...)
	tooManyWords.Blocks[0].WordBits = append(append([][]uint32(nil), m.Blocks[0].WordBits...), m.Blocks[0].WordBits[0])
	tooManyBlocks := m
	tooManyBlocks.Blocks = make([]ROMBlock, 257)
	for i := range tooManyBlocks.Blocks {
		tooManyBlocks.Blocks[i] = m.Blocks[0]
	}
	for name, candidate := range map[string]ROMMap{"words": tooManyWords, "blocks": tooManyBlocks} {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			_, err = ParseROMMap(context.Background(), data, m.BaseSHA256, m.SourceSize)
			if err == nil || !strings.Contains(err.Error(), map[string]string{"words": "256 words", "blocks": "block count"}[name]) {
				t.Fatalf("collection limit not enforced during decoding: %v", err)
			}
		})
	}
	for name, data := range map[string][]byte{
		"row":    bytes.Replace(raw, word, append([]byte("["+strings.Repeat(" ", maxROMMapValueBytes)), word[1:]...), 1),
		"key":    bytes.Replace(raw, []byte(`"format"`), []byte(`"`+strings.Repeat("x", maxROMMapValueBytes+1)+`"`), 1),
		"scalar": bytes.Replace(raw, []byte(`"device":"`), []byte(`"device":"`+strings.Repeat("x", maxROMMapValueBytes+1)), 1),
	} {
		t.Run("oversized "+name, func(t *testing.T) {
			_, err := ParseROMMap(context.Background(), data, m.BaseSHA256, m.SourceSize)
			if err == nil || !strings.Contains(err.Error(), "JSON value exceeds") {
				t.Fatalf("encoded value limit not enforced: %v", err)
			}
		})
	}
	// Ordinary producer indentation and reordered closed fields remain valid.
	var reordered map[string]any
	if err := json.Unmarshal(raw, &reordered); err != nil {
		t.Fatal(err)
	}
	indented, err := json.MarshalIndent(reordered, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseROMMap(context.Background(), indented, m.BaseSHA256, m.SourceSize); err != nil {
		t.Fatalf("reordered, indented producer map rejected: %v", err)
	}
}

type romMapCancelContext struct {
	context.Context
	checks, cancelAt int
}

func (c *romMapCancelContext) Err() error {
	c.checks++
	if c.checks >= c.cancelAt {
		return context.Canceled
	}
	return nil
}

func TestParseROMMapCancellationDuringWords(t *testing.T) {
	_, m := mistralFixture(t)
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &romMapCancelContext{Context: context.Background(), cancelAt: 100}
	if _, err := ParseROMMap(ctx, raw, m.BaseSHA256, m.SourceSize); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation during streaming decode: %v", err)
	}
}

func TestParseROMMapCancellationAtEOF(t *testing.T) {
	_, m := mistralFixture(t)
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	probe := &romMapCancelContext{Context: context.Background(), cancelAt: int(^uint(0) >> 1)}
	if _, err := ParseROMMap(probe, raw, m.BaseSHA256, m.SourceSize); err != nil {
		t.Fatal(err)
	}
	// Validation checks once per block. Cancel the preceding EOF
	// read, after every JSON field and word was already decoded.
	ctx := &romMapCancelContext{Context: context.Background(), cancelAt: probe.checks - len(m.Blocks)}
	if _, err := ParseROMMap(ctx, raw, m.BaseSHA256, m.SourceSize); !errors.Is(err, context.Canceled) {
		t.Fatalf("late decoding cancellation lost its identity: %v", err)
	}
}
