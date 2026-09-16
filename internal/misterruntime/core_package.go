package misterruntime

import (
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
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
	ProgrammingProfiles []string             `json:"programming_profiles"`
	ABIs                []Protocol2ABI       `json:"abis"`
	ActiveInterfaces    []Protocol2Interface `json:"active_interfaces"`
}

type Protocol2Observed struct {
	ABI     *Protocol2Contract `json:"abi"`
	BuildID *string            `json:"build_id"`
}

type Protocol2ActivePackage struct {
	PersistenceMode string                 `json:"persistence_mode,omitempty"`
	PackageID       string                 `json:"package_id"`
	Descriptor      corepackage.Descriptor `json:"descriptor"`
	Observed        Protocol2Observed      `json:"observed"`
}

type Protocol2Inspection struct {
	PersistenceLayout  *protocol.RuntimeContract `json:"persistence_layout,omitempty"`
	PackageID          string                    `json:"package_id"`
	Descriptor         corepackage.Descriptor    `json:"descriptor"`
	Compatible         bool                      `json:"compatible"`
	CompatibilityError *Protocol2Error           `json:"compatibility_error"`
}
