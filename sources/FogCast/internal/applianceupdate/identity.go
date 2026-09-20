package applianceupdate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	release "github.com/DeanoC/FogCast/appliance"
)

// ReadBootIdentity accepts only the bootstrap's ticket for this actual boot.
// The identity comes from the bootstrap, never the candidate's own manifest.
func ReadBootIdentity(ticketPath, bootIDPath string) (BootIdentity, error) {
	var result BootIdentity
	f, err := os.Open(ticketPath)
	if err != nil {
		return result, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return result, err
	}
	if len(data) > 4096 {
		return result, ErrIdentity
	}
	d := json.NewDecoder(bytes.NewReader(data))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		return result, ErrIdentity
	}
	seen := map[string]bool{}
	for d.More() {
		tok, err = d.Token()
		if err != nil {
			return result, err
		}
		name, ok := tok.(string)
		if !ok || seen[name] {
			return result, ErrIdentity
		}
		seen[name] = true
		switch name {
		case "boot_id":
			err = d.Decode(&result.BootID)
		case "image_sha256":
			err = d.Decode(&result.ImageSHA256)
		case "trial":
			var trial *bool
			err = d.Decode(&trial)
			if trial == nil {
				return result, ErrIdentity
			}
			result.Trial = *trial
		default:
			return result, errors.New("unknown bootstrap boot ticket field")
		}
		if err != nil {
			return result, err
		}
	}
	if _, err = d.Token(); err != nil {
		return result, err
	}
	if _, err = d.Token(); err != io.EOF {
		return result, ErrIdentity
	}
	boot, err := os.ReadFile(bootIDPath)
	if err != nil {
		return result, err
	}
	if len(seen) != 3 || result.BootID == "" || result.BootID != strings.TrimSpace(string(boot)) || !release.ValidHash(result.ImageSHA256) {
		return result, ErrIdentity
	}
	return result, nil
}
