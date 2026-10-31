package pack

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
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

func TestAtaDiskDeltaGolden(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/media-data-v1/ata-disk-delta.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Core      string `json:"core_id"`
		Game      string `json:"game_id"`
		Base      string `json:"base_media_id"`
		BaseSize  int    `json:"base_size"`
		ChunkSize int    `json:"chunk_size"`
		Chunks    []struct {
			Index, Bytes int
			Sha256, File string
		}
		Record    string `json:"record_hex"`
		Revision  string
		Namespace string
		Restored  string `json:"restored_sha256"`
		CRC       uint32 `json:"restored_crc32"`
		Invalid   []struct{ Name, Hex string }
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	decode := func(data []byte) ([][2]string, bool) {
		if len(data) < 174 || string(data[:8]) != "FESDELT1" {
			return nil, false
		}
		sum := sha256.Sum256(data[:len(data)-32])
		core, game := sha256.Sum256([]byte(v.Core)), sha256.Sum256([]byte(v.Game))
		base, _ := hex.DecodeString(v.Base)
		if !bytes.Equal(sum[:], data[len(data)-32:]) || !bytes.Equal(data[8:40], core[:]) ||
			!bytes.Equal(data[40:72], game[:]) || !bytes.Equal(data[72:104], base) {
			return nil, false
		}
		le16 := func(at int) int { return int(binary.LittleEndian.Uint16(data[at:])) }
		le32 := func(at int) int { return int(binary.LittleEndian.Uint32(data[at:])) }
		size, count := le32(112), le32(120)
		if le16(104) != 0 || le16(106) != 1 || le16(108) != 0 || le16(110) != 18 || le32(116) != 65536 ||
			string(data[124:142]) != "fes.ata-disk.delta" || size%512 != 0 || size < 1<<20 || size > 128<<20 ||
			len(data) != 174+36*count {
			return nil, false
		}
		var entries [][2]string
		last := -1
		for at := 142; at < 142+36*count; at += 36 {
			index := le32(at)
			if index <= last || index*65536 >= size {
				return nil, false
			}
			last = index
			entries = append(entries, [2]string{fmt.Sprint(index), hex.EncodeToString(data[at+4 : at+36])})
		}
		return entries, true
	}
	record, _ := hex.DecodeString(v.Record)
	entries, ok := decode(record)
	if !ok || len(entries) != len(v.Chunks) || v.ChunkSize != 65536 {
		t.Fatal("golden manifest rejected")
	}
	image := make([]byte, v.BaseSize)
	for offset := range image {
		image[offset] = byte(offset*7 + offset>>11)
	}
	if sum := sha256.Sum256(image); hex.EncodeToString(sum[:]) != v.Base {
		t.Fatal("base pattern differs")
	}
	for i, c := range v.Chunks {
		data := make([]byte, c.Bytes)
		for offset := range data {
			data[offset] = byte(c.Index*31 + offset*13 + offset>>8)
		}
		sum := sha256.Sum256(data)
		want := v.BaseSize - c.Index*65536
		if want > 65536 {
			want = 65536
		}
		if entries[i] != [2]string{fmt.Sprint(c.Index), hex.EncodeToString(sum[:])} || c.Bytes != want ||
			c.File != "chunk-"+hex.EncodeToString(sum[:]) {
			t.Fatalf("chunk %d differs", c.Index)
		}
		copy(image[c.Index*65536:], data)
	}
	restored := sha256.Sum256(image)
	revision := sha256.Sum256(record)
	namespace := sha256.Sum256([]byte("fes-media-data-v1\x00" + v.Core + "\x00" + v.Game + "\x000\x00" + v.Base))
	if hex.EncodeToString(restored[:]) != v.Restored || crc32.ChecksumIEEE(image) != v.CRC ||
		hex.EncodeToString(revision[:]) != v.Revision || hex.EncodeToString(namespace[:]) != v.Namespace {
		t.Fatal("restored image, revision or namespace differs")
	}
	for _, row := range v.Invalid {
		data, _ := hex.DecodeString(row.Hex)
		if _, ok := decode(data); ok {
			t.Errorf("invalid %s accepted", row.Name)
		}
	}
}
