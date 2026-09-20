package expansion

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
)

const (
	Slot             = "fes.expansion.zx81-ram"
	Map              = "fes.zx81-ram.socket/1"
	Device           = "5CSEBA6U23I7"
	MaxManifestBytes = 65536
	MaxArchiveBytes  = maxRBFBytes + MaxManifestBytes + 4096
)

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)
var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)
var hex40 = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Manifest binds one separately built cart to one sealed frozen shell. The
// payload is the placed cart's complete RBF, never an unbounded patch recipe.
type Manifest struct {
	CartSHA256     string `json:"cart_sha256"`
	CartSize       int64  `json:"cart_size"`
	Device         string `json:"device"`
	Format         int    `json:"format"`
	Map            string `json:"map"`
	RecipeSHA256   string `json:"recipe_sha256"`
	Revision       string `json:"revision"`
	ShellBuildID   string `json:"shell_build_id"`
	ShellPackageID string `json:"shell_package_id"`
	ShellSHA256    string `json:"shell_sha256"`
	Slot           string `json:"slot"`
	SlotMajor      int    `json:"slot_major"`
	SlotMinor      int    `json:"slot_minor"`
}

func hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func (m Manifest) validate() error {
	if m.Format != 1 || m.Device != Device || m.Map != Map || m.Slot != Slot || m.SlotMajor != 1 || m.SlotMinor != 0 {
		return errors.New("unsupported expansion target, socket or version")
	}
	if !hex64.MatchString(m.CartSHA256) || !hex64.MatchString(m.ShellPackageID) ||
		!hex64.MatchString(m.ShellSHA256) || !hex64.MatchString(m.RecipeSHA256) ||
		!hex40.MatchString(m.Revision) || !hex32.MatchString(m.ShellBuildID) {
		return errors.New("invalid expansion identity")
	}
	if m.CartSize < headerBytes || m.CartSize > maxRBFBytes {
		return errors.New("invalid expansion payload size")
	}
	return nil
}

func (m Manifest) canonical() ([]byte, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// Asset is immutable after reading. Callers may persist ManifestBytes and Cart
// in an existing content store; Validate rechecks every byte before linking.
type Asset struct {
	Manifest      Manifest
	ManifestBytes []byte
	Cart          []byte
	ID            string
}

func (a Asset) Validate() error {
	canonical, err := a.Manifest.canonical()
	if err != nil {
		return err
	}
	if !bytes.Equal(canonical, a.ManifestBytes) {
		return errors.New("expansion manifest is not canonical")
	}
	if int64(len(a.Cart)) != a.Manifest.CartSize || hash(a.Cart) != a.Manifest.CartSHA256 {
		return errors.New("expansion payload does not match manifest")
	}
	if a.ID != hash(append([]byte("fes-expansion-v1\x00"), canonical...)) {
		return errors.New("expansion ID does not match manifest")
	}
	return nil
}

func NewAsset(manifest Manifest, cart []byte) (Asset, error) {
	canonical, err := manifest.canonical()
	if err != nil {
		return Asset{}, err
	}
	result := Asset{Manifest: manifest, ManifestBytes: canonical, Cart: bytes.Clone(cart),
		ID: hash(append([]byte("fes-expansion-v1\x00"), canonical...))}
	return result, result.Validate()
}

// ReadAsset accepts exactly two regular tar entries with bounded sizes. The
// canonical manifest requirement also rejects duplicate/unknown JSON fields.
func ReadAsset(input io.Reader) (Asset, error) {
	bounded := &io.LimitedReader{R: input, N: MaxArchiveBytes + 1}
	reader := tar.NewReader(bounded)
	files := map[string][]byte{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Asset{}, err
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return Asset{}, errors.New("expansion entries must be regular files")
		}
		limit := int64(maxRBFBytes)
		switch header.Name {
		case "manifest.json":
			limit = MaxManifestBytes
		case "cart.rbf":
		default:
			return Asset{}, errors.New("unexpected expansion member")
		}
		if _, exists := files[header.Name]; exists || header.Size < 1 || header.Size > limit {
			return Asset{}, errors.New("duplicate or oversized expansion member")
		}
		data, err := io.ReadAll(io.LimitReader(reader, limit+1))
		if err != nil {
			return Asset{}, err
		}
		if int64(len(data)) != header.Size {
			return Asset{}, errors.New("truncated expansion member")
		}
		files[header.Name] = data
	}
	// tar.Reader stops at its terminator; require remaining bytes to be zero
	// padding and bound the entire request, not just declared member lengths.
	padding, err := io.ReadAll(bounded)
	if err != nil {
		return Asset{}, err
	}
	if bounded.N == 0 {
		return Asset{}, errors.New("expansion archive exceeds size limit")
	}
	for _, value := range padding {
		if value != 0 {
			return Asset{}, errors.New("unexpected trailing expansion bytes")
		}
	}
	if len(files) != 2 {
		return Asset{}, errors.New("expansion requires manifest.json and cart.rbf")
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(files["manifest.json"]))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&manifest); err != nil {
		return Asset{}, err
	}
	result, err := NewAsset(manifest, files["cart.rbf"])
	if err != nil {
		return Asset{}, err
	}
	if !bytes.Equal(result.ManifestBytes, files["manifest.json"]) {
		return Asset{}, errors.New("expansion manifest is not canonical")
	}
	return result, nil
}

func (a Asset) Write(output io.Writer) error {
	if err := a.Validate(); err != nil {
		return err
	}
	archive := tar.NewWriter(output)
	for _, entry := range []struct {
		name string
		data []byte
	}{{"manifest.json", a.ManifestBytes}, {"cart.rbf", a.Cart}} {
		if err := archive.WriteHeader(&tar.Header{Name: entry.name, Mode: 0600, Size: int64(len(entry.data)), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		if _, err := archive.Write(entry.data); err != nil {
			return err
		}
	}
	return archive.Close()
}

type Shell struct {
	PackageID string
	BuildID   string
	Payload   []byte
	Slot      string
	SlotMajor int
	SlotMinor int
}

// Composition describes the exact bytes programmed while retaining the shell
// package and on-FPGA BUILD_ID. It is not a new independently sealed package.
type Composition struct {
	ID            string `json:"composition_id"`
	PackageID     string `json:"package_id"`
	ExpansionID   string `json:"expansion_id"`
	ShellSHA256   string `json:"shell_sha256"`
	PayloadSHA256 string `json:"payload_sha256"`
	PayloadSize   int64  `json:"payload_size"`
}

func CompositionID(packageID, expansionID, payloadSHA256 string) (string, error) {
	if !hex64.MatchString(packageID) || !hex64.MatchString(expansionID) || !hex64.MatchString(payloadSHA256) {
		return "", errors.New("invalid composition identity")
	}
	return hash([]byte("fes-composition-v1\x00" + packageID + "\x00" + expansionID + "\x00" + payloadSHA256)), nil
}

// Admit rejects incompatible or changed inputs without running the linker.
func Admit(shell Shell, asset Asset) error {
	if err := asset.Validate(); err != nil {
		return err
	}
	if shell.Slot != Slot || shell.SlotMajor != 1 || shell.SlotMinor != 0 {
		return errors.New("shell does not declare the supported RAM socket")
	}
	if shell.PackageID != asset.Manifest.ShellPackageID || shell.BuildID != asset.Manifest.ShellBuildID ||
		hash(shell.Payload) != asset.Manifest.ShellSHA256 {
		return errors.New("expansion was built for a different frozen shell")
	}
	return nil
}

func Compose(shell Shell, asset Asset) (Composition, []byte, error) {
	if err := Admit(shell, asset); err != nil {
		return Composition{}, nil, err
	}
	linked, err := Link(shell.Payload, asset.Cart)
	if err != nil {
		return Composition{}, nil, fmt.Errorf("link expansion: %w", err)
	}
	result := Composition{PackageID: shell.PackageID, ExpansionID: asset.ID, ShellSHA256: asset.Manifest.ShellSHA256,
		PayloadSHA256: hash(linked), PayloadSize: int64(len(linked))}
	result.ID, err = CompositionID(result.PackageID, result.ExpansionID, result.PayloadSHA256)
	return result, linked, err
}
