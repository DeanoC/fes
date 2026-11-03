package targetclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/DeanoC/FogCast/appliance/software"
	"github.com/DeanoC/FogCast/appliance/softwarecontrol"
	"github.com/DeanoC/FogCast/protocol"
)

// healthResponse validates identity-bearing fields from their raw wire form,
// before encoding/json can collapse duplicate names or missing integer fields.
type healthResponse struct{ Health protocol.Health }

func (r *healthResponse) UnmarshalJSON(data []byte) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("invalid UTF-8 health response")
	}
	var health protocol.Health
	if err := json.Unmarshal(data, &health); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if token, err := dec.Token(); err != nil || token != json.Delim('{') {
		return fmt.Errorf("invalid health response")
	}
	var legacy, compact json.RawMessage
	seen := map[string]bool{}
	ambiguous := false
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		name, ok := token.(string)
		if !ok {
			return fmt.Errorf("invalid health member")
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return err
		}
		field := ""
		if strings.EqualFold(name, "software") {
			field = "software"
		}
		if strings.EqualFold(name, "software_identity_v1") {
			field = "software_identity_v1"
		}
		if field == "" {
			continue
		}
		if seen[field] || name != field {
			ambiguous = true
		}
		seen[field] = true
		if field == "software" {
			legacy = value
		} else {
			compact = value
		}
	}
	if token, err := dec.Token(); err != nil || token != json.Delim('}') {
		return fmt.Errorf("invalid health response")
	}
	if dec.Decode(new(json.RawMessage)) != io.EOF {
		return fmt.Errorf("trailing health response")
	}
	if seen["software"] && seen["software_identity_v1"] {
		ambiguous = true
	}
	if ambiguous {
		health.Software = nil
		health.SoftwareIdentityV1 = nil
	} else if seen["software"] {
		status, ok := softwarecontrol.DecodeStatus(legacy)
		if ok {
			health.Software = &status
		} else if health.Software != nil {
			// Preserve only the peer-reported active/base fields, never selection.
			health.Software = &softwarecontrol.Status{ActiveBundle: health.Software.ActiveBundle, BaseIdentity: health.Software.BaseIdentity, StorageError: "software status unverified"}
		}
		health.SoftwareIdentityV1 = nil
	} else if seen["software_identity_v1"] {
		identity, ok := decodeSoftwareIdentity(compact)
		if ok {
			health.SoftwareIdentityV1 = &identity
		} else {
			health.SoftwareIdentityV1 = nil
		}
		health.Software = nil
	} else {
		health.Software = nil
		health.SoftwareIdentityV1 = nil
	}
	r.Health = health
	return nil
}

func decodeSoftwareIdentity(data []byte) (protocol.SoftwareIdentityV1, bool) {
	var identity protocol.SoftwareIdentityV1
	if !utf8.Valid(data) {
		return identity, false
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if token, err := dec.Token(); err != nil || token != json.Delim('{') {
		return identity, false
	}
	known := []string{"active_bundle", "base_identity", "selected", "generation", "storage_error"}
	seen := map[string]bool{}
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return identity, false
		}
		name, ok := token.(string)
		if !ok {
			return identity, false
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return identity, false
		}
		for _, field := range known {
			if strings.EqualFold(field, name) {
				if name != field || seen[field] || field == "storage_error" || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
					return identity, false
				}
				seen[field] = true
			}
		}
	}
	if token, err := dec.Token(); err != nil || token != json.Delim('}') {
		return identity, false
	}
	if dec.Decode(new(json.RawMessage)) != io.EOF {
		return identity, false
	}
	for _, field := range known[:4] {
		if !seen[field] {
			return identity, false
		}
	}
	if json.Unmarshal(data, &identity) != nil || !software.ValidID(identity.Selected) {
		return protocol.SoftwareIdentityV1{}, false
	}
	return identity, true
}
