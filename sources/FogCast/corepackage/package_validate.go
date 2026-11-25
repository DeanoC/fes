package corepackage

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

// ValidateDescriptor applies every payload-independent field rule from the
// format-2 manifest contract. Readers use it before binding the declared
// payload size and digest to the bytes they opened; protocol projections reuse
// it so they cannot drift from package admission semantics.
func ValidateDescriptor(d Descriptor) error {
	if d.Format != 2 && d.Format != 3 && d.Format != 4 && d.Format != 5 {
		return errors.New("core package: unsupported format")
	}
	if err := validateStaticExpansion(d); err != nil {
		return err
	}
	if (d.Format == 3) != (d.ROM != nil) {
		return errors.New("core package: ROM declaration requires format 3")
	}
	if d.Format == 4 {
		if len(d.ROMs) != 2 || d.ROMMap == nil {
			return errors.New("format 4 requires two ROM sources and a map")
		}
		total := int64(0)
		for i, r := range d.ROMs {
			if err := identifier(r.ID, "roms.id"); err != nil {
				return err
			}
			if (i == 0 && r.Role != "firmware") || (i == 1 && r.Role != "cartridge") || r.SourceSize < 1024 || r.SourceSize > 262144 || r.SourceSize%1024 != 0 || r.SourceOffset != total {
				return errors.New("invalid ordered ROM requirements")
			}
			total += r.SourceSize
		}
		if d.ROMs[0].ID == d.ROMs[1].ID || total > 262144 {
			return errors.New("duplicate ROM ID or combined source too large")
		}
		m := d.ROMMap
		if m.File != "rom-map.json" || m.Size < 1 || m.Size > MaxROMMapSize || !hex64RE.MatchString(m.SHA256) {
			return errors.New("invalid ROM map metadata")
		}
	} else if len(d.ROMs) != 0 || d.ROMMap != nil {
		return errors.New("two-source ROM fields require format 4")
	}
	if r := d.ROM; r != nil {
		if err := identifier(r.ID, "rom.id"); err != nil {
			return err
		}
		if r.Role != "firmware" && r.Role != "cartridge" {
			return errors.New("invalid ROM role")
		}
		if r.SourceSize < 1024 || r.SourceSize > 262144 || r.SourceSize%1024 != 0 || r.File != "rom-map.json" || r.Size < 1 || r.Size > MaxROMMapSize || !hex64RE.MatchString(r.SHA256) {
			return errors.New("invalid ROM metadata")
		}
	}
	if err := identifier(d.Core.ID, "core.id"); err != nil {
		return err
	}
	if err := text(d.Core.Name, "core.name", true, 128); err != nil {
		return err
	}
	if err := text(d.Core.Description, "core.description", false, 2048); err != nil {
		return err
	}
	if !semverRE.MatchString(d.Core.Version) {
		return errors.New("core package: core.version must be full SemVer")
	}
	if d.Core.System != "" {
		if err := identifier(d.Core.System, "core.system"); err != nil {
			return err
		}
	}
	if err := identifier(d.Target.Platform, "target.platform"); err != nil {
		return err
	}
	if err := text(d.Target.Device, "target.device", true, 0); err != nil {
		return err
	}
	if err := identifier(d.Target.ProgrammingProfile, "target.programming_profile"); err != nil {
		return err
	}
	knownTarget := d.Target.Platform == "de10_nano" && d.Target.Device == "5CSEBA6U23I7" ||
		(d.Format == 2 || d.Format == 5 || nanoROM(d)) &&
			d.Target.Platform == "de25_nano" && d.Target.Device == "A5EB013BB23BE4SCS"
	if !knownTarget {
		return errors.New("core package: unsupported target")
	}
	if nanoROM(d) {
		if d.ROM.Role != "firmware" || d.ROM.Size > maxNanoROMMapBytes {
			return errors.New("core package: Nano requires a bounded firmware ROM map")
		}
		encoding, ok := nanoROMEncoding(d)
		if !ok {
			return errors.New("core package: Nano ROM requires exactly one firmware transport interface at 1.0")
		}
		if encoding == "cpu-csr-v1" && d.ROM.SourceSize != 4096 && d.ROM.SourceSize != 8192 {
			return errors.New("core package: Nano held ROM is exactly 4 or 8 KiB")
		}
	}
	if d.Payload.File != "core.rbf" {
		return errors.New("core package: payload.file must be core.rbf")
	}
	if d.Payload.Size < 1 || d.Payload.Size > MaxPayloadSize {
		return errors.New("core package: payload.size is out of bounds")
	}
	if !hex64RE.MatchString(d.Payload.SHA256) {
		return errors.New("core package: invalid payload.sha256")
	}
	if err := contract(d.ABI, "abi"); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(d.Interfaces))
	for index, value := range d.Interfaces {
		field := fmt.Sprintf("interfaces[%d]", index)
		if err := identifier(value.ID, field+".id"); err != nil {
			return err
		}
		if value.Major < 1 || value.Major > 65535 || value.Minor < 0 || value.Minor > 65535 {
			return fmt.Errorf("core package: %s version is out of bounds", field)
		}
		if _, exists := seen[value.ID]; exists {
			return fmt.Errorf("core package: duplicate interface id %s", value.ID)
		}
		seen[value.ID] = struct{}{}
	}
	if !hex32RE.MatchString(d.Build.ID) {
		return errors.New("core package: invalid build.id")
	}
	if !validRepository(d.Build.Repository) {
		return errors.New("core package: invalid build.repository")
	}
	if !hex40RE.MatchString(d.Build.Revision) {
		return errors.New("core package: invalid build.revision")
	}
	if !hex64RE.MatchString(d.Build.RecipeSHA256) {
		return errors.New("core package: invalid build.recipe_sha256")
	}
	if err := text(d.Build.Toolchain, "build.toolchain", true, 1024); err != nil {
		return err
	}
	return nil
}

func validateDescriptor(d Descriptor, payload []byte) error {
	if err := ValidateDescriptor(d); err != nil {
		return err
	}
	if int64(len(payload)) != d.Payload.Size {
		return errors.New("core package: payload size does not match manifest")
	}
	digest := sha256.Sum256(payload)
	if hex.EncodeToString(digest[:]) != d.Payload.SHA256 {
		return errors.New("core package: payload digest does not match manifest")
	}
	return nil
}

func contract(value Contract, field string) error {
	if err := identifier(value.ID, field+".id"); err != nil {
		return err
	}
	if value.Major < 1 || value.Major > 65535 || value.Minor < 0 || value.Minor > 65535 {
		return fmt.Errorf("core package: %s version is out of bounds", field)
	}
	return nil
}

func identifier(value, field string) error {
	if !identifierRE.MatchString(value) {
		return fmt.Errorf("core package: invalid %s", field)
	}
	return nil
}

func text(value, field string, nonempty bool, maximum int) error {
	if nonempty && value == "" {
		return fmt.Errorf("core package: %s must not be empty", field)
	}
	if maximum > 0 && len([]byte(value)) > maximum {
		return fmt.Errorf("core package: %s exceeds %d UTF-8 bytes", field, maximum)
	}
	for _, character := range value {
		if character < 32 || (character >= 127 && character <= 159) {
			return fmt.Errorf("core package: %s contains a control character", field)
		}
	}
	return nil
}

func validRepository(value string) bool {
	if !strings.HasPrefix(value, "https://") {
		return false
	}
	rest := value[len("https://"):]
	end := strings.IndexAny(rest, "/?#")
	authority := rest
	suffix := ""
	if end >= 0 {
		authority, suffix = rest[:end], rest[end:]
	}
	if authority == "" || strings.Contains(authority, "@") || !validAuthority(authority) {
		return false
	}
	path, query, fragment, ok := splitSuffix(suffix)
	return ok && validURIText(path, false) && validURIText(query, true) && validURIText(fragment, true)
}

func validAuthority(authority string) bool {
	host, port := authority, ""
	literal := false
	if strings.HasPrefix(authority, "[") {
		closing := strings.IndexByte(authority, ']')
		if closing < 0 {
			return false
		}
		host = authority[:closing+1]
		remaining := authority[closing+1:]
		if remaining != "" {
			if !strings.HasPrefix(remaining, ":") {
				return false
			}
			port = remaining[1:]
		}
		inside := host[1 : len(host)-1]
		parsed := net.ParseIP(inside)
		if (parsed == nil || !strings.Contains(inside, ":")) &&
			!ipvFutureRE.MatchString(inside) {
			return false
		}
		literal = true
	} else if colon := strings.LastIndexByte(authority, ':'); colon >= 0 {
		host, port = authority[:colon], authority[colon+1:]
		if strings.Contains(host, ":") {
			return false
		}
	}
	for _, digit := range port {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	if literal {
		return true
	}
	return validComponent(host, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_.~-!$&'()*+,;=%", true)
}

func splitSuffix(value string) (string, string, string, bool) {
	if strings.Count(value, "#") > 1 {
		return "", "", "", false
	}
	fragment := ""
	if at := strings.IndexByte(value, '#'); at >= 0 {
		fragment, value = value[at+1:], value[:at]
	}
	query := ""
	if at := strings.IndexByte(value, '?'); at >= 0 {
		query, value = value[at+1:], value[:at]
	}
	if value != "" && value[0] != '/' {
		return "", "", "", false
	}
	return value, query, fragment, true
}

func validURIText(value string, query bool) bool {
	allowed := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_.~-!$&'()*+,;=:@"
	if query {
		allowed += "/?"
	} else {
		allowed += "/"
	}
	return validComponent(value, allowed, true)
}

func validComponent(value, allowed string, percent bool) bool {
	for index := 0; index < len(value); index++ {
		if value[index] == '%' && percent {
			if index+2 >= len(value) || !isHex(value[index+1]) || !isHex(value[index+2]) {
				return false
			}
			index += 2
			continue
		}
		if value[index] >= utf8.RuneSelf || !strings.ContainsRune(allowed, rune(value[index])) {
			return false
		}
	}
	return true
}

func isHex(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
}

func packageIdentity(manifest, payload []byte, maps ...[]byte) string {
	digest := sha256.New()
	domain := "FES-CORE-PACKAGE-2\n"
	var mapping []byte
	if len(maps) == 1 {
		mapping = maps[0]
	}
	if mapping != nil {
		domain = "FES-CORE-PACKAGE-3\n"
		var header struct {
			Format int64 `toml:"format"`
		}
		if err := toml.Unmarshal(manifest, &header); err == nil {
			if header.Format == 4 {
				domain = "FES-CORE-PACKAGE-4\n"
			}
			if header.Format == 5 {
				domain = "FES-CORE-PACKAGE-5\n"
			}
		}
	}
	_, _ = digest.Write([]byte(domain))
	var length [8]byte
	binary.LittleEndian.PutUint64(length[:], uint64(len(manifest)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write(manifest)
	binary.LittleEndian.PutUint64(length[:], uint64(len(payload)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write(payload)
	if mapping != nil {
		binary.LittleEndian.PutUint64(length[:], uint64(len(mapping)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write(mapping)
	}
	return hex.EncodeToString(digest.Sum(nil))
}
