package expansion

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Synthetic placement isolates serialization and INIT encoding from extraction.
func romFixture() ([]byte, ROMMap) {
	m := ROMMap{Format: 1, Device: Device, Encoding: "m10k-1024x10-v1", SourceSize: 1024,
		Blocks: []ROMBlock{{BEL: "M10K.005.073", SourceOffset: 0, WordBits: make([][]uint32, 256)}}}
	base := loadedRBF{header: make([]byte, headerBytes), cram: make([]byte, cramBytes)}
	setCramBit(base.cram, 7000, 4000, 1)
	for w := range m.Blocks[0].WordBits {
		m.Blocks[0].WordBits[w] = make([]uint32, 40)
		for b := range m.Blocks[0].WordBits[w] {
			x, y := 315+w, 100+b
			m.Blocks[0].WordBits[w][b] = uint32(y*cramWidth + x)
			setCramBit(base.cram, x, y, 1)
		}
	}
	rbf := saveRBF(base)
	m.BaseSHA256 = digest(rbf)
	return rbf, m
}

func TestLinkROMEncodingAndIsolation(t *testing.T) {
	base, m := romFixture()
	original := bytes.Clone(base)
	rom := make([]byte, 1024)
	copy(rom, []byte{1, 2, 4, 8})
	out, err := LinkROM(context.Background(), base, m, rom)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := loadRBF(out)
	if err != nil {
		t.Fatal(err)
	}
	for w, bits := range m.Blocks[0].WordBits {
		for b, p := range bits {
			want := byte(1)
			// Logical bits 0,11,22,33 become stored bits 0,6,9,15.
			if w == 0 && (b == 0 || b == 6 || b == 9 || b == 15) {
				want = 0
			}
			if got := cramBit(decoded.cram, int(p)%cramWidth, int(p)/cramWidth); got != want {
				t.Fatalf("word %d bit %d = %d, want %d", w, b, got, want)
			}
		}
	}
	for _, bits := range m.Blocks[0].WordBits {
		for _, p := range bits {
			setCramBit(decoded.cram, int(p)%cramWidth, int(p)/cramWidth, 1)
		}
	}
	if !bytes.Equal(saveRBF(decoded), base) {
		t.Fatal("changed bits outside INIT")
	}
	if !bytes.Equal(base, original) {
		t.Fatal("mutated input")
	}
	noOp, err := LinkROM(context.Background(), base, m, make([]byte, 1024))
	if err != nil || !bytes.Equal(noOp, base) {
		t.Fatalf("zero ROM must be valid no-op: %v", err)
	}
}

func TestLinkROMRejectsInvalidMapAndInput(t *testing.T) {
	base, m := romFixture()
	cases := map[string]func(*ROMMap){
		"version":      func(m *ROMMap) { m.Format++ },
		"device":       func(m *ROMMap) { m.Device = "other" },
		"encoding":     func(m *ROMMap) { m.Encoding = "unknown" },
		"wrong base":   func(m *ROMMap) { m.BaseSHA256 = digest([]byte("wrong")) },
		"size":         func(m *ROMMap) { m.SourceSize++ },
		"offset":       func(m *ROMMap) { m.Blocks[0].SourceOffset = 1 },
		"missing word": func(m *ROMMap) { m.Blocks[0].WordBits = m.Blocks[0].WordBits[:255] },
		"short word":   func(m *ROMMap) { m.Blocks[0].WordBits[0] = m.Blocks[0].WordBits[0][:39] },
		"duplicate":    func(m *ROMMap) { m.Blocks[0].WordBits[0][0] = m.Blocks[0].WordBits[0][1] },
		"outside":      func(m *ROMMap) { m.Blocks[0].WordBits[0][0] = cramWidth * cramHeight },
		"framing":      func(m *ROMMap) { m.Blocks[0].WordBits[0][0] = 100 },
		"nonblank":     func(m *ROMMap) { m.Blocks[0].WordBits[0][0] = 500*cramWidth + 7000 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := m
			c.Blocks = append([]ROMBlock(nil), m.Blocks...)
			c.Blocks[0].WordBits = make([][]uint32, 256)
			for i, bits := range m.Blocks[0].WordBits {
				c.Blocks[0].WordBits[i] = append([]uint32(nil), bits...)
			}
			mutate(&c)
			if out, err := LinkROM(context.Background(), base, c, make([]byte, 1024)); err == nil || out != nil {
				t.Fatalf("invalid input accepted: %v", err)
			}
		})
	}
	if _, err := LinkROM(context.Background(), base, m, make([]byte, 1023)); err == nil {
		t.Fatal("short ROM accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LinkROM(ctx, base, m, make([]byte, 1024)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestLinkROMPreservesUncompressedFraming(t *testing.T) {
	base, m := romFixture()
	decoded, err := loadFrames(base)
	if err != nil {
		t.Fatal(err)
	}
	raw := append(bytes.Clone(decoded.header), decoded.frames...)
	raw = append(raw, postamble()...)
	m.BaseSHA256 = digest(raw)
	out, err := LinkROM(context.Background(), raw, m, make([]byte, 1024))
	if err != nil || !bytes.Equal(out, raw) {
		t.Fatalf("uncompressed no-op changed representation: %v", err)
	}
}

func readROMFixture(t testing.TB, name string) []byte {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "rom", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var r io.Reader = f
	if filepath.Ext(name) == ".gz" {
		z, err := gzip.NewReader(f)
		if err != nil {
			t.Fatal(err)
		}
		defer z.Close()
		r = z
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mistralFixture(t testing.TB) ([]byte, ROMMap) {
	t.Helper()
	var m ROMMap
	if err := json.Unmarshal(readROMFixture(t, "map.json.gz"), &m); err != nil {
		t.Fatal(err)
	}
	return readROMFixture(t, "blank.rbf.gz"), m
}

// Goldens were independently compiled and read back by Mistral; normal CI
// needs neither Python nor a compiler/database installation.
func TestROMMistralOracle(t *testing.T) {
	base, m := mistralFixture(t)
	var oracle struct {
		Cases []struct {
			Name      string
			ROMGzip   string `json:"rom_gzip"`
			ROMSHA256 string `json:"rom_sha256"`
			RBFSHA256 string `json:"rbf_sha256"`
			RBFSize   int    `json:"rbf_size"`
		}
	}
	if err := json.Unmarshal(readROMFixture(t, "oracle.json"), &oracle); err != nil {
		t.Fatal(err)
	}
	if len(oracle.Cases) != 5 {
		t.Fatal("incomplete oracle fixture")
	}
	for _, c := range oracle.Cases {
		t.Run(c.Name, func(t *testing.T) {
			rom := readROMFixture(t, c.ROMGzip)
			if digest(rom) != c.ROMSHA256 {
				t.Fatal("ROM fixture digest mismatch")
			}
			got, err := LinkROM(context.Background(), base, m, rom)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != c.RBFSize || digest(got) != c.RBFSHA256 {
				t.Fatalf("Mistral differs: got %s want %s", digest(got), c.RBFSHA256)
			}
		})
	}
}

func BenchmarkLinkROM(b *testing.B) {
	base, m := mistralFixture(b)
	rom := readROMFixture(b, "random.rom.gz")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := LinkROM(context.Background(), base, m, rom); err != nil {
			b.Fatal(err)
		}
	}
}
