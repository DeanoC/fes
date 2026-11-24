package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
)

type RuntimeInputKind string

const (
	RuntimeInputDescribed   RuntimeInputKind = "described"
	RuntimeInputDevelopment RuntimeInputKind = "development"
)

// RuntimeInputIdentity is one validated runtime instance/package/generation
// observation. Development explicitly uses an empty package ID; a missing
// identity is not an empty-package binding.
type RuntimeInputIdentity struct {
	Kind            RuntimeInputKind `json:"kind"`
	RuntimeInstance string           `json:"runtime_instance"`
	PackageID       string           `json:"package_id"`
	Generation      uint64           `json:"generation"`
}

var runtimeInputInstance = regexp.MustCompile(`^[0-9a-f]{32}$`)
var errRuntimeInputIdentity = errors.New("invalid runtime input identity")

// ValidInputAgentInstance identifies the input controller incarnation, not a
// kit boot or runtime process. Empty and all-zero identifiers are unavailable.
func ValidInputAgentInstance(value string) bool {
	return runtimeInputInstance.MatchString(value) && value != "00000000000000000000000000000000"
}

func (v RuntimeInputIdentity) Validate() error {
	if !ValidInputAgentInstance(v.RuntimeInstance) || v.Generation == 0 {
		return errRuntimeInputIdentity
	}
	switch v.Kind {
	case RuntimeInputDescribed:
		if ValidateDigest(v.PackageID) != nil {
			return errRuntimeInputIdentity
		}
	case RuntimeInputDevelopment:
		if v.PackageID != "" {
			return errRuntimeInputIdentity
		}
	default:
		return errRuntimeInputIdentity
	}
	return nil
}

// UnmarshalJSON requires the explicit package_id even when it is empty and
// rejects duplicate, unknown, missing or null fields before validating values.
func (v *RuntimeInputIdentity) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return errRuntimeInputIdentity
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return errRuntimeInputIdentity
	}
	seen := make(map[string]bool, 4)
	var out RuntimeInputIdentity
	for d.More() {
		keyToken, err := d.Token()
		key, ok := keyToken.(string)
		if err != nil || !ok || seen[key] {
			return errRuntimeInputIdentity
		}
		seen[key] = true
		var value json.RawMessage
		if err := d.Decode(&value); err != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errRuntimeInputIdentity
		}
		switch key {
		case "kind":
			err = json.Unmarshal(value, &out.Kind)
		case "runtime_instance":
			err = json.Unmarshal(value, &out.RuntimeInstance)
		case "package_id":
			err = json.Unmarshal(value, &out.PackageID)
		case "generation":
			err = json.Unmarshal(value, &out.Generation)
		default:
			return errRuntimeInputIdentity
		}
		if err != nil {
			return errRuntimeInputIdentity
		}
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') || len(seen) != 4 {
		return errRuntimeInputIdentity
	}
	if _, err := d.Token(); err != io.EOF || out.Validate() != nil {
		return errRuntimeInputIdentity
	}
	*v = out
	return nil
}
