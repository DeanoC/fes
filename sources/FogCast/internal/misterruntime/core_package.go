package misterruntime

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

type Protocol2Contract struct {
	ID    string `json:"id"`
	Major uint16 `json:"major"`
	Minor uint16 `json:"minor"`
}

type Protocol2Interface struct {
	ID    string `json:"id"`
	Major uint16 `json:"major"`
	Minor uint16 `json:"minor"`
}

type Protocol2ABI struct {
	ID         string               `json:"id"`
	Major      uint16               `json:"major"`
	Minor      uint16               `json:"minor"`
	Interfaces []Protocol2Interface `json:"interfaces"`
}

type Protocol2Capabilities struct {
	ROMLinking          uint64                          `json:"rom_linking,omitempty"`
	MediaStream         *protocol.MediaStreamCapability `json:"media_stream,omitempty"`
	MediaUnits          []protocol.MediaUnitStatus      `json:"media_units,omitempty"`
	ProgrammingProfiles []string                        `json:"programming_profiles"`
	ABIs                []Protocol2ABI                  `json:"abis"`
	ActiveInterfaces    []Protocol2Interface            `json:"active_interfaces"`
}

type Protocol2Observed struct {
	ABI     *Protocol2Contract `json:"abi"`
	BuildID *string            `json:"build_id"`
}

// Protocol2ActivePackage reports the running package. The runtime carries a
// single-socket v1 tuple or a multi-slot v2 tuple under the same composition
// key; SlotComposition holds the v2 form and Composition the v1 form.
type Protocol2ActivePackage struct {
	ROMLink         *corepackage.ROMLinkIdentity  `json:"rom_link,omitempty"`
	ROMLinks        *corepackage.ROMLinksIdentity `json:"rom_links,omitempty"`
	Composition     *expansion.Composition        `json:"composition,omitempty"`
	SlotComposition *expansion.SlotComposition    `json:"-"`
	PersistenceMode string                        `json:"persistence_mode,omitempty"`
	PackageID       string                        `json:"package_id"`
	Descriptor      corepackage.Descriptor        `json:"descriptor"`
	Observed        Protocol2Observed             `json:"observed"`
}

type protocol2ActivePackageWire struct {
	ROMLink         *corepackage.ROMLinkIdentity  `json:"rom_link,omitempty"`
	ROMLinks        *corepackage.ROMLinksIdentity `json:"rom_links,omitempty"`
	Composition     json.RawMessage               `json:"composition,omitempty"`
	PersistenceMode string                        `json:"persistence_mode,omitempty"`
	PackageID       string                        `json:"package_id"`
	Descriptor      corepackage.Descriptor        `json:"descriptor"`
	Observed        Protocol2Observed             `json:"observed"`
}

func (p Protocol2ActivePackage) MarshalJSON() ([]byte, error) {
	wire := protocol2ActivePackageWire{ROMLink: p.ROMLink, ROMLinks: p.ROMLinks, PersistenceMode: p.PersistenceMode,
		PackageID: p.PackageID, Descriptor: p.Descriptor, Observed: p.Observed}
	var err error
	switch {
	case p.Composition != nil && p.SlotComposition != nil:
		return nil, errors.New("active package carries two composition forms")
	case p.Composition != nil:
		wire.Composition, err = json.Marshal(p.Composition)
	case p.SlotComposition != nil:
		wire.Composition, err = json.Marshal(p.SlotComposition)
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(wire)
}

// UnmarshalJSON strictly decodes either composition form. The v2 form is the
// only one with an expansions array; unknown fields are rejected in both.
func (p *Protocol2ActivePackage) UnmarshalJSON(data []byte) error {
	var wire protocol2ActivePackageWire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}
	result := Protocol2ActivePackage{ROMLink: wire.ROMLink, ROMLinks: wire.ROMLinks, PersistenceMode: wire.PersistenceMode,
		PackageID: wire.PackageID, Descriptor: wire.Descriptor, Observed: wire.Observed}
	if len(wire.Composition) != 0 && !bytes.Equal(bytes.TrimSpace(wire.Composition), []byte("null")) {
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(wire.Composition, &probe); err != nil {
			return err
		}
		strict := json.NewDecoder(bytes.NewReader(wire.Composition))
		strict.DisallowUnknownFields()
		if _, slots := probe["expansions"]; slots {
			var value expansion.SlotComposition
			if err := strict.Decode(&value); err != nil {
				return err
			}
			result.SlotComposition = &value
		} else {
			var value expansion.Composition
			if err := strict.Decode(&value); err != nil {
				return err
			}
			result.Composition = &value
		}
	}
	*p = result
	return nil
}

type Protocol2Inspection struct {
	PersistenceLayout  *protocol.RuntimeContract `json:"persistence_layout,omitempty"`
	PackageID          string                    `json:"package_id"`
	Descriptor         corepackage.Descriptor    `json:"descriptor"`
	Compatible         bool                      `json:"compatible"`
	CompatibilityError *Protocol2Error           `json:"compatibility_error"`
}
