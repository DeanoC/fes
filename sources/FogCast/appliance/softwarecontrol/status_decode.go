package softwarecontrol

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

const MaxStatusBytes = 32 << 20

// A partially decoded response must never publish a selection: host health
// intentionally omits the supervisor's diagnostic error text.
func DecodeStatus(data []byte) (Status, bool) {
	var status Status
	if len(data) > MaxStatusBytes || !utf8.Valid(data) {
		return status, false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	open, err := decoder.Token()
	if err != nil || open != json.Delim('{') {
		return status, false
	}
	fields := make(map[string]json.RawMessage)
	seen := make(map[string]bool)
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return status, false
		}
		name, ok := key.(string)
		if !ok {
			return status, false
		}
		folded := foldedKnownName(name, softwareStatusFields)
		known := softwareStatusFields[folded]
		if known && seen[folded] {
			return status, false
		}
		seen[folded] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || (known && !validSoftwareJSON(value, folded)) {
			return status, false
		}
		fields[name] = value
	}
	if close, err := decoder.Token(); err != nil || close != json.Delim('}') {
		return status, false
	}
	if decoder.Decode(new(json.RawMessage)) != io.EOF {
		return status, false
	}
	for _, name := range []string{"version", "generation", "selected", "receipts", "active_bundle", "base_identity"} {
		value, exists := fields[name]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return status, false
		}
	}
	if json.Unmarshal(data, &status) != nil || ValidateState(status.State) != nil ||
		status.StorageError != "" {
		return Status{}, false
	}
	return status, true
}

// Only known fields carry the current status contract. Unknown additive fields
// are ignored, including null values, for forward-compatible health polling.
var softwareStatusFields = map[string]bool{
	"version": true, "generation": true, "selected": true, "previous": true,
	"pending": true, "unreleased": true, "receipts": true, "active_bundle": true,
	"base_identity": true, "service_fault": true, "storage_error": true,
	"held_back": true, "held_back_reason": true,
}

func validSoftwareJSON(value json.RawMessage, scope string) bool {
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return false
	}
	switch scope {
	case "pending", "unreleased", "receipts", "receipt":
		return validSoftwareObject(value, scope)
	default:
		return true // json.Unmarshal checks the actual scalar type.
	}
}

// Validate required request/journal members and reject ambiguous duplicate
// known keys. Unrecognized additive members remain forward compatible.
func validSoftwareObject(value json.RawMessage, scope string) bool {
	decoder := json.NewDecoder(bytes.NewReader(value))
	open, err := decoder.Token()
	if err != nil || open != json.Delim('{') {
		return false
	}
	known := map[string]bool{"id": true, "candidate": true, "requester": true, "expected_generation": true}
	required := []string{"id", "candidate", "requester", "expected_generation"}
	if scope == "pending" {
		known["previous"], known["phase"], known["reason"] = true, true, true
		required = append(required, "previous", "phase")
	} else if scope == "receipt" {
		known["result"], known["reason"] = true, true
		required = append(required, "result")
	} else if scope == "receipts" {
		required = nil
	}
	seen := make(map[string]bool)
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok {
			return false
		}
		folded := foldedKnownName(name, known)
		isKnown := scope == "receipts" || known[folded]
		if isKnown && seen[folded] {
			return false
		}
		seen[folded] = true
		var field json.RawMessage
		if err := decoder.Decode(&field); err != nil {
			return false
		}
		if scope == "receipts" {
			if !validSoftwareJSON(field, "receipt") {
				return false
			}
		} else if isKnown && !validSoftwareJSON(field, folded) {
			return false
		}
	}
	if close, err := decoder.Token(); err != nil || close != json.Delim('}') {
		return false
	}
	if decoder.Decode(new(json.RawMessage)) != io.EOF {
		return false
	}
	for _, name := range required {
		if !seen[name] {
			return false
		}
	}
	return true
}

// foldedKnownName matches encoding/json case-insensitive field matching, including Unicode simple-fold aliases.
func foldedKnownName(name string, known map[string]bool) string {
	for candidate := range known {
		if strings.EqualFold(candidate, name) {
			return candidate
		}
	}
	return name
}
