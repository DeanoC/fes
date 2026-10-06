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

// A producer word needs at most 40 ten-digit destinations plus punctuation.
// Bound any single JSON value/key, including its surrounding whitespace, so a
// malformed row cannot make encoding/json buffer the entire encoded map.
const maxROMMapValueBytes = 4 << 10

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
	// Retaining whole-map and block RawMessages multiplies the sealed map's
	// memory on the agent. Stream blocks and decode one fixed-size word.
	r := &romMapReader{ctx: ctx, reader: bytes.NewReader(data)}
	d := json.NewDecoder(r)
	r.consumed = d.InputOffset
	var m ROMMap
	err := romMapObject(ctx, d, []string{"format", "device", "encoding", "base_sha256", "source_size", "blocks"}, func(key string) error {
		switch key {
		case "format":
			return romMapScalar(d, &m.Format)
		case "device":
			return romMapScalar(d, &m.Device)
		case "encoding":
			return romMapScalar(d, &m.Encoding)
		case "base_sha256":
			return romMapScalar(d, &m.BaseSHA256)
		case "source_size":
			return romMapScalar(d, &m.SourceSize)
		case "blocks":
			if err := romMapDelimiter(d, '['); err != nil {
				return err
			}
			for d.More() {
				if len(m.Blocks) == 256 {
					return fmt.Errorf("invalid ROM block count")
				}
				b, err := parseROMBlock(ctx, d)
				if err != nil {
					return err
				}
				m.Blocks = append(m.Blocks, b)
			}
			return romMapDelimiter(d, ']')
		}
		return nil
	})
	if err != nil {
		return ROMMap{}, err
	}
	if _, err := d.Token(); err != io.EOF {
		if err != nil {
			return ROMMap{}, fmt.Errorf("trailing ROM map data: %w", err)
		}
		return ROMMap{}, fmt.Errorf("trailing ROM map data")
	}
	if err := validateROMMap(ctx, m, baseSHA256, sourceSize); err != nil {
		return ROMMap{}, err
	}
	return m, nil
}

func parseROMBlock(ctx context.Context, d *json.Decoder) (ROMBlock, error) {
	var b ROMBlock
	err := romMapObject(ctx, d, []string{"bel", "source_offset", "word_bits"}, func(key string) error {
		switch key {
		case "bel":
			return romMapScalar(d, &b.BEL)
		case "source_offset":
			return romMapScalar(d, &b.SourceOffset)
		case "word_bits":
			if err := romMapDelimiter(d, '['); err != nil {
				return err
			}
			b.WordBits = make([][]uint32, 0, 256)
			for d.More() {
				if err := ctx.Err(); err != nil {
					return err
				}
				if len(b.WordBits) == 256 {
					return fmt.Errorf("ROM block must have 256 words")
				}
				var word romMapWord
				if err := d.Decode(&word); err != nil {
					return err
				}
				b.WordBits = append(b.WordBits, word[:])
			}
			if err := romMapDelimiter(d, ']'); err != nil {
				return err
			}
			if len(b.WordBits) != 256 {
				return fmt.Errorf("ROM block must have 256 words")
			}
		}
		return nil
	})
	return b, err
}

// A fixed array bounds decoded destinations. Count first: encoding/json
// otherwise silently discards excess elements when decoding into a Go array.
type romMapWord [40]uint32

func (word *romMapWord) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) < 2 || data[0] != '[' || data[len(data)-1] != ']' || bytes.Count(data, []byte(",")) != 39 {
		return fmt.Errorf("ROM word must have 40 destinations")
	}
	return json.Unmarshal(data, (*[40]uint32)(word))
}

func romMapScalar[T int | string](d *json.Decoder, output *T) error {
	var value *T
	if err := d.Decode(&value); err != nil {
		return err
	}
	if value == nil {
		return fmt.Errorf("null ROM map field")
	}
	*output = *value
	return nil
}

func romMapDelimiter(d *json.Decoder, want byte) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	if token != json.Delim(want) {
		return fmt.Errorf("ROM map requires %c", want)
	}
	return nil
}

func romMapObject(ctx context.Context, d *json.Decoder, keys []string, field func(string) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := romMapDelimiter(d, '{'); err != nil {
		return err
	}
	seen := make([]bool, len(keys))
	count := 0
	for d.More() {
		if err := ctx.Err(); err != nil {
			return err
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		index := -1
		for i, candidate := range keys {
			if candidate == key {
				index = i
				break
			}
		}
		if !ok || index < 0 || seen[index] {
			return fmt.Errorf("duplicate or unknown ROM map key %v", token)
		}
		seen[index] = true
		count++
		if err := field(key); err != nil {
			return fmt.Errorf("ROM map %s: %w", key, err)
		}
	}
	if err := romMapDelimiter(d, '}'); err != nil {
		return err
	}
	if count != len(keys) {
		return fmt.Errorf("missing ROM map fields")
	}
	return nil
}

type romMapReader struct {
	ctx      context.Context
	reader   *bytes.Reader
	consumed func() int64
	read     int64
}

func (r *romMapReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	remaining := int64(maxROMMapValueBytes) - (r.read - r.consumed())
	if remaining <= 0 {
		return 0, fmt.Errorf("ROM map JSON value exceeds %d bytes", maxROMMapValueBytes)
	}
	if int64(len(data)) > remaining {
		data = data[:remaining]
	}
	n, err := r.reader.Read(data)
	r.read += int64(n)
	return n, err
}
