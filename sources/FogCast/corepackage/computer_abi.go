package corepackage

import "github.com/DeanoC/FogCast/corepackage/internal/generated"

// SupportedComputerABI accepts only computer versions understood by this host.
func SupportedComputerABI(abi Contract) bool {
	return abi.ID == generated.FesComputerABIID && abi.Major == int64(generated.FesComputerABIMajor) &&
		abi.Minor >= 0 && abi.Minor <= int64(generated.FesComputerABIMinor)
}
