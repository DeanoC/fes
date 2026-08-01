# MiSTer Remote POC 1A Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build and prove a macOS CLI plus a small Linux ARMv7 daemon that remotely launches one preloaded Mega Drive game and one preloaded SNES game on the dedicated MiSTer Pi.

**Architecture:** The public `host` package owns connection configuration, the two-game manifest, and the HTTP client. The target-side coordinator serializes launch and stop transitions, while a MiSTer adapter validates SD-card paths, writes an MGL atomically, commands `/dev/MiSTer_cmd`, and observes `/tmp/CORENAME`. POC 1A runs beside the stock MiSTer system and ends with a hardware acceptance report plus a deterministic inventory that gates the separate POC 1B image plan.

**Tech Stack:** Go 1.26.5, Go standard library, `github.com/pelletier/go-toml/v2` v2.4.3, HTTP/1.1 with JSON, POSIX shell for stock-MiSTer packaging and installation, macOS Apple Silicon host, Linux ARMv7 target.

## Global Constraints

- The authoritative design is `docs/superpowers/specs/2026-08-01-mister-remote-poc1-design.md`.
- Use module path `github.com/clawzai2-tech/mister-remote`, `go 1.26.0`, and `toolchain go1.26.5`.
- `github.com/pelletier/go-toml/v2` v2.4.3 is the only non-standard Go module in POC 1A.
- Build `misterctl` for Apple Silicon macOS and `mister-agent` with `CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7`.
- Support only system IDs `megadrive` and `snes` and only the fixed registry values in the approved design.
- Use HTTP port `8182`, bearer authentication on every endpoint except `GET /v1/health`, a 10-second launch deadline, a 5-second stop deadline, and a 12-second host request timeout.
- Treat `game_id`, `system`, `expected_core`, and `observed_core` as nullable JSON strings; always emit those keys.
- Keep ROMs, RBFs, `Main_MiSTer`, controller-map contents, real tokens, generated packages, and hardware reports out of Git.
- Do not add NAS access, ROM transfer, discovery, TLS, a GUI, streaming, extra systems, extra controllers, or POC 1B image work.
- Write tests before production behavior, run the named failing test before implementation, and commit after every task.
- Run `gofmt`, `go test -race ./...`, and `go vet ./...` before every software milestone.
- Do not change the SuperStation One.

---

## Delivery Boundary

This plan delivers POC 1A only. POC 1B receives a separate implementation plan after Task 12 has produced and committed the exact known-good MiSTer Pi source lock and runtime dependency inventory. That checkpoint prevents the reduced image from being planned around guessed kernel, root-filesystem, or library details.

## File Map

| Path | Responsibility |
|---|---|
| `go.mod`, `go.sum` | Pinned Go toolchain and the TOML checksum added when configuration loading is implemented |
| `.gitignore` | Excludes tokens, ROMs, binaries, packages, and HIL results |
| `Makefile` | Reproducible format, test, vet, native build, ARMv7 build, and package commands |
| `protocol/types.go` | Versioned JSON request, status, health, and error types |
| `protocol/validation.go` | Shared game-ID and system validation |
| `internal/core/registry.go` | Fixed Mega Drive and SNES core registry |
| `internal/mister/mgl.go` | Deterministic MGL rendering |
| `internal/mister/prepare.go` | ROM containment, extension, symlink, and regular-file validation |
| `internal/mister/atomic.go` | Atomic volatile MGL installation |
| `internal/mister/runtime.go` | Health, command dispatch, core observation, stop, and startup reconciliation |
| `internal/mister/process.go` | `/proc`-based `Main_MiSTer` process detection |
| `internal/agent/coordinator.go` | State machine, transition exclusion, and bounded state reporting |
| `internal/agentconfig/config.go` | Strict target TOML loading and validation |
| `internal/httpapi/server.go` | Authentication, JSON handling, routes, and structured request logging |
| `host/config.go` | Strict host connection TOML loading |
| `host/manifest.go` | Two-game manifest loading and validation |
| `host/client.go` | Typed HTTP client and API error decoding |
| `host/library.go` | Game-ID facade used by `misterctl` |
| `internal/cli/run.go` | Testable `misterctl` argument and output adapter |
| `internal/version/version.go` | Build-injected daemon and CLI version |
| `cmd/mister-agent/main.go` | Target process wiring and graceful shutdown |
| `cmd/misterctl/main.go` | macOS CLI entrypoint |
| `internal/packagepoc/archive.go`, `cmd/package-poc1a/main.go` | Deterministic deployment tar/gzip creation |
| `internal/integration/remote_control_test.go` | Real HTTP stack against an isolated fake MiSTer filesystem |
| `internal/hil/runner.go` | Automated and prompted hardware acceptance sequence |
| `cmd/mister-hil/main.go` | MacBook hardware-acceptance entrypoint |
| `deploy/poc1a/agent.toml.example` | Secret-free target configuration template |
| `deploy/poc1a/start-agent.sh` | Stock-image process supervision loop |
| `deploy/poc1a/user-startup.snippet.sh` | Idempotent stock startup hook |
| `deploy/poc1a/MiSTer.ini.fragment` | Black-idle settings from the design |
| `deploy/poc1a/inventory.sh` | Deterministic target artifact and runtime inventory |
| `scripts/package-poc1a.sh` | Creates a checksummed deployment archive without ROMs |
| `scripts/install-poc1a.sh` | Backs up and installs the package over development SSH |
| `scripts/capture-poc1a-lock.sh` | Captures the target inventory into the repository |
| `scripts/verify-poc1a-lock.sh` | Compares the running target with the committed inventory |
| `docs/runbooks/poc1a-deploy.md` | Operator steps, recovery, and acceptance procedure |
| `build/sources.poc1a.lock.toml` | Hardware-generated hashes and library closure committed only after acceptance |

---

### Task 1: Repository Foundation and Shared Protocol

**Files:**
- Create: `.gitignore`
- Create: `go.mod`
- Create: `Makefile`
- Create: `protocol/types.go`
- Create: `protocol/validation.go`
- Test: `protocol/types_test.go`
- Test: `protocol/validation_test.go`

**Interfaces:**
- Consumes: The exact API names, JSON keys, states, systems, and error codes from the approved design.
- Produces: `protocol.System`, `protocol.State`, `protocol.ErrorCode`, `protocol.APIError`, `protocol.ErrorEnvelope`, `protocol.Health`, `protocol.Status`, `protocol.LaunchRequest`, `protocol.ValidateGameID(string) error`, and `protocol.ValidateSystem(System) error`.

- [ ] **Step 1: Create the module definition and failing protocol tests**

Create `go.mod`:

```go
module github.com/clawzai2-tech/mister-remote

go 1.26.0

toolchain go1.26.5
```

Create `protocol/validation_test.go`:

```go
package protocol_test

import (
	"testing"

	"github.com/clawzai2-tech/mister-remote/protocol"
)

func TestValidateGameID(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"megadrive-test": true,
		"snes-test":      true,
		"MegaDrive":      false,
		"two words":      false,
		"../escape":      false,
		"":               false,
	}
	for input, wantValid := range tests {
		input, wantValid := input, wantValid
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			err := protocol.ValidateGameID(input)
			if (err == nil) != wantValid {
				t.Fatalf("ValidateGameID(%q) error = %v, want valid=%v", input, err, wantValid)
			}
		})
	}
}

func TestValidateSystem(t *testing.T) {
	t.Parallel()
	for _, system := range []protocol.System{protocol.SystemMegaDrive, protocol.SystemSNES} {
		if err := protocol.ValidateSystem(system); err != nil {
			t.Fatalf("ValidateSystem(%q): %v", system, err)
		}
	}
	if err := protocol.ValidateSystem("nes"); err == nil {
		t.Fatal("ValidateSystem(nes) succeeded")
	}
}
```

Create `protocol/types_test.go` with a status round-trip that asserts all nullable keys remain present:

```go
package protocol_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/clawzai2-tech/mister-remote/protocol"
)

func TestIdleStatusJSONIncludesNullFields(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(protocol.Status{State: protocol.StateIdle})
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, field := range []string{`"game_id":null`, `"system":null`, `"expected_core":null`, `"observed_core":null`, `"last_error":null`} {
		if !strings.Contains(got, field) {
			t.Fatalf("status JSON %s does not contain %s", got, field)
		}
	}
}
```

- [ ] **Step 2: Run the tests and verify the compile failure**

Run: `go test ./protocol`

Expected: FAIL with undefined `protocol` types and validation functions.

- [ ] **Step 3: Implement the exact shared protocol**

Create `protocol/types.go`:

```go
package protocol

type System string

const (
	SystemMegaDrive System = "megadrive"
	SystemSNES      System = "snes"
)

type State string

const (
	StateIdle      State = "idle"
	StateLaunching State = "launching"
	StateActive    State = "active"
	StateStopping  State = "stopping"
	StateFailed    State = "failed"
)

type ErrorCode string

const (
	CodeBadRequest        ErrorCode = "BAD_REQUEST"
	CodeUnauthorized      ErrorCode = "UNAUTHORIZED"
	CodeROMNotFound       ErrorCode = "ROM_NOT_FOUND"
	CodeBusy              ErrorCode = "BUSY"
	CodeUnsupportedSystem ErrorCode = "UNSUPPORTED_SYSTEM"
	CodeInvalidROMPath    ErrorCode = "INVALID_ROM_PATH"
	CodeMiSTerUnavailable ErrorCode = "MISTER_UNAVAILABLE"
	CodeCoreTimeout       ErrorCode = "CORE_TIMEOUT"
	CodeInternal          ErrorCode = "INTERNAL"
	CodeUnrecognizedCore  ErrorCode = "UNRECOGNIZED_CORE"
)

type APIError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

func (e *APIError) Error() string { return string(e.Code) + ": " + e.Message }

type ErrorEnvelope struct {
	Error APIError `json:"error"`
}

type Health struct {
	APIVersion    string `json:"api_version"`
	AgentVersion  string `json:"agent_version"`
	Ready         bool   `json:"ready"`
	MiSTerProcess bool   `json:"mister_process"`
	CommandPipe   bool   `json:"command_pipe"`
}

type Status struct {
	State        State     `json:"state"`
	GameID       *string   `json:"game_id"`
	System       *System   `json:"system"`
	ExpectedCore *string   `json:"expected_core"`
	ObservedCore *string   `json:"observed_core"`
	LastError    *APIError `json:"last_error"`
}

type LaunchRequest struct {
	GameID  string `json:"game_id"`
	System  System `json:"system"`
	ROMPath string `json:"rom_path"`
}
```

Create `protocol/validation.go`:

```go
package protocol

import (
	"fmt"
	"regexp"
)

var gameIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func ValidateGameID(id string) error {
	if !gameIDPattern.MatchString(id) {
		return fmt.Errorf("game ID %q must be a lowercase ASCII slug", id)
	}
	return nil
}

func ValidateSystem(system System) error {
	switch system {
	case SystemMegaDrive, SystemSNES:
		return nil
	default:
		return fmt.Errorf("unsupported system %q", system)
	}
}
```

- [ ] **Step 4: Add repository checks and exclusions**

Create `.gitignore`:

```gitignore
/artifacts/
/dist/
/bin/
/.cache/
/local-games/
/private-roms/
*.secret.toml
```

Create `Makefile`:

```make
.PHONY: fmt test vet check

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

test:
	go test -race ./...

vet:
	go vet ./...

check: fmt test vet
```

Run: `go mod tidy`

Expected: module metadata is valid; no dependency is added before a package imports it.

- [ ] **Step 5: Run the protocol and repository checks**

Run: `make check`

Expected: PASS with no formatting changes after the first `gofmt` run.

- [ ] **Step 6: Commit the protocol foundation**

```bash
git add .gitignore Makefile go.mod protocol
git commit -m "feat: define remote control protocol"
```

---

### Task 2: Fixed Core Registry and Deterministic MGL Renderer

**Files:**
- Create: `internal/core/registry.go`
- Test: `internal/core/registry_test.go`
- Create: `internal/mister/mgl.go`
- Test: `internal/mister/mgl_test.go`
- Create: `internal/mister/testdata/megadrive.mgl`
- Create: `internal/mister/testdata/snes.mgl`

**Interfaces:**
- Consumes: `protocol.System` and the exact registry values from the design.
- Produces: `core.Spec`, `core.Registry`, `core.DefaultRegistry() Registry`, `core.NewRegistry(...Spec) Registry`, `Registry.Lookup(protocol.System) (Spec, bool)`, `Registry.LookupObserved(string) (Spec, bool)`, and `mister.RenderMGL(core.Spec, string) ([]byte, error)`.

- [ ] **Step 1: Write failing registry tests**

Create `internal/core/registry_test.go`:

```go
package core_test

import (
	"testing"

	"github.com/clawzai2-tech/mister-remote/internal/core"
	"github.com/clawzai2-tech/mister-remote/protocol"
)

func TestRegistry(t *testing.T) {
	t.Parallel()
	registry := core.DefaultRegistry()
	mega, ok := registry.Lookup(protocol.SystemMegaDrive)
	if !ok || mega.ExpectedCore != "MegaDrive" || mega.RBFSelector != "_Console/MegaDrive" || mega.FileIndex != 1 || mega.FileDelay != 1 || mega.FileType != "f" {
		t.Fatalf("unexpected Mega Drive spec: %#v, ok=%v", mega, ok)
	}
	snes, ok := registry.Lookup(protocol.SystemSNES)
	if !ok || snes.ExpectedCore != "SNES" || snes.RBFSelector != "_Console/SNES" || snes.FileIndex != 0 || snes.FileDelay != 2 || snes.FileType != "f" {
		t.Fatalf("unexpected SNES spec: %#v, ok=%v", snes, ok)
	}
	if _, ok := registry.Lookup("nes"); ok {
		t.Fatal("unexpected NES registry entry")
	}
}

func TestLookupObserved(t *testing.T) {
	t.Parallel()
	registry := core.DefaultRegistry()
	spec, ok := registry.LookupObserved("MegaDrive")
	if !ok || spec.System != protocol.SystemMegaDrive {
		t.Fatalf("LookupObserved(MegaDrive) = %#v, %v", spec, ok)
	}
	if _, ok := registry.LookupObserved("Genesis"); ok {
		t.Fatal("deprecated Genesis core must not match the POC registry")
	}
}
```

- [ ] **Step 2: Run the registry test and verify it fails**

Run: `go test ./internal/core -run 'TestRegistry|TestLookupObserved' -v`

Expected: FAIL because package `internal/core` does not exist.

- [ ] **Step 3: Implement the immutable registry**

Create `internal/core/registry.go`:

```go
package core

import "github.com/clawzai2-tech/mister-remote/protocol"

type Spec struct {
	System        protocol.System
	ExpectedCore  string
	RBFSelector   string
	ROMRoot       string
	Extensions    map[string]struct{}
	FileDelay     int
	FileType      string
	FileIndex     int
}

var defaults = []Spec{
	{
		System: protocol.SystemMegaDrive, ExpectedCore: "MegaDrive", RBFSelector: "_Console/MegaDrive",
		ROMRoot: "/media/fat/games/MegaDrive", Extensions: extensionSet(".md", ".gen", ".bin"),
		FileDelay: 1, FileType: "f", FileIndex: 1,
	},
	{
		System: protocol.SystemSNES, ExpectedCore: "SNES", RBFSelector: "_Console/SNES",
		ROMRoot: "/media/fat/games/SNES", Extensions: extensionSet(".sfc", ".smc", ".bin"),
		FileDelay: 2, FileType: "f", FileIndex: 0,
	},
}

func extensionSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

type Registry struct { bySystem map[protocol.System]Spec }

func DefaultRegistry() Registry { return NewRegistry(defaults...) }

func NewRegistry(specs ...Spec) Registry {
	result := Registry{bySystem: make(map[protocol.System]Spec, len(specs))}
	for _, spec := range specs { result.bySystem[spec.System] = cloneSpec(spec) }
	return result
}

func (r Registry) Lookup(system protocol.System) (Spec, bool) {
	spec, ok := r.bySystem[system]
	return cloneSpec(spec), ok
}

func (r Registry) LookupObserved(name string) (Spec, bool) {
	for _, spec := range r.bySystem {
		if spec.ExpectedCore == name {
			return cloneSpec(spec), true
		}
	}
	return Spec{}, false
}

func cloneSpec(spec Spec) Spec {
	copy := spec
	copy.Extensions = make(map[string]struct{}, len(spec.Extensions))
	for extension := range spec.Extensions { copy.Extensions[extension] = struct{}{} }
	return copy
}
```

- [ ] **Step 4: Write failing golden MGL tests**

Create `internal/mister/testdata/megadrive.mgl`:

```xml
<mistergamedescription>
    <rbf>_Console/MegaDrive</rbf>
    <file delay="1" type="f" index="1" path="test.md"/>
</mistergamedescription>
```

Create `internal/mister/testdata/snes.mgl`:

```xml
<mistergamedescription>
    <rbf>_Console/SNES</rbf>
    <file delay="2" type="f" index="0" path="RPGs/test &amp; demo.sfc"/>
</mistergamedescription>
```

Create `internal/mister/mgl_test.go`:

```go
package mister_test

import (
	"os"
	"testing"

	"github.com/clawzai2-tech/mister-remote/internal/core"
	"github.com/clawzai2-tech/mister-remote/internal/mister"
	"github.com/clawzai2-tech/mister-remote/protocol"
)

func TestRenderMGLGolden(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, relativePath, golden string
		system protocol.System
	}{
		{name: "megadrive", system: protocol.SystemMegaDrive, relativePath: "test.md", golden: "testdata/megadrive.mgl"},
		{name: "snes", system: protocol.SystemSNES, relativePath: "RPGs/test & demo.sfc", golden: "testdata/snes.mgl"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec, _ := core.DefaultRegistry().Lookup(tt.system)
			got, err := mister.RenderMGL(spec, tt.relativePath)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(tt.golden)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Fatalf("MGL mismatch\nwant:\n%s\ngot:\n%s", want, got)
			}
		})
	}
}
```

- [ ] **Step 5: Run the MGL test and verify it fails**

Run: `go test ./internal/mister -run TestRenderMGLGolden -v`

Expected: FAIL with undefined `mister.RenderMGL`.

- [ ] **Step 6: Implement deterministic escaped rendering**

Create `internal/mister/mgl.go`:

```go
package mister

import (
	"bytes"
	"encoding/xml"
	"fmt"

	"github.com/clawzai2-tech/mister-remote/internal/core"
)

func RenderMGL(spec core.Spec, relativeROM string) ([]byte, error) {
	escape := func(value string) (string, error) {
		var b bytes.Buffer
		if err := xml.EscapeText(&b, []byte(value)); err != nil {
			return "", err
		}
		return b.String(), nil
	}
	rbf, err := escape(spec.RBFSelector)
	if err != nil {
		return nil, err
	}
	path, err := escape(relativeROM)
	if err != nil {
		return nil, err
	}
	result := fmt.Sprintf("<mistergamedescription>\n    <rbf>%s</rbf>\n    <file delay=\"%d\" type=\"%s\" index=\"%d\" path=\"%s\"/>\n</mistergamedescription>\n", rbf, spec.FileDelay, spec.FileType, spec.FileIndex, path)
	return []byte(result), nil
}
```

- [ ] **Step 7: Run registry and MGL checks**

Run: `go test -race ./internal/core ./internal/mister && go vet ./internal/core ./internal/mister`

Expected: PASS.

- [ ] **Step 8: Commit the core contract**

```bash
git add internal/core internal/mister
git commit -m "feat: add fixed core registry and MGL renderer"
```

---

### Task 3: Secure ROM Preparation and Atomic MGL Installation

**Files:**
- Create: `internal/mister/prepare.go`
- Create: `internal/mister/atomic.go`
- Test: `internal/mister/prepare_test.go`
- Test: `internal/mister/atomic_test.go`

**Interfaces:**
- Consumes: `core.Spec` and `mister.RenderMGL` from Task 2.
- Produces: `mister.PreparedLaunch`, `mister.PrepareLaunch(core.Spec, string) (PreparedLaunch, *protocol.APIError)`, and `mister.WriteAtomicMGL(string, []byte) (string, error)`.

- [ ] **Step 1: Write failing containment and extension tests**

Create `internal/mister/prepare_test.go` with this table and real temporary files:

```go
package mister_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/clawzai2-tech/mister-remote/internal/core"
	"github.com/clawzai2-tech/mister-remote/internal/mister"
	"github.com/clawzai2-tech/mister-remote/protocol"
)

func TestPrepareLaunch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rom := filepath.Join(root, "RPGs", "test & demo.sfc")
	if err := os.MkdirAll(filepath.Dir(rom), 0o755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(rom, []byte("test"), 0o644); err != nil { t.Fatal(err) }
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemSNES)
	spec.ROMRoot = root
	prepared, apiErr := mister.PrepareLaunch(spec, rom)
	if apiErr != nil { t.Fatal(apiErr) }
	if prepared.RelativeROM != "RPGs/test & demo.sfc" { t.Fatalf("relative path = %q", prepared.RelativeROM) }
	if len(prepared.MGL) == 0 { t.Fatal("empty MGL") }
}

func TestPrepareLaunchRejectsUnsafePaths(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.sfc")
	if err := os.WriteFile(outside, []byte("test"), 0o644); err != nil { t.Fatal(err) }
	insideWrongExtension := filepath.Join(root, "wrong.zip")
	if err := os.WriteFile(insideWrongExtension, []byte("test"), 0o644); err != nil { t.Fatal(err) }
	link := filepath.Join(root, "escape.sfc")
	if err := os.Symlink(outside, link); err != nil { t.Fatal(err) }
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemSNES)
	spec.ROMRoot = root
	tests := []struct {
		name, path string
		code protocol.ErrorCode
	}{
		{name: "relative", path: "test.sfc", code: protocol.CodeInvalidROMPath},
		{name: "nul", path: root + "/bad\x00.sfc", code: protocol.CodeInvalidROMPath},
		{name: "outside", path: outside, code: protocol.CodeInvalidROMPath},
		{name: "symlink escape", path: link, code: protocol.CodeInvalidROMPath},
		{name: "extension", path: insideWrongExtension, code: protocol.CodeInvalidROMPath},
		{name: "missing", path: filepath.Join(root, "missing.sfc"), code: protocol.CodeROMNotFound},
		{name: "root", path: root, code: protocol.CodeInvalidROMPath},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, apiErr := mister.PrepareLaunch(spec, tt.path)
			if apiErr == nil || apiErr.Code != tt.code {
				t.Fatalf("error = %#v, want code %s", apiErr, tt.code)
			}
		})
	}
}
```

- [ ] **Step 2: Run the preparation tests and verify they fail**

Run: `go test ./internal/mister -run TestPrepareLaunch -v`

Expected: FAIL with undefined `mister.PrepareLaunch`.

- [ ] **Step 3: Implement fail-closed ROM preparation**

Create `internal/mister/prepare.go` with this public shape and validation order:

```go
package mister

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/clawzai2-tech/mister-remote/internal/core"
	"github.com/clawzai2-tech/mister-remote/protocol"
)

type PreparedLaunch struct {
	Spec        core.Spec
	AbsoluteROM string
	RelativeROM string
	MGL         []byte
}

func PrepareLaunch(spec core.Spec, candidate string) (PreparedLaunch, *protocol.APIError) {
	fail := func(code protocol.ErrorCode, message string) (PreparedLaunch, *protocol.APIError) {
		return PreparedLaunch{}, &protocol.APIError{Code: code, Message: message}
	}
	if strings.IndexByte(candidate, 0) >= 0 || !filepath.IsAbs(candidate) {
		return fail(protocol.CodeInvalidROMPath, "ROM path must be absolute and contain no NUL byte")
	}
	cleaned := filepath.Clean(candidate)
	root, err := filepath.EvalSymlinks(spec.ROMRoot)
	if err != nil {
		return fail(protocol.CodeInternal, "registered ROM root cannot be resolved")
	}
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		if os.IsNotExist(err) { return fail(protocol.CodeROMNotFound, "ROM does not exist on the MiSTer SD card") }
		return fail(protocol.CodeInvalidROMPath, "ROM path cannot be resolved")
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fail(protocol.CodeInvalidROMPath, "ROM path escapes its registered root")
	}
	if _, ok := spec.Extensions[strings.ToLower(filepath.Ext(resolved))]; !ok {
		return fail(protocol.CodeInvalidROMPath, "ROM extension is not allowed for the selected system")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return fail(protocol.CodeROMNotFound, "ROM does not identify a regular file")
	}
	relative = filepath.ToSlash(relative)
	mgl, err := RenderMGL(spec, relative)
	if err != nil {
		return fail(protocol.CodeInternal, "MGL rendering failed")
	}
	return PreparedLaunch{Spec: spec, AbsoluteROM: resolved, RelativeROM: relative, MGL: mgl}, nil
}
```

- [ ] **Step 4: Write the failing atomic-install test**

Create `internal/mister/atomic_test.go`:

```go
package mister_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/clawzai2-tech/mister-remote/internal/mister"
)

func TestWriteAtomicMGL(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "mister-remote")
	path, err := mister.WriteAtomicMGL(dir, []byte("new mgl\n"))
	if err != nil { t.Fatal(err) }
	if path != filepath.Join(dir, "launch.mgl") { t.Fatalf("path = %q", path) }
	got, err := os.ReadFile(path)
	if err != nil { t.Fatal(err) }
	if string(got) != "new mgl\n" { t.Fatalf("content = %q", got) }
	if _, err := os.Stat(path + ".new"); !os.IsNotExist(err) { t.Fatalf("temporary file remains: %v", err) }
}
```

- [ ] **Step 5: Run the atomic-install test and verify it fails**

Run: `go test ./internal/mister -run TestWriteAtomicMGL -v`

Expected: FAIL with undefined `mister.WriteAtomicMGL`.

- [ ] **Step 6: Implement flush-close-rename installation**

Create `internal/mister/atomic.go`:

```go
package mister

import (
	"fmt"
	"os"
	"path/filepath"
)

func WriteAtomicMGL(directory string, content []byte) (string, error) {
	if err := os.MkdirAll(directory, 0o755); err != nil { return "", fmt.Errorf("create MGL directory: %w", err) }
	finalPath := filepath.Join(directory, "launch.mgl")
	temporaryPath := finalPath + ".new"
	defer os.Remove(temporaryPath)
	f, err := os.OpenFile(temporaryPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil { return "", fmt.Errorf("open temporary MGL: %w", err) }
	if _, err = f.Write(content); err == nil { err = f.Sync() }
	closeErr := f.Close()
	if err != nil { return "", fmt.Errorf("write temporary MGL: %w", err) }
	if closeErr != nil { return "", fmt.Errorf("close temporary MGL: %w", closeErr) }
	if err := os.Rename(temporaryPath, finalPath); err != nil { return "", fmt.Errorf("install MGL: %w", err) }
	return finalPath, nil
}
```

- [ ] **Step 7: Run all MiSTer preparation tests**

Run: `go test -race ./internal/core ./internal/mister && go vet ./internal/core ./internal/mister`

Expected: PASS, including the symlink escape and XML escaping cases.

- [ ] **Step 8: Commit secure launch preparation**

```bash
git add internal/mister
git commit -m "feat: validate ROM paths and stage MGL atomically"
```

---

### Task 4: MiSTer Runtime Adapter

**Files:**
- Create: `internal/mister/runtime.go`
- Create: `internal/mister/process.go`
- Test: `internal/mister/runtime_test.go`
- Test: `internal/mister/process_test.go`

**Interfaces:**
- Consumes: `PreparedLaunch`, `WriteAtomicMGL`, `core.Registry`, and protocol health/status/error types.
- Produces: `mister.Paths`, `mister.CommandWriter`, `mister.ProcessChecker`, `mister.Runtime`, `Runtime.Health(string) protocol.Health`, `Runtime.Reconcile(context.Context) protocol.Status`, `Runtime.Prepare(core.Spec, string)`, `Runtime.Launch(context.Context, PreparedLaunch)`, and `Runtime.Stop(context.Context)`.

- [ ] **Step 1: Write failing runtime tests with injected fakes**

Create `internal/mister/runtime_test.go` around these fakes and assertions:

```go
package mister_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/clawzai2-tech/mister-remote/internal/core"
	"github.com/clawzai2-tech/mister-remote/internal/mister"
	"github.com/clawzai2-tech/mister-remote/protocol"
)

type fakeWriter struct {
	mu sync.Mutex
	commands []string
	onWrite func(string)
}

func (f *fakeWriter) Write(_ context.Context, command string) error {
	f.mu.Lock()
	f.commands = append(f.commands, command)
	f.mu.Unlock()
	if f.onWrite != nil { f.onWrite(command) }
	return nil
}

type fixedProcess bool
func (f fixedProcess) Running(string) bool { return bool(f) }

func TestRuntimeLaunchAndStop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	coreName := filepath.Join(dir, "CORENAME")
	commandPipe := filepath.Join(dir, "MiSTer_cmd")
	if err := os.WriteFile(commandPipe, nil, 0o600); err != nil { t.Fatal(err) }
	writer := &fakeWriter{}
	registry := core.DefaultRegistry()
	runtime := mister.NewRuntime(mister.Paths{MiSTerProcessComm: "MiSTer", CommandPipe: commandPipe, CoreNameFile: coreName, MenuRBF: "/media/fat/menu.rbf", MGLDirectory: filepath.Join(dir, "mgl")}, registry, writer, fixedProcess(true), 5*time.Millisecond)
	spec, _ := registry.Lookup(protocol.SystemMegaDrive)
	romRoot := filepath.Join(dir, "games", "MegaDrive")
	if err := os.MkdirAll(romRoot, 0o755); err != nil { t.Fatal(err) }
	rom := filepath.Join(romRoot, "test.md")
	if err := os.WriteFile(rom, []byte("rom"), 0o644); err != nil { t.Fatal(err) }
	spec.ROMRoot = romRoot
	prepared, apiErr := runtime.Prepare(spec, rom)
	if apiErr != nil { t.Fatal(apiErr) }
	writer.onWrite = func(command string) {
		name := "MegaDrive"
		if command == "load_core /media/fat/menu.rbf\n" { name = "MENU" }
		if err := os.WriteFile(coreName, []byte(name+"\n"), 0o600); err != nil { panic(err) }
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	observed, apiErr := runtime.Launch(ctx, prepared)
	if apiErr != nil || observed != "MegaDrive" { t.Fatalf("launch = %q, %#v", observed, apiErr) }
	observed, apiErr = runtime.Stop(ctx)
	if apiErr != nil || observed != "MENU" { t.Fatalf("stop = %q, %#v", observed, apiErr) }
}

func TestRuntimeLaunchTimeout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	coreName := filepath.Join(dir, "CORENAME")
	commandPipe := filepath.Join(dir, "MiSTer_cmd")
	if err := os.WriteFile(commandPipe, nil, 0o600); err != nil { t.Fatal(err) }
	if err := os.WriteFile(coreName, []byte("MENU\n"), 0o600); err != nil { t.Fatal(err) }
	runtime := mister.NewRuntime(mister.Paths{MiSTerProcessComm: "MiSTer", CommandPipe: commandPipe, CoreNameFile: coreName, MenuRBF: "/media/fat/menu.rbf", MGLDirectory: filepath.Join(dir, "mgl")}, core.DefaultRegistry(), &fakeWriter{}, fixedProcess(true), time.Millisecond)
	prepared := mister.PreparedLaunch{Spec: core.Spec{ExpectedCore: "SNES"}, MGL: []byte("mgl\n")}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	observed, apiErr := runtime.Launch(ctx, prepared)
	if apiErr == nil || apiErr.Code != protocol.CodeCoreTimeout || observed != "MENU" {
		t.Fatalf("launch timeout = %q, %#v", observed, apiErr)
	}
}
```

Add these complete health and reconciliation tests in the same file:

```go
func TestRuntimeHealthRequiresProcessAndPipe(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pipe := filepath.Join(dir, "MiSTer_cmd")
	paths := mister.Paths{MiSTerProcessComm: "MiSTer", CommandPipe: pipe, CoreNameFile: filepath.Join(dir, "CORENAME"), MenuRBF: "/media/fat/menu.rbf", MGLDirectory: filepath.Join(dir, "mgl")}
	withoutPipe := mister.NewRuntime(paths, core.DefaultRegistry(), &fakeWriter{}, fixedProcess(true), time.Millisecond).Health("0.1.0")
	if withoutPipe.Ready || !withoutPipe.MiSTerProcess || withoutPipe.CommandPipe { t.Fatalf("health without pipe = %#v", withoutPipe) }
	if err := os.WriteFile(pipe, nil, 0o600); err != nil { t.Fatal(err) }
	withoutProcess := mister.NewRuntime(paths, core.DefaultRegistry(), &fakeWriter{}, fixedProcess(false), time.Millisecond).Health("0.1.0")
	if withoutProcess.Ready || withoutProcess.MiSTerProcess || !withoutProcess.CommandPipe { t.Fatalf("health without process = %#v", withoutProcess) }
	ready := mister.NewRuntime(paths, core.DefaultRegistry(), &fakeWriter{}, fixedProcess(true), time.Millisecond).Health("0.1.0")
	if !ready.Ready || ready.AgentVersion != "0.1.0" || ready.APIVersion != "v1" { t.Fatalf("ready health = %#v", ready) }
}

func TestReconcileCoreNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, coreName string
		create bool
		state protocol.State
		system *protocol.System
		code protocol.ErrorCode
	}{
		{name: "menu", coreName: "MENU", create: true, state: protocol.StateIdle},
		{name: "registered", coreName: "SNES", create: true, state: protocol.StateActive, system: func() *protocol.System { v := protocol.SystemSNES; return &v }()},
		{name: "unknown", coreName: "NES", create: true, state: protocol.StateFailed, code: protocol.CodeUnrecognizedCore},
		{name: "missing", create: false, state: protocol.StateFailed, code: protocol.CodeMiSTerUnavailable},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			coreNameFile := filepath.Join(dir, "CORENAME")
			commandPipe := filepath.Join(dir, "MiSTer_cmd")
			if err := os.WriteFile(commandPipe, nil, 0o600); err != nil { t.Fatal(err) }
			if tt.create {
				if err := os.WriteFile(coreNameFile, []byte(tt.coreName+"\n"), 0o600); err != nil { t.Fatal(err) }
			}
			runtime := mister.NewRuntime(mister.Paths{MiSTerProcessComm: "MiSTer", CommandPipe: commandPipe, CoreNameFile: coreNameFile, MenuRBF: "/media/fat/menu.rbf", MGLDirectory: filepath.Join(dir, "mgl")}, core.DefaultRegistry(), &fakeWriter{}, fixedProcess(true), time.Millisecond)
			ctx := context.Background()
			if !tt.create {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 10*time.Millisecond)
				defer cancel()
			}
			status := runtime.Reconcile(ctx)
			if status.State != tt.state || status.GameID != nil { t.Fatalf("status = %#v", status) }
			if tt.system != nil && (status.System == nil || *status.System != *tt.system) { t.Fatalf("system = %#v", status.System) }
			if tt.code != "" && (status.LastError == nil || status.LastError.Code != tt.code) { t.Fatalf("last error = %#v", status.LastError) }
		})
	}
}
```

- [ ] **Step 2: Run the runtime tests and verify they fail**

Run: `go test ./internal/mister -run 'TestRuntime|TestReconcile' -v`

Expected: FAIL with undefined runtime interfaces and constructor.

- [ ] **Step 3: Implement runtime dependencies and constructor**

Create the following foundation in `internal/mister/runtime.go`:

```go
package mister

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/clawzai2-tech/mister-remote/internal/core"
	"github.com/clawzai2-tech/mister-remote/protocol"
)

type Paths struct {
	MiSTerProcessComm string
	CommandPipe       string
	CoreNameFile      string
	MenuRBF           string
	MGLDirectory      string
}

type CommandWriter interface { Write(context.Context, string) error }
type ProcessChecker interface { Running(string) bool }

type Runtime struct {
	paths Paths
	registry core.Registry
	writer CommandWriter
	process ProcessChecker
	pollInterval time.Duration
}

func NewRuntime(paths Paths, registry core.Registry, writer CommandWriter, process ProcessChecker, pollInterval time.Duration) *Runtime {
	return &Runtime{paths: paths, registry: registry, writer: writer, process: process, pollInterval: pollInterval}
}

func (r *Runtime) Prepare(spec core.Spec, romPath string) (PreparedLaunch, *protocol.APIError) {
	return PrepareLaunch(spec, romPath)
}
```

Implement `FileCommandWriter.Write` so it opens the configured device for write, writes the complete newline-terminated command, and returns `ctx.Err()` when the context wins. The result channel must be buffered so the worker can finish after cancellation:

```go
type FileCommandWriter struct { Path string }

func (w FileCommandWriter) Write(ctx context.Context, command string) error {
	done := make(chan error, 1)
	go func() {
		f, err := os.OpenFile(w.Path, os.O_WRONLY, 0)
		if err == nil {
			_, err = f.WriteString(command)
			if closeErr := f.Close(); err == nil { err = closeErr }
		}
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
```

- [ ] **Step 4: Implement health, observation, launch, stop, and reconciliation**

Use these exact operations in `runtime.go`:

```go
func (r *Runtime) readCoreName() (string, error) {
	b, err := os.ReadFile(r.paths.CoreNameFile)
	if err != nil { return "", err }
	return strings.TrimSpace(string(b)), nil
}

func (r *Runtime) observe(ctx context.Context, expected string) (string, *protocol.APIError) {
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()
	last := ""
	for {
		if current, err := r.readCoreName(); err == nil {
			last = current
			if current == expected { return current, nil }
		}
		select {
		case <-ctx.Done():
			return last, &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "expected core did not appear before the deadline"}
		case <-ticker.C:
		}
	}
}

func (r *Runtime) Launch(ctx context.Context, prepared PreparedLaunch) (string, *protocol.APIError) {
	path, err := WriteAtomicMGL(r.paths.MGLDirectory, prepared.MGL)
	if err != nil { return r.currentCore(), &protocol.APIError{Code: protocol.CodeInternal, Message: "transient MGL could not be installed"} }
	if err := r.writer.Write(ctx, "load_core "+path+"\n"); err != nil { return r.currentCore(), &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "MiSTer command could not be dispatched"} }
	return r.observe(ctx, prepared.Spec.ExpectedCore)
}

func (r *Runtime) Stop(ctx context.Context) (string, *protocol.APIError) {
	if err := r.writer.Write(ctx, "load_core "+r.paths.MenuRBF+"\n"); err != nil { return r.currentCore(), &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "Menu-core command could not be dispatched"} }
	return r.observe(ctx, "MENU")
}

func (r *Runtime) currentCore() string {
	name, _ := r.readCoreName()
	return name
}
```

Complete the two methods with these rules:

```go
func (r *Runtime) Health(version string) protocol.Health {
	process := r.process.Running(r.paths.MiSTerProcessComm)
	_, pipeErr := os.Stat(r.paths.CommandPipe)
	pipe := pipeErr == nil
	return protocol.Health{APIVersion: "v1", AgentVersion: version, Ready: process && pipe, MiSTerProcess: process, CommandPipe: pipe}
}

func (r *Runtime) Reconcile(ctx context.Context) protocol.Status {
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()
	for {
		if r.Health("").Ready {
			if name, err := r.readCoreName(); err == nil {
				if name == "MENU" { return protocol.Status{State: protocol.StateIdle} }
				if spec, ok := r.registry.LookupObserved(name); ok {
					system, expected, observed := spec.System, spec.ExpectedCore, name
					return protocol.Status{State: protocol.StateActive, System: &system, ExpectedCore: &expected, ObservedCore: &observed}
				}
				observed := name
				return protocol.Status{State: protocol.StateFailed, ObservedCore: &observed, LastError: &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "observed core is not registered"}}
			}
		}
		select {
		case <-ctx.Done():
			return protocol.Status{State: protocol.StateFailed, LastError: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "MiSTer dependencies did not become ready before the startup deadline"}}
		case <-ticker.C:
		}
	}
}
```

`Reconcile` must never write a command.

- [ ] **Step 5: Implement and test `/proc` process detection**

Create `internal/mister/process.go`:

```go
package mister

import (
	"os"
	"path/filepath"
	"strings"
)

type ProcProcessChecker struct { Root string }

func (p ProcProcessChecker) Running(comm string) bool {
	entries, err := filepath.Glob(filepath.Join(p.Root, "[0-9]*", "comm"))
	if err != nil { return false }
	for _, path := range entries {
		b, err := os.ReadFile(path)
		if err == nil && strings.TrimSpace(string(b)) == comm { return true }
	}
	return false
}
```

Create `internal/mister/process_test.go` using a temporary `123/comm` file containing `MiSTer\n`; assert `Running("MiSTer")` is true and `Running("other")` is false.

- [ ] **Step 6: Run the complete runtime test cycle**

Run: `go test -race ./internal/mister -v && go vet ./internal/mister`

Expected: PASS. The timeout test must finish in under one second and report the last observed `MENU` core.

- [ ] **Step 7: Commit the runtime adapter**

```bash
git add internal/mister
git commit -m "feat: add MiSTer runtime adapter"
```

---

### Task 5: Launch Coordinator and State Machine

**Files:**
- Create: `internal/agent/coordinator.go`
- Test: `internal/agent/coordinator_test.go`

**Interfaces:**
- Consumes: `core.Registry`, `mister.PreparedLaunch`, and the runtime methods produced by Task 4.
- Produces: `agent.Runtime`, `agent.Coordinator`, `agent.New(Runtime, core.Registry, time.Duration, time.Duration)`, `Coordinator.Initialize(context.Context)`, `Coordinator.Health(string)`, `Coordinator.Status()`, `Coordinator.Launch(context.Context, protocol.LaunchRequest)`, and `Coordinator.Stop(context.Context)`.

- [ ] **Step 1: Write failing state-transition tests**

Create `internal/agent/coordinator_test.go` with a configurable fake implementing the exact `agent.Runtime` interface:

```go
package agent_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/clawzai2-tech/mister-remote/internal/agent"
	"github.com/clawzai2-tech/mister-remote/internal/core"
	"github.com/clawzai2-tech/mister-remote/internal/mister"
	"github.com/clawzai2-tech/mister-remote/protocol"
)

type fakeRuntime struct {
	mu sync.Mutex
	health protocol.Health
	reconciled protocol.Status
	prepared mister.PreparedLaunch
	prepareErr *protocol.APIError
	launchObserved string
	launchErr *protocol.APIError
	stopObserved string
	stopErr *protocol.APIError
	launchGate chan struct{}
}

func (f *fakeRuntime) Health(string) protocol.Health { return f.health }
func (f *fakeRuntime) Reconcile(context.Context) protocol.Status { return f.reconciled }
func (f *fakeRuntime) Prepare(spec core.Spec, path string) (mister.PreparedLaunch, *protocol.APIError) {
	if f.prepareErr != nil { return mister.PreparedLaunch{}, f.prepareErr }
	result := f.prepared
	result.Spec = spec
	return result, nil
}
func (f *fakeRuntime) Launch(context.Context, mister.PreparedLaunch) (string, *protocol.APIError) {
	if f.launchGate != nil { <-f.launchGate }
	return f.launchObserved, f.launchErr
}
func (f *fakeRuntime) Stop(context.Context) (string, *protocol.APIError) { return f.stopObserved, f.stopErr }

func TestLaunchTransitionsToActive(t *testing.T) {
	t.Parallel()
	runtime := &fakeRuntime{health: protocol.Health{Ready: true}, launchObserved: "MegaDrive"}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	status, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{GameID: "megadrive-test", System: protocol.SystemMegaDrive, ROMPath: "/media/fat/games/MegaDrive/test.md"})
	if apiErr != nil { t.Fatal(apiErr) }
	if status.State != protocol.StateActive || status.GameID == nil || *status.GameID != "megadrive-test" || status.ObservedCore == nil || *status.ObservedCore != "MegaDrive" {
		t.Fatalf("status = %#v", status)
	}
}

func TestConcurrentTransitionReturnsBusy(t *testing.T) {
	t.Parallel()
	gate := make(chan struct{})
	runtime := &fakeRuntime{health: protocol.Health{Ready: true}, launchObserved: "MegaDrive", launchGate: gate}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = coordinator.Launch(context.Background(), protocol.LaunchRequest{GameID: "megadrive-test", System: protocol.SystemMegaDrive, ROMPath: "/media/fat/games/MegaDrive/test.md"})
	}()
	for coordinator.Status().State != protocol.StateLaunching { time.Sleep(time.Millisecond) }
	_, apiErr := coordinator.Stop(context.Background())
	if apiErr == nil || apiErr.Code != protocol.CodeBusy { t.Fatalf("stop error = %#v", apiErr) }
	close(gate)
	<-done
}
```

Add explicit tests with full assertions for these cases:

```go
func TestInitializeUsesReconciledStatus(t *testing.T) {
	runtime := &fakeRuntime{reconciled: protocol.Status{State: protocol.StateActive, System: systemPtr(protocol.SystemSNES), ObservedCore: stringPtr("SNES")}}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	coordinator.Initialize(context.Background())
	status := coordinator.Status()
	if status.State != protocol.StateActive || status.GameID != nil || status.System == nil || *status.System != protocol.SystemSNES { t.Fatalf("status = %#v", status) }
}

func TestHealthRemainsNotReadyAfterUnavailableReconciliation(t *testing.T) {
	runtime := &fakeRuntime{
		health: protocol.Health{Ready: true, MiSTerProcess: true, CommandPipe: true},
		reconciled: protocol.Status{State: protocol.StateFailed, LastError: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "not ready"}},
	}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	coordinator.Initialize(context.Background())
	if health := coordinator.Health("0.1.0"); health.Ready { t.Fatalf("health = %#v", health) }
}

func TestInvalidLaunchPreservesPreviousState(t *testing.T) {
	runtime := &fakeRuntime{health: protocol.Health{Ready: true}, prepareErr: &protocol.APIError{Code: protocol.CodeInvalidROMPath, Message: "invalid"}}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	before := coordinator.Status()
	_, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{GameID: "snes-test", System: protocol.SystemSNES, ROMPath: "relative.sfc"})
	if apiErr == nil || apiErr.Code != protocol.CodeInvalidROMPath { t.Fatalf("error = %#v", apiErr) }
	if after := coordinator.Status(); after.State != before.State || after.LastError != before.LastError { t.Fatalf("state changed: before=%#v after=%#v", before, after) }
}

func TestLaunchTimeoutTransitionsToFailed(t *testing.T) {
	runtime := &fakeRuntime{health: protocol.Health{Ready: true}, launchObserved: "MENU", launchErr: &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "timeout"}}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	_, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{GameID: "snes-test", System: protocol.SystemSNES, ROMPath: "/media/fat/games/SNES/test.sfc"})
	if apiErr == nil || apiErr.Code != protocol.CodeCoreTimeout { t.Fatalf("error = %#v", apiErr) }
	status := coordinator.Status()
	if status.State != protocol.StateFailed || status.ObservedCore == nil || *status.ObservedCore != "MENU" || status.LastError == nil || status.LastError.Code != protocol.CodeCoreTimeout { t.Fatalf("status = %#v", status) }
}

func TestStopIsIdempotentWhenIdle(t *testing.T) {
	coordinator := agent.New(&fakeRuntime{health: protocol.Health{Ready: true}}, core.DefaultRegistry(), time.Second, time.Second)
	status, apiErr := coordinator.Stop(context.Background())
	if apiErr != nil || status.State != protocol.StateIdle { t.Fatalf("stop = %#v, %#v", status, apiErr) }
}
```

Define `stringPtr` and `systemPtr` in the test file as one-line address helpers.

- [ ] **Step 2: Run coordinator tests and verify they fail**

Run: `go test ./internal/agent -run 'TestLaunch|TestConcurrent|TestInitialize|TestInvalid|TestStop' -v`

Expected: FAIL because package `internal/agent` does not exist.

- [ ] **Step 3: Implement the runtime boundary and transition semaphore**

Create the foundation of `internal/agent/coordinator.go`:

```go
package agent

import (
	"context"
	"sync"
	"time"

	"github.com/clawzai2-tech/mister-remote/internal/core"
	"github.com/clawzai2-tech/mister-remote/internal/mister"
	"github.com/clawzai2-tech/mister-remote/protocol"
)

type Runtime interface {
	Health(string) protocol.Health
	Reconcile(context.Context) protocol.Status
	Prepare(core.Spec, string) (mister.PreparedLaunch, *protocol.APIError)
	Launch(context.Context, mister.PreparedLaunch) (string, *protocol.APIError)
	Stop(context.Context) (string, *protocol.APIError)
}

type Coordinator struct {
	runtime Runtime
	registry core.Registry
	launchTimeout time.Duration
	stopTimeout time.Duration
	transition chan struct{}
	mu sync.RWMutex
	status protocol.Status
}

func New(runtime Runtime, registry core.Registry, launchTimeout, stopTimeout time.Duration) *Coordinator {
	return &Coordinator{runtime: runtime, registry: registry, launchTimeout: launchTimeout, stopTimeout: stopTimeout, transition: make(chan struct{}, 1), status: protocol.Status{State: protocol.StateIdle}}
}

func (c *Coordinator) begin() bool {
	select { case c.transition <- struct{}{}: return true; default: return false }
}
func (c *Coordinator) end() { <-c.transition }
func (c *Coordinator) set(status protocol.Status) { c.mu.Lock(); c.status = status; c.mu.Unlock() }
func (c *Coordinator) Status() protocol.Status { c.mu.RLock(); defer c.mu.RUnlock(); return cloneStatus(c.status) }
func (c *Coordinator) Health(version string) protocol.Health {
	health := c.runtime.Health(version)
	status := c.Status()
	if status.State == protocol.StateFailed && status.LastError != nil && status.LastError.Code == protocol.CodeMiSTerUnavailable {
		health.Ready = false
	}
	return health
}
func (c *Coordinator) Initialize(ctx context.Context) { c.set(c.runtime.Reconcile(ctx)) }
```

`cloneStatus` must copy pointed-to strings, system, and `APIError` so callers cannot mutate coordinator state.

- [ ] **Step 4: Implement launch and stop transitions**

Implement `Launch` in this order:

```go
func (c *Coordinator) Launch(parent context.Context, request protocol.LaunchRequest) (protocol.Status, *protocol.APIError) {
	if !c.begin() { return c.Status(), &protocol.APIError{Code: protocol.CodeBusy, Message: "another launch or stop transition is running"} }
	defer c.end()
	if err := protocol.ValidateGameID(request.GameID); err != nil { return c.Status(), &protocol.APIError{Code: protocol.CodeBadRequest, Message: err.Error()} }
	spec, ok := c.registry.Lookup(request.System)
	if !ok { return c.Status(), &protocol.APIError{Code: protocol.CodeUnsupportedSystem, Message: "system is not Mega Drive or SNES"} }
	if !c.runtime.Health("").Ready { return c.Status(), &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "Main_MiSTer or command pipe is unavailable"} }
	prepared, apiErr := c.runtime.Prepare(spec, request.ROMPath)
	if apiErr != nil { return c.Status(), apiErr }
	gameID, system, expected := request.GameID, request.System, spec.ExpectedCore
	c.set(protocol.Status{State: protocol.StateLaunching, GameID: &gameID, System: &system, ExpectedCore: &expected})
	ctx, cancel := context.WithTimeout(parent, c.launchTimeout)
	defer cancel()
	observed, apiErr := c.runtime.Launch(ctx, prepared)
	if apiErr != nil {
		failed := protocol.Status{State: protocol.StateFailed, GameID: &gameID, System: &system, ExpectedCore: &expected, LastError: apiErr}
		if observed != "" { failed.ObservedCore = &observed }
		c.set(failed)
		return c.Status(), apiErr
	}
	active := protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ExpectedCore: &expected, ObservedCore: &observed}
	c.set(active)
	return c.Status(), nil
}
```

Implement `Stop` with the same immediate `BUSY` behavior. Return the current idle status without calling the runtime when already idle. Otherwise require readiness, set `stopping`, use `stopTimeout`, call `runtime.Stop`, and set either a clean idle status or failed status with the last observed core and exact error.

- [ ] **Step 5: Run state-machine tests and the race detector**

Run: `go test -race ./internal/agent -v && go vet ./internal/agent`

Expected: PASS. `TestConcurrentTransitionReturnsBusy` must not report a race.

- [ ] **Step 6: Commit the coordinator**

```bash
git add internal/agent
git commit -m "feat: coordinate MiSTer launch state"
```

---

### Task 6: Target Configuration and HTTP API

**Files:**
- Modify: `go.mod`
- Create: `go.sum`
- Create: `internal/agentconfig/config.go`
- Test: `internal/agentconfig/config_test.go`
- Create: `internal/httpapi/server.go`
- Test: `internal/httpapi/server_test.go`

**Interfaces:**
- Consumes: `agent.Coordinator` and all protocol request/response types.
- Produces: `agentconfig.Config`, `agentconfig.Load(string) (Config, error)`, `httpapi.Controller`, and `httpapi.New(Controller, string, string, *slog.Logger) http.Handler`.

- [ ] **Step 1: Write failing strict target-config tests**

Create `internal/agentconfig/config_test.go`:

```go
package agentconfig_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/clawzai2-tech/mister-remote/internal/agentconfig"
)

const validAgentConfig = `listen_address = "0.0.0.0:8182"
token = "test-token"
mister_process_comm = "MiSTer"
command_pipe = "/dev/MiSTer_cmd"
core_name_file = "/tmp/CORENAME"
menu_rbf = "/media/fat/menu.rbf"
mgl_directory = "/tmp/mister-remote"
`

func TestLoadAgentConfig(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "agent.toml")
	if err := os.WriteFile(path, []byte(validAgentConfig), 0o600); err != nil { t.Fatal(err) }
	got, err := agentconfig.Load(path)
	if err != nil { t.Fatal(err) }
	if got.ListenAddress != "0.0.0.0:8182" || got.Token != "test-token" || got.CommandPipe != "/dev/MiSTer_cmd" { t.Fatalf("config = %#v", got) }
}

func TestLoadAgentConfigRejectsUnknownAndUnsafeValues(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"unknown": validAgentConfig + "extra = true\n",
		"empty token":  strings.Replace(validAgentConfig, `token = "test-token"`, `token = ""`, 1),
		"relative pipe": strings.Replace(validAgentConfig, `command_pipe = "/dev/MiSTer_cmd"`, `command_pipe = "MiSTer_cmd"`, 1),
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agent.toml")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil { t.Fatal(err) }
			if _, err := agentconfig.Load(path); err == nil { t.Fatal("invalid config loaded") }
		})
	}
}
```

Import `strings` in this test file.

- [ ] **Step 2: Run config tests and verify they fail**

Run: `go test ./internal/agentconfig -v`

Expected: FAIL because package `internal/agentconfig` does not exist.

- [ ] **Step 3: Implement strict target configuration**

Pin the only third-party dependency before creating the implementation:

Run: `go get github.com/pelletier/go-toml/v2@v2.4.3`

Expected: `go.mod` contains the exact v2.4.3 requirement and `go.sum` contains its module checksums.

Create `internal/agentconfig/config.go`:

```go
package agentconfig

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	ListenAddress     string `toml:"listen_address"`
	Token             string `toml:"token"`
	MiSTerProcessComm string `toml:"mister_process_comm"`
	CommandPipe       string `toml:"command_pipe"`
	CoreNameFile      string `toml:"core_name_file"`
	MenuRBF           string `toml:"menu_rbf"`
	MGLDirectory      string `toml:"mgl_directory"`
}

func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil { return Config{}, err }
	defer f.Close()
	var cfg Config
	decoder := toml.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil { return Config{}, fmt.Errorf("decode target config: %w", err) }
	if _, _, err := net.SplitHostPort(cfg.ListenAddress); err != nil { return Config{}, fmt.Errorf("listen_address: %w", err) }
	if cfg.Token == "" || cfg.MiSTerProcessComm == "" { return Config{}, fmt.Errorf("token and mister_process_comm must not be empty") }
	for name, value := range map[string]string{"command_pipe": cfg.CommandPipe, "core_name_file": cfg.CoreNameFile, "menu_rbf": cfg.MenuRBF, "mgl_directory": cfg.MGLDirectory} {
		if !filepath.IsAbs(value) { return Config{}, fmt.Errorf("%s must be absolute", name) }
	}
	return cfg, nil
}
```

- [ ] **Step 4: Write failing HTTP contract tests**

Create this fake and table-driven contract test in `internal/httpapi/server_test.go`:

```go
type fakeController struct {
	health protocol.Health
	status protocol.Status
	launchErr *protocol.APIError
}
func (f *fakeController) Health(string) protocol.Health { return f.health }
func (f *fakeController) Status() protocol.Status { return f.status }
func (f *fakeController) Launch(context.Context, protocol.LaunchRequest) (protocol.Status, *protocol.APIError) { return f.status, f.launchErr }
func (f *fakeController) Stop(context.Context) (protocol.Status, *protocol.APIError) { return protocol.Status{State: protocol.StateIdle}, nil }

func TestAPIContract(t *testing.T) {
	t.Parallel()
	active := protocol.Status{State: protocol.StateActive}
	tests := []struct {
		name, method, path, body, authorization string
		launchErr *protocol.APIError
		wantStatus int
		wantCode protocol.ErrorCode
	}{
		{name: "health without auth", method: http.MethodGet, path: "/v1/health", wantStatus: http.StatusOK},
		{name: "status missing auth", method: http.MethodGet, path: "/v1/status", wantStatus: http.StatusUnauthorized, wantCode: protocol.CodeUnauthorized},
		{name: "status token without bearer scheme", method: http.MethodGet, path: "/v1/status", authorization: "test-token", wantStatus: http.StatusUnauthorized, wantCode: protocol.CodeUnauthorized},
		{name: "status wrong auth", method: http.MethodGet, path: "/v1/status", authorization: "Bearer wrong", wantStatus: http.StatusUnauthorized, wantCode: protocol.CodeUnauthorized},
		{name: "status valid auth", method: http.MethodGet, path: "/v1/status", authorization: "Bearer test-token", wantStatus: http.StatusOK},
		{name: "launch unknown field", method: http.MethodPost, path: "/v1/launch", authorization: "Bearer test-token", body: `{"game_id":"megadrive-test","system":"megadrive","rom_path":"/media/fat/games/MegaDrive/test.md","rbf":"bad"}`, wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
		{name: "launch active", method: http.MethodPost, path: "/v1/launch", authorization: "Bearer test-token", body: `{"game_id":"megadrive-test","system":"megadrive","rom_path":"/media/fat/games/MegaDrive/test.md"}`, wantStatus: http.StatusOK},
		{name: "launch missing ROM", method: http.MethodPost, path: "/v1/launch", authorization: "Bearer test-token", body: `{"game_id":"megadrive-test","system":"megadrive","rom_path":"/media/fat/games/MegaDrive/test.md"}`, launchErr: &protocol.APIError{Code: protocol.CodeROMNotFound, Message: "missing"}, wantStatus: http.StatusNotFound, wantCode: protocol.CodeROMNotFound},
		{name: "stop", method: http.MethodPost, path: "/v1/stop", authorization: "Bearer test-token", wantStatus: http.StatusOK},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			controller := &fakeController{health: protocol.Health{Ready: true}, status: active, launchErr: tt.launchErr}
			handler := httpapi.New(controller, "test-token", "0.1.0", slog.New(slog.NewJSONHandler(io.Discard, nil)))
			request := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.authorization != "" { request.Header.Set("Authorization", tt.authorization) }
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tt.wantStatus { t.Fatalf("status = %d, body=%s", response.Code, response.Body.String()) }
			if tt.wantCode != "" {
				var envelope protocol.ErrorEnvelope
				if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil { t.Fatal(err) }
				if envelope.Error.Code != tt.wantCode { t.Fatalf("code = %s", envelope.Error.Code) }
			}
		})
	}
}

func TestTokenNeverAppearsInLogs(t *testing.T) {
	var logs bytes.Buffer
	handler := httpapi.New(&fakeController{health: protocol.Health{Ready: true}}, "test-token", "0.1.0", slog.New(slog.NewJSONHandler(&logs, nil)))
	request := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if strings.Contains(logs.String(), "test-token") { t.Fatalf("token leaked in logs: %s", logs.String()) }
}
```

- [ ] **Step 5: Run HTTP tests and verify they fail**

Run: `go test ./internal/httpapi -v`

Expected: FAIL because package `internal/httpapi` does not exist.

- [ ] **Step 6: Implement authenticated strict JSON routes**

Create `internal/httpapi/server.go` with this boundary:

```go
package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/clawzai2-tech/mister-remote/protocol"
)

type Controller interface {
	Health(string) protocol.Health
	Status() protocol.Status
	Launch(context.Context, protocol.LaunchRequest) (protocol.Status, *protocol.APIError)
	Stop(context.Context) (protocol.Status, *protocol.APIError)
}

func New(controller Controller, token string, version string, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, controller.Health(version)) })
	mux.Handle("GET /v1/status", authenticate(token, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, controller.Status()) })))
	mux.Handle("POST /v1/launch", authenticate(token, launchHandler(controller)))
	mux.Handle("POST /v1/stop", authenticate(token, stopHandler(controller)))
	return requestLogger(logger, mux)
}

func authenticate(token string, next http.Handler) http.Handler {
	expected := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization := r.Header.Get("Authorization")
		if !strings.HasPrefix(authorization, "Bearer ") {
			writeAPIError(w, http.StatusUnauthorized, &protocol.APIError{Code: protocol.CodeUnauthorized, Message: "missing or incorrect bearer token"})
			return
		}
		provided := strings.TrimPrefix(authorization, "Bearer ")
		actual := sha256.Sum256([]byte(provided))
		if subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
			writeAPIError(w, http.StatusUnauthorized, &protocol.APIError{Code: protocol.CodeUnauthorized, Message: "missing or incorrect bearer token"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

`launchHandler` must wrap the body with `http.MaxBytesReader(w, r.Body, 64<<10)`, call `DisallowUnknownFields`, reject a second JSON value, and map the design error codes to their exact HTTP statuses. `stopHandler` must reject a non-empty body. `writeJSON` must set `Content-Type: application/json`, use an `Encoder`, and end responses with a newline. `requestLogger` may log method, path, status, duration, game ID, system, state, and symbolic error code; it must never log headers or request bodies.

- [ ] **Step 7: Run configuration and HTTP checks**

Run: `go test -race ./internal/agentconfig ./internal/httpapi && go vet ./internal/agentconfig ./internal/httpapi`

Expected: PASS with no token in captured logs.

- [ ] **Step 8: Commit target config and API**

```bash
git add go.mod go.sum internal/agentconfig internal/httpapi
git commit -m "feat: expose authenticated MiSTer control API"
```

---

### Task 7: Host Configuration, Manifest, and Client Library

**Files:**
- Create: `host/config.go`
- Create: `host/manifest.go`
- Create: `host/client.go`
- Create: `host/library.go`
- Test: `host/config_test.go`
- Test: `host/manifest_test.go`
- Test: `host/client_test.go`
- Test: `host/library_test.go`

**Interfaces:**
- Consumes: All public protocol types and the target API contract.
- Produces: `host.ConnectionConfig`, `host.Game`, `host.LoadConnection`, `host.LoadManifest`, `host.Client`, `host.NewClient`, `host.Library`, and `host.Open`.

- [ ] **Step 1: Write failing connection and manifest tests**

Create fixtures in each test with `t.TempDir()`. Use these exact types and assertions:

```go
func TestLoadConnectionResolvesRelativeManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := "base_url = \"http://192.0.2.10:8182\"\ntoken = \"test-token\"\nrequest_timeout_seconds = 12\nmanifest_path = \"games.toml\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { t.Fatal(err) }
	cfg, err := host.LoadConnection(path)
	if err != nil { t.Fatal(err) }
	if cfg.ManifestPath != filepath.Join(dir, "games.toml") || cfg.RequestTimeout != 12*time.Second { t.Fatalf("config = %#v", cfg) }
}

func TestLoadManifestRejectsDuplicatesAndUnsupportedSystems(t *testing.T) {
	tests := map[string]string{
		"duplicate": "[[games]]\nid=\"same\"\ntitle=\"One\"\nsystem=\"snes\"\nrom_path=\"/media/fat/games/SNES/one.sfc\"\n[[games]]\nid=\"same\"\ntitle=\"Two\"\nsystem=\"snes\"\nrom_path=\"/media/fat/games/SNES/two.sfc\"\n",
		"unsupported": "[[games]]\nid=\"nes-test\"\ntitle=\"NES\"\nsystem=\"nes\"\nrom_path=\"/media/fat/games/NES/test.nes\"\n",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "games.toml")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil { t.Fatal(err) }
			if _, err := host.LoadManifest(path); err == nil { t.Fatal("invalid manifest loaded") }
		})
	}
}
```

Add `TestLoadConnectionRejectsInvalidValues` as a table that starts from the valid connection text and substitutes each of these exact invalid values: base URL `192.0.2.10:8182`, base URL `http://user@192.0.2.10:8182`, empty token, timeout `0`, and the unknown key `discovery = true`. Add manifest rows with `rom_path="relative.sfc"`, `title=" "`, and `id="Bad ID"`; every row must assert a non-nil error.

- [ ] **Step 2: Run config and manifest tests and verify they fail**

Run: `go test ./host -run 'TestLoadConnection|TestLoadManifest' -v`

Expected: FAIL because package `host` does not exist.

- [ ] **Step 3: Implement strict host loading**

Define these public structures:

```go
package host

type ConnectionConfig struct {
	BaseURL        *url.URL
	Token          string
	RequestTimeout time.Duration
	ManifestPath   string
}

type Game struct {
	ID      string          `toml:"id" json:"id"`
	Title   string          `toml:"title" json:"title"`
	System  protocol.System `toml:"system" json:"system"`
	ROMPath string          `toml:"rom_path" json:"rom_path"`
}

type Manifest struct { Games []Game `toml:"games" json:"games"` }
```

Both loaders must use `toml.Decoder.DisallowUnknownFields`. `LoadConnection` must accept only the `http` scheme, reject URL userinfo and fragments, require a non-empty token and positive timeout, and resolve a relative `manifest_path` against `filepath.Dir(configPath)`. `LoadManifest` must call shared game-ID and system validation, require an absolute target ROM path, require a non-blank title, and reject duplicate IDs.

- [ ] **Step 4: Write failing HTTP client tests**

Use `httptest.Server` in these complete core tests:

```go
func TestClientAddsBearerAndDecodesStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/status" { t.Fatalf("request = %s %s", r.Method, r.URL.Path) }
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" { t.Fatalf("authorization = %q", got) }
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"state":"idle","game_id":null,"system":null,"expected_core":null,"observed_core":null,"last_error":null}`)
	}))
	defer server.Close()
	baseURL, err := url.Parse(server.URL)
	if err != nil { t.Fatal(err) }
	status, err := host.NewClient(baseURL, "test-token", server.Client()).Status(context.Background())
	if err != nil || status.State != protocol.StateIdle { t.Fatalf("status = %#v, %v", status, err) }
}

func TestClientDecodesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"ROM_NOT_FOUND","message":"missing"}}`)
	}))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL)
	_, err := host.NewClient(baseURL, "test-token", server.Client()).Launch(context.Background(), protocol.LaunchRequest{GameID: "snes-test", System: protocol.SystemSNES, ROMPath: "/media/fat/games/SNES/test.sfc"})
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeROMNotFound { t.Fatalf("error = %#v", err) }
}

func TestClientRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, strings.Repeat("x", (1<<20)+1)) }))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL)
	if _, err := host.NewClient(baseURL, "test-token", server.Client()).Status(context.Background()); err == nil { t.Fatal("oversized response accepted") }
}

func TestClientStopSendsEmptyPOST(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil { t.Fatal(err) }
		if r.Method != http.MethodPost || r.URL.Path != "/v1/stop" || len(body) != 0 { t.Fatalf("request = %s %s body=%q", r.Method, r.URL.Path, body) }
		_, _ = io.WriteString(w, `{"state":"idle","game_id":null,"system":null,"expected_core":null,"observed_core":null,"last_error":null}`)
	}))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL)
	if _, err := host.NewClient(baseURL, "test-token", server.Client()).Stop(context.Background()); err != nil { t.Fatal(err) }
}
```

Cap response bodies at 1 MiB before JSON decoding.

- [ ] **Step 5: Run client tests and verify they fail**

Run: `go test ./host -run 'TestClient' -v`

Expected: FAIL with undefined `host.NewClient`.

- [ ] **Step 6: Implement the typed host client**

Use this exported surface in `host/client.go`:

```go
type Client struct {
	baseURL *url.URL
	token string
	httpClient *http.Client
}

func NewClient(baseURL *url.URL, token string, httpClient *http.Client) *Client
func (c *Client) Health(context.Context) (protocol.Health, error)
func (c *Client) Status(context.Context) (protocol.Status, error)
func (c *Client) Launch(context.Context, protocol.LaunchRequest) (protocol.Status, error)
func (c *Client) Stop(context.Context) (protocol.Status, error)
```

Implement a private `doJSON(ctx, method, path string, requestBody any, responseBody any) error` that sets the bearer header except for health, sets JSON content type for POSTs, rejects responses larger than 1 MiB, decodes `protocol.ErrorEnvelope` for non-2xx responses, and returns an `*protocol.APIError` without replacing its code.

- [ ] **Step 7: Write and implement the library facade**

Create `host/library_test.go` asserting that `Games()` returns a defensive copy, `Launch(ctx, "snes-test")` sends the selected manifest entry, and an unknown ID fails without an HTTP request.

Implement this surface in `host/library.go`:

```go
type Library struct {
	games []Game
	byID map[string]Game
	client *Client
}

func Open(configPath string, httpClient *http.Client) (*Library, error)
func (l *Library) Games() []Game
func (l *Library) Health(context.Context) (protocol.Health, error)
func (l *Library) Status(context.Context) (protocol.Status, error)
func (l *Library) Launch(context.Context, string) (protocol.Status, error)
func (l *Library) Stop(context.Context) (protocol.Status, error)
```

`Open` loads connection and manifest files and creates a default `http.Client` using the configured 12-second timeout when the caller supplies `nil`.

- [ ] **Step 8: Run host library checks**

Run: `go test -race ./host -v && go vet ./host`

Expected: PASS, including the no-request unknown-ID assertion.

- [ ] **Step 9: Commit the host library**

```bash
git add host
git commit -m "feat: add macOS host control library"
```

---

### Task 8: `misterctl` CLI Adapter

**Files:**
- Create: `internal/cli/run.go`
- Test: `internal/cli/run_test.go`

**Interfaces:**
- Consumes: `host.Open`, the `host.Library` methods, and protocol JSON types.
- Produces: `cli.OpenLibrary`, `cli.Run(context.Context, []string, io.Writer, io.Writer, OpenLibrary) int` with stable exit codes `0` success, `1` operation failure, and `2` usage failure.

- [ ] **Step 1: Write failing command and output tests**

Create this fake library and command tests:

```go
type fakeLibrary struct {
	games []host.Game
	health protocol.Health
	status protocol.Status
	launchID string
	err error
}
func (f *fakeLibrary) Games() []host.Game { return f.games }
func (f *fakeLibrary) Health(context.Context) (protocol.Health, error) { return f.health, f.err }
func (f *fakeLibrary) Status(context.Context) (protocol.Status, error) { return f.status, f.err }
func (f *fakeLibrary) Launch(_ context.Context, id string) (protocol.Status, error) { f.launchID = id; return f.status, f.err }
func (f *fakeLibrary) Stop(context.Context) (protocol.Status, error) { return f.status, f.err }

func TestGamesHumanAndJSONOutput(t *testing.T) {
	games := []host.Game{
		{ID: "megadrive-test", Title: "Mega Drive test game", System: protocol.SystemMegaDrive},
		{ID: "snes-test", Title: "SNES test game", System: protocol.SystemSNES},
	}
	for _, tt := range []struct { name string; args []string; json bool }{
		{name: "human", args: []string{"--config", "ignored.toml", "games"}},
		{name: "json", args: []string{"--config", "ignored.toml", "--json", "games"}, json: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exit := cli.Run(context.Background(), tt.args, &stdout, &stderr, func(string) (cli.Library, error) { return &fakeLibrary{games: games}, nil })
			if exit != 0 || stderr.Len() != 0 { t.Fatalf("exit=%d stderr=%q", exit, stderr.String()) }
			if tt.json {
				var decoded []host.Game
				if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil || len(decoded) != 2 { t.Fatalf("JSON=%q error=%v", stdout.String(), err) }
				return
			}
			want := "megadrive-test  Mega Drive  Mega Drive test game\nsnes-test       SNES        SNES test game\n"
			if stdout.String() != want { t.Fatalf("output=%q want=%q", stdout.String(), want) }
		})
	}
}

func TestRunOperationAndErrorPaths(t *testing.T) {
	activeCore := "MegaDrive"
	active := protocol.Status{State: protocol.StateActive, ObservedCore: &activeCore}
	tests := []struct {
		name string
		args []string
		library *fakeLibrary
		wantExit int
		wantStdout, wantStderr, wantLaunchID string
	}{
		{name: "health", args: []string{"health"}, library: &fakeLibrary{health: protocol.Health{Ready: true}}, wantExit: 0, wantStdout: "ready\n"},
		{name: "status JSON", args: []string{"--json", "status"}, library: &fakeLibrary{status: active}, wantExit: 0},
		{name: "launch", args: []string{"launch", "megadrive-test"}, library: &fakeLibrary{status: active}, wantExit: 0, wantStdout: "active: megadrive-test (MegaDrive)\n", wantLaunchID: "megadrive-test"},
		{name: "stop", args: []string{"stop"}, library: &fakeLibrary{status: protocol.Status{State: protocol.StateIdle}}, wantExit: 0, wantStdout: "idle\n"},
		{name: "usage", args: []string{"launch"}, library: &fakeLibrary{}, wantExit: 2, wantStderr: "usage:"},
		{name: "API error", args: []string{"status"}, library: &fakeLibrary{err: &protocol.APIError{Code: protocol.CodeUnauthorized, Message: "bad token"}}, wantExit: 1, wantStderr: "UNAUTHORIZED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exit := cli.Run(context.Background(), tt.args, &stdout, &stderr, func(string) (cli.Library, error) { return tt.library, nil })
			if exit != tt.wantExit { t.Fatalf("exit=%d want=%d", exit, tt.wantExit) }
			if tt.wantStdout != "" && stdout.String() != tt.wantStdout { t.Fatalf("stdout=%q", stdout.String()) }
			if tt.wantStderr != "" && !strings.Contains(stderr.String(), tt.wantStderr) { t.Fatalf("stderr=%q", stderr.String()) }
			if tt.library.launchID != tt.wantLaunchID { t.Fatalf("launch ID=%q", tt.library.launchID) }
			if tt.name == "status JSON" { var status protocol.Status; if err := json.Unmarshal(stdout.Bytes(), &status); err != nil || status.State != protocol.StateActive { t.Fatalf("status JSON=%q error=%v", stdout.String(), err) } }
		})
	}
}
```

Use this exact expected human game output:

```text
megadrive-test  Mega Drive  Mega Drive test game
snes-test       SNES        SNES test game
```

Use `tabwriter.Writer` so columns align. JSON output must be one compact JSON value followed by a newline and must decode back into the public type it represents.

- [ ] **Step 2: Run CLI tests and verify they fail**

Run: `go test ./internal/cli -v`

Expected: FAIL because package `internal/cli` does not exist.

- [ ] **Step 3: Implement argument parsing and dependency injection**

Use this boundary in `internal/cli/run.go`:

```go
package cli

type Library interface {
	Games() []host.Game
	Health(context.Context) (protocol.Health, error)
	Status(context.Context) (protocol.Status, error)
	Launch(context.Context, string) (protocol.Status, error)
	Stop(context.Context) (protocol.Status, error)
}

type OpenLibrary func(configPath string) (Library, error)

func Run(ctx context.Context, args []string, stdout, stderr io.Writer, open OpenLibrary) int
```

Parse global flags with a private `flag.FlagSet`: obtain the home directory through `os.UserHomeDir`, build the default `--config` value with `filepath.Join(home, ".config", "mister-remote", "config.toml")`, and default `--json` to false. Accept exactly `games`, `health`, `status`, `launch <game-id>`, or `stop`. Disable the standard flag package's direct output and write concise usage text to `stderr` yourself.

- [ ] **Step 4: Implement stable human and JSON output**

For JSON, call `json.NewEncoder(stdout).Encode(value)`. Human launch output must be:

```text
active: megadrive-test (MegaDrive)
```

Human stop output must be `idle\n`. Human health output must be `ready\n` or `not ready\n`; not-ready health returns exit code `1`. Human failed status must include state, last observed core when present, and `last_error.code` without printing a token or ROM path.

- [ ] **Step 5: Run CLI behavior checks**

Run: `go test -race ./internal/cli -v && go vet ./internal/cli`

Expected: PASS for every command, output mode, and exit-code branch.

- [ ] **Step 6: Commit the CLI adapter**

```bash
git add internal/cli
git commit -m "feat: add misterctl command interface"
```

---

### Task 9: Process Entrypoints and End-to-End Integration

**Files:**
- Create: `internal/version/version.go`
- Create: `cmd/mister-agent/main.go`
- Create: `cmd/misterctl/main.go`
- Create: `internal/integration/remote_control_test.go`
- Modify: `Makefile`

**Interfaces:**
- Consumes: Target config, MiSTer runtime, coordinator, HTTP API, host library, and CLI from Tasks 1–8.
- Produces: Runnable `mister-agent`, `misterctl`, build-injected `version.Version`, and a real network contract test using the actual adapter with injected hardware boundaries.

- [ ] **Step 1: Write the failing end-to-end test**

Create `internal/integration/remote_control_test.go`. The fixture must:

1. Create temporary Mega Drive and SNES roots and one regular ROM in each.
2. Copy the two registry specs and replace only their ROM roots through a test registry injected into the coordinator.
3. Create temporary `CORENAME`, `MiSTer_cmd`, and MGL paths.
4. Use a fake `CommandWriter` that changes `CORENAME` to `MegaDrive`, `SNES`, or `MENU` based on the command and generated MGL.
5. Use the real `mister.Runtime`, `agent.Coordinator`, `httpapi.New`, `httptest.Server`, `host.Client`, and `host.Library` path.

The central assertions are:

```go
status, err := library.Launch(ctx, "megadrive-test")
if err != nil || status.State != protocol.StateActive || status.ObservedCore == nil || *status.ObservedCore != "MegaDrive" {
	t.Fatalf("Mega Drive launch = %#v, %v", status, err)
}
status, err = library.Launch(ctx, "snes-test")
if err != nil || status.ObservedCore == nil || *status.ObservedCore != "SNES" {
	t.Fatalf("SNES launch = %#v, %v", status, err)
}
status, err = library.Stop(ctx)
if err != nil || status.State != protocol.StateIdle {
	t.Fatalf("stop = %#v, %v", status, err)
}
```

Add a restart assertion by constructing a second coordinator over the same runtime after writing `SNES\n` to `CORENAME`, calling `Initialize`, and asserting `active`, system `snes`, and `game_id == nil`. Add negative HTTP assertions for incorrect token, unsupported system, missing ROM, and escaped path; after each, assert the active core is unchanged.

- [ ] **Step 2: Run the integration test and verify it fails**

Run: `go test ./internal/integration -run TestRemoteControlEndToEnd -v`

Expected: FAIL because the process entrypoint version package and test registry injection are not wired.

- [ ] **Step 3: Add the build version and agent entrypoint**

Create `internal/version/version.go`:

```go
package version

var Version = "dev"
```

Create `cmd/mister-agent/main.go` with this wiring order:

```go
func run(ctx context.Context, configPath string, logger *slog.Logger) error {
	cfg, err := agentconfig.Load(configPath)
	if err != nil { return err }
	paths := mister.Paths{MiSTerProcessComm: cfg.MiSTerProcessComm, CommandPipe: cfg.CommandPipe, CoreNameFile: cfg.CoreNameFile, MenuRBF: cfg.MenuRBF, MGLDirectory: cfg.MGLDirectory}
	registry := core.DefaultRegistry()
	runtime := mister.NewRuntime(paths, registry, mister.FileCommandWriter{Path: cfg.CommandPipe}, mister.ProcProcessChecker{Root: "/proc"}, 25*time.Millisecond)
	coordinator := agent.New(runtime, registry, 10*time.Second, 5*time.Second)
	startup, cancel := context.WithTimeout(ctx, 40*time.Second)
	coordinator.Initialize(startup)
	cancel()
	handler := httpapi.New(coordinator, cfg.Token, version.Version, logger)
	server := &http.Server{Addr: cfg.ListenAddress, Handler: handler, ReadHeaderTimeout: 2*time.Second, ReadTimeout: 15*time.Second, WriteTimeout: 15*time.Second, IdleTimeout: 30*time.Second}
	go func() { <-ctx.Done(); shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second); defer cancel(); _ = server.Shutdown(shutdown) }()
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) { return nil }
	return err
}
```

The 40-second reconciliation budget leaves five seconds inside the 45-second cold-boot acceptance window for HTTP startup and the health request. `main` must parse only `--config`, default it to `/media/fat/mister-remote/agent.toml`, create a JSON `slog` logger on standard output, use `signal.NotifyContext` for `SIGINT` and `SIGTERM`, call `run`, and exit non-zero on error without printing the token.

- [ ] **Step 4: Add the CLI entrypoint**

Create `cmd/misterctl/main.go`:

```go
package main

import (
	"context"
	"net/http"
	"os"

	"github.com/clawzai2-tech/mister-remote/host"
	"github.com/clawzai2-tech/mister-remote/internal/cli"
)

func main() {
	open := func(path string) (cli.Library, error) { return host.Open(path, (*http.Client)(nil)) }
	os.Exit(cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, open))
}
```

- [ ] **Step 5: Verify registry ownership at the process boundary**

In both the integration fixture and agent entrypoint, construct one registry and pass the same value to the runtime and coordinator:

```go
registry := core.DefaultRegistry()
runtime := mister.NewRuntime(paths, registry, writer, processChecker, 25*time.Millisecond)
coordinator := agent.New(runtime, registry, 10*time.Second, 5*time.Second)
```

In the integration fixture, replace `DefaultRegistry` with `core.NewRegistry(megaSpec, snesSpec)` after setting the two temporary ROM roots. Assert that target TOML containing a `cores` key is rejected by `agentconfig.Load`, proving the registry cannot be changed remotely.

- [ ] **Step 6: Add build targets**

Extend `Makefile`:

```make
VERSION ?= 0.1.0
LDFLAGS = -s -w -X github.com/clawzai2-tech/mister-remote/internal/version.Version=$(VERSION)

.PHONY: build build-cli build-agent

build: build-cli build-agent

build-cli:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/misterctl ./cmd/misterctl

build-agent:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/mister-agent-linux-armv7 ./cmd/mister-agent
```

- [ ] **Step 7: Run end-to-end and complete repository checks**

Run:

```bash
go test -race ./...
go vet ./...
make build VERSION=0.1.0
file bin/misterctl bin/mister-agent-linux-armv7
```

Expected:

- All tests pass.
- `misterctl` reports a Mach-O arm64 executable.
- `mister-agent-linux-armv7` reports a statically linked 32-bit ARM ELF executable.

- [ ] **Step 8: Commit executable wiring**

```bash
git add Makefile cmd internal/core internal/integration internal/version
git commit -m "feat: wire MiSTer remote executables"
```

---

### Task 10: Reproducible POC 1A Package and Safe Stock-Image Installer

**Files:**
- Create: `internal/packagepoc/archive.go`
- Test: `internal/packagepoc/archive_test.go`
- Create: `cmd/package-poc1a/main.go`
- Create: `deploy/poc1a/agent.toml.example`
- Create: `deploy/poc1a/start-agent.sh`
- Create: `deploy/poc1a/user-startup.snippet.sh`
- Create: `deploy/poc1a/MiSTer.ini.fragment`
- Create: `scripts/package-poc1a.sh`
- Create: `scripts/install-poc1a.sh`
- Create: `docs/runbooks/poc1a-deploy.md`
- Modify: `Makefile`

**Interfaces:**
- Consumes: `bin/mister-agent-linux-armv7`, the target config schema, and stock MiSTer `user-startup.sh` support.
- Produces: `dist/mister-remote-poc1a-0.1.0.tar.gz`, its SHA-256 file, a supervised stock deployment, an idempotent installation path, and a recovery runbook.

- [ ] **Step 1: Create the secret-free deployment templates**

Create `deploy/poc1a/agent.toml.example` exactly as follows:

```toml
listen_address = "0.0.0.0:8182"
token = "example-poc-token-not-valid"
mister_process_comm = "MiSTer"
command_pipe = "/dev/MiSTer_cmd"
core_name_file = "/tmp/CORENAME"
menu_rbf = "/media/fat/menu.rbf"
mgl_directory = "/tmp/mister-remote"
```

Create `deploy/poc1a/start-agent.sh` as POSIX shell:

```sh
#!/bin/sh
set -eu

base=/media/fat/mister-remote
pidfile=/tmp/mister-agent-supervisor.pid
logfile=/tmp/mister-agent.log

if [ -f "$pidfile" ] && kill -0 "$(cat "$pidfile")" 2>/dev/null; then
  exit 0
fi

echo "$$" > "$pidfile"
trap 'rm -f "$pidfile"' EXIT INT TERM

while true; do
  "$base/mister-agent" --config "$base/agent.toml" >> "$logfile" 2>&1 || true
  sleep 1
done
```

Create `deploy/poc1a/user-startup.snippet.sh`:

```sh
# BEGIN mister-remote
/media/fat/mister-remote/start-agent.sh &
# END mister-remote
```

Create `deploy/poc1a/MiSTer.ini.fragment`:

```ini
[MiSTer]
logo=0
fb_terminal=0
osd_timeout=5
video_off=1
video_off_logo=0
```

- [ ] **Step 2: Write the failing deterministic-archive test**

Create `internal/packagepoc/archive_test.go`. Build the same temporary source tree twice with deliberately different source-file modification times, then assert byte-identical archives and normalized headers:

```go
func TestCreateIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "mister-remote")
	if err := os.MkdirAll(root, 0o755); err != nil { t.Fatal(err) }
	files := map[string]string{"mister-agent": "binary", "agent.toml": "token", "start-agent.sh": "#!/bin/sh\n", "MiSTer.ini.fragment": "[MiSTer]\n"}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil { t.Fatal(err) }
	}
	fixed := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	one, two := filepath.Join(dir, "one.tar.gz"), filepath.Join(dir, "two.tar.gz")
	if err := packagepoc.Create(root, one, fixed); err != nil { t.Fatal(err) }
	if err := os.Chtimes(filepath.Join(root, "agent.toml"), time.Now(), time.Now()); err != nil { t.Fatal(err) }
	if err := packagepoc.Create(root, two, fixed); err != nil { t.Fatal(err) }
	a, _ := os.ReadFile(one)
	b, _ := os.ReadFile(two)
	if !bytes.Equal(a, b) { t.Fatal("archives differ") }
	entries := readArchiveHeaders(t, one)
	want := []string{"mister-remote/MiSTer.ini.fragment", "mister-remote/agent.toml", "mister-remote/mister-agent", "mister-remote/start-agent.sh"}
	if !slices.Equal(entryNames(entries), want) { t.Fatalf("entries = %v", entryNames(entries)) }
	for _, header := range entries {
		if header.Uid != 0 || header.Gid != 0 || !header.ModTime.Equal(fixed) { t.Fatalf("header = %#v", header) }
	}
}

func readArchiveHeaders(t *testing.T, path string) []*tar.Header {
	t.Helper()
	f, err := os.Open(path)
	if err != nil { t.Fatal(err) }
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil { t.Fatal(err) }
	defer gz.Close()
	reader := tar.NewReader(gz)
	var headers []*tar.Header
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) { return headers }
		if err != nil { t.Fatal(err) }
		copy := *header
		headers = append(headers, &copy)
	}
}

func entryNames(headers []*tar.Header) []string {
	names := make([]string, len(headers))
	for i, header := range headers { names[i] = header.Name }
	return names
}
```

- [ ] **Step 3: Run the archive test and verify it fails**

Run: `go test ./internal/packagepoc -run TestCreateIsDeterministic -v`

Expected: FAIL because package `internal/packagepoc` does not exist.

- [ ] **Step 4: Implement the deterministic Go archiver and package script**

Create `internal/packagepoc/archive.go` with this boundary:

```go
func Create(sourceDirectory, outputPath string, fixedTime time.Time) error
```

Implement it around an exact allowlist, rather than archiving arbitrary staging contents:

```go
var allowedFiles = map[string]int64{
	"MiSTer.ini.fragment": 0o600,
	"agent.toml":          0o600,
	"mister-agent":        0o755,
	"start-agent.sh":      0o755,
}
```

Read `sourceDirectory` with `os.ReadDir`, require its sorted names to equal the sorted allowlist exactly, and reject symlinks, subdirectories, device files, and non-regular files by checking `entry.Info().Mode().IsRegular()`. Create a temporary output in `filepath.Dir(outputPath)`, set its mode to `0600`, write and close tar then gzip in error-preserving order, `Sync` and close the file, and rename it over `outputPath` only after every step succeeds. Remove the temporary file on failure. Set `gzip.Header.ModTime` to `fixedTime`, set gzip OS to `255`, and leave gzip name/comment empty. For every tar header, prefix `mister-remote/`, set UID/GID to zero, clear user/group names, set access/change/modification times to `fixedTime`, use USTAR format, and use the allowlisted normalized mode. Add negative table tests for a missing allowlisted file, an extra file, a subdirectory, and a symlink.

Create `cmd/package-poc1a/main.go` to parse `--source`, `--output`, and RFC3339 `--time`, call `packagepoc.Create`, and exit non-zero on validation or archive error.

`scripts/package-poc1a.sh` must:

1. Require `MISTER_TOKEN` to match `^[A-Za-z0-9_-]{32,128}$`.
2. Require `VERSION`, defaulting to `0.1.0`.
3. Run the ARMv7 build.
4. Create a fresh `dist/staging/mister-remote` directory.
5. Copy the binary, startup script, INI fragment, and rendered `agent.toml` there.
6. Set executable files to mode `0755` and config to `0600` in the archive.
7. Run `go run ./cmd/package-poc1a --source dist/staging/mister-remote --output "$archive" --time 2026-08-01T00:00:00Z`.
8. Write `shasum -a 256` output beside the archive.
9. Fail if `find dist/staging -type f` detects ROM, RBF, or controller-map extensions.

The token substitution is safe because the accepted alphabet excludes the `sed` delimiter:

```sh
sed "s|example-poc-token-not-valid|$MISTER_TOKEN|" deploy/poc1a/agent.toml.example > dist/staging/mister-remote/agent.toml
```

Run: `sh -n scripts/package-poc1a.sh deploy/poc1a/start-agent.sh deploy/poc1a/user-startup.snippet.sh`

Expected: no output and exit zero.

- [ ] **Step 5: Write an idempotent, backup-first installer**

`scripts/install-poc1a.sh` must require `MISTER_TARGET` and a package path, verify the adjacent SHA-256 file locally, upload to `/tmp`, and execute a remote POSIX shell installer that:

1. Creates `/media/fat/mister-remote`.
2. Copies an existing `/media/fat/linux/user-startup.sh` to `/media/fat/linux/user-startup.sh.pre-mister-remote` only when the backup does not already exist.
3. Copies an existing `/media/fat/MiSTer.ini` to `/media/fat/MiSTer.ini.pre-mister-remote` only when the backup does not already exist.
4. Installs the package files and enforces executable modes.
5. Removes any previous text between `# BEGIN mister-remote` and `# END mister-remote` in `user-startup.sh`, then appends the committed snippet once.
6. Replaces existing values for `logo`, `fb_terminal`, `osd_timeout`, `video_off`, and `video_off_logo`, then appends the five committed values once under the existing `[MiSTer]` section.
7. Creates `/media/fat/games/SNES/.mister-remote-invalid.txt` as the non-ROM invalid-extension acceptance sentinel.
8. Starts `start-agent.sh` in the background without rebooting.

The script must never copy, rename, inspect, or hash a ROM file.

- [ ] **Step 6: Write installer and archive static tests**

Add a `package-test` target that runs:

```bash
sh -n scripts/package-poc1a.sh scripts/install-poc1a.sh deploy/poc1a/start-agent.sh deploy/poc1a/user-startup.snippet.sh
tar -tzf dist/mister-remote-poc1a-0.1.0.tar.gz
! tar -tzf dist/mister-remote-poc1a-0.1.0.tar.gz | rg -i '\.(rom|bin|gen|md|sfc|smc|rbf|map)$'
```

The archive listing must contain exactly the agent binary, `agent.toml`, `start-agent.sh`, and `MiSTer.ini.fragment` beneath one `mister-remote/` directory. Add this reproducibility assertion to `package-test`:

```bash
first_archive="$(mktemp -t mister-remote-poc1a.XXXXXX)"
trap 'rm -f "$first_archive"' EXIT
cp dist/mister-remote-poc1a-0.1.0.tar.gz "$first_archive"
MISTER_TOKEN=abcdefghijklmnopqrstuvwxyzABCDEF VERSION=0.1.0 ./scripts/package-poc1a.sh
cmp "$first_archive" dist/mister-remote-poc1a-0.1.0.tar.gz
```

- [ ] **Step 7: Write the deployment and recovery runbook**

`docs/runbooks/poc1a-deploy.md` must include:

- Confirm the dedicated MiSTer Pi, 128 MB SDRAM, wired Ethernet, HDMI, SD card, and single wired Xbox-compatible controller.
- Update and manually verify the stock MiSTer installation before installing the daemon.
- Ensure exactly one `MegaDrive_*.rbf` and one `SNES_*.rbf` are present under `/media/fat/_Console`.
- Put the lawful test ROMs at the two manifest paths without copying them into this repository.
- Complete the stock controller mapping workflow and verify both games manually once.
- Generate the token, package, install, check health, run HIL, collect inventory, and restore backups.
- Recover by stopping the supervisor, moving the `.pre-mister-remote` files back, removing `/media/fat/mister-remote`, and rebooting.
- Reflash the dedicated SD card if normal restoration fails; the SuperStation One remains untouched.

- [ ] **Step 8: Build and inspect the package locally**

Run:

```bash
MISTER_TOKEN=abcdefghijklmnopqrstuvwxyzABCDEF VERSION=0.1.0 make package-poc1a
shasum -a 256 -c dist/mister-remote-poc1a-0.1.0.tar.gz.sha256
make package-test
```

Expected: checksum PASS, exact allowlisted archive contents, and no forbidden extensions.

- [ ] **Step 9: Commit packaging and deployment tooling**

```bash
git add Makefile cmd/package-poc1a internal/packagepoc deploy/poc1a scripts/package-poc1a.sh scripts/install-poc1a.sh docs/runbooks/poc1a-deploy.md
git commit -m "build: package stock MiSTer behavior proof"
```

---

### Task 11: Hardware Acceptance Runner and Deterministic Inventory

**Files:**
- Create: `internal/hil/runner.go`
- Test: `internal/hil/runner_test.go`
- Create: `cmd/mister-hil/main.go`
- Create: `deploy/poc1a/inventory.sh`
- Create: `scripts/capture-poc1a-lock.sh`
- Create: `scripts/verify-poc1a-lock.sh`
- Modify: `Makefile`

**Interfaces:**
- Consumes: `host.Client`, the two validated manifest games, the daemon restart supervision from Task 10, and the target's stock binaries and root filesystem.
- Produces: `hil.Runner`, `hil.Report`, `mister-hil`, deterministic `build/sources.poc1a.lock.toml`, and target comparison scripts.

- [ ] **Step 1: Write failing acceptance-runner tests**

Define these test fakes:

```go
type fakeAPI struct {
	health protocol.Health
	status protocol.Status
	launches []protocol.LaunchRequest
	launchResult map[protocol.System]protocol.Status
	errors map[string]*protocol.APIError
}

type yesPrompter struct { prompts []string }
func (p *yesPrompter) Confirm(message string) (bool, error) { p.prompts = append(p.prompts, message); return true, nil }
```

Write `TestRunnerPassesCompletePOC1ASequence` with exactly two games. It must assert:

- health becomes ready before the injected 45-second deadline;
- launch order is Mega Drive, SNES, Mega Drive, SNES, Mega Drive, SNES, Mega Drive, SNES, Mega Drive, SNES;
- manual prompts mention HDMI video, HDMI audio, playable screen, and controller for each system once;
- invalid token, unsupported system, invalid extension sentinel, missing ROM, and escaped path cases expect the design codes;
- status after every invalid request retains the same observed core;
- stop observes idle and prompts for black HDMI;
- restart reconciliation observes the active core with nil game ID;
- the report contains no bearer token and records every machine and manual result.

Use injected `Now` and `Sleep` functions so the test completes immediately. Write `TestRunnerRecordsDeclinedManualCheck` with a prompter that returns false for `SNES HDMI audio`; assert `Report.Passed` is false and the named check is present with `Passed: false`.

Make the launch-order assertion concrete:

```go
var gotSystems []protocol.System
for _, launch := range api.launches {
	if launch.GameID == "megadrive-test" || launch.GameID == "snes-test" {
		gotSystems = append(gotSystems, launch.System)
	}
}
wantSystems := []protocol.System{
	protocol.SystemMegaDrive, protocol.SystemSNES,
	protocol.SystemMegaDrive, protocol.SystemSNES,
	protocol.SystemMegaDrive, protocol.SystemSNES,
	protocol.SystemMegaDrive, protocol.SystemSNES,
	protocol.SystemMegaDrive, protocol.SystemSNES,
}
if !slices.Equal(gotSystems, wantSystems) {
	t.Fatalf("launch order = %v, want %v", gotSystems, wantSystems)
}
encoded, err := json.Marshal(report)
if err != nil { t.Fatal(err) }
if bytes.Contains(encoded, []byte("test-token")) || bytes.Contains(encoded, []byte("/media/fat/games")) {
	t.Fatalf("report leaked a token or ROM path: %s", encoded)
}
```

- [ ] **Step 2: Run HIL tests and verify they fail**

Run: `go test ./internal/hil -v`

Expected: FAIL because package `internal/hil` does not exist.

- [ ] **Step 3: Implement the acceptance runner**

Create these exact public types in `internal/hil/runner.go`:

```go
type API interface {
	Health(context.Context) (protocol.Health, error)
	Status(context.Context) (protocol.Status, error)
	Launch(context.Context, protocol.LaunchRequest) (protocol.Status, error)
	Stop(context.Context) (protocol.Status, error)
}

type Prompter interface { Confirm(string) (bool, error) }

type Check struct {
	Name string `json:"name"`
	Passed bool `json:"passed"`
	Detail string `json:"detail"`
}

type Report struct {
	StartedAt time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Checks []Check `json:"checks"`
	Passed bool `json:"passed"`
}

type Runner struct {
	API API
	UnauthorizedAPI API
	Games []host.Game
	Prompt Prompter
	Now func() time.Time
	Sleep func(context.Context, time.Duration) error
}

func (r Runner) Run(context.Context) (Report, error)
```

Use the selected SNES game's `ROMPath` for the unsupported-system and invalid-token requests, then use these fixed filesystem probes:

```go
protocol.LaunchRequest{GameID: "unsupported-test", System: "nes", ROMPath: snesGame.ROMPath}
protocol.LaunchRequest{GameID: "invalid-extension", System: protocol.SystemSNES, ROMPath: "/media/fat/games/SNES/.mister-remote-invalid.txt"}
protocol.LaunchRequest{GameID: "missing-rom", System: protocol.SystemSNES, ROMPath: "/media/fat/games/SNES/__mister_remote_missing__.sfc"}
protocol.LaunchRequest{GameID: "escaped-rom", System: protocol.SystemSNES, ROMPath: "/media/fat/games/SNES/../../MiSTer"}
```

Run the sequence in this exact order:

1. Require exactly one manifest game for each supported system, then poll health until ready or 45 seconds elapse.
2. Alternate Mega Drive then SNES five times. After the first successful launch of each system, prompt once each for HDMI video, HDMI audio, a playable screen, and controller operation.
3. While the final SNES launch remains active, send invalid-token, unsupported-system, invalid-extension, missing-ROM, and escaped-path requests. After each expected API error, fetch status through the authenticated client and require the observed core to remain `SNES`.
4. Prompt the operator to restart only `mister-agent`; after confirmation, poll status for ten seconds and require `active`, system `snes`, observed core `SNES`, and a nil game ID.
5. Stop, require idle, and prompt for a black HDMI output.

Record a `Check` for every machine assertion and prompt. `Report.Passed` is true only when every check passed. Do not record request tokens or ROM paths in `Report.Detail`. Return a report even when a check fails, so the entrypoint can persist evidence before returning non-zero.

- [ ] **Step 4: Implement the MacBook HIL entrypoint**

`cmd/mister-hil/main.go` must accept `--config` and `--output`, load the manifest and connection config, create one normal client and one client whose token is `deliberately-invalid-poc-token`, prompt through standard input/output, and write indented JSON using mode `0600`. Default output is `artifacts/hil/poc1a.json`. At the restart checkpoint it must display:

```text
Restart mister-agent on the target now. From a development SSH shell, run: killall mister-agent
```

The instruction is display-only; the HIL binary must not invoke SSH. After the operator confirms, poll authenticated status for up to 10 seconds and require the rediscovered active core plus `game_id: null`.

- [ ] **Step 5: Write deterministic target inventory**

Create `deploy/poc1a/inventory.sh` as POSIX shell. It must fail unless exactly one file matches each core pattern and must emit stable TOML with no timestamp. Use this emitter for every artifact:

```sh
#!/bin/sh
set -eu
LC_ALL=C
export LC_ALL

escape_toml() { printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'; }
emit_artifact() {
  artifact_name="$1"
  artifact_path="$2"
  artifact_source="$3"
  artifact_digest="$(sha256sum "$artifact_path" | awk '{print $1}')"
  artifact_size="$(stat -c '%s' "$artifact_path")"
  printf '[[artifacts]]\nname = "%s"\npath = "%s"\nsha256 = "%s"\nsize = %s\nsource = "%s"\n\n' \
    "$(escape_toml "$artifact_name")" "$(escape_toml "$artifact_path")" "$artifact_digest" "$artifact_size" "$(escape_toml "$artifact_source")"
}

printf 'format = 1\n\n'
emit_artifact main_mister /media/fat/MiSTer https://github.com/MiSTer-devel/Main_MiSTer
```

Resolve singleton globs without silently accepting an unmatched literal or multiple versions:

```sh
resolve_one() {
  resolve_pattern="$1"
  set -- $resolve_pattern
  if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then
    printf 'expected exactly one regular file matching %s\n' "$resolve_pattern" >&2
    exit 1
  fi
  printf '%s\n' "$1"
}

megadrive_core="$(resolve_one '/media/fat/_Console/MegaDrive_*.rbf')"
snes_core="$(resolve_one '/media/fat/_Console/SNES_*.rbf')"
```

Emit artifact blocks for:

- `/media/fat/MiSTer`
- `/media/fat/menu.rbf`
- `/media/fat/linux/zImage_dtb`
- the one resolved `/media/fat/_Console/MegaDrive_*.rbf`
- the one resolved `/media/fat/_Console/SNES_*.rbf`
- every `/media/fat/config/input_*_v3.map`, sorted by path

Emit the five base artifacts in the listed order using stable names `main_mister`, `menu`, `kernel`, `megadrive_core`, and `snes_core` plus their authoritative upstream repository URLs. Require at least one regular controller-map match, then emit the matches in sorted path order with a `controller_` name prefix and source `local MiSTer controller mapping`:

```sh
set -- /media/fat/config/input_*_v3.map
[ "$#" -ge 1 ] && [ -f "$1" ] || { printf 'no controller maps found\n' >&2; exit 1; }
controller_maps="$(printf '%s\n' "$@" | sort)"
printf '%s\n' "$controller_maps" | while IFS= read -r controller_map; do
  [ -f "$controller_map" ] || { printf 'controller map is not a regular file: %s\n' "$controller_map" >&2; exit 1; }
  controller_name="controller_$(basename "$controller_map")"
  emit_artifact "$controller_name" "$controller_map" 'local MiSTer controller mapping'
done
```

Emit `[runtime]` with the escaped result of `uname -r`. Extract the dynamic-library closure with the following rule, require at least one regular file, and emit `[[libraries]]` blocks in sorted path order with path, SHA-256, and size:

```sh
ldd_output="$(ldd /media/fat/MiSTer)" || { printf 'ldd failed for Main_MiSTer\n' >&2; exit 1; }
library_paths="$(printf '%s\n' "$ldd_output" | awk '{ for (i = 1; i <= NF; i++) if ($i ~ /^\//) print $i }' | sort -u)"
[ -n "$library_paths" ] || { printf 'no Main_MiSTer runtime libraries found\n' >&2; exit 1; }
printf '%s\n' "$library_paths" | while IFS= read -r library_path; do
  [ -f "$library_path" ] || { printf 'library is not a regular file: %s\n' "$library_path" >&2; exit 1; }
  library_digest="$(sha256sum "$library_path" | awk '{print $1}')"
  library_size="$(stat -c '%s' "$library_path")"
  printf '[[libraries]]\npath = "%s"\nsha256 = "%s"\nsize = %s\n\n' \
    "$(escape_toml "$library_path")" "$library_digest" "$library_size"
done
```

Do not inspect or emit any file under `/media/fat/games`.

- [ ] **Step 6: Write capture and verification wrappers**

`scripts/capture-poc1a-lock.sh` must require `MISTER_TARGET`, upload the committed inventory script to `/tmp/mister-remote-inventory.sh`, execute it, write to a temporary local file, verify it contains `format = 1`, all five named base artifacts, at least one controller map, and at least one library, then atomically rename it to `build/sources.poc1a.lock.toml`.

`scripts/verify-poc1a-lock.sh` must capture a fresh temporary inventory and run:

```bash
diff -u build/sources.poc1a.lock.toml "$fresh_inventory"
```

It exits non-zero on any artifact, controller-map, kernel, or library change.

- [ ] **Step 7: Run software checks for HIL and inventory tools**

Run:

```bash
go test -race ./internal/hil ./...
go vet ./...
sh -n deploy/poc1a/inventory.sh scripts/capture-poc1a-lock.sh scripts/verify-poc1a-lock.sh
make build VERSION=0.1.0
```

Expected: PASS. `build/sources.poc1a.lock.toml` does not exist yet because it is created only from the accepted hardware.

- [ ] **Step 8: Commit acceptance and inventory tooling**

```bash
git add Makefile cmd/mister-hil internal/hil deploy/poc1a/inventory.sh scripts/capture-poc1a-lock.sh scripts/verify-poc1a-lock.sh
git commit -m "test: add MiSTer hardware acceptance harness"
```

---

### Task 12: Deploy to the Dedicated MiSTer Pi and Accept POC 1A

**Files:**
- Create from hardware: `build/sources.poc1a.lock.toml`
- Modify only when observed behavior requires a reviewed correction: `docs/runbooks/poc1a-deploy.md`

**Interfaces:**
- Consumes: The complete POC 1A package, dedicated MiSTer Pi, two lawful preloaded test ROMs, wired controller, HDMI display, wired LAN, and development SSH.
- Produces: A passing hardware report in ignored `artifacts/hil/poc1a.json`, a committed deterministic source/runtime lock, and the evidence gate for planning POC 1B.

- [ ] **Step 1: Prepare and manually verify the stock baseline**

Follow `docs/runbooks/poc1a-deploy.md` on the dedicated MiSTer Pi. Before installing the daemon, verify through the stock UI:

- `/media/fat/games/MegaDrive/test.md` launches through the selected `MegaDrive` core;
- `/media/fat/games/SNES/test.sfc` launches through `SNES`;
- HDMI video and audio work for both;
- the wired controller is mapped and playable in both;
- exactly one matching RBF exists for each selected core.

If the lawful ROM filenames differ, update only the local `games.toml` and the target SD paths together; never add those names or files to Git.

- [ ] **Step 2: Generate the token, package, and install**

Run on the MacBook:

```bash
printf 'MiSTer SSH target (for example root@192.0.2.10): '
read -r MISTER_TARGET
MISTER_TOKEN="$(openssl rand -hex 32)"
export MISTER_TARGET MISTER_TOKEN
VERSION=0.1.0 make package-poc1a
./scripts/install-poc1a.sh "dist/mister-remote-poc1a-0.1.0.tar.gz"
```

Expected: installer reports both backup paths, package checksum, installed files, one startup marker, updated INI keys, and a running supervisor.

- [ ] **Step 3: Create the local host configuration without committing secrets**

Create `~/.config/mister-remote/config.toml` with mode `0600`, using the target IP and generated token, and create sibling `games.toml` with the two approved game IDs and target paths. Run:

```bash
bin/misterctl health
bin/misterctl games
```

Expected: `ready` and exactly the two configured games.

- [ ] **Step 4: Run the complete hardware acceptance sequence**

Power-cycle the MiSTer Pi, immediately start the HIL runner, and answer only observations actually seen on HDMI/controller hardware:

```bash
bin/mister-hil --config ~/.config/mister-remote/config.toml --output artifacts/hil/poc1a.json
```

Expected: exit zero and a report with `"passed": true`. Inspect it with:

```bash
python3 -m json.tool artifacts/hil/poc1a.json
```

- [ ] **Step 5: Verify independence from an interactive SSH session**

Close all SSH sessions, then run:

```bash
bin/misterctl launch megadrive-test
bin/misterctl launch snes-test
bin/misterctl stop
```

Expected: both games launch and stop reaches idle without opening SSH or touching the MiSTer controls.

- [ ] **Step 6: Capture and verify the known-good lock**

Run:

```bash
./scripts/capture-poc1a-lock.sh
./scripts/verify-poc1a-lock.sh
! rg -n '/media/fat/games|rom_path|token' build/sources.poc1a.lock.toml
```

Expected: verification PASS and the final command exits zero, proving the lock contains no ROM path or token.

- [ ] **Step 7: Run final software and package verification**

Run:

```bash
make check
make build VERSION=0.1.0
make package-test
git diff --check
git status --short
```

Expected: all checks pass; only `build/sources.poc1a.lock.toml` is untracked or modified.

- [ ] **Step 8: Commit the accepted hardware baseline**

```bash
git add build/sources.poc1a.lock.toml
git commit -m "test: lock accepted MiSTer Pi baseline"
git status --short --branch
```

Expected: clean worktree on the implementation branch.

---

## POC 1A Completion Gate

POC 1A is complete only when Task 12 passes on the dedicated MiSTer Pi and the deterministic lock is committed. At that point, write the POC 1B implementation plan using:

- the exact kernel image and hash;
- the exact `Main_MiSTer`, Menu, Mega Drive, and SNES artifacts;
- the measured `Main_MiSTer` dynamic-library closure;
- the accepted controller-map set;
- the unchanged v1 HTTP contract and POC 1A hardware runner.

The POC 1B plan must preserve the known-good binary kernel checkpoint before building the pinned kernel from source and must rerun the identical HIL sequence at both checkpoints.
