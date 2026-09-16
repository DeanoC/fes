package kitlease

import contract "github.com/DeanoC/FogCast/kitlease"

// Keep these aliases for target-manager-local call sites while the public
// contract package owns the wire constants and predicates.
const (
	HostlessOwner   = contract.HostlessOwner
	HostlessPurpose = contract.HostlessPurpose
)

func HostlessSession(s Status) bool {
	return contract.HostlessSession(s)
}

func ForeignSession(s Status) bool {
	return contract.ForeignSession(s)
}
