// SPDX-License-Identifier: GPL-3.0-or-later

package agentapi

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/DeanoC/FogCast/targethttp"
	"github.com/DeanoC/fes/platform/embedded/tamago/mdns"
)

// ProvisioningPath contains the target identity and bearer configured by the
// device owner. A game or install catalog cannot supply this record.
const ProvisioningPath = "FES/AGENT.JSN"
const MaxProvisioningBytes = 8192

type Provisioning struct {
	TargetID string
	Token    string
	Port     uint16
}

// ParseProvisioning deliberately returns a value-free error. Decoder errors can
// contain fragments of the bearer and the boot console is visible locally.
func ParseProvisioning(raw []byte) (Provisioning, error) {
	var cfg Provisioning
	if len(raw) > MaxProvisioningBytes {
		return Provisioning{}, ErrInvalidConfig
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if start, err := d.Token(); err != nil || start != json.Delim('{') {
		return Provisioning{}, ErrInvalidConfig
	}
	seen := make(map[string]bool, 3)
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] {
			return Provisioning{}, ErrInvalidConfig
		}
		seen[name] = true
		switch name {
		case "target_id":
			err = d.Decode(&cfg.TargetID)
		case "token":
			err = d.Decode(&cfg.Token)
		case "port":
			err = d.Decode(&cfg.Port)
		default:
			return Provisioning{}, ErrInvalidConfig
		}
		if err != nil {
			return Provisioning{}, ErrInvalidConfig
		}
	}
	if end, err := d.Token(); err != nil || end != json.Delim('}') {
		return Provisioning{}, ErrInvalidConfig
	}
	if _, err := d.Token(); err != io.EOF || !mdns.ValidID(cfg.TargetID) ||
		cfg.Port == 0 || !targethttp.ValidBearerToken(cfg.Token) {
		return Provisioning{}, ErrInvalidConfig
	}
	return cfg, nil
}
