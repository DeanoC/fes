package kitlease

import contract "github.com/DeanoC/FogCast/kitlease"

// ForeignHID retains the internal test/call-site spelling while the public
// contract package owns the predicate.
func ForeignHID(s Status) bool {
	return contract.ForeignHID(s)
}
