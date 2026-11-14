package corepackage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

// Nano format 3 retains one firmware image for the CPU-fed held-ROM endpoint.
// This map names no physical address, bridge grant or bitstream destinations.
const maxNanoROMMapBytes = 1024

func nanoROM(d Descriptor) bool {
	return d.Format == 3 && d.Target.Platform == "de25_nano" && d.Target.Device == "A5EB013BB23BE4SCS"
}

func validateNanoROMMap(ctx context.Context, r io.Reader, size int64, d Descriptor) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !nanoROM(d) || d.ROM == nil || size < 1 || size > maxNanoROMMapBytes {
		return errors.New("invalid held-ROM map scope or bound")
	}
	raw, err := io.ReadAll(io.LimitReader(r, size+1))
	if err != nil {
		return err
	}
	if int64(len(raw)) != size || !utf8.Valid(raw) {
		return errors.New("invalid held-ROM map length or UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return errors.New("held-ROM map requires an object")
	}
	seen := make(map[string]bool)
	var format, sourceSize int64
	var device, encoding, base string
	for decoder.More() {
		token, err = decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return errors.New("invalid or duplicate held-ROM map key")
		}
		seen[key] = true
		switch key {
		case "format":
			err = decoder.Decode(&format)
		case "device":
			err = decoder.Decode(&device)
		case "encoding":
			err = decoder.Decode(&encoding)
		case "base_sha256":
			err = decoder.Decode(&base)
		case "source_size":
			err = decoder.Decode(&sourceSize)
		default:
			return errors.New("unknown held-ROM map key")
		}
		if err != nil {
			return err
		}
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') {
		return errors.New("incomplete held-ROM map")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || len(seen) != 5 || format != 1 ||
		device != d.Target.Device || encoding != "cpu-csr-v1" ||
		base != d.Payload.SHA256 || sourceSize != d.ROM.SourceSize {
		return errors.New("held-ROM map differs from sealed package")
	}
	return ctx.Err()
}
