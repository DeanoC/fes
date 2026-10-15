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
	m, err := decodeROMMap(ctx, bytes.NewReader(data), nil)
	if err != nil {
		return ROMMap{}, err
	}
	if err := validateROMMap(ctx, m, baseSHA256, sourceSize); err != nil {
		return ROMMap{}, err
	}
	return m, nil
}

// ValidateROMMapStream applies ParseROMMap's checks to a stream of size bytes.
// It does not retain the encoded map or decoded blocks. One fixed CRAM
// destination bitmap is allocated only after a block parses, then discarded
// with the result. Callers that only need admission, such as a catalog list,
// use this instead of buffering the member.
func ValidateROMMapStream(ctx context.Context, r io.Reader, size int64, baseSHA256 string, sourceSize int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if size <= 0 || size > MaxROMMapBytes {
		return fmt.Errorf("invalid ROM map size or UTF-8")
	}
	counted := &byteCounter{r: &utf8Reader{r: io.LimitReader(r, size)}}
	live := &romLive{}
	m, err := decodeROMMap(ctx, counted, live)
	if err != nil {
		return err
	}
	if counted.n != size {
		return fmt.Errorf("invalid ROM map size or UTF-8")
	}
	return live.finish(m.Format, m.Device, m.Encoding, m.BaseSHA256, m.SourceSize, baseSHA256, sourceSize)
}

// decodeROMMap walks one closed JSON object. live nil retains every block for
// ParseROMMap. A non-nil live checks each block and drops its word bits.
func decodeROMMap(ctx context.Context, raw io.Reader, live *romLive) (ROMMap, error) {
	// Retaining whole-map and block RawMessages multiplies the sealed map's
	// memory on the agent. Stream blocks and decode one fixed-size word.
	r := &romMapReader{ctx: ctx, reader: raw}
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
			if err := romMapScalar(d, &m.SourceSize); err != nil {
				return err
			}
			if live != nil {
				live.sourceSize = m.SourceSize
				live.haveSize = true
			}
			return nil
		case "blocks":
			if err := romMapDelimiter(d, '['); err != nil {
				return err
			}
			for d.More() {
				count := len(m.Blocks)
				if live != nil {
					count = live.blocks
				}
				if count == 256 {
					return fmt.Errorf("invalid ROM block count")
				}
				b, err := parseROMBlock(ctx, d)
				if err != nil {
					return err
				}
				if live != nil {
					if err := live.add(ctx, b); err != nil {
						return err
					}
				} else {
					m.Blocks = append(m.Blocks, b)
				}
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

type byteCounter struct {
	r io.Reader
	n int64
}

func (c *byteCounter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// utf8Reader rejects an invalid UTF-8 sequence without buffering the member.
// An incomplete trailing rune is held until the next read or EOF.
type utf8Reader struct {
	r    io.Reader
	held []byte
}

func (u *utf8Reader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for {
		space := len(p)
		if len(u.held) >= space {
			space = len(u.held) + 1
		}
		tmp := make([]byte, space-len(u.held))
		n, err := u.r.Read(tmp)
		if err != nil && err != io.EOF {
			return 0, err
		}
		if n == 0 && err == nil {
			return 0, nil
		}
		data := append(append([]byte{}, u.held...), tmp[:n]...)
		u.held = nil
		eof := err == io.EOF
		end := len(data)
		if !eof {
			end -= incompleteUTF8Suffix(data)
		}
		if !utf8.Valid(data[:end]) {
			return 0, fmt.Errorf("invalid ROM map size or UTF-8")
		}
		u.held = append([]byte{}, data[end:]...)
		if end == 0 {
			if eof {
				return 0, io.EOF
			}
			continue
		}
		copied := copy(p, data[:end])
		if copied < end {
			u.held = append(data[copied:end], u.held...)
		}
		if eof && len(u.held) == 0 {
			return copied, io.EOF
		}
		return copied, nil
	}
}

func incompleteUTF8Suffix(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	i := len(data) - 1
	for n := 1; n <= utf8.UTFMax && i >= 0; n++ {
		if utf8.RuneStart(data[i]) {
			if utf8.FullRune(data[i:]) {
				return 0
			}
			return len(data) - i
		}
		i--
	}
	return 0
}

type romMapReader struct {
	ctx      context.Context
	reader   io.Reader
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
