package expansion

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var fixtureOnce sync.Once
var fixtureShell, fixtureCart []byte

func fixtures() ([]byte, []byte) {
	fixtureOnce.Do(func() {
		base := loadedRBF{header: make([]byte, headerBytes), cram: make([]byte, cramBytes)}
		for i := range base.header {
			base.header[i] = byte(i * 17)
		}
		for _, point := range [][2]int{{100, 64}, {2000, 100}, {3500, 80}} {
			setCramBit(base.cram, point[0], point[1], 1)
		}
		fixtureShell = saveRBF(base)
		for _, point := range [][2]int{{2000, 100}, {2799, 7020}, {1800, 77}, {3500, 80}} {
			setCramBit(base.cram, point[0], point[1], cramBit(base.cram, point[0], point[1])^1)
		}
		fixtureCart = saveRBF(base)
	})
	return bytes.Clone(fixtureShell), bytes.Clone(fixtureCart)
}

func digest(data []byte) string { value := sha256.Sum256(data); return hex.EncodeToString(value[:]) }

func TestPythonGolden(t *testing.T) {
	shell, cart := fixtures()
	linked, err := Link(shell, cart)
	if err != nil {
		t.Fatal(err)
	}
	golden := map[string]string{
		"shell":  "f38894e270e9e2771e157ebdf8767e92ad58d63de0764e7b03afdef0842ec5a0",
		"cart":   "ebf60623524409a830b87c6aa99f50b61e648bd268d74b63a5163eff417f9def",
		"linked": "8be0d02e30165a365e563e52c8d6adea541f68fd1941c8c98f88f480dedba5fd",
	}
	for name, data := range map[string][]byte{"shell": shell, "cart": cart, "linked": linked} {
		if digest(data) != golden[name] {
			t.Fatalf("%s differs from Python golden", name)
		}
	}
	// Optional direct cross-language comparison consumes files produced by
	// scripts/cyclonev_rbf.py; the recorded digests below run without Python.
	if directory := os.Getenv("FES_EXPANSION_PYTHON_GOLDEN"); directory != "" {
		for name, data := range map[string][]byte{"shell": shell, "cart": cart, "linked": linked} {
			expected, err := os.ReadFile(filepath.Join(directory, name+".rbf"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, expected) {
				t.Fatalf("%s differs from Python: got %s, want %s", name, digest(data), digest(expected))
			}
		}
	}
	t.Logf("shell=%s cart=%s linked=%s", digest(shell), digest(cart), digest(linked))
	decoded, err := loadRBF(linked)
	if err != nil {
		t.Fatal(err)
	}
	if cramBit(decoded.cram, 100, 64) != 1 || cramBit(decoded.cram, 2000, 100) != 0 ||
		cramBit(decoded.cram, 2799, 7020) != 1 || cramBit(decoded.cram, 1800, 77) != 1 || cramBit(decoded.cram, 3500, 80) != 1 {
		t.Fatal("overlay failed to preserve shell bits outside socket")
	}
}

func TestMalformedOrOutsideSocketRejects(t *testing.T) {
	shell, cart := fixtures()
	for name, mutate := range map[string]func([]byte) []byte{
		"truncated": func(b []byte) []byte { return b[:headerBytes-1] },
		"trailing":  func(b []byte) []byte { return append(b, 0) },
		"bad-frame": func(b []byte) []byte { b[headerBytes+10] ^= 0x40; return b },
		"header":    func(b []byte) []byte { b[12] ^= 1; return b },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Link(shell, mutate(bytes.Clone(cart))); err == nil {
				t.Fatal("invalid cart accepted")
			}
		})
	}
	decoded, err := loadRBF(cart)
	if err != nil {
		t.Fatal(err)
	}
	setCramBit(decoded.cram, 64, 100, 1)
	if _, err = Link(shell, saveRBF(decoded)); err == nil || !strings.Contains(err.Error(), "outside socket") {
		t.Fatalf("outside change: %v", err)
	}
}

func TestUncompressedInput(t *testing.T) {
	shell, cart := fixtures()
	framed, _, err := decompress(cart[headerBytes:])
	if err != nil {
		t.Fatal(err)
	}
	raw := append(bytes.Clone(cart[:headerBytes]), framed...)
	raw = append(raw, postamble()...)
	shellFrames, _, err := decompress(shell[headerBytes:])
	if err != nil {
		t.Fatal(err)
	}
	rawShell := append(bytes.Clone(shell[:headerBytes]), shellFrames...)
	rawShell = append(rawShell, postamble()...)
	shellBefore, cartBefore := bytes.Clone(rawShell), bytes.Clone(raw)
	result, err := Link(rawShell, raw)
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := Link(shell, cart)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result, compressed) {
		t.Fatal("compressed and uncompressed cart compose differently")
	}
	if !bytes.Equal(rawShell, shellBefore) || !bytes.Equal(raw, cartBefore) {
		t.Fatal("modified uncompressed caller bytes")
	}
}

func TestFramingRejectsRecomputedOuterCRC(t *testing.T) {
	shell, _ := fixtures()
	decoded, err := loadRBF(shell)
	if err != nil {
		t.Fatal(err)
	}
	for _, offset := range []int{0, 30, frameBytes - 8, frameBytes - 3} {
		framed := packFrames(decoded.cram)
		framed[offset] ^= 1
		binary.LittleEndian.PutUint16(framed[frameBytes-2:frameBytes], crc16(framed[:frameBytes-2]))
		invalid := append(bytes.Clone(decoded.header), compress(framed)...)
		invalid = append(invalid, postamble()...)
		if _, err := loadRBF(invalid); err == nil {
			t.Fatalf("accepted noncanonical frame at %d", offset)
		}
	}
}

func TestCRC16TableMatchesBitReference(t *testing.T) {
	reference := func(data []byte) uint16 {
		crc := uint16(0xffff)
		for _, value := range data {
			for bit := 0; bit < 8; bit++ {
				feedback := (uint16(value>>bit) ^ crc) & 1
				crc >>= 1
				if feedback != 0 {
					crc ^= 0xa001
				}
			}
		}
		return crc
	}
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i*73 + i/7)
	}
	for _, length := range []int{0, 1, 2, 190, 914, 916, 4096} {
		if got, want := crc16(data[:length]), reference(data[:length]); got != want {
			t.Fatalf("length %d CRC %04x != %04x", length, got, want)
		}
	}
}

func TestFrameDomainLinkMatchesBitOverlay(t *testing.T) {
	base := loadedRBF{header: make([]byte, headerBytes), cram: make([]byte, cramBytes)}
	for y := 32; y < cramHeight; y += 17 {
		for x := 0; x < cramWidth; x += 31 {
			setCramBit(base.cram, x, y, byte((x+y)%2))
		}
	}
	cart := loadedRBF{header: bytes.Clone(base.header), cram: bytes.Clone(base.cram)}
	for y := 32; y < cramHeight; y += 13 {
		for x := 1769; x < 2806; x += 11 {
			setCramBit(cart.cram, x, y, cramBit(cart.cram, x, y)^1)
		}
	}
	// Allowed companion differences must remain the original shell's bytes.
	setCramBit(cart.cram, 3500, 100, cramBit(cart.cram, 3500, 100)^1)
	shellBytes, cartBytes := saveRBF(base), saveRBF(cart)
	shellCopy, cartCopy := bytes.Clone(shellBytes), bytes.Clone(cartBytes)
	got, err := Link(shellBytes, cartBytes)
	if err != nil {
		t.Fatal(err)
	}
	for y := 32; y < cramHeight; y++ {
		for x := 1769; x < 2806; x++ {
			setCramBit(base.cram, x, y, cramBit(cart.cram, x, y))
		}
	}
	want := saveRBF(base)
	if !bytes.Equal(got, want) {
		t.Fatal("frame-domain output differs from bit-domain overlay")
	}
	if !bytes.Equal(shellBytes, shellCopy) || !bytes.Equal(cartBytes, cartCopy) {
		t.Fatal("modified caller bytes")
	}
}
