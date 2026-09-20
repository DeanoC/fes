package protocol

import "github.com/DeanoC/FogCast/corepackage"

// CoreMediaCapabilities describes the host's supported interpretation of an
// installed package's declared contract. It is not a live target observation.
// ImportMaxBytes is storage policy, not a core's media capacity.
type CoreMediaCapabilities struct {
	PackageID      string                `json:"package_id"`
	Source         string                `json:"source"`
	Compatibility  string                `json:"compatibility"`
	ImportMaxBytes int64                 `json:"import_max_bytes"`
	Media          []CoreMediaCapability `json:"media"`
	Firmware       []CoreMediaCapability `json:"firmware,omitempty"`
}

type CoreMediaCapability struct {
	Role      string          `json:"role"`
	Format    string          `json:"format"`
	MinBytes  int64           `json:"min_bytes"`
	MaxBytes  int64           `json:"max_bytes"`
	Interface RuntimeContract `json:"interface"`
	Transport string          `json:"transport"`
}

// DeclaredCoreMediaCapabilities contains transport interpretations, never core
// IDs. Unknown interface versions cannot silently inherit legacy size limits
// or transport semantics. Optional declarations still require active runtime
// support at launch.
func DeclaredCoreMediaCapabilities(descriptor corepackage.Descriptor) []CoreMediaCapability {
	result := make([]CoreMediaCapability, 0)
	if !supportsBlobABI(descriptor.ABI.ID, descriptor.ABI.Major, descriptor.ABI.Minor) {
		return result
	}
	transport := "fes-simple-computer-mailbox"
	if descriptor.ABI.ID == "fes.application" {
		transport = "fes-application-mailbox"
	}
	legacyRequired, streamRequired := false, false
	for _, contract := range descriptor.Interfaces {
		if contract.Required && contract.Major == 1 && contract.Minor == 0 {
			legacyRequired = legacyRequired || contract.ID == "fes.media.blob"
			streamRequired = streamRequired || contract.ID == MediaStreamInterface().ID
		}
	}
	if legacyRequired && streamRequired {
		return append(result, CoreMediaCapability{Role: "blob", Format: "raw", MinBytes: 1,
			MaxBytes: MaxDeclaredMediaStreamBytes, Interface: MediaStreamInterface(),
			Transport: transport + "-stream-v1"})
	}
	for _, contract := range descriptor.Interfaces {
		if contract.ID == "fes.media.blob" && contract.Major == 1 && contract.Minor == 0 &&
			(descriptor.ABI.ID != "fes.application" || contract.Required) {
			return append(result, CoreMediaCapability{
				Role: "blob", Format: "raw", MinBytes: 1, MaxBytes: MaxDevelopmentMediaBytes,
				Interface: RuntimeContract{ID: contract.ID, Major: 1, Minor: 0},
				Transport: transport + "-v1",
			})
		}
	}
	return result
}

func supportsBlobABI(id string, major, minor int64) bool {
	return (id == "fes.simple-computer" || id == "fes.application") && major == 1 && minor == 0
}

// RequiresCoreMedia applies the application's explicit startup contract. Legacy
// computer packages retain their existing package-only launch behavior.
func RequiresCoreMedia(descriptor corepackage.Descriptor) bool {
	if descriptor.ABI.ID != "fes.application" || descriptor.ABI.Major != 1 || descriptor.ABI.Minor != 0 {
		return false
	}
	for _, contract := range descriptor.Interfaces {
		if contract.ID == "fes.media.blob" && contract.Major == 1 && contract.Minor == 0 && contract.Required {
			return true
		}
	}
	return false
}
