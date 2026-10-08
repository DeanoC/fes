package pack

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestMediaDataGolden(t *testing.T) {
	for _, sample := range []struct {
		name        string
		size, minor uint32
	}{{"atari-st.json", 737280, 0}, {"atari-st-geometry.json", 839680, 1}} {
		data, err := os.ReadFile("../../testdata/media-data-v1/" + sample.name)
		if err != nil {
			t.Fatal(err)
		}
		var vector struct {
			Core        string `json:"core_id"`
			Game        string `json:"game_id"`
			Base        string `json:"base_media_id"`
			Header      string `json:"header_hex"`
			Checksum    string `json:"checksum_hex"`
			Revision    string `json:"revision"`
			Namespace   string `json:"namespace"`
			PayloadHash string `json:"payload_sha256"`
			Size        int    `json:"record_size"`
			PayloadSize int    `json:"payload_size"`
		}
		if err := json.Unmarshal(data, &vector); err != nil {
			t.Fatal(err)
		}
		header, err := hex.DecodeString(vector.Header)
		if err != nil {
			t.Fatal(err)
		}
		if len(header) != 141 || string(header[:8]) != "FESDISK1" || string(header[116:]) != "fes.atari-st-floppy.image" ||
			binary.LittleEndian.Uint16(header[104:]) != 0 || binary.LittleEndian.Uint16(header[106:]) != 1 ||
			binary.LittleEndian.Uint16(header[108:]) != uint16(sample.minor) || binary.LittleEndian.Uint16(header[110:]) != 25 ||
			binary.LittleEndian.Uint32(header[112:]) != sample.size {
			t.Fatal("record header is not layout 1.0")
		}
		core, game := sha256.Sum256([]byte(vector.Core)), sha256.Sum256([]byte(vector.Game))
		base, err := hex.DecodeString(vector.Base)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(header[8:40], core[:]) || !bytes.Equal(header[40:72], game[:]) || !bytes.Equal(header[72:104], base) {
			t.Fatal("binding hashes differ")
		}
		payload := make([]byte, sample.size)
		for offset := range payload {
			payload[offset] = byte(offset*17 + (offset >> 9))
		}
		payloadHash := sha256.Sum256(payload)
		if hex.EncodeToString(payloadHash[:]) != vector.PayloadHash || vector.PayloadSize != len(payload) {
			t.Fatal("payload differs")
		}
		record := append(header, payload...)
		checksum := sha256.Sum256(record)
		if hex.EncodeToString(checksum[:]) != vector.Checksum {
			t.Fatal("checksum differs")
		}
		record = append(record, checksum[:]...)
		revision := sha256.Sum256(record)
		namespace := sha256.Sum256([]byte("fes-media-data-v1\x00" + vector.Core + "\x00" + vector.Game + "\x000\x00" + vector.Base))
		if len(record) != vector.Size || hex.EncodeToString(revision[:]) != vector.Revision || hex.EncodeToString(namespace[:]) != vector.Namespace {
			t.Fatal("record revision/namespace differs")
		}
	}
}
