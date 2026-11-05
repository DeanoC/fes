// Package release names the cores in the first FES release. Home, the rooms
// and the core catalog view show only v1 cores unless the launcher's Show all
// cores preference is on. docs/support-matrix.json records the same scope.
package release

// Scope is a core's place in the release plan.
type Scope string

const (
	V1       Scope = "v1"
	V2       Scope = "v2"
	HeldBack Scope = "held-back"
)

// v1 is the four families of the first release: ZX81, Spectrum, C64 and
// Atari ST. v2 is the PC lane. Every other core is held back.
var scopes = map[string]Scope{
	"fes.zx81":           V1,
	"fes.zx80":           V1,
	"fes.ts1000":         V1,
	"fes.ts1500":         V1,
	"fes.spectrum":       V1,
	"fes.spectrum128":    V1,
	"fes.spectrum-plus3": V1,
	"fes.pentagon":       V1,
	"fes.c64":            V1,
	"fes.atari-st":       V1,
	"fes.pc386":          V2,
	"fes.pcxt":           V2,
	"fes.pcxt-dos":       V2,
}

// Of returns a core's scope. An unknown core is held back.
func Of(coreID string) Scope {
	if scope, ok := scopes[coreID]; ok {
		return scope
	}
	return HeldBack
}

// Shown reports whether a core is listed: a v1 core always, any core when all
// is set.
func Shown(coreID string, all bool) bool {
	return all || Of(coreID) == V1
}
