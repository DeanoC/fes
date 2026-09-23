package expansion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

// MaxROMMapBytes bounds the encoded producer map before JSON decoding.
const MaxROMMapBytes = 32 << 20

// ParseROMMap validates the closed map syntax and binds its destinations to
// manifest metadata. It does not authenticate a package or decode the RBF;
// LinkROM checks the actual blank bits before applying any ROM bytes.
func ParseROMMap(ctx context.Context, data []byte, baseSHA256 string, sourceSize int) (ROMMap, error) {
	if err := ctx.Err(); err != nil {
		return ROMMap{}, err
	}
	if len(data) == 0 || len(data) > MaxROMMapBytes || !utf8.Valid(data) {
		return ROMMap{}, fmt.Errorf("invalid ROM map size or UTF-8")
	}
	fields, err := closedObject(data, []string{"format", "device", "encoding", "base_sha256", "source_size", "blocks"})
	if err != nil {
		return ROMMap{}, err
	}
	var m ROMMap
	for key, dst := range map[string]any{"format": &m.Format, "device": &m.Device, "encoding": &m.Encoding, "base_sha256": &m.BaseSHA256, "source_size": &m.SourceSize} {
		if err := json.Unmarshal(fields[key], dst); err != nil {
			return ROMMap{}, fmt.Errorf("ROM map %s: %w", key, err)
		}
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(fields["blocks"], &blocks); err != nil {
		return ROMMap{}, err
	}
	if len(blocks) < 1 || len(blocks) > 256 {
		return ROMMap{}, fmt.Errorf("invalid ROM block count")
	}
	for _, raw := range blocks {
		if err := ctx.Err(); err != nil {
			return ROMMap{}, err
		}
		f, err := closedObject(raw, []string{"bel", "source_offset", "word_bits"})
		if err != nil {
			return ROMMap{}, err
		}
		var b ROMBlock
		for key, dst := range map[string]any{"bel": &b.BEL, "source_offset": &b.SourceOffset, "word_bits": &b.WordBits} {
			if err := json.Unmarshal(f[key], dst); err != nil {
				return ROMMap{}, fmt.Errorf("ROM block %s: %w", key, err)
			}
		}
		m.Blocks = append(m.Blocks, b)
	}
	if err := validateROMMap(ctx, m, baseSHA256, sourceSize); err != nil {
		return ROMMap{}, err
	}
	return m, nil
}

// Unlike unmarshalling into a struct, this rejects duplicate keys and missing
// zero-valued fields such as source_offset, as well as null and unknown fields.
func closedObject(data []byte, keys []string) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("ROM map requires an object")
	}
	allowed := make(map[string]bool, len(keys))
	for _, k := range keys {
		allowed[k] = true
	}
	result := make(map[string]json.RawMessage, len(keys))
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok || !allowed[key] || result[key] != nil {
			return nil, fmt.Errorf("duplicate or unknown ROM map key %v", token)
		}
		var raw json.RawMessage
		if err = d.Decode(&raw); err != nil {
			return nil, err
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, fmt.Errorf("null ROM map field %s", key)
		}
		result[key] = raw
	}
	if _, err = d.Token(); err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing ROM map data")
	}
	if len(result) != len(keys) {
		return nil, fmt.Errorf("missing ROM map fields")
	}
	return result, nil
}
